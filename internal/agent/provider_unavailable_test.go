package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/providersignal"
	"github.com/liza-mas/liza/internal/testhelpers"
)

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

	if !providersignal.CheckProviderUnavailableSignal(projectRoot, "codex", providerKeysOfConfig(models.Config{})) {
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
		providersignal.ProviderUnavailableSignalPath(projectRoot, "codex-acp"),
		filepath.Join(lizaDir, "alerts.log"),
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %q: %v", path, err)
		}
		contents := string(data)
		if !strings.Contains(contents, testhelpers.CodexSessionAccessDiagnostic) {
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
	if !providersignal.CheckProviderUnavailableSignal(projectRoot, "codex", providerKeysOfConfig(models.Config{})) {
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
	}, testhelpers.ClaudeOAuthExpiredOutput)
	if !handled {
		t.Fatal("handleClassifiedProviderCrash returned false, want true: an expired login must not count as a crash")
	}
	if !providersignal.CheckProviderUnavailableSignal(projectRoot, "claude", providerKeysOfConfig(models.Config{})) {
		t.Fatal("claude provider unavailable signal should exist")
	}

	data, err := os.ReadFile(filepath.Join(lizaDir, "alerts.log"))
	if err != nil {
		t.Fatalf("failed to read alerts log: %v", err)
	}
	want := "PROVIDER UNAVAILABLE — claude: " + testhelpers.ClaudeOAuthExpiredDiagnostic
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
	if providersignal.CheckProviderUnavailableSignal(projectRoot, "codex", providerKeysOfConfig(models.Config{})) {
		t.Fatal("provider unavailable signal should not be written from stale output files")
	}
}

func TestHandleProviderUnavailableSignal(t *testing.T) {
	projectRoot := t.TempDir()
	lizaDir := filepath.Join(projectRoot, paths.ProjectDirName())
	if err := os.MkdirAll(lizaDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := providersignal.WriteProviderUnavailableSignal(projectRoot, &providersignal.ProviderUnavailable{Provider: "codex", Message: "session access denied"}); err != nil {
		t.Fatalf("WriteProviderUnavailableSignal failed: %v", err)
	}

	handled := handleProviderUnavailableSignal(SupervisorConfig{
		AgentID:     "coder-1",
		ProjectRoot: projectRoot,
		CLIName:     "codex",
	}, providerKeysOfConfig(models.Config{}))
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
	if err := providersignal.WriteProviderUnavailableSignal(projectRoot, &providersignal.ProviderUnavailable{Provider: "codex", Tool: "codex-acp", Message: "session access denied"}); err != nil {
		t.Fatalf("WriteProviderUnavailableSignal failed: %v", err)
	}

	for _, cliName := range []string{"codex", "codex-acp"} {
		t.Run(cliName, func(t *testing.T) {
			handled := handleProviderUnavailableSignal(SupervisorConfig{
				AgentID:     "coder-1",
				ProjectRoot: projectRoot,
				CLIName:     cliName,
			}, providerKeysOfConfig(models.Config{}))
			if !handled {
				t.Fatal("handleProviderUnavailableSignal returned false, want true")
			}
		})
	}
}
