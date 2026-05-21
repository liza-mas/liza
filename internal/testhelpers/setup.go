// Package testhelpers provides shared test utilities for the liza test suite.
//
// This package consolidates duplicated test setup code found across multiple test files,
// improving maintainability and consistency. It includes helpers for:
//   - Git repository initialization
//   - Liza directory structure creation
//   - Test worktree management
//   - Spec file creation
//
// Usage Example:
//
//	func TestSomething(t *testing.T) {
//	    tmpDir := t.TempDir()
//	    testhelpers.SetupTestGitRepo(t, tmpDir)
//	    statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
//	    // ... continue with test
//	}
package testhelpers

// setup.go contains test helpers for setting up test environments including
// git repositories, liza directory structures, worktrees, and spec files.
//
// Functions in this file handle the physical filesystem and git operations
// required to create realistic test environments that mirror production usage.

import (
	"os"
	"path/filepath"
	"testing"
)

// SetupTestGitRepo initializes a git repository with basic configuration.
// It performs the following:
//   - Initializes a git repo in tmpDir
//   - Sets user.email to "test@example.com"
//   - Sets user.name to "Test User"
//   - Creates a README.md file with "# Test\n"
//   - Creates an initial commit
//   - Creates an "integration" branch
//
// This helper eliminates ~25 lines of duplicated code that appears 8-10 times
// across test files (claim_task_test.go, wt_create_test.go, wt_delete_test.go, etc.)
func SetupTestGitRepo(t *testing.T, tmpDir string) {
	t.Helper()

	MustGit(t, tmpDir, "init", "-b", "main")
	MustGit(t, tmpDir, "config", "user.email", "test@example.com")
	MustGit(t, tmpDir, "config", "user.name", "Test User")

	readmePath := filepath.Join(tmpDir, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Test\n"), 0644); err != nil {
		t.Fatalf("Failed to create README: %v", err)
	}

	MustGit(t, tmpDir, "add", "README.md")
	MustGit(t, tmpDir, "commit", "-m", "Initial commit")
	MustGit(t, tmpDir, "branch", "integration")
}

// SetupLizaDir creates the .liza directory structure and returns paths to the state file and lock file.
// It performs the following:
//   - Creates .liza directory with 0755 permissions
//   - Creates state.yaml.lock file (empty)
//   - Returns (stateFile path, lockFile path)
//
// This helper eliminates ~8-10 lines of duplicated code that appears 15-18 times
// across test files.
func SetupLizaDir(t *testing.T, tmpDir string) (statePath, lockPath string) {
	t.Helper()

	lizaDir := filepath.Join(tmpDir, ".liza")
	if err := os.MkdirAll(lizaDir, 0755); err != nil {
		t.Fatalf("Failed to create .liza directory: %v", err)
	}

	statePath = filepath.Join(lizaDir, "state.yaml")
	lockPath = filepath.Join(lizaDir, "state.yaml.lock")

	// Create lock file
	if err := os.WriteFile(lockPath, []byte{}, 0644); err != nil {
		t.Fatalf("Failed to create lock file: %v", err)
	}

	// Pipeline config is mandatory — include it by default.
	SetupPipelineConfig(t, tmpDir)

	return statePath, lockPath
}

// SetupGlobalLiza creates a fake ~/.liza/CORE.md so that commands requiring
// 'liza setup' to have been run (like InitCommand) pass their prerequisite check.
// It overrides $HOME via t.Setenv (auto-reverted on test cleanup).
// Returns the fake home directory path.
func SetupGlobalLiza(t *testing.T) string {
	t.Helper()
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	globalLiza := filepath.Join(fakeHome, ".liza")
	if err := os.MkdirAll(globalLiza, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalLiza, "CORE.md"), []byte("# CORE\n"), 0644); err != nil {
		t.Fatal(err)
	}
	InstallFakeScipSearchTools(t, fakeHome)
	return fakeHome
}

// InstallFakeScipSearchTools adds fake scip-search and first-milestone indexers
// to PATH so init tests validate wiring without depending on host-installed tools.
func InstallFakeScipSearchTools(t *testing.T, root string) string {
	t.Helper()

	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\ncase \"$1\" in\n  --version) echo \"$0 0.0.0\" ;;\n  *) echo \"usage\" ;;\nesac\n"
	for _, name := range []string{"scip-search", "scip-go", "scip-typescript", "scip-python"} {
		path := filepath.Join(binDir, name)
		if err := os.WriteFile(path, []byte(script), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return binDir
}

// CreateTestWorktree creates a real git worktree for a task.
// It runs "git worktree add" to create a proper worktree with .git link file.
// Requires SetupTestGitRepo to have been called first (needs integration branch).
func CreateTestWorktree(t *testing.T, tmpDir, taskID string) {
	t.Helper()

	wtDir := filepath.Join(tmpDir, ".worktrees", taskID)
	branchName := "task/" + taskID
	MustGit(t, tmpDir, "worktree", "add", wtDir, "integration", "-b", branchName)
}

// CreateSpecFile creates a spec file in the specs/ directory.
// It creates the specs directory if needed and writes the content to the specified filename.
// Returns the full path to the created spec file.
//
// This helper eliminates ~5-6 lines of duplicated code that appears 3-4 times
// in add_task_test.go, init_test.go, and validate_test.go.
func CreateSpecFile(t *testing.T, tmpDir, filename, content string) string {
	t.Helper()

	specFile := filepath.Join(tmpDir, "specs", filename)
	if err := os.MkdirAll(filepath.Dir(specFile), 0755); err != nil {
		t.Fatalf("Failed to create spec directory: %v", err)
	}
	if err := os.WriteFile(specFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create spec file: %v", err)
	}

	return specFile
}

// CreateCommittedSpecFile creates a spec file and commits it to HEAD.
func CreateCommittedSpecFile(t *testing.T, tmpDir, filename, content string) string {
	t.Helper()

	specFile := CreateSpecFile(t, tmpDir, filename, content)
	relPath := filepath.ToSlash(filepath.Join("specs", filename))
	MustGit(t, tmpDir, "add", relPath)
	MustGit(t, tmpDir, "commit", "-m", "Add spec")
	return specFile
}

// CreateCommittedSpecFileOnIntegration creates a committed spec file and moves
// the test integration branch to that commit.
func CreateCommittedSpecFileOnIntegration(t *testing.T, tmpDir, filename, content string) string {
	t.Helper()

	specFile := CreateCommittedSpecFile(t, tmpDir, filename, content)
	MustGit(t, tmpDir, "branch", "-f", "integration", "HEAD")
	return specFile
}
