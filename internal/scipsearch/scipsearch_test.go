package scipsearch

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/doc"
	"go/parser"
	"go/token"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestParseEnvGate(t *testing.T) {
	tests := map[string]bool{"": false, "0": false, " false ": false, "1": true, " TRUE ": true, "yes": false}
	for value, want := range tests {
		if got := ParseEnvGate(value); got != want {
			t.Fatalf("ParseEnvGate(%q) = %v, want %v", value, got, want)
		}
	}
}

func TestRuntimeEnabled(t *testing.T) {
	tests := []struct {
		name      string
		envSet    bool
		envValue  string
		languages []string
		want      bool
	}{
		{name: "env unset disables configured languages", languages: []string{"go"}},
		{name: "env empty disables configured languages", envSet: true, envValue: "", languages: []string{"go"}},
		{name: "env zero disables configured languages", envSet: true, envValue: "0", languages: []string{"go"}},
		{name: "env false disables configured languages", envSet: true, envValue: " false ", languages: []string{"go"}},
		{name: "truthy env disables absent config", envSet: true, envValue: "true"},
		{name: "truthy env disables empty config", envSet: true, envValue: "true", languages: []string{}},
		{name: "truthy env enables at least one configured language", envSet: true, envValue: " TRUE ", languages: []string{"go"}, want: true},
		{name: "truthy numeric env enables at least one configured language", envSet: true, envValue: "1", languages: []string{"python"}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envSet {
				t.Setenv(EnvEnableScipSearch, tt.envValue)
			} else {
				unsetEnvForTest(t, EnvEnableScipSearch)
			}

			if got := RuntimeEnabled(tt.languages); got != tt.want {
				t.Fatalf("RuntimeEnabled(%v) with env %q set=%v = %v, want %v", tt.languages, tt.envValue, tt.envSet, got, tt.want)
			}
		})
	}
}

func unsetEnvForTest(t *testing.T, key string) {
	t.Helper()

	previous, wasSet := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("Unsetenv(%q) error = %v", key, err)
	}
	t.Cleanup(func() {
		if wasSet {
			if err := os.Setenv(key, previous); err != nil {
				t.Fatalf("Setenv(%q) cleanup error = %v", key, err)
			}
			return
		}
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("Unsetenv(%q) cleanup error = %v", key, err)
		}
	})
}

func TestRuntimeActivationContractIsDocumented(t *testing.T) {
	fset := token.NewFileSet()
	files := make([]*ast.File, 0, 2)
	for _, path := range []string{"doc.go", "scipsearch.go"} {
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("ParseFile(%q) error = %v", path, err)
		}
		files = append(files, file)
	}

	pkg, err := doc.NewFromFiles(fset, files, "./internal/scipsearch")
	if err != nil {
		t.Fatalf("NewFromFiles() error = %v", err)
	}
	packageDoc := pkg.Doc
	if !strings.Contains(packageDoc, "RuntimeEnabled") || !strings.Contains(packageDoc, EnvEnableScipSearch) || !strings.Contains(packageDoc, "Config.ScipSearch") {
		t.Fatalf("package doc = %q, want runtime activation contract for later callers", packageDoc)
	}

	var runtimeEnabledDoc string
	for _, fn := range pkg.Funcs {
		if fn.Name == "RuntimeEnabled" {
			runtimeEnabledDoc = fn.Doc
			break
		}
	}
	if !strings.Contains(runtimeEnabledDoc, EnvEnableScipSearch) || !strings.Contains(runtimeEnabledDoc, "configured language") {
		t.Fatalf("RuntimeEnabled doc = %q, want env and configured-language contract", runtimeEnabledDoc)
	}
}

func TestResolveInitConfigVersionFailureIsNonFatal(t *testing.T) {
	var calls []string
	got, err := ResolveInitConfig(InitOptions{
		ProjectRoot:       t.TempDir(),
		ExplicitLanguages: []string{"go"},
		EnvValue:          "true",
		CommandRunner: runnerFunc(&calls, func(name, argString string) (string, error) {
			if name == "scip-search" && argString == "--version" {
				return "version unavailable\n", errors.New("boom")
			}
			return "ok\n", nil
		}),
	})
	if err != nil {
		t.Fatalf("ResolveInitConfig() error = %v", err)
	}
	if !reflect.DeepEqual(got.Languages, []string{"go"}) || !hasCall(calls, "scip-search --version") {
		t.Fatalf("Languages = %v calls = %v, want [go] and version probe", got.Languages, calls)
	}
}

func TestResolveInitConfigLanguageSelection(t *testing.T) {
	t.Run("unsupported explicit language fails", func(t *testing.T) {
		_, err := ResolveInitConfig(InitOptions{
			ProjectRoot:       t.TempDir(),
			ExplicitLanguages: []string{"go", "ruby"},
			EnvValue:          "true",
			CommandRunner:     runnerFunc(nil, nil),
		})
		if err == nil || !strings.Contains(err.Error(), "ruby") {
			t.Fatalf("error = %v, want unsupported ruby language", err)
		}
	})

	t.Run("explicit languages dedupe even when env is false", func(t *testing.T) {
		got, err := ResolveInitConfig(InitOptions{
			ProjectRoot:       t.TempDir(),
			ExplicitLanguages: []string{"typescript", "go", "typescript", "python", "go"},
			EnvValue:          "0",
			CommandRunner:     runnerFunc(nil, nil),
		})
		if err != nil {
			t.Fatalf("ResolveInitConfig() error = %v", err)
		}
		if want := []string{"go", "typescript", "python"}; !reflect.DeepEqual(got.Languages, want) {
			t.Fatalf("Languages = %v, want %v", got.Languages, want)
		}
	})

	t.Run("env false skips autodetection", func(t *testing.T) {
		got, err := ResolveInitConfig(InitOptions{
			ProjectRoot:   t.TempDir(),
			EnvValue:      "false",
			CommandRunner: runnerFunc(nil, nil),
			GitFiles: func(string) ([]string, error) {
				t.Fatal("git files must not be consulted when env gate is false")
				return nil, nil
			},
		})
		if err != nil || len(got.Languages) != 0 {
			t.Fatalf("ResolveInitConfig() = %+v, %v; want no languages and no error", got, err)
		}
	})
}

func TestResolveInitConfigWarnsAndDropsMissingIndexers(t *testing.T) {
	got, err := ResolveInitConfig(InitOptions{
		ProjectRoot:       t.TempDir(),
		ExplicitLanguages: []string{"go", "typescript", "python"},
		EnvValue:          "true",
		CommandRunner: runnerFunc(nil, func(name, _ string) (string, error) {
			if name == "scip-typescript" {
				return "", errors.New("not found")
			}
			return "ok\n", nil
		}),
	})
	if err != nil {
		t.Fatalf("ResolveInitConfig() error = %v", err)
	}
	if want := []string{"go", "python"}; !reflect.DeepEqual(got.Languages, want) {
		t.Fatalf("Languages = %v, want %v", got.Languages, want)
	}
	if !strings.Contains(strings.Join(got.Warnings, "\n"), "typescript") {
		t.Fatalf("Warnings = %v, want dropped typescript warning", got.Warnings)
	}
}

func TestRuntimeCommandPlanningNoOpWhenDisabledOrUnconfigured(t *testing.T) {
	t.Run("env gate false skips git detection", func(t *testing.T) {
		t.Setenv(EnvEnableScipSearch, "false")

		plans, err := PlanRuntimeCommands(RuntimePlanOptions{
			TargetRoot:          t.TempDir(),
			ConfiguredLanguages: []string{"go"},
			GitFiles:            failGitFiles(t),
		})
		if err != nil {
			t.Fatalf("PlanRuntimeCommands() error = %v", err)
		}
		if len(plans) != 0 {
			t.Fatalf("PlanRuntimeCommands() = %v, want no plans", plans)
		}
	})

	t.Run("empty config skips git detection", func(t *testing.T) {
		t.Setenv(EnvEnableScipSearch, "true")

		plans, err := PlanRuntimeCommands(RuntimePlanOptions{
			TargetRoot:          t.TempDir(),
			ConfiguredLanguages: nil,
			GitFiles:            failGitFiles(t),
		})
		if err != nil {
			t.Fatalf("PlanRuntimeCommands() error = %v", err)
		}
		if len(plans) != 0 {
			t.Fatalf("PlanRuntimeCommands() = %v, want no plans", plans)
		}
	})
}

func TestRuntimeCommandPlanningFiltersDetectedConfiguredLanguagesInDeterministicOrder(t *testing.T) {
	t.Setenv(EnvEnableScipSearch, "true")
	target := t.TempDir()

	plans, err := PlanRuntimeCommands(RuntimePlanOptions{
		TargetRoot:          target,
		ConfiguredLanguages: []string{"python", "go", "typescript"},
		GitFiles: func(root string) ([]string, error) {
			if root != target {
				t.Fatalf("GitFiles root = %q, want %q", root, target)
			}
			return []string{
				"web/app.tsx",
				"README.md",
				"cmd/liza/main.go",
				"pyproject.toml",
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("PlanRuntimeCommands() error = %v", err)
	}

	if got, want := planLanguages(plans), []string{"go", "typescript", "python"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("plan languages = %v, want %v", got, want)
	}
	for _, plan := range plans {
		wantOutput := filepath.Join(target, ".liza", "scip", plan.Language+".scip")
		if plan.OutputPath != wantOutput {
			t.Fatalf("%s OutputPath = %q, want %q", plan.Language, plan.OutputPath, wantOutput)
		}
		if !filepath.IsAbs(plan.OutputPath) {
			t.Fatalf("%s OutputPath = %q, want absolute path", plan.Language, plan.OutputPath)
		}
	}
}

func TestRuntimeCommandPlanningIncludesOnlyConfiguredDetectedLanguages(t *testing.T) {
	t.Setenv(EnvEnableScipSearch, "true")

	plans, err := PlanRuntimeCommands(RuntimePlanOptions{
		TargetRoot:          t.TempDir(),
		ConfiguredLanguages: []string{"typescript", "python"},
		GitFiles: func(string) ([]string, error) {
			return []string{"go.mod", "cmd/main.go", "pkg/runtime.ts"}, nil
		},
	})
	if err != nil {
		t.Fatalf("PlanRuntimeCommands() error = %v", err)
	}

	if got, want := planLanguages(plans), []string{"typescript"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("plan languages = %v, want %v", got, want)
	}
}

func TestRuntimeCommandPlanningBuildsExactCommandPlans(t *testing.T) {
	t.Setenv(EnvEnableScipSearch, "true")
	target := t.TempDir()

	plans, err := PlanRuntimeCommands(RuntimePlanOptions{
		TargetRoot:          target,
		ConfiguredLanguages: []string{"go", "typescript", "python"},
		GitFiles: func(string) ([]string, error) {
			return []string{"go.mod", "tsconfig.json", "pyproject.toml"}, nil
		},
	})
	if err != nil {
		t.Fatalf("PlanRuntimeCommands() error = %v", err)
	}

	want := []RuntimeCommandPlan{
		{
			Language:   "go",
			Name:       "scip-go",
			Args:       []string{"index", "--module-root", target, "--skip-tests", "--output", filepath.Join(target, ".liza", "scip", "go.scip")},
			Dir:        target,
			OutputPath: filepath.Join(target, ".liza", "scip", "go.scip"),
		},
		{
			Language:   "typescript",
			Name:       "scip-typescript",
			Args:       []string{"index", "--cwd", target, "--output", filepath.Join(target, ".liza", "scip", "typescript.scip"), target},
			Dir:        target,
			OutputPath: filepath.Join(target, ".liza", "scip", "typescript.scip"),
		},
		{
			Language:   "python",
			Name:       "scip-python",
			Args:       []string{"index", "--cwd", target, "--output", filepath.Join(target, ".liza", "scip", "python.scip")},
			Dir:        target,
			OutputPath: filepath.Join(target, ".liza", "scip", "python.scip"),
		},
	}
	if !reflect.DeepEqual(plans, want) {
		t.Fatalf("PlanRuntimeCommands() = %#v, want %#v", plans, want)
	}
}

func TestRuntimeRefreshCreatesParentAndRunsExactCommandPlans(t *testing.T) {
	t.Setenv(EnvEnableScipSearch, "true")
	target := t.TempDir()
	var calls []RuntimeCommandPlan

	result, err := RefreshIndexes(RefreshOptions{
		TargetRoot:          target,
		ConfiguredLanguages: []string{"go", "typescript"},
		GitFiles: func(string) ([]string, error) {
			return []string{"go.mod", "web/app.ts"}, nil
		},
		Runner: func(plan RuntimeCommandPlan) (string, error) {
			calls = append(calls, cloneRuntimeCommandPlan(plan))
			if err := os.WriteFile(plan.OutputPath, []byte(plan.Language), 0o644); err != nil {
				t.Fatalf("WriteFile(%q) error = %v", plan.OutputPath, err)
			}
			return "", nil
		},
	})
	if err != nil {
		t.Fatalf("RefreshIndexes() error = %v", err)
	}

	goPath := filepath.Join(target, ".liza", "scip", "go.scip")
	tsPath := filepath.Join(target, ".liza", "scip", "typescript.scip")
	wantCalls := []RuntimeCommandPlan{
		{
			Language:   "go",
			Name:       "scip-go",
			Args:       []string{"index", "--module-root", target, "--skip-tests", "--output", goPath},
			Dir:        target,
			OutputPath: goPath,
		},
		{
			Language:   "typescript",
			Name:       "scip-typescript",
			Args:       []string{"index", "--cwd", target, "--output", tsPath, target},
			Dir:        target,
			OutputPath: tsPath,
		},
	}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("runner calls = %#v, want %#v", calls, wantCalls)
	}
	if !reflect.DeepEqual(result.Successes, []IndexRef{{Language: "go", Path: goPath}, {Language: "typescript", Path: tsPath}}) {
		t.Fatalf("successes = %#v, want go/typescript paths", result.Successes)
	}
	if len(result.Failures) != 0 {
		t.Fatalf("failures = %#v, want none", result.Failures)
	}
	if _, err := os.Stat(filepath.Join(target, ".liza", "scip")); err != nil {
		t.Fatalf("Stat(.liza/scip) error = %v", err)
	}
}

func TestRuntimeRefreshReportsBoundedFailureWithoutSuppressingSuccesses(t *testing.T) {
	t.Setenv(EnvEnableScipSearch, "true")
	target := t.TempDir()
	longOutput := strings.Repeat("x", maxFailureDiagnosticBytes+100)
	staleGoPath := filepath.Join(target, ".liza", "scip", "go.scip")
	if err := os.MkdirAll(filepath.Dir(staleGoPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(staleGoPath, []byte("stale go"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", staleGoPath, err)
	}

	result, err := RefreshIndexes(RefreshOptions{
		TargetRoot:          target,
		ConfiguredLanguages: []string{"go", "typescript"},
		GitFiles: func(string) ([]string, error) {
			return []string{"go.mod", "web/app.ts"}, nil
		},
		Runner: func(plan RuntimeCommandPlan) (string, error) {
			if plan.Language == "go" {
				if err := os.WriteFile(plan.OutputPath, []byte("partial go"), 0o644); err != nil {
					t.Fatalf("WriteFile(%q) error = %v", plan.OutputPath, err)
				}
				return longOutput, errors.New("go index failed")
			}
			if err := os.WriteFile(plan.OutputPath, []byte("typescript"), 0o644); err != nil {
				t.Fatalf("WriteFile(%q) error = %v", plan.OutputPath, err)
			}
			return "", nil
		},
	})
	if err != nil {
		t.Fatalf("RefreshIndexes() error = %v", err)
	}

	tsPath := filepath.Join(target, ".liza", "scip", "typescript.scip")
	if !reflect.DeepEqual(result.Successes, []IndexRef{{Language: "typescript", Path: tsPath}}) {
		t.Fatalf("successes = %#v, want only typescript", result.Successes)
	}
	if len(result.Failures) != 1 {
		t.Fatalf("failures = %#v, want one go failure", result.Failures)
	}
	failure := result.Failures[0]
	if failure.Language != "go" {
		t.Fatalf("failure language = %q, want go", failure.Language)
	}
	if len(failure.Diagnostic) > maxFailureDiagnosticBytes {
		t.Fatalf("failure diagnostic length = %d, want <= %d", len(failure.Diagnostic), maxFailureDiagnosticBytes)
	}
	if !strings.Contains(failure.Diagnostic, "go index failed") {
		t.Fatalf("failure diagnostic = %q, want runner error", failure.Diagnostic)
	}
	if _, err := os.Stat(staleGoPath); !os.IsNotExist(err) {
		t.Fatalf("Stat(%q) error = %v, want missing failed index", staleGoPath, err)
	}

	indexes, err := AvailableIndexes(RuntimePlanOptions{
		TargetRoot:          target,
		ConfiguredLanguages: []string{"go", "typescript"},
		GitFiles: func(string) ([]string, error) {
			return []string{"go.mod", "web/app.ts"}, nil
		},
	})
	if err != nil {
		t.Fatalf("AvailableIndexes() error = %v", err)
	}
	if !reflect.DeepEqual(indexes, []IndexRef{{Language: "typescript", Path: tsPath}}) {
		t.Fatalf("AvailableIndexes() = %#v, want only successful typescript index", indexes)
	}
}

func TestRuntimeAvailableIndexesReturnsOnlyExistingAbsolutePaths(t *testing.T) {
	t.Setenv(EnvEnableScipSearch, "true")
	target := t.TempDir()
	goPath := filepath.Join(target, ".liza", "scip", "go.scip")
	if err := os.MkdirAll(filepath.Dir(goPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(goPath, []byte("go"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", goPath, err)
	}

	indexes, err := AvailableIndexes(RuntimePlanOptions{
		TargetRoot:          target,
		ConfiguredLanguages: []string{"go", "typescript", "python"},
		GitFiles: func(string) ([]string, error) {
			return []string{"go.mod", "web/app.ts", "pyproject.toml"}, nil
		},
	})
	if err != nil {
		t.Fatalf("AvailableIndexes() error = %v", err)
	}

	if !reflect.DeepEqual(indexes, []IndexRef{{Language: "go", Path: goPath}}) {
		t.Fatalf("AvailableIndexes() = %#v, want only existing go index", indexes)
	}
	if !filepath.IsAbs(indexes[0].Path) {
		t.Fatalf("AvailableIndexes path = %q, want absolute", indexes[0].Path)
	}
}

func TestRuntimeTaskWorktreeIgnoreIsPrivateIdempotentAndClean(t *testing.T) {
	t.Setenv(EnvEnableScipSearch, "true")
	repo := newGitRepoWithWorktrees(t, "task-one")
	worktree := repo.worktrees["task-one"]
	privateExclude := filepath.Join(revParseGitDir(t, worktree), "info", "exclude")
	commonExclude := filepath.Join(repo.root, ".git", "info", "exclude")
	commonBefore := readFileString(t, commonExclude)
	var runnerCalls int
	var logOutput bytes.Buffer
	previousLogOutput := log.Writer()
	previousLogFlags := log.Flags()
	log.SetOutput(&logOutput)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(previousLogOutput)
		log.SetFlags(previousLogFlags)
	}()

	for i := 0; i < 2; i++ {
		result, err := RefreshIndexes(RefreshOptions{
			TargetRoot:          worktree,
			TargetKind:          TargetKindTaskWorktree,
			ConfiguredLanguages: []string{"go"},
			Runner: func(plan RuntimeCommandPlan) (string, error) {
				runnerCalls++
				assertIgnoreEntryInstalled(t, privateExclude)
				if _, err := os.Stat(plan.OutputPath); !os.IsNotExist(err) {
					t.Fatalf("Stat(%q) before index write error = %v, want missing", plan.OutputPath, err)
				}
				if got := readFileString(t, commonExclude); got != commonBefore {
					t.Fatalf("common exclude changed before index write: %q, want %q", got, commonBefore)
				}
				if err := os.WriteFile(plan.OutputPath, []byte("go"), 0o644); err != nil {
					t.Fatalf("WriteFile(%q) error = %v", plan.OutputPath, err)
				}
				return "", nil
			},
		})
		if err != nil {
			t.Fatalf("RefreshIndexes() iteration %d error = %v", i+1, err)
		}
		wantPath := filepath.Join(worktree, ".liza", "scip", "go.scip")
		if !reflect.DeepEqual(result.Successes, []IndexRef{{Language: "go", Path: wantPath}}) {
			t.Fatalf("successes iteration %d = %#v, want go index at %q", i+1, result.Successes, wantPath)
		}
		if len(result.Failures) != 0 {
			t.Fatalf("failures iteration %d = %#v, want none", i+1, result.Failures)
		}
	}

	if runnerCalls != 2 {
		t.Fatalf("runner calls = %d, want 2", runnerCalls)
	}
	if count := strings.Count(logOutput.String(), "enabled git extensions.worktreeConfig for scip-search task worktree excludes"); count != 1 {
		t.Fatalf("worktreeConfig log count = %d, want 1; logs: %q", count, logOutput.String())
	}
	if got := gitOutput(t, worktree, "config", "--get", "extensions.worktreeConfig"); got != "true" {
		t.Fatalf("extensions.worktreeConfig = %q, want true", got)
	}
	assertIgnoreEntryInstalled(t, privateExclude)
	if got := readFileString(t, commonExclude); got != commonBefore {
		t.Fatalf("common exclude = %q, want unchanged %q", got, commonBefore)
	}
	if status := gitOutput(t, worktree, "status", "--porcelain"); status != "" {
		t.Fatalf("git status --porcelain = %q, want clean", status)
	}
}

func TestRuntimeConcurrentTaskWorktreeRefreshesUsePrivateOutputsAndExcludes(t *testing.T) {
	t.Setenv(EnvEnableScipSearch, "true")
	repo := newGitRepoWithWorktrees(t, "task-one", "task-two")
	commonExclude := filepath.Join(repo.root, ".git", "info", "exclude")
	commonBefore := readFileString(t, commonExclude)
	privateExcludes := map[string]string{
		"task-one": filepath.Join(revParseGitDir(t, repo.worktrees["task-one"]), "info", "exclude"),
		"task-two": filepath.Join(revParseGitDir(t, repo.worktrees["task-two"]), "info", "exclude"),
	}
	var (
		mu      sync.Mutex
		outputs []string
		results = make(map[string]RefreshResult)
	)

	var wg sync.WaitGroup
	errs := make(chan error, len(repo.worktrees))
	for name, worktree := range repo.worktrees {
		name := name
		worktree := worktree
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := RefreshIndexes(RefreshOptions{
				TargetRoot:          worktree,
				TargetKind:          TargetKindTaskWorktree,
				ConfiguredLanguages: []string{"go"},
				Runner: func(plan RuntimeCommandPlan) (string, error) {
					if !filepath.IsAbs(plan.OutputPath) {
						return "", errors.New("output path is not absolute")
					}
					if !strings.HasPrefix(plan.OutputPath, worktree+string(os.PathSeparator)) {
						return "", errors.New("output path is outside task worktree")
					}
					if err := ignoreEntryError(privateExcludes[name]); err != nil {
						return "", err
					}
					if err := os.WriteFile(plan.OutputPath, []byte(name), 0o644); err != nil {
						return "", err
					}
					mu.Lock()
					outputs = append(outputs, plan.OutputPath)
					mu.Unlock()
					return "", nil
				},
			})
			if err != nil {
				errs <- err
				return
			}
			mu.Lock()
			results[name] = result
			mu.Unlock()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent refresh error = %v", err)
		}
	}

	if len(outputs) != 2 || outputs[0] == outputs[1] {
		t.Fatalf("outputs = %v, want two distinct output paths", outputs)
	}
	if privateExcludes["task-one"] == privateExcludes["task-two"] {
		t.Fatalf("private excludes share path %q", privateExcludes["task-one"])
	}
	for name, worktree := range repo.worktrees {
		wantPath := filepath.Join(worktree, ".liza", "scip", "go.scip")
		if !reflect.DeepEqual(results[name].Successes, []IndexRef{{Language: "go", Path: wantPath}}) {
			t.Fatalf("%s successes = %#v, want %q", name, results[name].Successes, wantPath)
		}
		if len(results[name].Failures) != 0 {
			t.Fatalf("%s failures = %#v, want none", name, results[name].Failures)
		}
		assertIgnoreEntryInstalled(t, privateExcludes[name])
		if status := gitOutput(t, worktree, "status", "--porcelain"); status != "" {
			t.Fatalf("%s git status --porcelain = %q, want clean", name, status)
		}
	}
	if got := readFileString(t, commonExclude); got != commonBefore {
		t.Fatalf("common exclude = %q, want unchanged %q", got, commonBefore)
	}
}

func runnerFunc(calls *[]string, fn func(name, argString string) (string, error)) CommandRunner {
	return func(name string, args ...string) (string, error) {
		argString := strings.Join(args, " ")
		if calls != nil {
			*calls = append(*calls, name+" "+argString)
		}
		if fn != nil {
			return fn(name, argString)
		}
		return "ok\n", nil
	}
}

func hasCall(calls []string, want string) bool {
	for _, call := range calls {
		if call == want {
			return true
		}
	}
	return false
}

func failGitFiles(t *testing.T) GitFilesFunc {
	t.Helper()

	return func(string) ([]string, error) {
		t.Fatal("git files must not be consulted")
		return nil, nil
	}
}

func planLanguages(plans []RuntimeCommandPlan) []string {
	languages := make([]string, 0, len(plans))
	for _, plan := range plans {
		languages = append(languages, plan.Language)
	}
	return languages
}

func cloneRuntimeCommandPlan(plan RuntimeCommandPlan) RuntimeCommandPlan {
	plan.Args = slices.Clone(plan.Args)
	return plan
}

type gitRepoFixture struct {
	root      string
	worktrees map[string]string
}

func newGitRepoWithWorktrees(t *testing.T, names ...string) gitRepoFixture {
	t.Helper()

	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", repo, err)
	}
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "liza@example.invalid")
	runGit(t, repo, "config", "user.name", "Liza Test")
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.test/repo\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(go.mod) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(main.go) error = %v", err)
	}
	runGit(t, repo, "add", "go.mod", "main.go")
	runGit(t, repo, "commit", "-m", "initial")
	runGit(t, repo, "branch", "-M", "main")

	fixture := gitRepoFixture{root: repo, worktrees: make(map[string]string, len(names))}
	for _, name := range names {
		worktree := filepath.Join(parent, name)
		runGit(t, repo, "worktree", "add", "-b", name, worktree, "main")
		fixture.worktrees[name] = worktree
	}
	return fixture
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s failed: %v\n%s", strings.Join(args, " "), dir, err, output)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s in %s failed: %v", strings.Join(args, " "), dir, err)
	}
	return strings.TrimSpace(string(output))
}

func revParseGitDir(t *testing.T, worktree string) string {
	t.Helper()

	gitDir := gitOutput(t, worktree, "rev-parse", "--git-dir")
	if filepath.IsAbs(gitDir) {
		return gitDir
	}
	return filepath.Clean(filepath.Join(worktree, gitDir))
}

func readFileString(t *testing.T, path string) string {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	return string(content)
}

func assertIgnoreEntryInstalled(t *testing.T, excludePath string) {
	t.Helper()

	if err := ignoreEntryError(excludePath); err != nil {
		t.Fatal(err)
	}
}

func ignoreEntryError(excludePath string) error {
	content, err := os.ReadFile(excludePath)
	if err != nil {
		return fmt.Errorf("read %s: %w", excludePath, err)
	}
	if count := strings.Count(string(content), ".liza/scip/"); count != 1 {
		return fmt.Errorf("%s contains .liza/scip/ %d times, want exactly once; content: %q", excludePath, count, content)
	}
	return nil
}
