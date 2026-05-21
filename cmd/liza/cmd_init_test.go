package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestInitDispatch_WorkspaceFlagsRequireDescription(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "branch without description errors",
			args:    []string{"init", "--branch", "custom"},
			wantErr: "requires a description argument",
		},
		{
			name:    "config without description errors",
			args:    []string{"init", "--config", "custom.yaml"},
			wantErr: "requires a description argument",
		},
		{
			name:    "spec without description errors",
			args:    []string{"init", "--spec", "custom-spec.md"},
			wantErr: "requires a description argument",
		},
		{
			name:    "post-worktree-cmd without description errors",
			args:    []string{"init", "--post-worktree-cmd", "make setup"},
			wantErr: "requires a description argument",
		},
		{
			name:    "entry-point without description errors",
			args:    []string{"init", "--entry-point", "detailed-spec"},
			wantErr: "requires a description argument",
		},
		{
			name:    "scip-search without description errors",
			args:    []string{"init", "--scip-search", "go"},
			wantErr: "requires a description argument",
		},
		{
			name:    "auto-resume without description gets specific error",
			args:    []string{"init", "--claude", "--auto-resume"},
			wantErr: "--auto-resume requires full workspace init",
		},
		{
			name:    "agent flag with workspace flag and no description errors",
			args:    []string{"init", "--claude", "--branch", "foo"},
			wantErr: "workspace flags",
		},
		{
			name:    "default-cli without description errors",
			args:    []string{"init", "--default-cli", "codex"},
			wantErr: "requires a description argument",
		},
		{
			name:    "default-doer-cli without description errors",
			args:    []string{"init", "--default-doer-cli", "codex"},
			wantErr: "requires a description argument",
		},
		{
			name:    "default-reviewer-cli without description errors",
			args:    []string{"init", "--default-reviewer-cli", "gemini"},
			wantErr: "requires a description argument",
		},
		{
			name:    "agent flag with default-cli and no description errors",
			args:    []string{"init", "--codex", "--default-cli", "codex"},
			wantErr: "workspace flags",
		},
		{
			name:    "agent flag with default-doer-cli and no description errors",
			args:    []string{"init", "--codex", "--default-doer-cli", "codex"},
			wantErr: "workspace flags",
		},
		{
			name:    "agent flag with scip-search and no description errors",
			args:    []string{"init", "--codex", "--scip-search", "go"},
			wantErr: "workspace flags",
		},
		{
			name:    "invalid default-cli value errors",
			args:    []string{"init", "--default-cli", "invalid", "Goal"},
			wantErr: "invalid --default-cli",
		},
		{
			name:    "invalid default-doer-cli value errors",
			args:    []string{"init", "--default-doer-cli", "invalid", "Goal"},
			wantErr: "invalid --default-doer-cli",
		},
		{
			name:    "invalid default-reviewer-cli value errors",
			args:    []string{"init", "--default-reviewer-cli", "invalid", "Goal"},
			wantErr: "invalid --default-reviewer-cli",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetRootCmdForTest(t)
			rootCmd.SetArgs(tt.args)
			err := rootCmd.Execute()

			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestInitDispatch_ScipSearchRepeatableFlagPersistsConfig(t *testing.T) {
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	testhelpers.SetupGlobalLiza(t)
	testhelpers.CreateCommittedSpecFile(t, projectRoot, "vision.md", "# Vision\n")

	err := executeRootCommand(
		t,
		projectRoot,
		"init",
		"--spec",
		"specs/vision.md",
		"--scip-search",
		"go",
		"--scip-search",
		"typescript",
		"Goal with scip-search",
	)
	if err != nil {
		t.Fatalf("init with repeated --scip-search failed: %v", err)
	}

	statePath := filepath.Join(projectRoot, ".liza", "state.yaml")
	state, err := db.New(statePath).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	want := []string{"go", "typescript"}
	if !slices.Equal(state.Config.ScipSearch, want) {
		t.Fatalf("state.Config.ScipSearch = %v, want %v", state.Config.ScipSearch, want)
	}
}

func TestInitDispatch_AgentFlagAlonePassesDispatch(t *testing.T) {
	// Run in a temp dir with fake HOME to prevent side effects on the
	// developer's workspace. The command will fail downstream (no git repo,
	// no ~/.liza), but it must NOT fail at the dispatch level.
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() { _ = os.Chdir(oldDir) }()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	resetRootCmdForTest(t)
	rootCmd.SetArgs([]string{"init", "--claude"})
	err = rootCmd.Execute()

	// It will error (no git repo / no global config), but not at dispatch.
	dispatchErrors := []string{"requires a description", "workspace flags", "--auto-resume requires"}
	if err != nil {
		for _, de := range dispatchErrors {
			if strings.Contains(err.Error(), de) {
				t.Fatalf("hit dispatch-level error: %v", err)
			}
		}
	}
}

func TestInitDispatch_WizardPathForwardsConfigDefault(t *testing.T) {
	// Create a temp HOME with ~/.liza/pipeline.yaml to simulate the scenario
	// where defaultPipelineConfigPath() returns a real path at init() time.
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	lizaDir := filepath.Join(tmpDir, ".liza")
	if err := os.MkdirAll(lizaDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	pipelinePath := filepath.Join(lizaDir, "pipeline.yaml")
	if err := os.WriteFile(pipelinePath, []byte("entry_points: {}\n"), 0o644); err != nil {
		t.Fatalf("write pipeline.yaml: %v", err)
	}

	resetRootCmdForTest(t)

	// Simulate what init() would have done if pipeline.yaml existed at
	// registration time: set the flag's default to the pipeline path.
	configFlag := initCmd.Flags().Lookup("config")
	if configFlag == nil {
		t.Fatal("config flag not registered on init command")
	}
	configFlag.DefValue = pipelinePath
	_ = configFlag.Value.Set(pipelinePath)
	configFlag.Changed = false

	// When no explicit flags are set, hasExplicitInitFlags must be false
	// (wizard entry condition), yet the cobra default must still be readable.
	if hasExplicitInitFlags(initCmd) {
		t.Fatal("hasExplicitInitFlags should be false when no flags are explicitly set")
	}

	configPath, err := initCmd.Flags().GetString("config")
	if err != nil {
		t.Fatalf("GetString(config): %v", err)
	}
	if configPath != pipelinePath {
		t.Errorf("wizard path ConfigPath = %q, want %q (cobra default not forwarded)", configPath, pipelinePath)
	}
}
