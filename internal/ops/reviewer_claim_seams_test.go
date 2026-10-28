package ops

import (
	"errors"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
)

func TestReviewerClaimFailureEnvelopeSelectsErrorByPrecedence(t *testing.T) {
	t.Parallel()
	failures := []ReviewClaimCandidateFailure{{TaskID: "task-a", Class: ReviewClaimClassReviewBoundaryRepair}}
	evidence := &AcceptanceEvidenceError{TaskID: "task-a", Field: "acceptance", Reason: "receipt stale"}

	t.Run("acceptance evidence wins over a repair-needed task", func(t *testing.T) {
		t.Parallel()
		c := &reviewerClaim{role: models.RoleCodeReviewer, candidateFailures: failures, acceptanceErrors: []error{evidence}, repairNeededTaskIDs: []string{"task-a"}}
		err := c.claimFailure()
		var envelope *ReviewClaimFailure
		if !errors.As(err, &envelope) || envelope.Class != ReviewClaimClassCandidateFailures || len(envelope.Candidates) != 1 {
			t.Fatalf("envelope = %#v (%v), want one candidate failure", envelope, err)
		}
		var got *AcceptanceEvidenceError
		if !errors.As(err, &got) || got != evidence {
			t.Fatalf("error %v does not carry the acceptance evidence error", err)
		}
	})

	t.Run("repair-needed tasks are named once with the repair command", func(t *testing.T) {
		t.Parallel()
		c := &reviewerClaim{role: models.RoleCodeReviewer, candidateFailures: failures}
		c.noteRepairNeeded(&models.Task{ID: "task-a"})
		c.noteRepairNeeded(&models.Task{ID: "task-b"})
		c.noteRepairNeeded(&models.Task{ID: "task-a"})
		err := c.claimFailure()
		var precondition *PreconditionError
		if !errors.As(err, &precondition) {
			t.Fatalf("error = %v, want a precondition error", err)
		}
		if !strings.Contains(precondition.Reason, "task(s): task-a, task-b — run ") || !strings.Contains(precondition.Reason, updateReviewCommitOperation) {
			t.Fatalf("reason = %q, want both tasks once and the %s command", precondition.Reason, updateReviewCommitOperation)
		}
	})

	t.Run("any other removal is a review boundary mismatch", func(t *testing.T) {
		t.Parallel()
		c := &reviewerClaim{role: models.RoleCodeReviewer, candidateFailures: failures}
		var failed *IntegrationFailedError
		if err := c.claimFailure(); !errors.As(err, &failed) || failed.Reason != IntegrationReasonReviewBoundaryMismatch {
			t.Fatalf("error = %v, want an integration failure for %q", err, IntegrationReasonReviewBoundaryMismatch)
		}
	})
}

func TestReviewCommitUpdateRequireRepairableStatus(t *testing.T) {
	t.Parallel()
	u := &reviewCommitUpdate{taskID: "task-1", submitted: "SUBMITTED", reviewing: "REVIEWING", reviewing2: "REVIEWING_2"}
	for _, status := range []models.TaskStatus{"SUBMITTED", "REVIEWING", "REVIEWING_2"} {
		if err := u.requireRepairableStatus(&models.Task{Status: status}); err != nil {
			t.Errorf("status %s refused: %v", status, err)
		}
	}
	err := u.requireRepairableStatus(&models.Task{Status: "EXECUTING"})
	var precondition *PreconditionError
	if !errors.As(err, &precondition) || precondition.Reason != "task task-1 must be in submitted or reviewing state (current: EXECUTING)" {
		t.Fatalf("error = %v, want the submitted-or-reviewing refusal naming EXECUTING", err)
	}

	// A role pair without a second review leaves reviewing2 empty; an empty
	// status must not match it.
	u.reviewing2 = ""
	if err := u.requireRepairableStatus(&models.Task{Status: ""}); err == nil {
		t.Error("a task with an empty status was accepted against an unconfigured reviewing-2 status")
	}
}
