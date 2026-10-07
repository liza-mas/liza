package ops

import (
	"fmt"
	"slices"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

// Output indexes are identities in one reviewed provider, not transferable
// positions in a replacement. Retire live consumer declarations first.
func rejectReferencedProviderRetirement(state *models.State, resolver *pipeline.Resolver, targetID string) error {
	check := func(owner string, deps []models.ProviderDependency) error {
		for _, dep := range deps {
			referenced := targetID == dep.ProviderTask
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
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if task.ID == targetID {
			continue
		}
		if !task.Status.IsTerminal() {
			if err := check(task.ID, task.ProviderDependencies); err != nil {
				return err
			}
		}
		if !task.TransitionsExecuted["replanned"] && !task.PlanHandoffRetired() && operationalOutputMayBeConsumed(state, resolver, task) {
			for index, output := range task.Output {
				if err := check(fmt.Sprintf("%s output[%d]", task.ID, index), output.ProviderDependencies); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
