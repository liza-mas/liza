package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/liza-mas/liza/internal/identity"
	"github.com/liza-mas/liza/internal/pipeline"
)

// loadResolverForRBAC loads a pipeline resolver from the project root using the
// frozen config (.liza/pipeline.yaml). Returns a fail-closed error on load failure.
func loadResolverForRBAC(projectRoot string) (*pipeline.Resolver, error) {
	cfg, err := pipeline.LoadFrozen(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("cannot authorize operation: failed to load pipeline config: %w", err)
	}
	return pipeline.NewResolver(cfg), nil
}

// loadResolverFromDir loads a pipeline resolver from a .liza directory path using
// pipeline.Load(filepath.Join(lizaDir, "pipeline.yaml")). Used by commands that
// operate without project root detection (e.g. add-task/add-tasks).
func loadResolverFromDir(lizaDir string) (*pipeline.Resolver, error) {
	cfg, err := pipeline.Load(filepath.Join(lizaDir, "pipeline.yaml"))
	if err != nil {
		return nil, fmt.Errorf("cannot authorize operation: failed to load pipeline config: %w", err)
	}
	return pipeline.NewResolver(cfg), nil
}

// validateAllowedOperation checks whether the agent identified by agentID is
// permitted to perform the named operation according to the pipeline resolver.
func validateAllowedOperation(resolver *pipeline.Resolver, agentID, operationName string) error {
	role, err := identity.ExtractRole(agentID)
	if err != nil {
		return fmt.Errorf("cannot validate operation %q for agent %q: %w", operationName, agentID, err)
	}
	ops, err := resolver.AllowedOperations(role)
	if err != nil {
		return fmt.Errorf("cannot validate operation %q for agent %q: %w", operationName, agentID, err)
	}
	for _, op := range ops {
		if op == operationName {
			return nil
		}
	}
	return fmt.Errorf("operation %q not allowed for role %q (agent %s)", operationName, role, agentID)
}

// validateRoleType checks whether the agent identified by agentID has one of
// the allowed role types according to the pipeline resolver.
func validateRoleType(resolver *pipeline.Resolver, agentID string, allowedTypes ...string) error {
	typesLabel := "[" + strings.Join(allowedTypes, ", ") + "]"

	role, err := identity.ExtractRole(agentID)
	if err != nil {
		return fmt.Errorf("cannot validate role type %s for agent %q: %w", typesLabel, agentID, err)
	}
	actualType, err := resolver.RoleType(role)
	if err != nil {
		return fmt.Errorf("cannot validate role type %s for agent %q: %w", typesLabel, agentID, err)
	}
	for _, allowed := range allowedTypes {
		if actualType == allowed {
			return nil
		}
	}
	return fmt.Errorf("command requires role type %s but agent %q has type %q", typesLabel, agentID, actualType)
}
