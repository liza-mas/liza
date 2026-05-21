package scipsearch

import (
	"errors"
	"go/ast"
	"go/doc"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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
