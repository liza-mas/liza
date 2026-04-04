package git

import (
	"fmt"
	"strings"

	"github.com/liza-mas/liza/internal/gitenv"
)

// RebaseConflictError indicates a merge conflict during git rebase.
type RebaseConflictError struct {
	Output string // raw git output containing conflict details
}

func (e *RebaseConflictError) Error() string {
	return fmt.Sprintf("rebase conflict: %s", e.Output)
}

// FetchFromLocal fetches latest commits for a branch from the project root
// Used in worktrees to sync with integration branch
func (g *Git) FetchFromLocal(wtPath string, branch string) error {
	_, err := g.execInDir(wtPath, "fetch", g.projectRoot, branch)
	if err != nil {
		return fmt.Errorf("failed to fetch branch %s from project root: %w", branch, err)
	}
	return nil
}

// RebaseOnto rebases the current branch in a worktree onto the specified base branch.
// Must be called from within a worktree context.
// Returns *RebaseConflictError for merge conflicts, generic error for other failures.
func (g *Git) RebaseOnto(wtPath string, baseBranch string) error {
	cmd := gitenv.Command("rebase", baseBranch)
	cmd.Dir = wtPath
	rawOutput, err := cmd.CombinedOutput()
	if err != nil {
		out := string(rawOutput)
		// Classify using canonical git conflict markers from command output only,
		// not from the exec error wrapper, to avoid false positives.
		if strings.Contains(out, "CONFLICT") ||
			strings.Contains(out, "could not apply") {
			return &RebaseConflictError{Output: out}
		}
		return fmt.Errorf("rebase failed: %w\nOutput: %s", err, out)
	}
	return nil
}

// AbortRebase aborts an in-progress rebase in a worktree
func (g *Git) AbortRebase(wtPath string) error {
	_, err := g.execInDir(wtPath, "rebase", "--abort")
	if err != nil {
		return fmt.Errorf("failed to abort rebase: %w", err)
	}
	return nil
}
