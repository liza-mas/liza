package agent

import (
	"fmt"
	"os"
	"os/exec"
)

// validCLIs is the canonical list of supported agent backends.
var validCLIs = []string{"claude", "codex", "codex-acp", "opencode", "opencode-acp", "gemini", "mistral", "kimi"}

// ValidCLIs returns the supported CLI backends. Returns a fresh copy to prevent mutation.
func ValidCLIs() []string {
	out := make([]string, len(validCLIs))
	copy(out, validCLIs)
	return out
}

// CLIExecutableName returns the local executable used for a configured CLI name.
func CLIExecutableName(cliName string) string {
	switch cliName {
	case "codex-acp":
		return "codex"
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
	if isACPXCLI(cliName) {
		return NewACPXAgent(outputsDir)
	}
	return NewCLIAgent(outputsDir)
}

// CheckCLIPrerequisites validates external binaries required by selected backends.
func CheckCLIPrerequisites(cliName string) error {
	if !isACPXCLI(cliName) {
		return nil
	}
	if _, err := exec.LookPath("acpx"); err != nil {
		return fmt.Errorf("%s requires acpx on PATH; install with: npm install -g acpx: %w", cliName, err)
	}
	return nil
}

func isACPXCLI(cliName string) bool {
	return cliName == "codex-acp" || cliName == "opencode-acp"
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
// Resolution order: configValue (from state.yaml) > LIZA_DEFAULT_CLI env var > DefaultCLI const.
func ResolveDefaultCLI(configValue string) string {
	if configValue != "" {
		return configValue
	}
	if v := os.Getenv("LIZA_DEFAULT_CLI"); v != "" {
		return v
	}
	return DefaultCLI
}

// ResolveDefaultCLIForRole returns the effective default CLI for a role type.
// Resolution order:
// role-specific config > role-specific env > global config > LIZA_DEFAULT_CLI > DefaultCLI const.
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
		return os.Getenv("LIZA_DEFAULT_DOER_CLI")
	case "reviewer":
		return os.Getenv("LIZA_DEFAULT_REVIEWER_CLI")
	default:
		return ""
	}
}
