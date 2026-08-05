package testhelpers

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// AssertExecutableScript asserts that path holds a shell script the platform
// can actually run.
//
// Windows has no executable bit: os.Stat derives the mode from the read-only
// attribute (0666 or 0444), so Mode()&0111 is zero for every file there and the
// Unix assertion could never pass. The portable statement behind that bit is
// "this hook will run when git or the agent invokes it", which `bash -n`
// verifies by parsing the script without executing it — the same mechanism
// brandrender uses on generated hooks. On Windows that is the stronger check of
// the two: a script marked executable but syntactically broken fails it.
func AssertExecutableScript(t *testing.T, path string) {
	t.Helper()

	if runtime.GOOS == "windows" {
		// Git Bash treats backslashes as escapes, so hand it the path in
		// forward-slash form.
		out, err := exec.Command(ResolveBashForScripts(t), "-n", filepath.ToSlash(path)).CombinedOutput()
		if err != nil {
			t.Errorf("%s is not a runnable script: %v\n%s", path, err, out)
		}
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Errorf("stat %s: %v", path, err)
		return
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("%s is not executable: mode=%v", path, info.Mode())
	}
}

// AssertRegularFileMode asserts that path was created with unixPerm.
//
// Windows has no POSIX mode bits: os.Stat reports 0666 for a writable file and
// 0444 for one carrying the read-only attribute, so an exact comparison against
// unixPerm can never hold. What these assertions guard — that the file was
// created usable rather than locked down — maps to "not read-only", the only
// observable the platform exposes.
func AssertRegularFileMode(t *testing.T, path string, unixPerm os.FileMode) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Errorf("stat %s: %v", path, err)
		return
	}
	if runtime.GOOS == "windows" {
		if info.Mode().Perm()&0o200 == 0 {
			t.Errorf("%s is read-only: mode=%v", path, info.Mode().Perm())
		}
		return
	}
	if info.Mode().Perm() != unixPerm {
		t.Errorf("%s has wrong permissions: got %o, want %o", path, info.Mode().Perm(), unixPerm)
	}
}
