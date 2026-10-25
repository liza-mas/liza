package git

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RefConflictError indicates a compare-and-swap failure on git update-ref.
// The ref's current value did not match the expected old SHA.
type RefConflictError struct {
	Ref      string
	Expected string
	Actual   string // may be empty if undetermined
}

func (e *RefConflictError) Error() string {
	return fmt.Sprintf("ref conflict on %s: expected %s, got %s", e.Ref, e.Expected, e.Actual)
}

// MergeTree computes a merge between two commits without touching the working tree.
// Returns the tree SHA of the merge result, a boolean indicating if it's clean (no conflicts),
// and any error. If not clean, the tree SHA contains the best-effort merge with conflict markers.
func (g *Git) MergeTree(baseCommit, branchCommit string) (treeSHA string, clean bool, err error) {
	// Use git merge-tree --write-tree which returns the tree SHA directly
	// Exit code 0 = clean merge, 1 = has conflicts
	output, err := g.exec("merge-tree", "--write-tree", "--no-messages", baseCommit, branchCommit)
	if err != nil {
		// Check if it's a conflict (exit code 1)
		errStr := err.Error()
		if strings.Contains(errStr, "exit status 1") {
			// Conflicts exist - the output still contains the tree SHA
			// with best-effort merge (with conflict markers)
			return output, false, nil
		}
		return "", false, fmt.Errorf("merge-tree failed: %w", err)
	}
	return output, true, nil
}

// CreateCommitFromTree creates a commit from a tree SHA with given parents and message.
// Returns the new commit SHA. This does not touch the working tree.
func (g *Git) CreateCommitFromTree(treeSHA string, parents []string, message string) (string, error) {
	args := []string{"commit-tree", treeSHA, "-m", message}
	for _, parent := range parents {
		args = append(args, "-p", parent)
	}
	output, err := g.exec(args...)
	if err != nil {
		return "", fmt.Errorf("commit-tree failed: %w", err)
	}
	return output, nil
}

// UpdateRef updates a ref to point to a new commit.
// When expectedOldSHA is non-empty, this is a compare-and-swap (CAS) operation:
// git update-ref <ref> <new> <old>. If the ref's current value doesn't match
// expectedOldSHA, git exits with status 128 and a RefConflictError is returned.
func (g *Git) UpdateRef(ref, commitSHA, expectedOldSHA string) error {
	args := []string{"update-ref", ref, commitSHA}
	if expectedOldSHA != "" {
		args = append(args, expectedOldSHA)
	}
	_, err := g.exec(args...)
	if err != nil {
		if expectedOldSHA != "" {
			errMsg := err.Error()
			// git update-ref CAS mismatch: exit status 128,
			// output contains "is at <actual> but expected <old>"
			if strings.Contains(errMsg, "but expected") {
				actual := extractActualSHA(errMsg)
				return &RefConflictError{Ref: ref, Expected: expectedOldSHA, Actual: actual}
			}
		}
		return fmt.Errorf("update-ref %s failed: %w", ref, err)
	}
	return nil
}

// extractActualSHA parses the actual SHA from a git update-ref CAS error message.
// Format: "...is at <sha> but expected <sha>"
func extractActualSHA(errMsg string) string {
	const marker = "is at "
	idx := strings.Index(errMsg, marker)
	if idx == -1 {
		return ""
	}
	rest := errMsg[idx+len(marker):]
	if spaceIdx := strings.IndexByte(rest, ' '); spaceIdx > 0 {
		return rest[:spaceIdx]
	}
	return ""
}

// MergeBranch attempts to merge a branch into the current branch
// Returns true if fast-forward was possible, false if merge commit was created
// Returns error if merge fails (e.g., conflicts)
func (g *Git) MergeBranch(branch string) (fastForward bool, mergeCommit string, err error) {
	// Try fast-forward first
	_, err = g.exec("merge", "--ff-only", branch)
	if err == nil {
		// Fast-forward succeeded
		commit, err := g.GetCommitSHA("HEAD")
		return true, commit, err
	}

	// Fast-forward failed, try regular merge
	var output string
	output, err = g.exec("merge", "--no-ff", "-m", "Merge "+branch, branch)
	if err != nil {
		// Check if it's a merge conflict
		if strings.Contains(output, "CONFLICT") || strings.Contains(err.Error(), "conflict") {
			return false, "", fmt.Errorf("merge conflict: %w", err)
		}
		return false, "", err
	}

	// Get the merge commit
	commit, err := g.GetCommitSHA("HEAD")
	return false, commit, err
}

// AbortMerge aborts an in-progress merge
func (g *Git) AbortMerge() error {
	_, err := g.exec("merge", "--abort")
	return err
}

// DiffFiles returns the list of files changed between two commits in a directory.
// Typically used as DiffFiles(wtPath, baseCommit, "HEAD") to get files changed in a worktree.
func (g *Git) DiffFiles(dir, commitA, commitB string) ([]string, error) {
	output, err := g.execInDir(dir, "diff", "--name-only", commitA+".."+commitB)
	if err != nil {
		return nil, err
	}
	if output == "" {
		return nil, nil
	}
	return strings.Split(output, "\n"), nil
}

// SyncMergedFiles updates the working tree and index for files changed between
// two commits. Required after update-ref advances a branch, since update-ref
// only moves the ref pointer without touching the working tree or index.
// Only touches files affected by the merge — safe for working trees with
// unrelated pending changes (for example, project runtime state).
//
// Handles all change types: added/modified files are checked out from toCommit,
// deleted files are removed, and renames are handled by removing the old path
// and checking out the new path.
func (g *Git) SyncMergedFiles(fromCommit, toCommit string) error {
	// Use --name-status to distinguish change types, including renames.
	// Format: "<status>\t<path>" or "<status>\t<old>\t<new>" for renames/copies.
	output, err := g.exec("diff", "--name-status", fromCommit+".."+toCommit)
	if err != nil {
		return fmt.Errorf("failed to diff %s..%s: %w", shortSHA(fromCommit), shortSHA(toCommit), err)
	}
	if output == "" {
		return nil
	}

	var checkoutPaths []string // files to checkout from toCommit
	var removePaths []string   // files to remove from working tree + index

	for _, line := range strings.Split(output, "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}
		status := parts[0]

		switch {
		case status == "D":
			// Deleted: remove old path.
			removePaths = append(removePaths, parts[1])

		case strings.HasPrefix(status, "R"):
			// Renamed: remove old path, checkout new path.
			// Format: "R100\told\tnew" (similarity index varies).
			if len(parts) >= 3 {
				removePaths = append(removePaths, parts[1])
				checkoutPaths = append(checkoutPaths, parts[2])
			}

		case strings.HasPrefix(status, "C"):
			// Copied: checkout new path only, old path still exists in toCommit.
			if len(parts) >= 3 {
				checkoutPaths = append(checkoutPaths, parts[2])
			}

		default:
			// Added, Modified, Type-changed: checkout from toCommit.
			checkoutPaths = append(checkoutPaths, parts[1])
		}
	}

	// Checkout files that should exist in toCommit.
	if len(checkoutPaths) > 0 {
		args := append([]string{"checkout", toCommit, "--"}, checkoutPaths...)
		if _, err := g.exec(args...); err != nil {
			return fmt.Errorf("failed to checkout merged files: %w", err)
		}
	}

	// Remove files that should not exist in toCommit.
	if len(removePaths) > 0 {
		for _, f := range removePaths {
			path := filepath.Join(g.projectRoot, f)
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed to remove deleted file %s: %w", f, err)
			}
		}
		args := append([]string{"rm", "--cached", "--ignore-unmatch", "--"}, removePaths...)
		if _, err := g.exec(args...); err != nil {
			return fmt.Errorf("failed to update index for removed files: %w", err)
		}
	}

	return nil
}

// RestoreSyncedFiles undoes the working-tree changes made by SyncMergedFiles.
// It restores each file affected by the syncFrom..syncTo range to the version
// at restoreRef. Files that don't exist at restoreRef are removed from the
// working tree and index. This keeps the working tree clean when the checked-out
// branch differs from the integration branch that was advanced by update-ref.
//
// Uses --no-renames so renames appear as delete+add — the ls-tree filter then
// handles both cases naturally.
func (g *Git) RestoreSyncedFiles(syncFrom, syncTo, restoreRef string) error {
	// Get affected files using --no-renames so renames flatten to delete+add.
	output, err := g.exec("diff", "--no-renames", "--name-only", syncFrom+".."+syncTo)
	if err != nil {
		return fmt.Errorf("failed to diff %s..%s: %w", shortSHA(syncFrom), shortSHA(syncTo), err)
	}
	if output == "" {
		return nil
	}

	affectedFiles := strings.Split(output, "\n")
	var nonEmpty []string
	for _, f := range affectedFiles {
		if f != "" {
			nonEmpty = append(nonEmpty, f)
		}
	}
	if len(nonEmpty) == 0 {
		return nil
	}

	// Check which files exist at restoreRef.
	// -r recurses into subdirectories so pathspec matching works for nested paths.
	lsArgs := append([]string{"ls-tree", "-r", "--name-only", restoreRef, "--"}, nonEmpty...)
	lsOutput, err := g.exec(lsArgs...)
	// ls-tree returns empty output (no error) when none of the files exist at restoreRef.
	if err != nil {
		return fmt.Errorf("failed to ls-tree %s: %w", restoreRef, err)
	}

	existsAtRef := make(map[string]bool)
	if lsOutput != "" {
		for _, f := range strings.Split(lsOutput, "\n") {
			if f != "" {
				existsAtRef[f] = true
			}
		}
	}

	var checkoutPaths []string
	var removePaths []string
	for _, f := range nonEmpty {
		if existsAtRef[f] {
			checkoutPaths = append(checkoutPaths, f)
		} else {
			removePaths = append(removePaths, f)
		}
	}

	// Restore files that exist at restoreRef.
	if len(checkoutPaths) > 0 {
		args := append([]string{"checkout", restoreRef, "--"}, checkoutPaths...)
		if _, err := g.exec(args...); err != nil {
			return fmt.Errorf("failed to checkout files from %s: %w", restoreRef, err)
		}
	}

	// Remove files that don't exist at restoreRef.
	if len(removePaths) > 0 {
		for _, f := range removePaths {
			path := filepath.Join(g.projectRoot, f)
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed to remove file %s: %w", f, err)
			}
		}
		args := append([]string{"rm", "--cached", "--ignore-unmatch", "--"}, removePaths...)
		if _, err := g.exec(args...); err != nil {
			return fmt.Errorf("failed to update index for removed files: %w", err)
		}
	}

	return nil
}

// shortSHALength is the abbreviated SHA length used in messages.
const shortSHALength = 7

// shortSHA returns the first 7 characters of a SHA, or the full string if shorter.
func shortSHA(sha string) string {
	if len(sha) > shortSHALength {
		return sha[:shortSHALength]
	}
	return sha
}
