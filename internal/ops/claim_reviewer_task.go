package ops

import (
	stderrors "errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/identity"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/roles"
)

// defaultReviewClaimCooldown is the duration after a review claim release
// during which the same agent cannot re-claim the same task. This prevents
// claim-release spin loops caused by repeated failures (worktree errors,
// CLI exits, supervisor restarts).
const defaultReviewClaimCooldown = 60 * time.Second

// ClaimReviewerTaskInput contains the parameters for claiming a reviewer task.
type ClaimReviewerTaskInput struct {
	ProjectRoot   string
	AgentID       string
	Role          string
	LeaseDuration int
}

// ClaimReviewerTaskResult contains the outcome of a successful reviewer task claim.
type ClaimReviewerTaskResult struct {
	TaskID       string
	Worktree     string
	ReviewCommit string
	LeaseExpires time.Time
}

// ClaimReviewerTask finds and claims a reviewable task for a code-reviewer agent.
// It atomically transitions the task to REVIEWING (or REVIEWING_2 for partially-
// approved tasks), assigns the reviewer, and updates the agent status.
//
// Claim priority: partially_approved candidates are selected before submitted
// candidates at the same priority level. Within each status tier, provider
// diversity is used as a soft preference for candidate selection.
func ClaimReviewerTask(input ClaimReviewerTaskInput) (*ClaimReviewerTaskResult, error) {
	if input.AgentID == "" {
		return nil, &PreconditionError{Reason: "agent ID is required"}
	}
	if input.LeaseDuration <= 0 {
		input.LeaseDuration = models.DefaultLeaseDurationSeconds
	}

	role := input.Role
	if role == "" {
		// Infer role from agent ID; default to code reviewer.
		inferred, err := identity.ExtractRole(input.AgentID)
		if err == nil && roles.IsValid(inferred) {
			role = inferred
		}
		if role == "" {
			role = models.RoleCodeReviewer
		}
	}

	lp := paths.New(input.ProjectRoot)
	bb := db.For(lp.StatePath())

	now := time.Now().UTC()
	leaseExpires := now.Add(time.Duration(input.LeaseDuration) * time.Second)

	var result ClaimReviewerTaskResult
	var reviewBoundaryErr error
	var repairNeededTaskIDs []string

	// Load pipeline config once for both IsClaimable and transition.
	pb, err := loadPipelineBundle(input.ProjectRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load pipeline config: %w", err)
	}
	pr := pb.pr

	err = bb.Modify(func(state *models.State) error {
		claimingAgent, err := requireRegisteredClaimAgent(state, input.AgentID, role)
		if err != nil {
			return err
		}

		// Find reviewable task with highest priority
		var candidates []*models.Task
		for i := range state.Tasks {
			if state.Tasks[i].IsClaimable(role, state.Tasks, pr) {
				candidates = append(candidates, &state.Tasks[i])
			}
		}

		if len(candidates) == 0 {
			return &PreconditionError{Reason: "no reviewable tasks found"}
		}

		// Independent-review filter: skip tasks the claiming agent already
		// approved. Without this, a reviewer-1 polling loop would re-claim
		// its own partially_approved task and self-rubber-stamp the quorum.
		candidates = filterAlreadyApprovedByAgent(candidates, input.AgentID)
		if len(candidates) == 0 {
			return &PreconditionError{Reason: "no reviewable tasks found (all candidates already approved by claimer)"}
		}

		// Filter out candidates in claim cooldown to prevent claim-release spin.
		candidates = filterReviewClaimCooldown(candidates, input.AgentID, defaultReviewClaimCooldown, now)
		if len(candidates) == 0 {
			return &PreconditionError{Reason: "all reviewable tasks in claim cooldown"}
		}

		// Look up claiming reviewer's provider from agent state.
		claimerProvider := claimingAgent.Provider

		// Filter by doer-provider diversity: when provider-diversity is configured,
		// block reviewers that share the doer's provider if a different-provider
		// reviewer is registered (even if busy).
		candidates = filterDoerProviderDiversity(candidates, claimerProvider, input.AgentID, state, pb.resolver)
		if len(candidates) == 0 {
			return &PreconditionError{Reason: "no reviewable tasks found"}
		}

		for len(candidates) > 0 {
			task := selectBestCandidate(candidates, pr, claimerProvider, input.AgentID, state)
			if task == nil {
				break
			}

			if err := validateReviewBoundaryForAssignment(input.ProjectRoot, task, state.Config.IntegrationBranch); err != nil {
				reviewBoundaryErr = err
				var repairNeeded *ReviewBoundaryRepairNeededError
				if stderrors.As(err, &repairNeeded) {
					repairNeededTaskIDs = append(repairNeededTaskIDs, task.ID)
					candidates = removeCandidate(candidates, task)
					continue
				}
				if markErr := markReviewBoundaryIntegrationFailed(state, task, input.AgentID, pb.transitions, err); markErr != nil {
					return markErr
				}
				candidates = removeCandidate(candidates, task)
				continue
			}

			if task.RolePair == "" {
				return &PreconditionError{Reason: fmt.Sprintf("task %s has no role_pair set", task.ID)}
			}

			// Determine target reviewing status based on task's current state.
			targetStatus, err := resolveReviewingTarget(task, pr)
			if err != nil {
				return err
			}
			if err := task.TransitionWith(targetStatus, pb.transitions); err != nil {
				return err
			}
			task.ReviewingBy = &input.AgentID
			task.ReviewLeaseExpires = &leaseExpires

			agent := claimingAgent
			agent.Status = models.AgentStatusReviewing
			currentTask := task.ID
			agent.CurrentTask = &currentTask
			agent.Heartbeat = now
			agent.LeaseExpires = &leaseExpires
			state.Agents[input.AgentID] = agent

			result.TaskID = task.ID
			if task.Worktree != nil {
				result.Worktree = *task.Worktree
			}
			if task.ReviewCommit != nil {
				result.ReviewCommit = *task.ReviewCommit
			}
			result.LeaseExpires = leaseExpires

			return nil
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	if reviewBoundaryErr != nil && result.TaskID == "" {
		if len(repairNeededTaskIDs) > 0 {
			return nil, &PreconditionError{
				Reason: fmt.Sprintf(
					"review boundary needs repair for task(s): %s — run %s <task-id>",
					strings.Join(repairNeededTaskIDs, ", "),
					brand.Command("update-review-commit"),
				),
			}
		}
		return nil, &IntegrationFailedError{Reason: IntegrationReasonReviewBoundaryMismatch}
	}

	return &result, nil
}

func removeCandidate(candidates []*models.Task, candidate *models.Task) []*models.Task {
	for i, task := range candidates {
		if task == candidate {
			return append(candidates[:i], candidates[i+1:]...)
		}
	}
	return candidates
}

// resolveReviewingTarget returns the appropriate reviewing status for the task:
// - submitted → reviewing (first review)
// - partially_approved → reviewing_2 (second review)
func resolveReviewingTarget(task *models.Task, pr models.PipelineResolver) (models.TaskStatus, error) {
	partiallyApproved, paErr := pr.PartiallyApprovedStatus(task.RolePair)
	if paErr == nil && task.Status == partiallyApproved {
		reviewing2, err := pr.Reviewing2Status(task.RolePair)
		if err != nil {
			return "", fmt.Errorf("failed to resolve reviewing-2 status for role-pair %q: %w", task.RolePair, err)
		}
		return reviewing2, nil
	}
	reviewing, err := pr.ReviewingStatus(task.RolePair)
	if err != nil {
		return "", fmt.Errorf("failed to resolve reviewing status for role-pair %q: %w", task.RolePair, err)
	}
	return reviewing, nil
}

// selectBestCandidate picks the best candidate from a list of claimable tasks.
// Selection order:
// 1. Top priority tier (lowest priority number)
// 2. Partially_approved tasks preferred over submitted tasks (claim priority)
// 3. Provider diversity as soft preference within each status group
// 4. Random selection among remaining equally-preferred candidates
func selectBestCandidate(
	candidates []*models.Task,
	pr models.PipelineResolver,
	claimerProvider string,
	claimerAgentID string,
	state *models.State,
) *models.Task {
	tier := models.TopPriorityTier(candidates)
	if len(tier) == 0 {
		return nil
	}

	// Split into partially_approved and submitted groups.
	var partiallyApprovedTasks, submittedTasks []*models.Task
	for _, t := range tier {
		pa, err := pr.PartiallyApprovedStatus(t.RolePair)
		if err == nil && t.Status == pa {
			partiallyApprovedTasks = append(partiallyApprovedTasks, t)
		} else {
			submittedTasks = append(submittedTasks, t)
		}
	}

	// Prefer partially_approved tasks (claim priority).
	if len(partiallyApprovedTasks) > 0 {
		return pickWithApprovalDiversity(partiallyApprovedTasks, claimerProvider)
	}

	// Fall back to submitted tasks with fresh-submission diversity.
	return pickWithFreshDiversity(submittedTasks, claimerProvider, claimerAgentID, pr, state)
}

// pickWithApprovalDiversity selects from partially_approved tasks, preferring
// tasks where the claimer's provider differs from existing approvals.
func pickWithApprovalDiversity(tasks []*models.Task, claimerProvider string) *models.Task {
	// No diversity preference possible: single candidate, or claimer has no
	// provider configured (falls back to random selection).
	if claimerProvider == "" || len(tasks) <= 1 {
		return pickRandom(tasks)
	}

	var diverse, same []*models.Task
	for _, t := range tasks {
		if hasDifferentProvider(t.Approvals, claimerProvider) {
			diverse = append(diverse, t)
		} else {
			same = append(same, t)
		}
	}

	if len(diverse) > 0 {
		return pickRandom(diverse)
	}
	return pickRandom(same)
}

// hasDifferentProvider returns true if any existing approval on the task was
// made by a provider different from the claiming reviewer's provider.
func hasDifferentProvider(approvals []models.Approval, claimerProvider string) bool {
	for _, a := range approvals {
		if a.Provider != claimerProvider {
			return true
		}
	}
	return false
}

// pickWithFreshDiversity selects from submitted tasks (no existing approvals),
// preferring tasks where provider diversity is satisfiable from the reviewer pool.
// Diversity is satisfiable when at least one other registered reviewer for the
// role-pair has a provider different from the claiming reviewer's provider.
func pickWithFreshDiversity(
	tasks []*models.Task,
	claimerProvider string,
	claimerAgentID string,
	pr models.PipelineResolver,
	state *models.State,
) *models.Task {
	// No diversity preference possible: single candidate, or claimer has no
	// provider configured (falls back to random selection).
	if claimerProvider == "" || len(tasks) <= 1 {
		return pickRandom(tasks)
	}

	var preferred, rest []*models.Task
	for _, t := range tasks {
		if isDiversitySatisfiable(t, claimerProvider, claimerAgentID, pr, state) {
			preferred = append(preferred, t)
		} else {
			rest = append(rest, t)
		}
	}

	if len(preferred) > 0 {
		return pickRandom(preferred)
	}
	return pickRandom(rest)
}

// isDiversitySatisfiable checks if at least one other valid registered reviewer
// for the task's role-pair has a provider different from the claiming reviewer.
func isDiversitySatisfiable(
	task *models.Task,
	claimerProvider string,
	claimerAgentID string,
	pr models.PipelineResolver,
	state *models.State,
) bool {
	reviewerRole, err := pr.ReviewerRole(task.RolePair)
	if err != nil {
		return false
	}

	now := time.Now().UTC()
	for agentID, agent := range state.Agents {
		if agentID == claimerAgentID {
			continue
		}
		// Check if this agent is a reviewer for the same role-pair.
		// agent.Role stores the runtime role name (e.g., "code-reviewer"),
		// which matches the format returned by pr.ReviewerRole().
		if !hasReviewerCapacity(agent, reviewerRole, now) {
			continue
		}
		if agent.Provider != claimerProvider {
			return true
		}
	}
	return false
}

// filterAlreadyApprovedByAgent removes candidates where the given agent has
// already recorded an approval. A reviewer must not be allowed to re-claim a
// task they already approved — the round-2 verdict has to come from an
// independent reviewer (quorum > 1 contract). Without this filter, the
// agent loop happily picks up its own partially_approved task during round
// 2 polling and double-counts the same approval.
func filterAlreadyApprovedByAgent(candidates []*models.Task, agentID string) []*models.Task {
	if agentID == "" {
		return candidates
	}
	var filtered []*models.Task
	for _, t := range candidates {
		if t.HasApprovalFromAgent(agentID) {
			continue
		}
		filtered = append(filtered, t)
	}
	return filtered
}

// filterReviewClaimCooldown removes candidates where the claiming agent has a
// recent claim_released or review_claim_released history event within the
// cooldown window. This prevents claim-release spin loops.
func filterReviewClaimCooldown(candidates []*models.Task, agentID string, cooldown time.Duration, now time.Time) []*models.Task {
	cutoff := now.Add(-cooldown)
	var filtered []*models.Task
	for _, t := range candidates {
		if isInReviewClaimCooldown(t, agentID, cutoff) {
			continue
		}
		filtered = append(filtered, t)
	}
	return filtered
}

// isInReviewClaimCooldown checks if the task has a recent claim release event
// from the specified agent within the cutoff time.
func isInReviewClaimCooldown(task *models.Task, agentID string, cutoff time.Time) bool {
	for i := len(task.History) - 1; i >= 0; i-- {
		h := task.History[i]
		if h.Time.Before(cutoff) {
			break
		}
		if h.Agent != nil && *h.Agent == agentID &&
			(h.Event == models.TaskEventClaimReleased || h.Event == models.TaskEventReviewClaimReleased) {
			return true
		}
	}
	return false
}

// filterDoerProviderDiversity removes candidates where the claiming reviewer
// shares the doer's provider and a different-provider reviewer is registered.
// When provider-diversity is not configured for a task's role-pair, the task
// is always kept. When the doer's agent is no longer in state, the filter
// is skipped for that task.
func filterDoerProviderDiversity(
	candidates []*models.Task,
	claimerProvider string,
	claimerAgentID string,
	state *models.State,
	resolver interface {
		ProviderDiversity(string, string) (string, error)
		ReviewerRole(string) (string, error)
	},
) []*models.Task {
	if claimerProvider == "" {
		return candidates
	}

	var filtered []*models.Task
	for _, t := range candidates {
		if isBlockedByDoerDiversity(t, claimerProvider, claimerAgentID, state, resolver) {
			continue
		}
		filtered = append(filtered, t)
	}
	return filtered
}

// isBlockedByDoerDiversity returns true when all of the following hold:
//  1. provider-diversity: preferred is configured for the task's effective impact level
//  2. the claiming reviewer's provider matches the doer's provider
//  3. at least one registered reviewer (for the role-pair) has a different provider
func isBlockedByDoerDiversity(
	task *models.Task,
	claimerProvider string,
	claimerAgentID string,
	state *models.State,
	resolver interface {
		ProviderDiversity(string, string) (string, error)
		ReviewerRole(string) (string, error)
	},
) bool {
	// Resolve effective impact from task history, then check if provider-diversity
	// is configured at that impact level (with base-level fallthrough).
	effectiveImpact := ResolveEffectiveImpact(task.History)
	diversity, err := resolver.ProviderDiversity(task.RolePair, effectiveImpact)
	if err != nil || diversity != "preferred" {
		return false
	}

	// Look up the doer's provider. Skip filter if doer is not in state.
	if task.AssignedTo == nil {
		return false
	}
	doerAgent, ok := state.Agents[*task.AssignedTo]
	if !ok || doerAgent.Provider == "" {
		return false
	}

	// Claimer has a different provider than the doer — no block.
	if claimerProvider != doerAgent.Provider {
		return false
	}

	// Claimer shares the doer's provider. Block only if a valid
	// different-provider reviewer is registered for this role-pair (even if busy).
	reviewerRole, err := resolver.ReviewerRole(task.RolePair)
	if err != nil {
		return false
	}
	now := time.Now().UTC()
	for agentID, agent := range state.Agents {
		if agentID == claimerAgentID {
			continue
		}
		if !hasReviewerCapacity(agent, reviewerRole, now) {
			continue
		}
		if agent.Provider != doerAgent.Provider {
			return true
		}
	}
	return false
}

// pickRandom selects a random task from the slice. Returns nil if empty.
func pickRandom(tasks []*models.Task) *models.Task {
	if len(tasks) == 0 {
		return nil
	}
	return tasks[rand.IntN(len(tasks))]
}
