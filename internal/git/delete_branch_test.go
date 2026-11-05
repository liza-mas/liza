package git

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// A plain branch is deleted without replacing the shared config, which a
// concurrent Windows reader can fail to open meanwhile.
func TestDeleteBranchLeavesConfigInPlace(t *testing.T) {
	t.Parallel()
	repoDir := setupTestRepo(t)
	g := New(repoDir)
	testhelpers.MustGit(t, repoDir, "branch", "task/plain")
	config := filepath.Join(repoDir, ".git", "config")
	before, err := os.Stat(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.DeleteBranch("task/plain"); err != nil {
		t.Fatalf("DeleteBranch() error = %v", err)
	}
	if exists, err := g.BranchExists("task/plain"); err != nil || exists {
		t.Fatalf("BranchExists after delete = %v, %v; want false", exists, err)
	}
	after, err := os.Stat(config)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("deleting a branch without a config section replaced the shared config")
	}
}

// A branch with a config section still loses it with the branch.
func TestDeleteBranchRemovesItsConfigSection(t *testing.T) {
	t.Parallel()
	repoDir := setupTestRepo(t)
	g := New(repoDir)
	testhelpers.MustGit(t, repoDir, "branch", "task/tracked")
	testhelpers.MustGit(t, repoDir, "config", "branch.task/tracked.merge", "refs/heads/main")
	if err := g.DeleteBranch("task/tracked"); err != nil {
		t.Fatalf("DeleteBranch() error = %v", err)
	}
	if exists, _ := g.BranchExists("task/tracked"); exists {
		t.Fatal("branch still exists")
	}
	if _, err := g.exec("config", "--get", "branch.task/tracked.merge"); !exitedWithStatus(err, 1) {
		t.Fatalf("branch config section kept: %v", err)
	}
}

// The refusals of `git branch -D` are kept: a missing branch and a branch
// checked out in a worktree are errors, and the checked-out branch survives.
func TestDeleteBranchKeepsRefusals(t *testing.T) {
	t.Parallel()
	repoDir := setupTestRepo(t)
	g := New(repoDir)
	if err := g.DeleteBranch("task/missing"); err == nil {
		t.Error("DeleteBranch(missing) = nil, want an error")
	}
	if _, err := g.CreateWorktree("held", "integration"); err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}
	held := paths.TaskBranchPrefix + "held"
	if err := g.DeleteBranch(held); err == nil {
		t.Error("DeleteBranch(checked out) = nil, want an error")
	}
	if exists, err := g.BranchExists(held); err != nil || !exists {
		t.Fatalf("checked-out branch after refused delete: exists %v, %v", exists, err)
	}
}
