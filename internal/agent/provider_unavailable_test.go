package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectProviderUnavailable_CodexSessionAccess(t *testing.T) {
	output := `Error: thread/start: thread/start failed: error creating thread: Fatal error: Codex cannot access session files at /Users/me/.codex/sessions (permission denied). If sessions were created using sudo, fix ownership: sudo chown -R $(whoami) /Users/me/.codex (underlying error: Operation not permitted (os error 1))`

	result := DetectProviderUnavailable(output, "codex")
	if result == nil {
		t.Fatal("expected provider unavailable detected, got nil")
	}
	if result.Provider != "codex" {
		t.Errorf("Provider = %q, want %q", result.Provider, "codex")
	}
	if !strings.Contains(result.Message, ".codex/sessions") {
		t.Errorf("Message = %q, want session path", result.Message)
	}
}

func TestDetectProviderUnavailable_WrongProvider(t *testing.T) {
	output := `Error: thread/start: thread/start failed: error creating thread: Fatal error: Codex cannot access session files at /Users/me/.codex/sessions (permission denied).`

	result := DetectProviderUnavailable(output, "claude")
	if result != nil {
		t.Errorf("expected nil for wrong provider, got %+v", result)
	}
}

func TestDetectProviderUnavailable_MultilineCodexSessionAccess(t *testing.T) {
	output := "Error: thread/start failed\nFatal error: Codex cannot access session files\npath: /Users/me/.codex/sessions\ncause: permission denied"

	result := DetectProviderUnavailable(output, "codex")
	if result == nil {
		t.Fatal("expected provider unavailable detected, got nil")
	}
	if !strings.Contains(result.Message, "Codex cannot access session files") {
		t.Errorf("Message = %q, want representative failure line", result.Message)
	}
}

func TestProviderUnavailableSignal_WriteCheckClear(t *testing.T) {
	projectRoot := t.TempDir()
	lizaDir := filepath.Join(projectRoot, ".liza")
	if err := os.MkdirAll(lizaDir, 0755); err != nil {
		t.Fatal(err)
	}

	if CheckProviderUnavailableSignal(projectRoot, "codex") {
		t.Fatal("signal should not exist before write")
	}

	if err := WriteProviderUnavailableSignal(projectRoot, "codex", "session access denied"); err != nil {
		t.Fatalf("WriteProviderUnavailableSignal failed: %v", err)
	}

	if !CheckProviderUnavailableSignal(projectRoot, "codex") {
		t.Fatal("signal should exist after write")
	}

	if CheckProviderUnavailableSignal(projectRoot, "claude") {
		t.Fatal("claude signal should not exist")
	}

	if err := ClearProviderUnavailableSignal(projectRoot, "codex"); err != nil {
		t.Fatalf("ClearProviderUnavailableSignal failed: %v", err)
	}

	if CheckProviderUnavailableSignal(projectRoot, "codex") {
		t.Fatal("signal should not exist after clear")
	}
}

func TestProviderUnavailableSignalUsesBrandedProjectDir(t *testing.T) {
	withTestProjectDirName(t, ".acme-agent")
	projectRoot := t.TempDir()
	brandedDir := filepath.Join(projectRoot, ".acme-agent")
	if err := os.MkdirAll(brandedDir, 0755); err != nil {
		t.Fatal(err)
	}

	if got := ProviderUnavailableSignalPath(projectRoot, "codex"); got != filepath.Join(brandedDir, "provider-unavailable-codex") {
		t.Fatalf("ProviderUnavailableSignalPath() = %q, want branded project dir", got)
	}
	if got := ProviderUnavailableSignalGlob(projectRoot); got != filepath.Join(brandedDir, "provider-unavailable-*") {
		t.Fatalf("ProviderUnavailableSignalGlob() = %q, want branded project dir", got)
	}
	if err := WriteProviderUnavailableSignal(projectRoot, "codex", "session access denied"); err != nil {
		t.Fatalf("WriteProviderUnavailableSignal failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(brandedDir, "provider-unavailable-codex")); err != nil {
		t.Fatalf("provider-unavailable signal not written under branded dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".liza")); !os.IsNotExist(err) {
		t.Fatalf("legacy .liza state = %v, want not created", err)
	}
}

func TestHandleClassifiedProviderCrash_WritesProviderUnavailableSignal(t *testing.T) {
	projectRoot := t.TempDir()
	lizaDir := filepath.Join(projectRoot, ".liza")
	if err := os.MkdirAll(lizaDir, 0755); err != nil {
		t.Fatal(err)
	}

	output := `Error: thread/start: thread/start failed: error creating thread: Fatal error: Codex cannot access session files at /Users/me/.codex/sessions (permission denied).`
	handled := handleClassifiedProviderCrash(SupervisorConfig{
		AgentID:     "orchestrator-1",
		ProjectRoot: projectRoot,
		CLIName:     "codex",
	}, output)
	if !handled {
		t.Fatal("handleClassifiedProviderCrash returned false, want true")
	}

	if !CheckProviderUnavailableSignal(projectRoot, "codex") {
		t.Fatal("provider unavailable signal should exist")
	}

	alertsPath := filepath.Join(lizaDir, "alerts.log")
	data, err := os.ReadFile(alertsPath)
	if err != nil {
		t.Fatalf("failed to read alerts log: %v", err)
	}
	if !strings.Contains(string(data), "PROVIDER UNAVAILABLE") {
		t.Fatalf("alerts log missing provider unavailable entry:\n%s", string(data))
	}
}

func TestHandleClassifiedProviderCrash_IgnoresStaleOutputFiles(t *testing.T) {
	projectRoot := t.TempDir()
	lizaDir := filepath.Join(projectRoot, ".liza")
	outputsDir := filepath.Join(lizaDir, "agent-outputs")
	if err := os.MkdirAll(outputsDir, 0755); err != nil {
		t.Fatal(err)
	}
	staleErr := `Error: thread/start: thread/start failed: error creating thread: Fatal error: Codex cannot access session files at /Users/me/.codex/sessions (permission denied).`
	if err := os.WriteFile(filepath.Join(outputsDir, "orchestrator-1-20260328-100000.err"), []byte(staleErr), 0644); err != nil {
		t.Fatal(err)
	}

	handled := handleClassifiedProviderCrash(SupervisorConfig{
		AgentID:     "orchestrator-1",
		ProjectRoot: projectRoot,
		CLIName:     "codex",
	}, "new unrelated crash")
	if handled {
		t.Fatal("handleClassifiedProviderCrash used stale output files, want current-output-only classification")
	}
	if CheckProviderUnavailableSignal(projectRoot, "codex") {
		t.Fatal("provider unavailable signal should not be written from stale output files")
	}
}

func TestHandleProviderUnavailableSignal(t *testing.T) {
	projectRoot := t.TempDir()
	lizaDir := filepath.Join(projectRoot, ".liza")
	if err := os.MkdirAll(lizaDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := WriteProviderUnavailableSignal(projectRoot, "codex", "session access denied"); err != nil {
		t.Fatalf("WriteProviderUnavailableSignal failed: %v", err)
	}

	handled := handleProviderUnavailableSignal(SupervisorConfig{
		AgentID:     "coder-1",
		ProjectRoot: projectRoot,
		CLIName:     "codex",
	})
	if !handled {
		t.Fatal("handleProviderUnavailableSignal returned false, want true")
	}
}
