package commands

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// C2 P3: the read-only watch prediction and ops.Analyze group provider-audit
// anomalies by canonical key through the same path, so equal state yields equal
// outcomes: watch alerts exactly when Analyze would halt.

func c2WatchState(config models.Config, providers map[string]string, healthStates map[string]models.AgentHealthState) *models.State {
	observedAt := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	registeredAt := observedAt.Add(-time.Hour)
	state := testhelpers.CreateValidState()
	state.Config.AgentTools = config.AgentTools
	state.Agents = map[string]models.Agent{}
	state.AgentHealth = map[string]models.AgentHealth{}
	pid := 100
	for _, id := range []string{"coder-1", "coder-2"} {
		pid++
		provider := providers[id]
		anomaly := watchProviderAuditAnomaly(observedAt.Add(time.Duration(pid-100)*time.Minute), id)
		anomaly.Details["provider"] = provider
		state.Anomalies = append(state.Anomalies, anomaly)
		state.Agents[id] = models.Agent{Role: "coder", Status: models.AgentStatusIdle, Heartbeat: observedAt, Provider: provider, PID: pid, RegisteredAt: registeredAt}
		state.AgentHealth[id] = models.AgentHealth{State: healthStates[id], Role: "coder", Provider: provider, PID: pid, RegisteredAt: &registeredAt, DegradedAt: observedAt}
	}
	return state
}

func TestC2AnalyzeWatchAgreeOnCanonicalProviderGroups(t *testing.T) {
	degraded, ok := models.AgentHealthDegraded, models.AgentHealthOK
	sharedKeys := models.Config{AgentTools: map[string]models.AgentToolConfig{
		"zz-one": {Executable: "one", ProviderKey: "shared"},
		"zz-two": {Executable: "two", ProviderKey: "shared"},
	}}
	tests := []struct {
		name      string
		config    models.Config
		providers map[string]string
		health    map[string]models.AgentHealthState
		wantHalt  bool
	}{
		{"aliases all degraded", models.Config{}, map[string]string{"coder-1": "codex", "coder-2": "codex-acp"},
			map[string]models.AgentHealthState{"coder-1": degraded, "coder-2": degraded}, true},
		{"custom tools sharing a key all degraded", sharedKeys, map[string]string{"coder-1": "zz-one", "coder-2": "zz-two"},
			map[string]models.AgentHealthState{"coder-1": degraded, "coder-2": degraded}, true},
		{"one healthy alias member", models.Config{}, map[string]string{"coder-1": "codex", "coder-2": "codex-acp"},
			map[string]models.AgentHealthState{"coder-1": degraded, "coder-2": ok}, false},
		{"keyless distinct tools do not group", models.Config{}, map[string]string{"coder-1": "keyless-a", "coder-2": "keyless-b"},
			map[string]models.AgentHealthState{"coder-1": degraded, "coder-2": degraded}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := c2WatchState(tt.config, tt.providers, tt.health)
			projectRoot := t.TempDir()
			stateFile, _ := testhelpers.SetupLizaDir(t, projectRoot)
			testhelpers.WriteInitialState(t, stateFile, state)

			alerts := checkCircuitBreakerEscalation(state, make(map[string]time.Time))
			result, err := ops.Analyze(projectRoot)
			if err != nil {
				t.Fatalf("Analyze() error: %v", err)
			}

			analyzeHalts := result.Response == models.CircuitBreakerResponseHalt
			if analyzeHalts != tt.wantHalt || (len(alerts) == 1) != tt.wantHalt {
				t.Fatalf("Analyze halt = %v (response %q), watch alerts = %d, want halt %v for both", analyzeHalts, result.Response, len(alerts), tt.wantHalt)
			}
			if tt.wantHalt && !strings.Contains(alerts[0].Message, result.Pattern) {
				t.Errorf("watch alert %q does not name Analyze's pattern %q", alerts[0].Message, result.Pattern)
			}
			if len(alerts) > 1 {
				t.Errorf("alerts = %+v, want at most one", alerts)
			}
		})
	}
}
