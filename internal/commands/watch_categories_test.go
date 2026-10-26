package commands

import (
	"testing"

	"github.com/liza-mas/liza/internal/alerts"
	"github.com/liza-mas/liza/internal/models"
)

// TestWatchCategoryMatchSetsKeepTheirSpellings pins which alert category
// spellings the watcher treats as stuck (written once per episode, retired on
// absence) and as freshness-tracked (resolved by the TUI when no longer active).
// The spellings are literals, so renaming a constant cannot change a set.
func TestWatchCategoryMatchSetsKeepTheirSpellings(t *testing.T) {
	cases := []struct {
		category          alerts.Category
		spelling          string
		stuck, freshTrack bool
	}{
		{alerts.CategoryAwaitingHuman, "AWAITING HUMAN", true, true},
		{alerts.CategoryBlocked, "BLOCKED", true, true},
		{alerts.CategoryHypothesisExhaustion, "HYPOTHESIS EXHAUSTION", true, true},
		{alerts.CategoryIntegrationFailed, "INTEGRATION FAILED", true, true},
		{alerts.CategoryInvalidState, "INVALID STATE", true, true},
		{alerts.CategoryDeadAgentProcess, "DEAD AGENT PROCESS", true, true},
		{alerts.CategoryRegisteredAgentProcess, "REGISTERED AGENT PROCESS", true, true},
		{alerts.CategoryInvalidAgentOwnership, "INVALID AGENT OWNERSHIP", true, true},
		{alerts.CategoryLeaseExpired, "LEASE EXPIRED", false, true},
		{alerts.CategoryReviewLeaseExpired, "REVIEW LEASE EXPIRED", false, true},
		{alerts.CategoryMissingRole, "MISSING ROLE", false, false},
		{alerts.CategoryStaleSentinel, "STALE SENTINEL", false, false},
		{alerts.CategoryStalled, "STALLED", false, false},
		{alerts.CategoryOrchestratorMissing, "ORCHESTRATOR MISSING", false, false},
		{alerts.CategoryAutoRepairFailed, "AUTO REPAIR FAILED", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.spelling, func(t *testing.T) {
			if tc.category.String() != tc.spelling {
				t.Fatalf("constant spells %q, want %q", tc.category.String(), tc.spelling)
			}
			if got := isStuckAlertCategory(tc.spelling); got != tc.stuck {
				t.Fatalf("isStuckAlertCategory = %t, want %t", got, tc.stuck)
			}
			if got := isFreshnessTrackedAlertCategory(tc.spelling); got != tc.freshTrack {
				t.Fatalf("isFreshnessTrackedAlertCategory = %t, want %t", got, tc.freshTrack)
			}
		})
	}
	for _, other := range []string{"", "blocked", "Blocked", "SOMETHING ELSE"} {
		if isStuckAlertCategory(other) || isFreshnessTrackedAlertCategory(other) {
			t.Fatalf("category %q must match no set", other)
		}
	}
}

// TestFilterAlertsAfterAutoRepairMatchesOnlyMissingRole keeps the comparison of
// Alert.Category (a string) with the MISSING ROLE category: only that category is
// replaced by the repair outcome, and only for the roles it handled.
func TestFilterAlertsAfterAutoRepairMatchesOnlyMissingRole(t *testing.T) {
	missing := func(category, message string) Alert {
		return Alert{Level: AlertLevelWarning, Category: category, Message: message}
	}
	in := []Alert{
		missing("MISSING ROLE", "no registered agent for role coder — 1 task(s) waiting"),
		missing("MISSING ROLE", "no registered agent for role code-reviewer — 1 task(s) waiting"),
		missing("BLOCKED", "no registered agent for role coder"),
	}
	got := FilterAlertsAfterAutoRepair(append([]Alert(nil), in...), AutoRepairAgentPoolOutcome{AttemptedRoles: []string{"coder"}})
	if len(got) != 2 || got[0].Message != in[1].Message || got[1].Category != "BLOCKED" {
		t.Fatalf("filtered alerts = %v, want the other role's MISSING ROLE and the BLOCKED alert", got)
	}
}

// TestWatchNamedValuesKeepTheirSpellings pins the cache-key spellings and
// thresholds named for K7: the watcher's cache is process-local, but the keys
// are frozen by the oracle and by the in-process tests that seed them.
func TestWatchNamedValuesKeepTheirSpellings(t *testing.T) {
	for name, pair := range map[string][2]string{
		"missing role":    {missingRoleCachePrefix, "missing-role:"},
		"circuit breaker": {circuitBreakerAlertKey, "circuit_breaker:alert"},
		"sprint stalled":  {sprintStalledAlertKey, "sprint_stalled:alert"},
		"legacy blocked":  {legacyBlockedCachePrefix, "blocked:"},
		"final attempt":   {finalAttemptCachePrefix, "attempt2:"},
		"sentinel":        {sentinelCachePrefix, "sentinel:"},
		"pipeline config": {pipelineConfigErrorKey, "pipeline-config-error"},
	} {
		if pair[0] != pair[1] {
			t.Errorf("%s key spells %q, want %q", name, pair[0], pair[1])
		}
	}
	if reviewLoopCliffCycles != 5 || approachingLimitWindow != 2 {
		t.Errorf("review loop cliff = %d, approaching window = %d; want 5 and 2", reviewLoopCliffCycles, approachingLimitWindow)
	}
}

// TestRunningTaskOwnerKindsAreTheRoleTypes keeps the owner kind the ownership
// alerts print ("has doer …", "has reviewer …") equal to the role type spelling.
func TestRunningTaskOwnerKindsAreTheRoleTypes(t *testing.T) {
	pr := loadSlotResolver(t, 1)
	doer, reviewer := "coder-1", "code-reviewer-1"
	executing := models.Task{RolePair: "coding-pair", Status: models.TaskStatusImplementing, AssignedTo: &doer}
	reviewing := models.Task{RolePair: "coding-pair", Status: models.TaskStatusReviewing, ReviewingBy: &reviewer}
	if id, kind, ok := runningTaskOwner(&executing, pr); !ok || id != doer || string(kind) != "doer" {
		t.Fatalf("executing task owner = %q %q %t, want coder-1 doer", id, kind, ok)
	}
	if id, kind, ok := runningTaskOwner(&reviewing, pr); !ok || id != reviewer || string(kind) != "reviewer" {
		t.Fatalf("reviewing task owner = %q %q %t, want code-reviewer-1 reviewer", id, kind, ok)
	}
}
