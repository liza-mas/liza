package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/roles"
)

// errGoalComplete is a sentinel error returned by waitWhilePaused when
// auto-resume detects that the goal is complete (no carried tasks, no
// pending transitions). The supervisor must handle this as a clean exit.
var errGoalComplete = errors.New("goal complete")

// checkAbort returns true if system mode is STOPPED
func checkAbort(projectRoot string) bool {
	statePath := paths.New(projectRoot).StatePath()
	if bb := db.For(statePath); bb != nil {
		state, err := bb.ReadSnapshot()
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

func logAutoResumeResult(result *ops.ResumeResult) {
	if result.TransitionError != "" {
		GetLogger().Warn("Auto-resume transition failures", "transitions_executed", result.TransitionsExecuted, "error", result.TransitionError)
	} else {
		GetLogger().Info("Auto-resume completed", "transitions_executed", result.TransitionsExecuted)
	}
}

// waitWhilePaused blocks while system is PAUSED, CIRCUIT_BREAKER_TRIPPED,
// or the current sprint checkpoint blocks this role type. Transition
// checkpoints gate orchestrator transition execution, but doer/reviewer roles
// may continue existing claimable/reviewable work.
// awaitRoleGate blocks on the role-aware PAUSE/CHECKPOINT gate and reports
// whether the supervisor must stop, with the error to return. Both of
// RunSupervisor's gate checks share it; a heartbeat failure outranks the gate
// error, and goal completion is an ordinary exit rather than a failure.
func awaitRoleGate(ctx context.Context, projectRoot, roleType string, checkHeartbeat func() error) (bool, error) {
	err := waitWhilePausedForSupervisor(ctx, projectRoot, roleType)
	if err == nil {
		return false, nil
	}
	if hbErr := checkHeartbeat(); hbErr != nil {
		return true, hbErr
	}
	if errors.Is(err, errGoalComplete) {
		GetLogger().Info("Goal complete, supervisor exiting")
		return true, nil
	}
	return true, err
}

// pausePollInterval is how often a paused supervisor re-reads the state to see
// whether the pause was lifted.
const pausePollInterval = 5 * time.Second

func waitWhilePaused(ctx context.Context, projectRoot string, roleType string) error {
	logger := GetLogger()
	statePath := paths.New(projectRoot).StatePath()

	ticker := time.NewTicker(pausePollInterval)
	defer ticker.Stop()

	for {
		// Fail closed: a state that cannot be read cannot show the pause lifted.
		isPaused := true
		pauseReason := "[UNKNOWN] cannot read state"

		if bb := db.For(statePath); bb != nil {
			state, err := bb.ReadSnapshot()
			if err != nil {
				pauseReason = fmt.Sprintf("[UNKNOWN] cannot read state: %v", err)
			} else {
				pauseReason = RolePauseReason(state, roleType)
				isPaused = pauseReason != ""
				switch {
				case state.Config.Mode == models.SystemModePaused || state.Config.Mode == models.SystemModeCircuitBreakerTripped:
					// System pause takes precedence over automatic sprint resume.
				case state.Sprint.Status == models.SprintStatusCheckpoint:
					// Before anything moves the sprint on — including
					// auto-resume below — claim the checkpoint's report. The
					// claim fixes its scope; the CLI then runs in the
					// background, so auto-resume does not wait for it. This gate is the only place every checkpoint is
					// observed: it polls on a timer, re-reads state each
					// poll, and blocks every role, so a checkpoint made from
					// the TUI, the CLI or the circuit breaker while the
					// orchestrator is idle is seen here even though no
					// orchestrator turn follows it.
					maybeEmitCheckpointSummary(ctx, bb, projectRoot, roleType, state)
					if state.Config.AutoResume && (!models.IsTransitionCheckpointTrigger(state.Sprint.CheckpointTrigger) || roleType == string(roles.RoleTypeOrchestrator)) {
						logger.Info("Auto-resuming from CHECKPOINT")
						if result, resumeErr := resumeCheckpoint(projectRoot, "auto-resume"); resumeErr != nil {
							logger.Warn("Auto-resume failed, waiting for next poll", "error", resumeErr)
						} else {
							logAutoResumeResult(result)
							continue // state changed, re-read immediately
						}
					}
				case state.Sprint.Status == models.SprintStatusCompleted && state.Config.AutoResume:
					logger.Info("Auto-resuming from COMPLETED")
					result, resumeErr := resumeCompletedSprint(projectRoot, "auto-resume")
					if resumeErr != nil {
						logger.Warn("Auto-resume from COMPLETED failed, waiting", "error", resumeErr)
					} else {
						logAutoResumeResult(result)
						if completionErr := stopAfterCompletedResume(projectRoot, result, stopCompletedGoal); completionErr != nil {
							if errors.Is(completionErr, errGoalComplete) {
								logger.Info("Goal complete — clean integration evidence stopped the system.")
							}
							return completionErr
						}
						continue // state changed, re-read immediately
					}
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

// RolePauseReason is the non-blocking half of waitWhilePaused. Launch
// revalidation uses the same predicate without running resume side effects.
func RolePauseReason(state *models.State, roleType string) string {
	switch {
	case state.Config.Mode == models.SystemModePaused:
		return "[PAUSED] System mode is PAUSED"
	case state.Config.Mode == models.SystemModeCircuitBreakerTripped:
		return "[CIRCUIT BREAKER] Circuit breaker triggered - system halted"
	case state.Sprint.Status == models.SprintStatusCheckpoint:
		_, reason := checkpointBlocksRole(state, roleType)
		return reason
	case state.Sprint.Status == models.SprintStatusCompleted && state.Config.AutoResume:
		return "[COMPLETED] Sprint completed, awaiting resume"
	default:
		return ""
	}
}

func checkpointBlocksRole(state *models.State, roleType string) (bool, string) {
	if state == nil || state.Sprint.Status != models.SprintStatusCheckpoint {
		return false, ""
	}

	if models.IsTransitionCheckpointTrigger(state.Sprint.CheckpointTrigger) {
		switch roleType {
		case string(roles.RoleTypeDoer), string(roles.RoleTypeReviewer):
			return false, ""
		case string(roles.RoleTypeOrchestrator):
			return true, fmt.Sprintf("[CHECKPOINT] Transition gate pending - run %q to create downstream tasks", brand.Command("resume"))
		default:
			return true, "[CHECKPOINT] Transition gate pending - unknown role type blocked"
		}
	}

	return true, "[CHECKPOINT] Sprint is at checkpoint"
}
