package models

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
)

// PlanAmendmentMode distinguishes legacy unused-plan scheduling corrections
// from reviewed replacements retaining the complete allocation manifest.
type PlanAmendmentMode string

const (
	PlanAmendmentContract         PlanAmendmentMode = "contract"
	PlanAmendmentPreserveIdentity PlanAmendmentMode = "preserve-output-identity"
)

func (mode PlanAmendmentMode) IsValid() bool {
	return mode == "" || mode == PlanAmendmentContract || mode == PlanAmendmentPreserveIdentity
}

// ValidateAmendmentOutputForMode keeps expanded contracts and identity-preserving
// replacements from changing already allocated work or selector semantics.
func ValidateAmendmentOutputForMode(original, correction []OutputEntry, mode PlanAmendmentMode) error {
	if !mode.IsValid() {
		return fmt.Errorf("unknown amendment mode %q", mode)
	}
	if mode != "" {
		// Publication and state reload normalize optional nil/empty lists
		// differently. Compare their complete serialized manifests, which omit
		// both spellings, without dropping declared values or explicit modes.
		before, err := json.Marshal(original)
		if err != nil {
			return fmt.Errorf("serializing original output manifest: %w", err)
		}
		after, err := json.Marshal(correction)
		if err != nil {
			return fmt.Errorf("serializing correction output manifest: %w", err)
		}
		if !bytes.Equal(before, after) {
			return fmt.Errorf("%s amendment requires the complete output manifest unchanged", mode)
		}
		return nil
	}
	return ValidateAmendmentOutput(original, correction)
}

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
