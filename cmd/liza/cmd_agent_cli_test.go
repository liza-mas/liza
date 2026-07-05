package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// setupAgentTestProject creates a minimal project with state.yaml for agent CLI tests.
func setupAgentTestProject(t *testing.T, defaultCLI string) string {
	t.Helper()

	// Neutralize ambient LIZA_AGENT_ID so tests are hermetic inside real
	// agent sessions (where the env var is set to the running agent's ID).
	// Without this, identity.Resolve picks up the ambient value, causing a
	// role-mismatch error before CLI validation is reached.
	t.Setenv("LIZA_AGENT_ID", "")

	testhelpers.SetupGlobalLiza(t)
	projectRoot := t.TempDir()

	for _, args := range [][]string{
		{"git", "-C", projectRoot, "init"},
		{"git", "-C", projectRoot, "config", "user.email", "test@test.com"},
		{"git", "-C", projectRoot, "config", "user.name", "Test"},
		{"git", "-C", projectRoot, "commit", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}

	lizaDir := filepath.Join(projectRoot, ".liza")
	if err := os.MkdirAll(lizaDir, 0o755); err != nil {
		t.Fatalf("mkdir .liza: %v", err)
	}

	if err := os.WriteFile(filepath.Join(lizaDir, "pipeline.yaml"), embedded.PipelineConfigContent(), 0o644); err != nil {
		t.Fatalf("write pipeline.yaml: %v", err)
	}

	state := &models.State{
		Version: 1,
		Goal:    models.Goal{ID: "goal-1", Status: models.GoalStatusInProgress},
		Config: models.Config{
			DefaultCLI:        defaultCLI,
			IntegrationBranch: "integration",
			Mode:              models.SystemModeRunning,
			HeartbeatInterval: 60,
			LeaseDuration:     1800,
		},
		Agents: make(map[string]models.Agent),
	}

	bb := db.For(filepath.Join(lizaDir, "state.yaml"))
	if err := bb.Write(state); err != nil {
		t.Fatalf("write state: %v", err)
	}

	return projectRoot
}

// TestAgentCmd_InvalidCLIFromStateIsRejected proves that the agent command reads
// state.Config.DefaultCLI at runtime. An invalid CLI value in state, with no --cli
// override, must produce an "invalid CLI" error naming the state's value.
func TestAgentCmd_InvalidCLIFromStateIsRejected(t *testing.T) {
	t.Setenv("LIZA_DEFAULT_CLI", "")
	t.Setenv("LIZA_DEFAULT_DOER_CLI", "")
	t.Setenv("LIZA_DEFAULT_REVIEWER_CLI", "")
	projectRoot := setupAgentTestProject(t, "nonexistent-cli")

	oldDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(oldDir) }()
	_ = os.Chdir(projectRoot)

	resetRootCmdForTest(t)
	rootCmd.SetArgs([]string{"agent", "coder"})
	err := rootCmd.Execute()

	if err == nil || !strings.Contains(err.Error(), "invalid CLI: nonexistent-cli") {
		t.Fatalf("expected 'invalid CLI: nonexistent-cli' error, got: %v", err)
	}
}

// TestAgentCmd_ExplicitFlagOverridesInvalidState proves that --cli takes precedence
// over state config. State has an invalid CLI, but explicit --cli provides a valid
// but nonexistent one (xxxcli) — the error should be about xxxcli, not nonexistent-cli,
// proving the flag won.
func TestAgentCmd_ExplicitFlagOverridesInvalidState(t *testing.T) {
	t.Setenv("LIZA_DEFAULT_CLI", "")
	projectRoot := setupAgentTestProject(t, "nonexistent-cli")

	oldDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(oldDir) }()
	_ = os.Chdir(projectRoot)

	resetRootCmdForTest(t)
	// Use another invalid CLI via --cli to verify it's the flag value that appears
	// in the error, not the state value.
	rootCmd.SetArgs([]string{"agent", "coder", "--cli", "xxxcli"})
	err := rootCmd.Execute()

	if err == nil || !strings.Contains(err.Error(), "invalid CLI: xxxcli") {
		t.Fatalf("expected 'invalid CLI: xxxcli' (from flag), got: %v", err)
	}
}

// TestAgentCmd_EnvVarOverridesConst proves that LIZA_DEFAULT_CLI env var is used
// when state config is empty and --cli is not set. We set the env to an invalid
// value to observe it in the error message.
func TestAgentCmd_EnvVarOverridesConst(t *testing.T) {
	t.Setenv("LIZA_DEFAULT_CLI", "envtestcli")
	t.Setenv("LIZA_DEFAULT_DOER_CLI", "")
	t.Setenv("LIZA_DEFAULT_REVIEWER_CLI", "")
	projectRoot := setupAgentTestProject(t, "")

	oldDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(oldDir) }()
	_ = os.Chdir(projectRoot)

	resetRootCmdForTest(t)
	rootCmd.SetArgs([]string{"agent", "coder"})
	err := rootCmd.Execute()

	if err == nil || !strings.Contains(err.Error(), "invalid CLI: envtestcli") {
		t.Fatalf("expected 'invalid CLI: envtestcli' (from env), got: %v", err)
	}
}

func TestAgentCmd_ExplainLaunchUsesConfiguredToolProfile(t *testing.T) {
	t.Setenv("LIZA_DEFAULT_CLI", "")
	t.Setenv("LIZA_DEFAULT_DOER_CLI", "")
	t.Setenv("LIZA_DEFAULT_REVIEWER_CLI", "")
	projectRoot := setupAgentTestProject(t, "")

	statePath := filepath.Join(projectRoot, ".liza", "state.yaml")
	bb := db.For(statePath)
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	state.Config.AgentTools = map[string]models.AgentToolConfig{
		"cursor": {
			Executable:      "cursor-agent",
			PromptTransport: agent.PromptTransportFile,
			RunArgs:         []string{"--cwd", "{{projectRoot}}", "--prompt-file", "{{promptFile}}", "--model", "{{profile.model}}"},
			ContractKey:     "none",
		},
	}
	state.Config.AgentProfiles = map[string]models.AgentProfileConfig{
		"careful": {CLI: "cursor", Vars: map[string]string{"model": "gpt-5"}},
	}
	state.Config.DefaultDoerProfile = "careful"
	if err := bb.Write(state); err != nil {
		t.Fatalf("write state: %v", err)
	}

	oldDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(oldDir) }()
	_ = os.Chdir(projectRoot)

	resetRootCmdForTest(t)
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetArgs([]string{"agent", "coder", "--explain-launch", "--no-log"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("agent --explain-launch error: %v", err)
	}

	output := out.String()
	for _, want := range []string{
		"tool: cursor",
		"profile: careful",
		"executable: cursor-agent",
		"--cwd ",
		"--prompt-file <prompt-file>",
		"--model gpt-5",
		"prompt_transport: file",
		"contract_key: none",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("explain output missing %q:\n%s", want, output)
		}
	}
}

func TestAgentCmd_RoleSpecificEnvOverridesGlobalEnv(t *testing.T) {
	tests := []struct {
		name     string
		role     string
		envName  string
		envValue string
	}{
		{
			name:     "doer",
			role:     "coder",
			envName:  "LIZA_DEFAULT_DOER_CLI",
			envValue: "doercli",
		},
		{
			name:     "orchestrator uses doer default",
			role:     "orchestrator",
			envName:  "LIZA_DEFAULT_DOER_CLI",
			envValue: "doercli",
		},
		{
			name:     "reviewer",
			role:     "code-reviewer",
			envName:  "LIZA_DEFAULT_REVIEWER_CLI",
			envValue: "reviewercli",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("LIZA_DEFAULT_CLI", "globalcli")
			t.Setenv("LIZA_DEFAULT_DOER_CLI", "")
			t.Setenv("LIZA_DEFAULT_REVIEWER_CLI", "")
			t.Setenv(tt.envName, tt.envValue)
			projectRoot := setupAgentTestProject(t, "")

			oldDir, _ := os.Getwd()
			defer func() { _ = os.Chdir(oldDir) }()
			_ = os.Chdir(projectRoot)

			resetRootCmdForTest(t)
			rootCmd.SetArgs([]string{"agent", tt.role})
			err := rootCmd.Execute()

			if err == nil || !strings.Contains(err.Error(), "invalid CLI: "+tt.envValue) {
				t.Fatalf("expected invalid CLI from %s=%s, got: %v", tt.envName, tt.envValue, err)
			}
		})
	}
}
