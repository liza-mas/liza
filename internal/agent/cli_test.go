package agent

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
)

func TestValidCLIsIncludesCodexACP(t *testing.T) {
	if !slices.Contains(ValidCLIs(), "codex-acp") {
		t.Fatalf("ValidCLIs() = %v, want codex-acp", ValidCLIs())
	}
	if !slices.Contains(ValidCLIs(), "cursor-acp") {
		t.Fatalf("ValidCLIs() = %v, want cursor-acp", ValidCLIs())
	}
	if !slices.Contains(ValidCLIs(), "opencode") {
		t.Fatalf("ValidCLIs() = %v, want opencode", ValidCLIs())
	}
	if !slices.Contains(ValidCLIs(), "opencode-acp") {
		t.Fatalf("ValidCLIs() = %v, want opencode-acp", ValidCLIs())
	}
}

func TestNewLLMAgentForCLI(t *testing.T) {
	if _, ok := NewLLMAgentForCLI("codex-acp", "").(*ACPXAgent); !ok {
		t.Fatalf("NewLLMAgentForCLI(codex-acp) did not return *ACPXAgent")
	}
	if _, ok := NewLLMAgentForCLI("cursor-acp", "").(*ACPXAgent); !ok {
		t.Fatalf("NewLLMAgentForCLI(cursor-acp) did not return *ACPXAgent")
	}
	if _, ok := NewLLMAgentForCLI("opencode-acp", "").(*ACPXAgent); !ok {
		t.Fatalf("NewLLMAgentForCLI(opencode-acp) did not return *ACPXAgent")
	}
	if _, ok := NewLLMAgentForCLI("codex", "").(*CLIAgent); !ok {
		t.Fatalf("NewLLMAgentForCLI(codex) did not return *CLIAgent")
	}
	if _, ok := NewLLMAgentForCLI("opencode", "").(*CLIAgent); !ok {
		t.Fatalf("NewLLMAgentForCLI(opencode) did not return *CLIAgent")
	}
}

func TestCLIExecutableNameMapsConfiguredNamesToBinaries(t *testing.T) {
	tests := map[string]string{
		"claude":       "claude",
		"codex":        "codex",
		"codex-acp":    "codex",
		"cursor-acp":   "cursor-agent",
		"opencode":     "opencode",
		"opencode-acp": "opencode",
		"gemini":       "gemini",
		"mistral":      "vibe",
		"kimi":         "kimi",
	}

	for cliName, want := range tests {
		t.Run(cliName, func(t *testing.T) {
			if got := CLIExecutableName(cliName); got != want {
				t.Fatalf("CLIExecutableName(%q) = %q, want %q", cliName, got, want)
			}
		})
	}
}

func TestCheckCLIPrerequisitesIgnoresPlainCLIs(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	if err := CheckCLIPrerequisites("codex"); err != nil {
		t.Fatalf("CheckCLIPrerequisites(codex) error = %v, want nil", err)
	}
	if err := CheckCLIPrerequisites("opencode"); err != nil {
		t.Fatalf("CheckCLIPrerequisites(opencode) error = %v, want nil", err)
	}
}

func TestCheckCLIPrerequisitesRequiresACPXForCodexACP(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	for _, cliName := range []string{"codex-acp", "cursor-acp", "opencode-acp"} {
		t.Run(cliName, func(t *testing.T) {
			err := CheckCLIPrerequisites(cliName)
			if err == nil {
				t.Fatalf("CheckCLIPrerequisites(%s) error = nil, want missing acpx error", cliName)
			}
			if !strings.Contains(err.Error(), cliName+" requires acpx on PATH") {
				t.Fatalf("error = %q, want %s PATH prerequisite", err, cliName)
			}
			if !strings.Contains(err.Error(), "npm install -g acpx") {
				t.Fatalf("error = %q, want install hint", err)
			}
		})
	}
}

func TestCheckCLIPrerequisitesAcceptsACPXOnPath(t *testing.T) {
	binDir := t.TempDir()
	acpxPath := filepath.Join(binDir, "acpx")
	if err := os.WriteFile(acpxPath, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	cursorPath := filepath.Join(binDir, "cursor-agent")
	if err := os.WriteFile(cursorPath, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	if err := CheckCLIPrerequisites("codex-acp"); err != nil {
		t.Fatalf("CheckCLIPrerequisites(codex-acp) error = %v, want nil", err)
	}
	if err := CheckCLIPrerequisites("cursor-acp"); err != nil {
		t.Fatalf("CheckCLIPrerequisites(cursor-acp) error = %v, want nil", err)
	}
}

func TestResolveDefaultCLI(t *testing.T) {
	// Clean env for test isolation
	t.Setenv("LIZA_DEFAULT_CLI", "")
	t.Setenv("LIZA_DEFAULT_DOER_CLI", "")
	t.Setenv("LIZA_DEFAULT_REVIEWER_CLI", "")

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

func TestResolveDefaultCLIUsesBrandedEnvBeforeLegacy(t *testing.T) {
	restore := setAgentTestBrandEnvPrefix(t, "ACME_AGENT")
	defer restore()
	t.Setenv("ACME_AGENT_DEFAULT_CLI", "codex")
	t.Setenv("LIZA_DEFAULT_CLI", "gemini")

	if got := ResolveDefaultCLI(""); got != "codex" {
		t.Fatalf("ResolveDefaultCLI() = %q, want branded env value", got)
	}
}

func TestResolveDefaultCLIForRole(t *testing.T) {
	t.Setenv("LIZA_DEFAULT_CLI", "")
	t.Setenv("LIZA_DEFAULT_DOER_CLI", "")
	t.Setenv("LIZA_DEFAULT_REVIEWER_CLI", "")

	tests := []struct {
		name     string
		roleType string
		config   CLIResolutionConfig
		env      map[string]string
		want     string
	}{
		{
			name:     "doer role-specific config wins",
			roleType: "doer",
			config: CLIResolutionConfig{
				DefaultCLI:     "claude",
				DefaultDoerCLI: "codex",
			},
			env:  map[string]string{"LIZA_DEFAULT_DOER_CLI": "gemini"},
			want: "codex",
		},
		{
			name:     "orchestrator uses doer config",
			roleType: "orchestrator",
			config: CLIResolutionConfig{
				DefaultCLI:     "claude",
				DefaultDoerCLI: "mistral",
			},
			want: "mistral",
		},
		{
			name:     "reviewer role-specific config wins",
			roleType: "reviewer",
			config: CLIResolutionConfig{
				DefaultCLI:         "claude",
				DefaultReviewerCLI: "gemini",
			},
			env:  map[string]string{"LIZA_DEFAULT_REVIEWER_CLI": "codex"},
			want: "gemini",
		},
		{
			name:     "role env wins over global config",
			roleType: "reviewer",
			config: CLIResolutionConfig{
				DefaultCLI: "codex",
			},
			env:  map[string]string{"LIZA_DEFAULT_REVIEWER_CLI": "gemini"},
			want: "gemini",
		},
		{
			name:     "doer env used before global env",
			roleType: "doer",
			env: map[string]string{
				"LIZA_DEFAULT_DOER_CLI": "codex",
				"LIZA_DEFAULT_CLI":      "gemini",
			},
			want: "codex",
		},
		{
			name:     "reviewer env used before global env",
			roleType: "reviewer",
			env: map[string]string{
				"LIZA_DEFAULT_REVIEWER_CLI": "mistral",
				"LIZA_DEFAULT_CLI":          "gemini",
			},
			want: "mistral",
		},
		{
			name:     "global env fallback",
			roleType: "reviewer",
			env:      map[string]string{"LIZA_DEFAULT_CLI": "gemini"},
			want:     "gemini",
		},
		{
			name:     "const fallback",
			roleType: "reviewer",
			want:     DefaultCLI,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("LIZA_DEFAULT_CLI", "")
			t.Setenv("LIZA_DEFAULT_DOER_CLI", "")
			t.Setenv("LIZA_DEFAULT_REVIEWER_CLI", "")
			for name, value := range tt.env {
				t.Setenv(name, value)
			}

			got := ResolveDefaultCLIForRole(tt.roleType, tt.config)
			if got != tt.want {
				t.Errorf("ResolveDefaultCLIForRole(%q, %+v) = %q, want %q", tt.roleType, tt.config, got, tt.want)
			}
		})
	}
}

func setAgentTestBrandEnvPrefix(t *testing.T, prefix string) func() {
	t.Helper()
	previous := brand.EnvPrefix
	brand.EnvPrefix = prefix
	return func() {
		brand.EnvPrefix = previous
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
