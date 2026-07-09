package functionalclusters

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/scipsearch"
	"github.com/liza-mas/liza/internal/stacklit"
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

func TestRefreshIndexBuildsFunctionalClustersAfterExports(t *testing.T) {
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	writeFile(t, filepath.Join(projectRoot, "go.mod"), "module example.com/project\n")
	testhelpers.MustGit(t, projectRoot, "add", "go.mod")
	testhelpers.MustGit(t, projectRoot, "commit", "-m", "Add go module")
	writeFile(t, filepath.Join(projectRoot, "stacklit.json"), "{}\n")
	writeFile(t, filepath.Join(projectRoot, paths.ProjectDirName(), "scip", "go.scip"), "go index\n")
	t.Setenv(EnvEnableFunctionalClusters, "true")
	t.Setenv(stacklit.EnvEnableStacklit, "true")
	t.Setenv(scipsearch.EnvEnableScipSearch, "true")

	var calls []string
	result, err := RefreshIndex(RefreshOptions{
		TargetRoot:          projectRoot,
		TargetKind:          TargetKindProjectRoot,
		ConfiguredLanguages: []string{"go"},
		Runner: func(plan RuntimeCommandPlan) (string, error) {
			calls = append(calls, plan.Name+" "+plan.Args[0])
			switch {
			case plan.Name == "stacklit" && len(plan.Args) >= 1 && plan.Args[0] == "export-architecture":
				if err := os.WriteFile(plan.OutputPath, []byte("architecture\n"), 0o644); err != nil {
					return "", err
				}
			case plan.Name == "scip-search" && len(plan.Args) >= 1 && plan.Args[0] == "graph-export":
				if err := os.WriteFile(plan.OutputPath, []byte("graph\n"), 0o644); err != nil {
					return "", err
				}
			case plan.Name == "functional-clusters" && len(plan.Args) >= 1 && plan.Args[0] == "build":
				if err := os.WriteFile(plan.OutputPath, []byte("clusters\n"), 0o644); err != nil {
					return "", err
				}
			default:
				return "", errors.New("unexpected command: " + plan.Name + " " + strings.Join(plan.Args, " "))
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
	wantCalls := []string{"stacklit export-architecture", "scip-search graph-export", "functional-clusters build"}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", calls, wantCalls)
	}
	wantPath := filepath.Join(projectRoot, "functional-clusters.json")
	if result.Successes[0].Path != wantPath {
		t.Fatalf("success path = %q, want %q", result.Successes[0].Path, wantPath)
	}
	if got := string(readFile(t, wantPath)); got != "clusters\n" {
		t.Fatalf("functional-clusters.json = %q, want generated clusters", got)
	}

	available, err := AvailableIndexes(RuntimePlanOptions{TargetRoot: projectRoot})
	if err != nil {
		t.Fatalf("AvailableIndexes() error = %v", err)
	}
	if len(available) != 1 || available[0].Path != wantPath {
		t.Fatalf("AvailableIndexes() = %#v, want %s", available, wantPath)
	}
}

func TestRefreshIndexTaskWorktreeGeneratedArtifactIsPromptLocalAndClean(t *testing.T) {
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	testhelpers.MustGit(t, projectRoot, "checkout", "integration")
	writeFile(t, filepath.Join(projectRoot, ".gitignore"), "stacklit.json\n"+paths.ProjectDirName()+"/scip/\n")
	writeFile(t, filepath.Join(projectRoot, "go.mod"), "module example.com/project\n")
	testhelpers.MustGit(t, projectRoot, "add", ".gitignore", "go.mod")
	testhelpers.MustGit(t, projectRoot, "commit", "-m", "Add indexed Go project")
	testhelpers.CreateTestWorktree(t, projectRoot, "task-1")
	worktreeRoot := filepath.Join(projectRoot, ".worktrees", "task-1")
	writeFile(t, filepath.Join(worktreeRoot, "stacklit.json"), "{}\n")
	writeFile(t, filepath.Join(worktreeRoot, paths.ProjectDirName(), "scip", "go.scip"), "go index\n")
	t.Setenv(EnvEnableFunctionalClusters, "true")
	t.Setenv(stacklit.EnvEnableStacklit, "true")
	t.Setenv(scipsearch.EnvEnableScipSearch, "true")

	result, err := RefreshIndex(RefreshOptions{
		TargetRoot:          worktreeRoot,
		TargetKind:          TargetKindTaskWorktree,
		ConfiguredLanguages: []string{"go"},
		Runner: func(plan RuntimeCommandPlan) (string, error) {
			if err := os.WriteFile(plan.OutputPath, []byte(plan.Name+"\n"), 0o644); err != nil {
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
	if status := gitOutput(t, worktreeRoot, "status", "--porcelain"); status != "" {
		t.Fatalf("git status --porcelain = %q, want clean", status)
	}
	if ignored := gitOutput(t, worktreeRoot, "check-ignore", "functional-clusters.json"); ignored != "functional-clusters.json" {
		t.Fatalf("git check-ignore functional-clusters.json = %q, want worktree-private exclude", ignored)
	}
}

func TestRunRuntimeCommandPlanTimesOutHungFunctionalClusters(t *testing.T) {
	testhelpers.WithShortSubprocessTimeout(t, 50*time.Millisecond)
	testhelpers.AddSlowCommandToPath(t, "functional-clusters")

	start := time.Now()
	output, err := runRuntimeCommandPlan(RuntimeCommandPlan{
		Name: "functional-clusters",
		Args: []string{"build"},
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
	if elapsed >= time.Second {
		t.Fatalf("runRuntimeCommandPlan() elapsed = %s, want under 1s", elapsed)
	}
	if !strings.Contains(output, "started") || strings.Contains(output, "late") {
		t.Fatalf("output = %q, want pre-timeout output only", output)
	}
}

func TestRefreshIndexDisabledNoop(t *testing.T) {
	t.Setenv(EnvEnableFunctionalClusters, "false")
	t.Setenv(stacklit.EnvEnableStacklit, "true")
	t.Setenv(scipsearch.EnvEnableScipSearch, "true")
	called := false
	result, err := RefreshIndex(RefreshOptions{
		TargetRoot:          t.TempDir(),
		TargetKind:          TargetKindProjectRoot,
		ConfiguredLanguages: []string{"go"},
		Runner: func(RuntimeCommandPlan) (string, error) {
			called = true
			return "", nil
		},
	})
	if err != nil {
		t.Fatalf("RefreshIndex() error = %v", err)
	}
	if called {
		t.Fatal("runner called despite disabled Functional Clusters gate")
	}
	if len(result.Successes) != 0 || len(result.Failures) != 0 {
		t.Fatalf("RefreshIndex() = %#v, want empty result", result)
	}
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
