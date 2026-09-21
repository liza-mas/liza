package ops

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/analysis"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestAnalyze_NoAnomalies(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Anomalies = []models.Anomaly{} // No anomalies
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Analyze(tmpDir)
	if err != nil {
		t.Fatalf("Analyze() error: %v", err)
	}

	if result.Triggered {
		t.Error("Should not be triggered with no anomalies")
	}

	// Verify state updated
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if readState.CircuitBreaker.Status != "OK" {
		t.Errorf("CircuitBreaker status = %q, want %q", readState.CircuitBreaker.Status, "OK")
	}
	if readState.Config.Mode != models.SystemModeRunning {
		t.Errorf("Mode = %v, want RUNNING", readState.Config.Mode)
	}
	if readState.CircuitBreaker.CurrentTrigger != nil {
		t.Error("CurrentTrigger should be nil")
	}

	// Verify history entry added
	if len(readState.CircuitBreaker.History) == 0 {
		t.Fatal("Expected history entry")
	}
	lastHistory := readState.CircuitBreaker.History[len(readState.CircuitBreaker.History)-1]
	if lastHistory.Result != "OK" {
		t.Errorf("History result = %q, want %q", lastHistory.Result, "OK")
	}
}

func TestAnalyze_TriggeredByRetryCluster(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	// Create 3+ retry_loop anomalies with similar error_pattern in Details to trigger retry_cluster
	state.Anomalies = []models.Anomaly{
		{Type: "retry_loop", Timestamp: now, Task: "task-1", Reporter: "coder-1", Details: map[string]any{"error_pattern": "connection refused"}},
		{Type: "retry_loop", Timestamp: now, Task: "task-1", Reporter: "coder-1", Details: map[string]any{"error_pattern": "connection refused"}},
		{Type: "retry_loop", Timestamp: now, Task: "task-1", Reporter: "coder-1", Details: map[string]any{"error_pattern": "connection refused"}},
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Analyze(tmpDir)
	if err != nil {
		t.Fatalf("Analyze() error: %v", err)
	}

	if !result.Triggered {
		t.Error("Should be triggered with 3 retry_loop anomalies")
	}
	if result.Pattern == "" {
		t.Error("Pattern should not be empty when triggered")
	}
	if result.Severity == "" {
		t.Error("Severity should not be empty when triggered")
	}
	if result.ReportPath == "" {
		t.Error("ReportPath should not be empty when triggered")
	}

	// Verify report file exists
	if _, err := os.Stat(result.ReportPath); os.IsNotExist(err) {
		t.Error("Report file should exist")
	}

	// Verify state updated
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if readState.CircuitBreaker.Status != "TRIGGERED" {
		t.Errorf("CircuitBreaker status = %q, want %q", readState.CircuitBreaker.Status, "TRIGGERED")
	}
	if readState.Config.Mode != models.SystemModeCircuitBreakerTripped {
		t.Errorf("Mode = %v, want CIRCUIT_BREAKER_TRIPPED", readState.Config.Mode)
	}
	if readState.CircuitBreaker.CurrentTrigger == nil {
		t.Fatal("CurrentTrigger should be set")
	}
	if readState.CircuitBreaker.CurrentTrigger.Pattern == "" {
		t.Error("Trigger pattern should not be empty")
	}
}

func TestAnalyze_BelowThreshold(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	// Only 2 anomalies — below the 3-anomaly threshold for retry_cluster
	state.Anomalies = []models.Anomaly{
		{Type: "retry_loop", Timestamp: now, Task: "task-1", Reporter: "coder-1", Details: map[string]any{"error_pattern": "timeout"}},
		{Type: "retry_loop", Timestamp: now, Task: "task-1", Reporter: "coder-1", Details: map[string]any{"error_pattern": "timeout"}},
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Analyze(tmpDir)
	if err != nil {
		t.Fatalf("Analyze() error: %v", err)
	}

	if result.Triggered {
		t.Error("Should not be triggered with only 2 retry_loop anomalies")
	}
}

func TestAnalyzeProviderAuditResponsesAtCommitBoundary(t *testing.T) {
	observedAt := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	registeredAt := observedAt.Add(-time.Hour)

	tests := []struct {
		name         string
		setup        func(*models.State)
		beforeModify func(*models.State)
		wantResponse models.CircuitBreakerResponseType
		wantClass    models.CircuitBreakerEvidenceClass
		wantMode     models.SystemMode
		wantSprint   models.SprintStatus
	}{
		{
			name: "warning leaves mode and sprint unchanged",
			setup: func(state *models.State) {
				state.Config.Mode = models.SystemModePaused
				addResolvedProviderAuditBoundary(state, observedAt.Add(2*time.Minute))
			},
			wantResponse: models.CircuitBreakerResponseWarning,
			wantClass:    models.CircuitBreakerEvidenceAcknowledgedHistorical,
			wantMode:     models.SystemModePaused,
			wantSprint:   models.SprintStatusInProgress,
		},
		{
			name:         "new evidence checkpoints without current health proof",
			wantResponse: models.CircuitBreakerResponseCheckpoint,
			wantClass:    models.CircuitBreakerEvidenceNew,
			wantMode:     models.SystemModeRunning,
			wantSprint:   models.SprintStatusCheckpoint,
		},
		{
			name: "exact current degraded epochs halt",
			setup: func(state *models.State) {
				setAnalyzeProviderAuditEpochs(state, "codex", models.AgentHealthDegraded, registeredAt)
			},
			wantResponse: models.CircuitBreakerResponseHalt,
			wantClass:    models.CircuitBreakerEvidenceNew,
			wantMode:     models.SystemModeCircuitBreakerTripped,
			wantSprint:   models.SprintStatusInProgress,
		},
		{
			name: "epoch health change before commit prevents stale halt",
			setup: func(state *models.State) {
				setAnalyzeProviderAuditEpochs(state, "codex", models.AgentHealthDegraded, registeredAt)
			},
			beforeModify: func(state *models.State) {
				health := state.AgentHealth["coder-2"]
				health.State = models.AgentHealthOK
				state.AgentHealth["coder-2"] = health
			},
			wantResponse: models.CircuitBreakerResponseCheckpoint,
			wantClass:    models.CircuitBreakerEvidenceNew,
			wantMode:     models.SystemModeRunning,
			wantSprint:   models.SprintStatusCheckpoint,
		},
		{
			name: "exact provider identity change before commit prevents stale halt",
			setup: func(state *models.State) {
				setAnalyzeProviderAuditEpochs(state, "codex", models.AgentHealthDegraded, registeredAt)
			},
			beforeModify: func(state *models.State) {
				for agentID, agent := range state.Agents {
					agent.Provider = "codex-acp"
					state.Agents[agentID] = agent
				}
			},
			wantResponse: models.CircuitBreakerResponseCheckpoint,
			wantClass:    models.CircuitBreakerEvidenceNew,
			wantMode:     models.SystemModeRunning,
			wantSprint:   models.SprintStatusCheckpoint,
		},
		{
			name: "acknowledgement advance prevents stale checkpoint",
			beforeModify: func(state *models.State) {
				addResolvedProviderAuditBoundary(state, observedAt.Add(2*time.Minute))
			},
			wantResponse: models.CircuitBreakerResponseWarning,
			wantClass:    models.CircuitBreakerEvidenceAcknowledgedHistorical,
			wantMode:     models.SystemModeRunning,
			wantSprint:   models.SprintStatusInProgress,
		},
		{
			name: "acknowledgement advance prevents stale halt",
			setup: func(state *models.State) {
				setAnalyzeProviderAuditEpochs(state, "codex", models.AgentHealthDegraded, registeredAt)
			},
			beforeModify: func(state *models.State) {
				addResolvedProviderAuditBoundary(state, observedAt.Add(2*time.Minute))
			},
			wantResponse: models.CircuitBreakerResponseWarning,
			wantClass:    models.CircuitBreakerEvidenceAcknowledgedHistorical,
			wantMode:     models.SystemModeRunning,
			wantSprint:   models.SprintStatusInProgress,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
			state := testhelpers.CreateValidState()
			state.Anomalies = []models.Anomaly{
				providerAuditAnomaly(observedAt, "coder-1"),
				providerAuditAnomaly(observedAt.Add(time.Minute), "coder-2"),
			}
			if tt.setup != nil {
				tt.setup(state)
			}
			testhelpers.WriteInitialState(t, stateFile, state)

			if tt.beforeModify != nil {
				bb := db.New(stateFile)
				testAnalyzeHooks = &analyzeTestHooks{beforeModify: func() {
					if err := bb.Modify(func(current *models.State) error {
						tt.beforeModify(current)
						return nil
					}); err != nil {
						t.Fatalf("beforeModify state update failed: %v", err)
					}
				}}
			}
			t.Cleanup(func() { testAnalyzeHooks = nil })

			result, err := Analyze(tmpDir)
			if err != nil {
				t.Fatalf("Analyze() error: %v", err)
			}
			testAnalyzeHooks = nil

			if result.Response != tt.wantResponse || result.Classification != tt.wantClass {
				t.Fatalf("result = {response:%q classification:%q}, want {%q %q}", result.Response, result.Classification, tt.wantResponse, tt.wantClass)
			}
			if result.Triggered != (tt.wantResponse == models.CircuitBreakerResponseHalt) {
				t.Errorf("Triggered = %v for response %q", result.Triggered, result.Response)
			}
			if result.Explanation == "" {
				t.Error("Explanation should describe the committed response")
			}
			if result.ReportPath == "" {
				t.Fatal("ReportPath should be populated for every provider-audit response")
			}

			readState, err := db.New(stateFile).Read()
			if err != nil {
				t.Fatalf("read committed state: %v", err)
			}
			if readState.Config.Mode != tt.wantMode || readState.Sprint.Status != tt.wantSprint {
				t.Errorf("committed mode/sprint = %s/%s, want %s/%s", readState.Config.Mode, readState.Sprint.Status, tt.wantMode, tt.wantSprint)
			}
			assertAnalyzeResponseHistory(t, readState, result)

			switch tt.wantResponse {
			case models.CircuitBreakerResponseWarning:
				if readState.CircuitBreaker.Status != "OK" || readState.CircuitBreaker.CurrentTrigger != nil || readState.CircuitBreaker.CurrentResponse != nil {
					t.Errorf("WARNING mutated active circuit-breaker state: %+v", readState.CircuitBreaker)
				}
			case models.CircuitBreakerResponseCheckpoint:
				if readState.CircuitBreaker.Status != "OK" || readState.CircuitBreaker.CurrentTrigger != nil {
					t.Errorf("CHECKPOINT hard-triggered circuit breaker: %+v", readState.CircuitBreaker)
				}
				assertActiveAnalyzeResponse(t, readState, result)
			case models.CircuitBreakerResponseHalt:
				if readState.CircuitBreaker.Status != "TRIGGERED" || readState.CircuitBreaker.CurrentTrigger == nil {
					t.Errorf("HALT did not hard-trigger circuit breaker: %+v", readState.CircuitBreaker)
				}
				assertActiveAnalyzeResponse(t, readState, result)
			}
		})
	}
}

func TestAnalyzePreservesActiveProviderResponseUntilResume(t *testing.T) {
	observedAt := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	registeredAt := observedAt.Add(-time.Hour)

	tests := []struct {
		name            string
		activeResponse  models.CircuitBreakerResponseType
		candidate       string
		wantEscalation  bool
		wantPattern     string
		resumeAndVerify bool
	}{
		{name: "halt followed by no match", activeResponse: models.CircuitBreakerResponseHalt, candidate: "none"},
		{name: "halt followed by provider checkpoint", activeResponse: models.CircuitBreakerResponseHalt, candidate: "provider_checkpoint"},
		{name: "halt followed by provider halt", activeResponse: models.CircuitBreakerResponseHalt, candidate: "provider_halt"},
		{name: "halt followed by generic halt", activeResponse: models.CircuitBreakerResponseHalt, candidate: "generic_halt"},
		{name: "checkpoint followed by no match", activeResponse: models.CircuitBreakerResponseCheckpoint, candidate: "none"},
		{name: "checkpoint followed by provider checkpoint", activeResponse: models.CircuitBreakerResponseCheckpoint, candidate: "provider_checkpoint"},
		{
			name: "checkpoint followed by exact provider halt", activeResponse: models.CircuitBreakerResponseCheckpoint,
			candidate: "provider_halt", wantEscalation: true, wantPattern: "provider_audit_degradation", resumeAndVerify: true,
		},
		{
			name: "checkpoint followed by generic halt", activeResponse: models.CircuitBreakerResponseCheckpoint,
			candidate: "generic_halt", wantEscalation: true, wantPattern: "retry_cluster",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
			state := testhelpers.CreateValidState()
			state.Anomalies = []models.Anomaly{
				providerAuditAnomaly(observedAt, "coder-1"),
				providerAuditAnomaly(observedAt.Add(time.Minute), "coder-2"),
			}
			if tt.activeResponse == models.CircuitBreakerResponseHalt {
				setAnalyzeProviderAuditEpochs(state, "codex", models.AgentHealthDegraded, registeredAt)
			}
			testhelpers.WriteInitialState(t, stateFile, state)

			firstResult, err := Analyze(tmpDir)
			if err != nil {
				t.Fatalf("first Analyze() error: %v", err)
			}
			if firstResult.Response != tt.activeResponse {
				t.Fatalf("first response = %q, want %q", firstResult.Response, tt.activeResponse)
			}

			bb := db.New(stateFile)
			activeReportPath := firstResult.ReportPath + ".active"
			report, err := os.ReadFile(firstResult.ReportPath)
			if err != nil {
				t.Fatalf("read first report: %v", err)
			}
			if err := os.WriteFile(activeReportPath, report, 0644); err != nil {
				t.Fatalf("write distinct active report fixture: %v", err)
			}
			if err := bb.Modify(func(current *models.State) error {
				current.CircuitBreaker.CurrentResponse.ReportFile = activeReportPath
				if current.CircuitBreaker.CurrentTrigger != nil {
					current.CircuitBreaker.CurrentTrigger.ReportFile = activeReportPath
				}
				return nil
			}); err != nil {
				t.Fatalf("set distinct active report reference: %v", err)
			}
			before, err := bb.Read()
			if err != nil {
				t.Fatalf("read active response: %v", err)
			}
			if before.CircuitBreaker.CurrentResponse == nil {
				t.Fatal("first Analyze() did not persist an active provider response")
			}
			beforeResponse := *before.CircuitBreaker.CurrentResponse
			beforeTrigger := before.CircuitBreaker.CurrentTrigger
			beforeHistory := append([]models.CircuitBreakerHistory(nil), before.CircuitBreaker.History...)
			beforeStatus := before.CircuitBreaker.Status
			beforeMode := before.Config.Mode
			beforeSprint := before.Sprint.Status

			if err := bb.Modify(func(current *models.State) error {
				switch tt.candidate {
				case "none":
					current.Anomalies = nil
					current.Agents = nil
					current.AgentHealth = nil
				case "provider_checkpoint":
					current.Agents = nil
					current.AgentHealth = nil
				case "provider_halt":
					setAnalyzeProviderAuditEpochs(current, "codex", models.AgentHealthDegraded, registeredAt)
				case "generic_halt":
					current.Anomalies = []models.Anomaly{
						retryLoopAnomaly(observedAt.Add(2*time.Minute), "coder-1"),
						retryLoopAnomaly(observedAt.Add(3*time.Minute), "coder-1"),
						retryLoopAnomaly(observedAt.Add(4*time.Minute), "coder-1"),
					}
					current.Agents = nil
					current.AgentHealth = nil
				default:
					t.Fatalf("unknown candidate fixture %q", tt.candidate)
				}
				return nil
			}); err != nil {
				t.Fatalf("prepare second analysis: %v", err)
			}

			secondResult, err := Analyze(tmpDir)
			if err != nil {
				t.Fatalf("second Analyze() error: %v", err)
			}
			after, err := bb.Read()
			if err != nil {
				t.Fatalf("read second analysis: %v", err)
			}

			if !tt.wantEscalation {
				if after.CircuitBreaker.Status != beforeStatus || after.Config.Mode != beforeMode || after.Sprint.Status != beforeSprint {
					t.Errorf("active response state changed: status/mode/sprint = %s/%s/%s, want %s/%s/%s", after.CircuitBreaker.Status, after.Config.Mode, after.Sprint.Status, beforeStatus, beforeMode, beforeSprint)
				}
				if !reflect.DeepEqual(after.CircuitBreaker.CurrentTrigger, beforeTrigger) {
					t.Errorf("current trigger changed: got %+v, want %+v", after.CircuitBreaker.CurrentTrigger, beforeTrigger)
				}
				if !reflect.DeepEqual(after.CircuitBreaker.CurrentResponse, &beforeResponse) {
					t.Errorf("current response changed: got %+v, want %+v", after.CircuitBreaker.CurrentResponse, beforeResponse)
				}
				if !reflect.DeepEqual(after.CircuitBreaker.History, beforeHistory) {
					t.Errorf("history changed while active response won: got %+v, want %+v", after.CircuitBreaker.History, beforeHistory)
				}
				assertAnalyzeResultMatchesResponse(t, secondResult, &beforeResponse)
				return
			}

			if secondResult.Response != models.CircuitBreakerResponseHalt || secondResult.Pattern != tt.wantPattern || !secondResult.Triggered {
				t.Fatalf("escalated result = {pattern:%q response:%q triggered:%v}, want {%q HALT true}", secondResult.Pattern, secondResult.Response, secondResult.Triggered, tt.wantPattern)
			}
			if len(after.CircuitBreaker.History) != len(beforeHistory)+1 {
				t.Fatalf("history length = %d, want %d after one escalation boundary", len(after.CircuitBreaker.History), len(beforeHistory)+1)
			}
			formerCheckpoint := after.CircuitBreaker.History[len(beforeHistory)-1]
			if formerCheckpoint.SupersededByResponse != models.CircuitBreakerResponseHalt || formerCheckpoint.Resolution != nil || formerCheckpoint.ResolvedAt != nil {
				t.Errorf("former checkpoint = %+v, want HALT supersession without acknowledgement", formerCheckpoint)
			}
			haltBoundary := after.CircuitBreaker.History[len(after.CircuitBreaker.History)-1]
			if haltBoundary.Response != models.CircuitBreakerResponseHalt || haltBoundary.Resolution != nil || haltBoundary.ResolvedAt != nil {
				t.Errorf("halt boundary = %+v, want unresolved HALT", haltBoundary)
			}
			assertActiveAnalyzeResponse(t, after, secondResult)
			assertAnalyzeResponseHistory(t, after, secondResult)

			if tt.resumeAndVerify {
				haltTimestamp := haltBoundary.Timestamp
				if _, err := Resume(tmpDir, "human"); err != nil {
					t.Fatalf("Resume() error: %v", err)
				}
				resumed, err := bb.Read()
				if err != nil {
					t.Fatalf("read resumed state: %v", err)
				}
				checkpointAfterResume := resumed.CircuitBreaker.History[len(beforeHistory)-1]
				if checkpointAfterResume.SupersededByResponse != models.CircuitBreakerResponseHalt || checkpointAfterResume.Resolution != nil || checkpointAfterResume.ResolvedAt != nil {
					t.Errorf("resume acknowledged superseded checkpoint: %+v", checkpointAfterResume)
				}
				resolvedHalt := resumed.CircuitBreaker.History[len(beforeHistory)]
				if !resolvedHalt.Timestamp.Equal(haltTimestamp) || resolvedHalt.Resolution == nil || resolvedHalt.ResolvedAt == nil {
					t.Errorf("resume did not acknowledge winning HALT boundary: %+v", resolvedHalt)
				}

				reanalyzed, err := Analyze(tmpDir)
				if err != nil {
					t.Fatalf("Analyze() after resume error: %v", err)
				}
				if reanalyzed.Triggered || reanalyzed.Response != models.CircuitBreakerResponseWarning || reanalyzed.Classification != models.CircuitBreakerEvidenceAcknowledgedHistorical {
					t.Errorf("unchanged post-resume evidence = {triggered:%v response:%q classification:%q}, want historical WARNING", reanalyzed.Triggered, reanalyzed.Response, reanalyzed.Classification)
				}
			}
		})
	}

	genericHaltCases := []struct {
		name         string
		candidate    string
		wantPattern  string
		wantResponse models.CircuitBreakerResponseType
	}{
		{name: "generic halt followed by no match", candidate: "none"},
		{name: "generic halt followed by repeated equal halt", candidate: "generic_halt", wantPattern: "retry_cluster", wantResponse: models.CircuitBreakerResponseHalt},
		{name: "generic halt followed by weaker provider checkpoint", candidate: "provider_checkpoint", wantPattern: "provider_audit_degradation", wantResponse: models.CircuitBreakerResponseCheckpoint},
	}

	for _, tt := range genericHaltCases {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
			state := testhelpers.CreateValidState()
			state.Anomalies = []models.Anomaly{
				retryLoopAnomaly(observedAt, "coder-1"),
				retryLoopAnomaly(observedAt.Add(time.Minute), "coder-1"),
				retryLoopAnomaly(observedAt.Add(2*time.Minute), "coder-1"),
			}
			testhelpers.WriteInitialState(t, stateFile, state)

			firstResult, err := Analyze(tmpDir)
			if err != nil {
				t.Fatalf("first Analyze() error: %v", err)
			}
			if firstResult.Pattern != "retry_cluster" || firstResult.Response != models.CircuitBreakerResponseHalt || !firstResult.Triggered {
				t.Fatalf("first result = {pattern:%q response:%q triggered:%v}, want retry_cluster/HALT/true", firstResult.Pattern, firstResult.Response, firstResult.Triggered)
			}

			bb := db.New(stateFile)
			activeReportPath := firstResult.ReportPath + ".active"
			report, err := os.ReadFile(firstResult.ReportPath)
			if err != nil {
				t.Fatalf("read first report: %v", err)
			}
			if err := os.WriteFile(activeReportPath, report, 0644); err != nil {
				t.Fatalf("write distinct active report fixture: %v", err)
			}
			if err := bb.Modify(func(current *models.State) error {
				current.CircuitBreaker.CurrentResponse.ReportFile = activeReportPath
				current.CircuitBreaker.CurrentTrigger.ReportFile = activeReportPath
				return nil
			}); err != nil {
				t.Fatalf("set distinct active report reference: %v", err)
			}
			before, err := bb.Read()
			if err != nil {
				t.Fatalf("read active generic response: %v", err)
			}
			if before.CircuitBreaker.CurrentResponse == nil || before.CircuitBreaker.CurrentTrigger == nil {
				t.Fatal("first Analyze() did not persist an active generic HALT")
			}
			beforeResponse := *before.CircuitBreaker.CurrentResponse
			beforeTrigger := *before.CircuitBreaker.CurrentTrigger
			beforeHistory := append([]models.CircuitBreakerHistory(nil), before.CircuitBreaker.History...)
			boundaryIndex := len(beforeHistory) - 1
			if boundaryIndex < 0 || beforeHistory[boundaryIndex].Resolution != nil || beforeHistory[boundaryIndex].ResolvedAt != nil {
				t.Fatalf("active HALT boundary is not unresolved: %+v", beforeHistory)
			}

			if err := bb.Modify(func(current *models.State) error {
				switch tt.candidate {
				case "none":
					current.Anomalies = nil
					current.Agents = nil
					current.AgentHealth = nil
				case "generic_halt":
					// Keep the same retry-cluster evidence to produce an equal HALT candidate.
				case "provider_checkpoint":
					current.Anomalies = []models.Anomaly{
						providerAuditAnomaly(observedAt.Add(3*time.Minute), "coder-1"),
						providerAuditAnomaly(observedAt.Add(4*time.Minute), "coder-2"),
					}
					current.Agents = nil
					current.AgentHealth = nil
				default:
					t.Fatalf("unknown candidate fixture %q", tt.candidate)
				}
				return nil
			}); err != nil {
				t.Fatalf("prepare second analysis: %v", err)
			}
			prepared, err := bb.Read()
			if err != nil {
				t.Fatalf("read prepared candidate: %v", err)
			}
			candidate, _, _ := analysis.DetectUnacknowledgedPatterns(prepared)
			if candidate.Pattern != tt.wantPattern || candidate.Response != tt.wantResponse {
				t.Fatalf("committed candidate fixture = {pattern:%q response:%q}, want {%q %q}", candidate.Pattern, candidate.Response, tt.wantPattern, tt.wantResponse)
			}

			result, err := Analyze(tmpDir)
			if err != nil {
				t.Fatalf("second Analyze() error: %v", err)
			}
			after, err := bb.Read()
			if err != nil {
				t.Fatalf("read generic re-analysis: %v", err)
			}
			if after.CircuitBreaker.Status != "TRIGGERED" || after.Config.Mode != models.SystemModeCircuitBreakerTripped {
				t.Errorf("retained status/mode = %s/%s, want TRIGGERED/CIRCUIT_BREAKER_TRIPPED", after.CircuitBreaker.Status, after.Config.Mode)
			}
			if !reflect.DeepEqual(after.CircuitBreaker.CurrentTrigger, &beforeTrigger) {
				t.Errorf("current trigger changed: got %+v, want %+v", after.CircuitBreaker.CurrentTrigger, beforeTrigger)
			}
			if !reflect.DeepEqual(after.CircuitBreaker.CurrentResponse, &beforeResponse) {
				t.Errorf("current response changed: got %+v, want %+v", after.CircuitBreaker.CurrentResponse, beforeResponse)
			}
			if after.CircuitBreaker.CurrentResponse == nil || after.CircuitBreaker.CurrentResponse.ReportFile != activeReportPath {
				t.Errorf("active report reference = %v, want %q", after.CircuitBreaker.CurrentResponse, activeReportPath)
			}
			if len(after.CircuitBreaker.History) != len(beforeHistory) || !reflect.DeepEqual(after.CircuitBreaker.History, beforeHistory) {
				t.Errorf("history changed while active HALT won: got %+v, want %+v", after.CircuitBreaker.History, beforeHistory)
			}
			retainedBoundary := after.CircuitBreaker.History[boundaryIndex]
			if !retainedBoundary.Timestamp.Equal(beforeResponse.Timestamp) || retainedBoundary.Response != models.CircuitBreakerResponseHalt || retainedBoundary.Resolution != nil || retainedBoundary.ResolvedAt != nil {
				t.Errorf("retained HALT boundary = %+v, want exact unresolved boundary at %s", retainedBoundary, beforeResponse.Timestamp)
			}
			assertAnalyzeResultMatchesResponse(t, result, &beforeResponse)

			if _, err := Resume(tmpDir, "human"); err != nil {
				t.Fatalf("Resume() error: %v", err)
			}
			resumed, err := bb.Read()
			if err != nil {
				t.Fatalf("read resumed state: %v", err)
			}
			if resumed.CircuitBreaker.CurrentResponse != nil || resumed.CircuitBreaker.CurrentTrigger != nil {
				t.Errorf("resume left active circuit-breaker state: %+v", resumed.CircuitBreaker)
			}
			if resumed.CircuitBreaker.Status != "OK" || resumed.Config.Mode != models.SystemModeRunning {
				t.Errorf("resume status/mode = %s/%s, want OK/RUNNING", resumed.CircuitBreaker.Status, resumed.Config.Mode)
			}
			if len(resumed.CircuitBreaker.History) != len(beforeHistory) {
				t.Fatalf("resume history length = %d, want %d", len(resumed.CircuitBreaker.History), len(beforeHistory))
			}
			resolvedBoundary := resumed.CircuitBreaker.History[boundaryIndex]
			if !resolvedBoundary.Timestamp.Equal(beforeResponse.Timestamp) || resolvedBoundary.Response != models.CircuitBreakerResponseHalt || resolvedBoundary.Resolution == nil || resolvedBoundary.ResolvedAt == nil {
				t.Errorf("resume did not resolve exact HALT boundary: %+v", resolvedBoundary)
			}
		})
	}
}

func TestAnalyze_ReportsOnlyUnacknowledgedAnomalies(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	watermark := time.Date(2026, 5, 20, 11, 0, 0, 0, time.UTC)
	pattern := "retry_cluster"
	severity := "ARCHITECTURE_FLAW"
	state := testhelpers.CreateValidState()
	state.CircuitBreaker = models.CircuitBreaker{
		Status: "OK",
		History: []models.CircuitBreakerHistory{
			{Timestamp: watermark, Pattern: &pattern, Severity: &severity, Result: "TRIGGERED"},
		},
	}
	state.Anomalies = []models.Anomaly{
		retryLoopAnomaly(watermark.Add(-time.Minute), "old-coder"),
		retryLoopAnomaly(watermark, "equal-coder"),
		retryLoopAnomaly(watermark.Add(time.Minute), "new-coder-1"),
		retryLoopAnomaly(watermark.Add(2*time.Minute), "new-coder-2"),
		retryLoopAnomaly(watermark.Add(3*time.Minute), "new-coder-3"),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Analyze(tmpDir)
	if err != nil {
		t.Fatalf("Analyze() error: %v", err)
	}
	if !result.Triggered {
		t.Fatal("Analyze() should trigger on post-watermark anomalies")
	}

	reportData, err := os.ReadFile(result.ReportPath)
	if err != nil {
		t.Fatalf("failed to read report: %v", err)
	}
	report := string(reportData)
	if !strings.Contains(report, "**Acknowledged anomalies suppressed:** 2") {
		t.Fatalf("report missing suppressed anomaly count:\n%s", report)
	}
	if strings.Contains(report, "old-coder") || strings.Contains(report, "equal-coder") {
		t.Fatalf("report includes acknowledged anomalies:\n%s", report)
	}
	if !strings.Contains(report, "new-coder-1") || !strings.Contains(report, "new-coder-2") || !strings.Contains(report, "new-coder-3") {
		t.Fatalf("report missing unacknowledged anomalies:\n%s", report)
	}
}

func TestAnalyze_InvalidStatePath(t *testing.T) {
	t.Parallel()

	_, err := Analyze("/nonexistent/path")
	if err == nil {
		t.Fatal("Expected error for nonexistent path")
	}
	if !strings.Contains(err.Error(), "failed to read state") {
		t.Errorf("Error = %q, want to contain 'failed to read state'", err.Error())
	}
}

func providerAuditAnomaly(timestamp time.Time, agentID string) models.Anomaly {
	return models.Anomaly{
		Timestamp: timestamp,
		Reporter:  agentID,
		Type:      "provider_audit_degraded",
		Details: map[string]any{
			"provider": "codex",
			"agent_id": agentID,
			"message":  "failed to record rollout items",
		},
	}
}

func retryLoopAnomaly(timestamp time.Time, agentID string) models.Anomaly {
	return models.Anomaly{
		Timestamp: timestamp,
		Reporter:  agentID,
		Type:      "retry_loop",
		Details: map[string]any{
			"error_pattern": "connection refused",
		},
	}
}

func addResolvedProviderAuditBoundary(state *models.State, boundary time.Time) {
	resolvedAt := boundary.Add(time.Second)
	state.CircuitBreaker.Status = "OK"
	state.CircuitBreaker.CurrentTrigger = nil
	state.CircuitBreaker.CurrentResponse = nil
	state.CircuitBreaker.History = append(state.CircuitBreaker.History, models.CircuitBreakerHistory{
		Timestamp:      boundary,
		Result:         "CHECKPOINT",
		Response:       models.CircuitBreakerResponseCheckpoint,
		Classification: models.CircuitBreakerEvidenceNew,
		ResolvedAt:     &resolvedAt,
	})
}

func setAnalyzeProviderAuditEpochs(state *models.State, provider string, healthState models.AgentHealthState, registeredAt time.Time) {
	state.Agents = map[string]models.Agent{
		"coder-1": {Provider: provider, PID: 101, RegisteredAt: registeredAt},
		"coder-2": {Provider: provider, PID: 102, RegisteredAt: registeredAt},
	}
	state.AgentHealth = map[string]models.AgentHealth{
		"coder-1": {State: healthState, Provider: provider, PID: 101, RegisteredAt: &registeredAt},
		"coder-2": {State: healthState, Provider: provider, PID: 102, RegisteredAt: &registeredAt},
	}
}

func assertActiveAnalyzeResponse(t *testing.T, state *models.State, result *AnalyzeResult) {
	t.Helper()
	response := state.CircuitBreaker.CurrentResponse
	if response == nil {
		t.Fatal("active circuit-breaker response was not persisted")
	}
	if response.Response != result.Response || response.Classification != result.Classification || response.Explanation != result.Explanation {
		t.Errorf("active response = {%q %q %q}, want result {%q %q %q}", response.Response, response.Classification, response.Explanation, result.Response, result.Classification, result.Explanation)
	}
	if response.ReportFile != result.ReportPath {
		t.Errorf("active response report = %q, want %q", response.ReportFile, result.ReportPath)
	}
}

func assertAnalyzeResultMatchesResponse(t *testing.T, result *AnalyzeResult, response *models.CircuitBreakerResponse) {
	t.Helper()
	if result.Pattern != response.Pattern || result.Severity != response.Severity || result.Response != response.Response || result.Classification != response.Classification || result.Explanation != response.Explanation {
		t.Errorf("AnalyzeResult = {pattern:%q severity:%q response:%q classification:%q explanation:%q}, want active response %+v", result.Pattern, result.Severity, result.Response, result.Classification, result.Explanation, response)
	}
	if result.Triggered != (response.Response == models.CircuitBreakerResponseHalt) {
		t.Errorf("AnalyzeResult.Triggered = %v for active response %q", result.Triggered, response.Response)
	}
	if result.ReportPath != response.ReportFile {
		t.Errorf("AnalyzeResult.ReportPath = %q, want active report %q", result.ReportPath, response.ReportFile)
	}
	if _, err := os.Stat(result.ReportPath); err != nil {
		t.Errorf("AnalyzeResult.ReportPath does not reference the existing active report: %v", err)
	}
}

func assertAnalyzeResponseHistory(t *testing.T, state *models.State, result *AnalyzeResult) {
	t.Helper()
	if len(state.CircuitBreaker.History) == 0 {
		t.Fatal("circuit-breaker response history was not persisted")
	}
	history := state.CircuitBreaker.History[len(state.CircuitBreaker.History)-1]
	if history.Response != result.Response || history.Classification != result.Classification || history.Explanation != result.Explanation {
		t.Errorf("history response = {%q %q %q}, want result {%q %q %q}", history.Response, history.Classification, history.Explanation, result.Response, result.Classification, result.Explanation)
	}
}

// rcaGateEntry is the TaskEventBlocked entry the verdict gate appends, carrying
// the detail keys telemetry reads. Timestamps round-trip through YAML as
// RFC3339 strings, exactly as the gate writes them.
func rcaGateEntry(gatedAt, firstRejectionAt time.Time) models.TaskHistoryEntry {
	return models.TaskHistoryEntry{
		Time:  gatedAt,
		Event: models.TaskEventBlocked,
		Extra: map[string]any{
			"blocked_class":      models.BlockedReasonRejectionRCARequired,
			"threshold":          4,
			"rejection_count":    4,
			"gated_at":           gatedAt.Format(time.RFC3339),
			"first_rejection_at": firstRejectionAt.Format(time.RFC3339),
		},
	}
}

func rcaRecordedEntry(at, gatedAt time.Time, causes ...string) models.TaskHistoryEntry {
	return models.TaskHistoryEntry{
		Time:  at,
		Event: models.TaskEventRejectionRCARecorded,
		Extra: map[string]any{
			"fingerprint":        "fp",
			"threshold":          4,
			"rejection_count":    4,
			"gated_at":           gatedAt.Format(time.RFC3339),
			"causes":             causes,
			"contribution_count": len(causes),
			"recorded_by":        "orchestrator-1",
		},
	}
}

func rcaResumedEntry(at, gatedAt time.Time, recoveryPath string) models.TaskHistoryEntry {
	return models.TaskHistoryEntry{
		Time:  at,
		Event: models.TaskEventRejectionRCAResumed,
		Extra: map[string]any{
			"fingerprint":       "fp",
			"recovery_path":     recoveryPath,
			"restore_mode":      models.RejectionRCARestoreMode(recoveryPath),
			"actor":             "orchestrator-1",
			"lifecycle_version": 3,
			"decided_at":        at.Format(time.RFC3339),
			"iteration_exempt":  false,
			"gated_at":          gatedAt.Format(time.RFC3339),
		},
	}
}

func rcaRecord(gatedAt time.Time, closed bool) *models.RejectionRCARecord {
	record := &models.RejectionRCARecord{
		SchemaVersion:  models.RejectionRCASchemaVersion,
		Threshold:      4,
		RejectionCount: 4,
		GatedAt:        gatedAt,
	}
	if closed {
		record.Disposition = &models.RejectionRCADisposition{
			RecoveryPath: models.RecoveryImplementationCorrection,
			RestoreMode:  models.RestoreModeClaimable,
			Actor:        "orchestrator-1",
			DecidedAt:    gatedAt.Add(time.Minute),
		}
	}
	return record
}

func gatedTask(id string, status models.TaskStatus, now time.Time, record *models.RejectionRCARecord, history ...models.TaskHistoryEntry) models.Task {
	task := testhelpers.BuildTaskByStatus(id, status, now)
	if status == models.TaskStatusBlocked {
		reason := models.BlockedReasonRejectionRCARequired + ": 4 durable rejections reached threshold 4"
		task.BlockedReason = &reason
	}
	task.ReviewCyclesTotal = 4
	task.RejectionRCA = record
	task.History = history
	return task
}

func TestAnalyzeRejectionRCATelemetry(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	now := time.Now().UTC().Truncate(time.Second)
	t0 := now.Add(-24 * time.Hour)

	// GIVEN one open gate (30m to gate), one resumed-and-merged task (45m),
	// one resumed-and-re-gated task whose first cycle (60m, capability
	// cause, capability_reroute) exists only in history while its live record
	// is the seeded second cycle (180m), one gated task with an unrecognized
	// cause (90m), one resumed task still executing (120m), and two ungated
	// tasks that must not count. The open gate's RCA was corrected once: the
	// superseded recorded entry of the same cycle (same gated_at) must not count.
	state := testhelpers.CreateValidState()
	openGate := gatedTask("gate-open", models.TaskStatusBlocked, now, rcaRecord(t0.Add(40*time.Minute), false),
		rcaGateEntry(t0.Add(40*time.Minute), t0.Add(10*time.Minute)),
		rcaRecordedEntry(t0.Add(45*time.Minute), t0.Add(40*time.Minute), models.RejectionCauseLifecycleRetry, "superseded_label"),
		rcaRecordedEntry(t0.Add(50*time.Minute), t0.Add(40*time.Minute), models.RejectionCauseCapabilityFailure, models.RejectionCauseProductDefect),
	)
	merged := gatedTask("resumed-merged", models.TaskStatusMerged, now, rcaRecord(t0.Add(2*time.Hour), true),
		rcaGateEntry(t0.Add(2*time.Hour), t0.Add(75*time.Minute)),
		rcaRecordedEntry(t0.Add(130*time.Minute), t0.Add(2*time.Hour), models.RejectionCauseLifecycleRetry, models.RejectionCauseUnknown),
		rcaResumedEntry(t0.Add(140*time.Minute), t0.Add(2*time.Hour), models.RecoveryLifecycleRepair),
		models.TaskHistoryEntry{Time: t0.Add(3 * time.Hour), Event: models.TaskEventMerged},
	)
	regated := gatedTask("resumed-regated", models.TaskStatusBlocked, now, rcaRecord(t0.Add(8*time.Hour), false),
		rcaGateEntry(t0.Add(4*time.Hour), t0.Add(3*time.Hour)),
		rcaRecordedEntry(t0.Add(250*time.Minute), t0.Add(4*time.Hour), models.RejectionCauseCapabilityFailure),
		rcaResumedEntry(t0.Add(260*time.Minute), t0.Add(4*time.Hour), models.RecoveryCapabilityReroute),
		rcaGateEntry(t0.Add(8*time.Hour), t0.Add(5*time.Hour)),
	)
	unrecognized := gatedTask("unrecognized-cause", models.TaskStatusBlocked, now, rcaRecord(t0.Add(10*time.Hour), false),
		rcaGateEntry(t0.Add(10*time.Hour), t0.Add(510*time.Minute)),
		rcaRecordedEntry(t0.Add(11*time.Hour), t0.Add(10*time.Hour), "flaky_infra"),
	)
	stillOpen := gatedTask("resumed-still-open", models.TaskStatusImplementing, now, rcaRecord(t0.Add(14*time.Hour), true),
		rcaGateEntry(t0.Add(14*time.Hour), t0.Add(12*time.Hour)),
		rcaRecordedEntry(t0.Add(15*time.Hour), t0.Add(14*time.Hour), models.RejectionCauseProductDefect),
		rcaResumedEntry(t0.Add(16*time.Hour), t0.Add(14*time.Hour), models.RecoveryImplementationCorrection),
	)
	ungated := testhelpers.BuildTaskByStatus("rejected-ungated", models.TaskStatusImplementing, now)
	ungated.History = []models.TaskHistoryEntry{
		{Time: t0, Event: models.TaskEventRejected},
		{Time: t0.Add(time.Hour), Event: models.TaskEventReviewVerdictRejected},
	}
	ordinaryBlocked := testhelpers.BuildTaskByStatus("blocked-ordinary", models.TaskStatusBlocked, now)
	ordinaryBlocked.History = []models.TaskHistoryEntry{{Time: t0, Event: models.TaskEventBlocked, Extra: map[string]any{"blocked_class": "dependency"}}}
	state.Tasks = []models.Task{openGate, merged, regated, unrecognized, stillOpen, ungated, ordinaryBlocked}
	testhelpers.WriteInitialState(t, stateFile, state)

	// WHEN
	result, err := Analyze(tmpDir)
	if err != nil {
		t.Fatalf("Analyze() error: %v", err)
	}

	// THEN every cumulative dimension comes from history and only the
	// currently-gated count from the live record.
	want := &RejectionRCATelemetry{
		TasksGated:     5,
		GateCycles:     6,
		CurrentlyGated: 3,
		Causes: map[string]int{
			models.RejectionCauseProductDefect:     2,
			models.RejectionCauseCapabilityFailure: 2,
			models.RejectionCauseLifecycleRetry:    1,
			models.RejectionCauseUnknown:           2,
		},
		UnrecognizedCauses:  1,
		MedianSecondsToGate: int64((75 * time.Minute).Seconds()),
		MaxSecondsToGate:    int64((180 * time.Minute).Seconds()),
		RecoveryPaths: map[string]int{
			models.RecoveryLifecycleRepair:          1,
			models.RecoveryCapabilityReroute:        1,
			models.RecoveryImplementationCorrection: 1,
		},
		ResumedThenTerminal: 1,
		ResumedThenRegated:  1,
		ResumedStillOpen:    1,
	}
	if !reflect.DeepEqual(result.RejectionRCA, want) {
		t.Fatalf("RejectionRCA telemetry:\n got %#v\nwant %#v", result.RejectionRCA, want)
	}
	if result.Triggered || result.Pattern != "" {
		t.Fatalf("telemetry must not trigger a pattern: %#v", result)
	}
}

func TestAnalyzeRejectionRCATelemetryAbsent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		anomalies  []models.Anomaly
		wantStatus string
		wantMode   models.SystemMode
	}{
		{name: "OK", wantStatus: "OK", wantMode: models.SystemModeRunning},
		{
			name: "triggered",
			anomalies: []models.Anomaly{
				retryLoopAnomaly(time.Now(), "coder-1"),
				retryLoopAnomaly(time.Now(), "coder-1"),
				retryLoopAnomaly(time.Now(), "coder-1"),
			},
			wantStatus: "TRIGGERED",
			wantMode:   models.SystemModeCircuitBreakerTripped,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmpDir := t.TempDir()
			stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
			now := time.Now().UTC()

			// GIVEN rejections and an ordinary block, but no gate entry and no live record.
			state := testhelpers.CreateValidState()
			rejected := testhelpers.BuildTaskByStatus("rejected", models.TaskStatusImplementing, now)
			rejected.History = []models.TaskHistoryEntry{{Time: now, Event: models.TaskEventRejected}}
			blocked := testhelpers.BuildTaskByStatus("blocked", models.TaskStatusBlocked, now)
			blocked.History = []models.TaskHistoryEntry{{Time: now, Event: models.TaskEventBlocked}}
			state.Tasks = []models.Task{rejected, blocked}
			state.Anomalies = tt.anomalies
			testhelpers.WriteInitialState(t, stateFile, state)

			result, err := Analyze(tmpDir)
			if err != nil {
				t.Fatalf("Analyze() error: %v", err)
			}

			// THEN the block is present but empty and the breaker behaves as before.
			if !reflect.DeepEqual(result.RejectionRCA, &RejectionRCATelemetry{}) {
				t.Fatalf("RejectionRCA = %#v, want empty block", result.RejectionRCA)
			}
			readState, err := db.New(stateFile).Read()
			if err != nil {
				t.Fatalf("Read() error: %v", err)
			}
			if readState.CircuitBreaker.Status != tt.wantStatus || readState.Config.Mode != tt.wantMode {
				t.Fatalf("circuit breaker status/mode = %q/%q, want %q/%q", readState.CircuitBreaker.Status, readState.Config.Mode, tt.wantStatus, tt.wantMode)
			}
			if result.Triggered != (tt.wantStatus == "TRIGGERED") {
				t.Fatalf("Triggered = %v for %s", result.Triggered, tt.name)
			}
		})
	}
}
