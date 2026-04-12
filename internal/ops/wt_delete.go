package ops

import (
	"fmt"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// DeleteWorktreeResult contains the outcome of deleting a worktree.
type DeleteWorktreeResult struct {
	TaskID         string            `json:"task_id"`
	PreviousStatus models.TaskStatus `json:"previous_status"`
	Existed        bool              `json:"existed"`
	Warnings       []string          `json:"warnings"`
}

// DeleteWorktree removes a task's git worktree. For SUPERSEDED tasks, the branch
// is preserved (successors may need it); for other statuses, both worktree and
// branch are deleted. Only BLOCKED, ABANDONED, SUPERSEDED, or MERGED tasks are
// eligible. No terminal I/O.
func DeleteWorktree(projectRoot, taskID string) (*DeleteWorktreeResult, error) {
	if taskID == "" {
		return nil, &PreconditionError{Reason: "task ID is required"}
	}

	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())
	_, task, err := readTaskState(bb, taskID)
	if err != nil {
		return nil, err
	}

	var warnings []string

	switch task.Status {
	case models.TaskStatusBlocked, models.TaskStatusAbandoned, models.TaskStatusSuperseded:
		// Safe to delete
	case models.TaskStatusMerged:
		warnings = append(warnings, fmt.Sprintf("Task %s is MERGED — worktree should already be cleaned", taskID))
	default:
		// Check if status is a pipeline-defined clean terminal state
		if !isCleanTerminalState(task, projectRoot) {
			return nil, &PreconditionError{Reason: fmt.Sprintf("cannot delete worktree for task %s (status: %s), deletion only allowed for: BLOCKED, ABANDONED, SUPERSEDED, MERGED, or pipeline clean states", taskID, task.Status)}
		}
	}

	if task.Worktree == nil {
		return &DeleteWorktreeResult{
			TaskID:         taskID,
			PreviousStatus: task.Status,
			Existed:        false,
		}, nil
	}

	gitWrapper := git.New(projectRoot)

	if task.Status == models.TaskStatusSuperseded {
		// Superseded tasks preserve their branch — successors may need it
		// via git show. Branch cleanup happens when all successors are terminal.
		if err := gitWrapper.RemoveWorktreeDir(taskID); err != nil {
			warnings = append(warnings, fmt.Sprintf("failed to remove worktree directory: %v", err))
		}
	} else {
		if err := gitWrapper.RemoveWorktree(taskID); err != nil {
			warnings = append(warnings, fmt.Sprintf("failed to remove worktree: %v", err))
		}
	}

	err = bb.Modify(func(state *models.State) error {
		task := state.FindTask(taskID)
		if task == nil {
			return &errors.NotFoundError{Entity: "task", ID: taskID}
		}
		task.Worktree = nil
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to update state: %w", err)
	}

	return &DeleteWorktreeResult{
		TaskID:         taskID,
		PreviousStatus: task.Status,
		Existed:        true,
		Warnings:       warnings,
	}, nil
}

// isCleanTerminalState checks whether a task is at a pipeline-defined clean
// terminal state (e.g., INTEGRATION_ANALYSIS_CLEAN).
func isCleanTerminalState(task *models.Task, projectRoot string) bool {
	if task.RolePair == "" {
		return false
	}
	resolver, _, err := loadResolver(projectRoot)
	if err != nil {
		return false
	}
	cleanStatus, err := resolver.CleanStatus(task.RolePair)
	return err == nil && task.Status == cleanStatus
}
