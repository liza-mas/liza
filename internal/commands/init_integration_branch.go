package commands

import (
	"errors"
	"fmt"
	"strings"

	gitpkg "github.com/liza-mas/liza/internal/gitenv"
)

// requireIntegrationBranchCreatable fails before anything is written when the
// integration branch is absent and the repository has no commit to branch from.
func (m *masInit) requireIntegrationBranchCreatable() error {
	if _, err := integrationBranchNeedsCreate(m.lizaPaths.ProjectRoot(), m.branch); err != nil {
		return fmt.Errorf("failed to create integration branch %q: %w", m.branch, err)
	}
	return nil
}

// ensureIntegrationBranch creates the integration branch if it doesn't exist; a
// failure removes the runtime directory.
func (m *masInit) ensureIntegrationBranch() error {
	if err := createIntegrationBranch(m.lizaPaths.ProjectRoot(), m.branch); err != nil {
		m.cleanupInit()
		return fmt.Errorf("failed to create integration branch %q: %w", m.branch, err)
	}
	return nil
}

// The helpers below are the C4 sites (Scope 18): their Git subprocesses run
// through gitenv's timed runners in dir. A timeout is an error, never an answer:
// it is not read as an invalid name, an absent branch or an unborn HEAD.

// isGitTimeout reports whether err is a Git subprocess killed by its timeout.
func isGitTimeout(err error) bool {
	var timeout *gitpkg.TimeoutError
	return errors.As(err, &timeout)
}

// validateBranchName checks in dir that name is a valid git branch name.
func validateBranchName(dir, name string) error {
	output, err := gitpkg.CombinedOutput(dir, "check-ref-format", "--branch", name)
	if err != nil {
		if isGitTimeout(err) {
			return err
		}
		return fmt.Errorf("not a valid git branch name: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func createIntegrationBranch(dir, name string) error {
	needsCreate, err := integrationBranchNeedsCreate(dir, name)
	if err != nil {
		return err
	}
	if !needsCreate {
		return nil
	}

	output, err := gitpkg.CombinedOutput(dir, "branch", name, "HEAD")
	if err != nil {
		return fmt.Errorf("git branch failed: %w: %s", err, string(output))
	}

	return nil
}

func integrationBranchNeedsCreate(dir, name string) (bool, error) {
	_, err := gitpkg.Output(dir, "rev-parse", "--verify", name)
	if err == nil {
		return false, nil
	}
	if isGitTimeout(err) {
		return false, err
	}

	if _, err := gitpkg.Output(dir, "rev-parse", "--verify", "HEAD"); err != nil {
		if isGitTimeout(err) {
			return false, err
		}
		return false, fmt.Errorf("repo has no commits (HEAD is unborn)")
	}

	return true, nil
}
