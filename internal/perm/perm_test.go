package perm

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDenyWritesBlocksFileCreation is the reason this package exists: a helper
// that silently fails to deny anything would turn every write-failure test into
// a success-path test that still reports green.
func TestDenyWritesBlocksFileCreation(t *testing.T) {
	requireUnprivileged(t)

	dir := t.TempDir()
	restore, err := DenyWrites(dir)
	if err != nil {
		t.Fatalf("DenyWrites: %v", err)
	}
	t.Cleanup(func() {
		if err := restore(); err != nil {
			t.Errorf("restore: %v", err)
		}
	})

	if err := os.WriteFile(filepath.Join(dir, "probe"), []byte("x"), 0o644); err == nil {
		t.Fatal("creating a file in a write-denied directory should fail")
	}
}

// TestDenyWritesLeavesExistingFilesWritable pins the POSIX semantics the
// Windows implementation stands in for. chmod 0555 on a directory stops entries
// being added or removed but says nothing about the files already inside, and
// callers depend on that: they pre-create a lock file precisely so it can still
// be opened for writing once the directory is closed.
func TestDenyWritesLeavesExistingFilesWritable(t *testing.T) {
	requireUnprivileged(t)

	dir := t.TempDir()
	existing := filepath.Join(dir, "existing")
	if err := os.WriteFile(existing, []byte("before"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	restore, err := DenyWrites(dir)
	if err != nil {
		t.Fatalf("DenyWrites: %v", err)
	}
	t.Cleanup(func() {
		if err := restore(); err != nil {
			t.Errorf("restore: %v", err)
		}
	})

	f, err := os.OpenFile(existing, os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("opening an existing file for writing should still succeed: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestRestoreAllowsWritesAgain(t *testing.T) {
	requireUnprivileged(t)

	dir := t.TempDir()
	restore, err := DenyWrites(dir)
	if err != nil {
		t.Fatalf("DenyWrites: %v", err)
	}
	if err := restore(); err != nil {
		t.Fatalf("restore: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "probe"), []byte("x"), 0o644); err != nil {
		t.Fatalf("writing after restore should succeed: %v", err)
	}
}

// requireUnprivileged skips when the process bypasses permission checks
// entirely, which no amount of denying can prevent.
func requireUnprivileged(t *testing.T) {
	t.Helper()
	if os.Getuid() == 0 {
		t.Skip("running as root: permission checks are bypassed")
	}
}
