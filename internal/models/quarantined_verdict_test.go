package models

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestIsFullReviewCommit(t *testing.T) {
	for _, tc := range []struct {
		name, commit string
		valid        bool
	}{
		{"sha1", strings.Repeat("ab", 20), true},
		{"sha256", strings.Repeat("cd", 32), true},
		{"empty", "", false},
		{"ref", "refs/heads/main", false},
		{"abbreviation", "147b8781", false},
		{"short sha1", strings.Repeat("a", 39), false},
		{"long sha1", strings.Repeat("a", 41), false},
		{"short sha256", strings.Repeat("a", 63), false},
		{"long sha256", strings.Repeat("a", 65), false},
		{"nonhex", strings.Repeat("g", 40), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsFullReviewCommit(tc.commit); got != tc.valid {
				t.Errorf("IsFullReviewCommit(%q) = %v, want %v", tc.commit, got, tc.valid)
			}
		})
	}
}

func TestIsVerdictDisposition(t *testing.T) {
	for _, disposition := range []string{"accepted", "refuted", "superseded", "escalated"} {
		if !IsVerdictDisposition(disposition) {
			t.Errorf("valid disposition %q rejected", disposition)
		}
	}
	for _, disposition := range []string{"", "APPROVED", "rejected", "ignored", "Accepted"} {
		if IsVerdictDisposition(disposition) {
			t.Errorf("unsupported disposition %q accepted", disposition)
		}
	}
}

func TestQuarantinedVerdictsStateYAMLCompatibility(t *testing.T) {
	var legacy State
	if err := yaml.Unmarshal([]byte("tasks:\n  - id: task-1\n"), &legacy); err != nil {
		t.Fatal(err)
	}
	if len(legacy.QuarantinedVerdicts) != 0 {
		t.Fatal("legacy state acquired quarantined evidence")
	}
	data, err := yaml.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "quarantined_verdicts:") {
		t.Fatal("empty optional evidence field was serialized into legacy state")
	}

	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	legacy.QuarantinedVerdicts = []QuarantinedVerdict{{
		ID: "finding-1", TaskID: "task-1", ReviewerID: "reviewer-1",
		ReviewCommit: strings.Repeat("a", 40), Verdict: "REJECTED", Reason: "Missing concurrency evidence",
		Timestamp: now, Matched: true, GenerationFingerprints: []string{strings.Repeat("b", 64)},
		Reconciliations: []VerdictReconciliation{
			{Actor: "orchestrator-1", Timestamp: now.Add(time.Minute), Disposition: "escalated", Reason: "Needs provider decision"},
			{Actor: "orchestrator-1", Timestamp: now.Add(2 * time.Minute), Disposition: "accepted", Reason: "Provider confirmed finding"},
		},
	}}
	data, err = yaml.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	var restored State
	if err := yaml.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored.QuarantinedVerdicts, legacy.QuarantinedVerdicts) {
		t.Fatalf("evidence or ordered reconciliation history changed on round trip: %#v", restored.QuarantinedVerdicts)
	}
}
