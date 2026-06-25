package initcheck

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/gitenv"
)

const preCommitConfigPath = ".pre-commit-config.yaml"

// EnsureSpecCommittedClean verifies that specPath is a repo-local file whose
// committed content at HEAD matches the index and working tree.
func EnsureSpecCommittedClean(projectRoot, specPath string) (string, error) {
	return ensureCommittedCleanFile(projectRoot, specPath, "HEAD", "spec file", true)
}

// EnsurePreCommitConfigCommittedClean verifies that the pre-commit config is
// committed on the branch Liza will use for agent worktrees. If that branch
// does not exist yet, init will create it from HEAD, so HEAD is checked.
func EnsurePreCommitConfigCommittedClean(projectRoot, integrationBranch string) (string, error) {
	targetRef, err := initWorktreeSourceRef(projectRoot, integrationBranch)
	if err != nil {
		return "", err
	}
	return ensureCommittedCleanFile(projectRoot, filepath.Join(projectRoot, preCommitConfigPath), targetRef, "pre-commit config", false)
}

func ensureCommittedCleanFile(projectRoot, filePath, targetRef, label string, requireWorktreeFile bool) (string, error) {
	repoRel, err := repoRelativePath(projectRoot, filePath, label)
	if err != nil {
		return "", err
	}

	if requireWorktreeFile {
		info, err := os.Stat(filepath.Join(projectRoot, repoRel))
		if err != nil {
			return "", fmt.Errorf("%s does not exist: %s", label, filePath)
		}
		if info.IsDir() {
			return "", fmt.Errorf("%s path is a directory: %s", label, filePath)
		}
	}

	gitPath := filepath.ToSlash(repoRel)
	if _, err := gitenv.CombinedOutput(projectRoot, "cat-file", "-e", targetRef+":"+gitPath); err != nil {
		if targetRef == "HEAD" {
			return "", fmt.Errorf("%s must be fully committed before %s: %s", label, brand.Command("init"), gitPath)
		}
		return "", fmt.Errorf("%s must exist on %s before %s: %s", label, targetRef, brand.Command("init"), gitPath)
	}
	if changed, err := gitDiffHasChanges(projectRoot, "--cached", "--", gitPath); err != nil {
		return "", fmt.Errorf("failed to check %s git status: %w", label, err)
	} else if changed {
		return "", fmt.Errorf("%s has staged changes; commit them before %s: %s", label, brand.Command("init"), gitPath)
	}
	if changed, err := gitDiffHasChanges(projectRoot, "--", gitPath); err != nil {
		return "", fmt.Errorf("failed to check %s git status: %w", label, err)
	} else if changed {
		return "", fmt.Errorf("%s has unstaged changes; commit them before %s: %s", label, brand.Command("init"), gitPath)
	}

	return gitPath, nil
}

func initWorktreeSourceRef(projectRoot, integrationBranch string) (string, error) {
	if integrationBranch == "" {
		integrationBranch = "integration"
	}
	if _, err := gitenv.CombinedOutput(projectRoot, "rev-parse", "--verify", "--quiet", integrationBranch+"^{commit}"); err == nil {
		return integrationBranch, nil
	}
	if _, err := gitenv.CombinedOutput(projectRoot, "rev-parse", "--verify", "--quiet", "HEAD^{commit}"); err != nil {
		return "", fmt.Errorf("failed to resolve init worktree source ref: HEAD is unborn")
	}
	return "HEAD", nil
}

func repoRelativePath(projectRoot, filePath, label string) (string, error) {
	if projectRoot == "" {
		return "", fmt.Errorf("project root is empty")
	}
	if filePath == "" {
		return "", fmt.Errorf("%s path is empty", label)
	}

	rootAbs, err := filepath.Abs(projectRoot)
	if err != nil {
		return "", fmt.Errorf("failed to resolve project root: %w", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", fmt.Errorf("failed to resolve project root symlinks: %w", err)
	}
	rootAbs = resolvedRoot

	fileAbs := filePath
	if !filepath.IsAbs(fileAbs) {
		fileAbs = filepath.Join(rootAbs, fileAbs)
	}
	fileAbs, err = filepath.Abs(fileAbs)
	if err != nil {
		return "", fmt.Errorf("failed to resolve %s path: %w", label, err)
	}
	resolvedFile, err := filepath.EvalSymlinks(fileAbs)
	if err != nil {
		if _, lstatErr := os.Lstat(fileAbs); lstatErr == nil {
			return "", fmt.Errorf("failed to resolve %s path symlinks: %w", label, err)
		}
	} else {
		fileAbs = resolvedFile
	}

	rel, err := filepath.Rel(rootAbs, fileAbs)
	if err != nil {
		return "", fmt.Errorf("failed to resolve %s path relative to repo: %w", label, err)
	}
	cleanRel := filepath.Clean(rel)
	if cleanRel == "." || cleanRel == ".." || strings.HasPrefix(cleanRel, ".."+string(filepath.Separator)) || filepath.IsAbs(cleanRel) {
		return "", fmt.Errorf("%s must be inside the git repository: %s", label, filePath)
	}
	return cleanRel, nil
}

func gitDiffHasChanges(projectRoot string, args ...string) (bool, error) {
	fullArgs := append([]string{"diff", "--quiet"}, args...)
	if _, err := gitenv.CombinedOutput(projectRoot, fullArgs...); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return true, nil
		}
		return false, err
	}
	return false, nil
}
