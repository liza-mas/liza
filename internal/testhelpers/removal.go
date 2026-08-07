package testhelpers

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// removalBlockSentinel is the file PreventRemoval keeps open on Windows. The
// name is visible in test failures, so it says what it is for.
const removalBlockSentinel = ".removal-blocked"

// PreventRemoval makes a recursive delete of dir fail, and restores removability
// on cleanup. Use it for tests that exercise what happens when a worktree or a
// directory cannot be deleted.
//
// The two platforms block removal by different means because they disagree on
// who owns the right to delete a file. On POSIX it belongs to the containing
// directory, so dropping its write bit is enough. On Windows the file carries
// the right itself, and an unprivileged process that owns the tree cannot deny
// itself that right: an explicit deny of DELETE and FILE_DELETE_CHILD, with and
// without inheritance, was measured to leave os.RemoveAll succeeding. What
// Windows does enforce is sharing — a file with an open handle cannot be
// unlinked — so that is what this uses. The observable outcome is the same on
// both: deleting dir fails while dir exists.
func PreventRemoval(t *testing.T, dir string) {
	t.Helper()

	sentinel := filepath.Join(dir, removalBlockSentinel)
	if err := os.WriteFile(sentinel, []byte("blocked"), 0o644); err != nil {
		t.Fatalf("prevent removal of %s: write sentinel: %v", dir, err)
	}

	if runtime.GOOS == "windows" {
		handle, err := os.Open(sentinel)
		if err != nil {
			t.Fatalf("prevent removal of %s: open sentinel: %v", dir, err)
		}
		t.Cleanup(func() {
			if err := handle.Close(); err != nil {
				t.Errorf("release removal block on %s: %v", dir, err)
			}
		})
		return
	}

	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("prevent removal of %s: chmod: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(dir, 0o755); err != nil {
			t.Errorf("release removal block on %s: %v", dir, err)
		}
	})
}
