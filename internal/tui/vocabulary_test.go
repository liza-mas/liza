package tui

import (
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/alerts"
)

func TestActivityKindsKeepTheirSpellings(t *testing.T) {
	for kind, want := range map[ActivityKind]string{
		ActivityKindLog:     "log",
		ActivityKindAlert:   "alert",
		ActivityKindAnomaly: "anomaly",
	} {
		if string(kind) != want {
			t.Errorf("activity kind %q, want %q", kind, want)
		}
	}
}

func TestActivityActionsKeepTheirSpellings(t *testing.T) {
	for got, want := range map[string]string{
		activityActionStateReadError: "state_read_error",
		activityActionWriteError:     "write_error",
		activityActionWatcherError:   "watcher_error",
	} {
		if got != want {
			t.Errorf("activity action %q, want %q", got, want)
		}
	}
}

func TestNamedDelaysKeepTheirValues(t *testing.T) {
	for name, tc := range map[string]struct{ got, want time.Duration }{
		"command result flash": {cmdResultFlashDelay, 3 * time.Second},
		"alert banner flash":   {alertBannerFlashDelay, 10 * time.Second},
		"tick interval":        {tickInterval, 10 * time.Second},
		"terminate wait":       {terminateWait, 5 * time.Second},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v", name, tc.got, tc.want)
		}
	}
	if maxActivityEntries != 200 {
		t.Errorf("maxActivityEntries = %d, want 200", maxActivityEntries)
	}
}

func TestAlertLevelsAreTheAlertsSpellings(t *testing.T) {
	if levelWarning != "⚠️" || levelCritical != "🚨" {
		t.Errorf("levels = %q, %q", levelWarning, levelCritical)
	}
	if levelWarning != string(alerts.AlertLevelWarning) || levelCritical != string(alerts.AlertLevelCritical) {
		t.Error("levels must be the alerts package spellings")
	}
}

func TestTransientAlertCategoriesAreTheLeaseExpiries(t *testing.T) {
	for category, want := range map[string]bool{
		"LEASE EXPIRED":            true,
		"REVIEW LEASE EXPIRED":     true,
		"BLOCKED":                  false,
		"AWAITING HUMAN":           false,
		"lease expired":            false,
		"":                         false,
		"state_read_error":         false,
		"INVALID AGENT OWNERSHIP":  false,
		"LEASE EXPIRED (extended)": false,
	} {
		if got := isTransientAlertCategory(category); got != want {
			t.Errorf("isTransientAlertCategory(%q) = %v, want %v", category, got, want)
		}
	}
	if string(alerts.CategoryLeaseExpired) != "LEASE EXPIRED" || string(alerts.CategoryReviewLeaseExpired) != "REVIEW LEASE EXPIRED" {
		t.Error("alerts lease-expiry constants changed spelling")
	}
}

func TestInlineActionPredicates(t *testing.T) {
	confirmations := map[InlineAction]bool{
		InlineActionStopConfirm:      true,
		InlineActionTerminateConfirm: true,
	}
	completing := map[InlineAction]bool{
		InlineActionSpawn:     true,
		InlineActionSpawnWith: true,
		InlineActionSpawnCLI:  true,
		InlineActionTerminate: true,
	}
	for _, action := range []InlineAction{
		InlineActionNone, InlineActionSpawn, InlineActionSpawnWith, InlineActionSpawnCLI,
		InlineActionPause, InlineActionStopConfirm, InlineActionTerminate, InlineActionTerminateConfirm,
	} {
		if got := action.isConfirmation(); got != confirmations[action] {
			t.Errorf("InlineAction(%d).isConfirmation() = %v, want %v", action, got, confirmations[action])
		}
		if got := action.hasCompletions(); got != completing[action] {
			t.Errorf("InlineAction(%d).hasCompletions() = %v, want %v", action, got, completing[action])
		}
	}
}
