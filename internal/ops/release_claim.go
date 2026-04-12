package ops

import (
	"fmt"
	"log"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/roles"
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
	hasClaimFn      func(*models.Task) bool
	agentFieldFn    func(*models.Task) *string
	leaseFieldFn    func(*models.Task) *time.Time
	activeStatus    models.TaskStatus
	releasedStatus  models.TaskStatus
	eventName       string
	clearFn         func(*models.Task)
	missingLeaseMsg string
	activeLeaseMsg  string
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
	},
	missingLeaseMsg: "lease expires missing for task %s, use --force to clear",
	activeLeaseMsg:  "doer lease still valid until %s, use --force to clear",
}

// ResolveReleaseStatuses returns the active/released status pairs for doer and
// reviewer claims, resolving from the pipeline config.
// Returns zero-value statuses when task has no RolePair or resolver is nil.
func ResolveReleaseStatuses(task *models.Task, resolver *pipeline.Resolver) (doerActive, doerReleased, reviewerActive, reviewerReleased models.TaskStatus) {
	if task.RolePair == "" || resolver == nil {
		return
	}
	initial, initialErr := resolver.InitialStatus(task.RolePair)
	executing, executingErr := resolver.ExecutingStatus(task.RolePair)
	if initialErr == nil && executingErr == nil {
		doerActive = executing
		doerReleased = initial
	}
	submitted, submittedErr := resolver.SubmittedStatus(task.RolePair)
	reviewing, reviewingErr := resolver.ReviewingStatus(task.RolePair)
	if submittedErr == nil && reviewingErr == nil {
		reviewerActive = reviewing
		reviewerReleased = submitted
	}
	return
}

// resolveClaimReleaseStatuses returns doer and reviewer claimRelease configs with
// pipeline-resolved active/released statuses when the task has a RolePair and a
// resolver is available.
func resolveClaimReleaseStatuses(task *models.Task, resolver *pipeline.Resolver) (doer claimRelease, reviewer claimRelease) {
	doer = doerRelease
	reviewer = reviewerRelease
	doer.activeStatus, doer.releasedStatus, reviewer.activeStatus, reviewer.releasedStatus = ResolveReleaseStatuses(task, resolver)
	return doer, reviewer
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
	if taskID == "" {
		return nil, &PreconditionError{Reason: "task ID is required"}
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

	err := bb.Modify(func(state *models.State) error {
		task := state.FindTask(taskID)
		if task == nil {
			return &errors.NotFoundError{Entity: "task", ID: taskID}
		}

		// Resolve pipeline-aware statuses for claim release
		effectiveCoderRelease, effectiveReviewerRelease := resolveClaimReleaseStatuses(task, resolver)

		if role == roles.ClaimReviewer || role == roles.ClaimBoth {
			released, err := releaseOneClaim(state, task, effectiveReviewerRelease, pipelineTransitions, force, agentID, reason, now)
			if err != nil {
				return err
			}
			releasedReviewer = released
		}

		if role == roles.ClaimDoer || role == roles.ClaimBoth {
			released, err := releaseOneClaim(state, task, effectiveCoderRelease, pipelineTransitions, force, agentID, reason, now)
			if err != nil {
				return err
			}
			releasedDoer = released
		}

		if !releasedReviewer && !releasedDoer {
			return &PreconditionError{Reason: fmt.Sprintf("no claims to release for task %s", taskID)}
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
