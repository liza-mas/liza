package git

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/testhelpers"
)

// setupTestRepo creates a test git repository with an integration branch
func setupTestRepo(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()
	repoDir := filepath.Join(tmpDir, "test-repo")

	// Create directory
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Initialize git repo with basic setup (init, config, initial commit, integration branch)
	testhelpers.SetupTestGitRepo(t, repoDir)

	// Checkout integration branch and add another commit (specific to worktree tests)
	testFile := filepath.Join(repoDir, "README.md")
	testhelpers.MustGit(t, repoDir, "checkout", "integration")
	if err := os.WriteFile(testFile, []byte("# Test\nIntegration branch\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, repoDir, "add", "README.md")
	testhelpers.MustGit(t, repoDir, "commit", "-m", "Integration setup")

	return repoDir
}

func TestCreateWorktree(t *testing.T) {
	repoDir := setupTestRepo(t)
	git := New(repoDir)

	taskID := "task-123"
	worktreePath := filepath.Join(repoDir, ".worktrees", taskID)

	// Create worktree from integration branch
	baseCommit, err := git.CreateWorktree(taskID, "integration")
	if err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}

	// Verify worktree directory exists
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		t.Error("Worktree directory was not created")
	}

	// Verify task branch was created
	branches := testhelpers.MustGit(t, repoDir, "branch", "--list", "task/"+taskID)
	if !strings.Contains(branches, "task/"+taskID) {
		t.Errorf("task/%s branch was not created", taskID)
	}

	// Verify base_commit is a full SHA
	if len(baseCommit) != 40 {
		t.Errorf("baseCommit length = %d, want 40", len(baseCommit))
	}

	// Verify worktree is on correct branch
	wtBranch := testhelpers.MustGit(t, worktreePath, "branch", "--show-current")
	if wtBranch != "task/"+taskID {
		t.Errorf("Worktree branch = %q, want %q", wtBranch, "task/"+taskID)
	}
}

func TestCreateWorktreeAlreadyExists(t *testing.T) {
	repoDir := setupTestRepo(t)
	git := New(repoDir)

	taskID := "task-123"

	// Create worktree first time
	if _, err := git.CreateWorktree(taskID, "integration"); err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}

	// Try to create again without fresh flag - should return error or succeed
	_, err := git.CreateWorktree(taskID, "integration")
	if err == nil {
		t.Log("CreateWorktree() succeeded when worktree already exists (acceptable)")
	} else {
		// Error is also acceptable behavior
		if !strings.Contains(err.Error(), "already exists") {
			t.Errorf("CreateWorktree() error should mention 'already exists', got: %v", err)
		}
	}
}

func TestCreateWorktreeFresh(t *testing.T) {
	repoDir := setupTestRepo(t)
	git := New(repoDir)

	taskID := "task-123"

	// Create worktree first time
	if _, err := git.CreateWorktree(taskID, "integration"); err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}

	// Create with fresh flag - should delete and recreate
	_, err := git.CreateWorktreeFresh(taskID, "integration")
	if err != nil {
		t.Fatalf("CreateWorktreeFresh() error = %v", err)
	}

	// Verify worktree still exists
	worktreePath := filepath.Join(repoDir, ".worktrees", taskID)
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		t.Error("Worktree directory does not exist after fresh create")
	}
}

func TestRemoveWorktree(t *testing.T) {
	repoDir := setupTestRepo(t)
	git := New(repoDir)

	taskID := "task-123"
	worktreePath := filepath.Join(repoDir, ".worktrees", taskID)

	// Create worktree first
	if _, err := git.CreateWorktree(taskID, "integration"); err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}

	// Remove worktree
	if err := git.RemoveWorktree(taskID); err != nil {
		t.Fatalf("RemoveWorktree() error = %v", err)
	}

	// Verify worktree directory is gone
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Error("Worktree directory still exists after removal")
	}

	// Verify branch is deleted
	branches := testhelpers.MustGit(t, repoDir, "branch", "--list", "task/"+taskID)
	if strings.Contains(branches, "task/"+taskID) {
		t.Errorf("task/%s branch still exists after removal", taskID)
	}
}

func TestRemoveWorktreeNotExists(t *testing.T) {
	repoDir := setupTestRepo(t)
	git := New(repoDir)

	// Try to remove non-existent worktree - should not error
	err := git.RemoveWorktree("task-nonexistent")
	if err != nil {
		t.Errorf("RemoveWorktree() on non-existent worktree error = %v, want nil", err)
	}
}

func TestListWorktrees(t *testing.T) {
	repoDir := setupTestRepo(t)
	git := New(repoDir)

	// Initially should have just the main worktree
	worktrees, err := git.ListWorktrees()
	if err != nil {
		t.Fatalf("ListWorktrees() error = %v", err)
	}
	if len(worktrees) == 0 {
		t.Fatal("ListWorktrees() returned empty list")
	}

	// Create a task worktree
	taskID := "task-123"
	if _, err := git.CreateWorktree(taskID, "integration"); err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}

	// List again
	worktrees, err = git.ListWorktrees()
	if err != nil {
		t.Fatalf("ListWorktrees() error = %v", err)
	}

	// Should have at least 2 worktrees now
	if len(worktrees) < 2 {
		t.Errorf("ListWorktrees() returned %d worktrees, want at least 2", len(worktrees))
	}

	// Check that our task worktree is in the list
	found := false
	expectedPath := filepath.Join(repoDir, ".worktrees", taskID)

	// Resolve symlinks for comparison (macOS /var -> /private/var)
	expectedResolved, err := filepath.EvalSymlinks(expectedPath)
	if err != nil {
		t.Fatalf("EvalSymlinks() error = %v", err)
	}

	for _, wt := range worktrees {
		wtResolved, err := filepath.EvalSymlinks(wt.Path)
		if err != nil {
			// Skip worktrees we can't resolve
			continue
		}

		if wtResolved == expectedResolved {
			found = true
			if wt.Branch != "task/"+taskID {
				t.Errorf("Worktree branch = %q, want %q", wt.Branch, "task/"+taskID)
			}
			break
		}
	}

	if !found {
		t.Errorf("Task worktree %s (resolved: %s) not found in list", expectedPath, expectedResolved)
	}
}

func TestGetWorktreePath(t *testing.T) {
	repoDir := "/test/repo"
	git := New(repoDir)

	taskID := "task-123"
	expected := filepath.Join(repoDir, ".worktrees", taskID)

	got := git.GetWorktreePath(taskID)
	if got != expected {
		t.Errorf("GetWorktreePath() = %q, want %q", got, expected)
	}
}

func TestGetWorktreeRelPath(t *testing.T) {
	git := New("/test/repo")

	taskID := "task-123"
	expected := filepath.Join(".worktrees", taskID)

	got := git.GetWorktreeRelPath(taskID)
	if got != expected {
		t.Errorf("GetWorktreeRelPath() = %q, want %q", got, expected)
	}
}

func TestWorktreeProgressSignatureTracksDirtyTrackedContent(t *testing.T) {
	repoDir := setupTestRepo(t)
	g := New(repoDir)

	taskID := "task-dirty-content"
	if _, err := g.CreateWorktree(taskID, "integration"); err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}
	worktreePath := g.GetWorktreePath(taskID)
	target := filepath.Join(worktreePath, "README.md")
	if err := os.WriteFile(target, []byte("# Test\nfirst edit\n"), 0644); err != nil {
		t.Fatalf("write first edit: %v", err)
	}
	first, err := g.WorktreeProgressSignature(taskID)
	if err != nil {
		t.Fatalf("WorktreeProgressSignature first error = %v", err)
	}
	if err := os.WriteFile(target, []byte("# Test\nsecond edit\n"), 0644); err != nil {
		t.Fatalf("write second edit: %v", err)
	}
	second, err := g.WorktreeProgressSignature(taskID)
	if err != nil {
		t.Fatalf("WorktreeProgressSignature second error = %v", err)
	}
	if first == second {
		t.Fatalf("signature did not change after dirty tracked content changed:\n%s", first)
	}
}

func TestWorktreeProgressSignatureIgnoresDirtyTrackedMTimeOnlyChange(t *testing.T) {
	repoDir := setupTestRepo(t)
	g := New(repoDir)

	taskID := "task-tracked-mtime"
	if _, err := g.CreateWorktree(taskID, "integration"); err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}
	target := filepath.Join(g.GetWorktreePath(taskID), "README.md")
	if err := os.WriteFile(target, []byte("# Test\nsame dirty content\n"), 0644); err != nil {
		t.Fatalf("write dirty tracked file: %v", err)
	}
	first, err := g.WorktreeProgressSignature(taskID)
	if err != nil {
		t.Fatalf("WorktreeProgressSignature first error = %v", err)
	}
	future := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(target, future, future); err != nil {
		t.Fatalf("touch dirty tracked file: %v", err)
	}
	second, err := g.WorktreeProgressSignature(taskID)
	if err != nil {
		t.Fatalf("WorktreeProgressSignature second error = %v", err)
	}
	if first != second {
		t.Fatalf("signature changed after dirty tracked mtime-only change:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestWorktreeProgressSignatureTracksUntrackedContent(t *testing.T) {
	repoDir := setupTestRepo(t)
	g := New(repoDir)

	taskID := "task-untracked-content"
	if _, err := g.CreateWorktree(taskID, "integration"); err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}
	target := filepath.Join(g.GetWorktreePath(taskID), "notes.txt")
	if err := os.WriteFile(target, []byte("first\n"), 0644); err != nil {
		t.Fatalf("write first untracked edit: %v", err)
	}
	first, err := g.WorktreeProgressSignature(taskID)
	if err != nil {
		t.Fatalf("WorktreeProgressSignature first error = %v", err)
	}
	if err := os.WriteFile(target, []byte("second\n"), 0644); err != nil {
		t.Fatalf("write second untracked edit: %v", err)
	}
	second, err := g.WorktreeProgressSignature(taskID)
	if err != nil {
		t.Fatalf("WorktreeProgressSignature second error = %v", err)
	}
	if first == second {
		t.Fatalf("signature did not change after untracked content changed:\n%s", first)
	}
}

func TestWorktreeProgressSignatureIgnoresUntrackedMTimeOnlyChange(t *testing.T) {
	repoDir := setupTestRepo(t)
	g := New(repoDir)

	taskID := "task-untracked-mtime"
	if _, err := g.CreateWorktree(taskID, "integration"); err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}
	target := filepath.Join(g.GetWorktreePath(taskID), "notes.txt")
	if err := os.WriteFile(target, []byte("same untracked content\n"), 0644); err != nil {
		t.Fatalf("write untracked file: %v", err)
	}
	first, err := g.WorktreeProgressSignature(taskID)
	if err != nil {
		t.Fatalf("WorktreeProgressSignature first error = %v", err)
	}
	future := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(target, future, future); err != nil {
		t.Fatalf("touch untracked file: %v", err)
	}
	second, err := g.WorktreeProgressSignature(taskID)
	if err != nil {
		t.Fatalf("WorktreeProgressSignature second error = %v", err)
	}
	if first != second {
		t.Fatalf("signature changed after untracked mtime-only change:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestWorktreeProgressSignatureHandlesLargeUntrackedContent(t *testing.T) {
	repoDir := setupTestRepo(t)
	g := New(repoDir)

	taskID := "task-large-untracked-content"
	if _, err := g.CreateWorktree(taskID, "integration"); err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}
	target := filepath.Join(g.GetWorktreePath(taskID), "large.bin")
	if err := os.WriteFile(target, bytes.Repeat([]byte("a"), maxProgressSignatureFileBytes+1), 0644); err != nil {
		t.Fatalf("write large untracked file: %v", err)
	}
	first, err := g.WorktreeProgressSignature(taskID)
	if err != nil {
		t.Fatalf("WorktreeProgressSignature first error = %v", err)
	}
	f, err := os.OpenFile(target, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open large untracked file for append: %v", err)
	}
	if _, err := f.Write([]byte("b")); err != nil {
		_ = f.Close()
		t.Fatalf("append large untracked file: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close large untracked file: %v", err)
	}
	second, err := g.WorktreeProgressSignature(taskID)
	if err != nil {
		t.Fatalf("WorktreeProgressSignature second error = %v", err)
	}
	if first == second {
		t.Fatalf("signature did not change after large untracked file changed:\n%s", first)
	}
}

func TestValidateWorktreeHealth(t *testing.T) {
	repoDir := setupTestRepo(t)
	g := New(repoDir)

	taskID := "task-health"

	// Create a healthy worktree
	if _, err := g.CreateWorktree(taskID, "integration"); err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}

	// Healthy worktree should pass validation
	if err := g.ValidateWorktreeHealth(taskID); err != nil {
		t.Errorf("ValidateWorktreeHealth() on healthy worktree: %v", err)
	}

	// Simulate orphaned worktree: remove .git file but keep directory
	worktreePath := g.GetWorktreePath(taskID)
	gitFile := filepath.Join(worktreePath, ".git")
	if err := os.Remove(gitFile); err != nil {
		t.Fatalf("failed to remove .git file: %v", err)
	}

	err := g.ValidateWorktreeHealth(taskID)
	if err == nil {
		t.Error("ValidateWorktreeHealth() should fail when .git file is missing")
	} else if !strings.Contains(err.Error(), ".git link file missing") {
		t.Errorf("ValidateWorktreeHealth() error = %v, want '.git link file missing'", err)
	}

	// Remove directory entirely
	if err := os.RemoveAll(worktreePath); err != nil {
		t.Fatalf("failed to remove worktree dir: %v", err)
	}

	err = g.ValidateWorktreeHealth(taskID)
	if err == nil {
		t.Error("ValidateWorktreeHealth() should fail when directory is missing")
	} else if !strings.Contains(err.Error(), "directory missing") {
		t.Errorf("ValidateWorktreeHealth() error = %v, want 'directory missing'", err)
	}
}

func TestValidateWorktreeHealthRejectsUnresolvableHEAD(t *testing.T) {
	repoDir := setupTestRepo(t)
	g := New(repoDir)

	taskID := "task-unborn-head"
	if _, err := g.CreateWorktree(taskID, "integration"); err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}

	worktreePath := g.GetWorktreePath(taskID)
	gitLinkPath := filepath.Join(worktreePath, ".git")
	gitLink, err := os.ReadFile(gitLinkPath)
	if err != nil {
		t.Fatalf("failed to read worktree .git link: %v", err)
	}
	gitDir, ok := strings.CutPrefix(strings.TrimSpace(string(gitLink)), "gitdir: ")
	if !ok {
		t.Fatalf("unexpected .git link contents: %q", string(gitLink))
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(worktreePath, gitDir)
	}

	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/task/missing-head\n"), 0644); err != nil {
		t.Fatalf("failed to corrupt worktree HEAD: %v", err)
	}

	err = g.ValidateWorktreeHealth(taskID)
	if err == nil {
		t.Fatal("ValidateWorktreeHealth() should reject a worktree whose HEAD cannot resolve")
	}
	if !strings.Contains(err.Error(), "HEAD") {
		t.Errorf("ValidateWorktreeHealth() error = %v, want HEAD resolution detail", err)
	}
}

func TestRemoveWorktreeFallbackCleansTargetedMetadata(t *testing.T) {
	repoDir := setupTestRepo(t)
	g := New(repoDir)

	taskA := "task-a"
	taskB := "task-b"

	// Create two worktrees
	if _, err := g.CreateWorktree(taskA, "integration"); err != nil {
		t.Fatalf("CreateWorktree(taskA) error = %v", err)
	}
	if _, err := g.CreateWorktree(taskB, "integration"); err != nil {
		t.Fatalf("CreateWorktree(taskB) error = %v", err)
	}

	// Verify both metadata dirs exist under .git/worktrees/
	metaA := filepath.Join(repoDir, ".git", "worktrees", taskA)
	metaB := filepath.Join(repoDir, ".git", "worktrees", taskB)
	if _, err := os.Stat(metaA); os.IsNotExist(err) {
		t.Fatalf("metadata for %s should exist", taskA)
	}
	if _, err := os.Stat(metaB); os.IsNotExist(err) {
		t.Fatalf("metadata for %s should exist", taskB)
	}

	// Simulate the fallback path: manually remove the worktree directory
	// (making git worktree remove --force fail), then call RemoveWorktree.
	// The .git file inside the worktree must be removed first, otherwise
	// git worktree remove --force may succeed on the partial directory.
	worktreePathA := g.GetWorktreePath(taskA)
	if err := os.RemoveAll(filepath.Join(worktreePathA, ".git")); err != nil {
		t.Fatalf("failed to remove .git from worktree A: %v", err)
	}

	if err := g.RemoveWorktree(taskA); err != nil {
		t.Fatalf("RemoveWorktree(taskA) error = %v", err)
	}

	// taskA metadata should be cleaned up
	if _, err := os.Stat(metaA); !os.IsNotExist(err) {
		t.Error("metadata for taskA should be removed after RemoveWorktree")
	}

	// taskB metadata must still exist (global prune would have removed it too)
	if _, err := os.Stat(metaB); os.IsNotExist(err) {
		t.Error("metadata for taskB should NOT be affected by removing taskA")
	}

	// taskB worktree should still be healthy
	if err := g.ValidateWorktreeHealth(taskB); err != nil {
		t.Errorf("taskB worktree should still be healthy after removing taskA: %v", err)
	}
}
