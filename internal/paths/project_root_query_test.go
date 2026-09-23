package paths

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestGitToplevelAndCommonDir_OneQueryAndLineBreakFallback(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	parent := t.TempDir()
	names := []string{"plain repo"}
	if runtime.GOOS != "windows" {
		// A line break in the toplevel makes the combined answer ambiguous and
		// must take the one-query-per-process route.
		names = append(names, "line\nbreak")
	}
	for _, name := range names {
		repo := filepath.Join(parent, name)
		if err := os.MkdirAll(repo, 0o755); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
			t.Fatalf("git init: %v\n%s", err, out)
		}
		toplevel, commonDir, err := gitToplevelAndCommonDir(repo)
		if err != nil {
			t.Fatalf("%q: gitToplevelAndCommonDir() error = %v", name, err)
		}
		wantTop, _ := filepath.EvalSymlinks(repo)
		gotTop, _ := filepath.EvalSymlinks(toplevel)
		if gotTop != wantTop {
			t.Errorf("%q: toplevel = %q, want %q", name, toplevel, repo)
		}
		if commonDir != ".git" && filepath.Base(commonDir) != ".git" {
			t.Errorf("%q: common dir = %q, want .git", name, commonDir)
		}
	}

	if _, _, err := gitToplevelAndCommonDir(t.TempDir()); err == nil {
		t.Error("gitToplevelAndCommonDir(non-repo) error = nil")
	}
}
