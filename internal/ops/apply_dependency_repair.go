package ops

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/statevalidate"
)

// AppliedDependencyUpdate reports the committed canonical dependency state for
// one task in a declarative repair.
type AppliedDependencyUpdate struct {
	TaskID                string   `json:"task_id"`
	CanonicalDependencies []string `json:"canonical_dependencies"`
}

// ApplyDependencyRepairResult contains the committed declarative repair batch.
type ApplyDependencyRepairResult struct {
	models.LifecycleOutcome
	SourceTaskID string                    `json:"source_task_id"`
	Updates      []AppliedDependencyUpdate `json:"updates"`
	Warnings     []string                  `json:"warnings,omitempty"`
}

func (r *ApplyDependencyRepairResult) GetWarnings() []string {
	if r == nil {
		return nil
	}
	return r.Warnings
}

type preparedDependencyUpdate struct {
	task      *models.Task
	requested models.DependencyUpdate
	canonical []string
}

// ApplyDependencyRepair consumes one blocked task's declarative repair request
// and commits every requested dependency list in one validated transaction.
func ApplyDependencyRepair(projectRoot, sourceTaskID, reason, agentID string) (*ApplyDependencyRepairResult, error) {
	return applyDependencyRepairWithOptionalAuthority(projectRoot, sourceTaskID, reason, agentID, nil)
}

// ApplyDependencyRepairWithAuthority fences the complete repair batch with the
// orchestrator's registration generation.
func ApplyDependencyRepairWithAuthority(projectRoot, sourceTaskID, reason string, authority models.AgentAuthority) (*ApplyDependencyRepairResult, error) {
	return applyDependencyRepairWithOptionalAuthority(projectRoot, sourceTaskID, reason, authority.ID, &authority)
}

// ApplyDependencyRepairWithOptions identifies consumption of the inspected repair.
func ApplyDependencyRepairWithOptions(projectRoot, sourceTaskID, reason, agentID string, opts LifecycleRequestOptions) (*ApplyDependencyRepairResult, error) {
	return applyDependencyRepairWithOptionalAuthority(projectRoot, sourceTaskID, reason, agentID, nil, opts)
}

// ApplyDependencyRepairWithAuthorityAndOptions fences an identified repair batch.
func ApplyDependencyRepairWithAuthorityAndOptions(projectRoot, sourceTaskID, reason string, authority models.AgentAuthority, opts LifecycleRequestOptions) (*ApplyDependencyRepairResult, error) {
	return applyDependencyRepairWithOptionalAuthority(projectRoot, sourceTaskID, reason, authority.ID, &authority, opts)
}

func applyDependencyRepairWithOptionalAuthority(projectRoot, sourceTaskID, reason, agentID string, authority *models.AgentAuthority, options ...LifecycleRequestOptions) (returned *ApplyDependencyRepairResult, retErr error) {
	invocation := NewLifecycleInvocation(projectRoot)
	const operation = models.RepairOperationApplyDependencyRepair
	var opts LifecycleRequestOptions
	if len(options) > 0 {
		opts = options[0]
	}
	var observed *models.Task
	effects := "none"
	defer func() {
		retErr = WrapLifecycleError(operation, observed, retErr, models.LifecycleInvalidInput, "correct_input", "none")
		var outcome models.LifecycleOutcome
		var warnings *[]string
		if returned != nil {
			outcome = returned.LifecycleOutcome
			warnings = &returned.Warnings
		}
		invocation.FinishResult(operation, outcome, &retErr, warnings)
	}()
	if err := ValidateLifecycleRequestOptions(opts); err != nil {
		return nil, err
	}
	if sourceTaskID == "" {
		return nil, &PreconditionError{Reason: "blocked task ID is required"}
	}
	if reason == "" {
		return nil, &PreconditionError{Reason: "reason is required"}
	}
	if agentID == "" {
		return nil, &PreconditionError{Reason: "orchestrator agent ID is required"}
	}

	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())
	resolver, _, err := loadResolver(projectRoot)
	if err != nil {
		return nil, WrapLifecycleError(operation, nil, fmt.Errorf("failed to load pipeline config: %w", err), models.LifecycleStateChanged, "requery", "none")
	}

	var result ApplyDependencyRepairResult
	now := time.Now().UTC()
	err = lifecycleMutation(bb, authority)(func(state *models.State) (callbackErr error) {
		defer func() {
			callbackErr = WrapLifecycleError(operation, observed, callbackErr, models.LifecycleInvalidInput, "correct_input", "none")
		}()
		source := state.FindTask(sourceTaskID)
		if source == nil {
			return &errors.NotFoundError{Entity: "task", ID: sourceTaskID}
		}
		copy := *source
		observed = &copy
		lifecycleRequest, err := NewLifecycleRequest(operation, source, agentID, authority, opts, reason)
		if err != nil {
			return err
		}
		receipt, err := CheckLifecycleRequest(source, lifecycleRequest, state.Agents)
		if err != nil {
			return err
		}
		if receipt != nil {
			result = ApplyDependencyRepairResult{SourceTaskID: sourceTaskID, LifecycleOutcome: LifecycleReplayOutcome(source, receipt, agentID)}
			return errLifecycleReplay
		}
		if source.Status != models.TaskStatusBlocked {
			return WrapLifecycleError(operation, source, &PreconditionError{Reason: fmt.Sprintf("cannot apply dependency repair from task %s in status %s (must be BLOCKED)", sourceTaskID, source.Status)}, models.LifecycleAlreadyTransitioned, "stop", "none")
		}
		if source.RepairRequest == nil {
			return WrapLifecycleError(operation, source, &PreconditionError{Reason: fmt.Sprintf("blocked task %s has no repair request", sourceTaskID)}, models.LifecycleStateChanged, "requery", "none")
		}

		request, err := normalizeRepairRequest(source.RepairRequest, sourceTaskID)
		if err != nil {
			return err
		}
		if request.Operation != models.RepairOperationApplyDependencyRepair {
			return &PreconditionError{Reason: fmt.Sprintf("blocked task %s repair request operation is %q, want %q", sourceTaskID, request.Operation, models.RepairOperationApplyDependencyRepair)}
		}
		consumed, err := json.Marshal(request)
		if err != nil {
			return err
		}
		consumedDigest := lifecycleDigest(consumed)

		updates, err := applyDependencyUpdatesInState(state, resolver, source, operation, request.DependencyUpdates, agentID, reason, now, map[string]any{
			"manual":                 true,
			"operation":              models.RepairOperationApplyDependencyRepair,
			"repair_source_task":     sourceTaskID,
			"repair_evidence":        append([]string{}, request.Evidence...),
			"repair_validation":      append([]string{}, request.Validation...),
			"repair_request_cleared": true,
			"repair_request_digest":  consumedDigest,
		})
		if err != nil {
			return err
		}
		sourceUpdated := slices.ContainsFunc(updates, func(update AppliedDependencyUpdate) bool {
			return update.TaskID == sourceTaskID
		})
		affectedTaskIDs := make([]string, len(updates))
		for i, update := range updates {
			affectedTaskIDs[i] = update.TaskID
		}
		if !sourceUpdated {
			note := "applied dependency repair without changing the source task dependencies"
			source.History = append(source.History, models.TaskHistoryEntry{
				Time:   now,
				Event:  models.TaskEventDependencyRepairApplied,
				Agent:  &agentID,
				Reason: &reason,
				Note:   &note,
				Extra: map[string]any{
					"manual":                 true,
					"operation":              models.RepairOperationApplyDependencyRepair,
					"affected_task_ids":      append([]string{}, affectedTaskIDs...),
					"repair_evidence":        append([]string{}, request.Evidence...),
					"repair_validation":      append([]string{}, request.Validation...),
					"repair_request_cleared": true,
					"repair_request_digest":  consumedDigest,
				},
			})
		}
		source.RepairRequest = nil

		if err := statevalidate.ValidateState(state, projectRoot, false, io.Discard); err != nil {
			return err
		}

		result = ApplyDependencyRepairResult{
			SourceTaskID: sourceTaskID,
			Updates:      updates,
		}
		result.LifecycleOutcome, err = CompleteLifecycleRequest(source, lifecycleRequest, models.LifecycleProjection{}, state.Agents)
		if err == nil {
			effects = "unknown"
		}
		return err
	})
	if isLifecycleReplay(err) {
		return &result, nil
	}
	if err != nil {
		return nil, WrapLifecycleError(operation, observed, fmt.Errorf("failed to apply dependency repair: %w", err), models.LifecycleStateChanged, "requery", effects)
	}

	updated := make([]string, 0, len(result.Updates))
	for _, update := range result.Updates {
		updated = append(updated, fmt.Sprintf("%s=[%s]", update.TaskID, strings.Join(update.CanonicalDependencies, ",")))
	}
	logEntry := log.Entry{
		Timestamp: now,
		Agent:     agentID,
		Action:    models.RepairOperationApplyDependencyRepair,
		Task:      &sourceTaskID,
		Detail:    fmt.Sprintf("updated=%s: %s", strings.Join(updated, " "), reason),
	}
	if err := log.New(lp.LogPath()).Append(logEntry); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("activity log write failed: %v", err))
	}

	return &result, nil
}

// applyDependencyUpdatesInState commits one declarative dependency-repair batch
// into an already-locked candidate state: existence and terminal rejection,
// expected-list equality, self-dependency rejection, canonicalization,
// assignment and the audit entry per rewritten task. A stale expected list is
// reported against source as STATE_CHANGED so the caller can requery. extra
// supplies the caller's common audit keys; the per-task keys are added here.
// Full-state validation and the lifecycle receipt stay with the caller.
func applyDependencyUpdatesInState(state *models.State, resolver *pipeline.Resolver, source *models.Task, operation string, updates []models.DependencyUpdate, agentID, reason string, now time.Time, extra map[string]any) ([]AppliedDependencyUpdate, error) {
	prepared := make([]preparedDependencyUpdate, 0, len(updates))
	for _, update := range updates {
		task := state.FindTask(update.TaskID)
		if task == nil {
			return nil, &errors.NotFoundError{Entity: "dependency repair task", ID: update.TaskID}
		}
		if task.Status.IsTerminal() {
			return nil, &PreconditionError{Reason: fmt.Sprintf("cannot apply dependency repair to terminal task %s (%s)", task.ID, task.Status)}
		}
		if !slices.Equal(task.DependsOn, update.ExpectedDependsOn) {
			return nil, WrapLifecycleError(operation, source, &PreconditionError{Reason: fmt.Sprintf("task %s dependencies changed since the repair request was created: got %v, expected %v", task.ID, task.DependsOn, update.ExpectedDependsOn)}, models.LifecycleStateChanged, "requery", "none")
		}
		for _, dependencyID := range update.DesiredDependsOn {
			if dependencyID == task.ID {
				return nil, &PreconditionError{Reason: fmt.Sprintf("task %s cannot depend on itself", task.ID)}
			}
			if state.FindTask(dependencyID) == nil {
				return nil, &PreconditionError{Reason: fmt.Sprintf("desired dependency %q for task %s does not exist", dependencyID, task.ID)}
			}
		}

		canonical, _, err := canonicalizeConcreteDependencyList(state, resolver, task.ID, task.RolePair, update.DesiredDependsOn)
		if err != nil {
			return nil, err
		}
		if err := rejectUnmetDependencyOnExecutingConsumer(state, resolver, operation, task, canonical); err != nil {
			return nil, err
		}
		prepared = append(prepared, preparedDependencyUpdate{
			task:      task,
			requested: update,
			canonical: append([]string{}, canonical...),
		})
	}

	affectedTaskIDs := make([]string, len(prepared))
	for i, update := range prepared {
		affectedTaskIDs[i] = update.task.ID
	}

	applied := make([]AppliedDependencyUpdate, 0, len(prepared))
	for _, update := range prepared {
		update.task.DependsOn = append([]string{}, update.canonical...)
		note := fmt.Sprintf("applied dependency repair requested by %s", source.ID)
		entryExtra := map[string]any{
			"affected_task_ids":      append([]string{}, affectedTaskIDs...),
			"expected_dependencies":  append([]string{}, update.requested.ExpectedDependsOn...),
			"desired_dependencies":   append([]string{}, update.requested.DesiredDependsOn...),
			"canonical_dependencies": append([]string{}, update.canonical...),
		}
		for key, value := range extra {
			entryExtra[key] = value
		}
		update.task.History = append(update.task.History, models.TaskHistoryEntry{
			Time:   now,
			Event:  models.TaskEventDependenciesRewritten,
			Agent:  &agentID,
			Reason: &reason,
			Note:   &note,
			Extra:  entryExtra,
		})
		if update.task.ID != source.ID {
			models.AdvanceLifecycle(update.task)
		}
		applied = append(applied, AppliedDependencyUpdate{
			TaskID:                update.task.ID,
			CanonicalDependencies: append([]string{}, update.canonical...),
		})
	}
	return applied, nil
}
