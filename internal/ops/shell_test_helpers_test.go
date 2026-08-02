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

// skipOnWindowsChmodUnsupported skips a test whose mechanism depends on POSIX
// chmod semantics (e.g. making a directory read-only to force a write
// failure). Windows chmod does not map owner-write bits to ACLs that block Go
// file writes, so such tests cannot reproduce their precondition on Windows.
func skipOnWindowsChmodUnsupported(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("skipping chmod-based test on Windows: POSIX permission bits are not enforced for Go file writes")
	}
}

