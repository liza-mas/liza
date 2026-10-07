package models

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/liza-mas/liza/internal/paths"
	"gopkg.in/yaml.v3"
)

// ProviderDependency retains a prerequisite on selected outputs of a provider's
// per-subtask transition, including before the generated tasks exist.
type ProviderDependency struct {
	ProviderTask string `yaml:"provider_task" json:"provider_task"`
	Transition   string `yaml:"transition" json:"transition"`
	Outputs      []int  `yaml:"outputs" json:"outputs"`
}

// UnmarshalJSON prevents JSON null elements from silently decoding to index 0.
// Missing or empty selections remain the shape validator's responsibility.
func (dep *ProviderDependency) UnmarshalJSON(data []byte) error {
	type plain ProviderDependency
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var raw struct {
		Outputs []json.RawMessage `json:"outputs"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for index, output := range raw.Outputs {
		if strings.TrimSpace(string(output)) == "null" {
			return fmt.Errorf("provider dependency outputs[%d] must be an integer, not null", index)
		}
	}
	*dep = ProviderDependency(decoded)
	return nil
}

// UnmarshalYAML requires integer scalar selections; YAML's typed decoding can
// otherwise coerce null or floating-point scalars into integer indexes.
func (dep *ProviderDependency) UnmarshalYAML(node *yaml.Node) error {
	type plain ProviderDependency
	var decoded plain
	if err := node.Decode(&decoded); err != nil {
		return err
	}
	var raw struct {
		Outputs []yaml.Node `yaml:"outputs"`
	}
	if err := node.Decode(&raw); err != nil {
		return err
	}
	for index := range raw.Outputs {
		scalar := &raw.Outputs[index]
		for scalar.Kind == yaml.AliasNode {
			scalar = scalar.Alias
		}
		if scalar.Kind != yaml.ScalarNode || scalar.Tag != "!!int" {
			return fmt.Errorf("provider dependency outputs[%d] must be an integer scalar", index)
		}
	}
	*dep = ProviderDependency(decoded)
	return nil
}

// ProviderTransition is the configured information needed to identify children.
// It is owned by models so readiness does not depend on the pipeline package.
type ProviderTransition struct {
	SourceRolePair string
	TargetRolePair string
	TaskSlug       string
	Cardinality    string
}

// ProviderTransitionResolver is optional for legacy PipelineResolver callers.
// Explicit declarations fail closed when the capability is absent.
type ProviderTransitionResolver interface {
	ProviderTransition(name string) (ProviderTransition, error)
}

// HasProviderDependencies reports whether any stored task or output declares
// provider prerequisites, including terminal records retained for audit.
func HasProviderDependencies(state *State) bool {
	if state == nil {
		return false
	}
	for _, task := range state.Tasks {
		if len(task.ProviderDependencies) > 0 {
			return true
		}
		for _, output := range task.Output {
			if len(output.ProviderDependencies) > 0 {
				return true
			}
		}
	}
	return false
}

// ValidateProviderDependencies validates declaration shape without requiring
// provider output to exist yet. Bounds and provider identity need state.
func ValidateProviderDependencies(deps []ProviderDependency) error {
	seen := make(map[[2]string]bool, len(deps))
	for i, dep := range deps {
		if err := paths.ValidateTaskID(dep.ProviderTask); err != nil {
			return fmt.Errorf("provider_dependencies[%d].provider_task: %w", i, err)
		}
		if dep.Transition == "" || strings.TrimSpace(dep.Transition) != dep.Transition {
			return fmt.Errorf("provider_dependencies[%d].transition must be non-empty and trimmed", i)
		}
		key := [2]string{dep.ProviderTask, dep.Transition}
		if seen[key] {
			return fmt.Errorf("provider_dependencies[%d] duplicates provider %q transition %q", i, dep.ProviderTask, dep.Transition)
		}
		seen[key] = true
		if len(dep.Outputs) == 0 {
			return fmt.Errorf("provider_dependencies[%d].outputs must select at least one output", i)
		}
		seenOutputs := make(map[int]bool, len(dep.Outputs))
		for _, output := range dep.Outputs {
			if output < 0 || seenOutputs[output] {
				return fmt.Errorf("provider_dependencies[%d].outputs must contain distinct nonnegative indexes", i)
			}
			seenOutputs[output] = true
		}
	}
	return nil
}

// CloneProviderDependencies copies declarations without sharing output slices.
func CloneProviderDependencies(deps []ProviderDependency) []ProviderDependency {
	cloned := slices.Clone(deps)
	for i := range cloned {
		cloned[i].Outputs = slices.Clone(deps[i].Outputs)
	}
	return cloned
}

// ProviderDependenciesEqual compares a generated declaration to its source,
// treating omitted and empty legacy declarations identically.
func ProviderDependenciesEqual(a, b []ProviderDependency) bool {
	return len(a) == 0 && len(b) == 0 || reflect.DeepEqual(a, b)
}

// ProviderDependencyChildren projects configured child IDs without requiring
// materialization. Callers validate bounds, source role, and Kind against state:
// Kind-deduplicated outputs cannot promise the deterministic child identity.
func ProviderDependencyChildren(dep ProviderDependency, pr PipelineResolver) (ProviderTransition, []string, error) {
	if err := ValidateProviderDependencies([]ProviderDependency{dep}); err != nil {
		return ProviderTransition{}, nil, err
	}
	projection, ok := pr.(ProviderTransitionResolver)
	if !ok {
		return ProviderTransition{}, nil, fmt.Errorf("provider transition resolution is unavailable")
	}
	td, err := projection.ProviderTransition(dep.Transition)
	if err != nil {
		return ProviderTransition{}, nil, err
	}
	if td.Cardinality != "per-subtask" || td.SourceRolePair == "" || td.TargetRolePair == "" || td.TaskSlug == "" {
		return ProviderTransition{}, nil, fmt.Errorf("provider transition %q must identify a per-subtask source, target and task slug", dep.Transition)
	}
	children := make([]string, len(dep.Outputs))
	for i, output := range dep.Outputs {
		children[i] = fmt.Sprintf("%s-%s-%d", dep.ProviderTask, td.TaskSlug, output)
	}
	return td, children, nil
}

// EffectiveProviderChildren is the projection with each replanned child
// resolved to its ReplanSuccessor, which keeps the provider's output slot and
// parent. An unresolvable replanned child keeps its projected ID, so the
// retirement checks on it still fail closed. Output indexes are never mapped.
func EffectiveProviderChildren(dep ProviderDependency, state *State, pr PipelineResolver) (ProviderTransition, []string, error) {
	td, children, err := ProviderDependencyChildren(dep, pr)
	if err != nil {
		return td, nil, err
	}
	for i, id := range children {
		if successor, ok := ReplanSuccessor(state, id); ok {
			children[i] = successor.ID
		}
	}
	return td, children, nil
}

// UnmetProviderDependencies applies the same fail-closed interpretation used by
// readiness and committing claims. Retired/malformed references are invalid;
// legitimate future children and unmerged work are pending, never satisfied.
func UnmetProviderDependencies(task *Task, allTasks []Task, pr PipelineResolver) []DependencySatisfaction {
	if task == nil {
		return nil
	}
	state := &State{Tasks: allTasks}
	if mismatch := generatedProviderDeclarationMismatch(task, state, pr); mismatch != "" {
		return []DependencySatisfaction{{DependencyID: task.ID, Kind: DependencyInvalidProvider, Reason: mismatch}}
	}
	if len(task.ProviderDependencies) == 0 {
		return nil
	}
	if err := ValidateProviderDependencies(task.ProviderDependencies); err != nil {
		return []DependencySatisfaction{{DependencyID: task.ID, Kind: DependencyInvalidProvider, Reason: err.Error()}}
	}
	var unmet []DependencySatisfaction
	for _, dep := range task.ProviderDependencies {
		result := resolveProviderDependency(dep, state, pr)
		if !result.Satisfied() {
			unmet = append(unmet, result)
		}
	}
	return unmet
}

// Generation intent remains explicit on the parent during crash recovery.
// A missing/conflicting copy must not make the existing child look legacy-ready.
func generatedProviderDeclarationMismatch(task *Task, state *State, pr PipelineResolver) string {
	if task.Status.IsTerminal() {
		return ""
	}
	for _, parentID := range task.EffectiveParentTasks() {
		parent := state.FindTask(parentID)
		if parent == nil || parent.Status == TaskStatusSuperseded || parent.Status == TaskStatusAbandoned || parent.TransitionsExecuted["replanned"] || parent.PlanHandoffRetired() {
			continue
		}
		for index, output := range parent.Output {
			if len(output.ProviderDependencies) == 0 {
				continue
			}
			if _, available := pr.(ProviderTransitionResolver); !available {
				return "generated provider declaration resolution is unavailable"
			}
			for name := range parent.TransitionsExecuted {
				td, ids, err := ProviderDependencyChildren(ProviderDependency{ProviderTask: parentID, Transition: name, Outputs: []int{index}}, pr)
				if err != nil || td.SourceRolePair != parent.RolePair || ids[0] != task.ID {
					continue
				}
				if td.TargetRolePair != task.RolePair || len(task.EffectiveParentTasks()) != 1 || !ProviderDependenciesEqual(task.ProviderDependencies, output.ProviderDependencies) {
					return fmt.Sprintf("generated provider_dependencies do not match %s output[%d]; recover transition %s before claiming", parentID, index, name)
				}
			}
		}
	}
	return ""
}

func resolveProviderDependency(dep ProviderDependency, state *State, pr PipelineResolver) DependencySatisfaction {
	result := DependencySatisfaction{DependencyID: dep.ProviderTask, Kind: DependencyInvalidProvider, Path: []string{dep.ProviderTask}}
	invalid := func(reason string) DependencySatisfaction {
		result.Reason = reason
		return result
	}
	pending := func(blocker, reason string) DependencySatisfaction {
		result.Kind = DependencyUnsatisfiedPending
		result.BlockingIDs = []string{blocker}
		result.Reason = reason
		return result
	}
	td, children, err := EffectiveProviderChildren(dep, state, pr)
	if err != nil {
		return invalid(err.Error())
	}
	provider := state.FindTask(dep.ProviderTask)
	if provider == nil {
		return invalid("provider task does not exist")
	}
	if provider.RolePair != td.SourceRolePair {
		return invalid("provider role_pair does not match transition source")
	}
	if provider.TransitionsExecuted["replanned"] || provider.PlanHandoffRetired() || (provider.Status.IsTerminal() && provider.Status != TaskStatusMerged) {
		return invalid("provider task or handoff was retired")
	}
	for position, output := range dep.Outputs {
		if output >= len(provider.Output) {
			if len(provider.Output) > 0 || provider.Status == TaskStatusMerged {
				return invalid(fmt.Sprintf("provider output index %d is out of range", output))
			}
		} else if provider.Output[output].Kind != "" {
			return invalid(fmt.Sprintf("provider output index %d has Kind and may be deduplicated", output))
		}
		if child := state.FindTask(children[position]); child != nil {
			parents := child.EffectiveParentTasks()
			if child.RolePair != td.TargetRolePair || len(parents) != 1 || parents[0] != provider.ID {
				return invalid(fmt.Sprintf("selected provider child %s has wrong role or provenance", child.ID))
			}
			if child.TransitionsExecuted["replanned"] || child.PlanHandoffRetired() || (child.Status.IsTerminal() && child.Status != TaskStatusMerged) {
				return invalid(fmt.Sprintf("selected provider child %s was retired", child.ID))
			}
		}
	}
	if provider.Status != TaskStatusMerged {
		return pending(provider.ID, "provider task is not MERGED")
	}
	if provider.PlanCheckVerdictOf() == PlanCheckHeld {
		return pending(provider.ID, "provider handoff is held")
	}
	if !provider.TransitionsExecuted[dep.Transition] {
		return pending(provider.ID, "provider transition has not executed")
	}
	for _, childID := range children {
		child := state.FindTask(childID)
		if child == nil {
			return pending(childID, "selected provider child does not exist yet")
		}
		if child.Status != TaskStatusMerged {
			return pending(childID, "selected provider child is not MERGED")
		}
	}
	result.Kind = DependencySatisfiedDirect
	return result
}
