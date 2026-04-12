package ops

import (
	"fmt"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// SupersedeResult contains the outcome of superseding a task.
type SupersedeResult struct {
	TaskID         string            `json:"task_id"`
	OriginalStatus models.TaskStatus `json:"original_status"`
	ReplacementIDs []string          `json:"replacement_ids"`
	Warnings       []string          `json:"warnings"`
}

// SupersedeTask transitions a BLOCKED, REJECTED, or READY task to SUPERSEDED,
// optionally linking it to replacement task IDs. When no replacements are given
// the task's branch is deleted immediately (no successors to trigger cleanup).
// No terminal I/O.
func SupersedeTask(projectRoot, taskID string, replacementIDs []string, reason, agentID string) (*SupersedeResult, error) {
	if taskID == "" {
		return nil, &PreconditionError{Reason: "task ID is required"}
	}
	if reason == "" {
		return nil, &PreconditionError{Reason: "rescope reason is required"}
	}
	if agentID == "" {
		return nil, &PreconditionError{Reason: "orchestrator agent ID is required"}
	}

	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())

	pb, err := loadPipelineBundle(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load pipeline config: %w", err)
	}

	// Phase 1: Read and Validate (no lock held)
	_, task, err := readTaskState(bb, taskID)
	if err != nil {
		return nil, err
	}

	originalStatus := task.Status
	if originalStatus != models.TaskStatusBlocked &&
		originalStatus != models.TaskStatusRejected &&
		originalStatus != models.TaskStatusReady {
		return nil, &PreconditionError{Reason: fmt.Sprintf("cannot supersede task %s in status %s (must be BLOCKED, REJECTED, or READY)", taskID, originalStatus)}
	}

	// Phase 2: Atomic State Update
	hadWorktree := task.Worktree != nil
	err = bb.Modify(func(state *models.State) error {
		currentTask := state.FindTask(taskID)
		if currentTask == nil {
			return &errors.NotFoundError{Entity: "task", ID: taskID}
		}

		if currentTask.Status != originalStatus {
			return &PreconditionError{Reason: fmt.Sprintf("cannot supersede task %s: status changed from %s to %s", taskID, originalStatus, currentTask.Status)}
		}

		if err := currentTask.TransitionWith(models.TaskStatusSuperseded, pb.transitions); err != nil {
			return err
		}
		currentTask.SupersededBy = replacementIDs
		currentTask.RescopeReason = &reason

		currentTask.AssignedTo = nil
		currentTask.LeaseExpires = nil
		currentTask.ReviewingBy = nil
		currentTask.ReviewLeaseExpires = nil
		currentTask.Worktree = nil

		now := time.Now().UTC()
		var note string
		if len(replacementIDs) > 0 {
			note = fmt.Sprintf("replaced by: %s", strings.Join(replacementIDs, ", "))
		} else {
			note = "superseded without replacements"
		}
		currentTask.History = append(currentTask.History, models.TaskHistoryEntry{
			Time:   now,
			Event:  models.TaskEventSuperseded,
			Agent:  &agentID,
			Reason: &reason,
			Note:   &note,
		})

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to supersede task: %w", err)
	}

	// Best-effort worktree cleanup (after state commit — safe to lose worktree now).
	var warnings []string
	gw := git.New(projectRoot)
	if hadWorktree {
		if rmErr := gw.RemoveWorktreeDir(taskID); rmErr != nil {
			warnings = append(warnings, fmt.Sprintf("failed to remove worktree directory: %v", rmErr))
		}
	}

	// When there are no successors, delete the branch immediately — no successor
	// will ever trigger cleanup via cleanupPredecessorBranches.
	// When successors exist, branch is preserved for git show access.
	if len(replacementIDs) == 0 {
		branchName := paths.TaskBranchPrefix + taskID
		exists, err := gw.BranchExists(branchName)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("failed to check branch %s: %v", branchName, err))
		} else if exists {
			if err := gw.DeleteBranch(branchName); err != nil {
				warnings = append(warnings, fmt.Sprintf("failed to delete branch %s: %v", branchName, err))
			}
		}
	}

	// This superseded task may itself be a successor — check if its terminal
	// transition releases an older predecessor's branch.
	warnings = append(warnings, cleanupPredecessorBranches(bb, gw, taskID)...)

	return &SupersedeResult{
		TaskID:         taskID,
		OriginalStatus: originalStatus,
		ReplacementIDs: replacementIDs,
		Warnings:       warnings,
	}, nil
}
