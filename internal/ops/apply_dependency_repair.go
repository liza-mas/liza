package ops

import (
	"fmt"
	"os"
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

const applyDependencyRepairOperation = models.RepairOperationApplyDependencyRepair

// applyRepairRun carries one apply-dependency-repair invocation through its
// concerns: request checks, in-lock admission, consumed-request resolution, the
// batch step with its source audit, complete-candidate adoption and result
// reporting. observed and effects feed the deferred lifecycle wrapping of the
// entry point.
type applyRepairRun struct {
	invocation   *LifecycleInvocation
	projectRoot  string
	logPath      string
	sourceTaskID string
	reason       string
	agentID      string
	authority    *models.AgentAuthority
	opts         LifecycleRequestOptions
	now          time.Time
	bb           *db.Blackboard
	resolver     *pipeline.Resolver
	observed     *models.Task
	effects      LifecycleEffects
	result       ApplyDependencyRepairResult
}

func applyDependencyRepairWithOptionalAuthority(projectRoot, sourceTaskID, reason, agentID string, authority *models.AgentAuthority, options ...LifecycleRequestOptions) (returned *ApplyDependencyRepairResult, retErr error) {
	run := &applyRepairRun{
		invocation:   NewLifecycleInvocation(projectRoot),
		projectRoot:  projectRoot,
		sourceTaskID: sourceTaskID,
		reason:       reason,
		agentID:      agentID,
		authority:    authority,
		effects:      LifecycleEffectsNone,
	}
	if len(options) > 0 {
		run.opts = options[0]
	}
	defer func() {
		retErr = WrapLifecycleError(applyDependencyRepairOperation, run.observed, retErr, models.LifecycleInvalidInput, LifecycleSafeActionCorrectInput, LifecycleEffectsNone)
		var outcome models.LifecycleOutcome
		var warnings *[]string
		if returned != nil {
			outcome = returned.LifecycleOutcome
			warnings = &returned.Warnings
		}
		run.invocation.FinishResult(applyDependencyRepairOperation, outcome, &retErr, warnings)
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
func (r *applyRepairRun) checkRequest() error {
	if err := ValidateLifecycleRequestOptions(r.opts); err != nil {
		return err
	}
	if r.sourceTaskID == "" {
		return &PreconditionError{Reason: "blocked task ID is required"}
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
func (r *applyRepairRun) openState() error {
	lp := paths.New(r.projectRoot)
	r.logPath = lp.LogPath()
	r.bb = RequestBlackboard(lp.StatePath(), r.authority, r.opts)
	resolver, _, err := loadResolver(r.projectRoot)
	if err != nil {
		return WrapLifecycleError(applyDependencyRepairOperation, nil, fmt.Errorf("failed to load pipeline config: %w", err), models.LifecycleStateChanged, LifecycleSafeActionRequery, LifecycleEffectsNone)
	}
	r.resolver = resolver
	r.now = time.Now().UTC()
	return nil
}

// mutate is the locked callback and the only place the batch is published:
// admission, request resolution, the batch step (every expected list compared
// before any write), the source audit, request clearing, complete-candidate
// validation and receipt completion, in that order.
func (r *applyRepairRun) mutate(state *models.State) (callbackErr error) {
	defer func() {
		callbackErr = WrapLifecycleError(applyDependencyRepairOperation, r.observed, callbackErr, models.LifecycleInvalidInput, LifecycleSafeActionCorrectInput, LifecycleEffectsNone)
	}()
	source, request, err := r.admit(state)
	if err != nil {
		return err
	}
	consumed, err := consumeRepairRequest(source)
	if err != nil {
		return err
	}
	updates, err := applyDependencyUpdatesInState(state, r.resolver, source, applyDependencyRepairOperation, consumed.request.DependencyUpdates, r.agentID, r.reason, r.now, consumed.batchAudit(source.ID))
	if err != nil {
		return err
	}
	consumed.auditSource(source, updates, r.agentID, r.reason, r.now)
	source.RepairRequest = nil
	if err := statevalidate.ValidateCandidate(state, r.bb.ReadSnapshot, r.projectRoot, false, os.Stderr); err != nil {
		return err
	}
	return r.complete(state, source, request, updates)
}

// admit looks the source up, records the observed copy, checks the request
// receipt (an already completed request is a replay) and refuses a source that
// is not BLOCKED or carries no repair request.
func (r *applyRepairRun) admit(state *models.State) (*models.Task, LifecycleRequest, error) {
	source := state.FindTask(r.sourceTaskID)
	if source == nil {
		return nil, LifecycleRequest{}, &errors.NotFoundError{Entity: notFoundEntityTask, ID: r.sourceTaskID}
	}
	copy := *source
	r.observed = &copy
	request, err := NewLifecycleRequest(applyDependencyRepairOperation, source, r.agentID, r.authority, r.opts, r.reason)
	if err != nil {
		return nil, request, err
	}
	receipt, err := CheckLifecycleRequest(source, request, state.Agents)
	if err != nil {
		return nil, request, err
	}
	if receipt != nil {
		r.result = ApplyDependencyRepairResult{SourceTaskID: r.sourceTaskID, LifecycleOutcome: LifecycleReplayOutcome(source, receipt, r.agentID)}
		return nil, request, errLifecycleReplay
	}
	if source.Status != models.TaskStatusBlocked {
		return nil, request, WrapLifecycleError(applyDependencyRepairOperation, source, &PreconditionError{Reason: fmt.Sprintf("cannot apply dependency repair from task %s in status %s (must be BLOCKED)", r.sourceTaskID, source.Status)}, models.LifecycleAlreadyTransitioned, LifecycleSafeActionStop, LifecycleEffectsNone)
	}
	if source.RepairRequest == nil {
		return nil, request, WrapLifecycleError(applyDependencyRepairOperation, source, &PreconditionError{Reason: fmt.Sprintf("blocked task %s has no repair request", r.sourceTaskID)}, models.LifecycleStateChanged, LifecycleSafeActionRequery, LifecycleEffectsNone)
	}
	return source, request, nil
}

// complete records the result and completes the lifecycle request.
func (r *applyRepairRun) complete(state *models.State, source *models.Task, request LifecycleRequest, updates []AppliedDependencyUpdate) error {
	r.result = ApplyDependencyRepairResult{
		SourceTaskID: r.sourceTaskID,
		Updates:      updates,
	}
	var err error
	r.result.LifecycleOutcome, err = CompleteLifecycleRequest(source, request, models.LifecycleProjection{}, state.Agents)
	if err == nil {
		r.effects = LifecycleEffectsUnknown
	}
	return err
}

// report maps the mutation outcome to the operation result: a replay returns
// the original completion, any other failure is wrapped as a changed state, and
// a commit is followed by the activity log.
func (r *applyRepairRun) report(err error) (*ApplyDependencyRepairResult, error) {
	if isLifecycleReplay(err) {
		return &r.result, nil
	}
	if err != nil {
		return nil, WrapLifecycleError(applyDependencyRepairOperation, r.observed, fmt.Errorf("failed to apply dependency repair: %w", err), models.LifecycleStateChanged, LifecycleSafeActionRequery, r.effects)
	}
	updated := make([]string, 0, len(r.result.Updates))
	for _, update := range r.result.Updates {
		updated = append(updated, fmt.Sprintf("%s=[%s]", update.TaskID, strings.Join(update.CanonicalDependencies, ",")))
	}
	logEntry := log.Entry{
		Timestamp: r.now,
		Agent:     r.agentID,
		Action:    applyDependencyRepairOperation,
		Task:      &r.sourceTaskID,
		Detail:    fmt.Sprintf("updated=%s: %s", strings.Join(updated, " "), r.reason),
	}
	if err := log.New(r.logPath).Append(logEntry); err != nil {
		r.result.Warnings = append(r.result.Warnings, fmt.Sprintf("activity log write failed: %v", err))
	}
	return &r.result, nil
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
			return nil, WrapLifecycleError(operation, source, &PreconditionError{Reason: fmt.Sprintf("task %s dependencies changed since the repair request was created: got %v, expected %v", task.ID, task.DependsOn, update.ExpectedDependsOn)}, models.LifecycleStateChanged, LifecycleSafeActionRequery, LifecycleEffectsNone)
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
