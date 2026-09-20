package git

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestPatchID_SurvivesRewriteAndIsEmptyForMerges(t *testing.T) {
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	run := func(args ...string) string { return testhelpers.MustGit(t, root, args...) }
	g := New(root)

	run("checkout", "-q", "integration")
	origin := run("rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(root, "plan.md"), []byte("plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "plan.md")
	run("commit", "-q", "-m", "docs: plan")
	original := run("rev-parse", "HEAD")

	run("checkout", "-q", "-b", "rewritten", origin)
	if err := os.WriteFile(filepath.Join(root, "other.md"), []byte("other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "other.md")
	run("commit", "-q", "-m", "chore: other")
	run("cherry-pick", original)
	rewritten := run("rev-parse", "HEAD")

	if original == rewritten {
		t.Fatal("fixture did not rewrite the commit")
	}
	a, err := g.PatchID(original)
	if err != nil {
		t.Fatal(err)
	}
	b, err := g.PatchID(rewritten)
	if err != nil {
		t.Fatal(err)
	}
	if a == "" || a != b {
		t.Fatalf("PatchID(original)=%q PatchID(rewritten)=%q, want equal and non-empty", a, b)
	}

	run("checkout", "-q", "-b", "merged", origin)
	run("merge", "--no-ff", "-m", "merge plan", original)
	merge := run("rev-parse", "HEAD")
	id, err := g.PatchID(merge)
	if err != nil {
		t.Fatal(err)
	}
	if id != "" {
		t.Fatalf("PatchID(merge)=%q, want empty: a merge has no content identity", id)
	}
}

func TestCommitsIn_ExcludesMerges(t *testing.T) {
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	run := func(args ...string) string { return testhelpers.MustGit(t, root, args...) }
	g := New(root)

	run("checkout", "-q", "integration")
	origin := run("rev-parse", "HEAD")
	run("checkout", "-q", "-b", "side", origin)
	if err := os.WriteFile(filepath.Join(root, "side.md"), []byte("side\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "side.md")
	run("commit", "-q", "-m", "side")
	run("checkout", "-q", "integration")
	run("merge", "--no-ff", "-m", "merge side", "side")
	merge := run("rev-parse", "HEAD")

	commits, err := g.CommitsIn("integration", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range commits {
		if c == merge {
			t.Fatal("CommitsIn must exclude merge commits; they carry no content identity")
		}
	}
	if len(commits) < 2 {
		t.Fatalf("CommitsIn = %v, want the side and origin commits at least", commits)
	}
}
