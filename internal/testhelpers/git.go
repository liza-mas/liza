package testhelpers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/gitenv"
)

// MustGit runs a git command and fails the test if it errors.
// Returns the trimmed output of the command.
// This is a shared helper used across multiple test files to reduce duplication.
//
// Example usage:
//
//	commitSHA := testhelpers.MustGit(t, repoDir, "rev-parse", "HEAD")
//	testhelpers.MustGit(t, repoDir, "add", "file.txt")
//	testhelpers.MustGit(t, repoDir, "commit", "-m", "message")
func MustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	output, err := gitenv.CombinedOutput(dir, args...)
	if err != nil {
		t.Fatalf("git %v failed: %v\nOutput: %s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

// IsolateGlobalGitConfig points GIT_CONFIG_GLOBAL at an empty file so the
// developer's ~/.gitconfig cannot leak into a test.
//
// os.DevNull looks like the obvious value, but Git for Windows cannot open
// "NUL" as a config file ("fatal: unable to access 'NUL': Invalid argument"),
// which fails every git call the test makes. An empty file works everywhere.
func IsolateGlobalGitConfig(t *testing.T) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("create empty global git config: %v", err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", path)
}
