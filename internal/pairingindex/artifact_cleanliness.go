package pairingindex

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/liza-mas/liza/internal/gitenv"
	"github.com/liza-mas/liza/internal/scipsearch"
)

var stacklitArtifactExcludeMu sync.Mutex

func ensureStacklitArtifactCleanliness(repoRoot string) error {
	repoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}

	if err := ensureGeneratedArtifactCleanliness(repoRoot, stacklitArtifactName); err != nil {
		return err
	}
	return ensureGeneratedArtifactCleanliness(repoRoot, stacklitInsightsArtifactName)
}

func ensureFunctionalClustersArtifactCleanliness(repoRoot string) error {
	repoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	return ensureGeneratedArtifactCleanliness(repoRoot, functionalClustersArtifactName)
}

func ensureScipArtifactCleanliness(repoRoot string, plans []scipsearch.LanguageAggregatePlan) error {
	for _, plan := range plans {
		rel, err := filepath.Rel(repoRoot, plan.OutputPath)
		if err != nil || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
			return fmt.Errorf("scip-search output path %q is outside repository root %q", plan.OutputPath, repoRoot)
		}
		if err := ensureGeneratedArtifactCleanliness(repoRoot, filepath.ToSlash(rel)); err != nil {
			return err
		}
	}
	return nil
}

func ensureGeneratedArtifactCleanliness(repoRoot, artifact string) error {
	tracked, err := artifactTracked(repoRoot, artifact)
	if err != nil {
		return err
	}
	if tracked {
		return nil
	}
	ignored, err := artifactIgnored(repoRoot, artifact)
	if err != nil {
		return err
	}
	if ignored {
		return nil
	}
	if _, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(artifact))); err == nil {
		return fmt.Errorf("%s is untracked and not ignored or privately excluded; commit it, add it to .gitignore, or add it to .git/info/exclude before enabling pairing index activation", artifact)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect repo-root %s: %w", artifact, err)
	}
	return ensureRepoPrivateExclude(repoRoot, artifact)
}

func artifactTracked(repoRoot, artifact string) (bool, error) {
	output, err := gitenv.CombinedOutput(repoRoot, "ls-files", "--error-unmatch", artifact)
	if err == nil {
		return true, nil
	}
	if gitUnmatchedPath(err, output) {
		return false, nil
	}
	return false, fmt.Errorf("inspect repo-root %s tracking: %w%s", artifact, err, outputSuffix(string(output)))
}

func artifactIgnored(repoRoot, artifact string) (bool, error) {
	output, err := gitenv.CombinedOutput(repoRoot, "check-ignore", artifact)
	if err == nil {
		return true, nil
	}
	if gitExitCode(err, 1) {
		return false, nil
	}
	return false, fmt.Errorf("inspect repo-root %s ignore state: %w%s", artifact, err, outputSuffix(string(output)))
}

func ensureRepoPrivateExclude(repoRoot, entry string) error {
	stacklitArtifactExcludeMu.Lock()
	defer stacklitArtifactExcludeMu.Unlock()

	gitDir, err := resolveGitDir(repoRoot)
	if err != nil {
		return err
	}
	excludePath := filepath.Join(gitDir, "info", "exclude")
	return appendPrivateExcludeEntry(excludePath, entry)
}

func resolveGitDir(repoRoot string) (string, error) {
	output, err := gitenv.Output(repoRoot, "rev-parse", "--git-dir")
	if err != nil {
		return "", fmt.Errorf("resolve repository gitdir: %w%s", err, outputSuffix(string(output)))
	}
	gitDir := strings.TrimSpace(string(output))
	if gitDir == "" {
		return "", fmt.Errorf("resolve repository gitdir: git returned an empty path")
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(repoRoot, gitDir)
	}
	return filepath.Clean(gitDir), nil
}

func appendPrivateExcludeEntry(excludePath, entry string) error {
	if err := os.MkdirAll(filepath.Dir(excludePath), 0755); err != nil {
		return fmt.Errorf("create private exclude directory: %w", err)
	}

	content, err := os.ReadFile(excludePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read private exclude: %w", err)
	}

	for _, line := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(line) == entry {
			return nil
		}
	}

	next := append([]byte(nil), content...)
	if len(next) > 0 && next[len(next)-1] != '\n' {
		next = append(next, '\n')
	}
	next = append(next, entry...)
	next = append(next, '\n')

	if err := os.WriteFile(excludePath, next, 0644); err != nil {
		return fmt.Errorf("write private exclude: %w", err)
	}
	return nil
}

func gitUnmatchedPath(err error, output []byte) bool {
	return gitExitCode(err, 1) && strings.Contains(string(output), "did not match any file")
}

func gitExitCode(err error, code int) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == code
}
