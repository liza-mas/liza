package ops

import (
	"crypto/sha256"
	stderrors "errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	lizaerrors "github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// Review-claim failure classes. A supervisor keys repeated claim failures on
// these values, never on error text, so the vocabulary is fixed here and
// assigned where the failure is produced.
const (
	// Candidate classes, decided at the removal site in claimReviewerTask.
	ReviewClaimClassAcceptanceEvidence   = "acceptance_evidence"
	ReviewClaimClassReviewBoundaryRepair = "review_boundary_repair"
	ReviewClaimClassWorktreeContext      = "worktree_context"
	ReviewClaimClassGitOperation         = "git_operation"
	ReviewClaimClassIntegrationFailed    = "integration_failed"

	// Envelope classes. ReviewClaimClassCandidateFailures marks a failure whose
	// disposition is decided per candidate; the rest classify a failure that
	// named no candidate at all.
	ReviewClaimClassCandidateFailures = "candidate_failures"
	ReviewClaimClassAuthority         = "authority"
	ReviewClaimClassDegraded          = "degraded"
	ReviewClaimClassNoWork            = "no_work"
	ReviewClaimClassUnclassified      = "unclassified"
)

// Operational codes review-boundary validation reports. The class vocabulary
// above deliberately mirrors them; these constants keep the mapping explicit.
const (
	reviewClaimCodeGitOperation    = "git_operation"
	reviewClaimCodeWorktreeContext = "worktree_context"
)

// ReviewClaimCandidateFailure names one candidate a reviewer claim removed: the
// class of the branch that removed it, whether that branch is worth retrying
// against unchanged state, and the boundary version it was observed against.
type ReviewClaimCandidateFailure struct {
	TaskID          string
	Class           string
	Transient       bool
	BoundaryVersion string
	Recovery        string
}

// ReviewClaimFailure wraps the error ClaimReviewerTask already returns so a
// caller can tell a deterministic, repair-required failure from transient
// contention, and can name the candidates that failed. Unwrap preserves every
// existing classification of the wrapped error, so lifecycle outcomes and
// recovery hints are unchanged.
type ReviewClaimFailure struct {
	Role       string
	Class      string
	Transient  bool
	Candidates []ReviewClaimCandidateFailure

	err error
}

func (e *ReviewClaimFailure) Error() string {
	if e.err != nil {
		return e.err.Error()
	}
	class := e.Class
	if class == "" {
		class = ReviewClaimClassUnclassified
	}
	return fmt.Sprintf("review claim failed: %s", class)
}

func (e *ReviewClaimFailure) Unwrap() error { return e.err }

// ClassifyReviewClaimError normalizes a reviewer-claim error into a typed
// failure. An error that already carries a *ReviewClaimFailure is returned
// unchanged: its candidates were classified where they failed, and the error it
// wraps no longer describes them. Only the role is filled, and only when empty.
func ClassifyReviewClaimError(role string, err error) *ReviewClaimFailure {
	if err == nil {
		return nil
	}
	var existing *ReviewClaimFailure
	if stderrors.As(err, &existing) {
		if existing.Role == "" {
			existing.Role = role
		}
		return existing
	}
	class, transient := classifyCandidateFreeReviewClaimError(err)
	return &ReviewClaimFailure{Role: role, Class: class, Transient: transient, err: err}
}

// classifyCandidateFreeReviewClaimError classifies a claim failure that named
// no candidate — registration rejected, no reviewable task, a failed probe.
func classifyCandidateFreeReviewClaimError(err error) (string, bool) {
	if IsAgentAuthorityError(err) {
		return ReviewClaimClassAuthority, false
	}
	if stderrors.Is(err, ErrAgentDegraded) {
		return ReviewClaimClassDegraded, false
	}
	if operationalErrorCode(err) == reviewClaimCodeGitOperation {
		return ReviewClaimClassGitOperation, true
	}
	var precondition *PreconditionError
	if stderrors.As(err, &precondition) {
		return ReviewClaimClassNoWork, false
	}
	return ReviewClaimClassUnclassified, true
}

// classifyReviewBoundaryRemoval classifies the removal branch that transitions a
// candidate to INTEGRATION_FAILED: a worktree the validator could not read, a
// failed git probe, or a boundary the validator rejected outright.
func classifyReviewBoundaryRemoval(err error) (string, bool) {
	var worktreeErr *lizaerrors.WorktreeContextError
	if stderrors.As(err, &worktreeErr) {
		return ReviewClaimClassWorktreeContext, false
	}
	switch operationalErrorCode(err) {
	case reviewClaimCodeWorktreeContext:
		return ReviewClaimClassWorktreeContext, false
	case reviewClaimCodeGitOperation:
		return ReviewClaimClassGitOperation, true
	}
	return ReviewClaimClassIntegrationFailed, false
}

func operationalErrorCode(err error) string {
	var operational *OperationalError
	if stderrors.As(err, &operational) {
		return operational.Code
	}
	return ""
}

// reviewClaimRecoveryHint returns the failure's own recovery hint where its type
// carries one. Hints are already brand-rendered by the code that produced them.
func reviewClaimRecoveryHint(err error) string {
	var repairNeeded *ReviewBoundaryRepairNeededError
	if stderrors.As(err, &repairNeeded) {
		return repairNeeded.RecoveryHint
	}
	var operational *OperationalError
	if stderrors.As(err, &operational) {
		if hint, ok := operational.Details["recovery_hint"].(string); ok {
			return hint
		}
	}
	return ""
}

// newReviewClaimCandidateFailure records one candidate at its removal site. The
// class is the branch that removed it, not a re-inspection of the error, and the
// boundary version is read before any transition the branch then applies.
func newReviewClaimCandidateFailure(task *models.Task, class string, transient bool, err error) ReviewClaimCandidateFailure {
	return ReviewClaimCandidateFailure{
		TaskID:          task.ID,
		Class:           class,
		Transient:       transient,
		BoundaryVersion: ReviewClaimBoundaryVersion(task),
		Recovery:        reviewClaimRecoveryHint(err),
	}
}

// appendReviewClaimCandidateFailure records each candidate at most once, so a
// candidate re-observed across Modify retries keeps its first classification.
func appendReviewClaimCandidateFailure(failures []ReviewClaimCandidateFailure, failure ReviewClaimCandidateFailure) []ReviewClaimCandidateFailure {
	for _, existing := range failures {
		if existing.TaskID == failure.TaskID {
			return failures
		}
	}
	return append(failures, failure)
}

// newReviewClaimFailure wraps — never replaces — the error the claim path
// already returns. With candidates present the envelope class is fixed and the
// disposition belongs to each candidate; without them the candidate-free table
// assigns the only class the failure has.
func newReviewClaimFailure(role string, candidates []ReviewClaimCandidateFailure, err error) *ReviewClaimFailure {
	if len(candidates) == 0 {
		return ClassifyReviewClaimError(role, err)
	}
	return &ReviewClaimFailure{
		Role:       role,
		Class:      ReviewClaimClassCandidateFailures,
		Candidates: candidates,
		err:        err,
	}
}

// ReviewClaimBoundaryVersion digests the state-visible fields of task whose
// change makes a previously failed claim worth attempting again: the documented
// repair writes review_commit and base_commit, and any lifecycle event moves
// status or history. The out-of-state inputs the boundary validator also reads
// (worktree HEAD, the integration merge base) are deliberately excluded — a
// repair that moves only those leaves state untouched and is covered by a
// time-bounded re-probe instead.
func ReviewClaimBoundaryVersion(task *models.Task) string {
	if task == nil {
		return ""
	}
	fields := []string{
		string(task.Status),
		reviewClaimOptionalField(task.ReviewCommit),
		reviewClaimOptionalField(task.BaseCommit),
		reviewClaimOptionalField(task.Worktree),
		strconv.Itoa(len(task.History)),
	}
	// Length prefixes keep adjacent field values from colliding across the
	// boundary between them.
	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		parts = append(parts, strconv.Itoa(len(field))+":"+field)
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(parts, "|"))))
}

// reviewClaimOptionalField keeps an unset field distinguishable from an empty
// one, so clearing a commit is a boundary change rather than a collision.
func reviewClaimOptionalField(value *string) string {
	if value == nil {
		return "\x00"
	}
	return "\x01" + *value
}

// reviewerClaimCircuitOpenErrorLimit bounds the masked error text one anomaly
// stores, so a quarantine record stays readable in state.yaml.
const reviewerClaimCircuitOpenErrorLimit = 2048

// ReviewerClaimCircuitOpenInput describes one quarantine decision: the failure
// key it was taken on, the counters the breaker accumulated for that key, and
// the failure itself. The registration generation is deliberately absent — it
// belongs to neither the key nor the record.
type ReviewerClaimCircuitOpenInput struct {
	ProjectRoot     string
	AgentID         string
	Authority       *models.AgentAuthority
	Role            string
	TaskID          string
	FailureClass    string
	BoundaryVersion string
	Recovery        string
	Err             error
	Attempts        int
	FirstFailure    time.Time
	LastFailure     time.Time
	CooldownUntil   time.Time
}

// RecordReviewerClaimCircuitOpen writes the durable record of a reviewer-claim
// quarantine. Exactly one anomaly exists per failure key however many
// supervisors or restarts observe the condition: the first observation appends
// it, and every later one advances attempts, last_failure and cooldown_until on
// that same record in place. Scan and write share one generation-fenced
// transaction, so a rejected authority leaves state untouched.
func RecordReviewerClaimCircuitOpen(input ReviewerClaimCircuitOpenInput) (bool, error) {
	if input.Role == "" || input.TaskID == "" || input.FailureClass == "" {
		return false, &PreconditionError{Reason: "reviewer claim circuit-open record requires role, task ID and failure class"}
	}

	lp := paths.New(input.ProjectRoot)
	bb := db.For(lp.StatePath())
	appended := false
	err := modifyLifecycleState(bb, input.Authority, func(state *models.State) error {
		// Reset per attempt: a transaction the blackboard re-runs must report
		// the outcome of the attempt that was persisted, not an earlier one.
		appended = false
		if existing := findReviewerClaimCircuitOpen(state.Anomalies, input); existing != nil {
			existing.Details["attempts"] = input.Attempts
			existing.Details["last_failure"] = reviewerClaimCircuitOpenTime(input.LastFailure)
			existing.Details["cooldown_until"] = reviewerClaimCircuitOpenTime(input.CooldownUntil)
			return nil
		}
		state.Anomalies = append(state.Anomalies, models.Anomaly{
			Timestamp: input.FirstFailure.UTC(),
			Task:      input.TaskID,
			Reporter:  agentIDOrSystem(input.AgentID),
			Type:      models.AnomalyTypeReviewerClaimCircuitOpen,
			Details: map[string]any{
				"role":             input.Role,
				"failure_class":    input.FailureClass,
				"boundary_version": input.BoundaryVersion,
				"attempts":         input.Attempts,
				"first_failure":    reviewerClaimCircuitOpenTime(input.FirstFailure),
				"last_failure":     reviewerClaimCircuitOpenTime(input.LastFailure),
				"cooldown_until":   reviewerClaimCircuitOpenTime(input.CooldownUntil),
				"recovery":         input.Recovery,
				"error":            boundedMaskedErrorString(input.Err, reviewerClaimCircuitOpenErrorLimit),
			},
		})
		appended = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return appended, nil
}

// findReviewerClaimCircuitOpen returns the anomaly already recorded for this
// failure key, or nil. A changed boundary version is a different key, so a
// repair that moved the candidate's state earns its own record.
func findReviewerClaimCircuitOpen(anomalies []models.Anomaly, input ReviewerClaimCircuitOpenInput) *models.Anomaly {
	for i := range anomalies {
		anomaly := &anomalies[i]
		if anomaly.Type != models.AnomalyTypeReviewerClaimCircuitOpen || anomaly.Task != input.TaskID {
			continue
		}
		if anomaly.Details["role"] == input.Role &&
			anomaly.Details["failure_class"] == input.FailureClass &&
			anomaly.Details["boundary_version"] == input.BoundaryVersion {
			return anomaly
		}
	}
	return nil
}

func reviewerClaimCircuitOpenTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339)
}
