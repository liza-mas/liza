package ops

import (
	"reflect"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// C2 P3: Analyze groups provider-audit anomalies and selects registered agents
// by canonical provider key, using the config of the state it locks. Tests that
// install testAnalyzeHooks mutate a package global and so do not run in parallel.

func c2AnalyzeState(config models.Config, anomalyProviders []string, agents map[string]models.Agent, health map[string]models.AgentHealth) *models.State {
	observedAt := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	state := testhelpers.CreateValidState()
	state.Config.AgentTools = config.AgentTools
	for i, provider := range anomalyProviders {
		id := "coder-" + string(rune('1'+i))
		anomaly := providerAuditAnomaly(observedAt.Add(time.Duration(i)*time.Minute), id)
		anomaly.Details["provider"] = provider
		state.Anomalies = append(state.Anomalies, anomaly)
	}
	state.Agents = agents
	state.AgentHealth = health
	return state
}

func c2Epochs(registeredAt time.Time, providers map[string]string, states map[string]models.AgentHealthState) (map[string]models.Agent, map[string]models.AgentHealth) {
	agents := map[string]models.Agent{}
	health := map[string]models.AgentHealth{}
	pid := 100
	for id, provider := range providers {
		pid++
		agents[id] = models.Agent{Provider: provider, PID: pid, RegisteredAt: registeredAt}
		health[id] = models.AgentHealth{State: states[id], Provider: provider, PID: pid, RegisteredAt: &registeredAt}
	}
	return agents, health
}

func TestC2AnalyzeAliasAnomaliesHaltOnlyWhenEveryKeyGroupMemberIsDegraded(t *testing.T) {
	registeredAt := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	degraded, ok := models.AgentHealthDegraded, models.AgentHealthOK
	tests := []struct {
		name         string
		config       models.Config
		anomalies    []string
		providers    map[string]string
		states       map[string]models.AgentHealthState
		wantResponse models.CircuitBreakerResponseType
	}{
		{"built-in aliases all degraded", models.Config{}, []string{"codex", "codex-acp"},
			map[string]string{"coder-1": "codex", "coder-2": "codex-acp"},
			map[string]models.AgentHealthState{"coder-1": degraded, "coder-2": degraded}, models.CircuitBreakerResponseHalt},
		{"one healthy alias member prevents the halt", models.Config{}, []string{"codex", "codex-acp"},
			map[string]string{"coder-1": "codex", "coder-2": "codex-acp"},
			map[string]models.AgentHealthState{"coder-1": degraded, "coder-2": ok}, models.CircuitBreakerResponseCheckpoint},
		{"custom tools sharing a provider_key", c2SharedKeyConfig(), []string{"zz-one", "zz-two"},
			map[string]string{"coder-1": "zz-one", "coder-2": "zz-two"},
			map[string]models.AgentHealthState{"coder-1": degraded, "coder-2": degraded}, models.CircuitBreakerResponseHalt},
		{"keyless distinct tools never combine", models.Config{}, []string{"keyless-a", "keyless-b"},
			map[string]string{"coder-1": "keyless-a", "coder-2": "keyless-b"},
			map[string]models.AgentHealthState{"coder-1": degraded, "coder-2": degraded}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
			agents, health := c2Epochs(registeredAt, tt.providers, tt.states)
			state := c2AnalyzeState(tt.config, tt.anomalies, agents, health)
			want := append([]models.Anomaly(nil), state.Anomalies...)
			testhelpers.WriteInitialState(t, stateFile, state)

			result, err := Analyze(tmpDir)
			if err != nil {
				t.Fatalf("Analyze() error: %v", err)
			}

			if result.Response != tt.wantResponse {
				t.Fatalf("response = %q, want %q (pattern %q, explanation %q)", result.Response, tt.wantResponse, result.Pattern, result.Explanation)
			}
			if result.Triggered != (tt.wantResponse == models.CircuitBreakerResponseHalt) {
				t.Errorf("Triggered = %v for response %q", result.Triggered, result.Response)
			}
			committed, err := db.New(stateFile).Read()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(committed.Anomalies, want) {
				t.Errorf("anomalies were rewritten:\n got %v\nwant %v", committed.Anomalies, want)
			}
			for id, agent := range agents {
				if committed.Agents[id].Provider != agent.Provider {
					t.Errorf("agent %s provider = %q, want the stored tool name %q", id, committed.Agents[id].Provider, agent.Provider)
				}
			}
		})
	}
}

// A health record of another tool never validates an alias member's epoch.
func TestC2AnalyzeKeepsExactHealthEpochRaw(t *testing.T) {
	registeredAt := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	agents, health := c2Epochs(registeredAt,
		map[string]string{"coder-1": "codex", "coder-2": "codex-acp"},
		map[string]models.AgentHealthState{"coder-1": models.AgentHealthDegraded, "coder-2": models.AgentHealthDegraded})
	stale := health["coder-2"]
	stale.Provider = "codex"
	health["coder-2"] = stale
	testhelpers.WriteInitialState(t, stateFile, c2AnalyzeState(models.Config{}, []string{"codex", "codex-acp"}, agents, health))

	result, err := Analyze(tmpDir)
	if err != nil {
		t.Fatalf("Analyze() error: %v", err)
	}

	if result.Response != models.CircuitBreakerResponseCheckpoint || result.Triggered {
		t.Fatalf("result = {response:%q triggered:%v}, want an unknown-health checkpoint: %s", result.Response, result.Triggered, result.Explanation)
	}
}

// Analyze derives keys from the config it locks, not from the preliminary read:
// a config change committed between the two decides the grouping.
func TestC2AnalyzeUsesLockedConfigAfterPreliminaryRead(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.WriteInitialState(t, stateFile, c2AnalyzeState(models.Config{}, []string{"codex", "codex-acp"}, nil, nil))
	bb := db.New(stateFile)
	testAnalyzeHooks = &analyzeTestHooks{beforeModify: func() {
		err := bb.Modify(func(current *models.State) error {
			current.Config.AgentTools = map[string]models.AgentToolConfig{"codex-acp": {ProviderKey: "acp-lane"}}
			return nil
		})
		if err != nil {
			t.Fatalf("config update failed: %v", err)
		}
	}}
	t.Cleanup(func() { testAnalyzeHooks = nil })

	result, err := Analyze(tmpDir)
	testAnalyzeHooks = nil
	if err != nil {
		t.Fatalf("Analyze() error: %v", err)
	}

	if result.Pattern != "" || result.Response != "" {
		t.Fatalf("pattern %q response %q: after the split, one anomaly per provider key is below the threshold", result.Pattern, result.Response)
	}
}
