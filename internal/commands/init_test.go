package commands

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// setupGlobalLiza delegates to testhelpers.SetupGlobalLiza.
func setupGlobalLiza(t *testing.T) string {
	return testhelpers.SetupGlobalLiza(t)
}

func TestInitCommand(t *testing.T) {
	tests := []struct {
		name        string
		description string
		specRef     string
		setup       func(t *testing.T, tmpDir string)
		skipGlobal  bool // if true, don't set up global liza
		wantErr     bool
		errContains string
	}{
		{
			name:        "successful initialization",
			description: "Test goal",
			specRef:     "specs/vision.md",
			setup: func(t *testing.T, tmpDir string) {
				testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")
			},
			wantErr: false,
		},
		{
			name:        ".liza already exists",
			description: "Test goal",
			specRef:     "specs/vision.md",
			setup: func(t *testing.T, tmpDir string) {
				testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")
				// Create .liza directory
				lizaDir := paths.New(tmpDir).LizaDir()
				if err := os.Mkdir(lizaDir, 0755); err != nil {
					t.Fatal(err)
				}
			},
			wantErr:     true,
			errContains: "already exists",
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
				testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")
			},
			wantErr:     true,
			errContains: "Run 'liza setup' first",
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
			err = InitCommand(tt.description, tt.specRef, nil)

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

	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")

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

	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")

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

	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")

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
	state, err := db.For(filepath.Join(".liza", "state.yaml")).Read()
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

	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")

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
				// Clean up .liza so next subtest can run
				os.RemoveAll(filepath.Join(tmpDir, ".liza"))
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
	wantSpecRef := filepath.Join(tmpDir, specRef)
	if state.Goal.SpecRef != wantSpecRef {
		t.Errorf("state.Goal.SpecRef = %q, want %q", state.Goal.SpecRef, wantSpecRef)
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
	if state.Config.CoderMaxWait != models.DefaultCoderMaxWait {
		t.Errorf("state.Config.CoderMaxWait = %d, want %d", state.Config.CoderMaxWait, models.DefaultCoderMaxWait)
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
	testhelpers.CreateSpecFile(t, gitDir, "vision.md", "# Vision\n")

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
	globalDir := filepath.Join(fakeHome, ".liza")
	expectedTarget := filepath.Join(globalDir, "CORE.md")
	for _, name := range []string{"CLAUDE.md", "AGENTS.md", "GEMINI.md"} {
		linkPath := filepath.Join(gitDir, name)
		target, err := os.Readlink(linkPath)
		if err != nil {
			t.Errorf("Symlink %s not created: %v", name, err)
			continue
		}
		if target != expectedTarget {
			t.Errorf("Symlink %s target = %q, want %q", name, target, expectedTarget)
		}
	}
}

func TestInitCommand_SkipsCorrectSymlinks(t *testing.T) {
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

	testhelpers.CreateSpecFile(t, gitDir, "vision.md", "# Vision\n")

	// Pre-create CLAUDE.md as the correct symlink (absolute to global)
	globalDir := filepath.Join(fakeHome, ".liza")
	correctTarget := filepath.Join(globalDir, "CORE.md")
	claudePath := filepath.Join(gitDir, "CLAUDE.md")
	if err := os.Symlink(correctTarget, claudePath); err != nil {
		t.Fatal(err)
	}

	// Run init
	if err := InitCommand("Test goal", "specs/vision.md", nil); err != nil {
		t.Fatalf("InitCommand failed: %v", err)
	}

	// CLAUDE.md should still point to the same target (untouched)
	target, err := os.Readlink(claudePath)
	if err != nil {
		t.Fatalf("CLAUDE.md is no longer a symlink: %v", err)
	}
	if target != correctTarget {
		t.Errorf("CLAUDE.md target changed; got %q, want %q", target, correctTarget)
	}
}

func TestInitCommand_BrownfieldFallsBackToGlobal(t *testing.T) {
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

	testhelpers.CreateSpecFile(t, gitDir, "vision.md", "# Vision\n")

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
	coreFile := filepath.Join(fakeHome, ".liza", "CORE.md")
	if target != coreFile {
		t.Errorf("Global fallback → %q, want %q", target, coreFile)
	}

	// AGENTS.md and GEMINI.md should still be created at repo root (no conflict)
	for _, name := range []string{"AGENTS.md", "GEMINI.md"} {
		linkPath := filepath.Join(gitDir, name)
		if _, err := os.Readlink(linkPath); err != nil {
			t.Errorf("Symlink %s not created: %v", name, err)
		}
	}
}

func TestInitCommand_BrownfieldExistingLizaAtGlobalSkipsCreation(t *testing.T) {
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)

	fakeHome := setupGlobalLiza(t)

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(gitDir)

	testhelpers.CreateSpecFile(t, gitDir, "vision.md", "# Vision\n")

	coreFile := filepath.Join(fakeHome, ".liza", "CORE.md")

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

	// AGENTS.md and GEMINI.md should be created at repo root (no global fallback for them)
	for _, name := range []string{"AGENTS.md", "GEMINI.md"} {
		linkPath := filepath.Join(gitDir, name)
		if _, err := os.Readlink(linkPath); err != nil {
			t.Errorf("Symlink %s not created: %v", name, err)
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

	testhelpers.CreateSpecFile(t, gitDir, "vision.md", "# Vision\n")

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

func TestInitCommand_BrownfieldDuplicateLizaWarns(t *testing.T) {
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)

	fakeHome := setupGlobalLiza(t)

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(gitDir)

	testhelpers.CreateSpecFile(t, gitDir, "vision.md", "# Vision\n")

	coreFile := filepath.Join(fakeHome, ".liza", "CORE.md")

	// Liza symlink at both repo root and global
	os.Symlink(coreFile, filepath.Join(gitDir, "CLAUDE.md"))
	globalClaude := filepath.Join(fakeHome, ".claude", "CLAUDE.md")
	os.MkdirAll(filepath.Dir(globalClaude), 0755)
	os.Symlink(coreFile, globalClaude)

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

	// Should warn about duplicate Liza symlinks
	stderr := string(stderrBytes)
	if !strings.Contains(stderr, "Liza symlinks at both") {
		t.Errorf("Expected 'duplicate Liza' warning in stderr, got: %s", stderr)
	}
}

func TestInitCommand_ContractActionLocalCreatesCLAUDELocalMd(t *testing.T) {
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)

	fakeHome := setupGlobalLiza(t)

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(gitDir)

	testhelpers.CreateSpecFile(t, gitDir, "vision.md", "# Vision\n")

	// Pre-create CLAUDE.md as a regular file (brownfield project)
	os.WriteFile(filepath.Join(gitDir, "CLAUDE.md"), []byte("project"), 0644)

	err := InitCommandWithConfig(InitParams{
		Description:    "Test goal",
		SpecRef:        "specs/vision.md",
		Agents:         []string{"claude"},
		ContractAction: "local",
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
	coreFile := filepath.Join(fakeHome, ".liza", "CORE.md")
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
	dir := t.TempDir()
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	// Create ~/.liza/CORE.md
	lizaDir := filepath.Join(fakeHome, ".liza")
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
	testhelpers.CreateSpecFile(t, gitDir, "vision.md", "# Vision\n")

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
	info, err := os.Stat(settingsPath)
	if os.IsNotExist(err) {
		t.Fatalf("settings.json not created")
	}

	// Verify file permissions
	if info.Mode().Perm() != 0644 {
		t.Errorf("settings.json has wrong permissions: got %o, want 0644", info.Mode().Perm())
	}

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

	// Verify defaultMode
	if _, ok := perms["defaultMode"]; !ok {
		t.Errorf("permissions.defaultMode field missing")
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
	hookInfo, hookErr := os.Stat(hookPath)
	if os.IsNotExist(hookErr) {
		t.Error(".claude/hooks/enforce-init.sh not created during workspace init")
	} else if hookErr == nil && hookInfo.Mode()&0111 == 0 {
		t.Errorf("enforce-init.sh should be executable, got %o", hookInfo.Mode())
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
        submitted: CODE_READY_FOR_REVIEW
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

	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")
	configPath := writePipelineConfig(t, tmpDir, validPipelineYAML)

	err = InitCommandWithConfig(InitParams{
		Description: "Pipeline goal",
		SpecRef:     "specs/vision.md",
		ConfigPath:  configPath,
	})
	if err != nil {
		t.Fatalf("InitCommandWithConfig() error = %v", err)
	}

	// Verify .liza/pipeline.yaml exists and is identical to input
	frozenPath := filepath.Join(tmpDir, ".liza", "pipeline.yaml")
	frozen, err := os.ReadFile(frozenPath)
	if err != nil {
		t.Fatalf("Failed to read frozen pipeline.yaml: %v", err)
	}
	if string(frozen) != validPipelineYAML {
		t.Errorf("Frozen pipeline.yaml differs from input.\nGot:\n%s\nWant:\n%s", string(frozen), validPipelineYAML)
	}

	// Verify state.yaml has pipeline_version: 2
	bb := db.New(filepath.Join(tmpDir, ".liza", "state.yaml"))
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if state.PipelineVersion != 2 {
		t.Errorf("state.PipelineVersion = %d, want 2", state.PipelineVersion)
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

	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")
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
	bb := db.New(filepath.Join(tmpDir, ".liza", "state.yaml"))
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if state.Goal.EntryPoint != "detailed-spec" {
		t.Errorf("state.Goal.EntryPoint = %q, want %q", state.Goal.EntryPoint, "detailed-spec")
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

	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	// Init without --config auto-freezes embedded pipeline
	err = InitCommand("Legacy goal", "specs/vision.md", nil)
	if err != nil {
		t.Fatalf("InitCommand() error = %v", err)
	}

	// Verify pipeline.yaml is auto-frozen from embedded config
	frozenPath := filepath.Join(tmpDir, ".liza", "pipeline.yaml")
	if _, err := os.Stat(frozenPath); os.IsNotExist(err) {
		t.Errorf("pipeline.yaml should be auto-frozen from embedded config")
	}

	// Verify pipeline_version is set
	bb := db.New(filepath.Join(tmpDir, ".liza", "state.yaml"))
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if state.PipelineVersion != 2 {
		t.Errorf("state.PipelineVersion = %d, want 2", state.PipelineVersion)
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

	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")

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

	// Verify .liza was not created (early validation)
	lizaDir := filepath.Join(tmpDir, ".liza")
	if _, statErr := os.Stat(lizaDir); !os.IsNotExist(statErr) {
		t.Errorf(".liza directory should not exist after config validation failure")
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

	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")
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

	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	err = InitCommandWithConfig(InitParams{
		Description:     "Goal with post-worktree-cmd",
		SpecRef:         "specs/vision.md",
		PostWorktreeCmd: "make sync-embedded",
	})
	if err != nil {
		t.Fatalf("InitCommandWithConfig() error = %v", err)
	}

	// Verify post_worktree_cmd is set in state
	bb := db.New(filepath.Join(tmpDir, ".liza", "state.yaml"))
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

	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	err = InitCommandWithConfig(InitParams{
		Description: "Goal without post-worktree-cmd",
		SpecRef:     "specs/vision.md",
	})
	if err != nil {
		t.Fatalf("InitCommandWithConfig() error = %v", err)
	}

	// Verify post_worktree_cmd is nil in state
	bb := db.New(filepath.Join(tmpDir, ".liza", "state.yaml"))
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

	// CLAUDE.md should be a symlink to ~/.liza/CORE.md
	target, err := os.Readlink(filepath.Join(gitDir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("CLAUDE.md not a symlink: %v", err)
	}
	expected := filepath.Join(fakeHome, ".liza", "CORE.md")
	if target != expected {
		t.Errorf("CLAUDE.md → %q, want %q", target, expected)
	}

	// .liza/ directory should NOT exist
	if _, err := os.Stat(filepath.Join(gitDir, ".liza")); !os.IsNotExist(err) {
		t.Error(".liza/ directory should not be created in pairing mode")
	}

	// .claude/settings.json should be written
	settingsPath := filepath.Join(gitDir, ".claude", "settings.json")
	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		t.Error(".claude/settings.json should be created for --claude pairing")
	}

	// .claude/hooks/enforce-init.sh should be deployed
	hookPath := filepath.Join(gitDir, ".claude", "hooks", "enforce-init.sh")
	hookInfo, err := os.Stat(hookPath)
	if os.IsNotExist(err) {
		t.Error(".claude/hooks/enforce-init.sh should be created for --claude pairing")
	} else if err == nil && hookInfo.Mode()&0111 == 0 {
		t.Errorf("enforce-init.sh should be executable, got %o", hookInfo.Mode())
	}

	// AGENTS.md and GEMINI.md should NOT exist (only --claude)
	for _, name := range []string{"AGENTS.md", "GEMINI.md"} {
		if _, err := os.Stat(filepath.Join(gitDir, name)); !os.IsNotExist(err) {
			t.Errorf("%s should not exist when only --claude is specified", name)
		}
	}
}

func TestInitPairingCommand_MultipleAgents(t *testing.T) {
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

	expected := filepath.Join(fakeHome, ".liza", "CORE.md")
	for _, tc := range []struct {
		agent string
		file  string
	}{
		{"claude", "CLAUDE.md"},
		{"codex", "AGENTS.md"},
		{"gemini", "GEMINI.md"},
	} {
		target, err := os.Readlink(filepath.Join(gitDir, tc.file))
		if err != nil {
			t.Errorf("%s (%s): not a symlink: %v", tc.file, tc.agent, err)
			continue
		}
		if target != expected {
			t.Errorf("%s → %q, want %q", tc.file, target, expected)
		}
	}
}

func TestInitPairingCommand_Idempotent(t *testing.T) {
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

	target, err := os.Readlink(filepath.Join(gitDir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("CLAUDE.md not a symlink: %v", err)
	}
	expected := filepath.Join(fakeHome, ".liza", "CORE.md")
	if target != expected {
		t.Errorf("CLAUDE.md → %q, want %q", target, expected)
	}
}

func TestInitPairingCommand_Mistral(t *testing.T) {
	fakeHome := setupGlobalLiza(t)

	err := InitPairingCommand(InitPairingParams{
		Agents: []string{"mistral"},
	})
	if err != nil {
		t.Fatalf("InitPairingCommand failed: %v", err)
	}

	// ~/.vibe/prompts/liza.md should be a symlink to ~/.liza/CORE.md
	linkPath := filepath.Join(fakeHome, ".vibe", "prompts", "liza.md")
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("liza.md not a symlink: %v", err)
	}
	expected := filepath.Join(fakeHome, ".liza", "CORE.md")
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

func TestInitPairingCommand_MistralDeclinesOverwrite(t *testing.T) {
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
	gitDir := setupGitRepo(t)
	defer os.RemoveAll(gitDir)
	fakeHome := setupGlobalLiza(t)

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(gitDir)

	coreFile := filepath.Join(fakeHome, ".liza", "CORE.md")

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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			for _, f := range tt.files {
				if err := os.WriteFile(filepath.Join(tmpDir, f), []byte("{}"), 0644); err != nil {
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

	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")

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
	bb := db.New(filepath.Join(tmpDir, ".liza", "state.yaml"))
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

	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")

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
	bb := db.New(filepath.Join(tmpDir, ".liza", "state.yaml"))
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

	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")

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
	bb := db.New(filepath.Join(tmpDir, ".liza", "state.yaml"))
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if state.Config.PostWorktreeCmd != nil {
		t.Errorf("state.Config.PostWorktreeCmd = %q, want nil (non-interactive should skip)", *state.Config.PostWorktreeCmd)
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

	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")

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
	bb := db.New(filepath.Join(tmpDir, ".liza", "state.yaml"))
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

	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")

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

	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")

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

	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	// --entry-point without --config now succeeds because embedded pipeline
	// is auto-loaded and "detailed-spec" exists in the embedded config
	err = InitCommandWithConfig(InitParams{
		Description: "Goal",
		SpecRef:     "specs/vision.md",
		EntryPoint:  "detailed-spec",
	})
	if err != nil {
		t.Fatalf("InitCommandWithConfig() error = %v", err)
	}

	// Verify entry_point is set
	bb := db.New(filepath.Join(tmpDir, ".liza", "state.yaml"))
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

	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	err = InitCommandWithConfig(InitParams{
		Description: "Goal with default CLI",
		SpecRef:     "specs/vision.md",
		DefaultCLI:  "codex",
	})
	if err != nil {
		t.Fatalf("InitCommandWithConfig() error = %v", err)
	}

	bb := db.New(filepath.Join(tmpDir, ".liza", "state.yaml"))
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if state.Config.DefaultCLI != "codex" {
		t.Errorf("state.Config.DefaultCLI = %q, want %q", state.Config.DefaultCLI, "codex")
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

	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	err = InitCommandWithConfig(InitParams{
		Description: "Goal without default CLI",
		SpecRef:     "specs/vision.md",
	})
	if err != nil {
		t.Fatalf("InitCommandWithConfig() error = %v", err)
	}

	bb := db.New(filepath.Join(tmpDir, ".liza", "state.yaml"))
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if state.Config.DefaultCLI != "" {
		t.Errorf("state.Config.DefaultCLI = %q, want empty", state.Config.DefaultCLI)
	}

	// Verify omitempty: default_cli should not appear in YAML
	data, err := os.ReadFile(filepath.Join(tmpDir, ".liza", "state.yaml"))
	if err != nil {
		t.Fatalf("Failed to read state.yaml: %v", err)
	}
	if strings.Contains(string(data), "default_cli") {
		t.Error("state.yaml contains default_cli key, want omitted when empty")
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

	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")

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
	if !strings.Contains(stdout, "Liza initialized at") {
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
