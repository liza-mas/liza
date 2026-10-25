package ops

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/identity"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
)

// claimOperation is the evidence claim admission captured for the later phases.
// Configuration, statuses and the strategy come from the first read; every
// locked phase re-reads current state and treats this evidence as a comparison
// baseline, never as write authority.
type claimOperation struct {
	projectRoot string
	statePath   string
	bb          *db.Blackboard
	git         *git.Git
	taskID      string
	agentID     string
	runtimeRole string
	taskStatus  models.TaskStatus
	config      claimConfig
	strategy    claimStrategy
	ctx         claimContext
	authority   *models.AgentAuthority
	invocation  *ownershipInvocation
	session     *ValidationSession
}

// claimConfig is the configuration captured at admission.
type claimConfig struct {
	integrationBranch      string
	postWorktreeCmd        *string
	postWorktreeCmdTimeout time.Duration
	copyWorktreeEnvFiles   bool
	scipSearchLanguages    []string
	leaseDuration          int
}

// claimStatuses are the pipeline statuses of the task's role pair.
type claimStatuses struct {
	initial, rejected, executing models.TaskStatus
	doerRole                     string
}

// admitClaimTask runs phase one: it validates ownership and eligibility under
// a read of current state, builds the claim request, and enforces the
// iteration and review limits before any filesystem work. A replayed request
// returns its recorded result instead of an operation.
func admitClaimTask(projectRoot, taskID, agentID string, authority *models.AgentAuthority, invocation *ownershipInvocation, sessions []*ValidationSession) (*claimOperation, *ClaimResult, error) {
	lp := paths.New(projectRoot)
	bb := RequestBlackboard(lp.StatePath(), authority, invocation.opts)
	state, task, err := readClaimAdmissionState(bb, taskID, authority, invocation)
	if err != nil {
		return nil, nil, err
	}
	runtimeRole, err := identity.ExtractRole(agentID)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid agent ID %s: %w", agentID, err)
	}
	resolver, _, err := loadResolver(projectRoot)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load pipeline config: %w", err)
	}
	statuses, err := resolveClaimStatuses(resolver, task, runtimeRole)
	if err != nil {
		return nil, nil, err
	}
	if _, err := requireRegisteredClaimAgent(state, agentID, runtimeRole); err != nil {
		return nil, nil, err
	}
	state, task, replay, err := establishClaimRequest(bb, lp.StatePath(), state, task, agentID, runtimeRole, resolver, authority, invocation)
	if err != nil || replay != nil {
		return nil, replay, err
	}
	strategy, err := selectClaimStrategy(task, statuses, runtimeRole)
	if err != nil {
		return nil, nil, err
	}
	if err := requireFreeClaimant(state, agentID, runtimeRole, taskID); err != nil {
		return nil, nil, err
	}
	op := &claimOperation{
		projectRoot: lp.ProjectRoot(),
		statePath:   lp.StatePath(),
		bb:          bb,
		git:         git.New(lp.ProjectRoot()),
		taskID:      taskID,
		agentID:     agentID,
		runtimeRole: runtimeRole,
		taskStatus:  task.Status,
		config:      captureClaimConfig(state.Config),
		strategy:    strategy,
		authority:   authority,
		invocation:  invocation,
		session:     validationSession(sessions),
	}
	worktreeRel := path.Join(paths.WorktreesDirName, taskID)
	op.ctx = claimContext{
		authority:           authority,
		request:             &invocation.request,
		taskID:              taskID,
		agentID:             agentID,
		taskStatus:          task.Status,
		targetStatus:        statuses.executing,
		worktreeDir:         filepath.Join(lp.ProjectRoot(), worktreeRel),
		worktreeRel:         worktreeRel,
		integrationBranch:   op.config.integrationBranch,
		pipelineTransitions: BuildPipelineTransitions(resolver),
		resolver:            resolver,
	}
	if err := op.checkEligibility(state, task, statuses.doerRole); err != nil {
		return nil, nil, err
	}
	if err := op.enforceLimits(state, task); err != nil {
		return nil, nil, err
	}
	return op, nil, nil
}

// readClaimAdmissionState reads state for the first admission decision.
func readClaimAdmissionState(bb *db.Blackboard, taskID string, authority *models.AgentAuthority, invocation *ownershipInvocation) (*models.State, *models.Task, error) {
	state, task, err := readTaskState(bb, taskID)
	if err != nil {
		return nil, nil, err
	}
	if authority != nil {
		if err := RequireAgentAuthority(state, *authority); err != nil {
			return nil, nil, err
		}
	}
	invocation.observe(task)
	// Fast refusal only; each claim-triggered write below rechecks under its lock.
	if err := RequireWorkAdmitted(state, claimTaskOperation); err != nil {
		return nil, nil, err
	}
	if testClaimTaskHooks != nil && testClaimTaskHooks.afterInitialAdmission != nil {
		testClaimTaskHooks.afterInitialAdmission()
	}
	return state, task, nil
}

// resolveClaimStatuses resolves the role pair's statuses and refuses a claimant
// that is not the pair's doer.
func resolveClaimStatuses(resolver *pipeline.Resolver, task *models.Task, runtimeRole string) (claimStatuses, error) {
	var statuses claimStatuses
	if task.RolePair == "" {
		return statuses, &PreconditionError{Reason: fmt.Sprintf("task %s has no role_pair set", task.ID)}
	}
	var err error
	if statuses.initial, err = resolver.InitialStatus(task.RolePair); err != nil {
		return statuses, fmt.Errorf("invalid role-pair %q: %w", task.RolePair, err)
	}
	if statuses.rejected, err = resolver.RejectedStatus(task.RolePair); err != nil {
		return statuses, fmt.Errorf("invalid role-pair %q: %w", task.RolePair, err)
	}
	if statuses.executing, err = resolver.ExecutingStatus(task.RolePair); err != nil {
		return statuses, fmt.Errorf("invalid role-pair %q: %w", task.RolePair, err)
	}
	if statuses.doerRole, err = resolver.DoerRole(task.RolePair); err != nil {
		return statuses, fmt.Errorf("invalid role-pair %q: %w", task.RolePair, err)
	}
	if runtimeRole != statuses.doerRole {
		return statuses, &PreconditionError{Reason: "agent role cannot claim this task"}
	}
	return statuses, nil
}

// establishClaimRequest builds the claim request on the admitted boundary. A
// stranded holder is taken over first, which advances that boundary, so the
// claim continues under a request rebuilt on the released state. A retained
// receipt for the request returns its replayed result.
func establishClaimRequest(bb *db.Blackboard, statePath string, state *models.State, task *models.Task, agentID, runtimeRole string, resolver *pipeline.Resolver, authority *models.AgentAuthority, invocation *ownershipInvocation) (*models.State, *models.Task, *ClaimResult, error) {
	request, err := NewLifecycleRequest(claimTaskOperation, task, agentID, authority, invocation.opts, nil)
	if err != nil {
		return nil, nil, nil, err
	}
	invocation.request = request
	if models.StrandedDoerClaimReason(state, task, resolver, time.Now().UTC()) != "" {
		// The takeover advances the boundary the request was built against, so
		// the claim continues under a request rebuilt on the released state.
		if state, task, err = takeOverStrandedDoerClaim(bb, statePath, state, task, agentID, runtimeRole, resolver, authority, invocation); err != nil {
			return nil, nil, nil, err
		}
		if request, err = NewLifecycleRequest(claimTaskOperation, task, agentID, authority, invocation.opts, nil); err != nil {
			return nil, nil, nil, err
		}
		invocation.request = request
	}
	receipt, err := CheckLifecycleRequest(task, request, state.Agents)
	if err != nil {
		return nil, nil, nil, err
	}
	if receipt != nil {
		invocation.outcome = LifecycleReplayOutcome(task, receipt, agentID)
		return state, task, replayClaimResult(task, receipt, agentID), nil
	}
	return state, task, nil, nil
}

// selectClaimStrategy picks the claim variation for the task's current status.
func selectClaimStrategy(task *models.Task, statuses claimStatuses, runtimeRole string) (claimStrategy, error) {
	// Sentinel guard: reject claims on tasks in transition (e.g. "$transitioning"
	// during TransitionToNewAttempt). Defense-in-depth — IsClaimable also checks this.
	if task.AssignedTo != nil && strings.HasPrefix(*task.AssignedTo, "$") {
		return nil, &PreconditionError{Reason: fmt.Sprintf("task %s is in transition (assigned_to: %s)", task.ID, *task.AssignedTo)}
	}
	switch task.Status {
	case statuses.initial:
		if task.Worktree != nil {
			return preservedInitialClaimStrategy{}, nil
		}
		return freshClaimStrategy{}, nil
	case statuses.rejected:
		return rejectedClaimStrategy{}, nil
	case models.TaskStatusIntegrationFailed:
		return integrationFixClaimStrategy{}, nil
	}
	return nil, &PreconditionError{Reason: fmt.Sprintf("task %s is %s (not claimable by %s)", task.ID, task.Status, runtimeRole)}
}

func captureClaimConfig(cfg models.Config) claimConfig {
	leaseDuration := cfg.LeaseDuration
	if leaseDuration == 0 {
		leaseDuration = models.DefaultLeaseDurationSeconds
	}
	return claimConfig{
		integrationBranch:      cfg.IntegrationBranch,
		postWorktreeCmd:        cfg.PostWorktreeCmd,
		postWorktreeCmdTimeout: models.EffectivePostWorktreeCmdTimeout(cfg),
		copyWorktreeEnvFiles:   cfg.CopyWorktreeEnvFiles,
		scipSearchLanguages:    append([]string(nil), cfg.ScipSearch...),
		leaseDuration:          leaseDuration,
	}
}

// checkEligibility applies the strategy's own validation and the dependency
// and claimant blocks to the admitted state.
func (op *claimOperation) checkEligibility(state *models.State, task *models.Task, doerRole string) error {
	if err := op.strategy.validate(task, state, op.runtimeRole, doerRole, &op.ctx); err != nil {
		return err
	}
	if unmet := models.UnmetProviderDependencies(task, state.Tasks, op.ctx.resolver); len(unmet) > 0 {
		return &PreconditionError{Reason: "task has unmet provider dependencies: " + formatDependencyResults(unmet)}
	}
	if reason := models.DoerClaimBlockedReason(state, task, op.runtimeRole, op.agentID, op.ctx.resolver, time.Now().UTC()); reason != "" {
		return &PreconditionError{Reason: reason}
	}
	return nil
}

// enforceLimits enforces coder iteration/review-cycle limits before any
// filesystem work. Attempt 1 cap hit → new attempt (TransitionToNewAttempt).
// Attempt 2 cap hit → BLOCKED. An escalation always ends the claim with a
// LifecycleError.
func (op *claimOperation) enforceLimits(state *models.State, task *models.Task) error {
	_, preservedClaim := op.strategy.(preservedInitialClaimStrategy)
	continuingPreservedClaim := preservedClaim && task.Continuation && task.Iteration > 0
	if !op.strategy.enforceIterationLimit() && !preservedClaim {
		return nil
	}
	iterationToCheck := task.Iteration
	if continuingPreservedClaim {
		// Resuming the same iteration does not waive the review budget.
		iterationToCheck = 0
	}
	escalation, shouldEscalate := classifyLimitEscalation(
		task.ReviewCyclesCurrent,
		effectiveReviewCycleLimit(state.Config),
		iterationToCheck,
		effectiveCoderIterationLimit(task, state.Config),
		task.EffectiveAttempt(),
	)
	if !shouldEscalate {
		return nil
	}
	switch escalation.action {
	case LimitActionNewAttempt:
		result, taErr := transitionToNewAttemptForClaim(op.projectRoot, op.taskID, escalation.reason, op.authority, op.invocation.opts)
		if taErr != nil {
			return fmt.Errorf("failed to transition to new attempt: %w", taErr)
		}
		op.invocation.effects = true
		outcome := result.LifecycleOutcome
		outcome.Operation, outcome.Outcome, outcome.SafeAction = claimTaskOperation, models.LifecycleAlreadyTransitioned, string(LifecycleSafeActionStop)
		return &LifecycleError{Outcome: outcome, Err: &PreconditionError{Reason: fmt.Sprintf(
			"task %s exhausted limits, transitioned to attempt %d — claimable on next cycle",
			op.taskID, result.NewAttempt,
		)}}
	case LimitActionBlocked:
		if err := enforceBlockedEscalation(op.bb, op.taskID, op.agentID, op.taskStatus, op.ctx.pipelineTransitions, escalation, op.authority, &op.invocation.outcome); err != nil {
			return fmt.Errorf("failed to enforce escalation limit: %w", err)
		}
		op.invocation.effects = true
		return &LifecycleError{Outcome: op.invocation.outcome, Err: &PreconditionError{Reason: fmt.Sprintf(
			"task %s transitioned to BLOCKED — %s", op.taskID, escalation.reason,
		)}}
	}
	return nil
}

// replayUnderTaskLock handles a concurrent identical call that completed while
// this invocation waited for the task lock. It replays before ordinary
// claimability is checked.
func (op *claimOperation) replayUnderTaskLock() (*ClaimResult, error) {
	state, current, err := readTaskState(op.bb, op.taskID)
	if err != nil {
		return nil, err
	}
	if op.authority != nil {
		if err := RequireAgentAuthority(state, *op.authority); err != nil {
			return nil, err
		}
	}
	op.invocation.observe(current)
	receipt, err := CheckLifecycleRequest(current, op.invocation.request, state.Agents)
	if err != nil {
		return nil, err
	}
	if receipt == nil {
		return nil, nil
	}
	op.invocation.outcome = LifecycleReplayOutcome(current, receipt, op.agentID)
	return replayClaimResult(current, receipt, op.agentID), nil
}

// captureEvidence records the integration commit this claim prepares and
// assigns against, and the acceptance evidence for it. Rejected claims replace
// baseCommit with the preserved base, so the captured integration commit also
// stays their rebase target.
func (op *claimOperation) captureEvidence(lockedTask *models.Task) (*acceptanceInput, error) {
	if _, rejected := op.strategy.(rejectedClaimStrategy); rejected && lockedTask.BaseCommit != nil {
		op.ctx.preservedBaseCommit = strings.TrimSpace(*lockedTask.BaseCommit)
	}
	failure := "failed to get integration branch commit: %w"
	if _, preserved := op.strategy.(preservedInitialClaimStrategy); preserved {
		op.ctx.preservedBaseCommit = *lockedTask.BaseCommit
		failure = "failed to get preserved claim integration commit: %w"
	}
	baseCommit, err := op.git.GetCommitSHA(op.config.integrationBranch)
	if err != nil {
		return nil, fmt.Errorf(failure, err)
	}
	op.ctx.baseCommit = baseCommit
	op.ctx.integrationCommit = baseCommit
	acceptanceState, err := op.bb.Read()
	if err != nil {
		return nil, err
	}
	acceptance, err := loadAcceptanceInput(op.projectRoot, acceptanceState, lockedTask, baseCommit)
	if err != nil {
		observeAcceptanceClaimRefusal(err, acceptanceState, lockedTask, baseCommit)
		return nil, err
	}
	// A doer claim needs every declared runtime input resolvable now, so a
	// missing one blocks the task before a coding turn is spent (ADR-0169).
	if refusal := checkRuntimeInputReadiness(op.projectRoot, acceptanceState, lockedTask, false); refusal != nil {
		observeAcceptanceClaimRefusal(refusal, acceptanceState, lockedTask, baseCommit)
		return nil, refusal
	}
	return acceptance, nil
}

// persistPreparation reserves the external effect while both the task worktree
// lock and the generation-fenced state transaction establish the current
// boundary.
func (op *claimOperation) persistPreparation() (models.LifecyclePreparation, error) {
	var preparation models.LifecyclePreparation
	err := lifecycleMutation(op.bb, op.authority)(func(state *models.State) error {
		if err := RequireWorkAdmitted(state, claimTaskOperation); err != nil {
			return err
		}
		task := state.FindTask(op.taskID)
		if task == nil {
			return &errors.NotFoundError{Entity: notFoundEntityTask, ID: op.taskID}
		}
		op.invocation.observe(task)
		if task.Status != op.taskStatus {
			return fmt.Errorf("task changed before claim preparation")
		}
		if _, err := requireRegisteredClaimAgent(state, op.agentID, op.runtimeRole); err != nil {
			return err
		}
		if reason := models.DoerClaimBlockedReason(state, task, op.runtimeRole, op.agentID, op.ctx.resolver, time.Now().UTC()); reason != "" {
			return &PreconditionError{Reason: reason}
		}
		if err := PrepareLifecycleRequest(task, op.invocation.request, state.Agents); err != nil {
			return err
		}
		preparation = *task.Lifecycle.Preparation
		return nil
	})
	return preparation, err
}

func enforceBlockedEscalation(
	bb *db.Blackboard,
	taskID, agentID string,
	expectedStatus models.TaskStatus,
	pipelineTransitions map[models.TaskStatus][]models.TaskStatus,
	escalation limitEscalation,
	authority *models.AgentAuthority,
	outcomes ...*models.LifecycleOutcome,
) error {
	now := time.Now().UTC()

	return lifecycleMutation(bb, authority)(func(state *models.State) error {
		// Claim-triggered: a halted system must not block the task on a claim's behalf.
		if err := RequireWorkAdmitted(state, claimTaskOperation); err != nil {
			return err
		}
		task := state.FindTask(taskID)
		if task == nil {
			return &errors.NotFoundError{Entity: notFoundEntityTask, ID: taskID}
		}
		if task.Status != expectedStatus {
			return fmt.Errorf("race condition: task status changed from %s to %s", expectedStatus, task.Status)
		}

		// TOCTOU re-verification: re-classify with current state values under lock.
		iterationLimit := effectiveCoderIterationLimit(task, state.Config)
		reviewLimit := effectiveReviewCycleLimit(state.Config)
		_, stillEscalated := classifyLimitEscalation(
			task.ReviewCyclesCurrent, reviewLimit,
			task.Iteration, iterationLimit,
			task.EffectiveAttempt(),
		)
		if !stillEscalated {
			return fmt.Errorf("race condition: escalation condition no longer holds")
		}

		blockedReason := escalation.reason
		questions := escalation.questions

		if err := task.TransitionWith(models.TaskStatusBlocked, pipelineTransitions); err != nil {
			return err
		}
		task.BlockedReason = &blockedReason
		task.BlockedQuestions = questions
		task.LeaseExpires = nil

		if task.AssignedTo != nil {
			previous := *task.AssignedTo
			if a, ok := state.Agents[previous]; ok {
				if a.CurrentTask != nil && *a.CurrentTask == task.ID {
					state.ReleaseAgent(previous)
				}
			}
		}
		task.AssignedTo = nil
		models.AdvanceLifecycle(task)

		agentPtr := &agentID
		reasonPtr := &blockedReason
		task.History = append(task.History, models.TaskHistoryEntry{
			Time:   now,
			Event:  models.TaskEventBlocked,
			Agent:  agentPtr,
			Reason: reasonPtr,
		})
		for _, outcome := range outcomes {
			*outcome = NewLifecycleOutcome(claimTaskOperation, task, models.LifecycleAlreadyTransitioned, LifecycleSafeActionStop, LifecycleEffectsCommitted)
		}

		return nil
	})
}
