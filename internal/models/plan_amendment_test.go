package models

import "testing"

func TestPlanAmendmentPreservesExistingIdentityAndPermissions(t *testing.T) {
	original := []OutputEntry{{Desc: "implement boundary", DoneWhen: "malformed input rejected", Scope: "boundary", SpecRef: "goal.md#Boundary", PlanRef: "plan.md#Task 1"}, {Desc: "separate producer", DoneWhen: "ready", Scope: "producer", SpecRef: "goal.md#Producer"}}
	for name, change := range map[string]func([]OutputEntry) []OutputEntry{
		"remove":                 func(o []OutputEntry) []OutputEntry { return o[:1] },
		"reorder":                func(o []OutputEntry) []OutputEntry { o[0], o[1] = o[1], o[0]; return o },
		"description":            func(o []OutputEntry) []OutputEntry { o[0].Desc = "different work"; return o },
		"completion":             func(o []OutputEntry) []OutputEntry { o[0].DoneWhen = "weaker acceptance"; return o },
		"scope":                  func(o []OutputEntry) []OutputEntry { o[0].Scope = "broader"; return o },
		"artifact":               func(o []OutputEntry) []OutputEntry { o[0].PlanRef = "other.md#Task 1"; return o },
		"kind":                   func(o []OutputEntry) []OutputEntry { o[0].Kind = "bootstrap-precommit"; return o },
		"replacement":            func(o []OutputEntry) []OutputEntry { o[0].Supersedes = "other"; return o },
		"destructive permission": func(o []OutputEntry) []OutputEntry { o[0].DestructiveDB = true; return o },
		"RCA permission":         func(o []OutputEntry) []OutputEntry { off := false; o[0].RCARequired = &off; return o },
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateAmendmentOutput(original, change(append([]OutputEntry(nil), original...))); err == nil {
				t.Fatal("existing slot was redefined")
			}
		})
	}
	correction := append([]OutputEntry(nil), original...)
	correction[0].DependsOn = []string{"2"}
	correction[0].Validation = []string{"check boundary"}
	correction[0].InheritInputs = &InheritInputs{Mode: InheritModeNone}
	correction = append(correction, OutputEntry{Desc: "new prerequisite producer", DoneWhen: "input provided", Scope: "prerequisite", SpecRef: "goal.md#Producer"})
	if err := ValidateAmendmentOutput(original, correction); err != nil {
		t.Fatalf("bounded scheduling and appended producer refused: %v", err)
	}
}
