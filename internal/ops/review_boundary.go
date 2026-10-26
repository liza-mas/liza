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
	// reviewBoundaryPhase is the OperationalError.Phase of review-boundary validation.
	reviewBoundaryPhase = "review-boundary"
)

// ReviewBoundaryRepairNeededError indicates submitted review metadata is stale
// but repairable by updating the task's review boundary from the worktree.
type ReviewBoundaryRepairNeededError struct {
	TaskID       string
	Reason       string
	RecoveryHint string
	// Orphaned marks a base_commit that is no longer on the integration
	// branch: no metadata-only repair exists, so the boundary is healed by
	// HealOrphanedReviewBase or routed to integration recovery (D-26).
	Orphaned bool
}

// reviewBaseOrphaned reports whether base resolves, is an ancestor of review,
// and is not an ancestor of the integration ref. Any Git error, including an
// unresolvable base, answers false: the regular boundary checks then report
// it. Assignment runs this under the state lock, so it costs two probes.
func reviewBaseOrphaned(g *gitpkg.Git, base, review, integration string) bool {
	onTask, err := g.IsAncestor(base, review)
	if err != nil || !onTask {
		return false
	}
	onIntegration, err := g.IsAncestor(base, integration)
	return err == nil && !onIntegration
}

// orphanedBaseRepairCommand is the manual repair for an orphaned base: replay
// only the task's own commits onto integration, then record the boundary.
func orphanedBaseRepairCommand(task *models.Task, integrationBranch string) string {
	return fmt.Sprintf("git -C %s rebase --onto %s %s, then run %s",
		optionalValue(task.Worktree), integrationBranch, optionalValue(task.BaseCommit), brand.Command(updateReviewCommitOperation, task.ID))
}

func (e *ReviewBoundaryRepairNeededError) Error() string {
	return fmt.Sprintf("task %s review boundary needs repair: %s — %s", e.TaskID, e.Reason, e.RecoveryHint)
}

// validateReviewBoundaryForAssignment verifies that assigning a reviewer would
// point them at the same commit currently checked out in the task worktree and
// the current effective review base.
func validateReviewBoundaryForAssignment(projectRoot string, state *models.State, task *models.Task) error {
	// An orphaned base invalidates the boundary whatever the evidence says, and
	// evidence is regenerated on the repaired boundary, so it is reported first.
	if task.Worktree != nil && task.ReviewCommit != nil && task.BaseCommit != nil &&
		reviewBaseOrphaned(gitpkg.New(projectRoot), *task.BaseCommit, *task.ReviewCommit, state.Config.IntegrationBranch) {
		return &ReviewBoundaryRepairNeededError{
			TaskID:       task.ID,
			Reason:       fmt.Sprintf("base_commit %s is no longer on integration branch %s", *task.BaseCommit, state.Config.IntegrationBranch),
			RecoveryHint: orphanedBaseRepairCommand(task, state.Config.IntegrationBranch),
			Orphaned:     true,
		}
	}
	if task.Worktree != nil || task.AcceptanceSource != nil {
		if err := validateAcceptanceForAssignment(projectRoot, state, task); err != nil {
			return err
		}
	}
	if task.Worktree == nil {
		return nil
	}
	if task.ReviewCommit == nil {
		return &PreconditionError{Reason: fmt.Sprintf("task %s has no review_commit — cannot assign for review", task.ID)}
	}
	g := gitpkg.New(projectRoot)
	integrationBranch := state.Config.IntegrationBranch
	expectedBase, err := g.GetMergeBase(*task.ReviewCommit, integrationBranch)
	if err != nil {
		return &OperationalError{
			Code:    operationalCodeGitOperation,
			Phase:   reviewBoundaryPhase,
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
			Code:    reviewClaimCodeWorktreeContext,
			Phase:   reviewBoundaryPhase,
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
			Code:    operationalCodeGitOperation,
			Phase:   reviewBoundaryPhase,
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
			RecoveryHint: fmt.Sprintf("run %s", brand.Command(updateReviewCommitOperation, task.ID)),
		}
	}
	if task.BaseCommit == nil {
		return &ReviewBoundaryRepairNeededError{
			TaskID:       task.ID,
			Reason:       fmt.Sprintf("base_commit is missing; expected effective review base %s", expectedBase),
			RecoveryHint: fmt.Sprintf("run %s", brand.Command(updateReviewCommitOperation, task.ID)),
		}
	}
	if *task.BaseCommit != expectedBase {
		return &ReviewBoundaryRepairNeededError{
			TaskID:       task.ID,
			Reason:       fmt.Sprintf("base_commit %s does not match effective review base %s", *task.BaseCommit, expectedBase),
			RecoveryHint: fmt.Sprintf("run %s", brand.Command(updateReviewCommitOperation, task.ID)),
		}
	}
	return nil
}

func markReviewBoundaryIntegrationFailed(state *models.State, task *models.Task, agentID string, transitions map[models.TaskStatus][]models.TaskStatus, cause error) error {
	reason := string(IntegrationReasonReviewBoundaryMismatch)
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
	models.AdvanceLifecycle(task)

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
