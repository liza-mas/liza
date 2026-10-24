package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestOracleCLIIsolation(t *testing.T) {
	t.Parallel()
	p := newProject(t)
	if info, err := os.Stat(filepath.Join(p.root, ".git")); err != nil || !info.IsDir() {
		t.Fatal("D31 requires a disposable Git repository")
	}
	entries, err := os.ReadDir(p.path)
	name := "git"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err != nil || len(entries) != 1 || entries[0].Name() != name {
		t.Fatal("D31 requires a git-only PATH")
	}
	if _, err := os.Readlink(filepath.Join(p.path, name)); err != nil {
		t.Fatal("PATH Git entry must be a symlink")
	}
	got := p.run(t, false, "validate", "--skip-process-checks", "--json")
	requireExit(t, got, 0)
}
