package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func withAgentBrandValues(t *testing.T, mutate func()) {
	t.Helper()
	oldBinaryName := brand.BinaryName
	oldProjectDirName := brand.ProjectDirName
	mutate()
	t.Cleanup(func() {
		brand.BinaryName = oldBinaryName
		brand.ProjectDirName = oldProjectDirName
	})
}

// withFakeCheckpointSummaryRunner swaps in a deterministic runner for the
// duration of a sub-test and restores the previous one on cleanup.
func withFakeCheckpointSummaryRunner(t *testing.T, fn func(projectRoot, cliName, prompt string, cfg models.Config) error) {
	t.Helper()
	prev := checkpointSummaryRunner
	checkpointSummaryRunner = fn
	t.Cleanup(func() { checkpointSummaryRunner = prev })
}

func TestEmitCheckpointSummary_DefaultOn(t *testing.T) {
	tmp := t.TempDir()
	var called bool
	var gotCLI, gotPrompt string

	withFakeCheckpointSummaryRunner(t, func(projectRoot, cliName, prompt string, _ models.Config) error {
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
	if !strings.Contains(gotPrompt, checkpointSummaryRelPath()) {
		t.Errorf("prompt = %q, want to mention report path %q", gotPrompt, checkpointSummaryRelPath())
	}
}

func TestEmitCheckpointSummary_UsesBrandedProjectPathsInPrompt(t *testing.T) {
	withAgentBrandValues(t, func() {
		brand.ProjectDirName = ".acme"
	})
	tmp := t.TempDir()
	var gotPrompt string
	withFakeCheckpointSummaryRunner(t, func(_, _, prompt string, _ models.Config) error {
		gotPrompt = prompt
		return nil
	})

	emitCheckpointSummary(tmp, "task-branded", models.Config{})

	for _, want := range []string{".acme/state.yaml", ".acme/checkpoint-summary.md"} {
		if !strings.Contains(gotPrompt, want) {
			t.Fatalf("prompt = %q, want %q", gotPrompt, want)
		}
	}
	if strings.Contains(gotPrompt, ".liza/") {
		t.Fatalf("prompt = %q, want no default project dir", gotPrompt)
	}
}

func TestEmitCheckpointSummary_OptOut(t *testing.T) {
	tmp := t.TempDir()
	called := false
	withFakeCheckpointSummaryRunner(t, func(string, string, string, models.Config) error {
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
	withFakeCheckpointSummaryRunner(t, func(string, string, string, models.Config) error {
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
	withFakeCheckpointSummaryRunner(t, func(string, string, string, models.Config) error {
		return os.ErrNotExist
	})

	// Must not panic / must not propagate — runner errors are best-effort.
	emitCheckpointSummary(tmp, "task-4", models.Config{})
}

func TestEmitCheckpointSummary_HonoursConfiguredCLI(t *testing.T) {
	tmp := t.TempDir()
	var gotCLI string
	withFakeCheckpointSummaryRunner(t, func(_, cliName, _ string, _ models.Config) error {
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
		{cli: "claude", stdin: true, argHints: []string{"-p"}},
		{cli: "codex", stdin: true, argHints: []string{"exec"}},
		{cli: "gemini", stdin: true, argHints: []string{"-p"}},
		{cli: "vibe", stdin: false, argHints: []string{"-p"}},
		{cli: "kimi", stdin: true, argHints: []string{"-p"}},
	}

	for _, c := range cases {
		args, useStdin, err := checkpointSummaryCLIArgs(c.cli, "prompt", nil)
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
	if _, _, err := checkpointSummaryCLIArgs("not-a-cli", "x", nil); err == nil {
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

func TestRunCheckpointSummaryCLI_WritesLizaOwnedReport(t *testing.T) {
	tmp := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmp)
	installFakeCLI(t, "claude", []string{
		"mkdir -p .liza",
		"printf '# checkpoint summary\\n' > .liza/checkpoint-summary.md",
	})

	err := runCheckpointSummaryCLI(tmp, "claude", "prompt", models.Config{})
	if err != nil {
		t.Fatalf("runCheckpointSummaryCLI() error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(tmp, filepath.FromSlash(checkpointSummaryRelPath()))); statErr != nil {
		t.Errorf("expected report at %s: %v", checkpointSummaryRelPath(), statErr)
	}
}

func TestRunCheckpointSummaryCLI_WritesBrandedReport(t *testing.T) {
	withAgentBrandValues(t, func() {
		brand.ProjectDirName = ".acme"
	})
	tmp := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmp)
	installFakeCLI(t, "claude", []string{
		"mkdir -p .acme",
		"printf '# checkpoint summary\\n' > .acme/checkpoint-summary.md",
	})

	err := runCheckpointSummaryCLI(tmp, "claude", "prompt", models.Config{})
	if err != nil {
		t.Fatalf("runCheckpointSummaryCLI() error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(tmp, ".acme", "checkpoint-summary.md")); statErr != nil {
		t.Errorf("expected branded report: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(tmp, ".liza", "checkpoint-summary.md")); !os.IsNotExist(statErr) {
		t.Errorf("default report path exists or stat failed unexpectedly: %v", statErr)
	}
}

func TestRunCheckpointSummaryCLI_ReportMissing(t *testing.T) {
	tmp := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmp)
	installFakeCLI(t, "claude", nil)

	err := runCheckpointSummaryCLI(tmp, "claude", "prompt", models.Config{})
	if err == nil {
		t.Fatal("expected report missing error, got nil")
	}
	if !strings.Contains(err.Error(), "report missing") {
		t.Fatalf("error = %q, want report missing", err.Error())
	}
}

func TestRunCheckpointSummaryCLI_RejectsUnexpectedProjectMutation(t *testing.T) {
	tmp := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmp)
	installFakeCLI(t, "claude", []string{
		"mkdir -p .liza",
		"printf '# checkpoint summary\\n' > .liza/checkpoint-summary.md",
		"printf 'unexpected\\n' > unexpected.txt",
	})

	err := runCheckpointSummaryCLI(tmp, "claude", "prompt", models.Config{})
	if err == nil {
		t.Fatal("expected unexpected mutation error, got nil")
	}
	if !strings.Contains(err.Error(), "modified unexpected paths") {
		t.Fatalf("error = %q, want unexpected paths", err.Error())
	}
	if !strings.Contains(err.Error(), "unexpected.txt") {
		t.Fatalf("error = %q, want unexpected.txt", err.Error())
	}
}

func TestRunCheckpointSummaryCLI_RejectsUnexpectedLizaMutation(t *testing.T) {
	tmp := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmp)
	installFakeCLI(t, "claude", []string{
		"mkdir -p .liza",
		"printf '# checkpoint summary\\n' > .liza/checkpoint-summary.md",
		"printf 'unexpected\\n' > .liza/other.md",
	})

	err := runCheckpointSummaryCLI(tmp, "claude", "prompt", models.Config{})
	if err == nil {
		t.Fatal("expected unexpected .liza mutation error, got nil")
	}
	if !strings.Contains(err.Error(), ".liza/other.md") {
		t.Fatalf("error = %q, want .liza/other.md", err.Error())
	}
}

func TestRunCheckpointSummaryCLI_RejectsAlreadyDirtyMutation(t *testing.T) {
	tmp := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmp)
	if err := os.WriteFile(filepath.Join(tmp, "notes.md"), []byte("clean\n"), 0o644); err != nil {
		t.Fatalf("write notes.md: %v", err)
	}
	testhelpers.MustGit(t, tmp, "add", "notes.md")
	testhelpers.MustGit(t, tmp, "commit", "-m", "Add notes")
	if err := os.WriteFile(filepath.Join(tmp, "notes.md"), []byte("human draft\n"), 0o644); err != nil {
		t.Fatalf("dirty notes.md: %v", err)
	}

	installFakeCLI(t, "claude", []string{
		"mkdir -p .liza",
		"printf '# checkpoint summary\\n' > .liza/checkpoint-summary.md",
		"printf 'cli overwrite with different size\\n' > notes.md",
	})

	err := runCheckpointSummaryCLI(tmp, "claude", "prompt", models.Config{})
	if err == nil {
		t.Fatal("expected already-dirty mutation error, got nil")
	}
	if !strings.Contains(err.Error(), "notes.md") {
		t.Fatalf("error = %q, want notes.md", err.Error())
	}
}

func installFakeCLI(t *testing.T, name string, body []string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake shell CLI helper is Unix-only")
	}

	binDir := t.TempDir()
	path := filepath.Join(binDir, name)
	lines := []string{"#!/bin/sh", "set -eu"}
	lines = append(lines, body...)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o755); err != nil {
		t.Fatalf("write fake CLI: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
