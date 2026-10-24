package pipeline

import (
	"fmt"
	"slices"
	"time"
)

// ResolvedTimeouts holds parsed timeout durations for a role.
type ResolvedTimeouts struct {
	Execution    time.Duration
	PollInterval time.Duration
	MaxWait      time.Duration
}

// RoleType returns the type (doer, reviewer, orchestrator) for the named role.
func (r *Resolver) RoleType(name string) (string, error) {
	role, ok := r.config.Pipeline.Roles[name]
	if !ok {
		return "", fmt.Errorf("unknown role %q", name)
	}
	return role.Type, nil
}

// DoerRoleNames returns the sorted names of all roles with type "doer".
func (r *Resolver) DoerRoleNames() []string {
	return r.roleNamesByType("doer")
}

// ReviewerRoleNames returns the sorted names of all roles with type "reviewer".
func (r *Resolver) ReviewerRoleNames() []string {
	return r.roleNamesByType("reviewer")
}

// AllRoleNames returns the sorted names of all declared roles.
func (r *Resolver) AllRoleNames() []string {
	names := make([]string, 0, len(r.config.Pipeline.Roles))
	for name := range r.config.Pipeline.Roles {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// EffectiveRoleCapabilities returns the named role's declarative type and a
// cloned operation set. The projection never infers authority from role type.
func (r *Resolver) EffectiveRoleCapabilities(name string) (EffectiveRoleCapabilities, error) {
	role, ok := r.config.Pipeline.Roles[name]
	if !ok {
		return EffectiveRoleCapabilities{}, fmt.Errorf("unknown role %q", name)
	}
	return EffectiveRoleCapabilities{
		RoleType:          role.Type,
		AllowedOperations: slices.Clone(role.AllowedOperations),
	}, nil
}

// AllowedOperations returns a cloned allowed-operations list for the named role.
func (r *Resolver) AllowedOperations(name string) ([]string, error) {
	capabilities, err := r.EffectiveRoleCapabilities(name)
	if err != nil {
		return nil, err
	}
	return capabilities.AllowedOperations, nil
}

// RoleTimeouts returns the parsed timeout durations for the named role.
func (r *Resolver) RoleTimeouts(name string) (*ResolvedTimeouts, error) {
	role, ok := r.config.Pipeline.Roles[name]
	if !ok {
		return nil, fmt.Errorf("unknown role %q", name)
	}
	if role.Timeouts == nil {
		return nil, fmt.Errorf("role %q has no timeouts defined", name)
	}
	exec, err := time.ParseDuration(role.Timeouts.Execution)
	if err != nil {
		return nil, fmt.Errorf("role %q: parsing execution timeout: %w", name, err)
	}
	poll, err := time.ParseDuration(role.Timeouts.PollInterval)
	if err != nil {
		return nil, fmt.Errorf("role %q: parsing poll-interval: %w", name, err)
	}
	maxWait, err := time.ParseDuration(role.Timeouts.MaxWait)
	if err != nil {
		return nil, fmt.Errorf("role %q: parsing max-wait: %w", name, err)
	}
	return &ResolvedTimeouts{
		Execution:    exec,
		PollInterval: poll,
		MaxWait:      maxWait,
	}, nil
}

// MaxInstances returns the max-instances for the named role.
// Orchestrator roles always return 1 regardless of YAML value (spec invariant).
// Returns 0 (unlimited) if the field is unset for non-orchestrator roles.
func (r *Resolver) MaxInstances(name string) (int, error) {
	role, ok := r.config.Pipeline.Roles[name]
	if !ok {
		return 0, fmt.Errorf("unknown role %q", name)
	}
	if role.Type == "orchestrator" {
		return 1, nil
	}
	return role.MaxInstances, nil
}

// RoleDisplayName returns the display-name for the named role.
// Returns the role key itself if the role is not found or has no display-name.
func (r *Resolver) RoleDisplayName(name string) string {
	role, ok := r.config.Pipeline.Roles[name]
	if !ok || role.DisplayName == "" {
		return name
	}
	return role.DisplayName
}

// roleNamesByType returns the sorted names of all roles matching the given type.
func (r *Resolver) roleNamesByType(roleType string) []string {
	var names []string
	for name, role := range r.config.Pipeline.Roles {
		if role.Type == roleType {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// ContextSections returns the effective context-sections list for the named
// role, including capability-checked compatibility sections.
func (r *Resolver) ContextSections(name string) ([]string, error) {
	role, ok := r.config.Pipeline.Roles[name]
	if !ok {
		return nil, fmt.Errorf("unknown role %q", name)
	}
	capabilities, err := r.EffectiveRoleCapabilities(name)
	if err != nil {
		return nil, err
	}

	sections := slices.Clone(role.ContextSections)
	var recoveryOperation string
	switch capabilities.RoleType {
	case "doer":
		recoveryOperation = "mark-blocked"
	case "reviewer":
		recoveryOperation = "submit-verdict"
	default:
		return sections, nil
	}
	if !capabilities.Allows(recoveryOperation) {
		return nil, fmt.Errorf(
			"role %q requires allowed operation %q for context section %q",
			name, recoveryOperation, "cli-failure-recovery",
		)
	}
	if !slices.Contains(sections, "cli-failure-recovery") {
		sections = append(sections, "cli-failure-recovery")
	}
	return sections, nil
}

// Skills returns the skills list for the named role.
func (r *Resolver) Skills(name string) ([]string, error) {
	role, ok := r.config.Pipeline.Roles[name]
	if !ok {
		return nil, fmt.Errorf("unknown role %q", name)
	}
	return role.Skills, nil
}

// MandatoryDocs returns the mandatory-docs list for the named role.
func (r *Resolver) MandatoryDocs(name string) ([]string, error) {
	role, ok := r.config.Pipeline.Roles[name]
	if !ok {
		return nil, fmt.Errorf("unknown role %q", name)
	}
	return role.MandatoryDocs, nil
}

// RolePairNames returns the sorted names of all role-pairs in the config.
func (r *Resolver) RolePairNames() []string {
	names := make([]string, 0, len(r.config.Pipeline.RolePairs))
	for name := range r.config.Pipeline.RolePairs {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// RolePair returns the definition for the named role-pair.
func (r *Resolver) RolePair(name string) (*RolePairDef, error) {
	rp, ok := r.config.Pipeline.RolePairs[name]
	if !ok {
		return nil, fmt.Errorf("unknown role-pair %q", name)
	}
	return &rp, nil
}

// DoerRole returns the doer agent-role key for the given role-pair.
func (r *Resolver) DoerRole(rolePair string) (string, error) {
	rp, ok := r.config.Pipeline.RolePairs[rolePair]
	if !ok {
		return "", fmt.Errorf("unknown role-pair %q", rolePair)
	}
	return rp.Doer, nil
}

// ReviewerRole returns the reviewer agent-role key for the given role-pair.
func (r *Resolver) ReviewerRole(rolePair string) (string, error) {
	rp, ok := r.config.Pipeline.RolePairs[rolePair]
	if !ok {
		return "", fmt.Errorf("unknown role-pair %q", rolePair)
	}
	return rp.Reviewer, nil
}
