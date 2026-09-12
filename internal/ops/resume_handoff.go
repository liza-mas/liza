package ops

import (
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/db"
	lizaerrors "github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// ResumeHandoffInput contains the parameters for resuming a handoff task.
type ResumeHandoffInput struct {
	ProjectRoot string
	AgentID     string
	Authority   *models.AgentAuthority
	Session     *ValidationSession
}

// ResumeHandoffResult contains the outcome of a successful handoff resumption.
type ResumeHandoffResult struct {
	TaskID   string
	Worktree string
	Found    bool
}

// ResumeHandoff looks for a handoff task assigned to agentID and resumes it.
// Returns Found=false when no resumable handoff exists.
func ResumeHandoff(input ResumeHandoffInput) (*ResumeHandoffResult, error) {
	if input.AgentID == "" {
		return nil, &PreconditionError{Reason: "agent ID is required"}
	}
	if input.Authority != nil {
		if err := requireAuthorityActor(*input.Authority, input.AgentID); err != nil {
			return nil, err
		}
	}

	lp := paths.New(input.ProjectRoot)
	bb := db.For(lp.StatePath())

	// Collect pipeline executing statuses
	resolver, _, err := loadResolver(input.ProjectRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load pipeline config: %w", err)
	}
	var executingStatuses []models.TaskStatus
	for _, rpName := range resolver.RolePairNames() {
		if es, err := resolver.ExecutingStatus(rpName); err == nil {
			executingStatuses = append(executingStatuses, es)
		}
	}

	state, err := bb.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read state: %w", err)
	}

	return resumeHandoffWithState(bb, state, input.AgentID, input.Authority, executingStatuses, input)
}

// resumeHandoffWithState performs the handoff resumption with an already-read state.
// This allows for efficient checking without re-reading state.
func resumeHandoffWithState(bb *db.Blackboard, state *models.State, agentID string, authority *models.AgentAuthority, executingStatuses []models.TaskStatus, inputs ...ResumeHandoffInput) (*ResumeHandoffResult, error) {
	now := time.Now().UTC()

	for i := range state.Tasks {
		task := &state.Tasks[i]
		if !isResumableHandoff(task, agentID, executingStatuses) {
			continue
		}
		if task.Worktree == nil {
			return nil, &PreconditionError{Reason: fmt.Sprintf("handoff task %s missing worktree", task.ID)}
		}

		id := task.ID
		wt := *task.Worktree
		var preflight *ValidationPreflight
		if len(inputs) > 0 {
			var err error
			preflight, err = prepareResumedValidation(inputs[0].ProjectRoot, id, agentID, wt, inputs[0].Session, authority)
			if err != nil {
				if releaseErr := ReleaseValidationOwnership(inputs[0].ProjectRoot, id, agentID, authority); releaseErr != nil {
					return nil, releaseErr
				}
				return nil, err
			}
		} else if len(task.ValidationPrerequisites) > 0 {
			return nil, validationError("current_session_required")
		}

		err := lifecycleMutation(bb, authority)(func(s *models.State) error {
			if err := preflight.CheckCurrent(s); err != nil {
				return err
			}
			t := s.FindTask(id)
			if t == nil {
				return &lizaerrors.NotFoundError{Entity: "task", ID: id}
			}
			if len(t.ValidationPrerequisites) > 0 && preflight == nil {
				return validationError("context_changed")
			}
			if !isExecutingStatus(t.Status, executingStatuses) {
				return &PreconditionError{Reason: fmt.Sprintf("task %s is no longer in an executing state (current: %s)", id, t.Status)}
			}
			if t.AssignedTo == nil || *t.AssignedTo != agentID {
				return &PreconditionError{Reason: fmt.Sprintf("task %s is no longer assigned to %s", id, agentID)}
			}

			renewLease(s, t)

			t.HandoffPending = false
			t.History = append(t.History, models.TaskHistoryEntry{
				Time:  now,
				Event: models.TaskEventHandoffResumed,
				Agent: &agentID,
			})

			agent, ok := s.Agents[agentID]
			if !ok {
				agent = models.Agent{Role: "coder"}
			}
			agent.Status = models.AgentStatusWorking
			agent.CurrentTask = &id
			agent.LeaseExpires = t.LeaseExpires
			agent.Heartbeat = now
			s.Agents[agentID] = agent
			return nil
		})
		if err != nil {
			if IsAgentAuthorityError(err) || preflight != nil {
				return nil, err
			}
			// Conflict on this candidate, try next
			continue
		}

		return &ResumeHandoffResult{
			TaskID:   id,
			Worktree: wt,
			Found:    true,
		}, nil
	}

	return &ResumeHandoffResult{Found: false}, nil
}

// isResumableHandoff checks if the task is a handoff that can be resumed by the given agent.
func isResumableHandoff(task *models.Task, agentID string, executingStatuses []models.TaskStatus) bool {
	if !isExecutingStatus(task.Status, executingStatuses) {
		return false
	}
	if task.AssignedTo == nil || *task.AssignedTo != agentID {
		return false
	}
	if !task.HandoffPending {
		return false
	}
	return true
}

// isExecutingStatus returns true if the status is a pipeline-defined executing state.
func isExecutingStatus(status models.TaskStatus, pipelineExecuting []models.TaskStatus) bool {
	for _, es := range pipelineExecuting {
		if status == es {
			return true
		}
	}
	return false
}
