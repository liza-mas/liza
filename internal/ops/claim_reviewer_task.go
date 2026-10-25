package ops

import (
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/identity"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/rolemodels"
	"github.com/liza-mas/liza/internal/roles"
)

// claimReviewerTaskOperation names the reviewer claim in its lifecycle identity,
// work-admission check and lock labels.
const claimReviewerTaskOperation = "claim-reviewer-task"

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

// ClaimReviewerTask finds and claims a reviewable task for a code-reviewer agent.
// It atomically transitions the task to REVIEWING (or REVIEWING_2 for partially-
// approved tasks), assigns the reviewer, and updates the agent status.
//
// Claim priority: partially_approved candidates are selected before submitted
// candidates at the same priority level. Within each status tier, provider
// diversity is used as a soft preference for candidate selection.
func ClaimReviewerTask(input ClaimReviewerTaskInput) (result *ClaimReviewerTaskResult, err error) {
	invocation := &ownershipInvocation{operation: claimReviewerTaskOperation, opts: input.RequestOptions, authority: input.Authority, metrics: NewLifecycleInvocation(input.ProjectRoot)}
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
	err = WithProjectLifecycleSharedLock(input.ProjectRoot, claimReviewerTaskOperation, func() error {
		drainDeferredLifecycleRetirements(input.ProjectRoot)
		var inner error
		result, inner = claimReviewerTask(input, invocation)
		return inner
	})
	return result, err
}

// reviewerClaim carries one reviewer claim across its transaction attempts and
// the task-lock phase between them. The first attempt may only reserve the
// selected candidate (needsTaskLock) or prepare its validation context
// (needsPreflight); the final attempt runs under the candidate's worktree lock
// and assigns.
type reviewerClaim struct {
	input        ClaimReviewerTaskInput
	invocation   *ownershipInvocation
	role         string
	bb           *db.Blackboard
	pb           *pipelineBundle
	slots        rolemodels.Selection
	now          time.Time
	leaseExpires time.Time

	result              ClaimReviewerTaskResult
	reviewBoundaryErr   error
	acceptanceErrors    []error
	repairNeededTaskIDs []string
	candidateFailures   []ReviewClaimCandidateFailure

	preflight          *ValidationPreflight
	selected           *models.Task
	preparation        *models.LifecyclePreparation
	pendingPreparation *models.LifecyclePreparation
	needsPreflight     bool
	needsTaskLock      bool
	taskLocked         bool
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
	lp := paths.New(input.ProjectRoot)
	c := &reviewerClaim{
		input:      input,
		invocation: invocation,
		role:       reviewerClaimRole(input),
		bb:         RequestBlackboard(lp.StatePath(), input.Authority, input.RequestOptions),
		now:        time.Now().UTC(),
	}
	c.leaseExpires = c.now.Add(time.Duration(input.LeaseDuration) * time.Second)
	if err := c.loadPolicy(); err != nil {
		return nil, err
	}
	defer c.retireFailedPreparation(&resultErr)

	err := c.attempt()
	if err == nil && c.needsTaskLock {
		err = c.attemptUnderTaskLock(lp.StatePath())
	}
	if err != nil && !isLifecycleReplay(err) {
		return nil, err
	}
	if c.reviewBoundaryErr != nil && c.result.TaskID == "" {
		return nil, c.claimFailure()
	}
	c.result.CandidateFailures = c.candidateFailures
	return &c.result, nil
}

// reviewerClaimRole is the claimer's role: the requested one, else the role its
// agent ID names, else code reviewer.
func reviewerClaimRole(input ClaimReviewerTaskInput) string {
	if input.Role != "" {
		return input.Role
	}
	if inferred, err := identity.ExtractRole(input.AgentID); err == nil && roles.IsValid(inferred) {
		return inferred
	}
	return models.RoleCodeReviewer
}

// loadPolicy loads the pipeline once for claimability and transitions, and the
// reviewer's models.yaml slots. An unreadable models.yaml refuses the claim
// rather than ignoring the review slots it may bind.
func (c *reviewerClaim) loadPolicy() error {
	pb, err := loadPipelineBundle(c.input.ProjectRoot)
	if err != nil {
		return fmt.Errorf("failed to load pipeline config: %w", err)
	}
	c.pb = pb
	modelsFile, err := rolemodels.Load(c.input.ProjectRoot)
	if err != nil {
		return &PreconditionError{Reason: fmt.Sprintf("cannot read review slots: %v", err)}
	}
	c.slots, _ = modelsFile.ReviewSlots(c.role)
	return nil
}

// retireFailedPreparation retires the reservation a failed claim left, or
// defers its retirement, and reports that outcome as the claim's error.
func (c *reviewerClaim) retireFailedPreparation(resultErr *error) {
	if c.preparation == nil {
		return
	}
	effects := LifecycleEffectsNone
	if c.invocation.effects {
		effects = LifecycleEffectsUnknown
	}
	*resultErr = retireOrDeferFailedLifecyclePreparation(c.input.ProjectRoot, c.selected.ID, c.input.Authority, c.preparation, *resultErr, effects)
}

func (c *reviewerClaim) requestFor(task *models.Task) (LifecycleRequest, error) {
	if c.selected != nil {
		return c.invocation.request, nil
	}
	return NewLifecycleRequest(claimReviewerTaskOperation, task, c.input.AgentID, c.input.Authority, c.input.RequestOptions, struct {
		Role          string
		LeaseDuration int
	}{c.role, c.input.LeaseDuration})
}

func (c *reviewerClaim) checkRequest(task *models.Task, request LifecycleRequest, agents map[string]models.Agent) (*models.LifecycleReceipt, error) {
	if c.preparation != nil {
		return nil, ValidateLifecyclePreparation(task, request)
	}
	return CheckLifecycleRequest(task, request, agents)
}

// attempt runs one claim transaction.
func (c *reviewerClaim) attempt() error {
	c.needsPreflight = false
	c.pendingPreparation = nil
	err := lifecycleMutation(c.bb, c.input.Authority)(c.transaction)
	if err == nil && c.pendingPreparation != nil {
		c.preparation = c.pendingPreparation
	}
	return err
}

// attemptUnderTaskLock repeats the claim transaction under the selected
// candidate's worktree lock, preparing its resumed validation context outside
// the blackboard lock between attempts. The durable preparation precedes
// resumed setup and probes; final assignment rechecks that exact reservation
// and preflight.
func (c *reviewerClaim) attemptUnderTaskLock(statePath string) error {
	lock := filelock.New(claimTaskWorktreeLockPath(statePath, c.selected.ID))
	return lock.WithLockOperation(claimReviewerTaskOperation, func() error {
		c.taskLocked = true
		c.input.TaskID = c.selected.ID
		for {
			if err := c.attempt(); err != nil || !c.needsPreflight {
				return err
			}
			c.invocation.effects = true
			var err error
			c.preflight, err = prepareResumedValidation(c.input.ProjectRoot, c.selected.ID, c.input.AgentID, *c.selected.Worktree, c.input.Session, c.input.Authority)
			if err != nil {
				return err
			}
			if c.preflight == nil {
				return validationError(validationCodeContextChanged)
			}
		}
	})
}

// transaction is the claim's blackboard transaction: eligibility from the
// state it holds, diversity selection, then the per-candidate checks and
// effects. K1-E1, K1-E2 and K1-E10 run inside it (see claimCandidate and
// snapshotCandidates).
func (c *reviewerClaim) transaction(state *models.State) error {
	if err := RequireWorkAdmitted(state, claimReviewerTaskOperation); err != nil {
		return err
	}
	claimingAgent, err := requireRegisteredClaimAgent(state, c.input.AgentID, c.role)
	if err != nil {
		return err
	}
	candidates, err := c.snapshotCandidates(state)
	if err != nil {
		return err
	}
	candidates, err = c.eligibleCandidates(state, candidates)
	if err != nil {
		return err
	}
	for len(candidates) > 0 {
		task := selectBestCandidate(candidates, c.pb.pr, claimingAgent.Provider, c.input.AgentID, state)
		if task == nil {
			break
		}
		removed, err := c.claimCandidate(state, task, claimingAgent)
		if err != nil {
			return err
		}
		if !removed {
			return nil
		}
		candidates = removeCandidate(candidates, task)
	}
	return nil
}

// claim reserves, prepares or assigns the candidate that passed its checks.
func (c *reviewerClaim) claim(state *models.State, task *models.Task, request LifecycleRequest, agent models.Agent) error {
	if task.RolePair == "" {
		return &PreconditionError{Reason: fmt.Sprintf("task %s has no role_pair set", task.ID)}
	}
	if !c.taskLocked {
		copyTask := *task
		c.selected = &copyTask
		c.invocation.request = request
		c.needsTaskLock = true
		// Commit earlier candidates' boundary failures before taking
		// this candidate's worktree lock outside the blackboard lock.
		return nil
	}
	if len(task.ValidationPrerequisites) > 0 && c.preflight == nil {
		if task.Worktree == nil || *task.Worktree == "" {
			return validationError(validationCodeWorktreeUnavailable)
		}
		if err := PrepareLifecycleRequest(task, request, state.Agents); err != nil {
			return err
		}
		copyPreparation := *task.Lifecycle.Preparation
		c.pendingPreparation = &copyPreparation
		c.needsPreflight = true
		return nil
	}
	if err := c.preflight.CheckCurrent(state); err != nil { // K1-E1
		return err
	}
	if c.input.Session != nil && c.input.Session.PreparationError != nil {
		return c.input.Session.PreparationError
	}
	return c.assign(state, task, request, agent)
}

// assign applies the claim's effects: the reviewing transition, the reviewer's
// ownership and lease (derived from LeaseDuration, C1), the claimed history
// entry, the claimer's agent record and the lifecycle receipt.
func (c *reviewerClaim) assign(state *models.State, task *models.Task, request LifecycleRequest, agent models.Agent) error {
	// Determine target reviewing status based on task's current state.
	targetStatus, err := resolveReviewingTarget(task, c.pb.pr)
	if err != nil {
		return err
	}
	if err := task.TransitionWith(targetStatus, c.pb.transitions); err != nil {
		return err
	}
	task.ReviewingBy = &c.input.AgentID
	task.ReviewLeaseExpires = &c.leaseExpires
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:  c.now,
		Event: models.TaskEventClaimed,
		Agent: &c.input.AgentID,
	})

	agent.Status = models.AgentStatusReviewing
	currentTask := task.ID
	agent.CurrentTask = &currentTask
	agent.Heartbeat = c.now
	agent.LeaseExpires = &c.leaseExpires
	state.Agents[c.input.AgentID] = agent

	c.result.TaskID = task.ID
	if task.Worktree != nil {
		c.result.Worktree = *task.Worktree
	}
	if task.ReviewCommit != nil {
		c.result.ReviewCommit = *task.ReviewCommit
	}
	c.result.LeaseExpires = c.leaseExpires
	c.invocation.outcome, err = CompleteLifecycleRequest(task, request, models.LifecycleProjection{ReviewCommit: c.result.ReviewCommit, LeaseExpires: c.leaseExpires.Format(time.RFC3339Nano)}, state.Agents)
	c.result.LifecycleOutcome = c.invocation.outcome
	if err == nil {
		c.invocation.effects = true
	}
	return err
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
