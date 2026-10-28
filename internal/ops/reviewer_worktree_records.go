package ops

import (
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
)

// RecordWorktreeRecovered appends the worktree_recovered event to the task's
// history inside the generation-fenced transaction. A task that no longer
// exists is a no-op. The filesystem recovery has already happened; the caller
// owns the warning when this write fails.
func RecordWorktreeRecovered(bb *db.Blackboard, authority models.AgentAuthority, taskID string) error {
	agentID := authority.ID
	return ModifyWithAgentAuthority(bb, authority, func(s *models.State) error {
		t := s.FindTask(taskID)
		if t != nil {
			t.History = append(t.History, models.TaskHistoryEntry{
				Time:  time.Now().UTC(),
				Event: models.TaskEventWorktreeRecovered,
				Agent: &agentID,
			})
		}
		return nil
	})
}

// ForceBlockReviewerTask forces a task into BLOCKED status, bypassing normal
// transition validation. This handles the exceptional case where a reviewer
// task's worktree is unrecoverable and no valid transition path to BLOCKED
// exists. A task that no longer exists is a no-op.
//
// Reserved for lost work. A task blocked here is restored by unblock-task to
// the doer's executing status, which discards review readiness: acceptable
// when the worktree is gone, not when it merely failed to prepare.
func ForceBlockReviewerTask(bb *db.Blackboard, authority models.AgentAuthority, taskID, reason string) error {
	agentID := authority.ID
	return ModifyWithAgentAuthority(bb, authority, func(state *models.State) error {
		t := state.FindTask(taskID)
		if t == nil {
			return nil
		}

		// Force status to BLOCKED: no valid transition exists from REVIEWING.
		t.Status = models.TaskStatusBlocked
		t.BlockedReason = &reason
		t.BlockedQuestions = []string{
			"Is the task branch recoverable from a remote or backup?",
			"Should this task be recreated with a fresh worktree?",
		}

		// Release the reviewer agent state before clearing claim fields.
		if t.ReviewingBy != nil {
			state.ReleaseAgent(*t.ReviewingBy)
		}

		// Clear reviewer claim.
		t.ReviewingBy = nil
		t.ReviewLeaseExpires = nil
		models.AdvanceLifecycle(t)

		t.History = append(t.History, models.TaskHistoryEntry{
			Time:   time.Now().UTC(),
			Event:  models.TaskEventBlocked,
			Agent:  &agentID,
			Reason: &reason,
		})
		return nil
	})
}
