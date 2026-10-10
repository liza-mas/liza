package models

import (
	"encoding/json"
	"testing"
)

func TestBoundedAmendmentRetainsCompleteManifest(t *testing.T) {
	t.Parallel()
	original := []OutputEntry{{Desc: "unit", DoneWhen: "behavior", Scope: "owned", ArchRef: "specs/arch.md#Scope 0", PlanRef: "specs/plan.md#Task 0", Validation: []string{"test"}, ProviderDependencies: []ProviderDependency{{ProviderTask: "provider", Transition: "to-code", Outputs: []int{0}}}}}
	for _, mode := range []PlanAmendmentMode{PlanAmendmentContract, PlanAmendmentPreserveIdentity} {
		if err := ValidateAmendmentOutputForMode(original, original, mode); err != nil {
			t.Fatal(err)
		}
		for name, change := range map[string]func(*[]OutputEntry){
			"scope":       func(out *[]OutputEntry) { (*out)[0].Scope = "other" },
			"validation":  func(out *[]OutputEntry) { (*out)[0].Validation = []string{"other"} },
			"provider":    func(out *[]OutputEntry) { (*out)[0].ProviderDependencies[0].Outputs = []int{1} },
			"ref":         func(out *[]OutputEntry) { (*out)[0].ArchRef = "specs/arch.md#Scope 1" },
			"permissions": func(out *[]OutputEntry) { (*out)[0].DestructiveDB = true },
			"append":      func(out *[]OutputEntry) { *out = append(*out, OutputEntry{Desc: "extra"}) },
		} {
			t.Run(string(mode)+"/"+name, func(t *testing.T) {
				data, _ := json.Marshal(original)
				var changed []OutputEntry
				if err := json.Unmarshal(data, &changed); err != nil {
					t.Fatal(err)
				}
				change(&changed)
				if err := ValidateAmendmentOutputForMode(original, changed, mode); err == nil {
					t.Fatal("changed reviewed allocation accepted")
				}
			})
		}
	}
}

func TestBoundedAmendmentNormalizesOnlyEmptyOptionalCollections(t *testing.T) {
	t.Parallel()
	original := []OutputEntry{{Desc: "unit", Scope: "owned"}}
	correction := []OutputEntry{{Desc: "unit", Scope: "owned", TaskDependsOn: []string{}, ProviderDependencies: []ProviderDependency{}}}
	for _, mode := range []PlanAmendmentMode{PlanAmendmentContract, PlanAmendmentPreserveIdentity} {
		if err := ValidateAmendmentOutputForMode(original, correction, mode); err != nil {
			t.Fatalf("empty optional collections changed allocation: %v", err)
		}
		changed := correction[0]
		changed.TaskDependsOn = []string{"new-prerequisite"}
		if err := ValidateAmendmentOutputForMode(original, []OutputEntry{changed}, mode); err == nil {
			t.Fatal("new prerequisite accepted as normalization")
		}
		changed = correction[0]
		changed.InheritInputs = &InheritInputs{Mode: InheritModeAll}
		if err := ValidateAmendmentOutputForMode(original, []OutputEntry{changed}, mode); err == nil {
			t.Fatal("explicit inheritance declaration accepted as normalization")
		}
	}
}

func TestPendingContractAmendmentFencesConsumersButNotCorrection(t *testing.T) {
	t.Parallel()
	parent := "arch"
	state := &State{Tasks: []Task{
		{ID: "arch", PlanAmendment: &PlanAmendment{Pending: "correction"}},
		{ID: "child", ParentTask: &parent},
		{ID: "downstream", DependsOn: []string{"child"}},
		{ID: "provider-consumer", ProviderDependencies: []ProviderDependency{{ProviderTask: "arch"}}},
		{ID: "reservation-consumer", ProviderReservations: []ProviderReservation{{ProviderTask: "arch"}}},
		{ID: "correction", AmendsPlan: "arch", DependsOn: []string{"arch"}},
		{ID: "unrelated"},
	}}
	for _, id := range []string{"child", "downstream", "provider-consumer", "reservation-consumer"} {
		if got := PendingPlanAmendment(state, state.FindTask(id)); got != "arch" {
			t.Fatalf("%s fence=%q", id, got)
		}
		if unmet := UnmetProviderDependencies(state.FindTask(id), state.Tasks, nil); len(unmet) != 1 || unmet[0].Kind != DependencyUnsatisfiedPending {
			t.Fatalf("%s readiness=%+v", id, unmet)
		}
	}
	for _, id := range []string{"correction", "unrelated"} {
		if got := PendingPlanAmendment(state, state.FindTask(id)); got != "" {
			t.Fatalf("%s unnecessarily fenced by %s", id, got)
		}
	}
}

func TestPendingAmendmentFollowsEffectiveReservationProvider(t *testing.T) {
	t.Parallel()
	old := "old"
	state := &State{Tasks: []Task{
		{ID: old, RolePair: "architecture-pair", TransitionsExecuted: map[string]bool{"replanned": true}},
		{ID: "current", RolePair: "architecture-pair", Supersedes: &old, PlanAmendment: &PlanAmendment{Pending: "correction"}},
		{ID: "consumer", ProviderReservations: []ProviderReservation{{ProviderTask: old, Transition: "architecture-to-code-plan"}}},
		{ID: "correction", AmendsPlan: "current", ProviderReservations: []ProviderReservation{{ProviderTask: old}}},
	}}
	if got := PendingPlanAmendment(state, state.FindTask("consumer")); got != "current" {
		t.Fatalf("reservation successor fence=%q", got)
	}
	if got := PendingPlanAmendment(state, state.FindTask("correction")); got != "" {
		t.Fatalf("correction fenced by %s", got)
	}
	state.FindTask("current").PlanAmendment.Pending = ""
	if got := PendingPlanAmendment(state, state.FindTask("consumer")); got != "" {
		t.Fatalf("adopted correction still fences consumer: %s", got)
	}
}
