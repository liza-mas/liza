package analysis

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

func TestDetectPatterns(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name         string
		anomalies    []models.Anomaly
		wantPattern  string
		wantSeverity string
		wantEvidence string
		wantResponse models.CircuitBreakerResponseType

		wantTriggered bool
	}{
		{
			name:          "no anomalies - OK",
			anomalies:     []models.Anomaly{},
			wantTriggered: false,
		},
		{
			name: "retry_cluster - 3+ retry_loops with similar error_pattern",
			anomalies: []models.Anomaly{
				{
					Timestamp: now,
					Task:      "task-1",
					Reporter:  "coder-1",
					Type:      "retry_loop",
					Details: map[string]any{
						"error_pattern": "serialization failure on nested entity",
						"count":         3,
					},
				},
				{
					Timestamp: now,
					Task:      "task-2",
					Reporter:  "coder-2",
					Type:      "retry_loop",
					Details: map[string]any{
						"error_pattern": "serialization failure on nested entity",
						"count":         2,
					},
				},
				{
					Timestamp: now,
					Task:      "task-3",
					Reporter:  "code-reviewer-1",
					Type:      "retry_loop",
					Details: map[string]any{
						"error_pattern": "serialization failure on nested entity",
						"count":         4,
					},
				},
			},
			wantTriggered: true,
			wantPattern:   "retry_cluster",
			wantSeverity:  "ARCHITECTURE_FLAW",
			wantEvidence:  "3 retry_loop anomalies with similar error patterns",
		},
		{
			name: "retry_cluster - 3 retry_loops but different error patterns",
			anomalies: []models.Anomaly{
				{
					Timestamp: now,
					Task:      "task-1",
					Reporter:  "coder-1",
					Type:      "retry_loop",
					Details: map[string]any{
						"error_pattern": "timeout",
					},
				},
				{
					Timestamp: now,
					Task:      "task-2",
					Reporter:  "coder-2",
					Type:      "retry_loop",
					Details: map[string]any{
						"error_pattern": "connection refused",
					},
				},
				{
					Timestamp: now,
					Task:      "task-3",
					Reporter:  "code-reviewer-1",
					Type:      "retry_loop",
					Details: map[string]any{
						"error_pattern": "parse error",
					},
				},
			},
			wantTriggered: false,
		},
		{
			name: "debt_accumulation - 3+ trade_offs with debt_created=true",
			anomalies: []models.Anomaly{
				{
					Timestamp: now,
					Task:      "task-1",
					Reporter:  "coder-1",
					Type:      "trade_off",
					Details: map[string]any{
						"what":         "flatten entity instead of fixing serializer",
						"debt_created": true,
					},
				},
				{
					Timestamp: now,
					Task:      "task-2",
					Reporter:  "coder-2",
					Type:      "trade_off",
					Details: map[string]any{
						"what":         "skip validation for now",
						"debt_created": true,
					},
				},
				{
					Timestamp: now,
					Task:      "task-3",
					Reporter:  "coder-1",
					Type:      "trade_off",
					Details: map[string]any{
						"what":         "hardcode value temporarily",
						"debt_created": true,
					},
				},
			},
			wantTriggered: true,
			wantPattern:   "debt_accumulation",
			wantSeverity:  "SCOPE_FLAW",
			wantEvidence:  "3 trade-offs creating technical debt",
		},
		{
			name: "assumption_cascade - 2+ assumption_violated with same assumption",
			anomalies: []models.Anomaly{
				{
					Timestamp: now,
					Task:      "task-1",
					Reporter:  "coder-1",
					Type:      "assumption_violated",
					Details: map[string]any{
						"assumption": "API supports pagination",
						"reality":    "API returns max 100, no cursor",
					},
				},
				{
					Timestamp: now,
					Task:      "task-3",
					Reporter:  "coder-2",
					Type:      "assumption_violated",
					Details: map[string]any{
						"assumption": "API supports pagination",
						"reality":    "No pagination support at all",
					},
				},
			},
			wantTriggered: true,
			wantPattern:   "assumption_cascade",
			wantSeverity:  "SPEC_FLAW",
			wantEvidence:  "Same assumption violated across multiple tasks",
		},
		{
			name: "spec_gap_cluster - 2+ spec_ambiguity with same spec_ref",
			anomalies: []models.Anomaly{
				{
					Timestamp: now,
					Task:      "task-1",
					Reporter:  "coder-1",
					Type:      "spec_ambiguity",
					Details: map[string]any{
						"spec_ref": "specs/requirements.md#FR-012",
						"gap":      "unclear error handling",
					},
				},
				{
					Timestamp: now,
					Task:      "task-2",
					Reporter:  "coder-2",
					Type:      "spec_ambiguity",
					Details: map[string]any{
						"spec_ref": "specs/requirements.md#FR-012",
						"gap":      "missing validation rules",
					},
				},
			},
			wantTriggered: true,
			wantPattern:   "spec_gap_cluster",
			wantSeverity:  "SPEC_FLAW",
			wantEvidence:  "Multiple tasks hitting same spec ambiguity",
		},
		{
			name: "workaround_pattern - 2+ workarounds/trade_offs with similar root_cause",
			anomalies: []models.Anomaly{
				{
					Timestamp: now,
					Task:      "task-1",
					Reporter:  "code-reviewer-1",
					Type:      "workaround",
					Details: map[string]any{
						"what":       "manual cleanup",
						"root_cause": "missing cleanup hook",
					},
				},
				{
					Timestamp: now,
					Task:      "task-2",
					Reporter:  "coder-1",
					Type:      "trade_off",
					Details: map[string]any{
						"what":       "defer cleanup",
						"root_cause": "missing cleanup hook",
					},
				},
			},
			wantTriggered: true,
			wantPattern:   "workaround_pattern",
			wantSeverity:  "ARCHITECTURE_FLAW",
			wantEvidence:  "2 workarounds/trade-offs with similar root causes",
		},
		{
			name: "external_service_outage - 2+ external_blockers with same blocker_service",
			anomalies: []models.Anomaly{
				{
					Timestamp: now,
					Task:      "task-1",
					Reporter:  "coder-1",
					Type:      "external_blocker",
					Details: map[string]any{
						"blocker_service": "GitHub API",
						"details":         "rate limited",
					},
				},
				{
					Timestamp: now,
					Task:      "task-3",
					Reporter:  "coder-2",
					Type:      "external_blocker",
					Details: map[string]any{
						"blocker_service": "GitHub API",
						"details":         "timeout",
					},
				},
			},
			wantTriggered: true,
			wantPattern:   "external_service_outage",
			wantSeverity:  "EXTERNAL_DEPENDENCY",
			wantEvidence:  "Multiple tasks blocked by same external service: GitHub API",
		},
		{
			name: "provider_audit_degradation - 2+ agents for same provider",
			anomalies: []models.Anomaly{
				{
					Timestamp: now,
					Task:      "task-1",
					Reporter:  "coder-1",
					Type:      "provider_audit_degraded",
					Details: map[string]any{
						"provider": "codex",
						"agent_id": "coder-1",
						"message":  "failed to record rollout items",
					},
				},
				{
					Timestamp: now,
					Task:      "task-2",
					Reporter:  "code-reviewer-1",
					Type:      "provider_audit_degraded",
					Details: map[string]any{
						"provider": "codex",
						"agent_id": "code-reviewer-1",
						"message":  "failed to record rollout items",
					},
				},
			},
			wantTriggered: false,
			wantPattern:   "provider_audit_degradation",
			wantSeverity:  "OBSERVABILITY_DEGRADED",
			wantEvidence:  "2 provider_audit_degraded anomalies for provider codex across 2 agents",
			wantResponse:  models.CircuitBreakerResponseCheckpoint,
		},
		{
			name: "provider_audit_degradation - 3+ hits from same agent",
			anomalies: []models.Anomaly{
				{
					Timestamp: now,
					Task:      "task-1",
					Reporter:  "coder-1",
					Type:      "provider_audit_degraded",
					Details: map[string]any{
						"provider": "codex",
						"agent_id": "coder-1",
						"message":  "failed to record rollout items",
					},
				},
				{
					Timestamp: now,
					Task:      "task-2",
					Reporter:  "coder-1",
					Type:      "provider_audit_degraded",
					Details: map[string]any{
						"provider": "codex",
						"agent_id": "coder-1",
						"message":  "failed to record rollout items",
					},
				},
				{
					Timestamp: now,
					Task:      "task-3",
					Reporter:  "coder-1",
					Type:      "provider_audit_degraded",
					Details: map[string]any{
						"provider": "codex",
						"agent_id": "coder-1",
						"message":  "failed to record rollout items",
					},
				},
			},
			wantTriggered: false,
			wantPattern:   "provider_audit_degradation",
			wantSeverity:  "OBSERVABILITY_DEGRADED",
			wantEvidence:  "3 provider_audit_degraded anomalies for provider codex across 1 agents",
			wantResponse:  models.CircuitBreakerResponseCheckpoint,
		},
		{
			name: "provider_audit_degradation - one hit below threshold",
			anomalies: []models.Anomaly{
				{
					Timestamp: now,
					Task:      "task-1",
					Reporter:  "coder-1",
					Type:      "provider_audit_degraded",
					Details: map[string]any{
						"provider": "codex",
						"agent_id": "coder-1",
						"message":  "failed to record rollout items",
					},
				},
			},
			wantTriggered: false,
		},
		{
			name: "multiple patterns - returns first match (retry_cluster)",
			anomalies: []models.Anomaly{
				// retry_cluster pattern
				{
					Timestamp: now,
					Task:      "task-1",
					Type:      "retry_loop",
					Details: map[string]any{
						"error_pattern": "timeout",
					},
				},
				{
					Timestamp: now,
					Task:      "task-2",
					Type:      "retry_loop",
					Details: map[string]any{
						"error_pattern": "timeout",
					},
				},
				{
					Timestamp: now,
					Task:      "task-3",
					Type:      "retry_loop",
					Details: map[string]any{
						"error_pattern": "timeout",
					},
				},
				// debt_accumulation pattern
				{
					Timestamp: now,
					Task:      "task-4",
					Type:      "trade_off",
					Details: map[string]any{
						"debt_created": true,
					},
				},
				{
					Timestamp: now,
					Task:      "task-5",
					Type:      "trade_off",
					Details: map[string]any{
						"debt_created": true,
					},
				},
				{
					Timestamp: now,
					Task:      "task-6",
					Type:      "trade_off",
					Details: map[string]any{
						"debt_created": true,
					},
				},
			},
			wantTriggered: true,
			wantPattern:   "retry_cluster",
			wantSeverity:  "ARCHITECTURE_FLAW",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectPatterns(tt.anomalies)

			if result.Triggered != tt.wantTriggered {
				t.Errorf("DetectPatterns() triggered = %v, want %v", result.Triggered, tt.wantTriggered)
			}

			if tt.wantPattern == "" {
				return
			}

			if result.Pattern != tt.wantPattern {
				t.Errorf("DetectPatterns() pattern = %v, want %v", result.Pattern, tt.wantPattern)
			}

			if result.Severity != tt.wantSeverity {
				t.Errorf("DetectPatterns() severity = %v, want %v", result.Severity, tt.wantSeverity)
			}

			if tt.wantEvidence != "" && result.Evidence != tt.wantEvidence {
				t.Errorf("DetectPatterns() evidence = %v, want %v", result.Evidence, tt.wantEvidence)
			}
			if tt.wantResponse != "" && result.Response != tt.wantResponse {
				t.Errorf("DetectPatterns() response = %v, want %v", result.Response, tt.wantResponse)
			}
		})
	}
}

func TestDetectUnacknowledgedPatterns(t *testing.T) {
	watermark1 := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	watermark2 := time.Date(2026, 5, 20, 11, 0, 0, 0, time.UTC)
	afterWatermark := watermark2.Add(time.Minute)
	okPattern := "retry_cluster"
	triggerPattern := "provider_audit_degradation"
	trigger := &models.CircuitBreakerTrigger{
		Timestamp:  watermark2,
		Pattern:    triggerPattern,
		Severity:   "OBSERVABILITY_DEGRADED",
		ReportFile: paths.ProjectDirName() + "/circuit_breaker_report.md",
	}

	tests := []struct {
		name                string
		state               *models.State
		wantTriggered       bool
		wantSuppressedCount int
		wantConsideredCount int
	}{
		{
			name: "cleared trigger suppresses anomalies at or before latest triggered history timestamp",
			state: &models.State{
				Anomalies: []models.Anomaly{
					providerAuditAnomaly(watermark1, "coder-1"),
					providerAuditAnomaly(watermark2, "coder-2"),
					providerAuditAnomaly(afterWatermark, "coder-1"),
				},
				CircuitBreaker: models.CircuitBreaker{
					Status: "OK",
					History: []models.CircuitBreakerHistory{
						{Timestamp: watermark2, Pattern: &triggerPattern, Severity: stringPtr("OBSERVABILITY_DEGRADED"), Result: "TRIGGERED"},
					},
				},
			},
			wantTriggered:       false,
			wantSuppressedCount: 2,
			wantConsideredCount: 3,
		},
		{
			name: "latest triggered history wins and later OK entries do not move watermark",
			state: &models.State{
				Anomalies: []models.Anomaly{
					providerAuditAnomaly(watermark1.Add(time.Minute), "old-coder"),
					providerAuditAnomaly(afterWatermark, "coder-1"),
					providerAuditAnomaly(afterWatermark.Add(time.Minute), "coder-2"),
				},
				CircuitBreaker: models.CircuitBreaker{
					Status: "OK",
					History: []models.CircuitBreakerHistory{
						{Timestamp: watermark1, Pattern: &okPattern, Severity: stringPtr("ARCHITECTURE_FLAW"), Result: "TRIGGERED"},
						{Timestamp: watermark2.Add(2 * time.Hour), Result: "OK"},
						{Timestamp: watermark2, Pattern: &triggerPattern, Severity: stringPtr("OBSERVABILITY_DEGRADED"), Result: "TRIGGERED"},
					},
				},
			},
			wantTriggered:       false,
			wantSuppressedCount: 1,
			wantConsideredCount: 3,
		},
		{
			name: "active triggered status does not use acknowledgement watermark",
			state: &models.State{
				Anomalies: []models.Anomaly{
					providerAuditAnomaly(watermark1, "coder-1"),
					providerAuditAnomaly(watermark1.Add(time.Minute), "coder-2"),
				},
				CircuitBreaker: models.CircuitBreaker{
					Status: "TRIGGERED",
					History: []models.CircuitBreakerHistory{
						{Timestamp: watermark2, Pattern: &triggerPattern, Severity: stringPtr("OBSERVABILITY_DEGRADED"), Result: "TRIGGERED"},
					},
				},
			},
			wantTriggered:       false,
			wantSuppressedCount: 0,
			wantConsideredCount: 2,
		},
		{
			name: "non-nil current trigger does not use acknowledgement watermark",
			state: &models.State{
				Anomalies: []models.Anomaly{
					providerAuditAnomaly(watermark1, "coder-1"),
					providerAuditAnomaly(watermark1.Add(time.Minute), "coder-2"),
				},
				CircuitBreaker: models.CircuitBreaker{
					Status:         "OK",
					CurrentTrigger: trigger,
					History: []models.CircuitBreakerHistory{
						{Timestamp: watermark2, Pattern: &triggerPattern, Severity: stringPtr("OBSERVABILITY_DEGRADED"), Result: "TRIGGERED"},
					},
				},
			},
			wantTriggered:       false,
			wantSuppressedCount: 0,
			wantConsideredCount: 2,
		},
		{
			name: "no trigger history preserves existing detection behavior",
			state: &models.State{
				Anomalies: []models.Anomaly{
					providerAuditAnomaly(watermark1, "coder-1"),
					providerAuditAnomaly(watermark1.Add(time.Minute), "coder-2"),
				},
				CircuitBreaker: models.CircuitBreaker{Status: "OK"},
			},
			wantTriggered:       false,
			wantSuppressedCount: 0,
			wantConsideredCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, considered, suppressedCount := DetectUnacknowledgedPatterns(tt.state)
			if result.Triggered != tt.wantTriggered {
				t.Errorf("Triggered = %v, want %v", result.Triggered, tt.wantTriggered)
			}
			if suppressedCount != tt.wantSuppressedCount {
				t.Errorf("suppressedCount = %d, want %d", suppressedCount, tt.wantSuppressedCount)
			}
			if len(considered) != tt.wantConsideredCount {
				t.Errorf("considered count = %d, want %d", len(considered), tt.wantConsideredCount)
			}
		})
	}
}

func TestResolvedProviderResponseDoesNotAcknowledgeRetryEvidence(t *testing.T) {
	boundary := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	resolvedAt := boundary.Add(time.Minute)
	state := &models.State{
		Anomalies: []models.Anomaly{
			{Timestamp: boundary.Add(-3 * time.Minute), Type: "retry_loop", Details: map[string]any{"error_pattern": "provider timeout"}},
			{Timestamp: boundary.Add(-2 * time.Minute), Type: "retry_loop", Details: map[string]any{"error_pattern": "provider timeout"}},
			{Timestamp: boundary.Add(-time.Minute), Type: "retry_loop", Details: map[string]any{"error_pattern": "provider timeout"}},
		},
		CircuitBreaker: models.CircuitBreaker{
			Status: "OK",
			History: []models.CircuitBreakerHistory{{
				Timestamp:  boundary,
				Result:     "CHECKPOINT",
				Response:   models.CircuitBreakerResponseCheckpoint,
				ResolvedAt: &resolvedAt,
			}},
		},
	}

	result, considered, suppressedCount := DetectUnacknowledgedPatterns(state)
	if result.Pattern != "retry_cluster" || !result.Triggered || result.Response != models.CircuitBreakerResponseHalt {
		t.Fatalf("result = {pattern:%q triggered:%v response:%q}, want retry_cluster/true/HALT", result.Pattern, result.Triggered, result.Response)
	}
	if len(considered) != 3 || suppressedCount != 0 {
		t.Fatalf("considered = %d, suppressed = %d, want 3 and 0", len(considered), suppressedCount)
	}
}

func TestProviderAuditEvidenceLifecycle(t *testing.T) {
	boundary := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	registeredAt := boundary.Add(-time.Hour)
	resolvedAt := boundary.Add(time.Minute)
	resolvedBoundary := models.CircuitBreaker{
		Status: "OK",
		History: []models.CircuitBreakerHistory{{
			Timestamp: boundary, Result: "CHECKPOINT",
			Response: models.CircuitBreakerResponseCheckpoint, Classification: models.CircuitBreakerEvidenceNew,
			ResolvedAt: &resolvedAt,
		}},
	}
	assertResult := func(t *testing.T, state *models.State, wantClass models.CircuitBreakerEvidenceClass, wantResponse models.CircuitBreakerResponseType, wantTriggered bool) PatternResult {
		t.Helper()
		result, _, _ := DetectUnacknowledgedPatterns(state)
		if result.Classification != wantClass || result.Response != wantResponse || result.Triggered != wantTriggered {
			t.Errorf("result = {class:%q response:%q triggered:%v}, want {%q %q %v}", result.Classification, result.Response, result.Triggered, wantClass, wantResponse, wantTriggered)
		}
		return result
	}

	lifecycleCases := []struct {
		name      string
		anomalies []models.Anomaly
		resolved  bool
		class     models.CircuitBreakerEvidenceClass
		response  models.CircuitBreakerResponseType
		unknown   bool
	}{
		{
			name: "qualifying historical evidence is acknowledged warning", resolved: true,
			anomalies: []models.Anomaly{providerAuditAnomaly(boundary.Add(-2*time.Minute), "coder-1"), providerAuditAnomaly(boundary.Add(-time.Minute), "coder-2")},
			class:     models.CircuitBreakerEvidenceAcknowledgedHistorical, response: models.CircuitBreakerResponseWarning,
		},
		{
			name:      "fresh qualifying evidence checkpoints without current health proof",
			anomalies: []models.Anomaly{providerAuditAnomaly(boundary.Add(time.Minute), "coder-1"), providerAuditAnomaly(boundary.Add(2*time.Minute), "coder-2")},
			class:     models.CircuitBreakerEvidenceNew, response: models.CircuitBreakerResponseCheckpoint, unknown: true,
		},
		{
			name: "new occurrence continues qualifying historical group", resolved: true,
			anomalies: []models.Anomaly{providerAuditAnomaly(boundary.Add(-2*time.Minute), "coder-1"), providerAuditAnomaly(boundary.Add(-time.Minute), "coder-2"), providerAuditAnomaly(boundary.Add(time.Minute), "coder-1")},
			class:     models.CircuitBreakerEvidenceContinuing, response: models.CircuitBreakerResponseCheckpoint,
		},
		{
			name: "new distinct agent completes historical sub-threshold group", resolved: true,
			anomalies: []models.Anomaly{providerAuditAnomaly(boundary.Add(-time.Minute), "coder-1"), providerAuditAnomaly(boundary.Add(time.Minute), "coder-2")},
			class:     models.CircuitBreakerEvidenceContinuing, response: models.CircuitBreakerResponseCheckpoint,
		},
	}
	for _, tt := range lifecycleCases {
		t.Run(tt.name, func(t *testing.T) {
			state := &models.State{Anomalies: tt.anomalies}
			if tt.resolved {
				state.CircuitBreaker = resolvedBoundary
			}
			result := assertResult(t, state, tt.class, tt.response, false)
			if tt.unknown && !strings.Contains(strings.ToLower(result.Explanation), "unknown") {
				t.Errorf("Explanation = %q, want unknown-health reason", result.Explanation)
			}
		})
	}

	t.Run("mixed providers prioritize current response with deterministic tie break", func(t *testing.T) {
		state := &models.State{
			Anomalies: []models.Anomaly{
				providerAuditAnomalyForProvider(boundary.Add(-2*time.Minute), "historical-1", "archive"),
				providerAuditAnomalyForProvider(boundary.Add(-time.Minute), "historical-2", "archive"),
				providerAuditAnomalyForProvider(boundary.Add(time.Minute), "zeta-1", "zeta"),
				providerAuditAnomalyForProvider(boundary.Add(2*time.Minute), "zeta-2", "zeta"),
				providerAuditAnomalyForProvider(boundary.Add(3*time.Minute), "alpha-1", "alpha"),
				providerAuditAnomalyForProvider(boundary.Add(4*time.Minute), "alpha-2", "alpha"),
			},
			CircuitBreaker: resolvedBoundary,
		}

		for range 50 {
			result := assertResult(t, state, models.CircuitBreakerEvidenceNew, models.CircuitBreakerResponseCheckpoint, false)
			if !strings.Contains(result.Evidence, "provider alpha") {
				t.Fatalf("Evidence = %q, want deterministic alpha provider tie break", result.Evidence)
			}
		}
	})

	t.Run("all exact current provider epochs degraded promotes halt", func(t *testing.T) {
		state := freshProviderAuditState(boundary)
		state.Agents = map[string]models.Agent{
			"coder-1": providerAgent("codex", 101, registeredAt), "coder-2": providerAgent("codex", 102, registeredAt),
		}
		state.AgentHealth = map[string]models.AgentHealth{
			"coder-1": degradedProviderHealth("codex", 101, registeredAt), "coder-2": degradedProviderHealth("codex", 102, registeredAt),
		}
		assertResult(t, state, models.CircuitBreakerEvidenceNew, models.CircuitBreakerResponseHalt, true)
	})

	unknownHealthCases := []struct {
		name  string
		setup func(*models.State)
	}{
		{"alias-only provider registration", func(s *models.State) { setProviderEpoch(s, "codex-acp", "codex-acp", 101, registeredAt) }},
		{"empty registered provider identity", func(s *models.State) { setProviderEpoch(s, "", "", 101, registeredAt) }},
		{"missing health record", func(s *models.State) {
			s.Agents = map[string]models.Agent{"coder-1": providerAgent("codex", 101, registeredAt)}
		}},
		{"mismatched health provider", func(s *models.State) { setProviderEpoch(s, "codex", "codex-acp", 101, registeredAt) }},
		{"stale health pid", func(s *models.State) { setProviderEpoch(s, "codex", "codex", 100, registeredAt) }},
		{"stale health registration time", func(s *models.State) {
			setProviderEpoch(s, "codex", "codex", 101, registeredAt)
			stale := registeredAt.Add(-time.Minute)
			health := s.AgentHealth["coder-1"]
			health.RegisteredAt = &stale
			s.AgentHealth["coder-1"] = health
		}},
		{"one exact match lacks health proof", func(s *models.State) {
			setProviderEpoch(s, "codex", "codex", 101, registeredAt)
			s.Agents["coder-2"] = providerAgent("codex", 102, registeredAt)
		}},
	}
	for _, tt := range unknownHealthCases {
		t.Run(tt.name, func(t *testing.T) {
			state := freshProviderAuditState(boundary)
			tt.setup(state)
			result := assertResult(t, state, models.CircuitBreakerEvidenceNew, models.CircuitBreakerResponseCheckpoint, false)
			if !strings.Contains(strings.ToLower(result.Explanation), "unknown") {
				t.Errorf("Explanation = %q, want unknown-health reason", result.Explanation)
			}
		})
	}
}

func TestProviderAuditSupersededCheckpointDoesNotAcknowledgeEvidence(t *testing.T) {
	boundary := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	pattern := "provider_audit_degradation"
	state := &models.State{
		Anomalies: []models.Anomaly{
			providerAuditAnomaly(boundary.Add(-2*time.Minute), "coder-1"),
			providerAuditAnomaly(boundary.Add(-time.Minute), "coder-2"),
		},
		CircuitBreaker: models.CircuitBreaker{
			Status: "OK",
			History: []models.CircuitBreakerHistory{{
				Timestamp:            boundary,
				Pattern:              &pattern,
				Result:               "CHECKPOINT",
				Response:             models.CircuitBreakerResponseCheckpoint,
				Classification:       models.CircuitBreakerEvidenceNew,
				SupersededByResponse: models.CircuitBreakerResponseHalt,
			}},
		},
	}

	if watermark, ok := latestResolvedResponseWatermark(state); ok {
		t.Fatalf("latestResolvedResponseWatermark() = %s, true; want zero, false for unresolved superseded checkpoint", watermark)
	}

	result, _, _ := DetectUnacknowledgedPatterns(state)
	if result.Classification != models.CircuitBreakerEvidenceNew || result.Response != models.CircuitBreakerResponseCheckpoint {
		t.Fatalf("unacknowledged superseded evidence = {classification:%q response:%q}, want NEW/CHECKPOINT", result.Classification, result.Response)
	}
}

func TestDetectPlanningReviewChurn(t *testing.T) {
	watermark := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	triggerPattern := "planning_review_churn"
	clearedCircuitBreaker := func() models.CircuitBreaker {
		return models.CircuitBreaker{
			Status: "OK",
			History: []models.CircuitBreakerHistory{
				{
					Timestamp: watermark,
					Pattern:   &triggerPattern,
					Severity:  stringPtr("PLANNING_CONVERGENCE_DEGRADED"),
					Result:    "TRIGGERED",
				},
			},
		}
	}
	assertPlanningChurn := func(t *testing.T, state *models.State, wantEvidence string) {
		t.Helper()

		result, _, _ := DetectUnacknowledgedPatterns(state)
		if !result.Triggered {
			t.Fatal("Triggered = false, want true")
		}
		if result.Pattern != "planning_review_churn" {
			t.Errorf("Pattern = %q, want planning_review_churn", result.Pattern)
		}
		if result.Severity != "PLANNING_CONVERGENCE_DEGRADED" {
			t.Errorf("Severity = %q, want PLANNING_CONVERGENCE_DEGRADED", result.Severity)
		}
		if result.Evidence != wantEvidence {
			t.Errorf("Evidence = %q, want %q", result.Evidence, wantEvidence)
		}
	}

	t.Run("durable review cycle total triggers after merge", func(t *testing.T) {
		state := &models.State{Tasks: []models.Task{{
			ID:                "plan-counter",
			Type:              models.TaskTypePlanning,
			Status:            models.TaskStatusMerged,
			ReviewCyclesTotal: 4,
		}}}

		assertPlanningChurn(t, state, "planning task plan-counter with status MERGED has 4 durable rejection cycles")
	})

	t.Run("timestamped rejection history triggers after merge when total is zero", func(t *testing.T) {
		state := &models.State{Tasks: []models.Task{{
			ID:     "plan-history",
			Type:   models.TaskTypePlanning,
			Status: models.TaskStatusMerged,
			History: []models.TaskHistoryEntry{
				{Time: watermark.Add(-4 * time.Minute), Event: models.TaskEventRejected},
				{Time: watermark.Add(-3 * time.Minute), Event: models.TaskEventReviewVerdictRejected},
				{Time: watermark.Add(-2 * time.Minute), Event: models.TaskEventRejected},
				{Time: watermark.Add(-time.Minute), Event: models.TaskEventReviewVerdictRejected},
			},
		}}}

		assertPlanningChurn(t, state, "planning task plan-history with status MERGED has 4 durable rejection cycles")
	})

	t.Run("count three does not trigger", func(t *testing.T) {
		state := &models.State{Tasks: []models.Task{{
			ID:                "plan-low-churn",
			Type:              models.TaskTypePlanning,
			Status:            models.TaskStatusMerged,
			ReviewCyclesTotal: 3,
		}}}

		result, _, _ := DetectUnacknowledgedPatterns(state)
		if result.Triggered {
			t.Errorf("Triggered = true, want false; result = %+v", result)
		}
	})

	t.Run("non-planning task does not trigger", func(t *testing.T) {
		state := &models.State{Tasks: []models.Task{{
			ID:                "coding-churn",
			Type:              models.TaskTypeCoding,
			Status:            models.TaskStatusMerged,
			ReviewCyclesTotal: 4,
		}}}

		result, _, _ := DetectUnacknowledgedPatterns(state)
		if result.Triggered {
			t.Errorf("Triggered = true, want false; result = %+v", result)
		}
	})

	t.Run("cleared trigger suppresses unchanged rejection history", func(t *testing.T) {
		state := &models.State{
			Tasks: []models.Task{{
				ID:                "plan-cleared",
				Type:              models.TaskTypePlanning,
				Status:            models.TaskStatusMerged,
				ReviewCyclesTotal: 4,
				History: []models.TaskHistoryEntry{
					{Time: watermark.Add(-3 * time.Minute), Event: models.TaskEventRejected},
					{Time: watermark.Add(-2 * time.Minute), Event: models.TaskEventRejected},
					{Time: watermark.Add(-time.Minute), Event: models.TaskEventReviewVerdictRejected},
					{Time: watermark, Event: models.TaskEventReviewVerdictRejected},
				},
			}},
			CircuitBreaker: clearedCircuitBreaker(),
		}

		result, _, _ := DetectUnacknowledgedPatterns(state)
		if result.Triggered {
			t.Errorf("Triggered = true, want false; result = %+v", result)
		}
	})

	t.Run("later rejection retriggers after cleared watermark", func(t *testing.T) {
		state := &models.State{
			Tasks: []models.Task{{
				ID:     "plan-retriggered",
				Type:   models.TaskTypePlanning,
				Status: models.TaskStatusMerged,
				History: []models.TaskHistoryEntry{
					{Time: watermark.Add(-3 * time.Minute), Event: models.TaskEventRejected},
					{Time: watermark.Add(-2 * time.Minute), Event: models.TaskEventRejected},
					{Time: watermark.Add(-time.Minute), Event: models.TaskEventReviewVerdictRejected},
					{Time: watermark, Event: models.TaskEventReviewVerdictRejected},
					{Time: watermark.Add(time.Minute), Event: models.TaskEventRejected},
				},
			}},
			CircuitBreaker: clearedCircuitBreaker(),
		}

		assertPlanningChurn(t, state, "planning task plan-retriggered with status MERGED has 5 durable rejection cycles")
	})

	t.Run("existing anomaly pattern keeps priority", func(t *testing.T) {
		state := &models.State{
			Anomalies: []models.Anomaly{
				{Type: "retry_loop", Details: map[string]any{"error_pattern": "timeout"}},
				{Type: "retry_loop", Details: map[string]any{"error_pattern": "timeout"}},
				{Type: "retry_loop", Details: map[string]any{"error_pattern": "timeout"}},
			},
			Tasks: []models.Task{{
				ID:                "plan-with-anomaly",
				Type:              models.TaskTypePlanning,
				Status:            models.TaskStatusMerged,
				ReviewCyclesTotal: 4,
			}},
		}

		result, _, _ := DetectUnacknowledgedPatterns(state)
		if !result.Triggered {
			t.Fatal("Triggered = false, want true")
		}
		if result.Pattern != "retry_cluster" {
			t.Errorf("Pattern = %q, want retry_cluster", result.Pattern)
		}
		if result.Severity != "ARCHITECTURE_FLAW" {
			t.Errorf("Severity = %q, want ARCHITECTURE_FLAW", result.Severity)
		}
	})
}

func TestGenerateReport(t *testing.T) {
	now := time.Date(2025, 1, 18, 17, 30, 0, 0, time.UTC)

	anomalies := []models.Anomaly{
		{
			Timestamp: now,
			Task:      "task-3",
			Reporter:  "coder-1",
			Type:      "retry_loop",
			Details: map[string]any{
				"count":         3,
				"error_pattern": "serialization failure on nested entity",
			},
		},
		{
			Timestamp: now,
			Task:      "task-5",
			Reporter:  "code-reviewer-1",
			Type:      "retry_loop",
			Details: map[string]any{
				"count":         2,
				"error_pattern": "serialization failure on nested entity",
			},
		},
	}

	result := PatternResult{
		Triggered:      true,
		Pattern:        "retry_cluster",
		Severity:       "ARCHITECTURE_FLAW",
		Evidence:       "3 retry_loop anomalies with similar error patterns",
		Response:       models.CircuitBreakerResponseHalt,
		Classification: models.CircuitBreakerEvidenceNew,
		Explanation:    "retry failures prove an architecture flaw",
	}

	report := GenerateReport(result, anomalies, now, 0)

	// Verify report contains key sections
	if report == "" {
		t.Fatal("GenerateReport() returned empty report")
	}

	expectedSections := []string{
		"# Circuit Breaker Report",
		"**Analyzed:**",
		"**Pattern:** retry_cluster",
		"**Severity:** ARCHITECTURE_FLAW",
		"**Evidence class:** NEW",
		"**Response:** HALT",
		"**Explanation:** retry failures prove an architecture flaw",
		"## Evidence",
		"3 retry_loop anomalies with similar error patterns",
		"## Response Action",
		"Circuit breaker triggered and execution halted.",
		"Run `" + brand.Command("resume") + "` after remediation.",
		"## Anomalies (trimmed)",
		"## Anomalies (raw)",
		"## Human Decision Required",
		"- [ ] Acknowledge report",
		"- [ ] Confirm severity assessment",
		"- [ ] Determine remediation",
		"- [ ] Release checkpoint with decision logged",
	}

	for _, section := range expectedSections {
		if !contains(report, section) {
			t.Errorf("GenerateReport() missing expected section: %q", section)
		}
	}
}

func TestGenerateReportProjectsProviderAuditResponse(t *testing.T) {
	now := time.Date(2026, 8, 25, 9, 30, 0, 0, time.UTC)
	tests := []struct {
		name    string
		result  PatternResult
		want    []string
		notWant []string
	}{
		{
			name: "acknowledged historical warning has no state action",
			result: PatternResult{
				Pattern:        "provider_audit_degradation",
				Severity:       "OBSERVABILITY_DEGRADED",
				Evidence:       "historical provider-audit evidence",
				Response:       models.CircuitBreakerResponseWarning,
				Classification: models.CircuitBreakerEvidenceAcknowledgedHistorical,
				Explanation:    "qualifying evidence is entirely at or before the resolved response boundary",
			},
			want: []string{
				"**Response:** WARNING",
				"**Evidence class:** ACKNOWLEDGED_HISTORICAL",
				"**Explanation:** qualifying evidence is entirely at or before the resolved response boundary",
				"No state action — this evidence was already acknowledged.",
			},
			notWant: []string{"triggered", "Run `" + brand.Command("resume") + "`"},
		},
		{
			name: "continuing evidence checkpoints with unknown alias health",
			result: PatternResult{
				Pattern:        "provider_audit_degradation",
				Severity:       "OBSERVABILITY_DEGRADED",
				Evidence:       "continuing provider-audit evidence",
				Response:       models.CircuitBreakerResponseCheckpoint,
				Classification: models.CircuitBreakerEvidenceContinuing,
				Explanation:    "current health is unknown: provider codex has no exact match for registered alias codex-acp",
			},
			want: []string{
				"**Severity:** OBSERVABILITY_DEGRADED",
				"**Response:** CHECKPOINT",
				"**Evidence class:** CONTINUING",
				"**Explanation:** current health is unknown: provider codex has no exact match for registered alias codex-acp",
				"Sprint moved to CHECKPOINT.",
				"Run `" + brand.Command("resume") + "` to continue.",
			},
			notWant: []string{"triggered"},
		},
		{
			name: "halt names exact current provider epoch proof",
			result: PatternResult{
				Triggered:      true,
				Pattern:        "provider_audit_degradation",
				Severity:       "OBSERVABILITY_DEGRADED",
				Evidence:       "current provider-audit evidence",
				Response:       models.CircuitBreakerResponseHalt,
				Classification: models.CircuitBreakerEvidenceNew,
				Explanation:    "all 2 exact provider codex matches have degraded health for the same agent ID, provider, PID, and registration-time epoch",
			},
			want: []string{
				"**Response:** HALT",
				"**Explanation:** all 2 exact provider codex matches have degraded health for the same agent ID, provider, PID, and registration-time epoch",
				"Circuit breaker triggered and execution halted.",
				"Run `" + brand.Command("resume") + "` after remediation.",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := GenerateReport(tt.result, nil, now, 0)
			for _, want := range tt.want {
				if !strings.Contains(report, want) {
					t.Errorf("report missing %q\n%s", want, report)
				}
			}
			lowerReport := strings.ToLower(report)
			for _, notWant := range tt.notWant {
				if strings.Contains(lowerReport, strings.ToLower(notWant)) {
					t.Errorf("report unexpectedly contains %q\n%s", notWant, report)
				}
			}
		})
	}
}

func TestGenerateReportTrimsProviderAuditMessages(t *testing.T) {
	now := time.Date(2026, 5, 14, 10, 45, 25, 0, time.UTC)
	longOutput := strings.Repeat("SUPPORT_FULL_TEXT ", 200)
	message := "{\"type\":\"item.completed\",\"item\":{\"type\":\"command_execution\",\"command\":\"/usr/bin/zsh -lc 'cat " + paths.ProjectDirName() + "/SUPPORT.md'\",\"aggregated_output\":\"" + longOutput + `","exit_code":0,"status":"completed"}}`
	anomalies := []models.Anomaly{
		{
			Timestamp: now,
			Reporter:  "orchestrator-1",
			Type:      "provider_audit_degraded",
			Details: map[string]any{
				"provider": "codex",
				"agent_id": "orchestrator-1",
				"impact":   "provider transcript or rollout persistence may be incomplete",
				"message":  message,
			},
		},
	}

	report := GenerateReport(PatternResult{
		Pattern:        "provider_audit_degradation",
		Severity:       "OBSERVABILITY_DEGRADED",
		Evidence:       "1 provider_audit_degraded anomaly",
		Response:       models.CircuitBreakerResponseCheckpoint,
		Classification: models.CircuitBreakerEvidenceNew,
		Explanation:    "current provider health is unknown",
	}, anomalies, now, 0)

	trimmedStart := strings.Index(report, "## Anomalies (trimmed)")
	rawStart := strings.Index(report, "## Anomalies (raw)")
	if trimmedStart == -1 {
		t.Fatal("report missing trimmed anomalies section")
	}
	if rawStart == -1 {
		t.Fatal("report missing raw anomalies section")
	}
	if trimmedStart > rawStart {
		t.Fatal("trimmed anomalies section should appear before raw anomalies section")
	}

	trimmedSection := report[trimmedStart:rawStart]
	expectedTrimmed := []string{
		"provider: `codex`",
		"agent_id: `orchestrator-1`",
		"provider_event: `item.completed / command_execution`",
		"command: `/usr/bin/zsh -lc 'cat " + paths.ProjectDirName() + "/SUPPORT.md'`",
		"status: `completed`, exit_code: `0`",
		"aggregated_output_chars:",
	}
	for _, want := range expectedTrimmed {
		if !strings.Contains(trimmedSection, want) {
			t.Errorf("trimmed section missing %q\nsection:\n%s", want, trimmedSection)
		}
	}
	if strings.Contains(trimmedSection, "SUPPORT_FULL_TEXT") {
		t.Fatal("trimmed section should not include raw aggregated output")
	}
	if !strings.Contains(report[rawStart:], "SUPPORT_FULL_TEXT") {
		t.Fatal("raw section should preserve full anomaly payload")
	}
}

func TestGenerateReportIncludesSuppressedAcknowledgedAnomalyCount(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	report := GenerateReport(PatternResult{
		Pattern:        "provider_audit_degradation",
		Severity:       "OBSERVABILITY_DEGRADED",
		Evidence:       "2 provider_audit_degraded anomalies for provider codex across 2 agents",
		Response:       models.CircuitBreakerResponseWarning,
		Classification: models.CircuitBreakerEvidenceAcknowledgedHistorical,
		Explanation:    "historical evidence was acknowledged",
	}, []models.Anomaly{
		providerAuditAnomaly(now, "coder-1"),
		providerAuditAnomaly(now.Add(time.Minute), "coder-2"),
	}, now, 4)

	if !strings.Contains(report, "**Acknowledged anomalies suppressed:** 4") {
		t.Fatalf("report missing suppressed anomaly count:\n%s", report)
	}
}

func providerAuditAnomaly(timestamp time.Time, agentID string) models.Anomaly {
	return providerAuditAnomalyForProvider(timestamp, agentID, "codex")
}

func providerAuditAnomalyForProvider(timestamp time.Time, agentID, provider string) models.Anomaly {
	return models.Anomaly{
		Timestamp: timestamp,
		Reporter:  agentID,
		Type:      "provider_audit_degraded",
		Details: map[string]any{
			"provider": provider,
			"agent_id": agentID,
			"message":  "failed to record rollout items",
		},
	}
}

func freshProviderAuditState(timestamp time.Time) *models.State {
	return &models.State{Anomalies: []models.Anomaly{
		providerAuditAnomaly(timestamp, "coder-1"),
		providerAuditAnomaly(timestamp.Add(time.Minute), "coder-2"),
	}}
}

func providerAgent(provider string, pid int, registeredAt time.Time) models.Agent {
	return models.Agent{Provider: provider, PID: pid, RegisteredAt: registeredAt}
}

func degradedProviderHealth(provider string, pid int, registeredAt time.Time) models.AgentHealth {
	return models.AgentHealth{
		State:        models.AgentHealthDegraded,
		Provider:     provider,
		PID:          pid,
		RegisteredAt: &registeredAt,
	}
}

func setProviderEpoch(state *models.State, agentProvider, healthProvider string, healthPID int, registeredAt time.Time) {
	state.Agents = map[string]models.Agent{"coder-1": providerAgent(agentProvider, 101, registeredAt)}
	state.AgentHealth = map[string]models.AgentHealth{"coder-1": degradedProviderHealth(healthProvider, healthPID, registeredAt)}
}

func stringPtr(value string) *string {
	return &value
}

// TestDetectPatterns_PendingMergeStallsDoNotFormRetryCluster pins why merge
// stalls have their own anomaly type: retry_cluster's default response is
// HALT, and repeated stalls of one supervisor loop are not a retry cluster
// inside task execution. Exclusion is by type, so the stall records here even
// share an error_pattern.
func TestDetectPatterns_PendingMergeStallsDoNotFormRetryCluster(t *testing.T) {
	now := time.Now()
	stalls := func() []models.Anomaly {
		var out []models.Anomaly
		for i := range 3 {
			out = append(out, models.Anomaly{
				Timestamp: now.Add(time.Duration(i) * time.Minute),
				Reporter:  "code-reviewer-1",
				Type:      "pending_merge_stalled",
				Details: map[string]any{
					"agent_id":      "code-reviewer-1",
					"role":          "code-reviewer",
					"rounds":        21,
					"error_pattern": "pending merge did not converge",
				},
			})
		}
		return out
	}
	retry := func(pattern string) models.Anomaly {
		return models.Anomaly{Timestamp: now, Task: "task-1", Reporter: "coder-1", Type: "retry_loop",
			Details: map[string]any{"count": 3, "error_pattern": pattern}}
	}

	t.Run("stalls alone never trigger", func(t *testing.T) {
		anomalies := append(stalls(), retry("connection refused"), retry("timeout"))
		if result := DetectPatterns(anomalies); result.Triggered {
			t.Errorf("DetectPatterns() = %s (%s), want no trigger from merge stalls", result.Pattern, result.Evidence)
		}
	})

	t.Run("genuine cluster still triggers and counts only retry loops", func(t *testing.T) {
		anomalies := append(stalls(), retry("connection refused"), retry("connection refused"), retry("timeout"))
		result := DetectPatterns(anomalies)
		if result.Pattern != "retry_cluster" {
			t.Fatalf("DetectPatterns() pattern = %q, want retry_cluster", result.Pattern)
		}
		if !strings.HasPrefix(result.Evidence, "3 retry_loop anomalies") {
			t.Errorf("evidence = %q, want it to count the 3 retry loops and not the stalls", result.Evidence)
		}
	})
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || contains(s[1:], substr)))
}
