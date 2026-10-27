package agent

import (
	"errors"
	"fmt"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/precommit"
)

// loopFlow is what a loop phase tells the supervisor loop to do next.
type loopFlow int

const (
	flowProceed  loopFlow = iota // run the next phase
	flowContinue                 // end this iteration and start the next
	flowExit                     // leave the loop, returning the phase's error
)

// supervisorTurn is the state of one loop iteration: the task it claimed
// versus the one it works on, the snapshot its prompt was built from and what
// the provider session returned.
type supervisorTurn struct {
	taskID        string // what the strategy claimed or selected
	claimedTaskID string // the doer claim, empty for reviewers and the orchestrator
	effectiveTask string // the task spinning, crash and runtime-failure budgets are charged to
	stateBefore   *models.State
	prompt        string
	exitCode      int
	output        string
	released      ops.ReleasedTaskClaim
}

// exit42TaskID is the task a graceful-abort restart is charged to.
func (t *supervisorTurn) exit42TaskID() string {
	if t.claimedTaskID != "" {
		return t.claimedTaskID
	}
	return t.taskID
}

// iterate runs one pass of the supervisor loop: the gates, work admission and
// claim, prompt preparation, the provider session and the completed-turn
// policy. A claim spends InitialTask whichever path ends the iteration, so a
// retry or a block cannot leave the next wait, claim or prompt on it.
func (lt *supervisorLifetime) iterate() (loopFlow, error) {
	if lt.initialTaskSpent {
		lt.config.InitialTask = ""
	}
	if flow, err := lt.admitIteration(); flow != flowProceed {
		return flow, err
	}
	if flow, err := lt.awaitWork(); flow != flowProceed {
		return flow, err
	}
	turn, flow, err := lt.claimTurn()
	if flow != flowProceed {
		return flow, err
	}
	if flow, err := lt.prepareTurn(turn); flow != flowProceed {
		return flow, err
	}
	if flow, err := lt.executeTurn(turn); flow != flowProceed {
		return flow, err
	}
	return lt.completeTurn(turn)
}

// admitIteration stops the loop on cancellation, ABORT or a provider stop
// signal and parks it while PAUSE/CHECKPOINT holds.
func (lt *supervisorLifetime) admitIteration() (loopFlow, error) {
	if err := lt.checkHeartbeat(); err != nil {
		return flowExit, err
	}
	// Check context cancellation (signal received)
	if lt.supervisorCtx.Err() != nil {
		if hbErr := lt.checkHeartbeat(); hbErr != nil {
			return flowExit, hbErr
		}
		if lt.ctx.Err() == nil {
			return flowExit, lt.supervisorCtx.Err()
		}
		GetLogger().Info("Signal received, shutting down")
		return flowExit, nil
	}

	if checkAbort(lt.config.ProjectRoot) {
		GetLogger().Info("ABORT signal received, system shutting down")
		return flowExit, nil
	}
	if handleProviderUnavailableSignal(lt.config) {
		return flowExit, nil
	}
	if handleQuotaSignal(lt.config) {
		return flowExit, nil
	}

	// Wait while PAUSE/CHECKPOINT
	if stop, err := lt.awaitGate(); stop {
		return flowExit, err
	}

	// Pre-work (reviewer: merge handling; others: no-op)
	shouldContinue, err := lt.strategy.PreWork(lt.supervisorCtx, lt.bb, lt.config)
	if err != nil {
		if hbErr := lt.checkHeartbeat(); hbErr != nil {
			return flowExit, hbErr
		}
		return flowExit, err
	}
	if shouldContinue {
		return flowContinue, nil
	}
	return flowProceed, nil
}

func (lt *supervisorLifetime) awaitGate() (bool, error) {
	return awaitRoleGate(lt.supervisorCtx, lt.config.ProjectRoot, string(lt.roleType), lt.checkHeartbeat)
}

// awaitWork waits for role-specific work. Each task boundary passes through
// here, and the wait checks surplus before work on every state read, so a
// surplus agent retires instead of claiming even when work is waiting.
func (lt *supervisorLifetime) awaitWork() (loopFlow, error) {
	hasWork, err := lt.strategy.WaitForWork(lt.supervisorCtx, lt.bb, lt.config, lt.pollInterval, lt.maxWait)
	if errors.Is(err, errPoolSurplus) {
		if lt.retired = lt.retireIfSurplus(); lt.retired {
			return flowExit, nil
		}
		// The locked recheck disagreed with the snapshot (a peer left or the
		// cap rose meanwhile); pace the retry so a persistent disagreement
		// cannot spin.
		if err := lt.waitForDelay(surplusRetryDelay); err != nil {
			return flowExit, err
		}
		return flowContinue, nil
	}
	if err != nil {
		if hbErr := lt.checkHeartbeat(); hbErr != nil {
			return flowExit, hbErr
		}
		return flowExit, err
	}
	if !hasWork {
		GetLogger().Info("No work available, supervisor exiting")
		return flowExit, nil
	}

	// Work can become available after a pause while WaitForWork is parked.
	// Recheck the role-aware gate before claiming; existing providers are
	// unaffected, and transition checkpoints retain their role exceptions.
	if stop, err := lt.awaitGate(); stop {
		return flowExit, err
	}
	if lt.supervisorCtx.Err() != nil {
		return flowContinue, nil // Preserve the existing cancellation/heartbeat exit handling.
	}
	if checkAbort(lt.config.ProjectRoot) {
		GetLogger().Info("ABORT signal received, system shutting down")
		return flowExit, nil
	}
	return flowProceed, nil
}

// claimTurn claims the work the wait found. A claim spends InitialTask.
func (lt *supervisorLifetime) claimTurn() (*supervisorTurn, loopFlow, error) {
	taskID, claimedTaskID, err := lt.strategy.ClaimTask(lt.config, lt.bb)
	if err != nil {
		flow, err := lt.handleClaimFailure(err)
		return nil, flow, err
	}
	lt.initialTaskSpent = true
	turn := &supervisorTurn{taskID: taskID, claimedTaskID: claimedTaskID, effectiveTask: claimedTaskID}
	if turn.effectiveTask == "" {
		turn.effectiveTask = taskID
	}
	return turn, flowProceed, nil
}

func (lt *supervisorLifetime) handleClaimFailure(err error) (loopFlow, error) {
	config := lt.config
	if errors.Is(err, ErrAgentDegraded) {
		GetLogger().Error("Agent marked degraded, exiting supervisor", "agent_id", config.AgentID, "role", config.Role, "error", err)
		return flowExit, nil
	}
	if ops.IsAgentAuthorityError(err) {
		GetLogger().Info("Agent authority lost, exiting supervisor", "agent_id", config.AgentID, "role", config.Role, "error", err)
		return flowExit, nil
	}
	delay := claimFailureDelay
	// Breaker-owning strategies pace and stop the loop; a halt refusal is not a claim failure.
	if observer, ok := lt.strategy.(claimFailureObserver); ok && !errors.Is(err, ops.ErrSystemHalted) {
		decision := observer.ObserveClaimFailure(err)
		if decision.Stop {
			return flowExit, nil
		}
		delay = decision.Delay
	}
	if err := lt.waitForDelay(delay); err != nil {
		return flowExit, err
	}
	return flowContinue, nil
}

// prepareTurn runs the pre-execution step, revalidates the selected wake,
// charges the spinning budget and builds and saves the prompt.
func (lt *supervisorLifetime) prepareTurn(turn *supervisorTurn) (loopFlow, error) {
	config := lt.config
	// Pre-execution (orchestrator: PLANNING status; others: no-op)
	if err := lt.strategy.PreExecution(lt.bb, config); err != nil {
		GetLogger().Warn("Pre-execution failed", "error", err, "agent_id", config.AgentID)
	}

	// Build and save prompt
	stateBefore, err := readStateSnapshot(lt.supervisorCtx, lt.bb)
	if err != nil {
		if lt.supervisorCtx.Err() != nil {
			return flowContinue, nil // Preserve the existing cancellation/heartbeat exit handling.
		}
		return flowExit, fmt.Errorf("failed to read state for prompt: %w", err)
	}
	turn.stateBefore = stateBefore
	if orchestrator, ok := lt.strategy.(*orchestratorStrategy); ok {
		launch, err := orchestrator.RevalidateWake(lt.supervisorCtx, lt.bb, stateBefore, config)
		if err != nil {
			return flowExit, err
		}
		if !launch {
			return flowContinue, nil
		}
	}

	// Spinning detection: track re-executions for the same task.
	if turn.effectiveTask != "" {
		if blockSpinningTask(lt.bb, config, lt.resolver, stateBefore, turn.effectiveTask, lt.trackers.spin) {
			return flowContinue, nil
		}
	} else if stopSpinningOrchestrator(config, stateBefore, lt.trackers.spin) {
		return flowExit, nil
	}

	if reviewer, ok := lt.strategy.(*reviewerStrategy); ok {
		if err := ensureReviewerPromptClaimed(stateBefore, config.AgentID, turn.taskID, reviewer.resolver); err != nil {
			return flowExit, fmt.Errorf("stale reviewer claim before prompt launch: %w", err)
		}
	}
	return lt.buildTurnPrompt(turn)
}

func (lt *supervisorLifetime) buildTurnPrompt(turn *supervisorTurn) (loopFlow, error) {
	config := lt.config
	prompt, err := lt.strategy.BuildPrompt(turn.stateBefore, config, turn.taskID)
	if err != nil {
		// Reviewers hold a review claim, not a doer claim, so claimedTaskID
		// is empty for them. Without this the failure escapes the loop and
		// kills the supervisor, and auto-repair respawns into the same
		// failure instead of surfacing a BLOCKED task with its reason.
		blockTarget := promptFailureBlockTarget(lt.strategy, turn.claimedTaskID, turn.taskID)
		if blockTarget != "" && errors.Is(err, precommit.ErrContextBuild) {
			reason := fmt.Sprintf("prompt context build failed: %v", err)
			GetLogger().Warn("Task blocked due to prompt-context build failure",
				"agent_id", config.AgentID,
				"task_id", blockTarget,
				"error", err)
			if blockErr := blockTaskFromSupervisor(lt.bb, config.ProjectRoot, blockTarget, config.Authority, reason); blockErr != nil {
				GetLogger().Warn("Failed to block task from supervisor", "error", blockErr, "task_id", blockTarget)
			}
			lt.trackers.spin.reset(turn.effectiveTask)
			return flowContinue, nil
		}
		return flowExit, fmt.Errorf("failed to build prompt: %w", err)
	}

	promptFile, err := savePrompt(paths.New(config.ProjectRoot).AgentPromptsDir(), config.AgentID, prompt)
	if err != nil {
		return flowExit, fmt.Errorf("failed to save prompt: %w", err)
	}
	GetLogger().Info("Prompt saved", "file", promptFile)
	turn.prompt = prompt
	return flowProceed, nil
}
