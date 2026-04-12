package ops

import (
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// CancelResult contains the outcome of cancelling a task.
type CancelResult struct {
	TaskID         string            `json:"task_id"`
	OriginalStatus models.TaskStatus `json:"original_status"`
	Warnings       []string          `json:"warnings"`
}

// CancelTask transitions a task to ABANDONED with a reason, preserving full audit trail.
// Cancellable states are determined by the pipeline transition map (TransitionWith).
// No terminal I/O.
func CancelTask(projectRoot, taskID, reason, agentID string) (*CancelResult, error) {
	if taskID == "" {
		return nil, &PreconditionError{Reason: "task ID is required"}
	}
	if reason == "" {
		return nil, &PreconditionError{Reason: "cancellation reason is required"}
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

	// Read current state (no lock held) to capture original status and worktree info.
	_, task, err := readTaskState(bb, taskID)
	if err != nil {
		return nil, err
	}

	originalStatus := task.Status

	// Atomic State Update
	err = bb.Modify(func(state *models.State) error {
		currentTask := state.FindTask(taskID)
		if currentTask == nil {
			return &errors.NotFoundError{Entity: "task", ID: taskID}
		}

		if currentTask.Status != originalStatus {
			return &PreconditionError{Reason: fmt.Sprintf("cannot cancel task %s: status changed from %s to %s", taskID, originalStatus, currentTask.Status)}
		}

		if err := currentTask.TransitionWith(models.TaskStatusAbandoned, pb.transitions); err != nil {
			return err
		}

		currentTask.AssignedTo = nil
		currentTask.LeaseExpires = nil
		currentTask.ReviewingBy = nil
		currentTask.ReviewLeaseExpires = nil
		currentTask.Worktree = nil

		now := time.Now().UTC()
		currentTask.History = append(currentTask.History, models.TaskHistoryEntry{
			Time:   now,
			Event:  models.TaskEventAbandoned,
			Agent:  &agentID,
			Reason: &reason,
		})

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to cancel task: %w", err)
	}

	// Best-effort cleanup (after state commit). Own branch is always deleted —
	// cancelled tasks have no successors that need it. Unconditional: branch
	// can outlive worktree directory after recovery or manual cleanup.
	var warnings []string
	gw := git.New(projectRoot)
	if rmErr := gw.RemoveWorktreeDir(taskID); rmErr != nil {
		warnings = append(warnings, fmt.Sprintf("failed to remove worktree directory: %v", rmErr))
	}
	taskBranch := paths.TaskBranchPrefix + taskID
	if exists, brErr := gw.BranchExists(taskBranch); brErr != nil {
		warnings = append(warnings, fmt.Sprintf("failed to check branch %s: %v", taskBranch, brErr))
	} else if exists {
		if delErr := gw.DeleteBranch(taskBranch); delErr != nil {
			warnings = append(warnings, fmt.Sprintf("failed to delete branch %s: %v", taskBranch, delErr))
		}
	}

	// This cancelled task may be a successor — check if its terminal
	// transition releases an older predecessor's branch.
	warnings = append(warnings, cleanupPredecessorBranches(bb, gw, taskID)...)

	return &CancelResult{
		TaskID:         taskID,
		OriginalStatus: originalStatus,
		Warnings:       warnings,
	}, nil
}
