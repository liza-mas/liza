package pipeline

import (
	"fmt"
	"slices"

	"github.com/liza-mas/liza/internal/models"
)

// Transition returns the transition definition for the given name.
// It searches across all sub-pipelines and pipeline-transitions.
func (r *Resolver) Transition(name string) (*TransitionDef, error) {
	for _, sp := range r.config.Pipeline.SubPipelines {
		for i := range sp.Transitions {
			if sp.Transitions[i].Name == name {
				return &sp.Transitions[i], nil
			}
		}
	}
	for i := range r.config.Pipeline.PipelineTransitions {
		if r.config.Pipeline.PipelineTransitions[i].Name == name {
			return &r.config.Pipeline.PipelineTransitions[i], nil
		}
	}
	return nil, fmt.Errorf("unknown transition %q", name)
}

// ProviderTransition projects configured child identity without importing
// pipeline definitions into the task model's readiness checks.
func (r *Resolver) ProviderTransition(name string) (models.ProviderTransition, error) {
	if r == nil || r.config == nil {
		return models.ProviderTransition{}, fmt.Errorf("provider transition configuration is unavailable")
	}
	td, err := r.Transition(name)
	if err != nil {
		return models.ProviderTransition{}, err
	}
	source, err := r.TransitionSourceRolePair(name)
	if err != nil {
		return models.ProviderTransition{}, err
	}
	target, err := r.TransitionTargetRolePair(name)
	if err != nil {
		return models.ProviderTransition{}, err
	}
	return models.ProviderTransition{
		SourceRolePair: source,
		TargetRolePair: target,
		TaskSlug:       td.TaskSlugOrName(),
		Cardinality:    td.Cardinality,
	}, nil
}

// IsPipelineTransition reports whether name is a top-level pipeline-transition.
func (r *Resolver) IsPipelineTransition(name string) bool {
	for i := range r.config.Pipeline.PipelineTransitions {
		if r.config.Pipeline.PipelineTransitions[i].Name == name {
			return true
		}
	}
	return false
}

// PipelineTransitionsSuppressed reports whether runtime availability should skip
// top-level pipeline-transitions.
func (r *Resolver) PipelineTransitionsSuppressed() bool {
	return r.noFollowUp
}

// AvailableManualTransitions returns manual transition names available for a task at
// the given status, excluding transitions already executed.
func (r *Resolver) AvailableManualTransitions(status models.TaskStatus, transitionsExecuted map[string]bool) []string {
	return r.availableTransitionsByTrigger(models.TransitionTriggerManual, status, transitionsExecuted)
}

// AvailableAutoTransitions returns auto transition names available for a task at
// the given status, excluding transitions already executed.
func (r *Resolver) AvailableAutoTransitions(status models.TaskStatus, transitionsExecuted map[string]bool) []string {
	return r.availableTransitionsByTrigger(models.TransitionTriggerAuto, status, transitionsExecuted)
}

// availableTransitionsByTrigger returns transition names matching the given trigger
// type that are available for a task at the given status, excluding already-executed.
func (r *Resolver) availableTransitionsByTrigger(trigger models.TransitionTrigger, status models.TaskStatus, transitionsExecuted map[string]bool) []string {
	var available []string
	for _, sp := range r.config.Pipeline.SubPipelines {
		for _, t := range sp.Transitions {
			if t.Trigger != trigger {
				continue
			}
			if transitionsExecuted[t.Name] {
				continue
			}
			fromPair, fromPhase, err := parseRef(t.From)
			if err != nil {
				continue
			}
			if r.resolvePhase(fromPair, fromPhase) == status {
				available = append(available, t.Name)
			}
		}
	}
	if !r.noFollowUp {
		for _, t := range r.config.Pipeline.PipelineTransitions {
			if t.Trigger != trigger {
				continue
			}
			if transitionsExecuted[t.Name] {
				continue
			}
			if r.resolve3PartPhase(t.From) == status {
				available = append(available, t.Name)
			}
		}
	}
	slices.Sort(available)
	return available
}

// TransitionTargetRolePair returns the target role-pair name for a transition.
// Handles both 2-part refs (sub-pipeline transitions) and 3-part refs (pipeline-transitions).
func (r *Resolver) TransitionTargetRolePair(transitionName string) (string, error) {
	t, err := r.Transition(transitionName)
	if err != nil {
		return "", err
	}
	// Try 3-part ref first (pipeline-transitions use sub-pipeline.role-pair.phase).
	_, toPair, _, err3 := parse3PartRef(t.To)
	if err3 == nil {
		return toPair, nil
	}
	// Fall back to 2-part ref (sub-pipeline transitions use role-pair.phase).
	toPair2, _, err2 := parseRef(t.To)
	if err2 != nil {
		return "", fmt.Errorf("transition %q: invalid to reference: %w", transitionName, err2)
	}
	return toPair2, nil
}

// OutputConsumerRolePairs returns role-pairs that can consume output[] entries
// from sourceRolePair through an outgoing per-subtask transition.
func (r *Resolver) OutputConsumerRolePairs(sourceRolePair string) ([]string, error) {
	return r.outputConsumerRolePairs(sourceRolePair, nil, false)
}

// OutputConsumerRolePairsForOutput returns only consumers of the explicit
// allocation mode. Static provider transition identities remain unchanged.
func (r *Resolver) OutputConsumerRolePairsForOutput(sourceRolePair string, output []models.OutputEntry) ([]string, error) {
	return r.outputConsumerRolePairs(sourceRolePair, output, true)
}

// TransitionApplies selects an exclusive route by the reviewed output marker.
// A marked manifest in a legacy topology is refused rather than routed to a
// planner that would never adopt its coding acceptance contract.
func (r *Resolver) TransitionApplies(name string, output []models.OutputEntry) bool {
	t, err := r.Transition(name)
	if err != nil {
		return false
	}
	return t.AppliesTo(output)
}

// AppliesTo is TransitionApplies for an already resolved transition, so pure
// evaluators holding TransitionDefs select routes by the same rule.
func (t TransitionDef) AppliesTo(output []models.OutputEntry) bool {
	direct := models.HasCodingAllocation(output)
	switch t.When {
	case models.AllocationRouteCodingAllocation:
		return direct
	case models.AllocationRouteScopeDecomposition, models.AllocationRouteNone:
		return !direct
	default:
		return false
	}
}

func (r *Resolver) outputConsumerRolePairs(sourceRolePair string, output []models.OutputEntry, selected bool) ([]string, error) {
	if _, ok := r.config.Pipeline.RolePairs[sourceRolePair]; !ok {
		return nil, fmt.Errorf("unknown role-pair %q", sourceRolePair)
	}
	var consumers []string
	for _, t := range r.AllTransitions() {
		if t.Cardinality != models.CardinalityPerSubtask {
			continue
		}
		if selected && !r.TransitionApplies(t.Name, output) {
			continue
		}
		fromPair, err := transitionFromRolePair(t)
		if err != nil || fromPair != sourceRolePair {
			continue
		}
		toPair, err := transitionToRolePair(t)
		if err != nil {
			return nil, fmt.Errorf("transition %q: invalid to reference: %w", t.Name, err)
		}
		if !slices.Contains(consumers, toPair) {
			consumers = append(consumers, toPair)
		}
	}
	slices.Sort(consumers)
	return consumers, nil
}

// DownstreamRolePairs returns every role-pair reachable from sourceRolePair by
// following configured pipeline transitions. The source role-pair itself is not
// included, even if a cycle exists.
func (r *Resolver) DownstreamRolePairs(sourceRolePair string) (map[string]bool, error) {
	if _, ok := r.config.Pipeline.RolePairs[sourceRolePair]; !ok {
		return nil, fmt.Errorf("unknown role-pair %q", sourceRolePair)
	}
	if r.downstreamRolePairs == nil {
		if err := r.buildDownstreamRolePairCache(); err != nil {
			return nil, err
		}
	}
	result := make(map[string]bool, len(r.downstreamRolePairs[sourceRolePair]))
	for rolePair := range r.downstreamRolePairs[sourceRolePair] {
		result[rolePair] = true
	}
	return result, nil
}

// IsRolePairDownstream reports whether dependencyRolePair is downstream of
// sourceRolePair in the configured transition graph.
func (r *Resolver) IsRolePairDownstream(sourceRolePair, dependencyRolePair string) (bool, error) {
	downstream, err := r.DownstreamRolePairs(sourceRolePair)
	if err != nil {
		return false, err
	}
	if _, ok := r.config.Pipeline.RolePairs[dependencyRolePair]; !ok {
		return false, fmt.Errorf("unknown role-pair %q", dependencyRolePair)
	}
	return downstream[dependencyRolePair], nil
}

func (r *Resolver) buildDownstreamRolePairCache() error {
	adjacency := make(map[string][]string)
	for rolePair := range r.config.Pipeline.RolePairs {
		adjacency[rolePair] = nil
	}
	for _, t := range r.AllTransitions() {
		fromPair, err := transitionFromRolePair(t)
		if err != nil {
			return fmt.Errorf("transition %q: invalid from reference: %w", t.Name, err)
		}
		toPair, err := transitionToRolePair(t)
		if err != nil {
			return fmt.Errorf("transition %q: invalid to reference: %w", t.Name, err)
		}
		if !slices.Contains(adjacency[fromPair], toPair) {
			adjacency[fromPair] = append(adjacency[fromPair], toPair)
		}
	}

	r.downstreamRolePairs = make(map[string]map[string]bool, len(adjacency))
	for source := range adjacency {
		seen := make(map[string]bool)
		var visit func(string)
		visit = func(current string) {
			for _, next := range adjacency[current] {
				if next == source || seen[next] {
					continue
				}
				seen[next] = true
				visit(next)
			}
		}
		visit(source)
		r.downstreamRolePairs[source] = seen
	}
	return nil
}

func transitionFromRolePair(t TransitionDef) (string, error) {
	if _, rolePair, _, err := parse3PartRef(t.From); err == nil {
		return rolePair, nil
	}
	rolePair, _, err := parseRef(t.From)
	return rolePair, err
}

func transitionToRolePair(t TransitionDef) (string, error) {
	if _, rolePair, _, err := parse3PartRef(t.To); err == nil {
		return rolePair, nil
	}
	rolePair, _, err := parseRef(t.To)
	return rolePair, err
}

// AllTransitions returns all transition definitions across all sub-pipelines
// and pipeline-transitions. Used for cardinality-based filtering (e.g., finding
// all many-to-one transitions for cohort detection).
func (r *Resolver) AllTransitions() []TransitionDef {
	var all []TransitionDef
	for _, sp := range r.config.Pipeline.SubPipelines {
		all = append(all, sp.Transitions...)
	}
	all = append(all, r.config.Pipeline.PipelineTransitions...)
	return all
}

// TransitionSourceRolePair returns the source role-pair name for a transition.
// Handles both 2-part refs (sub-pipeline transitions) and 3-part refs (pipeline-transitions).
// Mirrors TransitionTargetRolePair for the "from" side.
func (r *Resolver) TransitionSourceRolePair(name string) (string, error) {
	t, err := r.Transition(name)
	if err != nil {
		return "", err
	}
	// Try 3-part ref first (pipeline-transitions).
	_, fromPair, _, err3 := parse3PartRef(t.From)
	if err3 == nil {
		return fromPair, nil
	}
	// Fall back to 2-part ref (sub-pipeline transitions).
	fromPair2, _, err2 := parseRef(t.From)
	if err2 != nil {
		return "", fmt.Errorf("transition %q: invalid from reference: %w", name, err2)
	}
	return fromPair2, nil
}

// IsTransitionSourcePair checks if a role-pair is the "from" side of any
// transition (sub-pipeline or pipeline-transitions). Such role-pairs produce
// output that feeds the next pipeline stage (e.g. planning → coding).
func (r *Resolver) IsTransitionSourcePair(rolePair string) bool {
	return r.TransitionSourcePairs()[rolePair]
}

// TransitionSourcePairs returns the set of role-pair names that are
// the "from" side of any transition. The result is cached after the first call.
func (r *Resolver) TransitionSourcePairs() map[string]bool {
	if r.transitionSources != nil {
		return r.transitionSources
	}
	sources := make(map[string]bool)
	for _, sp := range r.config.Pipeline.SubPipelines {
		for _, t := range sp.Transitions {
			fromPair, _, _ := parseRef(t.From)
			sources[fromPair] = true
		}
	}
	for _, t := range r.config.Pipeline.PipelineTransitions {
		_, fromPair, _, err := parse3PartRef(t.From)
		if err == nil {
			sources[fromPair] = true
		}
	}
	r.transitionSources = sources
	return sources
}
