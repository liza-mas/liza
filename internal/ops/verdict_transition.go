package ops

import (
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/payloadschema"
	"github.com/liza-mas/liza/internal/pipeline"
)

// submittedVerdict is the closed set of verdicts a reviewer submits. Persisted
// and wire fields (VerdictResult.Verdict, quarantined findings, history) stay
// strings; convert at those boundaries.
type submittedVerdict string

const (
	submittedVerdictApproved submittedVerdict = "APPROVED"
	submittedVerdictRejected submittedVerdict = "REJECTED"
)

// History Extra keys the rejection-RCA gate writes beside the ones analyze.go
// reads back (rejectionRCA*Key); the telemetry does not read these two.
const (
	rejectionRCAThresholdKey      = "threshold"
	rejectionRCARejectionCountKey = "rejection_count"
)

// verdictPipeline is the pipeline vocabulary one verdict is judged against,
// resolved from the task's role pair before any Git or state write.
type verdictPipeline struct {
	resolver *pipeline.Resolver
	// reviewing2 and partiallyApproved are optional (quorum 1 declares neither),
	// and clean is declared only by the integration pair.
	reviewing, reviewing2, approved, rejected, partiallyApproved, clean models.TaskStatus
	transitions                                                         map[models.TaskStatus][]models.TaskStatus
}

// verdictEffects is what the verdict transaction decided, read by the result
// projection once the transaction committed.
type verdictEffects struct {
	outcome            models.LifecycleOutcome
	replayed           bool
	escalatedToBlocked bool
	blockedReason      string
	rejectionRCAGated  bool
	newAttemptNeeded   bool
	newAttemptReason   string
}

// isCleanIntegrationApproval reports whether an approval would close the
// goal-level integration analysis cleanly, which completion linearization
// orders against integration movement.
func (a *verdictAttempt) isCleanIntegrationApproval(task *models.Task) bool {
	return a.verdict == submittedVerdictApproved && task.IntegrationAnalysis != nil &&
		task.IntegrationAnalysis.Phase == models.IntegrationAnalysisPhaseGlobal && len(task.Output) == 0
}

func notReviewingReason(taskID string, status models.TaskStatus) string {
	return fmt.Sprintf("task %s is not in a reviewing state (current status: %s)", taskID, status)
}

// resolvePipeline loads the statuses the verdict moves the task between.
func (a *verdictAttempt) resolvePipeline(task *models.Task) error {
	resolver, _, resolverErr := loadResolver(a.projectRoot)
	if resolverErr != nil {
		return fmt.Errorf("failed to load pipeline config: %w", resolverErr)
	}
	if task.RolePair == "" {
		return &PreconditionError{Reason: fmt.Sprintf("task %s has no role_pair set", a.taskID)}
	}
	p := &verdictPipeline{resolver: resolver}
	var err error
	if p.reviewing, err = resolver.ReviewingStatus(task.RolePair); err != nil {
		return fmt.Errorf("invalid role-pair %q: %w", task.RolePair, err)
	}
	// Also accept reviewing_2 (second review in quorum flow).
	p.reviewing2, _ = resolver.Reviewing2Status(task.RolePair)
	if p.approved, err = resolver.ApprovedStatus(task.RolePair); err != nil {
		return fmt.Errorf("invalid role-pair %q: %w", task.RolePair, err)
	}
	if p.rejected, err = resolver.RejectedStatus(task.RolePair); err != nil {
		return fmt.Errorf("invalid role-pair %q: %w", task.RolePair, err)
	}
	// Quorum states are optional (they may not exist if quorum is always 1);
	// the clean state is declared only by the integration pair.
	p.partiallyApproved, _ = resolver.PartiallyApprovedStatus(task.RolePair)
	p.clean, _ = resolver.CleanStatus(task.RolePair)
	p.transitions = BuildPipelineTransitions(resolver)
	a.pipe = p
	return nil
}

// checkStandingBeforeGit is the fast-fail on the snapshot; the transaction
// re-checks it authoritatively.
func (a *verdictAttempt) checkStandingBeforeGit(task *models.Task) error {
	if !isReviewingStatus(task.Status, a.pipe.reviewing, a.pipe.reviewing2) {
		a.recordFailure = false
		return WrapLifecycleError(payloadschema.SubmitVerdictOperation, task, &PreconditionError{Reason: notReviewingReason(a.taskID, task.Status)}, models.LifecycleAlreadyTransitioned, LifecycleSafeActionStop, LifecycleEffectsNone)
	}
	if task.ReviewingBy == nil || *task.ReviewingBy != a.agentID {
		a.recordFailure = false
		return WrapLifecycleError(payloadschema.SubmitVerdictOperation, task, &PreconditionError{Reason: "only the assigned reviewer can submit a verdict"}, models.LifecycleStaleCaller, LifecycleSafeActionStop, LifecycleEffectsNone)
	}
	return nil
}

// resolveEffectiveImpact enforces that a verdict's impact never downgrades the
// impact resolved from history.
func (a *verdictAttempt) resolveEffectiveImpact(task *models.Task) error {
	a.effectiveImpact = ResolveEffectiveImpact(task.History)
	if a.impact == "" {
		return nil
	}
	if impactOrder[a.impact] < impactOrder[a.effectiveImpact] {
		return &PreconditionError{Reason: fmt.Sprintf("cannot downgrade impact from %q to %q — impact can only escalate", a.effectiveImpact, a.impact)}
	}
	a.effectiveImpact = a.impact
	return nil
}

// prepareCleanSourceVerification verifies the integration source of a clean
// verdict that would reach quorum, before the transaction opens.
func (a *verdictAttempt) prepareCleanSourceVerification(task *models.Task) error {
	if !a.isCleanIntegrationApproval(task) {
		return nil
	}
	effectiveQuorum, qErr := a.pipe.resolver.EffectiveQuorum(task.RolePair, pipeline.ReviewImpact(a.effectiveImpact))
	if qErr != nil {
		return fmt.Errorf("failed to resolve quorum: %w", qErr)
	}
	if task.ApprovalCount()+1 < effectiveQuorum {
		return nil
	}
	verification, verificationErr := verifyCleanIntegrationSourceForVerdict(a.projectRoot, task.IntegrationAnalysis.SourceCommit)
	if verificationErr != nil {
		return fmt.Errorf("failed to verify clean integration source: %w", verificationErr)
	}
	a.cleanSource = &verification
	return nil
}

// apply is the verdict transaction callback. It runs under the state lock and
// opens no lock: it revalidates the request, boundary and standing against the
// locked state, applies the approval or rejection, releases the reviewer, and
// completes the lifecycle request. Candidate validation of integration
// evidence (K1-E14) stays here, at the end, as before.
func (a *verdictAttempt) apply(state *models.State) error {
	task := state.FindTask(a.taskID)
	if task == nil {
		return &errors.NotFoundError{Entity: notFoundEntityTask, ID: a.taskID}
	}
	if err := a.revalidate(state, task); err != nil {
		return err
	}
	var previousLifecycleState *models.State
	if task.IntegrationAnalysis != nil {
		previousLifecycleState = snapshotIntegrationLifecycleState(state)
	}

	projectedIntegrationEvidence := false
	if a.verdict == submittedVerdictApproved {
		var err error
		if projectedIntegrationEvidence, err = a.approve(state, task); err != nil {
			return err
		}
	} else if err := a.reject(state, task); err != nil {
		return err
	}

	task.ReviewingBy = nil
	task.ReviewLeaseExpires = nil
	state.ReleaseAgent(a.agentID)

	if projectedIntegrationEvidence {
		if err := a.validateIntegrationEvidence(state, task, previousLifecycleState); err != nil {
			return err
		}
	}

	var completeErr error
	a.effects.outcome, completeErr = CompleteLifecycleRequest(task, a.request, models.LifecycleProjection{Verdict: string(a.verdict)}, state.Agents)
	return completeErr
}

// revalidate repeats, against the locked state, the request identity, the
// review boundary and the reviewer's standing the snapshot checked.
func (a *verdictAttempt) revalidate(state *models.State, task *models.Task) error {
	receipt, checkErr := CheckLifecycleRequest(task, a.request, state.Agents)
	if checkErr != nil {
		a.recordFailure = false
		if gateErr := rejectionRCAGateObservedError(task); gateErr != nil {
			return gateErr
		}
		return checkErr
	}
	if receipt != nil {
		a.effects.outcome, a.effects.replayed = LifecycleReplayOutcome(task, receipt, a.agentID), true
		return errLifecycleReplay
	}

	if err := validateVerdictReviewBoundary(state, task, string(a.verdict), a.reviewCommit); err != nil {
		return err
	}

	if !isReviewingStatus(task.Status, a.pipe.reviewing, a.pipe.reviewing2) {
		a.recordFailure = false
		return WrapLifecycleError(payloadschema.SubmitVerdictOperation, task, &PreconditionError{Reason: notReviewingReason(a.taskID, task.Status)}, models.LifecycleStateChanged, LifecycleSafeActionRequery, LifecycleEffectsNone)
	}
	if task.ReviewingBy == nil || *task.ReviewingBy != a.agentID {
		a.recordFailure = false
		return WrapLifecycleError(payloadschema.SubmitVerdictOperation, task, &PreconditionError{Reason: "review assignment changed"}, models.LifecycleStaleCaller, LifecycleSafeActionStop, LifecycleEffectsNone)
	}
	return nil
}

func (a *verdictAttempt) transition(task *models.Task, to models.TaskStatus) error {
	return task.TransitionWith(to, a.pipe.transitions)
}

// approve records the approval and moves the task to partially approved, clean
// or approved by quorum. It reports whether integration evidence was appended,
// which the transaction then validates as a candidate.
func (a *verdictAttempt) approve(state *models.State, task *models.Task) (projectedIntegrationEvidence bool, err error) {
	if task.AmendsPlan != "" {
		if err := rejectStaleDraftOutput(state, a.pipe.resolver, task); err != nil {
			a.recordFailure = false
			return false, err
		}
	}
	// Approved output reaches MERGED without another verdict (ADR-0188).
	if stale := staleOutputDeclaration(state, a.pipe.resolver, task.Output); stale != "" {
		a.recordFailure = false
		return false, &PreconditionError{Reason: fmt.Sprintf("task %s %s; it must be re-authored before approval: submit REJECTED citing the stale declaration", a.taskID, stale)}
	}
	// A fresh approval supersedes any stale integration attempt metadata
	// from an earlier failed merge/submission path. Keep review_commit:
	// wt-merge still needs the approved commit boundary.
	task.MergeCommit = nil
	task.IntegrationFailure = nil

	a.appendApproval(state, task)

	// Evaluate quorum: determine if more approvals are needed.
	effectiveQuorum, qErr := a.pipe.resolver.EffectiveQuorum(task.RolePair, pipeline.ReviewImpact(a.effectiveImpact))
	if qErr != nil {
		return false, fmt.Errorf("failed to resolve quorum: %w", qErr)
	}
	if task.ApprovalCount() < effectiveQuorum {
		// Quorum not met — need partially_approved state to continue
		if a.pipe.partiallyApproved == "" {
			return false, fmt.Errorf("quorum %d requires partially-approved state but none declared for %q", effectiveQuorum, task.RolePair)
		}
		if err := a.transition(task, a.pipe.partiallyApproved); err != nil {
			return false, err
		}
	} else if projectedIntegrationEvidence, err = a.completeApproval(state, task); err != nil {
		return false, err
	}

	// Derived field for backward compatibility
	agentID := a.agentID
	task.ApprovedBy = &agentID
	task.RejectionReason = nil
	return projectedIntegrationEvidence, nil
}

// appendApproval builds the approval from the agent registry and appends it,
// with the verdict's history entry and optional impact.
func (a *verdictAttempt) appendApproval(state *models.State, task *models.Task) {
	agentID := a.agentID
	provider, model := "", ""
	if agent, ok := state.Agents[agentID]; ok {
		provider, model = agent.Provider, agent.Model
	}
	task.Approvals = append(task.Approvals, models.Approval{
		Agent:     agentID,
		Provider:  provider,
		Model:     model,
		Timestamp: a.now,
	})

	historyEntry := models.TaskHistoryEntry{
		Time:   a.now,
		Event:  models.TaskEventApproved,
		Agent:  &agentID,
		Commit: task.ReviewCommit,
	}
	if a.impact != "" {
		historyEntry.Extra = map[string]any{verdictImpactExtraKey: a.impact}
	}
	task.History = append(task.History, historyEntry)
}

// completeApproval routes a task whose quorum is met to clean or approved and
// appends the integration evidence of an integration analysis.
func (a *verdictAttempt) completeApproval(state *models.State, task *models.Task) (projectedIntegrationEvidence bool, err error) {
	targetStatus := a.pipe.approved
	if a.pipe.clean != "" && len(task.Output) == 0 {
		targetStatus = a.pipe.clean
	}
	if err := a.transition(task, targetStatus); err != nil {
		return false, err
	}
	if task.IntegrationAnalysis == nil {
		return false, nil
	}
	if err := validateIntegrationAnalysisRolePair(task); err != nil {
		return false, err
	}
	if err := appendIntegrationVerdictEvidence(state, task, a.cleanSource); err != nil {
		return false, err
	}
	return true, nil
}

// validateIntegrationEvidence clears the waiting doer's task reference and
// validates the candidate state the integration evidence produced (K1-E14).
func (a *verdictAttempt) validateIntegrationEvidence(state *models.State, task *models.Task, previousLifecycleState *models.State) error {
	if task.AssignedTo != nil {
		assignedAgentID := *task.AssignedTo
		assignedAgent, ok := state.Agents[assignedAgentID]
		if ok && assignedAgent.Status == models.AgentStatusWaiting && assignedAgent.CurrentTask != nil && *assignedAgent.CurrentTask == task.ID {
			// The await command keeps the agent WAITING until process exit, but
			// a completed task is no longer a valid current-task reference.
			assignedAgent.CurrentTask = nil
			state.Agents[assignedAgentID] = assignedAgent
		}
	}
	if testSubmitVerdictHooks != nil && testSubmitVerdictHooks.beforeValidation != nil {
		testSubmitVerdictHooks.beforeValidation(state)
	}
	return validateIntegrationLifecycleCandidate(a.projectRoot, a.bb.ReadSnapshot, previousLifecycleState, state)
}

// reject moves the task to its rejected status, records the rejection and
// applies the review-cycle limits and the churn gate.
func (a *verdictAttempt) reject(state *models.State, task *models.Task) error {
	if err := a.transition(task, a.pipe.rejected); err != nil {
		return err
	}

	// Rejection at any stage clears all approvals (spec: both reviewers re-review)
	reason, agentID := a.reason, a.agentID
	task.RejectionReason = &reason
	task.ReviewCyclesCurrent++
	task.ReviewCyclesTotal++

	gatingCommit := ""
	if task.ReviewCommit != nil {
		gatingCommit = *task.ReviewCommit
	}
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:   a.now,
		Event:  models.TaskEventRejected,
		Agent:  &agentID,
		Reason: &reason,
		Commit: task.ReviewCommit,
	})
	clearAttemptState(task, attemptStateReviewRejection)

	// Refresh lease — coder needs time to address rejection.
	// If escalation triggers below, lease is cleared along with assignment.
	renewLease(state, task)

	return a.escalateRejection(state, task, gatingCommit)
}

// escalateRejection applies the review-cycle and iteration limits, then the
// churn gate. The existing limits keep precedence; the gate fires only on a
// verdict they let through.
func (a *verdictAttempt) escalateRejection(state *models.State, task *models.Task, gatingCommit string) error {
	reviewLimit := effectiveReviewCycleLimit(state.Config)
	iterationLimit := effectiveCoderIterationLimit(task, state.Config)

	escalation, shouldEscalate := classifyLimitEscalation(
		task.ReviewCyclesCurrent,
		reviewLimit,
		task.Iteration,
		iterationLimit,
		task.EffectiveAttempt(),
	)
	threshold := models.EffectiveHighChurnRejectionThreshold(state.Config)
	transitionTask := func(to models.TaskStatus) error { return a.transition(task, to) }
	switch {
	case shouldEscalate && escalation.action == LimitActionBlocked:
		if err := blockRejectedTask(state, task, transitionTask, a.agentID, a.now, escalation.reason, escalation.questions, nil); err != nil {
			return err
		}
		a.effects.escalatedToBlocked = true
		a.effects.blockedReason = escalation.reason
	case shouldEscalate && escalation.action == LimitActionNewAttempt:
		// Capture for post-Modify call — cannot nest bb.Modify
		a.effects.newAttemptNeeded = true
		a.effects.newAttemptReason = escalation.reason
	case task.RejectionRCAGateDue(threshold):
		// Concurrent resubmission is settled by this transaction: a second
		// verdict re-reads the task here and observes the gate already open.
		return a.openRejectionRCAGate(state, task, transitionTask, threshold, gatingCommit)
	}
	return nil
}

// openRejectionRCAGate blocks the task behind the rejection-RCA gate.
func (a *verdictAttempt) openRejectionRCAGate(state *models.State, task *models.Task, transitionTask func(models.TaskStatus) error, threshold int, gatingCommit string) error {
	firstRejectionAt := firstDurableRejectionAt(task)
	seedRejectionRCAGate(task, threshold, a.now, gatingCommit)
	record := task.RejectionRCA
	blockedReason := rejectionRCAGateReason(record.RejectionCount, threshold)
	if err := blockRejectedTask(state, task, transitionTask, a.agentID, a.now, blockedReason, rejectionRCAGateQuestions(), map[string]any{
		rejectionRCABlockedClassKey:     models.BlockedReasonRejectionRCARequired,
		rejectionRCAThresholdKey:        record.Threshold,
		rejectionRCARejectionCountKey:   record.RejectionCount,
		rejectionRCAGatedAtKey:          record.GatedAt.Format(time.RFC3339),
		rejectionRCAFirstRejectionAtKey: firstRejectionAt.Format(time.RFC3339),
	}); err != nil {
		return err
	}
	a.effects.escalatedToBlocked = true
	a.effects.blockedReason = blockedReason
	a.effects.rejectionRCAGated = true
	return nil
}

// blockRejectedTask escalates a just-rejected task to BLOCKED: it records the
// reason and questions, clears the doer's lease and assignment, releases the
// doer if it still holds the task, and appends the blocked event with the
// caller's detail keys. Both the limit path and the churn gate use it so the
// two escalations cannot drift in what they clear.
func blockRejectedTask(state *models.State, task *models.Task, transition func(models.TaskStatus) error, agentID string, now time.Time, reason string, questions []string, extra map[string]any) error {
	if err := transition(models.TaskStatusBlocked); err != nil {
		return err
	}
	task.BlockedReason = &reason
	task.BlockedQuestions = questions
	task.LeaseExpires = nil

	if task.AssignedTo != nil {
		assignedDoer := *task.AssignedTo
		if assignedDoer != agentID {
			if a, ok := state.Agents[assignedDoer]; ok {
				if a.CurrentTask != nil && *a.CurrentTask == task.ID {
					state.ReleaseAgent(assignedDoer)
				}
			}
		}
	}
	task.AssignedTo = nil

	task.History = append(task.History, models.TaskHistoryEntry{
		Time:   now,
		Event:  models.TaskEventBlocked,
		Agent:  &agentID,
		Reason: &reason,
		Extra:  extra,
	})
	return nil
}

// seedRejectionRCAGate opens a new RCA cycle on the task. Any earlier cycle's
// caller fields and disposition are dropped: the live record always describes
// the current cycle, and the previous one survives in history.
func seedRejectionRCAGate(task *models.Task, threshold int, now time.Time, gatingCommit string) {
	task.RejectionRCA = &models.RejectionRCARecord{
		SchemaVersion:  models.RejectionRCASchemaVersion,
		Threshold:      threshold,
		RejectionCount: task.DurableRejectionCount(),
		GatedAt:        now,
		GatingCommit:   gatingCommit,
	}
}

// firstDurableRejectionAt is the time of the earliest rejection event
// DurableRejectionCount would count; the gating rejection is already in
// history, so there is always one.
func firstDurableRejectionAt(task *models.Task) time.Time {
	var first time.Time
	for _, entry := range task.History {
		if entry.Time.IsZero() || (entry.Event != models.TaskEventRejected && entry.Event != models.TaskEventReviewVerdictRejected) {
			continue
		}
		if first.IsZero() || entry.Time.Before(first) {
			first = entry.Time
		}
	}
	return first
}

func rejectionRCAGateReason(rejectionCount, threshold int) string {
	return fmt.Sprintf("%s: %d durable rejections reached the high-churn threshold %d; a classified RCA and a recovery disposition are required before this task is restored",
		models.BlockedReasonRejectionRCARequired, rejectionCount, threshold)
}

func rejectionRCAGateQuestions() []string {
	return []string{
		"Which cause dominated the rejections: product_defect, capability_failure, lifecycle_retry or unknown?",
		"Which recovery path should resume the task: implementation_correction, capability_reroute, lifecycle_repair, rescope or human_override?",
	}
}

// rejectionRCAGateObservedError is the verdict outcome for a caller that
// reaches the locked callback after a concurrent rejection already gated the
// task: the task moved past this operation into a gated BLOCKED, so the
// caller stops rather than requeries.
func rejectionRCAGateObservedError(task *models.Task) error {
	if !task.RejectionRCAGateOpen() || task.Status != models.TaskStatusBlocked {
		return nil
	}
	return WrapLifecycleError(payloadschema.SubmitVerdictOperation, task, &PreconditionError{Reason: fmt.Sprintf(
		"task %s is already gated (%s) by a concurrent rejection; no further verdict applies until the RCA is recorded and resumed",
		task.ID, models.BlockedReasonRejectionRCARequired)}, models.LifecycleAlreadyTransitioned, LifecycleSafeActionStop, LifecycleEffectsNone)
}

func isReviewingStatus(status, expectedReviewingStatus, expectedReviewing2Status models.TaskStatus) bool {
	return status == expectedReviewingStatus ||
		(expectedReviewing2Status != "" && status == expectedReviewing2Status)
}
