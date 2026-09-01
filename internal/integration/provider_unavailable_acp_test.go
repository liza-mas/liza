package integration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

const (
	providerUnavailableTaskID  = "task-codex-acp-provider-unavailable"
	codexSessionStartupFailure = "Error: thread/start: thread/start failed: error creating thread: Fatal error: Codex cannot access session files at /tmp/.codex/sessions (permission denied)."
)

type deterministicLLMAgent struct {
	mu       sync.Mutex
	result   agent.LLMAgentRunResult
	backends []string
}

func (a *deterministicLLMAgent) Run(_ context.Context, req agent.LLMAgentRunRequest) (agent.LLMAgentRunResult, error) {
	a.mu.Lock()
	a.backends = append(a.backends, req.BackendName)
	a.mu.Unlock()
	return a.result, nil
}

func (*deterministicLLMAgent) RunInteractive(context.Context, agent.LLMAgentInteractiveRequest) (int, error) {
	return 0, errors.New("interactive mode is not supported by deterministic test agent")
}

func (a *deterministicLLMAgent) recordedBackends() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.backends...)
}

type providerUnavailableSupervisorFixture struct {
	projectRoot string
	statePath   string
	blackboard  *db.Blackboard
}

func newProviderUnavailableSupervisorFixture(t *testing.T) providerUnavailableSupervisorFixture {
	t.Helper()
	db.ResetInstances()
	t.Cleanup(db.ResetInstances)

	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.SetupPipelineConfig(t, projectRoot)

	state := testhelpers.CreateValidState()
	state.Config.CoderPollInterval = 1
	state.Config.DoerMaxWait = 1
	state.Config.LeaseDuration = 300
	state.Config.CrashRestartThreshold = 1
	state.Config.SpinningRestartThreshold = 10
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus(providerUnavailableTaskID, models.TaskStatusReady, time.Now().UTC()),
	}

	return providerUnavailableSupervisorFixture{
		projectRoot: projectRoot,
		statePath:   statePath,
		blackboard:  testhelpers.WriteInitialState(t, statePath, state),
	}
}

func runProviderUnavailableSupervisor(
	t *testing.T,
	fixture providerUnavailableSupervisorFixture,
	agentID string,
	cliName string,
	llmAgent agent.LLMAgent,
) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	lizaPaths := paths.New(fixture.projectRoot)
	if err := agent.RunSupervisor(ctx, agent.SupervisorConfig{
		AgentID:          agentID,
		Role:             models.RoleCoder,
		ProjectRoot:      fixture.projectRoot,
		StatePath:        fixture.statePath,
		LogPath:          lizaPaths.LogPath(),
		SpecsDir:         lizaPaths.SpecsDir(),
		CLIName:          cliName,
		LLMAgent:         llmAgent,
		ExecutionTimeout: 10 * time.Second,
	}); err != nil {
		t.Fatalf("RunSupervisor(%s, %s) error = %v", agentID, cliName, err)
	}
}

func writeProviderUnavailableFakeACPX(t *testing.T, executablePath, logPath string) {
	t.Helper()
	logPath = filepath.ToSlash(logPath)
	script := `#!/bin/sh
case "$*" in
  *" sessions show "*)
    exit 2
    ;;
  *" sessions ensure "*|*" set-mode "*)
    exit 0
    ;;
  *" prompt "*)
    cat >/dev/null
    printf '%s\n' 'PROMPT' >> "` + logPath + `"
    printf '%s\n' 'private transport context must not enter classification output' >&2
    printf '%s\n' '` + codexSessionStartupFailure + `' >&2
    exit 7
    ;;
  *)
    exit 2
    ;;
esac
`
	testhelpers.WriteShellStub(t, executablePath, script)
}

func TestCodexACPProviderUnavailableStopsSupervisorLifecycle(t *testing.T) {
	fixture := newProviderUnavailableSupervisorFixture(t)
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "acpx.log")
	writeProviderUnavailableFakeACPX(t, filepath.Join(binDir, "acpx"), logPath)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	runProviderUnavailableSupervisor(t, fixture, "coder-1", "codex-acp", agent.NewACPXAgent(""))

	promptLog, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read fake acpx log: %v", err)
	}
	if got := strings.Count(string(promptLog), "PROMPT\n"); got != 1 {
		t.Fatalf("provider prompt executions = %d, want one before generic restart; log=%q", got, promptLog)
	}

	state, err := fixture.blackboard.Read()
	if err != nil {
		t.Fatalf("blackboard.Read() error = %v", err)
	}
	task := state.FindTask(providerUnavailableTaskID)
	if task == nil {
		t.Fatalf("task %q not found", providerUnavailableTaskID)
	}
	if task.BlockedReason != nil && strings.Contains(*task.BlockedReason, "crash restart loop detected") {
		t.Fatalf("BlockedReason = %q, provider classification must win before generic crash handling", *task.BlockedReason)
	}

	canonicalSignal := agent.ProviderUnavailableSignalPath(fixture.projectRoot, "codex")
	signalData, err := os.ReadFile(canonicalSignal)
	if err != nil {
		t.Fatalf("read canonical provider-unavailable signal: %v", err)
	}
	signal := string(signalData)
	if !strings.Contains(signal, "provider: codex\n") || !strings.Contains(signal, "thread/start failed") || !strings.Contains(signal, ".codex/sessions") {
		t.Fatalf("canonical signal missing provider or representative failure:\n%s", signal)
	}
	if strings.Contains(signal, "private transport context") {
		t.Fatalf("canonical signal exposed unrelated stderr:\n%s", signal)
	}
	rawACPSignal := filepath.Join(filepath.Dir(canonicalSignal), "provider-unavailable-codex-acp")
	if _, err := os.Stat(rawACPSignal); !os.IsNotExist(err) {
		t.Fatalf("raw ACP provider-unavailable signal state = %v, want absent", err)
	}

	alertsData, err := os.ReadFile(paths.New(fixture.projectRoot).AlertsLogPath())
	if err != nil {
		t.Fatalf("read alerts log: %v", err)
	}
	alerts := string(alertsData)
	if count := strings.Count(alerts, "PROVIDER UNAVAILABLE"); count != 1 {
		t.Fatalf("PROVIDER UNAVAILABLE alert count = %d, want 1:\n%s", count, alerts)
	}
	if !strings.Contains(alerts, "PROVIDER UNAVAILABLE — codex:") || !strings.Contains(alerts, ".codex/sessions") {
		t.Fatalf("alerts log missing canonical Codex provider failure:\n%s", alerts)
	}
	if strings.Contains(alerts, "private transport context") {
		t.Fatalf("alerts log exposed unrelated stderr:\n%s", alerts)
	}

	for _, supervisor := range []struct {
		name    string
		agentID string
		cliName string
	}{
		{name: "native codex", agentID: "coder-2", cliName: "codex"},
		{name: "ACP codex", agentID: "coder-3", cliName: "codex-acp"},
	} {
		t.Run(supervisor.name, func(t *testing.T) {
			observer := &deterministicLLMAgent{}
			runProviderUnavailableSupervisor(t, fixture, supervisor.agentID, supervisor.cliName, observer)
			if got := observer.recordedBackends(); len(got) != 0 {
				t.Fatalf("provider executions = %v, want none while canonical signal exists", got)
			}
		})
	}
}

func TestCodexACPProviderUnavailableLeavesUnrelatedFailureOnCrashPath(t *testing.T) {
	fixture := newProviderUnavailableSupervisorFixture(t)
	failingAgent := &deterministicLLMAgent{
		result: agent.LLMAgentRunResult{ExitCode: 1, Output: "unrelated provider transport failure"},
	}

	runProviderUnavailableSupervisor(t, fixture, "coder-1", "codex-acp", failingAgent)

	if got := failingAgent.recordedBackends(); len(got) != 2 || got[0] != "codex-acp" || got[1] != "codex-acp" {
		t.Fatalf("provider executions = %v, want two codex-acp executions through generic crash handling", got)
	}

	state, err := fixture.blackboard.Read()
	if err != nil {
		t.Fatalf("blackboard.Read() error = %v", err)
	}
	task := state.FindTask(providerUnavailableTaskID)
	if task == nil {
		t.Fatalf("task %q not found", providerUnavailableTaskID)
	}
	if task.Status != models.TaskStatusBlocked {
		t.Fatalf("task status = %s, want BLOCKED", task.Status)
	}
	if task.BlockedReason == nil || !strings.Contains(*task.BlockedReason, "crash restart loop detected") {
		t.Fatalf("BlockedReason = %v, want generic crash restart loop reason", task.BlockedReason)
	}

	canonicalSignal := agent.ProviderUnavailableSignalPath(fixture.projectRoot, "codex")
	for _, signalPath := range []string{
		canonicalSignal,
		filepath.Join(filepath.Dir(canonicalSignal), "provider-unavailable-codex-acp"),
	} {
		if _, err := os.Stat(signalPath); !os.IsNotExist(err) {
			t.Fatalf("provider-unavailable signal %q state = %v, want absent", signalPath, err)
		}
	}

	alertsData, err := os.ReadFile(paths.New(fixture.projectRoot).AlertsLogPath())
	if err != nil {
		t.Fatalf("read alerts log: %v", err)
	}
	alerts := string(alertsData)
	if strings.Contains(alerts, "PROVIDER UNAVAILABLE") {
		t.Fatalf("unrelated failure wrote provider-unavailable alert:\n%s", alerts)
	}
	if !strings.Contains(alerts, "CRASH RESTART LOOP") {
		t.Fatalf("alerts log missing generic crash restart alert:\n%s", alerts)
	}
}
