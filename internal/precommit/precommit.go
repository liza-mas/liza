// Package precommit provides repo-state detection helpers for the
// pre-commit bootstrap planning step: checking whether the integration
// branch already carries .pre-commit-config.yaml, and whether a
// bootstrap task is already in flight anywhere in the state.
package precommit

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/liza-mas/liza/internal/gitenv"
	"github.com/liza-mas/liza/internal/models"
)

// Kind is the typed marker value (see architecture-1 §2.1) that the
// architect emits on output[].kind and that proceed.go treats as the
// authoritative dedup key. Exported so Go-side callers cannot drift on
// the literal. Go templates cannot import constants — the architect
// prompt template reads this value via RoleContextData.PreCommitKind
// rather than by package import.
const Kind = "bootstrap-precommit"

// ErrContextBuild is the sentinel returned (wrapped) by every error
// path in this package. Callers discriminate precommit-originated
// failures from other BuildPrompt failures via errors.Is — that
// discrimination lets the supervisor narrow its BLOCKED recovery to
// this package's error domain without misclassifying unrelated
// template/pipeline defects as task-local failures.
var ErrContextBuild = errors.New("precommit context build failed")

// ConfigExistsOnIntegration reports whether .pre-commit-config.yaml
// exists at the tip of the integration branch in the given project
// root. Reads committed state via git plumbing, not the working tree,
// so uncommitted human drift or in-progress worktree changes do not
// produce a false positive.
//
// Returns (true, nil) when the file is tracked on the branch.
// Returns (false, nil) when the branch exists and the file is not
// tracked on it. Returns (_, err) only for plumbing failures that
// prevent a reliable answer — empty inputs, invalid branch ref, or
// unexpected non-zero exit. Every error wraps ErrContextBuild.
func ConfigExistsOnIntegration(projectRoot, integrationBranch string) (bool, error) {
	if projectRoot == "" {
		return false, fmt.Errorf("precommit: projectRoot is empty: %w", ErrContextBuild)
	}
	if integrationBranch == "" {
		return false, fmt.Errorf("precommit: integrationBranch is empty: %w", ErrContextBuild)
	}

	// Step 1 — verify the ref exists. Isolates "branch invalid" as a
	// hard configuration error from "path absent on existing branch".
	verify := gitenv.Command("rev-parse", "--verify", "--quiet",
		integrationBranch+"^{commit}")
	verify.Dir = projectRoot
	if err := verify.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, fmt.Errorf(
				"precommit: integration branch %q not found in %s: %w",
				integrationBranch, projectRoot, ErrContextBuild)
		}
		return false, fmt.Errorf("precommit: rev-parse --verify %q: %v: %w",
			integrationBranch, err, ErrContextBuild)
	}

	// Step 2 — read the tree entry. Exit 0 with empty stdout means
	// "branch has no such path"; non-empty means tracked.
	lsTree := gitenv.Command("ls-tree", integrationBranch, "--",
		".pre-commit-config.yaml")
	lsTree.Dir = projectRoot
	var stdout, stderr bytes.Buffer
	lsTree.Stdout = &stdout
	lsTree.Stderr = &stderr
	if err := lsTree.Run(); err != nil {
		return false, fmt.Errorf(
			"precommit: ls-tree %q -- .pre-commit-config.yaml: %v (stderr: %s): %w",
			integrationBranch, err, strings.TrimSpace(stderr.String()), ErrContextBuild)
	}
	return stdout.Len() > 0, nil
}

// BootstrapInFlight reports whether any task in the given state has
// Kind == precommit.Kind and a non-terminal status. "Non-terminal"
// means Task.Status.IsTerminal() returns false — MERGED, ABANDONED,
// SUPERSEDED are terminal; every other status (including BLOCKED) is
// in flight. A blocked bootstrap will eventually merge once rescoped,
// so it genuinely is in flight (goal spec §Q2 "Rescope invariant").
//
// Repo-wide: scans state.Tasks with no goal/sprint filter; cross-goal
// parallelism is covered by construction. Returns false if state is
// nil.
func BootstrapInFlight(state *models.State) bool {
	if state == nil {
		return false
	}
	for i := range state.Tasks {
		t := &state.Tasks[i]
		if t.Kind == "" {
			continue
		}
		if t.Kind != Kind {
			continue
		}
		if t.Status.IsTerminal() {
			continue
		}
		return true
	}
	return false
}
