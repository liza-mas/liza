package statevalidate

import (
	"fmt"
	"slices"
	"strings"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

// placedWriter is a provider, after lineage, that an unsatisfied reservation
// holds a later writer behind, at the reserved transition (D-80, ADR-0195).
type placedWriter struct {
	provider, transition string
}

// validateProviderReservations checks every live reservation strictly (a
// reservation never goes stale: the retirement it would need is refused), then
// the placed-writer invariant.
func validateProviderReservations(v *violations, state *models.State, resolver *pipeline.Resolver) {
	holders := map[placedWriter][]string{}
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if len(task.ProviderReservations) == 0 {
			continue
		}
		if task.Status.IsTerminal() {
			continue // Supersession checks the drop when it happens (ops).
		}
		if err := models.ValidateProviderReservations(task.ProviderReservations); err != nil {
			v.add(fmt.Errorf("task %s %w", task.ID, err))
			continue
		}
		for j, res := range task.ProviderReservations {
			provider := models.EffectiveReservationProvider(state, res.ProviderTask)
			if provider == task.ID {
				v.add(fmt.Errorf("task %s provider_reservations[%d] reserves the task behind itself", task.ID, j))
				continue
			}
			result := models.ResolveReservation(res, state, resolver)
			if !result.Satisfied() && state.FindTask(provider) != nil {
				key := placedWriter{provider: provider, transition: res.Transition}
				holders[key] = append(holders[key], task.ID)
			}
			if result.Invalid() {
				v.addID("provider reservation "+task.ID+" "+res.ProviderTask+" "+res.Transition,
					fmt.Errorf("task %s provider_reservations[%d] names provider %s at %s: %s", task.ID, j, res.ProviderTask, res.Transition, result.Reason))
			}
		}
	}
	validatePlacedWriters(v, state, resolver, holders)
}

// chainedSelection is one typed selection of a placed writer's outputs.
type chainedSelection struct {
	owner   string
	outputs []int
}

// validatePlacedWriters requires every typed selection of a placed writer to
// cover its whole cap, and refuses Kind output on it: a writer selected by
// index must not author a child the selection misses or cannot name.
func validatePlacedWriters(v *violations, state *models.State, resolver *pipeline.Resolver, holders map[placedWriter][]string) {
	if len(holders) == 0 {
		return
	}
	selections := map[placedWriter][]chainedSelection{}
	collect := func(owner string, deps []models.ProviderDependency) {
		for i, dep := range deps {
			key := placedWriter{provider: models.EffectiveReservationProvider(state, dep.ProviderTask), transition: dep.Transition}
			if _, placed := holders[key]; placed {
				selections[key] = append(selections[key], chainedSelection{owner: fmt.Sprintf("%s[%d]", owner, i), outputs: dep.Outputs})
			}
		}
	}
	collectDescendants := func(owner string, deps []models.DescendantDependency) {
		for i, dep := range deps {
			collect(fmt.Sprintf("%s descendant_dependencies[%d].provider_dependencies", owner, i), dep.ProviderDependencies)
		}
	}
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if !task.Status.IsTerminal() {
			collect(task.ID+" provider_dependencies", task.ProviderDependencies)
			collectDescendants(task.ID, task.DescendantDependencies)
		}
		if !providerOutputIsLive(state, resolver, task) {
			continue
		}
		for j, output := range task.Output {
			collect(fmt.Sprintf("%s output[%d].provider_dependencies", task.ID, j), output.ProviderDependencies)
			collectDescendants(fmt.Sprintf("%s output[%d]", task.ID, j), output.DescendantDependencies)
		}
	}

	keys := make([]placedWriter, 0, len(holders))
	for key := range holders {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b placedWriter) int {
		return strings.Compare(a.provider+"\x00"+a.transition, b.provider+"\x00"+b.transition)
	})
	for _, key := range keys {
		writer := state.FindTask(key.provider)
		if writer == nil {
			continue // The holder's reservation is reported invalid.
		}
		heldBy := strings.Join(holders[key], ", ")
		if providerOutputIsLive(state, resolver, writer) {
			for j, output := range writer.Output {
				if output.Kind != "" {
					v.addID("placed writer kind "+writer.ID+" "+output.Kind,
						fmt.Errorf("task %s output[%d] has kind %q, but %s reserve %s's generated children (provider_reservations): a deduplicable output cannot name the child they wait for", writer.ID, j, output.Kind, heldBy, writer.ID))
				}
			}
		}
		for _, selection := range selections[key] {
			if writer.MaxOutputs > 0 && coversCap(selection.outputs, writer.MaxOutputs) {
				continue
			}
			v.addID("placed writer selection "+selection.owner+" "+writer.ID,
				fmt.Errorf("%s selects %s outputs %v at %s, but %s is a placed writer (reserved by %s) and needs max_outputs with a selection covering every output: a partial or uncapped selection lets an unselected writer race the chain", selection.owner, writer.ID, selection.outputs, key.transition, writer.ID, heldBy))
		}
	}
}

func coversCap(outputs []int, cap int) bool {
	if len(outputs) != cap {
		return false
	}
	sorted := slices.Sorted(slices.Values(outputs))
	for i, output := range sorted {
		if output != i {
			return false
		}
	}
	return true
}

// validateOutputCaps refuses live output exceeding its owner's max_outputs.
func validateOutputCaps(v *violations, state *models.State, resolver *pipeline.Resolver) {
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if task.MaxOutputs < 0 {
			v.add(fmt.Errorf("task %s max_outputs %d must be nonnegative", task.ID, task.MaxOutputs))
		}
		if task.MaxOutputs > 0 && len(task.Output) > task.MaxOutputs && (resolver == nil || providerOutputIsLive(state, resolver, task)) {
			v.addID("max_outputs "+task.ID,
				fmt.Errorf("task %s has %d output entries but max_outputs is %d: other writers select it by index, so an extra writer would escape their order", task.ID, len(task.Output), task.MaxOutputs))
		}
	}
}
