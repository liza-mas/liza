package ops

import (
	"fmt"
	"log"
	"os"
	"reflect"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// UpdateReviewCommitResult contains the outcome of updating a task's review commit.
type UpdateReviewCommitResult struct {
	TaskID           string  `json:"task_id"`
	OldReviewCommit  *string `json:"old_review_commit"`
	NewReviewCommit  string  `json:"new_review_commit"`
	OldBaseCommit    *string `json:"old_base_commit"`
	NewBaseCommit    string  `json:"new_base_commit"`
	ReviewerReleased bool    `json:"reviewer_released"`
	ChangedBy        string  `json:"changed_by"`
}

// UpdateReviewCommit updates the review boundary to the current worktree HEAD
// and effective merge base after an external rebase. This is an explicit
// resubmission boundary: if a reviewer has claimed the task, their claim is
// released so the task returns to submitted state for a fresh review pass.
// No terminal I/O.
func UpdateReviewCommit(projectRoot, taskID, changedBy string) (*UpdateReviewCommitResult, error) {
	if taskID == "" {
		return nil, &PreconditionError{Reason: "task ID is required"}
	}
	if changedBy == "" {
		changedBy = "human"
	}

	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())

	// Phase 1: Read state and validate preconditions
	state, task, err := readTaskState(bb, taskID)
	if err != nil {
		return nil, err
	}

	resolver, _, resolverErr := loadResolver(projectRoot)
	if resolverErr != nil {
		return nil, resolverErr
	}
	if task.RolePair == "" {
		return nil, &PreconditionError{Reason: fmt.Sprintf("task %s has no role_pair set", taskID)}
	}

	submittedStatus, err := resolver.SubmittedStatus(task.RolePair)
	if err != nil {
		return nil, fmt.Errorf("invalid role-pair %q: %w", task.RolePair, err)
	}
	reviewingStatus, err := resolver.ReviewingStatus(task.RolePair)
	if err != nil {
		return nil, fmt.Errorf("invalid role-pair %q: %w", task.RolePair, err)
	}
	reviewing2Status, _ := resolver.Reviewing2Status(task.RolePair)

	isSubmitted := task.Status == submittedStatus
	isReviewing := task.Status == reviewingStatus ||
		(reviewing2Status != "" && task.Status == reviewing2Status)
	if !isSubmitted && !isReviewing {
		return nil, &PreconditionError{Reason: fmt.Sprintf("task %s must be in submitted or reviewing state (current: %s)", taskID, task.Status)}
	}

	// Phase 2: Read worktree HEAD
	g := git.New(projectRoot)
	wtPath := g.GetWorktreePath(taskID)
	if _, statErr := os.Stat(wtPath); os.IsNotExist(statErr) {
		return nil, &errors.WorktreeContextError{
			Operation: "update-review-commit",
			TaskID:    taskID,
			Reason:    "worktree directory does not exist",
			Err:       statErr,
		}
	} else if statErr != nil {
		return nil, &errors.WorktreeContextError{
			Operation: "update-review-commit",
			TaskID:    taskID,
			Reason:    "failed to stat worktree",
			Err:       statErr,
		}
	}

	wtHEAD, err := g.GetWorktreeHEAD(taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to get worktree HEAD: %w", err)
	}
	effectiveBase, err := g.GetMergeBase(wtHEAD, state.Config.IntegrationBranch)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve effective review base: %w", err)
	}

	oldReviewCommit := cloneStringPtr(task.ReviewCommit)
	oldBaseCommit := cloneStringPtr(task.BaseCommit)
	integrationCommit, err := g.GetCommitSHA(state.Config.IntegrationBranch)
	if err != nil {
		return nil, err
	}
	acceptance, err := loadAcceptanceInput(projectRoot, state, task, integrationCommit)
	if err != nil {
		return nil, err
	}
	if oldReviewCommit != nil && *oldReviewCommit == wtHEAD && oldBaseCommit != nil && *oldBaseCommit == effectiveBase {
		if acceptance == nil || validateAcceptanceForAssignment(projectRoot, state, task) == nil {
			return nil, &PreconditionError{Reason: fmt.Sprintf("review boundary already matches worktree HEAD %s and base %s — no update needed", wtHEAD, effectiveBase)}
		}
	}
	receipt, err := executeAcceptanceReceipt(projectRoot, task, acceptance, wtHEAD)
	if err != nil {
		return nil, err
	}

	// Phase 3: Atomic state update
	pipelineTransitions := BuildPipelineTransitions(resolver)
	now := time.Now().UTC()
	reviewerReleased := false

	err = bb.Modify(func(state *models.State) error {
		task := state.FindTask(taskID)
		if task == nil {
			return &errors.NotFoundError{Entity: "task", ID: taskID}
		}

		// Re-check status inside lock
		isSubmittedAuth := task.Status == submittedStatus
		isReviewingAuth := task.Status == reviewingStatus ||
			(reviewing2Status != "" && task.Status == reviewing2Status)
		if !isSubmittedAuth && !isReviewingAuth {
			return &PreconditionError{Reason: fmt.Sprintf("task %s must be in submitted or reviewing state (current: %s)", taskID, task.Status)}
		}

		// Update the full review boundary.
		if !reflect.DeepEqual(task.ReviewCommit, oldReviewCommit) || !reflect.DeepEqual(task.BaseCommit, oldBaseCommit) {
			return &PreconditionError{Reason: "review boundary changed during repair"}
		}
		if err := recheckAcceptanceReceipt(projectRoot, state, task, acceptance, receipt); err != nil {
			return err
		}
		if receipt != nil {
			task.AcceptanceSource = &receipt.Source
		}
		task.AcceptanceReceipt = receipt
		models.AdvanceLifecycle(task)
		task.ReviewCommit = &wtHEAD
		task.BaseCommit = &effectiveBase

		// If reviewer is claimed, release them and reset to submitted —
		// they must re-claim and re-review the updated content.
		if isReviewingAuth && task.ReviewingBy != nil {
			releasedAgent := *task.ReviewingBy
			if a, ok := state.Agents[releasedAgent]; ok {
				if a.CurrentTask != nil && *a.CurrentTask == taskID {
					state.ReleaseAgent(releasedAgent)
				}
			}
			task.ReviewingBy = nil
			task.ReviewLeaseExpires = nil
			reviewerReleased = true

			if err := task.TransitionWith(submittedStatus, pipelineTransitions); err != nil {
				return err
			}

			log.Printf("update-review-commit %s: released reviewer %s", taskID, releasedAgent)
		}

		oldReviewForReason := "<nil>"
		if oldReviewCommit != nil {
			oldReviewForReason = *oldReviewCommit
		}
		oldBaseForReason := "<nil>"
		if oldBaseCommit != nil {
			oldBaseForReason = *oldBaseCommit
		}
		updateReason := fmt.Sprintf(
			"review boundary repaired from worktree HEAD: review_commit %s → %s, base_commit %s → %s",
			oldReviewForReason,
			wtHEAD,
			oldBaseForReason,
			effectiveBase,
		)
		task.History = append(task.History, models.TaskHistoryEntry{
			Time:   now,
			Event:  models.TaskEventReviewCommitUpdated,
			Agent:  &changedBy,
			Reason: &updateReason,
			Commit: &wtHEAD,
			Extra: map[string]any{
				"old_review_commit": oldReviewCommit,
				"new_review_commit": wtHEAD,
				"old_base_commit":   oldBaseCommit,
				"new_base_commit":   effectiveBase,
			},
		})

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to update review commit: %w", err)
	}

	return &UpdateReviewCommitResult{
		TaskID:           taskID,
		OldReviewCommit:  oldReviewCommit,
		NewReviewCommit:  wtHEAD,
		OldBaseCommit:    oldBaseCommit,
		NewBaseCommit:    effectiveBase,
		ReviewerReleased: reviewerReleased,
		ChangedBy:        changedBy,
	}, nil
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
