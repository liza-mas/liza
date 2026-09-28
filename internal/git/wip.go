package git

import (
	"fmt"
	"os"
	"path/filepath"
)

// interruptedOperationMarkers are the per-worktree git-dir entries Git leaves
// while an operation waits for the user to resolve or continue it.
var interruptedOperationMarkers = []struct {
	path string
	name string
}{
	{"MERGE_HEAD", "merge"},
	{"rebase-merge", "rebase"},
	{"rebase-apply", "rebase"},
	{"CHERRY_PICK_HEAD", "cherry-pick"},
	{"REVERT_HEAD", "revert"},
}

// InterruptedOperation names the Git operation left in progress in a worktree
// (merge, rebase, cherry-pick or revert), or "conflict resolution" when the
// index holds unmerged entries.
// It returns "" when the worktree is at rest. Staging such a worktree would
// mark conflicts as resolved, and committing it could complete the operation.
func (g *Git) InterruptedOperation(wtPath string) (string, error) {
	for _, marker := range interruptedOperationMarkers {
		markerPath, err := g.execInDir(wtPath, "rev-parse", "--git-path", marker.path)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(markerPath) {
			markerPath = filepath.Join(wtPath, markerPath)
		}
		if _, err := os.Stat(markerPath); err == nil {
			return marker.name, nil
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("inspect %s: %w", marker.path, err)
		}
	}
	unmerged, err := g.execInDir(wtPath, "ls-files", "--unmerged")
	if err != nil {
		return "", err
	}
	if unmerged != "" {
		return "conflict resolution", nil
	}
	return "", nil
}

// CommitAllWIP stages every change in a worktree, tracked and untracked, and
// commits it without running hooks. The commit records interrupted work so it
// survives a claim release; it attests no quality. committed is false, and no
// commit is made, when there is nothing to commit. If the commit itself fails
// the changes stay staged, with their content untouched.
func (g *Git) CommitAllWIP(wtPath, message string) (sha string, committed bool, err error) {
	if _, err := g.execInDir(wtPath, "add", "-A"); err != nil {
		return "", false, err
	}
	staged, err := g.execInDir(wtPath, "diff", "--cached", "--name-only")
	if err != nil {
		return "", false, err
	}
	if staged == "" {
		return "", false, nil
	}
	if _, err := g.execInDir(wtPath, "commit", "--no-verify", "-m", message); err != nil {
		return "", false, err
	}
	sha, err = g.execInDir(wtPath, "rev-parse", "HEAD")
	if err != nil {
		return "", true, err
	}
	return sha, true, nil
}
