package testhelpers

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/paths"
)

func TestSetupTestGitRepo(t *testing.T) {
	tmpDir := t.TempDir()

	globalHooksDir := filepath.Join(t.TempDir(), "global-hooks")
	if err := os.MkdirAll(globalHooksDir, 0755); err != nil {
		t.Fatalf("Failed to create global hooks dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(globalHooksDir, "post-commit"), []byte("#!/bin/sh\nexit 42\n"), 0755); err != nil {
		t.Fatalf("Failed to create global post-commit hook: %v", err)
	}
	globalConfig := filepath.Join(t.TempDir(), "gitconfig")
	// gitconfig treats backslashes as escape characters; on Windows the absolute
	// hooks path (C:\Users\...) would corrupt parsing. Use forward slashes, which
	// git accepts on every platform.
	hooksPathValue := filepath.ToSlash(globalHooksDir)
	if err := os.WriteFile(globalConfig, []byte("[core]\n\thooksPath = "+hooksPathValue+"\n"), 0644); err != nil {
		t.Fatalf("Failed to write global git config: %v", err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", globalConfig)

	// Run the setup
	SetupTestGitRepo(t, tmpDir)

	// Verify git repo was initialized
	gitDir := filepath.Join(tmpDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		t.Error(".git directory does not exist")
	}

	// Verify git config
	cmd := exec.Command("git", "-C", tmpDir, "config", "user.email")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("Failed to get git config: %v", err)
	}
	if string(output) != "test@example.com\n" {
		t.Errorf("Expected user.email=test@example.com, got %q", string(output))
	}

	cmd = exec.Command("git", "-C", tmpDir, "config", "user.name")
	output, err = cmd.Output()
	if err != nil {
		t.Fatalf("Failed to get git config: %v", err)
	}
	if string(output) != "Test User\n" {
		t.Errorf("Expected user.name=Test User, got %q", string(output))
	}

	cmd = exec.Command("git", "-C", tmpDir, "config", "--bool", "maintenance.auto")
	output, err = cmd.Output()
	if err != nil {
		t.Fatalf("Failed to get maintenance.auto: %v", err)
	}
	if string(output) != "false\n" {
		t.Errorf("Expected maintenance.auto=false, got %q", string(output))
	}

	cmd = exec.Command("git", "-C", tmpDir, "config", "core.hooksPath")
	output, err = cmd.Output()
	if err != nil {
		t.Fatalf("Failed to get core.hooksPath: %v", err)
	}
	expectedHooksPath, err := filepath.Abs(filepath.Join(tmpDir, ".git", "hooks"))
	if err != nil {
		t.Fatalf("Failed to resolve expected hooks path: %v", err)
	}
	if string(output) != expectedHooksPath+"\n" {
		t.Errorf("Expected core.hooksPath=%q, got %q", expectedHooksPath, string(output))
	}
	configContent, err := os.ReadFile(filepath.Join(gitDir, "config"))
	if err != nil {
		t.Fatalf("Failed to read copied git config: %v", err)
	}
	if count := strings.Count(string(configContent), "hooksPath"); count != 1 {
		t.Fatalf("copied git config hooksPath count = %d, want 1:\n%s", count, configContent)
	}
	cmd = exec.Command("git", "-C", tmpDir, "commit", "--allow-empty", "-m", "Verify local hooks path")
	if output, err = cmd.CombinedOutput(); err != nil {
		t.Fatalf("empty commit should use local hooks path, not failing global hook: %v\n%s", err, output)
	}

	// Verify README was created
	readmePath := filepath.Join(tmpDir, "README.md")
	if _, err := os.Stat(readmePath); os.IsNotExist(err) {
		t.Error("README.md does not exist")
	}

	content, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("Failed to read README.md: %v", err)
	}
	if string(content) != "# Test\n" {
		t.Errorf("Expected README.md content '# Test\\n', got %q", string(content))
	}

	// Verify initial commit exists
	cmd = exec.Command("git", "-C", tmpDir, "log", "--oneline")
	output, err = cmd.Output()
	if err != nil {
		t.Fatalf("Failed to get git log: %v", err)
	}
	if len(output) == 0 {
		t.Error("No commits found")
	}

	// Verify integration branch exists
	cmd = exec.Command("git", "-C", tmpDir, "branch", "--list", "integration")
	output, err = cmd.Output()
	if err != nil {
		t.Fatalf("Failed to list branches: %v", err)
	}
	if string(output) != "  integration\n" {
		t.Errorf("integration branch not found, got %q", string(output))
	}
}

func TestSetupTestGitRepoCopiesIsolatedFixture(t *testing.T) {
	first := t.TempDir()
	SetupTestGitRepo(t, first)

	extraPath := filepath.Join(first, "extra.txt")
	if err := os.WriteFile(extraPath, []byte("extra\n"), 0644); err != nil {
		t.Fatalf("write extra file: %v", err)
	}
	cmd := exec.Command("git", "-C", first, "add", "extra.txt")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add extra.txt failed: %v\n%s", err, output)
	}
	cmd = exec.Command("git", "-C", first, "commit", "-m", "Add extra file")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit extra.txt failed: %v\n%s", err, output)
	}

	second := t.TempDir()
	SetupTestGitRepo(t, second)
	if _, err := os.Stat(filepath.Join(second, "extra.txt")); !os.IsNotExist(err) {
		t.Fatalf("second fixture extra.txt stat err = %v, want missing isolated repo", err)
	}
}

func TestCreateTestGitRepoTemplateRemovesPreparationDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("TMPDIR", tmpDir)

	entries, err := createTestGitRepoTemplate(true)
	if err != nil {
		t.Fatalf("createTestGitRepoTemplate: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("createTestGitRepoTemplate returned no cached entries")
	}

	remaining, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("read TMPDIR: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("prepared fixture left %d entries in TMPDIR: %v", len(remaining), remaining)
	}
}

func TestSetupTestGitRepoPreservesExistingNonGitContents(t *testing.T) {
	tmpDir := t.TempDir()
	lizaDir := filepath.Join(tmpDir, paths.ProjectDirName())
	if err := os.MkdirAll(lizaDir, 0755); err != nil {
		t.Fatalf("create project runtime directory: %v", err)
	}
	statePath := filepath.Join(lizaDir, "state.yaml")
	if err := os.WriteFile(statePath, []byte("version: 1\n"), 0644); err != nil {
		t.Fatalf("write state file: %v", err)
	}

	SetupTestGitRepo(t, tmpDir)

	if _, err := os.Stat(filepath.Join(tmpDir, ".git")); err != nil {
		t.Fatalf(".git stat err = %v, want initialized repo", err)
	}
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("project runtime state stat err = %v, want preserved non-git contents", err)
	}
}

func TestSetupBasicTestGitRepoLeavesIntegrationBranchAbsent(t *testing.T) {
	tmpDir := t.TempDir()
	SetupBasicTestGitRepo(t, tmpDir)

	cmd := exec.Command("git", "-C", tmpDir, "branch", "--list", "integration")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("Failed to list branches: %v", err)
	}
	if string(output) != "" {
		t.Fatalf("integration branch exists in basic fixture: %q", string(output))
	}

	cmd = exec.Command("git", "-C", tmpDir, "log", "--oneline")
	output, err = cmd.Output()
	if err != nil {
		t.Fatalf("Failed to get git log: %v", err)
	}
	if len(output) == 0 {
		t.Fatal("No commits found")
	}
}

func TestSetupLizaDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Run the setup
	statePath, lockPath := SetupLizaDir(t, tmpDir)

	// Verify paths are correct
	expectedStatePath := filepath.Join(tmpDir, paths.ProjectDirName(), "state.yaml")
	if statePath != expectedStatePath {
		t.Errorf("Expected statePath=%q, got %q", expectedStatePath, statePath)
	}

	expectedLockPath := filepath.Join(tmpDir, paths.ProjectDirName(), "state.yaml.lock")
	if lockPath != expectedLockPath {
		t.Errorf("Expected lockPath=%q, got %q", expectedLockPath, lockPath)
	}

	// Verify project runtime directory exists
	lizaDir := filepath.Join(tmpDir, paths.ProjectDirName())
	info, err := os.Stat(lizaDir)
	if os.IsNotExist(err) {
		t.Fatal("project runtime directory does not exist")
	}
	if !info.IsDir() {
		t.Error("project runtime path is not a directory")
	}

	// Verify lock file exists
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		t.Error("Lock file does not exist")
	}

	// Verify lock file is empty
	content, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	if len(content) != 0 {
		t.Errorf("Expected empty lock file, got %d bytes", len(content))
	}
}

func TestCreateTestWorktree(t *testing.T) {
	tmpDir := t.TempDir()
	SetupTestGitRepo(t, tmpDir)

	// Create worktree for a task
	taskID := "task-123"
	CreateTestWorktree(t, tmpDir, taskID)

	// Verify worktree directory exists
	wtDir := filepath.Join(tmpDir, ".worktrees", taskID)
	info, err := os.Stat(wtDir)
	if os.IsNotExist(err) {
		t.Fatal("Worktree directory does not exist")
	}
	if !info.IsDir() {
		t.Error("Worktree path is not a directory")
	}

	// Verify .git link file exists (real worktree, not bare directory)
	gitFile := filepath.Join(wtDir, ".git")
	if _, err := os.Stat(gitFile); os.IsNotExist(err) {
		t.Error(".git link file does not exist in worktree")
	}
}

func TestCreateSpecFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a spec file
	filename := "vision.md"
	content := "# Vision\n\nThis is the vision.\n"
	specPath := CreateSpecFile(t, tmpDir, filename, content)

	// Verify returned path is correct
	expectedPath := filepath.Join(tmpDir, "specs", filename)
	if specPath != expectedPath {
		t.Errorf("Expected path=%q, got %q", expectedPath, specPath)
	}

	// Verify specs directory exists
	specsDir := filepath.Join(tmpDir, "specs")
	info, err := os.Stat(specsDir)
	if os.IsNotExist(err) {
		t.Fatal("specs directory does not exist")
	}
	if !info.IsDir() {
		t.Error("specs path is not a directory")
	}

	// Verify file exists and has correct content
	fileContent, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("Failed to read spec file: %v", err)
	}
	if string(fileContent) != content {
		t.Errorf("Expected content=%q, got %q", content, string(fileContent))
	}
}

func TestCreateSpecFile_MultipleFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create multiple spec files
	file1 := CreateSpecFile(t, tmpDir, "vision.md", "Vision content")
	file2 := CreateSpecFile(t, tmpDir, "architecture.md", "Architecture content")

	// Verify both files exist
	if _, err := os.Stat(file1); os.IsNotExist(err) {
		t.Error("First spec file does not exist")
	}
	if _, err := os.Stat(file2); os.IsNotExist(err) {
		t.Error("Second spec file does not exist")
	}

	// Verify contents
	content1, _ := os.ReadFile(file1)
	if string(content1) != "Vision content" {
		t.Errorf("First file has incorrect content: %q", string(content1))
	}

	content2, _ := os.ReadFile(file2)
	if string(content2) != "Architecture content" {
		t.Errorf("Second file has incorrect content: %q", string(content2))
	}
}
