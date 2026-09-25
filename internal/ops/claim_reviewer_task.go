package ops

import (
	stderrors "errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	lizaerrors "github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/filelock"
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
	ProjectRoot    string
	AgentID        string
	Role           string
	TaskID         string
	LeaseDuration  int
	Authority      *models.AgentAuthority
	Session        *ValidationSession
	RequestOptions LifecycleRequestOptions
}

// ClaimReviewerTaskResult contains the outcome of a successful reviewer task claim.
type ClaimReviewerTaskResult struct {
	models.LifecycleOutcome
	TaskID       string
	Worktree     string
	ReviewCommit string
	LeaseExpires time.Time
	// CandidateFailures retains classified removals even when a later candidate
	// is claimed successfully. Failed claims retain their existing typed errors.
	CandidateFailures []ReviewClaimCandidateFailure
}

// ReviewerClaimPolicyResolver exposes the review-policy lookups needed to
// project whether a registered reviewer can claim a specific task.
type ReviewerClaimPolicyResolver interface {
	ProviderDiversity(string, string) (string, error)
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
}

type reviewerClaimEligibility struct {
	registrationErr        error
	alreadyApproved        bool
	inCooldown             bool
	blockedByDoerDiversity bool
	validationFailed       bool
}

func (e reviewerClaimEligibility) eligible() bool {
	return e.registrationErr == nil && !e.alreadyApproved && !e.inCooldown && !e.blockedByDoerDiversity && !e.validationFailed
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
		blockedByDoerDiversity: isBlockedByDoerDiversityAt(input.Task, agent.Provider, input.AgentID, input.State, input.Resolver, input.Now),
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

// ClaimReviewerTask finds and claims a reviewable task for a code-reviewer agent.
// It atomically transitions the task to REVIEWING (or REVIEWING_2 for partially-
// approved tasks), assigns the reviewer, and updates the agent status.
//
// Claim priority: partially_approved candidates are selected before submitted
// candidates at the same priority level. Within each status tier, provider
// diversity is used as a soft preference for candidate selection.
func ClaimReviewerTask(input ClaimReviewerTaskInput) (result *ClaimReviewerTaskResult, err error) {
	invocation := &ownershipInvocation{operation: "claim-reviewer-task", opts: input.RequestOptions, authority: input.Authority, metrics: NewLifecycleInvocation(input.ProjectRoot)}
	defer func() { err = invocation.finish(input.ProjectRoot, err) }()
	if input.AgentID == "" {
		return nil, &PreconditionError{Reason: "agent ID is required"}
	}
	if err := ValidateLifecycleRequestOptions(input.RequestOptions); err != nil {
		return nil, &PreconditionError{Reason: err.Error()}
	}
	if input.RequestOptions.RequestID != "" && input.TaskID == "" {
		return nil, &PreconditionError{Reason: "task ID is required with reviewer claim request options"}
	}
	err = WithProjectLifecycleSharedLock(input.ProjectRoot, "claim-reviewer-task", func() error {
		drainDeferredLifecycleRetirements(input.ProjectRoot)
		var inner error
		result, inner = claimReviewerTask(input, invocation)
		return inner
	})
	return result, err
}

func claimReviewerTask(input ClaimReviewerTaskInput, invocation *ownershipInvocation) (_ *ClaimReviewerTaskResult, resultErr error) {
	if input.AgentID == "" {
		return nil, &PreconditionError{Reason: "agent ID is required"}
	}
	if input.Authority != nil {
		if err := requireAuthorityActor(*input.Authority, input.AgentID); err != nil {
			return nil, err
		}
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
	var acceptanceErrors []error
	var repairNeededTaskIDs []string
	var candidateFailures []ReviewClaimCandidateFailure

	// Load pipeline config once for both IsClaimable and transition.
	pb, err := loadPipelineBundle(input.ProjectRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load pipeline config: %w", err)
	}
	pr := pb.pr

	var preflight *ValidationPreflight
	var selected *models.Task
	var preparation *models.LifecyclePreparation
	defer func() {
		if preparation == nil {
			return
		}
		effects := "none"
		if invocation.effects {
			effects = "unknown"
		}
		resultErr = retireOrDeferFailedLifecyclePreparation(input.ProjectRoot, selected.ID, input.Authority, preparation, resultErr, effects)
	}()
	var needsPreflight, needsTaskLock, taskLocked bool
	requestFor := func(task *models.Task) (LifecycleRequest, error) {
		if selected != nil {
			return invocation.request, nil
		}
		return NewLifecycleRequest("claim-reviewer-task", task, input.AgentID, input.Authority, input.RequestOptions, struct {
			Role          string
			LeaseDuration int
		}{role, input.LeaseDuration})
	}
	checkRequest := func(task *models.Task, request LifecycleRequest, agents map[string]models.Agent) (*models.LifecycleReceipt, error) {
		if preparation != nil {
			return nil, ValidateLifecyclePreparation(task, request)
		}
		return CheckLifecycleRequest(task, request, agents)
	}
	attempt := func() error {
		needsPreflight = false
		var pendingPreparation *models.LifecyclePreparation
		err := lifecycleMutation(bb, input.Authority)(func(state *models.State) error {
			claimingAgent, err := requireRegisteredClaimAgent(state, input.AgentID, role)
			if err != nil {
				return err
			}

			var candidates []*models.Task
			if input.TaskID != "" {
				task := state.FindTask(input.TaskID)
				if task == nil {
					return &lizaerrors.NotFoundError{Entity: "task", ID: input.TaskID}
				}
				invocation.observe(task)
				expectedRole, err := pb.resolver.ReviewerRole(task.RolePair)
				if err != nil {
					return err
				}
				if expectedRole != role {
					return &PreconditionError{Reason: "agent role cannot review this task"}
				}
				request, err := requestFor(task)
				if err != nil {
					return err
				}
				receipt, err := checkRequest(task, request, state.Agents)
				if err != nil {
					return err
				}
				if receipt != nil {
					invocation.outcome = LifecycleReplayOutcome(task, receipt, input.AgentID)
					lease, _ := time.Parse(time.RFC3339Nano, receipt.Projection.LeaseExpires)
					result = ClaimReviewerTaskResult{LifecycleOutcome: invocation.outcome, TaskID: task.ID, ReviewCommit: receipt.Projection.ReviewCommit, LeaseExpires: lease}
					if task.Worktree != nil {
						result.Worktree = *task.Worktree
					}
					return errLifecycleReplay
				}
				if !task.IsClaimable(role, state.Tasks, pr) {
					return &PreconditionError{
						Reason: fmt.Sprintf("task %s is not reviewable by %s (current status: %s)", input.TaskID, role, task.Status),
					}
				}
				candidates = append(candidates, task)
			} else {
				// Find reviewable task with highest priority.
				for i := range state.Tasks {
					if state.Tasks[i].IsClaimable(role, state.Tasks, pr) {
						if ValidationRetryPending(input.ProjectRoot, state, &state.Tasks[i], input.AgentID, input.Session) {
							continue
						}
						candidates = append(candidates, &state.Tasks[i])
					}
				}
			}

			if len(candidates) == 0 {
				return &PreconditionError{Reason: "no reviewable tasks found"}
			}

			projected := make(map[*models.Task]reviewerClaimEligibility, len(candidates))
			for _, task := range candidates {
				projected[task] = projectReviewerClaimEligibility(ReviewerClaimEligibilityInput{
					State: state, Task: task, AgentID: input.AgentID,
					ReviewerRole: role, Now: now, Resolver: pb.resolver,
				})
			}

			// Independent-review filter: skip tasks the claiming agent already
			// approved. Without this, a reviewer-1 polling loop would re-claim
			// its own partially_approved task and self-rubber-stamp the quorum.
			candidates = filterByReviewerClaimEligibility(candidates, projected, func(e reviewerClaimEligibility) bool {
				return e.registrationErr == nil && !e.alreadyApproved
			})
			if len(candidates) == 0 {
				return &PreconditionError{Reason: "no reviewable tasks found (all candidates already approved by claimer)"}
			}

			// Filter out candidates in claim cooldown to prevent claim-release spin.
			candidates = filterByReviewerClaimEligibility(candidates, projected, func(e reviewerClaimEligibility) bool {
				return !e.inCooldown
			})
			if len(candidates) == 0 {
				return &PreconditionError{Reason: "all reviewable tasks in claim cooldown"}
			}

			// Look up claiming reviewer's provider from agent state.
			claimerProvider := claimingAgent.Provider

			// Filter by doer-provider diversity: when provider-diversity is configured,
			// block reviewers that share the doer's provider if a different-provider
			// reviewer is registered (even if busy).
			candidates = filterByReviewerClaimEligibility(candidates, projected, func(e reviewerClaimEligibility) bool {
				return !e.blockedByDoerDiversity
			})
			if len(candidates) == 0 {
				return &PreconditionError{Reason: "no reviewable tasks found"}
			}

			for len(candidates) > 0 {
				task := selectBestCandidate(candidates, pr, claimerProvider, input.AgentID, state)
				if task == nil {
					break
				}
				if selected != nil && (task.ID != selected.ID || optionalValue(task.Worktree) != optionalValue(selected.Worktree)) {
					return validationError("candidate_changed")
				}
				invocation.observe(task)
				request, err := requestFor(task)
				if err != nil {
					return err
				}
				if _, err := checkRequest(task, request, state.Agents); err != nil {
					return err
				}

				if err := validateReviewBoundaryForAssignment(input.ProjectRoot, state, task); err != nil {
					reviewBoundaryErr = err
					var evidenceErr *AcceptanceEvidenceError
					if stderrors.As(err, &evidenceErr) {
						acceptanceErrors = append(acceptanceErrors, evidenceErr)
						candidateFailures = appendReviewClaimCandidateFailure(candidateFailures,
							newReviewClaimCandidateFailure(task, ReviewClaimClassAcceptanceEvidence, false, err))
						candidates = removeCandidate(candidates, task)
						continue
					}
					var repairNeeded *ReviewBoundaryRepairNeededError
					if stderrors.As(err, &repairNeeded) {
						if !slices.Contains(repairNeededTaskIDs, task.ID) {
							repairNeededTaskIDs = append(repairNeededTaskIDs, task.ID)
						}
						candidateFailures = appendReviewClaimCandidateFailure(candidateFailures,
							newReviewClaimCandidateFailure(task, ReviewClaimClassReviewBoundaryRepair, false, err))
						candidates = removeCandidate(candidates, task)
						continue
					}
					// Classified before the transition below moves the task, so
					// the record names the boundary the failure was observed on.
					class, transient := classifyReviewBoundaryRemoval(err)
					failure := newReviewClaimCandidateFailure(task, class, transient, err)
					if markErr := markReviewBoundaryIntegrationFailed(state, task, input.AgentID, pb.transitions, err); markErr != nil {
						return markErr
					}
					candidateFailures = appendReviewClaimCandidateFailure(candidateFailures, failure)
					invocation.effects = true
					candidates = removeCandidate(candidates, task)
					continue
				}

				if task.RolePair == "" {
					return &PreconditionError{Reason: fmt.Sprintf("task %s has no role_pair set", task.ID)}
				}
				if !taskLocked {
					copyTask := *task
					selected = &copyTask
					invocation.request = request
					needsTaskLock = true
					// Commit earlier candidates' boundary failures before taking
					// this candidate's worktree lock outside the blackboard lock.
					return nil
				}
				if len(task.ValidationPrerequisites) > 0 && preflight == nil {
					if task.Worktree == nil || *task.Worktree == "" {
						return validationError("worktree_unavailable")
					}
					if err := PrepareLifecycleRequest(task, request, state.Agents); err != nil {
						return err
					}
					copyPreparation := *task.Lifecycle.Preparation
					pendingPreparation = &copyPreparation
					needsPreflight = true
					return nil
				}
				if err := preflight.CheckCurrent(state); err != nil {
					return err
				}
				if input.Session != nil && input.Session.PreparationError != nil {
					return input.Session.PreparationError
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
				task.History = append(task.History, models.TaskHistoryEntry{
					Time:  now,
					Event: models.TaskEventClaimed,
					Agent: &input.AgentID,
				})

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
				invocation.outcome, err = CompleteLifecycleRequest(task, request, models.LifecycleProjection{ReviewCommit: result.ReviewCommit, LeaseExpires: leaseExpires.Format(time.RFC3339Nano)}, state.Agents)
				result.LifecycleOutcome = invocation.outcome
				if err == nil {
					invocation.effects = true
				}
				return err
			}

			return nil
		})
		if err == nil && pendingPreparation != nil {
			preparation = pendingPreparation
		}
		return err
	}
	err = attempt()
	if err == nil && needsTaskLock {
		lock := filelock.New(claimTaskWorktreeLockPath(lp.StatePath(), selected.ID))
		err = lock.WithLockOperation("claim-reviewer-task", func() error {
			taskLocked = true
			input.TaskID = selected.ID
			for {
				if err := attempt(); err != nil || !needsPreflight {
					return err
				}
				// The durable preparation precedes resumed setup and probes.
				// Final assignment rechecks that exact reservation and preflight.
				invocation.effects = true
				var err error
				preflight, err = prepareResumedValidation(input.ProjectRoot, selected.ID, input.AgentID, *selected.Worktree, input.Session, input.Authority)
				if err != nil {
					return err
				}
				if preflight == nil {
					return validationError("context_changed")
				}
			}
		})
	}
	if err != nil && !isLifecycleReplay(err) {
		return nil, err
	}
	if reviewBoundaryErr != nil && result.TaskID == "" {
		// The error stays the one every caller already classifies; the envelope
		// only adds which candidates failed, in which class, against which
		// boundary version.
		if len(acceptanceErrors) > 0 {
			return nil, newReviewClaimFailure(role, candidateFailures, stderrors.Join(acceptanceErrors...))
		}
		if len(repairNeededTaskIDs) > 0 {
			return nil, newReviewClaimFailure(role, candidateFailures, &PreconditionError{
				Reason: fmt.Sprintf(
					"review boundary needs repair for task(s): %s — run %s <task-id>",
					strings.Join(repairNeededTaskIDs, ", "),
					brand.Command("update-review-commit"),
				),
			})
		}
		return nil, newReviewClaimFailure(role, candidateFailures, &IntegrationFailedError{Reason: IntegrationReasonReviewBoundaryMismatch})
	}

	result.CandidateFailures = candidateFailures
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
		if !hasReviewerCapacity(agent, reviewerRole, now) || models.ValidationTaskKnownFailed(state, task, agentID, now) {
			continue
		}
		if agent.Provider != claimerProvider {
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
	return isBlockedByDoerDiversityAt(task, claimerProvider, claimerAgentID, state, resolver, time.Now().UTC())
}

func isBlockedByDoerDiversityAt(
	task *models.Task,
	claimerProvider string,
	claimerAgentID string,
	state *models.State,
	resolver ReviewerClaimPolicyResolver,
	now time.Time,
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
	for agentID, agent := range state.Agents {
		if agentID == claimerAgentID {
			continue
		}
		if !hasReviewerCapacity(agent, reviewerRole, now) || models.ValidationTaskKnownFailed(state, task, agentID, now) {
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
