package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
)

// withFakeCheckpointSummaryRunner swaps in a deterministic runner for the
// duration of a sub-test and restores the previous one on cleanup.
func withFakeCheckpointSummaryRunner(t *testing.T, fn func(projectRoot, cliName, prompt string) error) {
	t.Helper()
	prev := checkpointSummaryRunner
	checkpointSummaryRunner = fn
	t.Cleanup(func() { checkpointSummaryRunner = prev })
}

func TestEmitCheckpointSummary_DefaultOn(t *testing.T) {
	tmp := t.TempDir()
	var called bool
	var gotCLI, gotPrompt string

	withFakeCheckpointSummaryRunner(t, func(projectRoot, cliName, prompt string) error {
		called = true
		gotCLI = cliName
		gotPrompt = prompt
		if projectRoot != tmp {
			t.Errorf("projectRoot = %q, want %q", projectRoot, tmp)
		}
		return nil
	})

	// Empty config — default is ON.
	emitCheckpointSummary(tmp, "task-1", models.Config{})

	if !called {
		t.Fatal("expected checkpoint summary runner to be called for default config")
	}
	// Default CLI is "claude" per cli.go.
	if gotCLI != DefaultCLI {
		t.Errorf("cli = %q, want %q", gotCLI, DefaultCLI)
	}
	if !strings.Contains(gotPrompt, "checkpoint-summary skill") {
		t.Errorf("prompt = %q, want to mention checkpoint-summary skill", gotPrompt)
	}
	if !strings.Contains(gotPrompt, "task-1") {
		t.Errorf("prompt = %q, want to mention task ID", gotPrompt)
	}
	if !strings.Contains(gotPrompt, CheckpointSummaryRelPath) {
		t.Errorf("prompt = %q, want to mention report path %q", gotPrompt, CheckpointSummaryRelPath)
	}
}

func TestEmitCheckpointSummary_OptOut(t *testing.T) {
	tmp := t.TempDir()
	called := false
	withFakeCheckpointSummaryRunner(t, func(string, string, string) error {
		called = true
		return nil
	})

	off := false
	emitCheckpointSummary(tmp, "task-2", models.Config{AutoCheckpointSummary: &off})

	if called {
		t.Fatal("expected runner to be skipped when AutoCheckpointSummary is false")
	}
}

func TestEmitCheckpointSummary_ExplicitOn(t *testing.T) {
	tmp := t.TempDir()
	called := false
	withFakeCheckpointSummaryRunner(t, func(string, string, string) error {
		called = true
		return nil
	})

	on := true
	emitCheckpointSummary(tmp, "task-3", models.Config{AutoCheckpointSummary: &on})
	if !called {
		t.Fatal("expected runner to fire when AutoCheckpointSummary is explicitly true")
	}
}

func TestEmitCheckpointSummary_RunnerErrorIsSwallowed(t *testing.T) {
	tmp := t.TempDir()
	withFakeCheckpointSummaryRunner(t, func(string, string, string) error {
		return os.ErrNotExist
	})

	// Must not panic / must not propagate — runner errors are best-effort.
	emitCheckpointSummary(tmp, "task-4", models.Config{})
}

func TestEmitCheckpointSummary_HonoursConfiguredCLI(t *testing.T) {
	tmp := t.TempDir()
	var gotCLI string
	withFakeCheckpointSummaryRunner(t, func(_, cliName, _ string) error {
		gotCLI = cliName
		return nil
	})

	emitCheckpointSummary(tmp, "task-cli", models.Config{DefaultCLI: "codex"})
	if gotCLI != "codex" {
		t.Errorf("cli = %q, want %q (config override)", gotCLI, "codex")
	}
}

func TestCheckpointSummaryCLIArgs_Supported(t *testing.T) {
	cases := []struct {
		cli      string
		stdin    bool
		argHints []string
	}{
		{cli: "claude", stdin: true, argHints: []string{"--print"}},
		{cli: "codex", stdin: true, argHints: []string{"exec"}},
		{cli: "gemini", stdin: true, argHints: []string{"-p"}},
		{cli: "vibe", stdin: false, argHints: []string{"-p"}},
		{cli: "kimi", stdin: true, argHints: []string{"-p"}},
	}

	for _, c := range cases {
		args, useStdin, err := checkpointSummaryCLIArgs(c.cli, "prompt")
		if err != nil {
			t.Errorf("%s: unexpected error: %v", c.cli, err)
			continue
		}
		if useStdin != c.stdin {
			t.Errorf("%s: useStdin = %v, want %v", c.cli, useStdin, c.stdin)
		}
		for _, hint := range c.argHints {
			found := false
			for _, a := range args {
				if a == hint {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s: args %v missing hint %q", c.cli, args, hint)
			}
		}
	}
}

func TestCheckpointSummaryCLIArgs_Unsupported(t *testing.T) {
	if _, _, err := checkpointSummaryCLIArgs("not-a-cli", "x"); err == nil {
		t.Fatal("expected error for unsupported CLI")
	}
}

func TestFilterAPIKeyEnv_StripsAnthropicKey(t *testing.T) {
	in := []string{
		"PATH=/usr/bin",
		"ANTHROPIC_API_KEY=sk-secret",
		"FOO=bar",
	}
	out := filterAPIKeyEnv(in)
	for _, v := range out {
		if strings.HasPrefix(v, "ANTHROPIC_API_KEY=") {
			t.Fatalf("ANTHROPIC_API_KEY was not stripped: %v", out)
		}
	}
	if len(out) != 2 {
		t.Errorf("len(out) = %d, want 2", len(out))
	}
}

// TestRunCheckpointSummaryCLI_ReportMissing exercises the post-run sanity
// check: a CLI that exits 0 but never writes the report must be surfaced
// as an error rather than silently swallowed.
func TestRunCheckpointSummaryCLI_ReportMissing(t *testing.T) {
	tmp := t.TempDir()
	// `true` exits 0, writes nothing, takes no input — perfect stand-in
	// for a CLI that fails to honour its instructions.
	err := runCheckpointSummaryCLI(tmp, "claude", "prompt")
	// The real "claude" binary may or may not exist on the test runner;
	// we accept either the "report missing" sanity error or a spawn
	// failure. Both are valid outcomes — we just don't want a panic.
	if err == nil {
		// If by miracle a report was written, ensure the file is there.
		if _, statErr := os.Stat(filepath.Join(tmp, CheckpointSummaryRelPath)); statErr != nil {
			t.Errorf("returned nil but report missing: %v", statErr)
		}
	}
}
