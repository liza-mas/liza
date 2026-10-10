package ops

import (
	"fmt"
	"slices"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/statevalidate"
)

// applyDescendantDependencies returns output with the waits task applies at
// its per-subtask transition merged into every entry (ADR-0193), so the
// reviewed output and the writers generated from it carry them as ordinary
// declarations. A declaration for the same provider and transition gains the
// missing output indexes.
func applyDescendantDependencies(task *models.Task, output []models.OutputEntry) []models.OutputEntry {
	applied := slices.Clone(output)
	for i := range applied {
		applied[i].ProviderDependencies = models.CloneProviderDependencies(applied[i].ProviderDependencies)
	}
	for _, dep := range models.DescendantProviderDependencies(task.DescendantDependencies) {
		for i := range applied {
			entry := &applied[i]
			j := slices.IndexFunc(entry.ProviderDependencies, func(declared models.ProviderDependency) bool {
				return declared.ProviderTask == dep.ProviderTask && declared.Transition == dep.Transition
			})
			if j < 0 {
				entry.ProviderDependencies = append(entry.ProviderDependencies, models.CloneProviderDependencies([]models.ProviderDependency{dep})...)
				continue
			}
			for _, index := range dep.Outputs {
				if !slices.Contains(entry.ProviderDependencies[j].Outputs, index) {
					entry.ProviderDependencies[j].Outputs = append(entry.ProviderDependencies[j].Outputs, index)
				}
			}
		}
	}
	return applied
}

// descendantOutputRefusal refuses output that would lose a descendant wait at
// transition: an owner declaration placed off its one per-subtask path, an
// entry deduplication may skip, or one not carrying every wait its owner
// applies there. Authoring merges the waits; generation and recovery refuse a
// stored manifest that lacks them (ADR-0193). Placement is rechecked here so
// a stored record that escaped validation is refused before any child exists.
func descendantOutputRefusal(resolver *pipeline.Resolver, task *models.Task, output []models.OutputEntry, transition string) error {
	if len(task.DescendantDependencies) > 0 {
		if resolver == nil {
			return &PreconditionError{Reason: fmt.Sprintf("task %s descendant_dependencies cannot be checked without the pipeline", task.ID)}
		}
		if err := statevalidate.ValidateDescendantPlacement(resolver, task.RolePair, task.DescendantDependencies, false); err != nil {
			return &PreconditionError{Reason: fmt.Sprintf("task %s: %v; its waits would not reach the writers it generates", task.ID, err)}
		}
	}
	if index, conflict := statevalidate.DescendantKindConflict(task, output); conflict != "" {
		return &handoffInputError{class: handoffOutputRefusal, index: index, err: fmt.Errorf("task %s %s", task.ID, conflict)}
	}
	applied := models.AppliedDescendantDependencies(task, transition)
	for i, entry := range output {
		if missing := models.MissingProviderDependencies(entry.ProviderDependencies, applied); len(missing) > 0 {
			return &handoffInputError{class: handoffOutputRefusal, index: i, err: fmt.Errorf(
				"task %s output[%d] does not carry its descendant_dependencies wait on %s %s outputs %v; re-author the output with %s",
				task.ID, i, missing[0].ProviderTask, missing[0].Transition, missing[0].Outputs, brand.Command("set-task-output", task.ID))}
		}
	}
	return nil
}

// planningWaitWarnings names each output declaration that holds a generated
// planning task until a provider's non-planning children merge: usually a
// writer-order wait placed one level too high (D-79). It stays expressible,
// so this is a warning.
func planningWaitWarnings(resolver *pipeline.Resolver, task *models.Task, output []models.OutputEntry) []string {
	planning := resolver.TransitionSourcePairs()
	consumers, err := resolver.OutputConsumerRolePairsForOutput(task.RolePair, output)
	if err != nil || !slices.ContainsFunc(consumers, func(rolePair string) bool { return planning[rolePair] }) {
		return nil
	}
	alternative := "declare it under descendant_dependencies, applied to that plan's outputs"
	if transition, err := statevalidate.DescendantTransition(resolver, task.RolePair, true); err == nil {
		alternative = fmt.Sprintf("declare it under descendant_dependencies with at_transition %s", transition)
	}
	var warnings []string
	for i, entry := range output {
		for j, dep := range entry.ProviderDependencies {
			target, err := resolver.TransitionTargetRolePair(dep.Transition)
			if err != nil || planning[target] {
				continue
			}
			warnings = append(warnings, fmt.Sprintf(
				"output[%d].provider_dependencies[%d] holds the generated plan until %s's %s children (%s) merge; if it orders that plan's writers, %s",
				i, j, dep.ProviderTask, dep.Transition, target, alternative))
		}
	}
	return warnings
}
