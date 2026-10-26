package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/providersignal"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// The supervisor's crash path raises the quota signal for #184's result, so
// every Claude supervisor stops until the announced reset.
func TestHandleClassifiedProviderCrash_ClaudeRateLimitedResultRaisesQuotaSignal(t *testing.T) {
	projectRoot := t.TempDir()
	lizaDir := filepath.Join(projectRoot, paths.ProjectDirName())
	if err := os.MkdirAll(lizaDir, 0755); err != nil {
		t.Fatal(err)
	}

	before := time.Now()
	if !handleClassifiedProviderCrash(SupervisorConfig{AgentID: "coder-1", ProjectRoot: projectRoot, CLIName: "claude"}, testhelpers.ClaudeRateLimitedResult429) {
		t.Fatal("handleClassifiedProviderCrash returned false, want true")
	}

	until, blocked := providersignal.QuotaBlockedUntil(projectRoot, "claude")
	if !blocked {
		t.Fatal("claude quota signal does not block")
	}
	wantMin := before.Add(3*time.Hour + 43*time.Minute + time.Minute).Truncate(time.Second)
	if until.Before(wantMin) || until.After(wantMin.Add(time.Minute)) {
		t.Fatalf("block lifts at %s, want the announced reset plus grace (~%s)", until.UTC().Format(time.RFC3339), wantMin.UTC().Format(time.RFC3339))
	}
	alerts, err := os.ReadFile(filepath.Join(lizaDir, "alerts.log"))
	if err != nil {
		t.Fatalf("read alerts log: %v", err)
	}
	if !strings.Contains(string(alerts), "PROVIDER QUOTA EXHAUSTED") {
		t.Fatalf("alerts log lacks PROVIDER QUOTA EXHAUSTED:\n%s", alerts)
	}
}

func TestHandleQuotaSignal_DoesNotWriteObserverAlert(t *testing.T) {
	projectRoot := t.TempDir()
	lizaDir := filepath.Join(projectRoot, paths.ProjectDirName())
	if err := os.MkdirAll(lizaDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := providersignal.LogQuotaAlert(projectRoot, &providersignal.QuotaExhaustion{
		Provider: "codex",
		Message:  "You've hit your usage limit",
	}); err != nil {
		t.Fatalf("LogQuotaAlert failed: %v", err)
	}

	if err := providersignal.WriteQuotaSignal(projectRoot, "codex", "You've hit your usage limit"); err != nil {
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
