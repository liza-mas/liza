package commands

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"

	bashpolicycli "github.com/liza-mas/liza/internal/bash-policy-cli"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/functionalclusters"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pairingindex"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/providers"
	"github.com/liza-mas/liza/internal/scipsearch"
	"github.com/liza-mas/liza/internal/semble"
	"github.com/liza-mas/liza/internal/stacklit"
	"github.com/liza-mas/liza/internal/testhelpers"
	"gopkg.in/yaml.v3"
)

// setupGlobalLiza delegates to testhelpers.SetupGlobalLiza.
func setupGlobalLiza(t *testing.T) string {
	fakeHome := testhelpers.SetupGlobalLiza(t)
	unsetEnvForTest(t, stacklit.EnvEnableStacklit)
	unsetEnvForTest(t, scipsearch.EnvEnableScipSearch)
	unsetEnvForTest(t, functionalclusters.EnvEnableFunctionalClusters)
	unsetEnvForTest(t, semble.EnvEnableSemble)
	unsetEnvForTest(t, bashpolicycli.EnvEnableBashPolicy)
	unsetEnvForTest(t, "CLAUDE_CONFIG_DIR")
	unsetEnvForTest(t, "CODEX_HOME")
	unsetEnvForTest(t, "XDG_CONFIG_HOME")
	unsetEnvForTest(t, "QWEN_HOME")
	return fakeHome
}

func writeOldCachedCursorProviderCatalog(t *testing.T, home string) {
	t.Helper()
	cachePath, metaPath := providers.CachePaths(home)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
		t.Fatal(err)
	}
	catalog := `version: 1
providers:
  - id: cursor-acp
    display_name: Cursor ACP
    backend: acpx
    detection:
      binaries: [cursor-agent]
      version_args: [--version]
    setup:
      contract:
        repo_file: AGENTS.md
        global_fallback: .codex/AGENTS.md
    runtime:
      provider_key: cursor
      executable: acpx
      prompt_transport: stdin
      required_executables: [acpx, cursor-agent]
      contract_key: codex
      acpx_agent: cursor
      acpx_session_name: liza-{{agentID}}
      acpx_show_args: [--cwd, "{{projectRoot}}", "{{acpxAgent}}", sessions, show, --name, "{{sessionName}}"]
      acpx_ensure_args: [--cwd, "{{projectRoot}}", "{{acpxAgent}}", sessions, ensure, --name, "{{sessionName}}"]
      acpx_prompt_args: [--cwd, "{{projectRoot}}", --format, json, --approve-all, "{{acpxAgent}}", prompt, -s, "{{sessionName}}", --file, "-"]
      acpx_event_mode: json
`
	if err := os.WriteFile(cachePath, []byte(catalog), 0644); err != nil {
		t.Fatal(err)
	}
	meta, err := json.Marshal(providers.CacheMeta{
		URL:       providers.DefaultCatalogURL,
		FetchedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metaPath, append(meta, '\n'), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestCanonicalInitProviderIDs_CursorExpansion(t *testing.T) {
	tests := []struct {
		name string
		ids  []string
		want []string
	}{
		{
			name: "cursor convenience flag expands dependencies",
			ids:  []string{"cursor"},
			want: []string{"claude", "codex", "cursor-acp"},
		},
		{
			name: "cursor-acp catalog provider stays exact",
			ids:  []string{"cursor-acp"},
			want: []string{"cursor-acp"},
		},
		{
			name: "cursor expansion deduplicates explicit dependencies",
			ids:  []string{"claude", "cursor", "codex"},
			want: []string{"claude", "codex", "cursor-acp"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := canonicalInitProviderIDs(tt.ids)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("canonicalInitProviderIDs(%v) = %v, want %v", tt.ids, got, tt.want)
			}
		})
	}
}

func TestInitCommand(t *testing.T) {
	tests := []struct {
		name        string
		description string
		specRef     string
		setup       func(t *testing.T, tmpDir string)
		stdin       io.Reader
		skipGlobal  bool // if true, don't set up global liza
		wantErr     bool
		errContains string
	}{
		{
			name:        "successful initialization",
			description: "Test goal",
			specRef:     "specs/vision.md",
			setup: func(t *testing.T, tmpDir string) {
				testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")
			},
			wantErr: false,
		},
		{
			name:        "runtime directory removal declined",
			description: "Test goal",
			specRef:     "specs/vision.md",
			setup: func(t *testing.T, tmpDir string) {
				testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")
				// Create the project runtime directory.
				lizaDir := paths.New(tmpDir).LizaDir()
				if err := os.Mkdir(lizaDir, 0755); err != nil {
					t.Fatal(err)
				}
			},
			stdin:       strings.NewReader("n\n"),
			wantErr:     true,
			errContains: "initialization cancelled by user",
		},
		{
			name:        "spec file does not exist",
			description: "Test goal",
			specRef:     "specs/vision.md",
			setup:       func(t *testing.T, tmpDir string) {}, // No spec file
			wantErr:     true,
			errContains: "spec file does not exist",
		},
		{
			name:        "global config not found",
			description: "Test goal",
			specRef:     "specs/vision.md",
			skipGlobal:  true,
			setup: func(t *testing.T, tmpDir string) {
				testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")
			},
			wantErr:     true,
			errContains: "Run '" + brand.BinaryName + " setup' first",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary git repo
			tmpDir := setupGitRepo(t)
			defer os.RemoveAll(tmpDir)

			// Set up global liza unless test skips it
			if !tt.skipGlobal {
				setupGlobalLiza(t)
			} else {
				// Point HOME to an empty dir so global check fails
				emptyHome := t.TempDir()
				t.Setenv("HOME", emptyHome)
			}

			// Change to temp directory
			originalDir, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			defer os.Chdir(originalDir)
			if err := os.Chdir(tmpDir); err != nil {
				t.Fatal(err)
			}

			// Run setup
			tt.setup(t, tmpDir)

			// Run init command
			err = InitCommand(tt.description, tt.specRef, tt.stdin)

			// Check error
			if (err != nil) != tt.wantErr {
				t.Errorf("InitCommand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && tt.errContains != "" {
				testhelpers.AssertErrorContains(t, err, tt.errContains)
				return
			}

			// If no error expected, verify the initialization
			if !tt.wantErr {
				verifyInitialization(t, tmpDir, tt.description, tt.specRef)
			}
		})
	}
}

func TestGlobalIntegrationGenerationLimitDefaults(t *testing.T) {
	tests := []struct {
		name  string
		limit int
		want  int
	}{
		{name: "zero defaults", limit: 0, want: 3},
		{name: "negative defaults", limit: -1, want: 3},
		{name: "positive is preserved", limit: 7, want: 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := setupGitRepo(t)
			defer os.RemoveAll(tmpDir)

			setupGlobalLiza(t)

			originalDir, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			defer os.Chdir(originalDir)
			if err := os.Chdir(tmpDir); err != nil {
				t.Fatal(err)
			}

			testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")
			if err := InitCommandWithConfig(InitParams{
				Description:                     "Test goal",
				SpecRef:                         "specs/vision.md",
				MaxGlobalIntegrationGenerations: tt.limit,
			}); err != nil {
				t.Fatalf("InitCommandWithConfig() error = %v", err)
			}

			statePath := filepath.Join(paths.New(tmpDir).LizaDir(), "state.yaml")
			state, err := db.For(statePath).Read()
			if err != nil {
				t.Fatalf("read initialized state: %v", err)
			}
			if got := state.Config.MaxGlobalIntegrationGenerations; got != tt.want {
				t.Fatalf("state.Config.MaxGlobalIntegrationGenerations = %d, want %d", got, tt.want)
			}

			data, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatalf("read persisted state: %v", err)
			}
			var persisted struct {
				Config struct {
					MaxGlobalIntegrationGenerations int `yaml:"max_global_integration_generations"`
				} `yaml:"config"`
			}
			if err := yaml.Unmarshal(data, &persisted); err != nil {
				t.Fatalf("unmarshal persisted state: %v", err)
			}
			if got := persisted.Config.MaxGlobalIntegrationGenerations; got != state.Config.MaxGlobalIntegrationGenerations {
				t.Fatalf("persisted max_global_integration_generations = %d, typed state = %d", got, state.Config.MaxGlobalIntegrationGenerations)
			}
		})
	}
}

func TestInitCommandDirectoryStructure(t *testing.T) {
	tmpDir := setupGitRepo(t)
	defer os.RemoveAll(tmpDir)

	setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	// Run init
	if err := InitCommand("Test goal", "specs/vision.md", nil); err != nil {
		t.Fatalf("InitCommand() error = %v", err)
	}

	// Verify directory structure
	lizaDir := paths.New(tmpDir).LizaDir()
	expectedDirs := []string{
		lizaDir,
		filepath.Join(lizaDir, "archive"),
	}

	for _, dir := range expectedDirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			t.Errorf("directory %s was not created", dir)
		}
	}

	// Verify files
	expectedFiles := []string{
		filepath.Join(lizaDir, "state.yaml"),
		filepath.Join(lizaDir, "log.yaml"),
		filepath.Join(lizaDir, "alerts.log"),
		filepath.Join(lizaDir, "state.yaml.lock"),
	}

	for _, file := range expectedFiles {
		if _, err := os.Stat(file); os.IsNotExist(err) {
			t.Errorf("file %s was not created", file)
		}
	}

	// Verify GUARDRAILS.md template was created at project root
	guardrailsPath := filepath.Join(tmpDir, "GUARDRAILS.md")
	if _, err := os.Stat(guardrailsPath); os.IsNotExist(err) {
		t.Error("GUARDRAILS.md was not created at project root")
	}
}

func TestInitCommandIntegrationBranch(t *testing.T) {
	tmpDir := setupGitRepo(t)
	defer os.RemoveAll(tmpDir)

	setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	// Verify integration branch doesn't exist
	cmd := exec.Command("git", "rev-parse", "--verify", "integration")
	if err := cmd.Run(); err == nil {
		t.Fatal("integration branch already exists before init")
	}

	// Run init
	if err := InitCommand("Test goal", "specs/vision.md", nil); err != nil {
		t.Fatalf("InitCommand() error = %v", err)
	}

	// Verify integration branch was created
	cmd = exec.Command("git", "rev-parse", "--verify", "integration")
	if err := cmd.Run(); err != nil {
		t.Error("integration branch was not created")
	}
}

func TestInitCommandExistingIntegrationBranch(t *testing.T) {
	tmpDir := setupGitRepo(t)
	defer os.RemoveAll(tmpDir)

	setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	cmd := exec.Command("git", "branch", "integration", "HEAD")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to create pre-existing integration branch: %v\n%s", err, output)
	}

	if err := InitCommand("Test goal", "specs/vision.md", nil); err != nil {
		t.Fatalf("InitCommand() error = %v", err)
	}

	cmd = exec.Command("git", "rev-parse", "--verify", "integration")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("integration branch missing after init: %v\n%s", err, output)
	}
}

func TestInitCommandNoCommitsWithUncommittedSpecFailsBeforeArtifacts(t *testing.T) {
	tmpDir := setupGitRepoNoCommit(t)
	defer os.RemoveAll(tmpDir)

	setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	err = InitCommand("Test goal", "specs/vision.md", nil)
	if err == nil {
		t.Fatal("InitCommand() succeeded in a no-commit repo")
	}
	if !strings.Contains(err.Error(), "spec file") || !strings.Contains(err.Error(), "commit") {
		t.Fatalf("InitCommand() error = %v, want spec commit precondition", err)
	}

	if _, err := os.Stat(paths.New(tmpDir).LizaDir()); !os.IsNotExist(err) {
		t.Fatalf("project runtime directory state after failed init = %v, want not exist", err)
	}
	for _, relPath := range []string{
		"GUARDRAILS.md",
		".claude",
		".claudeignore",
	} {
		if _, err := os.Stat(filepath.Join(tmpDir, relPath)); !os.IsNotExist(err) {
			t.Fatalf("%s state after failed init = %v, want not exist", relPath, err)
		}
	}

	cmd := exec.Command("git", "branch", "--list", "integration")
	cmd.Dir = tmpDir
	out, branchErr := cmd.CombinedOutput()
	if branchErr != nil {
		t.Fatalf("git branch --list integration failed: %v\n%s", branchErr, out)
	}
	if strings.TrimSpace(string(out)) != "" {
		t.Fatalf("integration branch exists after failed init: %s", out)
	}
}

func TestIntegrationBranchNeedsCreateNoCommits(t *testing.T) {
	tmpDir := setupGitRepoNoCommit(t)
	defer os.RemoveAll(tmpDir)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	_, err = integrationBranchNeedsCreate("integration")
	if err == nil {
		t.Fatal("integrationBranchNeedsCreate() succeeded in a no-commit repo")
	}
	if !strings.Contains(err.Error(), "HEAD is unborn") {
		t.Fatalf("integrationBranchNeedsCreate() error = %v, want unborn HEAD error", err)
	}
}

func TestInitCommandSpecMustBeFullyCommitted(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, tmpDir string)
	}{
		{
			name: "untracked spec",
			setup: func(t *testing.T, tmpDir string) {
				testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")
			},
		},
		{
			name: "staged new spec",
			setup: func(t *testing.T, tmpDir string) {
				testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")
				cmd := exec.Command("git", "add", "specs/vision.md")
				cmd.Dir = tmpDir
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("git add spec failed: %v\n%s", err, out)
				}
			},
		},
		{
			name: "staged spec modification",
			setup: func(t *testing.T, tmpDir string) {
				testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")
				specPath := filepath.Join(tmpDir, "specs", "vision.md")
				if err := os.WriteFile(specPath, []byte("# Changed\n"), 0644); err != nil {
					t.Fatal(err)
				}
				cmd := exec.Command("git", "add", "specs/vision.md")
				cmd.Dir = tmpDir
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("git add spec failed: %v\n%s", err, out)
				}
			},
		},
		{
			name: "unstaged spec modification",
			setup: func(t *testing.T, tmpDir string) {
				testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")
				specPath := filepath.Join(tmpDir, "specs", "vision.md")
				if err := os.WriteFile(specPath, []byte("# Changed\n"), 0644); err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := setupGitRepo(t)
			defer os.RemoveAll(tmpDir)

			setupGlobalLiza(t)

			originalDir, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			defer os.Chdir(originalDir)
			if err := os.Chdir(tmpDir); err != nil {
				t.Fatal(err)
			}

			tt.setup(t, tmpDir)

			err = InitCommand("Test goal", "specs/vision.md", nil)
			if err == nil {
				t.Fatal("InitCommand() succeeded with a spec that was not fully committed")
			}
			if !strings.Contains(err.Error(), "spec file") || !strings.Contains(err.Error(), "commit") {
				t.Fatalf("InitCommand() error = %v, want spec commit precondition", err)
			}
			if _, err := os.Stat(paths.New(tmpDir).LizaDir()); !os.IsNotExist(err) {
				t.Fatalf("project runtime directory state after failed init = %v, want not exist", err)
			}

			cmd := exec.Command("git", "branch", "--list", "integration")
			cmd.Dir = tmpDir
			out, branchErr := cmd.CombinedOutput()
			if branchErr != nil {
				t.Fatalf("git branch --list integration failed: %v\n%s", branchErr, out)
			}
			if strings.TrimSpace(string(out)) != "" {
				t.Fatalf("integration branch exists after failed init: %s", out)
			}
		})
	}
}

func TestInitCommandRequiresCommittedPreCommitConfig(t *testing.T) {
	tmpDir := setupGitRepoWithoutPreCommitConfig(t)
	defer os.RemoveAll(tmpDir)

	setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	err = InitCommand("Test goal", "specs/vision.md", nil)
	if err == nil {
		t.Fatal("InitCommand() succeeded without a pre-commit config")
	}
	if !strings.Contains(err.Error(), "pre-commit config") {
		t.Fatalf("InitCommand() error = %v, want pre-commit config precondition", err)
	}
	if _, statErr := os.Stat(paths.New(tmpDir).LizaDir()); !os.IsNotExist(statErr) {
		t.Fatalf("project runtime directory state after failed init = %v, want not exist", statErr)
	}
}

func TestInitCommandRequiresPreCommitConfigOnExistingIntegrationBranch(t *testing.T) {
	tmpDir := setupGitRepoWithoutPreCommitConfig(t)
	defer os.RemoveAll(tmpDir)

	setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")
	cmd := exec.Command("git", "branch", "integration", "HEAD")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to create integration branch without pre-commit config: %v\n%s", err, output)
	}
	testhelpers.CreateCommittedPreCommitConfig(t, tmpDir)

	err = InitCommand("Test goal", "specs/vision.md", nil)
	if err == nil {
		t.Fatal("InitCommand() succeeded when integration branch lacked pre-commit config")
	}
	if !strings.Contains(err.Error(), "pre-commit config") || !strings.Contains(err.Error(), "integration") {
		t.Fatalf("InitCommand() error = %v, want integration pre-commit config precondition", err)
	}
	if _, statErr := os.Stat(paths.New(tmpDir).LizaDir()); !os.IsNotExist(statErr) {
		t.Fatalf("project runtime directory state after failed init = %v, want not exist", statErr)
	}
}

func TestInitCommandPreCommitConfigMustBeClean(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, tmpDir string)
	}{
		{
			name: "staged modification",
			setup: func(t *testing.T, tmpDir string) {
				configPath := filepath.Join(tmpDir, ".pre-commit-config.yaml")
				if err := os.WriteFile(configPath, []byte("repos:\n  - repo: local\n"), 0644); err != nil {
					t.Fatal(err)
				}
				cmd := exec.Command("git", "add", ".pre-commit-config.yaml")
				cmd.Dir = tmpDir
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("git add pre-commit config failed: %v\n%s", err, out)
				}
			},
		},
		{
			name: "unstaged modification",
			setup: func(t *testing.T, tmpDir string) {
				configPath := filepath.Join(tmpDir, ".pre-commit-config.yaml")
				if err := os.WriteFile(configPath, []byte("repos:\n  - repo: local\n"), 0644); err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := setupGitRepo(t)
			defer os.RemoveAll(tmpDir)

			setupGlobalLiza(t)

			originalDir, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			defer os.Chdir(originalDir)
			if err := os.Chdir(tmpDir); err != nil {
				t.Fatal(err)
			}

			testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")
			tt.setup(t, tmpDir)

			err = InitCommand("Test goal", "specs/vision.md", nil)
			if err == nil {
				t.Fatal("InitCommand() succeeded with dirty pre-commit config")
			}
			if !strings.Contains(err.Error(), "pre-commit config") || !strings.Contains(err.Error(), "changes") {
				t.Fatalf("InitCommand() error = %v, want dirty pre-commit config precondition", err)
			}
			if _, statErr := os.Stat(paths.New(tmpDir).LizaDir()); !os.IsNotExist(statErr) {
				t.Fatalf("project runtime directory state after failed init = %v, want not exist", statErr)
			}
		})
	}
}

func TestInitCommandCustomBranch(t *testing.T) {
	tmpDir := setupGitRepo(t)
	defer os.RemoveAll(tmpDir)

	setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	customBranch := "develop"

	// Verify custom branch doesn't exist
	cmd := exec.Command("git", "rev-parse", "--verify", customBranch)
	if err := cmd.Run(); err == nil {
		t.Fatalf("%s branch already exists before init", customBranch)
	}

	// Run init with custom branch
	if err := InitCommandWithConfig(InitParams{
		Description: "Test goal",
		SpecRef:     "specs/vision.md",
		Branch:      customBranch,
	}); err != nil {
		t.Fatalf("InitCommandWithConfig() error = %v", err)
	}

	// Verify custom branch was created
	cmd = exec.Command("git", "rev-parse", "--verify", customBranch)
	if err := cmd.Run(); err != nil {
		t.Errorf("%s branch was not created", customBranch)
	}

	// Verify default "integration" branch was NOT created
	cmd = exec.Command("git", "rev-parse", "--verify", "integration")
	if err := cmd.Run(); err == nil {
		t.Error("default integration branch should not exist when custom branch is used")
	}

	// Verify state.yaml has the custom branch
	state, err := db.For(filepath.Join(paths.ProjectDirName(), "state.yaml")).Read()
	if err != nil {
		t.Fatalf("failed to read state: %v", err)
	}
	if state.Config.IntegrationBranch != customBranch {
		t.Errorf("state.Config.IntegrationBranch = %q, want %q", state.Config.IntegrationBranch, customBranch)
	}
}

func TestInitCommandInvalidBranchName(t *testing.T) {
	tmpDir := setupGitRepo(t)
	defer os.RemoveAll(tmpDir)

	setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	invalidNames := []string{"my branch", "..bad", "refs/heads/", "branch~1"}
	for _, name := range invalidNames {
		t.Run(name, func(t *testing.T) {
			err := InitCommandWithConfig(InitParams{
				Description: "Test goal",
				SpecRef:     "specs/vision.md",
				Branch:      name,
			})
			if err == nil {
				t.Errorf("expected error for invalid branch name %q, got nil", name)
				// Clean up the project runtime directory so the next subtest can run.
				os.RemoveAll(filepath.Join(tmpDir, paths.ProjectDirName()))
			}
			if err != nil && !strings.Contains(err.Error(), "invalid branch name") {
				t.Errorf("expected 'invalid branch name' error, got: %v", err)
			}
		})
	}
}

// Helper functions

func setupGitRepo(t *testing.T) string {
	t.Helper()

	tmpDir := setupGitRepoWithoutPreCommitConfig(t)
	testhelpers.CreateCommittedPreCommitConfig(t, tmpDir)
	return tmpDir
}

func setupGitRepoWithoutPreCommitConfig(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()

	// Resolve symlinks so paths match os.Getwd() on macOS
	// (macOS: /var -> /private/var, but t.TempDir() returns /var/...)
	tmpDir, err := filepath.EvalSymlinks(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Initialize git repo with "main" as default branch
	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	// Configure git
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	// Create initial commit
	readmeFile := filepath.Join(tmpDir, "README.md")
	if err := os.WriteFile(readmeFile, []byte("# Test\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cmd = exec.Command("git", "add", "README.md")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	return tmpDir
}

func setupGitRepoNoCommit(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()

	// Resolve symlinks so paths match os.Getwd() on macOS
	// (macOS: /var -> /private/var, but t.TempDir() returns /var/...)
	tmpDir, err := filepath.EvalSymlinks(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	return tmpDir
}

func verifyInitialization(t *testing.T, tmpDir, description, specRef string) {
	t.Helper()

	lizaDir := paths.New(tmpDir).LizaDir()
	statePath := filepath.Join(lizaDir, "state.yaml")

	// Read state
	bb := db.New(statePath)
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("failed to read state: %v", err)
	}

	// Verify version
	if state.Version != 1 {
		t.Errorf("state.Version = %d, want 1", state.Version)
	}

	// Verify goal
	if state.Goal.Description != description {
		t.Errorf("state.Goal.Description = %q, want %q", state.Goal.Description, description)
	}
	if state.Goal.SpecRef != specRef {
		t.Errorf("state.Goal.SpecRef = %q, want %q", state.Goal.SpecRef, specRef)
	}
	if state.Goal.Status != models.GoalStatusInProgress {
		t.Errorf("state.Goal.Status = %v, want %v", state.Goal.Status, models.GoalStatusInProgress)
	}
	if state.Goal.ID == "" {
		t.Error("state.Goal.ID is empty")
	}
	if state.Goal.Created.IsZero() {
		t.Error("state.Goal.Created is zero")
	}
	if len(state.Goal.AlignmentHistory) == 0 {
		t.Error("state.Goal.AlignmentHistory is empty")
	}

	// Verify tasks is empty
	if len(state.Tasks) != 0 {
		t.Errorf("state.Tasks length = %d, want 0", len(state.Tasks))
	}

	// Verify agents is empty
	if len(state.Agents) != 0 {
		t.Errorf("state.Agents length = %d, want 0", len(state.Agents))
	}

	// Verify sprint
	if state.Sprint.ID != "sprint-1" {
		t.Errorf("state.Sprint.ID = %q, want %q", state.Sprint.ID, "sprint-1")
	}
	if state.Sprint.GoalRef != state.Goal.ID {
		t.Errorf("state.Sprint.GoalRef = %q, want %q", state.Sprint.GoalRef, state.Goal.ID)
	}
	if state.Sprint.Status != models.SprintStatusInProgress {
		t.Errorf("state.Sprint.Status = %v, want %v", state.Sprint.Status, models.SprintStatusInProgress)
	}

	// Verify config
	if state.Config.MaxCoderIterations != 10 {
		t.Errorf("state.Config.MaxCoderIterations = %d, want 10", state.Config.MaxCoderIterations)
	}
	if state.Config.MaxReviewCycles != 5 {
		t.Errorf("state.Config.MaxReviewCycles = %d, want 5", state.Config.MaxReviewCycles)
	}
	if state.Config.IntegrationBranch != "integration" {
		t.Errorf("state.Config.IntegrationBranch = %q, want %q", state.Config.IntegrationBranch, "integration")
	}
	if state.Config.Mode != models.SystemModeRunning {
		t.Errorf("state.Config.Mode = %q, want %q", state.Config.Mode, models.SystemModeRunning)
	}
	if state.Config.DoerMaxWait != models.DefaultDoerMaxWait {
		t.Errorf("state.Config.DoerMaxWait = %d, want %d", state.Config.DoerMaxWait, models.DefaultDoerMaxWait)
	}
	if state.Config.OrchestratorMaxWait != models.DefaultOrchestratorMaxWait {
		t.Errorf("state.Config.OrchestratorMaxWait = %d, want %d", state.Config.OrchestratorMaxWait, models.DefaultOrchestratorMaxWait)
	}
	if state.Config.ReviewerMaxWait != models.DefaultReviewerMaxWait {
		t.Errorf("state.Config.ReviewerMaxWait = %d, want %d", state.Config.ReviewerMaxWait, models.DefaultReviewerMaxWait)
	}

	// Verify circuit breaker
	if state.CircuitBreaker.Status != "OK" {
		t.Errorf("state.CircuitBreaker.Status = %q, want %q", state.CircuitBreaker.Status, "OK")
	}

	// Verify timestamp is recent (within 5 seconds)
	now := time.Now().UTC()
	diff := now.Sub(state.Goal.Created)
	if diff < 0 || diff > 5*time.Second {
		t.Errorf("state.Goal.Created timestamp difference = %v, want < 5s", diff)
	}
}

func TestInitCommand_CreatesContractSymlinks(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	// Create temporary git repo
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)

	fakeHome := setupGlobalLiza(t)

	// Change to temp directory
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(gitDir); err != nil {
		t.Fatal(err)
	}

	// Setup
	testhelpers.CreateCommittedSpecFile(t, gitDir, "vision.md", "# Vision\n")

	// Run init with explicit agent flags
	err = InitCommandWithConfig(InitParams{
		Description: "Test goal",
		SpecRef:     "specs/vision.md",
		Agents:      []string{"claude", "codex", "gemini"},
	})
	if err != nil {
		t.Fatalf("InitCommand failed: %v", err)
	}

	// Verify contract symlinks point to absolute global path
	globalDir := filepath.Join(fakeHome, paths.GlobalDirName())
	expectedTarget := filepath.Join(globalDir, "CORE.md")
	for _, rel := range []string{
		filepath.Join(".claude", "CLAUDE.md"),
		filepath.Join(".codex", "AGENTS.md"),
		filepath.Join(".gemini", "GEMINI.md"),
	} {
		linkPath := filepath.Join(fakeHome, rel)
		target, err := os.Readlink(linkPath)
		if err != nil {
			t.Errorf("Symlink %s not created: %v", rel, err)
			continue
		}
		if target != expectedTarget {
			t.Errorf("Symlink %s target = %q, want %q", rel, target, expectedTarget)
		}
	}

	codexConfig := filepath.Join(fakeHome, ".codex", "config.toml")
	configContent, err := os.ReadFile(codexConfig)
	if err != nil {
		t.Fatalf("Codex config not created for --codex: %v", err)
	}
	for _, want := range []string{gitDir, filepath.Join(gitDir, ".git")} {
		// TOML basic strings escape the backslash, so a native Windows path is
		// written C:\\Users\\... and the raw path never appears verbatim. The
		// replacement is the identity on Unix.
		want = strings.ReplaceAll(want, `\`, `\\`)
		if !strings.Contains(string(configContent), want) {
			t.Errorf("Codex config missing writable root %q:\n%s", want, string(configContent))
		}
	}
	verifyCodexHooks(t, gitDir)
}

func TestInitCommandWithConfig_BashPolicyProviderScope(t *testing.T) {
	tests := []struct {
		name      string
		agents    []string
		providers []string
		wantCodex bool
	}{
		{name: "claude only by default", providers: []string{bashpolicycli.ProviderClaude}},
		{name: "claude and codex when codex selected", agents: []string{"codex"}, providers: []string{bashpolicycli.ProviderClaude, bashpolicycli.ProviderCodex}, wantCodex: true},
		{name: "claude, codex, and cursor when cursor selected", agents: []string{"cursor"}, providers: []string{bashpolicycli.ProviderClaude, bashpolicycli.ProviderCodex, bashpolicycli.ProviderCursor}, wantCodex: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gitDir := setupGitRepo(t)
			defer os.RemoveAll(gitDir)
			setupGlobalLiza(t)
			t.Setenv(bashpolicycli.EnvEnableBashPolicy, "1")

			runner := &initBashPolicyTestRunner{}
			restore := setInitBashPolicyHooksForTest(
				func(name string) (string, error) {
					return filepath.Join(gitDir, "bin", name), nil
				},
				runner,
			)
			defer restore()

			originalDir, _ := os.Getwd()
			defer os.Chdir(originalDir)
			os.Chdir(gitDir)
			testhelpers.CreateCommittedSpecFile(t, gitDir, "vision.md", "# Vision\n")

			if err := InitCommandWithConfig(InitParams{
				Description: "Test goal",
				SpecRef:     "specs/vision.md",
				Agents:      tt.agents,
			}); err != nil {
				t.Fatalf("InitCommandWithConfig() error = %v", err)
			}

			assertBashPolicyCommands(t, runner.commands, gitDir, tt.providers)
			if _, err := os.Stat(filepath.Join(gitDir, ".bash-policy.yaml")); err != nil {
				t.Fatalf(".bash-policy.yaml was not created for full init: %v", err)
			}
			if tt.wantCodex {
				verifyCodexHooks(t, gitDir)
			}
		})
	}
}

func TestInitCommandWithConfig_CursorSkipsBashPolicyWhenGateDisabled(t *testing.T) {
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)
	fakeHome := setupGlobalLiza(t)
	writeOldCachedCursorProviderCatalog(t, fakeHome)

	var lookups int
	runner := &initBashPolicyTestRunner{}
	restore := setInitBashPolicyHooksForTest(
		func(name string) (string, error) {
			lookups++
			return filepath.Join(gitDir, "bin", name), nil
		},
		runner,
	)
	defer restore()

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(gitDir)
	testhelpers.CreateCommittedSpecFile(t, gitDir, "vision.md", "# Vision\n")

	if err := InitCommandWithConfig(InitParams{
		Description: "Test goal",
		SpecRef:     "specs/vision.md",
		Agents:      []string{"cursor"},
	}); err != nil {
		t.Fatalf("InitCommandWithConfig() error = %v", err)
	}

	if lookups != 0 {
		t.Fatalf("bash-policy lookups = %d, want zero when gate disabled", lookups)
	}
	if len(runner.commands) != 0 {
		t.Fatalf("bash-policy commands = %d, want zero when gate disabled", len(runner.commands))
	}
	if _, err := os.Stat(filepath.Join(gitDir, ".bash-policy.yaml")); !os.IsNotExist(err) {
		t.Fatalf(".bash-policy.yaml state = %v, want absent when gate disabled", err)
	}
	verifyCodexHooks(t, gitDir)
}

func TestInitCommandWithConfig_BashPolicyPromptPreservesLaterInput(t *testing.T) {
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)
	setupGlobalLiza(t)
	t.Setenv(bashpolicycli.EnvEnableBashPolicy, "1")

	runner := &initBashPolicyTestRunner{readStdinLine: true}
	restore := setInitBashPolicyHooksForTest(
		func(name string) (string, error) {
			return filepath.Join(gitDir, "bin", name), nil
		},
		runner,
	)
	defer restore()

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(gitDir)
	testhelpers.CreateCommittedSpecFile(t, gitDir, "vision.md", "# Vision\n")

	policyPath := filepath.Join(gitDir, ".bash-policy.yaml")
	originalPolicy := []byte("rules: []\n")
	if err := os.WriteFile(policyPath, originalPolicy, 0644); err != nil {
		t.Fatal(err)
	}
	ignorePath := filepath.Join(gitDir, ".claudeignore")
	originalIgnore := []byte("# existing ignore\n")
	if err := os.WriteFile(ignorePath, originalIgnore, 0644); err != nil {
		t.Fatal(err)
	}

	if err := InitCommandWithConfig(InitParams{
		Description: "Test goal",
		SpecRef:     "specs/vision.md",
		Stdin:       strings.NewReader("n\ny\ny\n"),
	}); err != nil {
		t.Fatalf("InitCommandWithConfig() error = %v", err)
	}

	policyContent, err := os.ReadFile(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(policyContent, originalPolicy) {
		t.Fatalf(".bash-policy.yaml changed despite decline:\n%s", string(policyContent))
	}
	if runner.stdinLine != "y\n" {
		t.Fatalf("bash-policy subprocess stdin = %q, want second answer", runner.stdinLine)
	}
	ignoreContent, err := os.ReadFile(ignorePath)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Equal(ignoreContent, originalIgnore) {
		t.Fatalf(".claudeignore was not overwritten by later prompt")
	}
}

func TestInitCommandWithConfig_MultiProviderBashPolicyUsesSharedBufferedInput(t *testing.T) {
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)
	setupGlobalLiza(t)
	t.Setenv(bashpolicycli.EnvEnableBashPolicy, "1")

	runner := &initBashPolicyTestRunner{readStdinLine: true}
	restore := setInitBashPolicyHooksForTest(
		func(name string) (string, error) {
			return filepath.Join(gitDir, "bin", name), nil
		},
		runner,
	)
	defer restore()

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(gitDir)
	testhelpers.CreateCommittedSpecFile(t, gitDir, "vision.md", "# Vision\n")

	if err := InitCommandWithConfig(InitParams{
		Description: "Test goal",
		SpecRef:     "specs/vision.md",
		Agents:      []string{"codex"},
		Stdin:       strings.NewReader("line-a\nline-b\n"),
	}); err != nil {
		t.Fatalf("InitCommandWithConfig() error = %v", err)
	}

	assertBashPolicyCommands(t, runner.commands, gitDir, []string{bashpolicycli.ProviderClaude, bashpolicycli.ProviderCodex})
	if strings.Join(runner.stdinLines, "") != "line-a\nline-b\n" {
		t.Fatalf("bash-policy subprocess stdin lines = %q", runner.stdinLines)
	}
}

func TestInitCommand_OpenCodeCreatesGlobalContractWithoutCodexHooks(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)

	fakeHome := setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(gitDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, gitDir, "vision.md", "# Vision\n")

	err = InitCommandWithConfig(InitParams{
		Description: "Test goal",
		SpecRef:     "specs/vision.md",
		Agents:      []string{"opencode"},
	})
	if err != nil {
		t.Fatalf("InitCommand failed: %v", err)
	}

	coreFile := filepath.Join(fakeHome, paths.GlobalDirName(), "CORE.md")
	agentsPath := filepath.Join(fakeHome, ".config", "opencode", "AGENTS.md")
	target, err := os.Readlink(agentsPath)
	if err != nil {
		t.Fatalf("global OpenCode AGENTS.md symlink not created: %v", err)
	}
	if target != coreFile {
		t.Errorf("AGENTS.md → %q, want %q", target, coreFile)
	}

	if _, err := os.Stat(filepath.Join(gitDir, ".codex", "hooks.json")); !os.IsNotExist(err) {
		t.Fatalf("OpenCode init should not create Codex hooks, stat error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(fakeHome, ".codex", "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("OpenCode init should not create Codex config, stat error = %v", err)
	}

	execToolPath := filepath.Join(gitDir, ".opencode", "tools", "exec.ts")
	execTool, err := os.ReadFile(execToolPath)
	if err != nil {
		t.Fatalf("OpenCode exec tool not created at %s: %v", execToolPath, err)
	}
	for _, want := range []string{
		brand.NameUpper + " MANAGED FILE",
		"Prefer this exec tool",
		"Do not repeat the same successful command",
	} {
		if !strings.Contains(string(execTool), want) {
			t.Fatalf("OpenCode exec tool missing %q:\n%s", want, string(execTool))
		}
	}
}

func TestInitCommand_OpenCodePreservesUserExecTool(t *testing.T) {
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)

	setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(gitDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, gitDir, "vision.md", "# Vision\n")
	execToolPath := filepath.Join(gitDir, ".opencode", "tools", "exec.ts")
	originalExecTool := "// user OpenCode exec tool\nexport default {}\n"
	if err := os.MkdirAll(filepath.Dir(execToolPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(execToolPath, []byte(originalExecTool), 0644); err != nil {
		t.Fatal(err)
	}

	err = InitCommandWithConfig(InitParams{
		Description: "Test goal",
		SpecRef:     "specs/vision.md",
		Agents:      []string{"opencode"},
	})
	if err != nil {
		t.Fatalf("InitCommand failed: %v", err)
	}

	execTool, err := os.ReadFile(execToolPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(execTool) != originalExecTool {
		t.Fatalf("user OpenCode exec tool was overwritten:\n%s", string(execTool))
	}
}

func TestInitCommand_PreservesManagedRepoSymlinkRecordedForRepoOnlyProvider(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)

	fakeHome := setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(gitDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, gitDir, "vision.md", "# Vision\n")

	// Pre-create CLAUDE.md as the correct symlink (absolute to global)
	globalDir := filepath.Join(fakeHome, paths.GlobalDirName())
	correctTarget := filepath.Join(globalDir, "CORE.md")
	claudePath := filepath.Join(gitDir, "CLAUDE.md")
	if err := os.Symlink(correctTarget, claudePath); err != nil {
		t.Fatal(err)
	}
	statePath, err := repoContractActivationStatePath(gitDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeRepoContractActivationState(statePath, repoContractActivationState{
		Version:       repoContractActivationStateVersion,
		ProviderPaths: map[string]string{"kimi": "CLAUDE.md"},
	}); err != nil {
		t.Fatal(err)
	}

	// Run init with Claude explicitly selected so contract migration is active.
	if err := InitCommandWithConfig(InitParams{
		Description: "Test goal",
		SpecRef:     "specs/vision.md",
		Agents:      []string{"claude"},
	}); err != nil {
		t.Fatalf("InitCommand failed: %v", err)
	}

	if target, err := os.Readlink(claudePath); err != nil || target != correctTarget {
		t.Fatalf("repo CLAUDE.md target = %q, err = %v; want %q for Kimi", target, err, correctTarget)
	}
	target, err := os.Readlink(filepath.Join(fakeHome, ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("global CLAUDE.md is not a symlink: %v", err)
	}
	if target != correctTarget {
		t.Errorf("global CLAUDE.md target = %q, want %q", target, correctTarget)
	}
}

func TestInitCommand_NoProvidersLeavesManagedRepoSymlinkUntouched(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)

	fakeHome := setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(gitDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, gitDir, "vision.md", "# Vision\n")

	correctTarget := filepath.Join(fakeHome, paths.GlobalDirName(), "CORE.md")
	claudePath := filepath.Join(gitDir, "CLAUDE.md")
	if err := os.Symlink(correctTarget, claudePath); err != nil {
		t.Fatal(err)
	}

	if err := InitCommand("Test goal", "specs/vision.md", nil); err != nil {
		t.Fatalf("InitCommand failed: %v", err)
	}

	if target, err := os.Readlink(claudePath); err != nil || target != correctTarget {
		t.Fatalf("repo CLAUDE.md changed; target = %q, err = %v, want %q", target, err, correctTarget)
	}
	if _, err := os.Lstat(filepath.Join(fakeHome, ".claude", "CLAUDE.md")); !os.IsNotExist(err) {
		t.Fatalf("global CLAUDE.md should remain absent without provider selection; got %v", err)
	}
}

func TestInitCommand_BrownfieldFallsBackToGlobal(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)

	fakeHome := setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(gitDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, gitDir, "vision.md", "# Vision\n")

	// Pre-create CLAUDE.md as a regular file (brownfield project)
	existingContent := "# Custom contract\n"
	claudePath := filepath.Join(gitDir, "CLAUDE.md")
	if err := os.WriteFile(claudePath, []byte(existingContent), 0644); err != nil {
		t.Fatal(err)
	}

	err = InitCommandWithConfig(InitParams{
		Description: "Test goal",
		SpecRef:     "specs/vision.md",
		Agents:      []string{"claude", "codex", "gemini"},
	})
	if err != nil {
		t.Fatalf("InitCommand failed: %v", err)
	}

	// CLAUDE.md at repo root should be untouched
	content, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatalf("Failed to read CLAUDE.md: %v", err)
	}
	if string(content) != existingContent {
		t.Errorf("CLAUDE.md was modified; got %q, want %q", string(content), existingContent)
	}

	// CLAUDE.md should have been placed at global fallback (~/.claude/CLAUDE.md)
	globalClaude := filepath.Join(fakeHome, ".claude", "CLAUDE.md")
	target, err := os.Readlink(globalClaude)
	if err != nil {
		t.Fatalf("Global fallback symlink not created at %s: %v", globalClaude, err)
	}
	coreFile := filepath.Join(fakeHome, paths.GlobalDirName(), "CORE.md")
	if target != coreFile {
		t.Errorf("Global fallback → %q, want %q", target, coreFile)
	}

	// Other selected providers also use their preferred global contracts.
	for _, path := range []string{
		filepath.Join(fakeHome, ".codex", "AGENTS.md"),
		filepath.Join(fakeHome, ".gemini", "GEMINI.md"),
	} {
		if target, err := os.Readlink(path); err != nil || target != coreFile {
			t.Errorf("global contract %s target = %q, err = %v; want %q", path, target, err, coreFile)
		}
	}
	for _, name := range []string{"AGENTS.md", "GEMINI.md"} {
		if _, err := os.Lstat(filepath.Join(gitDir, name)); !os.IsNotExist(err) {
			t.Errorf("repo %s should be absent for preferred global activation; got %v", name, err)
		}
	}
}

func TestInitCommand_OpenCodeBrownfieldFallsBackToOpenCodeGlobal(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)

	fakeHome := setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(gitDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, gitDir, "vision.md", "# Vision\n")

	existingContent := "# Existing agents contract\n"
	agentsPath := filepath.Join(gitDir, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte(existingContent), 0644); err != nil {
		t.Fatal(err)
	}

	err = InitCommandWithConfig(InitParams{
		Description: "Test goal",
		SpecRef:     "specs/vision.md",
		Agents:      []string{"opencode"},
	})
	if err != nil {
		t.Fatalf("InitCommand failed: %v", err)
	}

	content, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatalf("Failed to read AGENTS.md: %v", err)
	}
	if string(content) != existingContent {
		t.Errorf("AGENTS.md was modified; got %q, want %q", string(content), existingContent)
	}

	globalOpenCode := filepath.Join(fakeHome, ".config", "opencode", "AGENTS.md")
	target, err := os.Readlink(globalOpenCode)
	if err != nil {
		t.Fatalf("OpenCode global fallback symlink not created at %s: %v", globalOpenCode, err)
	}
	coreFile := filepath.Join(fakeHome, paths.GlobalDirName(), "CORE.md")
	if target != coreFile {
		t.Errorf("OpenCode global fallback → %q, want %q", target, coreFile)
	}
}

func TestInitCommand_CodexAndOpenCodeBrownfieldCreateBothFallbacks(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)

	fakeHome := setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(gitDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, gitDir, "vision.md", "# Vision\n")

	agentsPath := filepath.Join(gitDir, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte("# Existing agents contract\n"), 0644); err != nil {
		t.Fatal(err)
	}

	err = InitCommandWithConfig(InitParams{
		Description: "Test goal",
		SpecRef:     "specs/vision.md",
		Agents:      []string{"codex", "opencode"},
	})
	if err != nil {
		t.Fatalf("InitCommand failed: %v", err)
	}

	coreFile := filepath.Join(fakeHome, paths.GlobalDirName(), "CORE.md")
	for _, path := range []string{
		filepath.Join(fakeHome, ".codex", "AGENTS.md"),
		filepath.Join(fakeHome, ".config", "opencode", "AGENTS.md"),
	} {
		target, err := os.Readlink(path)
		if err != nil {
			t.Fatalf("fallback symlink not created at %s: %v", path, err)
		}
		if target != coreFile {
			t.Errorf("%s → %q, want %q", path, target, coreFile)
		}
	}
}

func TestInitCommand_BrownfieldExistingLizaAtGlobalSkipsCreation(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)

	fakeHome := setupGlobalLiza(t)

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(gitDir)

	testhelpers.CreateCommittedSpecFile(t, gitDir, "vision.md", "# Vision\n")

	coreFile := filepath.Join(fakeHome, paths.GlobalDirName(), "CORE.md")

	// Pre-create Liza symlink at global fallback
	globalClaude := filepath.Join(fakeHome, ".claude", "CLAUDE.md")
	os.MkdirAll(filepath.Dir(globalClaude), 0755)
	os.Symlink(coreFile, globalClaude)

	err := InitCommandWithConfig(InitParams{
		Description: "Test goal",
		SpecRef:     "specs/vision.md",
		Agents:      []string{"claude", "codex", "gemini"},
	})
	if err != nil {
		t.Fatalf("InitCommand failed: %v", err)
	}

	// Repo root should NOT have a CLAUDE.md (global already has it)
	repoClaudePath := filepath.Join(gitDir, "CLAUDE.md")
	if _, err := os.Lstat(repoClaudePath); !os.IsNotExist(err) {
		t.Error("CLAUDE.md should not be created at repo root when global fallback already has Liza symlink")
	}

	// Other selected providers should also activate at their global paths.
	for _, path := range []string{
		filepath.Join(fakeHome, ".codex", "AGENTS.md"),
		filepath.Join(fakeHome, ".gemini", "GEMINI.md"),
	} {
		if target, err := os.Readlink(path); err != nil || target != coreFile {
			t.Errorf("global contract %s target = %q, err = %v; want %q", path, target, err, coreFile)
		}
	}
}

func TestInitCommand_BrownfieldBothOccupiedWarns(t *testing.T) {
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)

	fakeHome := setupGlobalLiza(t)

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(gitDir)

	testhelpers.CreateCommittedSpecFile(t, gitDir, "vision.md", "# Vision\n")

	// CLAUDE.md at repo root (non-Liza)
	os.WriteFile(filepath.Join(gitDir, "CLAUDE.md"), []byte("project"), 0644)

	// CLAUDE.md at global fallback (also non-Liza)
	globalClaude := filepath.Join(fakeHome, ".claude", "CLAUDE.md")
	os.MkdirAll(filepath.Dir(globalClaude), 0755)
	os.WriteFile(globalClaude, []byte("user config"), 0644)

	// Capture stderr
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	err := InitCommandWithConfig(InitParams{
		Description: "Test goal",
		SpecRef:     "specs/vision.md",
		Agents:      []string{"claude"},
	})
	if err != nil {
		w.Close()
		os.Stderr = oldStderr
		t.Fatalf("InitCommand failed: %v", err)
	}
	w.Close()
	stderrBytes, _ := io.ReadAll(r)
	os.Stderr = oldStderr

	// Should warn about both locations being occupied
	stderr := string(stderrBytes)
	if !strings.Contains(stderr, "CLAUDE.md exists at both repo root and") {
		t.Errorf("Expected 'both occupied' warning in stderr, got: %s", stderr)
	}

	// Neither file should be modified
	repoContent, _ := os.ReadFile(filepath.Join(gitDir, "CLAUDE.md"))
	if string(repoContent) != "project" {
		t.Error("Repo root CLAUDE.md was modified")
	}
	globalContent, _ := os.ReadFile(globalClaude)
	if string(globalContent) != "user config" {
		t.Error("Global CLAUDE.md was modified")
	}
}

func TestInitCommand_DuplicateClaudeSymlinkRemovesUnownedRepoCopy(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)

	fakeHome := setupGlobalLiza(t)
	t.Setenv(providers.EnvCatalogURL, "://invalid")

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(gitDir)

	testhelpers.CreateCommittedSpecFile(t, gitDir, "vision.md", "# Vision\n")

	coreFile := filepath.Join(fakeHome, paths.GlobalDirName(), "CORE.md")

	// Liza symlink at both repo root and global
	os.Symlink(coreFile, filepath.Join(gitDir, "CLAUDE.md"))
	globalClaude := filepath.Join(fakeHome, ".claude", "CLAUDE.md")
	os.MkdirAll(filepath.Dir(globalClaude), 0755)
	os.Symlink(coreFile, globalClaude)

	err := InitCommandWithConfig(InitParams{
		Description: "Test goal",
		SpecRef:     "specs/vision.md",
		Agents:      []string{"claude"},
	})
	if err != nil {
		t.Fatalf("InitCommand failed: %v", err)
	}

	if _, err := os.Lstat(filepath.Join(gitDir, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Errorf("repo CLAUDE.md should be removed when no repo-only activation evidence exists; got %v", err)
	}
	if target, err := os.Readlink(globalClaude); err != nil || target != coreFile {
		t.Errorf("global CLAUDE.md changed; target = %q, err = %v", target, err)
	}
}

func TestInitCommand_DuplicateCodexSymlinkPreservesRepoCopyForCursor(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)

	fakeHome := setupGlobalLiza(t)
	t.Setenv(providers.EnvCatalogURL, "://invalid")

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(gitDir)

	testhelpers.CreateCommittedSpecFile(t, gitDir, "vision.md", "# Vision\n")

	coreFile := filepath.Join(fakeHome, paths.GlobalDirName(), "CORE.md")
	repoAgents := filepath.Join(gitDir, "AGENTS.md")
	if err := os.Symlink(coreFile, repoAgents); err != nil {
		t.Fatalf("create repo AGENTS.md symlink: %v", err)
	}
	cursorDir := filepath.Join(gitDir, ".cursor")
	if err := os.MkdirAll(cursorDir, 0755); err != nil {
		t.Fatalf("create Cursor activation directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cursorDir, "hooks.json"), []byte("{}\n"), 0644); err != nil {
		t.Fatalf("create Cursor activation evidence: %v", err)
	}
	globalAgents := filepath.Join(fakeHome, ".codex", "AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(globalAgents), 0755); err != nil {
		t.Fatalf("create global Codex directory: %v", err)
	}
	if err := os.Symlink(coreFile, globalAgents); err != nil {
		t.Fatalf("create global AGENTS.md symlink: %v", err)
	}

	err := InitCommandWithConfig(InitParams{
		Description: "Test goal",
		SpecRef:     "specs/vision.md",
		Agents:      []string{"codex"},
	})
	if err != nil {
		t.Fatalf("InitCommand failed: %v", err)
	}

	if target, err := os.Readlink(repoAgents); err != nil || target != coreFile {
		t.Errorf("repo AGENTS.md target = %q, err = %v; want %q for Cursor", target, err, coreFile)
	}
	if target, err := os.Readlink(globalAgents); err != nil || target != coreFile {
		t.Errorf("global AGENTS.md changed; target = %q, err = %v", target, err)
	}
}

func TestInitPairingCommand_PrefersActiveProviderGlobalRoot(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	tests := []struct {
		name         string
		agent        string
		envName      string
		globalSuffix string
		repoFile     string
		defaultPath  string
	}{
		{name: "claude config dir", agent: "claude", envName: "CLAUDE_CONFIG_DIR", globalSuffix: "CLAUDE.md", repoFile: "CLAUDE.md", defaultPath: ".claude/CLAUDE.md"},
		{name: "codex home", agent: "codex", envName: "CODEX_HOME", globalSuffix: "AGENTS.md", repoFile: "AGENTS.md", defaultPath: ".codex/AGENTS.md"},
		{name: "opencode xdg config", agent: "opencode", envName: "XDG_CONFIG_HOME", globalSuffix: "opencode/AGENTS.md", repoFile: "AGENTS.md", defaultPath: ".config/opencode/AGENTS.md"},
		{name: "qwen home", agent: "qwen", envName: "QWEN_HOME", globalSuffix: "QWEN.md", repoFile: "QWEN.md", defaultPath: ".qwen/QWEN.md"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gitDir := setupGitRepo(t)
			defer os.RemoveAll(gitDir)
			fakeHome := setupGlobalLiza(t)
			activeRoot := t.TempDir()
			t.Setenv(tt.envName, activeRoot)

			originalDir, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			defer os.Chdir(originalDir)
			if err := os.Chdir(gitDir); err != nil {
				t.Fatal(err)
			}

			if err := InitPairingCommand(InitPairingParams{Agents: []string{tt.agent}}); err != nil {
				t.Fatalf("InitPairingCommand() error = %v", err)
			}

			contractTarget := filepath.Join(fakeHome, paths.GlobalDirName(), "CORE.md")
			activePath := filepath.Join(activeRoot, filepath.FromSlash(tt.globalSuffix))
			if target, err := os.Readlink(activePath); err != nil || target != contractTarget {
				t.Fatalf("active global contract target = %q, err = %v; want %q", target, err, contractTarget)
			}
			if _, err := os.Lstat(filepath.Join(gitDir, tt.repoFile)); !os.IsNotExist(err) {
				t.Fatalf("repo %s should be absent after active global setup; got %v", tt.repoFile, err)
			}
			if _, err := os.Lstat(filepath.Join(fakeHome, filepath.FromSlash(tt.defaultPath))); !os.IsNotExist(err) {
				t.Fatalf("inactive default global path should be absent; got %v", err)
			}
		})
	}
}

func TestInitPairingCommand_QwenRelativeHomeRetainsRepoContractAcrossWorkingDirectories(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)
	fakeHome := setupGlobalLiza(t)
	t.Setenv("QWEN_HOME", filepath.Join(".qwen-custom", "global"))

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(gitDir); err != nil {
		t.Fatal(err)
	}

	if err := InitPairingCommand(InitPairingParams{Agents: []string{"qwen"}}); err != nil {
		t.Fatalf("InitPairingCommand() error = %v", err)
	}

	contractTarget := filepath.Join(fakeHome, paths.GlobalDirName(), "CORE.md")
	repoPath := filepath.Join(gitDir, "QWEN.md")
	if target, err := os.Readlink(repoPath); err != nil || target != contractTarget {
		t.Fatalf("repo QWEN.md target = %q, err = %v; want %q", target, err, contractTarget)
	}
	initRelativePath := filepath.Join(gitDir, ".qwen-custom", "global", "QWEN.md")
	if _, err := os.Lstat(initRelativePath); !os.IsNotExist(err) {
		t.Fatalf("init-time relative QWEN_HOME path should remain absent; got %v", err)
	}

	nestedDir := filepath.Join(gitDir, "nested")
	if err := os.MkdirAll(nestedDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(nestedDir); err != nil {
		t.Fatal(err)
	}
	laterRelativePath := filepath.Join(nestedDir, ".qwen-custom", "global", "QWEN.md")
	if _, err := os.Lstat(laterRelativePath); !os.IsNotExist(err) {
		t.Fatalf("later cwd-relative QWEN_HOME path should remain absent; got %v", err)
	}
	if target, err := os.Readlink(repoPath); err != nil || target != contractTarget {
		t.Fatalf("stable repo QWEN.md target = %q, err = %v; want %q", target, err, contractTarget)
	}
}

func TestPreferredGlobalOccupiedRetainsManagedRepoContract(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("CODEX_HOME", "")
	projectRoot := t.TempDir()
	contractTarget := filepath.Join(homeDir, paths.GlobalDirName(), "CORE.md")
	repoPath := filepath.Join(projectRoot, "AGENTS.md")
	globalPath := filepath.Join(homeDir, ".codex", "AGENTS.md")
	if err := os.Symlink(contractTarget, repoPath); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(globalPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(globalPath, []byte("user instructions\n"), 0644); err != nil {
		t.Fatal(err)
	}
	provider, ok := providers.EmbeddedCatalog().Resolve("codex")
	if !ok {
		t.Fatal("embedded Codex provider missing")
	}

	stderr, err := captureStderrForTest(func() error {
		createContractSymlinksForProviders(projectRoot, contractTarget, []providers.Provider{provider}, contractSymlinkOptions{})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr, "retaining repo activation") {
		t.Fatalf("stderr = %q, want safe fallback diagnostic", stderr)
	}
	if target, err := os.Readlink(repoPath); err != nil || target != contractTarget {
		t.Fatalf("repo contract changed; target = %q, err = %v", target, err)
	}
	if content, err := os.ReadFile(globalPath); err != nil || string(content) != "user instructions\n" {
		t.Fatalf("user global file changed; content = %q, err = %v", content, err)
	}
}

func TestInitCommand_GlobalClaudeSymlinkPreservesRepoRegularFile(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)

	fakeHome := setupGlobalLiza(t)
	t.Setenv(providers.EnvCatalogURL, "://invalid")

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(gitDir)

	testhelpers.CreateCommittedSpecFile(t, gitDir, "vision.md", "# Vision\n")

	coreFile := filepath.Join(fakeHome, paths.GlobalDirName(), "CORE.md")
	repoClaude := filepath.Join(gitDir, "CLAUDE.md")
	if err := os.WriteFile(repoClaude, []byte("@AGENTS.md\n"), 0644); err != nil {
		t.Fatalf("write repo CLAUDE.md: %v", err)
	}
	globalClaude := filepath.Join(fakeHome, ".claude", "CLAUDE.md")
	if err := os.MkdirAll(filepath.Dir(globalClaude), 0755); err != nil {
		t.Fatalf("create global Claude directory: %v", err)
	}
	if err := os.Symlink(coreFile, globalClaude); err != nil {
		t.Fatalf("create global CLAUDE.md symlink: %v", err)
	}

	err := InitCommandWithConfig(InitParams{
		Description: "Test goal",
		SpecRef:     "specs/vision.md",
		Agents:      []string{"claude"},
	})
	if err != nil {
		t.Fatalf("InitCommand failed: %v", err)
	}

	content, err := os.ReadFile(repoClaude)
	if err != nil {
		t.Fatalf("read repo CLAUDE.md: %v", err)
	}
	if string(content) != "@AGENTS.md\n" {
		t.Errorf("repo CLAUDE.md changed; got %q", content)
	}
}

func TestInitCommand_ContractActionLocalCreatesCLAUDELocalMd(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)

	fakeHome := setupGlobalLiza(t)

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(gitDir)

	testhelpers.CreateCommittedSpecFile(t, gitDir, "vision.md", "# Vision\n")

	// Pre-create CLAUDE.md as a regular file (brownfield project)
	os.WriteFile(filepath.Join(gitDir, "CLAUDE.md"), []byte("project"), 0644)

	err := InitCommandWithConfig(InitParams{
		Description:     "Test goal",
		SpecRef:         "specs/vision.md",
		Agents:          []string{"claude"},
		ContractActions: map[string]string{"claude": "local"},
	})
	if err != nil {
		t.Fatalf("InitCommand failed: %v", err)
	}

	// CLAUDE.md at repo root should be untouched
	content, _ := os.ReadFile(filepath.Join(gitDir, "CLAUDE.md"))
	if string(content) != "project" {
		t.Error("Repo root CLAUDE.md was modified")
	}

	// CLAUDE.local.md should be a symlink to CORE.md
	coreFile := filepath.Join(fakeHome, paths.GlobalDirName(), "CORE.md")
	localPath := filepath.Join(gitDir, "CLAUDE.local.md")
	target, err := os.Readlink(localPath)
	if err != nil {
		t.Fatalf("CLAUDE.local.md symlink not created: %v", err)
	}
	if target != coreFile {
		t.Errorf("CLAUDE.local.md → %q, want %q", target, coreFile)
	}
}

func TestCheckContractConfigured_FindsLocalMd(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	dir := t.TempDir()
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	// Create the global managed contract.
	lizaDir := filepath.Join(fakeHome, paths.GlobalDirName())
	os.MkdirAll(lizaDir, 0755)
	coreFile := filepath.Join(lizaDir, "CORE.md")
	os.WriteFile(coreFile, []byte("core"), 0644)

	// Create CLAUDE.local.md as a Liza symlink
	os.Symlink(coreFile, filepath.Join(dir, "CLAUDE.local.md"))

	got := CheckContractConfigured(dir, "claude")
	if got == "" {
		t.Fatal("expected CheckContractConfigured to find CLAUDE.local.md")
	}
	if filepath.Base(got) != "CLAUDE.local.md" {
		t.Errorf("found %q, expected CLAUDE.local.md", got)
	}
}

func TestCheckContractConfigured_CodexACPUsesCodexContract(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	dir := t.TempDir()
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	lizaDir := filepath.Join(fakeHome, paths.GlobalDirName())
	if err := os.MkdirAll(lizaDir, 0755); err != nil {
		t.Fatal(err)
	}
	coreFile := filepath.Join(lizaDir, "CORE.md")
	if err := os.WriteFile(coreFile, []byte("core"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(coreFile, filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}

	got := CheckContractConfigured(dir, "codex-acp")
	if got == "" {
		t.Fatal("expected CheckContractConfigured to find AGENTS.md for codex-acp")
	}
	if filepath.Base(got) != "AGENTS.md" {
		t.Errorf("found %q, expected AGENTS.md", got)
	}
}

func TestCheckContractConfigured_CursorACPUsesAgentsContract(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	dir := t.TempDir()
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	lizaDir := filepath.Join(fakeHome, paths.GlobalDirName())
	if err := os.MkdirAll(lizaDir, 0755); err != nil {
		t.Fatal(err)
	}
	coreFile := filepath.Join(lizaDir, "CORE.md")
	if err := os.WriteFile(coreFile, []byte("core"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(coreFile, filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}

	got := CheckContractConfigured(dir, "cursor-acp")
	if got == "" {
		t.Fatal("expected CheckContractConfigured to find AGENTS.md for cursor-acp")
	}
	if filepath.Base(got) != "AGENTS.md" {
		t.Errorf("found %q, expected AGENTS.md", got)
	}
}

func TestCheckContractConfigured_OpenCodeUsesAgentsContract(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	dir := t.TempDir()
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	unsetEnvForTest(t, "XDG_CONFIG_HOME")

	lizaDir := filepath.Join(fakeHome, paths.GlobalDirName())
	if err := os.MkdirAll(lizaDir, 0755); err != nil {
		t.Fatal(err)
	}
	coreFile := filepath.Join(lizaDir, "CORE.md")
	if err := os.WriteFile(coreFile, []byte("core"), 0644); err != nil {
		t.Fatal(err)
	}
	globalOpenCode := filepath.Join(fakeHome, ".config", "opencode", "AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(globalOpenCode), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(coreFile, globalOpenCode); err != nil {
		t.Fatal(err)
	}

	for _, cliName := range []string{"opencode", "opencode-acp"} {
		t.Run(cliName, func(t *testing.T) {
			got := CheckContractConfigured(dir, cliName)
			if got != globalOpenCode {
				t.Fatalf("CheckContractConfigured(%s) = %q, want %q", cliName, got, globalOpenCode)
			}
		})
	}
}

func TestInitCommand_WritesClaudeSettings(t *testing.T) {
	// Create temporary git repo
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)

	setupGlobalLiza(t)

	// Change to temp directory
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(gitDir); err != nil {
		t.Fatal(err)
	}

	// Setup
	testhelpers.CreateCommittedSpecFile(t, gitDir, "vision.md", "# Vision\n")

	// Run init
	err = InitCommand("Test goal", "specs/vision.md", nil)
	if err != nil {
		t.Fatalf("InitCommand failed: %v", err)
	}

	// Verify .claude directory was created
	claudeDir := filepath.Join(gitDir, ".claude")
	if _, err := os.Stat(claudeDir); os.IsNotExist(err) {
		t.Errorf(".claude directory not created")
	}

	// Verify settings.json was created
	settingsPath := filepath.Join(claudeDir, "settings.json")
	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		t.Fatalf("settings.json not created")
	}

	// Verify file permissions
	testhelpers.AssertRegularFileMode(t, settingsPath, 0644)

	// Read and parse JSON
	content, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("Failed to read settings.json: %v", err)
	}

	var settings map[string]any
	if err := json.Unmarshal(content, &settings); err != nil {
		t.Fatalf("settings.json is not valid JSON: %v", err)
	}

	// Verify permissions structure exists
	perms, ok := settings["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("permissions field missing or invalid type")
	}

	// Verify defaultMode is not written: project-scope settings do not reliably
	// set the permission mode and would shadow the user's own Claude settings.
	if mode, ok := perms["defaultMode"]; ok {
		t.Errorf("permissions.defaultMode should not be written, got %v", mode)
	}

	// Verify allow array exists
	allow, ok := perms["allow"].([]any)
	if !ok {
		t.Fatalf("permissions.allow field missing or not an array")
	}

	// Verify allow array has some permissions
	if len(allow) == 0 {
		t.Errorf("permissions.allow array is empty")
	}

	// Verify .claude/hooks/enforce-init.sh was deployed
	hookPath := filepath.Join(claudeDir, "hooks", "enforce-init.sh")
	if _, hookErr := os.Stat(hookPath); os.IsNotExist(hookErr) {
		t.Error(".claude/hooks/enforce-init.sh not created during workspace init")
	} else if hookErr == nil {
		testhelpers.AssertExecutableScript(t, hookPath)
	}
}

// validPipelineYAML is a minimal valid pipeline config for testing.
const validPipelineYAML = `pipeline:
  roles:
    code-planner:
      type: doer
      display-name: "Code Planner"
    code-plan-reviewer:
      type: reviewer
      display-name: "Code Plan Reviewer"
    coder:
      type: doer
      display-name: "Coder"
    code-reviewer:
      type: reviewer
      display-name: "Code Reviewer"

  role-pairs:
    code-planning-pair:
      doer: code-planner
      reviewer: code-plan-reviewer
      states:
        initial: DRAFT_CODING_PLAN
        executing: CODE_PLANNING
        submitted: CODING_PLAN_TO_REVIEW
        reviewing: REVIEWING_CODING_PLAN
        approved: CODING_PLAN_APPROVED
        rejected: CODING_PLAN_REJECTED

    coding-pair:
      doer: coder
      reviewer: code-reviewer
      states:
        initial: DRAFT_CODE
        executing: IMPLEMENTING_CODE
        submitted: CODE_TO_REVIEW
        reviewing: REVIEWING_CODE
        approved: CODE_APPROVED
        rejected: CODE_REJECTED

  sub-pipelines:
    coding-subpipeline:
      steps:
        - code-planning-pair
        - coding-pair
      transitions:
        - name: code-plan-to-coding
          from: code-planning-pair.approved
          to: coding-pair.initial
          trigger: manual
          cardinality: per-subtask

  entry-points:
    detailed-spec: coding-subpipeline.code-planning-pair
    technical-spec: coding-subpipeline.code-planning-pair
`

func writePipelineConfig(t *testing.T, dir, content string) string {
	t.Helper()
	configPath := filepath.Join(dir, "pipeline.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return configPath
}

func TestInitCommandWithConfig_FreezesPipeline(t *testing.T) {
	tmpDir := setupGitRepo(t)
	defer os.RemoveAll(tmpDir)
	setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")
	configPath := writePipelineConfig(t, tmpDir, validPipelineYAML)

	err = InitCommandWithConfig(InitParams{
		Description: "Pipeline goal",
		SpecRef:     "specs/vision.md",
		ConfigPath:  configPath,
	})
	if err != nil {
		t.Fatalf("InitCommandWithConfig() error = %v", err)
	}

	// Verify the project pipeline config exists and is identical to the input.
	frozenPath := filepath.Join(tmpDir, paths.ProjectDirName(), "pipeline.yaml")
	frozen, err := os.ReadFile(frozenPath)
	if err != nil {
		t.Fatalf("Failed to read frozen pipeline.yaml: %v", err)
	}
	if string(frozen) != validPipelineYAML {
		t.Errorf("Frozen pipeline.yaml differs from input.\nGot:\n%s\nWant:\n%s", string(frozen), validPipelineYAML)
	}

	// Verify state.yaml has pipeline_version: 2
	bb := db.New(filepath.Join(tmpDir, paths.ProjectDirName(), "state.yaml"))
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if state.PipelineVersion != 3 {
		t.Errorf("state.PipelineVersion = %d, want 3", state.PipelineVersion)
	}
}

func TestInitCommandWithConfig_EntryPoint(t *testing.T) {
	tmpDir := setupGitRepo(t)
	defer os.RemoveAll(tmpDir)
	setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")
	configPath := writePipelineConfig(t, tmpDir, validPipelineYAML)

	err = InitCommandWithConfig(InitParams{
		Description: "Pipeline goal",
		SpecRef:     "specs/vision.md",
		ConfigPath:  configPath,
		EntryPoint:  "detailed-spec",
	})
	if err != nil {
		t.Fatalf("InitCommandWithConfig() error = %v", err)
	}

	// Verify goal.entry_point is set
	bb := db.New(filepath.Join(tmpDir, paths.ProjectDirName(), "state.yaml"))
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if state.Goal.EntryPoint != "detailed-spec" {
		t.Errorf("state.Goal.EntryPoint = %q, want %q", state.Goal.EntryPoint, "detailed-spec")
	}
}

func TestInitCommandWithConfig_NoFollowUp(t *testing.T) {
	tmpDir := setupGitRepo(t)
	defer os.RemoveAll(tmpDir)
	setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	err = InitCommandWithConfig(InitParams{
		Description: "Pipeline goal",
		SpecRef:     "specs/vision.md",
		NoFollowUp:  true,
	})
	if err != nil {
		t.Fatalf("InitCommandWithConfig() error = %v", err)
	}

	bb := db.New(filepath.Join(tmpDir, paths.ProjectDirName(), "state.yaml"))
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if !state.Config.NoFollowUp {
		t.Error("state.Config.NoFollowUp = false, want true")
	}
}

func TestInitCommandWithConfig_NewDefaultEntryPoints(t *testing.T) {
	for _, entryPoint := range []string{"functional-spec", "technical-spec"} {
		t.Run(entryPoint, func(t *testing.T) {
			tmpDir := setupGitRepo(t)
			defer os.RemoveAll(tmpDir)
			setupGlobalLiza(t)

			originalDir, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			defer os.Chdir(originalDir)
			if err := os.Chdir(tmpDir); err != nil {
				t.Fatal(err)
			}

			testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")

			err = InitCommandWithConfig(InitParams{
				Description: "Goal",
				SpecRef:     "specs/vision.md",
				EntryPoint:  entryPoint,
			})
			if err != nil {
				t.Fatalf("InitCommandWithConfig() error = %v", err)
			}

			bb := db.New(filepath.Join(tmpDir, paths.ProjectDirName(), "state.yaml"))
			state, err := bb.Read()
			if err != nil {
				t.Fatalf("Failed to read state: %v", err)
			}
			if state.Goal.EntryPoint != entryPoint {
				t.Errorf("state.Goal.EntryPoint = %q, want %q", state.Goal.EntryPoint, entryPoint)
			}
		})
	}
}

func TestInitCommandWithConfig_NoConfigAutoFreezes(t *testing.T) {
	tmpDir := setupGitRepo(t)
	defer os.RemoveAll(tmpDir)
	setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	// Init without --config auto-freezes embedded pipeline
	err = InitCommand("Legacy goal", "specs/vision.md", nil)
	if err != nil {
		t.Fatalf("InitCommand() error = %v", err)
	}

	// Verify pipeline.yaml is auto-frozen from embedded config
	frozenPath := filepath.Join(tmpDir, paths.ProjectDirName(), "pipeline.yaml")
	if _, err := os.Stat(frozenPath); os.IsNotExist(err) {
		t.Errorf("pipeline.yaml should be auto-frozen from embedded config")
	}

	supportPath := filepath.Join(tmpDir, paths.ProjectDirName(), "SUPPORT.md")
	if _, err := os.Stat(supportPath); os.IsNotExist(err) {
		t.Errorf("SUPPORT.md should be written during init")
	}

	// Verify pipeline_version is set
	bb := db.New(filepath.Join(tmpDir, paths.ProjectDirName(), "state.yaml"))
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if state.PipelineVersion != 3 {
		t.Errorf("state.PipelineVersion = %d, want 3", state.PipelineVersion)
	}

	// Verify no entry_point (not specified)
	if state.Goal.EntryPoint != "" {
		t.Errorf("state.Goal.EntryPoint = %q, want empty", state.Goal.EntryPoint)
	}
}

func TestInitCommandWithConfig_InvalidConfig(t *testing.T) {
	tmpDir := setupGitRepo(t)
	defer os.RemoveAll(tmpDir)
	setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	// Write invalid pipeline config (missing required fields)
	invalidYAML := `pipeline:
  role-pairs: {}
`
	configPath := writePipelineConfig(t, tmpDir, invalidYAML)

	err = InitCommandWithConfig(InitParams{
		Description: "Bad config goal",
		SpecRef:     "specs/vision.md",
		ConfigPath:  configPath,
	})
	if err == nil {
		t.Fatal("Expected error for invalid config, got nil")
	}
	testhelpers.AssertErrorContains(t, err, "invalid pipeline config")

	// Verify the project runtime directory was not created (early validation).
	lizaDir := filepath.Join(tmpDir, paths.ProjectDirName())
	if _, statErr := os.Stat(lizaDir); !os.IsNotExist(statErr) {
		t.Errorf("project runtime directory should not exist after config validation failure")
	}
}

func TestInitCommandWithConfig_NonexistentEntryPoint(t *testing.T) {
	tmpDir := setupGitRepo(t)
	defer os.RemoveAll(tmpDir)
	setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")
	configPath := writePipelineConfig(t, tmpDir, validPipelineYAML)

	err = InitCommandWithConfig(InitParams{
		Description: "Goal",
		SpecRef:     "specs/vision.md",
		ConfigPath:  configPath,
		EntryPoint:  "nonexistent",
	})
	if err == nil {
		t.Fatal("Expected error for nonexistent entry-point, got nil")
	}
	testhelpers.AssertErrorContains(t, err, "entry-point")
	testhelpers.AssertErrorContains(t, err, "not found")
}

func TestInitCommandWithConfig_PostWorktreeCmd(t *testing.T) {
	tmpDir := setupGitRepo(t)
	defer os.RemoveAll(tmpDir)
	setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	err = InitCommandWithConfig(InitParams{
		Description:     "Goal with post-worktree-cmd",
		SpecRef:         "specs/vision.md",
		PostWorktreeCmd: "make sync-embedded",
	})
	if err != nil {
		t.Fatalf("InitCommandWithConfig() error = %v", err)
	}

	// Verify post_worktree_cmd is set in state
	bb := db.New(filepath.Join(tmpDir, paths.ProjectDirName(), "state.yaml"))
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if state.Config.PostWorktreeCmd == nil {
		t.Fatal("state.Config.PostWorktreeCmd is nil, want non-nil")
	}
	if *state.Config.PostWorktreeCmd != "make sync-embedded" {
		t.Errorf("state.Config.PostWorktreeCmd = %q, want %q", *state.Config.PostWorktreeCmd, "make sync-embedded")
	}
}

func TestInitCommandWithConfig_CopyWorktreeEnvFiles(t *testing.T) {
	tmpDir := setupGitRepo(t)
	defer os.RemoveAll(tmpDir)
	setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	err = InitCommandWithConfig(InitParams{
		Description:          "Goal with env files",
		SpecRef:              "specs/vision.md",
		CopyWorktreeEnvFiles: true,
	})
	if err != nil {
		t.Fatalf("InitCommandWithConfig() error = %v", err)
	}

	bb := db.New(filepath.Join(tmpDir, paths.ProjectDirName(), "state.yaml"))
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if !state.Config.CopyWorktreeEnvFiles {
		t.Fatal("state.Config.CopyWorktreeEnvFiles = false, want true")
	}
}

func TestInitCommandWithConfig_CopyWorktreeEnvFilesFromEnv(t *testing.T) {
	tmpDir := setupGitRepo(t)
	defer os.RemoveAll(tmpDir)
	setupGlobalLiza(t)
	t.Setenv(models.EnvEnableCopyWorktreeEnvFiles, "true")

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	err = InitCommandWithConfig(InitParams{
		Description: "Goal with env files",
		SpecRef:     "specs/vision.md",
	})
	if err != nil {
		t.Fatalf("InitCommandWithConfig() error = %v", err)
	}

	bb := db.New(filepath.Join(tmpDir, paths.ProjectDirName(), "state.yaml"))
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if !state.Config.CopyWorktreeEnvFiles {
		t.Fatal("state.Config.CopyWorktreeEnvFiles = false, want true from env")
	}
}

func TestInitCommandWithConfig_ScipSearchPersistsConfig(t *testing.T) {
	tmpDir := setupGitRepo(t)
	defer os.RemoveAll(tmpDir)
	setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	var calls []string
	restore := scipsearch.SetCommandRunnerForTest(func(name string, args ...string) (string, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return "ok\n", nil
	})
	defer restore()

	err = InitCommandWithConfig(InitParams{
		Description: "Goal with scip-search config",
		SpecRef:     "specs/vision.md",
		ScipSearch:  []string{"go", "typescript"},
	})
	if err != nil {
		t.Fatalf("InitCommandWithConfig() error = %v", err)
	}

	statePath := filepath.Join(tmpDir, paths.ProjectDirName(), "state.yaml")
	bb := db.New(statePath)
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	want := []string{"go", "typescript"}
	if !slices.Equal(state.Config.ScipSearch, want) {
		t.Errorf("state.Config.ScipSearch = %v, want %v", state.Config.ScipSearch, want)
	}
	wantCalls := []string{
		"scip-search --help",
		"scip-search --version",
		"scip-go --help",
		"scip-typescript --help",
	}
	if !slices.Equal(calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", calls, wantCalls)
	}

	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("Failed to read state.yaml: %v", err)
	}
	content := string(data)
	for _, want := range []string{"scip_search:\n", "- go\n", "- typescript\n"} {
		if !strings.Contains(content, want) {
			t.Errorf("state.yaml missing %q in config.scip_search serialization; content:\n%s", want, content)
		}
	}
}

func TestInitCommandWithConfig_RejectsPairingScipSearchPlan(t *testing.T) {
	tmpDir := setupGitRepo(t)
	defer os.RemoveAll(tmpDir)
	setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	err = InitCommandWithConfig(InitParams{
		Description:     "Goal with pairing-only scip-search plan",
		ScipSearchPlans: []string{"go=."},
	})
	if err == nil {
		t.Fatal("InitCommandWithConfig() error = nil, want pairing-only flag rejection")
	}
	if !strings.Contains(err.Error(), "--scip-search-plan is only supported for pairing init") {
		t.Fatalf("InitCommandWithConfig() error = %v, want pairing-only diagnostic", err)
	}
}

func TestInitCommandWithConfig_AutodetectsAndPersistsValidatedScipSearchLanguages(t *testing.T) {
	tmpDir := setupGitRepo(t)
	defer os.RemoveAll(tmpDir)
	setupGlobalLiza(t)
	t.Setenv("LIZA_ENABLE_SCIP_SEARCH", " TRUE ")

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")
	writeTrackedFile(t, tmpDir, "go.mod", "module example.com/project\n")
	writeTrackedFile(t, tmpDir, "web/app.tsx", "export const App = () => null\n")
	writeTrackedFile(t, tmpDir, "api/app_test.py", "def test_app():\n    assert True\n")
	testhelpers.MustGit(t, tmpDir, "add", "go.mod", "web/app.tsx", "api/app_test.py")
	testhelpers.MustGit(t, tmpDir, "commit", "-m", "Add tracked code")

	var calls []string
	restore := scipsearch.SetCommandRunnerForTest(func(name string, args ...string) (string, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if name == "scip-typescript" {
			return "", os.ErrNotExist
		}
		if name == "scip-search" && strings.Join(args, " ") == "--version" {
			return "scip-search 1.2.3\n", nil
		}
		return "ok\n", nil
	})
	defer restore()

	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	originalStderr := os.Stderr
	os.Stderr = stderrWriter
	defer func() { os.Stderr = originalStderr }()
	err = InitCommandWithConfig(InitParams{
		Description: "Goal with autodetected scip-search config",
		SpecRef:     "specs/vision.md",
	})
	if closeErr := stderrWriter.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	stderrBytes, readErr := io.ReadAll(stderrReader)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if err != nil {
		t.Fatalf("InitCommandWithConfig() error = %v", err)
	}
	stderr := string(stderrBytes)
	if !strings.Contains(stderr, "scip-search --version: scip-search 1.2.3") {
		t.Fatalf("stderr = %q, want scip-search version diagnostic", stderr)
	}
	if !strings.Contains(stderr, "dropping scip-search language \"typescript\"") {
		t.Fatalf("stderr = %q, want dropped typescript warning", stderr)
	}

	statePath := filepath.Join(tmpDir, paths.ProjectDirName(), "state.yaml")
	state, err := db.New(statePath).Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	want := []string{"go", "python"}
	if !slices.Equal(state.Config.ScipSearch, want) {
		t.Fatalf("state.Config.ScipSearch = %v, want %v", state.Config.ScipSearch, want)
	}
	wantCalls := []string{
		"scip-search --help",
		"scip-search --version",
		"scip-go --help",
		"scip-typescript --help",
		"scip-python --help",
	}
	if !slices.Equal(calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", calls, wantCalls)
	}
}

func TestInitCommandWithConfig_SembleDisabledSkipsReadiness(t *testing.T) {
	tests := []struct {
		name  string
		value *string
	}{
		{name: "unset"},
		{name: "empty", value: stringPtrForTest("")},
		{name: "zero", value: stringPtrForTest("0")},
		{name: "false", value: stringPtrForTest("false")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := setupGitRepo(t)
			defer os.RemoveAll(tmpDir)
			setupGlobalLiza(t)
			if tt.value == nil {
				unsetEnvForTest(t, semble.EnvEnableSemble)
			} else {
				t.Setenv(semble.EnvEnableSemble, *tt.value)
			}
			t.Setenv("SEMBLE_MODEL_NAME", "disabled-"+tt.name)

			originalDir, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			defer os.Chdir(originalDir)
			if err := os.Chdir(tmpDir); err != nil {
				t.Fatal(err)
			}
			testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")

			var lookups, runs int
			restore := setInitSembleHooksForTest(
				func(name string) (string, error) {
					lookups++
					return filepath.Join(tmpDir, "bin", name), nil
				},
				func(plan semble.CommandPlan) (semble.CommandResult, error) {
					runs++
					return semble.CommandResult{ExitCode: 0}, nil
				},
			)
			defer restore()

			stderr, err := captureStderrForTest(func() error {
				return InitCommandWithConfig(InitParams{
					Description: "Goal with disabled optional search",
					SpecRef:     "specs/vision.md",
				})
			})
			if err != nil {
				t.Fatalf("InitCommandWithConfig() error = %v", err)
			}
			if lookups != 0 || runs != 0 {
				t.Fatalf("Semble lookups=%d runs=%d, want zero", lookups, runs)
			}
			if strings.Contains(strings.ToLower(stderr), "semble") {
				t.Fatalf("stderr = %q, want no Semble diagnostics", stderr)
			}
			assertStateHasNoSembleForTest(t, filepath.Join(tmpDir, paths.ProjectDirName(), "state.yaml"))
		})
	}
}

func TestInitCommandWithConfig_SembleEnabledPrewarmsBeforeStateWrite(t *testing.T) {
	tmpDir := setupGitRepo(t)
	defer os.RemoveAll(tmpDir)
	setupGlobalLiza(t)
	t.Setenv(semble.EnvEnableSemble, "true")
	t.Setenv("SEMBLE_MODEL_NAME", "enabled-before-state")

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	var calls []string
	restore := setInitSembleHooksForTest(
		func(name string) (string, error) {
			calls = append(calls, "lookup:"+name)
			return filepath.Join(tmpDir, "bin", name), nil
		},
		func(plan semble.CommandPlan) (semble.CommandResult, error) {
			if _, err := os.Stat(filepath.Join(tmpDir, paths.ProjectDirName(), "state.yaml")); !os.IsNotExist(err) {
				t.Fatalf("Semble runner observed state.yaml before returning: %v", err)
			}
			if len(plan.Env) == 0 {
				calls = append(calls, "prewarm")
			} else {
				calls = append(calls, "offline")
			}
			return semble.CommandResult{ExitCode: 0}, nil
		},
	)
	defer restore()

	stderr, err := captureStderrForTest(func() error {
		return InitCommandWithConfig(InitParams{
			Description: "Goal with enabled optional search",
			SpecRef:     "specs/vision.md",
		})
	})
	if err != nil {
		t.Fatalf("InitCommandWithConfig() error = %v", err)
	}
	if strings.Contains(strings.ToLower(stderr), "semble") {
		t.Fatalf("stderr = %q, want no Semble diagnostics", stderr)
	}
	wantCalls := []string{"lookup:semble", "prewarm", "lookup:semble", "offline"}
	if !slices.Equal(calls, wantCalls) {
		t.Fatalf("Semble calls = %v, want %v", calls, wantCalls)
	}
	assertStateHasNoSembleForTest(t, filepath.Join(tmpDir, paths.ProjectDirName(), "state.yaml"))
}

func TestInitCommandWithConfig_SembleReadinessDiagnosticsNonFatal(t *testing.T) {
	tests := []struct {
		name       string
		lookPath   semble.ExecutableLookup
		runner     semble.CommandRunner
		wantStderr string
		notStderr  string
		wantRuns   int
	}{
		{
			name: "missing executable",
			lookPath: func(name string) (string, error) {
				return "", os.ErrNotExist
			},
			runner: func(plan semble.CommandPlan) (semble.CommandResult, error) {
				t.Fatalf("Semble runner called for missing executable")
				return semble.CommandResult{}, nil
			},
			wantStderr: "semble: semble executable not found",
			wantRuns:   0,
		},
		{
			name: "offline unready model",
			lookPath: func(name string) (string, error) {
				return "/tmp/fake-," + name, nil
			},
			runner: func(plan semble.CommandPlan) (semble.CommandResult, error) {
				if len(plan.Env) == 0 {
					return semble.CommandResult{ExitCode: 0}, nil
				}
				return semble.CommandResult{
					ExitCode: 1,
					Stderr:   "LocalEntryNotFoundError: HF_HUB_OFFLINE=1 cache miss",
				}, errors.New("exit status 1")
			},
			wantStderr: "semble: model unavailable offline",
			wantRuns:   2,
		},
		{
			name: "generic execution failure",
			lookPath: func(name string) (string, error) {
				return "/tmp/fake-," + name, nil
			},
			runner: func(plan semble.CommandPlan) (semble.CommandResult, error) {
				return semble.CommandResult{
					ExitCode: 2,
					Stdout:   strings.Repeat("verbose-output ", 90) + "UNBOUNDED_TAIL",
				}, errors.New("exit status 2")
			},
			wantStderr: "semble: execution failed",
			notStderr:  "UNBOUNDED_TAIL",
			wantRuns:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := setupGitRepo(t)
			defer os.RemoveAll(tmpDir)
			setupGlobalLiza(t)
			t.Setenv(semble.EnvEnableSemble, "1")
			t.Setenv("SEMBLE_MODEL_NAME", "diagnostic-"+tt.name)

			originalDir, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			defer os.Chdir(originalDir)
			if err := os.Chdir(tmpDir); err != nil {
				t.Fatal(err)
			}
			testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")

			var runs int
			restore := setInitSembleHooksForTest(tt.lookPath, func(plan semble.CommandPlan) (semble.CommandResult, error) {
				runs++
				return tt.runner(plan)
			})
			defer restore()

			stderr, err := captureStderrForTest(func() error {
				return InitCommandWithConfig(InitParams{
					Description: "Goal with optional search diagnostic",
					SpecRef:     "specs/vision.md",
				})
			})
			if err != nil {
				t.Fatalf("InitCommandWithConfig() error = %v", err)
			}
			if !strings.Contains(stderr, tt.wantStderr) {
				t.Fatalf("stderr = %q, want %q", stderr, tt.wantStderr)
			}
			if tt.notStderr != "" && strings.Contains(stderr, tt.notStderr) {
				t.Fatalf("stderr = %q, want it to omit %q", stderr, tt.notStderr)
			}
			if runs != tt.wantRuns {
				t.Fatalf("Semble runner calls = %d, want %d", runs, tt.wantRuns)
			}
			assertStateHasNoSembleForTest(t, filepath.Join(tmpDir, paths.ProjectDirName(), "state.yaml"))
		})
	}
}

func writeTrackedFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("failed to create parent for %s: %v", rel, err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write %s: %v", rel, err)
	}
}

func TestInitCommandWithConfig_PostWorktreeCmdOmittedWhenEmpty(t *testing.T) {
	tmpDir := setupGitRepo(t)
	defer os.RemoveAll(tmpDir)
	setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	err = InitCommandWithConfig(InitParams{
		Description: "Goal without post-worktree-cmd",
		SpecRef:     "specs/vision.md",
	})
	if err != nil {
		t.Fatalf("InitCommandWithConfig() error = %v", err)
	}

	// Verify post_worktree_cmd is nil in state
	bb := db.New(filepath.Join(tmpDir, paths.ProjectDirName(), "state.yaml"))
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if state.Config.PostWorktreeCmd != nil {
		t.Errorf("state.Config.PostWorktreeCmd = %q, want nil", *state.Config.PostWorktreeCmd)
	}
}

// --- InitPairingCommand tests ---

func TestInitPairingCommand_Claude(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)

	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)
	fakeHome := setupGlobalLiza(t)

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(gitDir)

	err := InitPairingCommand(InitPairingParams{
		Agents: []string{"claude"},
	})
	if err != nil {
		t.Fatalf("InitPairingCommand failed: %v", err)
	}

	// Claude's documented global instruction path should point to the managed contract.
	target, err := os.Readlink(filepath.Join(fakeHome, ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("global CLAUDE.md not a symlink: %v", err)
	}
	expected := filepath.Join(fakeHome, paths.GlobalDirName(), "CORE.md")
	if target != expected {
		t.Errorf("CLAUDE.md → %q, want %q", target, expected)
	}

	// The project runtime directory should not exist.
	if _, err := os.Stat(filepath.Join(gitDir, paths.ProjectDirName())); !os.IsNotExist(err) {
		t.Error(paths.ProjectDirName() + "/ directory should not be created in pairing mode")
	}

	// .claude/settings.json should be written
	settingsPath := filepath.Join(gitDir, ".claude", "settings.json")
	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		t.Error(".claude/settings.json should be created for --claude pairing")
	}

	// .claude/hooks/enforce-init.sh should be deployed
	hookPath := filepath.Join(gitDir, ".claude", "hooks", "enforce-init.sh")
	if _, err := os.Stat(hookPath); os.IsNotExist(err) {
		t.Error(".claude/hooks/enforce-init.sh should be created for --claude pairing")
	} else if err == nil {
		testhelpers.AssertExecutableScript(t, hookPath)
	}

	// AGENTS.md and GEMINI.md should NOT exist (only --claude)
	for _, name := range []string{"AGENTS.md", "GEMINI.md"} {
		if _, err := os.Stat(filepath.Join(gitDir, name)); !os.IsNotExist(err) {
			t.Errorf("%s should not exist when only --claude is specified", name)
		}
	}
}

func TestInitPairingCommand_MultipleAgents(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)
	fakeHome := setupGlobalLiza(t)

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(gitDir)

	err := InitPairingCommand(InitPairingParams{
		Agents: []string{"claude", "codex", "gemini"},
	})
	if err != nil {
		t.Fatalf("InitPairingCommand failed: %v", err)
	}

	expected := filepath.Join(fakeHome, paths.GlobalDirName(), "CORE.md")
	for _, tc := range []struct {
		agent string
		file  string
	}{
		{"claude", filepath.Join(".claude", "CLAUDE.md")},
		{"codex", filepath.Join(".codex", "AGENTS.md")},
		{"gemini", filepath.Join(".gemini", "GEMINI.md")},
	} {
		target, err := os.Readlink(filepath.Join(fakeHome, tc.file))
		if err != nil {
			t.Errorf("%s (%s): not a symlink: %v", tc.file, tc.agent, err)
			continue
		}
		if target != expected {
			t.Errorf("%s → %q, want %q", tc.file, target, expected)
		}
	}
	verifyCodexHooks(t, gitDir)
}

func TestInitPairingCommand_BashPolicyDisabledSkipsLookup(t *testing.T) {
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)
	setupGlobalLiza(t)

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(gitDir)

	var lookups int
	restore := setInitBashPolicyHooksForTest(
		func(name string) (string, error) {
			lookups++
			return filepath.Join(gitDir, "bin", name), nil
		},
		&initBashPolicyTestRunner{},
	)
	defer restore()

	if err := InitPairingCommand(InitPairingParams{Agents: []string{"claude", "codex"}}); err != nil {
		t.Fatalf("InitPairingCommand() error = %v", err)
	}
	if lookups != 0 {
		t.Fatalf("bash-policy lookups = %d, want zero when gate disabled", lookups)
	}
	if _, err := os.Stat(filepath.Join(gitDir, ".bash-policy.yaml")); !os.IsNotExist(err) {
		t.Fatalf(".bash-policy.yaml state = %v, want absent when gate disabled", err)
	}
}

func TestInitPairingCommand_CursorSkipsBashPolicyWhenGateDisabled(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)

	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)
	fakeHome := setupGlobalLiza(t)
	writeOldCachedCursorProviderCatalog(t, fakeHome)

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(gitDir)

	var lookups int
	runner := &initBashPolicyTestRunner{}
	restore := setInitBashPolicyHooksForTest(
		func(name string) (string, error) {
			lookups++
			return filepath.Join(gitDir, "bin", name), nil
		},
		runner,
	)
	defer restore()

	if err := InitPairingCommand(InitPairingParams{Agents: []string{"cursor"}}); err != nil {
		t.Fatalf("InitPairingCommand() error = %v", err)
	}

	target, err := os.Readlink(filepath.Join(gitDir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("AGENTS.md symlink not created: %v", err)
	}
	if want := filepath.Join(fakeHome, paths.GlobalDirName(), "CORE.md"); target != want {
		t.Fatalf("AGENTS.md target = %q, want %q", target, want)
	}
	if lookups != 0 {
		t.Fatalf("bash-policy lookups = %d, want zero when gate disabled", lookups)
	}
	if len(runner.commands) != 0 {
		t.Fatalf("bash-policy commands = %d, want zero when gate disabled", len(runner.commands))
	}
	if _, err := os.Stat(filepath.Join(gitDir, ".bash-policy.yaml")); !os.IsNotExist(err) {
		t.Fatalf(".bash-policy.yaml state = %v, want absent when gate disabled", err)
	}
	verifyClaudeArtifacts(t, gitDir)
	verifyCodexHooks(t, gitDir)
}

func TestInitPairingCommand_CursorAndOpenCodeRetainSharedRepoContract(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)
	fakeHome := setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(gitDir); err != nil {
		t.Fatal(err)
	}

	if err := InitPairingCommand(InitPairingParams{Agents: []string{"cursor", "opencode"}}); err != nil {
		t.Fatalf("InitPairingCommand() error = %v", err)
	}
	contractTarget := filepath.Join(fakeHome, paths.GlobalDirName(), "CORE.md")
	repoPath := filepath.Join(gitDir, "AGENTS.md")
	if target, err := os.Readlink(repoPath); err != nil || target != contractTarget {
		t.Fatalf("shared repo AGENTS.md target = %q, err = %v; want %q", target, err, contractTarget)
	}
	globalPath := filepath.Join(fakeHome, ".config", "opencode", "AGENTS.md")
	if target, err := os.Readlink(globalPath); err != nil || target != contractTarget {
		t.Fatalf("OpenCode global AGENTS.md target = %q, err = %v; want %q", target, err, contractTarget)
	}
}

func TestInitPairingCommand_SequentialGlobalInitPreservesRepoOnlyProvider(t *testing.T) {
	tests := []struct {
		name         string
		firstAgent   string
		secondAgent  string
		repoFile     string
		globalTarget string
	}{
		{
			name:         "Cursor then Codex",
			firstAgent:   "cursor",
			secondAgent:  "codex",
			repoFile:     "AGENTS.md",
			globalTarget: filepath.Join(".codex", "AGENTS.md"),
		},
		{
			name:         "Kimi then Claude",
			firstAgent:   "kimi",
			secondAgent:  "claude",
			repoFile:     "CLAUDE.md",
			globalTarget: filepath.Join(".claude", "CLAUDE.md"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gitDir := setupGitRepo(t)
			fakeHome := setupGlobalLiza(t)
			contractTarget := filepath.Join(fakeHome, paths.GlobalDirName(), "CORE.md")

			originalDir, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chdir(originalDir) })
			if err := os.Chdir(gitDir); err != nil {
				t.Fatal(err)
			}

			if err := InitPairingCommand(InitPairingParams{Agents: []string{tt.firstAgent}}); err != nil {
				t.Fatalf("first InitPairingCommand(%s) error = %v", tt.firstAgent, err)
			}
			if err := InitPairingCommand(InitPairingParams{Agents: []string{tt.secondAgent}}); err != nil {
				t.Fatalf("second InitPairingCommand(%s) error = %v", tt.secondAgent, err)
			}

			repoPath := filepath.Join(gitDir, tt.repoFile)
			if target, err := os.Readlink(repoPath); err != nil || target != contractTarget {
				t.Fatalf("repo-only provider contract target = %q, err = %v; want %q", target, err, contractTarget)
			}
			globalPath := filepath.Join(fakeHome, tt.globalTarget)
			if target, err := os.Readlink(globalPath); err != nil || target != contractTarget {
				t.Fatalf("preferred global contract target = %q, err = %v; want %q", target, err, contractTarget)
			}
		})
	}
}

func TestInitPairingCommand_ProviderScopedConflictActionPreservesGlobalFirst(t *testing.T) {
	gitDir := setupGitRepo(t)
	fakeHome := setupGlobalLiza(t)
	contractTarget := filepath.Join(fakeHome, paths.GlobalDirName(), "CORE.md")
	if err := os.WriteFile(filepath.Join(gitDir, "AGENTS.md"), []byte("user-owned\n"), 0644); err != nil {
		t.Fatal(err)
	}

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalDir) })
	if err := os.Chdir(gitDir); err != nil {
		t.Fatal(err)
	}

	if err := InitPairingCommand(InitPairingParams{
		Agents:          []string{"cursor"},
		ContractActions: map[string]string{"cursor-acp": "rename"},
	}); err != nil {
		t.Fatalf("InitPairingCommand() error = %v", err)
	}

	globalPath := filepath.Join(fakeHome, ".codex", "AGENTS.md")
	if target, err := os.Readlink(globalPath); err != nil || target != contractTarget {
		t.Fatalf("Codex global contract target = %q, err = %v; want %q", target, err, contractTarget)
	}
	if content, err := os.ReadFile(filepath.Join(gitDir, "AGENTS.md.bak")); err != nil || string(content) != "user-owned\n" {
		t.Fatalf("Cursor conflict backup = %q, err = %v; want preserved user content", content, err)
	}
}

func TestInitPairingCommand_SharedRepoContractSurvivesGlobalActivationFailure(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)
	fakeHome := setupGlobalLiza(t)
	contractTarget := filepath.Join(fakeHome, paths.GlobalDirName(), "CORE.md")
	codexGlobalPath := filepath.Join(fakeHome, ".codex", "AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(codexGlobalPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codexGlobalPath, []byte("user-owned Codex instructions\n"), 0644); err != nil {
		t.Fatal(err)
	}

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(gitDir); err != nil {
		t.Fatal(err)
	}

	if err := InitPairingCommand(InitPairingParams{Agents: []string{"codex", "opencode"}}); err != nil {
		t.Fatalf("InitPairingCommand() error = %v", err)
	}
	repoPath := filepath.Join(gitDir, "AGENTS.md")
	if target, err := os.Readlink(repoPath); err != nil || target != contractTarget {
		t.Fatalf("shared repo AGENTS.md target = %q, err = %v; want %q", target, err, contractTarget)
	}
	opencodeGlobalPath := filepath.Join(fakeHome, ".config", "opencode", "AGENTS.md")
	if target, err := os.Readlink(opencodeGlobalPath); err != nil || target != contractTarget {
		t.Fatalf("OpenCode global AGENTS.md target = %q, err = %v; want %q", target, err, contractTarget)
	}
	if content, err := os.ReadFile(codexGlobalPath); err != nil || string(content) != "user-owned Codex instructions\n" {
		t.Fatalf("user-owned Codex global content = %q, err = %v", content, err)
	}
}

func TestCreateContractSymlinksForProviders_NormalizesSharedRepoPaths(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	projectRoot := t.TempDir()
	contractTarget := filepath.Join(homeDir, paths.GlobalDirName(), "CORE.md")
	blockedGlobalPath := filepath.Join(homeDir, ".first", "AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(blockedGlobalPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blockedGlobalPath, []byte("user-owned instructions\n"), 0644); err != nil {
		t.Fatal(err)
	}
	preferGlobal := true
	agents := []providers.Provider{
		{
			ID: "first",
			Setup: providers.Setup{Contract: providers.ContractLinks{
				RepoFile:       "AGENTS.md",
				GlobalFallback: ".first/AGENTS.md",
				PreferGlobal:   &preferGlobal,
			}},
		},
		{
			ID: "second",
			Setup: providers.Setup{Contract: providers.ContractLinks{
				RepoFile:       "./AGENTS.md",
				GlobalFallback: ".second/AGENTS.md",
				PreferGlobal:   &preferGlobal,
			}},
		},
	}

	createContractSymlinksForProviders(projectRoot, contractTarget, agents, contractSymlinkOptions{})
	repoPath := filepath.Join(projectRoot, "AGENTS.md")
	if target, err := os.Readlink(repoPath); err != nil || target != contractTarget {
		t.Fatalf("normalized shared repo target = %q, err = %v; want %q", target, err, contractTarget)
	}
	secondGlobalPath := filepath.Join(homeDir, ".second", "AGENTS.md")
	if target, err := os.Readlink(secondGlobalPath); err != nil || target != contractTarget {
		t.Fatalf("second global target = %q, err = %v; want %q", target, err, contractTarget)
	}
}

func TestInitPairingCommand_CursorWarnsWhenBashPolicyMissing(t *testing.T) {
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)
	fakeHome := setupGlobalLiza(t)
	writeOldCachedCursorProviderCatalog(t, fakeHome)
	t.Setenv(bashpolicycli.EnvEnableBashPolicy, "1")

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(gitDir)

	runner := &initBashPolicyTestRunner{}
	restore := setInitBashPolicyHooksForTest(
		func(name string) (string, error) {
			return "", exec.ErrNotFound
		},
		runner,
	)
	defer restore()

	stderr, err := captureStderrForTest(func() error {
		return InitPairingCommand(InitPairingParams{Agents: []string{"cursor"}})
	})
	if err != nil {
		t.Fatalf("InitPairingCommand() error = %v", err)
	}
	if !strings.Contains(stderr, "bash-policy requested by LIZA_ENABLE_BASH_POLICY") {
		t.Fatalf("stderr missing Cursor bash-policy warning:\n%s", stderr)
	}
	if len(runner.commands) != 0 {
		t.Fatalf("bash-policy commands = %d, want zero when executable is missing", len(runner.commands))
	}
}

func TestInitPairingCommand_CursorDoesNotTouchExistingHooksWhenBashPolicyMissing(t *testing.T) {
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)
	fakeHome := setupGlobalLiza(t)
	writeOldCachedCursorProviderCatalog(t, fakeHome)
	t.Setenv(bashpolicycli.EnvEnableBashPolicy, "1")
	cursorDir := filepath.Join(gitDir, ".cursor")
	if err := os.MkdirAll(cursorDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cursorDir, "hooks.json"), []byte(`{"version":1,"hooks":{}}`), 0644); err != nil {
		t.Fatal(err)
	}

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(gitDir)

	runner := &initBashPolicyTestRunner{}
	restore := setInitBashPolicyHooksForTest(
		func(name string) (string, error) {
			return "", exec.ErrNotFound
		},
		runner,
	)
	defer restore()

	stderr, err := captureStderrForTest(func() error {
		return InitPairingCommand(InitPairingParams{
			Agents: []string{"cursor"},
			Stdin:  strings.NewReader("n\n"),
		})
	})
	if err != nil {
		t.Fatalf("InitPairingCommand() error = %v", err)
	}
	if !strings.Contains(stderr, "bash-policy requested by LIZA_ENABLE_BASH_POLICY") {
		t.Fatalf("stderr missing Cursor bash-policy warning:\n%s", stderr)
	}
	if _, err := os.Stat(filepath.Join(cursorDir, "hooks", "cursor-bash-policy.sh")); !os.IsNotExist(err) {
		t.Fatalf("Cursor hook script state = %v, want absent because Liza no longer writes it", err)
	}
	content, err := os.ReadFile(filepath.Join(cursorDir, "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != `{"version":1,"hooks":{}}` {
		t.Fatalf("Cursor hooks.json changed despite bash-policy missing:\n%s", string(content))
	}
	if len(runner.commands) != 0 {
		t.Fatalf("bash-policy commands = %d, want zero when executable is missing", len(runner.commands))
	}
}

func TestInitPairingCommand_BashPolicyProviderScope(t *testing.T) {
	tests := []struct {
		name      string
		agents    []string
		providers []string
		wantRun   bool
	}{
		{name: "claude", agents: []string{"claude"}, providers: []string{bashpolicycli.ProviderClaude}, wantRun: true},
		{name: "codex", agents: []string{"codex"}, providers: []string{bashpolicycli.ProviderCodex}, wantRun: true},
		{name: "cursor", agents: []string{"cursor"}, providers: []string{bashpolicycli.ProviderClaude, bashpolicycli.ProviderCodex, bashpolicycli.ProviderCursor}, wantRun: true},
		{name: "claude and codex", agents: []string{"claude", "codex"}, providers: []string{bashpolicycli.ProviderClaude, bashpolicycli.ProviderCodex}, wantRun: true},
		{name: "unsupported providers only", agents: []string{"gemini", "opencode"}, wantRun: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gitDir := setupGitRepo(t)
			defer os.RemoveAll(gitDir)
			setupGlobalLiza(t)
			t.Setenv(bashpolicycli.EnvEnableBashPolicy, "true")

			runner := &initBashPolicyTestRunner{}
			restore := setInitBashPolicyHooksForTest(
				func(name string) (string, error) {
					return filepath.Join(gitDir, "bin", name), nil
				},
				runner,
			)
			defer restore()

			originalDir, _ := os.Getwd()
			defer os.Chdir(originalDir)
			os.Chdir(gitDir)

			if err := InitPairingCommand(InitPairingParams{Agents: tt.agents}); err != nil {
				t.Fatalf("InitPairingCommand() error = %v", err)
			}
			if !tt.wantRun {
				if len(runner.commands) != 0 {
					t.Fatalf("bash-policy commands = %d, want zero", len(runner.commands))
				}
				if _, err := os.Stat(filepath.Join(gitDir, ".bash-policy.yaml")); !os.IsNotExist(err) {
					t.Fatalf(".bash-policy.yaml state = %v, want absent without supported provider", err)
				}
				return
			}
			assertBashPolicyCommands(t, runner.commands, gitDir, tt.providers)
			if _, err := os.Stat(filepath.Join(gitDir, ".bash-policy.yaml")); err != nil {
				t.Fatalf(".bash-policy.yaml was not created for supported provider: %v", err)
			}
		})
	}
}

func TestInitPairingCommand_BashPolicyAutoConfirmPassesYesToSubprocesses(t *testing.T) {
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)
	setupGlobalLiza(t)
	t.Setenv(bashpolicycli.EnvEnableBashPolicy, "true")

	runner := &initBashPolicyTestRunner{}
	restore := setInitBashPolicyHooksForTest(
		func(name string) (string, error) {
			return filepath.Join(gitDir, "bin", name), nil
		},
		runner,
	)
	defer restore()

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(gitDir)

	if err := InitPairingCommand(InitPairingParams{
		Agents:      []string{"claude", "codex"},
		Stdin:       strings.NewReader(""),
		AutoConfirm: true,
	}); err != nil {
		t.Fatalf("InitPairingCommand() error = %v", err)
	}

	assertBashPolicyCommands(t, runner.commands, gitDir, []string{bashpolicycli.ProviderClaude, bashpolicycli.ProviderCodex})
	for i, command := range runner.commands {
		content, err := io.ReadAll(command.Stdin)
		if err != nil {
			t.Fatalf("read command %d stdin: %v", i, err)
		}
		if string(content) != strings.Repeat("yes\n", 16) {
			t.Fatalf("command %d stdin = %q, want scripted yes input", i, string(content))
		}
	}
}

func TestBashPolicySubprocessStdinKeepsBufferedReaderForScriptedInput(t *testing.T) {
	raw := strings.NewReader("y\n")
	buffered := bufio.NewReader(raw)

	got := bashPolicySubprocessStdin(raw, buffered)

	if got != buffered {
		t.Fatalf("stdin = %T, want shared buffered reader for scripted input", got)
	}
}

func TestBashPolicySubprocessStdinKeepsBufferedReaderForRegularFiles(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "stdin-*")
	if err != nil {
		t.Fatalf("create temp stdin: %v", err)
	}
	defer file.Close()
	buffered := bufio.NewReader(file)

	got := bashPolicySubprocessStdin(file, buffered)

	if got != buffered {
		t.Fatalf("stdin = %T, want shared buffered reader for regular file input", got)
	}
}

func TestBashPolicySubprocessStdinUsesRawCharacterDevice(t *testing.T) {
	file, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open os.DevNull: %v", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		t.Fatalf("stat os.DevNull: %v", err)
	}
	if info.Mode()&os.ModeCharDevice == 0 {
		t.Skip("os.DevNull is not reported as a character device on this platform")
	}
	buffered := bufio.NewReader(file)

	got := bashPolicySubprocessStdin(file, buffered)

	if got != file {
		t.Fatalf("stdin = %T, want raw character device", got)
	}
}

func TestInitPairingCommand_BashPolicyWarnsWhenMissing(t *testing.T) {
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)
	setupGlobalLiza(t)
	t.Setenv(bashpolicycli.EnvEnableBashPolicy, "1")

	restore := setInitBashPolicyHooksForTest(
		func(string) (string, error) {
			return "", errors.New("not found")
		},
		&initBashPolicyTestRunner{},
	)
	defer restore()

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(gitDir)

	stderr, err := captureStderrForTest(func() error {
		return InitPairingCommand(InitPairingParams{Agents: []string{"claude"}})
	})
	if err != nil {
		t.Fatalf("InitPairingCommand() error = %v", err)
	}
	if !strings.Contains(stderr, "bash-policy requested by LIZA_ENABLE_BASH_POLICY") {
		t.Fatalf("stderr missing bash-policy missing warning:\n%s", stderr)
	}
}

func TestInitPairingCommand_BashPolicyWarnsWhenInitFails(t *testing.T) {
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)
	setupGlobalLiza(t)
	t.Setenv(bashpolicycli.EnvEnableBashPolicy, "true")

	runner := &initBashPolicyTestRunner{
		output: bashpolicycli.CommandOutput{Stderr: "policy failure detail"},
		err:    errors.New("exit status 2"),
	}
	restore := setInitBashPolicyHooksForTest(
		func(name string) (string, error) {
			return filepath.Join(gitDir, "bin", name), nil
		},
		runner,
	)
	defer restore()

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(gitDir)

	stderr, err := captureStderrForTest(func() error {
		return InitPairingCommand(InitPairingParams{Agents: []string{"claude", "codex"}})
	})
	if err != nil {
		t.Fatalf("InitPairingCommand() error = %v", err)
	}
	for _, want := range []string{"failed to initialize or activate bash-policy hooks", "policy failure detail"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr missing %q:\n%s", want, stderr)
		}
	}
}

func TestInitPairingCommand_DisabledIndexGatesPreserveProviderHooksOnly(t *testing.T) {
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)
	unsetEnvForTest(t, stacklit.EnvEnableStacklit)
	unsetEnvForTest(t, scipsearch.EnvEnableScipSearch)
	unsetEnvForTest(t, functionalclusters.EnvEnableFunctionalClusters)
	unsetEnvForTest(t, semble.EnvEnableSemble)
	setupGlobalLiza(t)

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(gitDir)

	var scipCalls int
	restoreScip := scipsearch.SetCommandRunnerForTest(func(name string, args ...string) (string, error) {
		scipCalls++
		return "unexpected\n", nil
	})
	defer restoreScip()
	var sembleLookups int
	restoreSemble := setInitSembleHooksForTest(
		func(name string) (string, error) {
			sembleLookups++
			return filepath.Join(gitDir, "bin", name), nil
		},
		func(plan semble.CommandPlan) (semble.CommandResult, error) {
			t.Fatalf("Semble runner called with disabled gate")
			return semble.CommandResult{}, nil
		},
	)
	defer restoreSemble()

	if err := InitPairingCommand(InitPairingParams{Agents: []string{"claude", "codex"}}); err != nil {
		t.Fatalf("InitPairingCommand() error = %v", err)
	}

	verifyCodexHooks(t, gitDir)
	if scipCalls != 0 {
		t.Fatalf("scip calls = %d, want zero", scipCalls)
	}
	if sembleLookups != 0 {
		t.Fatalf("Semble lookups = %d, want zero", sembleLookups)
	}
	for _, rel := range []string{".git/hooks/" + brand.BinaryName + "-index.sh", ".sembleignore"} {
		if _, err := os.Stat(filepath.Join(gitDir, rel)); !os.IsNotExist(err) {
			t.Fatalf("%s stat err = %v, want missing", rel, err)
		}
	}
}

func TestInitPairingCommand_DisabledScipGateIgnoresExplicitLanguageFilter(t *testing.T) {
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)
	unsetEnvForTest(t, stacklit.EnvEnableStacklit)
	unsetEnvForTest(t, scipsearch.EnvEnableScipSearch)
	unsetEnvForTest(t, functionalclusters.EnvEnableFunctionalClusters)
	unsetEnvForTest(t, semble.EnvEnableSemble)
	setupGlobalLiza(t)
	writeTrackedFile(t, gitDir, "go.mod", "module example.com/project\n")
	testhelpers.MustGit(t, gitDir, "add", "go.mod")
	testhelpers.MustGit(t, gitDir, "commit", "-m", "Add Go module")

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(gitDir)

	if err := InitPairingCommand(InitPairingParams{
		Agents:     []string{"codex"},
		ScipSearch: []string{"go"},
	}); err != nil {
		t.Fatalf("InitPairingCommand() error = %v", err)
	}

	verifyCodexHooks(t, gitDir)
	if _, statErr := os.Stat(filepath.Join(gitDir, ".git", "hooks", brand.BinaryName+"-index.sh")); !os.IsNotExist(statErr) {
		t.Fatalf("liza-index.sh stat err = %v, want missing when SCIP env gate is disabled", statErr)
	}
	exclude, err := os.ReadFile(filepath.Join(gitDir, ".git", "info", "exclude"))
	if err != nil {
		t.Fatalf("read git private exclude: %v", err)
	}
	if strings.Contains(string(exclude), "go.scip") {
		t.Fatalf("git private exclude contains go.scip with SCIP env gate disabled:\n%s", exclude)
	}
}

func TestInitPairingCommand_StacklitEnabledInstallsIndexHooksWithoutGlobalToolsMutation(t *testing.T) {
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)
	fakeHome := setupGlobalLiza(t)
	t.Setenv(stacklit.EnvEnableStacklit, "true")

	toolsPath := filepath.Join(fakeHome, paths.GlobalDirName(), "AGENT_TOOLS.md")
	if err := os.WriteFile(toolsPath, []byte("global tools sentinel\n"), 0644); err != nil {
		t.Fatalf("write AGENT_TOOLS.md sentinel: %v", err)
	}
	beforeTools, err := os.ReadFile(toolsPath)
	if err != nil {
		t.Fatalf("read AGENT_TOOLS.md before init: %v", err)
	}
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(gitDir)

	if err := InitPairingCommand(InitPairingParams{Agents: []string{"claude", "codex"}}); err != nil {
		t.Fatalf("InitPairingCommand() error = %v", err)
	}

	verifyCodexHooks(t, gitDir)
	scriptPath := filepath.Join(gitDir, ".git", "hooks", brand.BinaryName+"-index.sh")
	script := readFileForTest(t, scriptPath)
	if !strings.Contains(script, pairingindex.ManagedIndexScriptMarker) {
		t.Fatalf("liza-index.sh missing managed marker:\n%s", script)
	}
	for _, hook := range pairingindex.DefaultLifecycleHooks() {
		content := readFileForTest(t, filepath.Join(gitDir, ".git", "hooks", hook))
		if !strings.Contains(content, pairingindex.ManagedHookMarker) {
			t.Fatalf("%s missing pairing index marker:\n%s", hook, content)
		}
	}
	if got := runGitOutputForTest(t, gitDir, "check-ignore", "stacklit.json"); got != "stacklit.json" {
		t.Fatalf("git check-ignore stacklit.json = %q, want private exclude", got)
	}
	afterTools, err := os.ReadFile(toolsPath)
	if err != nil {
		t.Fatalf("read AGENT_TOOLS.md after init: %v", err)
	}
	if string(afterTools) != string(beforeTools) {
		t.Fatal("pairing init modified global AGENT_TOOLS.md")
	}
}

func TestInitPairingCommand_ScipSearchGoFilterPlansConcreteHookCommand(t *testing.T) {
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)
	setupGlobalLiza(t)
	t.Setenv(scipsearch.EnvEnableScipSearch, "true")
	writeTrackedFile(t, gitDir, "go.mod", "module example.com/project\n")
	writeTrackedFile(t, gitDir, "web/package.json", "{}\n")
	writeTrackedFile(t, gitDir, "web/app.ts", "export const app = 1\n")
	testhelpers.MustGit(t, gitDir, "add", "go.mod", "web/package.json", "web/app.ts")
	testhelpers.MustGit(t, gitDir, "commit", "-m", "Add tracked source")

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(gitDir)

	if err := InitPairingCommand(InitPairingParams{
		Agents:     []string{"codex"},
		ScipSearch: []string{"go"},
	}); err != nil {
		t.Fatalf("InitPairingCommand() error = %v", err)
	}

	script := readFileForTest(t, filepath.Join(gitDir, ".git", "hooks", brand.BinaryName+"-index.sh"))
	if !strings.Contains(script, "scip-go index --module-root") {
		t.Fatalf("liza-index.sh missing Go SCIP command:\n%s", script)
	}
	if strings.Contains(script, "scip-typescript") || strings.Contains(script, "scip-python") {
		t.Fatalf("liza-index.sh ignored --scip-search go filter:\n%s", script)
	}
}

func TestInitPairingCommand_ScipSearchPlanOverridesAmbiguousRoots(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)
	setupGlobalLiza(t)
	t.Setenv(scipsearch.EnvEnableScipSearch, "true")
	writeTrackedFile(t, gitDir, "services/design-diagnosis/cli/go.mod", "module example.com/cli\n")
	writeTrackedFile(t, gitDir, "services/design-diagnosis/cli/main.go", "package main\n")
	writeTrackedFile(t, gitDir, "apps/web/tsconfig.json", "{}\n")
	writeTrackedFile(t, gitDir, "apps/web/src/App.tsx", "export const app = 1\n")
	writeTrackedFile(t, gitDir, "infra/cdk/tsconfig.json", "{}\n")
	writeTrackedFile(t, gitDir, "infra/cdk/app.ts", "export const cdk = 1\n")
	writeTrackedFile(t, gitDir, "apps/api/pyproject.toml", "[project]\nname = \"api\"\n")
	writeTrackedFile(t, gitDir, "apps/api/backend/main.py", "print('api')\n")
	writeTrackedFile(t, gitDir, "services/design-diagnosis/pyproject.toml", "[project]\nname = \"service\"\n")
	writeTrackedFile(t, gitDir, "services/design-diagnosis/app.py", "print('service')\n")
	testhelpers.MustGit(t, gitDir, "add", ".")
	testhelpers.MustGit(t, gitDir, "commit", "-m", "Add monorepo source roots")

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(gitDir)

	if err := InitPairingCommand(InitPairingParams{
		Agents: []string{"codex"},
		ScipSearchPlans: []string{
			"go=services/design-diagnosis/cli",
			"typescript=apps/web/src,apps/web",
			"python=apps/api",
		},
	}); err != nil {
		t.Fatalf("InitPairingCommand() error = %v", err)
	}

	script := readFileForTest(t, filepath.Join(gitDir, ".git", "hooks", brand.BinaryName+"-index.sh"))
	for _, want := range []string{
		"scip-go index --module-root " + testhelpers.ShellArg(filepath.Join(gitDir, "services", "design-diagnosis", "cli")) + " --output ",
		"scip-typescript index --cwd " + testhelpers.ShellArg(filepath.Join(gitDir, "apps", "web", "src")) + " --output ",
		"scip-python index --cwd " + testhelpers.ShellArg(filepath.Join(gitDir, "apps", "api")) + " --output ",
		"scip-search aggregate-index --project-root " + testhelpers.ShellArg(gitDir),
		"--root services/design-diagnosis/cli --index ",
		"--root apps/web/src --index ",
		"--root apps/api --index ",
		"--out " + testhelpers.ShellArg(filepath.Join(gitDir, "go.scip")),
		"--out " + testhelpers.ShellArg(filepath.Join(gitDir, "typescript.scip")),
		"--out " + testhelpers.ShellArg(filepath.Join(gitDir, "python.scip")),
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("liza-index.sh missing override command %q:\n%s", want, script)
		}
	}
}

func TestInitPairingCommand_ScipSearchEnabledWithNoLanguagesSkipsInertHooks(t *testing.T) {
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)
	setupGlobalLiza(t)
	t.Setenv(scipsearch.EnvEnableScipSearch, "true")

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(gitDir)

	if err := InitPairingCommand(InitPairingParams{Agents: []string{"codex"}}); err != nil {
		t.Fatalf("InitPairingCommand() error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(gitDir, ".git", "hooks", brand.BinaryName+"-index.sh")); !os.IsNotExist(statErr) {
		t.Fatalf("liza-index.sh stat err = %v, want missing with no SCIP language plans", statErr)
	}
}

func TestInitPairingCommand_ScipSearchSkipsStrayTypeScriptWithoutTSConfig(t *testing.T) {
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)
	setupGlobalLiza(t)
	t.Setenv(scipsearch.EnvEnableScipSearch, "true")
	writeTrackedFile(t, gitDir, "internal/embedded/opencode-tools/exec.ts", "export const exec = 1\n")
	testhelpers.MustGit(t, gitDir, "add", "internal/embedded/opencode-tools/exec.ts")
	testhelpers.MustGit(t, gitDir, "commit", "-m", "Add embedded TypeScript helper")

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(gitDir)

	if err := InitPairingCommand(InitPairingParams{Agents: []string{"codex"}}); err != nil {
		t.Fatalf("InitPairingCommand() error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(gitDir, ".git", "hooks", brand.BinaryName+"-index.sh")); !os.IsNotExist(statErr) {
		t.Fatalf("liza-index.sh stat err = %v, want missing with only stray TypeScript source", statErr)
	}
}

func TestInitPairingCommand_ScipSearchMultiRootInstallsAggregateHooks(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)
	setupGlobalLiza(t)
	t.Setenv(scipsearch.EnvEnableScipSearch, "true")
	writeTrackedFile(t, gitDir, "service-a/go.mod", "module example.com/a\n")
	writeTrackedFile(t, gitDir, "service-b/go.mod", "module example.com/b\n")
	testhelpers.MustGit(t, gitDir, "add", "service-a/go.mod", "service-b/go.mod")
	testhelpers.MustGit(t, gitDir, "commit", "-m", "Add ambiguous Go roots")

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(gitDir)

	err := InitPairingCommand(InitPairingParams{
		Agents:     []string{"codex"},
		ScipSearch: []string{"go"},
	})
	if err != nil {
		t.Fatalf("InitPairingCommand() error = %v", err)
	}
	script := readFileForTest(t, filepath.Join(gitDir, ".git", "hooks", brand.BinaryName+"-index.sh"))
	for _, want := range []string{"--root service-a --index ", "--root service-b --index ", "--out " + testhelpers.ShellArg(filepath.Join(gitDir, "go.scip"))} {
		if !strings.Contains(script, want) {
			t.Fatalf("liza-index.sh = %q, want %q", script, want)
		}
	}
}

func TestInitPairingCommand_AmbientScipSearchAggregatesMultiRoot(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)
	setupGlobalLiza(t)
	t.Setenv(scipsearch.EnvEnableScipSearch, "true")
	writeTrackedFile(t, gitDir, "service-a/go.mod", "module example.com/a\n")
	writeTrackedFile(t, gitDir, "service-b/go.mod", "module example.com/b\n")
	testhelpers.MustGit(t, gitDir, "add", "service-a/go.mod", "service-b/go.mod")
	testhelpers.MustGit(t, gitDir, "commit", "-m", "Add ambiguous Go roots")

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(gitDir)

	stderr, err := captureStderrForTest(func() error {
		return InitPairingCommand(InitPairingParams{Agents: []string{"codex"}})
	})
	if err != nil {
		t.Fatalf("InitPairingCommand() error = %v", err)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want no multi-root warning", stderr)
	}
	script := readFileForTest(t, filepath.Join(gitDir, ".git", "hooks", brand.BinaryName+"-index.sh"))
	for _, want := range []string{"--root service-a --index ", "--root service-b --index ", "--out " + testhelpers.ShellArg(filepath.Join(gitDir, "go.scip"))} {
		if !strings.Contains(script, want) {
			t.Fatalf("liza-index.sh = %q, want %q", script, want)
		}
	}
}

func TestInitPairingCommand_SembleEnabledEnsuresProjectRootIgnore(t *testing.T) {
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)
	setupGlobalLiza(t)
	t.Setenv(semble.EnvEnableSemble, "true")

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(gitDir)

	restoreSemble := setInitSembleHooksForTest(
		func(name string) (string, error) {
			return filepath.Join(gitDir, "bin", name), nil
		},
		func(plan semble.CommandPlan) (semble.CommandResult, error) {
			return semble.CommandResult{ExitCode: 0}, nil
		},
	)
	defer restoreSemble()

	if err := InitPairingCommand(InitPairingParams{Agents: []string{"gemini"}}); err != nil {
		t.Fatalf("InitPairingCommand() error = %v", err)
	}

	ignore := readFileForTest(t, filepath.Join(gitDir, ".sembleignore"))
	for _, want := range []string{paths.ProjectDirName() + "/", ".worktrees/", "stacklit.json", "*.scip", "*.pem"} {
		if !strings.Contains(ignore, want) {
			t.Fatalf(".sembleignore missing %q:\n%s", want, ignore)
		}
	}
}

func TestInitPairingCommand_IndexHookFailuresReportDiagnostics(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(t *testing.T, repo string)
		wantError string
	}{
		{
			name: "existing lifecycle hook collision",
			setup: func(t *testing.T, repo string) {
				writeFileForTest(t, filepath.Join(repo, ".git", "hooks", "post-commit"), "#!/bin/sh\necho user hook\n", 0755)
			},
			wantError: "not " + brand.NameTitle + "-managed",
		},
		{
			name: "legacy index script collision",
			setup: func(t *testing.T, repo string) {
				writeFileForTest(t, filepath.Join(repo, ".git", "hooks", brand.BinaryName+"-index.sh"), "#!/bin/sh\n# Refresh scip and stacklit indexes for current branch in liza pairing mode.\n", 0755)
			},
			wantError: "appears to be a legacy managed index hook",
		},
		{
			name: "unsafe hooksPath file",
			setup: func(t *testing.T, repo string) {
				hooksPath := filepath.Join(repo, ".git", "hooks-file")
				writeFileForTest(t, hooksPath, "not a directory\n", 0644)
				testhelpers.MustGit(t, repo, "config", "core.hooksPath", hooksPath)
			},
			wantError: "not a directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gitDir := setupGitRepo(t)
			defer os.RemoveAll(gitDir)
			setupGlobalLiza(t)
			t.Setenv(stacklit.EnvEnableStacklit, "true")
			tt.setup(t, gitDir)

			originalDir, _ := os.Getwd()
			defer os.Chdir(originalDir)
			os.Chdir(gitDir)

			err := InitPairingCommand(InitPairingParams{Agents: []string{"codex"}})
			if err == nil {
				t.Fatal("InitPairingCommand() error = nil, want indexing diagnostic")
			}
			if !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("error = %v, want %q", err, tt.wantError)
			}
		})
	}
}

func TestInitPairingCommand_Idempotent(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)

	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)
	fakeHome := setupGlobalLiza(t)

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(gitDir)

	// Run twice
	for i := 0; i < 2; i++ {
		err := InitPairingCommand(InitPairingParams{
			Agents: []string{"claude"},
		})
		if err != nil {
			t.Fatalf("run %d: InitPairingCommand failed: %v", i+1, err)
		}
	}

	target, err := os.Readlink(filepath.Join(fakeHome, ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("global CLAUDE.md not a symlink: %v", err)
	}
	expected := filepath.Join(fakeHome, paths.GlobalDirName(), "CORE.md")
	if target != expected {
		t.Errorf("CLAUDE.md → %q, want %q", target, expected)
	}
}

func TestInitPairingCommand_Mistral(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	fakeHome := setupGlobalLiza(t)

	err := InitPairingCommand(InitPairingParams{
		Agents: []string{"mistral"},
	})
	if err != nil {
		t.Fatalf("InitPairingCommand failed: %v", err)
	}

	// The provider prompt should be a symlink to the global managed contract.
	linkPath := filepath.Join(fakeHome, ".vibe", "prompts", "liza.md")
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("liza.md not a symlink: %v", err)
	}
	expected := filepath.Join(fakeHome, paths.GlobalDirName(), "CORE.md")
	if target != expected {
		t.Errorf("liza.md → %q, want %q", target, expected)
	}

	// config.toml should contain system_prompt_id = "liza"
	configPath := filepath.Join(fakeHome, ".vibe", "config.toml")
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("config.toml not created: %v", err)
	}
	if !strings.Contains(string(content), `system_prompt_id = "liza"`) {
		t.Errorf("config.toml missing system_prompt_id = \"liza\", got:\n%s", content)
	}
}

func TestInitPairingCommand_MistralReplacesExistingPromptID(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	fakeHome := setupGlobalLiza(t)

	// Pre-create config.toml with system_prompt_id = "cli"
	vibeDir := filepath.Join(fakeHome, ".vibe")
	os.MkdirAll(vibeDir, 0755)
	configPath := filepath.Join(vibeDir, "config.toml")
	os.WriteFile(configPath, []byte("system_prompt_id = \"cli\"\nother_setting = true\n"), 0644)

	// Provide "y\n" for the config.toml overwrite prompt
	err := InitPairingCommand(InitPairingParams{
		Agents: []string{"mistral"},
		Stdin:  strings.NewReader("y\n"),
	})
	if err != nil {
		t.Fatalf("InitPairingCommand failed: %v", err)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	if !strings.Contains(text, `system_prompt_id = "liza"`) {
		t.Errorf("system_prompt_id not replaced, got:\n%s", text)
	}
	if strings.Contains(text, `system_prompt_id = "cli"`) {
		t.Errorf("old system_prompt_id = \"cli\" still present, got:\n%s", text)
	}
	if !strings.Contains(text, "other_setting = true") {
		t.Error("other settings were lost during config.toml update")
	}
}

func TestInitPairingCommand_MistralAutoConfirmReplacesExistingPromptID(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	fakeHome := setupGlobalLiza(t)

	vibeDir := filepath.Join(fakeHome, ".vibe")
	if err := os.MkdirAll(vibeDir, 0755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(vibeDir, "config.toml")
	if err := os.WriteFile(configPath, []byte("system_prompt_id = \"cli\"\nother_setting = true\n"), 0644); err != nil {
		t.Fatal(err)
	}

	err := InitPairingCommand(InitPairingParams{
		Agents:      []string{"mistral"},
		Stdin:       strings.NewReader(""),
		AutoConfirm: true,
	})
	if err != nil {
		t.Fatalf("InitPairingCommand failed: %v", err)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	if !strings.Contains(text, `system_prompt_id = "liza"`) {
		t.Errorf("system_prompt_id not replaced, got:\n%s", text)
	}
	if strings.Contains(text, `system_prompt_id = "cli"`) {
		t.Errorf("old system_prompt_id = \"cli\" still present, got:\n%s", text)
	}
	if !strings.Contains(text, "other_setting = true") {
		t.Error("other settings were lost during config.toml update")
	}
}

func TestInitPairingCommand_MistralDeclinesOverwrite(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	fakeHome := setupGlobalLiza(t)

	// Pre-create config.toml with system_prompt_id = "cli"
	vibeDir := filepath.Join(fakeHome, ".vibe")
	os.MkdirAll(vibeDir, 0755)
	configPath := filepath.Join(vibeDir, "config.toml")
	os.WriteFile(configPath, []byte("system_prompt_id = \"cli\"\n"), 0644)

	// Decline the config.toml overwrite
	err := InitPairingCommand(InitPairingParams{
		Agents: []string{"mistral"},
		Stdin:  strings.NewReader("n\n"),
	})
	if err != nil {
		t.Fatalf("InitPairingCommand failed: %v", err)
	}

	// config.toml should still have "cli"
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), `system_prompt_id = "cli"`) {
		t.Error("config.toml was modified despite user declining")
	}
}

// TestInitPairingCommand_ClaudeBrownfieldUsesGlobalFallback verifies that when
// CLAUDE.md already exists at repo root, the Liza symlink goes to ~/.claude/CLAUDE.md.
func TestInitPairingCommand_ClaudeBrownfieldUsesGlobalFallback(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)
	fakeHome := setupGlobalLiza(t)

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(gitDir)

	coreFile := filepath.Join(fakeHome, paths.GlobalDirName(), "CORE.md")

	// Pre-create CLAUDE.md as a regular file (brownfield project)
	os.WriteFile(filepath.Join(gitDir, "CLAUDE.md"), []byte("existing"), 0644)

	// Pre-create .claude/settings.json to trigger merge prompt
	claudeDir := filepath.Join(gitDir, ".claude")
	os.MkdirAll(claudeDir, 0755)
	os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(`{"existing": true}`), 0644)

	// One "y\n" answer for settings merge (CLAUDE.md no longer prompts)
	err := InitPairingCommand(InitPairingParams{
		Agents: []string{"claude"},
		Stdin:  strings.NewReader("y\n"),
	})
	if err != nil {
		t.Fatalf("InitPairingCommand failed: %v", err)
	}

	// Repo root CLAUDE.md should be untouched
	content, err := os.ReadFile(filepath.Join(gitDir, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "existing" {
		t.Errorf("repo root CLAUDE.md was modified; got %q", string(content))
	}

	// Liza symlink should be at global fallback (~/.claude/CLAUDE.md)
	globalClaude := filepath.Join(fakeHome, ".claude", "CLAUDE.md")
	target, err := os.Readlink(globalClaude)
	if err != nil {
		t.Fatalf("Global fallback symlink not created at %s: %v", globalClaude, err)
	}
	if target != coreFile {
		t.Errorf("Global fallback → %q, want %q", target, coreFile)
	}

	// settings.json should have been merged
	settingsData, err := os.ReadFile(filepath.Join(claudeDir, "settings.json"))
	if err != nil {
		t.Fatalf("failed to read settings.json: %v", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(settingsData, &settings); err != nil {
		t.Fatalf("settings.json is not valid JSON: %v", err)
	}
	if _, ok := settings["existing"]; !ok {
		t.Error("settings.json lost existing user key during merge")
	}
}

func TestDetectPostWorktreeCmd(t *testing.T) {
	tests := []struct {
		name    string
		files   []string // files to create in tmpDir
		wantCmd string
		wantCtx string // expected detectPkgManagerContext output
	}{
		{
			name:    "no package.json",
			files:   nil,
			wantCmd: "",
		},
		{
			name:    "package.json only — defaults to npm",
			files:   []string{"package.json"},
			wantCmd: "npm install",
			wantCtx: "package.json",
		},
		{
			name:    "package.json + package-lock.json",
			files:   []string{"package.json", "package-lock.json"},
			wantCmd: "npm install",
			wantCtx: "package.json + package-lock.json",
		},
		{
			name:    "package.json + yarn.lock",
			files:   []string{"package.json", "yarn.lock"},
			wantCmd: "yarn install",
			wantCtx: "package.json + yarn.lock",
		},
		{
			name:    "package.json + pnpm-lock.yaml",
			files:   []string{"package.json", "pnpm-lock.yaml"},
			wantCmd: "pnpm install",
			wantCtx: "package.json + pnpm-lock.yaml",
		},
		{
			name:    "package.json + bun.lockb",
			files:   []string{"package.json", "bun.lockb"},
			wantCmd: "bun install",
			wantCtx: "package.json + bun.lockb",
		},
		{
			name:    "package.json + bun.lock",
			files:   []string{"package.json", "bun.lock"},
			wantCmd: "bun install",
			wantCtx: "package.json + bun.lock",
		},
		{
			name:    "pnpm takes precedence over npm",
			files:   []string{"package.json", "pnpm-lock.yaml", "package-lock.json"},
			wantCmd: "pnpm install",
			wantCtx: "package.json + pnpm-lock.yaml",
		},
		{
			name:    "single subdir with package.json",
			files:   []string{"web/package.json", "web/package-lock.json"},
			wantCmd: "cd web && npm install",
			wantCtx: "web/package.json + web/package-lock.json",
		},
		{
			name:    "single subdir with yarn",
			files:   []string{"frontend/package.json", "frontend/yarn.lock"},
			wantCmd: "cd frontend && yarn install",
			wantCtx: "frontend/package.json + frontend/yarn.lock",
		},
		{
			name:    "multiple subdirs — no auto-suggestion",
			files:   []string{"web/package.json", "api/package.json"},
			wantCmd: "",
		},
		{
			name:    "root package.json takes precedence over subdir",
			files:   []string{"package.json", "web/package.json"},
			wantCmd: "npm install",
			wantCtx: "package.json",
		},
		{
			name:    "dotfile subdir ignored",
			files:   []string{".hidden/package.json"},
			wantCmd: "",
		},
		{
			name:    "node_modules subdir ignored",
			files:   []string{"node_modules/package.json"},
			wantCmd: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			for _, f := range tt.files {
				fullPath := filepath.Join(tmpDir, f)
				if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(fullPath, []byte("{}"), 0644); err != nil {
					t.Fatal(err)
				}
			}

			got := detectPostWorktreeCmd(tmpDir)
			if got != tt.wantCmd {
				t.Errorf("detectPostWorktreeCmd() = %q, want %q", got, tt.wantCmd)
			}

			if tt.wantCtx != "" {
				gotCtx := detectPkgManagerContext(tmpDir)
				if gotCtx != tt.wantCtx {
					t.Errorf("detectPkgManagerContext() = %q, want %q", gotCtx, tt.wantCtx)
				}
			}
		})
	}
}

func TestConfirmMissingPostWorktreeCmd_MultipleNodeSubdirsExplainsAmbiguity(t *testing.T) {
	tmpDir := t.TempDir()
	for _, subdir := range []string{"api", "web"} {
		dir := filepath.Join(tmpDir, subdir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("os.MkdirAll(%q) error = %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}\n"), 0o644); err != nil {
			t.Fatalf("os.WriteFile(%q) error = %v", filepath.Join(dir, "package.json"), err)
		}
	}

	rawStdin := strings.NewReader("y\n")
	stderr, err := captureStderrForTest(func() error {
		return confirmMissingPostWorktreeCmd(
			InitParams{ForceInteractive: true},
			tmpDir,
			bufio.NewReader(rawStdin),
			rawStdin,
		)
	})
	if err != nil {
		t.Fatalf("confirmMissingPostWorktreeCmd() error = %v", err)
	}
	if !strings.Contains(stderr, "multiple Node.js projects (api, web)") {
		t.Errorf("stderr = %q, want multiple-project explanation", stderr)
	}
	if strings.Contains(stderr, "found no Node.js project layout") {
		t.Errorf("stderr = %q, contains false no-layout explanation", stderr)
	}
}

func TestInitCommandWithConfig_MissingPostWorktreeCmdDeclined(t *testing.T) {
	tmpDir := setupGitRepo(t)
	defer os.RemoveAll(tmpDir)
	setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	// No package.json: nothing to auto-detect, so the missing-command warning
	// must block. Declining aborts before any workspace state is written.
	err = InitCommandWithConfig(InitParams{
		Description:      "Goal without post-worktree-cmd",
		SpecRef:          "specs/vision.md",
		Stdin:            strings.NewReader("n\n"),
		ForceInteractive: true,
	})
	if err == nil {
		t.Fatal("InitCommandWithConfig() error = nil, want cancellation error")
	}
	if !strings.Contains(err.Error(), "cancelled by user") {
		t.Errorf("InitCommandWithConfig() error = %v, want cancellation error", err)
	}
	if _, statErr := os.Stat(filepath.Join(tmpDir, paths.ProjectDirName())); !os.IsNotExist(statErr) {
		t.Errorf("project runtime directory exists after declined init, want no workspace created")
	}
}

func TestInitCommandWithConfig_MissingPostWorktreeCmdConfirmed(t *testing.T) {
	tmpDir := setupGitRepo(t)
	defer os.RemoveAll(tmpDir)
	setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	err = InitCommandWithConfig(InitParams{
		Description:      "Goal without post-worktree-cmd",
		SpecRef:          "specs/vision.md",
		Stdin:            strings.NewReader("y\n"),
		ForceInteractive: true,
	})
	if err != nil {
		t.Fatalf("InitCommandWithConfig() error = %v", err)
	}

	bb := db.New(filepath.Join(tmpDir, paths.ProjectDirName(), "state.yaml"))
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if state.Config.PostWorktreeCmd != nil {
		t.Errorf("state.Config.PostWorktreeCmd = %q, want nil", *state.Config.PostWorktreeCmd)
	}
}

func TestInitCommandWithConfig_MissingPostWorktreeCmdUnansweredContinues(t *testing.T) {
	tmpDir := setupGitRepo(t)
	defer os.RemoveAll(tmpDir)
	setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	// Scripted callers reach the prompt because isInteractive also accepts
	// /dev/null. An unanswerable prompt must not cancel initialization.
	err = InitCommandWithConfig(InitParams{
		Description:      "Goal without post-worktree-cmd",
		SpecRef:          "specs/vision.md",
		Stdin:            strings.NewReader(""),
		ForceInteractive: true,
	})
	if err != nil {
		t.Fatalf("InitCommandWithConfig() error = %v", err)
	}

	bb := db.New(filepath.Join(tmpDir, paths.ProjectDirName(), "state.yaml"))
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if state.Config.PostWorktreeCmd != nil {
		t.Errorf("state.Config.PostWorktreeCmd = %q, want nil", *state.Config.PostWorktreeCmd)
	}
}

func TestInitCommandWithConfig_ExplicitPostWorktreeCmdSkipsWarning(t *testing.T) {
	tmpDir := setupGitRepo(t)
	defer os.RemoveAll(tmpDir)
	setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	// Empty stdin: succeeding proves no confirmation was requested.
	err = InitCommandWithConfig(InitParams{
		Description:      "Goal with explicit post-worktree-cmd",
		SpecRef:          "specs/vision.md",
		PostWorktreeCmd:  "make setup",
		Stdin:            strings.NewReader(""),
		ForceInteractive: true,
	})
	if err != nil {
		t.Fatalf("InitCommandWithConfig() error = %v", err)
	}

	bb := db.New(filepath.Join(tmpDir, paths.ProjectDirName(), "state.yaml"))
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if state.Config.PostWorktreeCmd == nil {
		t.Fatal("state.Config.PostWorktreeCmd is nil, want non-nil")
	}
	if *state.Config.PostWorktreeCmd != "make setup" {
		t.Errorf("state.Config.PostWorktreeCmd = %q, want %q", *state.Config.PostWorktreeCmd, "make setup")
	}
}

func TestInitCommandWithConfig_AutoConfirmSkipsMissingPostWorktreePrompt(t *testing.T) {
	tmpDir := setupGitRepo(t)
	defer os.RemoveAll(tmpDir)
	setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	// --yes auto-answers the warning without consuming stdin.
	err = InitCommandWithConfig(InitParams{
		Description: "Goal without post-worktree-cmd",
		SpecRef:     "specs/vision.md",
		Stdin:       strings.NewReader(""),
		AutoConfirm: true,
	})
	if err != nil {
		t.Fatalf("InitCommandWithConfig() error = %v", err)
	}

	bb := db.New(filepath.Join(tmpDir, paths.ProjectDirName(), "state.yaml"))
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if state.Config.PostWorktreeCmd != nil {
		t.Errorf("state.Config.PostWorktreeCmd = %q, want nil", *state.Config.PostWorktreeCmd)
	}
}

func TestInitCommandWithConfig_AutoSuggestsPostWorktreeCmd(t *testing.T) {
	tmpDir := setupGitRepo(t)
	defer os.RemoveAll(tmpDir)
	setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	// Create package.json + yarn.lock to trigger suggestion
	os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(`{"name":"test"}`), 0644)
	os.WriteFile(filepath.Join(tmpDir, "yarn.lock"), []byte(""), 0644)

	// Accept the suggestion (y) — ForceInteractive bypasses TTY check for testing
	err = InitCommandWithConfig(InitParams{
		Description:      "Goal with auto-detected post-worktree-cmd",
		SpecRef:          "specs/vision.md",
		Stdin:            strings.NewReader("y\n"),
		ForceInteractive: true,
	})
	if err != nil {
		t.Fatalf("InitCommandWithConfig() error = %v", err)
	}

	// Verify post_worktree_cmd is set to "yarn install"
	bb := db.New(filepath.Join(tmpDir, paths.ProjectDirName(), "state.yaml"))
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if state.Config.PostWorktreeCmd == nil {
		t.Fatal("state.Config.PostWorktreeCmd is nil, want non-nil")
	}
	if *state.Config.PostWorktreeCmd != "yarn install" {
		t.Errorf("state.Config.PostWorktreeCmd = %q, want %q", *state.Config.PostWorktreeCmd, "yarn install")
	}
}

func TestInitCommandWithConfig_AutoSuggestDeclined(t *testing.T) {
	tmpDir := setupGitRepo(t)
	defer os.RemoveAll(tmpDir)
	setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	// Create package.json to trigger suggestion
	os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(`{"name":"test"}`), 0644)

	// Decline the suggestion — ForceInteractive bypasses TTY check for testing
	err = InitCommandWithConfig(InitParams{
		Description:      "Goal declining suggestion",
		SpecRef:          "specs/vision.md",
		Stdin:            strings.NewReader("n\n"),
		ForceInteractive: true,
	})
	if err != nil {
		t.Fatalf("InitCommandWithConfig() error = %v", err)
	}

	// Verify post_worktree_cmd is nil
	bb := db.New(filepath.Join(tmpDir, paths.ProjectDirName(), "state.yaml"))
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if state.Config.PostWorktreeCmd != nil {
		t.Errorf("state.Config.PostWorktreeCmd = %q, want nil", *state.Config.PostWorktreeCmd)
	}
}

func TestInitCommandWithConfig_NonInteractiveSkipsAutoDetect(t *testing.T) {
	tmpDir := setupGitRepo(t)
	defer os.RemoveAll(tmpDir)
	setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	// Create package.json + yarn.lock — would trigger prompt in interactive mode
	os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(`{"name":"test"}`), 0644)
	os.WriteFile(filepath.Join(tmpDir, "yarn.lock"), []byte(""), 0644)

	// Non-interactive (strings.Reader, no ForceInteractive) — should NOT prompt
	err = InitCommandWithConfig(InitParams{
		Description: "Goal in non-interactive mode",
		SpecRef:     "specs/vision.md",
		Stdin:       strings.NewReader(""),
	})
	if err != nil {
		t.Fatalf("InitCommandWithConfig() error = %v", err)
	}

	// Verify post_worktree_cmd is nil (no prompt was shown)
	bb := db.New(filepath.Join(tmpDir, paths.ProjectDirName(), "state.yaml"))
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if state.Config.PostWorktreeCmd != nil {
		t.Errorf("state.Config.PostWorktreeCmd = %q, want nil (non-interactive should skip)", *state.Config.PostWorktreeCmd)
	}
}

func TestInitCommandWithConfig_AutoConfirmAcceptsPostWorktreeSuggestion(t *testing.T) {
	tmpDir := setupGitRepo(t)
	defer os.RemoveAll(tmpDir)
	setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	if err := os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(`{"name":"test"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "yarn.lock"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	err = InitCommandWithConfig(InitParams{
		Description: "Goal with auto-confirmed post-worktree-cmd",
		SpecRef:     "specs/vision.md",
		Stdin:       strings.NewReader(""),
		AutoConfirm: true,
	})
	if err != nil {
		t.Fatalf("InitCommandWithConfig() error = %v", err)
	}

	bb := db.New(filepath.Join(tmpDir, paths.ProjectDirName(), "state.yaml"))
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if state.Config.PostWorktreeCmd == nil {
		t.Fatal("state.Config.PostWorktreeCmd is nil, want non-nil")
	}
	if *state.Config.PostWorktreeCmd != "yarn install" {
		t.Errorf("state.Config.PostWorktreeCmd = %q, want %q", *state.Config.PostWorktreeCmd, "yarn install")
	}
}

func TestInitCommandWithConfig_ExplicitFlagSkipsAutoDetect(t *testing.T) {
	tmpDir := setupGitRepo(t)
	defer os.RemoveAll(tmpDir)
	setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	// Create package.json + yarn.lock
	os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(`{"name":"test"}`), 0644)
	os.WriteFile(filepath.Join(tmpDir, "yarn.lock"), []byte(""), 0644)

	// Explicit flag should take precedence — no prompt expected
	err = InitCommandWithConfig(InitParams{
		Description:     "Goal with explicit cmd",
		SpecRef:         "specs/vision.md",
		PostWorktreeCmd: "make setup",
	})
	if err != nil {
		t.Fatalf("InitCommandWithConfig() error = %v", err)
	}

	// Verify the explicit value was used, not the auto-detected one
	bb := db.New(filepath.Join(tmpDir, paths.ProjectDirName(), "state.yaml"))
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if state.Config.PostWorktreeCmd == nil {
		t.Fatal("state.Config.PostWorktreeCmd is nil, want non-nil")
	}
	if *state.Config.PostWorktreeCmd != "make setup" {
		t.Errorf("state.Config.PostWorktreeCmd = %q, want %q", *state.Config.PostWorktreeCmd, "make setup")
	}
}

func TestInitCommandWithConfig_WarnsWhenNodeModulesMissing(t *testing.T) {
	tmpDir := setupGitRepo(t)
	defer os.RemoveAll(tmpDir)
	setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	// Create package.json + lockfile but NO node_modules
	os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(`{"name":"test"}`), 0644)
	os.WriteFile(filepath.Join(tmpDir, "package-lock.json"), []byte("{}"), 0644)

	// Capture stderr
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	err = InitCommandWithConfig(InitParams{
		Description:     "Goal without node_modules",
		SpecRef:         "specs/vision.md",
		PostWorktreeCmd: "npm install",
	})
	if err != nil {
		w.Close()
		os.Stderr = oldStderr
		t.Fatalf("InitCommandWithConfig() error = %v", err)
	}
	w.Close()
	stderrBytes, _ := io.ReadAll(r)
	os.Stderr = oldStderr

	stderr := string(stderrBytes)
	if !strings.Contains(stderr, "node_modules/ is missing") {
		t.Errorf("Expected missing node_modules warning in stderr, got: %s", stderr)
	}
}

func TestInitCommandWithConfig_NoWarningWhenNodeModulesPresent(t *testing.T) {
	tmpDir := setupGitRepo(t)
	defer os.RemoveAll(tmpDir)
	setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	// Create package.json + lockfile AND node_modules
	os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(`{"name":"test"}`), 0644)
	os.WriteFile(filepath.Join(tmpDir, "package-lock.json"), []byte("{}"), 0644)
	os.MkdirAll(filepath.Join(tmpDir, "node_modules"), 0755)

	// Capture stderr
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	err = InitCommandWithConfig(InitParams{
		Description:     "Goal with node_modules",
		SpecRef:         "specs/vision.md",
		PostWorktreeCmd: "npm install",
	})
	if err != nil {
		w.Close()
		os.Stderr = oldStderr
		t.Fatalf("InitCommandWithConfig() error = %v", err)
	}
	w.Close()
	stderrBytes, _ := io.ReadAll(r)
	os.Stderr = oldStderr

	stderr := string(stderrBytes)
	if strings.Contains(stderr, "node_modules/ is missing") {
		t.Errorf("Expected no node_modules warning when node_modules exists, got: %s", stderr)
	}
}

func TestInitCommandWithConfig_EntryPointWithoutConfig(t *testing.T) {
	tmpDir := setupGitRepo(t)
	defer os.RemoveAll(tmpDir)
	setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	// --entry-point without --config now succeeds because embedded pipeline
	// is auto-loaded and "detailed-spec" exists in the embedded config as
	// a legacy alias for functional-spec.
	err = InitCommandWithConfig(InitParams{
		Description: "Goal",
		SpecRef:     "specs/vision.md",
		EntryPoint:  "detailed-spec",
	})
	if err != nil {
		t.Fatalf("InitCommandWithConfig() error = %v", err)
	}

	// Verify entry_point is set
	bb := db.New(filepath.Join(tmpDir, paths.ProjectDirName(), "state.yaml"))
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if state.Goal.EntryPoint != "detailed-spec" {
		t.Errorf("state.Goal.EntryPoint = %q, want %q", state.Goal.EntryPoint, "detailed-spec")
	}
}

func TestInitCommandWithConfig_DefaultCLI(t *testing.T) {
	tmpDir := setupGitRepo(t)
	defer os.RemoveAll(tmpDir)
	setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	err = InitCommandWithConfig(InitParams{
		Description: "Goal with default CLI",
		SpecRef:     "specs/vision.md",
		DefaultCLI:  "codex",
	})
	if err != nil {
		t.Fatalf("InitCommandWithConfig() error = %v", err)
	}

	bb := db.New(filepath.Join(tmpDir, paths.ProjectDirName(), "state.yaml"))
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if state.Config.DefaultCLI != "codex" {
		t.Errorf("state.Config.DefaultCLI = %q, want %q", state.Config.DefaultCLI, "codex")
	}
}

func TestInitCommandWithConfig_RoleSpecificDefaultCLIs(t *testing.T) {
	tmpDir := setupGitRepo(t)
	defer os.RemoveAll(tmpDir)
	setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	err = InitCommandWithConfig(InitParams{
		Description:        "Goal with role-specific default CLIs",
		SpecRef:            "specs/vision.md",
		DefaultDoerCLI:     "codex",
		DefaultReviewerCLI: "gemini",
	})
	if err != nil {
		t.Fatalf("InitCommandWithConfig() error = %v", err)
	}

	bb := db.New(filepath.Join(tmpDir, paths.ProjectDirName(), "state.yaml"))
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if state.Config.DefaultDoerCLI != "codex" {
		t.Errorf("state.Config.DefaultDoerCLI = %q, want %q", state.Config.DefaultDoerCLI, "codex")
	}
	if state.Config.DefaultReviewerCLI != "gemini" {
		t.Errorf("state.Config.DefaultReviewerCLI = %q, want %q", state.Config.DefaultReviewerCLI, "gemini")
	}
}

func TestInitCommandWithConfig_DefaultCLIOmittedWhenEmpty(t *testing.T) {
	tmpDir := setupGitRepo(t)
	defer os.RemoveAll(tmpDir)
	setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	err = InitCommandWithConfig(InitParams{
		Description: "Goal without default CLI",
		SpecRef:     "specs/vision.md",
	})
	if err != nil {
		t.Fatalf("InitCommandWithConfig() error = %v", err)
	}

	bb := db.New(filepath.Join(tmpDir, paths.ProjectDirName(), "state.yaml"))
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if state.Config.DefaultCLI != "" {
		t.Errorf("state.Config.DefaultCLI = %q, want empty", state.Config.DefaultCLI)
	}
	if state.Config.DefaultDoerCLI != "" {
		t.Errorf("state.Config.DefaultDoerCLI = %q, want empty", state.Config.DefaultDoerCLI)
	}
	if state.Config.DefaultReviewerCLI != "" {
		t.Errorf("state.Config.DefaultReviewerCLI = %q, want empty", state.Config.DefaultReviewerCLI)
	}

	// Verify omitempty: default_cli should not appear in YAML
	data, err := os.ReadFile(filepath.Join(tmpDir, paths.ProjectDirName(), "state.yaml"))
	if err != nil {
		t.Fatalf("Failed to read state.yaml: %v", err)
	}
	if strings.Contains(string(data), "default_cli") {
		t.Error("state.yaml contains default_cli key, want omitted when empty")
	}
	if strings.Contains(string(data), "default_doer_cli") {
		t.Error("state.yaml contains default_doer_cli key, want omitted when empty")
	}
	if strings.Contains(string(data), "default_reviewer_cli") {
		t.Error("state.yaml contains default_reviewer_cli key, want omitted when empty")
	}
}

func TestInitCommand_WorkspaceInit(t *testing.T) {
	tmpDir := setupGitRepo(t)
	defer os.RemoveAll(tmpDir)
	setupGlobalLiza(t)

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	testhelpers.CreateCommittedSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	// Capture stdout to verify init output
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err = InitCommandWithConfig(InitParams{
		Description: "CLI-only workspace",
		SpecRef:     "specs/vision.md",
	})

	w.Close()
	stdoutBytes, _ := io.ReadAll(r)
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("InitCommandWithConfig() error = %v", err)
	}

	stdout := string(stdoutBytes)

	// Must contain expected CLI-only output
	if !strings.Contains(stdout, brand.NameTitle+" initialized at") {
		t.Errorf("Expected 'Liza initialized at' in stdout, got: %s", stdout)
	}
	if !strings.Contains(stdout, "Integration branch:") {
		t.Errorf("Expected 'Integration branch:' in stdout, got: %s", stdout)
	}

	// Must NOT contain stale MCP wording
	if strings.Contains(stdout, "MCP tools and personal permissions") {
		t.Error("stdout still contains stale MCP note after MCP removal")
	}
}

func setInitSembleHooksForTest(lookPath semble.ExecutableLookup, runner semble.CommandRunner) func() {
	previousLookPath := initSembleLookPath
	previousRunner := initSembleRunner
	initSembleLookPath = lookPath
	initSembleRunner = runner
	return func() {
		initSembleLookPath = previousLookPath
		initSembleRunner = previousRunner
	}
}

type initBashPolicyTestRunner struct {
	commands      []bashpolicycli.Command
	output        bashpolicycli.CommandOutput
	err           error
	readStdinLine bool
	stdinLine     string
	stdinLines    []string
}

func (r *initBashPolicyTestRunner) Run(command bashpolicycli.Command) (bashpolicycli.CommandOutput, error) {
	r.commands = append(r.commands, command)
	if r.readStdinLine && len(command.Args) > 0 && command.Args[0] == "init" && command.Stdin != nil {
		reader, ok := command.Stdin.(*bufio.Reader)
		if !ok {
			return r.output, r.err
		}
		line, _ := reader.ReadString('\n')
		r.stdinLines = append(r.stdinLines, line)
		if r.stdinLine == "" {
			r.stdinLine = line
		}
	}
	return r.output, r.err
}

func setInitBashPolicyHooksForTest(lookPath bashpolicycli.ExecutableLookup, runner bashpolicycli.CommandRunner) func() {
	previousLookPath := initBashPolicyLookPath
	previousRunner := initBashPolicyRunner
	initBashPolicyLookPath = lookPath
	initBashPolicyRunner = runner
	return func() {
		initBashPolicyLookPath = previousLookPath
		initBashPolicyRunner = previousRunner
	}
}

func assertBashPolicyCommand(t *testing.T, command bashpolicycli.Command, projectRoot string, args []string) {
	t.Helper()
	if command.Path != filepath.Join(projectRoot, "bin", "bash-policy") {
		t.Fatalf("bash-policy path = %q", command.Path)
	}
	if command.Dir != projectRoot {
		t.Fatalf("bash-policy dir = %q, want %q", command.Dir, projectRoot)
	}
	wantArgs := strings.Join(args, "\x00")
	if strings.Join(command.Args, "\x00") != wantArgs {
		t.Fatalf("bash-policy args = %v", command.Args)
	}
}

func assertBashPolicyCommands(t *testing.T, commands []bashpolicycli.Command, projectRoot string, providers []string) {
	t.Helper()
	if len(commands) != len(providers)*2 {
		t.Fatalf("bash-policy commands = %d, want %d", len(commands), len(providers)*2)
	}
	for i, provider := range providers {
		commandOffset := i * 2
		assertBashPolicyCommand(t, commands[commandOffset], projectRoot, []string{
			"init",
			"--provider", provider,
			"--policy-artifact-root", projectRoot,
		})
		assertBashPolicyCommand(t, commands[commandOffset+1], projectRoot, []string{
			"activation", "on",
			"--provider", provider,
			"--policy-artifact-root", projectRoot,
		})
	}
}

func captureStderrForTest(fn func() error) (string, error) {
	originalStderr := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		return "", err
	}
	os.Stderr = writer
	runErr := fn()
	closeErr := writer.Close()
	os.Stderr = originalStderr
	stderrBytes, readErr := io.ReadAll(reader)
	if readErr != nil {
		return string(stderrBytes), readErr
	}
	if runErr != nil {
		return string(stderrBytes), runErr
	}
	return string(stderrBytes), closeErr
}

func assertStateHasNoSembleForTest(t *testing.T, statePath string) {
	t.Helper()
	state, err := db.New(statePath).Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	configJSON, err := json.Marshal(state.Config)
	if err != nil {
		t.Fatalf("Failed to marshal state.Config: %v", err)
	}
	if strings.Contains(strings.ToLower(string(configJSON)), "semble") {
		t.Fatalf("state.Config contains Semble data: %s", string(configJSON))
	}
	content, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("Failed to read state.yaml: %v", err)
	}
	if strings.Contains(strings.ToLower(string(content)), "semble") {
		t.Fatalf("state.yaml contains Semble data:\n%s", string(content))
	}
}

func unsetEnvForTest(t *testing.T, key string) {
	t.Helper()
	previous, hadPrevious := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if hadPrevious {
			_ = os.Setenv(key, previous)
			return
		}
		_ = os.Unsetenv(key)
	})
}

func stringPtrForTest(value string) *string {
	return &value
}

func readFileForTest(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}

func writeFileForTest(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("create parent for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func runGitOutputForTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s failed: %v\n%s", strings.Join(args, " "), dir, err, output)
	}
	return strings.TrimSpace(string(output))
}

func verifyClaudeArtifacts(t *testing.T, projectRoot string) {
	t.Helper()

	settingsPath := filepath.Join(projectRoot, ".claude", "settings.json")
	if _, err := os.Stat(settingsPath); err != nil {
		t.Fatalf("Claude settings.json not created: %v", err)
	}

	hookPath := filepath.Join(projectRoot, ".claude", "hooks", "enforce-init.sh")
	if _, err := os.Stat(hookPath); err != nil {
		t.Fatalf("Claude enforce-init.sh not created: %v", err)
	}
	testhelpers.AssertExecutableScript(t, hookPath)
}

func verifyCodexHooks(t *testing.T, projectRoot string) {
	t.Helper()

	configPath := filepath.Join(projectRoot, ".codex", "config.toml")
	configContent, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Codex project config not created: %v", err)
	}
	if !strings.Contains(string(configContent), "hooks = true") {
		t.Errorf("Codex project config missing hooks feature:\n%s", string(configContent))
	}

	hooksPath := filepath.Join(projectRoot, ".codex", "hooks.json")
	hooksContent, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatalf("Codex hooks.json not created: %v", err)
	}
	var hooks map[string]any
	if err := json.Unmarshal(hooksContent, &hooks); err != nil {
		t.Fatalf("Codex hooks.json is invalid JSON: %v", err)
	}
	for _, want := range []string{".codex/hooks/enforce-init.sh", ".codex/hooks/git-guard.sh", ".codex/hooks/worktree-path-guard.sh"} {
		if !strings.Contains(string(hooksContent), want) {
			t.Errorf("Codex hooks.json missing %q:\n%s", want, string(hooksContent))
		}
	}

	for _, name := range []string{"enforce-init.sh", "git-guard.sh", "worktree-path-guard.sh"} {
		hookPath := filepath.Join(projectRoot, ".codex", "hooks", name)
		if _, err := os.Stat(hookPath); err != nil {
			t.Fatalf("Codex hook %s not created: %v", name, err)
		}
		testhelpers.AssertExecutableScript(t, hookPath)
	}
}
