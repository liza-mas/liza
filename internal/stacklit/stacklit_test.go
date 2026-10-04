package stacklit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestRuntimeEnabledParsesEnvGate(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "true", value: "true", want: true},
		{name: "one", value: "1", want: true},
		{name: "case and space", value: " TRUE ", want: true},
		{name: "false", value: "false"},
		{name: "zero", value: "0"},
		{name: "empty", value: ""},
		{name: "unexpected", value: "yes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(EnvEnableStacklit, tt.value)
			if got := RuntimeEnabled(); got != tt.want {
				t.Fatalf("RuntimeEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRefreshIndexTaskWorktreeTrackedStacklitJSONCopiesRepoRootIndex(t *testing.T) {
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	commitTrackedStacklitJSON(t, projectRoot, "committed index\n")
	testhelpers.CreateTestWorktree(t, projectRoot, "task-1")
	worktreeRoot := filepath.Join(projectRoot, ".worktrees", "task-1")
	writeRepoRootIndex(t, projectRoot, "repo-root index\n")
	t.Setenv(EnvEnableStacklit, "true")

	result, err := RefreshIndex(RefreshOptions{ProjectRoot: projectRoot, TargetRoot: worktreeRoot})
	if err != nil {
		t.Fatalf("RefreshIndex() error = %v", err)
	}
	if len(result.Failures) != 0 || len(result.Successes) != 1 {
		t.Fatalf("RefreshIndex() = %#v, want one success and no failures", result)
	}
	if got := string(readFile(t, filepath.Join(worktreeRoot, "stacklit.json"))); got != "repo-root index\n" {
		t.Fatalf("stacklit.json content = %q, want the repo-root index", got)
	}
	if status := gitOutput(t, worktreeRoot, "status", "--porcelain"); status != "" {
		t.Fatalf("git status --porcelain = %q, want clean", status)
	}
	if ls := gitOutput(t, worktreeRoot, "ls-files", "-v", "stacklit.json"); !strings.HasPrefix(ls, "S ") {
		t.Fatalf("git ls-files -v stacklit.json = %q, want skip-worktree marker", ls)
	}

	available, err := AvailableIndexes(RuntimePlanOptions{TargetRoot: worktreeRoot})
	if err != nil {
		t.Fatalf("AvailableIndexes() error = %v", err)
	}
	wantPath := filepath.Join(worktreeRoot, "stacklit.json")
	if len(available) != 1 || available[0].Path != wantPath {
		t.Fatalf("AvailableIndexes() = %#v, want %s", available, wantPath)
	}
}

func TestRefreshIndexTaskWorktreeIgnoredStacklitJSONCopiesRepoRootIndex(t *testing.T) {
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	commitIgnoredStacklitJSON(t, projectRoot)
	testhelpers.CreateTestWorktree(t, projectRoot, "task-1")
	worktreeRoot := filepath.Join(projectRoot, ".worktrees", "task-1")
	writeRepoRootIndex(t, projectRoot, "repo-root index\n")
	t.Setenv(EnvEnableStacklit, "true")

	result, err := RefreshIndex(RefreshOptions{ProjectRoot: projectRoot, TargetRoot: worktreeRoot})
	if err != nil {
		t.Fatalf("RefreshIndex() error = %v", err)
	}
	if len(result.Failures) != 0 || len(result.Successes) != 1 {
		t.Fatalf("RefreshIndex() = %#v, want one success and no failures", result)
	}
	if got := string(readFile(t, filepath.Join(worktreeRoot, "stacklit.json"))); got != "repo-root index\n" {
		t.Fatalf("stacklit.json content = %q, want the repo-root index", got)
	}
	if status := gitOutput(t, worktreeRoot, "status", "--porcelain"); status != "" {
		t.Fatalf("git status --porcelain = %q, want clean", status)
	}
	if ignored := gitOutput(t, worktreeRoot, "check-ignore", "stacklit.json"); ignored != "stacklit.json" {
		t.Fatalf("git check-ignore stacklit.json = %q, want ignored stacklit.json", ignored)
	}
}

func TestRefreshIndexTaskWorktreeMissingRepoRootIndexRemovesStaleCopy(t *testing.T) {
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	commitIgnoredStacklitJSON(t, projectRoot)
	testhelpers.CreateTestWorktree(t, projectRoot, "task-1")
	worktreeRoot := filepath.Join(projectRoot, ".worktrees", "task-1")
	writeRepoRootIndex(t, worktreeRoot, "stale copy\n")
	t.Setenv(EnvEnableStacklit, "true")

	result, err := RefreshIndex(RefreshOptions{ProjectRoot: projectRoot, TargetRoot: worktreeRoot})
	if err != nil {
		t.Fatalf("RefreshIndex() error = %v", err)
	}
	if len(result.Successes) != 0 || len(result.Failures) != 1 {
		t.Fatalf("RefreshIndex() = %#v, want one failure and no successes", result)
	}
	if !strings.Contains(result.Failures[0].Diagnostic, "not found") {
		t.Fatalf("failure diagnostic = %q, want missing repo-root index", result.Failures[0].Diagnostic)
	}
	if _, err := os.Stat(filepath.Join(worktreeRoot, "stacklit.json")); !os.IsNotExist(err) {
		t.Fatalf("stacklit.json stat error = %v, want stale copy removed", err)
	}
	if status := gitOutput(t, worktreeRoot, "status", "--porcelain"); status != "" {
		t.Fatalf("git status --porcelain = %q, want clean", status)
	}
	if available, err := AvailableIndexes(RuntimePlanOptions{TargetRoot: worktreeRoot}); err != nil {
		t.Fatalf("AvailableIndexes() error = %v", err)
	} else if len(available) != 0 {
		t.Fatalf("AvailableIndexes() = %#v, want none without a repo-root index", available)
	}
}

func TestRefreshIndexTaskWorktreeRejectsUntrackedUnignoredStacklitJSON(t *testing.T) {
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	testhelpers.CreateTestWorktree(t, projectRoot, "task-1")
	worktreeRoot := filepath.Join(projectRoot, ".worktrees", "task-1")
	writeRepoRootIndex(t, projectRoot, "repo-root index\n")
	t.Setenv(EnvEnableStacklit, "true")

	result, err := RefreshIndex(RefreshOptions{ProjectRoot: projectRoot, TargetRoot: worktreeRoot})
	if err == nil {
		t.Fatal("RefreshIndex() error = nil, want stacklit.json git-state requirement")
	}
	if !strings.Contains(err.Error(), "neither tracked nor ignored") {
		t.Fatalf("RefreshIndex() error = %v, want tracked-or-ignored stacklit.json guidance", err)
	}
	if _, err := os.Stat(filepath.Join(worktreeRoot, "stacklit.json")); !os.IsNotExist(err) {
		t.Fatalf("stacklit.json stat error = %v, want no copy despite unsafe git state", err)
	}
	if len(result.Successes) != 0 || len(result.Failures) != 0 {
		t.Fatalf("RefreshIndex() = %#v, want empty result on preparation error", result)
	}
	if status := gitOutput(t, worktreeRoot, "status", "--porcelain"); status != "" {
		t.Fatalf("git status --porcelain = %q, want clean", status)
	}
}

func TestRefreshIndexDisabledNoop(t *testing.T) {
	t.Setenv(EnvEnableStacklit, "false")
	projectRoot := t.TempDir()
	targetRoot := t.TempDir()
	writeRepoRootIndex(t, projectRoot, "repo-root index\n")

	result, err := RefreshIndex(RefreshOptions{ProjectRoot: projectRoot, TargetRoot: targetRoot})
	if err != nil {
		t.Fatalf("RefreshIndex() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(targetRoot, "stacklit.json")); !os.IsNotExist(err) {
		t.Fatalf("stacklit.json stat error = %v, want no copy despite disabled Stacklit gate", err)
	}
	if len(result.Successes) != 0 || len(result.Failures) != 0 {
		t.Fatalf("RefreshIndex() = %#v, want empty result", result)
	}
}

func commitTrackedStacklitJSON(t *testing.T, projectRoot, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(projectRoot, "stacklit.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(stacklit.json) error = %v", err)
	}
	testhelpers.MustGit(t, projectRoot, "add", "stacklit.json")
	testhelpers.MustGit(t, projectRoot, "commit", "-m", "Add stacklit index")
	testhelpers.MustGit(t, projectRoot, "branch", "-f", "integration", "HEAD")
}

func commitIgnoredStacklitJSON(t *testing.T, projectRoot string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(projectRoot, ".gitignore"), []byte("stacklit.json\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(.gitignore) error = %v", err)
	}
	testhelpers.MustGit(t, projectRoot, "add", ".gitignore")
	testhelpers.MustGit(t, projectRoot, "commit", "-m", "Ignore stacklit index")
	testhelpers.MustGit(t, projectRoot, "branch", "-f", "integration", "HEAD")
}

func writeRepoRootIndex(t *testing.T, root, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "stacklit.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(stacklit.json) error = %v", err)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return strings.TrimSpace(string(testhelpers.MustGit(t, dir, args...)))
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	return content
}
