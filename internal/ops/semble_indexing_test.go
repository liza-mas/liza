package ops

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/semble"
	"github.com/liza-mas/liza/internal/testhelpers"
	"github.com/liza-mas/liza/internal/worktreeexclude"
)

func TestPrepareSembleWorktreeIgnoreWritesDefaultGeneratedPayload(t *testing.T) {
	t.Parallel()
	fixture := newPrepareSembleWorktreeIgnoreFixture(t)

	warnings := PrepareSembleWorktreeIgnore(fixture.worktree)

	assertNoPrepareSembleWorktreeIgnoreWarnings(t, warnings)
	assertPrepareSembleIgnorePayload(t, fixture.worktree)
	assertPrepareSemblePrivateExcludeCount(t, fixture.worktree, ".sembleignore", 1)
	assertGitStatusClean(t, fixture.worktree)
}

func TestPrepareSembleWorktreeIgnoreAppendsMissingGeneratedPatterns(t *testing.T) {
	t.Parallel()
	fixture := newPrepareSembleWorktreeIgnoreFixture(t)
	writePrepareSembleIgnoreLines(t, fixture.worktree, semble.DefaultIgnorePatterns()[:3])

	warnings := PrepareSembleWorktreeIgnore(fixture.worktree)

	assertNoPrepareSembleWorktreeIgnoreWarnings(t, warnings)
	assertPrepareSembleIgnorePayload(t, fixture.worktree)
	assertPrepareSemblePrivateExcludeCount(t, fixture.worktree, ".sembleignore", 1)
	assertGitStatusClean(t, fixture.worktree)
}

func TestPrepareSembleWorktreeIgnoreUsesSharedPrivateExcludeWithScip(t *testing.T) {
	t.Parallel()
	fixture := newPrepareSembleWorktreeIgnoreFixture(t)
	if err := worktreeexclude.EnsurePrivateExclude(fixture.worktree, paths.ProjectDirName()+"/scip/"); err != nil {
		t.Fatalf("EnsurePrivateExclude(%s/scip/) error = %v", paths.ProjectDirName(), err)
	}

	warnings := PrepareSembleWorktreeIgnore(fixture.worktree)

	assertNoPrepareSembleWorktreeIgnoreWarnings(t, warnings)
	privateExclude := prepareSemblePrivateExcludePath(t, fixture.worktree)
	if got := runGitInDir(t, fixture.worktree, "config", "--worktree", "--get", "core.excludesFile"); filepath.Clean(got) != filepath.Clean(privateExclude) {
		t.Fatalf("core.excludesFile = %q, want shared private exclude %q", got, privateExclude)
	}
	assertPrepareSemblePrivateExcludeCount(t, fixture.worktree, paths.ProjectDirName()+"/scip/", 1)
	assertPrepareSemblePrivateExcludeCount(t, fixture.worktree, ".sembleignore", 1)
	assertGitStatusClean(t, fixture.worktree)
}

func TestPrepareSembleWorktreeIgnoreRepeatedCallsIdempotent(t *testing.T) {
	t.Parallel()
	fixture := newPrepareSembleWorktreeIgnoreFixture(t)

	for i := 0; i < 3; i++ {
		warnings := PrepareSembleWorktreeIgnore(fixture.worktree)
		assertNoPrepareSembleWorktreeIgnoreWarnings(t, warnings)
	}

	assertPrepareSembleIgnorePayload(t, fixture.worktree)
	assertPrepareSemblePrivateExcludeCount(t, fixture.worktree, ".sembleignore", 1)
	assertGitStatusClean(t, fixture.worktree)
}

func TestPrepareSembleWorktreeIgnoreConcurrentCallsIdempotent(t *testing.T) {
	t.Parallel()
	fixture := newPrepareSembleWorktreeIgnoreFixture(t)
	const workers = 16
	warnings := make(chan []string, workers)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			warnings <- PrepareSembleWorktreeIgnore(fixture.worktree)
		}()
	}
	wg.Wait()
	close(warnings)

	for got := range warnings {
		assertNoPrepareSembleWorktreeIgnoreWarnings(t, got)
	}
	assertPrepareSembleIgnorePayload(t, fixture.worktree)
	assertPrepareSemblePrivateExcludeCount(t, fixture.worktree, ".sembleignore", 1)
	assertGitStatusClean(t, fixture.worktree)
}

func TestPrepareSembleWorktreeIgnoreLeavesTrackedCompleteFileVisible(t *testing.T) {
	t.Parallel()
	fixture := newPrepareSembleWorktreeIgnoreFixture(t)
	writePrepareSembleIgnorePayload(t, fixture.worktree, semble.GeneratedWorktreeIgnorePayload())
	runGitInDir(t, fixture.worktree, "add", ".sembleignore")
	runGitInDir(t, fixture.worktree, "commit", "-m", "track complete semble ignore")
	before := readPrepareSembleIgnoreFile(t, fixture.worktree)

	warnings := PrepareSembleWorktreeIgnore(fixture.worktree)

	assertNoPrepareSembleWorktreeIgnoreWarnings(t, warnings)
	if got := readPrepareSembleIgnoreFile(t, fixture.worktree); got != before {
		t.Fatalf("tracked complete .sembleignore mutated: got %q, want %q", got, before)
	}
	assertPrepareSemblePrivateExcludeCount(t, fixture.worktree, ".sembleignore", 0)
	assertGitStatusClean(t, fixture.worktree)
}

func TestPrepareSembleWorktreeIgnoreReportsTrackedIncompleteWithoutMutation(t *testing.T) {
	t.Parallel()
	fixture := newPrepareSembleWorktreeIgnoreFixture(t)
	before := "operator-owned marker\n" + paths.ProjectDirName() + "/\n"
	writePrepareSembleIgnorePayload(t, fixture.worktree, before)
	runGitInDir(t, fixture.worktree, "add", ".sembleignore")
	runGitInDir(t, fixture.worktree, "commit", "-m", "track incomplete semble ignore")

	warnings := PrepareSembleWorktreeIgnore(fixture.worktree)

	if len(warnings) != 1 {
		t.Fatalf("PrepareSembleWorktreeIgnore() warnings = %#v, want exactly one", warnings)
	}
	warning := warnings[0]
	for _, want := range []string{"tracked .sembleignore", "missing required patterns"} {
		if !strings.Contains(warning, want) {
			t.Fatalf("warning = %q, want to contain %q", warning, want)
		}
	}
	if strings.Contains(warning, "operator-owned marker") {
		t.Fatalf("warning includes file contents: %q", warning)
	}
	if len(warning) > 512 {
		t.Fatalf("warning length = %d, want bounded <= 512", len(warning))
	}
	if got := readPrepareSembleIgnoreFile(t, fixture.worktree); got != before {
		t.Fatalf("tracked incomplete .sembleignore mutated: got %q, want %q", got, before)
	}
	assertPrepareSemblePrivateExcludeCount(t, fixture.worktree, ".sembleignore", 0)
	assertGitStatusClean(t, fixture.worktree)
}

func TestPrepareSembleWorktreeIgnoreWarningPreventsPromptMetadata(t *testing.T) {
	t.Setenv(semble.EnvEnableSemble, "true")
	fixture := newPrepareSembleWorktreeIgnoreFixture(t)
	writePrepareSembleIgnorePayload(t, fixture.worktree, "operator-owned marker\n"+paths.ProjectDirName()+"/\n")
	runGitInDir(t, fixture.worktree, "add", ".sembleignore")
	runGitInDir(t, fixture.worktree, "commit", "-m", "track incomplete semble ignore")

	warnings := PrepareSembleWorktreeIgnore(fixture.worktree)
	if len(warnings) != 1 {
		t.Fatalf("PrepareSembleWorktreeIgnore() warnings = %#v, want exactly one", warnings)
	}
	metadata, ok := semble.BuildPromptMetadata(semble.PromptMetadataOptions{
		Kind:                 semble.TargetKindTaskWorktree,
		TargetRoot:           fixture.worktree,
		ExpectedWorktreeRoot: fixture.worktree,
		LookPath: func(string) (string, error) {
			return filepath.Join(t.TempDir(), "semble"), nil
		},
		Runner: func(semble.CommandPlan) (semble.CommandResult, error) {
			t.Fatal("runner called after unsafe task-root .sembleignore preparation warning")
			return semble.CommandResult{}, nil
		},
	})
	if ok {
		t.Fatalf("BuildPromptMetadata() after preparation warning = %#v, true; want omitted", metadata)
	}
}

func TestPrepareSembleWorktreeIgnoreConflictingPrivateExcludeDoesNotWriteGeneratedFile(t *testing.T) {
	t.Parallel()
	fixture := newPrepareSembleWorktreeIgnoreFixture(t)
	conflictingExclude := filepath.Join(t.TempDir(), "operator-exclude")
	runGitInDir(t, fixture.worktree, "config", "core.excludesFile", conflictingExclude)

	warnings := PrepareSembleWorktreeIgnore(fixture.worktree)

	if len(warnings) != 1 {
		t.Fatalf("PrepareSembleWorktreeIgnore() warnings = %#v, want exactly one", warnings)
	}
	warning := warnings[0]
	for _, want := range []string{"ensure private exclude", "core.excludesFile"} {
		if !strings.Contains(warning, want) {
			t.Fatalf("warning = %q, want to contain %q", warning, want)
		}
	}
	if _, err := os.Stat(filepath.Join(fixture.worktree, ".sembleignore")); !os.IsNotExist(err) {
		t.Fatalf("generated .sembleignore stat error = %v, want not exist", err)
	}
	assertPrepareSemblePrivateExcludeCount(t, fixture.worktree, ".sembleignore", 0)
	assertGitStatusClean(t, fixture.worktree)
}

type prepareSembleWorktreeIgnoreFixture struct {
	worktree string
}

func newPrepareSembleWorktreeIgnoreFixture(t *testing.T) prepareSembleWorktreeIgnoreFixture {
	t.Helper()

	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	worktree := filepath.Join(root, ".worktrees", "task-1")
	if err := os.MkdirAll(filepath.Dir(worktree), 0o755); err != nil {
		t.Fatalf("create worktrees dir: %v", err)
	}
	runGitInDir(t, root, "worktree", "add", "-b", "task-1", worktree, "integration")

	return prepareSembleWorktreeIgnoreFixture{
		worktree: worktree,
	}
}

func assertNoPrepareSembleWorktreeIgnoreWarnings(t *testing.T, warnings []string) {
	t.Helper()
	if len(warnings) != 0 {
		t.Fatalf("PrepareSembleWorktreeIgnore() warnings = %#v, want none", warnings)
	}
}

func assertPrepareSembleIgnorePayload(t *testing.T, worktree string) {
	t.Helper()
	got := prepareSembleNonEmptyLines(readPrepareSembleIgnoreFile(t, worktree))
	if want := semble.DefaultIgnorePatterns(); !reflect.DeepEqual(got, want) {
		t.Fatalf(".sembleignore non-empty lines = %#v, want %#v", got, want)
	}
}

func assertPrepareSemblePrivateExcludeCount(t *testing.T, worktree, entry string, want int) {
	t.Helper()
	content, err := os.ReadFile(prepareSemblePrivateExcludePath(t, worktree))
	if err != nil {
		if os.IsNotExist(err) && want == 0 {
			return
		}
		t.Fatalf("read private exclude: %v", err)
	}
	got := 0
	for _, line := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(line) == entry {
			got++
		}
	}
	if got != want {
		t.Fatalf("private exclude entry %q count = %d, want %d in:\n%s", entry, got, want, content)
	}
}

func prepareSemblePrivateExcludePath(t *testing.T, worktree string) string {
	t.Helper()
	gitDir := runGitInDir(t, worktree, "rev-parse", "--git-dir")
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(worktree, gitDir)
	}
	return filepath.Join(filepath.Clean(gitDir), "info", "exclude")
}

func writePrepareSembleIgnoreLines(t *testing.T, worktree string, lines []string) {
	t.Helper()
	writePrepareSembleIgnorePayload(t, worktree, strings.Join(lines, "\n")+"\n")
}

func writePrepareSembleIgnorePayload(t *testing.T, worktree, payload string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(worktree, ".sembleignore"), []byte(payload), 0o644); err != nil {
		t.Fatalf("write .sembleignore: %v", err)
	}
}

func readPrepareSembleIgnoreFile(t *testing.T, worktree string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(worktree, ".sembleignore"))
	if err != nil {
		t.Fatalf("read .sembleignore: %v", err)
	}
	return string(content)
}

func prepareSembleNonEmptyLines(content string) []string {
	lines := make([]string, 0)
	for _, line := range strings.Split(content, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	return lines
}
