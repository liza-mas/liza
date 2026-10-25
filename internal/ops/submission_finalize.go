package ops

import (
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
)

// finalize publishes the prepared candidate after the warning-only index
// refresh: it rechecks the candidate in Git, executes the acceptance batch under
// the ownership fence, then commits the final transaction. The caller holds the
// task lock; the blackboard lock is taken only by the transaction.
func (s *submissionRun) finalize() (*SubmitForReviewResult, error) {
	if err := s.recheckCandidate(); err != nil {
		return nil, err
	}
	if err := s.recheckPreparation(); err != nil {
		return nil, err
	}
	receipt, err := s.executeAcceptance()
	if err != nil {
		return nil, err
	}
	outcome, err := s.publish(receipt)
	if err != nil {
		return nil, err
	}
	result := &SubmitForReviewResult{
		LifecycleOutcome: outcome,
		TaskID:           s.taskID,
		ReviewCommit:     s.postRebaseCommit,
		AgentID:          s.agentID,
	}
	if receipt != nil {
		result.DeferredCommands = receipt.Deferred
	}
	return result, nil
}

// recheckCandidate verifies that indexing, which ran without task locking,
// left the worktree on the branch and at the commit about to be published.
func (s *submissionRun) recheckCandidate() error {
	liveBranch, err := s.git.GetWorktreeBranch(s.worktreePath)
	if err != nil {
		return err
	}
	liveHEAD, err := s.git.GetWorktreeHEAD(s.taskID)
	if err != nil {
		return err
	}
	if liveBranch != s.expectedBranch || liveHEAD != s.postRebaseCommit {
		return &LifecycleError{Outcome: NewLifecycleOutcome(s.request.Operation, s.invocation.task, models.LifecycleStateChanged, LifecycleSafeActionRequery, LifecycleEffectsUnknown), Err: fmt.Errorf("worktree candidate changed while refreshing indexes")}
	}
	return nil
}

// recheckPreparation reads the live state and requires the caller's authority
// and this request's preparation marker to be still current.
func (s *submissionRun) recheckPreparation() error {
	current, err := s.bb.Read()
	if err != nil {
		return err
	}
	if s.authority != nil {
		if err := RequireAgentAuthority(current, *s.authority); err != nil {
			return err
		}
	}
	return ValidateLifecyclePreparation(current.FindTask(s.taskID), s.request)
}

// executeAcceptance runs the acceptance batch, if the task declares one, on the
// rebased candidate under the ownership fence, with the slots pre-admission
// held or its own.
func (s *submissionRun) executeAcceptance() (*models.AcceptanceReceipt, error) {
	gate := runtimeInputGate{projectRoot: s.projectRoot, bb: s.bb, authority: s.authority, actor: s.agentID, operation: integrationOperationSubmitForReview}
	slots, ownSlots := s.held, s.held == nil && s.acceptance != nil
	if ownSlots {
		// Execution would take its slots itself; taken here so they
		// report to this submit's progress.
		slots = newSubmitAdmissionSlots(s.acceptance.batchTimeoutSeconds)
		slots.progress = s.invocation.progress
	}
	fenceCtx, stopFence := startAcceptanceOwnershipFence(s.bb, s.taskID, s.expectedStatus, s.agentID, s.authority)
	receipt, err := executeAcceptanceReceipt(fenceCtx, s.projectRoot, s.task, s.acceptance, s.postRebaseCommit, gate, s.admissionConfig, slots, s.retryPlan)
	if ownSlots {
		slots.release()
	}
	if stopFence() {
		return nil, &LifecycleError{Outcome: NewLifecycleOutcome(s.request.Operation, s.invocation.task, models.LifecycleStateChanged, LifecycleSafeActionRequery, LifecycleEffectsUnknown), Err: fmt.Errorf("submission ownership lost during canonical validation; validation stopped")}
	}
	return receipt, err
}

// publish is phase 3: one atomic update with the new commit SHA.
func (s *submissionRun) publish(receipt *models.AcceptanceReceipt) (models.LifecycleOutcome, error) {
	var outcome models.LifecycleOutcome
	now := time.Now().UTC()
	if submitReviewBeforeModifyTestHook != nil {
		submitReviewBeforeModifyTestHook()
	}

	err := modifyLifecycleState(s.bb, s.authority, func(current *models.State) error {
		var commitErr error
		outcome, commitErr = s.commitSubmission(current, receipt, now)
		return commitErr
	})
	if err != nil {
		return outcome, &OperationalError{
			Code:    operationalCodeStateWrite,
			Phase:   "write-state",
			Message: "failed to submit task for review",
			Details: map[string]any{
				"operation": integrationOperationSubmitForReview,
				"task_id":   s.taskID,
				// Err stays out of the agent envelope by design, but
				// withholding it entirely leaves a doer with a refusal
				// it cannot act on: two agents requeried, correctly
				// refused to blind-retry a requery action, and blocked
				// without ever naming the cause, because it was
				// discarded here. Details is the channel meant for
				// recovery, so the cause goes through it bounded and
				// masked rather than raw.
				"cause":         boundedMaskedErrorString(err, submitReviewCauseLimit),
				"recovery_hint": "Re-read task state, resolve any concurrent state change or validation issue, then retry submit-for-review.",
			},
			Err: err,
		}
	}
	return outcome, nil
}

// commitSubmission is the body of the final transaction. It receives the state
// the transaction holds and opens no lock. It revalidates everything the
// preparation relied on, including the bounded-correction diff (K1-E11) and
// the acceptance receipt (K1-E7), which both read Git inside the transaction.
func (s *submissionRun) commitSubmission(current *models.State, receipt *models.AcceptanceReceipt, now time.Time) (models.LifecycleOutcome, error) {
	task := current.FindTask(s.taskID)
	if task == nil {
		return models.LifecycleOutcome{}, &errors.NotFoundError{Entity: "task", ID: s.taskID}
	}
	s.invocation.task = task
	if err := s.guardCommit(current, task, receipt); err != nil {
		return models.LifecycleOutcome{}, err
	}
	return s.applySubmission(current, task, receipt, now)
}

// guardCommit refuses the commit when the task, its output or its plan
// amendment changed since preparation, or the receipt no longer holds.
func (s *submissionRun) guardCommit(current *models.State, task *models.Task, receipt *models.AcceptanceReceipt) error {
	if err := ValidateLifecyclePreparation(task, s.request); err != nil {
		return err
	}
	if task.Worktree == nil || *task.Worktree != *s.boundaryTask.Worktree {
		return &LifecycleError{Outcome: NewLifecycleOutcome(s.request.Operation, task, models.LifecycleStateChanged, LifecycleSafeActionRequery, LifecycleEffectsUnknown), Err: fmt.Errorf("task worktree changed before submission")}
	}

	if task.Status != s.expectedStatus {
		return &PreconditionError{Reason: fmt.Sprintf("task %s is not %s (current status: %s)", s.taskID, s.expectedStatus, task.Status)}
	}

	if task.AssignedTo == nil || *task.AssignedTo != s.agentID {
		return &PreconditionError{Reason: notAssignedReason(s.taskID, s.agentID, task)}
	}
	if err := validateOutputArtifactRefScalars(s.taskID, task.Output); err != nil {
		return err
	}
	if err := rejectStaleDraftOutput(current, s.resolver, task); err != nil {
		return err
	}
	if original := models.PendingPlanAmendment(current, task); original != "" {
		return &PreconditionError{Reason: "reviewed plan amendment pending for " + original}
	}
	bounded := *task
	bounded.BaseCommit = &s.rebaseBase
	if err := validateBoundedContractCorrection(s.projectRoot, current, current.FindTask(task.AmendsPlan), &bounded, s.postRebaseCommit); err != nil {
		return err
	}
	if err := checkPlanningOutputSnapshot(&s.boundaryTask, task); err != nil {
		return err
	}
	return recheckAcceptanceReceipt(s.projectRoot, current, task, s.acceptance, receipt)
}

// applySubmission moves the task to the submitted status, records the review
// boundary, history and handoff, releases the agent and completes the request.
func (s *submissionRun) applySubmission(current *models.State, task *models.Task, receipt *models.AcceptanceReceipt, now time.Time) (models.LifecycleOutcome, error) {
	if receipt != nil {
		task.AcceptanceSource = &receipt.Source
	}
	task.AcceptanceReceipt = receipt
	if err := task.TransitionWith(s.targetStatus, s.transitions); err != nil {
		return models.LifecycleOutcome{}, err
	}
	task.ReviewCommit = &s.postRebaseCommit
	// Update BaseCommit from "branched from" to "rebased onto" — this is the
	// integration HEAD the rebase targeted, ensuring the reviewer diffs only
	// the coder's changes (not integration commits that landed since claim).
	task.BaseCommit = &s.rebaseBase

	task.History = append(task.History, models.TaskHistoryEntry{
		Time:                  now,
		Event:                 models.TaskEventSubmittedForReview,
		Agent:                 &s.agentID,
		Commit:                &s.postRebaseCommit,
		SubmissionInputCommit: s.preRebaseCommit,
		SubmissionAttempt:     task.EffectiveAttempt(),
	})

	task.HandoffEvents = append(task.HandoffEvents, models.HandoffEvent{
		Timestamp: now,
		Agent:     s.agentID,
		Trigger:   models.HandoffTriggerSubmission,
	})

	if agent, ok := current.Agents[s.agentID]; ok {
		agent.Status = models.AgentStatusWaiting
		agent.CurrentTask = nil
		agent.LeaseExpires = nil
		current.Agents[s.agentID] = agent
	}

	return CompleteLifecycleRequest(task, s.request, models.LifecycleProjection{
		InputCommit: s.preRebaseCommit, ReviewCommit: s.postRebaseCommit, BaseCommit: s.rebaseBase,
		Attempt: task.EffectiveAttempt(), Iteration: task.Iteration, SourceStatus: s.expectedStatus,
	}, current.Agents)
}
