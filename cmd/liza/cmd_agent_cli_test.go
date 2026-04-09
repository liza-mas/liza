package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// setupAgentTestProject creates a minimal project with state.yaml for agent CLI tests.
func setupAgentTestProject(t *testing.T, defaultCLI string) string {
	t.Helper()

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
