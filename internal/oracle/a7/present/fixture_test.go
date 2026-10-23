package present

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func materialiseFixture(t *testing.T, e *environment) string {
	t.Helper()
	source := filepath.Join("testdata", "fixtures", "project")
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(e.project, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal("materialise committed fixture project")
	}
	fixtureGit(t, e, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(e.project, ".git", "info", "exclude"), []byte("/.liza/\n"), 0o644); err != nil {
		t.Fatal("exclude fixture runtime directory")
	}
	fixtureGit(t, e, "add", "spec.md", "README.md")
	fixtureGit(t, e, "commit", "-m", "fixture: freeze presentation project")
	fixtureGit(t, e, "branch", "integration")
	return e.project
}

func fixtureGit(t *testing.T, e *environment, args ...string) {
	t.Helper()
	cmd := exec.Command(filepath.Join(e.bin, "git"), args...)
	cmd.Dir, cmd.Env = e.project, e.vars
	if err := cmd.Run(); err != nil {
		t.Fatalf("fixture git %s failed", args[0])
	}
}

func fixtureLines(t *testing.T, name string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "fixtures", name))
	if err != nil {
		t.Fatalf("read fixture %s", name)
	}
	return nonemptyLines(string(data))
}

func TestFixtureStateValidates(t *testing.T) {
	skipUnlessLinux(t)
	e := newEnvironment(t)
	result := runCLI(t, e, materialiseFixture(t, e), nil, "validate")
	if result.exit != 0 {
		t.Fatalf("fixture validate exited %d:\n%s\n%s", result.exit, e.norm.Text(string(result.stdout)), e.norm.Text(string(result.stderr)))
	}
}
