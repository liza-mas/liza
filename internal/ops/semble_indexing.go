package ops

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/liza-mas/liza/internal/gitenv"
	"github.com/liza-mas/liza/internal/semble"
	"github.com/liza-mas/liza/internal/worktreeexclude"
)

const (
	sembleWorktreeIgnoreFile         = ".sembleignore"
	maxSemblePreparationWarningBytes = 512
	maxSembleMissingPatternsListed   = 8
)

var prepareSembleWorktreeIgnoreMu sync.Mutex

// PrepareSembleWorktreeIgnore creates or updates the generated task-worktree
// .sembleignore file and hides generated untracked files through the shared
// repository exclude, preserving user excludesFile configuration.
func PrepareSembleWorktreeIgnore(worktreeDir string) []string {
	prepareSembleWorktreeIgnoreMu.Lock()
	defer prepareSembleWorktreeIgnoreMu.Unlock()

	warning := prepareSembleWorktreeIgnore(worktreeDir)
	if warning == "" {
		return nil
	}
	return []string{warning}
}

func prepareSembleWorktreeIgnore(worktreeDir string) string {
	tracked, err := trackedSembleIgnore(worktreeDir)
	if err != nil {
		return boundedSemblePreparationWarning(fmt.Sprintf("semble .sembleignore: inspect tracked status: %v", err))
	}

	ignorePath := filepath.Join(worktreeDir, sembleWorktreeIgnoreFile)
	if tracked {
		missing, err := missingSembleIgnorePatterns(ignorePath)
		if err != nil {
			return boundedSemblePreparationWarning(fmt.Sprintf("semble .sembleignore: inspect tracked file: %v", err))
		}
		if len(missing) > 0 {
			return trackedSembleIgnoreMissingWarning(missing)
		}
		return ""
	}

	if err := worktreeexclude.EnsureRepoExclude(worktreeDir, "/"+sembleWorktreeIgnoreFile); err != nil {
		return boundedSemblePreparationWarning(fmt.Sprintf("semble .sembleignore: ensure repository exclude: %v", err))
	}
	if err := ensureGeneratedSembleIgnore(ignorePath); err != nil {
		return boundedSemblePreparationWarning(fmt.Sprintf("semble .sembleignore: prepare generated file: %v", err))
	}
	return ""
}

func trackedSembleIgnore(worktreeDir string) (bool, error) {
	_, err := gitenv.Output(worktreeDir, "ls-files", "--error-unmatch", sembleWorktreeIgnoreFile)
	if err == nil {
		return true, nil
	}
	if gitExitCode(err) == 1 {
		return false, nil
	}
	return false, err
}

func ensureGeneratedSembleIgnore(ignorePath string) error {
	content, err := os.ReadFile(ignorePath)
	if errors.Is(err, os.ErrNotExist) {
		return writeGeneratedSembleIgnore(ignorePath)
	}
	if err != nil {
		return err
	}

	lines := nonEmptySembleIgnoreLines(string(content))
	patterns := semble.DefaultIgnorePatterns()
	if sembleIgnoreLinesEqual(lines, patterns) {
		return nil
	}
	if generatedSembleIgnorePrefix(lines) {
		return appendMissingGeneratedSembleIgnorePatterns(ignorePath, content, patterns[len(lines):])
	}
	return writeGeneratedSembleIgnore(ignorePath)
}

func writeGeneratedSembleIgnore(ignorePath string) error {
	return os.WriteFile(ignorePath, []byte(semble.GeneratedWorktreeIgnorePayload()), 0o644)
}

func appendMissingGeneratedSembleIgnorePatterns(ignorePath string, content []byte, missing []string) error {
	next := append([]byte(nil), content...)
	for _, pattern := range missing {
		if len(next) > 0 && next[len(next)-1] != '\n' {
			next = append(next, '\n')
		}
		next = append(next, pattern...)
		next = append(next, '\n')
	}
	return os.WriteFile(ignorePath, next, 0o644)
}

func missingSembleIgnorePatterns(ignorePath string) ([]string, error) {
	content, err := os.ReadFile(ignorePath)
	if err != nil {
		return nil, err
	}

	existing := make(map[string]struct{})
	for _, line := range nonEmptySembleIgnoreLines(string(content)) {
		existing[line] = struct{}{}
	}

	patterns := semble.DefaultIgnorePatterns()
	missing := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		if _, ok := existing[pattern]; !ok {
			missing = append(missing, pattern)
		}
	}
	return missing, nil
}

func generatedSembleIgnorePrefix(lines []string) bool {
	patterns := semble.DefaultIgnorePatterns()
	if len(lines) > len(patterns) {
		return false
	}
	for i, line := range lines {
		if line != patterns[i] {
			return false
		}
	}
	return true
}

func sembleIgnoreLinesEqual(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func nonEmptySembleIgnoreLines(content string) []string {
	lines := make([]string, 0)
	for _, line := range strings.Split(content, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	return lines
}

func trackedSembleIgnoreMissingWarning(missing []string) string {
	listed := missing
	suffix := ""
	if len(listed) > maxSembleMissingPatternsListed {
		listed = missing[:maxSembleMissingPatternsListed]
		suffix = fmt.Sprintf(" (+%d more)", len(missing)-len(listed))
	}
	return boundedSemblePreparationWarning(fmt.Sprintf(
		"semble .sembleignore: tracked .sembleignore missing required patterns: %s%s",
		strings.Join(listed, ", "),
		suffix,
	))
}

func boundedSemblePreparationWarning(message string) string {
	message = strings.TrimSpace(message)
	if len(message) <= maxSemblePreparationWarningBytes {
		return message
	}
	return message[:maxSemblePreparationWarningBytes-3] + "..."
}

func gitExitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}
