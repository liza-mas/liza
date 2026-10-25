package ops

import (
	stderrors "errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/identity"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/payloadschema"
	"github.com/liza-mas/liza/internal/pipeline"
)

// statWorktreePath is a seam for the "stat failed for a reason other than
// absence" branch below. A fixture can produce that on POSIX by putting a
// regular file where the worktree parent belongs (ENOTDIR), but Windows maps
// the same situation to "path not found", which os.IsNotExist reports as true,
// and an unprivileged process cannot deny itself access to a path it owns.
var statWorktreePath = os.Stat

// VerdictResult contains the outcome of a successful verdict submission.
type VerdictResult struct {
	pendingAttemptReason string
	models.LifecycleOutcome
	TaskID              string `json:"task_id"`
	Verdict             string `json:"verdict"` // "APPROVED" or "REJECTED"
	AgentID             string `json:"agent_id"`
	Reason              string `json:"reason"` // non-empty for rejections
	EscalatedToBlocked  bool   `json:"escalated_to_blocked"`
	BlockedReason       string `json:"blocked_reason"`
	RejectionRCAGated   bool   `json:"rejection_rca_gated,omitempty"` // escalation is the churn gate, not a limit
	NewAttemptTriggered bool   `json:"new_attempt_triggered"`
}

// impactOrder defines the ordering for impact levels.
// Higher index = higher impact. Used by ResolveEffectiveImpact.
var impactOrder = map[string]int{
	string(pipeline.ReviewImpactStandard):     0,
	string(pipeline.ReviewImpactSignificant):  1,
	string(pipeline.ReviewImpactArchitecture): 2,
}

// verdictImpactExtraKey is the history Extra key a verdict and a checkpoint
// record their impact under; ResolveEffectiveImpact reads it back.
const verdictImpactExtraKey = "impact"

type submitVerdictTestHooks struct {
	beforeModify     func()
	beforeValidation func(*models.State)
}

var testSubmitVerdictHooks *submitVerdictTestHooks
var verifyCleanIntegrationSourceForVerdict = verifyCleanIntegrationSource
var readTaskStateForSubmitVerdict = readTaskState

// IsValidImpact returns whether v is a recognized impact classification.
// Empty string is valid (means "not specified").
func IsValidImpact(v string) bool {
	if v == "" {
		return true
	}
	_, ok := impactOrder[v]
	return ok
}

// ResolveEffectiveImpact scans checkpoint and verdict history entries since the
// last rejection, returning the maximum impact found.
// Ordering: standard < significant < architecture; default: "standard".
func ResolveEffectiveImpact(history []models.TaskHistoryEntry) string {
	maxImpact := string(pipeline.ReviewImpactStandard)
	maxRank := 0

	// Iterate in reverse; stop at the last rejection boundary.
	for i := len(history) - 1; i >= 0; i-- {
		entry := history[i]

		if entry.Event == models.TaskEventRejected {
			break // rejection resets the cycle
		}

		// Only checkpoint and verdict entries contribute impact.
		if entry.Event != models.TaskEventPreExecutionCheckpoint && entry.Event != models.TaskEventApproved {
			continue
		}

		if v, ok := entry.Extra[verdictImpactExtraKey].(string); ok && v != "" {
			if rank, known := impactOrder[v]; known && rank > maxRank {
				maxRank = rank
				maxImpact = v
			}
		}
	}

	return maxImpact
}

// SubmitVerdict atomically applies a review verdict: APPROVED transitions to
// APPROVED or PARTIALLY_APPROVED status (based on quorum), REJECTED increments
// review cycles and requires a reason. The optional impact parameter records
// the reviewer's impact classification; it cannot downgrade the effective impact.
// No terminal I/O.
func SubmitVerdict(projectRoot, taskID, verdict, reason, agentID, impact string) (*VerdictResult, error) {
	return submitVerdictLifecycle(projectRoot, taskID, verdict, reason, agentID, nil, impact, "", LifecycleRequestOptions{})
}

// SubmitVerdictWithAuthority retains the explicit immutable review boundary
// required for quarantined evidence and approval/merge reconciliation.
func SubmitVerdictWithAuthority(projectRoot, taskID, verdict, reason string, authority models.AgentAuthority, impact, reviewCommit string) (*VerdictResult, error) {
	return SubmitVerdictWithAuthorityAndOptions(projectRoot, taskID, verdict, reason, authority, impact, reviewCommit, LifecycleRequestOptions{})
}

func SubmitVerdictWithAuthorityAndOptions(projectRoot, taskID, verdict, reason string, authority models.AgentAuthority, impact, reviewCommit string, opts LifecycleRequestOptions) (*VerdictResult, error) {
	return submitDurableVerdict(projectRoot, taskID, verdict, reason, authority, impact, reviewCommit, opts)
}

func submitVerdictLifecycle(projectRoot, taskID, verdict, reason, agentID string, authority *models.AgentAuthority, impact, reviewCommit string, opts LifecycleRequestOptions) (result *VerdictResult, retErr error) {
	invocation := NewLifecycleInvocation(projectRoot)
	defer func() { retErr = finishVerdictLifecycle(invocation, result, retErr) }()
	verdict, reviewCommit, err := admitVerdictRequest(taskID, verdict, reason, agentID, authority, impact, reviewCommit, opts)
	if err != nil {
		return nil, err
	}
	retErr = WithProjectLifecycleSharedLock(projectRoot, payloadschema.SubmitVerdictOperation, func() error {
		err := withTaskReviewLock(projectRoot, taskID, payloadschema.SubmitVerdictOperation, func() error {
			var err error
			result, err = submitVerdictUnderReviewLock(projectRoot, taskID, verdict, reason, agentID, authority, impact, reviewCommit, opts)
			return err
		})
		if err != nil || result == nil || result.pendingAttemptReason == "" {
			return err
		}
		// Review lock is released before taking the ownership lock. The verdict
		// completion token prevents this follow-up from ending a newer claim.
		result, err = rolloverAfterRejection(projectRoot, taskID, agentID, verdict, authority, opts, result)
		return err
	})
	return result, retErr
}

// finishVerdictLifecycle classifies a failed verdict call into its lifecycle
// error and records the invocation's result.
func finishVerdictLifecycle(invocation *LifecycleInvocation, result *VerdictResult, retErr error) error {
	outcome, action, effects := models.LifecycleStateChanged, LifecycleSafeActionRequery, LifecycleEffectsNone
	var invalid *PreconditionError
	if stderrors.As(retErr, &invalid) {
		outcome, action = models.LifecycleInvalidInput, LifecycleSafeActionCorrectInput
	}
	var quarantined *QuarantinedVerdictError
	if stderrors.As(retErr, &quarantined) {
		effects = LifecycleEffectsCommitted
	}
	retErr = WrapLifecycleError(payloadschema.SubmitVerdictOperation, nil, retErr, outcome, action, effects)
	var completed models.LifecycleOutcome
	if result != nil {
		completed = result.LifecycleOutcome
	}
	invocation.FinishResult(payloadschema.SubmitVerdictOperation, completed, &retErr, nil)
	return retErr
}

// admitVerdictRequest refuses a malformed request before any lock or state
// read and returns the normalized verdict and review commit.
func admitVerdictRequest(taskID, verdict, reason, agentID string, authority *models.AgentAuthority, impact, reviewCommit string, opts LifecycleRequestOptions) (string, string, error) {
	if err := ValidateLifecycleRequestOptions(opts); err != nil {
		return "", "", &PreconditionError{Reason: err.Error()}
	}
	if err := validateVerdictPayload(taskID, verdict, reason, agentID, impact, reviewCommit, authority); err != nil {
		return "", "", err
	}
	verdict, reviewCommit = strings.ToUpper(verdict), strings.ToLower(reviewCommit)
	if _, err := identity.ExtractRole(agentID); err != nil {
		return "", "", &PreconditionError{Reason: fmt.Sprintf("invalid agent ID: %v", err)}
	}
	return verdict, reviewCommit, nil
}

// submitVerdictUnderReviewLock runs the verdict call for an admitted request.
// A caller whose registration generation is no longer current leaves only
// quarantined evidence.
func submitVerdictUnderReviewLock(projectRoot, taskID, verdict, reason, agentID string, authority *models.AgentAuthority, impact, reviewCommit string, opts LifecycleRequestOptions) (*VerdictResult, error) {
	bb := RequestBlackboard(paths.New(projectRoot).StatePath(), authority, opts)
	safeReason := reason
	if authority != nil {
		state, _, err := readTaskState(bb, taskID)
		if err != nil {
			return nil, err
		}
		if err := RequireAgentAuthority(state, *authority); err != nil {
			if authority.Generation == "" {
				return nil, err
			}
			return nil, quarantineFencedVerdict(bb, taskID, verdict, reason, reviewCommit, *authority)
		}
		safeReason = sanitizeVerdictReason(reason, state, *authority)
	}
	result, err := submitVerdict(projectRoot, taskID, verdict, safeReason, agentID, authority, impact, reviewCommit, reason, false, opts)
	if authority != nil && IsAgentAuthorityError(err) && authority.Generation != "" {
		return result, quarantineFencedVerdict(bb, taskID, verdict, reason, reviewCommit, *authority)
	}
	return result, err
}

// rolloverAfterRejection runs the attempt transition a committed rejection
// asked for and projects the final lifecycle result. A follow-up failure after
// the verdict committed is recorded and reported, never treated as success.
func rolloverAfterRejection(projectRoot, taskID, agentID, verdict string, authority *models.AgentAuthority, opts LifecycleRequestOptions, result *VerdictResult) (*VerdictResult, error) {
	result.NewAttemptTriggered = false
	_, err := transitionToNewAttemptAfterVerdict(projectRoot, taskID, result.pendingAttemptReason, authority, result.CompletedTransitionID, opts)
	state, readErr := RequestBlackboard(paths.New(projectRoot).StatePath(), authority, opts).Read()
	var task *models.Task
	if readErr == nil && authority != nil {
		readErr = RequireAgentAuthority(state, *authority)
	}
	if IsAgentAuthorityError(readErr) {
		err = readErr // Current authority loss always requires stop.
	}
	if readErr == nil {
		task = state.FindTask(taskID)
	}
	if err != nil {
		failure := fmt.Errorf("rejection committed but attempt transition failed: %w", err)
		// The verdict already committed. Record the follow-up failure here,
		// after the review lock is released, without treating it as success.
		lp := paths.New(projectRoot)
		recordSubmitVerdictFailure(RequestBlackboard(lp.StatePath(), authority, opts), lp.LogPath(), taskID, agentID, authority, verdict, failure)
		failureKind, action := models.LifecycleStateChanged, LifecycleSafeActionRequery
		if IsAgentAuthorityError(err) {
			task = nil
			failureKind, action = models.LifecycleStaleCaller, LifecycleSafeActionStop
		}
		outcome := NewLifecycleOutcome(payloadschema.SubmitVerdictOperation, task, failureKind, action, LifecycleEffectsCommitted)
		outcome.CompletedTransitionID, outcome.RequestID = result.CompletedTransitionID, result.RequestID
		return nil, &LifecycleError{Outcome: outcome, Err: failure}
	}
	if task == nil {
		outcome := NewLifecycleOutcome(payloadschema.SubmitVerdictOperation, nil, models.LifecycleStateChanged, LifecycleSafeActionRequery, LifecycleEffectsCommitted)
		outcome.CompletedTransitionID, outcome.RequestID = result.CompletedTransitionID, result.RequestID
		return nil, &LifecycleError{Outcome: outcome, Err: fmt.Errorf("rejection and attempt transition committed but current task state is unavailable")}
	}
	result.NewAttemptTriggered = true
	completed := result.CompletedTransitionID
	result.LifecycleOutcome = NewLifecycleOutcome(payloadschema.SubmitVerdictOperation, task, models.LifecycleCompleted, LifecycleSafeActionContinue, LifecycleEffectsCommitted)
	result.CompletedTransitionID, result.RequestID = completed, opts.RequestID
	return result, nil
}

// verdictRequestPayload is the intent a verdict request's identity hashes. The
// direct call and the durable outbox must build it the same way, or a replayed
// verdict would carry a different payload digest than the one it saved.
func verdictRequestPayload(verdict, reason, impact, reviewCommit string) map[string]string {
	return map[string]string{"verdict": verdict, "reason": reason, "impact": impact, "review_commit": reviewCommit}
}

// validateVerdictPayload rejects a structurally invalid verdict before any
// state is read or locked, through the same schema validate-payload uses, so
// the two boundaries cannot disagree. The canonical object carries the values
// exactly as the caller supplied them: normalizing first would let the
// mutation boundary accept an object preflight rejects.
//
// The operation's own precondition checks still run, and their message stays
// the cause of the lifecycle error, so a caller reading prose sees no change
// while a caller reading result.diagnostics now gets the field-level detail.
func validateVerdictPayload(taskID, verdict, reason, agentID, impact, reviewCommit string, authority *models.AgentAuthority) error {
	version, diagnostics, err := payloadschema.Validate(payloadschema.SubmitVerdictOperation, payloadschema.SubmitVerdictPayload(taskID, verdict, reason, agentID, pipeline.ReviewImpact(impact), reviewCommit))
	if err != nil {
		return err
	}

	preconditionErr := verdictPreconditionError(
		taskID, strings.ToUpper(verdict), reason, agentID, impact, strings.ToLower(reviewCommit), authority)
	if len(diagnostics) == 0 {
		return preconditionErr
	}
	if preconditionErr == nil {
		preconditionErr = &PreconditionError{Reason: fmt.Sprintf(
			"payload rejected by the %s schema, version %d: %s %s",
			payloadschema.SubmitVerdictOperation, version, diagnostics[0].Field, diagnostics[0].Constraint)}
	}
	return NewLifecycleInvalidInputError(payloadschema.SubmitVerdictOperation, nil, diagnostics, preconditionErr)
}

// verdictPreconditionError runs the operation's own checks on the normalized
// values, dispatching on whether the call carries an explicit review boundary.
func verdictPreconditionError(taskID, verdict, reason, agentID, impact, reviewCommit string, authority *models.AgentAuthority) error {
	if authority == nil {
		return validateVerdictInput(taskID, verdict, reason, agentID, impact)
	}
	return validateAuthenticatedVerdict(taskID, verdict, reason, impact, reviewCommit, *authority)
}

func validateVerdictInput(taskID, verdict, reason, agentID, impact string) error {
	if taskID == "" {
		return &PreconditionError{Reason: "task ID is required"}
	}
	if verdict == "" {
		return &PreconditionError{Reason: "verdict is required"}
	}
	if agentID == "" {
		return &PreconditionError{Reason: fmt.Sprintf("%s is required", brand.EnvName(agentIDEnvSuffix))}
	}

	if v := submittedVerdict(verdict); v != submittedVerdictApproved && v != submittedVerdictRejected {
		return &PreconditionError{Reason: "verdict must be APPROVED or REJECTED"}
	}

	if submittedVerdict(verdict) == submittedVerdictRejected && strings.TrimSpace(reason) == "" {
		return &PreconditionError{Reason: "rejection reason is required for REJECTED verdict"}
	}

	if !IsValidImpact(impact) {
		return &PreconditionError{Reason: "invalid impact value (must be standard, significant, or architecture)"}
	}
	if submittedVerdict(verdict) == submittedVerdictRejected {
		return validateVerdictReasonSize(reason)
	}
	return nil
}

// verdictAttempt is one verdict call under the task review lock. It carries the
// request, the resolved pipeline and what the transaction decided between the
// phases: snapshot preconditions, reviewed-commit check, the atomic transition
// (verdict_transition.go) and the failure projection (verdict_failure_anomaly.go).
type verdictAttempt struct {
	projectRoot, taskID, agentID string
	verdict                      submittedVerdict
	reason                       string // sanitized; what a rejection records
	authority                    *models.AgentAuthority
	impact, reviewCommit         string
	requestReason                string // as supplied; the request identity hashes it
	opts                         LifecycleRequestOptions

	bb      *db.Blackboard
	logPath string
	// recordFailure is cleared by every refusal that carries its own outcome or
	// belongs to a concurrent winner, so only unexpected failures leave an anomaly.
	recordFailure bool

	request         LifecycleRequest
	pipe            *verdictPipeline
	effectiveImpact string
	cleanSource     *cleanIntegrationSourceVerification
	now             time.Time
	effects         verdictEffects
}

func submitVerdict(projectRoot, taskID, verdict, reason, agentID string, authority *models.AgentAuthority, impact, reviewCommit, requestReason string, completionLinearized bool, opts LifecycleRequestOptions) (result *VerdictResult, retErr error) {
	lp := paths.New(projectRoot)
	a := &verdictAttempt{
		projectRoot: projectRoot, taskID: taskID, agentID: agentID, verdict: submittedVerdict(verdict), reason: reason,
		authority: authority, impact: impact, reviewCommit: reviewCommit, requestReason: requestReason, opts: opts,
		bb: RequestBlackboard(lp.StatePath(), authority, opts), logPath: lp.LogPath(), recordFailure: true,
	}
	defer func() { a.projectFailure(retErr) }()
	return a.run(completionLinearized)
}

// run executes the phases in their fixed order: snapshot preconditions, pipeline
// and standing, impact, reviewed-commit check, optional completion
// linearization, clean-source verification, then the atomic transition.
func (a *verdictAttempt) run(completionLinearized bool) (*VerdictResult, error) {
	if _, err := identity.ExtractRole(a.agentID); err != nil {
		return nil, fmt.Errorf("invalid agent ID %s: %w", a.agentID, err)
	}
	state, task, replay, err := a.readSnapshot()
	if err != nil || replay != nil {
		return replay, err
	}
	if err := validateVerdictReviewBoundary(state, task, string(a.verdict), a.reviewCommit); err != nil {
		return nil, err
	}
	if err := a.resolvePipeline(task); err != nil {
		return nil, err
	}
	// Fast-fail before git operations; re-checked authoritatively inside Modify.
	if err := a.checkStandingBeforeGit(task); err != nil {
		return nil, err
	}
	if err := a.resolveEffectiveImpact(task); err != nil {
		return nil, err
	}
	if err := a.checkReviewedCommit(task); err != nil {
		return nil, err
	}
	if !completionLinearized && a.isCleanIntegrationApproval(task) {
		return a.runLinearized()
	}
	a.now = time.Now().UTC()
	if err := a.prepareCleanSourceVerification(task); err != nil {
		return nil, err
	}
	if testSubmitVerdictHooks != nil && testSubmitVerdictHooks.beforeModify != nil {
		testSubmitVerdictHooks.beforeModify()
	}
	return a.commit()
}

// readSnapshot reads the task and checks the caller's authority, the request
// identity and any retained receipt. A replayed receipt is returned as the
// result without a transaction.
func (a *verdictAttempt) readSnapshot() (*models.State, *models.Task, *VerdictResult, error) {
	initialState, task, err := readTaskStateForSubmitVerdict(a.bb, a.taskID)
	if err != nil {
		return nil, nil, nil, err
	}
	if a.authority != nil {
		if err := RequireAgentAuthority(initialState, *a.authority); err != nil {
			return nil, nil, nil, err
		}
	}
	request, err := NewLifecycleRequest(payloadschema.SubmitVerdictOperation, task, a.agentID, a.authority, a.opts, verdictRequestPayload(string(a.verdict), a.requestReason, a.impact, a.reviewCommit))
	if err != nil {
		return nil, nil, nil, err
	}
	if a.opts.verdictPayloadDigest != "" {
		request.PayloadDigest = a.opts.verdictPayloadDigest
	}
	receipt, err := CheckLifecycleRequest(task, request, initialState.Agents)
	if err != nil {
		a.recordFailure = false
		return nil, nil, nil, err
	}
	a.request = request
	if receipt != nil {
		return initialState, task, &VerdictResult{LifecycleOutcome: LifecycleReplayOutcome(task, receipt, a.agentID), TaskID: a.taskID, Verdict: receipt.Projection.Verdict, AgentID: a.agentID}, nil
	}
	return initialState, task, nil, nil
}

// checkReviewedCommit requires the review commit to match the worktree HEAD, so
// the verdict covers the commit that was reviewed.
func (a *verdictAttempt) checkReviewedCommit(task *models.Task) error {
	if task.ReviewCommit == nil {
		return &PreconditionError{Reason: fmt.Sprintf("task %s has no review_commit — cannot submit verdict", a.taskID)}
	}
	g := git.New(a.projectRoot)
	wtPath := g.GetWorktreePath(a.taskID)
	if _, statErr := statWorktreePath(wtPath); os.IsNotExist(statErr) {
		// Worktree absent on disk (e.g. tests without real worktrees) — skip check.
		return nil
	} else if statErr != nil {
		return fmt.Errorf("failed to stat worktree %s: %w", wtPath, statErr)
	}
	wtHEAD, headErr := g.GetWorktreeHEAD(a.taskID)
	if headErr != nil {
		return fmt.Errorf("failed to get worktree HEAD: %w", headErr)
	}
	if *task.ReviewCommit != wtHEAD {
		return &PreconditionError{Reason: fmt.Sprintf("review_commit %s does not match worktree HEAD %s — worktree was modified after submission", *task.ReviewCommit, wtHEAD)}
	}
	return nil
}

// runLinearized re-enters the verdict under the completion lock, which orders a
// clean integration verdict against integration movement.
func (a *verdictAttempt) runLinearized() (result *VerdictResult, err error) {
	callbackEntered := false
	linearizationErr := withEffectiveIntegrationCompletionLinearization(a.projectRoot, "clean integration verdict "+a.taskID, func() error {
		callbackEntered = true
		result, err = submitVerdict(a.projectRoot, a.taskID, string(a.verdict), a.reason, a.agentID, a.authority, a.impact, a.reviewCommit, a.requestReason, true, a.opts)
		return err
	})
	if callbackEntered {
		a.recordFailure = false
	}
	return result, linearizationErr
}

// commit runs the atomic verdict transaction and projects its result.
func (a *verdictAttempt) commit() (*VerdictResult, error) {
	err := modifyLifecycleState(a.bb, a.authority, a.apply)
	if isLifecycleReplay(err) && a.effects.replayed {
		return &VerdictResult{LifecycleOutcome: a.effects.outcome, TaskID: a.taskID, Verdict: string(a.verdict), AgentID: a.agentID}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to submit verdict: %w", err)
	}
	return &VerdictResult{
		pendingAttemptReason: a.effects.newAttemptReason,
		LifecycleOutcome:     a.effects.outcome,
		TaskID:               a.taskID,
		Verdict:              string(a.verdict),
		AgentID:              a.agentID,
		Reason:               a.reason,
		EscalatedToBlocked:   a.effects.escalatedToBlocked,
		BlockedReason:        a.effects.blockedReason,
		RejectionRCAGated:    a.effects.rejectionRCAGated,
		NewAttemptTriggered:  !a.effects.escalatedToBlocked && a.effects.newAttemptNeeded,
	}, nil
}
