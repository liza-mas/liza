package agent

import (
	"errors"
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/providersignal"
)

const (
	// exitCodeGracefulAbort is the provider exit code that asks the
	// supervisor to restart the task (exit-42 restart policy).
	exitCodeGracefulAbort = 42
	// restartTrackerFailureDelay paces the retry when the exit-42 tracker
	// cannot record the restart.
	restartTrackerFailureDelay = 2 * time.Second
)

// executeTurn runs the provider session. A start the halt, a lost review claim
// or lock contention refused is no completed turn: the claim is released
// without charging any loop budget. Otherwise it records the exit code and
// output on the turn.
func (lt *supervisorLifetime) executeTurn(turn *supervisorTurn) (loopFlow, error) {
	config := lt.config
	exitCode, output, err := executeAgent(lt.supervisorCtx, config, turn.prompt, nil, turn.effectiveTask, turn.stateBefore.Config)
	if halted := errors.Is(err, errLaunchHalted); halted || errors.Is(err, errReviewOwnershipLost) || errors.Is(err, errLaunchContended) {
		return lt.releaseUnstarted(turn, halted, errors.Is(err, errLaunchContended))
	}
	// Drain before ownership reset; cancellation retains pending verdicts.
	if lt.supervisorCtx.Err() == nil {
		if replayErr := ops.ReplayPendingVerdicts(lt.supervisorCtx, config.ProjectRoot, config.AgentID); replayErr != nil {
			err = errors.Join(err, fmt.Errorf("replay reached verdicts after provider exit: %w", replayErr))
		}
	}
	if err != nil {
		if hbErr := lt.checkHeartbeat(); hbErr != nil {
			return flowExit, errors.Join(err, hbErr)
		}
		return flowExit, fmt.Errorf("agent execution error: %w", err)
	}
	// Preserve completed-turn bookkeeping when cancellation ends the loop.
	if lt.supervisorCtx.Err() != nil {
		lt.switchToCleanupBlackboard()
	}
	turn.exitCode, turn.output = exitCode, output
	return flowProceed, nil
}

func (lt *supervisorLifetime) releaseUnstarted(turn *supervisorTurn, halted, contended bool) (loopFlow, error) {
	if releaseErr := releaseUnstartedTurn(lt.bb, lt.config, turn.effectiveTask, halted, contended); releaseErr != nil {
		if ops.IsAgentAuthorityError(releaseErr) {
			return flowExit, nil
		}
		return flowExit, releaseErr
	}
	trackers := &lt.trackers
	trackers.spin.reset(turn.effectiveTask)
	trackers.crash.reset(turn.effectiveTask)
	trackers.exit42.reset(turn.taskID)
	trackers.runtimeFailure.reset(turn.effectiveTask)
	// Pace repeated claim/launch disagreement; a halt then parks at the gate.
	if err := lt.waitForDelay(lt.pollInterval); err != nil {
		return flowExit, err
	}
	return flowContinue, nil
}

// completeTurn applies the completed-turn policy: an observed runtime failure
// retries, otherwise quota WIP is committed before the release, the release is
// witnessed, the provider audit runs and the exit code selects the restart
// policy.
func (lt *supervisorLifetime) completeTurn(turn *supervisorTurn) (loopFlow, error) {
	config := lt.config
	trackers := &lt.trackers
	if turn.exitCode == 0 && turn.effectiveTask != "" {
		if failure := detectObservedRuntimeFailure(turn.output); failure != nil {
			handleObservedRuntimeFailureRetry(lt.bb, config, turn.effectiveTask, turn.stateBefore.Config, *failure, trackers.runtimeFailure, trackers.spin)
			if err := resetAgentAfterExit(lt.bb, config.Authority, config.ProjectRoot); err != nil {
				GetLogger().Warn("Failed to reset agent status after runtime failure", "error", err, "agent_id", config.AgentID)
			}
			trackers.exit42.reset(turn.taskID)
			trackers.crash.reset(turn.effectiveTask)
			trackers.consecutiveCrashes = 0
			return flowContinue, nil
		}
	}

	commitQuotaTerminationWIP(lt.supervisorCtx, config, turn.exitCode, turn.effectiveTask, turn.output)
	// Reset runtime status after CLI exits, but preserve explicit command-driven
	// states such as WAITING and HANDOFF. The loop blocks below act after
	// this release, so they may only block the continuation it left.
	released, err := releaseAgentAfterExit(lt.bb, config.Authority, config.ProjectRoot)
	if err != nil {
		GetLogger().Warn("Failed to reset agent status after exit", "error", err, "agent_id", config.AgentID)
	}
	turn.released = released

	if _, auditErr := handleProviderAuditDegraded(lt.bb, config, turn.effectiveTask, turn.output); auditErr != nil {
		return flowExit, auditErr
	}

	switch turn.exitCode {
	case 0:
		lt.finishSuccessfulTurn(turn)
		return flowContinue, nil
	case exitCodeGracefulAbort:
		return lt.restartAfterGracefulAbort(turn)
	default:
		return lt.recoverFromCrash(turn)
	}
}

func (lt *supervisorLifetime) finishSuccessfulTurn(turn *supervisorTurn) {
	config := lt.config
	trackers := &lt.trackers
	if turn.effectiveTask != "" {
		trackers.runtimeFailure.reset(turn.effectiveTask)
	}

	if !preserveClaimForBackgroundJob(lt.bb, config, turn.effectiveTask, turn.stateBefore.Config, turn.output, turn.released, trackers.backgroundTask, trackers.spin) {
		GetLogger().Info("Agent completed, checking for more work")
		if err := lt.strategy.PostExecution(lt.bb, config, turn.taskID, turn.claimedTaskID, turn.stateBefore); err != nil {
			GetLogger().Warn("Post-execution error", "error", err)
		}
	}
	trackers.exit42.reset(turn.taskID)
	trackers.crash.reset(turn.effectiveTask)
	trackers.consecutiveCrashes = 0
}

// restartAfterGracefulAbort charges the exit-42 restart budget and paces the
// restart, or leaves the task blocked once the budget is spent.
func (lt *supervisorLifetime) restartAfterGracefulAbort(turn *supervisorTurn) (loopFlow, error) {
	config := lt.config
	trackers := &lt.trackers
	trackers.consecutiveCrashes = 0
	restartTaskID := turn.exit42TaskID()
	trackers.runtimeFailure.reset(restartTaskID)

	outcome, trackErr := trackers.exit42.Handle(lt.bb, config.ProjectRoot, config.Role, restartTaskID, config.Authority)
	if trackErr != nil {
		GetLogger().Warn("Exit-42 tracker failed, using default retry delay",
			"error", trackErr,
			"task_id", restartTaskID)
		return lt.pace(restartTrackerFailureDelay)
	}

	if outcome.BlockedTask {
		GetLogger().Warn("Task transitioned to BLOCKED after repeated exit 42 restarts",
			"task_id", restartTaskID,
			"restart_count", outcome.RestartCount)
		return flowContinue, nil
	}

	GetLogger().Info("Agent aborted gracefully, restarting",
		"exit_code", exitCodeGracefulAbort,
		"task_id", restartTaskID,
		"restart_count", outcome.RestartCount,
		"delay_seconds", int(outcome.Delay/time.Second))
	return lt.pace(outcome.Delay)
}

// pace waits out a restart delay, then ends the iteration.
func (lt *supervisorLifetime) pace(delay time.Duration) (loopFlow, error) {
	if err := lt.waitForDelay(delay); err != nil {
		return flowExit, err
	}
	return flowContinue, nil
}

// recoverFromCrash handles a non-zero, non-42 exit: a provider-scoped failure
// stops the supervisor, anything else backs off and, past the per-task crash
// threshold without progress, blocks the task.
func (lt *supervisorLifetime) recoverFromCrash(turn *supervisorTurn) (loopFlow, error) {
	config := lt.config
	trackers := &lt.trackers
	trackers.exit42.reset(turn.taskID)
	trackers.runtimeFailure.reset(turn.effectiveTask)

	// Check if crash was caused by a provider-scoped failure.
	if handleClassifiedProviderCrash(config, turn.output) {
		return flowExit, nil
	}
	trackers.consecutiveCrashes++
	delay := crashRestartDelay(trackers.consecutiveCrashes)

	// Track crash restarts per task.
	if turn.effectiveTask != "" && lt.crashLoopBlocked(turn) {
		// Blocking the task does not clear a failure shared by
		// every task; pace the next claim as any other crash.
		return lt.pace(delay)
	}

	GetLogger().Error("Agent crashed, restarting",
		"exit_code", turn.exitCode,
		"consecutive_crashes", trackers.consecutiveCrashes,
		"delay_seconds", int(delay/time.Second))
	return lt.pace(delay)
}

// crashLoopBlocked charges one crash to the task and, past the threshold
// without progress, alerts and blocks the continuation the release left.
func (lt *supervisorLifetime) crashLoopBlocked(turn *supervisorTurn) bool {
	config := lt.config
	taskID := turn.effectiveTask
	var sig string
	if s, rErr := lt.bb.ReadSnapshot(); rErr == nil {
		if task := s.FindTask(taskID); task != nil {
			sig = exit42TaskProgressSignature(task)
		}
	}
	crashCount := lt.trackers.crash.Increment(taskID, sig)
	crashThreshold := effectiveCrashRestartThreshold(turn.stateBefore.Config)
	if crashCount <= crashThreshold {
		return false
	}
	reason := fmt.Sprintf("crash restart loop detected: %d consecutive crashes for task %s without progress (threshold=%d, last exit code=%d)",
		crashCount, taskID, crashThreshold, turn.exitCode)
	GetLogger().Error("Crash restart loop detected, blocking task",
		"task_id", taskID,
		"agent_id", config.AgentID,
		"crash_count", crashCount,
		"exit_code", turn.exitCode)
	if alertErr := providersignal.LogAlert(config.ProjectRoot, alertLevelCritical, alertCategoryCrashRestartLoop, reason); alertErr != nil {
		GetLogger().Warn("Failed to write crash alert", "error", alertErr)
	}
	if blockErr := blockReleasedTaskFromSupervisor(lt.bb, config.ProjectRoot, taskID, config.Authority, turn.released, reason); blockErr != nil {
		GetLogger().Warn("Failed to block task from supervisor", "error", blockErr, "task_id", taskID)
	}
	lt.trackers.crash.reset(taskID)
	return true
}
