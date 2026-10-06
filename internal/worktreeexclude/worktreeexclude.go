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

	"github.com/liza-mas/liza/internal/filelock"
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

	alreadyConfigured, err := ensureWorktreeExcludeConfig(root, excludePath)
	if err != nil {
		return err
	}
	if err := appendMissingEntries(excludePath, normalizedEntries); err != nil {
		return err
	}
	// The worktree value was just read as exactly this path; rewriting it would
	// be a no-op that still takes the config lock and spawns git.
	if alreadyConfigured {
		return nil
	}
	if err := setWorktreeExcludeConfig(root, excludePath); err != nil {
		return err
	}
	return nil
}

// EnsureRepoExclude appends missing gitignore patterns to the repository's
// shared info/exclude, which Git reads natively in the main checkout and in
// every linked worktree. Unlike EnsurePrivateExclude it never touches Git
// config, so a user's core.excludesFile keeps applying. Patterns may be
// root-anchored ("/dir/"). Updates serialize across processes on the common
// exclude path so concurrent worktree setup cannot lose another task's rules.
func EnsureRepoExclude(repoRoot string, patterns ...string) error {
	trimmed := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		if strings.ContainsAny(pattern, "\r\n") {
			return fmt.Errorf("repository exclude pattern %q must be a single line", pattern)
		}
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			return errors.New("repository exclude pattern is empty")
		}
		trimmed = append(trimmed, pattern)
	}

	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	// --git-path resolves info/exclude to the common directory, which
	// --git-dir would miss from a linked worktree.
	output, err := gitenv.Output(root, "rev-parse", "--git-path", "info/exclude")
	if err != nil {
		return fmt.Errorf("resolve repository exclude path: %w", err)
	}
	excludePath := strings.TrimSpace(string(output))
	if excludePath == "" {
		return errors.New("resolve repository exclude path: git rev-parse --git-path returned empty path")
	}
	if !filepath.IsAbs(excludePath) {
		excludePath = filepath.Join(root, excludePath)
	}
	excludePath = filepath.Clean(excludePath)
	if err := os.MkdirAll(filepath.Dir(excludePath), 0o755); err != nil {
		return fmt.Errorf("create repository exclude directory: %w", err)
	}

	privateExcludeMu.Lock()
	defer privateExcludeMu.Unlock()
	return filelock.New(excludePath).WithLockOperation("repository-exclude", func() error {
		return appendMissingEntries(excludePath, trimmed)
	})
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

// ensureWorktreeExcludeConfig verifies no conflicting exclude file is
// configured and reports whether the worktree value already equals
// excludePath exactly, in which case the caller need not write it again.
func ensureWorktreeExcludeConfig(worktreeRoot, excludePath string) (bool, error) {
	if err := ensureNoEffectiveExcludeConflict(worktreeRoot, excludePath); err != nil {
		return false, err
	}

	output, err := gitConfigWrite(worktreeRoot, "config", "extensions.worktreeConfig", "true")
	if err != nil {
		return false, fmt.Errorf("enable worktree config for private exclude: %w%s", err, outputSuffix(string(output)))
	}

	output, err = gitenv.Output(worktreeRoot, "config", "--worktree", "--get", "core.excludesFile")
	if err == nil {
		current := strings.TrimSpace(string(output))
		if current != "" && filepath.Clean(current) != filepath.Clean(excludePath) {
			return false, fmt.Errorf("worktree core.excludesFile already configured as %s, want %s", quotePath(current), quotePath(excludePath))
		}
		return current == excludePath, nil
	}
	if !gitConfigUnset(err) {
		return false, fmt.Errorf("inspect worktree core.excludesFile: %w", err)
	}
	return false, nil
}

func ensureNoEffectiveExcludeConflict(worktreeRoot, excludePath string) error {
	output, err := gitenv.Output(worktreeRoot, "config", "--get", "core.excludesFile")
	if err == nil {
		current := strings.TrimSpace(string(output))
		if current != "" && filepath.Clean(current) != filepath.Clean(excludePath) {
			return fmt.Errorf("effective core.excludesFile already configured as %s, want %s", quotePath(current), quotePath(excludePath))
		}
		return nil
	}
	if !gitConfigUnset(err) {
		return fmt.Errorf("inspect effective core.excludesFile: %w", err)
	}
	return nil
}

// quotePath wraps a path in double quotes for human-facing error messages
// without escaping inner characters. fmt's %q verb backslash-escapes Windows
// paths (C:\Users -> C:\\Users), which makes error messages confusing and
// breaks substring assertions; this helper quotes literally.
func quotePath(path string) string {
	return `"` + path + `"`
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
