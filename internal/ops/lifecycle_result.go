package ops

import (
	"errors"
	"fmt"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// readLifecycleTask observes committed state after an operation fails. A failed
// transaction's in-memory task may contain mutations that were never written.
// Unreadable state or lost authority cannot expose an authoritative boundary.
func readLifecycleTask(projectRoot, taskID string, authority *models.AgentAuthority) *models.Task {
	state, err := db.For(paths.New(projectRoot).StatePath()).Read()
	if err != nil || (authority != nil && RequireAgentAuthority(state, *authority) != nil) {
		return nil
	}
	return state.FindTask(taskID)
}

// LifecycleError preserves an operation's error cause while exposing a single
// server-selected recovery action through both text and structured output.
type LifecycleError struct {
	Outcome models.LifecycleOutcome
	Err     error
}

func (e *LifecycleError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("%s: %s (safe_action=%s)", e.Outcome.Operation, e.Outcome.Outcome, e.Outcome.SafeAction)
	}
	cause := e.Err.Error()
	var operational *OperationalError
	if errors.As(e.Err, &operational) {
		// Operational causes are retained for errors.As, but only their
		// explicitly safe message belongs in agent-facing output.
		cause = operational.Message
		var authority *AgentAuthorityError
		var invalid *PreconditionError
		// These typed messages are already safe presentation contracts. Keep
		// their actionable details without printing an arbitrary raw chain.
		switch {
		case errors.As(e.Err, &authority):
			cause += ": " + authority.Error()
		case errors.As(e.Err, &invalid):
			cause += ": " + invalid.Error()
		}
	}
	return fmt.Sprintf("%s: %s (safe_action=%s): %s", e.Outcome.Operation, e.Outcome.Outcome, e.Outcome.SafeAction, cause)
}

func (e *LifecycleError) Unwrap() error { return e.Err }

func (e *LifecycleError) LifecycleResult() any { return e.Outcome }

func (e *LifecycleError) SafeDetails() map[string]any {
	o := e.Outcome
	details := make(map[string]any)
	var cause interface{ SafeDetails() map[string]any }
	if errors.As(e.Err, &cause) {
		for key, value := range cause.SafeDetails() {
			details[key] = value
		}
	}
	for key, value := range map[string]any{
		"operation": o.Operation, "task_id": o.TaskID, "outcome": o.Outcome,
		"safe_action": o.SafeAction, "task_status": string(o.TaskStatus), "effects": o.Effects,
	} {
		details[key] = value
	}
	for key, value := range map[string]string{
		"current_assignee": o.CurrentAssignee, "current_reviewer": o.CurrentReviewer,
		"transition_id": o.TransitionID, "completed_transition_id": o.CompletedTransitionID,
		"request_id": o.RequestID,
	} {
		delete(details, key)
		if value != "" {
			details[key] = value
		}
	}
	// The outcome owns these keys: a cause may not assert change or supply
	// its own diagnostics, and an absent value is omitted rather than false.
	delete(details, "changed")
	delete(details, "diagnostics")
	if o.Changed != nil {
		details["changed"] = *o.Changed
	}
	if len(o.Diagnostics) > 0 {
		details["diagnostics"] = o.Diagnostics
	}
	return details
}

// WrapLifecycleError retains explicit command policy except for typed authority
// rejection and lock contention known to precede all effects. It never derives
// policy by matching error text, and keeps the original cause discoverable.
func WrapLifecycleError(operation string, task *models.Task, err error, outcome, action, effects string) error {
	if err == nil {
		return nil
	}
	var existing *LifecycleError
	if errors.As(err, &existing) {
		if err == existing {
			return err
		}
		// Preserve outer safe diagnostics while retaining the inner policy.
		return &LifecycleError{Outcome: existing.Outcome, Err: err}
	}
	if IsAgentAuthorityError(err) {
		task = nil
		outcome, action = models.LifecycleStaleCaller, "stop"
	} else if effects == "none" && filelock.IsLockErrorType(err, filelock.LockErrorTimeout) {
		outcome, action = models.LifecycleRetryable, "retry"
	}
	return &LifecycleError{Outcome: NewLifecycleOutcome(operation, task, outcome, action, effects), Err: err}
}

// NewLifecycleOutcome requires an already-authorized task observation. Pass
// nil when state cannot be read or authorization forbids exposing ownership.
func NewLifecycleOutcome(operation string, task *models.Task, outcome, safeAction, effects string) models.LifecycleOutcome {
	o := models.LifecycleOutcome{Operation: operation, Outcome: outcome, SafeAction: safeAction, Effects: effects, TaskStatus: "UNKNOWN"}
	o.Changed = models.LifecycleChanged(outcome)
	if task == nil {
		return o
	}
	o.TaskID = task.ID
	o.TaskStatus = task.Status
	o.TransitionID = models.TaskTransitionID(task)
	if task.AssignedTo != nil {
		o.CurrentAssignee = *task.AssignedTo
	}
	if task.ReviewingBy != nil {
		o.CurrentReviewer = *task.ReviewingBy
	}
	return o
}

// LifecycleReplayOutcome reports the original completion separately from the
// live task boundary. Historical completion never transfers current ownership.
func LifecycleReplayOutcome(task *models.Task, receipt *models.LifecycleReceipt, actor string) models.LifecycleOutcome {
	action := "stop"
	if task != nil && models.TaskTransitionID(task) == receipt.TransitionID &&
		((task.AssignedTo != nil && *task.AssignedTo == actor) ||
			(task.ReviewingBy != nil && *task.ReviewingBy == actor)) {
		action = "continue"
	}
	o := NewLifecycleOutcome(receipt.Operation, task, models.LifecycleAlreadyCompleted, action, "committed")
	o.CompletedTransitionID = receipt.TransitionID
	o.RequestID = receipt.RequestID
	return o
}

// NewLifecycleNoChangeOutcome reports a valid, authorized request whose
// effective result already equals durable state: nothing was appended.
func NewLifecycleNoChangeOutcome(operation string, task *models.Task) models.LifecycleOutcome {
	return NewLifecycleOutcome(operation, task, models.LifecycleNoChange, "stop", "none")
}

// NewLifecycleInvalidInputError rejects a payload before any effect, naming
// the offending fields without echoing their values.
func NewLifecycleInvalidInputError(operation string, task *models.Task, diagnostics []models.FieldDiagnostic, err error) *LifecycleError {
	return newDiagnosticLifecycleError(operation, task, models.LifecycleInvalidInput, "correct_input", diagnostics, err)
}

// NewLifecycleConflictError reports an identity or idempotency key reused
// with a materially different payload; diagnostics name the conflict.
func NewLifecycleConflictError(operation string, task *models.Task, diagnostics []models.FieldDiagnostic, err error) *LifecycleError {
	return newDiagnosticLifecycleError(operation, task, models.LifecycleConflict, "stop", diagnostics, err)
}

func newDiagnosticLifecycleError(operation string, task *models.Task, outcome, safeAction string, diagnostics []models.FieldDiagnostic, err error) *LifecycleError {
	o := NewLifecycleOutcome(operation, task, outcome, safeAction, "none")
	o.Diagnostics = models.NormalizeFieldDiagnostics(diagnostics)
	return &LifecycleError{Outcome: o, Err: err}
}
