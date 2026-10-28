package ops

import (
	"context"
	"fmt"
	"log"
	"os"
	"reflect"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
)

// updateReviewCommitOperation names the operation in lock labels, worktree
// context errors and the runtime-input gate.
const updateReviewCommitOperation = "update-review-commit"

// History Extra keys of a review_commit_updated entry, written by the repair
// and by the orphaned-base heal.
const (
	oldReviewCommitExtraKey = "old_review_commit"
	newReviewCommitExtraKey = "new_review_commit"
	oldBaseCommitExtraKey   = "old_base_commit"
	newBaseCommitExtraKey   = "new_base_commit"
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
func UpdateReviewCommit(projectRoot, taskID, changedBy string) (result *UpdateReviewCommitResult, err error) {
	if taskID == "" {
		return nil, &PreconditionError{Reason: "task ID is required"}
	}
	if changedBy == "" {
		changedBy = actorHuman
	}
	// The per-task lock keeps this read of worktree HEAD out of an orphaned-base
	// heal's rebase, which holds the same lock until its boundary is published.
	lock := filelock.New(claimTaskWorktreeLockPath(paths.New(projectRoot).StatePath(), taskID))
	err = lock.WithLockOperation(updateReviewCommitOperation, func() error {
		var inner error
		result, inner = updateReviewCommitLocked(projectRoot, taskID, changedBy)
		return inner
	})
	return result, err
}

// reviewCommitUpdate carries one update-review-commit repair through its
// phases: preconditions from a snapshot, the worktree boundary, acceptance
// evidence outside the lock, and the final transaction.
type reviewCommitUpdate struct {
	projectRoot, taskID, changedBy string
	bb                             *db.Blackboard
	g                              *git.Git
	submitted                      models.TaskStatus
	reviewing                      models.TaskStatus
	reviewing2                     models.TaskStatus
	transitions                    map[models.TaskStatus][]models.TaskStatus

	wtHEAD          string
	effectiveBase   string
	oldReviewCommit *string
	oldBaseCommit   *string
	acceptance      *acceptanceInput
	receipt         *models.AcceptanceReceipt
	now             time.Time

	reviewerReleased bool
}

func updateReviewCommitLocked(projectRoot, taskID, changedBy string) (*UpdateReviewCommitResult, error) {
	u := &reviewCommitUpdate{projectRoot: projectRoot, taskID: taskID, changedBy: changedBy, bb: db.For(paths.New(projectRoot).StatePath()), g: git.New(projectRoot)}

	// Phase 1: Read state and validate preconditions
	state, task, err := readTaskState(u.bb, taskID)
	if err != nil {
		return nil, err
	}
	resolver, _, resolverErr := loadResolver(projectRoot)
	if resolverErr != nil {
		return nil, resolverErr
	}
	if err := u.checkPreconditions(task, resolver); err != nil {
		return nil, err
	}
	u.transitions = BuildPipelineTransitions(resolver)

	// Phase 2: Read worktree HEAD
	if err := u.readWorktreeBoundary(state); err != nil {
		return nil, err
	}
	if err := u.prepareAcceptance(state, task); err != nil {
		return nil, err
	}

	// Phase 3: Atomic state update
	u.now = time.Now().UTC()
	if err := u.bb.Modify(u.apply); err != nil {
		return nil, fmt.Errorf("failed to update review commit: %w", err)
	}
	return &UpdateReviewCommitResult{
		TaskID:           taskID,
		OldReviewCommit:  u.oldReviewCommit,
		NewReviewCommit:  u.wtHEAD,
		OldBaseCommit:    u.oldBaseCommit,
		NewBaseCommit:    u.effectiveBase,
		ReviewerReleased: u.reviewerReleased,
		ChangedBy:        changedBy,
	}, nil
}

// checkPreconditions resolves the role pair's statuses and requires the task
// to be submitted or under review.
func (u *reviewCommitUpdate) checkPreconditions(task *models.Task, resolver *pipeline.Resolver) error {
	if task.RolePair == "" {
		return &PreconditionError{Reason: fmt.Sprintf("task %s has no role_pair set", u.taskID)}
	}
	var err error
	if u.submitted, err = resolver.SubmittedStatus(task.RolePair); err != nil {
		return fmt.Errorf("invalid role-pair %q: %w", task.RolePair, err)
	}
	if u.reviewing, err = resolver.ReviewingStatus(task.RolePair); err != nil {
		return fmt.Errorf("invalid role-pair %q: %w", task.RolePair, err)
	}
	u.reviewing2, _ = resolver.Reviewing2Status(task.RolePair)
	return u.requireRepairableStatus(task)
}

// requireRepairableStatus requires the task to be submitted or under review.
// It runs on the snapshot and again inside the final transaction.
func (u *reviewCommitUpdate) requireRepairableStatus(task *models.Task) error {
	if task.Status != u.submitted && !u.underReview(task) {
		return &PreconditionError{Reason: fmt.Sprintf("task %s must be in submitted or reviewing state (current: %s)", u.taskID, task.Status)}
	}
	return nil
}

func (u *reviewCommitUpdate) underReview(task *models.Task) bool {
	return task.Status == u.reviewing || (u.reviewing2 != "" && task.Status == u.reviewing2)
}

// readWorktreeBoundary reads the worktree HEAD and the effective review base
// the repaired boundary will record.
func (u *reviewCommitUpdate) readWorktreeBoundary(state *models.State) error {
	wtPath := u.g.GetWorktreePath(u.taskID)
	if _, statErr := os.Stat(wtPath); statErr != nil {
		reason := "failed to stat worktree"
		if os.IsNotExist(statErr) {
			reason = "worktree directory does not exist"
		}
		return &errors.WorktreeContextError{Operation: updateReviewCommitOperation, TaskID: u.taskID, Reason: reason, Err: statErr}
	}
	var err error
	if u.wtHEAD, err = u.g.GetWorktreeHEAD(u.taskID); err != nil {
		return fmt.Errorf("failed to get worktree HEAD: %w", err)
	}
	if u.effectiveBase, err = u.g.GetMergeBase(u.wtHEAD, state.Config.IntegrationBranch); err != nil {
		return fmt.Errorf("failed to resolve effective review base: %w", err)
	}
	return nil
}

// prepareAcceptance refuses a repair that would change nothing, then executes
// the acceptance evidence for the worktree HEAD outside the lock. A
// runtime-input refusal leaves the review boundary and receipt as they were. A
// reviewer repairing its own claimed task cannot review without the receipt, so
// the refusal releases that claim the way a successful repair does (ADR-0169);
// an operator or supervisor caller touches none.
func (u *reviewCommitUpdate) prepareAcceptance(state *models.State, task *models.Task) error {
	u.oldReviewCommit = cloneStringPtr(task.ReviewCommit)
	u.oldBaseCommit = cloneStringPtr(task.BaseCommit)
	integrationCommit, err := u.g.GetCommitSHA(state.Config.IntegrationBranch)
	if err != nil {
		return err
	}
	u.acceptance, err = loadAcceptanceInput(u.projectRoot, state, task, integrationCommit)
	if err != nil {
		return err
	}
	if u.oldReviewCommit != nil && *u.oldReviewCommit == u.wtHEAD && u.oldBaseCommit != nil && *u.oldBaseCommit == u.effectiveBase {
		if u.acceptance == nil || validateAcceptanceForAssignment(u.projectRoot, state, task) == nil {
			return &PreconditionError{Reason: fmt.Sprintf("review boundary already matches worktree HEAD %s and base %s — no update needed", u.wtHEAD, u.effectiveBase)}
		}
	}
	gate := runtimeInputGate{projectRoot: u.projectRoot, bb: u.bb, actor: u.changedBy, operation: updateReviewCommitOperation,
		onRefusal: func(state *models.State, task *models.Task) error {
			if task.ReviewingBy == nil || *task.ReviewingBy != u.changedBy {
				return nil
			}
			return releaseReviewClaimToSubmitted(state, task, u.submitted, u.reviewing, u.reviewing2, u.transitions)
		}}
	u.receipt, err = executeAcceptanceReceipt(context.Background(), u.projectRoot, task, u.acceptance, u.wtHEAD, gate, state.Config, nil, nil)
	return err
}

// apply is the update transaction: it revalidates the task against the
// snapshot, republishes the boundary and receipt, releases a claimed reviewer
// and records the repair. K1-E8: recheckAcceptanceReceipt reads Git inside it.
func (u *reviewCommitUpdate) apply(state *models.State) error {
	task := state.FindTask(u.taskID)
	if task == nil {
		return &errors.NotFoundError{Entity: notFoundEntityTask, ID: u.taskID}
	}

	// Re-check status inside lock
	if err := u.requireRepairableStatus(task); err != nil {
		return err
	}

	// Update the full review boundary.
	if !reflect.DeepEqual(task.ReviewCommit, u.oldReviewCommit) || !reflect.DeepEqual(task.BaseCommit, u.oldBaseCommit) {
		return &PreconditionError{Reason: "review boundary changed during repair"}
	}
	if err := recheckAcceptanceReceipt(u.projectRoot, state, task, u.acceptance, u.receipt); err != nil {
		return err
	}
	if u.receipt != nil {
		task.AcceptanceSource = &u.receipt.Source
	}
	task.AcceptanceReceipt = u.receipt
	models.AdvanceLifecycle(task)
	task.ReviewCommit = &u.wtHEAD
	task.BaseCommit = &u.effectiveBase

	// If reviewer is claimed, release them and reset to submitted —
	// they must re-claim and re-review the updated content.
	if u.underReview(task) && task.ReviewingBy != nil {
		if err := releaseReviewClaimToSubmitted(state, task, u.submitted, u.reviewing, u.reviewing2, u.transitions); err != nil {
			return err
		}
		u.reviewerReleased = true
	}
	u.recordRepair(task)
	return nil
}

// recordRepair appends the review_commit_updated history entry.
func (u *reviewCommitUpdate) recordRepair(task *models.Task) {
	oldReviewForReason := "<nil>"
	if u.oldReviewCommit != nil {
		oldReviewForReason = *u.oldReviewCommit
	}
	oldBaseForReason := "<nil>"
	if u.oldBaseCommit != nil {
		oldBaseForReason = *u.oldBaseCommit
	}
	updateReason := fmt.Sprintf(
		"review boundary repaired from worktree HEAD: review_commit %s → %s, base_commit %s → %s",
		oldReviewForReason,
		u.wtHEAD,
		oldBaseForReason,
		u.effectiveBase,
	)
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:   u.now,
		Event:  models.TaskEventReviewCommitUpdated,
		Agent:  &u.changedBy,
		Reason: &updateReason,
		Commit: &u.wtHEAD,
		Extra: map[string]any{
			oldReviewCommitExtraKey: u.oldReviewCommit,
			newReviewCommitExtraKey: u.wtHEAD,
			oldBaseCommitExtraKey:   u.oldBaseCommit,
			newBaseCommitExtraKey:   u.effectiveBase,
		},
	})
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

// releaseReviewClaimToSubmitted releases a claimed reviewer and returns the
// task to its submitted status, keeping receipt and review boundary: a
// reviewing task without a reviewer is not a valid state. A task that is not
// in a reviewing status is left unchanged.
func releaseReviewClaimToSubmitted(state *models.State, task *models.Task, submittedStatus, reviewingStatus, reviewing2Status models.TaskStatus, transitions map[models.TaskStatus][]models.TaskStatus) error {
	reviewing := task.Status == reviewingStatus || (reviewing2Status != "" && task.Status == reviewing2Status)
	if !reviewing || task.ReviewingBy == nil {
		return nil
	}
	releasedAgent := *task.ReviewingBy
	if a, ok := state.Agents[releasedAgent]; ok {
		if a.CurrentTask != nil && *a.CurrentTask == task.ID {
			state.ReleaseAgent(releasedAgent)
		}
	}
	task.ReviewingBy = nil
	task.ReviewLeaseExpires = nil
	if err := task.TransitionWith(submittedStatus, transitions); err != nil {
		return err
	}
	log.Printf("update-review-commit %s: released reviewer %s", task.ID, releasedAgent)
	return nil
}
