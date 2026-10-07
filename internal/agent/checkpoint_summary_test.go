package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// withAgentBrandValues applies mutate to the brand variables and restores them
// when the test ends.
//
// Those variables are process-wide: the change is visible to every goroutine,
// not just this test. That is safe here because no test in this package runs in
// parallel and none that reads brand values outlives itself — a goroutine
// still reading brand values after its test returned would race with the
// restore below, and the detector would report it against whichever test
// happened to be running at the time.
//
// Only BinaryName, ProjectDirName, and EnvPrefix are captured, since those are
// what the callers change. Rather than track that list by hand — a mutation of
// any other field would leak into every later test in the package, silently —
// the restore checks the whole struct and fails if anything is left behind.
func withAgentBrandValues(t *testing.T, mutate func()) {
	t.Helper()
	before := brand.RuntimeValues()
	oldBinaryName := brand.BinaryName
	oldProjectDirName := brand.ProjectDirName
	oldEnvPrefix := brand.EnvPrefix
	mutate()
	t.Cleanup(func() {
		brand.BinaryName = oldBinaryName
		brand.ProjectDirName = oldProjectDirName
		brand.EnvPrefix = oldEnvPrefix
		// A field left as "" rather than mutated is masked here: RuntimeValues()
		// runs withDerivedDefaults(), which refills empty fields from
		// NameLower, so a blanked field compares equal and this check misses it.
		if after := brand.RuntimeValues(); after != before {
			t.Errorf("brand values not restored after the test:\n got  %+v\n want %+v\n"+
				"mutate() changed a field this helper does not capture — add it, or the change leaks into the tests that follow",
				after, before)
		}
	})
}

// withFakeCheckpointSummaryRunner swaps in a deterministic runner for the
// duration of a sub-test and restores the previous one on cleanup.
func withFakeCheckpointSummaryRunner(t *testing.T, fn func(ctx context.Context, projectRoot, cliName, prompt string, cfg models.Config) error) {
	t.Helper()
	prev := checkpointSummaryRunner
	checkpointSummaryRunner = fn
	t.Cleanup(func() { checkpointSummaryRunner = prev })
	// Registered last so it runs first: a background summary must not outlive
	// the test and occupy the slot for the next one.
	t.Cleanup(waitForCheckpointSummary)
}

// clearCLIDefaultsEnv isolates CLI resolution from the developer's shell.
func clearCLIDefaultsEnv(t *testing.T) {
	t.Helper()
	t.Setenv("LIZA_DEFAULT_CLI", "")
	t.Setenv("LIZA_DEFAULT_DOER_CLI", "")
	t.Setenv("LIZA_DEFAULT_REVIEWER_CLI", "")
}

func TestEmitCheckpointSummary_DefaultOn(t *testing.T) {
	clearCLIDefaultsEnv(t)
	tmp := t.TempDir()
	var called bool
	var gotCLI, gotPrompt string

	withFakeCheckpointSummaryRunner(t, func(_ context.Context, projectRoot, cliName, prompt string, _ models.Config) error {
		called = true
		gotCLI = cliName
		gotPrompt = prompt
		if projectRoot != tmp {
			t.Errorf("projectRoot = %q, want %q", projectRoot, tmp)
		}
		return nil
	})

	// Empty config — default is ON.
	emitCheckpointSummary(context.Background(), tmp, "task-1", checkpointSummaryScope{}, models.Config{})

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
	withFakeCheckpointSummaryRunner(t, func(_ context.Context, _, _, prompt string, _ models.Config) error {
		gotPrompt = prompt
		return nil
	})

	emitCheckpointSummary(context.Background(), tmp, "task-branded", checkpointSummaryScope{}, models.Config{})

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
	withFakeCheckpointSummaryRunner(t, func(context.Context, string, string, string, models.Config) error {
		called = true
		return nil
	})

	off := false
	emitCheckpointSummary(context.Background(), tmp, "task-2", checkpointSummaryScope{}, models.Config{AutoCheckpointSummary: &off})

	if called {
		t.Fatal("expected runner to be skipped when AutoCheckpointSummary is false")
	}
}

func TestEmitCheckpointSummary_ExplicitOn(t *testing.T) {
	tmp := t.TempDir()
	called := false
	withFakeCheckpointSummaryRunner(t, func(context.Context, string, string, string, models.Config) error {
		called = true
		return nil
	})

	on := true
	emitCheckpointSummary(context.Background(), tmp, "task-3", checkpointSummaryScope{}, models.Config{AutoCheckpointSummary: &on})
	if !called {
		t.Fatal("expected runner to fire when AutoCheckpointSummary is explicitly true")
	}
}

func TestEmitCheckpointSummary_RunnerErrorIsSwallowed(t *testing.T) {
	tmp := t.TempDir()
	withFakeCheckpointSummaryRunner(t, func(context.Context, string, string, string, models.Config) error {
		return os.ErrNotExist
	})

	// Must not panic / must not propagate — runner errors are best-effort.
	emitCheckpointSummary(context.Background(), tmp, "task-4", checkpointSummaryScope{}, models.Config{})
}

func TestEmitCheckpointSummary_HonoursConfiguredCLI(t *testing.T) {
	clearCLIDefaultsEnv(t)
	tmp := t.TempDir()
	var gotCLI string
	withFakeCheckpointSummaryRunner(t, func(_ context.Context, _, cliName, _ string, _ models.Config) error {
		gotCLI = cliName
		return nil
	})

	emitCheckpointSummary(context.Background(), tmp, "task-cli", checkpointSummaryScope{}, models.Config{DefaultCLI: "codex"})
	if gotCLI != "codex" {
		t.Errorf("cli = %q, want %q (config override)", gotCLI, "codex")
	}
}

func TestEmitCheckpointSummary_FollowsOrchestratorCLI(t *testing.T) {
	clearCLIDefaultsEnv(t)
	t.Setenv("LIZA_DEFAULT_DOER_CLI", "codex")
	tmp := t.TempDir()
	var gotCLI string
	withFakeCheckpointSummaryRunner(t, func(_ context.Context, _, cliName, _ string, _ models.Config) error {
		gotCLI = cliName
		return nil
	})

	emitCheckpointSummary(context.Background(), tmp, "task-cli", checkpointSummaryScope{}, models.Config{})
	if gotCLI != "codex" {
		t.Errorf("cli = %q, want %q (orchestrator role default)", gotCLI, "codex")
	}
}

func TestCheckpointSummaryLaunchPlan_Supported(t *testing.T) {
	cases := []struct {
		cli      string
		stdin    bool
		argHints []string
	}{
		{cli: "claude", stdin: true, argHints: []string{"-p"}},
		{cli: "codex", stdin: true, argHints: []string{"exec"}},
		{cli: "gemini", stdin: true, argHints: []string{"-p"}},
		{cli: "vibe", stdin: false, argHints: []string{"-p", "prompt"}},
		{cli: "kimi", stdin: true, argHints: []string{"-p"}},
		{cli: "opencode", stdin: true, argHints: []string{"run", "--dangerously-skip-permissions"}},
	}

	for _, c := range cases {
		plan, err := checkpointSummaryLaunchPlan(t.TempDir(), c.cli, "prompt", models.Config{})
		if err != nil {
			t.Errorf("%s: unexpected error: %v", c.cli, err)
			continue
		}
		if plan.UsesStdin != c.stdin {
			t.Errorf("%s: UsesStdin = %v, want %v", c.cli, plan.UsesStdin, c.stdin)
		}
		for _, hint := range c.argHints {
			if !slices.Contains(plan.Args, hint) {
				t.Errorf("%s: args %v missing hint %q", c.cli, plan.Args, hint)
			}
		}
	}
}

func TestCheckpointSummaryLaunchPlan_ACPToolUsesCLICounterpart(t *testing.T) {
	plan, err := checkpointSummaryLaunchPlan(t.TempDir(), "opencode-acp", "prompt", models.Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.Backend != ToolBackendCLI || plan.Executable != "opencode" {
		t.Errorf("plan = backend %q executable %q, want cli/opencode", plan.Backend, plan.Executable)
	}
	if !plan.UsesStdin {
		t.Error("opencode should read the prompt from stdin, not argv")
	}
}

func TestCheckpointSummaryLaunchPlan_Unsupported(t *testing.T) {
	if _, err := checkpointSummaryLaunchPlan(t.TempDir(), "not-a-cli", "x", models.Config{}); err == nil {
		t.Fatal("expected error for unknown CLI")
	}
}

func TestCheckpointSummaryLaunchPlan_RejectsPromptFileTransport(t *testing.T) {
	cfg := models.Config{AgentTools: map[string]models.AgentToolConfig{
		"filetool": {Backend: ToolBackendCLI, Executable: "filetool", PromptTransport: PromptTransportFile},
	}}
	if _, err := checkpointSummaryLaunchPlan(t.TempDir(), "filetool", "x", cfg); err == nil {
		t.Fatal("expected error for prompt-file transport")
	}
}

func TestEmitCheckpointSummary_FailureWritesAlert(t *testing.T) {
	clearCLIDefaultsEnv(t)
	tmp := t.TempDir()
	withFakeCheckpointSummaryRunner(t, func(context.Context, string, string, string, models.Config) error {
		return errors.New("boom")
	})

	if err := os.MkdirAll(filepath.Join(tmp, paths.ProjectDirName()), 0o755); err != nil {
		t.Fatal(err)
	}
	emitCheckpointSummary(context.Background(), tmp, "SPRINT_COMPLETE", checkpointSummaryScope{}, models.Config{DefaultCLI: "opencode"})

	data, err := os.ReadFile(paths.New(tmp).AlertsLogPath())
	if err != nil {
		t.Fatalf("expected alerts log: %v", err)
	}
	got := string(data)
	for _, want := range []string{"CHECKPOINT SUMMARY FAILED", "opencode", "boom"} {
		if !strings.Contains(got, want) {
			t.Errorf("alert %q missing %q", got, want)
		}
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
		"mkdir -p " + paths.ProjectDirName(),
		"printf '# checkpoint summary\\n' > " + paths.ProjectDirName() + "/checkpoint-summary.md",
	})

	err := runCheckpointSummaryCLI(context.Background(), tmp, "claude", "prompt", models.Config{})
	if err != nil {
		t.Fatalf("runCheckpointSummaryCLI(context.Background(), ) error = %v", err)
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

	err := runCheckpointSummaryCLI(context.Background(), tmp, "claude", "prompt", models.Config{})
	if err != nil {
		t.Fatalf("runCheckpointSummaryCLI(context.Background(), ) error = %v", err)
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

	err := runCheckpointSummaryCLI(context.Background(), tmp, "claude", "prompt", models.Config{})
	if err == nil {
		t.Fatal("expected report missing error, got nil")
	}
	if !strings.Contains(err.Error(), "report missing") {
		t.Fatalf("error = %q, want report missing", err.Error())
	}
}

func TestRunCheckpointSummaryCLI_ToleratesConcurrentWrites(t *testing.T) {
	tmp := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmp)
	if err := os.WriteFile(filepath.Join(tmp, "notes.md"), []byte("clean\n"), 0o644); err != nil {
		t.Fatalf("write notes.md: %v", err)
	}
	testhelpers.MustGit(t, tmp, "add", "notes.md")
	testhelpers.MustGit(t, tmp, "commit", "-m", "Add notes")

	// The handshake is a FIFO outside the repository, so it never shows in its
	// status: opening its write end blocks until the CLI opens it for reading,
	// which proves the run started, and the CLI then waits for a line.
	release := filepath.Join(t.TempDir(), "release")
	installFakeCLI(t, "claude", []string{
		"mkdir -p " + paths.ProjectDirName(),
		"read _ < '" + release + "'",
		"printf '# checkpoint summary\\n' > " + paths.ProjectDirName() + "/checkpoint-summary.md",
	})
	if output, err := exec.Command("mkfifo", release).CombinedOutput(); err != nil {
		t.Fatalf("mkfifo: %v\n%s", err, output)
	}

	result := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		result <- runCheckpointSummaryCLI(context.Background(), tmp, "claude", "prompt", models.Config{})
	}()
	opened := make(chan *os.File, 1)
	go func() {
		defer close(opened)
		if f, err := os.OpenFile(release, os.O_WRONLY, 0); err == nil {
			opened <- f
		}
	}()
	// On every exit path, close the write end (the CLI's read then fails and it
	// exits) or, if the CLI never opened the FIFO, satisfy the pending open, so
	// neither the CLI nor a goroutine outlives the test.
	var writer *os.File
	t.Cleanup(func() {
		if writer == nil {
			go func() {
				if f, err := os.Open(release); err == nil {
					f.Close()
				}
			}()
			if f, ok := <-opened; ok {
				writer = f
			}
		}
		if writer != nil {
			writer.Close()
		}
		select {
		case <-finished:
		case <-time.After(15 * time.Second):
			t.Error("checkpoint-summary runner still running after release")
		}
	})

	select {
	case f, ok := <-opened:
		if !ok {
			t.Fatal("open release FIFO for writing failed")
		}
		writer = f
	case err := <-result:
		t.Fatalf("runCheckpointSummaryCLI(context.Background(), ) returned before the CLI started: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("fake CLI never opened the release FIFO")
	}

	// Writes by other processes while the CLI runs, as supervisors and hooks do.
	statePath := filepath.Join(tmp, paths.ProjectDirName(), paths.StateFileName)
	if err := os.WriteFile(statePath, []byte("sprint: {}\n"), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "notes.md"), []byte("hook output\n"), 0o644); err != nil {
		t.Fatalf("modify notes.md: %v", err)
	}
	if _, err := writer.WriteString("go\n"); err != nil {
		t.Fatalf("release fake CLI: %v", err)
	}

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("runCheckpointSummaryCLI(context.Background(), ) error = %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("runCheckpointSummaryCLI(context.Background(), ) did not return")
	}
	if _, err := os.Stat(filepath.Join(tmp, filepath.FromSlash(checkpointSummaryRelPath()))); err != nil {
		t.Errorf("expected report at %s: %v", checkpointSummaryRelPath(), err)
	}
}

func TestRunCheckpointSummaryCLI_NonZeroExitReturnsError(t *testing.T) {
	tmp := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmp)
	installFakeCLI(t, "claude", []string{
		"mkdir -p " + paths.ProjectDirName(),
		"printf '# checkpoint summary\\n' > " + paths.ProjectDirName() + "/checkpoint-summary.md",
		"exit 7",
	})

	err := runCheckpointSummaryCLI(context.Background(), tmp, "claude", "prompt", models.Config{})
	if err == nil {
		t.Fatal("expected non-zero exit error, got nil")
	}
	if !strings.Contains(err.Error(), "exit status 7") {
		t.Fatalf("error = %q, want exit status 7", err.Error())
	}
}

func TestRunCheckpointSummaryCLI_FailureIncludesOutputTail(t *testing.T) {
	tmp := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmp)
	installFakeCLI(t, "claude", []string{
		"echo 'API Error: 401 invalid credentials' >&2",
		"exit 1",
	})

	err := runCheckpointSummaryCLI(context.Background(), tmp, "claude", "prompt", models.Config{})
	if err == nil {
		t.Fatal("expected non-zero exit error, got nil")
	}
	for _, want := range []string{"exit status 1", "API Error: 401 invalid credentials"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err.Error(), want)
		}
	}
}

func TestRunCheckpointSummaryCLI_FailureMasksSecrets(t *testing.T) {
	const secret = "sk-test-secret-value-123"
	t.Setenv("SUMMARY_TEST_API_KEY", secret)
	tmp := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmp)
	installFakeCLI(t, "claude", []string{
		"echo \"bad key $SUMMARY_TEST_API_KEY\" >&2",
		"exit 1",
	})

	err := runCheckpointSummaryCLI(context.Background(), tmp, "claude", "prompt", models.Config{})
	if err == nil {
		t.Fatal("expected non-zero exit error, got nil")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaks the secret: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "bad key") {
		t.Errorf("error = %q, want the masked output tail", err.Error())
	}
}

func TestOutputTail_KeepsLastBytes(t *testing.T) {
	tail := &outputTail{limit: checkpointSummaryOutputTailBytes}
	data := []byte(strings.Repeat("a", 5000-1) + "z")
	if n, err := tail.Write(data); n != len(data) || err != nil {
		t.Fatalf("Write = %d, %v; want %d, nil", n, err, len(data))
	}
	if len(tail.buf) != checkpointSummaryOutputTailBytes || !tail.truncated {
		t.Fatalf("len = %d, truncated = %v; want %d, true", len(tail.buf), tail.truncated, checkpointSummaryOutputTailBytes)
	}
	if tail.buf[len(tail.buf)-1] != 'z' {
		t.Fatal("tail did not keep the last byte written")
	}
}

func TestRunCheckpointSummaryCLI_ReportEmpty(t *testing.T) {
	tmp := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmp)
	installFakeCLI(t, "claude", []string{
		"mkdir -p " + paths.ProjectDirName(),
		": > " + paths.ProjectDirName() + "/checkpoint-summary.md",
	})

	err := runCheckpointSummaryCLI(context.Background(), tmp, "claude", "prompt", models.Config{})
	if err == nil {
		t.Fatal("expected empty report error, got nil")
	}
	if !strings.Contains(err.Error(), "report is empty") {
		t.Fatalf("error = %q, want report is empty", err.Error())
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

func mergedTask(id string, at time.Time) models.Task {
	return models.Task{ID: id, History: []models.TaskHistoryEntry{{Event: models.TaskEventMerged, Time: at}}}
}

// #171: a summary told to read the whole run outgrew its bound. The scope is
// the work merged since the previous checkpoint, newest first.
func TestBuildCheckpointSummaryScope_MergesSinceThePreviousCheckpoint(t *testing.T) {
	base := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	since := base
	state := &models.State{Tasks: []models.Task{
		mergedTask("before", base.Add(-time.Minute)),
		mergedTask("at-since", base),
		mergedTask("older", base.Add(time.Minute)),
		mergedTask("newer", base.Add(2*time.Minute)),
		mergedTask("after", base.Add(4*time.Minute)),
		{ID: "unmerged"},
	}}
	scope := buildCheckpointSummaryScope(state, &models.PendingCheckpointSummary{At: base.Add(3 * time.Minute), Since: &since})

	if want := []string{"newer", "older"}; !slices.Equal(scope.TaskIDs, want) {
		t.Errorf("TaskIDs = %v, want %v", scope.TaskIDs, want)
	}
	if !scope.Since.Equal(since) || scope.Omitted != 0 {
		t.Errorf("Since = %v, Omitted = %d; want %v, 0", scope.Since, scope.Omitted, since)
	}
}

// Without a previous checkpoint (or on an obligation recorded before Since
// existed) the window opens at the sprint start.
func TestBuildCheckpointSummaryScope_FallsBackToTheSprintStart(t *testing.T) {
	start := time.Date(2026, 10, 7, 8, 0, 0, 0, time.UTC)
	state := &models.State{Tasks: []models.Task{
		mergedTask("previous-sprint", start.Add(-time.Hour)),
		mergedTask("this-sprint", start.Add(time.Hour)),
	}}
	state.Sprint.Timeline.Started = start

	scope := buildCheckpointSummaryScope(state, &models.PendingCheckpointSummary{At: start.Add(2 * time.Hour)})
	if !slices.Equal(scope.TaskIDs, []string{"this-sprint"}) || !scope.Since.Equal(start) {
		t.Errorf("scope = %+v, want only this-sprint since %v", scope, start)
	}
}

// The scope is capped; the remainder is counted so the report can say what
// it does not cover.
func TestBuildCheckpointSummaryScope_CapsTheTaskList(t *testing.T) {
	base := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	state := &models.State{}
	for i := range checkpointSummaryScopeLimit + 3 {
		state.Tasks = append(state.Tasks, mergedTask(fmt.Sprintf("task-%02d", i), base.Add(time.Duration(i+1)*time.Second)))
	}
	scope := buildCheckpointSummaryScope(state, &models.PendingCheckpointSummary{At: base.Add(time.Hour), Since: &base})

	if len(scope.TaskIDs) != checkpointSummaryScopeLimit || scope.Omitted != 3 {
		t.Fatalf("len = %d, Omitted = %d; want %d, 3", len(scope.TaskIDs), scope.Omitted, checkpointSummaryScopeLimit)
	}
	if want := fmt.Sprintf("task-%02d", checkpointSummaryScopeLimit+2); scope.TaskIDs[0] != want {
		t.Errorf("first = %q, want the newest %q", scope.TaskIDs[0], want)
	}
}

func TestBuildCheckpointSummaryPrompt_NamesTheScopeAndTheOneTurnRule(t *testing.T) {
	since := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	prompt := buildCheckpointSummaryPrompt("PLANNING_COMPLETE", checkpointSummaryScope{Since: since, TaskIDs: []string{"task-a", "task-b"}, Omitted: 4})
	for _, want := range []string{
		"since 2026-10-07T09:00:00Z", "- task-a\n", "- task-b\n",
		"4 earlier tasks merged in this window are not covered",
		"do not delegate to subagents", "write the report before ending your",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt lacks %q:\n%s", want, prompt)
		}
	}

	empty := buildCheckpointSummaryPrompt("", checkpointSummaryScope{Since: since})
	if !strings.Contains(empty, "(no task merged in this window)") || strings.Contains(empty, "not covered") {
		t.Errorf("empty-scope prompt:\n%s", empty)
	}
}

// The one-shot summary always runs Claude without subagents: a delegated read
// let the session end its turn before writing (#171).
func TestCheckpointSummaryLaunchPlan_ClaudeDisallowsSubagents(t *testing.T) {
	plan, err := checkpointSummaryLaunchPlan(t.TempDir(), "claude", "prompt", models.Config{})
	if err != nil {
		t.Fatalf("launch plan: %v", err)
	}
	i := slices.Index(plan.Args, "--disallowedTools")
	if i < 0 || i+1 >= len(plan.Args) || plan.Args[i+1] != "Task" {
		t.Errorf("args = %v, want --disallowedTools Task", plan.Args)
	}
}

// A session that ends its turn without writing the report must say what it
// did instead.
func TestRunCheckpointSummaryCLI_ReportMissingIncludesOutputTail(t *testing.T) {
	tmp := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmp)
	installFakeCLI(t, "claude", []string{
		"echo \"I'll write the report once the subagent digests arrive\"",
	})

	err := runCheckpointSummaryCLI(context.Background(), tmp, "claude", "prompt", models.Config{})
	if err == nil {
		t.Fatal("expected report missing error, got nil")
	}
	for _, want := range []string{"report missing", "once the subagent digests arrive"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err.Error(), want)
		}
	}
}

// A stopping supervisor kills the CLI instead of leaving it to run on.
func TestRunCheckpointSummaryCLI_ParentCancellationStopsTheCLI(t *testing.T) {
	tmp := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmp)
	installFakeCLI(t, "claude", []string{"exec sleep 30"})

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)
	start := time.Now()
	err := runCheckpointSummaryCLI(ctx, tmp, "claude", "prompt", models.Config{})
	if err == nil || !strings.Contains(err.Error(), "cancelled: supervisor stopping") {
		t.Fatalf("error = %v, want a cancellation error", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("returned after %s; the CLI outlived the cancellation", elapsed)
	}
}
