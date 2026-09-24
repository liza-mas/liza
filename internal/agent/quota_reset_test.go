package agent

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/paths"
)

func mustParseUTC(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestDetectQuotaExhaustion_ClaudeResetSources(t *testing.T) {
	if _, err := time.LoadLocation("Europe/Paris"); err != nil {
		t.Skipf("zoneinfo unavailable: %v", err)
	}
	textResult := func(reset string) string {
		return `{"type":"result","is_error":true,"result":"You've hit your session limit · resets ` + reset + `"}`
	}
	tests := []struct {
		name   string
		output string
		now    string
		want   string // "" = not announced
	}{
		{
			name: "rejected event wins over text",
			output: `{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","resetsAt":1790215200}}` + "\n" +
				textResult("9pm (Europe/Paris)"),
			now:  "2026-09-24T01:54:00Z",
			want: "2026-09-24T02:00:00Z",
		},
		{
			name: "allowed event is ignored",
			output: `{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","resetsAt":4102444800}}` + "\n" +
				textResult("4am (Europe/Paris)"),
			now:  "2026-09-24T01:54:00Z",
			want: "2026-09-24T02:00:00Z",
		},
		{name: "hour with minutes", output: textResult("2:20pm (Europe/Paris)"), now: "2026-09-24T10:00:00Z", want: "2026-09-24T12:20:00Z"},
		{name: "passed clock time rolls to next day", output: textResult("4am (Europe/Paris)"), now: "2026-09-24T03:30:00Z", want: "2026-09-25T02:00:00Z"},
		{name: "just-passed clock time stays today", output: textResult("4am (Europe/Paris)"), now: "2026-09-24T02:30:00Z", want: "2026-09-24T02:00:00Z"},
		{name: "unknown zone", output: textResult("4am (Nowhere/Invalid)"), now: "2026-09-24T01:54:00Z"},
		{name: "no reset text", output: `{"type":"error","message":"You've hit your limit."}`, now: "2026-09-24T01:54:00Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qe := detectQuotaExhaustionAt(tt.output, "claude", mustParseUTC(t, tt.now))
			if qe == nil {
				t.Fatal("expected quota exhaustion detected, got nil")
			}
			got := ""
			if !qe.ResetsAt.IsZero() {
				got = qe.ResetsAt.UTC().Format(time.RFC3339)
			}
			if got != tt.want {
				t.Fatalf("ResetsAt = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDetectQuotaExhaustion_CodexResetSources(t *testing.T) {
	message := func(suffix string) string {
		return `{"type":"error","message":"You've hit your usage limit. Visit https://chatgpt.com/codex/settings/usage to purchase more credits or try again at ` + suffix + `"}`
	}
	local := func(day, hour, minute int) time.Time {
		return time.Date(2026, time.September, day, hour, minute, 0, 0, time.Local)
	}
	tests := []struct {
		name   string
		output string
		now    time.Time
		want   time.Time
	}{
		{name: "dated", output: message("Sep 29th, 2026 2:57 AM."), now: local(22, 12, 0), want: local(29, 2, 57)},
		{name: "same day", output: message("2:57 PM."), now: local(24, 13, 0), want: local(24, 14, 57)},
		{name: "date-less passed time rolls to next day", output: message("2:57 AM."), now: local(24, 10, 0), want: local(25, 2, 57)},
		{name: "no reset text", output: `{"type":"error","message":"You've hit your usage limit. Upgrade to Pro."}`, now: local(24, 10, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qe := detectQuotaExhaustionAt(tt.output, "codex", tt.now)
			if qe == nil {
				t.Fatal("expected quota exhaustion detected, got nil")
			}
			if !qe.ResetsAt.Equal(tt.want) {
				t.Fatalf("ResetsAt = %v, want %v", qe.ResetsAt, tt.want)
			}
		})
	}
}

func TestDetectQuotaExhaustion_CursorAnnouncesNoReset(t *testing.T) {
	qe := detectQuotaExhaustionAt("Upgrade your plan to continue", "cursor", time.Now())
	if qe == nil {
		t.Fatal("expected quota exhaustion detected, got nil")
	}
	if !qe.ResetsAt.IsZero() {
		t.Fatalf("ResetsAt = %v, want zero", qe.ResetsAt)
	}
}

func TestQuotaSignalExpiry(t *testing.T) {
	detected := mustParseUTC(t, "2026-09-24T01:54:00Z")
	tests := []struct {
		name     string
		resetsAt time.Time
		want     string
	}{
		{name: "announced reset plus grace", resetsAt: mustParseUTC(t, "2026-09-24T02:00:00Z"), want: "2026-09-24T02:01:00Z"},
		{name: "reset soon after detection keeps the floor", resetsAt: mustParseUTC(t, "2026-09-24T01:55:00Z"), want: "2026-09-24T01:59:00Z"},
		{name: "already-past reset keeps the floor", resetsAt: mustParseUTC(t, "2026-09-24T01:00:00Z"), want: "2026-09-24T01:59:00Z"},
		{name: "not announced uses the fallback", want: "2026-09-24T02:24:00Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := quotaSignalExpiry(tt.resetsAt, detected).UTC().Format(time.RFC3339); got != tt.want {
				t.Fatalf("quotaSignalExpiry = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestCheckQuotaSignal_UnreadableFieldsStayBlocked(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name    string
		content string
	}{
		{name: "empty file (writer truncated)", content: ""},
		{name: "truncated expires falls back to detected", content: "provider: codex\ndetected: " + now.Format(time.RFC3339) + "\nmessage: limit\nresets_at: unknown\nexpires: 2026-0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectRoot := t.TempDir()
			writeRawQuotaSignal(t, projectRoot, "codex", tt.content)
			if !CheckQuotaSignal(projectRoot, "codex") {
				t.Fatal("CheckQuotaSignal = false for a just-written unreadable signal, want true")
			}
		})
	}

	t.Run("stale empty file lifts after the fallback", func(t *testing.T) {
		projectRoot := t.TempDir()
		writeRawQuotaSignal(t, projectRoot, "codex", "")
		old := now.Add(-time.Hour)
		if err := os.Chtimes(QuotaSignalPath(projectRoot, "codex"), old, old); err != nil {
			t.Fatal(err)
		}
		if CheckQuotaSignal(projectRoot, "codex") {
			t.Fatal("CheckQuotaSignal = true for a 1h-old empty signal, want false")
		}
	})
}

func TestQuotaAlerts_UseBrandedNames(t *testing.T) {
	withTestProjectDirName(t, ".acme")
	previousBinary := brand.BinaryName
	brand.BinaryName = "acme"
	t.Cleanup(func() { brand.BinaryName = previousBinary })
	projectRoot := t.TempDir()
	makeQuotaProjectDir(t, projectRoot)

	if err := RaiseQuotaExhaustion(projectRoot, &QuotaExhaustion{Provider: "codex", Message: "You've hit your usage limit"}); err != nil {
		t.Fatalf("RaiseQuotaExhaustion failed: %v", err)
	}
	if err := LogQuotaSpawnBlockedAlert(projectRoot, "codex", "coder"); err != nil {
		t.Fatalf("LogQuotaSpawnBlockedAlert failed: %v", err)
	}

	data, err := os.ReadFile(paths.New(projectRoot).AlertsLogPath())
	if err != nil {
		t.Fatal(err)
	}
	alerts := string(data)
	if !strings.Contains(alerts, "run acme pause then acme resume to lift it earlier") {
		t.Fatalf("spawn-blocked alert does not use the branded command:\n%s", alerts)
	}
	for _, literal := range []string{"liza", "Liza", "LIZA"} {
		if strings.Contains(alerts, literal) {
			t.Fatalf("alerts contain default-brand literal %q:\n%s", literal, alerts)
		}
	}
}

func TestQuotaAlerts_NameTheExpiry(t *testing.T) {
	projectRoot := t.TempDir()
	makeQuotaProjectDir(t, projectRoot)
	resetsAt := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	wantUntil := resetsAt.Add(quotaResetGrace).Format(time.RFC3339)

	if err := RaiseQuotaExhaustion(projectRoot, &QuotaExhaustion{Provider: "claude", Message: "You've hit your session limit", ResetsAt: resetsAt}); err != nil {
		t.Fatalf("RaiseQuotaExhaustion failed: %v", err)
	}
	if err := LogQuotaSpawnBlockedAlert(projectRoot, "claude", "coder"); err != nil {
		t.Fatalf("LogQuotaSpawnBlockedAlert failed: %v", err)
	}

	data, err := os.ReadFile(paths.New(projectRoot).AlertsLogPath())
	if err != nil {
		t.Fatal(err)
	}
	alerts := string(data)
	if !strings.Contains(alerts, "claude: You've hit your session limit (spawns blocked until "+wantUntil+")") {
		t.Fatalf("exhaustion alert does not name the expiry %s:\n%s", wantUntil, alerts)
	}
	if !strings.Contains(alerts, "claude: refused to spawn coder while quota signal is set until "+wantUntil+"; it lifts automatically then") {
		t.Fatalf("spawn-blocked alert does not name the expiry %s:\n%s", wantUntil, alerts)
	}
}
