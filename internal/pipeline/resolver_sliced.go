package pipeline

import (
	"fmt"
	"slices"
)

// SlicedIntegrationCapability reports whether the configured pipeline has the
// complete slice and global integration topology required for sliced coverage.
func (r *Resolver) SlicedIntegrationCapability() (SlicedIntegrationCapability, error) {
	capability := SlicedIntegrationCapability{}
	planningTransitions, err := r.preIntegrationPlanningTransitions()
	if err != nil {
		return capability, err
	}
	capability.PreIntegrationPlanningTransitions = planningTransitions
	capability.PreIntegrationFollowUpTransitions = make(map[string]bool)
	for _, transition := range r.config.Pipeline.PipelineTransitions {
		capability.PreIntegrationFollowUpTransitions[transition.Name] = true
	}
	if _, ok := r.config.Pipeline.RolePairs["code-planning-pair"]; ok {
		rootRolePair, _, err := r.DecompositionRootForTarget("code-planning-pair")
		if err != nil {
			return SlicedIntegrationCapability{}, fmt.Errorf("resolve pre-integration decomposition root: %w", err)
		}
		capability.PreIntegrationDecompositionRoot = rootRolePair
	}

	slicePair, sliceOK := r.config.Pipeline.RolePairs["slice-integration-pair"]
	globalPair, globalOK := r.config.Pipeline.RolePairs["integration-pair"]
	integration, integrationOK := r.config.Pipeline.SubPipelines["integration-subpipeline"]
	if sliceOK && globalOK && integrationOK &&
		slicePair.Doer == "integration-analyst" && slicePair.Reviewer == "integration-reviewer" &&
		globalPair.Doer == "integration-analyst" && globalPair.Reviewer == "integration-reviewer" &&
		slicePair.States.Clean != "" && globalPair.States.Clean != "" &&
		slices.Contains(integration.Steps, "slice-integration-pair") &&
		slices.Contains(integration.Steps, "integration-pair") &&
		slices.Contains(integration.Steps, "coding-pair") &&
		hasIntegrationFindingTransition(integration.Transitions, "slice-integration-to-fix", "slice-integration-pair.approved") &&
		hasIntegrationFindingTransition(integration.Transitions, "integration-to-fix", "integration-pair.approved") {
		capability.Available = true
		return capability, nil
	}

	capability.Code = SlicedIntegrationUpgradeRequired
	capability.Guidance = "Sliced integration requires a fresh workspace or a manual frozen pipeline topology update."
	return capability, nil
}

// preIntegrationPlanningTransitions includes every upstream producer, not only
// the immediate code-planning decomposition root. Integration analyses create
// repair work and are outside the initial contributing cohort.
func (r *Resolver) preIntegrationPlanningTransitions() (map[string][]TransitionDef, error) {
	result := make(map[string][]TransitionDef)
	for _, transition := range r.AllTransitions() {
		from, err := transitionFromRolePair(transition)
		if err != nil {
			return nil, fmt.Errorf("integration planning transition %q: %w", transition.Name, err)
		}
		if from == "coding-pair" || from == "integration-pair" || from == "slice-integration-pair" {
			continue
		}
		to, err := transitionToRolePair(transition)
		if err != nil {
			return nil, fmt.Errorf("integration planning transition %q: %w", transition.Name, err)
		}
		downstream, err := r.DownstreamRolePairs(to)
		if err != nil {
			return nil, err
		}
		if to == "coding-pair" || downstream["coding-pair"] {
			result[from] = append(result[from], transition)
		}
	}
	return result, nil
}

func hasIntegrationFindingTransition(transitions []TransitionDef, name, from string) bool {
	return slices.ContainsFunc(transitions, func(transition TransitionDef) bool {
		return transition.Name == name &&
			transition.From == from &&
			transition.To == "coding-pair.initial" &&
			transition.Trigger == "auto" &&
			transition.Cardinality == "per-subtask"
	})
}
