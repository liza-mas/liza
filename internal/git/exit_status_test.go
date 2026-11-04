package git

import (
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers"
)

// Only git's exit status 1 means "not an ancestor" or "conflicts"; a fatal
// failure (status 128, whose text also contains "exit status 1") must surface
// as an error instead of a false negative.
func TestExitStatusOneOnlyMeansNegativeAnswer(t *testing.T) {
	repoDir := setupTestRepo(t)
	g := New(repoDir)
	base := testhelpers.MustGit(t, repoDir, "rev-parse", "main")
	head := testhelpers.MustGit(t, repoDir, "rev-parse", "integration")

	if ok, err := g.IsAncestor(head, base); err != nil || ok {
		t.Fatalf("IsAncestor(head, base) = %v, %v; want false, nil", ok, err)
	}

	defer hideRepository(t, repoDir)()
	// Swapped arguments: a true answer may come from the memo, a false one may not.
	if ok, err := g.IsAncestor(head, base); err == nil {
		t.Errorf("IsAncestor without a repository = %v, nil; want an error", ok)
	}
	if _, clean, err := g.MergeTree(base, head); err == nil {
		t.Errorf("MergeTree without a repository = clean %v, nil; want an error", clean)
	}
}
