package ops

import (
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/identity"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/roles"
)

// AssessHypothesisExhaustedResult contains the outcome of recording an orchestrator assessment.
type AssessHypothesisExhaustedResult struct {
	models.LifecycleOutcome
	TaskID   string   `json:"task_id"`
	Warnings []string `json:"warnings,omitempty"`
}

func (r *AssessHypothesisExhaustedResult) GetWarnings() []string {
	if r == nil {
		return nil
	}
	return r.Warnings
}

// AssessHypothesisExhausted records that the orchestrator has assessed a hypothesis-exhausted task.
// If the exhausted task is not already BLOCKED, it transitions the task to BLOCKED
// so the task cannot be reassigned unchanged.
func AssessHypothesisExhausted(projectRoot, taskID, note, agentID string) (*AssessHypothesisExhaustedResult, error) {
	return assessHypothesisExhaustedWithOptionalAuthority(projectRoot, taskID, note, agentID, nil)
}

// AssessHypothesisExhaustedWithAuthority fences the assessment write with the
// orchestrator's registration generation.
func AssessHypothesisExhaustedWithAuthority(projectRoot, taskID, note string, authority models.AgentAuthority) (*AssessHypothesisExhaustedResult, error) {
	return assessHypothesisExhaustedWithOptionalAuthority(projectRoot, taskID, note, authority.ID, &authority)
}

// AssessHypothesisExhaustedWithOptions identifies one assessment occurrence.
func AssessHypothesisExhaustedWithOptions(projectRoot, taskID, note, agentID string, opts LifecycleRequestOptions) (*AssessHypothesisExhaustedResult, error) {
	return assessHypothesisExhaustedWithOptionalAuthority(projectRoot, taskID, note, agentID, nil, opts)
}

// AssessHypothesisExhaustedWithAuthorityAndOptions fences an identified assessment.
func AssessHypothesisExhaustedWithAuthorityAndOptions(projectRoot, taskID, note string, authority models.AgentAuthority, opts LifecycleRequestOptions) (*AssessHypothesisExhaustedResult, error) {
	return assessHypothesisExhaustedWithOptionalAuthority(projectRoot, taskID, note, authority.ID, &authority, opts)
}

const assessHypothesisExhaustedOperation = "assess-hypothesis-exhausted"

// assessHypothesisExhaustedCall carries one assessment across its phases:
// caller validation, the locked mutation and the result it fills.
type assessHypothesisExhaustedCall struct {
	taskID, note, agentID string
	authority             *models.AgentAuthority
	opts                  LifecycleRequestOptions
	now                   time.Time
	transitions           map[models.TaskStatus][]models.TaskStatus

	result   *AssessHypothesisExhaustedResult
	observed *models.Task
	effects  string
}

func assessHypothesisExhaustedWithOptionalAuthority(projectRoot, taskID, note, agentID string, authority *models.AgentAuthority, options ...LifecycleRequestOptions) (returned *AssessHypothesisExhaustedResult, retErr error) {
	invocation := NewLifecycleInvocation(projectRoot)
	call := &assessHypothesisExhaustedCall{taskID: taskID, note: note, agentID: agentID, authority: authority, effects: "none",
		result: &AssessHypothesisExhaustedResult{TaskID: taskID}}
	if len(options) > 0 {
		call.opts = options[0]
	}
	defer func() {
		retErr = WrapLifecycleError(assessHypothesisExhaustedOperation, call.observed, retErr, models.LifecycleInvalidInput, "correct_input", "none")
		var outcome models.LifecycleOutcome
		var warnings *[]string
		if returned != nil {
			outcome = returned.LifecycleOutcome
			warnings = &returned.Warnings
		}
		invocation.FinishResult(assessHypothesisExhaustedOperation, outcome, &retErr, warnings)
	}()
	if err := call.validateCaller(); err != nil {
		return nil, err
	}
	lp := paths.New(projectRoot)
	bb := RequestBlackboard(lp.StatePath(), authority, call.opts)
	call.now = time.Now().UTC()
	resolver, _, err := loadResolver(projectRoot)
	if err != nil {
		return nil, WrapLifecycleError(assessHypothesisExhaustedOperation, nil, fmt.Errorf("failed to load pipeline config: %w", err), models.LifecycleStateChanged, "requery", "none")
	}
	call.transitions = BuildPipelineTransitions(resolver)

	err = lifecycleMutation(bb, authority)(call.mutate)
	if isLifecycleReplay(err) {
		return call.result, nil
	}

	if err != nil {
		return nil, WrapLifecycleError(assessHypothesisExhaustedOperation, call.observed, fmt.Errorf("failed to assess hypothesis-exhausted task: %w", err), models.LifecycleStateChanged, "requery", call.effects)
	}

	return call.result, nil
}

func (c *assessHypothesisExhaustedCall) validateCaller() error {
	if err := ValidateLifecycleRequestOptions(c.opts); err != nil {
		return err
	}
	if c.taskID == "" {
		return &PreconditionError{Reason: "task ID is required"}
	}
	if c.agentID == "" {
		return &PreconditionError{Reason: "agent ID is required"}
	}
	// Defense-in-depth: orchestrator_assessment history entries suppress future wakes,
	// so this must be restricted to orchestrator agents even though the MCP handler
	// also gates via resolveOrchestratorID.
	if err := identity.ValidateRole(c.agentID, roles.Orchestrator); err != nil {
		return WrapLifecycleError(assessHypothesisExhaustedOperation, nil, &PreconditionError{Reason: fmt.Sprintf("only orchestrator agents can assess hypothesis-exhausted tasks: %v", err)}, models.LifecycleForbidden, "stop", "none")
	}
	return nil
}

// mutate is the assessment transaction body: it resolves the request identity,
// blocks the exhausted task when it is not already BLOCKED and records the
// assessment.
func (c *assessHypothesisExhaustedCall) mutate(state *models.State) (callbackErr error) {
	defer func() {
		callbackErr = WrapLifecycleError(assessHypothesisExhaustedOperation, c.observed, callbackErr, models.LifecycleInvalidInput, "correct_input", "none")
	}()
	task := state.FindTask(c.taskID)
	if task == nil {
		return &errors.NotFoundError{Entity: "task", ID: c.taskID}
	}
	copy := *task
	c.observed = &copy
	request, err := NewLifecycleRequest(assessHypothesisExhaustedOperation, task, c.agentID, c.authority, c.opts, c.note)
	if err != nil {
		return err
	}
	receipt, err := CheckLifecycleRequest(task, request, state.Agents)
	if err != nil {
		return err
	}
	if receipt != nil {
		c.result.LifecycleOutcome = LifecycleReplayOutcome(task, receipt, c.agentID)
		return errLifecycleReplay
	}

	if len(task.FailedBy) < 2 {
		return &PreconditionError{Reason: fmt.Sprintf("task must have 2+ entries in failed_by to assess as hypothesis-exhausted, has %d", len(task.FailedBy))}
	}
	if task.Status.IsTerminal() {
		return WrapLifecycleError(assessHypothesisExhaustedOperation, task, &PreconditionError{Reason: fmt.Sprintf("task must not be in terminal status, current status: %s", task.Status)}, models.LifecycleAlreadyTransitioned, "stop", "none")
	}

	if _, err := blockTaskForHypothesisExhaustion(state, task, c.agentID, c.transitions, c.now); err != nil {
		return err
	}

	entry := models.TaskHistoryEntry{
		Time:  c.now,
		Event: models.TaskEventOrchestratorAssessment,
		Agent: &c.agentID,
	}
	if c.note != "" {
		entry.Note = &c.note
	}

	dropSupersededWakeSnapshots(task)
	task.History = append(task.History, entry)
	c.result.LifecycleOutcome, err = CompleteLifecycleRequest(task, request, models.LifecycleProjection{}, state.Agents)
	if err == nil {
		c.effects = "unknown"
	}
	return err
}
