package statevalidate

import (
	"fmt"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

// DescendantTransition returns the transition at which descendant
// declarations owned by a task of rolePair apply: that role pair's own
// per-subtask transition for task-level declarations, or its generated
// child's for output-level ones. Only a single per-subtask path is supported,
// one intermediate plan deep (ADR-0193), so a declaration reaches exactly the
// writers it names.
func DescendantTransition(resolver *pipeline.Resolver, rolePair string, outputLevel bool) (string, error) {
	transition, err := soleSubtaskTransition(resolver, rolePair)
	if err != nil || !outputLevel {
		return transition.Name, err
	}
	target, err := resolver.TransitionTargetRolePair(transition.Name)
	if err != nil {
		return "", err
	}
	transition, err = soleSubtaskTransition(resolver, target)
	return transition.Name, err
}

func soleSubtaskTransition(resolver *pipeline.Resolver, rolePair string) (pipeline.TransitionDef, error) {
	var found []pipeline.TransitionDef
	for _, transition := range resolver.AllTransitions() {
		source, err := resolver.TransitionSourceRolePair(transition.Name)
		if err == nil && source == rolePair && transition.Cardinality == "per-subtask" && transition.When != "coding-allocation" {
			found = append(found, transition)
		}
	}
	if len(found) != 1 {
		return pipeline.TransitionDef{}, fmt.Errorf("descendant_dependencies need a single per-subtask path: %s has %d outgoing per-subtask transitions", rolePair, len(found))
	}
	return found[0], nil
}

// ValidateDescendantPlacement checks the declarations' shape and that each
// applies at the one transition the topology allows.
func ValidateDescendantPlacement(resolver *pipeline.Resolver, rolePair string, deps []models.DescendantDependency, outputLevel bool) error {
	if len(deps) == 0 {
		return nil
	}
	if err := models.ValidateDescendantDependencies(deps); err != nil {
		return err
	}
	transition, err := DescendantTransition(resolver, rolePair, outputLevel)
	if err != nil {
		return err
	}
	for i, dep := range deps {
		if dep.AtTransition != transition {
			return fmt.Errorf("descendant_dependencies[%d].at_transition %q must be %s, the per-subtask transition its waits apply at", i, dep.AtTransition, transition)
		}
	}
	return nil
}

// DescendantKindConflict names the first output entry that declares, or would
// receive from its owner, descendant waits while carrying a kind: generation
// may skip it for an in-flight incumbent that never had the wait (ADR-0193).
func DescendantKindConflict(owner *models.Task, output []models.OutputEntry) (int, string) {
	for i, entry := range output {
		if entry.Kind != "" && (len(owner.DescendantDependencies) > 0 || len(entry.DescendantDependencies) > 0) {
			return i, fmt.Sprintf("output[%d] has kind %q; descendant waits cannot apply to a deduplicable output", i, entry.Kind)
		}
	}
	return -1, ""
}

// validateDescendantOwner applies placement and the provider-reference rules,
// strictly: a descendant declaration never goes stale (ADR-0193), so the
// retirement it names is refused while it is live.
func validateDescendantOwner(v *violations, state *models.State, resolver *pipeline.Resolver, owner, rolePair string, deps []models.DescendantDependency, outputLevel bool) {
	if len(deps) == 0 {
		return
	}
	if err := ValidateDescendantPlacement(resolver, rolePair, deps, outputLevel); err != nil {
		v.add(fmt.Errorf("task %s: %w", owner, err))
		return
	}
	validateProviderOwner(v, state, resolver, owner, models.DescendantProviderDependencies(deps), nil)
}
