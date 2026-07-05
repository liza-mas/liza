package worktreeexclude

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/liza-mas/liza/internal/gitenv"
)

var privateExcludeMu sync.Mutex

const (
	// gitConfigLockRetries bounds how many times a shared-config write is
	// retried while a concurrent git process holds the config lock.
	gitConfigLockRetries = 12
	// gitConfigLockBackoff is the base backoff between config-lock retries; the
	// effective delay grows linearly per attempt.
	gitConfigLockBackoff = 25 * time.Millisecond
)

// EnsurePrivateExclude makes the linked worktree's private info/exclude file
// the worktree-specific core.excludesFile and appends missing relative entries
// without duplicating existing lines.
func EnsurePrivateExclude(worktreeRoot string, entries ...string) error {
	normalizedEntries, err := normalizeEntries(entries)
	if err != nil {
		return err
	}

	privateExcludeMu.Lock()
	defer privateExcludeMu.Unlock()

	root, gitDir, err := resolveWorktree(worktreeRoot)
	if err != nil {
		return err
	}
	excludePath := filepath.Join(gitDir, "info", "exclude")

	if err := ensureWorktreeExcludeConfig(root, excludePath); err != nil {
		return err
	}
	if err := appendMissingEntries(excludePath, normalizedEntries); err != nil {
		return err
	}
	if err := setWorktreeExcludeConfig(root, excludePath); err != nil {
		return err
	}
	return nil
}

func normalizeEntries(entries []string) ([]string, error) {
	seen := make(map[string]struct{}, len(entries))
	normalized := make([]string, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			return nil, errors.New("worktree private exclude entry is empty")
		}
		if filepath.IsAbs(entry) {
			return nil, fmt.Errorf("worktree private exclude entry %q must be relative", entry)
		}
		if _, ok := seen[entry]; ok {
			continue
		}
		seen[entry] = struct{}{}
		normalized = append(normalized, entry)
	}
	return normalized, nil
}

func resolveWorktree(worktreeRoot string) (string, string, error) {
	root, err := filepath.Abs(worktreeRoot)
	if err != nil {
		return "", "", fmt.Errorf("resolve worktree root: %w", err)
	}

	output, err := gitenv.Output(root, "rev-parse", "--git-dir")
	if err != nil {
		return "", "", fmt.Errorf("resolve worktree gitdir: %w", err)
	}
	gitDir := strings.TrimSpace(string(output))
	if gitDir == "" {
		return "", "", errors.New("resolve worktree gitdir: git rev-parse --git-dir returned empty path")
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(root, gitDir)
	}
	return root, filepath.Clean(gitDir), nil
}

func ensureWorktreeExcludeConfig(worktreeRoot, excludePath string) error {
	if err := ensureNoEffectiveExcludeConflict(worktreeRoot, excludePath); err != nil {
		return err
	}

	output, err := gitConfigWrite(worktreeRoot, "config", "extensions.worktreeConfig", "true")
	if err != nil {
		return fmt.Errorf("enable worktree config for private exclude: %w%s", err, outputSuffix(string(output)))
	}

	output, err = gitenv.Output(worktreeRoot, "config", "--worktree", "--get", "core.excludesFile")
	if err == nil {
		current := strings.TrimSpace(string(output))
		if current != "" && filepath.Clean(current) != filepath.Clean(excludePath) {
			return fmt.Errorf("worktree core.excludesFile already configured as %q, want %q", current, excludePath)
		}
		return nil
	}
	if !gitConfigUnset(err) {
		return fmt.Errorf("inspect worktree core.excludesFile: %w", err)
	}
	return nil
}

func ensureNoEffectiveExcludeConflict(worktreeRoot, excludePath string) error {
	output, err := gitenv.Output(worktreeRoot, "config", "--get", "core.excludesFile")
	if err == nil {
		current := strings.TrimSpace(string(output))
		if current != "" && filepath.Clean(current) != filepath.Clean(excludePath) {
			return fmt.Errorf("effective core.excludesFile already configured as %q, want %q", current, excludePath)
		}
		return nil
	}
	if !gitConfigUnset(err) {
		return fmt.Errorf("inspect effective core.excludesFile: %w", err)
	}
	return nil
}

func appendMissingEntries(excludePath string, entries []string) error {
	if err := os.MkdirAll(filepath.Dir(excludePath), 0o755); err != nil {
		return fmt.Errorf("create worktree private exclude directory: %w", err)
	}

	content, err := os.ReadFile(excludePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read worktree private exclude: %w", err)
	}

	existing := make(map[string]struct{})
	for _, line := range strings.Split(string(content), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			existing[trimmed] = struct{}{}
		}
	}

	next := append([]byte(nil), content...)
	for _, entry := range entries {
		if _, ok := existing[entry]; ok {
			continue
		}
		if len(next) > 0 && next[len(next)-1] != '\n' {
			next = append(next, '\n')
		}
		next = append(next, entry...)
		next = append(next, '\n')
		existing[entry] = struct{}{}
	}

	if err := os.WriteFile(excludePath, next, 0o644); err != nil {
		return fmt.Errorf("write worktree private exclude: %w", err)
	}
	return nil
}

func setWorktreeExcludeConfig(worktreeRoot, excludePath string) error {
	output, err := gitConfigWrite(worktreeRoot, "config", "--worktree", "core.excludesFile", excludePath)
	if err != nil {
		return fmt.Errorf("configure worktree private exclude: %w%s", err, outputSuffix(string(output)))
	}
	return nil
}

// gitConfigWrite runs a git config write that mutates a lock-guarded config file
// and retries when git cannot acquire the lock because a concurrent git process
// (e.g. `git worktree add` on the shared repository) holds it. Git writes config
// through a `<file>.lock` sentinel and fails fast with "could not lock config
// file" instead of waiting, so concurrent worktree setup must retry itself.
func gitConfigWrite(worktreeRoot string, args ...string) ([]byte, error) {
	var output []byte
	var err error
	for attempt := 0; attempt < gitConfigLockRetries; attempt++ {
		output, err = gitenv.CombinedOutput(worktreeRoot, args...)
		if err == nil || !isConfigLockContention(output) {
			return output, err
		}
		time.Sleep(gitConfigLockBackoff * time.Duration(attempt+1))
	}
	return output, err
}

func isConfigLockContention(output []byte) bool {
	return strings.Contains(string(output), "could not lock config file")
}

func gitConfigUnset(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == 1
}

func outputSuffix(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}
	return ": " + output
}
