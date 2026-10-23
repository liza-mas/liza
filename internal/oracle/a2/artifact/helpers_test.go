//go:build linux

package artifact

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

type fileEntry struct {
	Path    string
	Mode    fs.FileMode
	Content []byte
}

type envDirs struct{ home, tmp, bin, work string }

func isolateEnv(t *testing.T, withGit bool) envDirs {
	t.Helper()
	var git string
	if withGit {
		var err error
		git, err = exec.LookPath("git")
		if err != nil {
			t.Fatal("resolve fixture git executable")
		}
	}
	root := t.TempDir()
	e := envDirs{filepath.Join(root, "home"), filepath.Join(root, "tmp"), filepath.Join(root, "bin"), filepath.Join(root, "work")}
	for _, dir := range []string{e.home, e.tmp, e.bin, e.work} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal("create isolated directory")
		}
	}
	if withGit {
		if err := os.Symlink(git, filepath.Join(e.bin, "git")); err != nil {
			t.Fatal("link fixture git executable")
		}
	}
	for key, value := range map[string]string{"HOME": e.home, "TMPDIR": e.tmp, "PATH": e.bin, "GIT_CONFIG_NOSYSTEM": "1"} {
		t.Setenv(key, value)
	}
	for _, key := range []string{"SHELL", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY", "GIT_CONFIG", "GIT_CONFIG_GLOBAL"} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s", key)
		}
	}
	return e
}

func gitRun(t *testing.T, e envDirs, dir string, args ...string) {
	t.Helper()
	env, err := oracle.ChildEnv(oracle.EnvBase{Home: e.home, Path: e.bin, Tmp: e.tmp},
		"GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid",
		"GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid",
		"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatal("build fixture Git environment")
	}
	cmd := exec.Command(filepath.Join(e.bin, "git"), args...)
	cmd.Dir, cmd.Env = dir, env
	if err := cmd.Run(); err != nil {
		if cmd.ProcessState != nil {
			t.Fatalf("fixture git %s exited %d", args[0], cmd.ProcessState.ExitCode())
		}
		t.Fatalf("start fixture git %s", args[0])
	}
}

func initRepo(t *testing.T, e envDirs) string {
	t.Helper()
	repo := filepath.Join(e.work, "repo")
	template := filepath.Join(e.work, "empty-template")
	for _, dir := range []string{repo, template} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal("create fixture repository directories")
		}
	}
	gitRun(t, e, repo, "init", "-q", "--template="+template)
	return repo
}

func readFile(t *testing.T, root, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s", rel)
	}
	return b
}

func writeFile(t *testing.T, root, rel string, content []byte, mode fs.FileMode) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent of %s", rel)
	}
	if err := os.WriteFile(path, content, mode); err != nil {
		t.Fatalf("write %s", rel)
	}
}

func snapshot(t *testing.T, root string) []fileEntry {
	t.Helper()
	var entries []fileEntry
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		entries = append(entries, fileEntry{filepath.ToSlash(rel), info.Mode().Perm(), readFile(t, root, rel)})
		return nil
	})
	if err != nil {
		t.Fatal("snapshot disposable tree")
	}
	return entries
}

func manifest(entries []fileEntry, content bool) []byte {
	entries = append([]fileEntry(nil), entries...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	var b strings.Builder
	for _, entry := range entries {
		if content {
			fmt.Fprintf(&b, "%04o %d %x %s\n", entry.Mode.Perm(), len(entry.Content), sha256.Sum256(entry.Content), entry.Path)
		} else {
			fmt.Fprintf(&b, "%04o %s\n", entry.Mode.Perm(), entry.Path)
		}
	}
	return []byte(b.String())
}

// Preserve literal /tmp roots in Codex config; replace only registered paths.
func tokenise(b []byte, pairs map[string]string) []byte {
	tokens := make(map[string]string)
	for path, token := range pairs {
		tokens[path] = token
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			tokens[resolved] = token
		}
	}
	paths := make([]string, 0, len(tokens))
	for path := range tokens {
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i, j int) bool {
		if len(paths[i]) != len(paths[j]) {
			return len(paths[i]) > len(paths[j])
		}
		return paths[i] < paths[j]
	})
	for _, path := range paths {
		b = bytes.ReplaceAll(b, []byte(path), []byte(tokens[path]))
	}
	return b
}

func checkTree(t *testing.T, prefix string, entries []fileEntry, pairs map[string]string) {
	t.Helper()
	for _, entry := range entries {
		oracle.Check(t, oracle.Artifact, prefix+"/"+entry.Path, tokenise(entry.Content, pairs))
	}
}
