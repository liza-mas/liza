package ops

import (
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/identity"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/roles"
)

// AssessHypothesisExhaustedResult contains the outcome of recording an orchestrator assessment.
type AssessHypothesisExhaustedResult struct {
	TaskID string `json:"task_id"`
}

// AssessHypothesisExhausted records that the orchestrator has assessed a hypothesis-exhausted task.
// Appends an orchestrator_assessment history entry without changing task status.
// This prevents the wake-detection loop where the orchestrator repeatedly wakes
// for hypothesis-exhausted tasks it has already triaged.
func AssessHypothesisExhausted(projectRoot, taskID, note, agentID string) (*AssessHypothesisExhaustedResult, error) {
	if taskID == "" {
		return nil, &PreconditionError{Reason: "task ID is required"}
	}
	if agentID == "" {
		return nil, &PreconditionError{Reason: "agent ID is required"}
	}
	// Defense-in-depth: orchestrator_assessment history entries suppress future wakes,
	// so this must be restricted to orchestrator agents even though the MCP handler
	// also gates via resolveOrchestratorID.
	if err := identity.ValidateRole(agentID, roles.Orchestrator); err != nil {
		return nil, &PreconditionError{Reason: fmt.Sprintf("only orchestrator agents can assess hypothesis-exhausted tasks: %v", err)}
	}
	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())
	now := time.Now().UTC()

	err := bb.Modify(func(state *models.State) error {
		task := state.FindTask(taskID)
		if task == nil {
			return &errors.NotFoundError{Entity: "task", ID: taskID}
		}

		if len(task.FailedBy) < 2 {
			return &PreconditionError{Reason: fmt.Sprintf("task must have 2+ entries in failed_by to assess as hypothesis-exhausted, has %d", len(task.FailedBy))}
		}
		if task.Status.IsTerminal() {
			return &PreconditionError{Reason: fmt.Sprintf("task must not be in terminal status, current status: %s", task.Status)}
		}

		entry := models.TaskHistoryEntry{
			Time:  now,
			Event: models.TaskEventOrchestratorAssessment,
			Agent: &agentID,
		}
		if note != "" {
			entry.Note = &note
		}

		task.History = append(task.History, entry)
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to assess hypothesis-exhausted task: %w", err)
	}

	return &AssessHypothesisExhaustedResult{TaskID: taskID}, nil
}
