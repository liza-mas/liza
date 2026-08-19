package ops

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/semble"
	"github.com/liza-mas/liza/internal/testhelpers"
	"gopkg.in/yaml.v3"
)

// setupInitTestDir creates a temp dir with a git repo and a spec file,
// mimicking a valid project root for InitProject.
func setupInitTestDir(t *testing.T) (projectRoot, specFile string) {
	t.Helper()

	testhelpers.SetupGlobalLiza(t)
	projectRoot = t.TempDir()

	// Initialize a git repo so branch operations work.
	gitInit(t, projectRoot)

	specFile = testhelpers.CreateCommittedSpecFile(t, projectRoot, "goal.md", "# Test Goal\n")
	testhelpers.CreateCommittedPreCommitConfig(t, projectRoot)

	return projectRoot, specFile
}

func setupInitTestDirNoCommit(t *testing.T) (projectRoot, specFile string) {
	t.Helper()

	testhelpers.SetupGlobalLiza(t)
	projectRoot = t.TempDir()

	cmds := [][]string{
		{"git", "-C", projectRoot, "init"},
		{"git", "-C", projectRoot, "config", "user.email", "test@test.com"},
		{"git", "-C", projectRoot, "config", "user.name", "Test"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git command %v failed: %v\n%s", args, err, out)
		}
	}

	specDir := filepath.Join(projectRoot, "specs")
	if err := os.MkdirAll(specDir, 0755); err != nil {
		t.Fatalf("Failed to create specs dir: %v", err)
	}
	specFile = filepath.Join(specDir, "goal.md")
	if err := os.WriteFile(specFile, []byte("# Test Goal\n"), 0644); err != nil {
		t.Fatalf("Failed to write spec file: %v", err)
	}

	return projectRoot, specFile
}

// gitInit initializes a bare-minimum git repo with an initial commit.
func gitInit(t *testing.T, dir string) {
	t.Helper()
	testhelpers.SetupBasicTestGitRepo(t, dir)
}

func TestInitProject_Success(t *testing.T) {
	t.Setenv(models.EnvEnableCopyWorktreeEnvFiles, "0")
	projectRoot, specFile := setupInitTestDir(t)

	err := InitProject(projectRoot, InitProjectParams{
		Description: "Test project",
		SpecRef:     specFile,
	})
	if err != nil {
		t.Fatalf("InitProject() error: %v", err)
	}

	// Verify .liza directory exists
	lizaDir := filepath.Join(projectRoot, ".liza")
	if _, err := os.Stat(lizaDir); os.IsNotExist(err) {
		t.Fatal(".liza directory was not created")
	}

	// Verify state.yaml is readable with expected values
	statePath := filepath.Join(lizaDir, "state.yaml")
	bb := db.For(statePath)
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	if state.Goal.Status != models.GoalStatusInProgress {
		t.Errorf("GoalStatus = %v, want IN_PROGRESS", state.Goal.Status)
	}
	if state.Config.Mode != models.SystemModeRunning {
		t.Errorf("Mode = %v, want RUNNING", state.Config.Mode)
	}
	if state.Config.IntegrationBranch != "integration" {
		t.Errorf("IntegrationBranch = %q, want %q", state.Config.IntegrationBranch, "integration")
	}
	if state.Goal.Description != "Test project" {
		t.Errorf("Description = %q, want %q", state.Goal.Description, "Test project")
	}
	if state.Goal.SpecRef != "specs/goal.md" {
		t.Errorf("SpecRef = %q, want %q", state.Goal.SpecRef, "specs/goal.md")
	}
	if state.Config.CopyWorktreeEnvFiles {
		t.Error("CopyWorktreeEnvFiles = true, want false by default")
	}

	// Verify log file exists
	logPath := filepath.Join(lizaDir, "log.yaml")
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Fatal("log.yaml was not created")
	}

	// Verify alerts.log exists
	alertsPath := filepath.Join(lizaDir, "alerts.log")
	if _, err := os.Stat(alertsPath); os.IsNotExist(err) {
		t.Fatal("alerts.log was not created")
	}

	// Verify lock file exists
	lockPath := filepath.Join(lizaDir, "state.yaml.lock")
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		t.Fatal("state.yaml.lock was not created")
	}

	// Verify archive directory exists
	archiveDir := filepath.Join(lizaDir, "archive")
	if _, err := os.Stat(archiveDir); os.IsNotExist(err) {
		t.Fatal("archive directory was not created")
	}

	// Verify SUPPORT.md exists
	supportPath := filepath.Join(lizaDir, "SUPPORT.md")
	if _, err := os.Stat(supportPath); os.IsNotExist(err) {
		t.Fatal("SUPPORT.md was not created")
	}

	// Verify pipeline.yaml frozen
	pipelinePath := filepath.Join(lizaDir, "pipeline.yaml")
	if _, err := os.Stat(pipelinePath); os.IsNotExist(err) {
		t.Fatal("pipeline.yaml was not created")
	}
}

func TestGlobalIntegrationGenerationLimitDefaults(t *testing.T) {
	tests := []struct {
		name  string
		input int
		want  int
	}{
		{name: "zero defaults", input: 0, want: 3},
		{name: "negative defaults", input: -1, want: 3},
		{name: "positive preserved", input: 7, want: 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectRoot, specFile := setupInitTestDir(t)
			if err := InitProject(projectRoot, InitProjectParams{
				Description:                     "Test project",
				SpecRef:                         specFile,
				MaxGlobalIntegrationGenerations: tt.input,
			}); err != nil {
				t.Fatalf("InitProject() error: %v", err)
			}

			statePath := filepath.Join(projectRoot, ".liza", "state.yaml")
			state, err := db.For(statePath).Read()
			if err != nil {
				t.Fatalf("read typed state: %v", err)
			}
			if got := state.Config.MaxGlobalIntegrationGenerations; got != tt.want {
				t.Fatalf("typed max global integration generations = %d, want %d", got, tt.want)
			}

			data, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatalf("read state.yaml: %v", err)
			}
			var persisted struct {
				Config struct {
					MaxGlobalIntegrationGenerations int `yaml:"max_global_integration_generations"`
				} `yaml:"config"`
			}
			if err := yaml.Unmarshal(data, &persisted); err != nil {
				t.Fatalf("unmarshal state.yaml: %v", err)
			}
			persistedLimit := persisted.Config.MaxGlobalIntegrationGenerations
			if persistedLimit != state.Config.MaxGlobalIntegrationGenerations {
				t.Fatalf("persisted max global integration generations = %d, typed state = %d", persistedLimit, state.Config.MaxGlobalIntegrationGenerations)
			}
		})
	}
}

func TestInitProject_CopyWorktreeEnvFiles(t *testing.T) {
	projectRoot, specFile := setupInitTestDir(t)

	err := InitProject(projectRoot, InitProjectParams{
		Description:          "Test project",
		SpecRef:              specFile,
		CopyWorktreeEnvFiles: true,
	})
	if err != nil {
		t.Fatalf("InitProject() error: %v", err)
	}

	bb := db.For(filepath.Join(projectRoot, ".liza", "state.yaml"))
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if !state.Config.CopyWorktreeEnvFiles {
		t.Fatal("CopyWorktreeEnvFiles = false, want true")
	}
}

func TestInitProject_CopyWorktreeEnvFilesFromEnv(t *testing.T) {
	projectRoot, specFile := setupInitTestDir(t)
	t.Setenv(models.EnvEnableCopyWorktreeEnvFiles, "1")

	err := InitProject(projectRoot, InitProjectParams{
		Description: "Test project",
		SpecRef:     specFile,
	})
	if err != nil {
		t.Fatalf("InitProject() error: %v", err)
	}

	bb := db.For(filepath.Join(projectRoot, ".liza", "state.yaml"))
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if !state.Config.CopyWorktreeEnvFiles {
		t.Fatal("CopyWorktreeEnvFiles = false, want true from env")
	}
}

func TestInitProject_SembleDisabledSkipsLookupValidationAndDiagnostics(t *testing.T) {
	tests := []struct {
		name  string
		value string
		unset bool
	}{
		{name: "unset", unset: true},
		{name: "empty", value: ""},
		{name: "zero", value: "0"},
		{name: "false", value: "false"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.unset {
				unsetEnvForInitProjectTest(t, semble.EnvEnableSemble)
			} else {
				t.Setenv(semble.EnvEnableSemble, tt.value)
			}
			projectRoot, specFile := setupInitTestDir(t)
			var diagnostics []string
			restore := setInitProjectSembleHooksForTest(t,
				func(string) (string, error) {
					t.Fatal("Semble executable lookup called while disabled")
					return "", nil
				},
				func(semble.CommandPlan) (semble.CommandResult, error) {
					t.Fatal("Semble command runner called while disabled")
					return semble.CommandResult{}, nil
				},
			)
			defer restore()

			err := InitProject(projectRoot, InitProjectParams{
				Description: "Test project",
				SpecRef:     specFile,
				SembleDiagnosticSink: func(message string) {
					diagnostics = append(diagnostics, message)
				},
			})
			if err != nil {
				t.Fatalf("InitProject() error: %v", err)
			}
			if len(diagnostics) != 0 {
				t.Fatalf("Semble diagnostics = %#v, want none", diagnostics)
			}
		})
	}
}

func TestInitProject_SembleEnabledSkipsLookupWhenHardPreconditionsFail(t *testing.T) {
	t.Setenv(semble.EnvEnableSemble, "true")
	testhelpers.SetupGlobalLiza(t)
	projectRoot := t.TempDir()
	gitInit(t, projectRoot)

	specFile := testhelpers.CreateSpecFile(t, projectRoot, "goal.md", "# Test Goal\n")
	var diagnostics []string
	restore := setInitProjectSembleHooksForTest(t,
		func(string) (string, error) {
			t.Fatal("Semble executable lookup called before spec/pre-commit preconditions passed")
			return "", nil
		},
		func(semble.CommandPlan) (semble.CommandResult, error) {
			t.Fatal("Semble command runner called before spec/pre-commit preconditions passed")
			return semble.CommandResult{}, nil
		},
	)
	defer restore()

	err := InitProject(projectRoot, InitProjectParams{
		Description: "Test project",
		SpecRef:     specFile,
		SembleDiagnosticSink: func(message string) {
			diagnostics = append(diagnostics, message)
		},
	})
	if err == nil {
		t.Fatal("InitProject() succeeded with an uncommitted spec")
	}
	if !strings.Contains(err.Error(), "spec file") || !strings.Contains(err.Error(), "commit") {
		t.Fatalf("InitProject() error = %v, want spec commit precondition", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("Semble diagnostics = %#v, want none", diagnostics)
	}
	if _, statErr := os.Stat(filepath.Join(projectRoot, ".liza")); !os.IsNotExist(statErr) {
		t.Fatalf(".liza directory state after failed init = %v, want not exist", statErr)
	}
}

func TestInitProject_SembleEnabledRunsPrewarmAndOfflineValidationBeforeLizaCreation(t *testing.T) {
	t.Setenv(semble.EnvEnableSemble, "true")
	t.Setenv("SEMBLE_MODEL_NAME", t.Name())
	projectRoot, specFile := setupInitTestDir(t)
	executablePath := fakeSembleExecutable(t)
	var runnerCalls []string
	var lookupCalls []string
	restore := setInitProjectSembleHooksForTest(t,
		func(name string) (string, error) {
			lookupCalls = append(lookupCalls, name)
			return executablePath, nil
		},
		func(plan semble.CommandPlan) (semble.CommandResult, error) {
			if _, statErr := os.Stat(filepath.Join(projectRoot, ".liza")); !os.IsNotExist(statErr) {
				t.Fatalf(".liza state during Semble validation = %v, want not exist", statErr)
			}
			if hasOfflineEnv(plan.Env) {
				runnerCalls = append(runnerCalls, "offline")
			} else {
				runnerCalls = append(runnerCalls, "prewarm")
			}
			return semble.CommandResult{ExitCode: 0}, nil
		},
	)
	defer restore()

	var diagnostics []string
	err := InitProject(projectRoot, InitProjectParams{
		Description: "Test project",
		SpecRef:     specFile,
		SembleDiagnosticSink: func(message string) {
			diagnostics = append(diagnostics, message)
		},
	})
	if err != nil {
		t.Fatalf("InitProject() error: %v", err)
	}
	if !reflect.DeepEqual(runnerCalls, []string{"prewarm", "offline"}) {
		t.Fatalf("Semble runner calls = %#v, want prewarm then offline", runnerCalls)
	}
	if !reflect.DeepEqual(lookupCalls, []string{"semble", "semble"}) {
		t.Fatalf("Semble lookup calls = %#v, want two semble lookups", lookupCalls)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("Semble diagnostics = %#v, want none", diagnostics)
	}
	assertNoSembleState(t, projectRoot)
}

func TestInitProject_SembleDiagnosticsAreTransientNonFatalAndSilent(t *testing.T) {
	tests := []struct {
		name        string
		lookPath    func(t *testing.T) semble.ExecutableLookup
		runner      semble.CommandRunner
		wantMessage string
	}{
		{
			name: "missing executable",
			lookPath: func(t *testing.T) semble.ExecutableLookup {
				t.Helper()
				return func(string) (string, error) {
					return "", exec.ErrNotFound
				}
			},
			runner: func(semble.CommandPlan) (semble.CommandResult, error) {
				t.Fatal("runner called for missing executable")
				return semble.CommandResult{}, nil
			},
			wantMessage: "semble executable not found",
		},
		{
			name: "offline model unavailable",
			lookPath: func(t *testing.T) semble.ExecutableLookup {
				t.Helper()
				executablePath := fakeSembleExecutable(t)
				return func(string) (string, error) {
					return executablePath, nil
				}
			},
			runner: func(plan semble.CommandPlan) (semble.CommandResult, error) {
				if hasOfflineEnv(plan.Env) {
					return semble.CommandResult{ExitCode: 1, Stderr: "HF_HUB_OFFLINE localentrynotfounderror model not in cache"}, nil
				}
				return semble.CommandResult{ExitCode: 0}, nil
			},
			wantMessage: "semble: model unavailable offline",
		},
		{
			name: "generic execution failure",
			lookPath: func(t *testing.T) semble.ExecutableLookup {
				t.Helper()
				executablePath := fakeSembleExecutable(t)
				return func(string) (string, error) {
					return executablePath, nil
				}
			},
			runner: func(semble.CommandPlan) (semble.CommandResult, error) {
				return semble.CommandResult{ExitCode: 2, Stderr: "raw output should be bounded"}, errors.New("runner failed")
			},
			wantMessage: "semble: execution failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(semble.EnvEnableSemble, "true")
			t.Setenv("SEMBLE_MODEL_NAME", t.Name())
			projectRoot, specFile := setupInitTestDir(t)
			restore := setInitProjectSembleHooksForTest(t, tt.lookPath(t), tt.runner)
			defer restore()

			var diagnostics []string
			stdout, stderr, err := captureInitProjectOutput(t, func() error {
				return InitProject(projectRoot, InitProjectParams{
					Description: "Test project",
					SpecRef:     specFile,
					SembleDiagnosticSink: func(message string) {
						diagnostics = append(diagnostics, message)
					},
				})
			})
			if err != nil {
				t.Fatalf("InitProject() error: %v", err)
			}
			if stdout != "" || stderr != "" {
				t.Fatalf("InitProject wrote stdout=%q stderr=%q, want no terminal output", stdout, stderr)
			}
			if len(diagnostics) != 1 {
				t.Fatalf("Semble diagnostics = %#v, want exactly one", diagnostics)
			}
			if !strings.Contains(diagnostics[0], tt.wantMessage) {
				t.Fatalf("Semble diagnostic = %q, want to contain %q", diagnostics[0], tt.wantMessage)
			}
			if len(diagnostics[0]) > 1024 {
				t.Fatalf("Semble diagnostic length = %d, want bounded <= 1024", len(diagnostics[0]))
			}
			if _, statErr := os.Stat(filepath.Join(projectRoot, ".liza")); statErr != nil {
				t.Fatalf(".liza after non-fatal Semble diagnostic = %v, want created", statErr)
			}
			assertNoSembleState(t, projectRoot)
		})
	}
}

func TestInitProject_AlreadyExists(t *testing.T) {
	projectRoot, specFile := setupInitTestDir(t)

	// Pre-create .liza directory
	lizaDir := filepath.Join(projectRoot, ".liza")
	if err := os.MkdirAll(lizaDir, 0755); err != nil {
		t.Fatalf("Failed to create .liza dir: %v", err)
	}

	err := InitProject(projectRoot, InitProjectParams{
		Description: "Test project",
		SpecRef:     specFile,
	})
	if err == nil {
		t.Fatal("Expected error when .liza already exists")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("Error = %q, want to contain 'already exists'", err.Error())
	}
}

func TestInitProject_WorktreesAlreadyExist(t *testing.T) {
	projectRoot, specFile := setupInitTestDir(t)
	worktreesDir := filepath.Join(projectRoot, paths.WorktreesDirName)
	if err := os.Mkdir(worktreesDir, 0755); err != nil {
		t.Fatalf("create worktrees directory: %v", err)
	}

	err := InitProject(projectRoot, InitProjectParams{
		Description: "Test project",
		SpecRef:     specFile,
	})
	if err == nil {
		t.Fatal("InitProject() succeeded with an existing worktrees directory")
	}
	if !strings.Contains(err.Error(), "workspace data already exists") {
		t.Fatalf("InitProject() error = %q, want cleanup precondition", err.Error())
	}
	if _, statErr := os.Stat(worktreesDir); statErr != nil {
		t.Fatalf("InitProject() changed existing worktrees directory: %v", statErr)
	}
}

func TestInitProject_MissingSpecFile(t *testing.T) {
	projectRoot, _ := setupInitTestDir(t)

	err := InitProject(projectRoot, InitProjectParams{
		Description: "Test project",
		SpecRef:     filepath.Join(projectRoot, "specs", "nonexistent.md"),
	})
	if err == nil {
		t.Fatal("Expected error when spec file is missing")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("Error = %q, want to contain 'does not exist'", err.Error())
	}
}

func TestInitProject_EmptyBranchDefaultsToIntegration(t *testing.T) {
	projectRoot, specFile := setupInitTestDir(t)

	err := InitProject(projectRoot, InitProjectParams{
		Description: "Test project",
		SpecRef:     specFile,
		Branch:      "", // empty should default to "integration"
	})
	if err != nil {
		t.Fatalf("InitProject() error: %v", err)
	}

	statePath := filepath.Join(projectRoot, ".liza", "state.yaml")
	bb := db.For(statePath)
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if state.Config.IntegrationBranch != "integration" {
		t.Errorf("IntegrationBranch = %q, want %q", state.Config.IntegrationBranch, "integration")
	}
}

func TestInitProject_InvalidEntryPoint(t *testing.T) {
	projectRoot, specFile := setupInitTestDir(t)

	err := InitProject(projectRoot, InitProjectParams{
		Description: "Test project",
		SpecRef:     specFile,
		EntryPoint:  "nonexistent-entry-point",
	})
	if err == nil {
		t.Fatal("Expected error for invalid entry-point")
	}
	if !strings.Contains(err.Error(), "not found in pipeline config") {
		t.Errorf("Error = %q, want to contain 'not found in pipeline config'", err.Error())
	}
}

func TestInitProject_StateReadableAfterSuccess(t *testing.T) {
	projectRoot, specFile := setupInitTestDir(t)

	err := InitProject(projectRoot, InitProjectParams{
		Description: "Test project",
		SpecRef:     specFile,
	})
	if err != nil {
		t.Fatalf("InitProject() error: %v", err)
	}

	// Use db.For() as specified in done_when
	statePath := filepath.Join(projectRoot, ".liza", "state.yaml")
	bb := db.For(statePath)
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("db.For().Read() error: %v", err)
	}
	if state.Goal.Status != models.GoalStatusInProgress {
		t.Errorf("GoalStatus = %v, want IN_PROGRESS", state.Goal.Status)
	}
	if state.Config.Mode != models.SystemModeRunning {
		t.Errorf("Mode = %v, want RUNNING", state.Config.Mode)
	}
}

func TestInitProject_NoCommitsDoesNotLeaveMissingIntegrationBranch(t *testing.T) {
	projectRoot, specFile := setupInitTestDirNoCommit(t)

	err := InitProject(projectRoot, InitProjectParams{
		Description: "Test project",
		SpecRef:     specFile,
	})
	if err != nil {
		if !strings.Contains(err.Error(), "commit") &&
			!strings.Contains(err.Error(), "HEAD") &&
			!strings.Contains(err.Error(), "integration branch") {
			t.Fatalf("InitProject() error = %v, want no error with integration branch created or a clear no-commits failure", err)
		}
		return
	}

	out, branchErr := exec.Command("git", "-C", projectRoot, "branch", "--list", "integration").CombinedOutput()
	if branchErr != nil {
		t.Fatalf("git branch --list integration failed: %v\n%s", branchErr, out)
	}
	if strings.TrimSpace(string(out)) == "" {
		t.Fatal("InitProject() succeeded in a no-commit repo but did not create the integration branch")
	}
}

func TestInitProject_UncommittedSpecFailsBeforeArtifacts(t *testing.T) {
	testhelpers.SetupGlobalLiza(t)
	projectRoot := t.TempDir()
	gitInit(t, projectRoot)

	specFile := testhelpers.CreateSpecFile(t, projectRoot, "goal.md", "# Test Goal\n")

	err := InitProject(projectRoot, InitProjectParams{
		Description: "Test project",
		SpecRef:     specFile,
	})
	if err == nil {
		t.Fatal("InitProject() succeeded with an uncommitted spec")
	}
	if !strings.Contains(err.Error(), "spec file") || !strings.Contains(err.Error(), "commit") {
		t.Fatalf("InitProject() error = %v, want spec commit precondition", err)
	}
	if _, statErr := os.Stat(filepath.Join(projectRoot, ".liza")); !os.IsNotExist(statErr) {
		t.Fatalf(".liza directory state after failed init = %v, want not exist", statErr)
	}
}

func TestInitProject_MissingPreCommitConfigFailsBeforeArtifacts(t *testing.T) {
	testhelpers.SetupGlobalLiza(t)
	projectRoot := t.TempDir()
	gitInit(t, projectRoot)

	specFile := testhelpers.CreateCommittedSpecFile(t, projectRoot, "goal.md", "# Test Goal\n")

	err := InitProject(projectRoot, InitProjectParams{
		Description: "Test project",
		SpecRef:     specFile,
	})
	if err == nil {
		t.Fatal("InitProject() succeeded without a pre-commit config")
	}
	if !strings.Contains(err.Error(), "pre-commit config") {
		t.Fatalf("InitProject() error = %v, want pre-commit config precondition", err)
	}
	if _, statErr := os.Stat(filepath.Join(projectRoot, ".liza")); !os.IsNotExist(statErr) {
		t.Fatalf(".liza directory state after failed init = %v, want not exist", statErr)
	}
}

func TestInitProject_PreCommitConfigMustBeClean(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, projectRoot string)
	}{
		{
			name: "staged modification",
			setup: func(t *testing.T, projectRoot string) {
				configPath := filepath.Join(projectRoot, ".pre-commit-config.yaml")
				if err := os.WriteFile(configPath, []byte("repos:\n  - repo: local\n"), 0644); err != nil {
					t.Fatal(err)
				}
				cmd := exec.Command("git", "-C", projectRoot, "add", ".pre-commit-config.yaml")
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("git add pre-commit config failed: %v\n%s", err, out)
				}
			},
		},
		{
			name: "unstaged modification",
			setup: func(t *testing.T, projectRoot string) {
				configPath := filepath.Join(projectRoot, ".pre-commit-config.yaml")
				if err := os.WriteFile(configPath, []byte("repos:\n  - repo: local\n"), 0644); err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectRoot, specFile := setupInitTestDir(t)
			tt.setup(t, projectRoot)

			err := InitProject(projectRoot, InitProjectParams{
				Description: "Test project",
				SpecRef:     specFile,
			})
			if err == nil {
				t.Fatal("InitProject() succeeded with dirty pre-commit config")
			}
			if !strings.Contains(err.Error(), "pre-commit config") || !strings.Contains(err.Error(), "changes") {
				t.Fatalf("InitProject() error = %v, want dirty pre-commit config precondition", err)
			}
			if _, statErr := os.Stat(filepath.Join(projectRoot, ".liza")); !os.IsNotExist(statErr) {
				t.Fatalf(".liza directory state after failed init = %v, want not exist", statErr)
			}
		})
	}
}

func TestInitProject_CustomBranch(t *testing.T) {
	projectRoot, specFile := setupInitTestDir(t)

	err := InitProject(projectRoot, InitProjectParams{
		Description: "Test project",
		SpecRef:     specFile,
		Branch:      "develop",
	})
	if err != nil {
		t.Fatalf("InitProject() error: %v", err)
	}

	statePath := filepath.Join(projectRoot, ".liza", "state.yaml")
	bb := db.For(statePath)
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if state.Config.IntegrationBranch != "develop" {
		t.Errorf("IntegrationBranch = %q, want %q", state.Config.IntegrationBranch, "develop")
	}
}

func TestInitProject_AutoResumeFlag(t *testing.T) {
	projectRoot, specFile := setupInitTestDir(t)

	err := InitProject(projectRoot, InitProjectParams{
		Description: "Test project",
		SpecRef:     specFile,
		AutoResume:  true,
	})
	if err != nil {
		t.Fatalf("InitProject() error: %v", err)
	}

	statePath := filepath.Join(projectRoot, ".liza", "state.yaml")
	bb := db.For(statePath)
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if !state.Config.AutoResume {
		t.Error("AutoResume = false, want true")
	}
}

func TestInitProject_NoFollowUpFlag(t *testing.T) {
	projectRoot, specFile := setupInitTestDir(t)

	err := InitProject(projectRoot, InitProjectParams{
		Description: "Test project",
		SpecRef:     specFile,
		NoFollowUp:  true,
	})
	if err != nil {
		t.Fatalf("InitProject() error: %v", err)
	}

	statePath := filepath.Join(projectRoot, ".liza", "state.yaml")
	bb := db.For(statePath)
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if !state.Config.NoFollowUp {
		t.Error("NoFollowUp = false, want true")
	}
}

func TestInitProject_DefaultCLI(t *testing.T) {
	projectRoot, specFile := setupInitTestDir(t)

	err := InitProject(projectRoot, InitProjectParams{
		Description: "Test project",
		SpecRef:     specFile,
		DefaultCLI:  "codex",
	})
	if err != nil {
		t.Fatalf("InitProject() error: %v", err)
	}

	statePath := filepath.Join(projectRoot, ".liza", "state.yaml")
	bb := db.For(statePath)
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if state.Config.DefaultCLI != "codex" {
		t.Errorf("DefaultCLI = %q, want %q", state.Config.DefaultCLI, "codex")
	}
}

func TestInitProject_RoleSpecificDefaultCLIs(t *testing.T) {
	projectRoot, specFile := setupInitTestDir(t)

	err := InitProject(projectRoot, InitProjectParams{
		Description:        "Test project",
		SpecRef:            specFile,
		DefaultDoerCLI:     "codex",
		DefaultReviewerCLI: "gemini",
	})
	if err != nil {
		t.Fatalf("InitProject() error: %v", err)
	}

	statePath := filepath.Join(projectRoot, ".liza", "state.yaml")
	bb := db.For(statePath)
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if state.Config.DefaultDoerCLI != "codex" {
		t.Errorf("DefaultDoerCLI = %q, want %q", state.Config.DefaultDoerCLI, "codex")
	}
	if state.Config.DefaultReviewerCLI != "gemini" {
		t.Errorf("DefaultReviewerCLI = %q, want %q", state.Config.DefaultReviewerCLI, "gemini")
	}
}

func TestInitProject_DefaultCLIEmpty(t *testing.T) {
	projectRoot, specFile := setupInitTestDir(t)

	err := InitProject(projectRoot, InitProjectParams{
		Description: "Test project",
		SpecRef:     specFile,
	})
	if err != nil {
		t.Fatalf("InitProject() error: %v", err)
	}

	statePath := filepath.Join(projectRoot, ".liza", "state.yaml")
	bb := db.For(statePath)
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if state.Config.DefaultCLI != "" {
		t.Errorf("DefaultCLI = %q, want empty", state.Config.DefaultCLI)
	}
	if state.Config.DefaultDoerCLI != "" {
		t.Errorf("DefaultDoerCLI = %q, want empty", state.Config.DefaultDoerCLI)
	}
	if state.Config.DefaultReviewerCLI != "" {
		t.Errorf("DefaultReviewerCLI = %q, want empty", state.Config.DefaultReviewerCLI)
	}

	// Verify omitempty
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("Failed to read state.yaml: %v", err)
	}
	if strings.Contains(string(data), "default_cli") {
		t.Error("state.yaml contains default_cli, want omitted when empty")
	}
	if strings.Contains(string(data), "default_doer_cli") {
		t.Error("state.yaml contains default_doer_cli, want omitted when empty")
	}
	if strings.Contains(string(data), "default_reviewer_cli") {
		t.Error("state.yaml contains default_reviewer_cli, want omitted when empty")
	}
}

func setInitProjectSembleHooksForTest(t *testing.T, lookPath semble.ExecutableLookup, runner semble.CommandRunner) func() {
	t.Helper()
	previousLookPath := initProjectSembleLookPath
	previousRunner := initProjectSembleRunner
	initProjectSembleLookPath = lookPath
	initProjectSembleRunner = runner
	return func() {
		initProjectSembleLookPath = previousLookPath
		initProjectSembleRunner = previousRunner
	}
}

func unsetEnvForInitProjectTest(t *testing.T, name string) {
	t.Helper()
	previous, hadPrevious := os.LookupEnv(name)
	if err := os.Unsetenv(name); err != nil {
		t.Fatalf("unset %s: %v", name, err)
	}
	t.Cleanup(func() {
		if hadPrevious {
			_ = os.Setenv(name, previous)
		} else {
			_ = os.Unsetenv(name)
		}
	})
}

func hasOfflineEnv(vars []semble.EnvVar) bool {
	for _, env := range vars {
		if env.Name == "HF_HUB_OFFLINE" && env.Value == "1" {
			return true
		}
	}
	return false
}

func fakeSembleExecutable(t *testing.T) string {
	t.Helper()
	executablePath := filepath.Join(t.TempDir(), "semble")
	if err := os.WriteFile(executablePath, []byte("fake Semble executable\n"), 0o755); err != nil {
		t.Fatalf("write fake Semble executable: %v", err)
	}
	return executablePath
}

func captureInitProjectOutput(t *testing.T, fn func() error) (string, string, error) {
	t.Helper()
	originalStdout := os.Stdout
	originalStderr := os.Stderr
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stderr: %v", err)
	}
	os.Stdout = stdoutWriter
	os.Stderr = stderrWriter
	defer func() {
		os.Stdout = originalStdout
		os.Stderr = originalStderr
	}()
	callErr := fn()
	_ = stdoutWriter.Close()
	_ = stderrWriter.Close()
	stdoutBytes, readStdoutErr := io.ReadAll(stdoutReader)
	stderrBytes, readStderrErr := io.ReadAll(stderrReader)
	if readStdoutErr != nil {
		t.Fatalf("read stdout: %v", readStdoutErr)
	}
	if readStderrErr != nil {
		t.Fatalf("read stderr: %v", readStderrErr)
	}
	return string(stdoutBytes), string(stderrBytes), callErr
}

func assertNoSembleState(t *testing.T, projectRoot string) {
	t.Helper()
	statePath := filepath.Join(projectRoot, ".liza", "state.yaml")
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("Failed to read state.yaml: %v", err)
	}
	if strings.Contains(strings.ToLower(string(data)), "semble") {
		t.Fatalf("state.yaml contains Semble data, want no durable Semble state:\n%s", data)
	}
	if _, ok := reflect.TypeOf(models.Config{}).FieldByName("Semble"); ok {
		t.Fatal("models.Config contains Semble field, want environment-only activation")
	}
}
