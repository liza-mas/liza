// Package git provides git worktree operations for task isolation
package git

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/liza-mas/liza/internal/gitenv"
)

// Git provides git operations for worktree management
type Git struct {
	projectRoot string
}

// Worktree represents a git worktree entry
type Worktree struct {
	Path   string
	Commit string
	Branch string
}

// New creates a new Git instance for the given project root
func New(projectRoot string) *Git {
	return &Git{
		projectRoot: projectRoot,
	}
}

// exec runs a git command in the project root and returns output
func (g *Git) exec(args ...string) (string, error) {
	output, err := gitenv.CombinedOutput(g.projectRoot, args...)
	if err != nil {
		return "", fmt.Errorf("git %v failed: %w\nOutput: %s", args, err, output)
	}
	return strings.TrimSpace(string(output)), nil
}

// exitedWithStatus reports whether err is git exiting with status code. Match
// the code, never the error text: "exit status 128" contains "exit status 1".
func exitedWithStatus(err error, code int) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == code
}

// execInDir runs a git command in a specific directory
func (g *Git) execInDir(dir string, args ...string) (string, error) {
	output, err := gitenv.CombinedOutput(dir, args...)
	if err != nil {
		return "", fmt.Errorf("git %v failed: %w\nOutput: %s", args, err, output)
	}
	return strings.TrimSpace(string(output)), nil
}

// execInDirContext runs a git command in a specific directory and aborts the
// subprocess when ctx is canceled.
func (g *Git) execInDirContext(ctx context.Context, dir string, args ...string) (string, error) {
	output, err := gitenv.CombinedOutputContext(ctx, dir, args...)
	if err != nil {
		return "", fmt.Errorf("git %v failed: %w\nOutput: %s", args, err, output)
	}
	return strings.TrimSpace(string(output)), nil
}

// GetCurrentBranch returns the name of the current branch
func (g *Git) GetCurrentBranch() (string, error) {
	return g.exec("branch", "--show-current")
}

// GetCommitSHA returns the commit SHA for a given ref
// If short is true, returns 7-character short SHA
func (g *Git) GetCommitSHA(ref string, short ...bool) (string, error) {
	useShort := len(short) > 0 && short[0]

	if useShort {
		return g.exec("rev-parse", "--short", ref)
	}
	return g.exec("rev-parse", ref)
}

// BranchExists checks if a branch exists
func (g *Git) BranchExists(branch string) (bool, error) {
	_, err := g.exec("rev-parse", "--verify", "refs/heads/"+branch)
	if err != nil {
		// Check if error is because branch doesn't exist
		errMsg := err.Error()
		if strings.Contains(errMsg, "unknown revision") ||
			strings.Contains(errMsg, "not a valid ref") ||
			strings.Contains(errMsg, "Needed a single revision") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// CreateBranch creates a new branch from the current HEAD
func (g *Git) CreateBranch(branch string) error {
	_, err := g.exec("branch", branch)
	return err
}

// DeleteBranch deletes a branch (force delete with -D)
func (g *Git) DeleteBranch(branch string) error {
	_, err := g.exec("branch", "-D", branch)
	return err
}

// CheckoutBranch checks out a branch
func (g *Git) CheckoutBranch(branch string) error {
	_, err := g.exec("checkout", branch)
	return err
}

// ResetHard resets the current branch to the given commit, discarding all changes.
func (g *Git) ResetHard(ref string) error {
	_, err := g.exec("reset", "--hard", ref)
	return err
}
