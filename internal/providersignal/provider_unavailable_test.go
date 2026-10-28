package providersignal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/agentlaunch"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// defaultKeys are the provider keys of the embedded catalog under an empty
// configuration.
func defaultKeys() MatchKeys {
	return NewMatchKeys(agentlaunch.BuiltInAgentTools(), models.Config{})
}

func TestDetectProviderUnavailable_CodexSessionAccess(t *testing.T) {
	output := `Error: thread/start: thread/start failed: error creating thread: Fatal error: Codex cannot access session files at /Users/me/.codex/sessions (permission denied). If sessions were created using sudo, fix ownership: sudo chown -R $(whoami) /Users/me/.codex (underlying error: Operation not permitted (os error 1))`

	result := DetectProviderUnavailable(output, "codex")
	if result == nil {
		t.Fatal("expected provider unavailable detected, got nil")
	}
	if result.Provider != "codex" {
		t.Errorf("Provider = %q, want %q", result.Provider, "codex")
	}
	if result.Message != testhelpers.CodexSessionAccessDiagnostic {
		t.Errorf("Message = %q, want bounded diagnostic %q", result.Message, testhelpers.CodexSessionAccessDiagnostic)
	}
	if strings.Contains(result.Message, "/Users/me") {
		t.Errorf("Message exposed private session path: %q", result.Message)
	}
}

func TestDetectProviderUnavailable_CodexACPSessionAccess(t *testing.T) {
	output := `Error: thread/start: thread/start failed: error creating thread: Fatal error: Codex cannot access session files at /Users/me/.codex/sessions (permission denied).`

	result := DetectProviderUnavailable(output, "codex-acp")
	if result == nil {
		t.Fatal("expected provider unavailable detected, got nil")
	}
	if result.Provider != "codex" {
		t.Errorf("Provider = %q, want %q", result.Provider, "codex")
	}
	if result.Message != testhelpers.CodexSessionAccessDiagnostic {
		t.Errorf("Message = %q, want bounded diagnostic %q", result.Message, testhelpers.CodexSessionAccessDiagnostic)
	}
}

func TestDetectProviderUnavailable_WrongProvider(t *testing.T) {
	output := `Error: thread/start: thread/start failed: error creating thread: Fatal error: Codex cannot access session files at /Users/me/.codex/sessions (permission denied).`

	result := DetectProviderUnavailable(output, "claude")
	if result != nil {
		t.Errorf("expected nil for wrong provider, got %+v", result)
	}
}

func TestDetectProviderUnavailable_CodexACPIgnoresUnrelatedOutput(t *testing.T) {
	if result := DetectProviderUnavailable("new unrelated crash", "codex-acp"); result != nil {
		t.Errorf("expected nil for unrelated output, got %+v", result)
	}
}

func TestDetectProviderUnavailable_RequiresBoundedEvidenceLine(t *testing.T) {
	output := "Error: thread/start failed\nFatal error: Codex cannot access session files\npath: /Users/me/.codex/sessions\ncause: permission denied"

	result := DetectProviderUnavailable(output, "codex")
	if result != nil {
		t.Fatalf("DetectProviderUnavailable() = %+v, want nil for cross-line evidence", result)
	}
}

func TestDetectProviderUnavailable_ClaudeOAuthSessionExpired(t *testing.T) {
	result := DetectProviderUnavailable(testhelpers.ClaudeOAuthExpiredOutput, "claude")
	if result == nil {
		t.Fatal("expected provider unavailable detected, got nil")
	}
	if result.Provider != "claude" {
		t.Errorf("Provider = %q, want %q", result.Provider, "claude")
	}
	if result.Message != testhelpers.ClaudeOAuthExpiredDiagnostic {
		t.Errorf("Message = %q, want %q", result.Message, testhelpers.ClaudeOAuthExpiredDiagnostic)
	}

	if result := DetectProviderUnavailable(testhelpers.ClaudeOAuthExpiredOutput, "codex"); result != nil {
		t.Errorf("expected nil for wrong provider, got %+v", result)
	}
}

func TestDetectProviderUnavailable_ClaudeIgnoresQuotedAuthFailure(t *testing.T) {
	// A healthy session that read alerts quoting an earlier expired login.
	output := `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"Failed to authenticate: OAuth session expired and could not be refreshed"}]}}
{"type":"result","subtype":"success","is_error":false,"result":"Unblocked the tasks held while the OAuth session was expired."}`

	if result := DetectProviderUnavailable(output, "claude"); result != nil {
		t.Errorf("expected nil for quoted auth failure, got %+v", result)
	}
}

func TestProviderUnavailableSignal_WriteCheckClear(t *testing.T) {
	projectRoot := t.TempDir()
	lizaDir := filepath.Join(projectRoot, paths.ProjectDirName())
	if err := os.MkdirAll(lizaDir, 0755); err != nil {
		t.Fatal(err)
	}

	if CheckProviderUnavailableSignal(projectRoot, "codex", defaultKeys) {
		t.Fatal("signal should not exist before write")
	}

	if err := WriteProviderUnavailableSignal(projectRoot, &ProviderUnavailable{Provider: "codex", Message: "session access denied"}); err != nil {
		t.Fatalf("WriteProviderUnavailableSignal failed: %v", err)
	}

	if !CheckProviderUnavailableSignal(projectRoot, "codex", defaultKeys) {
		t.Fatal("signal should exist after write")
	}

	if CheckProviderUnavailableSignal(projectRoot, "claude", defaultKeys) {
		t.Fatal("claude signal should not exist")
	}

	if err := ClearProviderUnavailableSignal(ProviderUnavailableSignalPath(projectRoot, "codex")); err != nil {
		t.Fatalf("ClearProviderUnavailableSignal failed: %v", err)
	}

	if CheckProviderUnavailableSignal(projectRoot, "codex", defaultKeys) {
		t.Fatal("signal should not exist after clear")
	}
}

func TestProviderUnavailableSignal_AliasesObserveEachOthersRecord(t *testing.T) {
	projectRoot := t.TempDir()
	lizaDir := filepath.Join(projectRoot, paths.ProjectDirName())
	if err := os.MkdirAll(lizaDir, 0755); err != nil {
		t.Fatal(err)
	}

	for _, writer := range []string{"codex-acp", "codex"} {
		t.Run(writer, func(t *testing.T) {
			if err := WriteProviderUnavailableSignal(projectRoot, &ProviderUnavailable{Provider: "codex", Tool: writer, Message: "session access denied"}); err != nil {
				t.Fatalf("WriteProviderUnavailableSignal failed: %v", err)
			}
			for _, provider := range []string{"codex", "codex-acp"} {
				if !CheckProviderUnavailableSignal(projectRoot, provider, defaultKeys) {
					t.Fatalf("%s should find the signal written for %s", provider, writer)
				}
			}

			if err := ClearProviderUnavailableSignal(ProviderUnavailableSignalPath(projectRoot, writer)); err != nil {
				t.Fatalf("ClearProviderUnavailableSignal failed: %v", err)
			}
			for _, provider := range []string{"codex", "codex-acp"} {
				if CheckProviderUnavailableSignal(projectRoot, provider, defaultKeys) {
					t.Fatalf("%s should not find the signal after %s record clear", provider, writer)
				}
			}
		})
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
	if err := WriteProviderUnavailableSignal(projectRoot, &ProviderUnavailable{Provider: "codex", Message: "session access denied"}); err != nil {
		t.Fatalf("WriteProviderUnavailableSignal failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(brandedDir, "provider-unavailable-codex")); err != nil {
		t.Fatalf("provider-unavailable signal not written under branded dir: %v", err)
	}
	if signals := ListProviderUnavailableSignals(projectRoot); len(signals) != 1 || signals[0].Path != filepath.Join(brandedDir, "provider-unavailable-codex") {
		t.Fatalf("ListProviderUnavailableSignals() = %+v, want the record under the branded project dir", signals)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".liza")); !os.IsNotExist(err) {
		t.Fatalf("legacy .liza state = %v, want not created", err)
	}
}
