package models

import (
	"fmt"
	"strconv"
	"strings"
)

// DecompositionManifest captures structured ownership metadata for a decomposed task scope.
type DecompositionManifest struct {
	OwnedFiles            []string `yaml:"owned_files,omitempty" json:"owned_files,omitempty"`
	OwnedModules          []string `yaml:"owned_modules,omitempty" json:"owned_modules,omitempty"`
	ReadOnlyDependsOn     []int    `yaml:"read_only_depends_on,omitempty" json:"read_only_depends_on,omitempty"`
	ReadOnlyTaskDependsOn []string `yaml:"read_only_task_depends_on,omitempty" json:"read_only_task_depends_on,omitempty"`
	InterfacesOwned       []string `yaml:"interfaces_owned,omitempty" json:"interfaces_owned,omitempty"`
	InterfacesConsumed    []string `yaml:"interfaces_consumed,omitempty" json:"interfaces_consumed,omitempty"`
	CoverageNotes         string   `yaml:"coverage_notes,omitempty" json:"coverage_notes,omitempty"`
}

// OutputEntry represents a structured subtask definition produced by a doer role.
// When a task completes with output[], each entry defines a downstream child task.
type OutputEntry struct {
	CodingAllocation        bool                     `yaml:"coding_allocation,omitempty" json:"coding_allocation,omitempty"`
	Desc                    string                   `yaml:"desc" json:"desc"`
	DoneWhen                string                   `yaml:"done_when" json:"done_when"`
	Scope                   string                   `yaml:"scope" json:"scope"`
	SpecRef                 string                   `yaml:"spec_ref" json:"spec_ref"`
	EpicRef                 string                   `yaml:"epic_ref,omitempty" json:"epic_ref,omitempty"`
	PlanRef                 string                   `yaml:"plan_ref,omitempty" json:"plan_ref,omitempty"`
	ArchRef                 string                   `yaml:"arch_ref,omitempty" json:"arch_ref,omitempty"`
	Kind                    string                   `yaml:"kind,omitempty" json:"kind,omitempty"`
	Validation              []string                 `yaml:"validation,omitempty" json:"validation,omitempty"`
	ValidationPrerequisites []ValidationPrerequisite `yaml:"validation_prerequisites,omitempty" json:"validation_prerequisites,omitempty"`
	RuntimeInputs           []RuntimeInput           `yaml:"runtime_inputs,omitempty" json:"runtime_inputs,omitempty"`
	DestructiveDB           bool                     `yaml:"destructive_db,omitempty" json:"destructive_db,omitempty"`
	RCARequired             *bool                    `yaml:"rca_required,omitempty" json:"rca_required,omitempty"`
	DependsOn               []string                 `yaml:"depends_on,omitempty" json:"depends_on,omitempty"`
	Decomposition           *DecompositionManifest   `yaml:"decomposition,omitempty" json:"decomposition,omitempty"`
	// TaskDependsOn names existing concrete task IDs to copy onto generated child tasks.
	TaskDependsOn        []string             `yaml:"task_depends_on,omitempty" json:"task_depends_on,omitempty"`
	ProviderDependencies []ProviderDependency `yaml:"provider_dependencies,omitempty" json:"provider_dependencies,omitempty"`
	// DescendantDependencies are copied onto the generated child, which applies
	// them to its own outputs: waits for the tasks it generates (D-79).
	DescendantDependencies []DescendantDependency `yaml:"descendant_dependencies,omitempty" json:"descendant_dependencies,omitempty"`
	// Supersedes names one existing task this output replaces. Generating the
	// child retires that task and retargets its consumers in the same state
	// transaction; several outputs may name one task to split it.
	Supersedes string `yaml:"supersedes,omitempty" json:"supersedes,omitempty"`
	// Changed states what differs from the attempt that blocked. Required when
	// Supersedes names a BLOCKED task (ADR-0171).
	Changed string `yaml:"changed,omitempty" json:"changed,omitempty"`
	// InheritInputs declares whether this child waits for a whole upstream
	// phase or only for selected upstream outputs. Nil means the whole-phase
	// barrier, which is the pre-existing behavior.
	InheritInputs *InheritInputs `yaml:"inherit_inputs,omitempty" json:"inherit_inputs,omitempty"`
}

// InheritMode selects how a generated child inherits upstream phase outputs.
// Authoring validation (ValidateInheritInputs) refuses any value outside the
// set; a defined string type still holds one, so unknown input loads and is
// refused there.
type InheritMode string

// Inherit-input modes. Omitting inherit_inputs entirely is equivalent to
// InheritModeAll and remains the default.
const (
	// InheritModeAll waits for every child of every upstream dependency —
	// the phase-gate barrier described by ADR-0048.
	InheritModeAll InheritMode = "all"
	// InheritModeSelected waits only for the upstream outputs named in
	// Selections.
	InheritModeSelected InheritMode = "selected"
	// InheritModeNone waits for no upstream phase output at all: the child's
	// cross-phase ordering is fully expressed by its sibling and
	// task_depends_on edges. It is the explicit spelling of "external
	// prerequisites: none added", which mode "selected" cannot express because
	// it requires at least one selection.
	InheritModeNone InheritMode = "none"
)

// InheritInputs expresses a planner's dependency intent for one generated
// child: wait for the whole upstream phase, or only for named outputs of it.
//
// Omitted intent is not the same as an empty selection. A nil InheritInputs
// inherits every upstream child (ADR-0048's automatic phase gate); a
// "selected" mode with no resolvable selection is an error, never an empty
// dependency set. Dropping a prerequisite silently is the failure this
// distinction exists to prevent.
type InheritInputs struct {
	Mode       InheritMode      `yaml:"mode" json:"mode"`
	Selections []InputSelection `yaml:"selections,omitempty" json:"selections,omitempty"`
}

// InputSelection names specific outputs of one upstream task.
//
// Outputs are indexes into the upstream task's own output[], not task IDs:
// the upstream's children usually do not exist when a planner authors this,
// so they cannot be named directly. Resolution to concrete child IDs is
// deferred to generation.
type InputSelection struct {
	UpstreamTask string `yaml:"upstream_task" json:"upstream_task"`
	Outputs      []int  `yaml:"outputs" json:"outputs"`
}

// IsSelective reports whether this entry narrows its inherited dependencies.
// Nil and mode "all" are both whole-phase barriers; "selected" and "none"
// both narrow, to named outputs and to nothing respectively.
func (i *InheritInputs) IsSelective() bool {
	return i != nil && (i.Mode == InheritModeSelected || i.Mode == InheritModeNone)
}

// InheritsNothing reports whether this entry declares that no upstream phase
// output is a prerequisite.
func (i *InheritInputs) InheritsNothing() bool {
	return i != nil && i.Mode == InheritModeNone
}

// SelectionFor returns the selected output indexes for one upstream task and
// whether that upstream was named at all. Callers must distinguish "named,
// selecting nothing from it" from "not named": only the latter means the
// upstream contributes no edges.
func (i *InheritInputs) SelectionFor(upstreamTaskID string) ([]int, bool) {
	if !i.IsSelective() {
		return nil, false
	}
	for _, selection := range i.Selections {
		if selection.UpstreamTask == upstreamTaskID {
			return selection.Outputs, true
		}
	}
	return nil, false
}

// ValidateInheritInputs checks authoring-time structure. Index bounds and
// upstream materialization are deliberately not checked here — the upstream's
// output[] may not exist yet — and are enforced at generation instead.
func ValidateInheritInputs(inherit *InheritInputs, entryIndex int) error {
	if inherit == nil {
		return nil
	}
	switch inherit.Mode {
	case InheritModeAll, InheritModeNone:
		if len(inherit.Selections) > 0 {
			return fmt.Errorf("output[%d].inherit_inputs: mode %q must not carry selections", entryIndex, inherit.Mode)
		}
		return nil
	case InheritModeSelected:
	default:
		return fmt.Errorf("output[%d].inherit_inputs: mode must be %q, %q or %q (got %q)",
			entryIndex, InheritModeAll, InheritModeSelected, InheritModeNone, inherit.Mode)
	}

	if len(inherit.Selections) == 0 {
		return fmt.Errorf("output[%d].inherit_inputs: mode %q requires at least one selection; "+
			"to wait for every upstream output use mode %q or omit inherit_inputs",
			entryIndex, InheritModeSelected, InheritModeAll)
	}

	seenUpstream := make(map[string]struct{}, len(inherit.Selections))
	for selectionIndex, selection := range inherit.Selections {
		upstream := strings.TrimSpace(selection.UpstreamTask)
		if upstream == "" || upstream != selection.UpstreamTask {
			return fmt.Errorf("output[%d].inherit_inputs.selections[%d]: upstream_task must be non-empty and trimmed",
				entryIndex, selectionIndex)
		}
		if _, duplicate := seenUpstream[upstream]; duplicate {
			return fmt.Errorf("output[%d].inherit_inputs.selections[%d]: duplicate upstream_task %q",
				entryIndex, selectionIndex, upstream)
		}
		seenUpstream[upstream] = struct{}{}

		if len(selection.Outputs) == 0 {
			return fmt.Errorf("output[%d].inherit_inputs.selections[%d]: upstream_task %q selects no outputs; "+
				"remove the selection to depend on nothing from it, or name the outputs required",
				entryIndex, selectionIndex, upstream)
		}
		seenOutput := make(map[int]struct{}, len(selection.Outputs))
		for _, output := range selection.Outputs {
			if output < 0 {
				return fmt.Errorf("output[%d].inherit_inputs.selections[%d]: negative output index %d",
					entryIndex, selectionIndex, output)
			}
			if _, duplicate := seenOutput[output]; duplicate {
				return fmt.Errorf("output[%d].inherit_inputs.selections[%d]: duplicate output index %d",
					entryIndex, selectionIndex, output)
			}
			seenOutput[output] = struct{}{}
		}
	}
	return nil
}

// ValidateDependsOn checks that DependsOn indices are valid references within
// [0, totalEntries), are numeric, and don't self-reference. Returns a plain error
// suitable for wrapping by callers into their preferred error type.
func ValidateDependsOn(deps []string, entryIndex, totalEntries int) error {
	for _, ref := range deps {
		idx, err := strconv.Atoi(ref)
		if err != nil {
			return fmt.Errorf("output[%d] depends_on contains non-numeric reference %q", entryIndex, ref)
		}
		if idx < 0 || idx >= totalEntries {
			return fmt.Errorf("output[%d] depends_on reference %q out of range [0, %d)", entryIndex, ref, totalEntries)
		}
		if idx == entryIndex {
			return fmt.Errorf("output[%d] depends_on references itself", entryIndex)
		}
	}
	return nil
}
