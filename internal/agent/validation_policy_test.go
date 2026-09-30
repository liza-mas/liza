package agent

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
)

type fakeRoleTypes map[string]string

func (f fakeRoleTypes) RoleType(name string) (string, error) {
	if t, ok := f[name]; ok {
		return t, nil
	}
	return "", fmt.Errorf("unknown role %q", name)
}

var codingPairTypes = fakeRoleTypes{"coder": "doer", "code-reviewer": "reviewer"}

// clearCLIEnv isolates the default CLI chain from the developer's shell.
func clearCLIEnv(t *testing.T) {
	t.Helper()
	for _, suffix := range []string{"DEFAULT_CLI", "DEFAULT_DOER_CLI", "DEFAULT_REVIEWER_CLI"} {
		t.Setenv(brand.EnvName(suffix), "")
		t.Setenv(brand.LegacyEnvName(suffix), "")
	}
}

func TestDefaultRoleCLIs(t *testing.T) {
	tests := []struct {
		name    string
		config  models.Config
		models  RoleModels
		env     map[string]string
		want    []RoleCLI
		wantErr string
	}{
		{name: "no configuration falls back to the built-in default",
			want: []RoleCLI{{"coder", DefaultCLI}, {"code-reviewer", DefaultCLI}}},
		{name: "role type env defaults apply without flags",
			env:  map[string]string{"DEFAULT_REVIEWER_CLI": "codex"},
			want: []RoleCLI{{"coder", DefaultCLI}, {"code-reviewer", "codex"}}},
		{name: "config role defaults",
			config: models.Config{DefaultDoerCLI: "codex", DefaultReviewerCLI: "claude"},
			want:   []RoleCLI{{"coder", "codex"}, {"code-reviewer", "claude"}}},
		{name: "models.yaml type defaults beat config",
			config: models.Config{DefaultCLI: "claude"},
			models: RoleModels{Defaults: RoleModelDefaults{Reviewer: &RoleModelEntry{CLI: "codex"}}},
			want:   []RoleCLI{{"coder", "claude"}, {"code-reviewer", "codex"}}},
		{name: "models.yaml role entry beats type default",
			models: RoleModels{Defaults: RoleModelDefaults{Doer: &RoleModelEntry{CLI: "codex"}}, Roles: map[string]RoleModelEntry{"coder": {CLI: "opencode"}}},
			want:   []RoleCLI{{"coder", "opencode"}, {"code-reviewer", DefaultCLI}}},
		{name: "unknown default profile is a resolution error",
			config:  models.Config{DefaultDoerProfile: "missing"},
			wantErr: "role coder"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearCLIEnv(t)
			for suffix, value := range tt.env {
				t.Setenv(brand.EnvName(suffix), value)
			}
			got, err := DefaultRoleCLIs([]string{"coder", "code-reviewer"}, codingPairTypes, tt.config, tt.models)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("DefaultRoleCLIs() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("DefaultRoleCLIs() error = %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("DefaultRoleCLIs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDefaultRoleCLIsUnknownRoleType(t *testing.T) {
	clearCLIEnv(t)
	_, err := DefaultRoleCLIs([]string{"ghost"}, codingPairTypes, models.Config{}, RoleModels{})
	if err == nil || !strings.Contains(err.Error(), "role ghost") {
		t.Fatalf("DefaultRoleCLIs() error = %v, want role ghost resolution error", err)
	}
}

func TestAllValidationLocal(t *testing.T) {
	local := models.AgentToolConfig{ValidationExecution: ValidationExecutionLocal}
	pair := []RoleCLI{{"coder", "claude"}, {"code-reviewer", "codex"}}
	tests := []struct {
		name  string
		roles []RoleCLI
		tools map[string]models.AgentToolConfig
		want  bool
	}{
		{name: "doer and reviewer CLIs local", roles: pair, tools: map[string]models.AgentToolConfig{"claude": local, "codex": local}, want: true},
		{name: "reviewer CLI unset", roles: pair, tools: map[string]models.AgentToolConfig{"claude": local}},
		{name: "reviewer CLI artifact-only", roles: pair, tools: map[string]models.AgentToolConfig{"claude": local, "codex": {ValidationExecution: "artifact-only"}}},
		{name: "no roles resolved", tools: map[string]models.AgentToolConfig{"claude": local}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AllValidationLocal(tt.roles, models.Config{AgentTools: tt.tools}); got != tt.want {
				t.Fatalf("AllValidationLocal() = %v, want %v", got, tt.want)
			}
		})
	}
}
