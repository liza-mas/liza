package agent

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/agentlaunch"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// claudeCeiling is the command lifetime a Claude session must allow: the
// longest submit the supervisor itself waits for (ADR-0176, D-90).
var claudeCeiling = strconv.FormatInt(ops.MaxInflightSubmitGrace.Milliseconds(), 10)

// envEntries returns every value of name, in order, so a test sees duplicates
// that a last-wins lookup would hide.
func envEntries(env []string, name string) []string {
	var values []string
	for _, entry := range env {
		if value, ok := strings.CutPrefix(entry, name+"="); ok {
			values = append(values, value)
		}
	}
	return values
}

// A Claude session stops a command started with run_in_background at its
// requested timeout, capped by BASH_MAX_TIMEOUT_MS (2 h by default); the
// default and auto-background limits are separate. A submit whose canonical
// batch may legitimately run 4 h must be launched with a ceiling at least as
// long as the supervisor's own grace.
func TestResolveLaunchEnvironmentRaisesClaudeCommandCeiling(t *testing.T) {
	tests := []struct {
		name    string
		plan    agentlaunch.LaunchPlan
		ambient string // "" leaves the variable unset
		envFile string // claude.env content; "" writes no file
		want    string
	}{
		{name: "unset, provider key", plan: agentlaunch.LaunchPlan{ProviderKey: "claude"}, want: claudeCeiling},
		{name: "unset, tool name alias", plan: agentlaunch.LaunchPlan{ToolName: "claude"}, want: claudeCeiling},
		{name: "ambient higher is kept", plan: agentlaunch.LaunchPlan{ProviderKey: "claude"}, ambient: "30000000", want: "30000000"},
		{name: "ambient lower is raised", plan: agentlaunch.LaunchPlan{ProviderKey: "claude"}, ambient: "600000", want: claudeCeiling},
		{name: "env file lower is raised", plan: agentlaunch.LaunchPlan{ProviderKey: "claude", EnvFiles: []string{"claude.env"}}, envFile: "BASH_MAX_TIMEOUT_MS=600000\n", want: claudeCeiling},
		{name: "env file higher is kept", plan: agentlaunch.LaunchPlan{ProviderKey: "claude", EnvFiles: []string{"claude.env"}}, envFile: "BASH_MAX_TIMEOUT_MS=30000000\n", want: "30000000"},
		{name: "malformed is replaced", plan: agentlaunch.LaunchPlan{ProviderKey: "claude"}, ambient: "two-hours", want: claudeCeiling},
		{name: "negative is replaced", plan: agentlaunch.LaunchPlan{ProviderKey: "claude"}, ambient: "-5", want: claudeCeiling},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// GIVEN a project root and the ambient/overlay values of the case
			root := t.TempDir()
			t.Setenv("BASH_MAX_TIMEOUT_MS", tt.ambient)
			if tt.ambient == "" {
				os.Unsetenv("BASH_MAX_TIMEOUT_MS")
			}
			t.Setenv("BASH_DEFAULT_TIMEOUT_MS", "")
			os.Unsetenv("BASH_DEFAULT_TIMEOUT_MS")
			if tt.envFile != "" {
				if err := os.WriteFile(filepath.Join(root, "claude.env"), []byte(tt.envFile), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			// WHEN the launch environment is resolved
			env, err := resolveLaunchEnvironment(tt.plan, root, "coder-1", "gen-1", nil)

			// THEN exactly one ceiling entry carries the expected value, and the
			// default timeout of ordinary commands is left alone
			if err != nil {
				t.Fatalf("resolveLaunchEnvironment: %v", err)
			}
			if got := envEntries(env, "BASH_MAX_TIMEOUT_MS"); len(got) != 1 || got[0] != tt.want {
				t.Fatalf("BASH_MAX_TIMEOUT_MS entries = %v, want exactly [%s]", got, tt.want)
			}
			if got := envEntries(env, "BASH_DEFAULT_TIMEOUT_MS"); len(got) != 0 {
				t.Fatalf("BASH_DEFAULT_TIMEOUT_MS = %v, want it left unset", got)
			}
		})
	}
}

// Other providers have no such harness cap: their environment is not touched.
func TestResolveLaunchEnvironmentLeavesNonClaudeCommandCeilingAlone(t *testing.T) {
	root := t.TempDir()
	t.Setenv("BASH_MAX_TIMEOUT_MS", "")
	os.Unsetenv("BASH_MAX_TIMEOUT_MS")
	env, err := resolveLaunchEnvironment(agentlaunch.LaunchPlan{ProviderKey: "codex", ToolName: "codex"}, root, "coder-1", "gen-1", nil)
	if err != nil || len(envEntries(env, "BASH_MAX_TIMEOUT_MS")) != 0 {
		t.Fatalf("env = %v, %v; want no ceiling for codex", envEntries(env, "BASH_MAX_TIMEOUT_MS"), err)
	}

	t.Setenv("BASH_MAX_TIMEOUT_MS", "123")
	env, err = resolveLaunchEnvironment(agentlaunch.LaunchPlan{ProviderKey: "codex", ToolName: "codex"}, root, "coder-1", "gen-1", nil)
	if got := envEntries(env, "BASH_MAX_TIMEOUT_MS"); err != nil || len(got) != 1 || got[0] != "123" {
		t.Fatalf("env = %v, %v; want the ambient value passed through unchanged", got, err)
	}
}

// Runtime-input policy wins (ADR-0169): a project that declares the name as a
// runtime input gets it stripped from the session, ceiling included.
func TestResolveLaunchEnvironmentStripsClaudeCeilingDeclaredAsRuntimeInput(t *testing.T) {
	root := t.TempDir()
	t.Setenv("BASH_MAX_TIMEOUT_MS", "")
	os.Unsetenv("BASH_MAX_TIMEOUT_MS")
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("live", models.TaskStatusReady, time.Now().UTC())
	task.Validation = []string{"sh boundary_test.sh"}
	task.RuntimeInputs = []models.RuntimeInput{{ID: "cap", Commands: task.Validation, Recipe: "project.cap", Consumption: models.RuntimeInputReusable, Env: []string{"BASH_MAX_TIMEOUT_MS"}}}
	state.Tasks = append(state.Tasks, task)
	testhelpers.WriteInitialState(t, statePath, state)

	env, err := resolveLaunchEnvironment(agentlaunch.LaunchPlan{ProviderKey: "claude"}, root, "coder-1", "gen-1", nil)
	if err != nil || len(envEntries(env, "BASH_MAX_TIMEOUT_MS")) != 0 {
		t.Fatalf("env = %v, %v; want the declared runtime-input name stripped", envEntries(env, "BASH_MAX_TIMEOUT_MS"), err)
	}
}

// The frozen snapshot was produced by this same function and is reused
// verbatim, so the ceiling is decided once per session.
func TestResolveLaunchEnvironmentReturnsFrozenSnapshotUnchanged(t *testing.T) {
	frozen := []string{"PATH=/bin", "BASH_MAX_TIMEOUT_MS=600000"}
	env, err := resolveLaunchEnvironment(agentlaunch.LaunchPlan{ProviderKey: "claude"}, t.TempDir(), "coder-1", "gen-1", frozen)
	if err != nil || strings.Join(env, ",") != strings.Join(frozen, ",") {
		t.Fatalf("env = %v, %v; want the frozen snapshot unchanged", env, err)
	}
}

// Agents choose the timeout of the commands they start; the contract must name
// the ceiling the engine grants, and start submit-for-review in the background
// so it is not moved there by the foreground timeout and stopped 30 min later.
func TestAgentToolsContractStatesClaudeSubmitCeiling(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "contracts", "AGENT_TOOLS.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	start := strings.Index(text, "#### Background Jobs - Claude only")
	end := strings.Index(text, "#### RTK")
	if start < 0 || end < start {
		t.Fatalf("Claude background-jobs section not found")
	}
	section := text[start:end]
	for _, want := range []string{"`submit-for-review`", "`run_in_background: true`", "`timeout: " + claudeCeiling + "`", "30 min"} {
		if !strings.Contains(section, want) {
			t.Errorf("Claude background-jobs section missing %q", want)
		}
	}
}
