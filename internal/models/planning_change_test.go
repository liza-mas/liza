package models

import (
	"reflect"
	"testing"
	"time"
)

func TestPlanningChangesCountCommissionedTasksByUTCCreation(t *testing.T) {
	stamp := time.Date(2026, 10, 10, 0, 30, 0, 0, time.FixedZone("east", 2*60*60))
	originalID := "architecture"
	state := &State{Tasks: []Task{
		{ID: originalID, Type: TaskTypeArchitecture, TransitionsExecuted: map[string]bool{"replanned": true}},
		{ID: "correction", Type: TaskTypePlanning, Created: stamp,
			PlanningChange: NewPlanningChange(PlanningChangeCorrection, "consumer-gap", originalID),
			History:        []TaskHistoryEntry{{Event: TaskEventSubmittedForReview}, {Event: TaskEventSubmittedForReview}}},
		{ID: "second-correction", Created: stamp, PlanningChange: NewPlanningChange(PlanningChangeCorrection, "consumer-gap", originalID)},
		{ID: "replacement", Created: stamp.Add(24 * time.Hour), PlanningChange: NewPlanningChange(PlanningChangeReplan, "incompatible-scope", originalID)},
		{ID: "legacy-correction", Created: stamp, AmendsPlan: originalID},
		{ID: "legacy-replan", Created: stamp, Supersedes: &originalID},
		{ID: "ordinary-arm-task", Type: TaskTypeArchitecture, Created: stamp},
		{ID: "undated", AmendsPlan: originalID},
	}}
	want := PlanningChangeMetrics{Daily: []PlanningChangeDay{
		{Date: "2026-10-09", Kind: PlanningChangeCorrection, Trigger: "consumer-gap", Architecture: true, Count: 2},
		{Date: "2026-10-09", Kind: PlanningChangeCorrection, Trigger: "unknown", Architecture: true, Count: 1},
		{Date: "2026-10-09", Kind: PlanningChangeReplan, Trigger: "unknown", Architecture: true, Count: 1},
		{Date: "2026-10-10", Kind: PlanningChangeReplan, Trigger: "incompatible-scope", Architecture: true, Count: 1},
	}, UnknownAttribution: 3, MissingCreated: 1}
	if got := state.ComputePlanningChanges(); !reflect.DeepEqual(got, want) {
		t.Fatalf("creation metrics = %#v, want %#v", got, want)
	}
	state.Tasks[1].Status = TaskStatusMerged
	state.Tasks[0].PlanAmendment = &PlanAmendment{Applied: []string{"correction"}}
	if got := state.ComputeSprintMetrics().PlanningChanges; !reflect.DeepEqual(got, want) {
		t.Fatalf("review/apply changed creation counts: %#v", got)
	}
}

func TestPlanningChangesUnknownOriginalAndEmptyState(t *testing.T) {
	state := &State{Tasks: []Task{{ID: "change", Created: time.Now(), PlanningChange: NewPlanningChange(PlanningChangeCorrection, "cause", "missing")}}}
	got := state.ComputePlanningChanges()
	if got.UnknownAttribution != 1 || len(got.Daily) != 1 || got.Daily[0].Architecture {
		t.Fatalf("missing original invented attribution: %#v", got)
	}
	if empty := (*State)(nil).ComputePlanningChanges(); len(empty.Daily) != 0 || empty.UnknownAttribution != 0 {
		t.Fatalf("nil state metrics: %#v", empty)
	}
}
