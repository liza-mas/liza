package ops

import (
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
)

// FinalizeCleanTerminalTask mirrors the non-Git part of the merge path's
// cleanup for a task at a pipeline-defined clean terminal state: it releases
// the assigned agent only when that agent's current task is this one, and
// appends the completion handoff event. The Git deletion of the worktree
// precedes this call. A task that no longer exists is a no-op.
//
// The event timestamp is local time, as every other handoff event is.
func FinalizeCleanTerminalTask(bb *db.Blackboard, authority models.AgentAuthority, taskID string) error {
	return ModifyWithAgentAuthority(bb, authority, func(s *models.State) error {
		t := s.FindTask(taskID)
		if t == nil {
			return nil
		}
		if t.AssignedTo != nil {
			if a, ok := s.Agents[*t.AssignedTo]; ok {
				if a.CurrentTask != nil && *a.CurrentTask == taskID {
					s.ReleaseAgent(*t.AssignedTo)
				}
			}
		}
		t.HandoffEvents = append(t.HandoffEvents, models.HandoffEvent{
			Timestamp: time.Now(),
			Agent:     actorSystem,
			Trigger:   models.HandoffTriggerCompletion,
		})
		return nil
	})
}
