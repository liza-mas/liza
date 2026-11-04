package git

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers"
)

// hideRepository makes every git command in repoDir fail until the returned
// function restores it, so an answer that still arrives came from the memo.
func hideRepository(t *testing.T, repoDir string) (restore func()) {
	t.Helper()
	gitDir := filepath.Join(repoDir, ".git")
	if err := os.Rename(gitDir, gitDir+"-hidden"); err != nil {
		t.Fatal(err)
	}
	return func() {
		if err := os.Rename(gitDir+"-hidden", gitDir); err != nil {
			t.Fatal(err)
		}
	}
}

// Answers keyed by full object IDs are served from the memo; the same query by
// ref name, whose target can move, always runs git.
func TestObjectQueriesMemoizeFullObjectIDsOnly(t *testing.T) {
	repoDir := setupTestRepo(t)
	g := New(repoDir)
	writeAndCommit(t, repoDir, "dir/entry.md", "entry body\n")
	base := testhelpers.MustGit(t, repoDir, "rev-parse", "HEAD~1")
	head := testhelpers.MustGit(t, repoDir, "rev-parse", "HEAD")

	entry, _, err := g.TreeEntryAt(head, "dir/entry.md")
	if err != nil {
		t.Fatal(err)
	}
	mode, _, err := g.TreePathMode(head, "dir/entry.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, present, err := g.TreePathMode(head, "absent.md"); err != nil || present {
		t.Fatalf("TreePathMode(absent) = %v, %v", present, err)
	}
	body, err := g.ReadBlobObject(entry.OID)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := g.IsAncestor(base, head); err != nil || !ok {
		t.Fatalf("IsAncestor(base, head) = %v, %v", ok, err)
	}
	// The same queries by ref name, so a memo that kept them would have them.
	if _, _, err := g.TreeEntryAt("HEAD", "dir/entry.md"); err != nil {
		t.Fatal(err)
	}
	if ok, err := g.IsAncestor("HEAD~1", "HEAD"); err != nil || !ok {
		t.Fatalf("IsAncestor(HEAD~1, HEAD) = %v, %v", ok, err)
	}

	defer hideRepository(t, repoDir)()
	if got, present, err := g.TreeEntryAt(head, "dir/entry.md"); err != nil || !present || got != entry {
		t.Errorf("memoized TreeEntryAt = %+v, %v, %v; want %+v", got, present, err, entry)
	}
	if got, present, err := g.TreePathMode(head, "dir/entry.md"); err != nil || !present || got != mode {
		t.Errorf("memoized TreePathMode = %q, %v, %v; want %q", got, present, err, mode)
	}
	if _, present, err := g.TreePathMode(head, "absent.md"); err != nil || present {
		t.Errorf("memoized absent TreePathMode = %v, %v; want absent", present, err)
	}
	if got, err := g.ReadBlobObject(entry.OID); err != nil || got != body {
		t.Errorf("memoized ReadBlobObject = %q, %v; want %q", got, err, body)
	}
	if ok, err := g.IsAncestor(base, head); err != nil || !ok {
		t.Errorf("memoized IsAncestor = %v, %v; want true", ok, err)
	}
	if _, _, err := g.TreeEntryAt("HEAD", "dir/entry.md"); err == nil {
		t.Error("TreeEntryAt(HEAD) answered from the memo; a ref name must reach git")
	}
	if ok, err := g.IsAncestor("HEAD~1", "HEAD"); ok && err == nil {
		t.Error("IsAncestor by ref names answered from the memo; ref names must reach git")
	}
}

// "Not an ancestor" is never stored: deepening a shallow clone reveals
// ancestry the shallow history hid.
func TestObjectQueriesDoNotStoreNonAncestry(t *testing.T) {
	source := setupTestRepo(t)
	base := testhelpers.MustGit(t, source, "rev-parse", "HEAD~1")
	head := testhelpers.MustGit(t, source, "rev-parse", "HEAD")
	clone := filepath.Join(t.TempDir(), "clone")
	testhelpers.MustGit(t, filepath.Dir(clone), "clone", "-q", "--depth", "1", "--branch", "integration", "file://"+source, clone)
	g := New(clone)

	if ok, _ := g.IsAncestor(base, head); ok {
		t.Fatal("a depth-1 clone reported an ancestor it does not hold")
	}
	testhelpers.MustGit(t, clone, "fetch", "-q", "--unshallow")
	if ok, err := g.IsAncestor(base, head); err != nil || !ok {
		t.Errorf("IsAncestor after deepening = %v, %v; the shallow false must not be stored", ok, err)
	}
}

// Failures are never stored: a missing object may arrive later.
func TestObjectQueriesDoNotStoreFailures(t *testing.T) {
	repoDir := setupTestRepo(t)
	g := New(repoDir)
	head := testhelpers.MustGit(t, repoDir, "rev-parse", "HEAD")

	restore := hideRepository(t, repoDir)
	if _, _, err := g.TreeEntryAt(head, "README.md"); err == nil {
		t.Fatal("TreeEntryAt succeeded without a repository")
	}
	restore()
	if _, present, err := g.TreeEntryAt(head, "README.md"); err != nil || !present {
		t.Errorf("TreeEntryAt after a failure = %v, %v; the failure must not be stored", present, err)
	}
}

// One repository never answers for another, which may lack the object.
func TestObjectQueriesAreKeyedByRepository(t *testing.T) {
	repoDir := setupTestRepo(t)
	head := testhelpers.MustGit(t, repoDir, "rev-parse", "HEAD")
	if _, _, err := New(repoDir).TreeEntryAt(head, "README.md"); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	testhelpers.MustGit(t, other, "init", "-q")
	if _, _, err := New(other).TreeEntryAt(head, "README.md"); err == nil {
		t.Error("a repository without the commit was answered from another repository's memo")
	}
}
