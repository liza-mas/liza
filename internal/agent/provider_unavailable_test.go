package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/paths"
)

const wantCodexSessionAccessDiagnostic = "thread/start failed: error creating thread: Codex cannot access session files under .codex/sessions (permission denied)"

func TestDetectProviderUnavailable_CodexSessionAccess(t *testing.T) {
	output := `Error: thread/start: thread/start failed: error creating thread: Fatal error: Codex cannot access session files at /Users/me/.codex/sessions (permission denied). If sessions were created using sudo, fix ownership: sudo chown -R $(whoami) /Users/me/.codex (underlying error: Operation not permitted (os error 1))`

	result := DetectProviderUnavailable(output, "codex")
	if result == nil {
		t.Fatal("expected provider unavailable detected, got nil")
	}
	if result.Provider != "codex" {
		t.Errorf("Provider = %q, want %q", result.Provider, "codex")
	}
	if result.Message != wantCodexSessionAccessDiagnostic {
		t.Errorf("Message = %q, want bounded diagnostic %q", result.Message, wantCodexSessionAccessDiagnostic)
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
	if result.Message != wantCodexSessionAccessDiagnostic {
		t.Errorf("Message = %q, want bounded diagnostic %q", result.Message, wantCodexSessionAccessDiagnostic)
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

const wantClaudeOAuthExpiredDiagnostic = "Failed to authenticate: OAuth session expired and could not be refreshed"

// claudeOAuthExpiredOutput is the tail of a Claude stream-json session whose
// login expired, trimmed from an observed run: the CLI fails before any turn.
const claudeOAuthExpiredOutput = `{"type":"assistant","message":{"model":"<synthetic>","role":"assistant","content":[{"type":"text","text":"Failed to authenticate: OAuth session expired and could not be refreshed"}]},"error":"authentication_failed","is_api_error_message":true}
{"type":"result","subtype":"success","is_error":true,"terminal_reason":"api_error","duration_ms":247,"num_turns":1,"result":"Failed to authenticate: OAuth session expired and could not be refreshed"}`

func TestDetectProviderUnavailable_ClaudeOAuthSessionExpired(t *testing.T) {
	result := DetectProviderUnavailable(claudeOAuthExpiredOutput, "claude")
	if result == nil {
		t.Fatal("expected provider unavailable detected, got nil")
	}
	if result.Provider != "claude" {
		t.Errorf("Provider = %q, want %q", result.Provider, "claude")
	}
	if result.Message != wantClaudeOAuthExpiredDiagnostic {
		t.Errorf("Message = %q, want %q", result.Message, wantClaudeOAuthExpiredDiagnostic)
	}

	if result := DetectProviderUnavailable(claudeOAuthExpiredOutput, "codex"); result != nil {
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

func TestProviderUnavailableSignal_NormalizesACPXProviderAliases(t *testing.T) {
	projectRoot := t.TempDir()
	lizaDir := filepath.Join(projectRoot, paths.ProjectDirName())
	if err := os.MkdirAll(lizaDir, 0755); err != nil {
		t.Fatal(err)
	}

	if ProviderUnavailableSignalPath(projectRoot, "codex-acp") != ProviderUnavailableSignalPath(projectRoot, "codex") {
		t.Fatal("codex-acp provider-unavailable signal path should use canonical codex provider")
	}

	tests := []struct {
		name    string
		writer  string
		clearer string
	}{
		{name: "ACP writer native clearer", writer: "codex-acp", clearer: "codex"},
		{name: "native writer ACP clearer", writer: "codex", clearer: "codex-acp"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := WriteProviderUnavailableSignal(projectRoot, tt.writer, "session access denied"); err != nil {
				t.Fatalf("WriteProviderUnavailableSignal failed: %v", err)
			}
			for _, provider := range []string{"codex", "codex-acp"} {
				if !CheckProviderUnavailableSignal(projectRoot, provider) {
					t.Fatalf("%s should find canonical codex signal after %s write", provider, tt.writer)
				}
			}

			if err := ClearProviderUnavailableSignal(projectRoot, tt.clearer); err != nil {
				t.Fatalf("ClearProviderUnavailableSignal failed: %v", err)
			}
			for _, provider := range []string{"codex", "codex-acp"} {
				if CheckProviderUnavailableSignal(projectRoot, provider) {
					t.Fatalf("%s should not find canonical codex signal after %s clear", provider, tt.clearer)
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
	lizaDir := filepath.Join(projectRoot, paths.ProjectDirName())
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

func TestHandleClassifiedProviderCrash_PersistsOnlyBoundedDiagnostic(t *testing.T) {
	projectRoot := t.TempDir()
	lizaDir := filepath.Join(projectRoot, paths.ProjectDirName())
	if err := os.MkdirAll(lizaDir, 0755); err != nil {
		t.Fatal(err)
	}

	privateContext := "/Users/private-account/.codex/sessions"
	output := "transport context: --approve-all\nError: thread/start: thread/start failed: error creating thread: Fatal error: Codex cannot access session files at " + privateContext + " (permission denied)."
	handled := handleClassifiedProviderCrash(SupervisorConfig{
		AgentID:     "orchestrator-1",
		ProjectRoot: projectRoot,
		CLIName:     "codex-acp",
	}, output)
	if !handled {
		t.Fatal("handleClassifiedProviderCrash returned false, want true")
	}

	for _, path := range []string{
		ProviderUnavailableSignalPath(projectRoot, "codex"),
		filepath.Join(lizaDir, "alerts.log"),
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %q: %v", path, err)
		}
		contents := string(data)
		if !strings.Contains(contents, wantCodexSessionAccessDiagnostic) {
			t.Errorf("%s missing bounded diagnostic:\n%s", path, contents)
		}
		for _, forbidden := range []string{privateContext, "--approve-all", "transport context"} {
			if strings.Contains(contents, forbidden) {
				t.Errorf("%s exposed %q:\n%s", path, forbidden, contents)
			}
		}
	}
}

func TestHandleClassifiedProviderCrash_CodexACPWritesCanonicalSignal(t *testing.T) {
	projectRoot := t.TempDir()
	lizaDir := filepath.Join(projectRoot, paths.ProjectDirName())
	if err := os.MkdirAll(lizaDir, 0755); err != nil {
		t.Fatal(err)
	}

	output := `Error: thread/start: thread/start failed: error creating thread: Fatal error: Codex cannot access session files at /Users/me/.codex/sessions (permission denied).`
	handled := handleClassifiedProviderCrash(SupervisorConfig{
		AgentID:     "orchestrator-1",
		ProjectRoot: projectRoot,
		CLIName:     "codex-acp",
	}, output)
	if !handled {
		t.Fatal("handleClassifiedProviderCrash returned false, want true")
	}
	if !CheckProviderUnavailableSignal(projectRoot, "codex") {
		t.Fatal("canonical codex provider unavailable signal should exist")
	}
}

func TestHandleClassifiedProviderCrash_ClaudeOAuthSessionExpiredWritesSignal(t *testing.T) {
	projectRoot := t.TempDir()
	lizaDir := filepath.Join(projectRoot, paths.ProjectDirName())
	if err := os.MkdirAll(lizaDir, 0755); err != nil {
		t.Fatal(err)
	}

	handled := handleClassifiedProviderCrash(SupervisorConfig{
		AgentID:     "orchestrator-1",
		ProjectRoot: projectRoot,
		CLIName:     "claude",
	}, claudeOAuthExpiredOutput)
	if !handled {
		t.Fatal("handleClassifiedProviderCrash returned false, want true: an expired login must not count as a crash")
	}
	if !CheckProviderUnavailableSignal(projectRoot, "claude") {
		t.Fatal("claude provider unavailable signal should exist")
	}

	data, err := os.ReadFile(filepath.Join(lizaDir, "alerts.log"))
	if err != nil {
		t.Fatalf("failed to read alerts log: %v", err)
	}
	want := "PROVIDER UNAVAILABLE — claude: " + wantClaudeOAuthExpiredDiagnostic
	if got := strings.Count(string(data), want); got != 1 {
		t.Fatalf("alerts log has %d %q entries, want exactly 1:\n%s", got, want, string(data))
	}
}

func TestHandleClassifiedProviderCrash_IgnoresStaleOutputFiles(t *testing.T) {
	projectRoot := t.TempDir()
	lizaDir := filepath.Join(projectRoot, paths.ProjectDirName())
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
	lizaDir := filepath.Join(projectRoot, paths.ProjectDirName())
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

func TestHandleProviderUnavailableSignal_CodexAliasesObserveCanonicalSignal(t *testing.T) {
	projectRoot := t.TempDir()
	lizaDir := filepath.Join(projectRoot, paths.ProjectDirName())
	if err := os.MkdirAll(lizaDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := WriteProviderUnavailableSignal(projectRoot, "codex-acp", "session access denied"); err != nil {
		t.Fatalf("WriteProviderUnavailableSignal failed: %v", err)
	}

	for _, cliName := range []string{"codex", "codex-acp"} {
		t.Run(cliName, func(t *testing.T) {
			handled := handleProviderUnavailableSignal(SupervisorConfig{
				AgentID:     "coder-1",
				ProjectRoot: projectRoot,
				CLIName:     cliName,
			})
			if !handled {
				t.Fatal("handleProviderUnavailableSignal returned false, want true")
			}
		})
	}
}
