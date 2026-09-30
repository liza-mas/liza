package agent

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/rolemodels"
)

// RoleTypeSource exposes the pipeline roles models.yaml is validated against.
type RoleTypeSource interface {
	AllRoleNames() []string
	RoleType(role string) (string, error)
}

// RoleTypesOf maps each pipeline role to its type.
func RoleTypesOf(src RoleTypeSource) map[string]string {
	types := make(map[string]string)
	for _, role := range src.AllRoleNames() {
		if roleType, err := src.RoleType(role); err == nil {
			types[role] = roleType
		}
	}
	return types
}

// LoadValidatedRoleModels loads models.yaml and validates it against the
// pipeline's roles and the configured tools.
func LoadValidatedRoleModels(projectRoot string, roleTypes map[string]string, config models.Config) (rolemodels.File, error) {
	f, err := rolemodels.Load(projectRoot)
	if err != nil {
		return rolemodels.File{}, err
	}
	if err := ValidateRoleModels(f, roleTypes, config); err != nil {
		return rolemodels.File{}, fmt.Errorf("%s: %w", paths.New(projectRoot).ModelsPath(), err)
	}
	return f, nil
}

// ValidateRoleModels refuses entries naming an unknown role or CLI, models
// set for a CLI that cannot take one, and lists outside reviewer roles.
func ValidateRoleModels(f rolemodels.File, roleTypes map[string]string, config models.Config) error {
	check := func(where, roleType string, s rolemodels.Selection) error {
		if s.List && roleType != "reviewer" {
			return fmt.Errorf("%s: only reviewer roles take an entry list", where)
		}
		for i, e := range s.Items {
			at := where
			if s.List {
				at = fmt.Sprintf("%s[%d]", where, i)
			}
			if e.CLI == "" {
				return fmt.Errorf("%s: cli is required", at)
			}
			if !IsValidCLI(e.CLI, config) {
				return fmt.Errorf("%s: unknown CLI %q (must be %s)", at, e.CLI, strings.Join(AvailableCLIs(config), ", "))
			}
			if e.Model != "" && !SupportsModelSelection(AgentToolRegistry(config)[e.CLI]) {
				return fmt.Errorf("%s: %s does not support model selection", at, e.CLI)
			}
		}
		return nil
	}
	if s := f.Defaults.Doer; s != nil {
		if err := check("defaults.doer", "doer", *s); err != nil {
			return err
		}
	}
	if s := f.Defaults.Reviewer; s != nil {
		if err := check("defaults.reviewer", "reviewer", *s); err != nil {
			return err
		}
	}
	roleNames := slices.Sorted(maps.Keys(roleTypes))
	for _, name := range slices.Sorted(maps.Keys(f.Roles)) {
		roleType, ok := roleTypes[name]
		if !ok {
			return fmt.Errorf("roles.%s: unknown role (valid: %s)", name, strings.Join(roleNames, ", "))
		}
		if err := check("roles."+name, roleType, f.Roles[name]); err != nil {
			return err
		}
	}
	return nil
}
