package ops

import (
	"fmt"
	"slices"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

// providerRetirement says whether retiring a task mints a replan successor.
// Only replan does; every other retirement (supersession, cancellation,
// plan-check replacement) is permanent: the task can never be replanned.
type providerRetirement bool

const (
	retirePermanently providerRetirement = false
	retireByReplan    providerRetirement = true
)

// Output indexes are identities in one reviewed provider, not transferable
// positions in a replacement. Retire live consumer declarations first.
//
// Exceptions (ADR-0185, ADR-0186): an unexpanded plan's output holds neither
// the provider it names directly nor a selected child retired permanently.
// That declaration goes stale; hand-off classification then requires the
// plan's replan, and generation refuses it (staleProviderReference). A child
// retired by replan still holds: lineage would silently resolve the slot to
// the successor, hiding the staleness.
func rejectReferencedProviderRetirement(state *models.State, resolver *pipeline.Resolver, targetID string, retirement providerRetirement) error {
	check := func(owner string, deps []models.ProviderDependency, mayGoStale func() bool) error {
		for _, dep := range deps {
			direct := targetID == dep.ProviderTask
			if !direct {
				_, children, err := models.EffectiveProviderChildren(dep, state, resolver)
				if err != nil || !slices.Contains(children, targetID) {
					continue
				}
			}
			if (direct || retirement == retirePermanently) && mayGoStale() {
				continue
			}
			return &PreconditionError{Reason: fmt.Sprintf("cannot retire provider %s while %s has a live provider_dependencies declaration; replace or retire that consumer declaration first", targetID, owner)}
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

// staleProviderReference names what leaves an unexpanded plan's declaration
// stale: the declared provider when retired, else the first existing selected
// child that is (child true). Without a resolver, or for a selection that does
// not resolve, only the provider is judged; validation reports the rest.
func staleProviderReference(state *models.State, resolver *pipeline.Resolver, dep models.ProviderDependency) (id string, child bool) {
	if provider := state.FindTask(dep.ProviderTask); provider != nil && models.ProviderRetired(provider) {
		return provider.ID, false
	}
	if resolver == nil {
		return "", false
	}
	_, children, err := models.EffectiveProviderChildren(dep, state, resolver)
	if err != nil {
		return "", false
	}
	for _, childID := range children {
		if task := state.FindTask(childID); task != nil && models.ProviderRetired(task) {
			return childID, true
		}
	}
	return "", false
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
