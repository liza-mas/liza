package ops

import (
	"runtime"
	"testing"

	"github.com/liza-mas/liza/internal/gitbash"
)

// requirePosixShell skips the calling test when the host cannot run POSIX
// shell commands (touch, printf, redirections).
//
// Several post_worktree_cmd / semble-ignore tests configure shell commands
// that are only meaningful under a POSIX shell. Git Bash is the supported
// Windows implementation, so capability-gate the tests on the same resolver as
// production.
func requirePosixShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" {
		return
	}
	if _, err := gitbash.Resolve(); err != nil {
		t.Skipf("skipping POSIX-shell test on Windows without Git Bash: %v", err)
	}
}
