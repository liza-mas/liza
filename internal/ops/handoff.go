package ops

import (
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/identity"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// HandoffResult contains the outcome of a successful handoff initiation.
type HandoffResult struct {
	TaskID  string `json:"task_id"`
	AgentID string `json:"agent_id"`
}

// HandoffInput carries all parameters for a handoff operation.
// Summary and NextAction are required legacy fields. The optional structured
// fields override the legacy mapping when provided: if Succeeded is non-empty
// it is used directly; otherwise Summary maps to Succeeded: [summary].
// NextAction always maps to HandoffEvent.NextStep.
type HandoffInput struct {
	ProjectRoot string
	TaskID      string
	Summary     string // required — legacy field
	NextAction  string // required — maps to NextStep
	AgentID     string
	Succeeded   []string // optional — overrides Summary→Succeeded mapping
	Failed      []string // optional
	Hypothesis  string   // optional
	KeyFiles    []string // optional
	DeadEnds    []string // optional
}

// Handoff atomically marks a task for context-exhaustion handoff: sets
// handoff_pending, appends a HandoffEvent to the task, and transitions
// the initiating agent to HANDOFF status. No terminal I/O.
func Handoff(input *HandoffInput) (*HandoffResult, error) {
	if input.TaskID == "" {
		return nil, &PreconditionError{Reason: "task ID is required"}
	}
	if input.Summary == "" {
		return nil, &PreconditionError{Reason: "summary is required"}
	}
	if input.NextAction == "" {
		return nil, &PreconditionError{Reason: "next action is required"}
	}
	if input.AgentID == "" {
		return nil, &PreconditionError{Reason: "LIZA_AGENT_ID is required"}
	}

	lp := paths.New(input.ProjectRoot)
	bb := db.For(lp.StatePath())
	now := time.Now().UTC()

	runtimeRole, err := identity.ExtractRole(input.AgentID)
	if err != nil {
		return nil, fmt.Errorf("invalid agent ID %s: %w", input.AgentID, err)
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

	err = bb.Modify(func(state *models.State) error {
		task := state.FindTask(input.TaskID)
		if task == nil {
			return &errors.NotFoundError{Entity: "task", ID: input.TaskID}
		}

		if !isExecutingStatus(task.Status, pipelineExecuting) {
			return &PreconditionError{Reason: fmt.Sprintf("task %s is not in an executing status (current status: %s)", input.TaskID, task.Status)}
		}

		if task.AssignedTo == nil || *task.AssignedTo != input.AgentID {
			return &PreconditionError{Reason: fmt.Sprintf("task %s is not assigned to agent %s", input.TaskID, input.AgentID)}
		}

		task.HandoffPending = true
		note := fmt.Sprintf("summary: %s | next_action: %s", input.Summary, input.NextAction)
		task.History = append(task.History, models.TaskHistoryEntry{
			Time:  now,
			Event: models.TaskEventHandoffInitiated,
			Agent: &input.AgentID,
			Note:  &note,
		})

		task.HandoffEvents = append(task.HandoffEvents, models.HandoffEvent{
			Timestamp:  now,
			Agent:      input.AgentID,
			Trigger:    models.HandoffTriggerContextExhaustion,
			Succeeded:  succeeded,
			Failed:     input.Failed,
			Hypothesis: input.Hypothesis,
			NextStep:   input.NextAction,
			KeyFiles:   input.KeyFiles,
			DeadEnds:   input.DeadEnds,
		})

		agent, exists := state.Agents[input.AgentID]
		if !exists {
			agent = models.Agent{Role: runtimeRole}
		}
		agent.Status = models.AgentStatusHandoff
		agent.CurrentTask = &input.TaskID
		agent.Heartbeat = now
		state.Agents[input.AgentID] = agent

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initiate handoff: %w", err)
	}

	return &HandoffResult{
		TaskID:  input.TaskID,
		AgentID: input.AgentID,
	}, nil
}
