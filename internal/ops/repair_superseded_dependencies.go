package ops

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/statevalidate"
)

const repairSupersededDependenciesOperation = "repair-superseded-dependencies"

// RepairSupersededDependenciesResult contains the audited dependency cleanup.
type RepairSupersededDependenciesResult struct {
	models.LifecycleOutcome
	TaskID               string   `json:"task_id"`
	RemovedDependencies  []string `json:"removed_dependencies"`
	RetainedDependencies []string `json:"retained_dependencies"`
	Warnings             []string `json:"warnings,omitempty"`
}

func (r *RepairSupersededDependenciesResult) GetWarnings() []string {
	if r == nil {
		return nil
	}
	return r.Warnings
}

// RepairSupersededDependencies removes all downstream-role dependency edges
// from one SUPERSEDED task in a single validated state transaction.
func RepairSupersededDependencies(projectRoot, taskID, reason, agentID string) (*RepairSupersededDependenciesResult, error) {
	return repairSupersededDependenciesWithOptionalAuthority(projectRoot, taskID, reason, agentID, nil)
}

// RepairSupersededDependenciesWithAuthority fences the terminal dependency
// repair with the orchestrator's registration generation.
func RepairSupersededDependenciesWithAuthority(projectRoot, taskID, reason string, authority models.AgentAuthority) (*RepairSupersededDependenciesResult, error) {
	return repairSupersededDependenciesWithOptionalAuthority(projectRoot, taskID, reason, authority.ID, &authority)
}

// RepairSupersededDependenciesWithOptions identifies a terminal metadata repair.
func RepairSupersededDependenciesWithOptions(projectRoot, taskID, reason, agentID string, opts LifecycleRequestOptions) (*RepairSupersededDependenciesResult, error) {
	return repairSupersededDependenciesWithOptionalAuthority(projectRoot, taskID, reason, agentID, nil, opts)
}

// RepairSupersededDependenciesWithAuthorityAndOptions fences an identified repair.
func RepairSupersededDependenciesWithAuthorityAndOptions(projectRoot, taskID, reason string, authority models.AgentAuthority, opts LifecycleRequestOptions) (*RepairSupersededDependenciesResult, error) {
	return repairSupersededDependenciesWithOptionalAuthority(projectRoot, taskID, reason, authority.ID, &authority, opts)
}

func repairSupersededDependenciesWithOptionalAuthority(projectRoot, taskID, reason, agentID string, authority *models.AgentAuthority, options ...LifecycleRequestOptions) (returned *RepairSupersededDependenciesResult, retErr error) {
	invocation := NewLifecycleInvocation(projectRoot)
	const operation = repairSupersededDependenciesOperation
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
	if taskID == "" {
		return nil, &PreconditionError{Reason: "task ID is required"}
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

	var result RepairSupersededDependenciesResult
	now := time.Now().UTC()
	err = lifecycleMutation(bb, authority)(func(state *models.State) (callbackErr error) {
		defer func() {
			callbackErr = WrapLifecycleError(operation, observed, callbackErr, models.LifecycleInvalidInput, "correct_input", "none")
		}()
		task := state.FindTask(taskID)
		if task == nil {
			return &errors.NotFoundError{Entity: "task", ID: taskID}
		}
		copy := *task
		observed = &copy
		request, err := NewLifecycleRequest(operation, task, agentID, authority, opts, reason)
		if err != nil {
			return err
		}
		receipt, err := CheckLifecycleRequest(task, request)
		if err != nil {
			return err
		}
		if receipt != nil {
			result = RepairSupersededDependenciesResult{TaskID: taskID, LifecycleOutcome: LifecycleReplayOutcome(task, receipt, agentID)}
			return errLifecycleReplay
		}
		if task.Status != models.TaskStatusSuperseded {
			return &PreconditionError{Reason: fmt.Sprintf("cannot repair dependencies on task %s in status %s (must be SUPERSEDED)", taskID, task.Status)}
		}

		retained, removed, err := pruneDownstreamDependencies(state, resolver, task)
		if err != nil {
			return err
		}
		if len(removed) == 0 {
			return WrapLifecycleError(operation, task, &PreconditionError{Reason: fmt.Sprintf("task %s has no illegal downstream dependencies", taskID)}, models.LifecycleStateChanged, "requery", "none")
		}

		task.DependsOn = retained
		note := fmt.Sprintf("removed downstream dependencies: %s", strings.Join(removed, ", "))
		task.History = append(task.History, models.TaskHistoryEntry{
			Time:   now,
			Event:  models.TaskEventDependenciesRewritten,
			Agent:  &agentID,
			Reason: &reason,
			Note:   &note,
			Extra: map[string]any{
				"manual":                true,
				"operation":             repairSupersededDependenciesOperation,
				"removed_dependencies":  append([]string(nil), removed...),
				"retained_dependencies": append([]string(nil), retained...),
			},
		})

		if err := statevalidate.ValidateState(state, projectRoot, false, io.Discard); err != nil {
			return err
		}

		result = RepairSupersededDependenciesResult{
			TaskID:               taskID,
			RemovedDependencies:  append([]string(nil), removed...),
			RetainedDependencies: append([]string(nil), retained...),
		}
		result.LifecycleOutcome, err = CompleteLifecycleRequest(task, request, models.LifecycleProjection{})
		if err == nil {
			effects = "unknown"
		}
		return err
	})
	if isLifecycleReplay(err) {
		return &result, nil
	}
	if err != nil {
		return nil, WrapLifecycleError(operation, observed, fmt.Errorf("failed to repair superseded dependencies: %w", err), models.LifecycleStateChanged, "requery", effects)
	}

	logEntry := log.Entry{
		Timestamp: now,
		Agent:     agentID,
		Action:    repairSupersededDependenciesOperation,
		Task:      &taskID,
		Detail: fmt.Sprintf(
			"removed=%s retained=%s: %s",
			strings.Join(result.RemovedDependencies, ","),
			strings.Join(result.RetainedDependencies, ","),
			reason,
		),
	}
	if err := log.New(lp.LogPath()).Append(logEntry); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("activity log write failed: %v", err))
	}

	return &result, nil
}
