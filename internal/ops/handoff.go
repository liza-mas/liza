package ops

import (
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/identity"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

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
		return nil, WrapLifecycleError("handoff", nil, fmt.Errorf("handoff input is required"), models.LifecycleInvalidInput, "correct_input", "none")
	}
	invocation := NewLifecycleInvocation(input.ProjectRoot)
	var observed *models.Task
	defer func() {
		retErr = WrapLifecycleError("handoff", observed, retErr, models.LifecycleInvalidInput, "correct_input", "none")
		var outcome models.LifecycleOutcome
		var warnings *[]string
		if result != nil {
			outcome, warnings = result.LifecycleOutcome, &result.Warnings
		}
		invocation.FinishResult("handoff", outcome, &retErr, warnings)
	}()
	if err := ValidateLifecycleRequestOptions(input.Request); err != nil {
		return nil, err
	}
	if input.TaskID == "" {
		return nil, &PreconditionError{Reason: "task ID is required"}
	}
	if input.Summary == "" {
		return nil, &PreconditionError{Reason: "summary is required"}
	}
	if input.NextAction == "" {
		return nil, &PreconditionError{Reason: "next action is required"}
	}
	agentID, err := lifecycleAgentID(input.AgentID, input.Authority)
	if err != nil {
		return nil, err
	}
	if agentID == "" {
		return nil, &PreconditionError{Reason: fmt.Sprintf("%s is required", brand.EnvName("AGENT_ID"))}
	}

	lp := paths.New(input.ProjectRoot)
	bb := db.For(lp.StatePath())
	now := time.Now().UTC()

	runtimeRole, err := identity.ExtractRole(agentID)
	if err != nil {
		return nil, fmt.Errorf("invalid agent ID %s: %w", agentID, err)
	}

	resolver, _, resolverErr := loadResolver(input.ProjectRoot)
	if resolverErr != nil {
		return nil, fmt.Errorf("failed to load pipeline config: %w", resolverErr)
	}
	var pipelineExecuting []models.TaskStatus
	for _, rpName := range resolver.RolePairNames() {
		if es, err := resolver.ExecutingStatus(rpName); err == nil {
			pipelineExecuting = append(pipelineExecuting, es)
		}
	}

	// Build HandoffEvent with backward-compat mapping
	succeeded := input.Succeeded
	if len(succeeded) == 0 {
		succeeded = []string{input.Summary}
	}

	if handoffBeforeModifyTestHook != nil {
		handoffBeforeModifyTestHook()
	}
	var outcome models.LifecycleOutcome
	err = modifyLifecycleState(bb, input.Authority, func(state *models.State) error {
		task := state.FindTask(input.TaskID)
		if task == nil {
			return &errors.NotFoundError{Entity: "task", ID: input.TaskID}
		}
		observed = task
		payload := struct {
			Summary, NextAction, Hypothesis       string
			Succeeded, Failed, KeyFiles, DeadEnds []string
		}{input.Summary, input.NextAction, input.Hypothesis, succeeded, input.Failed, input.KeyFiles, input.DeadEnds}
		request, err := NewLifecycleRequest("handoff", task, agentID, input.Authority, input.Request, payload)
		if err != nil {
			return err
		}
		receipt, err := CheckLifecycleRequest(task, request)
		if err != nil {
			return err
		}
		if receipt != nil {
			outcome = LifecycleReplayOutcome(task, receipt, agentID)
			return errLifecycleReplay
		}

		if !isExecutingStatus(task.Status, pipelineExecuting) {
			return WrapLifecycleError("handoff", task, &PreconditionError{Reason: fmt.Sprintf("task %s is not in an executing status (current status: %s)", input.TaskID, task.Status)}, models.LifecycleAlreadyTransitioned, "stop", "none")
		}

		if task.AssignedTo == nil || *task.AssignedTo != agentID {
			return WrapLifecycleError("handoff", task, &PreconditionError{Reason: fmt.Sprintf("task %s is not assigned to agent %s", input.TaskID, agentID)}, models.LifecycleStaleCaller, "stop", "none")
		}
		if task.HandoffPending {
			return WrapLifecycleError("handoff", task, fmt.Errorf("handoff is already pending"), models.LifecycleAlreadyTransitioned, "stop", "none")
		}

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
			Succeeded:  succeeded,
			Failed:     input.Failed,
			Hypothesis: input.Hypothesis,
			NextStep:   input.NextAction,
			KeyFiles:   input.KeyFiles,
			DeadEnds:   input.DeadEnds,
		})

		agent, exists := state.Agents[agentID]
		if !exists {
			agent = models.Agent{Role: runtimeRole}
		}
		agent.Status = models.AgentStatusHandoff
		agent.CurrentTask = &input.TaskID
		agent.Heartbeat = now
		state.Agents[agentID] = agent

		outcome, err = CompleteLifecycleRequest(task, request, models.LifecycleProjection{})
		return err
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
