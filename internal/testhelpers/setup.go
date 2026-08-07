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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/liza-mas/liza/internal/gitenv"
)

type preparedGitRepoTemplate struct {
	once    sync.Once
	entries []gitRepoTemplateEntry
	err     error
}

type gitRepoTemplateEntry struct {
	relPath    string
	mode       os.FileMode
	content    []byte
	linkTarget string
}

var (
	basicGitRepoTemplate preparedGitRepoTemplate
	testGitRepoTemplate  preparedGitRepoTemplate
)

// SetupTestGitRepo initializes a git repository with basic configuration.
// It performs the following:
//   - Copies a prepared git repo fixture into tmpDir
//   - Retains user.email "test@example.com" from the prepared template
//   - Retains user.name "Test User" from the prepared template
//   - Sets local core.hooksPath to tmpDir's .git/hooks
//   - Provides README.md, an initial commit, and an "integration" branch
//
// This helper eliminates ~25 lines of duplicated code that appears 8-10 times
// across test files (claim_task_test.go, wt_create_test.go, wt_delete_test.go, etc.)
func SetupTestGitRepo(t *testing.T, tmpDir string) {
	t.Helper()

	if err := prepareGitFixtureDir(tmpDir); err != nil {
		t.Fatalf("Failed to prepare test git repo directory: %v", err)
	}
	template := gitRepoTemplate(t, &testGitRepoTemplate, true)
	if err := materializeGitRepoTemplate(template, tmpDir); err != nil {
		t.Fatalf("Failed to copy test git repo template: %v", err)
	}
	configureTestGitRepo(t, tmpDir)
}

// SetupBasicTestGitRepo initializes a prepared git repository without creating
// an integration branch. Use this for tests that need to exercise branch
// creation behavior themselves.
func SetupBasicTestGitRepo(t *testing.T, tmpDir string) {
	t.Helper()

	if err := prepareGitFixtureDir(tmpDir); err != nil {
		t.Fatalf("Failed to prepare basic test git repo directory: %v", err)
	}
	template := gitRepoTemplate(t, &basicGitRepoTemplate, false)
	if err := materializeGitRepoTemplate(template, tmpDir); err != nil {
		t.Fatalf("Failed to copy basic test git repo template: %v", err)
	}
	configureTestGitRepo(t, tmpDir)
}

func gitRepoTemplate(t *testing.T, template *preparedGitRepoTemplate, includeIntegrationBranch bool) []gitRepoTemplateEntry {
	t.Helper()

	template.once.Do(func() {
		template.entries, template.err = createTestGitRepoTemplate(includeIntegrationBranch)
	})
	if template.err != nil {
		t.Fatalf("Failed to create test git repo template: %v", template.err)
	}
	return template.entries
}

func createTestGitRepoTemplate(includeIntegrationBranch bool) (entries []gitRepoTemplateEntry, err error) {
	dir, err := os.MkdirTemp("", "liza-test-git-template-*")
	if err != nil {
		return nil, err
	}
	defer func() {
		if removeErr := os.RemoveAll(dir); err == nil && removeErr != nil {
			err = fmt.Errorf("remove prepared test git repo: %w", removeErr)
		}
	}()
	if err := initializeTestGitRepo(dir, includeIntegrationBranch); err != nil {
		return nil, err
	}
	return snapshotGitRepoTemplate(dir)
}

func initializeTestGitRepo(dir string, includeIntegrationBranch bool) error {
	if err := runGitForSetup(dir, "init", "-b", "main"); err != nil {
		return err
	}
	// Keep the prepared fixture stable while it is snapshotted.
	if err := runGitForSetup(dir, "config", "maintenance.auto", "false"); err != nil {
		return err
	}
	if err := runGitForSetup(dir, "config", "user.email", "test@example.com"); err != nil {
		return err
	}
	if err := runGitForSetup(dir, "config", "user.name", "Test User"); err != nil {
		return err
	}
	// Fixtures are compared byte for byte. Git for Windows defaults to
	// core.autocrlf=true, which rewrites LF to CRLF on checkout — including
	// when a worktree is added — and makes those comparisons fail. Set once on
	// the template: every copy inherits .git/config as materialized.
	if err := runGitForSetup(dir, "config", "core.autocrlf", "false"); err != nil {
		return err
	}
	if err := configureTestGitRepoHooks(dir); err != nil {
		return err
	}

	readmePath := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Test\n"), 0644); err != nil {
		return fmt.Errorf("create README: %w", err)
	}

	if err := runGitForSetup(dir, "add", "README.md"); err != nil {
		return err
	}
	if err := runGitForSetup(dir, "commit", "-m", "Initial commit"); err != nil {
		return err
	}
	if !includeIntegrationBranch {
		return nil
	}
	return runGitForSetup(dir, "branch", "integration")
}

func configureTestGitRepo(t *testing.T, repoDir string) {
	t.Helper()

	if err := rewriteCopiedTestGitRepoHooks(repoDir); err != nil {
		t.Fatalf("Failed to configure test repo hooks path: %v", err)
	}
}

func rewriteCopiedTestGitRepoHooks(repoDir string) error {
	hooksDir, err := filepath.Abs(filepath.Join(repoDir, ".git", "hooks"))
	if err != nil {
		return fmt.Errorf("resolve test hooks path: %w", err)
	}
	configPath := filepath.Join(repoDir, ".git", "config")
	content, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read copied test git config: %w", err)
	}

	lines := strings.Split(string(content), "\n")
	section := ""
	replaced := false
	kept := lines[:0]
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "["), "]")))
		}
		if section == "core" {
			key, _, found := strings.Cut(trimmed, "=")
			if found && strings.EqualFold(strings.TrimSpace(key), "hooksPath") {
				if !replaced {
					kept = append(kept, "\thooksPath = "+hooksDir)
					replaced = true
				}
				continue
			}
		}
		kept = append(kept, line)
	}
	if !replaced {
		return fmt.Errorf("copied test git config has no core.hooksPath entry")
	}
	if err := os.WriteFile(configPath, []byte(strings.Join(kept, "\n")), 0o644); err != nil {
		return fmt.Errorf("write copied test git config: %w", err)
	}
	return nil
}

func configureTestGitRepoHooks(repoDir string) error {
	hooksDir, err := filepath.Abs(filepath.Join(repoDir, ".git", "hooks"))
	if err != nil {
		return fmt.Errorf("resolve test hooks path: %w", err)
	}
	return runGitForSetup(repoDir, "config", "core.hooksPath", hooksDir)
}

func runGitForSetup(dir string, args ...string) error {
	output, err := gitenv.CombinedOutput(dir, args...)
	if err != nil {
		return fmt.Errorf("git %v failed: %w\nOutput: %s", args, err, output)
	}
	return nil
}

func prepareGitFixtureDir(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return fmt.Errorf("%s already contains a git repo", dir)
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func snapshotGitRepoTemplate(src string) ([]gitRepoTemplateEntry, error) {
	var entries []gitRepoTemplateEntry
	err := filepath.WalkDir(src, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == src {
			return nil
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		mode := info.Mode()
		templateEntry := gitRepoTemplateEntry{relPath: rel, mode: mode}
		switch {
		case mode.IsDir():
			entries = append(entries, templateEntry)
			return nil
		case mode.Type()&os.ModeSymlink != 0:
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return err
			}
			templateEntry.linkTarget = linkTarget
			entries = append(entries, templateEntry)
			return nil
		case mode.IsRegular():
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			templateEntry.content = content
			entries = append(entries, templateEntry)
			return nil
		default:
			return fmt.Errorf("unsupported prepared git fixture entry %s (%s)", rel, mode)
		}
	})
	return entries, err
}

func materializeGitRepoTemplate(entries []gitRepoTemplateEntry, dst string) error {
	for _, entry := range entries {
		target := filepath.Join(dst, entry.relPath)
		switch {
		case entry.mode.IsDir():
			if err := os.MkdirAll(target, entry.mode.Perm()); err != nil {
				return err
			}
		case entry.mode.Type()&os.ModeSymlink != 0:
			if err := os.Symlink(entry.linkTarget, target); err != nil {
				return err
			}
		case entry.mode.IsRegular():
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(target, entry.content, entry.mode.Perm()); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported cached git fixture entry %s (%s)", entry.relPath, entry.mode)
		}
	}
	return nil
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

// CreateCommittedPreCommitConfig creates a minimal .pre-commit-config.yaml and
// commits it to HEAD.
func CreateCommittedPreCommitConfig(t *testing.T, tmpDir string) string {
	t.Helper()

	configPath := filepath.Join(tmpDir, ".pre-commit-config.yaml")
	content := []byte("repos: []\n")
	if err := os.WriteFile(configPath, content, 0644); err != nil {
		t.Fatalf("Failed to create pre-commit config: %v", err)
	}
	MustGit(t, tmpDir, "add", ".pre-commit-config.yaml")
	MustGit(t, tmpDir, "commit", "-m", "Add pre-commit config")
	return configPath
}

// CreateCommittedSpecFileOnIntegration creates a committed spec file and moves
// the test integration branch to that commit.
func CreateCommittedSpecFileOnIntegration(t *testing.T, tmpDir, filename, content string) string {
	t.Helper()

	specFile := CreateCommittedSpecFile(t, tmpDir, filename, content)
	CreateCommittedPreCommitConfig(t, tmpDir)
	MustGit(t, tmpDir, "branch", "-f", "integration", "HEAD")
	return specFile
}
