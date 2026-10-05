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
	// PlanCheckReplaced retires an unused hand-off by an operator declaration.
	PlanCheckReplaced PlanCheckVerdict = "replaced"
)

// PlanCheck records the orchestrator's plan-executability disposition of a
// merged planning task. The task ID is the plan's identity: a merged plan's
// output is fixed. An operator can retire its unused hand-off by a separately
// merged correction without changing its delivered MERGED status.
type PlanCheck struct {
	Verdict    PlanCheckVerdict `yaml:"verdict" json:"verdict"`
	Ask        string           `yaml:"ask,omitempty" json:"ask,omitempty"`
	ReplacedBy string           `yaml:"replaced_by,omitempty" json:"replaced_by,omitempty"`
	By         string           `yaml:"by" json:"by"`
	At         time.Time        `yaml:"at" json:"at"`
}

// PlanHandoffRetired reports an explicit retirement, distinct from replan.
func (t *Task) PlanHandoffRetired() bool {
	return t.PlanCheckVerdictOf() == PlanCheckReplaced
}

// PlanCheckVerdictOf returns the task's disposition, or "" when it has none.
func (t *Task) PlanCheckVerdictOf() PlanCheckVerdict {
	if t == nil || t.PlanCheck == nil {
		return ""
	}
	return t.PlanCheck.Verdict
}
