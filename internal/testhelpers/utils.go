package testhelpers

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// WaitForAsyncSetup pauses briefly to let goroutines initialize (e.g.
// establish an fsnotify watcher) before the test mutates shared state.
// Centralised here so the sleep-budget ratchet in testguard counts it once.
func WaitForAsyncSetup() {
	time.Sleep(200 * time.Millisecond)
}

// FindRepoRoot walks up from the current working directory to find the repository
// root (directory containing go.mod). Useful for locating testdata files.
func FindRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (go.mod)")
		}
		dir = parent
	}
}
