package ops

import (
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

// The digests below were captured from the tree before the K7 vocabulary types
// replaced the string spellings of assessment outcomes, awaited states and
// state-lock-hold refusals; they pin that the hashed bytes are unchanged.

// k7DigestState holds one dependency of every outcome class the assessment
// fingerprint distinguishes, so the digests below cover each outcome spelling.
func k7DigestState() (*models.State, *models.Task) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	task := func(id string, status models.TaskStatus) models.Task {
		return models.Task{ID: id, Status: status, Created: at}
	}
	superseded := task("dep-superseded", models.TaskStatusSuperseded)
	superseded.SupersededBy = []string{"dep-replacement"}
	orphaned := task("dep-orphaned", models.TaskStatusSuperseded)
	target := task("target", models.TaskStatusBlocked)
	target.DependsOn = []string{"dep-merged", "dep-blocked", "dep-pending", "dep-superseded", "dep-orphaned", "dep-unknown", "dep-missing"}
	state := &models.State{
		Tasks: []models.Task{
			target,
			task("dep-merged", models.TaskStatusMerged),
			task("dep-blocked", models.TaskStatusBlocked),
			task("dep-pending", models.TaskStatusImplementing),
			superseded, orphaned,
			task("dep-replacement", models.TaskStatusMerged),
			task("dep-unknown", ""),
		},
		HumanNotes: []models.HumanNote{{For: "target"}, {For: "all"}, {For: "other"}},
	}
	return state, state.FindTask("target")
}

func TestK7AssessmentFingerprintDigestsUnchanged(t *testing.T) {
	t.Parallel()
	state, target := k7DigestState()
	candidate := AssessmentFingerprintCandidate{Reason: "blocked on dependencies", Questions: []string{"which first?"}, Note: "note"}
	for name, tc := range map[string]struct {
		candidate AssessmentFingerprintCandidate
		want      string
	}{
		"outcome classes":   {candidate, "fa4c4bf6bb056524e5129eba1eae124e7700eda10ed7e0dd0245de0223586f8e"},
		"awaited pending":   {AssessmentFingerprintCandidate{Reason: candidate.Reason, Questions: candidate.Questions, Awaited: []string{"dep-pending"}}, "04d4ee9b2f735bb4106638065109a894b71fbc2190388c6b813c9798efd989a1"},
		"awaited failed":    {AssessmentFingerprintCandidate{Reason: candidate.Reason, Questions: candidate.Questions, Awaited: []string{"dep-blocked"}}, "e99de15b88d912dcf6db7cc6242ccd3928365da1c9ec94315528e6d029440df6"},
		"awaited satisfied": {AssessmentFingerprintCandidate{Reason: candidate.Reason, Questions: candidate.Questions, Awaited: []string{"dep-merged"}}, "9bdf8c5e6d8b902d68ded0932fa24ac023758f89b22a42706ba97c6d92138dff"},
	} {
		if got := BuildAssessmentFingerprint(state, target, tc.candidate); got != tc.want {
			t.Errorf("%s: fingerprint = %s, want %s", name, got, tc.want)
		}
	}
}

func TestK7StateLockHoldMaterialDigestsUnchanged(t *testing.T) {
	t.Parallel()
	state, target := k7DigestState()
	for _, tc := range []struct{ refusal, want string }{
		{"system_not_running", "a5a67d213cccf0c7788be4b7cb11f43d7cc55ea56050e0127cd5e365b808e2dc"},
		{"unblock_domain_refused", "47940e71c86d33a20b141a68efb0fc82c196abd30a3c68165812e9b88c0932e4"},
	} {
		if got := stateLockHoldMaterial(state, target, tc.refusal); got != tc.want {
			t.Errorf("%s: material = %s, want %s", tc.refusal, got, tc.want)
		}
	}
}
