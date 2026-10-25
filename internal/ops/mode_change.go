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

// Operation names of the two mode commands that also label their locks and
// refusals.
const (
	pauseOperation  = "pause"
	resumeOperation = "resume"
)

const (
	goalCompleteStopReservedNamespace = "system:goal-complete:"
	goalCompleteStopTokenPrefix       = goalCompleteStopReservedNamespace + "v1:"
	goalCompleteStopOperationIDBytes  = 16
	goalCompleteStopWriteStop         = "stop"
	goalCompleteStopWriteRestore      = "restore"

	// goalCompleteRestoredBy is the changed-by identity of the restore that
	// follows a stop whose completion no longer verifies.
	goalCompleteRestoredBy = "system:integration-head-verification"

	// Linearization operation labels of the two goal-complete mode writes.
	goalCompleteStopLinearization    = "goal-complete stop"
	goalCompleteRestoreLinearization = "restore stale goal-complete stop"
)

// Persisted circuit-breaker statuses. models.CircuitBreaker.Status is a plain
// string and models declares no constants for its two values.
const (
	circuitBreakerStatusOK        = "OK"
	circuitBreakerStatusTriggered = "TRIGGERED"
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
	err = withEffectiveIntegrationCompletionLinearization(projectRoot, goalCompleteStopLinearization, func() error {
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
	return nil, errors.Join(integrationCompletionPreconditionError(&IntegrationProgressReason{Code: integrationStateChangedCode}), restoreErr)
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
	return withEffectiveIntegrationCompletionLinearization(projectRoot, goalCompleteRestoreLinearization, func() error {
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
			changedBy := goalCompleteRestoredBy
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

// Pause transitions system mode to PAUSED, then waits at the work-admission
// barrier, so on success no provider start admitted before the pause is still
// pending. Pausing an already PAUSED system writes nothing and only waits, which
// makes a timed-out barrier safe to retry. No terminal I/O.
func Pause(projectRoot, reason, changedBy string) (*ModeChangeResult, error) {
	result, err := changeModeAllowingSame(projectRoot, reason, changedBy, models.SystemModePaused, true)
	if err != nil {
		return nil, err
	}
	if err := AwaitWorkAdmissionBarrier(projectRoot, pauseOperation); err != nil {
		return nil, err
	}
	return result, nil
}

// changeMode is the shared implementation for Start, Stop, and Pause.
// It validates the transition via the systemModeTransitions table and applies it.
func changeMode(projectRoot, reason, changedBy string, target models.SystemMode) (*ModeChangeResult, error) {
	return changeModeAllowingSame(projectRoot, reason, changedBy, target, false)
}

// changeModeAllowingSame is changeMode; with allowSame, a system already in
// target is left unwritten and reported as an unchanged transition.
func changeModeAllowingSame(projectRoot, reason, changedBy string, target models.SystemMode, allowSame bool) (*ModeChangeResult, error) {
	statePath := paths.New(projectRoot).StatePath()
	blackboard := db.For(statePath)

	timestamp := time.Now()
	var previousMode models.SystemMode
	errUnchanged := errors.New("mode unchanged")

	err := blackboard.Modify(func(s *models.State) error {
		previousMode = s.Config.Mode
		if previousMode == "" {
			previousMode = models.SystemModeRunning
		}
		if allowSame && previousMode == target {
			return errUnchanged // Aborts the transaction: nothing to write.
		}

		if err := previousMode.ValidateTransition(target); err != nil {
			return &PreconditionError{Reason: err.Error()}
		}
		if target == models.SystemModeRunning && hasActiveHaltResponse(s) {
			return &PreconditionError{Reason: fmt.Sprintf("cannot start while an active HALT response is unresolved; use %q to acknowledge it", brand.Command(resumeOperation))}
		}

		s.Config.Mode = target
		s.Config.ModeChangedAt = &timestamp
		s.Config.ModeChangedBy = &changedBy

		return nil
	})

	if err != nil && !errors.Is(err, errUnchanged) {
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
	return s.CircuitBreaker.Status == circuitBreakerStatusTriggered && s.CircuitBreaker.CurrentTrigger != nil
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
		s.CircuitBreaker.Status = circuitBreakerStatusOK
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
			s.CircuitBreaker.Status = circuitBreakerStatusOK
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
			return string(models.SprintStatusCheckpoint), nil, nil
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
		return string(models.SprintStatusCheckpoint), nil, nil

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
	blackboard := db.For(paths.New(projectRoot).StatePath())
	requiresCompletion, err := resumePreflight(blackboard, projectRoot, origin)
	if err != nil {
		return nil, err
	}

	var decision resumeDecision
	resumeMutation := func(completionAuthorization *effectiveIntegrationCompletionAuthorization) error {
		return blackboard.Modify(func(s *models.State) error {
			return decision.apply(s, projectRoot, changedBy, origin, completionAuthorization)
		})
	}
	if requiresCompletion {
		err = withEffectiveIntegrationCompletionAuthorization(projectRoot, resumeOperation, false, resumeMutation)
	} else {
		err = resumeMutation(nil)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to resume system: %w", err)
	}

	var transitions resumeTransitionReport
	if decision.runTransitions {
		transitions = reportResumeTransitions(projectRoot, origin, decision.checkpoint)
	}

	return &ResumeResult{
		ResumedFrom:          decision.resumedFrom,
		ChangedBy:            changedBy,
		SystemRemainsStopped: decision.systemRemainsStopped,
		SprintAdvanced:       decision.advance,
		TransitionsExecuted:  transitions.executed,
		TransitionError:      transitions.failure,
	}, nil
}

// resumePreflight reads the state before the resume transaction and reports
// whether the sprint branch it will take needs effective integration completion.
// An operator acknowledging a HALT on a STOPPED system never does.
func resumePreflight(blackboard *db.Blackboard, projectRoot string, origin resumeOrigin) (bool, error) {
	preflightState, err := blackboard.Read()
	if err != nil {
		return false, fmt.Errorf("failed to read state before resume: %w", err)
	}
	preflightStoppedWithActiveHalt := origin == resumeOriginOperator &&
		preflightState.Config.Mode == models.SystemModeStopped && hasActiveHaltResponse(preflightState)
	if preflightStoppedWithActiveHalt {
		return false, nil
	}
	requiresCompletion, err := resumeRequiresEffectiveIntegrationCompletion(preflightState, projectRoot)
	if err != nil {
		return false, fmt.Errorf("failed to evaluate resume completion branch: %w", err)
	}
	return requiresCompletion, nil
}

// resumeDecision is what the locked resume transaction decided, carried out of
// the callback for the result and the post-commit transition pass.
type resumeDecision struct {
	resumedFrom          string
	systemRemainsStopped bool
	advance              *AdvanceSprintResult
	checkpoint           models.Sprint // sprint as found, to protect a newer checkpoint from cleanup
	runTransitions       bool
}

// apply is the mode/sprint decision of resume. It runs inside the single
// Modify callback: it starts no Git or process effect and takes no other lock.
func (d *resumeDecision) apply(s *models.State, projectRoot, changedBy string, origin resumeOrigin, completionAuthorization *effectiveIntegrationCompletionAuthorization) error {
	timestamp := time.Now()
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
				return integrationCompletionPreconditionError(&IntegrationProgressReason{Code: integrationStateChangedCode})
			}
			if err := completionAuthorization.validateState(s, false); err != nil {
				return err
			}
		}
	}

	wasTransitionCheckpoint := s.Sprint.Status == models.SprintStatusCheckpoint &&
		models.IsTransitionCheckpointTrigger(s.Sprint.CheckpointTrigger)
	d.checkpoint = s.Sprint

	if stoppedWithActiveHalt {
		d.resumedFrom = "active HALT response"
		d.systemRemainsStopped = true
		if s.CircuitBreaker.CurrentResponse == nil {
			// Legacy HALTs have no typed history boundary to resolve.
			s.CircuitBreaker.Status = circuitBreakerStatusOK
			s.CircuitBreaker.CurrentTrigger = nil
		}
	} else {
		d.resumedFrom = resumeSystemMode(s, timestamp, changedBy)

		sprintDesc, advResult, err := resumeSprint(s, paths.New(projectRoot), projectRoot, timestamp)
		if err != nil {
			return err
		}
		d.advance = advResult
		d.runTransitions = advResult != nil ||
			(wasTransitionCheckpoint && s.Sprint.Status == models.SprintStatusInProgress)
		if sprintDesc != "" {
			if d.resumedFrom != "" {
				d.resumedFrom += " and " + sprintDesc
			} else {
				d.resumedFrom = sprintDesc
			}
		}
	}
	return acknowledgeCircuitBreakerResponse(s, timestamp, changedBy)
}
