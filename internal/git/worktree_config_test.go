package git

import (
	"os"
	"path/filepath"
	"testing"
)

// The extension is written when off and the shared config left in place when
// already on: git replaces the file on every write, and on Windows a concurrent
// reader can fail to open it meanwhile.
func TestEnableWorktreeConfigExtensionWritesOnlyWhenOff(t *testing.T) {
	t.Parallel()
	repoDir := setupTestRepo(t)
	g := New(repoDir)
	if err := g.EnableWorktreeConfigExtension(); err != nil {
		t.Fatalf("EnableWorktreeConfigExtension() error = %v", err)
	}
	if got, err := g.exec("config", "--bool", "--get", "extensions.worktreeConfig"); err != nil || got != "true" {
		t.Fatalf("extensions.worktreeConfig = %q, %v; want true", got, err)
	}
	config := filepath.Join(repoDir, ".git", "config")
	before, err := os.Stat(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.EnableWorktreeConfigExtension(); err != nil {
		t.Fatalf("second EnableWorktreeConfigExtension() error = %v", err)
	}
	after, err := os.Stat(config)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("the shared config was replaced although the extension was already on")
	}
}
