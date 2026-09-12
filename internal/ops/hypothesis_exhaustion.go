package ops

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

const hypothesisExhaustionBlockedReason = "hypothesis_exhaustion: two distinct agents failed this task; rescope, split, or abandon before reassignment"

var hypothesisExhaustionBlockedQuestions = []string{
	"What root cause made two distinct agents fail this task?",
	"Should the orchestrator supersede, split, or abandon this task?",
}

func blockTaskForHypothesisExhaustion(state *models.State, task *models.Task, agentID string, transitions map[models.TaskStatus][]models.TaskStatus, now time.Time) (bool, error) {
	if len(task.FailedBy) < 2 || task.Status == models.TaskStatusBlocked || task.Status.IsTerminal() {
		return false, nil
	}
	if err := task.TransitionWith(models.TaskStatusBlocked, transitions); err != nil {
		return false, fmt.Errorf("transition hypothesis-exhausted task %s from %s to BLOCKED: %w", task.ID, task.Status, err)
	}

	reason := hypothesisExhaustionBlockedReason
	task.BlockedReason = &reason
	task.BlockedQuestions = slices.Clone(hypothesisExhaustionBlockedQuestions)
	releaseAgentsForTask(state, task.ID)
	task.AssignedTo = nil
	task.LeaseExpires = nil
	task.ReviewingBy = nil
	task.ReviewLeaseExpires = nil
	models.AdvanceLifecycle(task)

	note := fmt.Sprintf("failed_by: %s", strings.Join(task.FailedBy, ", "))
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:   now,
		Event:  models.TaskEventBlocked,
		Agent:  &agentID,
		Reason: &reason,
		Note:   &note,
	})

	return true, nil
}
