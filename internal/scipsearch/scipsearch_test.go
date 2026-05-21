package scipsearch

import (
	"errors"
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
