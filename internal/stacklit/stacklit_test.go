package stacklit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/subprocess"
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

func TestRefreshIndexTaskWorktreeTrackedStacklitJSONIsPromptLocalAndClean(t *testing.T) {
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	commitTrackedStacklitJSON(t, projectRoot, "committed index\n")
	testhelpers.CreateTestWorktree(t, projectRoot, "task-1")
	worktreeRoot := filepath.Join(projectRoot, ".worktrees", "task-1")
	t.Setenv(EnvEnableStacklit, "true")

	result, err := RefreshIndex(RefreshOptions{
		TargetRoot: worktreeRoot,
		Runner: func(plan RuntimeCommandPlan) (string, error) {
			assertStacklitPlan(t, plan, worktreeRoot)
			if err := os.WriteFile(plan.OutputPath, []byte("task-local index\n"), 0o644); err != nil {
				return "", err
			}
			return "", nil
		},
	})
	if err != nil {
		t.Fatalf("RefreshIndex() error = %v", err)
	}
	if len(result.Failures) != 0 || len(result.Successes) != 1 {
		t.Fatalf("RefreshIndex() = %#v, want one success and no failures", result)
	}
	if got := string(readFile(t, filepath.Join(worktreeRoot, "stacklit.json"))); got != "task-local index\n" {
		t.Fatalf("stacklit.json content = %q, want task-local index", got)
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

func TestRefreshIndexTaskWorktreeIgnoredStacklitJSONIsPromptLocalAndClean(t *testing.T) {
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	commitIgnoredStacklitJSON(t, projectRoot)
	testhelpers.CreateTestWorktree(t, projectRoot, "task-1")
	worktreeRoot := filepath.Join(projectRoot, ".worktrees", "task-1")
	t.Setenv(EnvEnableStacklit, "true")

	result, err := RefreshIndex(RefreshOptions{
		TargetRoot: worktreeRoot,
		Runner: func(plan RuntimeCommandPlan) (string, error) {
			assertStacklitPlan(t, plan, worktreeRoot)
			if err := os.WriteFile(plan.OutputPath, []byte("task-local index\n"), 0o644); err != nil {
				return "", err
			}
			return "", nil
		},
	})
	if err != nil {
		t.Fatalf("RefreshIndex() error = %v", err)
	}
	if len(result.Failures) != 0 || len(result.Successes) != 1 {
		t.Fatalf("RefreshIndex() = %#v, want one success and no failures", result)
	}
	if got := string(readFile(t, filepath.Join(worktreeRoot, "stacklit.json"))); got != "task-local index\n" {
		t.Fatalf("stacklit.json content = %q, want task-local index", got)
	}
	if status := gitOutput(t, worktreeRoot, "status", "--porcelain"); status != "" {
		t.Fatalf("git status --porcelain = %q, want clean", status)
	}
	if ignored := gitOutput(t, worktreeRoot, "check-ignore", "stacklit.json"); ignored != "stacklit.json" {
		t.Fatalf("git check-ignore stacklit.json = %q, want ignored stacklit.json", ignored)
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

func TestRefreshIndexTaskWorktreeFailureRemovesPromptLocalIndex(t *testing.T) {
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	commitTrackedStacklitJSON(t, projectRoot, "committed index\n")
	testhelpers.CreateTestWorktree(t, projectRoot, "task-1")
	worktreeRoot := filepath.Join(projectRoot, ".worktrees", "task-1")
	t.Setenv(EnvEnableStacklit, "true")

	result, err := RefreshIndex(RefreshOptions{
		TargetRoot: worktreeRoot,
		Runner: func(RuntimeCommandPlan) (string, error) {
			return "stacklit stderr", errors.New("stacklit failed")
		},
	})
	if err != nil {
		t.Fatalf("RefreshIndex() error = %v", err)
	}
	if len(result.Successes) != 0 || len(result.Failures) != 1 {
		t.Fatalf("RefreshIndex() = %#v, want one failure and no successes", result)
	}
	if _, err := os.Stat(filepath.Join(worktreeRoot, "stacklit.json")); !os.IsNotExist(err) {
		t.Fatalf("stacklit.json stat error = %v, want removed after failed task refresh", err)
	}
	if status := gitOutput(t, worktreeRoot, "status", "--porcelain"); status != "" {
		t.Fatalf("git status --porcelain = %q, want clean", status)
	}
	if available, err := AvailableIndexes(RuntimePlanOptions{TargetRoot: worktreeRoot}); err != nil {
		t.Fatalf("AvailableIndexes() error = %v", err)
	} else if len(available) != 0 {
		t.Fatalf("AvailableIndexes() = %#v, want none after failed refresh", available)
	}
}

func TestRunRuntimeCommandPlanTimesOutHungStacklit(t *testing.T) {
	const timeout = 5 * time.Second
	testhelpers.WithShortSubprocessTimeout(t, timeout)
	testhelpers.AddSlowCommandToPathWithDelay(t, "stacklit", 15*time.Second)

	start := time.Now()
	output, err := runRuntimeCommandPlan(RuntimeCommandPlan{
		Name: "stacklit",
		Args: []string{"generate-json", "-o", "stacklit.json"},
		Dir:  t.TempDir(),
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("runRuntimeCommandPlan() error = nil, want timeout")
	}
	var timeoutErr *subprocess.TimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("runRuntimeCommandPlan() error = %T %v, want *subprocess.TimeoutError", err, err)
	}
	if elapsed >= timeout+2*time.Second {
		t.Fatalf("runRuntimeCommandPlan() elapsed = %s, want under %s", elapsed, timeout+2*time.Second)
	}
	if !strings.Contains(output, "started") || strings.Contains(output, "late") {
		t.Fatalf("output = %q, want pre-timeout output only", output)
	}
}

func TestRefreshIndexTaskWorktreeRejectsUntrackedUnignoredStacklitJSON(t *testing.T) {
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	testhelpers.CreateTestWorktree(t, projectRoot, "task-1")
	worktreeRoot := filepath.Join(projectRoot, ".worktrees", "task-1")
	t.Setenv(EnvEnableStacklit, "true")

	called := false
	result, err := RefreshIndex(RefreshOptions{
		TargetRoot: worktreeRoot,
		Runner: func(RuntimeCommandPlan) (string, error) {
			called = true
			return "", nil
		},
	})
	if err == nil {
		t.Fatal("RefreshIndex() error = nil, want stacklit.json git-state requirement")
	}
	if !strings.Contains(err.Error(), "neither tracked nor ignored") {
		t.Fatalf("RefreshIndex() error = %v, want tracked-or-ignored stacklit.json guidance", err)
	}
	if called {
		t.Fatal("runner called despite unsafe stacklit.json git state")
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
	called := false
	result, err := RefreshIndex(RefreshOptions{
		TargetRoot: t.TempDir(),
		Runner: func(RuntimeCommandPlan) (string, error) {
			called = true
			return "", nil
		},
	})
	if err != nil {
		t.Fatalf("RefreshIndex() error = %v", err)
	}
	if called {
		t.Fatal("runner called despite disabled Stacklit gate")
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

func assertStacklitPlan(t *testing.T, plan RuntimeCommandPlan, worktreeRoot string) {
	t.Helper()
	if plan.Name != "stacklit" {
		t.Fatalf("plan.Name = %q, want stacklit", plan.Name)
	}
	if strings.Join(plan.Args, " ") != "generate-json -o stacklit.json" {
		t.Fatalf("plan.Args = %v, want generate-json -o stacklit.json", plan.Args)
	}
	if plan.Dir != worktreeRoot {
		t.Fatalf("plan.Dir = %q, want %q", plan.Dir, worktreeRoot)
	}
	if plan.OutputPath != filepath.Join(worktreeRoot, "stacklit.json") {
		t.Fatalf("plan.OutputPath = %q, want worktree stacklit.json", plan.OutputPath)
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
