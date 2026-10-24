package main

import (
	"fmt"
	"path/filepath"
	"strings"

	lizaerrors "github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/identity"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/roles"
)

// afterRBACAdmissionTestHook is a deterministic test seam for replacing an
// agent generation after CLI authorization but before command dispatch.
var afterRBACAdmissionTestHook func(agentID string)

func runAfterRBACAdmissionTestHook(agentID string) {
	if afterRBACAdmissionTestHook != nil {
		afterRBACAdmissionTestHook(agentID)
	}
}

// loadResolverForRBAC loads a pipeline resolver from the project root using the
// frozen project pipeline config. Returns a fail-closed error on load failure.
func loadResolverForRBAC(projectRoot string) (*pipeline.Resolver, error) {
	cfg, err := pipeline.LoadFrozen(projectRoot)
	if err != nil {
		return nil, &lizaerrors.PipelineConfigError{Operation: "rbac", Err: err}
	}
	return pipeline.NewResolver(cfg), nil
}

// loadResolverFromDir loads a pipeline resolver from a runtime directory path using
// pipeline.Load(filepath.Join(lizaDir, "pipeline.yaml")). Used by commands that
// operate without project root detection (e.g. add-task/add-tasks).
func loadResolverFromDir(lizaDir string) (*pipeline.Resolver, error) {
	cfg, err := pipeline.Load(filepath.Join(lizaDir, "pipeline.yaml"))
	if err != nil {
		return nil, &lizaerrors.PipelineConfigError{Operation: "rbac", Err: err}
	}
	return pipeline.NewResolver(cfg), nil
}

// validateAllowedOperation checks whether the agent identified by agentID is
// permitted to perform the named operation according to the pipeline resolver.
func validateAllowedOperation(resolver *pipeline.Resolver, agentID, operationName string) error {
	role, err := identity.ExtractRole(agentID)
	if err != nil {
		return &lizaerrors.PermissionError{
			Operation: operationName,
			AgentID:   agentID,
			Reason:    fmt.Sprintf("cannot validate operation %q for agent %q", operationName, agentID),
			Err:       err,
		}
	}
	capabilities, err := resolver.EffectiveRoleCapabilities(role)
	if err != nil {
		return &lizaerrors.PermissionError{
			Operation: operationName,
			AgentID:   agentID,
			Role:      role,
			Reason:    fmt.Sprintf("cannot validate operation %q for agent %q", operationName, agentID),
			Err:       err,
		}
	}
	if capabilities.Allows(operationName) {
		runAfterRBACAdmissionTestHook(agentID)
		return nil
	}
	return &lizaerrors.PermissionError{
		Operation: operationName,
		AgentID:   agentID,
		Role:      role,
		Reason:    fmt.Sprintf("operation %q not allowed for role %q (agent %s)", operationName, role, agentID),
	}
}

// validateRoleType checks whether the agent identified by agentID has one of
// the allowed role types according to the pipeline resolver.
func validateRoleType(resolver *pipeline.Resolver, agentID string, allowedTypes ...string) error {
	typesLabel := "[" + strings.Join(allowedTypes, ", ") + "]"

	role, err := identity.ExtractRole(agentID)
	if err != nil {
		return &lizaerrors.PermissionError{
			AgentID: agentID,
			Reason:  fmt.Sprintf("cannot validate role type %s for agent %q", typesLabel, agentID),
			Err:     err,
		}
	}
	actualType, err := resolver.RoleType(role)
	if err != nil {
		return &lizaerrors.PermissionError{
			AgentID: agentID,
			Role:    role,
			Reason:  fmt.Sprintf("cannot validate role type %s for agent %q", typesLabel, agentID),
			Err:     err,
		}
	}
	for _, allowed := range allowedTypes {
		if actualType == roles.RoleType(allowed) {
			runAfterRBACAdmissionTestHook(agentID)
			return nil
		}
	}
	return &lizaerrors.PermissionError{
		AgentID: agentID,
		Role:    role,
		Reason:  fmt.Sprintf("command requires role type %s but agent %q has type %q", typesLabel, agentID, actualType),
	}
}
