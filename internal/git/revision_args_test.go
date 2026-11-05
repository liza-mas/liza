package git

import (
	"strings"
	"testing"
)

// Revision ranges are passed with "--", so git never stats them as file
// names. A revision expression longer than a path component makes that stat
// fail on Linux the way a deep worktree path does on Windows ("Filename too
// long", exit 128).
func TestRangeCommandsDoNotStatRevisionsAsFiles(t *testing.T) {
	t.Parallel()
	repoDir := setupTestRepo(t)
	g := New(repoDir)
	long := "main" + strings.Repeat("~0", 140) // resolves to main; 284 chars

	if files, err := g.DiffFiles(repoDir, "main", long); err != nil || len(files) != 0 {
		t.Errorf("DiffFiles = %v, %v; want no files, nil", files, err)
	}
	if err := g.SyncMergedFiles("main", long); err != nil {
		t.Errorf("SyncMergedFiles: %v", err)
	}
	if err := g.RestoreSyncedFiles("main", long, "main"); err != nil {
		t.Errorf("RestoreSyncedFiles: %v", err)
	}
	if commits, _, err := g.RangeCommits("main", long); err != nil || len(commits) != 0 {
		t.Errorf("RangeCommits = %v, %v; want none, nil", commits, err)
	}
	if drift, err := g.CalculateDrift("main", long); err != nil || drift != 0 {
		t.Errorf("CalculateDrift = %d, %v; want 0, nil", drift, err)
	}
}
