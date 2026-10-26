package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/sessionvalidation"
)

// errLaunchHalted reports a provider start refused because the system entered
// a halt mode after the claim. No provider ran; the supervisor releases the
// claim without charging any budget and parks at the pause gate.
var errLaunchHalted = errors.New("provider start refused: system halted")

var errLaunchContended = errors.New("provider start delayed: lock contention")

func newProviderLaunchGate(config SupervisorConfig) LLMAgentLaunchGate {
	return newTaskProviderLaunchGate(config, "", nil)
}

func newTaskProviderLaunchGate(config SupervisorConfig, taskID string, validation *ops.ValidationPreflight) LLMAgentLaunchGate {
	return func(ctx context.Context, start func() error) error {
		return ops.WithAgentLifecycleLock(ctx, config.ProjectRoot, config.Authority.ID, "provider-start", func() error {
			// Held shared across the mode check and start, so a halt's barrier
			// orders every start before or after it (ADR-0173).
			return ops.WithWorkAdmissionSharedLock(ctx, config.ProjectRoot, "provider-start", func() error {
				// Registration and recover-agent take this agent's lifecycle lock, so
				// the snapshot cannot miss a generation change for it.
				state, err := readStateSnapshot(ctx, db.For(config.StatePath))
				if err != nil {
					return fmt.Errorf("read current agent authority before provider start: %w", err)
				}
				if err := ops.RequireAgentAuthority(state, config.Authority); err != nil {
					return err
				}
				if _, err := newReviewExecution(state, config, taskID); err != nil {
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
				if err := ops.RequireWorkAdmitted(state, "provider-start"); err != nil {
					return err
				}
				return start()
			})
		})
	}
}

// executeAgent executes the CLI with timeout.
func executeAgent(ctx context.Context, config SupervisorConfig, prompt string, additionalDirs []string, taskID string, runtimeConfig models.Config) (exitCode int, output string, runErr error) {
	logger := GetLogger()
	defer config.sessionActivity.stop()
	startAttempted := false
	var launchErr error
	defer func() {
		// Some backends mask gate failures as an exit code. Only contention
		// proven to precede the provider start is a coordination retry; a
		// callback that may have started the provider must never be replayed.
		if !startAttempted {
			if filelock.IsLockErrorType(launchErr, filelock.LockErrorTimeout) {
				runErr = launchErr
			}
			if filelock.IsLockErrorType(runErr, filelock.LockErrorTimeout) {
				runErr = fmt.Errorf("%w: %w", errLaunchContended, runErr)
			}
		}
	}()

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
		validation, err = ops.PrepareValidationPreflightContext(ctx, config.ProjectRoot, taskID, config.AgentID, "", session)
		if err != nil {
			return 0, "", releaseFailedValidation(config, taskID, err)
		}
	} else if session.PreparationError != nil {
		return 0, "", session.PreparationError
	}
	checkedGate := newTaskProviderLaunchGate(config, taskID, validation)
	// Backends report a gate refusal differently (ACPX masks it as exit 1), so
	// the refusal is recorded here rather than recovered from Run's result.
	halted := false
	launchGate := LLMAgentLaunchGate(func(ctx context.Context, start func() error) error {
		err := checkedGate.launch(ctx, func() error {
			startAttempted = true
			config.sessionActivity.start(taskID)
			if err := start(); err != nil {
				config.sessionActivity.stop()
				return err
			}
			return nil
		})
		halted = errors.Is(err, ops.ErrSystemHalted)
		launchErr = err
		return releaseFailedValidation(config, taskID, err)
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
			Model:          config.Model,
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
		if halted {
			return 0, "", errLaunchHalted
		}
		return exitCode, "", err
	}

	// Create timeout context for CLI execution
	execCtx, cancelExec := withExecutionDeadline(ctx, config.ExecutionTimeout, taskID, sessionInflightSubmit(config, taskID), sessionSubmitRunning(config, taskID))
	defer cancelExec()
	stopReviewWatchdog, err := startReviewExecutionWatchdog(execCtx, config, taskID, models.NormalizeHeartbeatInterval(runtimeConfig.HeartbeatInterval), cancelExec)
	if err != nil {
		return 0, "", err
	}
	defer stopReviewWatchdog()
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
		Model:          config.Model,
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
	if halted {
		return 0, "", errLaunchHalted
	}
	if stopReviewWatchdog() || errors.Is(err, errReviewOwnershipLost) {
		// Cancellation can surface as an exit code, WaitDelay or a pipe error.
		// Preserve the coordination outcome before provider-failure handling.
		return 0, result.Output, errReviewOwnershipLost
	}
	if watchdogResult.Blocked {
		// A session the provider ended (quota, outage) looks stalled when a
		// background job keeps the CLI alive. That is a provider failure, not
		// a stalled task: the non-zero exit sends it down the supervisor's
		// provider-failure path with the claim still held (D-101).
		if providerEndedSession(result.Output, config.CLIName) {
			logger.Warn("Execution progress timeout after the provider ended the session; handling it as a provider failure",
				"agent_id", config.AgentID,
				"task_id", taskID)
			return 1, result.Output, nil
		}
		if blockErr := blockTaskFromSupervisor(db.For(config.StatePath), config.ProjectRoot, taskID, config.Authority, watchdogResult.Reason); blockErr != nil {
			logger.Warn("Failed to block task from execution watchdog", "error", blockErr, "task_id", taskID)
		}
		logger.Error("Agent execution stopped after stale progress",
			"agent_id", config.AgentID,
			"task_id", taskID,
			"reason", watchdogResult.Reason)
		return 0, result.Output, nil
	}

	// A session held past its timeout for its own submit ends with that
	// submit's task settled, never silently released (D-174).
	if cause := context.Cause(execCtx); errors.Is(cause, errInflightSubmitInterrupted) || errors.Is(cause, errSubmitHandbackEnded) {
		return endSessionAfterInflightSubmit(config, taskID, cause), result.Output, nil
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
	if executionTimedOut(execCtx) {
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
