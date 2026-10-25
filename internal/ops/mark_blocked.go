package ops

import (
	stderrors "errors"
	"fmt"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/alerts"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/payloadschema"
	"github.com/liza-mas/liza/internal/pipeline"
)

// MarkBlockedResult contains the outcome of marking a task as blocked.
type MarkBlockedResult struct {
	models.LifecycleOutcome
	TaskID        string                `json:"task_id"`
	Reason        string                `json:"reason"`
	DependsOn     []string              `json:"depends_on,omitempty"`
	RepairRequest *models.RepairRequest `json:"repair_request,omitempty"`
	Warnings      []string              `json:"warnings,omitempty"`
}

func (r *MarkBlockedResult) GetWarnings() []string {
	if r == nil {
		return nil
	}
	return r.Warnings
}

// MarkBlockedOptions contains optional metadata for a block transition.
type MarkBlockedOptions struct {
	Request       LifecycleRequestOptions
	RepairRequest *models.RepairRequest
	DependsOn     []string
	// HumanAction, when set, is the action only a human can take to clear the
	// block; it raises AWAITING HUMAN (ADR-0172).
	HumanAction      string
	StateLockTimeout bool
}

// MarkBlocked transitions a task from an executing status to BLOCKED. Only the
// assigned agent can block its own task. Requires reason and 1-3 clarifying
// questions per the blocking protocol. Uses pipeline-defined executing statuses.
func MarkBlocked(projectRoot, taskID, reason string, questions []string, agentID string) (*MarkBlockedResult, error) {
	return MarkBlockedWithOptions(projectRoot, taskID, reason, questions, agentID, MarkBlockedOptions{})
}

// MarkBlockedWithOptions transitions a task from an executing status to BLOCKED
// and optionally records a structured repair request for orchestrator follow-up.
func MarkBlockedWithOptions(projectRoot, taskID, reason string, questions []string, agentID string, opts MarkBlockedOptions) (*MarkBlockedResult, error) {
	return markBlockedWithOptionalAuthority(projectRoot, taskID, reason, questions, agentID, opts, nil)
}

// MarkBlockedWithAuthority fences the block transition with the caller's
// registration generation.
func MarkBlockedWithAuthority(projectRoot, taskID, reason string, questions []string, authority models.AgentAuthority, opts MarkBlockedOptions) (*MarkBlockedResult, error) {
	return markBlockedWithOptionalAuthority(projectRoot, taskID, reason, questions, authority.ID, opts, &authority)
}

// markBlockedCall carries one block transition across its phases: input
// normalization, pipeline loading, the locked mutation and the post-commit alert.
type markBlockedCall struct {
	projectRoot, taskID, reason string
	questions                   []string
	agentID                     string
	opts                        MarkBlockedOptions
	authority                   *models.AgentAuthority

	repairRequest       *models.RepairRequest
	dependsOn           []string
	bb                  *db.Blackboard
	now                 time.Time
	resolver            *pipeline.Resolver
	pipelineExecuting   []models.TaskStatus
	pipelineTransitions map[models.TaskStatus][]models.TaskStatus

	mutationStarted bool
	replay          bool
	outcome         models.LifecycleOutcome
	resultDependsOn []string
}

func markBlockedWithOptionalAuthority(projectRoot, taskID, reason string, questions []string, agentID string, opts MarkBlockedOptions, authority *models.AgentAuthority) (result *MarkBlockedResult, retErr error) {
	call := &markBlockedCall{projectRoot: projectRoot, taskID: taskID, reason: reason, questions: questions, agentID: agentID, opts: opts, authority: authority}
	invocation := NewLifecycleInvocation(projectRoot)
	defer func() {
		if retErr != nil {
			retErr = call.classifyFailure(retErr)
		}
		var outcome models.LifecycleOutcome
		var warnings *[]string
		if result != nil {
			outcome, warnings = result.LifecycleOutcome, &result.Warnings
		}
		invocation.FinishResult("mark-blocked", outcome, &retErr, warnings)
	}()
	if err := call.normalizeInput(); err != nil {
		return nil, err
	}

	lp := paths.New(projectRoot)
	call.bb = RequestBlackboard(lp.StatePath(), authority, opts.Request)
	call.now = time.Now().UTC()
	if err := call.loadPipeline(); err != nil {
		return nil, err
	}

	err := lifecycleMutation(call.bb, authority)(call.mutate)
	if isLifecycleReplay(err) && call.replay {
		err = nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to mark task as blocked: %w", err)
	}
	if call.replay {
		return &MarkBlockedResult{LifecycleOutcome: call.outcome, TaskID: taskID}, nil
	}
	return call.completed(lp), nil
}

// classifyFailure wraps a failed call with the outcome the result contract
// prescribes for it, observing the task as it now stands.
func (c *markBlockedCall) classifyFailure(err error) error {
	outcome, action, effects := models.LifecycleStateChanged, LifecycleSafeActionRequery, LifecycleEffectsNone
	var invalid *PreconditionError
	if stderrors.As(err, &invalid) {
		outcome, action = models.LifecycleInvalidInput, LifecycleSafeActionCorrectInput
	} else if c.mutationStarted {
		effects = LifecycleEffectsUnknown
	}
	if IsAgentAuthorityError(err) || filelock.IsLockErrorType(err, filelock.LockErrorTimeout) {
		effects = LifecycleEffectsNone
	}
	observed := readLifecycleTask(c.projectRoot, c.taskID, c.authority, c.opts.Request)
	return WrapLifecycleError("mark-blocked", observed, err, outcome, action, effects)
}

// normalizeInput validates and normalizes the caller's input before any state
// path is opened.
func (c *markBlockedCall) normalizeInput() error {
	if err := ValidateLifecycleRequestOptions(c.opts.Request); err != nil {
		return WrapLifecycleError("mark-blocked", nil, err, models.LifecycleInvalidInput, LifecycleSafeActionCorrectInput, LifecycleEffectsNone)
	}
	if c.agentID == "" {
		return &PreconditionError{Reason: "agent ID is required"}
	}
	// One structural verdict for the preflight and this boundary, reached
	// before any state path is opened.
	if err := markBlockedStructuralError(MarkBlockedPayload(c.taskID, c.reason, c.questions, c.opts)); err != nil {
		return err
	}
	var err error
	if c.repairRequest, err = normalizeRepairRequest(c.opts.RepairRequest, c.taskID); err != nil {
		return err
	}
	c.dependsOn, err = normalizeDependsOn(c.opts.DependsOn)
	return err
}

// loadPipeline loads the pipeline config for status checks and transitions.
func (c *markBlockedCall) loadPipeline() error {
	resolver, _, err := loadResolver(c.projectRoot)
	if err != nil {
		return fmt.Errorf("failed to load pipeline config: %w", err)
	}
	c.resolver = resolver
	for _, rpName := range resolver.RolePairNames() {
		if es, err := resolver.ExecutingStatus(rpName); err == nil {
			c.pipelineExecuting = append(c.pipelineExecuting, es)
		}
	}
	c.pipelineTransitions = BuildPipelineTransitions(resolver)
	return nil
}

// completed raises the BLOCKED alert for a committed block and builds its result.
func (c *markBlockedCall) completed(lp paths.LizaPaths) *MarkBlockedResult {
	var warnings []string
	message := fmt.Sprintf("%s — %s", c.taskID, c.reason)
	if err := alerts.Write(lp.AlertsLogPath(), alerts.Alert{
		Timestamp: c.now,
		Level:     alerts.AlertLevelWarning,
		Category:  "BLOCKED",
		Message:   message,
		// now is the blocked history entry's time, so watchers derive the same
		// key from state and this episode is logged once between us.
		OnceKey: alerts.BlockedEpisodeKey(c.taskID, c.now, message),
	}); err != nil {
		warnings = append(warnings, fmt.Sprintf("alert write failed: %v", err))
	}

	return &MarkBlockedResult{
		LifecycleOutcome: c.outcome,
		TaskID:           c.taskID,
		Reason:           c.reason,
		DependsOn:        c.resultDependsOn,
		RepairRequest:    c.repairRequest,
		Warnings:         warnings,
	}
}

// MarkBlockedPayload builds the canonical object of one blocking call. The
// agent ID authorizing the call is absent by design: no structural rule reads
// it, so it stays at this boundary rather than entering the payload.
func MarkBlockedPayload(taskID, reason string, questions []string, opts MarkBlockedOptions) payloadschema.MarkBlockedPayload {
	return payloadschema.MarkBlockedPayload{
		TaskID:           taskID,
		Reason:           reason,
		Questions:        questions,
		DependsOn:        opts.DependsOn,
		RepairRequest:    opts.RepairRequest,
		HumanAction:      opts.HumanAction,
		StateLockTimeout: opts.StateLockTimeout,
	}
}

// markBlockedStructuralError reports the schema's verdict as the failure the
// result contract prescribes: INVALID_INPUT carrying every rejected field,
// with the first constraint as the cause agents already read.
func markBlockedStructuralError(payload payloadschema.MarkBlockedPayload) error {
	_, diagnostics, err := payloadschema.Validate(payloadschema.MarkBlockedOperation, payload)
	if err != nil {
		return err
	}
	if len(diagnostics) == 0 {
		return nil
	}
	return NewLifecycleInvalidInputError("mark-blocked", nil, diagnostics, preconditionFromDiagnostics(diagnostics))
}

// preconditionFromDiagnostics restates the schema's first rejection as the
// precondition error this package's direct callers already handle, so one
// implementation decides the verdict wherever it is reached.
func preconditionFromDiagnostics(diagnostics []models.FieldDiagnostic) error {
	if len(diagnostics) == 0 {
		return nil
	}
	return &PreconditionError{Reason: diagnostics[0].Constraint}
}

func normalizeDependsOn(values []string) ([]string, error) {
	if err := preconditionFromDiagnostics(payloadschema.ValidateMarkBlockedDependsOn(values)); err != nil {
		return nil, err
	}

	var normalized []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			normalized = append(normalized, strings.TrimSpace(part))
		}
	}
	return normalized, nil
}

func validateDependsOnForBlockedTask(state *models.State, task *models.Task, deps []string) error {
	if len(deps) == 0 {
		return nil
	}
	existing := make(map[string]bool, len(task.DependsOn))
	for _, depID := range task.DependsOn {
		trimmed := strings.TrimSpace(depID)
		if trimmed == "" {
			return &PreconditionError{Reason: fmt.Sprintf("task %s has empty existing depends_on entry", task.ID)}
		}
		if existing[trimmed] {
			return &PreconditionError{Reason: fmt.Sprintf("task %s has duplicate existing depends_on entry %q", task.ID, trimmed)}
		}
		existing[trimmed] = true
	}
	for _, depID := range deps {
		if depID == task.ID {
			return &PreconditionError{Reason: fmt.Sprintf("task %s cannot depend on itself", task.ID)}
		}
		if existing[depID] {
			return &PreconditionError{Reason: fmt.Sprintf("depends-on entry %q already exists on task %s", depID, task.ID)}
		}
		if state.FindTask(depID) == nil {
			return &PreconditionError{Reason: fmt.Sprintf("depends-on references non-existent task %q", depID)}
		}
		if dependencyReachesTask(state, depID, task.ID, map[string]bool{}) {
			return &PreconditionError{Reason: fmt.Sprintf("depends-on entry %q would create a dependency cycle for task %s", depID, task.ID)}
		}
	}
	return nil
}

func dependencyReachesTask(state *models.State, currentID, targetID string, visited map[string]bool) bool {
	if currentID == targetID {
		return true
	}
	if visited[currentID] {
		return false
	}
	visited[currentID] = true
	current := state.FindTask(currentID)
	if current == nil {
		return false
	}
	for _, depID := range current.DependsOn {
		if dependencyReachesTask(state, depID, targetID, visited) {
			return true
		}
	}
	return false
}

func normalizeRepairRequest(request *models.RepairRequest, blockedTaskID string) (*models.RepairRequest, error) {
	if request == nil {
		return nil, nil
	}
	if err := preconditionFromDiagnostics(payloadschema.ValidateMarkBlockedRepairRequest(request, blockedTaskID)); err != nil {
		return nil, err
	}

	normalized := &models.RepairRequest{
		Operation:  strings.TrimSpace(request.Operation),
		Target:     strings.TrimSpace(request.Target),
		Command:    strings.TrimSpace(request.Command),
		Evidence:   compactNonEmpty(request.Evidence),
		Validation: compactNonEmpty(request.Validation),
	}
	if normalized.Operation == models.RepairOperationApplyDependencyRepair {
		dependencyUpdates, err := normalizeDependencyUpdates(request.DependencyUpdates)
		if err != nil {
			return nil, err
		}
		normalized.DependencyUpdates = dependencyUpdates
	}
	return normalized, nil
}

func normalizeDependencyUpdates(updates []models.DependencyUpdate) ([]models.DependencyUpdate, error) {
	if err := preconditionFromDiagnostics(payloadschema.ValidateMarkBlockedDependencyUpdates(updates)); err != nil {
		return nil, err
	}

	normalized := make([]models.DependencyUpdate, 0, len(updates))
	for i, update := range updates {
		expected, err := normalizeExplicitDependencyList(update.ExpectedDependsOn, "expected_depends_on", i)
		if err != nil {
			return nil, err
		}
		desired, err := normalizeExplicitDependencyList(update.DesiredDependsOn, "desired_depends_on", i)
		if err != nil {
			return nil, err
		}
		normalized = append(normalized, models.DependencyUpdate{
			TaskID:            strings.TrimSpace(update.TaskID),
			ExpectedDependsOn: expected,
			DesiredDependsOn:  desired,
		})
	}
	return normalized, nil
}

func normalizeExplicitDependencyList(values []string, field string, updateIndex int) ([]string, error) {
	if err := preconditionFromDiagnostics(payloadschema.ValidateMarkBlockedDependencyList(values, field, updateIndex)); err != nil {
		return nil, err
	}

	normalized := make([]string, 0, len(values))
	for _, value := range values {
		normalized = append(normalized, strings.TrimSpace(value))
	}
	return normalized, nil
}

func compactNonEmpty(values []string) []string {
	return payloadschema.CompactNonEmpty(values)
}
