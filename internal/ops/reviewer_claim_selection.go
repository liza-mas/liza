package ops

import (
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/liza-mas/liza/internal/agentlaunch"
	lizaerrors "github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/rolemodels"
)

// Eligibility and selection of the reviewer claim: which tasks a registered
// reviewer may take (snapshot and gates) and which one it takes (priority,
// approval diversity, fresh diversity). The diversity comparisons live in the
// selection functions below and compare canonical provider keys, never the
// recorded tool names (ADR-0229).

// defaultReviewClaimCooldown is the duration after a review claim release
// during which the same agent cannot re-claim the same task. This prevents
// claim-release spin loops caused by repeated failures (worktree errors,
// CLI exits, supervisor restarts).
const defaultReviewClaimCooldown = 60 * time.Second

// ReviewerClaimPolicyResolver exposes the review-policy lookups needed to
// project whether a registered reviewer can claim a specific task.
type ReviewerClaimPolicyResolver interface {
	ProviderDiversity(string, pipeline.ReviewImpact) (pipeline.ProviderDiversityMode, error)
	ReviewerRole(string) (string, error)
}

// ReviewerClaimEligibilityInput contains the state and policy inputs for one
// reviewer/task eligibility projection.
type ReviewerClaimEligibilityInput struct {
	State        *models.State
	Task         *models.Task
	AgentID      string
	ReviewerRole string
	Now          time.Time
	Resolver     ReviewerClaimPolicyResolver
	// Slots is the reviewer role's models.yaml selection; only a list binds.
	Slots rolemodels.Selection
	// Keys derives the provider keys diversity compares, from the config of State.
	// The zero value compares recorded tool names exactly.
	Keys agentlaunch.ProviderKeys
}

type reviewerClaimEligibility struct {
	registrationErr        error
	alreadyApproved        bool
	inCooldown             bool
	wrongSlot              bool
	blockedByDoerDiversity bool
	validationFailed       bool
}

func (e reviewerClaimEligibility) eligible() bool {
	return e.registrationErr == nil && !e.alreadyApproved && !e.inCooldown && !e.wrongSlot && !e.blockedByDoerDiversity && !e.validationFailed
}

// ReviewerSlotAdmits reports whether agent may take task's next review: always
// when sel is not a list, else only when agent runs the entry for review slot
// len(task.Approvals) (ADR-0167).
func ReviewerSlotAdmits(sel rolemodels.Selection, task *models.Task, agent models.Agent) bool {
	return !sel.List || sel.Slot(len(task.Approvals)).Matches(agent.Provider, agent.Model)
}

// ReviewerClaimEligible projects the agent-specific reviewer claim gates for
// task. Task claimability and review-boundary validation remain separate.
func ReviewerClaimEligible(input ReviewerClaimEligibilityInput) bool {
	return projectReviewerClaimEligibility(input).eligible()
}

func projectReviewerClaimEligibility(input ReviewerClaimEligibilityInput) reviewerClaimEligibility {
	if input.State == nil || input.Task == nil || input.Resolver == nil {
		return reviewerClaimEligibility{registrationErr: &PreconditionError{Reason: "reviewer claim eligibility inputs are incomplete"}}
	}
	agent, err := requireRegisteredClaimAgent(input.State, input.AgentID, input.ReviewerRole)
	if err != nil {
		return reviewerClaimEligibility{registrationErr: err}
	}
	return reviewerClaimEligibility{
		alreadyApproved:        input.Task.HasApprovalFromAgent(input.AgentID),
		inCooldown:             isInReviewClaimCooldown(input.Task, input.AgentID, input.Now.Add(-defaultReviewClaimCooldown)),
		wrongSlot:              !ReviewerSlotAdmits(input.Slots, input.Task, agent),
		blockedByDoerDiversity: isBlockedByDoerDiversityAt(input.Task, agent.Provider, input.AgentID, input.State, input.Resolver, input.Slots, input.Now, input.Keys),
		validationFailed:       models.ValidationTaskKnownFailed(input.State, input.Task, input.AgentID, input.Now),
	}
}

func filterByReviewerClaimEligibility(
	candidates []*models.Task,
	projected map[*models.Task]reviewerClaimEligibility,
	keep func(reviewerClaimEligibility) bool,
) []*models.Task {
	filtered := make([]*models.Task, 0, len(candidates))
	for _, task := range candidates {
		if keep(projected[task]) {
			filtered = append(filtered, task)
		}
	}
	return filtered
}

// snapshotCandidates lists the tasks the claim may take: the one named task, or
// every claimable task not awaiting a validation retry. It runs inside the claim
// transaction (K1-E10: ValidationRetryPending resolves Git state).
func (c *reviewerClaim) snapshotCandidates(state *models.State) ([]*models.Task, error) {
	var candidates []*models.Task
	if c.input.TaskID != "" {
		task, err := c.namedCandidate(state)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, task)
	} else {
		// Find reviewable task with highest priority.
		for i := range state.Tasks {
			if state.Tasks[i].IsClaimable(c.role, state.Tasks, c.pb.pr) {
				if ValidationRetryPending(c.input.ProjectRoot, state, &state.Tasks[i], c.input.AgentID, c.input.Session) {
					continue
				}
				candidates = append(candidates, &state.Tasks[i])
			}
		}
	}
	if len(candidates) == 0 {
		return nil, &PreconditionError{Reason: "no reviewable tasks found"}
	}
	return candidates, nil
}

// namedCandidate resolves the task the request names: it must be reviewable by
// the claimer's role, and an exact replay of a completed request ends the claim
// with the receipt's outcome.
func (c *reviewerClaim) namedCandidate(state *models.State) (*models.Task, error) {
	task := state.FindTask(c.input.TaskID)
	if task == nil {
		return nil, &lizaerrors.NotFoundError{Entity: notFoundEntityTask, ID: c.input.TaskID}
	}
	c.invocation.observe(task)
	expectedRole, err := c.pb.resolver.ReviewerRole(task.RolePair)
	if err != nil {
		return nil, err
	}
	if expectedRole != c.role {
		return nil, &PreconditionError{Reason: "agent role cannot review this task"}
	}
	request, err := c.requestFor(task)
	if err != nil {
		return nil, err
	}
	receipt, err := c.checkRequest(task, request, state.Agents)
	if err != nil {
		return nil, err
	}
	if receipt != nil {
		c.invocation.outcome = LifecycleReplayOutcome(task, receipt, c.input.AgentID)
		lease, _ := time.Parse(time.RFC3339Nano, receipt.Projection.LeaseExpires)
		c.result = ClaimReviewerTaskResult{LifecycleOutcome: c.invocation.outcome, TaskID: task.ID, ReviewCommit: receipt.Projection.ReviewCommit, LeaseExpires: lease}
		if task.Worktree != nil {
			c.result.Worktree = *task.Worktree
		}
		return nil, errLifecycleReplay
	}
	if !task.IsClaimable(c.role, state.Tasks, c.pb.pr) {
		return nil, &PreconditionError{
			Reason: fmt.Sprintf("task %s is not reviewable by %s (current status: %s)", c.input.TaskID, c.role, task.Status),
		}
	}
	return task, nil
}

// eligibilityGate is one agent-specific claim gate: the candidates it keeps and
// the refusal when it keeps none.
type eligibilityGate struct {
	keep    func(reviewerClaimEligibility) bool
	refusal string
}

// eligibleCandidates applies the eligibility gates in order, each against the
// projection taken before any gate ran.
func (c *reviewerClaim) eligibleCandidates(state *models.State, candidates []*models.Task, keys agentlaunch.ProviderKeys) ([]*models.Task, error) {
	projected := make(map[*models.Task]reviewerClaimEligibility, len(candidates))
	for _, task := range candidates {
		projected[task] = projectReviewerClaimEligibility(ReviewerClaimEligibilityInput{
			State: state, Task: task, AgentID: c.input.AgentID,
			ReviewerRole: c.role, Now: c.now, Resolver: c.pb.resolver, Slots: c.slots, Keys: keys,
		})
	}
	gates := []eligibilityGate{
		// Independent-review filter: skip tasks the claiming agent already
		// approved. Without this, a reviewer-1 polling loop would re-claim
		// its own partially_approved task and self-rubber-stamp the quorum.
		{func(e reviewerClaimEligibility) bool { return e.registrationErr == nil && !e.alreadyApproved },
			"no reviewable tasks found (all candidates already approved by claimer)"},
		// Claim cooldown prevents claim-release spin.
		{func(e reviewerClaimEligibility) bool { return !e.inCooldown }, "all reviewable tasks in claim cooldown"},
		{func(e reviewerClaimEligibility) bool { return !e.wrongSlot }, "no reviewable tasks match this reviewer's " + paths.ModelsFileName + " slot"},
		// Doer-provider diversity: when provider-diversity is configured, block
		// reviewers that share the doer's provider if a different-provider
		// reviewer is registered (even if busy).
		{func(e reviewerClaimEligibility) bool { return !e.blockedByDoerDiversity }, "no reviewable tasks found"},
	}
	for _, gate := range gates {
		candidates = filterByReviewerClaimEligibility(candidates, projected, gate.keep)
		if len(candidates) == 0 {
			return nil, &PreconditionError{Reason: gate.refusal}
		}
	}
	return candidates, nil
}

func removeCandidate(candidates []*models.Task, candidate *models.Task) []*models.Task {
	for i, task := range candidates {
		if task == candidate {
			return append(candidates[:i], candidates[i+1:]...)
		}
	}
	return candidates
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
	keys agentlaunch.ProviderKeys,
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
		return pickWithApprovalDiversity(partiallyApprovedTasks, claimerProvider, keys)
	}

	// Fall back to submitted tasks with fresh-submission diversity.
	return pickWithFreshDiversity(submittedTasks, claimerProvider, claimerAgentID, pr, state, keys)
}

// pickWithApprovalDiversity selects from partially_approved tasks, preferring
// tasks where the claimer's provider differs from existing approvals.
func pickWithApprovalDiversity(tasks []*models.Task, claimerProvider string, keys agentlaunch.ProviderKeys) *models.Task {
	// No diversity preference possible: single candidate, or claimer has no
	// provider configured (falls back to random selection).
	if claimerProvider == "" || len(tasks) <= 1 {
		return pickRandom(tasks)
	}

	var diverse, same []*models.Task
	for _, t := range tasks {
		if hasDifferentProvider(t.Approvals, claimerProvider, keys) {
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
func hasDifferentProvider(approvals []models.Approval, claimerProvider string, keys agentlaunch.ProviderKeys) bool {
	for _, a := range approvals {
		if !keys.Same(a.Provider, claimerProvider) {
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
	keys agentlaunch.ProviderKeys,
) *models.Task {
	// No diversity preference possible: single candidate, or claimer has no
	// provider configured (falls back to random selection).
	if claimerProvider == "" || len(tasks) <= 1 {
		return pickRandom(tasks)
	}

	var preferred, rest []*models.Task
	for _, t := range tasks {
		if isDiversitySatisfiable(t, claimerProvider, claimerAgentID, pr, state, keys) {
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
	keys agentlaunch.ProviderKeys,
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
		if !hasReviewerCapacity(agent, reviewerRole, now) || models.ValidationTaskKnownFailed(state, task, agentID, now) {
			continue
		}
		if !keys.Same(agent.Provider, claimerProvider) {
			return true
		}
	}
	return false
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
	resolver ReviewerClaimPolicyResolver,
	keys agentlaunch.ProviderKeys,
) []*models.Task {
	if claimerProvider == "" {
		return candidates
	}

	var filtered []*models.Task
	for _, t := range candidates {
		if isBlockedByDoerDiversity(t, claimerProvider, claimerAgentID, state, resolver, keys) {
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
	resolver ReviewerClaimPolicyResolver,
	keys agentlaunch.ProviderKeys,
) bool {
	return isBlockedByDoerDiversityAt(task, claimerProvider, claimerAgentID, state, resolver, rolemodels.Selection{}, time.Now().UTC(), keys)
}

func isBlockedByDoerDiversityAt(
	task *models.Task,
	claimerProvider string,
	claimerAgentID string,
	state *models.State,
	resolver ReviewerClaimPolicyResolver,
	slots rolemodels.Selection,
	now time.Time,
	keys agentlaunch.ProviderKeys,
) bool {
	// Resolve effective impact from task history, then check if provider-diversity
	// is configured at that impact level (with base-level fallthrough).
	effectiveImpact := ResolveEffectiveImpact(task.History)
	diversity, err := resolver.ProviderDiversity(task.RolePair, pipeline.ReviewImpact(effectiveImpact))
	if err != nil || diversity != pipeline.ProviderDiversityPreferred {
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
	if !keys.Same(claimerProvider, doerAgent.Provider) {
		return false
	}

	// Claimer shares the doer's provider. Block only if a valid
	// different-provider reviewer is registered for this role-pair (even if
	// busy) and its review slot binding lets it take this review.
	reviewerRole, err := resolver.ReviewerRole(task.RolePair)
	if err != nil {
		return false
	}
	for agentID, agent := range state.Agents {
		if agentID == claimerAgentID {
			continue
		}
		if !hasReviewerCapacity(agent, reviewerRole, now) || models.ValidationTaskKnownFailed(state, task, agentID, now) {
			continue
		}
		if !keys.Same(agent.Provider, doerAgent.Provider) && ReviewerSlotAdmits(slots, task, agent) {
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
