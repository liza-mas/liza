package main

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/liza-mas/liza/internal/db"
	"github.com/spf13/cobra"
)

func TestResetRootCmdForTestResetsIdentityFlags(t *testing.T) {
	// --agent-id is now a local flag on specific commands (e.g. submit-verdict)
	if err := submitVerdictCmd.Flags().Set("agent-id", "coder-123"); err != nil {
		t.Fatalf("set --agent-id failed: %v", err)
	}
	// --changed-by is now a local flag on specific commands (e.g. pause)
	if err := pauseCmd.Flags().Set("changed-by", "auditor-4"); err != nil {
		t.Fatalf("set --changed-by failed: %v", err)
	}

	resetRootCmdForTest(t)

	agentID, err := submitVerdictCmd.Flags().GetString("agent-id")
	if err != nil {
		t.Fatalf("get --agent-id failed: %v", err)
	}
	if agentID != "" {
		t.Fatalf("--agent-id = %q, want empty", agentID)
	}

	changedBy, err := pauseCmd.Flags().GetString("changed-by")
	if err != nil {
		t.Fatalf("get --changed-by failed: %v", err)
	}
	if changedBy != "" {
		t.Fatalf("--changed-by = %q, want empty", changedBy)
	}
}

func TestResetRootCmdForTestClearsBlackboardSingletons(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.yaml")

	first := db.For(statePath)
	if first == nil {
		t.Fatal("db.For returned nil")
	}

	resetRootCmdForTest(t)

	second := db.For(statePath)
	if second == first {
		t.Fatal("expected singleton map reset to return a fresh instance")
	}
}

func TestResetRootCmdForTestClearsHelpFlagState(t *testing.T) {
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs([]string{"get", "--help"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("execute get --help failed: %v", err)
	}

	getCmd, _, err := rootCmd.Find([]string{"get"})
	if err != nil {
		t.Fatalf("find get command failed: %v", err)
	}
	helpFlag := getCmd.Flags().Lookup("help")
	if helpFlag == nil {
		t.Fatal("get command help flag not found")
	}
	if !helpFlag.Changed {
		t.Fatal("expected help flag to be marked changed after get --help")
	}

	resetRootCmdForTest(t)

	if helpFlag.Changed {
		t.Fatal("expected help flag changed state to be reset")
	}
	if helpFlag.Value.String() != helpFlag.DefValue {
		t.Fatalf("help flag value = %q, want default %q", helpFlag.Value.String(), helpFlag.DefValue)
	}
}

func resetRootCmdForTest(t *testing.T) {
	t.Helper()

	// Reset once; t.Cleanup ensures no leaked singletons after the test.
	// NOTE: These tests are sequential (os.Chdir prevents t.Parallel).
	// If parallelism is ever enabled, ResetInstances would need per-test
	// isolation (e.g. a scoped registry) instead of a global clear.
	db.ResetInstances()
	t.Cleanup(db.ResetInstances)

	resetHelpFlag(t, rootCmd)
	for _, child := range rootCmd.Commands() {
		resetHelpFlag(t, child)
		resetFlagIfPresent(child, "agent-id")
		resetFlagIfPresent(child, "changed-by")
		// Init command workspace flags — must reset Changed state between tests.
		for _, name := range []string{"spec", "config", "entry-point", "branch", "post-worktree-cmd", "auto-resume", "default-cli", "cli", "claude", "codex", "gemini", "mistral"} {
			resetFlagIfPresent(child, name)
		}
	}

	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs(nil)
}

func resetFlagIfPresent(cmd *cobra.Command, name string) {
	f := cmd.Flags().Lookup(name)
	if f != nil {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	}
}

func resetHelpFlag(t *testing.T, cmd *cobra.Command) {
	t.Helper()

	helpFlag := cmd.Flags().Lookup("help")
	if helpFlag == nil {
		return
	}
	if err := cmd.Flags().Set("help", "false"); err != nil {
		t.Fatalf("failed to reset help flag for %s: %v", cmd.CommandPath(), err)
	}
	helpFlag.Changed = false
}
