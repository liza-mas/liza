package functionalclusters

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/scipsearch"
	"github.com/liza-mas/liza/internal/stacklit"
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
			t.Setenv(EnvEnableFunctionalClusters, tt.value)
			if got := RuntimeEnabled(); got != tt.want {
				t.Fatalf("RuntimeEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRefreshEnabledRequiresFunctionalClustersStacklitAndScip(t *testing.T) {
	tests := []struct {
		name       string
		functional string
		stacklit   string
		scip       string
		languages  []string
		want       bool
	}{
		{name: "all enabled", functional: "true", stacklit: "true", scip: "true", languages: []string{"go"}, want: true},
		{name: "functional disabled", functional: "false", stacklit: "true", scip: "true", languages: []string{"go"}},
		{name: "stacklit disabled", functional: "true", stacklit: "false", scip: "true", languages: []string{"go"}},
		{name: "scip disabled", functional: "true", stacklit: "true", scip: "false", languages: []string{"go"}},
		{name: "no configured languages", functional: "true", stacklit: "true", scip: "true"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(EnvEnableFunctionalClusters, tt.functional)
			t.Setenv(stacklit.EnvEnableStacklit, tt.stacklit)
			t.Setenv(scipsearch.EnvEnableScipSearch, tt.scip)
			if got := RefreshEnabled(tt.languages); got != tt.want {
				t.Fatalf("RefreshEnabled(%v) = %v, want %v", tt.languages, got, tt.want)
			}
		})
	}
}

func TestRefreshIndexTaskWorktreeCopiesRepoRootArtifactAndStaysClean(t *testing.T) {
	projectRoot, worktreeRoot := newIndexedWorktree(t)
	writeFile(t, filepath.Join(projectRoot, "functional-clusters.json"), "repo-root clusters\n")
	enableFunctionalClusters(t)

	result, err := RefreshIndex(RefreshOptions{
		ProjectRoot:         projectRoot,
		TargetRoot:          worktreeRoot,
		ConfiguredLanguages: []string{"go"},
	})
	if err != nil {
		t.Fatalf("RefreshIndex() error = %v", err)
	}
	wantPath := filepath.Join(worktreeRoot, "functional-clusters.json")
	if len(result.Failures) != 0 || len(result.Successes) != 1 || result.Successes[0].Path != wantPath {
		t.Fatalf("RefreshIndex() = %#v, want one success at %s", result, wantPath)
	}
	if got := string(readFile(t, wantPath)); got != "repo-root clusters\n" {
		t.Fatalf("functional-clusters.json = %q, want the repo-root artifact", got)
	}
	if status := gitOutput(t, worktreeRoot, "status", "--porcelain"); status != "" {
		t.Fatalf("git status --porcelain = %q, want clean", status)
	}
	if ignored := gitOutput(t, worktreeRoot, "check-ignore", "functional-clusters.json"); ignored != "functional-clusters.json" {
		t.Fatalf("git check-ignore functional-clusters.json = %q, want worktree-private exclude", ignored)
	}

	available, err := AvailableIndexes(RuntimePlanOptions{TargetRoot: worktreeRoot})
	if err != nil {
		t.Fatalf("AvailableIndexes() error = %v", err)
	}
	if len(available) != 1 || available[0].Path != wantPath {
		t.Fatalf("AvailableIndexes() = %#v, want %s", available, wantPath)
	}
}

func TestD47RefreshClustersPreservesUserExcludes(t *testing.T) {
	projectRoot, worktreeRoot := newIndexedWorktree(t)
	enableFunctionalClusters(t)
	userExclude := filepath.Join(t.TempDir(), "user-exclude")
	writeFile(t, userExclude, "user-owned/\n")
	testhelpers.MustGit(t, worktreeRoot, "config", "core.excludesFile", userExclude)
	writeFile(t, filepath.Join(projectRoot, "functional-clusters.json"), "repo-root clusters\n")

	result, err := RefreshIndex(RefreshOptions{
		ProjectRoot: projectRoot, TargetRoot: worktreeRoot, ConfiguredLanguages: []string{"go"},
	})
	if err != nil || len(result.Successes) != 1 || len(result.Failures) != 0 {
		t.Fatalf("RefreshIndex = %#v, %v, want successful copy", result, err)
	}
	if got := string(readFile(t, filepath.Join(worktreeRoot, "functional-clusters.json"))); got != "repo-root clusters\n" {
		t.Fatalf("worktree artifact = %q", got)
	}
	if got := gitOutput(t, worktreeRoot, "config", "--get", "core.excludesFile"); got != userExclude {
		t.Fatalf("user config changed: %q", got)
	}
	if got := string(readFile(t, userExclude)); got != "user-owned/\n" {
		t.Fatalf("user exclude changed: %q", got)
	}
	testhelpers.MustGit(t, worktreeRoot, "check-ignore", "-q", "user-owned/data")
	testhelpers.MustGit(t, worktreeRoot, "check-ignore", "-q", "functional-clusters.json")
	if got := gitOutput(t, worktreeRoot, "status", "--porcelain"); got != "" {
		t.Fatalf("generated artifact dirtied worktree: %q", got)
	}
}

func TestRefreshIndexTaskWorktreeMissingRepoRootArtifactRemovesStaleCopy(t *testing.T) {
	projectRoot, worktreeRoot := newIndexedWorktree(t)
	stalePath := filepath.Join(worktreeRoot, "functional-clusters.json")
	writeFile(t, stalePath, "stale copy\n")
	enableFunctionalClusters(t)

	result, err := RefreshIndex(RefreshOptions{
		ProjectRoot:         projectRoot,
		TargetRoot:          worktreeRoot,
		ConfiguredLanguages: []string{"go"},
	})
	if err != nil {
		t.Fatalf("RefreshIndex() error = %v", err)
	}
	if len(result.Successes) != 0 || len(result.Failures) != 1 || !strings.Contains(result.Failures[0].Diagnostic, "not found") {
		t.Fatalf("RefreshIndex() = %#v, want one missing-artifact failure", result)
	}
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Fatalf("functional-clusters.json stat error = %v, want stale copy removed", err)
	}
	if status := gitOutput(t, worktreeRoot, "status", "--porcelain"); status != "" {
		t.Fatalf("git status --porcelain = %q, want clean", status)
	}
}

func TestRefreshIndexDisabledNoop(t *testing.T) {
	t.Setenv(EnvEnableFunctionalClusters, "false")
	t.Setenv(stacklit.EnvEnableStacklit, "true")
	t.Setenv(scipsearch.EnvEnableScipSearch, "true")
	projectRoot := t.TempDir()
	targetRoot := t.TempDir()
	writeFile(t, filepath.Join(projectRoot, "functional-clusters.json"), "repo-root clusters\n")

	result, err := RefreshIndex(RefreshOptions{
		ProjectRoot:         projectRoot,
		TargetRoot:          targetRoot,
		ConfiguredLanguages: []string{"go"},
	})
	if err != nil {
		t.Fatalf("RefreshIndex() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(targetRoot, "functional-clusters.json")); !os.IsNotExist(err) {
		t.Fatalf("functional-clusters.json stat error = %v, want no copy despite disabled gate", err)
	}
	if len(result.Successes) != 0 || len(result.Failures) != 0 {
		t.Fatalf("RefreshIndex() = %#v, want empty result", result)
	}
}

func newIndexedWorktree(t *testing.T) (projectRoot, worktreeRoot string) {
	t.Helper()
	projectRoot = t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	testhelpers.MustGit(t, projectRoot, "checkout", "integration")
	writeFile(t, filepath.Join(projectRoot, "go.mod"), "module example.com/project\n")
	testhelpers.MustGit(t, projectRoot, "add", "go.mod")
	testhelpers.MustGit(t, projectRoot, "commit", "-m", "Add indexed Go project")
	testhelpers.CreateTestWorktree(t, projectRoot, "task-1")
	return projectRoot, filepath.Join(projectRoot, ".worktrees", "task-1")
}

func enableFunctionalClusters(t *testing.T) {
	t.Helper()
	t.Setenv(EnvEnableFunctionalClusters, "true")
	t.Setenv(stacklit.EnvEnableStacklit, "true")
	t.Setenv(scipsearch.EnvEnableScipSearch, "true")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", path, err)
	}
	return content
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}
