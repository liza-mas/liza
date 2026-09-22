package agent

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// expectedPiGatePath mirrors the {{globalDir}} template resolution in
// launchTemplateVars so tests assert against the same absolute path the
// resolver produces.
func expectedPiGatePath(t *testing.T) string {
	t.Helper()
	home, err := paths.UserHomeDir()
	if err != nil || home == "" {
		t.Fatalf("resolve home dir: %v", err)
	}
	return filepath.Join(home, brand.RuntimeValues().GlobalDirName, "extensions", "liza-init-gate.ts")
}

func TestPiLaunchPlanReferencesGlobalInitGate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pi adapter tests use /bin/sh stubs")
	}

	gatePath := expectedPiGatePath(t)
	plan, err := ResolveLaunchPlan(LaunchPlanRequest{
		ToolName:      "pi",
		Prompt:        "do it",
		ProjectRoot:   t.TempDir(),
		RuntimeConfig: models.Config{},
	})
	if err != nil {
		t.Fatalf("ResolveLaunchPlan() error = %v", err)
	}
	wantArgs := []string{"-p", "-e", gatePath}
	if plan.Executable != "pi" || !slices.Equal(plan.Args, wantArgs) || !plan.UsesStdin {
		t.Fatalf("plan = exe %q args %v stdin %v, want exe %q args %v stdin true", plan.Executable, plan.Args, plan.UsesStdin, "pi", wantArgs)
	}

	logged, err := ResolveLaunchPlan(LaunchPlanRequest{
		ToolName:      "pi",
		Prompt:        "do it",
		OutputsDir:    "/logs",
		ProjectRoot:   t.TempDir(),
		RuntimeConfig: models.Config{},
	})
	if err != nil {
		t.Fatalf("ResolveLaunchPlan(logged) error = %v", err)
	}
	wantLogged := []string{"-p", "--mode", "json", "-e", gatePath}
	if !slices.Equal(logged.Args, wantLogged) || !logged.UsesStdin {
		t.Fatalf("logged plan = args %v stdin %v, want args %v stdin true", logged.Args, logged.UsesStdin, wantLogged)
	}
}

func TestDevinLaunchPlanSupportsModelOverride(t *testing.T) {
	config := models.Config{
		AgentTools: map[string]models.AgentToolConfig{
			"devin": {
				RunArgs:         []string{"--permission-mode", "dangerous", "--model", "swe-2-max", "-p", "{{prompt}}"},
				LoggedRunArgs:   []string{"--permission-mode", "dangerous", "--model", "swe-2-max", "-p", "{{prompt}}"},
				PromptTransport: PromptTransportArg,
			},
		},
	}
	plan, err := ResolveLaunchPlan(LaunchPlanRequest{
		ToolName:      "devin",
		Prompt:        "do it",
		ProjectRoot:   t.TempDir(),
		RuntimeConfig: config,
	})
	if err != nil {
		t.Fatalf("ResolveLaunchPlan() error = %v", err)
	}
	wantArgs := []string{"--permission-mode", "dangerous", "--model", "swe-2-max", "-p", "do it"}
	if plan.Executable != "devin" || !slices.Equal(plan.Args, wantArgs) || plan.UsesStdin {
		t.Fatalf("plan = exe %q args %v stdin %v, want exe %q args %v stdin false", plan.Executable, plan.Args, plan.UsesStdin, "devin", wantArgs)
	}
}

// writeRecordingStub installs a stub CLI that records its argv and stdin, plus
// the value of the given environment variable, then exits 0.
func writeRecordingStub(t *testing.T, binDir, name, envName string) (argsFile, stdinFile, envFile string) {
	t.Helper()
	argsFile = filepath.Join(t.TempDir(), name+"-args.txt")
	stdinFile = filepath.Join(t.TempDir(), name+"-stdin.txt")
	envFile = filepath.Join(t.TempDir(), name+"-env.txt")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" > " + shQuote(argsFile) + "\n" +
		"cat > " + shQuote(stdinFile) + "\n" +
		"printf '%s' \"$" + envName + "\" > " + shQuote(envFile) + "\n" +
		"echo 'stub done'\n"
	if err := os.WriteFile(filepath.Join(binDir, name), []byte(script), 0755); err != nil {
		t.Fatalf("write stub %s: %v", name, err)
	}
	return argsFile, stdinFile, envFile
}

func shQuote(path string) string {
	return "'" + strings.ReplaceAll(path, "'", "'\\''") + "'"
}

func TestCLIAgentRunsPiInSubprocess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pi adapter tests use /bin/sh stubs")
	}

	projectRoot := t.TempDir()
	binDir := t.TempDir()
	argsFile, stdinFile, _ := writeRecordingStub(t, binDir, "pi", "PI_RECORDED_ENV")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	executor := NewCLIAgent("")
	result, err := executor.Run(context.Background(), LLMAgentRunRequest{
		BackendName: "pi",
		AgentID:     "coder-pi-1",
		Prompt:      "read the contract then answer",
		ProjectRoot: projectRoot,
		LaunchGate:  immediateLaunchGate,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0 (output %q)", result.ExitCode, result.Output)
	}

	gotArgs, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	wantArgs := strings.Join([]string{"-p", "-e", expectedPiGatePath(t)}, " ") + "\n"
	if string(gotArgs) != wantArgs {
		t.Fatalf("pi argv = %q, want %q", string(gotArgs), wantArgs)
	}

	gotStdin, err := os.ReadFile(stdinFile)
	if err != nil {
		t.Fatalf("read stdin file: %v", err)
	}
	if string(gotStdin) != "read the contract then answer" {
		t.Fatalf("pi stdin = %q, want the prompt delivered on stdin", string(gotStdin))
	}
}

func TestCLIAgentRunsDevinWithSWE2MaxModelInSubprocess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("devin adapter tests use /bin/sh stubs")
	}

	projectRoot := t.TempDir()
	binDir := t.TempDir()
	argsFile, _, _ := writeRecordingStub(t, binDir, "devin", "DEVIN_RECORDED_ENV")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	config := models.Config{
		AgentTools: map[string]models.AgentToolConfig{
			"devin": {
				RunArgs:       []string{"--permission-mode", "dangerous", "--model", "swe-2-max", "-p", "{{prompt}}"},
				LoggedRunArgs: []string{"--permission-mode", "dangerous", "--model", "swe-2-max", "-p", "{{prompt}}"},
			},
		},
	}
	executor := NewCLIAgent("")
	result, err := executor.Run(context.Background(), LLMAgentRunRequest{
		BackendName:   "devin",
		AgentID:       "coder-devin-1",
		Prompt:        "ship it",
		ProjectRoot:   projectRoot,
		RuntimeConfig: config,
		LaunchGate:    immediateLaunchGate,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0 (output %q)", result.ExitCode, result.Output)
	}

	gotArgs, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	wantArgs := "--permission-mode dangerous --model swe-2-max -p ship it\n"
	if string(gotArgs) != wantArgs {
		t.Fatalf("devin argv = %q, want %q", string(gotArgs), wantArgs)
	}
}

func TestCLIAgentLoadsCodexEnvFileForAPIKey(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("codex adapter tests use /bin/sh stubs")
	}

	projectRoot := t.TempDir()
	binDir := t.TempDir()
	argsFile, stdinFile, envFile := writeRecordingStub(t, binDir, "codex", "ZAI_API_KEY")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ZAI_API_KEY", "")

	if err := os.WriteFile(filepath.Join(projectRoot, "codex.env"), []byte("# z.ai GLM coding plan key\nZAI_API_KEY=test-glm-key-123\n"), 0644); err != nil {
		t.Fatalf("write codex.env: %v", err)
	}

	executor := NewCLIAgent("")
	result, err := executor.Run(context.Background(), LLMAgentRunRequest{
		BackendName: "codex",
		AgentID:     "coder-codex-1",
		Prompt:      "reply with OK",
		ProjectRoot: projectRoot,
		LaunchGate:  immediateLaunchGate,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0 (output %q)", result.ExitCode, result.Output)
	}

	gotEnv, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	if string(gotEnv) != "test-glm-key-123" {
		t.Fatalf("codex subprocess ZAI_API_KEY = %q, want %q", string(gotEnv), "test-glm-key-123")
	}

	gotArgs, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(gotArgs)) != "exec -" {
		t.Fatalf("codex argv = %q, want %q", string(gotArgs), "exec -")
	}

	gotStdin, err := os.ReadFile(stdinFile)
	if err != nil {
		t.Fatalf("read stdin file: %v", err)
	}
	if string(gotStdin) != "reply with OK" {
		t.Fatalf("codex stdin = %q, want the prompt delivered on stdin", string(gotStdin))
	}
}
