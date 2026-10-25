package ops

import (
	"fmt"
	"log"
	"slices"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

// recoverAgentClaims is the registration and task-claim evidence captured
// before any recovery effect; later transactions compare against it.
type recoverAgentClaims struct {
	agent     models.Agent
	exists    bool
	doers     []string
	reviewers []string
	all       []string
}

// captureRecoverAgentClaims snapshots the agent and its claims and requires the
// claim set to equal the task locks the caller holds.
func captureRecoverAgentClaims(state *models.State, agentID string, lockedIDs []string) (recoverAgentClaims, error) {
	agent, exists := state.Agents[agentID]
	doerTaskIDs, reviewerTaskIDs := TaskClaimsForAgent(state, agentID)
	claims := append(append([]string(nil), doerTaskIDs...), reviewerTaskIDs...)
	slices.Sort(claims)
	claims = slices.Compact(claims)
	if !slices.Equal(claims, lockedIDs) {
		return recoverAgentClaims{}, WrapLifecycleError("recover-agent", nil, fmt.Errorf("agent task claims changed before recovery"), models.LifecycleStateChanged, LifecycleSafeActionRequery, LifecycleEffectsNone)
	}
	return recoverAgentClaims{agent: agent, exists: exists, doers: doerTaskIDs, reviewers: reviewerTaskIDs, all: claims}, nil
}

// recoverAgentAnchor selects the task whose receipt anchors the request.
// Receipts are task scoped; an agent-wide or taskless request cannot invent a boundary.
func recoverAgentAnchor(state *models.State, agentID string, claims []string, opts LifecycleRequestOptions) (*models.Task, error) {
	var anchor *models.Task
	if len(claims) == 1 {
		anchor = state.FindTask(claims[0])
	}
	if opts.RequestID != "" && anchor == nil {
		for index := range state.Tasks {
			task := &state.Tasks[index]
			if task.Lifecycle == nil {
				continue
			}
			for _, receipt := range task.Lifecycle.Receipts {
				if receipt.Operation == "recover-agent" && receipt.Actor == agentID && receipt.RequestID == opts.RequestID && receipt.ExpectedTransition == opts.ExpectedTransition {
					if anchor != nil && anchor.ID != task.ID {
						return nil, &PreconditionError{Reason: "request_id requires one task boundary for recover-agent"}
					}
					anchor = task
				}
			}
		}
		if anchor == nil {
			return nil, &PreconditionError{Reason: "expected_transition requires a single affected task for recover-agent"}
		}
	}
	return anchor, nil
}

// recoverAgentRequest builds the anchored request and resolves a retained
// receipt; a non-nil replay result is the already-completed answer.
func recoverAgentRequest(anchor *models.Task, agentID string, force bool, reason string, opts LifecycleRequestOptions) (LifecycleRequest, *RecoverAgentResult, error) {
	request, err := NewLifecycleRequest("recover-agent", anchor, agentID, nil, opts, struct {
		Force  bool
		Reason string
	}{force, reason})
	if err != nil {
		return request, nil, err
	}
	receipt, err := checkOwnerEndingRequest(anchor, request)
	if err != nil {
		return request, nil, err
	}
	if receipt != nil {
		return request, &RecoverAgentResult{LifecycleOutcome: LifecycleReplayOutcome(anchor, receipt, agentID), AgentID: agentID, TaskID: anchor.ID, AlreadyClean: true}, nil
	}
	return request, nil, nil
}

// recoverAgentReleaseClaims releases the doer then reviewer claims inside the
// completion transaction and reports whether any failed release must keep the
// agent registered.
func recoverAgentReleaseClaims(state *models.State, run recoverAgentRun, doers, reviewers []string, now time.Time) (preserveAgent bool) {
	agentID, result, pipelineTransitions := run.agentID, run.result, run.transitions
	if run.resolver == nil {
		for _, ownedTaskID := range append(doers, reviewers...) {
			log.Printf("WARNING: recover-agent %s: claim release skipped for task %s — resolver not loaded", agentID, ownedTaskID)
			result.Warnings = append(result.Warnings, fmt.Sprintf("claim release skipped for task %s — resolver not loaded", ownedTaskID))
		}
		return len(doers) > 0 || len(reviewers) > 0
	}
	resolver := run.resolver
	reason := run.reason
	for _, ownedTaskID := range doers {
		task := state.FindTask(ownedTaskID)
		if task == nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("task %s not found in state", ownedTaskID))
			continue
		}
		effectiveCoderRelease := resolveDoerClaimReleaseStatus(task, resolver)
		released, err := releaseOneClaim(state, task, effectiveCoderRelease, pipelineTransitions, true, agentID, reason, now)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("doer claim release for %s: %v", ownedTaskID, err))
			preserveAgent = true
			continue
		}
		if released {
			result.ClaimReleased = true
			// Clear worktree reference since we removed it
			task.Worktree = nil
		}
	}
	for _, ownedTaskID := range reviewers {
		task := state.FindTask(ownedTaskID)
		if task == nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("task %s not found in state", ownedTaskID))
			continue
		}
		effectiveReviewerRelease, err := resolveReviewerClaimReleaseStatus(task, resolver)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("reviewer claim release for %s: %v", ownedTaskID, err))
			preserveAgent = true
			continue
		}
		released, err := releaseOneClaim(state, task, effectiveReviewerRelease, pipelineTransitions, true, agentID, reason, now)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("reviewer claim release for %s: %v", ownedTaskID, err))
			preserveAgent = true
			continue
		}
		if released {
			result.ClaimReleased = true
		}
	}
	return preserveAgent
}
