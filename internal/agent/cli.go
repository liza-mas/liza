package agent

import (
	"fmt"
	"os"
	"os/exec"
	"slices"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
)

// ValidCLIs returns the supported CLI backends. Returns a fresh copy to prevent mutation.
func ValidCLIs() []string {
	return AvailableCLIs(models.Config{})
}

// CLIExecutableName returns the local executable used for a configured CLI name.
func CLIExecutableName(cliName string) string {
	switch cliName {
	case "codex-acp":
		return "codex"
	case "cursor-acp":
		return "cursor-agent"
	case "opencode-acp":
		return "opencode"
	case "mistral":
		return "vibe"
	default:
		return cliName
	}
}

// InteractiveCLICommand returns the argv used to start an interactive CLI session.
func InteractiveCLICommand(cliName string) []string {
	return []string{CLIExecutableName(cliName)}
}

// NewLLMAgentForCLI creates the provider backend for a configured CLI name.
func NewLLMAgentForCLI(cliName string, outputsDir string) LLMAgent {
	return NewLLMAgentForCLIWithConfig(cliName, outputsDir, models.Config{})
}

// NewLLMAgentForCLIWithConfig creates the provider backend for a configured CLI name.
func NewLLMAgentForCLIWithConfig(cliName string, outputsDir string, config models.Config) LLMAgent {
	tool, ok := AgentToolRegistry(config)[cliName]
	if ok && tool.Backend == ToolBackendACPX {
		return NewACPXAgent(outputsDir)
	}
	return NewCLIAgent(outputsDir)
}

// CheckCLIPrerequisites validates external binaries required by selected backends.
func CheckCLIPrerequisites(cliName string) error {
	return CheckCLIPrerequisitesWithConfig(cliName, models.Config{})
}

// CheckCLIPrerequisitesWithConfig validates external binaries required by configured backends.
func CheckCLIPrerequisitesWithConfig(cliName string, config models.Config) error {
	tool, ok := AgentToolRegistry(config)[cliName]
	if !ok {
		return fmt.Errorf("unknown CLI: %s", cliName)
	}
	required := tool.RequiredExecutables
	if len(required) == 0 && tool.Backend == ToolBackendACPX {
		required = []string{"acpx"}
	}
	for _, executable := range required {
		if _, err := exec.LookPath(executable); err != nil {
			if executable == "acpx" && isACPXCLI(cliName) {
				return fmt.Errorf("%s requires acpx on PATH; install with: npm install -g acpx: %w", cliName, err)
			}
			return fmt.Errorf("%s requires %s on PATH: %w", cliName, executable, err)
		}
	}
	return nil
}

func isACPXCLI(cliName string) bool {
	tool, ok := AgentToolRegistry(models.Config{})[cliName]
	return ok && tool.Backend == ToolBackendACPX
}

func IsValidCLI(cliName string, config models.Config) bool {
	return slices.Contains(AvailableCLIs(config), cliName)
}

func ContractKeyForCLI(cliName string, config models.Config) string {
	tool, ok := AgentToolRegistry(config)[cliName]
	if !ok {
		return cliName
	}
	return tool.ContractKey
}

// DefaultCLI is the CLI used when none is specified.
const DefaultCLI = "claude"

// CLIResolutionConfig holds configured CLI defaults from state.yaml.
type CLIResolutionConfig struct {
	DefaultCLI         string
	DefaultDoerCLI     string
	DefaultReviewerCLI string
}

// ResolveDefaultCLI returns the effective default CLI.
// Resolution order: configValue (from state.yaml) > branded DEFAULT_CLI env
// var or legacy LIZA_DEFAULT_CLI alias > DefaultCLI const.
func ResolveDefaultCLI(configValue string) string {
	if configValue != "" {
		return configValue
	}
	if v := brandedEnvValue("DEFAULT_CLI"); v != "" {
		return v
	}
	return DefaultCLI
}

// ResolveDefaultCLIForRole returns the effective default CLI for a role type.
// Resolution order:
// role-specific config > role-specific env > global config > branded
// DEFAULT_CLI env or legacy LIZA_DEFAULT_CLI alias > DefaultCLI const.
// Orchestrator roles use the doer defaults because they perform work rather than review it.
func ResolveDefaultCLIForRole(roleType string, config CLIResolutionConfig) string {
	if v := roleSpecificConfigCLI(roleType, config); v != "" {
		return v
	}
	if v := roleSpecificEnvCLI(roleType); v != "" {
		return v
	}
	if config.DefaultCLI != "" {
		return config.DefaultCLI
	}
	return ResolveDefaultCLI("")
}

// ResolveCLIFromState resolves the effective CLI for an agent command.
// When flagChanged is true, flagValue is used directly (explicit --cli override).
// Otherwise, the state config's default_cli is resolved through the full chain.
// The stateConfigCLI parameter is the value of state.Config.DefaultCLI (empty if state unreadable).
func ResolveCLIFromState(flagChanged bool, flagValue, stateConfigCLI string) string {
	if flagChanged {
		return flagValue
	}
	return ResolveDefaultCLI(stateConfigCLI)
}

// ResolveCLIFromStateForRole resolves the effective CLI for an agent command.
// When flagChanged is true, flagValue is used directly (explicit --cli override).
// Otherwise, configured and environment defaults are resolved for the role type.
func ResolveCLIFromStateForRole(flagChanged bool, flagValue, roleType string, config CLIResolutionConfig) string {
	if flagChanged {
		return flagValue
	}
	return ResolveDefaultCLIForRole(roleType, config)
}

func roleSpecificConfigCLI(roleType string, config CLIResolutionConfig) string {
	switch roleType {
	case "doer", "orchestrator":
		return config.DefaultDoerCLI
	case "reviewer":
		return config.DefaultReviewerCLI
	default:
		return ""
	}
}

func roleSpecificEnvCLI(roleType string) string {
	switch roleType {
	case "doer", "orchestrator":
		return brandedEnvValue("DEFAULT_DOER_CLI")
	case "reviewer":
		return brandedEnvValue("DEFAULT_REVIEWER_CLI")
	default:
		return ""
	}
}

func brandedEnvValue(suffix string) string {
	lookup := brand.LookupEnv(os.Getenv, suffix)
	if lookup.Warning != "" {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", lookup.Warning)
	}
	return lookup.Value
}
