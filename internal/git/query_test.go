package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/referencecontract"
	"github.com/liza-mas/liza/internal/testhelpers"
)

var _ referencecontract.Repository = (*Git)(nil)

func TestCalculateDrift(t *testing.T) {
	repoDir := setupTestRepo(t)
	git := New(repoDir)

	// Get current integration HEAD as base
	baseCommit, err := git.GetCommitSHA("integration")
	if err != nil {
		t.Fatalf("GetCommitSHA() error = %v", err)
	}

	// No drift initially
	drift, err := git.CalculateDrift(baseCommit, "integration")
	if err != nil {
		t.Fatalf("CalculateDrift() error = %v", err)
	}
	if drift != 0 {
		t.Errorf("CalculateDrift() = %d, want 0", drift)
	}

	// Make a new commit on integration
	testFile := filepath.Join(repoDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, repoDir, "add", "test.txt")
	testhelpers.MustGit(t, repoDir, "commit", "-m", "New commit")

	// Now there should be 1 commit of drift
	drift, err = git.CalculateDrift(baseCommit, "integration")
	if err != nil {
		t.Fatalf("CalculateDrift() error = %v", err)
	}
	if drift != 1 {
		t.Errorf("CalculateDrift() = %d, want 1", drift)
	}
}

func TestTreePathMode(t *testing.T) {
	repoDir := setupTestRepo(t)
	git := New(repoDir)

	files := map[string]struct {
		content string
		mode    os.FileMode
	}{
		"regular.txt":        {content: "regular\n", mode: 0644},
		"script.sh":          {content: "#!/bin/sh\nexit 0\n", mode: 0755},
		"path with space.md": {content: "# spaces\n", mode: 0644},
		"docs/readme.md":     {content: "# docs\n", mode: 0644},
	}
	for path, file := range files {
		fullPath := filepath.Join(repoDir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(fullPath), err)
		}
		if err := os.WriteFile(fullPath, []byte(file.content), file.mode); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", fullPath, err)
		}
	}
	if err := os.Symlink("regular.txt", filepath.Join(repoDir, "regular.link")); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	testhelpers.MustGit(t, repoDir, "add", "regular.txt", "script.sh", "path with space.md", "docs/readme.md", "regular.link")
	testhelpers.MustGit(t, repoDir, "update-index", "--chmod=+x", "script.sh")
	gitlinkCommit := testhelpers.MustGit(t, repoDir, "rev-parse", "HEAD")
	testhelpers.MustGit(t, repoDir, "update-index", "--add", "--cacheinfo", "160000,"+gitlinkCommit+",vendor/module")
	testhelpers.MustGit(t, repoDir, "commit", "-m", "Add tree path mode fixtures")

	treeish := "HEAD"
	tests := []struct {
		name        string
		requirement string
		path        string
		wantMode    string
		wantPresent bool
	}{
		{
			name:        "regular file",
			requirement: "NFR-001-1 distinguishes regular files",
			path:        "regular.txt",
			wantMode:    "100644",
			wantPresent: true,
		},
		{
			name:        "executable file",
			requirement: "NFR-001-1 distinguishes executable regular files",
			path:        "script.sh",
			wantMode:    "100755",
			wantPresent: true,
		},
		{
			name:        "directory",
			requirement: "FR-001-10 distinguishes directories",
			path:        "docs",
			wantMode:    "040000",
			wantPresent: true,
		},
		{
			name:        "symlink",
			requirement: "FR-001-10 distinguishes symlinks",
			path:        "regular.link",
			wantMode:    "120000",
			wantPresent: true,
		},
		{
			name:        "gitlink",
			requirement: "FR-001-10 distinguishes submodules/gitlinks",
			path:        "vendor/module",
			wantMode:    "160000",
			wantPresent: true,
		},
		{
			name:        "path with spaces",
			requirement: "NFR-001-1 preserves exact repo-relative path queries",
			path:        "path with space.md",
			wantMode:    "100644",
			wantPresent: true,
		},
		{
			name:        "missing path",
			requirement: "NFR-001-1 distinguishes missing paths",
			path:        "missing.txt",
			wantPresent: false,
		},
		{
			name:        "pathspec magic treated literally",
			requirement: "artifact refs are exact repo-relative paths",
			path:        ":(top)README.md",
			wantPresent: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mode, present, err := git.TreePathMode(treeish, tt.path)
			if err != nil {
				t.Fatalf("%s: TreePathMode(%q, %q) error = %v", tt.requirement, treeish, tt.path, err)
			}
			if present != tt.wantPresent {
				t.Fatalf("%s: TreePathMode(%q, %q) present = %v, want %v", tt.requirement, treeish, tt.path, present, tt.wantPresent)
			}
			if mode != tt.wantMode {
				t.Errorf("%s: TreePathMode(%q, %q) mode = %q, want %q", tt.requirement, treeish, tt.path, mode, tt.wantMode)
			}
		})
	}
}

func TestReferenceContractRepositoryQueriesImmutableGitObjects(t *testing.T) {
	repoDir := setupTestRepo(t)
	git := New(repoDir)
	path := "--literal reference.md"
	committedContent := "  leading spaces\nbody\n\n"
	if err := os.WriteFile(filepath.Join(repoDir, path), []byte(committedContent), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, repoDir, "add", "--", path)
	testhelpers.MustGit(t, repoDir, "commit", "-m", "Add immutable blob fixture")

	resolved, err := git.ResolveCommit("HEAD")
	if err != nil {
		t.Fatalf("ResolveCommit() error = %v", err)
	}
	wantCommit := testhelpers.MustGit(t, repoDir, "rev-parse", "HEAD")
	if resolved != wantCommit {
		t.Errorf("ResolveCommit(HEAD) = %q, want %q", resolved, wantCommit)
	}

	content, err := git.ReadBlob(resolved, path)
	if err != nil {
		t.Fatalf("ReadBlob() error = %v", err)
	}
	if content != committedContent {
		t.Errorf("ReadBlob() = %q, want exact bytes %q", content, committedContent)
	}

	oid, err := git.BlobOID(resolved, path)
	if err != nil {
		t.Fatalf("BlobOID() error = %v", err)
	}
	wantOID := testhelpers.MustGit(t, repoDir, "rev-parse", resolved+":"+path)
	if oid != wantOID {
		t.Errorf("BlobOID() = %q, want %q", oid, wantOID)
	}

	if err := os.WriteFile(filepath.Join(repoDir, path), []byte("dirty worktree\n"), 0644); err != nil {
		t.Fatal(err)
	}
	content, err = git.ReadBlob(resolved, path)
	if err != nil {
		t.Fatalf("ReadBlob() after worktree edit error = %v", err)
	}
	if content != committedContent {
		t.Errorf("ReadBlob() consulted worktree: got %q, want %q", content, committedContent)
	}
}

func TestReferenceContractRepositoryQueriesRejectInvalidInputs(t *testing.T) {
	repoDir := setupTestRepo(t)
	git := New(repoDir)

	if _, err := git.ResolveCommit(""); err == nil {
		t.Error("ResolveCommit(empty) error = nil")
	}
	if _, err := git.ResolveCommit("--help"); err == nil {
		t.Error("ResolveCommit(option-like ref) error = nil")
	}
	if _, err := git.ReadBlob("HEAD", "missing.md"); err == nil {
		t.Error("ReadBlob(missing path) error = nil")
	}
	if _, err := git.ReadBlob("", "README.md"); err == nil {
		t.Error("ReadBlob(empty revision) error = nil")
	}
	if _, err := git.BlobOID("HEAD", "missing.md"); err == nil {
		t.Error("BlobOID(missing path) error = nil")
	}
	if _, err := git.BlobOID("HEAD", "."); err == nil {
		t.Error("BlobOID(tree path) error = nil, want blob-type rejection")
	}
	if _, err := git.ReadBlob("HEAD", "."); err == nil {
		t.Error("ReadBlob(tree path) error = nil, want blob-type rejection")
	}
}

func TestGetWorktreeHEAD(t *testing.T) {
	repoDir := setupTestRepo(t)
	git := New(repoDir)

	taskID := "task-123"

	// Create a worktree
	if _, err := git.CreateWorktree(taskID, "integration"); err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}

	// Get the worktree's HEAD
	headSHA, err := git.GetWorktreeHEAD(taskID)
	if err != nil {
		t.Fatalf("GetWorktreeHEAD() error = %v", err)
	}

	// Verify it's a valid SHA (40 chars)
	if len(headSHA) != 40 {
		t.Errorf("GetWorktreeHEAD() returned %d chars, want 40", len(headSHA))
	}

	// Get integration branch HEAD for comparison
	integrationSHA, err := git.GetCommitSHA("integration")
	if err != nil {
		t.Fatalf("GetCommitSHA() error = %v", err)
	}

	// Worktree should be at same commit as integration (since we just created it)
	if headSHA != integrationSHA {
		t.Errorf("GetWorktreeHEAD() = %s, want %s", headSHA, integrationSHA)
	}
}

func TestResolveWorktreeCommit_Head(t *testing.T) {
	repoDir := setupTestRepo(t)
	git := New(repoDir)

	taskID := "task-resolve-head"
	if _, err := git.CreateWorktree(taskID, "integration"); err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}

	resolvedSHA, err := git.ResolveWorktreeCommit(taskID, "HEAD")
	if err != nil {
		t.Fatalf("ResolveWorktreeCommit() error = %v", err)
	}
	headSHA, err := git.GetWorktreeHEAD(taskID)
	if err != nil {
		t.Fatalf("GetWorktreeHEAD() error = %v", err)
	}

	if resolvedSHA != headSHA {
		t.Errorf("ResolveWorktreeCommit(HEAD) = %s, want %s", resolvedSHA, headSHA)
	}
}

func TestResolveWorktreeCommit_InvalidRef(t *testing.T) {
	repoDir := setupTestRepo(t)
	git := New(repoDir)

	taskID := "task-resolve-invalid"
	if _, err := git.CreateWorktree(taskID, "integration"); err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}

	_, err := git.ResolveWorktreeCommit(taskID, "not-a-ref")
	if err == nil {
		t.Fatal("ResolveWorktreeCommit() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "failed to resolve worktree commit ref") {
		t.Fatalf("ResolveWorktreeCommit() error = %v, want ref-resolution error", err)
	}
}

func TestGetWorktreeBranch_Success(t *testing.T) {
	repoDir := setupTestRepo(t)
	git := New(repoDir)

	// Create a worktree
	taskID := "task-branch-test"
	if _, err := git.CreateWorktree(taskID, "integration"); err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}
	wtPath := git.GetWorktreePath(taskID)

	// Get the branch name
	branch, err := git.GetWorktreeBranch(wtPath)
	if err != nil {
		t.Fatalf("GetWorktreeBranch() error = %v", err)
	}

	expectedBranch := "task/" + taskID
	if branch != expectedBranch {
		t.Errorf("GetWorktreeBranch() = %q, want %q", branch, expectedBranch)
	}
}

func TestGetWorktreeBranch_DetachedHead(t *testing.T) {
	repoDir := setupTestRepo(t)
	git := New(repoDir)

	// Create a worktree
	taskID := "task-detached-head"
	if _, err := git.CreateWorktree(taskID, "integration"); err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}
	wtPath := git.GetWorktreePath(taskID)

	// Detach HEAD by checking out a commit SHA
	commitSHA := testhelpers.MustGit(t, wtPath, "rev-parse", "HEAD")
	testhelpers.MustGit(t, wtPath, "checkout", commitSHA)

	// Get the branch name (should be empty for detached HEAD)
	branch, err := git.GetWorktreeBranch(wtPath)
	if err != nil {
		t.Fatalf("GetWorktreeBranch() error = %v", err)
	}

	if branch != "" {
		t.Errorf("GetWorktreeBranch() = %q, want empty string for detached HEAD", branch)
	}
}

func TestIsAncestor(t *testing.T) {
	repoDir := setupTestRepo(t)
	git := New(repoDir)

	// Get the initial commit
	mainSHA, err := git.GetCommitSHA("main")
	if err != nil {
		t.Fatal(err)
	}
	integrationSHA, err := git.GetCommitSHA("integration")
	if err != nil {
		t.Fatal(err)
	}

	// main should be ancestor of integration
	isAnc, err := git.IsAncestor(mainSHA, integrationSHA)
	if err != nil {
		t.Fatalf("IsAncestor() error = %v", err)
	}
	if !isAnc {
		t.Error("Expected main to be ancestor of integration")
	}

	// integration should NOT be ancestor of main (it has extra commits)
	isAnc, err = git.IsAncestor(integrationSHA, mainSHA)
	if err != nil {
		t.Fatalf("IsAncestor() error = %v", err)
	}
	if isAnc {
		t.Error("Expected integration NOT to be ancestor of main")
	}

	// same commit is ancestor of itself
	isAnc, err = git.IsAncestor(integrationSHA, integrationSHA)
	if err != nil {
		t.Fatalf("IsAncestor() error = %v", err)
	}
	if !isAnc {
		t.Error("Expected commit to be ancestor of itself")
	}
}

func TestGetMergeBase(t *testing.T) {
	repoDir := setupTestRepo(t)
	git := New(repoDir)

	mainSHA, err := git.GetCommitSHA("main")
	if err != nil {
		t.Fatal(err)
	}

	base, err := git.GetMergeBase("main", "integration")
	if err != nil {
		t.Fatalf("GetMergeBase() error = %v", err)
	}
	if base != mainSHA {
		t.Errorf("GetMergeBase(main, integration) = %s, want %s", base, mainSHA)
	}
}
