package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/prompts"
	"github.com/liza-mas/liza/internal/sessionvalidation"
	"github.com/liza-mas/liza/internal/usage"
)

// errGoalComplete is a sentinel error returned by waitWhilePaused when
// auto-resume detects that the goal is complete (no carried tasks, no
// pending transitions). The supervisor must handle this as a clean exit.
var errGoalComplete = errors.New("goal complete")

// checkAbort returns true if system mode is STOPPED
func checkAbort(projectRoot string) bool {
	statePath := paths.New(projectRoot).StatePath()
	if bb := db.For(statePath); bb != nil {
		state, err := bb.Read()
		if err == nil {
			stopped, _ := isSystemStopped(state)
			return stopped
		}
	}
	return false
}

// isSystemStopped checks if system is in STOPPED mode from already-read state
// Returns (stopped bool, reason string)
func isSystemStopped(state *models.State) (bool, string) {
	if state.Config.Mode == models.SystemModeStopped {
		return true, "System mode is STOPPED"
	}
	return false, ""
}

// isGoalComplete returns true when a sprint advance produced no carried tasks
// and no transitions fired successfully. This means there's no work left.
// Pure decision function — no side effects, independently testable.
func isGoalComplete(result *ops.ResumeResult) bool {
	return result.SprintAdvanced != nil &&
		len(result.SprintAdvanced.CarriedTasks) == 0 &&
		result.TransitionsExecuted == 0 &&
		result.TransitionError == ""
}

type goalCompletionStopFunc func(projectRoot, reason string) (*ops.ModeChangeResult, error)

var (
	resumeCheckpoint      = ops.AutoResume
	resumeCompletedSprint = ops.AutoResume
	stopCompletedGoal     = ops.StopForGoalCompletion
)

func stopAfterCompletedResume(projectRoot string, result *ops.ResumeResult, stop goalCompletionStopFunc) error {
	if !isGoalComplete(result) {
		return nil
	}
	if _, err := stop(projectRoot, "goal complete"); err != nil {
		return fmt.Errorf("goal complete but failed to stop system: %w", err)
	}
	return errGoalComplete
}

// autoResumeAction returns the sprint status to auto-resume, or "" if none.
// Pure decision function — no side effects, independently testable.
func autoResumeAction(state *models.State) models.SprintStatus {
	if !state.Config.AutoResume {
		return ""
	}
	switch state.Sprint.Status {
	case models.SprintStatusCheckpoint, models.SprintStatusCompleted:
		return state.Sprint.Status
	}
	return ""
}

// waitWhilePaused blocks while system is PAUSED, CIRCUIT_BREAKER_TRIPPED,
// or the current sprint checkpoint blocks this role type. Transition
// checkpoints gate orchestrator transition execution, but doer/reviewer roles
// may continue existing claimable/reviewable work.
func waitWhilePaused(ctx context.Context, projectRoot string, roleType string) error {
	logger := GetLogger()
	statePath := paths.New(projectRoot).StatePath()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		isPaused := false
		pauseReason := ""

		if bb := db.For(statePath); bb != nil {
			state, err := bb.Read()
			if err == nil {
				switch {
				case state.Config.Mode == models.SystemModePaused:
					isPaused = true
					pauseReason = "[PAUSED] System mode is PAUSED"
				case state.Config.Mode == models.SystemModeCircuitBreakerTripped:
					isPaused = true
					pauseReason = "[CIRCUIT BREAKER] Circuit breaker triggered - system halted"
				case state.Sprint.Status == models.SprintStatusCheckpoint:
					if state.Config.AutoResume {
						logger.Info("Auto-resuming from CHECKPOINT")
						if _, resumeErr := resumeCheckpoint(projectRoot, "auto-resume"); resumeErr != nil {
							logger.Warn("Auto-resume failed, waiting for next poll", "error", resumeErr)
						} else {
							continue // state changed, re-read immediately
						}
					}
					if wait, reason := checkpointBlocksRole(state, roleType); wait {
						isPaused = true
						pauseReason = reason
					}
				case state.Sprint.Status == models.SprintStatusCompleted && state.Config.AutoResume:
					logger.Info("Auto-resuming from COMPLETED")
					result, resumeErr := resumeCompletedSprint(projectRoot, "auto-resume")
					if resumeErr != nil {
						logger.Warn("Auto-resume from COMPLETED failed, waiting", "error", resumeErr)
					} else {
						if completionErr := stopAfterCompletedResume(projectRoot, result, stopCompletedGoal); completionErr != nil {
							if errors.Is(completionErr, errGoalComplete) {
								logger.Info("Goal complete — clean integration evidence stopped the system.")
							}
							return completionErr
						}
						continue // state changed, re-read immediately
					}
					isPaused = true
					pauseReason = "[COMPLETED] Sprint completed, auto-resume pending"
				}
			}
		}

		if !isPaused {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			logger.Info("System paused, waiting for resume", "pause_reason", pauseReason)
		}
	}
}

func checkpointBlocksRole(state *models.State, roleType string) (bool, string) {
	if state == nil || state.Sprint.Status != models.SprintStatusCheckpoint {
		return false, ""
	}

	if models.IsTransitionCheckpointTrigger(state.Sprint.CheckpointTrigger) {
		switch roleType {
		case "doer", "reviewer":
			return false, ""
		case "orchestrator":
			return true, fmt.Sprintf("[CHECKPOINT] Transition gate pending - run %q to create downstream tasks", brand.Command("resume"))
		default:
			return true, "[CHECKPOINT] Transition gate pending - unknown role type blocked"
		}
	}

	return true, "[CHECKPOINT] Sprint is at checkpoint"
}

func newProviderLaunchGate(config SupervisorConfig) LLMAgentLaunchGate {
	return newTaskProviderLaunchGate(config, "", nil)
}

func newTaskProviderLaunchGate(config SupervisorConfig, taskID string, validation *ops.ValidationPreflight) LLMAgentLaunchGate {
	return func(ctx context.Context, start func() error) error {
		return ops.WithAgentLifecycleLock(ctx, config.ProjectRoot, config.Authority.ID, "provider-start", func() error {
			state, err := db.For(config.StatePath).ReadContext(ctx)
			if err != nil {
				return fmt.Errorf("read current agent authority before provider start: %w", err)
			}
			if err := ops.RequireAgentAuthority(state, config.Authority); err != nil {
				return err
			}
			// A legacy task can acquire prerequisites after preparation. Such a
			// change needs a fresh observation before any provider process starts.
			if task := state.FindTask(taskID); task != nil && len(task.ValidationPrerequisites) > 0 && validation == nil {
				return &sessionvalidation.Error{Code: "context_changed", CommandIndex: -1, CheckIndex: -1}
			}
			if err := validation.CheckLaunchCurrent(state); err != nil {
				return err
			}
			return start()
		})
	}
}

// executeAgent executes the CLI with timeout.
func executeAgent(ctx context.Context, config SupervisorConfig, prompt string, additionalDirs []string, taskID string, runtimeConfig models.Config) (int, string, error) {
	logger := GetLogger()

	agent, err := resolveLLMAgent(config)
	if err != nil {
		return 0, "", err
	}
	session, err := prepareSupervisorSession(config, runtimeConfig)
	if err != nil {
		return 0, "", err
	}
	// A provider start always probes freshly. Claim-loop failures alone are
	// rate-limited; repaired dependency files need not change the environment.
	session.ForceCheck = true
	var validation *ops.ValidationPreflight
	if taskID != "" {
		validation, err = ops.PrepareValidationPreflight(config.ProjectRoot, taskID, config.AgentID, "", session)
		if err != nil {
			return 0, "", releaseFailedValidation(config, taskID, err)
		}
	} else if session.PreparationError != nil {
		return 0, "", session.PreparationError
	}
	checkedGate := newTaskProviderLaunchGate(config, taskID, validation)
	launchGate := LLMAgentLaunchGate(func(ctx context.Context, start func() error) error {
		return releaseFailedValidation(config, taskID, checkedGate.launch(ctx, start))
	})

	// Interactive mode: launch CLI without -p so user can paste the prompt
	if config.Interactive {
		fmt.Println("=== INTERACTIVE MODE ===")
		fmt.Println("Paste the prompt from the file above into the CLI session.")
		fmt.Printf("Launching: %s\n", config.CLIName)
		exitCode, err := agent.RunInteractive(ctx, LLMAgentInteractiveRequest{
			BackendName:    config.CLIName,
			AgentID:        config.AgentID,
			Generation:     config.Authority.Generation,
			TaskID:         taskID,
			SessionID:      taskID,
			ProfileName:    config.ProfileName,
			ProfileVars:    config.ProfileVars,
			ProjectRoot:    config.ProjectRoot,
			AdditionalDirs: additionalDirs,
			RuntimeConfig:  runtimeConfig,
			EventSink: NewUsageEventSink(UsageSinkConfig{
				ProjectRoot:     config.ProjectRoot,
				AgentID:         config.AgentID,
				Role:            config.Role,
				Provider:        config.CLIName,
				SessionID:       taskID,
				TaskID:          taskID,
				SupervisorRunID: supervisorRunID(),
			}),
			LaunchGate:   launchGate,
			Environment:  session.Environment,
			SessionScope: validation.SessionScope(),
		})
		return exitCode, "", err
	}

	// Create timeout context for CLI execution
	execCtx, cancelExec := context.WithTimeout(ctx, config.ExecutionTimeout)
	defer cancelExec()
	progressCh := make(chan struct{}, 1)
	markProgress := func() {
		select {
		case progressCh <- struct{}{}:
		default:
		}
	}
	execCtx = withExecutionProgressCallback(execCtx, markProgress)
	stopWatchdog := startExecutionProgressWatchdog(execCtx, config, taskID, progressCh, cancelExec)
	defer stopWatchdog()

	// Heartbeat is managed by RunSupervisor for the full supervisor lifetime,
	// so we don't start one here.

	// Execute CLI with timeout
	result, err := agent.Run(execCtx, LLMAgentRunRequest{
		BackendName:    config.CLIName,
		AgentID:        config.AgentID,
		Generation:     config.Authority.Generation,
		TaskID:         taskID,
		SessionID:      taskID,
		WarmSession:    false,
		ProfileName:    config.ProfileName,
		ProfileVars:    config.ProfileVars,
		Prompt:         prompt,
		ProjectRoot:    config.ProjectRoot,
		AdditionalDirs: additionalDirs,
		RuntimeConfig:  runtimeConfig,
		EventSink: NewUsageEventSink(UsageSinkConfig{
			ProjectRoot:     config.ProjectRoot,
			AgentID:         config.AgentID,
			Role:            config.Role,
			Provider:        config.CLIName,
			SessionID:       taskID,
			TaskID:          taskID,
			SupervisorRunID: supervisorRunID(),
		}),
		LaunchGate:   launchGate,
		Environment:  session.Environment,
		SessionScope: validation.SessionScope(),
	})
	watchdogResult := stopWatchdog()
	if watchdogResult.Blocked {
		if blockErr := blockTaskFromSupervisor(db.For(config.StatePath), config.ProjectRoot, taskID, config.Authority, watchdogResult.Reason); blockErr != nil {
			logger.Warn("Failed to block task from execution watchdog", "error", blockErr, "task_id", taskID)
		}
		logger.Error("Agent execution stopped after stale progress",
			"agent_id", config.AgentID,
			"task_id", taskID,
			"reason", watchdogResult.Reason)
		return 0, result.Output, nil
	}

	// Check if execution timed out
	if err != nil && errors.Is(err, context.DeadlineExceeded) {
		logger.Error("Agent execution timeout",
			"agent_id", config.AgentID,
			"timeout", config.ExecutionTimeout,
			"hint", "CLI may be hung, will retry")
		return 1, result.Output, nil // Return failure code to trigger retry
	}

	// Check if timeout context was cancelled (even if Execute returned successfully)
	if execCtx.Err() == context.DeadlineExceeded {
		logger.Error("Agent execution timeout (context deadline exceeded)",
			"agent_id", config.AgentID,
			"timeout", config.ExecutionTimeout,
			"hint", "CLI may be hung, will retry")
		return 1, result.Output, nil // Return failure code to trigger retry
	}
	if err != nil && result.ExitCode != 0 {
		// A provider/CLI failure must reach RunSupervisor's non-zero exit path.
		// Returning it as a Go error exits the supervisor, resets its in-memory
		// loop tracker, and lets auto-repair respawn into the same task session.
		logger.Warn("Agent process failed", "agent_id", config.AgentID, "exit_code", result.ExitCode, "error", err)
		return result.ExitCode, result.Output, nil
	}

	return result.ExitCode, result.Output, err
}

// newSupervisorRunID mints one opaque 128-bit identity. Each supervisor process
// mints exactly one, so a restart or a handoff is observable in usage records on
// every provider path — including the default CLI one, where session_id is a
// copy of the task id. It is not a registration generation and confers no
// authority (specs/protocols/lifecycle-results.md, Result contract).
func newSupervisorRunID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// A record must never carry an empty identity. Process id plus start
		// time still separates two supervisors on one host.
		return fmt.Sprintf("%08x-%016x", os.Getpid(), time.Now().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}

var (
	supervisorRunIDOnce  sync.Once
	supervisorRunIDValue string
)

// supervisorRunID is the process-scoped — therefore supervisor-session-scoped —
// run identity every record written by this process carries.
func supervisorRunID() string {
	supervisorRunIDOnce.Do(func() { supervisorRunIDValue = newSupervisorRunID() })
	return supervisorRunIDValue
}

// UsageSinkConfig is the supervisor identity a usage record is attributed to.
// An empty SupervisorRunID falls back to this process's run identity; a caller
// that supplies one can reproduce a handoff or a supervisor restart.
type UsageSinkConfig struct {
	ProjectRoot     string
	AgentID         string
	Role            string
	Provider        string
	SessionID       string
	TaskID          string
	SupervisorRunID string
}

// NewUsageEventSink returns the supervisor event sink for one provider turn. It
// keeps the existing log lines and writes exactly one durable usage record when
// the turn completes. The record is telemetry: a failing append is logged and
// never changes the run's result.
func NewUsageEventSink(cfg UsageSinkConfig) LLMAgentEventSink {
	if cfg.SupervisorRunID == "" {
		cfg.SupervisorRunID = supervisorRunID()
	}
	return &supervisorLLMAgentEventSink{cfg: cfg}
}

type supervisorLLMAgentEventSink struct {
	cfg UsageSinkConfig

	mu        sync.Mutex
	startedAt time.Time
	reported  LLMAgentUsage
	sawUsage  bool
}

func (s *supervisorLLMAgentEventSink) RecordLLMAgentEvent(_ context.Context, event LLMAgentEvent) {
	switch event.Kind {
	case LLMAgentEventStarted:
		logLLMAgentLifecycleEvent(event)
		s.mu.Lock()
		s.startedAt = eventTime(event)
		s.mu.Unlock()
	case LLMAgentEventUsage:
		reported, _ := event.Payload["usage"].(LLMAgentUsage)
		GetLogger().Info("LLM agent usage",
			"backend", event.BackendName,
			"agent_id", event.AgentID,
			"task_id", event.TaskID,
			"session_id", event.SessionID,
			"input_tokens", reported.InputTokens,
			"output_tokens", reported.OutputTokens,
			"cached_read_tokens", reported.CachedReadTokens,
			"cached_write_tokens", reported.CachedWriteTokens)
		s.mu.Lock()
		s.reported, s.sawUsage = reported, true
		s.mu.Unlock()
	case LLMAgentEventCompleted:
		logLLMAgentLifecycleEvent(event)
		s.appendRecord(event)
	default:
		// Provider content chunks stay in the normal output stream/log files. The
		// supervisor metadata sink intentionally avoids duplicating content events.
	}
}

func logLLMAgentLifecycleEvent(event LLMAgentEvent) {
	GetLogger().Info("LLM agent event",
		"kind", string(event.Kind),
		"backend", event.BackendName,
		"agent_id", event.AgentID,
		"task_id", event.TaskID,
		"session_id", event.SessionID)
}

// appendRecord writes this turn's single record. A completed event without a
// preceding started one — the launch-gate failure path — records unknown
// provenance over a zero-length interval rather than no record at all.
func (s *supervisorLLMAgentEventSink) appendRecord(event LLMAgentEvent) {
	endedAt := eventTime(event)
	s.mu.Lock()
	startedAt, reported, sawUsage := s.startedAt, s.reported, s.sawUsage
	s.mu.Unlock()
	if startedAt.IsZero() {
		startedAt = endedAt
	}
	// The provider reports its own session; cfg.SessionID is the caller's
	// fallback, which on the default CLI path is a copy of the task id.
	sessionID := s.cfg.SessionID
	if event.SessionID != "" {
		sessionID = event.SessionID
	}
	record := usage.Record{
		TaskID:          s.cfg.TaskID,
		Role:            s.cfg.Role,
		AgentID:         s.cfg.AgentID,
		SupervisorRunID: s.cfg.SupervisorRunID,
		SessionID:       sessionID,
		Provider:        s.cfg.Provider,
		StartedAt:       startedAt,
		EndedAt:         endedAt,
		// WarmSession stays false: no provider event carries warm-session reuse,
		// and this output adds no LLMAgentEvent field.
		FreshInputTokens: reported.InputTokens,
		CacheReadTokens:  reported.CachedReadTokens,
		CacheWriteTokens: reported.CachedWriteTokens,
		OutputTokens:     reported.OutputTokens,
		Provenance:       classifyUsageProvenance(reported, sawUsage),
		ExitCode:         completedExitCode(event),
	}
	if err := usage.Append(s.cfg.ProjectRoot, record); err != nil {
		GetLogger().Warn("Failed to append usage record",
			"error", err,
			"agent_id", s.cfg.AgentID,
			"task_id", s.cfg.TaskID,
			"provider", s.cfg.Provider)
	}
}

// classifyUsageProvenance pins the record's quality at write time, so a later
// before/after comparison is not silently re-based when log volume changes.
func classifyUsageProvenance(reported LLMAgentUsage, sawUsage bool) usage.Provenance {
	switch {
	case !sawUsage:
		return usage.ProvenanceUnknown
	case reported.InputTokens > 0 || reported.OutputTokens > 0:
		return usage.ProvenanceTerminalAuthoritative
	case reported.CachedReadTokens > 0 || reported.CachedWriteTokens > 0:
		// Some fields arrived; the fresh-input and output counts did not.
		return usage.ProvenancePartial
	default:
		// The provider reports no usage at all, as the CLI transport does.
		return usage.ProvenanceUnknown
	}
}

func completedExitCode(event LLMAgentEvent) int {
	if code, ok := event.Payload["exit_code"].(int); ok {
		return code
	}
	if _, failed := event.Payload["error"]; failed {
		return 1
	}
	return 0
}

func eventTime(event LLMAgentEvent) time.Time {
	if event.Time.IsZero() {
		return time.Now().UTC()
	}
	return event.Time.UTC()
}

// verifyOrchestratorStateChanges checks if orchestrator made expected state changes after completion
func verifyOrchestratorStateChanges(bb *db.Blackboard, stateBefore *models.State, pipelineTerminals []models.TaskStatus, planningPairs map[string]bool, m2oTransitions []ops.ManyToOneTransitionInfo) error {
	logger := GetLogger()
	projectRoot := filepath.Dir(filepath.Dir(bb.GetStatePath()))
	// Read state after agent execution
	stateAfter, err := bb.ReadCached()
	if err != nil {
		return fmt.Errorf("failed to read state after agent execution: %w", err)
	}

	// Detect the wake trigger that caused this orchestrator run
	result := DetectOrchestratorWakeTriggersForProject(projectRoot, stateBefore, pipelineTerminals, planningPairs, m2oTransitions)

	// Verify expected changes based on trigger
	switch result.Trigger {
	case WakeTriggerInitialPlanning:
		// INITIAL_PLANNING: expect tasks to be created
		if len(stateAfter.Tasks) == 0 {
			return fmt.Errorf("orchestrator completed with INITIAL_PLANNING trigger but no tasks were created")
		}
		logger.Info("Orchestrator created tasks", "task_count", len(stateAfter.Tasks))

	case WakeTriggerBlocked:
		// BLOCKED_TASKS: expect blocked tasks to be unblocked or superseded
		blockedBefore := 0
		blockedAfter := 0
		for _, task := range stateBefore.Tasks {
			if task.Status == models.TaskStatusBlocked {
				blockedBefore++
			}
		}
		for _, task := range stateAfter.Tasks {
			if task.Status == models.TaskStatusBlocked {
				blockedAfter++
			}
		}
		if blockedAfter < blockedBefore {
			logger.Info("Orchestrator resolved blocked tasks", "before", blockedBefore, "after", blockedAfter)
		} else {
			logger.Info("Orchestrator could not resolve blocked tasks",
				"before", blockedBefore, "after", blockedAfter,
				"hint", "Blocks may require human intervention")
		}

	case WakeTriggerHypothesisExhausted:
		// HYPOTHESIS_EXHAUSTED: expect exhausted tasks to be blocked, superseded, or otherwise made non-claimable.
		exhaustedBefore := countUnresolvedHypothesisExhausted(stateBefore)
		exhaustedAfter := countUnresolvedHypothesisExhausted(stateAfter)
		if exhaustedAfter < exhaustedBefore {
			logger.Info("Orchestrator handled exhausted hypotheses", "before", exhaustedBefore, "after", exhaustedAfter)
		} else {
			return fmt.Errorf("orchestrator completed with HYPOTHESIS_EXHAUSTED trigger but unresolved exhausted count didn't decrease (before: %d, after: %d)", exhaustedBefore, exhaustedAfter)
		}

	case WakeTriggerHumanNote:
		// HUMAN_NOTE: the note carries the operator's request; there is no
		// single expected state change. Log for the audit trail only.
		logger.Info("Orchestrator completed an operator-note turn",
			"unseen_before", ops.CountUnseenHumanNotes(stateBefore))

	case WakeTriggerImmediateDiscovery:
		// IMMEDIATE_DISCOVERY: expect discoveries to be converted to tasks
		immediateBefore := 0
		immediateAfter := 0
		for _, disc := range stateBefore.Discovered {
			if disc.Urgency == "immediate" && disc.ConvertedToTask == nil {
				immediateBefore++
			}
		}
		for _, disc := range stateAfter.Discovered {
			if disc.Urgency == "immediate" && disc.ConvertedToTask == nil {
				immediateAfter++
			}
		}
		if immediateAfter >= immediateBefore {
			return fmt.Errorf("orchestrator completed with IMMEDIATE_DISCOVERY trigger but unconverted count didn't decrease (before: %d, after: %d)", immediateBefore, immediateAfter)
		}
		logger.Info("Orchestrator handled immediate discoveries", "before", immediateBefore, "after", immediateAfter)

	case WakeTriggerPlanningComplete:
		// PLANNING_COMPLETE: expect sprint checkpointed with trigger set. Child tasks
		// are created later by orchestrator PreWork after the human resumes the sprint.
		if stateAfter.Sprint.Status != models.SprintStatusCheckpoint && stateAfter.Sprint.Status != models.SprintStatusCompleted {
			return fmt.Errorf("orchestrator completed with PLANNING_COMPLETE trigger but sprint status is %s (expected CHECKPOINT or COMPLETED)", stateAfter.Sprint.Status)
		}
		if stateAfter.Sprint.Timeline.CheckpointAt == nil {
			return fmt.Errorf("orchestrator completed with PLANNING_COMPLETE trigger but checkpoint_at is not set")
		}
		if stateAfter.Sprint.CheckpointTrigger != models.CheckpointTriggerPlanningComplete {
			logger.Warn("Orchestrator checkpointed but checkpoint_trigger is not PLANNING_COMPLETE — transitions may not execute after resume",
				"actual_trigger", stateAfter.Sprint.CheckpointTrigger)
		}
		logger.Info("Orchestrator checkpointed planning completion")

	case WakeTriggerManyToOneReady:
		// MANY_TO_ONE_READY: expect sprint checkpointed so transitions execute after human resume.
		if stateAfter.Sprint.Status != models.SprintStatusCheckpoint && stateAfter.Sprint.Status != models.SprintStatusCompleted {
			return fmt.Errorf("orchestrator completed with MANY_TO_ONE_READY trigger but sprint status is %s (expected CHECKPOINT or COMPLETED)", stateAfter.Sprint.Status)
		}
		if stateAfter.Sprint.Timeline.CheckpointAt == nil {
			return fmt.Errorf("orchestrator completed with MANY_TO_ONE_READY trigger but checkpoint_at is not set")
		}
		logger.Info("Orchestrator checkpointed many-to-one cohort readiness")

	case WakeTriggerCodingComplete:
		if result.Integration.Status != "reconciliation_needed" {
			return fmt.Errorf("orchestrator completed with CODING_COMPLETE trigger but integration status is %q", result.Integration.Status)
		}
		return reconcileEffectiveIntegrationOutcome(projectRoot, bb, result.Integration, ops.ReconcileIntegrationAnalyses)

	case WakeTriggerIntegrationBlocked, WakeTriggerIntegrationExhausted:
		return verifyEffectiveIntegrationOutcome(stateAfter, result.Integration, nil)

	case WakeTriggerIntegrationWaiting, WakeTriggerIntegrationUnavailable:
		return verifyEffectiveIntegrationOutcome(stateAfter, result.Integration, nil)

	case WakeTriggerSprintComplete:
		// SPRINT_COMPLETE: expect sprint status to be CHECKPOINT (or COMPLETED)
		if stateAfter.Sprint.Status != models.SprintStatusCheckpoint && stateAfter.Sprint.Status != models.SprintStatusCompleted {
			return fmt.Errorf("orchestrator completed with SPRINT_COMPLETE trigger but sprint status is %s (expected CHECKPOINT or COMPLETED)", stateAfter.Sprint.Status)
		}
		if stateAfter.Sprint.Timeline.CheckpointAt == nil {
			return fmt.Errorf("orchestrator completed with SPRINT_COMPLETE trigger but checkpoint_at is not set")
		}
		logger.Info("Orchestrator completed sprint", "status", stateAfter.Sprint.Status)
	}

	return nil
}

type integrationReconcileFunc func(projectRoot string) (*ops.ReconcileIntegrationAnalysesResult, error)

func reconcileEffectiveIntegrationOutcome(projectRoot string, bb *db.Blackboard, projection prompts.EffectiveIntegrationCompletion, reconcile integrationReconcileFunc) error {
	reconciliation, err := reconcile(projectRoot)
	if err != nil {
		return fmt.Errorf("reconcile requested integration analyses: %w", err)
	}
	state, err := bb.Read()
	if err != nil {
		return fmt.Errorf("read reconciled integration state: %w", err)
	}
	return verifyEffectiveIntegrationOutcome(state, projection, reconciliation)
}

func verifyEffectiveIntegrationOutcome(state *models.State, projection prompts.EffectiveIntegrationCompletion, reconciliation *ops.ReconcileIntegrationAnalysesResult) error {
	switch projection.Status {
	case "blocked", "exhausted":
		return nil
	case "reconciliation_needed":
		if reconciliation != nil && reconciliation.Reason != nil {
			lifecycle := state.Goal.Integration
			if lifecycle == nil || lifecycle.Closure == nil ||
				(lifecycle.Closure.Status != models.IntegrationClosureStatusBlocked && lifecycle.Closure.Status != models.IntegrationClosureStatusExhausted) ||
				lifecycle.Closure.Reason != reconciliation.Reason.Code {
				return fmt.Errorf("integration reconciliation returned %q without matching blocked or exhausted closure", reconciliation.Reason.Code)
			}
			return nil
		}
		if len(projection.RequestKeys) == 0 {
			return fmt.Errorf("integration reconciliation projected no requested analysis keys")
		}
		for _, key := range projection.RequestKeys {
			taskCount := 0
			plannedCount := 0
			for i := range state.Tasks {
				if state.Tasks[i].IntegrationAnalysis != nil && state.Tasks[i].IntegrationAnalysis.Key == key {
					taskCount++
					for _, plannedID := range state.Sprint.Scope.Planned {
						if plannedID == state.Tasks[i].ID {
							plannedCount++
						}
					}
				}
			}
			if taskCount != 1 || plannedCount != 1 {
				return fmt.Errorf("requested integration analysis %q has duplicate or missing membership: tasks=%d planned=%d", key, taskCount, plannedCount)
			}
		}
		return nil
	default:
		return fmt.Errorf("integration outcome %q (%s) is not an accepted post-run result", projection.Status, projection.ReasonCode)
	}
}

func countUnresolvedHypothesisExhausted(state *models.State) int {
	count := 0
	for _, task := range state.Tasks {
		if len(task.FailedBy) >= 2 && task.Status != models.TaskStatusBlocked && !task.Status.IsTerminal() {
			count++
		}
	}
	return count
}
