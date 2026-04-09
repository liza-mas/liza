package agent

import (
	"testing"
)

func TestResolveDefaultCLI(t *testing.T) {
	// Clean env for test isolation
	t.Setenv("LIZA_DEFAULT_CLI", "")

	tests := []struct {
		name        string
		configValue string
		envValue    string
		want        string
	}{
		{
			name:        "empty config and env returns const",
			configValue: "",
			envValue:    "",
			want:        DefaultCLI,
		},
		{
			name:        "config value wins",
			configValue: "codex",
			envValue:    "",
			want:        "codex",
		},
		{
			name:        "env var used when config empty",
			configValue: "",
			envValue:    "gemini",
			want:        "gemini",
		},
		{
			name:        "config value wins over env var",
			configValue: "codex",
			envValue:    "gemini",
			want:        "codex",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("LIZA_DEFAULT_CLI", tt.envValue)
			got := ResolveDefaultCLI(tt.configValue)
			if got != tt.want {
				t.Errorf("ResolveDefaultCLI(%q) = %q, want %q", tt.configValue, got, tt.want)
			}
		})
	}
}

func TestResolveCLIFromState(t *testing.T) {
	tests := []struct {
		name           string
		flagChanged    bool
		flagValue      string
		stateConfigCLI string
		envValue       string
		want           string
	}{
		{
			name:           "explicit flag wins over state config",
			flagChanged:    true,
			flagValue:      "gemini",
			stateConfigCLI: "codex",
			want:           "gemini",
		},
		{
			name:           "explicit flag wins over env var",
			flagChanged:    true,
			flagValue:      "gemini",
			stateConfigCLI: "",
			envValue:       "codex",
			want:           "gemini",
		},
		{
			name:           "state config used when flag not set",
			flagChanged:    false,
			flagValue:      "",
			stateConfigCLI: "codex",
			want:           "codex",
		},
		{
			name:           "env var used when flag not set and no state config",
			flagChanged:    false,
			flagValue:      "",
			stateConfigCLI: "",
			envValue:       "gemini",
			want:           "gemini",
		},
		{
			name:           "const used when nothing else set",
			flagChanged:    false,
			flagValue:      "",
			stateConfigCLI: "",
			want:           DefaultCLI,
		},
		{
			name:           "state config wins over env var",
			flagChanged:    false,
			flagValue:      "",
			stateConfigCLI: "codex",
			envValue:       "gemini",
			want:           "codex",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("LIZA_DEFAULT_CLI", tt.envValue)
			got := ResolveCLIFromState(tt.flagChanged, tt.flagValue, tt.stateConfigCLI)
			if got != tt.want {
				t.Errorf("ResolveCLIFromState(%v, %q, %q) = %q, want %q",
					tt.flagChanged, tt.flagValue, tt.stateConfigCLI, got, tt.want)
			}
		})
	}
}
