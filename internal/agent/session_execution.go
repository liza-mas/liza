package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

// sessionLaunch records what the launch gate observed for one provider session.
// Backends report a gate refusal differently (ACPX masks it as exit 1), so the
// refusal is recorded here rather than recovered from Run's result.
type sessionLaunch struct {
	startAttempted bool
	launchErr      error
	halted         bool
}

// gate wraps the checked launch gate: it marks the start attempt no later than
// the provider start itself and keeps the gate's own outcome apart from the
// backend's result.
func (l *sessionLaunch) gate(config SupervisorConfig, taskID string, checked LLMAgentLaunchGate) LLMAgentLaunchGate {
	return func(ctx context.Context, start func() error) error {
		err := checked.launch(ctx, func() error {
			l.startAttempted = true
			config.sessionActivity.start(taskID)
			if err := start(); err != nil {
				config.sessionActivity.stop()
				return err
			}
			return nil
		})
		l.halted = errors.Is(err, ops.ErrSystemHalted)
		l.launchErr = err
		return releaseFailedValidation(config, taskID, err)
	}
}

// settle turns a state-lock timeout into a coordination retry. Some backends
// mask gate failures as an exit code, and only contention proven to precede
// the provider start is retryable: a callback that may have started the
// provider must never be replayed.
func (l *sessionLaunch) settle(runErr error) error {
	if l.startAttempted {
		return runErr
	}
	if filelock.IsLockErrorType(l.launchErr, filelock.LockErrorTimeout) {
		runErr = l.launchErr
	}
	if filelock.IsLockErrorType(runErr, filelock.LockErrorTimeout) {
		runErr = fmt.Errorf("%w: %w", errLaunchContended, runErr)
	}
	return runErr
}

// agentSession is one provider session prepared by executeAgent: its backend,
// validated environment and gated start.
type agentSession struct {
	config        SupervisorConfig
	agent         LLMAgent
	taskID        string
	runtimeConfig models.Config
	environment   *ops.ValidationSession
	validation    *ops.ValidationPreflight
	launch        *sessionLaunch
}

// executeAgent executes the CLI with timeout.
func executeAgent(ctx context.Context, config SupervisorConfig, prompt string, additionalDirs []string, taskID string, runtimeConfig models.Config) (exitCode int, output string, runErr error) {
	defer config.sessionActivity.stop()
	launch := &sessionLaunch{}
	defer func() { runErr = launch.settle(runErr) }()

	session, err := prepareAgentSession(ctx, config, taskID, runtimeConfig, launch)
	if err != nil {
		return 0, "", err
	}
	// Interactive mode: launch CLI without -p so user can paste the prompt
	if config.Interactive {
		return session.runInteractive(ctx, additionalDirs)
	}
	return session.runWatched(ctx, prompt, additionalDirs)
}

// prepareAgentSession resolves the backend and validates the session context
// before any provider start.
func prepareAgentSession(ctx context.Context, config SupervisorConfig, taskID string, runtimeConfig models.Config, launch *sessionLaunch) (*agentSession, error) {
	agent, err := resolveLLMAgent(config)
	if err != nil {
		return nil, err
	}
	environment, err := prepareSupervisorSession(config, runtimeConfig)
	if err != nil {
		return nil, err
	}
	// A provider start always probes freshly. Claim-loop failures alone are
	// rate-limited; repaired dependency files need not change the environment.
	environment.ForceCheck = true
	var validation *ops.ValidationPreflight
	if taskID != "" {
		validation, err = ops.PrepareValidationPreflightContext(ctx, config.ProjectRoot, taskID, config.AgentID, "", environment)
		if err != nil {
			return nil, releaseFailedValidation(config, taskID, err)
		}
	} else if environment.PreparationError != nil {
		return nil, environment.PreparationError
	}
	return &agentSession{
		config:        config,
		agent:         agent,
		taskID:        taskID,
		runtimeConfig: runtimeConfig,
		environment:   environment,
		validation:    validation,
		launch:        launch,
	}, nil
}

// eventSink is the usage sink of this session's turn.
func (s *agentSession) eventSink() LLMAgentEventSink {
	return NewUsageEventSink(UsageSinkConfig{
		ProjectRoot:     s.config.ProjectRoot,
		AgentID:         s.config.AgentID,
		Role:            s.config.Role,
		Provider:        s.config.CLIName,
		SessionID:       s.taskID,
		TaskID:          s.taskID,
		SupervisorRunID: supervisorRunID(),
	})
}

func (s *agentSession) launchGate() LLMAgentLaunchGate {
	return s.launch.gate(s.config, s.taskID, newTaskProviderLaunchGate(s.config, s.taskID, s.validation))
}

// runInteractive hands the terminal to the provider; there is no watchdog,
// deadline or output to classify.
func (s *agentSession) runInteractive(ctx context.Context, additionalDirs []string) (int, string, error) {
	fmt.Println("=== INTERACTIVE MODE ===")
	fmt.Println("Paste the prompt from the file above into the CLI session.")
	fmt.Printf("Launching: %s\n", s.config.CLIName)
	exitCode, err := s.agent.RunInteractive(ctx, LLMAgentInteractiveRequest{
		BackendName:    s.config.CLIName,
		AgentID:        s.config.AgentID,
		Generation:     s.config.Authority.Generation,
		TaskID:         s.taskID,
		SessionID:      s.taskID,
		ProfileName:    s.config.ProfileName,
		ProfileVars:    s.config.ProfileVars,
		Model:          s.config.Model,
		ProjectRoot:    s.config.ProjectRoot,
		AdditionalDirs: additionalDirs,
		RuntimeConfig:  s.runtimeConfig,
		EventSink:      s.eventSink(),
		LaunchGate:     s.launchGate(),
		Environment:    s.environment.Environment,
		SessionScope:   s.validation.SessionScope(),
	})
	if s.launch.halted {
		return 0, "", errLaunchHalted
	}
	return exitCode, "", err
}

// sessionRun is a finished backend run and what the watchdogs concluded.
type sessionRun struct {
	result     LLMAgentRunResult
	err        error
	execCtx    context.Context
	progress   executionProgressWatchdogResult
	reviewLost bool
}

// runWatched runs the provider under the execution deadline, the review
// ownership watchdog and the progress watchdog, then classifies the result.
// The watchdogs and the deadline outlive the classification, which reads the
// deadline's cause.
func (s *agentSession) runWatched(ctx context.Context, prompt string, additionalDirs []string) (int, string, error) {
	config := s.config
	// Create timeout context for CLI execution
	execCtx, cancelExec := withExecutionDeadline(ctx, config.ExecutionTimeout, s.taskID, sessionInflightSubmit(config, s.taskID), sessionSubmitRunning(config, s.taskID))
	defer cancelExec()
	stopReviewWatchdog, err := startReviewExecutionWatchdog(execCtx, config, s.taskID, models.NormalizeHeartbeatInterval(s.runtimeConfig.HeartbeatInterval), cancelExec)
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
	stopWatchdog := startExecutionProgressWatchdog(execCtx, config, s.taskID, progressCh, cancelExec)
	defer stopWatchdog()

	// Heartbeat is managed by RunSupervisor for the full supervisor lifetime,
	// so we don't start one here.

	// Execute CLI with timeout
	result, runErr := s.agent.Run(execCtx, LLMAgentRunRequest{
		BackendName:    config.CLIName,
		AgentID:        config.AgentID,
		Generation:     config.Authority.Generation,
		TaskID:         s.taskID,
		SessionID:      s.taskID,
		WarmSession:    false,
		ProfileName:    config.ProfileName,
		ProfileVars:    config.ProfileVars,
		Model:          config.Model,
		Prompt:         prompt,
		ProjectRoot:    config.ProjectRoot,
		AdditionalDirs: additionalDirs,
		RuntimeConfig:  s.runtimeConfig,
		EventSink:      s.eventSink(),
		LaunchGate:     s.launchGate(),
		Environment:    s.environment.Environment,
		SessionScope:   s.validation.SessionScope(),
	})
	run := sessionRun{result: result, err: runErr, execCtx: execCtx, progress: stopWatchdog()}
	if s.launch.halted {
		return 0, "", errLaunchHalted
	}
	run.reviewLost = stopReviewWatchdog()
	return s.classify(run)
}

// classify maps a finished run to the supervisor's exit code, output and error.
// A gate refusal outranks the backend's result, and a coordination outcome
// outranks provider-failure handling.
func (s *agentSession) classify(run sessionRun) (int, string, error) {
	if run.reviewLost || errors.Is(run.err, errReviewOwnershipLost) {
		// Cancellation can surface as an exit code, WaitDelay or a pipe error.
		// Preserve the coordination outcome before provider-failure handling.
		return 0, run.result.Output, errReviewOwnershipLost
	}
	if run.progress.Blocked {
		return s.classifyStalled(run)
	}

	// A session held past its timeout for its own submit ends with that
	// submit's task settled, never silently released (D-174).
	if cause := context.Cause(run.execCtx); errors.Is(cause, errInflightSubmitInterrupted) || errors.Is(cause, errSubmitHandbackEnded) {
		return endSessionAfterInflightSubmit(s.config, s.taskID, cause), run.result.Output, nil
	}

	// Check if execution timed out, or its context was cancelled even though
	// Execute returned successfully.
	if errors.Is(run.err, context.DeadlineExceeded) || executionTimedOut(run.execCtx) {
		GetLogger().Error("Agent execution timeout",
			"agent_id", s.config.AgentID,
			"timeout", s.config.ExecutionTimeout,
			"hint", "CLI may be hung, will retry")
		return 1, run.result.Output, nil // Return failure code to trigger retry
	}
	if run.err != nil && run.result.ExitCode != 0 {
		// A provider/CLI failure must reach RunSupervisor's non-zero exit path.
		// Returning it as a Go error exits the supervisor, resets its in-memory
		// loop tracker, and lets auto-repair respawn into the same task session.
		GetLogger().Warn("Agent process failed", "agent_id", s.config.AgentID, "exit_code", run.result.ExitCode, "error", run.err)
		return run.result.ExitCode, run.result.Output, nil
	}

	return run.result.ExitCode, run.result.Output, run.err
}

// classifyStalled settles a session the progress watchdog ended.
func (s *agentSession) classifyStalled(run sessionRun) (int, string, error) {
	config, taskID, logger := s.config, s.taskID, GetLogger()
	// A session the provider ended (quota, outage) looks stalled when a
	// background job keeps the CLI alive. That is a provider failure, not
	// a stalled task: the non-zero exit sends it down the supervisor's
	// provider-failure path with the claim still held (D-101).
	if providerEndedSession(run.result.Output, config.CLIName) {
		logger.Warn("Execution progress timeout after the provider ended the session; handling it as a provider failure",
			"agent_id", config.AgentID,
			"task_id", taskID)
		return 1, run.result.Output, nil
	}
	if blockErr := blockTaskFromSupervisor(db.For(config.StatePath), config.ProjectRoot, taskID, config.Authority, run.progress.Reason); blockErr != nil {
		logger.Warn("Failed to block task from execution watchdog", "error", blockErr, "task_id", taskID)
	}
	logger.Error("Agent execution stopped after stale progress",
		"agent_id", config.AgentID,
		"task_id", taskID,
		"reason", run.progress.Reason)
	return 0, run.result.Output, nil
}
