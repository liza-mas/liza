package ops

import (
	"fmt"
	"log"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// TransitionAttemptResult contains the outcome of transitioning to a new attempt.
type TransitionAttemptResult struct {
	models.LifecycleOutcome
	TaskID          string
	NewAttempt      int
	WorktreeDeleted bool
	InitialStatus   models.TaskStatus
}

// transitioning is the sentinel value for AssignedTo during attempt transition.
const transitioning = "$transitioning"

// transitionTestHooks provides injection points for testing intermediate states
// in TransitionToNewAttempt. Nil in production — zero overhead.
type transitionTestHooks struct {
	// afterPhase1 is called after Phase 1 commits to the blackboard,
	// before Phase 2 git operations. Use to inspect intermediate state.
	afterPhase1 func()
}

// testTransitionHooks is nil in production. Tests set it to observe/inject
// behavior between phases.
var testTransitionHooks *transitionTestHooks

// TransitionToNewAttempt implements the 3-phase attempt boundary operation.
//
// Phase 1 (bb.Modify): Set Attempt=2, reset counters, set sentinel, append
// history, release agent. Preserves Status, Worktree, BaseCommit, RejectionReason.
//
// Phase 2 (git ops, outside lock): Delete worktree and branch best-effort.
//
// Phase 3 (bb.Modify): Re-check sentinel, clear AssignedTo/RejectionReason/
// Worktree/BaseCommit, transition to initial pipeline status.
func TransitionToNewAttempt(projectRoot, taskID, reason string) (*TransitionAttemptResult, error) {
	return transitionToNewAttemptWithProjectLock(projectRoot, taskID, reason, nil)
}

// TransitionToNewAttemptWithAuthority fences both state phases for an
// authenticated lifecycle caller.
func TransitionToNewAttemptWithAuthority(projectRoot, taskID, reason string, authority models.AgentAuthority) (*TransitionAttemptResult, error) {
	return transitionToNewAttemptWithProjectLock(projectRoot, taskID, reason, &authority)
}

func transitionToNewAttemptWithProjectLock(projectRoot, taskID, reason string, authority *models.AgentAuthority) (*TransitionAttemptResult, error) {
	var result *TransitionAttemptResult
	err := WithProjectLifecycleSharedLock(projectRoot, "transition-attempt", func() error {
		var inner error
		result, inner = transitionToNewAttemptWithOptionalAuthority(projectRoot, taskID, reason, authority)
		return inner
	})
	return result, err
}

func transitionToNewAttemptWithOptionalAuthority(projectRoot, taskID, reason string, authority *models.AgentAuthority) (*TransitionAttemptResult, error) {
	return transitionToNewAttemptAtBoundary(projectRoot, taskID, reason, authority, "")
}

// transitionToNewAttemptAfterVerdict runs only after the outer verdict review
// lock is released, while its project lifecycle lock remains held. The token
// prevents a delayed rejection follow-up from resetting an intervening claim.
func transitionToNewAttemptAfterVerdict(projectRoot, taskID, reason string, authority *models.AgentAuthority, expectedCompletionToken string) (*TransitionAttemptResult, error) {
	if !lifecycleDigestValid(expectedCompletionToken) {
		return nil, &PreconditionError{Reason: "attempt rollover requires the completed verdict transition token"}
	}
	return transitionToNewAttemptAtBoundary(projectRoot, taskID, reason, authority, expectedCompletionToken)
}

func transitionToNewAttemptAtBoundary(projectRoot, taskID, reason string, authority *models.AgentAuthority, expectedCompletionToken string) (*TransitionAttemptResult, error) {
	// Claim invokes rollover before taking this same task lock; verdict invokes
	// it only after releasing its final state transaction.
	lock := filelock.New(claimTaskWorktreeLockPath(paths.New(projectRoot).StatePath(), taskID))
	var result *TransitionAttemptResult
	err := lock.WithLockOperation("transition-attempt", func() error {
		var inner error
		result, inner = transitionToNewAttemptLocked(projectRoot, taskID, reason, authority, expectedCompletionToken)
		return inner
	})
	return result, err
}

func transitionToNewAttemptLocked(projectRoot, taskID, reason string, authority *models.AgentAuthority, expectedCompletionToken string) (*TransitionAttemptResult, error) {
	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())

	pb, err := loadPipelineBundle(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load pipeline config: %w", err)
	}

	var (
		worktreePath     string
		previousAgent    string
		originalStatus   models.TaskStatus
		initialStatus    models.TaskStatus
		lifecycleOutcome models.LifecycleOutcome
	)

	// Phase 1: mark attempt boundary, block claims via sentinel.
	err = lifecycleMutation(bb, authority)(func(state *models.State) error {
		task := state.FindTask(taskID)
		if task == nil {
			return &errors.NotFoundError{Entity: "task", ID: taskID}
		}
		if expectedCompletionToken != "" && models.TaskTransitionID(task) != expectedCompletionToken {
			return &PreconditionError{Reason: "task boundary changed after verdict completion; attempt rollover was not applied"}
		}

		if task.EffectiveAttempt() != 1 {
			return &PreconditionError{
				Reason: fmt.Sprintf("task %s is on attempt %d, only attempt 1 can transition", taskID, task.EffectiveAttempt()),
			}
		}

		// Resolve initial status for the task's role-pair.
		var resolveErr error
		initialStatus, resolveErr = pb.pr.InitialStatus(task.RolePair)
		if resolveErr != nil {
			return fmt.Errorf("failed to resolve initial status for role-pair %s: %w", task.RolePair, resolveErr)
		}

		// Capture fields for Phase 2 and Phase 3.
		originalStatus = task.Status
		if task.Worktree != nil {
			worktreePath = *task.Worktree
		}
		if task.AssignedTo != nil {
			previousAgent = *task.AssignedTo
		}

		// Capture rejection feedback before mutations for history preservation.
		var rejectionNote *string
		if task.RejectionReason != nil && *task.RejectionReason != "" {
			rr := *task.RejectionReason
			rejectionNote = &rr
		}

		// Mutations.
		models.AdvanceLifecycle(task)
		task.Attempt = 2
		task.Iteration = 0
		task.ReviewCyclesCurrent = 0
		task.LeaseExpires = nil

		sentinel := transitioning
		task.AssignedTo = &sentinel

		now := time.Now().UTC()
		task.History = append(task.History, models.TaskHistoryEntry{
			Time:   now,
			Event:  models.TaskEventNewAttempt,
			Reason: &reason,
			Note:   rejectionNote,
		})

		if previousAgent != "" {
			state.ReleaseAgent(previousAgent)
		}

		// Preserved: Status, Worktree, BaseCommit, RejectionReason.
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Test hook: inspect intermediate state after Phase 1.
	if testTransitionHooks != nil && testTransitionHooks.afterPhase1 != nil {
		testTransitionHooks.afterPhase1()
	}

	// Phase 2: delete worktree best-effort (outside lock).
	worktreeDeleted := false
	if worktreePath != "" {
		gw := git.New(projectRoot)
		if rmErr := gw.RemoveWorktree(taskID); rmErr != nil {
			log.Printf("WARNING: failed to remove worktree for task %s: %v", taskID, rmErr)
		} else {
			worktreeDeleted = true
		}
	}

	// Add attempt-boundary transition: original status → initial status.
	// This transition is specific to the attempt boundary and not part of
	// the standard pipeline transitions (e.g. REJECTED → initial).
	pb.transitions[originalStatus] = append(pb.transitions[originalStatus], initialStatus)

	// Phase 3: release sentinel, make task claimable.
	err = lifecycleMutation(bb, authority)(func(state *models.State) error {
		task := state.FindTask(taskID)
		if task == nil {
			return &errors.NotFoundError{Entity: "task", ID: taskID}
		}

		// Re-check sentinel — concurrent modification detection.
		if task.AssignedTo == nil || *task.AssignedTo != transitioning {
			actual := "<nil>"
			if task.AssignedTo != nil {
				actual = *task.AssignedTo
			}
			return fmt.Errorf("sentinel replaced: expected %s, got %s (concurrent modification)", transitioning, actual)
		}

		// Release reviewer agent before clearing task fields (same transaction).
		if task.ReviewingBy != nil {
			state.ReleaseAgent(*task.ReviewingBy)
		}

		task.AssignedTo = nil
		models.AdvanceLifecycle(task)
		task.RejectionReason = nil
		task.Worktree = nil
		task.BaseCommit = nil
		task.ReviewingBy = nil
		task.ReviewLeaseExpires = nil
		clearAttemptState(task, attemptStateInitialReset)

		if err := task.TransitionWith(initialStatus, pb.transitions); err != nil {
			return err
		}
		lifecycleOutcome = NewLifecycleOutcome("transition-attempt", task, models.LifecycleCompleted, "continue", "committed")
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("phase 3 failed: %w", err)
	}

	return &TransitionAttemptResult{
		LifecycleOutcome: lifecycleOutcome,
		TaskID:           taskID,
		NewAttempt:       2,
		WorktreeDeleted:  worktreeDeleted,
		InitialStatus:    initialStatus,
	}, nil
}
