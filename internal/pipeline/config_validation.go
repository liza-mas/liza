package pipeline

import "fmt"

// validateRoleDefs checks the optional roles section.
func validateRoleDefs(p *Pipeline) error {
	for name, role := range p.Roles {
		if role.Type == "" {
			return fmt.Errorf("role %q: type field is required", name)
		}
		if !validRoleTypes[role.Type] {
			return fmt.Errorf("role %q: invalid type %q (must be doer, reviewer, or orchestrator)", name, role.Type)
		}
	}
	return nil
}

// validateRolePairDefs checks each role-pair's slug, doer and reviewer roles,
// review policy and quorum states.
func validateRolePairDefs(p *Pipeline) error {
	for name, rp := range p.RolePairs {
		if rp.TaskSlug != "" && !kebabCaseRe.MatchString(rp.TaskSlug) {
			return fmt.Errorf("role-pair %q: task-slug %q must be lowercase kebab-case (e.g. %q)", name, rp.TaskSlug, "code-planning")
		}
		doer, ok := p.Roles[rp.Doer]
		if !ok {
			return fmt.Errorf("role-pair %q: doer %q not found in roles", name, rp.Doer)
		}
		if doer.Type != "doer" {
			return fmt.Errorf("role-pair %q: doer role %q has type %q, expected %q", name, rp.Doer, doer.Type, "doer")
		}
		reviewer, ok := p.Roles[rp.Reviewer]
		if !ok {
			return fmt.Errorf("role-pair %q: reviewer %q not found in roles", name, rp.Reviewer)
		}
		if reviewer.Type != "reviewer" {
			return fmt.Errorf("role-pair %q: reviewer role %q has type %q, expected %q", name, rp.Reviewer, reviewer.Type, "reviewer")
		}
		if err := validateReviewPolicy(name, rp.ReviewPolicy); err != nil {
			return err
		}
		if err := validateQuorumStates(name, rp); err != nil {
			return err
		}
	}
	return nil
}

// validateRolePairStates checks that required states are declared and that no
// state name is shared between role-pairs.
func validateRolePairStates(p *Pipeline) error {
	stateOwner := make(map[string]string) // state name → role-pair name
	for name, rp := range p.RolePairs {
		states := []struct {
			phase    string
			value    string
			required bool
		}{
			{"initial", rp.States.Initial, true},
			{"executing", rp.States.Executing, true},
			{"submitted", rp.States.Submitted, true},
			{"reviewing", rp.States.Reviewing, true},
			{"approved", rp.States.Approved, true},
			{"rejected", rp.States.Rejected, true},
			{"partially-approved", rp.States.PartiallyApproved, false},
			{"reviewing-2", rp.States.Reviewing2, false},
			{"clean", rp.States.Clean, false},
		}
		for _, s := range states {
			if s.required && s.value == "" {
				return fmt.Errorf("role-pair %q: %s state is empty", name, s.phase)
			}
			if s.value == "" {
				continue // optional state not declared
			}
			if owner, exists := stateOwner[s.value]; exists {
				return fmt.Errorf("duplicate state name %q: used by role-pairs %q and %q", s.value, owner, name)
			}
			stateOwner[s.value] = name
		}
	}
	return nil
}

// validateSubPipelines checks each sub-pipeline's steps and transitions,
// recording transition names in transitionOwner (name → sub-pipeline name).
func validateSubPipelines(p *Pipeline, transitionOwner map[string]string) error {
	for spName, sp := range p.SubPipelines {
		if len(sp.Steps) == 0 {
			return fmt.Errorf("sub-pipeline %q: must have at least one step", spName)
		}
		for _, step := range sp.Steps {
			if _, ok := p.RolePairs[step]; !ok {
				return fmt.Errorf("sub-pipeline %q: step %q not found in role-pairs", spName, step)
			}
		}

		for _, t := range sp.Transitions {
			if err := validateTransition(t, p, sp.Steps); err != nil {
				return fmt.Errorf("sub-pipeline %q transition %q: %w", spName, t.Name, err)
			}
			if owner, exists := transitionOwner[t.Name]; exists {
				return fmt.Errorf("duplicate transition name %q: used by sub-pipelines %q and %q", t.Name, owner, spName)
			}
			transitionOwner[t.Name] = spName
		}
	}
	return nil
}

// validatePipelineTransitions checks the top-level pipeline-transitions against
// the names already recorded in transitionOwner by validateSubPipelines.
func validatePipelineTransitions(p *Pipeline, transitionOwner map[string]string) error {
	for _, t := range p.PipelineTransitions {
		if err := validatePipelineTransition(t, p); err != nil {
			return fmt.Errorf("pipeline-transition %q: %w", t.Name, err)
		}
		if owner, exists := transitionOwner[t.Name]; exists {
			return fmt.Errorf("duplicate transition name %q: used by %q and pipeline-transitions", t.Name, owner)
		}
		transitionOwner[t.Name] = "pipeline-transitions"
	}
	return nil
}

// validateEntryPoints checks every declared entry-point.
func validateEntryPoints(p *Pipeline) error {
	for epName, epValue := range p.EntryPoints {
		if err := validateEntryPoint(epName, epValue, p); err != nil {
			return err
		}
	}
	return nil
}
