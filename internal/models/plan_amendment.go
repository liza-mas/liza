package models

import (
	"fmt"
	"reflect"
	"slices"
)

// PlanAmendment retains the original allocation manifest and ordered correction
// lineage. Original review facts remain immutable; only Applied supplies current
// review authority. Quarantined merged predecessors supply historical authorship.
type PlanAmendment struct {
	Pending        string        `yaml:"pending,omitempty" json:"pending,omitempty"`
	OriginalOutput []OutputEntry `yaml:"original_output" json:"original_output"`
	Corrections    []string      `yaml:"corrections" json:"corrections"`
	Applied        []string      `yaml:"applied,omitempty" json:"applied,omitempty"`
	Quarantined    []string      `yaml:"quarantined,omitempty" json:"quarantined,omitempty"`
}

// PlanGenerationFenced applies to every transition, including operator and
// recovery paths, regardless of current task status or pipeline cardinality.
func (t *Task) PlanGenerationFenced() bool {
	return t != nil && (t.AmendsPlan != "" || t.PlanAmendment != nil && t.PlanAmendment.Pending != "")
}

// ValidateAmendmentOutput freezes every existing slot except an explicit
// scheduling/prerequisite allowlist. Added producers append without remapping
// indexes selected by existing consumers.
func ValidateAmendmentOutput(original, correction []OutputEntry) error {
	if len(correction) < len(original) {
		return fmt.Errorf("amendment cannot remove existing output slots")
	}
	for i, old := range original {
		next := correction[i]
		freeze := func(entry OutputEntry) OutputEntry {
			entry.DependsOn, entry.TaskDependsOn = nil, nil
			entry.ProviderDependencies, entry.DescendantDependencies = nil, nil
			entry.InheritInputs = nil
			entry.Validation, entry.ValidationPrerequisites, entry.RuntimeInputs = nil, nil, nil
			return entry
		}
		if !reflect.DeepEqual(freeze(old), freeze(next)) {
			return fmt.Errorf("amendment output[%d] changes existing slot identity or permission; existing slots cannot be reordered or redefined", i)
		}
	}
	return nil
}

// PlanAmendmentTaskIDs is the conservative set read by amendment evidence
// consumers. Include pending and quarantined records in concurrency digests.
func PlanAmendmentTaskIDs(task *Task) []string {
	if task == nil || task.PlanAmendment == nil {
		return nil
	}
	return slices.Clone(task.PlanAmendment.Corrections)
}
