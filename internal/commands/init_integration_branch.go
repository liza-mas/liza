package commands

import (
	"fmt"
	"strings"

	gitpkg "github.com/liza-mas/liza/internal/gitenv"
)

// requireIntegrationBranchCreatable fails before anything is written when the
// integration branch is absent and the repository has no commit to branch from.
func (m *masInit) requireIntegrationBranchCreatable() error {
	if _, err := integrationBranchNeedsCreate(m.branch); err != nil {
		return fmt.Errorf("failed to create integration branch %q: %w", m.branch, err)
	}
	return nil
}

// ensureIntegrationBranch creates the integration branch if it doesn't exist; a
// failure removes the runtime directory.
func (m *masInit) ensureIntegrationBranch() error {
	if err := createIntegrationBranch(m.branch); err != nil {
		m.cleanupInit()
		return fmt.Errorf("failed to create integration branch %q: %w", m.branch, err)
	}
	return nil
}

// The three helpers below are the C4 sites (Scope 18): their Git subprocesses
// keep the inherited environment and working directory and have no timeout.

// validateBranchName checks that name is a valid git branch name.
func validateBranchName(name string) error {
	cmd := gitpkg.Command("check-ref-format", "--branch", name)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("not a valid git branch name: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func createIntegrationBranch(name string) error {
	needsCreate, err := integrationBranchNeedsCreate(name)
	if err != nil {
		return err
	}
	if !needsCreate {
		return nil
	}

	cmd := gitpkg.Command("branch", name, "HEAD")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git branch failed: %w: %s", err, string(output))
	}

	return nil
}

func integrationBranchNeedsCreate(name string) (bool, error) {
	cmd := gitpkg.Command("rev-parse", "--verify", name)
	if err := cmd.Run(); err == nil {
		return false, nil
	}

	cmd = gitpkg.Command("rev-parse", "--verify", "HEAD")
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("repo has no commits (HEAD is unborn)")
	}

	return true, nil
}
