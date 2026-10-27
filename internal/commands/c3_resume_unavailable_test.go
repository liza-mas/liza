package commands

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

// C3: resume lists provider-unavailable records by their display provider and
// clears each by the handle it enumerated, whatever the file name encodes.

func c3WriteLegacyUnavailable(t *testing.T, projectRoot, name, provider string) string {
	t.Helper()
	path := filepath.Join(projectRoot, paths.ProjectDirName(), "provider-unavailable-"+name)
	content := "provider: " + provider + "\ndetected: 2020-01-01T00:00:00Z\nmessage: thread/start failed\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestC3ResumeCommandListsAndClearsRecordsByHandle(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModePaused
	testhelpers.WriteInitialState(t, stateFile, state)

	legacyClaude := c3WriteLegacyUnavailable(t, tmpDir, "claude", "claude")
	legacyCodex := c3WriteLegacyUnavailable(t, tmpDir, "codex", "codex")
	legacyStray := c3WriteLegacyUnavailable(t, tmpDir, "zz-stray-acp", "zz-stray-acp")
	acpRaise := &providersignal.ProviderUnavailable{Provider: "codex", Tool: "codex-acp", Message: "session access denied"}
	if err := providersignal.WriteProviderUnavailableSignal(tmpDir, acpRaise); err != nil {
		t.Fatal(err)
	}
	custom := &providersignal.ProviderUnavailable{Provider: "foo", Message: "custom"}
	if err := providersignal.WriteProviderUnavailableSignal(tmpDir, custom); err != nil {
		t.Fatal(err)
	}

	stdout := captureStdout(t, func() {
		if err := ResumeCommand(tmpDir, "human"); err != nil {
			t.Fatalf("ResumeCommand() error = %v", err)
		}
	})

	var cleared []string
	for _, line := range strings.Split(stdout, "\n") {
		if provider, ok := strings.CutPrefix(line, "  Cleared provider-unavailable signal for provider: "); ok {
			cleared = append(cleared, provider)
		}
	}
	// codex and the codex-acp record are one displayed row; the legacy stray
	// keeps the name of its file.
	if got, want := strings.Join(cleared, ","), "claude,codex,foo,zz-stray-acp"; got != want {
		t.Fatalf("cleared rows = %q, want %q (values and order unchanged for legacy files)\n%s", got, want, stdout)
	}
	for _, path := range []string{legacyClaude, legacyCodex, legacyStray, providersignal.ProviderUnavailableSignalPath(tmpDir, "codex-acp"), providersignal.ProviderUnavailableSignalPath(tmpDir, "foo")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("record %s still present after resume: %v", filepath.Base(path), err)
		}
	}
	if left := providersignal.ListProviderUnavailableSignals(tmpDir); len(left) != 0 {
		t.Fatalf("hidden residual block: %+v", left)
	}
	if strings.Contains(stdout, "Warning: failed to clear") {
		t.Fatalf("unexpected clear warning:\n%s", stdout)
	}
}

func TestC3ResumeCommandClearFailureAttributesDisplayProvider(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModePaused
	testhelpers.WriteInitialState(t, stateFile, state)

	// A codex-acp record that cannot be removed is reported as the codex row.
	blocked := providersignal.ProviderUnavailableSignalPath(tmpDir, "codex-acp")
	if err := os.WriteFile(blocked, []byte("provider: codex\ntool: codex-acp\ndetected: 2020-01-01T00:00:00Z\nmessage: m\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blocked, "blocker"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	stdout := captureStdout(t, func() {
		if err := ResumeCommand(tmpDir, "human"); err != nil {
			t.Fatalf("ResumeCommand() error = %v", err)
		}
	})
	if !strings.Contains(stdout, "Warning: failed to clear provider signals: unavailable/codex-acp:") {
		t.Fatalf("failure attribution changed:\n%s", stdout)
	}
	if _, err := os.Stat(blocked); err != nil {
		t.Fatalf("the unclearable record must remain: %v", err)
	}
}
