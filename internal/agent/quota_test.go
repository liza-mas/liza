package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/paths"
)

func withTestProjectDirName(t *testing.T, dirName string) {
	t.Helper()
	previous := brand.ProjectDirName
	brand.ProjectDirName = dirName
	t.Cleanup(func() {
		brand.ProjectDirName = previous
	})
}

func TestDetectQuotaExhaustion_CodexMatch(t *testing.T) {
	output := `{"type":"turn.started"}
{"type":"error","message":"You've hit your usage limit. Upgrade to Pro."}
{"type":"turn.failed"}`

	result := DetectQuotaExhaustion(output, "codex")
	if result == nil {
		t.Fatal("expected quota exhaustion detected, got nil")
	}
	if result.Provider != "codex" {
		t.Errorf("Provider = %q, want %q", result.Provider, "codex")
	}
	if result.Message == "" {
		t.Error("Message should not be empty")
	}
}

func TestDetectQuotaExhaustion_ClaudeMatch(t *testing.T) {
	output := `{"type":"result","subtype":"success","is_error":true,"duration_ms":521,"duration_api_ms":0,"num_turns":1,"result":"You're out of extra usage · resets 7pm (Europe/Paris)","stop_reason":"stop_sequence"}`

	result := DetectQuotaExhaustion(output, "claude")
	if result == nil {
		t.Fatal("expected quota exhaustion detected, got nil")
	}
	if result.Provider != "claude" {
		t.Errorf("Provider = %q, want %q", result.Provider, "claude")
	}
	if result.Message == "" {
		t.Error("Message should not be empty")
	}
}

func TestDetectQuotaExhaustion_ClaudeHitYourLimitMatch(t *testing.T) {
	output := `{"type":"result","subtype":"success","is_error":true,"duration_ms":7072,"duration_api_ms":0,"num_turns":1,"result":"You've hit your limit · resets 8pm (Europe/Paris)","stop_reason":"stop_sequence"}`

	result := DetectQuotaExhaustion(output, "claude")
	if result == nil {
		t.Fatal("expected quota exhaustion detected, got nil")
	}
	if result.Provider != "claude" {
		t.Errorf("Provider = %q, want %q", result.Provider, "claude")
	}
	if result.Message == "" {
		t.Error("Message should not be empty")
	}
}

func TestDetectQuotaExhaustion_ClaudeSessionLimitMatch(t *testing.T) {
	output := `{"type":"result","subtype":"success","is_error":true,"duration_ms":7072,"duration_api_ms":0,"num_turns":1,"result":"You've hit your session limit · resets 2:20pm (Europe/Paris)","stop_reason":"stop_sequence"}`

	result := DetectQuotaExhaustion(output, "claude")
	if result == nil {
		t.Fatal("expected quota exhaustion detected, got nil")
	}
	if result.Provider != "claude" {
		t.Errorf("Provider = %q, want %q", result.Provider, "claude")
	}
	if result.Message == "" {
		t.Error("Message should not be empty")
	}
}

func TestDetectQuotaExhaustion_ClaudeHitYourLimitWithoutResetMatches(t *testing.T) {
	output := `{"type":"error","message":"You've hit your limit."}`

	result := DetectQuotaExhaustion(output, "claude")
	if result == nil {
		t.Fatal("expected quota exhaustion detected, got nil")
	}
	if result.Provider != "claude" {
		t.Errorf("Provider = %q, want %q", result.Provider, "claude")
	}
	if result.Message == "" {
		t.Error("Message should not be empty")
	}
}

func TestDetectQuotaExhaustion_CursorACPUpgradePlanMatch(t *testing.T) {
	output := "\n\nUpgrade your plan to continue"

	result := DetectQuotaExhaustion(output, "cursor-acp")
	if result == nil {
		t.Fatal("expected quota exhaustion detected, got nil")
	}
	if result.Provider != "cursor" {
		t.Errorf("Provider = %q, want %q", result.Provider, "cursor")
	}
	if !strings.Contains(result.Message, "Upgrade your plan") {
		t.Errorf("Message = %q, want Cursor upgrade message", result.Message)
	}
}

func TestDetectQuotaExhaustion_WrongProvider(t *testing.T) {
	output := `{"type":"error","message":"You're out of extra usage."}`

	result := DetectQuotaExhaustion(output, "codex")
	if result != nil {
		t.Errorf("expected nil for non-matching provider, got %+v", result)
	}
}

func TestDetectQuotaExhaustion_NoMatch(t *testing.T) {
	output := `{"type":"turn.completed","usage":{"input_tokens":100}}`

	result := DetectQuotaExhaustion(output, "codex")
	if result != nil {
		t.Errorf("expected nil for non-matching output, got %+v", result)
	}
}

func TestDetectQuotaExhaustion_EmptyOutput(t *testing.T) {
	result := DetectQuotaExhaustion("", "codex")
	if result != nil {
		t.Errorf("expected nil for empty output, got %+v", result)
	}
}

func TestQuotaSignal_WriteCheckClear(t *testing.T) {
	projectRoot := t.TempDir()
	lizaDir := filepath.Join(projectRoot, paths.ProjectDirName())
	if err := os.MkdirAll(lizaDir, 0755); err != nil {
		t.Fatal(err)
	}

	if CheckQuotaSignal(projectRoot, "codex") {
		t.Fatal("signal should not exist before write")
	}

	if err := WriteQuotaSignal(projectRoot, "codex", "You've hit your usage limit"); err != nil {
		t.Fatalf("WriteQuotaSignal failed: %v", err)
	}

	if !CheckQuotaSignal(projectRoot, "codex") {
		t.Fatal("signal should exist after write")
	}

	// Other providers unaffected
	if CheckQuotaSignal(projectRoot, "claude") {
		t.Fatal("claude signal should not exist")
	}

	if err := ClearQuotaSignal(projectRoot, "codex"); err != nil {
		t.Fatalf("ClearQuotaSignal failed: %v", err)
	}

	if CheckQuotaSignal(projectRoot, "codex") {
		t.Fatal("signal should not exist after clear")
	}
}

func TestQuotaSignalUsesBrandedProjectDir(t *testing.T) {
	withTestProjectDirName(t, ".acme-agent")
	projectRoot := t.TempDir()
	brandedDir := filepath.Join(projectRoot, ".acme-agent")
	if err := os.MkdirAll(brandedDir, 0755); err != nil {
		t.Fatal(err)
	}

	if got := QuotaSignalPath(projectRoot, "codex"); got != filepath.Join(brandedDir, "provider-quota-exhausted-codex") {
		t.Fatalf("QuotaSignalPath() = %q, want branded project dir", got)
	}
	if got := QuotaSignalGlob(projectRoot); got != filepath.Join(brandedDir, "provider-quota-exhausted-*") {
		t.Fatalf("QuotaSignalGlob() = %q, want branded project dir", got)
	}
	if err := RaiseQuotaExhaustion(projectRoot, &QuotaExhaustion{Provider: "codex", Message: "limit"}); err != nil {
		t.Fatalf("RaiseQuotaExhaustion failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(brandedDir, "provider-quota-exhausted-codex")); err != nil {
		t.Fatalf("quota signal not written under branded dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(brandedDir, "alerts.log")); err != nil {
		t.Fatalf("alerts log not written under branded dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".liza")); !os.IsNotExist(err) {
		t.Fatalf("legacy .liza state = %v, want not created", err)
	}
}

func TestQuotaSignal_NormalizesACPXProviderAliases(t *testing.T) {
	projectRoot := t.TempDir()
	lizaDir := filepath.Join(projectRoot, paths.ProjectDirName())
	if err := os.MkdirAll(lizaDir, 0755); err != nil {
		t.Fatal(err)
	}

	if QuotaSignalPath(projectRoot, "codex-acp") != QuotaSignalPath(projectRoot, "codex") {
		t.Fatalf("codex-acp quota signal path should use canonical codex provider")
	}

	if err := WriteQuotaSignal(projectRoot, "codex-acp", "You've hit your usage limit"); err != nil {
		t.Fatalf("WriteQuotaSignal failed: %v", err)
	}
	if !CheckQuotaSignal(projectRoot, "codex") {
		t.Fatal("codex signal should exist after codex-acp write")
	}
	if !CheckQuotaSignal(projectRoot, "codex-acp") {
		t.Fatal("codex-acp should find canonical codex signal")
	}

	if err := ClearQuotaSignal(projectRoot, "codex-acp"); err != nil {
		t.Fatalf("ClearQuotaSignal failed: %v", err)
	}
	if CheckQuotaSignal(projectRoot, "codex") {
		t.Fatal("canonical codex signal should not exist after clearing codex-acp")
	}
}

func TestRaiseQuotaExhaustion_WritesAlertAndSignal(t *testing.T) {
	projectRoot := t.TempDir()
	lizaDir := filepath.Join(projectRoot, paths.ProjectDirName())
	if err := os.MkdirAll(lizaDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := RaiseQuotaExhaustion(projectRoot, &QuotaExhaustion{
		Provider: "codex",
		Message:  "You've hit your usage limit",
	}); err != nil {
		t.Fatalf("RaiseQuotaExhaustion failed: %v", err)
	}

	if !CheckQuotaSignal(projectRoot, "codex") {
		t.Fatal("signal should exist after raise")
	}

	alertsPath := filepath.Join(lizaDir, "alerts.log")
	data, err := os.ReadFile(alertsPath)
	if err != nil {
		t.Fatalf("failed to read alerts log: %v", err)
	}
	alerts := string(data)
	if !strings.Contains(alerts, "PROVIDER QUOTA EXHAUSTED") {
		t.Fatalf("alerts log missing quota alert:\n%s", alerts)
	}
	if !strings.Contains(alerts, "codex: You've hit your usage limit") {
		t.Fatalf("alerts log missing quota details:\n%s", alerts)
	}
}

func TestClearQuotaSignal_Idempotent(t *testing.T) {
	projectRoot := t.TempDir()

	// Clear on non-existent file should not error.
	if err := ClearQuotaSignal(projectRoot, "codex"); err != nil {
		t.Fatalf("ClearQuotaSignal on missing file: %v", err)
	}
}

func TestHandleQuotaSignal_DoesNotWriteObserverAlert(t *testing.T) {
	projectRoot := t.TempDir()
	lizaDir := filepath.Join(projectRoot, paths.ProjectDirName())
	if err := os.MkdirAll(lizaDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := LogQuotaAlert(projectRoot, &QuotaExhaustion{
		Provider: "codex",
		Message:  "You've hit your usage limit",
	}); err != nil {
		t.Fatalf("LogQuotaAlert failed: %v", err)
	}

	if err := WriteQuotaSignal(projectRoot, "codex", "You've hit your usage limit"); err != nil {
		t.Fatalf("WriteQuotaSignal failed: %v", err)
	}

	handled := handleQuotaSignal(SupervisorConfig{
		AgentID:     "coder-1",
		ProjectRoot: projectRoot,
		CLIName:     "codex",
	})
	if !handled {
		t.Fatal("handleQuotaSignal returned false, want true")
	}

	alertsPath := filepath.Join(lizaDir, "alerts.log")
	data, err := os.ReadFile(alertsPath)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("failed to read alerts log: %v", err)
	}
	if got := strings.Count(string(data), "PROVIDER QUOTA EXHAUSTED"); got != 1 {
		t.Fatalf("observer changed quota alert count: got %d, want 1\n%s", got, string(data))
	}
}

func TestLatestOutputContent(t *testing.T) {
	dir := t.TempDir()

	// Write two files — should return the latest (lexicographically last).
	if err := os.WriteFile(filepath.Join(dir, "agent-1-20260328-100000.txt"), []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent-1-20260328-110000.txt"), []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}

	content := latestOutputContent(dir, "agent-1", ".txt")
	if content != "new" {
		t.Errorf("latestOutputContent = %q, want %q", content, "new")
	}
}

func TestLatestAgentOutputContent_IncludesStderr(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "agent-1-20260328-110000.txt"), []byte("stdout"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent-1-20260328-110000.err"), []byte("stderr"), 0644); err != nil {
		t.Fatal(err)
	}

	content := latestAgentOutputContent(dir, "agent-1")
	if !strings.Contains(content, "stdout") {
		t.Errorf("latestAgentOutputContent = %q, want stdout content", content)
	}
	if !strings.Contains(content, "stderr") {
		t.Errorf("latestAgentOutputContent = %q, want stderr content", content)
	}
}

func TestLatestAgentOutputContent_StderrOnly(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "agent-1-20260328-110000.err"), []byte("stderr"), 0644); err != nil {
		t.Fatal(err)
	}

	content := latestAgentOutputContent(dir, "agent-1")
	if strings.Contains(content, "stdout") {
		t.Errorf("latestAgentOutputContent = %q, did not expect stdout content", content)
	}
	if !strings.Contains(content, "stderr") {
		t.Errorf("latestAgentOutputContent = %q, want stderr content", content)
	}
}

func TestLatestAgentOutputContent_DoesNotMixStaleStderr(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "agent-1-20260328-100000.err"), []byte("old provider unavailable"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent-1-20260328-110000.txt"), []byte("new unrelated crash"), 0644); err != nil {
		t.Fatal(err)
	}

	content := latestAgentOutputContent(dir, "agent-1")
	if strings.Contains(content, "old provider unavailable") {
		t.Errorf("latestAgentOutputContent = %q, should not include stale stderr", content)
	}
	if !strings.Contains(content, "new unrelated crash") {
		t.Errorf("latestAgentOutputContent = %q, want latest stdout content", content)
	}
}

func TestLatestOutputContent_NoFiles(t *testing.T) {
	dir := t.TempDir()

	content := latestOutputContent(dir, "agent-1", ".txt")
	if content != "" {
		t.Errorf("latestOutputContent = %q, want empty", content)
	}
}

// writeRawQuotaSignal writes a signal file as another binary or an earlier
// detection would have left it, so tests control its recorded times.
func writeRawQuotaSignal(t *testing.T, projectRoot, provider, content string) {
	t.Helper()
	makeQuotaProjectDir(t, projectRoot)
	if err := os.WriteFile(QuotaSignalPath(projectRoot, provider), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func makeQuotaProjectDir(t *testing.T, projectRoot string) {
	t.Helper()
	if err := os.MkdirAll(paths.New(projectRoot).LizaDir(), 0755); err != nil {
		t.Fatal(err)
	}
}

func readQuotaSignal(t *testing.T, projectRoot, provider string) string {
	t.Helper()
	data, err := os.ReadFile(QuotaSignalPath(projectRoot, provider))
	if err != nil {
		t.Fatalf("read quota signal: %v", err)
	}
	return string(data)
}

func TestRaiseQuotaExhaustion_RecordsClaudeAnnouncedReset(t *testing.T) {
	// GIVEN a Claude turn rejected by its session limit, whose stream carries the
	// exact reset (2100-01-01T00:00:00Z) and a zone-qualified text form.
	projectRoot := t.TempDir()
	makeQuotaProjectDir(t, projectRoot)
	output := `{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","resetsAt":4102444800,"rateLimitType":"five_hour"}}
{"type":"result","subtype":"success","is_error":true,"result":"You've hit your session limit · resets 4am (Europe/Paris)"}`

	// WHEN the supervisor detects and raises it
	qe := DetectQuotaExhaustion(output, "claude")
	if qe == nil {
		t.Fatal("expected quota exhaustion detected, got nil")
	}
	if err := RaiseQuotaExhaustion(projectRoot, qe); err != nil {
		t.Fatalf("RaiseQuotaExhaustion failed: %v", err)
	}

	// THEN the signal keeps the announced reset
	if got := readQuotaSignal(t, projectRoot, "claude"); !strings.Contains(got, "resets_at: 2100-01-01T00:00:00Z\n") {
		t.Fatalf("quota signal does not record the announced reset:\n%s", got)
	}
}

func TestRaiseQuotaExhaustion_RecordsCodexAnnouncedReset(t *testing.T) {
	// GIVEN a Codex usage-limit error, whose reset is printed in host-local time
	projectRoot := t.TempDir()
	makeQuotaProjectDir(t, projectRoot)
	output := `{"type":"error","message":"You've hit your usage limit. Visit https://chatgpt.com/codex/settings/usage to purchase more credits or try again at Sep 29th, 2099 2:57 AM."}`

	// WHEN the supervisor detects and raises it
	qe := DetectQuotaExhaustion(output, "codex")
	if qe == nil {
		t.Fatal("expected quota exhaustion detected, got nil")
	}
	if err := RaiseQuotaExhaustion(projectRoot, qe); err != nil {
		t.Fatalf("RaiseQuotaExhaustion failed: %v", err)
	}

	// THEN the signal keeps the announced reset, converted to UTC
	want := "resets_at: " + time.Date(2099, time.September, 29, 2, 57, 0, 0, time.Local).UTC().Format(time.RFC3339) + "\n"
	if got := readQuotaSignal(t, projectRoot, "codex"); !strings.Contains(got, want) {
		t.Fatalf("quota signal does not record the announced reset %q:\n%s", want, got)
	}
}

func TestCheckQuotaSignal_LiftsAtRecordedExpiry(t *testing.T) {
	now := time.Now().UTC()
	signal := func(expires time.Time) string {
		return "provider: claude\n" +
			"detected: " + now.Add(-2*time.Hour).Format(time.RFC3339) + "\n" +
			"message: You've hit your session limit · resets 4am (Europe/Paris)\n" +
			"resets_at: " + expires.Add(-time.Minute).Format(time.RFC3339) + "\n" +
			"expires: " + expires.Format(time.RFC3339) + "\n"
	}

	t.Run("past expiry no longer blocks", func(t *testing.T) {
		projectRoot := t.TempDir()
		writeRawQuotaSignal(t, projectRoot, "claude", signal(now.Add(-time.Minute)))
		if CheckQuotaSignal(projectRoot, "claude") {
			t.Fatal("CheckQuotaSignal = true after the recorded expiry, want false")
		}
	})

	t.Run("future expiry still blocks", func(t *testing.T) {
		projectRoot := t.TempDir()
		writeRawQuotaSignal(t, projectRoot, "claude", signal(now.Add(time.Hour)))
		if !CheckQuotaSignal(projectRoot, "claude") {
			t.Fatal("CheckQuotaSignal = false before the recorded expiry, want true")
		}
	})
}

func TestCheckQuotaSignal_LegacySignalUsesBoundedFallback(t *testing.T) {
	// A signal written before expiries were recorded has only detected:.
	signal := func(detected time.Time) string {
		return "provider: codex\n" +
			"detected: " + detected.UTC().Format(time.RFC3339) + "\n" +
			"message: You've hit your usage limit\n"
	}

	t.Run("old legacy signal no longer blocks", func(t *testing.T) {
		projectRoot := t.TempDir()
		writeRawQuotaSignal(t, projectRoot, "codex", signal(time.Now().Add(-2*time.Hour)))
		if CheckQuotaSignal(projectRoot, "codex") {
			t.Fatal("CheckQuotaSignal = true for a 2h-old legacy signal, want false")
		}
	})

	t.Run("fresh legacy signal still blocks", func(t *testing.T) {
		projectRoot := t.TempDir()
		writeRawQuotaSignal(t, projectRoot, "codex", signal(time.Now().Add(-time.Minute)))
		if !CheckQuotaSignal(projectRoot, "codex") {
			t.Fatal("CheckQuotaSignal = false for a 1min-old legacy signal, want true")
		}
	})
}

func TestQuotaSignal_FreshDetectionRearmsExpiredSignal(t *testing.T) {
	// GIVEN an expired signal: the block has lifted and a probe turn may run
	projectRoot := t.TempDir()
	past := time.Now().UTC().Add(-time.Hour)
	writeRawQuotaSignal(t, projectRoot, "codex", "provider: codex\n"+
		"detected: "+past.Add(-time.Hour).Format(time.RFC3339)+"\n"+
		"message: You've hit your usage limit\n"+
		"resets_at: "+past.Format(time.RFC3339)+"\n"+
		"expires: "+past.Format(time.RFC3339)+"\n")
	if CheckQuotaSignal(projectRoot, "codex") {
		t.Fatal("CheckQuotaSignal = true for an expired signal, want false")
	}

	// WHEN the probe turn hits the limit again
	qe := DetectQuotaExhaustion(`{"type":"error","message":"You've hit your usage limit."}`, "codex")
	if qe == nil {
		t.Fatal("expected quota exhaustion detected, got nil")
	}
	if err := RaiseQuotaExhaustion(projectRoot, qe); err != nil {
		t.Fatalf("RaiseQuotaExhaustion failed: %v", err)
	}

	// THEN the shared predicate blocks again
	if !CheckQuotaSignal(projectRoot, "codex") {
		t.Fatal("CheckQuotaSignal = false after a fresh detection, want true")
	}
}
