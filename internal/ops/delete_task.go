package ops

import (
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// DeleteTaskInfo contains pre-check information about a task deletion.
// Used by CLI to make interactive decisions before calling DeleteTask.
type DeleteTaskInfo struct {
	TaskID         string
	Status         models.TaskStatus
	HasWorktree    bool
	WorktreePath   string
	DependentTasks []string
}

// validateDeletePreconditions checks whether a task can be deleted based on
// its status and active claims. Does not check dependencies (callers handle
// those differently). Returns nil if deletion is allowed.
func validateDeletePreconditions(task *models.Task, taskID string) error {
	if task.Status == models.TaskStatusMerged {
		return fmt.Errorf("cannot delete MERGED task %s (use --force if you really want to)", taskID)
	}
	if task.Status == models.TaskStatusImplementing && task.LeaseExpires != nil && task.LeaseExpires.After(time.Now().UTC()) {
		return fmt.Errorf("cannot delete task %s in status %s (actively being worked on)", taskID, task.Status)
	}
	if task.Status.IsReadyForReviewStatus() || task.Status == models.TaskStatusReviewing {
		return fmt.Errorf("cannot delete task %s in status %s (under review)", taskID, task.Status)
	}
	return nil
}

// findDependentTasks returns IDs of tasks that depend on the given taskID.
func findDependentTasks(tasks []models.Task, taskID string) []string {
	var deps []string
	for _, t := range tasks {
		for _, depID := range t.DependsOn {
			if depID == taskID {
				deps = append(deps, t.ID)
			}
		}
	}
	return deps
}

// findParentReferencingTasks returns IDs of tasks that reference the given
// taskID via parent_task or parent_tasks.
func findParentReferencingTasks(tasks []models.Task, taskID string) []string {
	var refs []string
	for _, t := range tasks {
		for _, parentID := range t.EffectiveParentTasks() {
			if parentID == taskID {
				refs = append(refs, t.ID)
				break
			}
		}
	}
	return refs
}

// CheckDeleteTask validates that a task can be deleted and returns info needed
// for interactive decisions (e.g., APPROVED confirmation, worktree deletion prompt).
// Returns an error for hard blocks (MERGED without force, active lease, under review,
// dependencies without force). No terminal I/O.
func CheckDeleteTask(projectRoot, taskID string, force bool) (*DeleteTaskInfo, error) {
	if taskID == "" {
		return nil, fmt.Errorf("task ID required")
	}

	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())

	state, task, err := readTaskState(bb, taskID)
	if err != nil {
		return nil, err
	}

	if !force {
		if err := validateDeletePreconditions(task, taskID); err != nil {
			return nil, err
		}
	}

	dependentTasks := findDependentTasks(state.Tasks, taskID)
	if len(dependentTasks) > 0 && !force {
		return nil, fmt.Errorf("cannot delete task %s: tasks %v depend on it (use --force to override)", taskID, dependentTasks)
	}
	parentReferencingTasks := findParentReferencingTasks(state.Tasks, taskID)
	if len(parentReferencingTasks) > 0 && !force {
		return nil, fmt.Errorf("cannot delete task %s: task %s references it via parent_task (use --force to override)", taskID, parentReferencingTasks[0])
	}

	info := &DeleteTaskInfo{
		TaskID:         taskID,
		Status:         task.Status,
		DependentTasks: dependentTasks,
	}

	if task.Worktree != nil {
		info.HasWorktree = true
		gitWrapper := git.New(lp.ProjectRoot())
		info.WorktreePath = gitWrapper.GetWorktreePath(taskID)
	}

	return info, nil
}

// DeleteTaskResult contains the outcome of a successful task deletion.
type DeleteTaskResult struct {
	TaskID            string
	PreviousStatus    models.TaskStatus
	WorktreeDeleted   bool
	WorktreePreserved bool
	WorktreePath      string
	Warnings          []string // non-fatal warnings (e.g., worktree removal failure)
}

// DeleteTask removes a task from the state database and optionally its worktree.
// Enforces all preconditions independently — callers need not call CheckDeleteTask
// first (though CLI does for interactive prompt info). No terminal I/O.
func DeleteTask(projectRoot, taskID string, force, deleteWorktree bool, reason string) (*DeleteTaskResult, error) {
	if taskID == "" {
		return nil, fmt.Errorf("task ID required")
	}

	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())

	state, task, err := readTaskState(bb, taskID)
	if err != nil {
		return nil, err
	}

	// Pre-side-effect validation (before worktree deletion to avoid
	// orphaning worktree on validation failure)
	if !force {
		if err := checkDeleteBlockers(state, task, taskID); err != nil {
			return nil, err
		}
	}

	// Build result early; destructive side effects happen only after commit succeeds.
	result := &DeleteTaskResult{
		TaskID:         taskID,
		PreviousStatus: task.Status,
	}
	taskHasWorktree := task.Worktree != nil

	err = bb.Modify(func(state *models.State) error {
		return removeTaskInState(state, taskID, force, reason)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to delete task: %w", err)
	}

	// Handle worktree cleanup after successful state mutation to avoid
	// state/filesystem drift when commit fails.
	if taskHasWorktree {
		cleanupDeletedTaskWorktree(lp.ProjectRoot(), taskID, deleteWorktree, result)
	}

	return result, nil
}
