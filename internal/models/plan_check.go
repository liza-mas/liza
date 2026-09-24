package models

import "time"

// PlanCheckVerdict is the orchestrator's disposition of a merged plan before
// its children exist.
type PlanCheckVerdict string

const (
	// PlanCheckPassed admits the plan's reviewed hand-off transitions.
	PlanCheckPassed PlanCheckVerdict = "passed"
	// PlanCheckHeld parks the plan until a human performs Ask and an operator
	// clears the hold. Nothing but that clear releases it.
	PlanCheckHeld PlanCheckVerdict = "held"
)

// PlanCheck records the orchestrator's plan-executability disposition of a
// merged planning task. The task ID is the plan's identity: a merged plan's
// output is fixed, and every correction goes through replan, which mints a
// new task without a PlanCheck.
type PlanCheck struct {
	Verdict PlanCheckVerdict `yaml:"verdict" json:"verdict"`
	Ask     string           `yaml:"ask,omitempty" json:"ask,omitempty"`
	By      string           `yaml:"by" json:"by"`
	At      time.Time        `yaml:"at" json:"at"`
}

// PlanCheckVerdictOf returns the task's disposition, or "" when it has none.
func (t *Task) PlanCheckVerdictOf() PlanCheckVerdict {
	if t == nil || t.PlanCheck == nil {
		return ""
	}
	return t.PlanCheck.Verdict
}
