package commands

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func TestAutoRepairSuppressionCooldown(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.October, 4, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		age  time.Duration
		due  bool
	}{
		{"before expiry", AutoRepairAgentPoolSuppressionCooldown - time.Nanosecond, false},
		{"at expiry", AutoRepairAgentPoolSuppressionCooldown, true},
		{"after expiry", AutoRepairAgentPoolSuppressionCooldown + time.Nanosecond, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			now := start.Add(tc.age)
			cache := map[string]time.Time{
				autoRepairAgentPoolStartCountPrefix + "architect": autoRepairCountTime(AutoRepairAgentPoolMaxStarts, start),
				// A recent backoff must not prevent the cooldown's automatic retry.
				autoRepairAgentPoolCachePrefix + "architect":      now,
				autoRepairAgentPoolSuppressedPrefix + "architect": start,
			}
			missing := []MissingRoleWork{{Role: "architect", SpawnCount: 1}}

			alerts, suppressed := autoRepairSuppressedAlerts(missing, cache, now)
			due := autoRepairDueRoles(missing, cache, now)

			if len(alerts) != 0 {
				t.Fatalf("repeated cooldown poll alerted: %v", alerts)
			}
			if tc.due {
				if len(suppressed) != 0 || !slices.Equal(due, []string{"architect"}) {
					t.Fatalf("suppressed = %v, due = %v; want architect ready after cooldown", suppressed, due)
				}
				if len(cache) != 0 {
					t.Fatalf("failed-start episode survives cooldown: %v", cache)
				}
			} else if !slices.Equal(suppressed, []string{"architect"}) || len(due) != 0 {
				t.Fatalf("suppressed = %v, due = %v; want architect suppressed during cooldown", suppressed, due)
			}
		})
	}
}

func TestAutoRepairSuppressionCooldownRenewsAfterNewFailures(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.October, 4, 12, 0, 0, 0, time.UTC)
	cache := make(map[string]time.Time)
	missing := []MissingRoleWork{{Role: "architect", SpawnCount: 1}}
	for episode := range 2 {
		for range AutoRepairAgentPoolMaxStarts {
			recordAutoRepairFailedStart(cache, "architect", now)
		}
		alerts, suppressed := autoRepairSuppressedAlerts(missing, cache, now)
		if len(alerts) != 1 || !slices.Equal(suppressed, []string{"architect"}) {
			t.Fatalf("episode %d: alerts = %v, suppressed = %v; want one warning", episode, alerts, suppressed)
		}
		if !strings.Contains(alerts[0].Message, "automatic retry after 5m0s cooldown") {
			t.Fatalf("warning does not describe automatic retry: %q", alerts[0].Message)
		}
		if due := autoRepairDueRoles(missing, cache, now); len(due) != 0 {
			t.Fatalf("episode %d: due roles during cooldown = %v", episode, due)
		}
		now = now.Add(AutoRepairAgentPoolSuppressionCooldown)
		autoRepairSuppressedAlerts(missing, cache, now)
		if due := autoRepairDueRoles(missing, cache, now); !slices.Equal(due, []string{"architect"}) {
			t.Fatalf("episode %d: due roles after cooldown = %v", episode, due)
		}
	}
}

func TestAutoRepairSuppressionCooldownPreservesOtherCapacityGuards(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.October, 4, 12, 0, 0, 0, time.UTC)
	now := start.Add(AutoRepairAgentPoolSuppressionCooldown)
	pendingKey := autoRepairPendingKey("architect", 4242, "architect-1", 0)
	timeoutKey := autoRepairAgentPoolPendingTimeoutPrefix + strings.TrimPrefix(pendingKey, autoRepairAgentPoolPendingPrefix)
	blockedKey := autoRepairAgentPoolProviderBlockedPrefix + "architect:0:claude"
	cache := map[string]time.Time{
		autoRepairAgentPoolStartCountPrefix + "architect": autoRepairCountTime(AutoRepairAgentPoolMaxStarts, start),
		autoRepairAgentPoolCachePrefix + "architect":      start,
		autoRepairAgentPoolSuppressedPrefix + "architect": start,
		pendingKey: start,
		timeoutKey: start,
		blockedKey: start,
	}
	missing := []MissingRoleWork{{Role: "architect", SpawnCount: 1}}

	autoRepairSuppressedAlerts(missing, cache, now)
	for _, key := range []string{pendingKey, timeoutKey, blockedKey} {
		if stamp, ok := cache[key]; !ok || !stamp.Equal(start) {
			t.Fatalf("cooldown changed capacity guard %q", key)
		}
	}
	missing = subtractPendingSpawns(missing, map[string][]int{"architect": {0}})
	if due := autoRepairDueRoles(missing, cache, now); len(due) != 0 {
		t.Fatalf("pending process lost capacity after cooldown: due = %v", due)
	}
}
