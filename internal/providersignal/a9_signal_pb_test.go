package providersignal

import (
	"os"
	"strings"
	"testing"
	"time"
)

// A9 preserved-behaviour (PB) tests for the provider-signal capability, written
// on the tree before the providersignal extraction and moved with the code.

// PB-5: quota detection takes the first matching output line, not the first
// matching pattern, and normalises provider aliases; unavailable detection
// takes the first matching pattern.
func TestA9SignalDetectionOrder(t *testing.T) {
	output := "You're out of extra usage\nYou've hit your session limit\n"
	qe := DetectQuotaExhaustion(output, "claude")
	if qe == nil || qe.Message != "You're out of extra usage" || qe.Provider != "claude" {
		t.Fatalf("DetectQuotaExhaustion = %+v, want the first matching line", qe)
	}
	if qe := DetectQuotaExhaustion("You've hit your usage limit", "codex-acp"); qe == nil || qe.Provider != "codex" {
		t.Fatalf("alias detection = %+v, want canonical provider codex", qe)
	}
	if qe := DetectQuotaExhaustion("You're out of extra usage", "codex"); qe != nil {
		t.Fatalf("another provider's pattern matched: %+v", qe)
	}

	both := "thread/start failed: error creating thread using .codex/sessions\n" +
		"Codex cannot access session files under .codex/sessions: permission denied\n"
	pu := DetectProviderUnavailable(both, "codex-acp")
	if pu == nil || pu.Provider != "codex" || pu.Message != codexSessionAccessDiagnostic {
		t.Fatalf("DetectProviderUnavailable = %+v, want the first pattern's diagnostic", pu)
	}
}

// PB-6: the signal file records RFC3339 UTC timestamps; an announced reset adds
// the grace, an absent one the fallback, and an alias names the canonical file.
func TestA9QuotaSignalRecordForm(t *testing.T) {
	detected := time.Date(2026, 9, 24, 1, 54, 0, 0, time.UTC)
	for _, tt := range []struct {
		name     string
		resetsAt time.Time
		want     string
	}{
		{"announced", detected.Add(6 * time.Minute), "resets_at: 2026-09-24T02:00:00Z\nexpires: 2026-09-24T02:01:00Z\n"},
		{"unannounced", time.Time{}, "resets_at: unknown\nexpires: 2026-09-24T02:24:00Z\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			makeQuotaProjectDir(t, root)
			if err := writeQuotaSignal(root, "codex-acp", "limit hit", tt.resetsAt, detected); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(QuotaSignalPath(root, "codex"))
			if err != nil {
				t.Fatalf("alias must write the canonical signal file: %v", err)
			}
			want := "provider: codex\ndetected: 2026-09-24T01:54:00Z\nmessage: limit hit\n" + tt.want
			if string(data) != want {
				t.Fatalf("signal file = %q, want %q", data, want)
			}
		})
	}
}

// PB-7: a signal that cannot be read blocks (fail closed) and names no expiry.
func TestA9UnreadableQuotaSignalFailsClosed(t *testing.T) {
	root := t.TempDir()
	makeQuotaProjectDir(t, root)
	if err := os.Mkdir(QuotaSignalPath(root, "codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	until, blocked := QuotaBlockedUntil(root, "codex")
	if !blocked || !until.IsZero() {
		t.Fatalf("QuotaBlockedUntil = (%v, %t), want (zero, true) for an unreadable signal", until, blocked)
	}
	if !CheckQuotaSignal(root, "codex") {
		t.Fatal("CheckQuotaSignal = false for an unreadable signal")
	}
	sweep := ClearExpiredQuotaSignals(root)
	if len(sweep.Held) != 1 || len(sweep.Cleared) != 0 || sweep.Held[0].Provider != "codex" || !strings.HasSuffix(sweep.Held[0].File, "provider-quota-exhausted-codex") {
		t.Fatalf("sweep = %+v, want the unreadable signal held", sweep)
	}
}
