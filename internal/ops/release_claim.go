package ops

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/roles"
	"github.com/liza-mas/liza/internal/statevalidate"
)

// releaseClaimOperation names the release-claim operation.
const releaseClaimOperation = "release-claim"

// ReleaseClaimResult contains the outcome of releasing a claim.
type ReleaseClaimResult struct {
	models.LifecycleOutcome
	TaskID           string `json:"task_id"`
	Role             string `json:"role"`
	ReleasedReviewer bool   `json:"released_reviewer"`
	ReleasedDoer     bool   `json:"released_doer"`
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
	return ReleaseClaimWithRequest(projectRoot, taskID, role, force, reason, agentID, authority, LifecycleRequestOptions{})
}

// ReleaseClaimWithRequest binds release retries to the inspected ownership
// boundary. Legacy human/admin callers may omit authority and request options.
func ReleaseClaimWithRequest(projectRoot, taskID, role string, force bool, reason, agentID string, authority *models.AgentAuthority, opts LifecycleRequestOptions) (result *ReleaseClaimResult, err error) {
	invocation := &ownershipInvocation{operation: releaseClaimOperation, opts: opts, authority: authority, metrics: NewLifecycleInvocation(projectRoot)}
	defer func() { err = invocation.finish(projectRoot, err) }()
	if taskID == "" {
		return nil, &PreconditionError{Reason: "task ID is required"}
	}
	if claim := roles.ClaimType(role); claim != roles.ClaimReviewer && claim != roles.ClaimDoer && claim != roles.ClaimBoth {
		return nil, &PreconditionError{Reason: fmt.Sprintf("role must be reviewer, doer, or both, got: %s", role)}
	}
	if err := ValidateLifecycleRequestOptions(opts); err != nil {
		return nil, &PreconditionError{Reason: err.Error()}
	}
	err = withOwnershipTaskLock(projectRoot, taskID, releaseClaimOperation, func() error {
		var inner error
		result, inner = releaseClaimLocked(projectRoot, taskID, role, force, reason, agentID, authority, invocation)
		return inner
	})
	return result, err
}

// releaseClaimRun is the normalised input and resolved pipeline of one locked release.
type releaseClaimRun struct {
	projectRoot, taskID, role, reason, agentID string
	force                                      bool
	authority                                  *models.AgentAuthority
	invocation                                 *ownershipInvocation
	transitions                                map[models.TaskStatus][]models.TaskStatus
	resolver                                   *pipeline.Resolver
	now                                        time.Time
}

func releaseClaimLocked(projectRoot, taskID, role string, force bool, reason, agentID string, authority *models.AgentAuthority, invocation *ownershipInvocation) (*ReleaseClaimResult, error) {
	if taskID == "" {
		return nil, &PreconditionError{Reason: "task ID is required"}
	}
	if authority != nil {
		if err := requireAuthorityActor(*authority, agentID); err != nil {
			return nil, err
		}
	}

	if claim := roles.ClaimType(role); claim != roles.ClaimReviewer && claim != roles.ClaimDoer && claim != roles.ClaimBoth {
		return nil, &PreconditionError{Reason: fmt.Sprintf("role must be reviewer, doer, or both, got: %s", role)}
	}

	if agentID == "" {
		agentID = actorHuman
	}

	if reason == "" {
		reason = "manual release"
	}
	lp := paths.New(projectRoot)
	bb := RequestBlackboard(lp.StatePath(), authority, invocation.opts)

	now := time.Now().UTC()

	// Load pipeline resolver for status resolution
	resolver, _, resolverErr := loadResolver(projectRoot)
	if resolverErr != nil {
		return nil, fmt.Errorf("failed to load pipeline config: %w", resolverErr)
	}
	run := releaseClaimRun{projectRoot: projectRoot, taskID: taskID, role: role, reason: reason, agentID: agentID, force: force, authority: authority, invocation: invocation, transitions: BuildPipelineTransitions(resolver), resolver: resolver, now: now}

	var released models.LifecycleProjection
	err := lifecycleMutation(bb, authority)(func(state *models.State) error {
		return releaseClaimTransaction(state, bb, run, &released)
	})

	if err != nil && !isLifecycleReplay(err) {
		return nil, fmt.Errorf("failed to release claim: %w", err)
	}

	// Worktree and branch cleanup is deliberately deferred to the next ClaimTask,
	// which removes stale worktrees/branches in handleReadyClaimWorktree before
	// creating new ones. This avoids a race where ReleaseClaim's post-lock
	// cleanup deletes a worktree that a concurrent ClaimTask just created.
	// Orphaned worktrees in .worktrees/ are gitignored and harmless until re-claimed.
	// See handleReadyClaimWorktree in claim_task.go for the cleanup path.
	if released.ReleasedDoer && !isLifecycleReplay(err) {
		log.Printf("INFO: release-claim %s: worktree cleanup deferred to next claim", taskID)
	}

	return &ReleaseClaimResult{
		LifecycleOutcome: invocation.outcome,
		TaskID:           taskID,
		Role:             role,
		ReleasedReviewer: released.ReleasedReviewer,
		ReleasedDoer:     released.ReleasedDoer,
	}, nil
}

// releaseClaimTransaction is the locked release callback: request identity and
// replay, reviewer then doer release, rejected-handoff candidate validation and
// receipt completion. It starts no Git or process effect.
func releaseClaimTransaction(state *models.State, bb *db.Blackboard, run releaseClaimRun, released *models.LifecycleProjection) error {
	taskID, agentID, invocation := run.taskID, run.agentID, run.invocation
	task := state.FindTask(taskID)
	if task == nil {
		return &errors.NotFoundError{Entity: notFoundEntityTask, ID: taskID}
	}
	invocation.observe(task)
	request, err := NewLifecycleRequest(releaseClaimOperation, task, agentID, run.authority, invocation.opts, struct {
		Role   string
		Force  bool
		Reason string
	}{run.role, run.force, run.reason})
	if err != nil {
		return err
	}
	invocation.request = request
	receipt, err := checkOwnerEndingRequest(task, request)
	if err != nil {
		return err
	}
	if receipt != nil {
		invocation.outcome = LifecycleReplayOutcome(task, receipt, agentID)
		released.ReleasedDoer, released.ReleasedReviewer = receipt.Projection.ReleasedDoer, receipt.Projection.ReleasedReviewer
		return errLifecycleReplay
	}

	validateRejectedHandoff, err := releaseClaimRoles(state, task, run, released)
	if err != nil {
		return err
	}
	if !released.ReleasedReviewer && !released.ReleasedDoer {
		return &LifecycleError{Outcome: NewLifecycleOutcome(releaseClaimOperation, task, models.LifecycleAlreadyTransitioned, LifecycleSafeActionStop, LifecycleEffectsNone), Err: &PreconditionError{Reason: fmt.Sprintf("no claims to release for task %s", taskID)}}
	}
	if validateRejectedHandoff {
		if err := statevalidate.ValidateCandidate(state, bb.ReadSnapshot, run.projectRoot, true, os.Stderr); err != nil {
			return fmt.Errorf("released rejected claim produced invalid state: %w", err)
		}
	}

	invocation.outcome, err = CompleteLifecycleRequest(task, request, *released, state.Agents)
	if err == nil {
		invocation.effects = true
	}
	return err
}

// releaseClaimRoles releases the reviewer claim, then the doer claim, as the
// requested role selects. It reports whether a rejected doer handoff was
// released and so needs candidate validation.
func releaseClaimRoles(state *models.State, task *models.Task, run releaseClaimRun, released *models.LifecycleProjection) (validateRejectedHandoff bool, err error) {
	claim := roles.ClaimType(run.role)
	if claim == roles.ClaimReviewer || claim == roles.ClaimBoth {
		effectiveReviewerRelease, err := resolveReviewerClaimReleaseStatus(task, run.resolver)
		if err != nil {
			return false, err
		}
		releasedReviewer, err := releaseOneClaim(state, task, effectiveReviewerRelease, run.transitions, run.force, run.agentID, run.reason, run.now)
		if err != nil {
			return false, err
		}
		released.ReleasedReviewer = releasedReviewer
	}
	if claim == roles.ClaimDoer || claim == roles.ClaimBoth {
		effectiveCoderRelease := resolveDoerClaimReleaseStatus(task, run.resolver)
		releasedDoer, err := releaseOneClaim(state, task, effectiveCoderRelease, run.transitions, run.force, run.agentID, run.reason, run.now)
		if err != nil {
			return false, err
		}
		released.ReleasedDoer = releasedDoer
		validateRejectedHandoff = releasedDoer && effectiveCoderRelease.validateRejectedHandoff
	}
	return validateRejectedHandoff, nil
}
