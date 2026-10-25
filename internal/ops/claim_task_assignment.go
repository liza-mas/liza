package ops

import (
	"fmt"
	"os"
	"time"

	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/statevalidate"
)

// validationContextChanged is the preflight failure code of a claim or resume
// whose revalidated context no longer matches the one observed.
const validationContextChanged = "context_changed"

// claimAssignment is phase three: the locked recheck of current state and the
// commit of the claim. The operation's captured evidence is compared with
// current state under the lock; it is not used as authority for the write.
type claimAssignment struct {
	op           *claimOperation
	acceptance   *acceptanceInput
	preflight    *ValidationPreflight
	now          time.Time
	leaseExpires time.Time
}

func (op *claimOperation) newAssignment(acceptance *acceptanceInput, preflight *ValidationPreflight) *claimAssignment {
	now := time.Now().UTC()
	a := &claimAssignment{op: op, acceptance: acceptance, preflight: preflight, now: now}
	a.leaseExpires = now.Add(time.Duration(op.config.leaseDuration) * time.Second)
	op.ctx.leaseExpires = a.leaseExpires
	return a
}

// commit assigns the claim. A preserved initial claim holds the completion
// lock across the final integration-ref equality check and the assignment,
// ordering both against cooperating integration movement, without holding the
// integration mutation lock across the blackboard write.
func (a *claimAssignment) commit() error {
	op := a.op
	if _, preserved := op.strategy.(preservedInitialClaimStrategy); !preserved {
		return lifecycleMutation(op.bb, op.authority)(a.apply)
	}
	return withEffectiveIntegrationCompletionLinearization(op.projectRoot, "claim preserved task "+op.taskID, func() error {
		if err := a.verifyPreservedBoundary(); err != nil {
			return err
		}
		if testClaimTaskHooks != nil && testClaimTaskHooks.afterPreservedIntegrationEqualityCheck != nil {
			testClaimTaskHooks.afterPreservedIntegrationEqualityCheck()
		}
		return lifecycleMutation(op.bb, op.authority)(a.apply)
	})
}

// verifyPreservedBoundary compares the captured integration commit and
// worktree head with the repository as it is now.
func (a *claimAssignment) verifyPreservedBoundary() error {
	op := a.op
	var currentIntegrationCommit string
	if lockErr := withIntegrationMutationLock(op.projectRoot, "verify preserved claim integration "+op.taskID, func() error {
		var resolveErr error
		currentIntegrationCommit, resolveErr = op.git.GetCommitSHA(op.config.integrationBranch)
		return resolveErr
	}); lockErr != nil {
		return fmt.Errorf("verify preserved claim integration ref: %w", lockErr)
	}
	if currentIntegrationCommit != op.ctx.baseCommit {
		return fmt.Errorf(
			"integration ref changed before preserved claim assignment: got %s, want %s",
			currentIntegrationCommit,
			op.ctx.baseCommit,
		)
	}
	currentHead, headErr := op.git.GetWorktreeHEAD(op.taskID)
	if headErr != nil {
		return fmt.Errorf("read preserved worktree HEAD before assignment: %w", headErr)
	}
	if currentHead != op.ctx.worktreeHead {
		return fmt.Errorf("preserved worktree HEAD changed before assignment: got %s, want %s", currentHead, op.ctx.worktreeHead)
	}
	ancestor, ancestorErr := op.git.IsAncestor(op.ctx.baseCommit, currentHead)
	if ancestorErr != nil {
		return fmt.Errorf("verify preserved worktree ancestry before assignment: %w", ancestorErr)
	}
	if !ancestor {
		return fmt.Errorf("preserved worktree HEAD %s does not descend from captured integration commit %s", currentHead, op.ctx.baseCommit)
	}
	return nil
}

// recheckCurrent re-validates the locked task and claimant against the
// captured evidence and returns them for the write.
func (a *claimAssignment) recheckCurrent(state *models.State) (*models.Task, models.Agent, error) {
	op := a.op
	if err := RequireWorkAdmitted(state, claimTaskOperation); err != nil {
		return nil, models.Agent{}, err
	}
	if err := a.preflight.CheckCurrent(state); err != nil {
		return nil, models.Agent{}, err
	}
	// Re-check task exists and status hasn't changed
	task := state.FindTask(op.taskID)
	if task == nil {
		return nil, models.Agent{}, &errors.NotFoundError{Entity: notFoundEntityTask, ID: op.taskID}
	}
	if len(task.ValidationPrerequisites) > 0 && a.preflight == nil {
		return nil, models.Agent{}, validationError(validationContextChanged)
	}
	op.invocation.observe(task)
	if task.Status != op.taskStatus {
		return nil, models.Agent{}, fmt.Errorf("race condition: task status changed from %s to %s", op.taskStatus, task.Status)
	}
	if reason := models.DoerClaimBlockedReason(state, task, op.runtimeRole, op.agentID, op.ctx.resolver, a.now); reason != "" {
		return nil, models.Agent{}, &PreconditionError{Reason: reason}
	}
	if err := ValidateLifecyclePreparation(task, op.invocation.request); err != nil {
		return nil, models.Agent{}, err
	}
	// Verify worktree health before committing state (unconditional —
	// concurrent RemoveWorktree can corrupt even pre-existing worktrees).
	if err := op.git.ValidateWorktreeHealth(op.taskID); err != nil {
		return nil, models.Agent{}, &PreconditionError{Reason: fmt.Sprintf("worktree not healthy: %v", err)}
	}
	// Re-check dependencies under lock for strategies that require it
	if op.strategy.requiresDependencyRecheck() || len(task.ProviderDependencies) > 0 || len(task.ProviderReservations) > 0 {
		if unmet := models.UnmetClaimDependencies(task, state, op.ctx.resolver); len(unmet) > 0 {
			return nil, models.Agent{}, fmt.Errorf("race condition: dependencies changed: %s", formatDependencyResults(unmet))
		}
	}
	// Re-check agent availability
	agent, err := requireRegisteredClaimAgent(state, op.agentID, op.runtimeRole)
	if err != nil {
		return nil, models.Agent{}, err
	}
	if agent.CurrentTask != nil && *agent.CurrentTask != "" && *agent.CurrentTask != op.taskID {
		return nil, models.Agent{}, fmt.Errorf("race condition: agent %s became busy with %s", op.agentID, *agent.CurrentTask)
	}
	return task, agent, nil
}

// apply is the locked state mutation: recheck, transition, assignment, history
// and the completion receipt.
func (a *claimAssignment) apply(state *models.State) error {
	op := a.op
	task, agent, err := a.recheckCurrent(state)
	if err != nil {
		return err
	}
	agentID, taskID, leaseExpires := op.agentID, op.taskID, a.leaseExpires
	if err := recheckAcceptanceReceipt(op.projectRoot, state, task, a.acceptance, nil); err != nil {
		return err
	}
	if err := task.TransitionWith(op.ctx.targetStatus, op.ctx.pipelineTransitions); err != nil {
		return err
	}
	assignmentsReleased, err := a.releaseRetainedAssignments(state)
	if err != nil {
		return err
	}
	task.AssignedTo = &agentID
	task.LeaseExpires = &leaseExpires

	// A continuation resumes the preserved work of the current iteration;
	// every other claim starts one (0 -> 1 on first claim, then 2, 3, etc.).
	_, preserved := op.strategy.(preservedInitialClaimStrategy)
	op.ctx.continuation = preserved && task.Continuation && task.Iteration > 0
	task.Continuation = false
	if !op.ctx.continuation {
		task.Iteration++
	}

	op.strategy.mutateTask(task, &op.ctx)
	if a.acceptance != nil {
		task.AcceptanceSource = &a.acceptance.source
		task.AcceptanceReceipt = nil
	}
	task.History = append(task.History, op.strategy.historyEntry(a.now, &op.ctx))

	agent.Status = models.AgentStatusWorking
	agent.CurrentTask = &taskID
	agent.LeaseExpires = &leaseExpires
	agent.Heartbeat = a.now
	state.Agents[agentID] = agent
	_, rejected := op.strategy.(rejectedClaimStrategy)
	if rejected || assignmentsReleased {
		if err := statevalidate.ValidateCandidate(state, op.bb.ReadSnapshot, op.projectRoot, true, os.Stderr); err != nil {
			return fmt.Errorf("claim produced invalid state: %w", err)
		}
	}

	var completeErr error
	op.invocation.outcome, completeErr = CompleteLifecycleRequest(task, op.invocation.request, models.LifecycleProjection{
		SourceStatus: op.taskStatus, BaseCommit: op.ctx.baseCommit, LeaseExpires: leaseExpires.Format(time.RFC3339Nano), Attempt: task.EffectiveAttempt(), Iteration: task.Iteration,
	}, state.Agents)
	return completeErr
}

// releaseRetainedAssignments reconciles the claimant's other retained
// assignments in this transaction. CurrentTask alone misses assignments kept
// after a verdict, and a failed claim must not release prior work.
func (a *claimAssignment) releaseRetainedAssignments(state *models.State) (bool, error) {
	op := a.op
	agentID, resolver := op.agentID, op.ctx.resolver
	released := false
	for i := range state.Tasks {
		previous := &state.Tasks[i]
		if previous.ID == op.taskID || previous.AssignedTo == nil || *previous.AssignedTo != agentID || models.IsOperationallyTerminal(previous, resolver) {
			continue
		}
		if models.IsExecutingStatus(previous, resolver) {
			return false, &PreconditionError{Reason: fmt.Sprintf("agent %s is already assigned to executing task %s", agentID, previous.ID)}
		}
		previous.AssignedTo = nil
		previous.LeaseExpires = nil
		released = true
		models.AdvanceLifecycle(previous)
		reason := fmt.Sprintf("doer claimed task %s", op.taskID)
		previous.History = append(previous.History, models.TaskHistoryEntry{
			Time: a.now, Event: models.TaskEventDoerClaimReleased,
			Agent: &agentID, PreviousAssignee: &agentID, Reason: &reason,
			Extra: map[string]any{"claimed_task": op.taskID},
		})
	}
	return released, nil
}

func (op *claimOperation) result(prepared claimPreparedWorktree, leaseExpires time.Time) *ClaimResult {
	return &ClaimResult{
		LifecycleOutcome:  op.invocation.outcome,
		TaskID:            op.taskID,
		AgentID:           op.agentID,
		SourceStatus:      op.taskStatus,
		WorktreeRel:       op.ctx.worktreeRel,
		BaseCommit:        op.ctx.baseCommit,
		LeaseExpires:      leaseExpires,
		IntegrationFix:    op.taskStatus == models.TaskStatusIntegrationFailed,
		PreviousAssignee:  op.ctx.previousAssignee,
		WorktreeRecreated: prepared.phase.deleted && prepared.phase.created,
		Warnings:          prepared.warnings.all(),
	}
}
