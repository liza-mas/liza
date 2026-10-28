package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/providersignal"
)

// C3: the classified-crash writer records the launch tool, and the supervisor
// admission check and the orchestrator's final wake revalidation stop on C2
// provider keys of the configuration they decide under.

const c3CodexStartupFailure = "Error: thread/start: thread/start failed: error creating thread: Fatal error: Codex cannot access session files at /tmp/.codex/sessions (permission denied)."

var (
	c3SplitAlias = models.Config{AgentTools: map[string]models.AgentToolConfig{"codex-acp": {ProviderKey: "acp-lane"}}}
	c3SharedKeys = models.Config{AgentTools: map[string]models.AgentToolConfig{
		"foo": {ProviderKey: "shared"}, "bar": {ProviderKey: "shared"}, "baz": {ProviderKey: "other"},
	}}
)

func c3Project(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, paths.ProjectDirName()), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestC3CrashWriterRaiseStopsAliasSupervisorBothWays(t *testing.T) {
	for _, tc := range []struct{ raiser, stopped string }{{"codex", "codex-acp"}, {"codex-acp", "codex"}} {
		t.Run(tc.raiser+" raise stops "+tc.stopped, func(t *testing.T) {
			root := c3Project(t)
			if !handleClassifiedProviderCrash(SupervisorConfig{AgentID: "coder-1", ProjectRoot: root, CLIName: tc.raiser}, c3CodexStartupFailure) {
				t.Fatal("startup failure not classified as a provider crash")
			}
			stopped := handleProviderUnavailableSignal(SupervisorConfig{AgentID: "coder-2", ProjectRoot: root, CLIName: tc.stopped}, providerKeysOfConfig(models.Config{}))
			if !stopped {
				t.Fatalf("%s supervisor must stop on the signal raised by %s", tc.stopped, tc.raiser)
			}
			if handleProviderUnavailableSignal(SupervisorConfig{AgentID: "coder-3", ProjectRoot: root, CLIName: "claude"}, providerKeysOfConfig(models.Config{})) {
				t.Fatal("an unrelated provider must keep running")
			}
		})
	}
}

func TestC3CrashWriterKeepsOriginatingToolSeparateFromLabel(t *testing.T) {
	root := c3Project(t)
	handleClassifiedProviderCrash(SupervisorConfig{AgentID: "coder-1", ProjectRoot: root, CLIName: "codex-acp"}, c3CodexStartupFailure)
	signals := providersignal.ListProviderUnavailableSignals(root)
	if len(signals) != 1 || signals[0].Tool != "codex-acp" || signals[0].Provider != "codex" {
		t.Fatalf("records = %+v, want tool codex-acp displayed as codex", signals)
	}
	// The alert keeps the classifier label, as before.
	alerts, err := os.ReadFile(paths.New(root).AlertsLogPath())
	if err != nil || !strings.Contains(string(alerts), "PROVIDER UNAVAILABLE — codex:") {
		t.Fatalf("alert = %q (%v), want the classifier label", alerts, err)
	}
}

func TestC3SupervisorDecidesUnderSnapshotConfigAtCheckTime(t *testing.T) {
	root := t.TempDir()
	bb := newOrchestratorScipTestBlackboard(t, root, nil)
	handleClassifiedProviderCrash(SupervisorConfig{AgentID: "coder-1", ProjectRoot: root, CLIName: "codex"}, c3CodexStartupFailure)
	config := SupervisorConfig{AgentID: "coder-2", ProjectRoot: root, CLIName: "codex-acp"}

	if !handleProviderUnavailableSignal(config, providerKeysOfSnapshot(context.Background(), bb)) {
		t.Fatal("default configuration: the alias supervisor must stop")
	}
	setAgentTools(t, bb, c3SplitAlias.AgentTools)
	if handleProviderUnavailableSignal(config, providerKeysOfSnapshot(context.Background(), bb)) {
		t.Fatal("the published configuration separates the alias; the same record must no longer stop it")
	}
	setAgentTools(t, bb, nil)
	if !handleProviderUnavailableSignal(config, providerKeysOfSnapshot(context.Background(), bb)) {
		t.Fatal("keys are derived at evaluation time: restoring the configuration restores the stop")
	}
}

func setAgentTools(t *testing.T, bb *db.Blackboard, tools map[string]models.AgentToolConfig) {
	t.Helper()
	if err := bb.Modify(func(s *models.State) error {
		s.Config.AgentTools = tools
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestC3SupervisorAndOrchestratorWakeAgree(t *testing.T) {
	type fixture struct {
		name   string
		config models.Config
		raiser string
		tool   string
		stop   bool
	}
	for _, f := range []fixture{
		{"alias codex to codex-acp", models.Config{}, "codex", "codex-acp", true},
		{"alias codex-acp to codex", models.Config{}, "codex-acp", "codex", true},
		{"custom shared key", c3SharedKeys, "foo", "bar", true},
		{"custom unrelated key", c3SharedKeys, "foo", "baz", false},
		{"override separates aliases", c3SplitAlias, "codex", "codex-acp", false},
	} {
		t.Run(f.name, func(t *testing.T) {
			root := t.TempDir()
			bb := newOrchestratorScipTestBlackboard(t, root, func(s *models.State) {
				s.Config.AgentTools = f.config.AgentTools
				s.HumanNotes = []models.HumanNote{{For: "all", Message: "Inspect the run", Timestamp: time.Now().UTC()}}
			})
			if err := providersignal.WriteProviderUnavailableSignal(root, &providersignal.ProviderUnavailable{Provider: f.raiser, Tool: f.raiser, Message: "raised"}); err != nil {
				t.Fatal(err)
			}
			config := orchestratorScipConfig(root)
			config.CLIName = f.tool

			supervisorStops := handleProviderUnavailableSignal(config, providerKeysOfSnapshot(context.Background(), bb))
			strategy := &orchestratorStrategy{wake: &OrchestratorWakeResult{Trigger: WakeTriggerHumanNote, Count: 1}}
			launch, err := strategy.RevalidateWake(context.Background(), bb, mustReadState(t, bb), config)
			if err != nil {
				t.Fatal(err)
			}
			if supervisorStops != f.stop || launch == f.stop {
				t.Fatalf("supervisor stops=%v, wake revalidation launches=%v, want stop=%v for both", supervisorStops, launch, f.stop)
			}
		})
	}
}
