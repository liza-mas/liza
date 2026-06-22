package ops

import (
	"fmt"
	"os"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	lizaerrors "github.com/liza-mas/liza/internal/errors"
	gitpkg "github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
)

const (
	reviewBoundaryOperationAssignment = "review-assignment"
)

// ReviewBoundaryRepairNeededError indicates submitted review metadata is stale
// but repairable by updating the task's review boundary from the worktree.
type ReviewBoundaryRepairNeededError struct {
	TaskID       string
	Reason       string
	RecoveryHint string
}

func (e *ReviewBoundaryRepairNeededError) Error() string {
	return fmt.Sprintf("task %s review boundary needs repair: %s — %s", e.TaskID, e.Reason, e.RecoveryHint)
}

// validateReviewBoundaryForAssignment verifies that assigning a reviewer would
// point them at the same commit currently checked out in the task worktree and
// the current effective review base.
func validateReviewBoundaryForAssignment(projectRoot string, task *models.Task, integrationBranch string) error {
	if task.Worktree == nil {
		return nil
	}
	if task.ReviewCommit == nil {
		return &PreconditionError{Reason: fmt.Sprintf("task %s has no review_commit — cannot assign for review", task.ID)}
	}
	g := gitpkg.New(projectRoot)
	expectedBase, err := g.GetMergeBase(*task.ReviewCommit, integrationBranch)
	if err != nil {
		return &OperationalError{
			Code:    "git_operation",
			Phase:   "review-boundary",
			Message: "failed to resolve effective review base",
			Details: map[string]any{
				"operation":          reviewBoundaryOperationAssignment,
				"task_id":            task.ID,
				"integration_branch": integrationBranch,
				"review_commit":      *task.ReviewCommit,
				"recovery_hint":      "Inspect the task worktree and integration branch, then retry the review operation.",
			},
			Err: err,
		}
	}
	return validateReviewBoundaryCommit(projectRoot, task, *task.ReviewCommit, expectedBase)
}

func validateReviewBoundaryCommit(projectRoot string, task *models.Task, reviewCommit, expectedBase string) error {
	if task.Worktree == nil {
		return nil
	}

	g := gitpkg.New(projectRoot)
	wtPath := g.GetWorktreePath(task.ID)
	if _, err := os.Stat(wtPath); err != nil {
		if os.IsNotExist(err) {
			return &lizaerrors.WorktreeContextError{
				Operation: reviewBoundaryOperationAssignment,
				TaskID:    task.ID,
				Reason:    "task has recorded worktree but worktree directory does not exist",
				Err:       err,
			}
		}
		return &OperationalError{
			Code:    "worktree_context",
			Phase:   "review-boundary",
			Message: "failed to stat task worktree",
			Details: map[string]any{
				"operation":     reviewBoundaryOperationAssignment,
				"task_id":       task.ID,
				"recovery_hint": "Inspect the task worktree path and filesystem permissions, then retry the review operation.",
			},
			Err: err,
		}
	}

	wtHEAD, err := g.GetWorktreeHEAD(task.ID)
	if err != nil {
		return &OperationalError{
			Code:    "git_operation",
			Phase:   "review-boundary",
			Message: "failed to get task worktree HEAD",
			Details: map[string]any{
				"operation":     reviewBoundaryOperationAssignment,
				"task_id":       task.ID,
				"recovery_hint": "Inspect the task worktree git metadata, ensure HEAD resolves, then retry the review operation.",
			},
			Err: err,
		}
	}
	if reviewCommit != wtHEAD {
		return &ReviewBoundaryRepairNeededError{
			TaskID:       task.ID,
			Reason:       fmt.Sprintf("review_commit %s does not match worktree HEAD %s", reviewCommit, wtHEAD),
			RecoveryHint: fmt.Sprintf("run %s", brand.Command("update-review-commit", task.ID)),
		}
	}
	if task.BaseCommit == nil {
		return &ReviewBoundaryRepairNeededError{
			TaskID:       task.ID,
			Reason:       fmt.Sprintf("base_commit is missing; expected effective review base %s", expectedBase),
			RecoveryHint: fmt.Sprintf("run %s", brand.Command("update-review-commit", task.ID)),
		}
	}
	if *task.BaseCommit != expectedBase {
		return &ReviewBoundaryRepairNeededError{
			TaskID:       task.ID,
			Reason:       fmt.Sprintf("base_commit %s does not match effective review base %s", *task.BaseCommit, expectedBase),
			RecoveryHint: fmt.Sprintf("run %s", brand.Command("update-review-commit", task.ID)),
		}
	}
	return nil
}

func markReviewBoundaryIntegrationFailed(state *models.State, task *models.Task, agentID string, transitions map[models.TaskStatus][]models.TaskStatus, cause error) error {
	reason := IntegrationReasonReviewBoundaryMismatch
	if err := task.TransitionWith(models.TaskStatusIntegrationFailed, transitions); err != nil {
		return err
	}

	if task.ReviewingBy != nil {
		state.ReleaseAgent(*task.ReviewingBy)
	}
	if task.AssignedTo != nil {
		assignedAgent := *task.AssignedTo
		if agent, ok := state.Agents[assignedAgent]; ok && agent.CurrentTask != nil && *agent.CurrentTask == task.ID {
			state.ReleaseAgent(assignedAgent)
		}
	}

	task.FailedBy = appendUniqueAgentID(task.FailedBy, agentID)
	task.IntegrationFix = false
	task.AssignedTo = nil
	task.LeaseExpires = nil
	task.ReviewingBy = nil
	task.ReviewLeaseExpires = nil

	diagnostic := map[string]any{
		"operation":     reviewBoundaryOperationAssignment,
		"reason":        reason,
		"detail":        cause.Error(),
		"recovery_hint": "claim the task for integration fix, verify the worktree HEAD, then resubmit for review",
	}
	task.IntegrationFailure = cloneMapForTaskDiagnostic(diagnostic)

	now := time.Now().UTC()
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:   now,
		Event:  models.TaskEventIntegrationFailed,
		Agent:  &agentID,
		Reason: &reason,
		Extra: map[string]any{
			"diagnostic": diagnostic,
		},
	})
	return nil
}
