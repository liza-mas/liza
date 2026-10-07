package ops

import (
	"fmt"
	"slices"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

// Output indexes are identities in one reviewed provider, not transferable
// positions in a replacement. Retire live consumer declarations first.
//
// One exception (ADR-0185): an unexpanded plan's output naming the provider
// directly does not hold it. That declaration goes stale; hand-off
// classification then requires the plan's replan, and generation refuses it.
// A selected child slot still holds: replan lineage would silently follow it.
func rejectReferencedProviderRetirement(state *models.State, resolver *pipeline.Resolver, targetID string) error {
	check := func(owner string, deps []models.ProviderDependency, mayGoStale func() bool) error {
		for _, dep := range deps {
			referenced := targetID == dep.ProviderTask && !mayGoStale()
			if !referenced {
				_, children, err := models.EffectiveProviderChildren(dep, state, resolver)
				referenced = err == nil && slices.Contains(children, targetID)
			}
			if referenced {
				return &PreconditionError{Reason: fmt.Sprintf("cannot retire provider %s while %s has a live provider_dependencies declaration; replace or retire that consumer declaration first", targetID, owner)}
			}
		}
		return nil
	}
	never := func() bool { return false }
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if task.ID == targetID {
			continue
		}
		if !task.Status.IsTerminal() {
			if err := check(task.ID, task.ProviderDependencies, never); err != nil {
				return err
			}
		}
		if !task.TransitionsExecuted["replanned"] && !task.PlanHandoffRetired() && operationalOutputMayBeConsumed(state, resolver, task) {
			unexpanded := func() bool { return models.PlanUnexpanded(state, task) }
			for index, output := range task.Output {
				if err := check(fmt.Sprintf("%s output[%d]", task.ID, index), output.ProviderDependencies, unexpanded); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// staleProviderConsumerWarnings names each unexpanded plan output that
// declares providerID directly: what retiring it leaves to be re-authored.
func staleProviderConsumerWarnings(state *models.State, providerID string) []string {
	var warnings []string
	for i := range state.Tasks {
		task := &state.Tasks[i]
		for index, output := range task.Output {
			if slices.ContainsFunc(output.ProviderDependencies, func(dep models.ProviderDependency) bool { return dep.ProviderTask == providerID }) &&
				models.PlanUnexpanded(state, task) {
				warnings = append(warnings, fmt.Sprintf("task %s output[%d] declares replanned provider %s; replan %s to re-author it", task.ID, index, providerID, task.ID))
			}
		}
	}
	return warnings
}
