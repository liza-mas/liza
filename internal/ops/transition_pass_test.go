package ops

import (
	"slices"
	"testing"

	"github.com/liza-mas/liza/internal/models"
)

// The trigger filter stays a string (no API migration) and its spellings are the
// models trigger constants'; the empty filter selects both triggers.
func TestTransitionPassTriggerFilter(t *testing.T) {
	t.Parallel()
	if string(models.TransitionTriggerAuto) != "auto" || string(models.TransitionTriggerManual) != "manual" {
		t.Fatalf("trigger constants = %q/%q, want auto/manual", models.TransitionTriggerAuto, models.TransitionTriggerManual)
	}

	tmpDir, _ := setupPipelineProceedTest(t)
	resolver, _, err := loadResolverWithRuntimePolicy(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := resolver.ApprovedStatus("code-planning-pair")
	if err != nil {
		t.Fatal(err)
	}
	task := &models.Task{RolePair: "code-planning-pair", Status: models.TaskStatusMerged}

	for _, tc := range []struct {
		filter string
		want   []string
	}{
		{filter: "auto", want: []string{"auto-code-plan-to-coding"}},
		{filter: "manual", want: []string{"code-plan-to-coding"}},
		{filter: "", want: []string{"code-plan-to-coding", "auto-code-plan-to-coding"}},
	} {
		pass := &transitionPass{triggerFilter: tc.filter, resolver: resolver}
		if got := pass.availableTransitions(approved, task); !slices.Equal(got, tc.want) {
			t.Errorf("filter %q available = %v, want %v", tc.filter, got, tc.want)
		}
	}
}

// The same transition found by the available and the incomplete scans is queued
// once, keeping the first occurrence's position.
func TestDedupePendingKeepsFirstOccurrence(t *testing.T) {
	t.Parallel()
	pending := []pendingTx{
		{taskID: "a", name: "t", origIdx: 0},
		{taskID: "b", name: "t", origIdx: 1},
		{taskID: "a", name: "t", origIdx: 2},
		{taskID: "a", name: "u", origIdx: 3},
	}
	got := dedupePending(pending)
	if len(got) != 3 || got[0].origIdx != 0 || got[1].origIdx != 1 || got[2].origIdx != 3 {
		t.Fatalf("dedupePending = %+v, want origIdx 0, 1, 3", got)
	}
}
