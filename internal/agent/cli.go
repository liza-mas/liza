package agent

import "os"

// validCLIs is the canonical list of supported CLI backends.
var validCLIs = []string{"claude", "codex", "gemini", "mistral", "kimi"}

// ValidCLIs returns the supported CLI backends. Returns a fresh copy to prevent mutation.
func ValidCLIs() []string {
	out := make([]string, len(validCLIs))
	copy(out, validCLIs)
	return out
}

// DefaultCLI is the CLI used when none is specified.
const DefaultCLI = "claude"

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
