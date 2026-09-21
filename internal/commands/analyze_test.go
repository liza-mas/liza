package commands

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestAnalyzeCommand(t *testing.T) {
	tests := []struct {
		name             string
		anomalies        []models.Anomaly
		wantError        bool
		wantTriggered    bool
		wantReportExists bool
	}{
		{
			name:             "no anomalies - OK status",
			anomalies:        []models.Anomaly{},
			wantError:        false,
			wantTriggered:    false,
			wantReportExists: false,
		},
		{
			name: "retry_cluster detected - triggers circuit breaker",
			anomalies: []models.Anomaly{
				{
					Timestamp: time.Now(),
					Task:      "task-1",
					Reporter:  "coder-1",
					Type:      "retry_loop",
					Details: map[string]any{
						"error_pattern": "timeout",
					},
				},
				{
					Timestamp: time.Now(),
					Task:      "task-2",
					Reporter:  "coder-2",
					Type:      "retry_loop",
					Details: map[string]any{
						"error_pattern": "timeout",
					},
				},
				{
					Timestamp: time.Now(),
					Task:      "task-3",
					Reporter:  "code-reviewer-1",
					Type:      "retry_loop",
					Details: map[string]any{
						"error_pattern": "timeout",
					},
				},
			},
			wantError:        false,
			wantTriggered:    true,
			wantReportExists: true,
		},
		{
			name: "debt_accumulation detected",
			anomalies: []models.Anomaly{
				{
					Timestamp: time.Now(),
					Task:      "task-1",
					Reporter:  "coder-1",
					Type:      "trade_off",
					Details: map[string]any{
						"debt_created": true,
					},
				},
				{
					Timestamp: time.Now(),
					Task:      "task-2",
					Reporter:  "coder-2",
					Type:      "trade_off",
					Details: map[string]any{
						"debt_created": true,
					},
				},
				{
					Timestamp: time.Now(),
					Task:      "task-3",
					Reporter:  "coder-1",
					Type:      "trade_off",
					Details: map[string]any{
						"debt_created": true,
					},
				},
			},
			wantError:        false,
			wantTriggered:    true,
			wantReportExists: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test environment
			tempDir := t.TempDir()
			reportPath := paths.New(tempDir).CircuitBreakerReportPath()

			// Create test state with anomalies
			state := testhelpers.CreateValidState()
			state.Anomalies = tt.anomalies

			// Setup liza directory and write state
			statePath, _ := testhelpers.SetupLizaDir(t, tempDir)
			testhelpers.SetupPipelineConfig(t, tempDir)
			bb := testhelpers.WriteInitialState(t, statePath, state)

			// Run analyze command
			err := AnalyzeCommand(tempDir)

			// Check error
			if (err != nil) != tt.wantError {
				t.Errorf("AnalyzeCommand() error = %v, wantError %v", err, tt.wantError)
			}

			// Read updated state
			updatedState, err := bb.Read()
			if err != nil {
				t.Fatalf("Failed to read updated state: %v", err)
			}

			// Check circuit breaker status
			if tt.wantTriggered {
				if updatedState.CircuitBreaker.Status != "TRIGGERED" {
					t.Errorf("CircuitBreaker status = %v, want TRIGGERED", updatedState.CircuitBreaker.Status)
				}

				if updatedState.CircuitBreaker.CurrentTrigger == nil {
					t.Error("CurrentTrigger should be set when triggered")
				} else {
					if updatedState.CircuitBreaker.CurrentTrigger.Pattern == "" {
						t.Error("CurrentTrigger pattern should not be empty")
					}
					if updatedState.CircuitBreaker.CurrentTrigger.Severity == "" {
						t.Error("CurrentTrigger severity should not be empty")
					}
				}

				// Check history
				if len(updatedState.CircuitBreaker.History) == 0 {
					t.Error("History should have at least one entry")
				}
			} else {
				if updatedState.CircuitBreaker.Status != "OK" {
					t.Errorf("CircuitBreaker status = %v, want OK", updatedState.CircuitBreaker.Status)
				}

				if updatedState.CircuitBreaker.CurrentTrigger != nil {
					t.Error("CurrentTrigger should be nil when not triggered")
				}
			}

			// Check system mode
			if tt.wantTriggered {
				if updatedState.Config.Mode != models.SystemModeCircuitBreakerTripped {
					t.Errorf("Config.Mode = %v, want CIRCUIT_BREAKER_TRIPPED", updatedState.Config.Mode)
				}
			} else {
				if updatedState.Config.Mode != models.SystemModeRunning {
					t.Errorf("Config.Mode = %v, want RUNNING", updatedState.Config.Mode)
				}
			}

			// Check report file
			_, err = os.Stat(reportPath)
			reportExists := err == nil
			if reportExists != tt.wantReportExists {
				t.Errorf("Report file exists = %v, want %v", reportExists, tt.wantReportExists)
			}

			// Verify report content if it should exist
			if tt.wantReportExists && reportExists {
				reportData, err := os.ReadFile(reportPath)
				if err != nil {
					t.Fatalf("Failed to read report: %v", err)
				}

				report := string(reportData)
				expectedSections := []string{
					"# Circuit Breaker Report",
					"**Analyzed:**",
					"**Pattern:**",
					"**Severity:**",
					"## Evidence",
					"## Anomalies (trimmed)",
					"## Anomalies (raw)",
					"## Human Decision Required",
				}

				for _, section := range expectedSections {
					if !contains(report, section) {
						t.Errorf("Report missing expected section: %q", section)
					}
				}
			}
		})
	}
}

func TestWriteAnalyzeResultProjectsTypedResponse(t *testing.T) {
	tests := []struct {
		name    string
		result  *ops.AnalyzeResult
		want    []string
		notWant []string
	}{
		{
			name:   "no pattern",
			result: &ops.AnalyzeResult{},
			want:   []string{"Circuit breaker: OK — no patterns detected"},
		},
		{
			name: "acknowledged historical warning",
			result: &ops.AnalyzeResult{
				Pattern:        "provider_audit_degradation",
				Severity:       "OBSERVABILITY_DEGRADED",
				Evidence:       "historical evidence",
				Response:       models.CircuitBreakerResponseWarning,
				Classification: models.CircuitBreakerEvidenceAcknowledgedHistorical,
				Explanation:    "qualifying evidence is entirely at or before the resolved response boundary",
				ReportPath:     "/tmp/report.md",
			},
			want: []string{
				"Circuit breaker response: WARNING",
				"Evidence class: ACKNOWLEDGED_HISTORICAL",
				"Explanation: qualifying evidence is entirely at or before the resolved response boundary",
				"State action: none — this evidence was already acknowledged",
			},
			notWant: []string{"TRIGGERED", "Recovery:"},
		},
		{
			name: "checkpoint with unknown alias health",
			result: &ops.AnalyzeResult{
				Pattern:        "provider_audit_degradation",
				Severity:       "OBSERVABILITY_DEGRADED",
				Evidence:       "continuing evidence",
				Response:       models.CircuitBreakerResponseCheckpoint,
				Classification: models.CircuitBreakerEvidenceContinuing,
				Explanation:    "current health is unknown: provider codex has no exact match for registered alias codex-acp",
				ReportPath:     "/tmp/report.md",
			},
			want: []string{
				"Circuit breaker response: CHECKPOINT",
				"Severity: OBSERVABILITY_DEGRADED",
				"Evidence class: CONTINUING",
				"Explanation: current health is unknown: provider codex has no exact match for registered alias codex-acp",
				"State action: sprint moved to CHECKPOINT",
				"Recovery: run `",
				" resume`",
			},
			notWant: []string{"TRIGGERED"},
		},
		{
			name: "halt with exact provider epoch proof",
			result: &ops.AnalyzeResult{
				Triggered:      true,
				Pattern:        "provider_audit_degradation",
				Severity:       "OBSERVABILITY_DEGRADED",
				Evidence:       "current evidence",
				Response:       models.CircuitBreakerResponseHalt,
				Classification: models.CircuitBreakerEvidenceNew,
				Explanation:    "all 2 exact provider codex matches have degraded health for the same agent ID, provider, PID, and registration-time epoch",
				ReportPath:     "/tmp/report.md",
			},
			want: []string{
				"CIRCUIT BREAKER TRIGGERED — HALT",
				"Response: HALT",
				"Evidence class: NEW",
				"Explanation: all 2 exact provider codex matches have degraded health for the same agent ID, provider, PID, and registration-time epoch",
				"State action: execution halted",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			writeAnalyzeResult(&output, tt.result)
			for _, want := range tt.want {
				if !strings.Contains(output.String(), want) {
					t.Errorf("output missing %q\n%s", want, output.String())
				}
			}
			for _, notWant := range tt.notWant {
				if strings.Contains(output.String(), notWant) {
					t.Errorf("output unexpectedly contains %q\n%s", notWant, output.String())
				}
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || contains(s[1:], substr)))
}

func TestWriteAnalyzeResultRejectionRCA(t *testing.T) {
	telemetry := &ops.RejectionRCATelemetry{
		TasksGated:          3,
		GateCycles:          4,
		CurrentlyGated:      2,
		Causes:              map[string]int{"capability_failure": 2, "product_defect": 1, "unknown": 2},
		UnrecognizedCauses:  1,
		MedianSecondsToGate: 4500,
		MaxSecondsToGate:    10800,
		RecoveryPaths:       map[string]int{"lifecycle_repair": 1, "capability_reroute": 1},
		ResumedThenTerminal: 1,
		ResumedThenRegated:  1,
		ResumedStillOpen:    0,
	}
	wantBlock := []string{
		"Rejection RCA gate:",
		"Tasks gated: 3 (4 cycles), currently gated: 2",
		"Causes: capability_failure=2 product_defect=1 unknown=2 (unrecognized folded into unknown: 1)",
		"Time to gate: median 1h15m0s, max 3h0m0s",
		"Recovery paths: capability_reroute=1 lifecycle_repair=1",
		"Convergence after resume: terminal=1 re-gated=1 still open=0",
	}
	tests := []struct {
		name    string
		result  *ops.AnalyzeResult
		want    []string
		notWant []string
	}{
		{
			name:   "OK breaker renders the block after the status line",
			result: &ops.AnalyzeResult{RejectionRCA: telemetry},
			want:   append([]string{"Circuit breaker: OK — no patterns detected"}, wantBlock...),
		},
		{
			name: "triggered breaker renders the block after the report path",
			result: &ops.AnalyzeResult{
				Triggered:    true,
				Pattern:      "retry_cluster",
				Severity:     "HIGH",
				Response:     models.CircuitBreakerResponseHalt,
				ReportPath:   "/tmp/report.md",
				RejectionRCA: telemetry,
			},
			want: append([]string{"CIRCUIT BREAKER TRIGGERED — HALT", "Report written to: /tmp/report.md"}, wantBlock...),
		},
		{
			name:    "empty block",
			result:  &ops.AnalyzeResult{RejectionRCA: &ops.RejectionRCATelemetry{}},
			want:    []string{"Rejection RCA gate: no gated tasks"},
			notWant: []string{"Causes:", "Time to gate:"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			writeAnalyzeResult(&output, tt.result)
			got := output.String()
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("output missing %q\n%s", want, got)
				}
			}
			for _, notWant := range append(tt.notWant, "token", "Token") {
				if strings.Contains(got, notWant) {
					t.Errorf("output unexpectedly contains %q\n%s", notWant, got)
				}
			}
			if idx := strings.Index(got, "Rejection RCA gate:"); idx >= 0 && strings.Contains(got[idx:], "Circuit breaker") {
				t.Errorf("block must render after the existing output\n%s", got)
			}
		})
	}
}
