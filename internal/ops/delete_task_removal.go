package ops

import (
	"fmt"
	"slices"
	"time"

	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// checkDeleteBlockers returns the first refusal that stops an unforced
// deletion: status preconditions, then the first dependent, then the first
// parent reference. DeleteTask runs it before the transaction and again inside
// it, so both points refuse with the same text.
func checkDeleteBlockers(state *models.State, task *models.Task, taskID string) error {
	if err := validateDeletePreconditions(task, taskID); err != nil {
		return err
	}
	if deps := findDependentTasks(state.Tasks, taskID); len(deps) > 0 {
		return fmt.Errorf("cannot delete task %s: task %s depends on it (use --force to override)", taskID, deps[0])
	}
	if parentRefs := findParentReferencingTasks(state.Tasks, taskID); len(parentRefs) > 0 {
		return fmt.Errorf("cannot delete task %s: task %s references it via parent_task (use --force to override)", taskID, parentRefs[0])
	}
	return nil
}

// removeTaskInState is the in-lock half of DeleteTask: it rechecks the
// blockers against the locked state (race prevention), releases the assigned
// agent, removes the task and the records that name it, and appends the audit
// note.
func removeTaskInState(state *models.State, taskID string, force bool, reason string) error {
	taskIndex := state.FindTaskIndex(taskID)
	if taskIndex == -1 {
		return &errors.NotFoundError{Entity: "task", ID: taskID}
	}

	currentTask := &state.Tasks[taskIndex]
	if !force {
		if err := checkDeleteBlockers(state, currentTask, taskID); err != nil {
			return err
		}
	}

	if currentTask.AssignedTo != nil {
		if agent, ok := state.Agents[*currentTask.AssignedTo]; ok {
			if agent.CurrentTask != nil && *agent.CurrentTask == taskID {
				state.ReleaseAgent(*currentTask.AssignedTo)
			}
		}
	}

	state.Tasks = append(state.Tasks[:taskIndex], state.Tasks[taskIndex+1:]...)
	state.Sprint.Scope.Planned = slices.DeleteFunc(state.Sprint.Scope.Planned, func(id string) bool { return id == taskID })
	state.Sprint.Scope.Stretch = slices.DeleteFunc(state.Sprint.Scope.Stretch, func(id string) bool { return id == taskID })

	// Explicit task deletion also removes its evidence and reconciliation
	// audit; orphaned findings would invalidate all remaining task operations.
	state.QuarantinedVerdicts = slices.DeleteFunc(state.QuarantinedVerdicts, func(finding models.QuarantinedVerdict) bool {
		return finding.TaskID == taskID
	})

	// currentTask still points at the removed slot, which now holds the task
	// that followed it; the audit note has always read its status from there
	// (frozen by the delete-task oracle goldens), so it is built after removal.
	appendDeleteAuditNote(state, taskID, currentTask.Status, reason)
	return nil
}

// appendDeleteAuditNote records the deletion as an already-seen audit note.
func appendDeleteAuditNote(state *models.State, taskID string, status models.TaskStatus, reason string) {
	now := time.Now().UTC()
	humanNote := models.HumanNote{
		Timestamp: now,
		Message:   fmt.Sprintf("Task %s deleted (was %s): %s", taskID, status, reason),
		For:       "all",
	}
	humanNote.MarkSeenByOrchestrator(now) // audit record, not a request
	state.HumanNotes = append(state.HumanNotes, humanNote)
}

// cleanupDeletedTaskWorktree is the post-commit half of DeleteTask: it removes
// the worktree and branch when asked and otherwise reports the worktree as
// preserved. Failures become warnings because the state change is committed.
func cleanupDeletedTaskWorktree(projectRoot, taskID string, deleteWorktree bool, result *DeleteTaskResult) {
	gitWrapper := git.New(projectRoot)
	result.WorktreePath = gitWrapper.GetWorktreePath(taskID)

	if !deleteWorktree {
		result.WorktreePreserved = true
		return
	}
	if err := gitWrapper.RemoveWorktree(taskID); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("failed to remove worktree: %v", err))
	} else {
		result.WorktreeDeleted = true
	}

	// Delete branch (ignore errors if branch doesn't exist)
	_ = gitWrapper.DeleteBranch(paths.TaskBranchPrefix + taskID)
}
