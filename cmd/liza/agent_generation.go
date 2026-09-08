package main

import (
	"fmt"
	"os"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/spf13/cobra"
)

// requireAgentAuthority resolves the stable CLI identity together with the
// caller-held registration generation. Command families adopt this resolver as
// their authoritative mutations are generation-fenced.
func requireAgentAuthority(cmd *cobra.Command) (models.AgentAuthority, error) {
	agentID, err := requireAgentID(cmd)
	if err != nil {
		return models.AgentAuthority{}, err
	}
	return requireAgentAuthorityForID(cmd, agentID)
}

func requireAgentAuthorityForID(cmd *cobra.Command, agentID string) (models.AgentAuthority, error) {
	lookup := brand.LookupEnv(os.Getenv, agentGenerationEnvSuffix)
	if lookup.Warning != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s\n", lookup.Warning)
	}
	if lookup.Value == "" {
		return models.AgentAuthority{}, cliValidationError(fmt.Sprintf(
			"agent generation required (set %s environment variable; legacy %s alias is also accepted)",
			brand.EnvName("AGENT_GENERATION"),
			brand.LegacyEnvName("AGENT_GENERATION"),
		))
	}
	return models.AgentAuthority{ID: agentID, Generation: lookup.Value}, nil
}
