package ops

import (
	"fmt"
	"io"
	"log"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/roles"
	"github.com/liza-mas/liza/internal/statevalidate"
)

// ReleaseClaimResult contains the outcome of releasing a claim.
type ReleaseClaimResult struct {
	TaskID           string `json:"task_id"`
	Role             string `json:"role"`
	ReleasedReviewer bool   `json:"released_reviewer"`
	ReleasedDoer     bool   `json:"released_doer"`
}

// claimRelease describes the field access pattern for one role's claim on a task.
type claimRelease struct {
	hasClaimFn              func(*models.Task) bool
	agentFieldFn            func(*models.Task) *string
	leaseFieldFn            func(*models.Task) *time.Time
	activeStatus            models.TaskStatus
	releasedStatus          models.TaskStatus
	eventName               string
	clearFn                 func(*models.Task)
	missingLeaseMsg         string
	activeLeaseMsg          string
	validateRejectedHandoff bool
}

var reviewerRelease = claimRelease{
	hasClaimFn:      func(t *models.Task) bool { return t.ReviewingBy != nil || t.ReviewLeaseExpires != nil },
	agentFieldFn:    func(t *models.Task) *string { return t.ReviewingBy },
	leaseFieldFn:    func(t *models.Task) *time.Time { return t.ReviewLeaseExpires },
	activeStatus:    models.TaskStatusReviewing,
	releasedStatus:  models.TaskStatusReadyForReview,
	eventName:       "review_claim_released",
	clearFn:         func(t *models.Task) { t.ReviewingBy = nil; t.ReviewLeaseExpires = nil },
	missingLeaseMsg: "review lease expires missing for task %s, use --force to clear",
	activeLeaseMsg:  "review lease still valid until %s, use --force to clear",
}

var doerRelease = claimRelease{
	hasClaimFn:     func(t *models.Task) bool { return t.AssignedTo != nil || t.LeaseExpires != nil },
	agentFieldFn:   func(t *models.Task) *string { return t.AssignedTo },
	leaseFieldFn:   func(t *models.Task) *time.Time { return t.LeaseExpires },
	activeStatus:   models.TaskStatusImplementing,
	releasedStatus: models.TaskStatusReady,
	eventName:      "doer_claim_released",
	clearFn: func(t *models.Task) {
		t.AssignedTo = nil
		t.LeaseExpires = nil
		t.Worktree = nil
		t.BaseCommit = nil
		t.Iteration = 0
		clearAttemptState(t, attemptStateClaimReleaseReset)
	},
	missingLeaseMsg: "lease expires missing for task %s, use --force to clear",
	activeLeaseMsg:  "doer lease still valid until %s, use --force to clear",
}

// ResolveReleaseStatuses returns the active/released status pairs for doer and
// reviewer claims, resolving from the pipeline config.
// Returns zero-value statuses when task has no RolePair or resolver is nil.
// This legacy convenience helper intentionally drops reviewer release resolution
// errors. Mutating reviewer-release paths must use ResolveReviewerReleaseStatus
// so they fail closed instead of clearing ownership in-place.
func ResolveReleaseStatuses(task *models.Task, resolver *pipeline.Resolver) (doerActive, doerReleased, reviewerActive, reviewerReleased models.TaskStatus) {
	doerActive, doerReleased = ResolveDoerReleaseStatus(task, resolver)
	reviewerActive, reviewerReleased, _ = ResolveReviewerReleaseStatus(task, resolver)
	return
}

// ResolveDoerReleaseStatus returns the active/released status pair for a doer
// claim, resolving from the pipeline config when available.
func ResolveDoerReleaseStatus(task *models.Task, resolver models.PipelineResolver) (active, released models.TaskStatus) {
	if task.RolePair == "" || resolver == nil {
		return
	}
	initial, initialErr := resolver.InitialStatus(task.RolePair)
	executing, executingErr := resolver.ExecutingStatus(task.RolePair)
	if initialErr == nil && executingErr == nil {
		active = executing
		released = initial
	}
	return
}

// ResolveReviewerReleaseStatus returns the active/released status pair for an
// active reviewer claim. Non-reviewing statuses return zero values with no
// error so passive/orphaned review fields can still be cleared. Reviewing states
// fail closed when their release target cannot be resolved.
func ResolveReviewerReleaseStatus(task *models.Task, resolver models.PipelineResolver) (active, released models.TaskStatus, err error) {
	if task.RolePair == "" || resolver == nil {
		return
	}

	reviewing, reviewingErr := resolver.ReviewingStatus(task.RolePair)
	if reviewingErr == nil && task.Status == reviewing {
		submitted, submittedErr := resolver.SubmittedStatus(task.RolePair)
		if submittedErr != nil {
			return "", "", fmt.Errorf("task %s is in reviewing state %s but submitted status resolution failed for role-pair %q: %w",
				task.ID, task.Status, task.RolePair, submittedErr)
		}
		return reviewing, submitted, nil
	}

	reviewing2, reviewing2Err := resolver.Reviewing2Status(task.RolePair)
	if reviewing2Err == nil && task.Status == reviewing2 {
		partiallyApproved, partiallyApprovedErr := resolver.PartiallyApprovedStatus(task.RolePair)
		if partiallyApprovedErr != nil {
			return "", "", fmt.Errorf("task %s is in reviewing-2 state %s but partially-approved status resolution failed for role-pair %q: %w",
				task.ID, task.Status, task.RolePair, partiallyApprovedErr)
		}
		return reviewing2, partiallyApproved, nil
	}

	return
}

// resolveDoerClaimReleaseStatus returns the doer claimRelease config with
// pipeline-resolved active/released statuses when the task has a RolePair and a
// resolver is available.
func resolveDoerClaimReleaseStatus(task *models.Task, resolver models.PipelineResolver) claimRelease {
	doer := doerRelease
	doerActive, doerReleased := ResolveDoerReleaseStatus(task, resolver)
	if doerActive != "" && doerReleased != "" {
		doer.activeStatus = doerActive
		doer.releasedStatus = doerReleased
	}
	rejected := isRejectedDoerRelease(task, resolver)
	doer.validateRejectedHandoff = rejected
	preserveAttemptEvidence := rejected || task.Status == models.TaskStatusMerged
	if preserveAttemptEvidence {
		// Completed output and review/merge evidence outlive ownership. A
		// merged task is not starting a new attempt when its claim is released.
		doer.clearFn = func(t *models.Task) {
			t.AssignedTo = nil
			t.LeaseExpires = nil
		}
	}
	return doer
}

func isRejectedDoerRelease(task *models.Task, resolver models.PipelineResolver) bool {
	if task.Status == models.TaskStatusRejected || task.Status == models.TaskStatusCodingPlanRejected {
		return true
	}
	if task.RolePair == "" || resolver == nil {
		return false
	}
	rejected, err := resolver.RejectedStatus(task.RolePair)
	return err == nil && task.Status == rejected
}

// resolveReviewerClaimReleaseStatus returns the reviewer claimRelease config
// with pipeline-resolved active/released statuses. It returns an error when an
// active reviewing status is recognized but its release target is unavailable.
func resolveReviewerClaimReleaseStatus(task *models.Task, resolver models.PipelineResolver) (claimRelease, error) {
	reviewer := reviewerRelease
	if resolver == nil && (task.ReviewingBy != nil || task.ReviewLeaseExpires != nil) {
		return reviewer, fmt.Errorf("cannot release reviewer claim for task %s: pipeline resolver not loaded", task.ID)
	}
	reviewerActive, reviewerReleased, err := ResolveReviewerReleaseStatus(task, resolver)
	if err != nil {
		return reviewer, err
	}
	if reviewerActive != "" && reviewerReleased != "" {
		reviewer.activeStatus = reviewerActive
		reviewer.releasedStatus = reviewerReleased
	}
	return reviewer, nil
}

// releaseOneClaim executes the 9-step release sequence for a single role's claim.
// pipelineTransitions, if non-nil, overrides the default transition map.
// Returns true if a claim was released.
func releaseOneClaim(state *models.State, task *models.Task, cfg claimRelease, pipelineTransitions map[models.TaskStatus][]models.TaskStatus, force bool, agentID, reason string, now time.Time) (bool, error) {
	if !cfg.hasClaimFn(task) {
		return false, nil
	}

	agent := cfg.agentFieldFn(task)
	lease := cfg.leaseFieldFn(task)

	if agent != nil && lease == nil && !force {
		return false, &PreconditionError{Reason: fmt.Sprintf(cfg.missingLeaseMsg, task.ID)}
	}

	if lease != nil && !force {
		if lease.After(now) {
			return false, &PreconditionError{Reason: fmt.Sprintf(cfg.activeLeaseMsg, lease.Format(time.RFC3339))}
		}
	}

	if task.Status == cfg.activeStatus && pipelineTransitions != nil {
		if err := task.TransitionWith(cfg.releasedStatus, pipelineTransitions); err != nil {
			return false, err
		}
	}

	if agent != nil {
		if a, ok := state.Agents[*agent]; ok {
			if a.CurrentTask != nil && *a.CurrentTask == task.ID {
				state.ReleaseAgent(*agent)
			}
		}
	}

	cfg.clearFn(task)

	task.History = append(task.History, models.TaskHistoryEntry{
		Time:   now,
		Event:  cfg.eventName,
		Agent:  &agentID,
		Reason: &reason,
	})

	return true, nil
}

// ReleaseClaim releases reviewer, doer, or both claims on a task. Without
// force, refuses if lease is still valid. No terminal I/O.
func ReleaseClaim(projectRoot, taskID, role string, force bool, reason, agentID string) (*ReleaseClaimResult, error) {
	return releaseClaim(projectRoot, taskID, role, force, reason, agentID, nil)
}

// ReleaseClaimWithAuthority fences a supervisor-driven claim release with the
// caller's current registration generation. The legacy entry point remains
// available for explicit audit-only human/admin recovery.
func ReleaseClaimWithAuthority(projectRoot, taskID, role string, force bool, reason string, authority models.AgentAuthority) (*ReleaseClaimResult, error) {
	return releaseClaim(projectRoot, taskID, role, force, reason, authority.ID, &authority)
}

func releaseClaim(projectRoot, taskID, role string, force bool, reason, agentID string, authority *models.AgentAuthority) (*ReleaseClaimResult, error) {
	if taskID == "" {
		return nil, &PreconditionError{Reason: "task ID is required"}
	}
	if authority != nil {
		if err := requireAuthorityActor(*authority, agentID); err != nil {
			return nil, err
		}
	}

	if role != roles.ClaimReviewer && role != roles.ClaimDoer && role != roles.ClaimBoth {
		return nil, &PreconditionError{Reason: fmt.Sprintf("role must be reviewer, doer, or both, got: %s", role)}
	}

	if agentID == "" {
		agentID = "human"
	}

	if reason == "" {
		reason = "manual release"
	}

	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())

	releasedReviewer := false
	releasedDoer := false

	now := time.Now().UTC()

	// Load pipeline resolver for status resolution
	resolver, _, resolverErr := loadResolver(projectRoot)
	if resolverErr != nil {
		return nil, fmt.Errorf("failed to load pipeline config: %w", resolverErr)
	}
	pipelineTransitions := BuildPipelineTransitions(resolver)

	err := lifecycleMutation(bb, authority)(func(state *models.State) error {
		task := state.FindTask(taskID)
		if task == nil {
			return &errors.NotFoundError{Entity: "task", ID: taskID}
		}

		if role == roles.ClaimReviewer || role == roles.ClaimBoth {
			effectiveReviewerRelease, err := resolveReviewerClaimReleaseStatus(task, resolver)
			if err != nil {
				return err
			}
			released, err := releaseOneClaim(state, task, effectiveReviewerRelease, pipelineTransitions, force, agentID, reason, now)
			if err != nil {
				return err
			}
			releasedReviewer = released
		}

		validateRejectedHandoff := false
		if role == roles.ClaimDoer || role == roles.ClaimBoth {
			effectiveCoderRelease := resolveDoerClaimReleaseStatus(task, resolver)
			released, err := releaseOneClaim(state, task, effectiveCoderRelease, pipelineTransitions, force, agentID, reason, now)
			if err != nil {
				return err
			}
			releasedDoer = released
			validateRejectedHandoff = released && effectiveCoderRelease.validateRejectedHandoff
		}

		if !releasedReviewer && !releasedDoer {
			return &PreconditionError{Reason: fmt.Sprintf("no claims to release for task %s", taskID)}
		}
		if validateRejectedHandoff {
			if err := statevalidate.ValidateState(state, projectRoot, true, io.Discard); err != nil {
				return fmt.Errorf("released rejected claim produced invalid state: %w", err)
			}
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to release claim: %w", err)
	}

	// Worktree and branch cleanup is deliberately deferred to the next ClaimTask,
	// which removes stale worktrees/branches in handleReadyClaimWorktree before
	// creating new ones. This avoids a race where ReleaseClaim's post-lock
	// cleanup deletes a worktree that a concurrent ClaimTask just created.
	// Orphaned worktrees in .worktrees/ are gitignored and harmless until re-claimed.
	// See handleReadyClaimWorktree in claim_task.go for the cleanup path.
	if releasedDoer {
		log.Printf("INFO: release-claim %s: worktree cleanup deferred to next claim", taskID)
	}

	return &ReleaseClaimResult{
		TaskID:           taskID,
		Role:             role,
		ReleasedReviewer: releasedReviewer,
		ReleasedDoer:     releasedDoer,
	}, nil
}
