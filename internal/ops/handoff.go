package ops

import (
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/identity"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
)

// handoffOperation names the handoff operation.
const handoffOperation = "handoff"

// HandoffResult contains the outcome of a successful handoff initiation.
type HandoffResult struct {
	models.LifecycleOutcome
	Warnings []string `json:"warnings,omitempty"`
	TaskID   string   `json:"task_id"`
	AgentID  string   `json:"agent_id"`
}

// HandoffInput carries all parameters for a handoff operation.
// Summary and NextAction are required legacy fields. The optional structured
// fields override the legacy mapping when provided: if Succeeded is non-empty
// it is used directly; otherwise Summary maps to Succeeded: [summary].
// NextAction always maps to HandoffEvent.NextStep.
type HandoffInput struct {
	Request     LifecycleRequestOptions
	ProjectRoot string
	TaskID      string
	Summary     string // required — legacy field
	NextAction  string // required — maps to NextStep
	AgentID     string
	Authority   *models.AgentAuthority
	Succeeded   []string // optional — overrides Summary→Succeeded mapping
	Failed      []string // optional
	Hypothesis  string   // optional
	KeyFiles    []string // optional
	DeadEnds    []string // optional
}

// Handoff atomically marks a task for context-exhaustion handoff: sets
// handoff_pending, appends a HandoffEvent to the task, and transitions
// the initiating agent to HANDOFF status. No terminal I/O.
func Handoff(input *HandoffInput) (result *HandoffResult, retErr error) {
	if input == nil {
		return nil, WrapLifecycleError(handoffOperation, nil, fmt.Errorf("handoff input is required"), models.LifecycleInvalidInput, LifecycleSafeActionCorrectInput, LifecycleEffectsNone)
	}
	invocation := NewLifecycleInvocation(input.ProjectRoot)
	var observed *models.Task
	defer func() {
		retErr = WrapLifecycleError(handoffOperation, observed, retErr, models.LifecycleInvalidInput, LifecycleSafeActionCorrectInput, LifecycleEffectsNone)
		var outcome models.LifecycleOutcome
		var warnings *[]string
		if result != nil {
			outcome, warnings = result.LifecycleOutcome, &result.Warnings
		}
		invocation.FinishResult(handoffOperation, outcome, &retErr, warnings)
	}()
	if err := ValidateLifecycleRequestOptions(input.Request); err != nil {
		return nil, err
	}
	if input.TaskID == "" {
		return nil, &PreconditionError{Reason: "task ID is required"}
	}
	// Structural validation first: a malformed payload is rejected before the
	// boundary opens the blackboard or reads state.
	if err := rejectInvalidLifecyclePayload(handoffOperation, HandoffPayload(input)); err != nil {
		return nil, err
	}
	agentID, err := lifecycleAgentID(input.AgentID, input.Authority)
	if err != nil {
		return nil, err
	}
	if agentID == "" {
		return nil, &PreconditionError{Reason: fmt.Sprintf("%s is required", brand.EnvName("AGENT_ID"))}
	}

	lp := paths.New(input.ProjectRoot)
	bb := RequestBlackboard(lp.StatePath(), input.Authority, input.Request)
	now := time.Now().UTC()

	runtimeRole, err := identity.ExtractRole(agentID)
	if err != nil {
		return nil, fmt.Errorf("invalid agent ID %s: %w", agentID, err)
	}

	resolver, _, resolverErr := loadResolver(input.ProjectRoot)
	if resolverErr != nil {
		return nil, fmt.Errorf("failed to load pipeline config: %w", resolverErr)
	}
	pipelineExecuting := handoffExecutingStatuses(resolver)

	// Build HandoffEvent with backward-compat mapping
	succeeded := input.Succeeded
	if len(succeeded) == 0 {
		succeeded = []string{input.Summary}
	}
	run := handoffRun{input: input, agentID: agentID, runtimeRole: runtimeRole, succeeded: succeeded, pipelineExecuting: pipelineExecuting, now: now}

	if handoffBeforeModifyTestHook != nil {
		handoffBeforeModifyTestHook()
	}
	var outcome models.LifecycleOutcome
	err = modifyLifecycleState(bb, input.Authority, func(state *models.State) error {
		return handoffTransaction(state, run, &observed, &outcome)
	})
	if err != nil && !isLifecycleReplay(err) {
		return nil, fmt.Errorf("failed to initiate handoff: %w", err)
	}

	return &HandoffResult{
		LifecycleOutcome: outcome,
		TaskID:           input.TaskID,
		AgentID:          agentID,
	}, nil
}

var handoffBeforeModifyTestHook func()

// handoffRun is the validated input of one handoff, resolved before the state lock.
type handoffRun struct {
	input             *HandoffInput
	agentID           string
	runtimeRole       string
	succeeded         []string
	pipelineExecuting []models.TaskStatus
	now               time.Time
}

func handoffExecutingStatuses(resolver *pipeline.Resolver) []models.TaskStatus {
	var executing []models.TaskStatus
	for _, rpName := range resolver.RolePairNames() {
		if es, err := resolver.ExecutingStatus(rpName); err == nil {
			executing = append(executing, es)
		}
	}
	return executing
}

// handoffTransaction is the one atomic callback: receipt check, status,
// assignee and pending checks, history, event, agent state and completion.
func handoffTransaction(state *models.State, run handoffRun, observed **models.Task, outcome *models.LifecycleOutcome) error {
	input, agentID := run.input, run.agentID
	task := state.FindTask(input.TaskID)
	if task == nil {
		return &errors.NotFoundError{Entity: notFoundEntityTask, ID: input.TaskID}
	}
	*observed = task
	payload := struct {
		Summary, NextAction, Hypothesis       string
		Succeeded, Failed, KeyFiles, DeadEnds []string
	}{input.Summary, input.NextAction, input.Hypothesis, run.succeeded, input.Failed, input.KeyFiles, input.DeadEnds}
	request, err := NewLifecycleRequest(handoffOperation, task, agentID, input.Authority, input.Request, payload)
	if err != nil {
		return err
	}
	receipt, err := CheckLifecycleRequest(task, request, state.Agents)
	if err != nil {
		return err
	}
	if receipt != nil {
		*outcome = LifecycleReplayOutcome(task, receipt, agentID)
		return errLifecycleReplay
	}

	if !isExecutingStatus(task.Status, run.pipelineExecuting) {
		return WrapLifecycleError(handoffOperation, task, &PreconditionError{Reason: fmt.Sprintf("task %s is not in an executing status (current status: %s)", input.TaskID, task.Status)}, models.LifecycleAlreadyTransitioned, LifecycleSafeActionStop, LifecycleEffectsNone)
	}

	if task.AssignedTo == nil || *task.AssignedTo != agentID {
		return WrapLifecycleError(handoffOperation, task, &PreconditionError{Reason: fmt.Sprintf("task %s is not assigned to agent %s", input.TaskID, agentID)}, models.LifecycleStaleCaller, LifecycleSafeActionStop, LifecycleEffectsNone)
	}
	if task.HandoffPending {
		return WrapLifecycleError(handoffOperation, task, fmt.Errorf("handoff is already pending"), models.LifecycleAlreadyTransitioned, LifecycleSafeActionStop, LifecycleEffectsNone)
	}

	handoffRecord(state, task, run)

	*outcome, err = CompleteLifecycleRequest(task, request, models.LifecycleProjection{}, state.Agents)
	return err
}

// handoffRecord marks the task pending, appends its history and event, and
// moves the initiating agent to HANDOFF.
func handoffRecord(state *models.State, task *models.Task, run handoffRun) {
	input, agentID, now := run.input, run.agentID, run.now
	task.HandoffPending = true
	note := fmt.Sprintf("summary: %s | next_action: %s", input.Summary, input.NextAction)
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:  now,
		Event: models.TaskEventHandoffInitiated,
		Agent: &agentID,
		Note:  &note,
	})

	task.HandoffEvents = append(task.HandoffEvents, models.HandoffEvent{
		Timestamp:  now,
		Agent:      agentID,
		Trigger:    models.HandoffTriggerContextExhaustion,
		Succeeded:  run.succeeded,
		Failed:     input.Failed,
		Hypothesis: input.Hypothesis,
		NextStep:   input.NextAction,
		KeyFiles:   input.KeyFiles,
		DeadEnds:   input.DeadEnds,
	})

	agent, exists := state.Agents[agentID]
	if !exists {
		agent = models.Agent{Role: run.runtimeRole}
	}
	agent.Status = models.AgentStatusHandoff
	agent.CurrentTask = &input.TaskID
	agent.Heartbeat = now
	state.Agents[agentID] = agent
}

// HandoffPayload builds the canonical object of the handoff operation. The
// preflight command and this mutation boundary validate the same object
// through the same schema. Optional fields are omitted when unset, exactly as
// an agent's own preflight file omits them.
func HandoffPayload(input *HandoffInput) map[string]any {
	payload := map[string]any{"summary": input.Summary, "next_action": input.NextAction}
	if input.Hypothesis != "" {
		payload["hypothesis"] = input.Hypothesis
	}
	for key, entries := range map[string][]string{
		"succeeded": input.Succeeded,
		"failed":    input.Failed,
		"key_files": input.KeyFiles,
		"dead_ends": input.DeadEnds,
	} {
		if len(entries) > 0 {
			payload[key] = entries
		}
	}
	return payload
}
