package analysis

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

// C2 P3: provider-audit grouping and registered-agent membership compare
// canonical provider keys supplied by the caller; the exact health epoch stays
// bound to the recorded tool name, PID and registration time.

// c2Keys stands for a resolved registry: codex and codex-acp share one key, two
// custom tools declare another, and every other tool is keyed by its name.
func c2Keys(recorded string) string {
	switch recorded {
	case "codex", "codex-acp":
		return "codex"
	case "custom-a", "custom-b":
		return "shared"
	default:
		return recorded
	}
}

func c2Anomalies(base time.Time, providers ...string) []models.Anomaly {
	anomalies := make([]models.Anomaly, 0, len(providers))
	for i, provider := range providers {
		agentID := "coder-" + string(rune('1'+i))
		anomalies = append(anomalies, providerAuditAnomalyForProvider(base.Add(time.Duration(i)*time.Minute), agentID, provider))
	}
	return anomalies
}

func TestC2ProviderAuditGroupsAliasesAsOneProvider(t *testing.T) {
	base := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		providers []string
		keys      ProviderKeyFunc
		want      bool
		wantLabel string
	}{
		{"built-in aliases cross the distinct-agent threshold", []string{"codex", "codex-acp"}, c2Keys, true, "provider codex across 2 agents"},
		{"custom tools sharing a key cross it", []string{"custom-b", "custom-a"}, c2Keys, true, "provider custom-a across 2 agents"},
		{"three mixed tools form one group", []string{"codex-acp", "codex", "codex-acp"}, c2Keys, true, "3 provider_audit_degraded anomalies for provider codex across 3 agents"},
		{"keyless distinct tools do not combine", []string{"keyless-a", "keyless-b"}, c2Keys, false, ""},
		{"without a key func recorded values compare exactly", []string{"codex", "codex-acp"}, nil, false, ""},
		{"distinct keys stay distinct", []string{"codex", "claude"}, c2Keys, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			anomalies := c2Anomalies(base, tt.providers...)

			fromAnomalies := DetectPatterns(anomalies, tt.keys)
			fromState, _, _ := DetectUnacknowledgedPatterns(&models.State{Anomalies: anomalies}, tt.keys)

			for entry, got := range map[string]PatternResult{"DetectPatterns": fromAnomalies, "DetectUnacknowledgedPatterns": fromState} {
				if (got.Pattern == "provider_audit_degradation") != tt.want {
					t.Fatalf("%s: pattern = %q, want provider-audit trigger %v", entry, got.Pattern, tt.want)
				}
				if tt.want && !strings.Contains(got.Evidence, tt.wantLabel) {
					t.Errorf("%s: evidence = %q, want it to contain %q", entry, got.Evidence, tt.wantLabel)
				}
			}
			if tt.want && fromAnomalies.Evidence != fromState.Evidence {
				t.Errorf("entry paths disagree on equivalent inputs: %q vs %q", fromAnomalies.Evidence, fromState.Evidence)
			}
		})
	}
}

func TestC2ProviderAuditSingleValueGroupKeepsRecordedLabel(t *testing.T) {
	base := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	anomalies := c2Anomalies(base, "codex-acp", "codex-acp")

	want := DetectPatterns(anomalies, nil)
	got := DetectPatterns(anomalies, c2Keys)

	if got.Evidence != want.Evidence || got.Explanation != want.Explanation {
		t.Fatalf("single-value group changed under canonical keys:\n got %q / %q\nwant %q / %q", got.Evidence, got.Explanation, want.Evidence, want.Explanation)
	}
	if !strings.Contains(got.Evidence, "provider codex-acp") {
		t.Fatalf("Evidence = %q, want the recorded provider named", got.Evidence)
	}
}

func TestC2ProviderAuditHealthMembershipSpansTheKeyGroup(t *testing.T) {
	boundary := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	registeredAt := boundary.Add(-time.Hour)
	degraded := func(provider string, pid int) models.AgentHealth {
		return degradedProviderHealth(provider, pid, registeredAt)
	}
	healthy := models.AgentHealth{State: models.AgentHealthOK, Provider: "codex-acp", PID: 102, RegisteredAt: &registeredAt}

	tests := []struct {
		name         string
		agents       map[string]models.Agent
		health       map[string]models.AgentHealth
		keys         ProviderKeyFunc
		wantResponse models.CircuitBreakerResponseType
		wantInReason string
	}{
		{
			name:         "alias-only registration with exact degraded epoch halts",
			agents:       map[string]models.Agent{"coder-1": providerAgent("codex-acp", 101, registeredAt)},
			health:       map[string]models.AgentHealth{"coder-1": degraded("codex-acp", 101)},
			keys:         c2Keys,
			wantResponse: models.CircuitBreakerResponseHalt,
			wantInReason: "all 1 currently registered agents",
		},
		{
			name:         "every alias member degraded halts",
			agents:       map[string]models.Agent{"coder-1": providerAgent("codex", 101, registeredAt), "coder-2": providerAgent("codex-acp", 102, registeredAt)},
			health:       map[string]models.AgentHealth{"coder-1": degraded("codex", 101), "coder-2": degraded("codex-acp", 102)},
			keys:         c2Keys,
			wantResponse: models.CircuitBreakerResponseHalt,
			wantInReason: "all 2 currently registered agents",
		},
		{
			name:         "one healthy alias member prevents the all-degraded conclusion",
			agents:       map[string]models.Agent{"coder-1": providerAgent("codex", 101, registeredAt), "coder-2": providerAgent("codex-acp", 102, registeredAt)},
			health:       map[string]models.AgentHealth{"coder-1": degraded("codex", 101), "coder-2": healthy},
			keys:         c2Keys,
			wantResponse: models.CircuitBreakerResponseCheckpoint,
			wantInReason: "not degraded",
		},
		{
			name:         "one alias member without a health record is unknown",
			agents:       map[string]models.Agent{"coder-1": providerAgent("codex", 101, registeredAt), "coder-2": providerAgent("codex-acp", 102, registeredAt)},
			health:       map[string]models.AgentHealth{"coder-1": degraded("codex", 101)},
			keys:         c2Keys,
			wantResponse: models.CircuitBreakerResponseCheckpoint,
			wantInReason: "no health record",
		},
		{
			name:         "another tool's health record never validates the epoch",
			agents:       map[string]models.Agent{"coder-1": providerAgent("codex-acp", 101, registeredAt)},
			health:       map[string]models.AgentHealth{"coder-1": degraded("codex", 101)},
			keys:         c2Keys,
			wantResponse: models.CircuitBreakerResponseCheckpoint,
			wantInReason: "does not match its provider, PID, and registration-time epoch",
		},
		{
			name:         "stale PID stays invalid for an alias member",
			agents:       map[string]models.Agent{"coder-1": providerAgent("codex-acp", 101, registeredAt)},
			health:       map[string]models.AgentHealth{"coder-1": degraded("codex-acp", 100)},
			keys:         c2Keys,
			wantResponse: models.CircuitBreakerResponseCheckpoint,
			wantInReason: "does not match its provider, PID, and registration-time epoch",
		},
		{
			name: "agents of another key are not members",
			agents: map[string]models.Agent{
				"coder-1": providerAgent("codex-acp", 101, registeredAt),
				"coder-2": providerAgent("claude", 102, registeredAt),
			},
			health:       map[string]models.AgentHealth{"coder-1": degraded("codex-acp", 101)},
			keys:         c2Keys,
			wantResponse: models.CircuitBreakerResponseHalt,
			wantInReason: "all 1 currently registered agents",
		},
		{
			name:         "without a key func the alias-only registration stays unknown",
			agents:       map[string]models.Agent{"coder-1": providerAgent("codex-acp", 101, registeredAt)},
			health:       map[string]models.AgentHealth{"coder-1": degraded("codex-acp", 101)},
			keys:         nil,
			wantResponse: models.CircuitBreakerResponseCheckpoint,
			wantInReason: "no currently registered agent has an exact provider match",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &models.State{
				Anomalies:   c2Anomalies(boundary, "codex", "codex-acp"),
				Agents:      tt.agents,
				AgentHealth: tt.health,
			}
			if tt.keys == nil {
				state.Anomalies = c2Anomalies(boundary, "codex", "codex")
			}

			got, _, _ := DetectUnacknowledgedPatterns(state, tt.keys)

			if got.Response != tt.wantResponse || got.Triggered != (tt.wantResponse == models.CircuitBreakerResponseHalt) {
				t.Fatalf("result = {response:%q triggered:%v}, want response %q", got.Response, got.Triggered, tt.wantResponse)
			}
			if !strings.Contains(got.Explanation, tt.wantInReason) {
				t.Errorf("Explanation = %q, want it to contain %q", got.Explanation, tt.wantInReason)
			}
		})
	}
}

func TestC2ProviderAuditKeepsOriginalEvidence(t *testing.T) {
	base := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	state := &models.State{Anomalies: c2Anomalies(base, "codex", "codex-acp")}
	want := c2Anomalies(base, "codex", "codex-acp")

	got, evidence, _ := DetectUnacknowledgedPatterns(state, c2Keys)

	if got.Pattern != "provider_audit_degradation" {
		t.Fatalf("pattern = %q, want the provider-audit trigger", got.Pattern)
	}
	if !reflect.DeepEqual(evidence, want) || !reflect.DeepEqual(state.Anomalies, want) {
		t.Fatalf("anomalies were rewritten: evidence=%v state=%v want=%v", evidence, state.Anomalies, want)
	}
	report := GenerateReport(got, evidence, base, 0)
	for _, provider := range []string{"codex-acp"} {
		if !strings.Contains(report, provider) {
			t.Errorf("report omits the recorded provider %q", provider)
		}
	}
}

// A non-nil key func owns its result, even when it is empty: tools whose
// provider_key override is whitespace-only share the empty key, as in launch.
func TestC2ProviderAuditHonoursEmptyCanonicalKey(t *testing.T) {
	emptyKeys := func(recorded string) string {
		if recorded == "custom-a" || recorded == "custom-b" {
			return ""
		}
		return recorded
	}
	base := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	registeredAt := base.Add(-time.Hour)
	anomalies := c2Anomalies(base, "custom-a", "custom-b")

	for entry, got := range map[string]PatternResult{
		"DetectPatterns": DetectPatterns(anomalies, emptyKeys),
		"DetectUnacknowledgedPatterns": func() PatternResult {
			result, _, _ := DetectUnacknowledgedPatterns(&models.State{Anomalies: anomalies}, emptyKeys)
			return result
		}(),
	} {
		if got.Pattern != "provider_audit_degradation" {
			t.Errorf("%s: pattern = %q, want the empty-key tools grouped as one provider", entry, got.Pattern)
		}
	}
	if got := DetectPatterns(anomalies, nil); got.Pattern != "" {
		t.Errorf("a nil key func compares recorded values exactly: pattern = %q, want none", got.Pattern)
	}

	state := &models.State{
		Anomalies: anomalies,
		Agents: map[string]models.Agent{
			"coder-1": providerAgent("custom-a", 101, registeredAt),
			"coder-2": providerAgent("", 102, registeredAt),
		},
		AgentHealth: map[string]models.AgentHealth{"coder-1": degradedProviderHealth("custom-a", 101, registeredAt)},
	}
	got, _, _ := DetectUnacknowledgedPatterns(state, emptyKeys)
	if got.Response != models.CircuitBreakerResponseHalt || !strings.Contains(got.Explanation, "all 1 currently registered agents") {
		t.Fatalf("result = {response:%q explanation:%q}, want HALT with the empty-key member only: an agent with no recorded provider is never a member", got.Response, got.Explanation)
	}
}
