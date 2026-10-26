package commands

import (
	"slices"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/alerts"
)

const stuckAlertCachePrefix = "stuck-alert:"

// ParseAlertLine parses a line written by Alert.String() back into an Alert.
// Returns the parsed alert and true on success, or zero value and false on
// malformed input.
//
// Format: [<RFC3339>] <level> <CATEGORY>: <message>
func ParseAlertLine(line string) (Alert, bool) {
	return alerts.ParseLine(line)
}

func FilterAlertsAfterAutoRepair(alertsIn []Alert, repairOutcome AutoRepairAgentPoolOutcome) []Alert {
	handledRoles := make(map[string]bool, len(repairOutcome.AttemptedRoles)+len(repairOutcome.SuppressedRoles)+len(repairOutcome.BlockedRoles))
	for _, role := range slices.Concat(repairOutcome.AttemptedRoles, repairOutcome.SuppressedRoles, repairOutcome.BlockedRoles) {
		handledRoles[role] = true
	}
	if len(handledRoles) == 0 {
		return alertsIn
	}

	filtered := alertsIn[:0]
	for _, alert := range alertsIn {
		if alert.Category == alerts.CategoryMissingRole.String() && missingRoleAlertHandled(alert, handledRoles) {
			continue
		}
		filtered = append(filtered, alert)
	}
	return filtered
}

func missingRoleAlertHandled(alert Alert, handledRoles map[string]bool) bool {
	for role := range handledRoles {
		if containsRoleToken(alert.Message, role) {
			return true
		}
	}
	return false
}

func containsRoleToken(message, role string) bool {
	needle := "role " + role
	for offset := 0; offset < len(message); {
		idx := strings.Index(message[offset:], needle)
		if idx < 0 {
			return false
		}
		end := offset + idx + len(needle)
		if end == len(message) || isRoleTokenBoundary(message[end]) {
			return true
		}
		offset = end
	}
	return false
}

func isRoleTokenBoundary(ch byte) bool {
	return !((ch >= 'a' && ch <= 'z') ||
		(ch >= 'A' && ch <= 'Z') ||
		(ch >= '0' && ch <= '9') ||
		ch == '-' || ch == '_')
}

func activeAlertKeys(raised []Alert) map[string]bool {
	keys := make(map[string]bool, len(raised))
	for _, alert := range raised {
		if !isFreshnessTrackedAlertCategory(alert.Category) {
			continue
		}
		keys[AlertKey(alert)] = true
	}
	return keys
}

// AlertKey returns a stable identity for an alert condition.
func AlertKey(alert Alert) string {
	return alerts.Key(alert)
}

func isFreshnessTrackedAlertCategory(category string) bool {
	// Only track categories that are recomputed and emitted on every check while
	// active, or are deduped after building a full active set. Categories with
	// internal throttle caches, such as MISSING ROLE or STALE SENTINEL, need their
	// checks to expose active identities before the TUI can safely resolve them.
	if isStuckAlertCategory(category) {
		return true
	}
	switch alerts.Category(category) {
	case alerts.CategoryBlocked, alerts.CategoryLeaseExpired, alerts.CategoryReviewLeaseExpired:
		return true
	default:
		return false
	}
}

// StateReadFailureAlerts reports a state that could not be read, for a
// caller whose checks cannot run without one. The INVALID STATE alert is
// emitted once per episode, like validation's: suppressed while its identity
// is cached, which retires on the next clean validation (reconcileStuckAlerts).
// Other cached identities are left alone, since no check observed them.
func StateReadFailureAlerts(err error, cache map[string]time.Time) []Alert {
	now := time.Now().UTC()
	alert := Alert{
		Timestamp: now,
		Level:     AlertLevelCritical,
		Category:  alerts.CategoryInvalidState.String(),
		Message:   err.Error(),
	}
	key := stuckAlertCacheKey(alert)
	if _, seen := cache[key]; seen {
		return nil
	}
	cache[key] = now
	return []Alert{alert}
}

// reconcileStuckAlerts emits each condition once while it stays active: an
// alert whose gate identity was active on the previous check is suppressed, and
// an identity absent from a check retires, so a condition that resolves and
// recurs alerts again.
//
// INVALID STATE is the exception: validation reports only its first error, so
// an error missing from a failing validation may merely be masked. Its
// identities retire only on a clean validation (no INVALID STATE alert).
// Limitation: an error that resolves and recurs while another error persists
// throughout is not re-announced until a clean validation intervenes.
func reconcileStuckAlerts(raised []Alert, cache map[string]time.Time) []Alert {
	if cache == nil {
		return raised
	}

	now := time.Now().UTC()
	activeKeys := make(map[string]bool)
	deduped := make([]Alert, 0, len(raised))
	validationClean := true

	for _, alert := range raised {
		if alert.Category == alerts.CategoryInvalidState.String() {
			validationClean = false
		}
		key := stuckAlertCacheKey(alert)
		activeKeys[key] = true
		if _, seen := cache[key]; seen {
			continue
		}
		cache[key] = now
		deduped = append(deduped, alert)
	}

	invalidStatePrefix := stuckAlertCachePrefix + alerts.CategoryInvalidState.String() + ":"
	for key := range cache {
		if !strings.HasPrefix(key, stuckAlertCachePrefix) || activeKeys[key] {
			continue
		}
		if !validationClean && strings.HasPrefix(key, invalidStatePrefix) {
			continue
		}
		delete(cache, key)
	}

	return deduped
}

func isStuckAlertCategory(category string) bool {
	switch alerts.Category(category) {
	case alerts.CategoryAwaitingHuman, alerts.CategoryBlocked, alerts.CategoryHypothesisExhaustion, alerts.CategoryIntegrationFailed,
		alerts.CategoryInvalidState, alerts.CategoryDeadAgentProcess, alerts.CategoryRegisteredAgentProcess:
		return true
	case alerts.CategoryInvalidAgentOwnership:
		return true
	default:
		return false
	}
}

func stuckAlertCacheKey(alert Alert) string {
	return stuckAlertCachePrefix + alerts.GateKey(alert)
}

// WriteAlert appends an alert to the alerts log file.
func WriteAlert(alertsLog string, a Alert) error {
	return alerts.Write(alertsLog, a)
}
