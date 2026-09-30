package agent

import (
	"fmt"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/rolemodels"
)

// ValidationExecutionLocal is the only validation_execution value under which
// declared validation prerequisites can be checked (ADR-0136).
const ValidationExecutionLocal = "local"

// RoleCLI is the CLI a role launches with when started without --cli,
// --profile or --model.
type RoleCLI struct {
	Role string
	CLI  string
}

// RoleTyper resolves a pipeline role's type.
type RoleTyper interface {
	RoleType(name string) (string, error)
}

// DefaultRoleCLIs resolves each role's default launch CLI through
// ResolveLaunchSelection: models.yaml, then the configured CLI chain. A
// reviewer list contributes every item's CLI, since pool repair starts each
// item for its review slot. Explicit agent-start flags bypass this; only
// claim/launch preflight can check those.
func DefaultRoleCLIs(roles []string, types RoleTyper, config models.Config, roleModels rolemodels.File) ([]RoleCLI, error) {
	out := make([]RoleCLI, 0, len(roles))
	for _, role := range roles {
		roleType, err := types.RoleType(role)
		if err != nil {
			return nil, fmt.Errorf("role %s: %w", role, err)
		}
		sel, err := ResolveLaunchSelection(LaunchSelectionRequest{Role: role, RoleType: roleType, Config: config, RoleModels: roleModels})
		if err != nil {
			return nil, fmt.Errorf("role %s: %w", role, err)
		}
		if sel.CLI == "" {
			return nil, fmt.Errorf("role %s: no CLI resolved", role)
		}
		out = append(out, RoleCLI{Role: role, CLI: sel.CLI})
		if slots, bound := roleModels.ReviewSlots(role); bound && roleType == "reviewer" {
			for _, item := range slots.Items[1:] {
				out = append(out, RoleCLI{Role: role, CLI: item.CLI})
			}
		}
	}
	return out, nil
}

// AllValidationLocal reports whether every resolved CLI declares
// validation_execution: local in state config, the value validation preflight
// enforces. An empty list is not local.
func AllValidationLocal(roleCLIs []RoleCLI, config models.Config) bool {
	if len(roleCLIs) == 0 {
		return false
	}
	for _, rc := range roleCLIs {
		if config.AgentTools[rc.CLI].ValidationExecution != ValidationExecutionLocal {
			return false
		}
	}
	return true
}
