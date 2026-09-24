package process

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestBuildSpawnCommand_PassesExpectedArgs(t *testing.T) {
	projectRoot := t.TempDir()
	cmd, devNull, readyPath, err := buildSpawnCommand(projectRoot, "coder", "codex", "--agent-id", "coder-9")
	if err != nil {
		t.Fatalf("buildSpawnCommand() error = %v", err)
	}
	defer devNull.Close()
	defer os.Remove(readyPath)

	args, logPaths := splitSupervisorLogArgs(t, cmd.Args)
	wantArgs := []string{"liza", "agent", "coder", "--cli", "codex", "--agent-id", "coder-9"}
	if len(args) != len(wantArgs) {
		t.Fatalf("len(args) = %d, want %d (%v)", len(args), len(wantArgs), args)
	}
	for i, want := range wantArgs {
		if args[i] != want {
			t.Fatalf("args[%d] = %q, want %q (all args: %v)", i, args[i], want, args)
		}
	}
	assertSupervisorLogPaths(t, projectRoot, "coder", logPaths)

	if cmd.Dir != projectRoot {
		t.Fatalf("Dir = %q, want %q", cmd.Dir, projectRoot)
	}
}

func TestBuildSpawnCommand_AddsGoalIDFromState(t *testing.T) {
	projectRoot := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	state := testhelpers.CreateValidState()
	state.Goal.ID = "goal-xyz"
	testhelpers.WriteInitialState(t, statePath, state)

	cmd, devNull, readyPath, err := buildSpawnCommand(projectRoot, "coder", "codex")
	if err != nil {
		t.Fatalf("buildSpawnCommand() error = %v", err)
	}
	defer devNull.Close()
	defer os.Remove(readyPath)

	args, _ := splitSupervisorLogArgs(t, cmd.Args)
	wantArgs := []string{"liza", "agent", "coder", "--cli", "codex", "--goal-id", "goal-xyz"}
	if strings.Join(args, " ") != strings.Join(wantArgs, " ") {
		t.Fatalf("args = %v, want %v", args, wantArgs)
	}
}

func TestBuildSpawnCommand_DoesNotOverrideExplicitGoalID(t *testing.T) {
	projectRoot := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	state := testhelpers.CreateValidState()
	state.Goal.ID = "goal-xyz"
	testhelpers.WriteInitialState(t, statePath, state)

	cmd, devNull, readyPath, err := buildSpawnCommand(projectRoot, "coder", "codex", "--goal-id", "manual-goal")
	if err != nil {
		t.Fatalf("buildSpawnCommand() error = %v", err)
	}
	defer devNull.Close()
	defer os.Remove(readyPath)

	args, _ := splitSupervisorLogArgs(t, cmd.Args)
	wantArgs := []string{"liza", "agent", "coder", "--cli", "codex", "--goal-id", "manual-goal"}
	if strings.Join(args, " ") != strings.Join(wantArgs, " ") {
		t.Fatalf("args = %v, want %v", args, wantArgs)
	}
}

func TestBuildSpawnCommand_BindsAllStdioToDevNull(t *testing.T) {
	projectRoot := t.TempDir()
	cmd, devNull, readyPath, err := buildSpawnCommand(projectRoot, "coder", "codex")
	if err != nil {
		t.Fatalf("buildSpawnCommand() error = %v", err)
	}
	defer devNull.Close()
	defer os.Remove(readyPath)

	for name, stream := range map[string]any{
		"stdin":  cmd.Stdin,
		"stdout": cmd.Stdout,
		"stderr": cmd.Stderr,
	} {
		file, ok := stream.(*os.File)
		if !ok {
			t.Fatalf("%s type = %T, want *os.File to preserve detached lifetime", name, stream)
		}
		if file.Name() != os.DevNull {
			t.Fatalf("%s file = %q, want %q", name, file.Name(), os.DevNull)
		}
	}
}

func TestBuildSpawnCommand_DetachedChildSurvivesSpawnerExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("helper uses a POSIX shell")
	}

	projectRoot := t.TempDir()
	binDir := t.TempDir()
	sentinel := filepath.Join(t.TempDir(), "child-survived")
	fakeBinary := filepath.Join(binDir, brand.BinaryName)
	script := "#!/bin/sh\nset -e\nsleep 0.3\nprintf 'detached child output\\n'\nprintf survived > \"$LIZA_TEST_DETACHED_SENTINEL\"\n"
	if err := os.WriteFile(fakeBinary, []byte(script), 0755); err != nil {
		t.Fatalf("write fake agent binary: %v", err)
	}

	spawner := exec.Command(os.Args[0], "-test.run=^TestDetachedSpawnerHelper$")
	spawner.Env = append(os.Environ(),
		"LIZA_TEST_DETACHED_SPAWNER=1",
		"LIZA_TEST_DETACHED_PROJECT="+projectRoot,
		"LIZA_TEST_DETACHED_SENTINEL="+sentinel,
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	if output, err := spawner.CombinedOutput(); err != nil {
		t.Fatalf("helper spawner failed: %v\n%s", err, output)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		data, err := os.ReadFile(sentinel)
		if err == nil {
			if string(data) != "survived" {
				t.Fatalf("sentinel = %q, want survived", data)
			}
			break
		}
		if !os.IsNotExist(err) {
			t.Fatalf("read sentinel: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("detached child did not write after its spawner exited")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestDetachedSpawnerHelper(t *testing.T) {
	if os.Getenv("LIZA_TEST_DETACHED_SPAWNER") != "1" {
		return
	}

	cmd, devNull, _, err := buildSpawnCommand(os.Getenv("LIZA_TEST_DETACHED_PROJECT"), "coder", "codex")
	if err != nil {
		t.Fatalf("buildSpawnCommand() error = %v", err)
	}
	if err := cmd.Start(); err != nil {
		devNull.Close()
		t.Fatalf("start detached child: %v", err)
	}
	if err := devNull.Close(); err != nil {
		t.Fatalf("close parent devnull: %v", err)
	}
}

func TestSpawnAgentWaitsForChildLoggingReadiness(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake agent binary uses a POSIX shell")
	}

	readySentinel := filepath.Join(t.TempDir(), "child-ready")
	projectRoot := setupSpawnHandshakeTest(t, `#!/bin/sh
set -e
ready_file=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--supervisor-ready-file" ]; then
    ready_file="$2"
    shift 2
  else
    shift
  fi
done
sleep 0.3
printf ready > "$LIZA_TEST_READY_SENTINEL"
printf 'ready\n' > "$ready_file"
sleep 2
`)
	t.Setenv("LIZA_TEST_READY_SENTINEL", readySentinel)

	cmd, err := SpawnAgent(projectRoot, "coder", "fake")
	if err != nil {
		t.Fatalf("SpawnAgent() error = %v", err)
	}
	defer cmd.Process.Kill()
	if data, err := os.ReadFile(readySentinel); err != nil || string(data) != "ready" {
		t.Fatalf("SpawnAgent returned before child readiness marker: data=%q err=%v", data, err)
	}
}

func TestSpawnAgentReturnsChildBootstrapFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake agent binary uses a POSIX shell")
	}

	projectRoot := setupSpawnHandshakeTest(t, `#!/bin/sh
set -e
ready_file=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--supervisor-ready-file" ]; then
    ready_file="$2"
    shift 2
  else
    shift
  fi
done
printf 'error: cannot open masked supervisor logs\n' > "$ready_file"
exit 1
`)

	cmd, err := SpawnAgent(projectRoot, "coder", "fake")
	if err == nil {
		t.Fatal("SpawnAgent() error = nil, want bootstrap failure")
	}
	if cmd != nil {
		t.Fatalf("SpawnAgent() command = %#v, want nil", cmd)
	}
	if !strings.Contains(err.Error(), "supervisor bootstrap failed: cannot open masked supervisor logs") {
		t.Fatalf("SpawnAgent() error = %q, want child bootstrap detail", err)
	}
}

func setupSpawnHandshakeTest(t *testing.T, script string) string {
	t.Helper()
	projectRoot := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	state := testhelpers.CreateValidState()
	state.Config.AgentTools = map[string]models.AgentToolConfig{
		"fake": {
			Executable:  "unused",
			ContractKey: "none",
		},
	}
	testhelpers.WriteInitialState(t, statePath, state)

	binDir := t.TempDir()
	fakeBinary := filepath.Join(binDir, brand.BinaryName)
	if err := os.WriteFile(fakeBinary, []byte(script), 0755); err != nil {
		t.Fatalf("write fake agent binary: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return projectRoot
}

func splitSupervisorLogArgs(t *testing.T, args []string) ([]string, map[string]string) {
	t.Helper()
	clean := make([]string, 0, len(args))
	paths := make(map[string]string, 3)
	for i := 0; i < len(args); i++ {
		name := strings.TrimPrefix(args[i], "--")
		if name == agent.SupervisorStdoutLogFlag || name == agent.SupervisorStderrLogFlag || name == agent.SupervisorReadyFileFlag {
			if i+1 >= len(args) {
				t.Fatalf("missing value after %s", args[i])
			}
			paths[name] = args[i+1]
			i++
			continue
		}
		clean = append(clean, args[i])
	}
	if len(paths) != 3 {
		t.Fatalf("supervisor paths = %v, want stdout, stderr, and readiness", paths)
	}
	return clean, paths
}

func assertSupervisorLogPaths(t *testing.T, projectRoot, role string, logPaths map[string]string) {
	t.Helper()
	outputsDir := filepath.Join(projectRoot, paths.ProjectDirName(), "agent-outputs")
	for flag, suffix := range map[string]string{
		agent.SupervisorStdoutLogFlag: ".stdout.log",
		agent.SupervisorStderrLogFlag: ".stderr.log",
		agent.SupervisorReadyFileFlag: ".ready",
	} {
		path := logPaths[flag]
		if filepath.Dir(path) != outputsDir {
			t.Fatalf("--%s directory = %q, want %q", flag, filepath.Dir(path), outputsDir)
		}
		base := strings.TrimPrefix(filepath.Base(path), ".")
		if !strings.HasPrefix(base, "supervisor-"+role+"-") || !strings.HasSuffix(base, suffix) {
			t.Fatalf("--%s path = %q, want .?supervisor-%s-*%s", flag, path, role, suffix)
		}
	}
}

func TestSpawnAgent_QuotaSignalBlocksSpawnAndAlerts(t *testing.T) {
	projectRoot := t.TempDir()
	lizaDir := filepath.Join(projectRoot, paths.ProjectDirName())
	if err := os.MkdirAll(lizaDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := agent.WriteQuotaSignal(projectRoot, "codex", "quota hit"); err != nil {
		t.Fatal(err)
	}

	cmd, err := SpawnAgent(projectRoot, "coder", "codex")
	if err == nil {
		t.Fatal("SpawnAgent error = nil, want quota refusal")
	}
	if cmd != nil {
		t.Fatalf("SpawnAgent command = %#v, want nil", cmd)
	}
	if !strings.Contains(err.Error(), "provider quota exhausted for codex") {
		t.Fatalf("SpawnAgent error = %q, want quota refusal", err)
	}

	alertsPath := filepath.Join(lizaDir, "alerts.log")
	data, readErr := os.ReadFile(alertsPath)
	if readErr != nil {
		t.Fatalf("failed to read alerts log: %v", readErr)
	}
	alerts := string(data)
	if !strings.Contains(alerts, "PROVIDER QUOTA SPAWN BLOCKED") {
		t.Fatalf("alerts log missing spawn-blocked alert:\n%s", alerts)
	}
	if !strings.Contains(alerts, "codex: refused to spawn coder while quota signal is set") {
		t.Fatalf("alerts log missing spawn-blocked details:\n%s", alerts)
	}
	if !strings.Contains(alerts, "delete the flag file or run liza pause then liza resume") {
		t.Fatalf("alerts log missing recovery hint:\n%s", alerts)
	}
}

func TestSpawnAgent_ExpiredQuotaSignalNoLongerBlocksSpawn(t *testing.T) {
	// GIVEN a quota signal whose recorded expiry has passed, and a
	// provider-unavailable signal that stops the spawn at the next gate
	projectRoot := t.TempDir()
	lizaDir := filepath.Join(projectRoot, paths.ProjectDirName())
	if err := os.MkdirAll(lizaDir, 0755); err != nil {
		t.Fatal(err)
	}
	past := time.Now().UTC().Add(-time.Hour)
	expired := "provider: codex\n" +
		"detected: " + past.Add(-time.Hour).Format(time.RFC3339) + "\n" +
		"message: You've hit your usage limit\n" +
		"resets_at: " + past.Format(time.RFC3339) + "\n" +
		"expires: " + past.Format(time.RFC3339) + "\n"
	if err := os.WriteFile(agent.QuotaSignalPath(projectRoot, "codex"), []byte(expired), 0644); err != nil {
		t.Fatal(err)
	}
	if err := agent.WriteProviderUnavailableSignal(projectRoot, "codex", "session access denied"); err != nil {
		t.Fatal(err)
	}

	// WHEN auto-repair spawns a coder
	cmd, err := SpawnAgent(projectRoot, "coder", "codex")

	// THEN the quota gate is passed and the provider-unavailable gate refuses
	if cmd != nil {
		t.Fatalf("SpawnAgent command = %#v, want nil", cmd)
	}
	if err == nil || !strings.Contains(err.Error(), "provider unavailable for codex") {
		t.Fatalf("SpawnAgent error = %v, want the provider-unavailable refusal after the quota gate", err)
	}
	data, readErr := os.ReadFile(filepath.Join(lizaDir, "alerts.log"))
	if readErr != nil {
		t.Fatalf("failed to read alerts log: %v", readErr)
	}
	if strings.Contains(string(data), "PROVIDER QUOTA SPAWN BLOCKED") {
		t.Fatalf("expired quota signal still blocked the spawn:\n%s", data)
	}
}

func TestSpawnAgent_ProviderUnavailableSignalBlocksSpawnAndAlerts(t *testing.T) {
	projectRoot := t.TempDir()
	lizaDir := filepath.Join(projectRoot, paths.ProjectDirName())
	if err := os.MkdirAll(lizaDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := agent.WriteProviderUnavailableSignal(projectRoot, "codex", "session access denied"); err != nil {
		t.Fatal(err)
	}

	cmd, err := SpawnAgent(projectRoot, "coder", "codex")
	if err == nil {
		t.Fatal("SpawnAgent error = nil, want provider-unavailable refusal")
	}
	if cmd != nil {
		t.Fatalf("SpawnAgent command = %#v, want nil", cmd)
	}
	if !strings.Contains(err.Error(), "provider unavailable for codex") {
		t.Fatalf("SpawnAgent error = %q, want provider-unavailable refusal", err)
	}

	alertsPath := filepath.Join(lizaDir, "alerts.log")
	data, readErr := os.ReadFile(alertsPath)
	if readErr != nil {
		t.Fatalf("failed to read alerts log: %v", readErr)
	}
	alerts := string(data)
	if !strings.Contains(alerts, "PROVIDER UNAVAILABLE SPAWN BLOCKED") {
		t.Fatalf("alerts log missing spawn-blocked alert:\n%s", alerts)
	}
	if !strings.Contains(alerts, "codex: refused to spawn coder while provider-unavailable signal is set") {
		t.Fatalf("alerts log missing spawn-blocked details:\n%s", alerts)
	}
	if !strings.Contains(alerts, "repair the provider, then delete the flag file or run liza pause then liza resume") {
		t.Fatalf("alerts log missing recovery hint:\n%s", alerts)
	}
}

func TestSpawnAgent_CodexACPRequiresACPXBeforeSpawn(t *testing.T) {
	projectRoot := t.TempDir()
	t.Setenv("PATH", t.TempDir())

	cmd, err := SpawnAgent(projectRoot, "coder", "codex-acp")
	if err == nil {
		t.Fatal("SpawnAgent error = nil, want acpx prerequisite error")
	}
	if cmd != nil {
		t.Fatalf("SpawnAgent command = %#v, want nil", cmd)
	}
	if !strings.Contains(err.Error(), "spawn coder with codex-acp") {
		t.Fatalf("SpawnAgent error = %q, want spawn context", err)
	}
	if !strings.Contains(err.Error(), "npm install -g acpx") {
		t.Fatalf("SpawnAgent error = %q, want install hint", err)
	}
}
