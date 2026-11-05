package semble

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/paths"
)

func TestActivation(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{name: "unset", env: nil},
		{name: "empty", env: map[string]string{EnvEnableSemble: ""}},
		{name: "zero", env: map[string]string{EnvEnableSemble: "0"}},
		{name: "false", env: map[string]string{EnvEnableSemble: "false"}},
		{name: "unexpected", env: map[string]string{EnvEnableSemble: "yes"}},
		{name: "one", env: map[string]string{EnvEnableSemble: "1"}, want: true},
		{name: "true", env: map[string]string{EnvEnableSemble: "true"}, want: true},
		{name: "trimmed uppercase true", env: map[string]string{EnvEnableSemble: " TRUE "}, want: true},
		{name: "trimmed mixed one", env: map[string]string{EnvEnableSemble: " 1 "}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unsetEnvForTest(t, EnvEnableSemble)
			for key, value := range tt.env {
				t.Setenv(key, value)
			}

			if got := RuntimeEnabled(); got != tt.want {
				t.Fatalf("RuntimeEnabled() = %v, want %v", got, tt.want)
			}

			if !tt.want {
				result := PlanCommands(CommandPlanOptions{
					FixtureDir: t.TempDir(),
					LookPath: func(string) (string, error) {
						t.Fatal("executable lookup called while Semble is disabled")
						return "", nil
					},
				})
				if result.Enabled || result.Prewarm.Enabled || result.OfflineValidation.Enabled {
					t.Fatalf("PlanCommands() = %#v, want disabled no-op plans", result)
				}
				if len(result.Diagnostics) != 0 {
					t.Fatalf("PlanCommands() diagnostics = %#v, want none", result.Diagnostics)
				}
			}
		})
	}
}

func TestCommandPlan(t *testing.T) {
	fixtureDir := t.TempDir()
	var lookups []string
	t.Setenv(EnvEnableSemble, "true")

	result := PlanCommands(CommandPlanOptions{
		FixtureDir: fixtureDir,
		LookPath: func(name string) (string, error) {
			lookups = append(lookups, name)
			return "/opt/bin/semble", nil
		},
	})

	if !result.Enabled {
		t.Fatalf("PlanCommands().Enabled = false, want true")
	}
	if !reflect.DeepEqual(lookups, []string{"semble"}) {
		t.Fatalf("lookups = %#v, want semble lookup once", lookups)
	}
	if result.ExecutablePath != "/opt/bin/semble" {
		t.Fatalf("ExecutablePath = %q, want resolved executable path", result.ExecutablePath)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Diagnostics = %#v, want none", result.Diagnostics)
	}

	wantBase := CommandPlan{
		Enabled:        true,
		Name:           "semble",
		ExecutablePath: "/opt/bin/semble",
		Args:           []string{"search", "__semble_prewarm__", fixtureDir, "--top-k", "1", "--content", "code"},
		Dir:            fixtureDir,
		Timeout:        SembleValidationTimeout,
		Fixture: FixtureIdentity{
			FileName:    "prewarm.py",
			FileContent: "def semble_prewarm(): pass\n",
			Query:       "__semble_prewarm__",
			TopK:        1,
			ContentMode: "code",
		},
	}
	if !reflect.DeepEqual(result.Prewarm, wantBase) {
		t.Fatalf("Prewarm = %#v, want %#v", result.Prewarm, wantBase)
	}

	wantOffline := wantBase
	wantOffline.Env = []EnvVar{{Name: "HF_HUB_OFFLINE", Value: "1"}}
	if !reflect.DeepEqual(result.OfflineValidation, wantOffline) {
		t.Fatalf("OfflineValidation = %#v, want %#v", result.OfflineValidation, wantOffline)
	}
	if result.Prewarm.Timeout != 30*time.Second || result.OfflineValidation.Timeout != 30*time.Second {
		t.Fatalf("timeouts = %s/%s, want 30s", result.Prewarm.Timeout, result.OfflineValidation.Timeout)
	}
}

func TestCommandPlanMissingExecutable(t *testing.T) {
	t.Setenv(EnvEnableSemble, "true")

	result := PlanCommands(CommandPlanOptions{
		FixtureDir: t.TempDir(),
		LookPath: func(string) (string, error) {
			return "", errors.New("not found")
		},
	})

	if !result.Enabled {
		t.Fatalf("PlanCommands().Enabled = false, want true activation")
	}
	if result.Prewarm.Enabled || result.OfflineValidation.Enabled {
		t.Fatalf("PlanCommands() = %#v, want no executable-backed command plans", result)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Kind != DiagnosticMissingExecutable {
		t.Fatalf("Diagnostics = %#v, want one missing-executable diagnostic", result.Diagnostics)
	}
}

func TestDefaultIgnorePatterns(t *testing.T) {
	want := []string{
		paths.ProjectDirName() + "/",
		".worktrees/",
		"stacklit.json",
		"*.scip",
		".env",
		".env.*",
		"*.env",
		"credentials.*",
		"secrets.*",
		"*secret*.*",
		"*.pem",
		"*.key",
		"*.p12",
		"*.pfx",
		"*.jks",
		"*_rsa",
		"*_dsa",
		"*_ecdsa",
		"*_ed25519",
		"*.keystore",
		"*.truststore",
		"config/secrets/",
		"**/secrets/",
		"serviceAccountKey.json",
		"*-credentials.json",
	}

	got := DefaultIgnorePatterns()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DefaultIgnorePatterns() = %#v, want %#v", got, want)
	}
	got[0] = "mutated"
	if again := DefaultIgnorePatterns(); again[0] != paths.ProjectDirName()+"/" {
		t.Fatalf("DefaultIgnorePatterns() returned mutable backing slice: %#v", again)
	}
}

func TestEnsureProjectRootIgnore(t *testing.T) {
	t.Run("creates missing physical ignore with default payload", func(t *testing.T) {
		root := t.TempDir()

		result := EnsureProjectRootIgnore(root)

		if !result.Safe {
			t.Fatalf("EnsureProjectRootIgnore() = %#v, want safe created ignore", result)
		}
		if result.Kind != TargetKindProjectRoot {
			t.Fatalf("Kind = %q, want project root", result.Kind)
		}
		assertFileContent(t, filepath.Join(root, ".sembleignore"), GeneratedWorktreeIgnorePayload())
		safety := ValidateTargetSafety(TargetSafetyOptions{
			Kind:       TargetKindProjectRoot,
			TargetRoot: root,
		})
		if !safety.Safe {
			t.Fatalf("ValidateTargetSafety() after ensure = %#v, want safe", safety)
		}
	})

	t.Run("verifies safe existing ignore without rewriting", func(t *testing.T) {
		root := t.TempDir()
		ignorePath := filepath.Join(root, ".sembleignore")
		content := "# keep local comment\n" + GeneratedWorktreeIgnorePayload()
		if err := os.WriteFile(ignorePath, []byte(content), 0o644); err != nil {
			t.Fatalf("write safe .sembleignore: %v", err)
		}

		result := EnsureProjectRootIgnore(root)

		if !result.Safe {
			t.Fatalf("EnsureProjectRootIgnore() = %#v, want safe existing ignore", result)
		}
		assertFileContent(t, ignorePath, content)
	})

	t.Run("reports incomplete existing ignore without overwriting user content", func(t *testing.T) {
		root := t.TempDir()
		ignorePath := filepath.Join(root, ".sembleignore")
		partial := strings.Join(DefaultIgnorePatterns()[:2], "\n") + "\n"
		if err := os.WriteFile(ignorePath, []byte(partial), 0o644); err != nil {
			t.Fatalf("write incomplete .sembleignore: %v", err)
		}

		result := EnsureProjectRootIgnore(root)

		if result.Safe {
			t.Fatalf("EnsureProjectRootIgnore() = %#v, want unsafe incomplete ignore", result)
		}
		if len(result.MissingIgnorePatterns) == 0 {
			t.Fatalf("MissingIgnorePatterns = %#v, want missing required patterns", result.MissingIgnorePatterns)
		}
		if result.Diagnostic.Kind != DiagnosticExecutionFailure {
			t.Fatalf("Diagnostic = %#v, want execution failure", result.Diagnostic)
		}
		if !strings.Contains(result.Diagnostic.Message, "missing required patterns") {
			t.Fatalf("Diagnostic.Message = %q, want missing required patterns", result.Diagnostic.Message)
		}
		assertFileContent(t, ignorePath, partial)
	})

	t.Run("reports unreadable existing ignore without replacing it", func(t *testing.T) {
		root := t.TempDir()
		ignorePath := filepath.Join(root, ".sembleignore")
		if err := os.Mkdir(ignorePath, 0o755); err != nil {
			t.Fatalf("create directory at .sembleignore: %v", err)
		}

		result := EnsureProjectRootIgnore(root)

		if result.Safe {
			t.Fatalf("EnsureProjectRootIgnore() = %#v, want unsafe unreadable ignore", result)
		}
		if result.Diagnostic.Kind != DiagnosticExecutionFailure {
			t.Fatalf("Diagnostic = %#v, want execution failure", result.Diagnostic)
		}
		if !strings.Contains(result.Diagnostic.Message, "project root .sembleignore") {
			t.Fatalf("Diagnostic.Message = %q, want project-root ignore diagnostic", result.Diagnostic.Message)
		}
		info, err := os.Stat(ignorePath)
		if err != nil {
			t.Fatalf("Stat(%q) error = %v", ignorePath, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s was replaced; want original directory preserved", ignorePath)
		}
	})

	t.Run("reports create failure when project root cannot receive ignore", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "missing-root")

		result := EnsureProjectRootIgnore(root)

		if result.Safe {
			t.Fatalf("EnsureProjectRootIgnore() = %#v, want unsafe creation failure", result)
		}
		if result.Diagnostic.Kind != DiagnosticExecutionFailure {
			t.Fatalf("Diagnostic = %#v, want execution failure", result.Diagnostic)
		}
		if !strings.Contains(result.Diagnostic.Message, "create project root .sembleignore") {
			t.Fatalf("Diagnostic.Message = %q, want creation diagnostic", result.Diagnostic.Message)
		}
		assertBoundedDiagnostic(t, result.Diagnostic)
	})
}

func TestValidation(t *testing.T) {
	t.Run("prewarm creates fixture outside target root and cleans it", func(t *testing.T) {
		t.Setenv(EnvEnableSemble, "true")
		targetRoot := t.TempDir()
		var fixtureDir string

		result := ExecutePrewarm(ValidationOptions{
			TargetRoot: targetRoot,
			LookPath:   fixedLookPath("/opt/bin/semble"),
			Runner: func(plan CommandPlan) (CommandResult, error) {
				fixtureDir = plan.Dir
				if len(plan.Env) != 0 {
					t.Fatalf("prewarm env = %#v, want inherited environment only", plan.Env)
				}
				if pathWithinForTest(targetRoot, fixtureDir) {
					t.Fatalf("fixture dir %q is inside target root %q", fixtureDir, targetRoot)
				}
				content, err := os.ReadFile(filepath.Join(fixtureDir, "prewarm.py"))
				if err != nil {
					t.Fatalf("read fixture: %v", err)
				}
				if string(content) != "def semble_prewarm(): pass\n" {
					t.Fatalf("fixture content = %q", content)
				}
				if !reflect.DeepEqual(plan.Args, []string{"search", "__semble_prewarm__", fixtureDir, "--top-k", "1", "--content", "code"}) {
					t.Fatalf("prewarm args = %#v", plan.Args)
				}
				return CommandResult{ExitCode: 0, Stdout: "no hits"}, nil
			},
		})

		if !result.Ready {
			t.Fatalf("ExecutePrewarm() = %#v, want ready", result)
		}
		if fixtureDir == "" {
			t.Fatal("runner did not observe fixture dir")
		}
		if _, err := os.Stat(fixtureDir); !os.IsNotExist(err) {
			t.Fatalf("fixture dir stat after prewarm = %v, want cleaned up", err)
		}
	})

	t.Run("offline validation sets offline env", func(t *testing.T) {
		t.Setenv(EnvEnableSemble, "true")
		resetReadinessCacheForTest()
		var gotEnv []EnvVar

		result := CheckOfflineReadiness(ValidationOptions{
			TargetRoot: t.TempDir(),
			LookPath:   fixedLookPath("/opt/bin/semble"),
			Runner: func(plan CommandPlan) (CommandResult, error) {
				gotEnv = plan.Env
				if !reflect.DeepEqual(plan.Args, []string{"search", "__semble_prewarm__", plan.Dir, "--top-k", "1", "--content", "code"}) {
					t.Fatalf("offline args = %#v", plan.Args)
				}
				return CommandResult{ExitCode: 0}, nil
			},
		})

		if !result.Ready || result.Cached {
			t.Fatalf("CheckOfflineReadiness() = %#v, want fresh ready result", result)
		}
		if !reflect.DeepEqual(gotEnv, []EnvVar{{Name: "HF_HUB_OFFLINE", Value: "1"}}) {
			t.Fatalf("offline env = %#v", gotEnv)
		}
	})
}

func TestReadinessCache(t *testing.T) {
	t.Setenv(EnvEnableSemble, "true")
	t.Setenv("SEMBLE_MODEL_NAME", "")
	t.Setenv("HF_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	resetReadinessCacheForTest()

	calls := 0
	opts := ValidationOptions{
		TargetRoot: t.TempDir(),
		LookPath:   fixedLookPath("/opt/bin/semble"),
		Runner: func(CommandPlan) (CommandResult, error) {
			calls++
			return CommandResult{ExitCode: 0}, nil
		},
	}

	if result := CheckOfflineReadiness(opts); !result.Ready || result.Cached {
		t.Fatalf("first CheckOfflineReadiness() = %#v, want fresh ready", result)
	}
	if result := CheckOfflineReadiness(opts); !result.Ready || !result.Cached {
		t.Fatalf("second CheckOfflineReadiness() = %#v, want cached ready", result)
	}
	if calls != 1 {
		t.Fatalf("runner calls = %d, want 1", calls)
	}

	cacheMissCases := []struct {
		name  string
		opts  ValidationOptions
		env   map[string]string
		calls int
	}{
		{name: "executable", opts: ValidationOptions{TargetRoot: opts.TargetRoot, LookPath: fixedLookPath("/usr/local/bin/semble"), Runner: opts.Runner}, calls: 2},
		{name: "model env", opts: opts, env: map[string]string{"SEMBLE_MODEL_NAME": "local-model"}, calls: 3},
		{name: "hf home env", opts: opts, env: map[string]string{"HF_HOME": "/tmp/hf-cache"}, calls: 4},
		{name: "xdg cache env", opts: opts, env: map[string]string{"XDG_CACHE_HOME": "/tmp/xdg-cache"}, calls: 5},
		{name: "timeout", opts: ValidationOptions{TargetRoot: opts.TargetRoot, LookPath: opts.LookPath, Runner: opts.Runner, Timeout: time.Second}, calls: 6},
		{name: "fixture", opts: ValidationOptions{TargetRoot: opts.TargetRoot, LookPath: opts.LookPath, Runner: opts.Runner, Fixture: FixtureIdentity{FileName: "prewarm.py", FileContent: "def changed(): pass\n", Query: "__semble_prewarm__", TopK: 1, ContentMode: "code"}}, calls: 7},
	}

	for _, tc := range cacheMissCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SEMBLE_MODEL_NAME", "")
			t.Setenv("HF_HOME", "")
			t.Setenv("XDG_CACHE_HOME", "")
			for key, value := range tc.env {
				t.Setenv(key, value)
			}
			if result := CheckOfflineReadiness(tc.opts); !result.Ready || result.Cached {
				t.Fatalf("CheckOfflineReadiness() = %#v, want cache miss", result)
			}
			if calls != tc.calls {
				t.Fatalf("runner calls = %d, want %d", calls, tc.calls)
			}
			if result := CheckOfflineReadiness(tc.opts); !result.Ready || !result.Cached {
				t.Fatalf("repeat CheckOfflineReadiness() = %#v, want cache hit", result)
			}
			if calls != tc.calls {
				t.Fatalf("runner calls after repeat = %d, want %d", calls, tc.calls)
			}
		})
	}
}

func TestDiagnostics(t *testing.T) {
	t.Setenv(EnvEnableSemble, "true")
	resetReadinessCacheForTest()

	missing := CheckOfflineReadiness(ValidationOptions{
		TargetRoot: t.TempDir(),
		LookPath: func(string) (string, error) {
			return "", errors.New(strings.Repeat("missing ", maxDiagnosticBytes))
		},
	})
	if missing.Diagnostic.Kind != DiagnosticMissingExecutable {
		t.Fatalf("missing diagnostic = %#v", missing.Diagnostic)
	}
	assertBoundedDiagnostic(t, missing.Diagnostic)

	modelFailure := CheckOfflineReadiness(ValidationOptions{
		TargetRoot: t.TempDir(),
		LookPath:   fixedLookPath("/opt/bin/semble"),
		Runner: func(CommandPlan) (CommandResult, error) {
			return CommandResult{
				ExitCode: 1,
				Stderr:   "LocalEntryNotFoundError: model is unavailable while HF_HUB_OFFLINE=1\n" + strings.Repeat("cache-detail ", maxDiagnosticBytes),
			}, nil
		},
	})
	if modelFailure.Diagnostic.Kind != DiagnosticModelUnavailableOffline {
		t.Fatalf("model diagnostic = %#v", modelFailure.Diagnostic)
	}
	assertBoundedDiagnostic(t, modelFailure.Diagnostic)

	resetReadinessCacheForTest()
	executionFailure := CheckOfflineReadiness(ValidationOptions{
		TargetRoot: t.TempDir(),
		LookPath:   fixedLookPath("/opt/bin/semble"),
		Runner: func(CommandPlan) (CommandResult, error) {
			return CommandResult{
				ExitCode: 2,
				Stdout:   strings.Repeat("verbose-output ", maxDiagnosticBytes) + "UNBOUNDED_TAIL",
				Stderr:   "syntax exploded",
			}, errors.New("runner failed")
		},
	})
	if executionFailure.Diagnostic.Kind != DiagnosticExecutionFailure {
		t.Fatalf("execution diagnostic = %#v", executionFailure.Diagnostic)
	}
	assertBoundedDiagnostic(t, executionFailure.Diagnostic)
	if strings.Contains(executionFailure.Diagnostic.Message, "UNBOUNDED_TAIL") {
		t.Fatalf("diagnostic contains unbounded raw output tail: %q", executionFailure.Diagnostic.Message)
	}
}

func TestTargetSafety(t *testing.T) {
	t.Run("project root requires physical complete sembleignore", func(t *testing.T) {
		root := t.TempDir()

		result := ValidateTargetSafety(TargetSafetyOptions{
			Kind:       TargetKindProjectRoot,
			TargetRoot: root,
		})
		if result.Safe {
			t.Fatalf("ValidateTargetSafety() = %#v, want unsafe without physical .sembleignore", result)
		}
		if result.TargetRoot != filepath.Clean(root) {
			t.Fatalf("TargetRoot = %q, want absolute clean root %q", result.TargetRoot, filepath.Clean(root))
		}

		patterns := DefaultIgnorePatterns()
		if err := os.WriteFile(filepath.Join(root, ".sembleignore"), []byte(strings.Join(patterns[:len(patterns)-1], "\n")+"\n"), 0o644); err != nil {
			t.Fatalf("write incomplete .sembleignore: %v", err)
		}
		result = ValidateTargetSafety(TargetSafetyOptions{
			Kind:       TargetKindProjectRoot,
			TargetRoot: root,
		})
		if result.Safe {
			t.Fatalf("ValidateTargetSafety() = %#v, want unsafe with missing required pattern", result)
		}
		if !reflect.DeepEqual(result.MissingIgnorePatterns, []string{patterns[len(patterns)-1]}) {
			t.Fatalf("MissingIgnorePatterns = %#v, want final required pattern", result.MissingIgnorePatterns)
		}

		if err := os.WriteFile(filepath.Join(root, ".sembleignore"), []byte(GeneratedWorktreeIgnorePayload()), 0o644); err != nil {
			t.Fatalf("write complete .sembleignore: %v", err)
		}
		result = ValidateTargetSafety(TargetSafetyOptions{
			Kind:       TargetKindProjectRoot,
			TargetRoot: root,
		})
		if !result.Safe {
			t.Fatalf("ValidateTargetSafety() = %#v, want safe with complete .sembleignore", result)
		}
		if len(result.MissingIgnorePatterns) != 0 {
			t.Fatalf("MissingIgnorePatterns = %#v, want none", result.MissingIgnorePatterns)
		}
	})

	t.Run("task worktree requires exact root and complete physical sembleignore", func(t *testing.T) {
		projectRoot := t.TempDir()
		taskRoot := filepath.Join(projectRoot, ".worktrees", "task-1")
		if err := os.MkdirAll(taskRoot, 0o755); err != nil {
			t.Fatalf("create task root: %v", err)
		}

		result := ValidateTargetSafety(TargetSafetyOptions{
			Kind:                 TargetKindTaskWorktree,
			TargetRoot:           taskRoot,
			ExpectedWorktreeRoot: taskRoot,
		})
		if result.Safe {
			t.Fatalf("ValidateTargetSafety() = %#v, want unsafe without physical .sembleignore", result)
		}
		patterns := DefaultIgnorePatterns()
		if !reflect.DeepEqual(result.MissingIgnorePatterns, patterns) {
			t.Fatalf("MissingIgnorePatterns = %#v, want all required patterns", result.MissingIgnorePatterns)
		}

		if err := os.WriteFile(filepath.Join(taskRoot, ".sembleignore"), []byte(strings.Join(patterns[:1], "\n")+"\n"), 0o644); err != nil {
			t.Fatalf("write incomplete .sembleignore: %v", err)
		}
		result = ValidateTargetSafety(TargetSafetyOptions{
			Kind:                 TargetKindTaskWorktree,
			TargetRoot:           taskRoot,
			ExpectedWorktreeRoot: taskRoot,
		})
		if result.Safe {
			t.Fatalf("ValidateTargetSafety() = %#v, want unsafe with missing required patterns", result)
		}
		if !reflect.DeepEqual(result.MissingIgnorePatterns, patterns[1:]) {
			t.Fatalf("MissingIgnorePatterns = %#v, want required patterns after first", result.MissingIgnorePatterns)
		}

		if err := os.WriteFile(filepath.Join(taskRoot, ".sembleignore"), []byte(GeneratedWorktreeIgnorePayload()), 0o644); err != nil {
			t.Fatalf("write complete .sembleignore: %v", err)
		}
		result = ValidateTargetSafety(TargetSafetyOptions{
			Kind:                 TargetKindTaskWorktree,
			TargetRoot:           taskRoot,
			ExpectedWorktreeRoot: taskRoot,
		})
		if !result.Safe {
			t.Fatalf("ValidateTargetSafety() = %#v, want exact task worktree root safe with complete .sembleignore", result)
		}

		result = ValidateTargetSafety(TargetSafetyOptions{
			Kind:                 TargetKindTaskWorktree,
			TargetRoot:           projectRoot,
			ExpectedWorktreeRoot: taskRoot,
		})
		if result.Safe {
			t.Fatalf("ValidateTargetSafety() = %#v, want parent project-root substitution rejected", result)
		}
	})

	t.Run("generated payload follows default ordered source of truth", func(t *testing.T) {
		payload := GeneratedWorktreeIgnorePayload()
		if !strings.HasSuffix(payload, "\n") {
			t.Fatalf("GeneratedWorktreeIgnorePayload() = %q, want trailing newline", payload)
		}
		if payload != DefaultIgnorePayload() {
			t.Fatalf("GeneratedWorktreeIgnorePayload() = %q, want shared default payload %q", payload, DefaultIgnorePayload())
		}
		lines := strings.Split(strings.TrimSuffix(payload, "\n"), "\n")
		if !reflect.DeepEqual(lines, DefaultIgnorePatterns()) {
			t.Fatalf("GeneratedWorktreeIgnorePayload() lines = %#v, want DefaultIgnorePatterns()", lines)
		}
	})
}

func TestPromptMetadata(t *testing.T) {
	t.Run("omitted while disabled without side effects", func(t *testing.T) {
		unsetEnvForTest(t, EnvEnableSemble)

		metadata, ok := BuildPromptMetadata(PromptMetadataOptions{
			Kind:       TargetKindTaskWorktree,
			TargetRoot: t.TempDir(),
			LookPath: func(string) (string, error) {
				t.Fatal("executable lookup called while Semble is disabled")
				return "", nil
			},
			Runner: func(CommandPlan) (CommandResult, error) {
				t.Fatal("runner called while Semble is disabled")
				return CommandResult{}, nil
			},
		})
		if ok {
			t.Fatalf("BuildPromptMetadata() = %#v, true; want omitted", metadata)
		}
	})

	t.Run("omitted when unavailable not ready or unsafe", func(t *testing.T) {
		t.Setenv(EnvEnableSemble, "true")
		resetReadinessCacheForTest()
		targetRoot := t.TempDir()
		if err := os.WriteFile(filepath.Join(targetRoot, ".sembleignore"), []byte(GeneratedWorktreeIgnorePayload()), 0o644); err != nil {
			t.Fatalf("write complete .sembleignore: %v", err)
		}

		metadata, ok := BuildPromptMetadata(PromptMetadataOptions{
			Kind:                 TargetKindTaskWorktree,
			TargetRoot:           targetRoot,
			ExpectedWorktreeRoot: targetRoot,
			LookPath: func(string) (string, error) {
				return "", errors.New("missing /tmp/cache/path")
			},
		})
		if ok {
			t.Fatalf("BuildPromptMetadata() unavailable = %#v, true; want omitted", metadata)
		}

		metadata, ok = BuildPromptMetadata(PromptMetadataOptions{
			Kind:                 TargetKindTaskWorktree,
			TargetRoot:           targetRoot,
			ExpectedWorktreeRoot: targetRoot,
			LookPath:             fixedLookPath("/opt/bin/semble"),
			Runner: func(CommandPlan) (CommandResult, error) {
				return CommandResult{ExitCode: 1, Stderr: "/tmp/hf-cache unavailable"}, nil
			},
		})
		if ok {
			t.Fatalf("BuildPromptMetadata() not ready = %#v, true; want omitted", metadata)
		}

		resetReadinessCacheForTest()
		parentRoot := filepath.Dir(filepath.Dir(targetRoot))
		metadata, ok = BuildPromptMetadata(PromptMetadataOptions{
			Kind:                 TargetKindTaskWorktree,
			TargetRoot:           parentRoot,
			ExpectedWorktreeRoot: targetRoot,
			LookPath:             fixedLookPath("/opt/bin/semble"),
			Runner: func(CommandPlan) (CommandResult, error) {
				t.Fatal("runner called before target safety rejected parent root")
				return CommandResult{}, nil
			},
		})
		if ok {
			t.Fatalf("BuildPromptMetadata() unsafe = %#v, true; want omitted", metadata)
		}
	})

	t.Run("task worktree requires complete physical ignore before metadata", func(t *testing.T) {
		t.Setenv(EnvEnableSemble, "true")
		resetReadinessCacheForTest()
		targetRoot := t.TempDir()
		patterns := DefaultIgnorePatterns()
		runner := func(CommandPlan) (CommandResult, error) {
			return CommandResult{ExitCode: 0}, nil
		}

		metadata, ok := BuildPromptMetadata(PromptMetadataOptions{
			Kind:                 TargetKindTaskWorktree,
			TargetRoot:           targetRoot,
			ExpectedWorktreeRoot: targetRoot,
			LookPath:             fixedLookPath("/opt/bin/semble"),
			Runner: func(CommandPlan) (CommandResult, error) {
				t.Fatal("runner called before missing .sembleignore rejected task worktree")
				return CommandResult{}, nil
			},
		})
		if ok {
			t.Fatalf("BuildPromptMetadata() missing ignore = %#v, true; want omitted", metadata)
		}

		if err := os.WriteFile(filepath.Join(targetRoot, ".sembleignore"), []byte(strings.Join(patterns[:len(patterns)-1], "\n")+"\n"), 0o644); err != nil {
			t.Fatalf("write incomplete .sembleignore: %v", err)
		}
		metadata, ok = BuildPromptMetadata(PromptMetadataOptions{
			Kind:                 TargetKindTaskWorktree,
			TargetRoot:           targetRoot,
			ExpectedWorktreeRoot: targetRoot,
			LookPath:             fixedLookPath("/opt/bin/semble"),
			Runner: func(CommandPlan) (CommandResult, error) {
				t.Fatal("runner called before incomplete .sembleignore rejected task worktree")
				return CommandResult{}, nil
			},
		})
		if ok {
			t.Fatalf("BuildPromptMetadata() incomplete ignore = %#v, true; want omitted", metadata)
		}

		if err := os.WriteFile(filepath.Join(targetRoot, ".sembleignore"), []byte(GeneratedWorktreeIgnorePayload()), 0o644); err != nil {
			t.Fatalf("write complete .sembleignore: %v", err)
		}
		metadata, ok = BuildPromptMetadata(PromptMetadataOptions{
			Kind:                 TargetKindTaskWorktree,
			TargetRoot:           targetRoot,
			ExpectedWorktreeRoot: targetRoot,
			LookPath:             fixedLookPath("/opt/bin/semble"),
			Runner:               runner,
		})
		if !ok {
			t.Fatalf("BuildPromptMetadata() complete ignore = %#v, false; want metadata", metadata)
		}
	})

	t.Run("ready metadata is prompt safe", func(t *testing.T) {
		t.Setenv(EnvEnableSemble, "true")
		resetReadinessCacheForTest()
		targetRoot := filepath.Join(t.TempDir(), "repo root's worktree")
		if err := os.MkdirAll(targetRoot, 0o755); err != nil {
			t.Fatalf("create target root: %v", err)
		}
		if err := os.WriteFile(filepath.Join(targetRoot, ".sembleignore"), []byte(GeneratedWorktreeIgnorePayload()), 0o644); err != nil {
			t.Fatalf("write complete .sembleignore: %v", err)
		}

		metadata, ok := BuildPromptMetadata(PromptMetadataOptions{
			Kind:                 TargetKindTaskWorktree,
			TargetRoot:           targetRoot,
			ExpectedWorktreeRoot: targetRoot,
			LookPath:             fixedLookPath("/opt/bin/semble"),
			Runner: func(CommandPlan) (CommandResult, error) {
				return CommandResult{ExitCode: 0, Stdout: "/tmp/hf-cache raw output"}, nil
			},
		})
		if !ok {
			t.Fatalf("BuildPromptMetadata() ok = false, metadata = %#v", metadata)
		}
		if metadata.TargetRoot != filepath.Clean(targetRoot) {
			t.Fatalf("TargetRoot = %q, want %q", metadata.TargetRoot, filepath.Clean(targetRoot))
		}
		if metadata.ShellTargetRoot != "'"+strings.ReplaceAll(filepath.Clean(targetRoot), "'", "'\\''")+"'" {
			t.Fatalf("ShellTargetRoot = %q, want shell-quoted target", metadata.ShellTargetRoot)
		}
		for _, forbidden := range []string{"/tmp/hf-cache", "raw output", "/opt/bin/semble"} {
			if strings.Contains(metadata.TargetRoot, forbidden) || strings.Contains(metadata.ShellTargetRoot, forbidden) {
				t.Fatalf("metadata leaked %q: %#v", forbidden, metadata)
			}
		}
	})
}

func TestPromptMetadataShellQuotesTargetRoot(t *testing.T) {
	t.Setenv(EnvEnableSemble, "true")
	resetReadinessCacheForTest()
	targetRoot := filepath.Join(t.TempDir(), "quoted root's path")
	if err := os.MkdirAll(targetRoot, 0o755); err != nil {
		t.Fatalf("create target root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(targetRoot, ".sembleignore"), []byte(GeneratedWorktreeIgnorePayload()), 0o644); err != nil {
		t.Fatalf("write complete .sembleignore: %v", err)
	}

	metadata, ok := BuildPromptMetadata(PromptMetadataOptions{
		Kind:                 TargetKindTaskWorktree,
		TargetRoot:           targetRoot,
		ExpectedWorktreeRoot: targetRoot,
		LookPath:             fixedLookPath("/opt/bin/semble"),
		Runner: func(CommandPlan) (CommandResult, error) {
			return CommandResult{ExitCode: 0}, nil
		},
	})
	if !ok {
		t.Fatalf("BuildPromptMetadata() ok = false, metadata = %#v", metadata)
	}

	quotedRoot := "'" + strings.ReplaceAll(filepath.Clean(targetRoot), "'", "'\\''") + "'"
	if metadata.ShellTargetRoot != quotedRoot {
		t.Fatalf("ShellTargetRoot = %q, want %q", metadata.ShellTargetRoot, quotedRoot)
	}
}

func TestStartClearOrphansDisabledSkipsLookup(t *testing.T) {
	unsetEnvForTest(t, EnvEnableSemble)

	err := StartClearOrphans(func(string) (string, error) {
		t.Fatal("executable lookup called while Semble is disabled")
		return "", nil
	})
	if err != nil {
		t.Fatalf("StartClearOrphans() error = %v, want nil", err)
	}
}

func TestStartClearOrphansMissingExecutableIsNoOp(t *testing.T) {
	t.Setenv(EnvEnableSemble, "1")

	err := StartClearOrphans(func(string) (string, error) {
		return "", errors.New("not found")
	})
	if err != nil {
		t.Fatalf("StartClearOrphans() error = %v, want nil", err)
	}
}

func TestStartClearOrphansRunsDetachedOfflineCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake executable is a POSIX shell script")
	}
	t.Setenv(EnvEnableSemble, "1")
	dir := t.TempDir()
	// Reading the FIFO blocks until the detached child writes, so no polling.
	marker := filepath.Join(dir, "marker")
	if out, err := exec.Command("mkfifo", marker).CombinedOutput(); err != nil {
		t.Fatalf("mkfifo error = %v: %s", err, out)
	}
	// The marker path travels through the environment so TMPDIR spaces or
	// quotes cannot split it.
	t.Setenv("FAKE_SEMBLE_MARKER", marker)
	// A regular file the script creates first, so a timeout tells a child that
	// never ran from one blocked on the FIFO.
	started := filepath.Join(dir, "started")
	t.Setenv("FAKE_SEMBLE_STARTED", started)
	script := filepath.Join(dir, "semble")
	content := "#!/bin/sh\n: > \"$FAKE_SEMBLE_STARTED\"\nprintf '%s HF_HUB_OFFLINE=%s\\n' \"$*\" \"$HF_HUB_OFFLINE\" > \"$FAKE_SEMBLE_MARKER\"\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile(fake semble) error = %v", err)
	}

	got := make(chan string, 1)
	go func() {
		data, _ := os.ReadFile(marker)
		got <- string(data)
	}()
	if err := StartClearOrphans(fixedLookPath(script)); err != nil {
		t.Fatalf("StartClearOrphans() error = %v", err)
	}

	select {
	case invocation := <-got:
		if want := "clear orphans HF_HUB_OFFLINE=1\n"; invocation != want {
			t.Fatalf("fake semble invocation = %q, want %q", invocation, want)
		}
	case <-time.After(10 * time.Second):
		if _, err := os.Stat(started); err != nil {
			t.Fatalf("fake semble was not run: it did not start within 10s (%v)", err)
		}
		t.Fatal("fake semble started but did not write the marker FIFO within 10s")
	}
}

func unsetEnvForTest(t *testing.T, key string) {
	t.Helper()
	previous, wasSet := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("Unsetenv(%s) error = %v", key, err)
	}
	t.Cleanup(func() {
		var err error
		if wasSet {
			err = os.Setenv(key, previous)
		} else {
			err = os.Unsetenv(key)
		}
		if err != nil {
			t.Fatalf("restore %s env error = %v", key, err)
		}
	})
}

func fixedLookPath(path string) ExecutableLookup {
	return func(string) (string, error) {
		return path, nil
	}
}

func assertBoundedDiagnostic(t *testing.T, diagnostic Diagnostic) {
	t.Helper()
	if diagnostic.Message == "" {
		t.Fatal("diagnostic message is empty")
	}
	if len(diagnostic.Message) > maxDiagnosticBytes {
		t.Fatalf("diagnostic length = %d, want <= %d: %q", len(diagnostic.Message), maxDiagnosticBytes, diagnostic.Message)
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	if got := string(content); got != want {
		t.Fatalf("%s content = %q, want %q", path, got, want)
	}
}

func pathWithinForTest(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." && !filepath.IsAbs(rel))
}
