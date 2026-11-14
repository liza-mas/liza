package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/preventionrecord/recordfixture"
)

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = recordfixture.Environ(t.TempDir())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", args[0], err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestMaterialize(t *testing.T) {
	root := t.TempDir()

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
			if got := strings.TrimSpace(stdout.String()); got != target {
				t.Errorf("%s: printed %q, want the target path", name, got)
			}
			if got := gitOut(t, target, "rev-parse", "--verify", "case/c18^{commit}"); len(got) != 40 {
				t.Errorf("%s: case/c18 resolves to %q", name, got)
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
		if code := run(nil, &stdout, &stderr); code == 0 || stderr.Len() == 0 {
			t.Errorf("exit %d, stderr %q", code, stderr.String())
		}
	})
}
