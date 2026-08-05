package ops

import (
	"os/exec"
	"runtime"
	"testing"
)

// requirePosixShell skips the calling test when the host cannot run POSIX
// shell commands (touch, printf, redirections).
//
// Several post_worktree_cmd / semble-ignore tests configure shell commands
// that are only meaningful under sh. On Windows the production code falls back
// to `cmd /c` when sh is absent, but cmd cannot run `touch` or `printf`, so
// those tests would produce false failures. Git for Windows provides sh.exe;
// when it is on PATH the tests run normally on every platform.
func requirePosixShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" {
		return
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skipf("skipping POSIX-shell test on Windows without sh on PATH: %v", err)
	}
}
