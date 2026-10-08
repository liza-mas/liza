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
		if len(task.ProviderDependencies) > 0 || len(task.DescendantDependencies) > 0 || len(task.ProviderReservations) > 0 {
			return true
		}
		for _, output := range task.Output {
			if len(output.ProviderDependencies) > 0 || len(output.DescendantDependencies) > 0 {
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

// DescendantDependency defers typed provider waits by one generation (D-79,
// ADR-0193): the task carrying it applies them to every output it writes for
// AtTransition, so a writer-order wait holds the writers, not their planner.
type DescendantDependency struct {
	AtTransition         string               `yaml:"at_transition" json:"at_transition"`
	ProviderDependencies []ProviderDependency `yaml:"provider_dependencies" json:"provider_dependencies"`
}

// ValidateDescendantDependencies validates declaration shape; the transition
// topology and provider references need the pipeline and state.
func ValidateDescendantDependencies(deps []DescendantDependency) error {
	seen := make(map[string]bool, len(deps))
	for i, dep := range deps {
		if dep.AtTransition == "" || strings.TrimSpace(dep.AtTransition) != dep.AtTransition {
			return fmt.Errorf("descendant_dependencies[%d].at_transition must be non-empty and trimmed", i)
		}
		if seen[dep.AtTransition] {
			return fmt.Errorf("descendant_dependencies[%d] duplicates at_transition %q", i, dep.AtTransition)
		}
		seen[dep.AtTransition] = true
		if len(dep.ProviderDependencies) == 0 {
			return fmt.Errorf("descendant_dependencies[%d].provider_dependencies must declare at least one wait", i)
		}
		if err := ValidateProviderDependencies(dep.ProviderDependencies); err != nil {
			return fmt.Errorf("descendant_dependencies[%d].%w", i, err)
		}
	}
	return nil
}

// CloneDescendantDependencies deep-copies descendant declarations.
func CloneDescendantDependencies(deps []DescendantDependency) []DescendantDependency {
	cloned := slices.Clone(deps)
	for i := range cloned {
		cloned[i].ProviderDependencies = CloneProviderDependencies(deps[i].ProviderDependencies)
	}
	return cloned
}

// DescendantProviderDependencies flattens the waits of every descendant entry.
func DescendantProviderDependencies(deps []DescendantDependency) []ProviderDependency {
	var flat []ProviderDependency
	for _, dep := range deps {
		flat = append(flat, dep.ProviderDependencies...)
	}
	return flat
}

// AppliedDescendantDependencies returns the waits task applies to its output
// for transition.
func AppliedDescendantDependencies(task *Task, transition string) []ProviderDependency {
	for _, dep := range task.DescendantDependencies {
		if dep.AtTransition == transition {
			return dep.ProviderDependencies
		}
	}
	return nil
}

// MissingProviderDependencies names each wait in required that deps does not
// carry: a declaration for the same provider and transition selecting every
// required output.
func MissingProviderDependencies(deps, required []ProviderDependency) []ProviderDependency {
	var missing []ProviderDependency
	for _, want := range required {
		if !slices.ContainsFunc(deps, func(dep ProviderDependency) bool {
			return dep.ProviderTask == want.ProviderTask && dep.Transition == want.Transition &&
				!slices.ContainsFunc(want.Outputs, func(output int) bool { return !slices.Contains(dep.Outputs, output) })
		}) {
			missing = append(missing, want)
		}
	}
	return missing
}

// GeneratedDeclarationsMatch reports whether a generated child carries its
// source output's declarations: the descendant ones, and the provider ones
// except any an operator deferred to the child's descendants
// (defer-provider-dependency). Nothing is dropped, invented or moved back.
func GeneratedDeclarationsMatch(child *Task, output OutputEntry) bool {
	if !containsAllProviderDependencies(output.ProviderDependencies, child.ProviderDependencies) {
		return false // invented
	}
	expected := DescendantProviderDependencies(output.DescendantDependencies)
	for _, dep := range output.ProviderDependencies {
		if !containsProviderDependency(child.ProviderDependencies, dep) {
			expected = append(expected, dep) // deferred
		}
	}
	actual := DescendantProviderDependencies(child.DescendantDependencies)
	return containsAllProviderDependencies(actual, expected) && containsAllProviderDependencies(expected, actual)
}

func containsProviderDependency(deps []ProviderDependency, want ProviderDependency) bool {
	return slices.ContainsFunc(deps, func(dep ProviderDependency) bool { return reflect.DeepEqual(dep, want) })
}

func containsAllProviderDependencies(deps, wanted []ProviderDependency) bool {
	return !slices.ContainsFunc(wanted, func(dep ProviderDependency) bool { return !containsProviderDependency(deps, dep) })
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

// EffectiveProviderChildren is the projection with each replanned or
// replace-task-replaced child resolved to its ProviderChildSuccessor, which
// keeps the provider's output slot and parent. An unresolvable child keeps its
// projected ID, so the retirement checks on it still fail closed. Output
// indexes are never mapped.
func EffectiveProviderChildren(dep ProviderDependency, state *State, pr PipelineResolver) (ProviderTransition, []string, error) {
	td, children, err := ProviderDependencyChildren(dep, pr)
	if err != nil {
		return td, nil, err
	}
	for i, id := range children {
		if successor, ok := ProviderChildSuccessor(state, id); ok {
			children[i] = successor.ID
		}
	}
	return td, children, nil
}

// ProviderRetired reports a provider whose reviewed output can never be
// generated: replanned, hand-off retired, or terminal other than MERGED.
func ProviderRetired(provider *Task) bool {
	return provider.TransitionsExecuted["replanned"] || provider.PlanHandoffRetired() ||
		(provider.Status.IsTerminal() && provider.Status != TaskStatusMerged)
}

// PlanUnexpanded reports a MERGED plan whose output has generated nothing: no
// transition marker and no task naming it as parent. Its output declarations
// are reviewed content awaiting hand-off, not generated prerequisites, so a
// provider retired under them leaves the plan stale rather than the state
// invalid (ADR-0185).
func PlanUnexpanded(state *State, task *Task) bool {
	if task.Status != TaskStatusMerged || len(task.Output) == 0 || len(task.TransitionsExecuted) > 0 || task.PlanHandoffRetired() {
		return false
	}
	for i := range state.Tasks {
		if slices.Contains(state.Tasks[i].EffectiveParentTasks(), task.ID) {
			return false
		}
	}
	return true
}

// DraftOutput reports output nothing has been generated from, and nothing can
// be until it passes a fresh approval: its owner is in its role pair's
// initial, executing, rejected, submitted, reviewing or quorum status,
// BLOCKED, or INTEGRATION_FAILED. The approved status reaches MERGED without a
// new verdict, so its output is not a draft; neither is any status the
// pipeline does not name. INTEGRATION_FAILED can also reach MERGED by external
// reconciliation, which leaves an unexpanded plan whose stale declarations
// hand-off refuses (ADR-0185, ADR-0188).
func DraftOutput(task *Task, pr PipelineResolver) bool {
	if task == nil || pr == nil || len(task.Output) == 0 {
		return false
	}
	if task.Status == TaskStatusBlocked || task.Status == TaskStatusIntegrationFailed {
		return true
	}
	for _, status := range []func(string) (TaskStatus, error){
		pr.InitialStatus, pr.ExecutingStatus, pr.RejectedStatus, pr.SubmittedStatus,
		pr.ReviewingStatus, pr.Reviewing2Status, pr.PartiallyApprovedStatus,
	} {
		if draft, err := status(task.RolePair); err == nil && draft == task.Status {
			return true
		}
	}
	return false
}

// OutputMayGoStale reports output whose declarations a retired provider leaves
// stale rather than the state invalid: an unexpanded plan (ADR-0185) or draft
// output (ADR-0188).
func OutputMayGoStale(state *State, task *Task, pr PipelineResolver) bool {
	return PlanUnexpanded(state, task) || DraftOutput(task, pr)
}

// UnstartedProviderConsumer reports a task nobody has worked on: in its role
// pair's initial status or BLOCKED, never claimed, with no assignee, lease,
// worktree or pending hand-off. Its task-level declarations consume nothing
// yet, so a provider they name directly retired, or a selected child retired
// permanently, leaves the task stale rather than the state invalid (ADR-0187,
// ADR-0190).
func UnstartedProviderConsumer(task *Task, pr PipelineResolver) bool {
	if task == nil || pr == nil || (task.AssignedTo != nil && *task.AssignedTo != "") || task.LeaseExpires != nil ||
		(task.Worktree != nil && *task.Worktree != "") || task.HandoffPending {
		return false
	}
	if task.Status != TaskStatusBlocked {
		if initial, err := pr.InitialStatus(task.RolePair); err != nil || task.Status != initial {
			return false
		}
	}
	return !slices.ContainsFunc(task.History, func(entry TaskHistoryEntry) bool {
		return entry.Event == TaskEventClaimed || entry.Event == TaskEventClaimedForIntegrationFix
	})
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
	unmet := UnmetProviderReservations(task, state, pr)
	if len(task.ProviderDependencies) == 0 {
		return unmet
	}
	if err := ValidateProviderDependencies(task.ProviderDependencies); err != nil {
		return append(unmet, DependencySatisfaction{DependencyID: task.ID, Kind: DependencyInvalidProvider, Reason: err.Error()})
	}
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
			if len(output.ProviderDependencies) == 0 && len(output.DescendantDependencies) == 0 {
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
				if td.TargetRolePair != task.RolePair || len(task.EffectiveParentTasks()) != 1 || !GeneratedDeclarationsMatch(task, output) {
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
	if ProviderRetired(provider) {
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
