package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
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
	lizaDir := filepath.Join(projectRoot, ".liza")
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
	lizaDir := filepath.Join(projectRoot, ".liza")
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
	lizaDir := filepath.Join(projectRoot, ".liza")
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
	lizaDir := filepath.Join(projectRoot, ".liza")
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
