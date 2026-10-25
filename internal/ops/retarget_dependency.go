package ops

import (
	stderrors "errors"
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

const (
	retargetDependencyOperation              = "retarget-dependency"
	retargetDependencyRejectedAction         = "retarget_dependency_rejected"
	retargetDependencyCandidateValidation    = "candidate-state-validation"
	retargetDependencyRejectedDetailMaxBytes = 2048
)

// RetargetDependencyResult contains the outcome of retargeting one task
// dependency edge.
type RetargetDependencyResult struct {
	models.LifecycleOutcome
	TaskID                string   `json:"task_id"`
	OldDependency         string   `json:"old_dependency"`
	NewDependencies       []string `json:"new_dependencies"`
	CanonicalDependencies []string `json:"canonical_dependencies"`
	RepairRequestCleared  bool     `json:"repair_request_cleared"`
	Warnings              []string `json:"warnings,omitempty"`
}

func (r *RetargetDependencyResult) GetWarnings() []string {
	if r == nil {
		return nil
	}
	return r.Warnings
}

// RetargetDependency replaces one non-terminal task's direct depends_on edge
// with one or more direct dependencies. It is a metadata repair operation: it
// does not unblock the task or execute repair validation commands.
func RetargetDependency(projectRoot, taskID, oldDependency string, newDependencies []string, reason, agentID string) (*RetargetDependencyResult, error) {
	return retargetDependencyWithOptionalAuthority(projectRoot, taskID, oldDependency, newDependencies, reason, agentID, nil)
}

// RetargetDependencyWithAuthority fences the dependency rewrite with the
// orchestrator's registration generation.
func RetargetDependencyWithAuthority(projectRoot, taskID, oldDependency string, newDependencies []string, reason string, authority models.AgentAuthority) (*RetargetDependencyResult, error) {
	return retargetDependencyWithOptionalAuthority(projectRoot, taskID, oldDependency, newDependencies, reason, authority.ID, &authority)
}

// RetargetDependencyWithOptions identifies a dependency replacement invocation.
func RetargetDependencyWithOptions(projectRoot, taskID, oldDependency string, newDependencies []string, reason, agentID string, opts LifecycleRequestOptions) (*RetargetDependencyResult, error) {
	return retargetDependencyWithOptionalAuthority(projectRoot, taskID, oldDependency, newDependencies, reason, agentID, nil, opts)
}

// RetargetDependencyWithAuthorityAndOptions fences an identified replacement.
func RetargetDependencyWithAuthorityAndOptions(projectRoot, taskID, oldDependency string, newDependencies []string, reason string, authority models.AgentAuthority, opts LifecycleRequestOptions) (*RetargetDependencyResult, error) {
	return retargetDependencyWithOptionalAuthority(projectRoot, taskID, oldDependency, newDependencies, reason, authority.ID, &authority, opts)
}

// retargetRun carries one retarget-dependency invocation through its concerns:
// request checks, in-lock admission, eligibility, rewrite planning, adoption
// and result reporting. observed and effects feed the deferred lifecycle
// wrapping of the entry point.
type retargetRun struct {
	invocation      *LifecycleInvocation
	projectRoot     string
	logPath         string
	taskID          string
	oldDependency   string
	newDependencies []string
	reason          string
	agentID         string
	authority       *models.AgentAuthority
	opts            LifecycleRequestOptions
	now             time.Time
	bb              *db.Blackboard
	resolver        *pipeline.Resolver
	observed        *models.Task
	effects         LifecycleEffects
	result          RetargetDependencyResult

	repairRequestCleared bool
}

func retargetDependencyWithOptionalAuthority(projectRoot, taskID, oldDependency string, newDependencies []string, reason, agentID string, authority *models.AgentAuthority, options ...LifecycleRequestOptions) (returned *RetargetDependencyResult, retErr error) {
	run := &retargetRun{
		invocation:    NewLifecycleInvocation(projectRoot),
		projectRoot:   projectRoot,
		taskID:        taskID,
		oldDependency: oldDependency,
		reason:        reason,
		agentID:       agentID,
		authority:     authority,
		effects:       LifecycleEffectsNone,
	}
	if len(options) > 0 {
		run.opts = options[0]
	}
	defer func() {
		retErr = WrapLifecycleError(retargetDependencyOperation, run.observed, retErr, models.LifecycleInvalidInput, LifecycleSafeActionCorrectInput, LifecycleEffectsNone)
		var outcome models.LifecycleOutcome
		var warnings *[]string
		if returned != nil {
			outcome = returned.LifecycleOutcome
			warnings = &returned.Warnings
		}
		run.invocation.FinishResult(retargetDependencyOperation, outcome, &retErr, warnings)
	}()
	if err := run.checkRequest(newDependencies); err != nil {
		return nil, err
	}
	if err := run.openState(); err != nil {
		return nil, err
	}
	err := lifecycleMutation(run.bb, authority)(run.mutate)
	return run.report(err)
}

// checkRequest refuses a malformed request before any state is read and
// normalises the new dependencies.
func (r *retargetRun) checkRequest(newDependencies []string) error {
	if err := ValidateLifecycleRequestOptions(r.opts); err != nil {
		return err
	}
	if r.taskID == "" {
		return &PreconditionError{Reason: "task ID is required"}
	}
	if r.oldDependency == "" {
		return &PreconditionError{Reason: "old dependency is required"}
	}
	if r.reason == "" {
		return &PreconditionError{Reason: "reason is required"}
	}
	if r.agentID == "" {
		return &PreconditionError{Reason: "orchestrator agent ID is required"}
	}
	normalized, err := normalizeRetargetNewDependencies(newDependencies)
	if err != nil {
		return err
	}
	r.newDependencies = normalized
	return nil
}

// openState binds the request blackboard and the pipeline resolver.
func (r *retargetRun) openState() error {
	lp := paths.New(r.projectRoot)
	r.logPath = lp.LogPath()
	r.bb = RequestBlackboard(lp.StatePath(), r.authority, r.opts)
	resolver, _, err := loadResolver(r.projectRoot)
	if err != nil {
		return WrapLifecycleError(retargetDependencyOperation, nil, fmt.Errorf("failed to load pipeline config: %w", err), models.LifecycleStateChanged, LifecycleSafeActionRequery, LifecycleEffectsNone)
	}
	r.resolver = resolver
	r.now = time.Now().UTC()
	return nil
}

// mutate is the locked callback: admission, eligibility, rewrite planning,
// adoption, full-candidate validation and receipt completion, in that order.
func (r *retargetRun) mutate(state *models.State) (callbackErr error) {
	defer func() {
		callbackErr = WrapLifecycleError(retargetDependencyOperation, r.observed, callbackErr, models.LifecycleInvalidInput, LifecycleSafeActionCorrectInput, LifecycleEffectsNone)
	}()
	task, request, err := r.admit(state)
	if err != nil {
		return err
	}
	lineageRepair, err := r.checkEligibility(state, task)
	if err != nil {
		return err
	}
	canonical, err := r.planRewrite(state, task)
	if err != nil {
		return err
	}
	r.adopt(task, canonical, lineageRepair)
	if err := statevalidate.ValidateCandidate(state, r.bb.ReadSnapshot, r.projectRoot, false, os.Stderr); err != nil {
		return err
	}
	return r.complete(state, task, request, canonical)
}

// admit looks the task up, records the observed copy and checks the request
// receipt; an already completed request is a replay.
func (r *retargetRun) admit(state *models.State) (*models.Task, LifecycleRequest, error) {
	task := state.FindTask(r.taskID)
	if task == nil {
		return nil, LifecycleRequest{}, &errors.NotFoundError{Entity: "task", ID: r.taskID}
	}
	copy := *task
	r.observed = &copy
	request, err := NewLifecycleRequest(retargetDependencyOperation, task, r.agentID, r.authority, r.opts, struct {
		OldDependency   string
		NewDependencies []string
		Reason          string
	}{r.oldDependency, r.newDependencies, r.reason})
	if err != nil {
		return nil, request, err
	}
	receipt, err := CheckLifecycleRequest(task, request, state.Agents)
	if err != nil {
		return nil, request, err
	}
	if receipt != nil {
		r.result = RetargetDependencyResult{TaskID: r.taskID, LifecycleOutcome: LifecycleReplayOutcome(task, receipt, r.agentID)}
		return nil, request, errLifecycleReplay
	}
	return task, request, nil
}

// complete records the result and completes the lifecycle request.
func (r *retargetRun) complete(state *models.State, task *models.Task, request LifecycleRequest, canonical []string) error {
	r.result = RetargetDependencyResult{
		TaskID:                r.taskID,
		OldDependency:         r.oldDependency,
		NewDependencies:       append([]string(nil), r.newDependencies...),
		CanonicalDependencies: append([]string(nil), canonical...),
		RepairRequestCleared:  r.repairRequestCleared,
	}
	var err error
	r.result.LifecycleOutcome, err = CompleteLifecycleRequest(task, request, models.LifecycleProjection{}, state.Agents)
	if err == nil {
		r.effects = LifecycleEffectsUnknown
	}
	return err
}

// report maps the mutation outcome to the operation result: a replay returns
// the original completion, a dependency cycle is logged and wrapped with its
// diagnostics, any other failure is wrapped as a changed state, and a commit
// is followed by the activity log.
func (r *retargetRun) report(err error) (*RetargetDependencyResult, error) {
	if isLifecycleReplay(err) {
		return &r.result, nil
	}
	if err != nil {
		var cycleErr *statevalidate.DependencyCycleError
		if stderrors.As(err, &cycleErr) {
			return nil, r.cycleRejection(cycleErr, err)
		}
		return nil, WrapLifecycleError(retargetDependencyOperation, r.observed, fmt.Errorf("failed to retarget dependency: %w", err), models.LifecycleStateChanged, LifecycleSafeActionRequery, r.effects)
	}
	logEntry := log.Entry{
		Timestamp: r.now,
		Agent:     r.agentID,
		Action:    retargetDependencyOperation,
		Task:      &r.taskID,
		Detail:    fmt.Sprintf("%s -> %s: %s", r.oldDependency, strings.Join(r.newDependencies, ","), r.reason),
	}
	if err := log.New(r.logPath).Append(logEntry); err != nil {
		r.result.Warnings = append(r.result.Warnings, fmt.Sprintf("activity log write failed: %v", err))
	}
	return &r.result, nil
}

// cycleRejection logs the rejected candidate and wraps the cycle with the
// diagnostics a caller needs to repair it.
func (r *retargetRun) cycleRejection(cycleErr *statevalidate.DependencyCycleError, err error) error {
	cyclePath := slices.Clone(cycleErr.CyclePath)
	recordRetargetDependencyRejection(r.logPath, r.now, r.agentID, r.taskID, r.oldDependency, r.newDependencies, cyclePath, r.reason, err)
	return WrapLifecycleError(retargetDependencyOperation, r.observed, &OperationalError{
		Code:    "validation",
		Phase:   retargetDependencyCandidateValidation,
		Message: "retarget dependency rejected because the candidate state contains a dependency cycle",
		Details: map[string]any{
			"operation":         retargetDependencyOperation,
			"task_id":           r.taskID,
			"old_dependency":    r.oldDependency,
			"new_dependencies":  append([]string(nil), r.newDependencies...),
			"cycle_path":        cyclePath,
			"diagnostic_action": retargetDependencyRejectedAction,
		},
		Err: err,
	}, models.LifecycleInvalidInput, LifecycleSafeActionCorrectInput, LifecycleEffectsNone)
}

func recordRetargetDependencyRejection(logPath string, timestamp time.Time, agentID, taskID, oldDependency string, newDependencies, cyclePath []string, reason string, err error) {
	detail := fmt.Sprintf(
		"operation=%s phase=%s task_id=%s old_dependency=%s new_dependencies=%s cycle_path=%s reason=%s error=%v",
		retargetDependencyOperation,
		retargetDependencyCandidateValidation,
		taskID,
		oldDependency,
		strings.Join(newDependencies, ","),
		strings.Join(cyclePath, " -> "),
		reason,
		err,
	)
	// boundedString appends this marker after applying its byte limit, so reserve
	// the marker bytes and discard any partial UTF-8 rune at the cut point.
	const truncationMarker = "... [truncated]"
	detail = strings.ToValidUTF8(
		boundedMaskedString(detail, retargetDependencyRejectedDetailMaxBytes-len(truncationMarker)),
		"",
	)
	// The candidate mutation is already rejected; a secondary audit-log failure
	// must not replace the validation result.
	_ = log.New(logPath).Append(log.Entry{
		Timestamp: timestamp,
		Agent:     agentID,
		Action:    retargetDependencyRejectedAction,
		Task:      &taskID,
		Detail:    detail,
	})
}

func normalizeRetargetNewDependencies(values []string) ([]string, error) {
	var normalized []string
	seen := make(map[string]bool)
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			depID := strings.TrimSpace(part)
			if depID == "" {
				return nil, &PreconditionError{Reason: "new dependency entries cannot be empty"}
			}
			if seen[depID] {
				return nil, &PreconditionError{Reason: fmt.Sprintf("duplicate new dependency %q", depID)}
			}
			seen[depID] = true
			normalized = append(normalized, depID)
		}
	}
	if len(normalized) == 0 {
		return nil, &PreconditionError{Reason: "at least one new dependency is required"}
	}
	return normalized, nil
}
