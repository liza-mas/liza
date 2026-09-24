package commands

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	lizalog "github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// setupOrchestratorRepairProject writes a running goal with the given agents
// and no tasks, so the orchestrator is the only possible repair demand.
func setupOrchestratorRepairProject(t *testing.T, agents map[string]models.Agent) (string, *models.State) {
	t.Helper()
	projectRoot := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.SetupPipelineConfig(t, projectRoot)
	state := orchestratorTestState(agents)
	testhelpers.WriteInitialState(t, stateFile, state)
	return projectRoot, state
}

func rewriteOrchestratorRepairState(t *testing.T, projectRoot string, state *models.State) {
	t.Helper()
	testhelpers.WriteInitialState(t, paths.New(projectRoot).StatePath(), state)
}

func agedOrchestratorAbsence(age time.Duration) map[string]time.Time {
	return map[string]time.Time{orchestratorMissingSinceKey: time.Now().UTC().Add(-age)}
}

func runOrchestratorAutoRepair(projectRoot string, state *models.State, cache map[string]time.Time) AutoRepairAgentPoolOutcome {
	return RunAutoRepairAgentPool(context.Background(), state, WatchConfig{
		ProjectRoot: projectRoot,
		StateCache:  cache,
		WarnWriter:  io.Discard,
	})
}

func spawnedRoles(calls []spawnedAgentCall) []string {
	roles := make([]string, 0, len(calls))
	for _, call := range calls {
		roles = append(roles, call.role)
	}
	return roles
}

func TestRunAutoRepairAgentPool_RestartsMissingOrchestratorAfterGrace(t *testing.T) {
	unsetAutoRepairAgentPoolEnv(t)
	isolateOrchestratorProcfs(t)
	projectRoot, state := setupOrchestratorRepairProject(t, nil)
	var calls []spawnedAgentCall
	withFakeRepairSpawner(t, &calls, nil)

	outcome := runOrchestratorAutoRepair(projectRoot, state, agedOrchestratorAbsence(2*time.Minute))

	if !slices.Equal(spawnedRoles(calls), []string{"orchestrator"}) {
		t.Fatalf("spawned roles = %v, want [orchestrator]", spawnedRoles(calls))
	}
	if !slices.Contains(outcome.AttemptedRoles, "orchestrator") || len(outcome.Alerts) != 0 {
		t.Fatalf("outcome = %+v, want orchestrator attempted without alerts", outcome)
	}
	entries, err := lizalog.New(paths.New(projectRoot).LogPath()).Read()
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if len(entries) != 1 || entries[0].Action != "auto_repair_agent_spawned" ||
		!strings.Contains(entries[0].Detail, brand.Command("agent", "orchestrator")+" --cli") {
		t.Fatalf("log entries = %+v, want one orchestrator auto_repair_agent_spawned", entries)
	}
}

func TestRunAutoRepairAgentPool_WaitsForOrchestratorGrace(t *testing.T) {
	unsetAutoRepairAgentPoolEnv(t)
	isolateOrchestratorProcfs(t)
	projectRoot, state := setupOrchestratorRepairProject(t, nil)
	var calls []spawnedAgentCall
	withFakeRepairSpawner(t, &calls, nil)

	cache := make(map[string]time.Time)
	runOrchestratorAutoRepair(projectRoot, state, cache)
	if len(calls) != 0 {
		t.Fatalf("first observation spawned %v, want nothing", spawnedRoles(calls))
	}
	if _, ok := cache[orchestratorMissingSinceKey]; !ok {
		t.Fatalf("first observation did not start the absence episode; cache = %v", cache)
	}

	runOrchestratorAutoRepair(projectRoot, state, agedOrchestratorAbsence(45*time.Second))
	if len(calls) != 0 {
		t.Fatalf("absence within grace spawned %v, want nothing", spawnedRoles(calls))
	}
}

func TestRunAutoRepairAgentPool_OrchestratorPresenceIsLeaseFirst(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name      string
		agent     models.Agent
		wantSpawn bool
	}{
		{
			name:      "fresh lease with dead pid holds ownership",
			agent:     occupiedAgent("orchestrator", now, 987654321),
			wantSpawn: false,
		},
		{
			name: "expired lease is replaced",
			agent: models.Agent{
				Role:         "orchestrator",
				Status:       models.AgentStatusIdle,
				LeaseExpires: testhelpers.TimePtr(now.Add(-3 * time.Minute)),
				Heartbeat:    now.Add(-31 * time.Minute),
				PID:          987654321,
			},
			wantSpawn: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unsetAutoRepairAgentPoolEnv(t)
			isolateOrchestratorProcfs(t)
			projectRoot, state := setupOrchestratorRepairProject(t, map[string]models.Agent{"orchestrator-1": tt.agent})
			var calls []spawnedAgentCall
			withFakeRepairSpawner(t, &calls, nil)

			runOrchestratorAutoRepair(projectRoot, state, agedOrchestratorAbsence(2*time.Minute))

			if got := slices.Contains(spawnedRoles(calls), "orchestrator"); got != tt.wantSpawn {
				t.Fatalf("orchestrator spawned = %v, want %v (calls %v)", got, tt.wantSpawn, spawnedRoles(calls))
			}
		})
	}
}

func TestRunAutoRepairAgentPool_OrchestratorNotRequiredEndsEpisode(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*models.State)
	}{
		{"system paused", func(s *models.State) { s.Config.Mode = models.SystemModePaused }},
		{"goal completed", func(s *models.State) { s.Goal.Status = models.GoalStatusCompleted }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unsetAutoRepairAgentPoolEnv(t)
			isolateOrchestratorProcfs(t)
			projectRoot, state := setupOrchestratorRepairProject(t, nil)
			tt.mutate(state)
			rewriteOrchestratorRepairState(t, projectRoot, state)
			var calls []spawnedAgentCall
			withFakeRepairSpawner(t, &calls, nil)
			cache := agedOrchestratorAbsence(2 * time.Minute)

			runOrchestratorAutoRepair(projectRoot, state, cache)

			if len(calls) != 0 {
				t.Fatalf("spawned %v while no orchestrator is required", spawnedRoles(calls))
			}
			if _, ok := cache[orchestratorMissingSinceKey]; ok {
				t.Fatalf("absence episode survived a lapsed requirement; cache = %v", cache)
			}
		})
	}
}

func TestRunAutoRepairAgentPool_OrchestratorBackoffSuppressionAndRecovery(t *testing.T) {
	unsetAutoRepairAgentPoolEnv(t)
	isolateOrchestratorProcfs(t)
	projectRoot, absent := setupOrchestratorRepairProject(t, nil)
	var calls []spawnedAgentCall
	withFakeRepairSpawner(t, &calls, nil)
	cache := agedOrchestratorAbsence(2 * time.Minute)

	runOrchestratorAutoRepair(projectRoot, absent, cache)
	runOrchestratorAutoRepair(projectRoot, absent, cache)
	if !slices.Equal(spawnedRoles(calls), []string{"orchestrator"}) {
		t.Fatalf("spawned roles = %v, want one orchestrator within the backoff", spawnedRoles(calls))
	}

	cache[autoRepairAgentPoolStartCountPrefix+"orchestrator"] = autoRepairCountTime(AutoRepairAgentPoolMaxStarts, time.Now().UTC())
	outcome := runOrchestratorAutoRepair(projectRoot, absent, cache)
	if len(calls) != 1 {
		t.Fatalf("spawned roles = %v, want no spawn once suppressed", spawnedRoles(calls))
	}
	if len(outcome.Alerts) != 1 || !strings.Contains(outcome.Alerts[0].Message, "auto repair suppressed for role orchestrator") {
		t.Fatalf("alerts = %v, want one orchestrator suppression warning", outcome.Alerts)
	}

	present := orchestratorTestState(map[string]models.Agent{
		"orchestrator-1": occupiedAgent("orchestrator", time.Now().UTC(), 987654321),
	})
	rewriteOrchestratorRepairState(t, projectRoot, present)
	runOrchestratorAutoRepair(projectRoot, present, cache)
	for key := range cache {
		if strings.HasSuffix(key, ":orchestrator") || strings.HasPrefix(key, "orchestrator-missing:") {
			t.Fatalf("cache key %q survived the orchestrator's return; cache = %v", key, cache)
		}
	}
}

func TestRunAutoRepairAgentPool_OrchestratorSpawnFailureKeepsStartBudget(t *testing.T) {
	unsetAutoRepairAgentPoolEnv(t)
	isolateOrchestratorProcfs(t)
	projectRoot, state := setupOrchestratorRepairProject(t, nil)
	var calls []spawnedAgentCall
	withFakeRepairSpawner(t, &calls, errors.New("provider quota exhausted for claude; refusing to spawn orchestrator"))
	cache := agedOrchestratorAbsence(2 * time.Minute)

	outcome := runOrchestratorAutoRepair(projectRoot, state, cache)

	if len(outcome.Alerts) != 1 || outcome.Alerts[0].Category != "AUTO REPAIR FAILED" ||
		!strings.Contains(outcome.Alerts[0].Message, "orchestrator: provider quota exhausted") {
		t.Fatalf("alerts = %v, want AUTO REPAIR FAILED naming the orchestrator", outcome.Alerts)
	}
	if got := autoRepairStartCount(cache, "orchestrator"); got != 0 {
		t.Fatalf("start count = %d, want 0 after a refused spawn", got)
	}
}

func TestRunAutoRepairAgentPool_DisabledLeavesOrchestratorToOperator(t *testing.T) {
	t.Setenv(EnvAutoRepairAgentPool, "false")
	isolateOrchestratorProcfs(t)
	projectRoot, state := setupOrchestratorRepairProject(t, nil)
	var calls []spawnedAgentCall
	withFakeRepairSpawner(t, &calls, nil)

	runOrchestratorAutoRepair(projectRoot, state, agedOrchestratorAbsence(2*time.Minute))

	if len(calls) != 0 {
		t.Fatalf("spawned %v with auto-repair disabled", spawnedRoles(calls))
	}
}

func TestCheckMissingOrchestrator_NamesAutoRepairSetting(t *testing.T) {
	now := time.Date(2026, 9, 24, 4, 4, 18, 0, time.UTC)
	command := brand.Command("agent", "orchestrator")
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{"enabled", "", "auto-repair is enabled — if no orchestrator returns, start one with `" + command + "`"},
		{"disabled", "false", "auto-repair is disabled by " + EnvAutoRepairAgentPool + " — start one with `" + command + "`"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(EnvAutoRepairAgentPool, tt.value)
			isolateOrchestratorProcfs(t)
			cache := map[string]time.Time{orchestratorMissingSinceKey: now.Add(-2 * time.Minute)}

			alerts := checkMissingOrchestrator(orchestratorTestState(nil), defaultOrchestratorTestResolver(), cache, now)

			if len(alerts) != 1 || !strings.Contains(alerts[0].Message, tt.want) {
				t.Fatalf("alerts = %v, want message containing %q", alerts, tt.want)
			}
		})
	}
}

// TestOrchestratorAutoRepairAndAlertShareOneEpisode drives auto-repair and the
// alert snapshot in watch/TUI order with one retained cache.
func TestOrchestratorAutoRepairAndAlertShareOneEpisode(t *testing.T) {
	unsetAutoRepairAgentPoolEnv(t)
	isolateOrchestratorProcfs(t)
	projectRoot, absent := setupOrchestratorRepairProject(t, nil)
	var calls []spawnedAgentCall
	withFakeRepairSpawner(t, &calls, nil)
	config := WatchConfig{ProjectRoot: projectRoot, StateCache: make(map[string]time.Time), WarnWriter: io.Discard}
	tick := func(state *models.State) (int, int) {
		t.Helper()
		before := len(calls)
		RunAutoRepairAgentPool(context.Background(), state, config)
		alerts := RunChecksWithStateSnapshot(state, config).Alerts
		return len(calls) - before, countAlertsByCategory(alerts, "ORCHESTRATOR MISSING")
	}

	if spawned, alerted := tick(absent); spawned != 0 || alerted != 0 {
		t.Fatalf("first tick: spawned %d, alerted %d; want neither", spawned, alerted)
	}
	since, ok := config.StateCache[orchestratorMissingSinceKey]
	if !ok {
		t.Fatal("first tick did not start the absence episode")
	}
	if spawned, alerted := tick(absent); spawned != 0 || alerted != 0 || !config.StateCache[orchestratorMissingSinceKey].Equal(since) {
		t.Fatalf("second tick: spawned %d, alerted %d, since moved = %v; want one unchanged episode start",
			spawned, alerted, !config.StateCache[orchestratorMissingSinceKey].Equal(since))
	}

	config.StateCache[orchestratorMissingSinceKey] = since.Add(-2 * time.Minute)
	if spawned, alerted := tick(absent); spawned != 1 || alerted != 1 {
		t.Fatalf("grace-elapsed tick: spawned %d, alerted %d; want one repair and one alert", spawned, alerted)
	}
	if spawned, alerted := tick(absent); spawned != 0 || alerted != 0 {
		t.Fatalf("continued absence: spawned %d, alerted %d; want backoff and no duplicate alert", spawned, alerted)
	}

	present := orchestratorTestState(map[string]models.Agent{
		"orchestrator-1": occupiedAgent("orchestrator", time.Now().UTC(), 987654321),
	})
	rewriteOrchestratorRepairState(t, projectRoot, present)
	if spawned, alerted := tick(present); spawned != 0 || alerted != 0 {
		t.Fatalf("orchestrator back: spawned %d, alerted %d; want neither", spawned, alerted)
	}
	if _, ok := config.StateCache[orchestratorMissingSinceKey]; ok {
		t.Fatal("episode survived the orchestrator's return")
	}

	rewriteOrchestratorRepairState(t, projectRoot, absent)
	if spawned, alerted := tick(absent); spawned != 0 || alerted != 0 {
		t.Fatalf("new absence: spawned %d, alerted %d; want a fresh grace", spawned, alerted)
	}
	if _, ok := config.StateCache[orchestratorMissingSinceKey]; !ok {
		t.Fatal("new absence did not start a fresh episode")
	}
}

func TestRepairAgentPool_ReportsMissingOrchestrator(t *testing.T) {
	isolateOrchestratorProcfs(t)
	projectRoot, _ := setupOrchestratorRepairProject(t, nil)

	result, err := RepairAgentPool(RepairAgentPoolOptions{ProjectRoot: projectRoot, DryRun: true})
	if err != nil {
		t.Fatalf("RepairAgentPool() error = %v", err)
	}
	if len(result.Missing) != 1 || result.Missing[0].Role != "orchestrator" || result.Missing[0].Reason == "" || result.Missing[0].TaskCount != 0 {
		t.Fatalf("missing = %+v, want one orchestrator entry with a reason", result.Missing)
	}
	wantCommand := brand.Command("agent", "orchestrator") + " --cli " + result.Missing[0].CLI
	if !slices.Equal(result.Commands, []string{wantCommand}) {
		t.Fatalf("commands = %v, want [%s]", result.Commands, wantCommand)
	}
}

func TestRepairAgentPool_OrchestratorByTypeAndFreshRead(t *testing.T) {
	t.Run("custom orchestrator role key is spawned by type", func(t *testing.T) {
		isolateOrchestratorProcfs(t)
		projectRoot, _ := setupOrchestratorRepairProject(t, nil)
		pipelinePath := filepath.Join(projectRoot, paths.ProjectDirName(), "pipeline.yaml")
		content, err := os.ReadFile(pipelinePath)
		if err != nil {
			t.Fatalf("read pipeline: %v", err)
		}
		renamed := strings.Replace(string(content), "    orchestrator:\n      type: orchestrator", "    conductor:\n      type: orchestrator", 1)
		if renamed == string(content) {
			t.Fatal("pipeline fixture no longer declares the orchestrator role as expected")
		}
		testhelpers.SetupPipelineConfigBytes(t, projectRoot, []byte(renamed))

		result, err := RepairAgentPool(RepairAgentPoolOptions{ProjectRoot: projectRoot, DryRun: true})
		if err != nil {
			t.Fatalf("RepairAgentPool() error = %v", err)
		}
		if len(result.Missing) != 1 || result.Missing[0].Role != "conductor" {
			t.Fatalf("missing = %+v, want the conductor role", result.Missing)
		}
	})

	t.Run("fresh read sees an orchestrator the watcher snapshot missed", func(t *testing.T) {
		unsetAutoRepairAgentPoolEnv(t)
		isolateOrchestratorProcfs(t)
		projectRoot, staleSnapshot := setupOrchestratorRepairProject(t, nil)
		rewriteOrchestratorRepairState(t, projectRoot, orchestratorTestState(map[string]models.Agent{
			"orchestrator-1": occupiedAgent("orchestrator", time.Now().UTC(), 987654321),
		}))
		var calls []spawnedAgentCall
		withFakeRepairSpawner(t, &calls, nil)

		runOrchestratorAutoRepair(projectRoot, staleSnapshot, agedOrchestratorAbsence(2*time.Minute))

		if len(calls) != 0 {
			t.Fatalf("spawned %v although the current state has an orchestrator", spawnedRoles(calls))
		}
	})
}
