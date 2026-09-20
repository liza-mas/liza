package git

import (
	"fmt"
	"strings"
)

// PatchID returns the content identity of a commit: the stable hash of its
// diff, independent of its SHA, parents, committer or date. A rebase preserves
// it, which is what lets a rewritten commit be recognized as the same change.
//
// Merge commits have no single diff and yield no patch id; callers must treat
// an empty result as "no content identity", never as a match.
func (g *Git) PatchID(commit string) (string, error) {
	if commit == "" {
		return "", fmt.Errorf("patch id requires a non-empty commit")
	}
	resolved, err := g.ResolveCommit(commit)
	if err != nil {
		return "", err
	}
	diff, err := g.exec("diff-tree", "--patch", "--no-color", "--full-index", "--end-of-options", resolved)
	if err != nil {
		return "", fmt.Errorf("failed to read diff for %s: %w", commit, err)
	}
	if strings.TrimSpace(diff) == "" {
		return "", nil
	}
	out, err := gitPatchID(g.projectRoot, diff)
	if err != nil {
		return "", fmt.Errorf("failed to compute patch id for %s: %w", commit, err)
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return "", nil
	}
	return fields[0], nil
}

// CommitsIn lists commit object IDs reachable from ref, newest first.
func (g *Git) CommitsIn(ref string, limit int) ([]string, error) {
	if ref == "" {
		return nil, fmt.Errorf("commit listing requires a non-empty ref")
	}
	args := []string{"rev-list", "--no-merges"}
	if limit > 0 {
		args = append(args, fmt.Sprintf("--max-count=%d", limit))
	}
	args = append(args, "--end-of-options", ref)
	output, err := g.exec(args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list commits for %q: %w", ref, err)
	}
	if strings.TrimSpace(output) == "" {
		return nil, nil
	}
	return strings.Fields(output), nil
}
