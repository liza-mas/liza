package testhelpers

import (
	"os"
	"path/filepath"
	"testing"
)

// TestPreventRemovalBlocksRecursiveDelete is the reason this helper exists: the
// mechanism differs per platform, so the promise has to be checked where it runs
// rather than assumed from the code.
func TestPreventRemovalBlocksRecursiveDelete(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "locked")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	PreventRemoval(t, dir)

	if err := os.RemoveAll(dir); err == nil {
		t.Fatal("RemoveAll succeeded, want failure while removal is blocked")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("blocked directory should still exist: %v", err)
	}
}
