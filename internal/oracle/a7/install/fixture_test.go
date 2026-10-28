package install

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

type fixture struct {
	root, home, project, tmp string
	env                      []string
	git                      string
	commit                   string
	human                    strings.Builder
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{root: t.TempDir()}
	f.home = filepath.Join(f.root, "home")
	f.project = filepath.Join(f.root, "project")
	f.tmp = filepath.Join(f.root, "tmp")
	bin := filepath.Join(f.root, "bin")
	for _, dir := range []string{f.home, f.project, f.tmp, bin} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal("create disposable directory")
		}
	}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal("git executable unavailable")
	}
	f.git = filepath.Join(bin, "git")
	if err := os.Symlink(git, f.git); err != nil {
		t.Fatal("create bin/git symlink")
	}
	writeFile(t, filepath.Join(bin, "pre-commit"), []byte("#!/bin/sh\nexit 0\n"), 0o755)
	f.env, err = oracle.ChildEnv(oracle.EnvBase{Home: f.home, Path: bin, Tmp: f.tmp},
		"LIZA_PROVIDER_CATALOG_URL=http://catalog.invalid/", "LIZA_SKIP_AUTO_UPDATE=1",
		"GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=Oracle", "GIT_AUTHOR_EMAIL=oracle@example.invalid",
		"GIT_COMMITTER_NAME=Oracle", "GIT_COMMITTER_EMAIL=oracle@example.invalid",
		"GIT_AUTHOR_DATE=2000-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2000-01-01T00:00:00Z")
	if err != nil {
		t.Fatal("construct AC3 child environment")
	}
	return f
}

func writeFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal("create fixture parent directory")
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal("write fixture file")
	}
}

func copyFixture(t *testing.T, source, destination string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "fixtures", filepath.FromSlash(source)))
	if err != nil {
		t.Fatalf("read fixture %s", source)
	}
	writeFile(t, destination, data, 0o644)
}

func (f *fixture) gitRun(t *testing.T, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, f.git, args...)
	cmd.Dir, cmd.Env = f.project, f.env
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("fixture git %s failed", args[0])
	}
	return strings.TrimSpace(string(out))
}

func (f *fixture) materialiseRepo(t *testing.T, commitSpec bool) {
	t.Helper()
	copyFixture(t, "repo/specs/vision.md", filepath.Join(f.project, "specs", "vision.md"))
	copyFixture(t, "repo/.pre-commit-config.yaml", filepath.Join(f.project, ".pre-commit-config.yaml"))
	f.gitRun(t, "init", "-q", "-b", "main")
	f.gitRun(t, "add", ".pre-commit-config.yaml")
	if commitSpec {
		f.gitRun(t, "add", "specs/vision.md")
	}
	f.gitRun(t, "commit", "-q", "-m", "fixture")
	f.commit = f.gitRun(t, "rev-parse", "HEAD")
}

func (f *fixture) stateFixture(t *testing.T, name string) {
	t.Helper()
	copyFixture(t, "state/"+name, filepath.Join(f.project, ".liza", "state.yaml"))
	copyFixture(t, "pipeline.yaml", filepath.Join(f.project, ".liza", "pipeline.yaml"))
}
