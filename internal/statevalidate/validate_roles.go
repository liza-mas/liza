package statevalidate

import (
	"fmt"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
)

// validateRoleNames checks that no agent uses the legacy underscore-form role
// name (e.g. "code_reviewer"). If detected, reports a violation directing the
// user to run the branded migrate command for normalization.
func validateRoleNames(v *violations, state *models.State) {
	for _, agentID := range sortedAgentIDs(state) {
		agent := state.Agents[agentID]
		if strings.Contains(agent.Role, "_") {
			v.add(fmt.Errorf(
				"agent %s has unmigrated role name %q — run %q to fix",
				agentID, agent.Role, brand.Command("migrate"),
			))
		}
	}
}
