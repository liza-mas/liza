package git

import (
	"fmt"
	"strconv"
	"strings"
)

// TreePathMode returns the raw Git object mode for a path in a treeish.
func (g *Git) TreePathMode(treeish, path string) (mode string, present bool, err error) {
	if path == "" {
		return "", false, fmt.Errorf("tree path mode requires a non-empty path")
	}

	output, err := g.exec("ls-tree", "--full-tree", "-z", treeish, "--", literalPathspec(path))
	if err != nil {
		return "", false, fmt.Errorf("failed to query tree path mode for %q at %q: %w", path, treeish, err)
	}
	if output == "" {
		return "", false, nil
	}

	records := strings.Split(strings.TrimSuffix(output, "\x00"), "\x00")
	if len(records) != 1 {
		return "", false, fmt.Errorf("expected one tree entry for %q at %q, got %d", path, treeish, len(records))
	}

	mode, _, ok := strings.Cut(records[0], " ")
	if !ok || mode == "" {
		return "", false, fmt.Errorf("malformed tree entry for %q at %q: %q", path, treeish, records[0])
	}
	return mode, true, nil
}

func literalPathspec(path string) string {
	return ":(literal)" + path
}

// CalculateDrift returns the number of commits between baseCommit and targetBranch
// This indicates how far the integration branch has moved since the task started
func (g *Git) CalculateDrift(baseCommit, targetBranch string) (int, error) {
	// git rev-list --count base..target
	output, err := g.exec("rev-list", "--count", baseCommit+".."+targetBranch)
	if err != nil {
		return 0, fmt.Errorf("failed to calculate drift: %w", err)
	}

	count, err := strconv.Atoi(output)
	if err != nil {
		return 0, fmt.Errorf("failed to parse commit count: %w", err)
	}

	return count, nil
}

// IsAncestor checks if commitA is an ancestor of commitB
func (g *Git) IsAncestor(commitA, commitB string) (bool, error) {
	_, err := g.exec("merge-base", "--is-ancestor", commitA, commitB)
	if err != nil {
		// Check if error is "not an ancestor" (exit code 1)
		if strings.Contains(err.Error(), "exit status 1") {
			return false, nil
		}
		return false, fmt.Errorf("merge-base failed: %w", err)
	}
	return true, nil
}

// GetWorktreeHEAD returns the commit SHA of the worktree's HEAD
func (g *Git) GetWorktreeHEAD(taskID string) (string, error) {
	wtPath := g.GetWorktreePath(taskID)
	return g.execInDir(wtPath, "rev-parse", "HEAD")
}

// ResolveWorktreeCommit resolves ref to a full commit SHA inside a task worktree.
func (g *Git) ResolveWorktreeCommit(taskID, ref string) (string, error) {
	wtPath := g.GetWorktreePath(taskID)
	commit, err := g.execInDir(wtPath, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("failed to resolve worktree commit ref %q: %w", ref, err)
	}
	return commit, nil
}

// GetWorktreeBranch returns the current branch name in a worktree
func (g *Git) GetWorktreeBranch(wtPath string) (string, error) {
	branch, err := g.execInDir(wtPath, "branch", "--show-current")
	if err != nil {
		return "", fmt.Errorf("failed to get worktree branch: %w", err)
	}
	return branch, nil
}
