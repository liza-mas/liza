package agent

import (
	"github.com/liza-mas/liza/internal/agentlaunch"
	"github.com/liza-mas/liza/internal/models"
)

// NewLLMAgentForCLI creates the provider backend for a configured CLI name.
func NewLLMAgentForCLI(cliName string, outputsDir string) LLMAgent {
	return NewLLMAgentForCLIWithConfig(cliName, outputsDir, models.Config{})
}

// NewLLMAgentForCLIWithConfig creates the provider backend for a configured CLI name.
func NewLLMAgentForCLIWithConfig(cliName string, outputsDir string, config models.Config) LLMAgent {
	tool, ok := agentlaunch.AgentToolRegistry(config)[cliName]
	if ok && tool.Backend == agentlaunch.ToolBackendACPX {
		return NewACPXAgent(outputsDir)
	}
	return NewCLIAgent(outputsDir)
}
