package ops

import (
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
)

// PersistExit42Restart is the fenced half of an exit-42 restart: it records
// restartCount on the task and, once the count passes restartLimit, blocks a
// task this agent owns in an executing or reviewing state. It reports whether
// the task was blocked. An empty taskID still runs the fenced transaction and
// changes nothing. The in-memory signature and backoff stay with the caller.
func PersistExit42Restart(bb *db.Blackboard, authority models.AgentAuthority, projectRoot, taskID string, restartCount, restartLimit int) (bool, error) {
	blockedTask := false
	err := ModifyWithAgentAuthority(bb, authority, func(s *models.State) error {
		if taskID == "" {
			return nil
		}

		task := s.FindTask(taskID)
		if task == nil {
			return nil
		}

		task.Exit42RestartCount = restartCount

		if restartCount <= restartLimit {
			return nil
		}
		blocked, err := blockExit42RestartLoop(task, projectRoot, authority.ID, restartCount, restartLimit)
		blockedTask = blocked
		return err
	})
	return blockedTask, err
}

// blockExit42RestartLoop blocks a task that restarted past the limit. Only an
// active task owned by agentID is blocked; it reports whether it was. Unlike a
// supervisor block it releases no agent row: the exit already did.
func blockExit42RestartLoop(task *models.Task, projectRoot, agentID string, restartCount, restartLimit int) (bool, error) {
	// Check if task is in an active state owned by this agent.
	pr, prErr := LoadResolverForModels(projectRoot)
	if prErr != nil {
		return false, prErr
	}
	isActive := models.IsExecutingStatus(task, pr) || taskInReviewingStatus(task, pr)
	if !isActive {
		return false, nil
	}
	ownedByAgent := (task.AssignedTo != nil && *task.AssignedTo == agentID) ||
		(task.ReviewingBy != nil && *task.ReviewingBy == agentID)
	if !ownedByAgent {
		return false, nil
	}

	reason := fmt.Sprintf(
		"exit code 42 restart loop detected: %d consecutive restarts without progress (threshold=%d)",
		restartCount,
		restartLimit,
	)
	questions := []string{
		"What task/environment issue is causing repeated exit code 42 without progress?",
		"Should this task be decomposed or the spec clarified before retrying?",
	}

	pipelineTransitions, ptErr := LoadPipelineTransitions(projectRoot)
	if ptErr != nil {
		return false, ptErr
	}
	if err := task.TransitionWith(models.TaskStatusBlocked, pipelineTransitions); err != nil {
		return false, err
	}

	now := time.Now().UTC()
	task.BlockedReason = &reason
	task.BlockedQuestions = questions
	task.AssignedTo = nil
	task.LeaseExpires = nil
	models.AdvanceLifecycle(task)
	task.ReviewingBy = nil
	task.ReviewLeaseExpires = nil
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:   now,
		Event:  models.TaskEventBlocked,
		Agent:  &agentID,
		Reason: &reason,
	})
	return true, nil
}

// taskInReviewingStatus checks if a task is in a reviewing state.
func taskInReviewingStatus(task *models.Task, pr models.PipelineResolver) bool {
	if task.RolePair == "" || pr == nil {
		return false
	}
	reviewing, err := pr.ReviewingStatus(task.RolePair)
	return err == nil && task.Status == reviewing
}
