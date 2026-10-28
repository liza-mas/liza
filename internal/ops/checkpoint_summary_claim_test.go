package ops

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

func mergedTask(id string, at time.Time) models.Task {
	return models.Task{ID: id, History: []models.TaskHistoryEntry{{Event: models.TaskEventMerged, Time: at}}}
}

// #171: a summary told to read the whole run outgrew its bound. The scope is
// the work merged since the previous checkpoint, newest first.
func TestBuildCheckpointSummaryScope_MergesSinceThePreviousCheckpoint(t *testing.T) {
	base := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	since := base
	state := &models.State{Tasks: []models.Task{
		mergedTask("before", base.Add(-time.Minute)),
		mergedTask("at-since", base),
		mergedTask("older", base.Add(time.Minute)),
		mergedTask("newer", base.Add(2*time.Minute)),
		mergedTask("after", base.Add(4*time.Minute)),
		{ID: "unmerged"},
	}}
	scope := buildCheckpointSummaryScope(state, &models.PendingCheckpointSummary{At: base.Add(3 * time.Minute), Since: &since})

	if want := []string{"newer", "older"}; !slices.Equal(scope.TaskIDs, want) {
		t.Errorf("TaskIDs = %v, want %v", scope.TaskIDs, want)
	}
	if !scope.Since.Equal(since) || scope.Omitted != 0 {
		t.Errorf("Since = %v, Omitted = %d; want %v, 0", scope.Since, scope.Omitted, since)
	}
}

// Without a previous checkpoint (or on an obligation recorded before Since
// existed) the window opens at the sprint start.
func TestBuildCheckpointSummaryScope_FallsBackToTheSprintStart(t *testing.T) {
	start := time.Date(2026, 10, 7, 8, 0, 0, 0, time.UTC)
	state := &models.State{Tasks: []models.Task{
		mergedTask("previous-sprint", start.Add(-time.Hour)),
		mergedTask("this-sprint", start.Add(time.Hour)),
	}}
	state.Sprint.Timeline.Started = start

	scope := buildCheckpointSummaryScope(state, &models.PendingCheckpointSummary{At: start.Add(2 * time.Hour)})
	if !slices.Equal(scope.TaskIDs, []string{"this-sprint"}) || !scope.Since.Equal(start) {
		t.Errorf("scope = %+v, want only this-sprint since %v", scope, start)
	}
}

// The scope is capped; the remainder is counted so the report can say what
// it does not cover.
func TestBuildCheckpointSummaryScope_CapsTheTaskList(t *testing.T) {
	base := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	state := &models.State{}
	for i := range checkpointSummaryScopeLimit + 3 {
		state.Tasks = append(state.Tasks, mergedTask(fmt.Sprintf("task-%02d", i), base.Add(time.Duration(i+1)*time.Second)))
	}
	scope := buildCheckpointSummaryScope(state, &models.PendingCheckpointSummary{At: base.Add(time.Hour), Since: &base})

	if len(scope.TaskIDs) != checkpointSummaryScopeLimit || scope.Omitted != 3 {
		t.Fatalf("len = %d, Omitted = %d; want %d, 3", len(scope.TaskIDs), scope.Omitted, checkpointSummaryScopeLimit)
	}
	if want := fmt.Sprintf("task-%02d", checkpointSummaryScopeLimit+2); scope.TaskIDs[0] != want {
		t.Errorf("first = %q, want the newest %q", scope.TaskIDs[0], want)
	}
}

func c6PendingSummaryFixture(t *testing.T, pending *models.PendingCheckpointSummary) *c6Fixture {
	t.Helper()
	base := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	return newC6Fixture(t, func(s *models.State) {
		s.Sprint.Timeline.Started = base
		s.Tasks = []models.Task{mergedTask("old", base.Add(time.Minute)), mergedTask("new", base.Add(2*time.Minute)), {ID: "unmerged"}}
		s.PendingCheckpointSummary = pending
	})
}

func TestC6ClaimCheckpointSummaryReturnsTriggerAndScopeAndClearsTheObligation(t *testing.T) {
	base := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	since := base.Add(30 * time.Second)
	f := c6PendingSummaryFixture(t, &models.PendingCheckpointSummary{At: base.Add(time.Hour), Trigger: "SPRINT_COMPLETE", Since: &since})

	claim, err := ClaimCheckpointSummary(f.bb)
	if err != nil || claim == nil {
		t.Fatalf("ClaimCheckpointSummary = (%+v, %v), want a claim", claim, err)
	}
	if claim.Trigger != "SPRINT_COMPLETE" || !slices.Equal(claim.Scope.TaskIDs, []string{"new", "old"}) ||
		!claim.Scope.Since.Equal(since) || claim.Scope.Omitted != 0 {
		t.Fatalf("claim = %+v, want trigger SPRINT_COMPLETE and the newest-first scope since the previous checkpoint", claim)
	}
	if f.read(t).PendingCheckpointSummary != nil {
		t.Fatal("obligation still pending after the claim")
	}

	again, err := ClaimCheckpointSummary(f.bb)
	if err != nil || again != nil {
		t.Fatalf("second ClaimCheckpointSummary = (%+v, %v), want none: one observer claims it", again, err)
	}
}

func TestC6ClaimCheckpointSummaryScopeFallsBackToTheSprintStartAndKeepsAnEmptyTrigger(t *testing.T) {
	base := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	f := c6PendingSummaryFixture(t, &models.PendingCheckpointSummary{At: base.Add(time.Hour)})

	claim, err := ClaimCheckpointSummary(f.bb)
	if err != nil || claim == nil {
		t.Fatalf("ClaimCheckpointSummary = (%+v, %v), want a claim", claim, err)
	}
	if claim.Trigger != "" || !claim.Scope.Since.Equal(base) || !slices.Equal(claim.Scope.TaskIDs, []string{"new", "old"}) {
		t.Fatalf("claim = %+v, want an empty trigger and a window opening at the sprint start", claim)
	}
}

func TestC6ClaimCheckpointSummaryWithNothingPendingChangesNothing(t *testing.T) {
	f := c6PendingSummaryFixture(t, nil)
	before := f.bytes(t)

	claim, err := ClaimCheckpointSummary(f.bb)
	if err != nil || claim != nil {
		t.Fatalf("ClaimCheckpointSummary = (%+v, %v), want none", claim, err)
	}
	f.requireUnchanged(t, before, "claiming with no pending obligation")
}
