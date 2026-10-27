package tui

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

// C3: the TUI resume clears each provider-unavailable record by the handle it
// enumerated, so no record survives under a name a display value would not
// reconstruct.

func TestC3ResumeSystemCmdClearsEveryRecordByHandle(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModePaused
	testhelpers.WriteInitialState(t, stateFile, state)

	legacy := filepath.Join(tmpDir, paths.ProjectDirName(), "provider-unavailable-codex-acp")
	if err := os.WriteFile(legacy, []byte("provider: codex-acp\ndetected: 2020-01-01T00:00:00Z\nmessage: m\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, pu := range []*providersignal.ProviderUnavailable{
		{Provider: "codex", Message: "m"},
		{Provider: "codex", Tool: "codex-acp", Message: "m"},
		{Provider: "foo", Message: "m"},
	} {
		if err := providersignal.WriteProviderUnavailableSignal(tmpDir, pu); err != nil {
			t.Fatal(err)
		}
	}

	result, ok := resumeSystemCmd(tmpDir)().(CmdResultMsg)
	if !ok || !result.Success || result.Message != "System resumed" {
		t.Fatalf("resume result = %+v, want success with the unchanged message", result)
	}
	if left := providersignal.ListProviderUnavailableSignals(tmpDir); len(left) != 0 {
		t.Fatalf("records survive resume: %+v", left)
	}
}

func TestC3ResumeSystemCmdClearFailureNamesDisplayProvider(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModePaused
	testhelpers.WriteInitialState(t, stateFile, state)

	blocked := providersignal.ProviderUnavailableSignalPath(tmpDir, "foo")
	if err := os.MkdirAll(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blocked, "blocker"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	result := resumeSystemCmd(tmpDir)().(CmdResultMsg)
	if !result.Success || !strings.Contains(result.Message, "(warning: failed to clear provider signals: unavailable/foo:") {
		t.Fatalf("message = %q, want the unchanged warning attribution", result.Message)
	}
}
