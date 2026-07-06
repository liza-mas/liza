package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/scipsearch"
	"github.com/liza-mas/liza/internal/semble"
	"github.com/liza-mas/liza/internal/testhelpers"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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
			name:    "copy-worktree-env-files without description errors",
			args:    []string{"init", "--copy-worktree-env-files"},
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
			name:    "no-follow-up without description gets specific error",
			args:    []string{"init", "--claude", "--no-follow-up"},
			wantErr: "--no-follow-up requires full workspace init",
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
			name:    "agent flag with copy-worktree-env-files and no description errors",
			args:    []string{"init", "--codex", "--copy-worktree-env-files"},
			wantErr: "workspace flags",
		},
		{
			name:    "agent flag with default-doer-cli and no description errors",
			args:    []string{"init", "--codex", "--default-doer-cli", "codex"},
			wantErr: "workspace flags",
		},
		{
			name:    "agent flag with no-follow-up and no description errors",
			args:    []string{"init", "--codex", "--no-follow-up"},
			wantErr: "--no-follow-up requires full workspace init",
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

func TestHasExplicitInitFlags_NoFollowUpDoesNotBypassWizard(t *testing.T) {
	resetRootCmdForTest(t)
	defer resetRootCmdForTest(t)

	if err := initCmd.Flags().Set("no-follow-up", "true"); err != nil {
		t.Fatalf("set no-follow-up: %v", err)
	}
	if hasExplicitInitFlags(initCmd) {
		t.Fatal("hasExplicitInitFlags() = true for --no-follow-up, want false so the wizard path can pass it through")
	}
}

func TestCollectAgentFlagsIncludesCursor(t *testing.T) {
	cmd := &cobra.Command{Use: "init"}
	cmd.Flags().StringArray("provider", nil, "")
	for _, name := range agentFlagNames {
		cmd.Flags().Bool(name, false, "")
	}

	if err := cmd.Flags().Set("cursor", "true"); err != nil {
		t.Fatalf("set cursor flag: %v", err)
	}

	got := collectAgentFlags(cmd)
	if !slices.Contains(got, "cursor") {
		t.Fatalf("collectAgentFlags() = %v, want cursor", got)
	}
}

func TestInitDispatch_NoSembleFlagsOrInitParams(t *testing.T) {
	resetRootCmdForTest(t)
	defer resetRootCmdForTest(t)

	var flagNames []string
	initCmd.Flags().VisitAll(func(flag *pflag.Flag) {
		flagNames = append(flagNames, flag.Name)
		if strings.Contains(strings.ToLower(flag.Name), "semble") {
			t.Fatalf("init flag %q contains Semble; Semble must remain environment-only", flag.Name)
		}
		if strings.Contains(strings.ToLower(flag.Usage), "semble") {
			t.Fatalf("init flag %q usage mentions Semble: %q", flag.Name, flag.Usage)
		}
	})
	if initCmd.Flags().Lookup("semble") != nil {
		t.Fatal("init command registered --semble, want no Semble CLI flag")
	}
	if initCmd.Flags().Lookup("enable-semble") != nil {
		t.Fatal("init command registered --enable-semble, want no Semble CLI flag")
	}

	paramsType := reflect.TypeOf(commands.InitParams{})
	for i := 0; i < paramsType.NumField(); i++ {
		field := paramsType.Field(i)
		if strings.Contains(strings.ToLower(field.Name), "semble") {
			t.Fatalf("commands.InitParams field %q contains Semble; Semble must not be forwarded as durable CLI params", field.Name)
		}
	}

	help, err := executeInitHelpForTest(t)
	if err != nil {
		t.Fatalf("init --help failed: %v", err)
	}
	if strings.Contains(strings.ToLower(help), "semble") {
		t.Fatalf("init --help mentions Semble; flags=%v help:\n%s", flagNames, help)
	}
}

func TestHasExplicitInitFlags_SembleEnvDoesNotForceWorkspaceInit(t *testing.T) {
	resetRootCmdForTest(t)
	defer resetRootCmdForTest(t)
	t.Setenv(semble.EnvEnableSemble, "true")

	if hasExplicitInitFlags(initCmd) {
		t.Fatal("hasExplicitInitFlags() = true for LIZA_ENABLE_SEMBLE=true, want false")
	}

	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	testhelpers.SetupGlobalLiza(t)

	err := executeRootCommand(t, projectRoot, "init", "--gemini")
	if err != nil {
		t.Fatalf("pairing init with Semble env failed: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(projectRoot, ".liza")); !os.IsNotExist(statErr) {
		t.Fatalf(".liza stat error = %v, want missing so Semble env does not force full workspace init", statErr)
	}
	linkTarget, err := os.Readlink(filepath.Join(projectRoot, "GEMINI.md"))
	if err != nil {
		t.Fatalf("GEMINI.md symlink missing after pairing init: %v", err)
	}
	if !strings.HasSuffix(linkTarget, filepath.Join(".liza", "CORE.md")) {
		t.Fatalf("GEMINI.md target = %q, want global CORE.md symlink", linkTarget)
	}
}

func TestInitDispatch_PairingScipSearchFlagIsAllowedWithAgent(t *testing.T) {
	resetRootCmdForTest(t)
	defer resetRootCmdForTest(t)

	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	testhelpers.SetupGlobalLiza(t)
	t.Setenv(scipsearch.EnvEnableScipSearch, "true")
	if err := os.WriteFile(filepath.Join(projectRoot, "go.mod"), []byte("module example.com/project\n"), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	testhelpers.MustGit(t, projectRoot, "add", "go.mod")
	testhelpers.MustGit(t, projectRoot, "commit", "-m", "Add Go module")

	err := executeRootCommand(t, projectRoot, "init", "--codex", "--scip-search", "go")
	if err != nil {
		t.Fatalf("pairing init with --scip-search failed: %v", err)
	}

	scriptPath := filepath.Join(projectRoot, ".git", "hooks", "liza-index.sh")
	script, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("liza-index.sh missing after pairing SCIP init: %v", err)
	}
	if !strings.Contains(string(script), "scip-go index --module-root") {
		t.Fatalf("liza-index.sh missing Go SCIP command:\n%s", string(script))
	}
	if _, statErr := os.Stat(filepath.Join(projectRoot, ".liza")); !os.IsNotExist(statErr) {
		t.Fatalf(".liza stat error = %v, want missing so --scip-search keeps pairing mode", statErr)
	}
}

func TestInitDispatch_PairingScipSearchPlanFlagWritesOverrideCommands(t *testing.T) {
	resetRootCmdForTest(t)
	defer resetRootCmdForTest(t)

	projectRoot := t.TempDir()
	projectRoot, err := filepath.EvalSymlinks(projectRoot)
	if err != nil {
		t.Fatalf("resolve temp project root: %v", err)
	}
	testhelpers.SetupTestGitRepo(t, projectRoot)
	testhelpers.SetupGlobalLiza(t)
	t.Setenv(scipsearch.EnvEnableScipSearch, "true")
	writeFile(t, filepath.Join(projectRoot, "services", "design-diagnosis", "cli", "go.mod"), "module example.com/cli\n")
	writeFile(t, filepath.Join(projectRoot, "services", "design-diagnosis", "cli", "main.go"), "package main\n")
	writeFile(t, filepath.Join(projectRoot, "apps", "web", "tsconfig.json"), "{}\n")
	writeFile(t, filepath.Join(projectRoot, "apps", "web", "src", "App.tsx"), "export const app = 1\n")
	writeFile(t, filepath.Join(projectRoot, "infra", "cdk", "tsconfig.json"), "{}\n")
	writeFile(t, filepath.Join(projectRoot, "infra", "cdk", "app.ts"), "export const cdk = 1\n")
	writeFile(t, filepath.Join(projectRoot, "apps", "api", "pyproject.toml"), "[project]\nname = \"api\"\n")
	writeFile(t, filepath.Join(projectRoot, "apps", "api", "backend", "main.py"), "print('api')\n")
	writeFile(t, filepath.Join(projectRoot, "services", "design-diagnosis", "pyproject.toml"), "[project]\nname = \"service\"\n")
	writeFile(t, filepath.Join(projectRoot, "services", "design-diagnosis", "app.py"), "print('service')\n")
	testhelpers.MustGit(t, projectRoot, "add", ".")
	testhelpers.MustGit(t, projectRoot, "commit", "-m", "Add monorepo roots")

	err = executeRootCommand(t, projectRoot,
		"init",
		"--codex",
		"--scip-search-plan", "go=services/design-diagnosis/cli",
		"--scip-search-plan", "typescript=apps/web/src,apps/web",
		"--scip-search-plan", "python=apps/api",
	)
	if err != nil {
		t.Fatalf("pairing init with --scip-search-plan failed: %v", err)
	}

	scriptPath := filepath.Join(projectRoot, ".git", "hooks", "liza-index.sh")
	script, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("liza-index.sh missing after pairing SCIP init: %v", err)
	}
	for _, want := range []string{
		"scip-go index --module-root " + projectRoot + "/services/design-diagnosis/cli --output ",
		"scip-typescript index --cwd " + projectRoot + "/apps/web/src --output ",
		"scip-python index --cwd " + projectRoot + "/apps/api --output ",
		"scip-search aggregate-index --project-root " + projectRoot,
		"--root services/design-diagnosis/cli --index ",
		"--root apps/web/src --index ",
		"--root apps/api --index ",
		"--out " + projectRoot + "/go.scip",
		"--out " + projectRoot + "/typescript.scip",
		"--out " + projectRoot + "/python.scip",
	} {
		if !strings.Contains(string(script), want) {
			t.Fatalf("liza-index.sh missing override command %q:\n%s", want, string(script))
		}
	}
}

func TestInitDispatch_ScipSearchPlanFlagRequiresPairingInit(t *testing.T) {
	resetRootCmdForTest(t)
	defer resetRootCmdForTest(t)

	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	testhelpers.SetupGlobalLiza(t)

	err := executeRootCommand(t, projectRoot, "init", "--scip-search-plan", "go=.", "Goal with invalid pairing plan")
	if err == nil {
		t.Fatal("full init with --scip-search-plan error = nil, want pairing-only rejection")
	}
	if !strings.Contains(err.Error(), "--scip-search-plan is only supported for pairing init") {
		t.Fatalf("full init error = %v, want pairing-only diagnostic", err)
	}
}

func TestInitDispatch_ScipSearchRepeatableFlagPersistsConfig(t *testing.T) {
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	testhelpers.SetupGlobalLiza(t)
	testhelpers.CreateCommittedSpecFile(t, projectRoot, "vision.md", "# Vision\n")
	testhelpers.CreateCommittedPreCommitConfig(t, projectRoot)
	testhelpers.MustGit(t, projectRoot, "branch", "-f", "integration", "HEAD")

	var calls []string
	restore := scipsearch.SetCommandRunnerForTest(func(name string, args ...string) (string, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return "ok\n", nil
	})
	defer restore()

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
	wantCalls := []string{
		"scip-search --help",
		"scip-search --version",
		"scip-go --help",
		"scip-typescript --help",
	}
	if !slices.Equal(calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", calls, wantCalls)
	}
}

func TestInitDispatch_FullInitSkipsScipSearchWhenEnvDisabled(t *testing.T) {
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	testhelpers.SetupGlobalLiza(t)
	testhelpers.CreateCommittedSpecFile(t, projectRoot, "vision.md", "# Vision\n")
	testhelpers.CreateCommittedPreCommitConfig(t, projectRoot)
	testhelpers.MustGit(t, projectRoot, "branch", "-f", "integration", "HEAD")
	t.Setenv(scipsearch.EnvEnableScipSearch, "false")

	var calls []string
	restore := scipsearch.SetCommandRunnerForTest(func(name string, args ...string) (string, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return "missing\n", os.ErrNotExist
	})
	defer restore()

	err := executeRootCommand(
		t,
		projectRoot,
		"init",
		"--spec",
		"specs/vision.md",
		"Goal with missing scip-search",
	)
	if err != nil {
		t.Fatalf("init failed with scip-search disabled: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(projectRoot, ".liza")); statErr != nil {
		t.Fatalf(".liza missing after init with scip-search disabled: stat err = %v", statErr)
	}
	if len(calls) != 0 {
		t.Fatalf("calls = %v, want no scip-search calls", calls)
	}
}

func TestInitDispatch_SembleEnabledFullInitThroughCobraHasNoDurableSurface(t *testing.T) {
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	fakeHome := testhelpers.SetupGlobalLiza(t)
	testhelpers.CreateCommittedSpecFile(t, projectRoot, "vision.md", "# Vision\n")
	testhelpers.CreateCommittedPreCommitConfig(t, projectRoot)
	testhelpers.MustGit(t, projectRoot, "branch", "-f", "integration", "HEAD")
	t.Setenv(semble.EnvEnableSemble, "true")

	logPath := filepath.Join(t.TempDir(), "semble.log")
	t.Setenv("SEMBLE_TEST_LOG", logPath)
	writeFakeSembleForTest(t, filepath.Join(fakeHome, "bin", "semble"))

	err := executeRootCommand(
		t,
		projectRoot,
		"init",
		"--spec",
		"specs/vision.md",
		"Goal with semantic env",
	)
	if err != nil {
		t.Fatalf("init with enabled Semble failed: %v", err)
	}

	logContent, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read fake Semble log: %v", err)
	}
	logText := string(logContent)
	if got := strings.Count(logText, "__liza_semble_prewarm__"); got != 2 {
		t.Fatalf("Semble invocation count = %d, want prewarm and offline validation; log:\n%s", got, logText)
	}
	for _, want := range []string{"search __liza_semble_prewarm__", "--top-k 1", "--content code", "HF_HUB_OFFLINE=1"} {
		if !strings.Contains(logText, want) {
			t.Fatalf("fake Semble log missing %q:\n%s", want, logText)
		}
	}

	statePath := filepath.Join(projectRoot, ".liza", "state.yaml")
	state, err := db.New(statePath).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	configJSON := marshalConfigForTest(t, state.Config)
	if strings.Contains(strings.ToLower(string(configJSON)), "semble") {
		t.Fatalf("state.Config contains Semble data: %s", string(configJSON))
	}
	stateYAML, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state.yaml: %v", err)
	}
	if strings.Contains(strings.ToLower(string(stateYAML)), "semble") {
		t.Fatalf("state.yaml contains Semble data:\n%s", string(stateYAML))
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

func executeInitHelpForTest(t *testing.T) (string, error) {
	t.Helper()

	var out strings.Builder
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs([]string{"init", "--help"})
	err := rootCmd.Execute()
	return out.String(), err
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func writeFakeSembleForTest(t *testing.T, path string) {
	t.Helper()

	script := `#!/bin/sh
{
  printf '%s\n' "$*"
  printf 'HF_HUB_OFFLINE=%s\n' "$HF_HUB_OFFLINE"
} >> "$SEMBLE_TEST_LOG"
exit 0
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake semble: %v", err)
	}
}

func marshalConfigForTest(t *testing.T, config any) []byte {
	t.Helper()

	content, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	return content
}
