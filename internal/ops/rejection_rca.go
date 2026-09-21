package ops

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	lizaerrors "github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/identity"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/payloadschema"
	"github.com/liza-mas/liza/internal/roles"
	"github.com/liza-mas/liza/internal/statevalidate"
)

// RejectionRCAOptions carries the lifecycle request identity of one
// record-rejection-rca or resume-rejection-rca invocation.
type RejectionRCAOptions struct {
	Request LifecycleRequestOptions
}

// RejectionRCAResult is the outcome of either rejection-RCA operation together
// with the task's live record after the transaction.
type RejectionRCAResult struct {
	models.LifecycleOutcome
	RejectionRCA *models.RejectionRCARecord `json:"rejection_rca,omitempty"`
	Warnings     []string                   `json:"warnings,omitempty"`
}

func (r *RejectionRCAResult) GetWarnings() []string {
	if r == nil {
		return nil
	}
	return r.Warnings
}

// errRejectionRCANoChange aborts the write of a request whose effective result
// already equals durable state; the caller turns it into NO_CHANGE.
var errRejectionRCANoChange = errors.New("rejection rca request already recorded")

// rejectionRCAMutation is one operation's domain step, run inside the locked
// callback after the request identity and replay checks. It edits the task in
// place and returns errRejectionRCANoChange when nothing must be written.
type rejectionRCAMutation func(task *models.Task, actor string, now time.Time) error

// RecordRejectionRCA stores a classified RCA on a task whose rejection-RCA gate
// is open. The canonical object is models.RejectionRCARequest decoded from the
// request file; every other record field stays as the gate seeded it.
func RecordRejectionRCA(projectRoot, taskID string, payload any, agentID string, opts RejectionRCAOptions) (*RejectionRCAResult, error) {
	return recordRejectionRCA(projectRoot, taskID, payload, agentID, nil, opts)
}

// RecordRejectionRCAWithAuthority fences the record with the orchestrator's
// registration generation.
func RecordRejectionRCAWithAuthority(projectRoot, taskID string, payload any, authority models.AgentAuthority, opts RejectionRCAOptions) (*RejectionRCAResult, error) {
	return recordRejectionRCA(projectRoot, taskID, payload, authority.ID, &authority, opts)
}

// ResumeRejectionRCA closes an open gate with an authorized disposition. The
// canonical object is models.RejectionRCADispositionRequest; the restore mode,
// actor, lifecycle version and decision time are derived here. The task stays
// BLOCKED — only unblock-task restores it, in the form the disposition allows.
func ResumeRejectionRCA(projectRoot, taskID string, payload any, agentID string, opts RejectionRCAOptions) (*RejectionRCAResult, error) {
	return resumeRejectionRCA(projectRoot, taskID, payload, agentID, nil, opts)
}

// ResumeRejectionRCAWithAuthority fences the disposition with the
// orchestrator's registration generation.
func ResumeRejectionRCAWithAuthority(projectRoot, taskID string, payload any, authority models.AgentAuthority, opts RejectionRCAOptions) (*RejectionRCAResult, error) {
	return resumeRejectionRCA(projectRoot, taskID, payload, authority.ID, &authority, opts)
}

func recordRejectionRCA(projectRoot, taskID string, payload any, agentID string, authority *models.AgentAuthority, opts RejectionRCAOptions) (*RejectionRCAResult, error) {
	const operation = payloadschema.RecordRejectionRCAOperation
	var request models.RejectionRCARequest
	decode := func() (any, error) {
		if err := decodeRejectionRCAPayload(operation, payload, &request); err != nil {
			return nil, err
		}
		request = models.NormalizeRejectionRCARequest(request)
		return request, nil
	}
	mutate := func(task *models.Task, actor string, now time.Time) error {
		if err := requireOpenRejectionRCAGate(operation, task, false); err != nil {
			return err
		}
		record := task.RejectionRCA
		fingerprint := models.RejectionRCAFingerprint(request)
		if record.Fingerprint == fingerprint {
			return errRejectionRCANoChange
		}
		// The bound needs the task, so it is a precondition of the operation
		// rather than a diagnostic the state-free preflight could report.
		if highest := highestRejectionIndex(request.Contributions); highest > task.DurableRejectionCount() {
			return &PreconditionError{Reason: fmt.Sprintf(
				"highest rejection_index %d exceeds the task's durable rejection count %d", highest, task.DurableRejectionCount())}
		}

		recordedAt := now
		record.Summary = request.Summary
		record.Contributions = request.Contributions
		record.Fingerprint = fingerprint
		record.RecordedAt = &recordedAt
		record.RecordedBy = actor
		task.History = append(task.History, models.TaskHistoryEntry{
			Time:  now,
			Event: models.TaskEventRejectionRCARecorded,
			Agent: &actor,
			Extra: map[string]any{
				"fingerprint":        fingerprint,
				"threshold":          record.Threshold,
				"rejection_count":    record.RejectionCount,
				"gated_at":           record.GatedAt.Format(time.RFC3339),
				"causes":             rejectionRCACauses(request.Contributions),
				"contribution_count": len(request.Contributions),
				"recorded_by":        actor,
			},
		})
		return nil
	}
	return runRejectionRCAOperation(projectRoot, operation, taskID, agentID, authority, opts, decode, mutate)
}

func resumeRejectionRCA(projectRoot, taskID string, payload any, agentID string, authority *models.AgentAuthority, opts RejectionRCAOptions) (*RejectionRCAResult, error) {
	const operation = payloadschema.ResumeRejectionRCAOperation
	var request models.RejectionRCADispositionRequest
	decode := func() (any, error) {
		if err := decodeRejectionRCAPayload(operation, payload, &request); err != nil {
			return nil, err
		}
		request.Rationale = strings.TrimSpace(request.Rationale)
		return request, nil
	}
	mutate := func(task *models.Task, actor string, now time.Time) error {
		if err := requireOpenRejectionRCAGate(operation, task, true); err != nil {
			return err
		}
		record := task.RejectionRCA
		restoreMode := models.RejectionRCARestoreMode(request.RecoveryPath)
		// An assign restore skips the claim path, so the doer's next cycle
		// consumes no product iteration; the counter reset records the same
		// decision for the review budget.
		iterationExempt := restoreMode == models.RestoreModeAssign
		var lifecycleVersion uint64
		if task.Lifecycle != nil {
			lifecycleVersion = task.Lifecycle.Revision
		}
		record.Disposition = &models.RejectionRCADisposition{
			RecoveryPath:     request.RecoveryPath,
			RestoreMode:      restoreMode,
			Actor:            actor,
			LifecycleVersion: lifecycleVersion,
			DecidedAt:        now,
			Rationale:        request.Rationale,
			IterationExempt:  iterationExempt,
		}
		if iterationExempt {
			task.ReviewCyclesCurrent = 0
		}
		task.History = append(task.History, models.TaskHistoryEntry{
			Time:  now,
			Event: models.TaskEventRejectionRCAResumed,
			Agent: &actor,
			Extra: map[string]any{
				"fingerprint":       record.Fingerprint,
				"recovery_path":     request.RecoveryPath,
				"restore_mode":      restoreMode,
				"actor":             actor,
				"lifecycle_version": lifecycleVersion,
				"decided_at":        now.Format(time.RFC3339),
				"iteration_exempt":  iterationExempt,
				"gated_at":          record.GatedAt.Format(time.RFC3339),
			},
		})
		return nil
	}
	return runRejectionRCAOperation(projectRoot, operation, taskID, agentID, authority, opts, decode, mutate)
}

// runRejectionRCAOperation is the shared lifecycle shape of both operations:
// registry validation before the lock, then one locked callback holding the
// request identity, replay check, domain mutation, state validation and
// completion receipt.
func runRejectionRCAOperation(projectRoot, operation, taskID, agentID string, authority *models.AgentAuthority, opts RejectionRCAOptions,
	decode func() (any, error), mutate rejectionRCAMutation) (returned *RejectionRCAResult, retErr error) {
	var invocation *LifecycleInvocation
	var observed *models.Task
	effects := "none"
	defer func() {
		retErr = WrapLifecycleError(operation, observed, retErr, models.LifecycleInvalidInput, "correct_input", "none")
		if invocation == nil {
			return
		}
		var outcome models.LifecycleOutcome
		var warnings *[]string
		if returned != nil {
			outcome = returned.LifecycleOutcome
			warnings = &returned.Warnings
		}
		invocation.FinishResult(operation, outcome, &retErr, warnings)
	}()
	if err := ValidateLifecycleRequestOptions(opts.Request); err != nil {
		return nil, err
	}
	if taskID == "" {
		return nil, &PreconditionError{Reason: "task ID is required"}
	}
	if agentID == "" {
		return nil, &PreconditionError{Reason: "agent ID is required"}
	}
	if err := identity.ValidateRole(agentID, roles.Orchestrator); err != nil {
		return nil, WrapLifecycleError(operation, nil, &PreconditionError{Reason: fmt.Sprintf("only orchestrator agents can run %s: %v", operation, err)}, models.LifecycleForbidden, "stop", "none")
	}
	// One structural verdict for the preflight and this boundary, reached
	// before any state path is opened.
	normalized, err := decode()
	if err != nil {
		return nil, err
	}

	// Capturing the telemetry sprint reads state under lock, so structural
	// refusals must return before initializing the invocation too.
	invocation = NewLifecycleInvocation(projectRoot)
	bb := db.For(paths.New(projectRoot).StatePath())
	now := time.Now().UTC()
	result := RejectionRCAResult{}

	err = modifyLifecycleState(bb, authority, func(state *models.State) (callbackErr error) {
		defer func() {
			if isLifecycleReplay(callbackErr) || errors.Is(callbackErr, errRejectionRCANoChange) {
				return
			}
			callbackErr = WrapLifecycleError(operation, observed, callbackErr, models.LifecycleInvalidInput, "correct_input", "none")
		}()
		task := state.FindTask(taskID)
		if task == nil {
			return &lizaerrors.NotFoundError{Entity: "task", ID: taskID}
		}
		copy := *task
		observed = &copy
		request, err := NewLifecycleRequest(operation, task, agentID, authority, opts.Request, normalized)
		if err != nil {
			return err
		}
		receipt, err := CheckLifecycleRequest(task, request, state.Agents)
		if err != nil {
			return err
		}
		if receipt != nil {
			result.LifecycleOutcome = LifecycleReplayOutcome(task, receipt, agentID)
			result.RejectionRCA = cloneRejectionRCARecord(task.RejectionRCA)
			return errLifecycleReplay
		}

		if err := mutate(task, agentID, now); err != nil {
			if errors.Is(err, errRejectionRCANoChange) {
				result.LifecycleOutcome = NewLifecycleNoChangeOutcome(operation, task)
				result.RejectionRCA = cloneRejectionRCARecord(task.RejectionRCA)
			}
			return err
		}
		if err := statevalidate.ValidateState(state, projectRoot, false, io.Discard); err != nil {
			return err
		}
		result.LifecycleOutcome, err = CompleteLifecycleRequest(task, request, models.LifecycleProjection{}, state.Agents)
		if err == nil {
			result.RejectionRCA = cloneRejectionRCARecord(task.RejectionRCA)
			effects = "unknown"
		}
		return err
	})
	if isLifecycleReplay(err) || errors.Is(err, errRejectionRCANoChange) {
		return &result, nil
	}
	if err != nil {
		return nil, WrapLifecycleError(operation, observed, fmt.Errorf("failed to run %s: %w", operation, err), models.LifecycleStateChanged, "requery", effects)
	}
	return &result, nil
}

// decodeRejectionRCAPayload runs the canonical object through the operation's
// registered schema and, once accepted, decodes it into the request type. A
// structural rejection is the INVALID_INPUT result the caller must correct
// against; it reads the payload and nothing else.
func decodeRejectionRCAPayload(operation string, payload, target any) error {
	version, diagnostics, err := payloadschema.Validate(operation, payload)
	if err != nil {
		return err
	}
	if len(diagnostics) > 0 {
		return NewLifecycleInvalidInputError(operation, nil, diagnostics,
			fmt.Errorf("payload rejected by the %s schema, version %d: %s %s",
				operation, version, diagnostics[0].Field, diagnostics[0].Constraint))
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, target)
}

// requireOpenRejectionRCAGate refuses a task that is not gated, or — when the
// operation needs one — that has no recorded RCA yet. Refusals are
// preconditions on task state, so the caller must requery rather than correct
// its payload.
func requireOpenRejectionRCAGate(operation string, task *models.Task, needRecorded bool) error {
	var reason string
	switch {
	case task.Status != models.TaskStatusBlocked:
		reason = fmt.Sprintf("task must be BLOCKED with an open rejection_rca gate, current status: %s", task.Status)
	case !task.RejectionRCAGateOpen():
		reason = "task has no open rejection_rca gate"
	case needRecorded && task.RejectionRCA.Fingerprint == "":
		reason = "task has no recorded RCA; run record-rejection-rca first"
	default:
		return nil
	}
	return WrapLifecycleError(operation, task, &PreconditionError{Reason: reason}, models.LifecycleStateChanged, "requery", "none")
}

func highestRejectionIndex(contributions []models.RejectionRCAContribution) int {
	highest := 0
	for _, contribution := range contributions {
		highest = max(highest, contribution.RejectionIndex)
	}
	return highest
}

// rejectionRCACauses is the sorted distinct union of every contribution's
// categories, unrecognized values included verbatim.
func rejectionRCACauses(contributions []models.RejectionRCAContribution) []string {
	var causes []string
	for _, contribution := range contributions {
		causes = append(causes, contribution.Categories...)
	}
	slices.Sort(causes)
	return slices.Compact(causes)
}

func cloneRejectionRCARecord(record *models.RejectionRCARecord) *models.RejectionRCARecord {
	if record == nil {
		return nil
	}
	clone := *record
	clone.Contributions = slices.Clone(record.Contributions)
	if record.Disposition != nil {
		disposition := *record.Disposition
		clone.Disposition = &disposition
	}
	return &clone
}
