package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/historyfixture"
)

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = historyfixture.Environ(t.TempDir())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", args[0], err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestMaterialize(t *testing.T) {
	root := t.TempDir()

	// The heads a direct Build yields; the materializer must match them.
	reference, err := historyfixture.Build(filepath.Join(t.TempDir(), "reference"))
	if err != nil {
		t.Fatal(err)
	}

	t.Run("absent and empty targets", func(t *testing.T) {
		empty := filepath.Join(root, "empty")
		if err := os.Mkdir(empty, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, target := range map[string]string{
			"absent": filepath.Join(root, "absent", "nested"),
			"empty":  empty,
		} {
			var stdout, stderr bytes.Buffer
			if code := run([]string{target}, &stdout, &stderr); code != 0 {
				t.Fatalf("%s: exit %d: %s", name, code, stderr.String())
			}
			want := filepath.Join(target, historyfixture.Upstream) + "\n" + filepath.Join(target, historyfixture.Downstream) + "\n"
			if stdout.String() != want {
				t.Errorf("%s: stdout %q, want exactly the two repository paths", name, stdout.String())
			}
			if stderr.Len() != 0 {
				t.Errorf("%s: stderr %q", name, stderr.String())
			}
			paths := strings.Fields(stdout.String())
			if len(paths) != 2 {
				continue
			}
			for i, wantHead := range []string{reference.Upstream.Head, reference.Downstream.Head} {
				if got := gitOut(t, paths[i], "rev-parse", "HEAD"); got != wantHead {
					t.Errorf("%s: head of %s is %q, want %q", name, filepath.Base(paths[i]), got, wantHead)
				}
			}
		}
	})

	t.Run("relative absent and empty targets", func(t *testing.T) {
		t.Chdir(t.TempDir())
		if err := os.Mkdir("empty", 0o755); err != nil {
			t.Fatal(err)
		}
		for name, target := range map[string]string{"absent": "fixture", "empty": "empty"} {
			var stdout, stderr bytes.Buffer
			if code := run([]string{target}, &stdout, &stderr); code != 0 {
				t.Fatalf("%s: exit %d: %s", name, code, stderr.String())
			}
			paths := strings.Fields(stdout.String())
			if len(paths) != 2 {
				t.Fatalf("%s: stdout %q, want two repository paths", name, stdout.String())
			}
			for i, wantHead := range []string{reference.Upstream.Head, reference.Downstream.Head} {
				if got := gitOut(t, paths[i], "rev-parse", "HEAD"); got != wantHead {
					t.Errorf("%s: head of %s is %q, want %q", name, filepath.Base(paths[i]), got, wantHead)
				}
			}
		}
	})

	t.Run("non-empty target is refused untouched", func(t *testing.T) {
		target := filepath.Join(root, "busy")
		if err := os.Mkdir(target, 0o755); err != nil {
			t.Fatal(err)
		}
		keep := filepath.Join(target, "keep.txt")
		if err := os.WriteFile(keep, []byte("mine\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		if code := run([]string{target}, &stdout, &stderr); code == 0 {
			t.Fatal("exit 0 for a non-empty directory")
		}
		if stdout.Len() != 0 || !strings.Contains(stderr.String(), "not empty") {
			t.Errorf("stdout %q, stderr %q", stdout.String(), stderr.String())
		}
		entries, err := os.ReadDir(target)
		if err != nil || len(entries) != 1 {
			t.Fatalf("target changed: %v, %d entries", err, len(entries))
		}
		if got, err := os.ReadFile(keep); err != nil || string(got) != "mine\n" {
			t.Errorf("existing file changed: %v, %q", err, got)
		}
	})

	t.Run("missing argument", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		if code := run(nil, &stdout, &stderr); code == 0 || stderr.Len() == 0 || stdout.Len() != 0 {
			t.Errorf("exit %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
		}
	})
}
