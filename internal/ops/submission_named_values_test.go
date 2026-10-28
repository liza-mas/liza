package ops

import (
	"testing"

	"github.com/liza-mas/liza/internal/models"
)

// TestSubmissionNamedValuesKeepTheirValues compares each K7 named value of the
// submission files with the literal it replaced, independently of the constant.
func TestSubmissionNamedValuesKeepTheirValues(t *testing.T) {
	for name, got := range map[string]int{
		"submitRebaseDiagnosticLimit":  submitRebaseDiagnosticLimit,
		"submitHoldDigestBytes":        submitHoldDigestBytes,
		"inflightSubmitNonceBytes":     inflightSubmitNonceBytes,
		"submissionSHA1HexLength":      submissionSHA1HexLength,
		"submissionSHA256HexLength":    submissionSHA256HexLength,
		"submitTimeoutRequestIDLength": submitTimeoutRequestIDLength,
	} {
		want := map[string]int{
			"submitRebaseDiagnosticLimit":  2000,
			"submitHoldDigestBytes":        8,
			"inflightSubmitNonceBytes":     8,
			"submissionSHA1HexLength":      40,
			"submissionSHA256HexLength":    64,
			"submitTimeoutRequestIDLength": 16,
		}[name]
		if got != want {
			t.Errorf("%s = %d, want %d", name, got, want)
		}
	}
	if operationalCodeStateWrite != "state_write" {
		t.Errorf("operationalCodeStateWrite = %q, want state_write", operationalCodeStateWrite)
	}
}

// TestNotAssignedReasonNamesTheCurrentAssignee pins the refusal text shared by
// the snapshot check and the final transaction.
func TestNotAssignedReasonNamesTheCurrentAssignee(t *testing.T) {
	other := "coder-2"
	for _, tc := range []struct {
		name     string
		assigned *string
		want     string
	}{
		{"unassigned", nil, "task task-1 is not assigned to agent coder-1 (currently assigned to: none)"},
		{"another agent", &other, "task task-1 is not assigned to agent coder-1 (currently assigned to: coder-2)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := notAssignedReason("task-1", "coder-1", &models.Task{AssignedTo: tc.assigned}); got != tc.want {
				t.Fatalf("reason = %q, want %q", got, tc.want)
			}
		})
	}
}
