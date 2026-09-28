package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/gitenv"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func writeWIPTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// conflictingBranches leaves "side" and the checked-out branch with
// conflicting README.md commits.
func conflictingBranches(t *testing.T, repoDir string) {
	t.Helper()
	testhelpers.MustGit(t, repoDir, "checkout", "-b", "side")
	writeWIPTestFile(t, repoDir, "README.md", "side change\n")
	testhelpers.MustGit(t, repoDir, "commit", "-am", "side")
	testhelpers.MustGit(t, repoDir, "checkout", "-")
	writeWIPTestFile(t, repoDir, "README.md", "main change\n")
	testhelpers.MustGit(t, repoDir, "commit", "-am", "main")
}

func TestCommitAllWIP_CommitsTrackedAndUntrackedDespiteFailingHook(t *testing.T) {
	repoDir := setupTestRepo(t)
	hooksDir := filepath.Join(repoDir, "failing-hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeWIPTestFile(t, hooksDir, "pre-commit", "#!/bin/sh\nexit 1\n")
	if err := os.Chmod(filepath.Join(hooksDir, "pre-commit"), 0o755); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, repoDir, "config", "core.hooksPath", hooksDir)
	headBefore := testhelpers.MustGit(t, repoDir, "rev-parse", "HEAD")
	writeWIPTestFile(t, repoDir, "README.md", "interrupted edit\n")
	writeWIPTestFile(t, repoDir, "new.txt", "new work\n")

	sha, committed, err := New(repoDir).CommitAllWIP(repoDir, "WIP: test")

	if err != nil || !committed {
		t.Fatalf("CommitAllWIP() = %q, %v, %v; want a commit", sha, committed, err)
	}
	if head := testhelpers.MustGit(t, repoDir, "rev-parse", "HEAD"); head != sha || head == headBefore {
		t.Fatalf("HEAD = %s, reported %s, before %s; want a new reported commit", head, sha, headBefore)
	}
	files := testhelpers.MustGit(t, repoDir, "show", "--name-only", "--format=", "HEAD")
	for _, want := range []string{"README.md", "new.txt"} {
		if !strings.Contains(files, want) {
			t.Fatalf("WIP commit files = %q, want %s", files, want)
		}
	}
	if status := testhelpers.MustGit(t, repoDir, "status", "--short"); status != "" {
		t.Fatalf("worktree dirty after WIP commit:\n%s", status)
	}
}

func TestCommitAllWIP_CleanWorktreeMakesNoCommit(t *testing.T) {
	repoDir := setupTestRepo(t)
	headBefore := testhelpers.MustGit(t, repoDir, "rev-parse", "HEAD")

	sha, committed, err := New(repoDir).CommitAllWIP(repoDir, "WIP: test")

	if err != nil || committed || sha != "" {
		t.Fatalf("CommitAllWIP() = %q, %v, %v; want no commit", sha, committed, err)
	}
	if head := testhelpers.MustGit(t, repoDir, "rev-parse", "HEAD"); head != headBefore {
		t.Fatalf("HEAD = %s, want unchanged %s", head, headBefore)
	}
}

func TestInterruptedOperation(t *testing.T) {
	t.Run("at rest", func(t *testing.T) {
		repoDir := setupTestRepo(t)
		writeWIPTestFile(t, repoDir, "README.md", "ordinary edit\n")
		if got, err := New(repoDir).InterruptedOperation(repoDir); err != nil || got != "" {
			t.Fatalf("InterruptedOperation() = %q, %v; want at rest", got, err)
		}
	})
	t.Run("conflicted merge", func(t *testing.T) {
		repoDir := setupTestRepo(t)
		conflictingBranches(t, repoDir)
		if out, err := gitenv.CombinedOutput(repoDir, "merge", "side"); err == nil {
			t.Fatalf("merge succeeded, want a conflict:\n%s", out)
		}
		if got, err := New(repoDir).InterruptedOperation(repoDir); err != nil || got != "merge" {
			t.Fatalf("InterruptedOperation() = %q, %v; want merge", got, err)
		}
	})
	t.Run("stopped rebase", func(t *testing.T) {
		repoDir := setupTestRepo(t)
		conflictingBranches(t, repoDir)
		if out, err := gitenv.CombinedOutput(repoDir, "rebase", "side"); err == nil {
			t.Fatalf("rebase succeeded, want a conflict:\n%s", out)
		}
		if got, err := New(repoDir).InterruptedOperation(repoDir); err != nil || got != "rebase" {
			t.Fatalf("InterruptedOperation() = %q, %v; want rebase", got, err)
		}
	})
	t.Run("unmerged entries without operation state", func(t *testing.T) {
		repoDir := setupTestRepo(t)
		conflictingBranches(t, repoDir)
		if out, err := gitenv.CombinedOutput(repoDir, "merge", "side"); err == nil {
			t.Fatalf("merge succeeded, want a conflict:\n%s", out)
		}
		if err := os.Remove(filepath.Join(repoDir, ".git", "MERGE_HEAD")); err != nil {
			t.Fatal(err)
		}
		if got, err := New(repoDir).InterruptedOperation(repoDir); err != nil || got != "conflict resolution" {
			t.Fatalf("InterruptedOperation() = %q, %v; want conflict resolution", got, err)
		}
	})
}
