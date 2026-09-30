package models

import (
	"slices"
	"testing"
	"time"
)

// AnomalyViolations is the rule statevalidate reports and the write boundary
// enforces (ADR-0166): its identities and messages are the ones validate
// has always printed, one per unknown type and one per missing detail.
func TestAnomalyViolations_IdentitiesAndMessages(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	anomalies := []Anomaly{
		{Timestamp: now, Type: "retry_loop", Details: map[string]any{"count": 3, "error_pattern": "x"}},
		{Timestamp: now, Type: "retry_loop", Details: map[string]any{"error_pattern": (*string)(nil)}},
		{Timestamp: now, Type: "not_a_type"},
		{Timestamp: now, Type: "trade_off", Details: map[string]any{"what": []string(nil), "why": map[string]any{}}},
	}

	var got []string
	for _, violation := range AnomalyViolations(anomalies) {
		if violation.ID != violation.Err.Error() {
			t.Errorf("violation ID %q differs from message %q; validate's identities are its messages", violation.ID, violation.Err)
		}
		got = append(got, violation.ID)
	}
	want := []string{
		"retry_loop anomaly at index 1 missing required details (count)",
		"retry_loop anomaly at index 1 missing required details (error_pattern)",
		"unknown anomaly type 'not_a_type' at index 2",
		"trade_off anomaly at index 3 missing required details (debt_created)",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("AnomalyViolations() = %q, want %q", got, want)
	}
}
