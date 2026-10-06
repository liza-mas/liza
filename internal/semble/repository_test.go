package semble

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/filelock"
)

func TestD48ModelReadinessDoesNotAdvertiseColdRepository(t *testing.T) {
	t.Setenv(EnvEnableSemble, "true")
	resetReadinessCacheForTest()
	root := t.TempDir()
	if safety := EnsureProjectRootIgnore(root); !safety.Safe {
		t.Fatalf("fixture root unsafe: %+v", safety)
	}
	var repositoryCalls int
	metadata, ok := BuildPromptMetadata(PromptMetadataOptions{
		Kind:       TargetKindProjectRoot,
		TargetRoot: root,
		LookPath:   fixedLookPath("/opt/bin/semble"),
		Runner: func(plan CommandPlan) (CommandResult, error) {
			if len(plan.Args) > 2 && filepath.Clean(plan.Args[2]) == root {
				repositoryCalls++
				return CommandResult{ExitCode: 1}, errors.New("repository corpus unavailable")
			}
			// A ready model can successfully search the controlled tiny fixture.
			return CommandResult{ExitCode: 0}, nil
		},
	})
	if ok || metadata != (PromptMetadata{}) {
		t.Fatalf("cold repository advertised after fixture-only success: %+v", metadata)
	}
	if repositoryCalls != 1 {
		t.Fatalf("repository readiness queries = %d, want one actual-target query", repositoryCalls)
	}
}

func TestRepositoryQueriesUseActualOfflineAllCorpus(t *testing.T) {
	t.Setenv(EnvEnableSemble, "true")
	for _, tt := range []struct {
		name    string
		run     func(ValidationOptions) ValidationResult
		timeout time.Duration
	}{
		{"preparation", PrepareRepository, RepositoryPreparationTimeout},
		{"readiness", CheckRepositoryReadiness, SembleValidationTimeout},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if !EnsureProjectRootIgnore(root).Safe {
				t.Fatal("root ignore safety failed")
			}
			calls := 0
			result := tt.run(ValidationOptions{TargetRoot: root, LookPath: fixedLookPath("/opt/bin/semble"), Runner: func(plan CommandPlan) (CommandResult, error) {
				calls++
				want := []string{"search", prewarmQuery, root, "--top-k", "1", "--content", "all"}
				if !reflect.DeepEqual(plan.Args, want) || plan.Dir != root {
					t.Fatalf("query = %v in %q, want %v in actual root", plan.Args, plan.Dir, want)
				}
				if plan.Timeout != tt.timeout || !reflect.DeepEqual(plan.Env, []EnvVar{{Name: "HF_HUB_OFFLINE", Value: "1"}}) {
					t.Fatalf("query timeout/env = %v/%v", plan.Timeout, plan.Env)
				}
				return CommandResult{ExitCode: 0, Stdout: "corpus chunk"}, nil
			}})
			if !result.Ready || calls != 1 || result.Diagnostic != (Diagnostic{}) {
				t.Fatalf("result = %+v, calls = %d", result, calls)
			}
		})
	}
}

func TestRepositoryQueriesRespectDisabledAndUnsafeGates(t *testing.T) {
	for _, enabled := range []string{"false", "true"} {
		t.Run(enabled, func(t *testing.T) {
			t.Setenv(EnvEnableSemble, enabled)
			result := PrepareRepository(ValidationOptions{TargetRoot: t.TempDir(), LookPath: func(string) (string, error) {
				t.Fatal("lookup before disabled/unsafe gate")
				return "", nil
			}, Runner: func(CommandPlan) (CommandResult, error) {
				t.Fatal("query before disabled/unsafe gate")
				return CommandResult{}, nil
			}})
			if result.Ready || result.Enabled != (enabled == "true") {
				t.Fatalf("disabled/unsafe result = %+v", result)
			}
		})
	}
}

func TestRepositoryFailureDiagnosticsExcludeCorpusOutput(t *testing.T) {
	t.Setenv(EnvEnableSemble, "true")
	root := t.TempDir()
	if !EnsureProjectRootIgnore(root).Safe {
		t.Fatal("root ignore safety failed")
	}
	for _, failure := range []error{errors.New("UNTRUSTED_CORPUS_OUTPUT"), context.DeadlineExceeded} {
		result := PrepareRepository(ValidationOptions{TargetRoot: root, LookPath: fixedLookPath("/opt/bin/semble"), Runner: func(CommandPlan) (CommandResult, error) {
			return CommandResult{ExitCode: 1, Stdout: "UNTRUSTED_CORPUS_OUTPUT", Stderr: "UNTRUSTED_CORPUS_OUTPUT"}, failure
		}})
		if result.Ready || strings.Contains(result.Diagnostic.Message, "UNTRUSTED_CORPUS_OUTPUT") {
			t.Fatalf("unsafe failure diagnostic = %+v", result)
		}
		assertBoundedDiagnostic(t, result.Diagnostic)
		if errors.Is(failure, context.DeadlineExceeded) && !strings.Contains(result.Diagnostic.Message, "timed out") {
			t.Fatalf("timeout not reported: %+v", result)
		}
	}
}

func TestRepositoryBusyProbeDoesNotStartAnotherBuild(t *testing.T) {
	t.Setenv(EnvEnableSemble, "true")
	root := t.TempDir()
	if !EnsureProjectRootIgnore(root).Safe {
		t.Fatal("root ignore safety failed")
	}
	lockPath, err := repositoryLockPath(root)
	if err != nil {
		t.Fatal(err)
	}
	held, acquired, err := filelock.New(lockPath).TryHold("preparation")
	if err != nil || !acquired {
		t.Fatalf("lock = %v/%v", acquired, err)
	}
	result := CheckRepositoryReadiness(ValidationOptions{TargetRoot: root, LookPath: fixedLookPath("/opt/bin/semble"), Runner: func(CommandPlan) (CommandResult, error) {
		t.Fatal("busy corpus started another query")
		return CommandResult{}, nil
	}})
	if err := held.Release(); err != nil {
		t.Fatal(err)
	}
	if result.Ready || !strings.Contains(result.Diagnostic.Message, "already running") {
		t.Fatalf("busy result = %+v", result)
	}
	result = CheckRepositoryReadiness(ValidationOptions{TargetRoot: root, LookPath: fixedLookPath("/opt/bin/semble"), Runner: func(CommandPlan) (CommandResult, error) {
		return CommandResult{ExitCode: 0}, nil
	}})
	if !result.Ready {
		t.Fatalf("released corpus remains unavailable: %+v", result)
	}
}

func TestPromptCorpusReadinessIsRecheckedAfterSuccess(t *testing.T) {
	t.Setenv(EnvEnableSemble, "true")
	resetReadinessCacheForTest()
	root := t.TempDir()
	if !EnsureProjectRootIgnore(root).Safe {
		t.Fatal("root ignore safety failed")
	}
	corpusCalls := 0
	opts := PromptMetadataOptions{Kind: TargetKindProjectRoot, TargetRoot: root, LookPath: fixedLookPath("/opt/bin/semble"), Runner: func(plan CommandPlan) (CommandResult, error) {
		if plan.Args[2] == root {
			corpusCalls++
			if corpusCalls > 1 {
				return CommandResult{ExitCode: 1}, errors.New("cache changed")
			}
		}
		return CommandResult{ExitCode: 0}, nil
	}}
	if _, ready := BuildPromptMetadata(opts); !ready {
		t.Fatal("prepared corpus not advertised")
	}
	if _, ready := BuildPromptMetadata(opts); ready || corpusCalls != 2 {
		t.Fatalf("stale success reused: ready=%v, corpus calls=%d", ready, corpusCalls)
	}
}

func TestPreparationLockWaitIsBounded(t *testing.T) {
	t.Setenv(EnvEnableSemble, "true")
	root := t.TempDir()
	if !EnsureProjectRootIgnore(root).Safe {
		t.Fatal("root ignore safety failed")
	}
	lockPath, err := repositoryLockPath(root)
	if err != nil {
		t.Fatal(err)
	}
	held, acquired, err := filelock.New(lockPath).TryHold("readiness")
	if err != nil || !acquired {
		t.Fatalf("lock = %v/%v", acquired, err)
	}
	defer held.Release()
	result := PrepareRepository(ValidationOptions{TargetRoot: root, Timeout: 20 * time.Millisecond, LookPath: fixedLookPath("/opt/bin/semble"), Runner: func(CommandPlan) (CommandResult, error) {
		t.Fatal("preparation ran without the corpus lock")
		return CommandResult{}, nil
	}})
	if result.Ready || !strings.Contains(result.Diagnostic.Message, "lock timed out") {
		t.Fatalf("bounded preparation = %+v", result)
	}
}
