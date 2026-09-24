package ops

import (
	stderrors "errors"
	"fmt"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/alerts"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/payloadschema"
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

func markBlockedWithOptionalAuthority(projectRoot, taskID, reason string, questions []string, agentID string, opts MarkBlockedOptions, authority *models.AgentAuthority) (result *MarkBlockedResult, retErr error) {
	var observed *models.Task
	mutationStarted := false
	invocation := NewLifecycleInvocation(projectRoot)
	defer func() {
		if retErr != nil {
			outcome, action, effects := models.LifecycleStateChanged, "requery", "none"
			var invalid *PreconditionError
			if stderrors.As(retErr, &invalid) {
				outcome, action = models.LifecycleInvalidInput, "correct_input"
			} else if mutationStarted {
				effects = "unknown"
			}
			if IsAgentAuthorityError(retErr) || filelock.IsLockErrorType(retErr, filelock.LockErrorTimeout) {
				effects = "none"
			}
			observed = readLifecycleTask(projectRoot, taskID, authority)
			retErr = WrapLifecycleError("mark-blocked", observed, retErr, outcome, action, effects)
		}
		var outcome models.LifecycleOutcome
		var warnings *[]string
		if result != nil {
			outcome, warnings = result.LifecycleOutcome, &result.Warnings
		}
		invocation.FinishResult("mark-blocked", outcome, &retErr, warnings)
	}()
	if err := ValidateLifecycleRequestOptions(opts.Request); err != nil {
		return nil, WrapLifecycleError("mark-blocked", nil, err, models.LifecycleInvalidInput, "correct_input", "none")
	}
	if agentID == "" {
		return nil, &PreconditionError{Reason: "agent ID is required"}
	}
	// One structural verdict for the preflight and this boundary, reached
	// before any state path is opened.
	if err := markBlockedStructuralError(MarkBlockedPayload(taskID, reason, questions, opts)); err != nil {
		return nil, err
	}
	repairRequest, err := normalizeRepairRequest(opts.RepairRequest, taskID)
	if err != nil {
		return nil, err
	}
	dependsOn, err := normalizeDependsOn(opts.DependsOn)
	if err != nil {
		return nil, err
	}

	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())
	now := time.Now().UTC()
	var resultDependsOn []string
	var outcome models.LifecycleOutcome
	var replay bool

	// Load pipeline config for status checks and transitions.
	resolver, _, err := loadResolver(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load pipeline config: %w", err)
	}
	var pipelineExecuting []models.TaskStatus
	for _, rpName := range resolver.RolePairNames() {
		if es, err := resolver.ExecutingStatus(rpName); err == nil {
			pipelineExecuting = append(pipelineExecuting, es)
		}
	}
	pipelineTransitions := BuildPipelineTransitions(resolver)

	err = lifecycleMutation(bb, authority)(func(state *models.State) error {
		task := state.FindTask(taskID)
		if task == nil {
			return &errors.NotFoundError{Entity: "task", ID: taskID}
		}
		observed = task
		request, err := NewLifecycleRequest("mark-blocked", task, agentID, authority, opts.Request, struct {
			Reason       string
			Questions    []string
			Repair       *models.RepairRequest
			Dependencies []string
		}{reason, questions, repairRequest, dependsOn})
		if err != nil {
			return err
		}
		receipt, err := checkOwnerEndingRequest(task, request)
		if err != nil {
			return err
		}
		if receipt != nil {
			outcome, replay = LifecycleReplayOutcome(task, receipt, agentID), true
			return errLifecycleReplay
		}

		if !isExecutingStatus(task.Status, pipelineExecuting) {
			return WrapLifecycleError("mark-blocked", task, &PreconditionError{Reason: fmt.Sprintf("task must be in an executing status to be marked blocked, current status: %s", task.Status)}, models.LifecycleAlreadyTransitioned, "stop", "none")
		}

		if task.AssignedTo == nil || *task.AssignedTo != agentID {
			return WrapLifecycleError("mark-blocked", task, &PreconditionError{Reason: "only the assigned agent can mark task as blocked"}, models.LifecycleStaleCaller, "stop", "none")
		}
		if err := validateDependsOnForBlockedTask(state, task, dependsOn); err != nil {
			return err
		}
		// Validate added edges without preventing emergency blocking to repair existing metadata.
		if err := validateDependencyDirection(state, resolver, task.ID, task.RolePair, dependsOn); err != nil {
			return err
		}

		// Blocking ends the authorized owner's work and retires its unfinished preparation.
		mutationStarted = true
		models.AdvanceLifecycle(task)
		if err := task.TransitionWith(models.TaskStatusBlocked, pipelineTransitions); err != nil {
			return err
		}
		task.BlockedReason = &reason
		task.BlockedQuestions = questions
		task.RepairRequest = repairRequest
		if len(dependsOn) > 0 {
			task.DependsOn = append(task.DependsOn, dependsOn...)
		}
		resultDependsOn = append([]string(nil), task.DependsOn...)
		releaseAgentsForTask(state, taskID)
		task.AssignedTo = nil
		task.LeaseExpires = nil

		task.History = append(task.History, models.TaskHistoryEntry{
			Time:   now,
			Event:  models.TaskEventBlocked,
			Agent:  &agentID,
			Reason: &reason,
		})

		outcome, err = CompleteLifecycleRequest(task, request, models.LifecycleProjection{}, state.Agents)
		return err
	})

	if isLifecycleReplay(err) && replay {
		err = nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to mark task as blocked: %w", err)
	}
	if replay {
		return &MarkBlockedResult{LifecycleOutcome: outcome, TaskID: taskID}, nil
	}

	var warnings []string
	message := fmt.Sprintf("%s — %s", taskID, reason)
	if err := alerts.Write(lp.AlertsLogPath(), alerts.Alert{
		Timestamp: now,
		Level:     alerts.AlertLevelWarning,
		Category:  "BLOCKED",
		Message:   message,
		// now is the blocked history entry's time, so watchers derive the same
		// key from state and this episode is logged once between us.
		OnceKey: alerts.BlockedEpisodeKey(taskID, now, message),
	}); err != nil {
		warnings = append(warnings, fmt.Sprintf("alert write failed: %v", err))
	}

	return &MarkBlockedResult{
		LifecycleOutcome: outcome,
		TaskID:           taskID,
		Reason:           reason,
		DependsOn:        resultDependsOn,
		RepairRequest:    repairRequest,
		Warnings:         warnings,
	}, nil
}

// MarkBlockedPayload builds the canonical object of one blocking call. The
// agent ID authorizing the call is absent by design: no structural rule reads
// it, so it stays at this boundary rather than entering the payload.
func MarkBlockedPayload(taskID, reason string, questions []string, opts MarkBlockedOptions) payloadschema.MarkBlockedPayload {
	return payloadschema.MarkBlockedPayload{
		TaskID:        taskID,
		Reason:        reason,
		Questions:     questions,
		DependsOn:     opts.DependsOn,
		RepairRequest: opts.RepairRequest,
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
