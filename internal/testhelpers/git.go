package testhelpers

import (
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
	cmd := gitenv.Command(args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\nOutput: %s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}
