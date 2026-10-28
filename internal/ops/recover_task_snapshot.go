package ops

import (
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/roles"
)

// recoverTaskSnapshot is the claim and worktree evidence captured before any
// recovery effect; every transaction compares it with the locked task.
type recoverTaskSnapshot struct {
	status                models.TaskStatus
	assignedTo            string
	hasAssignedTo         bool
	leaseExpires          time.Time
	hasLeaseExpires       bool
	reviewingBy           string
	hasReviewingBy        bool
	reviewLeaseExpires    time.Time
	hasReviewLeaseExpires bool
	worktree              string
	hasWorktree           bool
	baseCommit            string
	hasBaseCommit         bool
	reviewCommit          string
	hasReviewCommit       bool
}

func newRecoverTaskSnapshot(task *models.Task) recoverTaskSnapshot {
	snapshot := recoverTaskSnapshot{status: task.Status}
	snapshot.assignedTo, snapshot.hasAssignedTo = optionalString(task.AssignedTo)
	snapshot.leaseExpires, snapshot.hasLeaseExpires = optionalTime(task.LeaseExpires)
	snapshot.reviewingBy, snapshot.hasReviewingBy = optionalString(task.ReviewingBy)
	snapshot.reviewLeaseExpires, snapshot.hasReviewLeaseExpires = optionalTime(task.ReviewLeaseExpires)
	snapshot.worktree, snapshot.hasWorktree = optionalString(task.Worktree)
	snapshot.baseCommit, snapshot.hasBaseCommit = optionalString(task.BaseCommit)
	snapshot.reviewCommit, snapshot.hasReviewCommit = optionalString(task.ReviewCommit)
	return snapshot
}

func optionalString(value *string) (string, bool) {
	if value == nil {
		return "", false
	}
	return *value, true
}

func optionalTime(value *time.Time) (time.Time, bool) {
	if value == nil {
		return time.Time{}, false
	}
	return value.UTC(), true
}

func (s recoverTaskSnapshot) validate(taskID string, task *models.Task) error {
	if task == nil {
		return fmt.Errorf("task %s disappeared during recover-task", taskID)
	}
	if task.Status != s.status {
		return fmt.Errorf("race condition: task %s status changed from %s to %s during recover-task", taskID, s.status, task.Status)
	}
	if err := validateSnapshotString(taskID, "assigned_to", s.assignedTo, s.hasAssignedTo, task.AssignedTo); err != nil {
		return err
	}
	if err := validateSnapshotTime(taskID, "lease_expires", s.leaseExpires, s.hasLeaseExpires, task.LeaseExpires); err != nil {
		return err
	}
	if err := validateSnapshotString(taskID, "reviewing_by", s.reviewingBy, s.hasReviewingBy, task.ReviewingBy); err != nil {
		return err
	}
	if err := validateSnapshotTime(taskID, "review_lease_expires", s.reviewLeaseExpires, s.hasReviewLeaseExpires, task.ReviewLeaseExpires); err != nil {
		return err
	}
	if err := validateSnapshotString(taskID, "worktree", s.worktree, s.hasWorktree, task.Worktree); err != nil {
		return err
	}
	if err := validateSnapshotString(taskID, "base_commit", s.baseCommit, s.hasBaseCommit, task.BaseCommit); err != nil {
		return err
	}
	if err := validateSnapshotString(taskID, "review_commit", s.reviewCommit, s.hasReviewCommit, task.ReviewCommit); err != nil {
		return err
	}
	return nil
}

func validateSnapshotString(taskID, field, expected string, expectedSet bool, current *string) error {
	currentValue, currentSet := optionalString(current)
	if expectedSet == currentSet && expected == currentValue {
		return nil
	}
	return fmt.Errorf("race condition: task %s %s changed during recover-task", taskID, field)
}

func validateSnapshotTime(taskID, field string, expected time.Time, expectedSet bool, current *time.Time) error {
	currentValue, currentSet := optionalTime(current)
	if expectedSet == currentSet && (!expectedSet || expected.Equal(currentValue)) {
		return nil
	}
	return fmt.Errorf("race condition: task %s %s changed during recover-task", taskID, field)
}

func (s recoverTaskSnapshot) agentsToRecover() map[string]bool {
	agents := map[string]bool{}
	if s.hasAssignedTo {
		agents[s.assignedTo] = true
	}
	if s.hasReviewingBy {
		agents[s.reviewingBy] = true
	}
	return agents
}

func recoverTaskCheckLiveClaims(taskID string, task *models.Task, state *models.State, force bool, fresh bool, result *RecoverTaskResult) error {
	var coderAgentID string
	var reviewerAgentID string
	forceHint := "--force"
	if fresh {
		forceHint = "--fresh --force"
	}
	if task.AssignedTo != nil {
		coderAgentID = *task.AssignedTo
		if agent, exists := state.Agents[coderAgentID]; exists {
			if !force && agent.PID != 0 && IsProcessAlive(agent.PID) {
				return fmt.Errorf("task %s: coder agent %s (PID %d) still running, use %s to recover",
					taskID, coderAgentID, agent.PID, forceHint)
			}
		}
	}
	if task.ReviewingBy != nil {
		reviewerAgentID = *task.ReviewingBy
		if agent, exists := state.Agents[reviewerAgentID]; exists {
			if !force && agent.PID != 0 && IsProcessAlive(agent.PID) {
				return fmt.Errorf("task %s: reviewer agent %s (PID %d) still running, use %s to recover",
					taskID, reviewerAgentID, agent.PID, forceHint)
			}
		}
	}
	if coderAgentID != "" {
		result.AgentID = coderAgentID
		result.AgentRole = roles.Coder
	} else if reviewerAgentID != "" {
		result.AgentID = reviewerAgentID
		result.AgentRole = roles.CodeReviewer
	}
	return nil
}

func recoverTaskProjection(task *models.Task, snapshot recoverTaskSnapshot) models.LifecycleProjection {
	projection := models.LifecycleProjection{SourceStatus: snapshot.status, ReleasedDoer: snapshot.hasAssignedTo && task.AssignedTo == nil, ReleasedReviewer: snapshot.hasReviewingBy && task.ReviewingBy == nil}
	if task.BaseCommit != nil {
		projection.BaseCommit = *task.BaseCommit
	}
	return projection
}
