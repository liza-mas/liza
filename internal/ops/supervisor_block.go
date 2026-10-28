package ops

import (
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/roles"
)

// SupervisorBlockOwner names the ownership a supervisor block expects the task
// to still be in when it commits.
type SupervisorBlockOwner int

const (
	// SupervisorBlockOwnerDoer expects the agent to hold the doer slot.
	SupervisorBlockOwnerDoer SupervisorBlockOwner = iota + 1
	// SupervisorBlockOwnerReviewer expects the agent to hold the reviewer slot.
	SupervisorBlockOwnerReviewer
	// SupervisorBlockOwnerReleased expects an unowned continuation: the task
	// whose claim the agent's exit just released, with no doer and no reviewer.
	SupervisorBlockOwnerReleased
)

// SupervisorBlockExpectation is the owner slot and lifecycle token the caller
// observed before deciding to block. The block applies only while the locked
// task still shows both, so a stale observation never becomes a new block.
type SupervisorBlockExpectation struct {
	Owner        SupervisorBlockOwner
	TransitionID string
}

// SupervisorBlockOutcome reports what a supervisor block did.
type SupervisorBlockOutcome int

const (
	// SupervisorBlockApplied: the task moved to BLOCKED.
	SupervisorBlockApplied SupervisorBlockOutcome = iota + 1
	// SupervisorBlockTaskMissing: the task no longer exists; nothing changed.
	SupervisorBlockTaskMissing
	// SupervisorBlockStale: the task's owner or lifecycle moved before the
	// block; it is left to its current owner and nothing changed.
	SupervisorBlockStale
)

// SupervisorTaskSlot names the ownership agentID holds on task: ClaimDoer
// (assigned_to), ClaimReviewer (reviewing_by), or "" for none.
func SupervisorTaskSlot(task *models.Task, agentID string) roles.ClaimType {
	switch {
	case task.AssignedTo != nil && *task.AssignedTo == agentID:
		return roles.ClaimDoer
	case task.ReviewingBy != nil && *task.ReviewingBy == agentID:
		return roles.ClaimReviewer
	}
	return ""
}

func (e SupervisorBlockExpectation) holds(task *models.Task, agentID string) bool {
	if models.TaskTransitionID(task) != e.TransitionID {
		return false
	}
	switch e.Owner {
	case SupervisorBlockOwnerDoer:
		return SupervisorTaskSlot(task, agentID) == roles.ClaimDoer
	case SupervisorBlockOwnerReviewer:
		return SupervisorTaskSlot(task, agentID) == roles.ClaimReviewer
	case SupervisorBlockOwnerReleased:
		return task.AssignedTo == nil && task.ReviewingBy == nil
	}
	return false
}

// BlockSupervisorTask moves the task to BLOCKED with reason when the
// expectation still holds inside the generation-fenced transaction, releasing
// its owners and their agent rows' claims. Any WIP save happens before the
// call, outside the state lock.
func BlockSupervisorTask(bb *db.Blackboard, pipelineTransitions map[models.TaskStatus][]models.TaskStatus, authority models.AgentAuthority, taskID, reason string, expected SupervisorBlockExpectation) (SupervisorBlockOutcome, error) {
	agentID := authority.ID
	outcome := SupervisorBlockTaskMissing
	err := ModifyWithAgentAuthority(bb, authority, func(s *models.State) error {
		outcome = SupervisorBlockTaskMissing
		task := s.FindTask(taskID)
		if task == nil {
			return nil
		}
		if !expected.holds(task, agentID) {
			outcome = SupervisorBlockStale
			return nil
		}
		if err := task.TransitionWith(models.TaskStatusBlocked, pipelineTransitions); err != nil {
			return err
		}
		now := time.Now().UTC()
		task.BlockedReason = &reason
		task.BlockedQuestions = []string{
			"Is the task spec unclear or incomplete?",
			"Is there an environment or tooling issue preventing progress?",
		}
		task.AssignedTo = nil
		task.LeaseExpires = nil
		models.AdvanceLifecycle(task)
		task.ReviewingBy = nil
		task.ReviewLeaseExpires = nil
		ReleaseAgentsForTask(s, taskID)
		task.History = append(task.History, models.TaskHistoryEntry{
			Time:   now,
			Event:  models.TaskEventBlocked,
			Agent:  &agentID,
			Reason: &reason,
		})
		outcome = SupervisorBlockApplied
		return nil
	})
	if err != nil {
		return 0, err
	}
	return outcome, nil
}
