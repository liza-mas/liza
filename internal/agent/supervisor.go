package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/precommit"
)

// SupervisorConfig contains all configuration for the agent supervisor
type SupervisorConfig struct {
	AgentID          string
	Authority        models.AgentAuthority
	Role             string // runtime role name from pipeline YAML (e.g. "coder", "code-reviewer", "orchestrator")
	ProjectRoot      string
	StatePath        string
	LogPath          string
	SpecsDir         string // For prompt building
	CLIName, Model   string // CLI ("claude", "codex", ...); first-class model, "" for the tool default
	ProfileName      string // Optional structured launch profile name
	ProfileVars      map[string]string
	Interactive      bool          // Print prompt location, don't execute
	InitialTask      string        // Optional task ID the first claim targets; a successful claim spends it
	LLMAgent         LLMAgent      // Agent execution backend (CLI-based or future ACP-based)
	Executor         CLIExecutor   // Legacy field name, preserved for backward compatibility. Deprecated: Use LLMAgent.
	ExecutionTimeout time.Duration // Max time for agent execution before timeout
	// ExecutionProgressTimeout is the maximum time an executing task may go
	// without observable state, worktree, or provider-output progress.
	ExecutionProgressTimeout time.Duration
	sessionActivity          *providerSessionActivity
	// poolSurplus is set after registration for non-orchestrator roles; a
	// waiting strategy that passes it returns errPoolSurplus when it holds.
	poolSurplus surplusCheckFunc
}

var waitWhilePausedForSupervisor = waitWhilePaused
var newSupervisorDelayTimer = time.NewTimer // claim-failure pacing; overridable in tests

// readStateSnapshot is the lock-free read for supervisor observation and gate
// paths (D100): it returns ctx's error when already cancelled, as the locked
// ReadContext it replaces did, then reads the last published state.
func readStateSnapshot(ctx context.Context, bb *db.Blackboard) (*models.State, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return bb.ReadSnapshot()
}

// promptFailureBlockTarget names the task a prompt-context build failure must
// block. Doers report the task they claimed; reviewers hold a review claim
// rather than a doer claim, so claimedTaskID is empty for them and the task
// under review is the target. Without a target the failure escapes the
// supervisor loop and ends the session, and auto-repair respawns into the same
// failure instead of surfacing a BLOCKED task carrying the reason.
//
// This currently equals effectiveTask for all three strategies, and the
// narrowing is deliberate: blocking is an ownership-bearing write, so a future
// strategy that reports a taskID it does not own must opt in here rather than
// inherit the fallback.
func promptFailureBlockTarget(strategy RoleStrategy, claimedTaskID, taskID string) string {
	if claimedTaskID != "" {
		return claimedTaskID
	}
	if _, isReviewer := strategy.(*reviewerStrategy); isReviewer {
		return taskID
	}
	return ""
}

func successfulTurnProgressSignature(_, _, taskSnapshot string) string {
	return taskSnapshot
}

// --- Effective config helpers ---

func effectiveAgentProgressTimeout(cfg models.Config) time.Duration {
	seconds := cfg.AgentProgressTimeout
	if seconds <= 0 {
		seconds = models.DefaultAgentProgressTimeoutSec
	}
	return time.Duration(seconds) * time.Second
}

// RunSupervisor is the main entry point for the agent supervisor.
func RunSupervisor(ctx context.Context, config SupervisorConfig) error {
	lizaPaths := paths.New(config.ProjectRoot)
	supervisorCtx, cancelSupervisor := context.WithCancel(ctx)
	defer cancelSupervisor()
	bb := db.For(config.StatePath).WithLockRetryContext(supervisorCtx)

	// Validate identity, then withhold denied host variables (ADR-0213)
	if err := validateIdentityAndWithholdEnvironment(config.AgentID, config.Role, config.ProjectRoot); err != nil {
		return err
	}

	// Load pipeline resolver for role type classification.
	// The resolver reads role definitions from the pipeline YAML,
	// enabling custom YAML-defined roles without Go code changes.
	pipelineCfg, pipelineErr := pipeline.LoadFrozen(config.ProjectRoot)
	if pipelineErr != nil {
		return fmt.Errorf("loading pipeline config for strategy selection: %w", pipelineErr)
	}
	resolver := pipeline.NewResolver(pipelineCfg)

	roleType, err := resolver.RoleType(config.Role)
	if err != nil {
		return fmt.Errorf("resolving role type for %q: %w", config.Role, err)
	}

	if roleType == "orchestrator" {
		defer drainPendingCheckpointSummary(ctx, bb, config.ProjectRoot)
	}

	strategy, err := NewRoleStrategy(config.Role, resolver)
	if err != nil {
		return err
	}

	// Apply YAML-sourced timeouts from pipeline config to the strategy.
	if timeouts, tErr := resolver.RoleTimeouts(config.Role); tErr == nil {
		ApplyYAMLTimeouts(strategy, timeouts.Execution, timeouts.PollInterval, timeouts.MaxWait)
	}

	if err := ops.ReplayPendingVerdicts(supervisorCtx, config.ProjectRoot, config.AgentID); err != nil {
		return fmt.Errorf("replay reached verdicts before registration: %w", err)
	}
	authority, err := registerAgentWithAuthority(bb, config.ProjectRoot, config.AgentID, config.Role, "terminal-1", 1800, config.CLIName, config.Model, resolver)
	if err != nil {
		return err
	}
	config.Authority = authority
	// Registration enforces orchestrator singularity, so only the registered
	// orchestrator repairs hooks; a rejected duplicate launch never gets here.
	if roleType == "orchestrator" {
		ensureProjectRootIndexActivation(bb, config.ProjectRoot)
	} else {
		config.poolSurplus = func(state *models.State) bool {
			return poolSurplus(state, resolver, authority.ID, time.Now().UTC())
		}
	}
	// retired is set once releaseIfSurplus has removed this registration; the
	// deferred unregister would then fail its authority check on the missing row.
	retired := false
	defer func() {
		if retired {
			return
		}
		// Shutdown cleanup has an independent bounded context.
		cleanupBB, cancelCleanup := supervisorCleanupBlackboard(config.StatePath)
		defer cancelCleanup()
		if err := unregisterAgent(cleanupBB, authority, config.ProjectRoot); err != nil {
			GetLogger().Warn("Failed to unregister agent", "error", err, "agent_id", authority.ID)
		}
	}()

	state, err := bb.ReadSnapshot()
	if err != nil {
		return fmt.Errorf("failed to read state: %w", err)
	}

	// Start supervisor-lifetime heartbeat to keep the lease alive across
	// the entire loop (including IDLE wait-for-work periods, not just
	// during CLI execution). Without this, an IDLE agent's lease can
	// expire, causing auto-assigned ID collision with new agents.
	heartbeatErrCh := make(chan error, 1)
	config.sessionActivity = &providerSessionActivity{}

	hb := NewHeartbeat(HeartbeatConfig{
		Authority:          config.Authority,
		StatePath:          config.StatePath,
		State:              state,
		ActiveProviderTask: config.sessionActivity.currentTask,
	})

	stopHeartbeat := startSupervisorHeartbeat(supervisorCtx, hb.Start, func(err error) {
		GetLogger().Error("Heartbeat error", "error", err, "agent_id", config.AgentID)
		select {
		case heartbeatErrCh <- err:
		default:
		}
		cancelSupervisor()
	})
	defer stopHeartbeat()
	checkHeartbeat := func() error {
		select {
		case err := <-heartbeatErrCh:
			return fmt.Errorf("heartbeat stopped for agent %s: %w", config.AgentID, err)
		default:
			return nil
		}
	}
	waitForSupervisorDelay := func(delay time.Duration) error {
		timer := newSupervisorDelayTimer(delay)
		defer timer.Stop()

		select {
		case <-supervisorCtx.Done():
			if hbErr := checkHeartbeat(); hbErr != nil {
				return hbErr
			}
			if ctx.Err() != nil {
				return nil
			}
			return supervisorCtx.Err()
		case <-timer.C:
			return nil
		}
	}

	// retireIfSurplus removes this registration when the role is over its
	// max-instances and the agent holds no in-flight work. A failed check
	// leaves the registration and claims untouched, so the agent stays.
	retireIfSurplus := func() bool {
		if config.poolSurplus == nil {
			return false
		}
		released, err := releaseIfSurplus(bb, authority, resolver)
		if err != nil {
			GetLogger().Warn("Surplus retirement check failed; staying in pool", "error", err, "agent_id", config.AgentID)
			return false
		}
		if released {
			GetLogger().Info("Role over max-instances, supervisor exiting", "agent_id", config.AgentID, "role", config.Role)
		}
		return released
	}

	pollInterval, maxWait := strategy.WaitConfig(state)

	// Set execution timeout if not configured
	if config.ExecutionTimeout == 0 {
		config.ExecutionTimeout = strategy.DefaultTimeout()
	}
	if config.ExecutionProgressTimeout == 0 {
		config.ExecutionProgressTimeout = effectiveAgentProgressTimeout(state.Config)
	}

	exit42Tracker := newExit42RestartTracker()
	crashTracker := newCrashRestartTracker()
	consecutiveCrashes := 0 // across tasks; any exit 0 or 42 resets it
	spinTracker := newSpinningTracker()
	runtimeFailureTracker, backgroundTaskTracker := newRuntimeFailureTracker(), newRuntimeFailureTracker()
	initialTaskSpent := false

	for {
		// A claim spends InitialTask whichever path ends the iteration, so a
		// retry or a block cannot leave the next wait, claim or prompt on it.
		if initialTaskSpent {
			config.InitialTask = ""
		}
		if err := checkHeartbeat(); err != nil {
			return err
		}
		// Check context cancellation (signal received)
		if supervisorCtx.Err() != nil {
			if hbErr := checkHeartbeat(); hbErr != nil {
				return hbErr
			}
			if ctx.Err() == nil {
				return supervisorCtx.Err()
			}
			GetLogger().Info("Signal received, shutting down")
			return nil
		}

		// Check ABORT
		if checkAbort(config.ProjectRoot) {
			GetLogger().Info("ABORT signal received, system shutting down")
			return nil
		}

		if handleProviderUnavailableSignal(config) {
			return nil
		}

		if handleQuotaSignal(config) {
			return nil
		}

		// Wait while PAUSE/CHECKPOINT
		if stop, err := awaitRoleGate(supervisorCtx, config.ProjectRoot, roleType, checkHeartbeat); stop {
			return err
		}

		// Pre-work (reviewer: merge handling; others: no-op)
		shouldContinue, err := strategy.PreWork(supervisorCtx, bb, config)
		if err != nil {
			if hbErr := checkHeartbeat(); hbErr != nil {
				return hbErr
			}
			return err
		}
		if shouldContinue {
			continue
		}

		// Wait for work. Each task boundary passes through here, and the wait
		// checks surplus before work on every state read, so a surplus agent
		// retires instead of claiming even when work is waiting.
		hasWork, err := strategy.WaitForWork(supervisorCtx, bb, config, pollInterval, maxWait)
		if errors.Is(err, errPoolSurplus) {
			if retired = retireIfSurplus(); retired {
				return nil
			}
			// The locked recheck disagreed with the snapshot (a peer left or the
			// cap rose meanwhile); pace the retry so a persistent disagreement
			// cannot spin.
			if err := waitForSupervisorDelay(5 * time.Second); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			if hbErr := checkHeartbeat(); hbErr != nil {
				return hbErr
			}
			return err
		}
		if !hasWork {
			GetLogger().Info("No work available, supervisor exiting")
			return nil
		}

		// Work can become available after a pause while WaitForWork is parked.
		// Recheck the role-aware gate before claiming; existing providers are
		// unaffected, and transition checkpoints retain their role exceptions.
		if stop, err := awaitRoleGate(supervisorCtx, config.ProjectRoot, roleType, checkHeartbeat); stop {
			return err
		}
		if supervisorCtx.Err() != nil {
			continue // Preserve the existing cancellation/heartbeat exit handling.
		}
		if checkAbort(config.ProjectRoot) {
			GetLogger().Info("ABORT signal received, system shutting down")
			return nil
		}

		// Claim task
		taskID, claimedTaskID, err := strategy.ClaimTask(config, bb)
		if err != nil {
			if errors.Is(err, ErrAgentDegraded) {
				GetLogger().Error("Agent marked degraded, exiting supervisor", "agent_id", config.AgentID, "role", config.Role, "error", err)
				return nil
			}
			if ops.IsAgentAuthorityError(err) {
				GetLogger().Info("Agent authority lost, exiting supervisor", "agent_id", config.AgentID, "role", config.Role, "error", err)
				return nil
			}
			delay := 5 * time.Second
			// Breaker-owning strategies pace and stop the loop; a halt refusal is not a claim failure.
			if observer, ok := strategy.(claimFailureObserver); ok && !errors.Is(err, ops.ErrSystemHalted) {
				decision := observer.ObserveClaimFailure(err)
				if decision.Stop {
					return nil
				}
				delay = decision.Delay
			}
			if err := waitForSupervisorDelay(delay); err != nil {
				return err
			}
			continue
		}
		initialTaskSpent = true

		// Pre-execution (orchestrator: PLANNING status; others: no-op)
		if err := strategy.PreExecution(bb, config); err != nil {
			GetLogger().Warn("Pre-execution failed", "error", err, "agent_id", config.AgentID)
		}

		// Build and save prompt
		stateBefore, err := readStateSnapshot(supervisorCtx, bb)
		if err != nil {
			if supervisorCtx.Err() != nil {
				continue // Preserve the existing cancellation/heartbeat exit handling.
			}
			return fmt.Errorf("failed to read state for prompt: %w", err)
		}
		if orchestrator, ok := strategy.(*orchestratorStrategy); ok {
			launch, err := orchestrator.RevalidateWake(supervisorCtx, bb, stateBefore, config)
			if err != nil {
				return err
			}
			if !launch {
				continue
			}
		}

		// Spinning detection: track re-executions for the same task.
		effectiveTask := claimedTaskID
		if effectiveTask == "" {
			effectiveTask = taskID
		}
		if effectiveTask != "" {
			if blockSpinningTask(bb, config, resolver, stateBefore, effectiveTask, spinTracker) {
				continue
			}
		} else if stopSpinningOrchestrator(config, stateBefore, spinTracker) {
			return nil
		}

		if reviewer, ok := strategy.(*reviewerStrategy); ok {
			if err := ensureReviewerPromptClaimed(stateBefore, config.AgentID, taskID, reviewer.resolver); err != nil {
				return fmt.Errorf("stale reviewer claim before prompt launch: %w", err)
			}
		}

		prompt, err := strategy.BuildPrompt(stateBefore, config, taskID)
		if err != nil {
			// Reviewers hold a review claim, not a doer claim, so claimedTaskID
			// is empty for them. Without this the failure escapes the loop and
			// kills the supervisor, and auto-repair respawns into the same
			// failure instead of surfacing a BLOCKED task with its reason.
			blockTarget := promptFailureBlockTarget(strategy, claimedTaskID, taskID)
			if blockTarget != "" && errors.Is(err, precommit.ErrContextBuild) {
				reason := fmt.Sprintf("prompt context build failed: %v", err)
				GetLogger().Warn("Task blocked due to prompt-context build failure",
					"agent_id", config.AgentID,
					"task_id", blockTarget,
					"error", err)
				if blockErr := blockTaskFromSupervisor(bb, config.ProjectRoot, blockTarget, config.Authority, reason); blockErr != nil {
					GetLogger().Warn("Failed to block task from supervisor", "error", blockErr, "task_id", blockTarget)
				}
				spinTracker.reset(effectiveTask)
				continue
			}
			return fmt.Errorf("failed to build prompt: %w", err)
		}

		promptFile, err := savePrompt(lizaPaths.AgentPromptsDir(), config.AgentID, prompt)
		if err != nil {
			return fmt.Errorf("failed to save prompt: %w", err)
		}
		GetLogger().Info("Prompt saved", "file", promptFile)

		// Execute agent
		exitCode, currentOutput, err := executeAgent(supervisorCtx, config, prompt, nil, effectiveTask, stateBefore.Config)
		if halted := errors.Is(err, errLaunchHalted); halted || errors.Is(err, errReviewOwnershipLost) || errors.Is(err, errLaunchContended) {
			// No provider turn ran to completion: neither a crash nor a turn
			// without progress, so no loop budget is charged.
			if releaseErr := releaseUnstartedTurn(bb, config, effectiveTask, halted, errors.Is(err, errLaunchContended)); releaseErr != nil {
				if ops.IsAgentAuthorityError(releaseErr) {
					return nil
				}
				return releaseErr
			}
			spinTracker.reset(effectiveTask)
			crashTracker.reset(effectiveTask)
			exit42Tracker.reset(taskID)
			runtimeFailureTracker.reset(effectiveTask)
			// Pace repeated claim/launch disagreement; a halt then parks at the gate.
			if err := waitForSupervisorDelay(pollInterval); err != nil {
				return err
			}
			continue
		}
		// Drain before ownership reset; cancellation retains pending verdicts.
		if supervisorCtx.Err() == nil {
			if replayErr := ops.ReplayPendingVerdicts(supervisorCtx, config.ProjectRoot, config.AgentID); replayErr != nil {
				err = errors.Join(err, fmt.Errorf("replay reached verdicts after provider exit: %w", replayErr))
			}
		}
		if err != nil {
			if hbErr := checkHeartbeat(); hbErr != nil {
				return errors.Join(err, hbErr)
			}
			return fmt.Errorf("agent execution error: %w", err)
		}
		// Preserve completed-turn bookkeeping when cancellation ends the loop.
		if supervisorCtx.Err() != nil {
			cleanupBB, cancelCleanup := supervisorCleanupBlackboard(config.StatePath)
			defer cancelCleanup()
			bb = cleanupBB
		}

		if exitCode == 0 && effectiveTask != "" {
			if failure := detectObservedRuntimeFailure(currentOutput); failure != nil {
				handleObservedRuntimeFailureRetry(bb, config, effectiveTask, stateBefore.Config, *failure, runtimeFailureTracker, spinTracker)
				if err := resetAgentAfterExit(bb, config.Authority, config.ProjectRoot); err != nil {
					GetLogger().Warn("Failed to reset agent status after runtime failure", "error", err, "agent_id", config.AgentID)
				}
				exit42Tracker.reset(taskID)
				crashTracker.reset(effectiveTask)
				consecutiveCrashes = 0
				continue
			}
		}

		commitQuotaTerminationWIP(supervisorCtx, config, exitCode, effectiveTask, currentOutput)
		// Reset runtime status after CLI exits, but preserve explicit command-driven
		// states such as WAITING and HANDOFF. The loop blocks below act after
		// this release, so they may only block the continuation it left.
		released, err := releaseAgentAfterExit(bb, config.Authority, config.ProjectRoot)
		if err != nil {
			GetLogger().Warn("Failed to reset agent status after exit", "error", err, "agent_id", config.AgentID)
		}

		if _, auditErr := handleProviderAuditDegraded(bb, config, effectiveTask, currentOutput); auditErr != nil {
			return auditErr
		}

		// Handle exit code
		switch exitCode {
		case 0:
			if effectiveTask != "" {
				runtimeFailureTracker.reset(effectiveTask)
			}

			if !preserveClaimForBackgroundJob(bb, config, effectiveTask, stateBefore.Config, currentOutput, released, backgroundTaskTracker, spinTracker) {
				GetLogger().Info("Agent completed, checking for more work")
				if err := strategy.PostExecution(bb, config, taskID, claimedTaskID, stateBefore); err != nil {
					GetLogger().Warn("Post-execution error", "error", err)
				}
			}
			exit42Tracker.reset(taskID)
			crashTracker.reset(effectiveTask)
			consecutiveCrashes = 0
		case 42:
			consecutiveCrashes = 0
			restartTaskID := claimedTaskID
			if restartTaskID == "" {
				restartTaskID = taskID
			}
			runtimeFailureTracker.reset(restartTaskID)

			outcome, trackErr := exit42Tracker.Handle(bb, config.ProjectRoot, config.Role, restartTaskID, config.Authority)
			if trackErr != nil {
				GetLogger().Warn("Exit-42 tracker failed, using default retry delay",
					"error", trackErr,
					"task_id", restartTaskID)
				if err := waitForSupervisorDelay(2 * time.Second); err != nil {
					return err
				}
				break
			}

			if outcome.BlockedTask {
				GetLogger().Warn("Task transitioned to BLOCKED after repeated exit 42 restarts",
					"task_id", restartTaskID,
					"restart_count", outcome.RestartCount)
				break
			}

			GetLogger().Info("Agent aborted gracefully, restarting",
				"exit_code", 42,
				"task_id", restartTaskID,
				"restart_count", outcome.RestartCount,
				"delay_seconds", int(outcome.Delay/time.Second))
			if err := waitForSupervisorDelay(outcome.Delay); err != nil {
				return err
			}
		default:
			exit42Tracker.reset(taskID)
			runtimeFailureTracker.reset(effectiveTask)

			// Check if crash was caused by a provider-scoped failure.
			if handleClassifiedProviderCrash(config, currentOutput) {
				return nil
			}
			consecutiveCrashes++
			delay := crashRestartDelay(consecutiveCrashes)

			// Track crash restarts per task.
			if effectiveTask != "" {
				var sig string
				if s, rErr := bb.ReadSnapshot(); rErr == nil {
					if task := s.FindTask(effectiveTask); task != nil {
						sig = exit42TaskProgressSignature(task)
					}
				}
				crashCount := crashTracker.Increment(effectiveTask, sig)
				crashThreshold := effectiveCrashRestartThreshold(stateBefore.Config)
				if crashCount > crashThreshold {
					reason := fmt.Sprintf("crash restart loop detected: %d consecutive crashes for task %s without progress (threshold=%d, last exit code=%d)",
						crashCount, effectiveTask, crashThreshold, exitCode)
					GetLogger().Error("Crash restart loop detected, blocking task",
						"task_id", effectiveTask,
						"agent_id", config.AgentID,
						"crash_count", crashCount,
						"exit_code", exitCode)
					if alertErr := LogAlert(config.ProjectRoot, "🚨", "CRASH RESTART LOOP", reason); alertErr != nil {
						GetLogger().Warn("Failed to write crash alert", "error", alertErr)
					}
					if blockErr := blockReleasedTaskFromSupervisor(bb, config.ProjectRoot, effectiveTask, config.Authority, released, reason); blockErr != nil {
						GetLogger().Warn("Failed to block task from supervisor", "error", blockErr, "task_id", effectiveTask)
					}
					crashTracker.reset(effectiveTask)
					// Blocking the task does not clear a failure shared by
					// every task; pace the next claim as any other crash.
					if err := waitForSupervisorDelay(delay); err != nil {
						return err
					}
					continue
				}
			}

			GetLogger().Error("Agent crashed, restarting",
				"exit_code", exitCode,
				"consecutive_crashes", consecutiveCrashes,
				"delay_seconds", int(delay/time.Second))
			if err := waitForSupervisorDelay(delay); err != nil {
				return err
			}
		}
	}
}

func handleQuotaSignal(config SupervisorConfig) bool {
	if !CheckQuotaSignal(config.ProjectRoot, config.CLIName) {
		return false
	}

	GetLogger().Info("Provider quota exhausted, shutting down",
		"provider", config.CLIName,
		"agent_id", config.AgentID)
	return true
}

func handleProviderUnavailableSignal(config SupervisorConfig) bool {
	if !CheckProviderUnavailableSignal(config.ProjectRoot, config.CLIName) {
		return false
	}

	GetLogger().Info("Provider unavailable, shutting down",
		"provider", config.CLIName,
		"agent_id", config.AgentID)
	return true
}

func handleClassifiedProviderCrash(config SupervisorConfig, output string) bool {
	if pu := DetectProviderUnavailable(output, config.CLIName); pu != nil {
		GetLogger().Error("Provider unavailable, terminating",
			"provider", pu.Provider,
			"agent_id", config.AgentID,
			"message", pu.Message)
		if alertErr := LogProviderUnavailableAlert(config.ProjectRoot, pu); alertErr != nil {
			GetLogger().Warn("Failed to write provider unavailable alert", "error", alertErr)
		}
		if err := WriteProviderUnavailableSignal(config.ProjectRoot, pu.Provider, pu.Message); err != nil {
			GetLogger().Warn("Failed to write provider unavailable signal", "error", err)
		}
		return true
	}

	if qe := DetectQuotaExhaustion(output, config.CLIName); qe != nil {
		GetLogger().Error("Provider quota exhausted, terminating",
			"provider", qe.Provider,
			"agent_id", config.AgentID,
			"message", qe.Message)
		if err := RaiseQuotaExhaustion(config.ProjectRoot, qe); err != nil {
			GetLogger().Warn("Failed to raise quota exhaustion", "error", err)
		}
		return true
	}

	return false
}
