package alerts

import (
	"testing"
	"time"
)

// TestCategoryConstantsKeepTheirLogSpelling pins every constant to the spelling
// an alerts.log line carries, so a rename cannot change the log format, and
// round-trips each through Alert.String and ParseLine with Category a string.
func TestCategoryConstantsKeepTheirLogSpelling(t *testing.T) {
	cases := []struct {
		category Category
		spelling string
	}{
		{CategoryApproachingLimit, "APPROACHING LIMIT"},
		{CategoryAttempt, "ATTEMPT"},
		{CategoryAutoRepairFailed, "AUTO REPAIR FAILED"},
		{CategoryAutoRepairUnservable, "AUTO REPAIR UNSERVABLE"},
		{CategoryAwaitingHuman, "AWAITING HUMAN"},
		{CategoryBlocked, "BLOCKED"},
		{CategoryCircuitBreaker, "CIRCUIT BREAKER"},
		{CategoryDeadAgentProcess, "DEAD AGENT PROCESS"},
		{CategoryHypothesisExhaustion, "HYPOTHESIS EXHAUSTION"},
		{CategoryImmediateDiscovery, "IMMEDIATE DISCOVERY"},
		{CategoryIntegrationFailed, "INTEGRATION FAILED"},
		{CategoryInvalidAgentOwnership, "INVALID AGENT OWNERSHIP"},
		{CategoryInvalidState, "INVALID STATE"},
		{CategoryLeaseExpired, "LEASE EXPIRED"},
		{CategoryMissingRole, "MISSING ROLE"},
		{CategoryNoUsableProvider, "NO USABLE PROVIDER"},
		{CategoryOrchestratorMissing, "ORCHESTRATOR MISSING"},
		{CategoryOrphanedRejected, "ORPHANED REJECTED"},
		{CategoryPipelineConfig, "PIPELINE CONFIG"},
		{CategoryRegisteredAgentProcess, "REGISTERED AGENT PROCESS"},
		{CategoryReviewLeaseExpired, "REVIEW LEASE EXPIRED"},
		{CategoryReviewLoop, "REVIEW LOOP"},
		{CategorySprintStalled, "SPRINT STALLED"},
		{CategoryStaleDraft, "STALE DRAFT"},
		{CategoryStaleSentinel, "STALE SENTINEL"},
		{CategoryStalled, "STALLED"},
	}
	seen := make(map[string]bool, len(cases))
	at := time.Date(2001, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, tc := range cases {
		t.Run(tc.spelling, func(t *testing.T) {
			if tc.category.String() != tc.spelling {
				t.Fatalf("category spells %q, want %q", tc.category.String(), tc.spelling)
			}
			if seen[tc.spelling] {
				t.Fatalf("spelling %q declared twice", tc.spelling)
			}
			seen[tc.spelling] = true

			alert := Alert{Timestamp: at, Level: AlertLevelWarning, Category: tc.category.String(), Message: "task-1 — detail"}
			parsed, ok := ParseLine(alert.String())
			if !ok {
				t.Fatalf("line %q does not parse", alert.String())
			}
			if parsed.Category != tc.spelling || Category(parsed.Category) != tc.category {
				t.Fatalf("parsed category %q, want %q", parsed.Category, tc.spelling)
			}
			if parsed.Level != alert.Level || parsed.Message != alert.Message || !parsed.Timestamp.Equal(at) {
				t.Fatalf("round trip changed level, message or time of %q", tc.spelling)
			}
		})
	}
}

// TestUndeclaredCategoryStillParses keeps Alert.Category open: lifecycle writers
// in other packages log categories this package declares no constant for.
func TestUndeclaredCategoryStillParses(t *testing.T) {
	alert := Alert{Timestamp: time.Date(2001, 1, 2, 3, 4, 5, 0, time.UTC), Level: AlertLevelCritical, Category: "SOMETHING ELSE", Message: "m"}
	parsed, ok := ParseLine(alert.String())
	if !ok || parsed.Category != "SOMETHING ELSE" {
		t.Fatalf("undeclared category must round-trip as a string, got %q ok=%t", parsed.Category, ok)
	}
}
