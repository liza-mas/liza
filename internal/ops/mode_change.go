package ops

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

const (
	goalCompleteStopReservedNamespace = "system:goal-complete:"
	goalCompleteStopTokenPrefix       = goalCompleteStopReservedNamespace + "v1:"
	goalCompleteStopOperationIDBytes  = 16
	goalCompleteStopWriteStop         = "stop"
	goalCompleteStopWriteRestore      = "restore"
)

type goalCompleteStopToken struct {
	AnalysisKey  string `json:"analysis_key"`
	Generation   int    `json:"generation"`
	SourceCommit string `json:"source_commit"`
	OperationID  string `json:"operation_id"`
}

var (
	goalCompleteStopNow                        = time.Now
	generateGoalCompleteStopOperationID        = newGoalCompleteStopOperationID
	afterGoalCompleteStopAuthorizationTestHook func()
	beforeGoalCompleteStopStateWriteTestHook   func(string)
	afterGoalCompleteStopModeWriteTestHook     func(string)
)

// ModeChangeResult contains the outcome of a system mode change.
type ModeChangeResult struct {
	Previous  models.SystemMode
	New       models.SystemMode
	ChangedBy string
	Reason    string
}

// Start transitions system mode from STOPPED to RUNNING. No terminal I/O.
func Start(projectRoot, reason, changedBy string) (*ModeChangeResult, error) {
	return changeMode(projectRoot, reason, changedBy, models.SystemModeRunning)
}

// Stop transitions system mode to STOPPED. Agents detect this and exit
// cleanly. No terminal I/O.
func Stop(projectRoot, reason, changedBy string) (*ModeChangeResult, error) {
	if strings.HasPrefix(changedBy, goalCompleteStopReservedNamespace) {
		return nil, &PreconditionError{Reason: "changed-by identity is reserved for automatic goal completion"}
	}
	return changeMode(projectRoot, reason, changedBy, models.SystemModeStopped)
}

// StopForGoalCompletion stops only for clean integration evidence at current
// HEAD and records exact ownership in the reserved ModeChangedBy token.
func StopForGoalCompletion(projectRoot, reason string) (*ModeChangeResult, error) {
	authorization, err := authorizeEffectiveIntegrationCompletion(projectRoot, true)
	if err != nil {
		return nil, err
	}
	operationID, err := generateGoalCompleteStopOperationID()
	if err != nil {
		return nil, fmt.Errorf("generate goal-complete stop operation ID: %w", err)
	}
	rawToken, err := encodeGoalCompleteStopToken(goalCompleteStopToken{
		AnalysisKey:  authorization.analysisKey,
		Generation:   authorization.generation,
		SourceCommit: authorization.sourceCommit,
		OperationID:  operationID,
	})
	if err != nil {
		return nil, fmt.Errorf("encode goal-complete stop token: %w", err)
	}
	if afterGoalCompleteStopAuthorizationTestHook != nil {
		afterGoalCompleteStopAuthorizationTestHook()
	}

	blackboard := db.For(paths.New(projectRoot).StatePath())
	timestamp := goalCompleteStopNow()
	var previousMode models.SystemMode
	err = withEffectiveIntegrationCompletionLinearization(projectRoot, "goal-complete stop", func() error {
		if beforeGoalCompleteStopStateWriteTestHook != nil {
			beforeGoalCompleteStopStateWriteTestHook(goalCompleteStopWriteStop)
		}
		return blackboard.Modify(func(state *models.State) error {
			if err := authorization.validateState(state, true); err != nil {
				return err
			}
			previousMode = state.Config.Mode
			if previousMode == "" {
				previousMode = models.SystemModeRunning
			}
			if err := previousMode.ValidateTransition(models.SystemModeStopped); err != nil {
				return &PreconditionError{Reason: err.Error()}
			}
			state.Config.Mode = models.SystemModeStopped
			state.Config.ModeChangedAt = &timestamp
			state.Config.ModeChangedBy = &rawToken
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	if afterGoalCompleteStopModeWriteTestHook != nil {
		afterGoalCompleteStopModeWriteTestHook(rawToken)
	}

	snapshot, verificationErr := readEffectiveIntegrationCompletion(projectRoot)
	if verificationErr == nil && goalCompleteStopAuthorizationMatches(authorization, snapshot) {
		return &ModeChangeResult{
			Previous: previousMode, New: models.SystemModeStopped, ChangedBy: rawToken, Reason: reason,
		}, nil
	}
	restoreErr := restoreRunningForExactGoalCompleteStop(projectRoot, rawToken)
	if verificationErr != nil {
		return nil, errors.Join(fmt.Errorf("verify goal-complete stop: %w", verificationErr), restoreErr)
	}
	return nil, errors.Join(integrationCompletionPreconditionError(&IntegrationProgressReason{Code: "integration_state_changed"}), restoreErr)
}

func goalCompleteStopAuthorizationMatches(
	authorization *effectiveIntegrationCompletionAuthorization,
	snapshot effectiveIntegrationCompletionSnapshot,
) bool {
	closure := snapshot.closure
	return snapshot.decision.IntegrationComplete && snapshot.cohortFrozen && closure != nil &&
		closure.Status == models.IntegrationClosureStatusClean &&
		closure.Generation == authorization.generation &&
		closure.AnalysisKey == authorization.analysisKey &&
		closure.SourceCommit == authorization.sourceCommit &&
		snapshot.mutationReceiptCount == authorization.mutationReceiptCount
}

func restoreRunningForExactGoalCompleteStop(projectRoot, rawToken string) error {
	return withEffectiveIntegrationCompletionLinearization(projectRoot, "restore stale goal-complete stop", func() error {
		if beforeGoalCompleteStopStateWriteTestHook != nil {
			beforeGoalCompleteStopStateWriteTestHook(goalCompleteStopWriteRestore)
		}
		blackboard := db.For(paths.New(projectRoot).StatePath())
		return blackboard.Modify(func(state *models.State) error {
			if state.Config.Mode != models.SystemModeStopped || state.Config.ModeChangedBy == nil ||
				*state.Config.ModeChangedBy != rawToken {
				return nil
			}
			if err := state.Config.Mode.ValidateTransition(models.SystemModeRunning); err != nil {
				return &PreconditionError{Reason: err.Error()}
			}
			timestamp := goalCompleteStopNow()
			changedBy := "system:integration-head-verification"
			state.Config.Mode = models.SystemModeRunning
			state.Config.ModeChangedAt = &timestamp
			state.Config.ModeChangedBy = &changedBy
			return nil
		})
	})
}

func invalidateGoalCompleteStopForMutation(state *models.State, receipt models.IntegrationMutationReceipt) error {
	if state.Config.Mode != models.SystemModeStopped || state.Config.ModeChangedBy == nil {
		return nil
	}
	rawToken := *state.Config.ModeChangedBy
	token, ok := decodeGoalCompleteStopToken(rawToken)
	if !ok || token.SourceCommit != receipt.BeforeCommit || state.Config.ModeChangedBy == nil ||
		*state.Config.ModeChangedBy != rawToken {
		return nil
	}
	if err := state.Config.Mode.ValidateTransition(models.SystemModeRunning); err != nil {
		return &PreconditionError{Reason: err.Error()}
	}
	timestamp := time.Now()
	changedBy := receipt.TaskID
	state.Config.Mode = models.SystemModeRunning
	state.Config.ModeChangedAt = &timestamp
	state.Config.ModeChangedBy = &changedBy
	return nil
}

func newGoalCompleteStopOperationID() (string, error) {
	raw := make([]byte, goalCompleteStopOperationIDBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func encodeGoalCompleteStopToken(token goalCompleteStopToken) (string, error) {
	if !validGoalCompleteStopToken(token) {
		return "", fmt.Errorf("goal-complete stop token is incomplete")
	}
	payload, err := json.Marshal(token)
	if err != nil {
		return "", err
	}
	return goalCompleteStopTokenPrefix + base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeGoalCompleteStopToken(rawToken string) (goalCompleteStopToken, bool) {
	if !strings.HasPrefix(rawToken, goalCompleteStopTokenPrefix) {
		return goalCompleteStopToken{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(rawToken, goalCompleteStopTokenPrefix))
	if err != nil {
		return goalCompleteStopToken{}, false
	}
	var token goalCompleteStopToken
	if err := json.Unmarshal(payload, &token); err != nil || !validGoalCompleteStopToken(token) {
		return goalCompleteStopToken{}, false
	}
	canonical, err := json.Marshal(token)
	if err != nil || !bytes.Equal(payload, canonical) {
		return goalCompleteStopToken{}, false
	}
	return token, true
}

func validGoalCompleteStopToken(token goalCompleteStopToken) bool {
	operationID, err := base64.RawURLEncoding.DecodeString(token.OperationID)
	return token.AnalysisKey != "" && token.Generation > 0 && token.SourceCommit != "" &&
		err == nil && len(operationID) == goalCompleteStopOperationIDBytes
}

// Pause transitions system mode to PAUSED. Agents block until resumed.
// No terminal I/O.
func Pause(projectRoot, reason, changedBy string) (*ModeChangeResult, error) {
	return changeMode(projectRoot, reason, changedBy, models.SystemModePaused)
}

// changeMode is the shared implementation for Start, Stop, and Pause.
// It validates the transition via the systemModeTransitions table and applies it.
func changeMode(projectRoot, reason, changedBy string, target models.SystemMode) (*ModeChangeResult, error) {
	statePath := paths.New(projectRoot).StatePath()
	blackboard := db.For(statePath)

	timestamp := time.Now()
	var previousMode models.SystemMode

	err := blackboard.Modify(func(s *models.State) error {
		previousMode = s.Config.Mode
		if previousMode == "" {
			previousMode = models.SystemModeRunning
		}

		if err := previousMode.ValidateTransition(target); err != nil {
			return &PreconditionError{Reason: err.Error()}
		}
		if target == models.SystemModeRunning && hasActiveHaltResponse(s) {
			return &PreconditionError{Reason: fmt.Sprintf("cannot start while an active HALT response is unresolved; use %q to acknowledge it", brand.Command("resume"))}
		}

		s.Config.Mode = target
		s.Config.ModeChangedAt = &timestamp
		s.Config.ModeChangedBy = &changedBy

		return nil
	})

	if err != nil {
		return nil, err
	}

	return &ModeChangeResult{
		Previous:  previousMode,
		New:       target,
		ChangedBy: changedBy,
		Reason:    reason,
	}, nil
}

func hasActiveHaltResponse(s *models.State) bool {
	if s.CircuitBreaker.CurrentResponse != nil {
		return s.CircuitBreaker.CurrentResponse.Response == models.CircuitBreakerResponseHalt
	}
	return s.CircuitBreaker.Status == "TRIGGERED" && s.CircuitBreaker.CurrentTrigger != nil
}

// ResumeResult contains the outcome of a system resume.
type ResumeResult struct {
	ResumedFrom          string
	ChangedBy            string
	SystemRemainsStopped bool                 // true when Resume acknowledged a HALT without restarting a stopped system
	SprintAdvanced       *AdvanceSprintResult // non-nil when sprint was advanced to next
	TransitionsExecuted  int                  // number of transitions fired on advance
	TransitionError      string               // non-empty if post-advance transitions failed
}

// resumeSystemMode transitions PAUSED or CIRCUIT_BREAKER_TRIPPED to RUNNING.
// Returns a description of what was resumed, or empty if mode was already running.
func resumeSystemMode(s *models.State, timestamp time.Time, changedBy string) string {
	switch s.Config.Mode {
	case models.SystemModePaused:
		s.Config.Mode = models.SystemModeRunning
		s.Config.ModeChangedAt = &timestamp
		s.Config.ModeChangedBy = &changedBy
		return "PAUSED mode"
	case models.SystemModeCircuitBreakerTripped:
		s.Config.Mode = models.SystemModeRunning
		s.Config.ModeChangedAt = &timestamp
		s.Config.ModeChangedBy = &changedBy
		s.CircuitBreaker.Status = "OK"
		s.CircuitBreaker.CurrentTrigger = nil
		return "CIRCUIT_BREAKER_TRIPPED mode"
	default:
		return ""
	}
}

func acknowledgeCircuitBreakerResponse(s *models.State, timestamp time.Time, changedBy string) error {
	response := s.CircuitBreaker.CurrentResponse
	if response == nil {
		return nil
	}
	if response.Response != models.CircuitBreakerResponseCheckpoint && response.Response != models.CircuitBreakerResponseHalt {
		return fmt.Errorf("cannot acknowledge unsupported circuit-breaker response %q", response.Response)
	}

	for i := len(s.CircuitBreaker.History) - 1; i >= 0; i-- {
		entry := &s.CircuitBreaker.History[i]
		if !entry.Timestamp.Equal(response.Timestamp) || entry.Response != response.Response || entry.Pattern == nil || *entry.Pattern != response.Pattern {
			continue
		}

		resolution := fmt.Sprintf("resumed by %s", changedBy)
		entry.Resolution = &resolution
		entry.ResolvedAt = &timestamp
		s.CircuitBreaker.CurrentResponse = nil
		if response.Response == models.CircuitBreakerResponseHalt {
			s.CircuitBreaker.Status = "OK"
			s.CircuitBreaker.CurrentTrigger = nil
		}
		return nil
	}

	return fmt.Errorf("active circuit-breaker response has no matching history boundary")
}

// resumeSprint handles CHECKPOINT and COMPLETED sprint transitions.
// Returns a description and optional advance result. No-op when sprint is in
// neither state.
func resumeSprint(s *models.State, lizaPaths paths.LizaPaths, projectRoot string, timestamp time.Time) (string, *AdvanceSprintResult, error) {
	switch s.Sprint.Status {
	case models.SprintStatusCompleted:
		// COMPLETED sprint — archive and create new sprint.
		// Pipeline transitions are executed post-Modify by the caller (Resume).
		plan, err := planSprintAdvanceFromCompleted(s, timestamp.UTC(), projectRoot)
		if err != nil {
			return "", nil, fmt.Errorf("sprint advance failed: %w", err)
		}
		archivePath := lizaPaths.SprintArchivePath(plan.archivedSprint.Number)

		if err := writeSprintArchive(archivePath, &plan.archivedSprint); err != nil {
			return "", nil, fmt.Errorf("archive write failed (state unchanged): %w", err)
		}

		applySprintAdvance(s, plan)
		return "COMPLETED sprint", &AdvanceSprintResult{
			ArchivedSprintID: plan.archivedSprint.ID,
			NewSprintID:      plan.newSprintID,
			NewSprintNumber:  plan.newNumber,
			CarriedTasks:     plan.carriedTasks,
			ArchivePath:      archivePath,
		}, nil

	case models.SprintStatusCheckpoint:
		if models.IsTransitionCheckpointTrigger(s.Sprint.CheckpointTrigger) {
			// Transition checkpoints are review gates for downstream task creation,
			// even when every current planned task is terminal. Resume the same
			// sprint so post-resume transition execution can add child work before
			// sprint-completion handling runs.
			s.Sprint.Status = models.SprintStatusInProgress
			return "CHECKPOINT", nil, nil
		}

		allTerminal, termErr := allPlannedTasksTerminalForProject(s, projectRoot)
		if termErr != nil {
			return "", nil, termErr
		}
		if allTerminal {
			// Sprint is truly done — mark COMPLETED for human review.
			// Human runs liza proceed, then liza resume again to advance.
			s.Sprint.Status = models.SprintStatusCompleted
			// Clear trigger — COMPLETED sprint won't run orchestrator PreWork.
			s.Sprint.CheckpointTrigger = ""
		} else {
			// Mid-sprint checkpoint — resume the same sprint.
			// checkpoint_trigger is preserved so orchestrator PreWork can check it.
			// PreWork clears it after executing transitions.
			s.Sprint.Status = models.SprintStatusInProgress
		}
		return "CHECKPOINT", nil, nil

	default:
		return "", nil, nil
	}
}

func resumeRequiresEffectiveIntegrationCompletion(state *models.State, projectRoot string) (bool, error) {
	switch state.Sprint.Status {
	case models.SprintStatusCompleted:
		return true, nil
	case models.SprintStatusCheckpoint:
		if models.IsTransitionCheckpointTrigger(state.Sprint.CheckpointTrigger) {
			return false, nil
		}
		return allPlannedTasksTerminalForProject(state, projectRoot)
	default:
		return false, nil
	}
}

type resumeOrigin uint8

const (
	resumeOriginOperator resumeOrigin = iota
	resumeOriginAutomatic
)

// Resume transitions from PAUSED or CIRCUIT_BREAKER_TRIPPED to RUNNING,
// and/or resumes sprint from CHECKPOINT or COMPLETED. As an explicit operator
// action, it may acknowledge an active HALT while leaving a STOPPED system
// stopped. No terminal I/O.
//
// Sprint transitions:
//   - CHECKPOINT + not all terminal → IN_PROGRESS (mid-sprint resume)
//   - CHECKPOINT + all terminal → COMPLETED (sprint done, ready for proceed)
//   - COMPLETED → archive sprint, create new IN_PROGRESS sprint (advance)
//
// Mode changes and sprint operations happen in a single Modify to avoid
// partial mutations on failure.
func Resume(projectRoot, changedBy string) (*ResumeResult, error) {
	return resume(projectRoot, changedBy, resumeOriginOperator)
}

// AutoResume performs automatic checkpoint/completion recovery. It cannot
// acknowledge an active HALT while the system is STOPPED; that boundary
// requires an explicit operator Resume.
func AutoResume(projectRoot, changedBy string) (*ResumeResult, error) {
	return resume(projectRoot, changedBy, resumeOriginAutomatic)
}

func resume(projectRoot, changedBy string, origin resumeOrigin) (*ResumeResult, error) {
	lizaPaths := paths.New(projectRoot)
	statePath := lizaPaths.StatePath()
	blackboard := db.For(statePath)
	preflightState, err := blackboard.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read state before resume: %w", err)
	}
	preflightStoppedWithActiveHalt := origin == resumeOriginOperator &&
		preflightState.Config.Mode == models.SystemModeStopped && hasActiveHaltResponse(preflightState)
	requiresCompletion := false
	if !preflightStoppedWithActiveHalt {
		requiresCompletion, err = resumeRequiresEffectiveIntegrationCompletion(preflightState, projectRoot)
		if err != nil {
			return nil, fmt.Errorf("failed to evaluate resume completion branch: %w", err)
		}
	}
	timestamp := time.Now()
	var resumedFrom string
	var systemRemainsStopped bool
	var advanceResult *AdvanceSprintResult
	runTransitionsAfterResume := false

	resumeMutation := func(completionAuthorization *effectiveIntegrationCompletionAuthorization) error {
		return blackboard.Modify(func(s *models.State) error {
			timestamp = time.Now()
			currentMode := s.Config.Mode
			if currentMode == "" {
				currentMode = models.SystemModeRunning
			}

			stoppedWithActiveHalt := origin == resumeOriginOperator &&
				currentMode == models.SystemModeStopped && hasActiveHaltResponse(s)
			// STOPPED normally rejects resume. An active HALT is the sole exception:
			// acknowledge it without restarting agents or mutating sprint state.
			if currentMode == models.SystemModeStopped && !stoppedWithActiveHalt {
				return &PreconditionError{Reason: "cannot resume from STOPPED state (system must be restarted)"}
			}

			canResumeMode := currentMode == models.SystemModePaused || currentMode == models.SystemModeCircuitBreakerTripped || stoppedWithActiveHalt
			canResumeSprint := s.Sprint.Status == models.SprintStatusCheckpoint || s.Sprint.Status == models.SprintStatusCompleted

			if !canResumeMode && !canResumeSprint {
				return &PreconditionError{Reason: fmt.Sprintf("system is not PAUSED, circuit breaker not tripped, and sprint is not at CHECKPOINT or COMPLETED (current mode: %s, sprint status: %s)", currentMode, s.Sprint.Status)}
			}
			if !stoppedWithActiveHalt {
				currentRequiresCompletion, completionErr := resumeRequiresEffectiveIntegrationCompletion(s, projectRoot)
				if completionErr != nil {
					return completionErr
				}
				if currentRequiresCompletion {
					if completionAuthorization == nil {
						return integrationCompletionPreconditionError(&IntegrationProgressReason{Code: "integration_state_changed"})
					}
					if err := completionAuthorization.validateState(s, false); err != nil {
						return err
					}
				}
			}

			wasTransitionCheckpoint := s.Sprint.Status == models.SprintStatusCheckpoint &&
				models.IsTransitionCheckpointTrigger(s.Sprint.CheckpointTrigger)

			if stoppedWithActiveHalt {
				resumedFrom = "active HALT response"
				systemRemainsStopped = true
				if s.CircuitBreaker.CurrentResponse == nil {
					// Legacy HALTs have no typed history boundary to resolve.
					s.CircuitBreaker.Status = "OK"
					s.CircuitBreaker.CurrentTrigger = nil
				}
			} else {
				resumedFrom = resumeSystemMode(s, timestamp, changedBy)

				sprintDesc, advResult, err := resumeSprint(s, lizaPaths, projectRoot, timestamp)
				if err != nil {
					return err
				}
				advanceResult = advResult
				runTransitionsAfterResume = advResult != nil ||
					(wasTransitionCheckpoint && s.Sprint.Status == models.SprintStatusInProgress)
				if sprintDesc != "" {
					if resumedFrom != "" {
						resumedFrom += " and " + sprintDesc
					} else {
						resumedFrom = sprintDesc
					}
				}
			}
			if err := acknowledgeCircuitBreakerResponse(s, timestamp, changedBy); err != nil {
				return err
			}

			return nil
		})
	}
	if requiresCompletion {
		err = withEffectiveIntegrationCompletionAuthorization(projectRoot, "resume", false, resumeMutation)
	} else {
		err = resumeMutation(nil)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to resume system: %w", err)
	}

	// After sprint advance, execute available transitions so child tasks are
	// created in the new sprint. This handles merged planning tasks with
	// unconsumed output[] (e.g., epic → US writing, code plan → coding).
	// Mid-sprint transition checkpoints use the same path so ready planning or
	// many-to-one output can hand off without waiting for a separate orchestrator
	// PreWork cycle. The human already reviewed by resuming from the
	// checkpoint/COMPLETED state; transitions are idempotent via TransitionsExecuted.
	var transitionsExecuted int
	var transitionError string
	if runTransitionsAfterResume {
		// An automatic resume carries no human review, so the reviewed plan
		// hand-off needs the orchestrator's pass; an operator resume is that review.
		admission := AdmitOperator
		if origin == resumeOriginAutomatic {
			admission = AdmitReviewed
		}
		if report, err := ExecuteTransitionsReportWith(projectRoot, "", admission); err != nil {
			transitionError = err.Error()
		} else {
			transitionsExecuted = len(report.Results)
			failures := make([]string, 0, len(report.Failures)+1)
			for _, failure := range report.Failures {
				failures = append(failures, failure.String())
			}
			if err := clearTransitionCheckpointTrigger(projectRoot); err != nil {
				failures = append(failures, err.Error())
			}
			transitionError = strings.Join(failures, "; ")
		}
	}

	return &ResumeResult{
		ResumedFrom:          resumedFrom,
		ChangedBy:            changedBy,
		SystemRemainsStopped: systemRemainsStopped,
		SprintAdvanced:       advanceResult,
		TransitionsExecuted:  transitionsExecuted,
		TransitionError:      transitionError,
	}, nil
}

func clearTransitionCheckpointTrigger(projectRoot string) error {
	statePath := paths.New(projectRoot).StatePath()
	blackboard := db.For(statePath)
	return blackboard.Modify(func(s *models.State) error {
		if models.IsTransitionCheckpointTrigger(s.Sprint.CheckpointTrigger) {
			s.Sprint.CheckpointTrigger = ""
		}
		return nil
	})
}
