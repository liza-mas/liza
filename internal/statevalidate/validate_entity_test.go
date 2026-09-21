package statevalidate

import (
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
)

// completeReviewerClaimCircuitOpenDetails returns the detail set a
// reviewer_claim_circuit_open anomaly carries when the writer recorded it.
func completeReviewerClaimCircuitOpenDetails() map[string]any {
	return map[string]any{
		"role":             "code-reviewer",
		"failure_class":    "review_boundary_repair",
		"boundary_version": "3f1c0d",
		"attempts":         3,
		"first_failure":    "2026-09-18T10:00:00Z",
		"last_failure":     "2026-09-18T10:00:12Z",
		"cooldown_until":   "2026-09-18T10:05:12Z",
		"recovery":         "run update-review-commit for the task",
		"error":            "review boundary needs repair",
	}
}

func reviewerClaimCircuitOpenState(details map[string]any) *models.State {
	return &models.State{
		Anomalies: []models.Anomaly{{
			Task:     "task-1",
			Reporter: "code-reviewer-1",
			Type:     models.AnomalyTypeReviewerClaimCircuitOpen,
			Details:  details,
		}},
	}
}

func TestValidateAnomalyReviewerClaimCircuitOpen(t *testing.T) {
	t.Parallel()

	t.Run("complete anomaly is accepted", func(t *testing.T) {
		t.Parallel()

		state := reviewerClaimCircuitOpenState(completeReviewerClaimCircuitOpenDetails())
		if err := validateAnomalies(state, "", true); err != nil {
			t.Fatalf("validateAnomalies() = %v, want nil for a complete anomaly", err)
		}
	})

	required := []string{"role", "failure_class", "attempts", "first_failure", "last_failure", "recovery"}
	for _, field := range required {
		t.Run("missing "+field+" is rejected", func(t *testing.T) {
			t.Parallel()

			details := completeReviewerClaimCircuitOpenDetails()
			delete(details, field)
			err := validateAnomalies(reviewerClaimCircuitOpenState(details), "", true)
			if err == nil {
				t.Fatalf("validateAnomalies() = nil, want rejection for missing %q", field)
			}
			if !strings.Contains(err.Error(), models.AnomalyTypeReviewerClaimCircuitOpen) {
				t.Errorf("error = %q, want it to name the anomaly type", err)
			}
			if !strings.Contains(err.Error(), field) {
				t.Errorf("error = %q, want it to name the missing detail %q", err, field)
			}
		})
	}
}

// completeObligationContentDriftedDetails returns the detail set an
// obligation_content_drifted anomaly carries when the merge recorded it.
func completeObligationContentDriftedDetails() map[string]any {
	return map[string]any{
		"path":             "specs/protocols/lifecycle-results.md",
		"heading":          "Coverage and observation counters",
		"change":           "repinned",
		"reviewed_section": "3f1c0d5e6a7b8c9d0e1f2a3b4c5d6e7f80912a3b",
		"current_section":  "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678",
		"carriers":         []string{"specs/plans/cp-0.md", "specs/plans/cp-1.md"},
		"obligations":      []string{"AC-157-8-counter", "LR-counters"},
		"reference_ids":    []string{"lr-counters"},
	}
}

func obligationContentDriftedState(details map[string]any) *models.State {
	return &models.State{
		Anomalies: []models.Anomaly{{
			Reporter: "coder-1",
			Type:     models.AnomalyTypeObligationContentDrifted,
			Details:  details,
		}},
	}
}

func TestValidateAnomalyObligationContentDrifted(t *testing.T) {
	t.Parallel()

	// The record carries no task: the drift belongs to a section several
	// plans rest on, not to whichever merge happened to reveal it.
	t.Run("complete anomaly without a task is accepted", func(t *testing.T) {
		t.Parallel()

		state := obligationContentDriftedState(completeObligationContentDriftedDetails())
		if err := validateAnomalies(state, "", true); err != nil {
			t.Fatalf("validateAnomalies() = %v, want nil for a complete anomaly", err)
		}
	})

	required := []string{"path", "heading", "change", "reviewed_section", "current_section", "carriers", "obligations"}
	for _, field := range required {
		t.Run("missing "+field+" is rejected", func(t *testing.T) {
			t.Parallel()

			details := completeObligationContentDriftedDetails()
			delete(details, field)
			err := validateAnomalies(obligationContentDriftedState(details), "", true)
			if err == nil {
				t.Fatalf("validateAnomalies() = nil, want rejection for missing %q", field)
			}
			if !strings.Contains(err.Error(), models.AnomalyTypeObligationContentDrifted) {
				t.Errorf("error = %q, want it to name the anomaly type", err)
			}
			if !strings.Contains(err.Error(), field) {
				t.Errorf("error = %q, want it to name the missing detail %q", err, field)
			}
		})
	}
}

// A dropped section has no current content to read. The field is still
// required, so the record always states both sides of the comparison — one of
// them being empty is the claim, not an omission.
func TestValidateAnomalyObligationContentDroppedKeepsEmptyCurrentSection(t *testing.T) {
	t.Parallel()

	details := completeObligationContentDriftedDetails()
	details["change"] = "dropped"
	details["current_section"] = ""
	if err := validateAnomalies(obligationContentDriftedState(details), "", true); err != nil {
		t.Fatalf("validateAnomalies() = %v, want nil for a dropped section", err)
	}

	delete(details, "current_section")
	if err := validateAnomalies(obligationContentDriftedState(details), "", true); err == nil {
		t.Fatal("validateAnomalies() = nil, want rejection when current_section is absent rather than empty")
	}
}
