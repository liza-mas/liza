package statevalidate

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

func quarantinedVerdictState() *models.State {
	return &models.State{
		Tasks: []models.Task{{ID: "task-1"}},
		QuarantinedVerdicts: []models.QuarantinedVerdict{{
			ID: "finding-1", TaskID: "task-1", ReviewerID: "reviewer-1",
			ReviewCommit: strings.Repeat("a", 40), Verdict: "REJECTED", Reason: "Provider contract is incomplete",
			Timestamp:              time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC),
			GenerationFingerprints: []string{strings.Repeat("b", 64)}, Matched: true,
		}},
	}
}

func TestValidateQuarantinedVerdictsAcceptsValidEvidence(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*models.State)
	}{
		{"legacy state", func(s *models.State) { s.QuarantinedVerdicts = nil }},
		{"matched rejection", func(s *models.State) {}},
		{"unmatched evidence", func(s *models.State) { s.QuarantinedVerdicts[0].Matched = false }},
		{"sha256 review", func(s *models.State) { s.QuarantinedVerdicts[0].ReviewCommit = strings.Repeat("c", 64) }},
		{"empty approval reason", func(s *models.State) {
			s.QuarantinedVerdicts[0].Verdict = "APPROVED"
			s.QuarantinedVerdicts[0].Reason = ""
		}},
		{"reason byte limit", func(s *models.State) { s.QuarantinedVerdicts[0].Reason = strings.Repeat("é", 2048) }},
		{"two distinct generations", func(s *models.State) {
			s.QuarantinedVerdicts[0].GenerationFingerprints = append(s.QuarantinedVerdicts[0].GenerationFingerprints, strings.Repeat("c", 64))
		}},
		{"ordered audit", func(s *models.State) {
			f := &s.QuarantinedVerdicts[0]
			for i, disposition := range []string{"escalated", "accepted", "refuted", "superseded"} {
				f.Reconciliations = append(f.Reconciliations, models.VerdictReconciliation{
					Actor: "orchestrator-1", Timestamp: f.Timestamp.Add(time.Duration(i) * time.Minute),
					Disposition: disposition, Reason: strings.Repeat("é", 2048),
				})
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := quarantinedVerdictState()
			tc.edit(s)
			if err := ValidateQuarantinedVerdicts(s); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestValidateQuarantinedVerdictsRejectsMalformedEvidence(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*models.QuarantinedVerdict)
		want string
	}{
		{"empty id", func(f *models.QuarantinedVerdict) { f.ID = "" }, "unique nonempty id"},
		{"missing task", func(f *models.QuarantinedVerdict) { f.TaskID = "unknown" }, "existing task"},
		{"missing reviewer", func(f *models.QuarantinedVerdict) { f.ReviewerID = "" }, "reviewer"},
		{"missing timestamp", func(f *models.QuarantinedVerdict) { f.Timestamp = time.Time{} }, "timestamp"},
		{"mutable ref", func(f *models.QuarantinedVerdict) { f.ReviewCommit = "main" }, "full review commit"},
		{"abbreviated commit", func(f *models.QuarantinedVerdict) { f.ReviewCommit = "147b8781" }, "full review commit"},
		{"unknown verdict", func(f *models.QuarantinedVerdict) { f.Verdict = "COMMENT" }, "invalid verdict"},
		{"empty rejection", func(f *models.QuarantinedVerdict) { f.Reason = "" }, "reason"},
		{"whitespace rejection", func(f *models.QuarantinedVerdict) { f.Reason = " \n\t" }, "reason"},
		{"invalid utf8", func(f *models.QuarantinedVerdict) { f.Reason = string([]byte{0xff}) }, "UTF-8"},
		{"oversize bytes", func(f *models.QuarantinedVerdict) { f.Reason = strings.Repeat("é", 2048) + "x" }, "bounded"},
		{"no fingerprint", func(f *models.QuarantinedVerdict) { f.GenerationFingerprints = nil }, "generation fingerprints"},
		{"short fingerprint", func(f *models.QuarantinedVerdict) { f.GenerationFingerprints = []string{strings.Repeat("a", 40)} }, "SHA-256"},
		{"nonhex fingerprint", func(f *models.QuarantinedVerdict) { f.GenerationFingerprints = []string{strings.Repeat("g", 64)} }, "SHA-256"},
		{"duplicate fingerprint", func(f *models.QuarantinedVerdict) {
			f.GenerationFingerprints = append(f.GenerationFingerprints, f.GenerationFingerprints[0])
		}, "unique SHA-256"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := quarantinedVerdictState()
			tc.edit(&s.QuarantinedVerdicts[0])
			if err := ValidateQuarantinedVerdicts(s); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
	t.Run("duplicate finding id", func(t *testing.T) {
		s := quarantinedVerdictState()
		s.QuarantinedVerdicts = append(s.QuarantinedVerdicts, s.QuarantinedVerdicts[0])
		if err := ValidateQuarantinedVerdicts(s); err == nil || !strings.Contains(err.Error(), "unique nonempty id") {
			t.Fatalf("duplicate finding error = %v", err)
		}
	})
}

func TestValidateQuarantinedVerdictsRejectsMalformedAudit(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*models.VerdictReconciliation)
	}{
		{"empty actor", func(r *models.VerdictReconciliation) { r.Actor = "" }},
		{"zero timestamp", func(r *models.VerdictReconciliation) { r.Timestamp = time.Time{} }},
		{"before capture", func(r *models.VerdictReconciliation) { r.Timestamp = r.Timestamp.Add(-3 * time.Minute) }},
		{"before previous judgment", func(r *models.VerdictReconciliation) { r.Timestamp = r.Timestamp.Add(-90 * time.Second) }},
		{"unknown disposition", func(r *models.VerdictReconciliation) { r.Disposition = "ignored" }},
		{"empty reason", func(r *models.VerdictReconciliation) { r.Reason = " \n" }},
		{"invalid utf8", func(r *models.VerdictReconciliation) { r.Reason = string([]byte{0xff}) }},
		{"oversize reason", func(r *models.VerdictReconciliation) { r.Reason = strings.Repeat("é", 2048) + "x" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := quarantinedVerdictState()
			f := &s.QuarantinedVerdicts[0]
			f.Reconciliations = []models.VerdictReconciliation{
				{Actor: "orchestrator-1", Timestamp: f.Timestamp.Add(time.Minute), Disposition: "escalated", Reason: "Awaiting evidence"},
				{Actor: "orchestrator-1", Timestamp: f.Timestamp.Add(2 * time.Minute), Disposition: "refuted", Reason: "Evidence disproves finding"},
			}
			tc.edit(&f.Reconciliations[1])
			if err := ValidateQuarantinedVerdicts(s); err == nil || !strings.Contains(err.Error(), "quarantined_verdicts[0].reconciliations[1]") {
				t.Fatalf("error = %v, want second audit entry rejected", err)
			}
		})
	}
}
