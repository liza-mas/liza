package ops

import (
	"fmt"
	"os"
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

// repairSupersededRun carries one repair-superseded-dependencies invocation
// through its concerns: request checks, in-lock admission, eligibility,
// pruning, adoption and result reporting. observed and effects feed the
// deferred lifecycle wrapping of the entry point.
type repairSupersededRun struct {
	invocation  *LifecycleInvocation
	projectRoot string
	logPath     string
	taskID      string
	reason      string
	agentID     string
	authority   *models.AgentAuthority
	opts        LifecycleRequestOptions
	now         time.Time
	bb          *db.Blackboard
	resolver    *pipeline.Resolver
	observed    *models.Task
	effects     LifecycleEffects
	result      RepairSupersededDependenciesResult
}

func repairSupersededDependenciesWithOptionalAuthority(projectRoot, taskID, reason, agentID string, authority *models.AgentAuthority, options ...LifecycleRequestOptions) (returned *RepairSupersededDependenciesResult, retErr error) {
	run := &repairSupersededRun{
		invocation:  NewLifecycleInvocation(projectRoot),
		projectRoot: projectRoot,
		taskID:      taskID,
		reason:      reason,
		agentID:     agentID,
		authority:   authority,
		effects:     LifecycleEffectsNone,
	}
	if len(options) > 0 {
		run.opts = options[0]
	}
	defer func() {
		retErr = WrapLifecycleError(repairSupersededDependenciesOperation, run.observed, retErr, models.LifecycleInvalidInput, LifecycleSafeActionCorrectInput, LifecycleEffectsNone)
		var outcome models.LifecycleOutcome
		var warnings *[]string
		if returned != nil {
			outcome = returned.LifecycleOutcome
			warnings = &returned.Warnings
		}
		run.invocation.FinishResult(repairSupersededDependenciesOperation, outcome, &retErr, warnings)
	}()
	if err := run.checkRequest(); err != nil {
		return nil, err
	}
	if err := run.openState(); err != nil {
		return nil, err
	}
	err := lifecycleMutation(run.bb, authority)(run.mutate)
	return run.report(err)
}

// checkRequest refuses a malformed request before any state is read.
func (r *repairSupersededRun) checkRequest() error {
	if err := ValidateLifecycleRequestOptions(r.opts); err != nil {
		return err
	}
	if r.taskID == "" {
		return &PreconditionError{Reason: "task ID is required"}
	}
	if r.reason == "" {
		return &PreconditionError{Reason: "reason is required"}
	}
	if r.agentID == "" {
		return &PreconditionError{Reason: "orchestrator agent ID is required"}
	}
	return nil
}

// openState binds the request blackboard and the pipeline resolver.
func (r *repairSupersededRun) openState() error {
	lp := paths.New(r.projectRoot)
	r.logPath = lp.LogPath()
	r.bb = RequestBlackboard(lp.StatePath(), r.authority, r.opts)
	resolver, _, err := loadResolver(r.projectRoot)
	if err != nil {
		return WrapLifecycleError(repairSupersededDependenciesOperation, nil, fmt.Errorf("failed to load pipeline config: %w", err), models.LifecycleStateChanged, LifecycleSafeActionRequery, LifecycleEffectsNone)
	}
	r.resolver = resolver
	r.now = time.Now().UTC()
	return nil
}

// mutate is the locked callback: admission and SUPERSEDED eligibility, pruning,
// adoption, full-candidate validation and receipt completion, in that order.
func (r *repairSupersededRun) mutate(state *models.State) (callbackErr error) {
	defer func() {
		callbackErr = WrapLifecycleError(repairSupersededDependenciesOperation, r.observed, callbackErr, models.LifecycleInvalidInput, LifecycleSafeActionCorrectInput, LifecycleEffectsNone)
	}()
	task, request, err := r.admit(state)
	if err != nil {
		return err
	}
	retained, removed, err := r.prune(state, task)
	if err != nil {
		return err
	}
	r.adopt(task, retained, removed)
	if err := statevalidate.ValidateCandidate(state, r.bb.ReadSnapshot, r.projectRoot, false, os.Stderr); err != nil {
		return err
	}
	r.result = RepairSupersededDependenciesResult{
		TaskID:               r.taskID,
		RemovedDependencies:  append([]string(nil), removed...),
		RetainedDependencies: append([]string(nil), retained...),
	}
	r.result.LifecycleOutcome, err = CompleteLifecycleRequest(task, request, models.LifecycleProjection{}, state.Agents)
	if err == nil {
		r.effects = LifecycleEffectsUnknown
	}
	return err
}

// admit looks the task up, records the observed copy, checks the request
// receipt (an already completed request is a replay) and requires SUPERSEDED.
func (r *repairSupersededRun) admit(state *models.State) (*models.Task, LifecycleRequest, error) {
	task := state.FindTask(r.taskID)
	if task == nil {
		return nil, LifecycleRequest{}, &errors.NotFoundError{Entity: notFoundEntityTask, ID: r.taskID}
	}
	copy := *task
	r.observed = &copy
	request, err := NewLifecycleRequest(repairSupersededDependenciesOperation, task, r.agentID, r.authority, r.opts, r.reason)
	if err != nil {
		return nil, request, err
	}
	receipt, err := CheckLifecycleRequest(task, request, state.Agents)
	if err != nil {
		return nil, request, err
	}
	if receipt != nil {
		r.result = RepairSupersededDependenciesResult{TaskID: r.taskID, LifecycleOutcome: LifecycleReplayOutcome(task, receipt, r.agentID)}
		return nil, request, errLifecycleReplay
	}
	if task.Status != models.TaskStatusSuperseded {
		return nil, request, &PreconditionError{Reason: fmt.Sprintf("cannot repair dependencies on task %s in status %s (must be SUPERSEDED)", r.taskID, task.Status)}
	}
	return task, request, nil
}

// prune splits the task's edges into retained and illegal downstream ones and
// refuses a repair with nothing to remove.
func (r *repairSupersededRun) prune(state *models.State, task *models.Task) (retained, removed []string, err error) {
	retained, removed, err = pruneDownstreamDependencies(state, r.resolver, task)
	if err != nil {
		return nil, nil, err
	}
	if len(removed) == 0 {
		return nil, nil, WrapLifecycleError(repairSupersededDependenciesOperation, task, &PreconditionError{Reason: fmt.Sprintf("task %s has no illegal downstream dependencies", r.taskID)}, models.LifecycleStateChanged, LifecycleSafeActionRequery, LifecycleEffectsNone)
	}
	return retained, removed, nil
}

// adopt assigns the retained list and appends the audit entry.
func (r *repairSupersededRun) adopt(task *models.Task, retained, removed []string) {
	task.DependsOn = retained
	note := fmt.Sprintf("removed downstream dependencies: %s", strings.Join(removed, ", "))
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:   r.now,
		Event:  models.TaskEventDependenciesRewritten,
		Agent:  &r.agentID,
		Reason: &r.reason,
		Note:   &note,
		Extra: map[string]any{
			"manual":                true,
			"operation":             repairSupersededDependenciesOperation,
			"removed_dependencies":  append([]string(nil), removed...),
			"retained_dependencies": append([]string(nil), retained...),
		},
	})
}

// report maps the mutation outcome to the operation result: a replay returns
// the original completion, any failure is wrapped as a changed state, and a
// commit is followed by the activity log.
func (r *repairSupersededRun) report(err error) (*RepairSupersededDependenciesResult, error) {
	if isLifecycleReplay(err) {
		return &r.result, nil
	}
	if err != nil {
		return nil, WrapLifecycleError(repairSupersededDependenciesOperation, r.observed, fmt.Errorf("failed to repair superseded dependencies: %w", err), models.LifecycleStateChanged, LifecycleSafeActionRequery, r.effects)
	}
	logEntry := log.Entry{
		Timestamp: r.now,
		Agent:     r.agentID,
		Action:    repairSupersededDependenciesOperation,
		Task:      &r.taskID,
		Detail: fmt.Sprintf(
			"removed=%s retained=%s: %s",
			strings.Join(r.result.RemovedDependencies, ","),
			strings.Join(r.result.RetainedDependencies, ","),
			r.reason,
		),
	}
	if err := log.New(r.logPath).Append(logEntry); err != nil {
		r.result.Warnings = append(r.result.Warnings, fmt.Sprintf("activity log write failed: %v", err))
	}
	return &r.result, nil
}
