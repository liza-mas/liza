package models

import (
	"fmt"
	"strings"

	"github.com/liza-mas/liza/internal/paths"
)

// ProviderReservation holds the writer carrying it until every child its
// provider generates at Transition has merged (D-80, ADR-0195). Unlike a
// ProviderDependency it selects no output indexes, so it covers outputs the
// provider has not authored yet and cannot be escaped by an extra output.
type ProviderReservation struct {
	ProviderTask string `yaml:"provider_task" json:"provider_task"`
	Transition   string `yaml:"transition" json:"transition"`
}

// ValidateProviderReservations validates reservation shape; provider identity,
// role and transition need state.
func ValidateProviderReservations(reservations []ProviderReservation) error {
	seen := make(map[ProviderReservation]bool, len(reservations))
	for i, res := range reservations {
		if err := paths.ValidateTaskID(res.ProviderTask); err != nil {
			return fmt.Errorf("provider_reservations[%d].provider_task: %w", i, err)
		}
		if res.Transition == "" || strings.TrimSpace(res.Transition) != res.Transition {
			return fmt.Errorf("provider_reservations[%d].transition must be non-empty and trimmed", i)
		}
		if seen[res] {
			return fmt.Errorf("provider_reservations[%d] duplicates provider %q transition %q", i, res.ProviderTask, res.Transition)
		}
		seen[res] = true
	}
	return nil
}

// EffectiveReservationProvider follows a reserved provider retired into a
// replan or same-pair replace-task successor, which keeps its work (D-70):
// the reservation holds the successor in the same transaction that retires
// the original. Any other ID is its own effective provider.
func EffectiveReservationProvider(state *State, id string) string {
	if successor, ok := ProviderChildSuccessor(state, id); ok {
		return successor.ID
	}
	return id
}

// ReservationDependency is the reservation as a provider declaration selecting
// every output its effective provider has authored, or false while there is
// none: every child those outputs can generate, and nothing else.
func ReservationDependency(res ProviderReservation, state *State) (ProviderDependency, bool) {
	provider := state.FindTask(EffectiveReservationProvider(state, res.ProviderTask))
	if provider == nil || len(provider.Output) == 0 {
		return ProviderDependency{}, false
	}
	dep := ProviderDependency{ProviderTask: provider.ID, Transition: res.Transition, Outputs: make([]int, len(provider.Output))}
	for i := range dep.Outputs {
		dep.Outputs[i] = i
	}
	return dep, true
}

// ResolveReservation applies the provider-declaration rules to every output of
// the effective provider, failing closed on an unusable transition, a missing
// or retired provider or a Kind output. A MERGED provider whose hand-off ran
// with no output satisfies it: it generated nothing to wait for.
func ResolveReservation(res ProviderReservation, state *State, pr PipelineResolver) DependencySatisfaction {
	if dep, ok := ReservationDependency(res, state); ok {
		result := resolveProviderDependency(dep, state, pr)
		if result.Invalid() {
			result.DependencyID = res.ProviderTask
		}
		return result
	}
	providerID := EffectiveReservationProvider(state, res.ProviderTask)
	result := DependencySatisfaction{DependencyID: res.ProviderTask, Kind: DependencyInvalidProvider, Path: []string{providerID}}
	invalid := func(reason string) DependencySatisfaction {
		result.Reason = reason
		return result
	}
	projection, ok := pr.(ProviderTransitionResolver)
	if !ok {
		return invalid("provider transition resolution is unavailable")
	}
	td, err := projection.ProviderTransition(res.Transition)
	if err != nil {
		return invalid(err.Error())
	}
	if td.Cardinality != "per-subtask" || td.SourceRolePair == "" || td.TargetRolePair == "" || td.TaskSlug == "" {
		return invalid(fmt.Sprintf("provider transition %q must identify a per-subtask source, target and task slug", res.Transition))
	}
	provider := state.FindTask(providerID)
	if provider == nil {
		return invalid("provider task does not exist")
	}
	if provider.RolePair != td.SourceRolePair {
		return invalid("provider role_pair does not match transition source")
	}
	if ProviderRetired(provider) {
		return invalid("provider task or handoff was retired")
	}
	result.Kind = DependencyUnsatisfiedPending
	result.BlockingIDs = []string{provider.ID}
	switch {
	case provider.Status != TaskStatusMerged:
		result.Reason = "provider task is not MERGED"
	case provider.PlanCheckVerdictOf() == PlanCheckHeld:
		result.Reason = "provider handoff is held"
	case !provider.TransitionsExecuted[res.Transition]:
		result.Reason = "provider transition has not executed"
	default:
		return DependencySatisfaction{DependencyID: res.ProviderTask, Kind: DependencySatisfiedDirect, Path: []string{providerID}}
	}
	return result
}

// UnmetProviderReservations lists the reservations of task that do not yet
// admit it, with the same fail-closed interpretation as readiness.
func UnmetProviderReservations(task *Task, state *State, pr PipelineResolver) []DependencySatisfaction {
	if task == nil || len(task.ProviderReservations) == 0 {
		return nil
	}
	if err := ValidateProviderReservations(task.ProviderReservations); err != nil {
		return []DependencySatisfaction{{DependencyID: task.ID, Kind: DependencyInvalidProvider, Reason: err.Error()}}
	}
	var unmet []DependencySatisfaction
	for _, res := range task.ProviderReservations {
		if result := ResolveReservation(res, state, pr); !result.Satisfied() {
			unmet = append(unmet, result)
		}
	}
	return unmet
}

// HoldsReservation reports whether task carries a reservation on provider at
// transition.
func HoldsReservation(task *Task, provider, transition string) bool {
	for _, res := range task.ProviderReservations {
		if res.ProviderTask == provider && res.Transition == transition {
			return true
		}
	}
	return false
}
