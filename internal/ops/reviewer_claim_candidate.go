package ops

import (
	stderrors "errors"
	"fmt"
	"slices"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
)

// Per-candidate checks of the reviewer claim and the candidate-failure envelope.
// A candidate that fails a check is removed and recorded; the claim goes on with
// the others, and the envelope only reports when none was claimed.

// claimCandidate checks one selected candidate inside the claim transaction and
// claims it. removed reports a candidate dropped with its failure recorded, so
// the caller selects again; a nil error without removal ends the transaction.
func (c *reviewerClaim) claimCandidate(state *models.State, task *models.Task, agent models.Agent) (removed bool, err error) {
	if c.selected != nil && (task.ID != c.selected.ID || optionalValue(task.Worktree) != optionalValue(c.selected.Worktree)) {
		return false, validationError(validationCodeCandidateChanged)
	}
	c.invocation.observe(task)
	request, err := c.requestFor(task)
	if err != nil {
		return false, err
	}
	if removed, err := c.deferUnsettledHeal(state, task, request); removed || err != nil {
		return removed, err
	}
	if removed, err := c.checkBoundary(state, task); removed || err != nil { // K1-E2
		return removed, err
	}
	return false, c.claim(state, task, request, agent)
}

// deferUnsettledHeal removes a candidate whose lifecycle check fails because an
// orphaned-base heal is unsettled on it. An unsettled heal reserves only its own
// task: it is named for the supervisor's heal, which settles it, and the claim
// keeps going.
func (c *reviewerClaim) deferUnsettledHeal(state *models.State, task *models.Task, request LifecycleRequest) (removed bool, err error) {
	_, err = c.checkRequest(task, request, state.Agents)
	if err == nil {
		return false, nil
	}
	if c.preparation != nil || !orphanedBaseHealPending(task) {
		return false, err
	}
	c.reviewBoundaryErr = err
	c.noteRepairNeeded(task)
	failure := newReviewClaimCandidateFailure(task, ReviewClaimClassReviewBoundaryRepair, false, err)
	failure.Orphaned = true
	failure.Recovery = orphanedBaseRepairCommand(task, state.Config.IntegrationBranch)
	c.candidateFailures = appendReviewClaimCandidateFailure(c.candidateFailures, failure)
	return true, nil
}

// checkBoundary validates the candidate's review boundary for assignment
// (K1-E2: the validation runs Git inside the claim transaction). An acceptance
// or repair-needed failure only removes the candidate; any other failure also
// routes the task to INTEGRATION_FAILED.
func (c *reviewerClaim) checkBoundary(state *models.State, task *models.Task) (removed bool, err error) {
	err = validateReviewBoundaryForAssignment(c.input.ProjectRoot, state, task)
	if err == nil {
		return false, nil
	}
	c.reviewBoundaryErr = err
	var evidenceErr *AcceptanceEvidenceError
	if stderrors.As(err, &evidenceErr) {
		c.acceptanceErrors = append(c.acceptanceErrors, evidenceErr)
		c.recordRemoval(task, ReviewClaimClassAcceptanceEvidence, false, err)
		return true, nil
	}
	var repairNeeded *ReviewBoundaryRepairNeededError
	if stderrors.As(err, &repairNeeded) {
		c.noteRepairNeeded(task)
		c.recordRemoval(task, ReviewClaimClassReviewBoundaryRepair, false, err)
		return true, nil
	}
	// Classified before the transition below moves the task, so
	// the record names the boundary the failure was observed on.
	class, transient := classifyReviewBoundaryRemoval(err)
	failure := newReviewClaimCandidateFailure(task, class, transient, err)
	if markErr := markReviewBoundaryIntegrationFailed(state, task, c.input.AgentID, c.pb.transitions, err); markErr != nil {
		return false, markErr
	}
	c.candidateFailures = appendReviewClaimCandidateFailure(c.candidateFailures, failure)
	c.invocation.effects = true
	return true, nil
}

func (c *reviewerClaim) recordRemoval(task *models.Task, class ReviewClaimClass, transient bool, err error) {
	c.candidateFailures = appendReviewClaimCandidateFailure(c.candidateFailures,
		newReviewClaimCandidateFailure(task, class, transient, err))
}

func (c *reviewerClaim) noteRepairNeeded(task *models.Task) {
	if !slices.Contains(c.repairNeededTaskIDs, task.ID) {
		c.repairNeededTaskIDs = append(c.repairNeededTaskIDs, task.ID)
	}
}

// claimFailure is the error of a claim that took no candidate because a review
// boundary check removed them. The error stays the one every caller already
// classifies; the envelope only adds which candidates failed, in which class,
// against which boundary version.
func (c *reviewerClaim) claimFailure() error {
	if len(c.acceptanceErrors) > 0 {
		return newReviewClaimFailure(c.role, c.candidateFailures, stderrors.Join(c.acceptanceErrors...))
	}
	if len(c.repairNeededTaskIDs) > 0 {
		return newReviewClaimFailure(c.role, c.candidateFailures, &PreconditionError{
			Reason: fmt.Sprintf(
				"review boundary needs repair for task(s): %s — run %s <task-id>",
				strings.Join(c.repairNeededTaskIDs, ", "),
				brand.Command(updateReviewCommitOperation),
			),
		})
	}
	return newReviewClaimFailure(c.role, c.candidateFailures, &IntegrationFailedError{Reason: IntegrationReasonReviewBoundaryMismatch})
}
