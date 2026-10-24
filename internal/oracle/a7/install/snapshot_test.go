package install

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func readOptional(t *testing.T, root, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
	if os.IsNotExist(err) {
		return []byte("<absent>\n")
	}
	if err != nil {
		t.Fatalf("read observation %s", name)
	}
	return data
}

func (f *fixture) manifest(t *testing.T, root string) map[string]string {
	t.Helper()
	entries := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		// WalkDir uses Lstat and never follows symlinks. Exclusions are exact
		// AC6 runtime names plus .git and the home-only cache subtree.
		if rel == ".git" || (root == f.home && rel == ".liza/cache") {
			return filepath.SkipDir
		}
		base := entry.Name()
		if strings.HasSuffix(base, ".lock") || strings.HasSuffix(base, ".lock.owner.json") || base == "log.yaml" || base == "alerts.log" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		kind, content := "directory", "-"
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			kind = "symlink"
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			content = f.pathOnly(target)
		case info.Mode().IsRegular():
			kind = "file"
			if root == f.project && rel == ".liza/state.yaml" {
				content = "state-golden" // D29: asserted separately, never hashed.
			} else {
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				content = fmt.Sprintf("%x", sha256.Sum256([]byte(f.pathOnly(string(data)))))
			}
		case !info.IsDir():
			return fmt.Errorf("unsupported entry %s", rel)
		}
		entries[rel] = fmt.Sprintf("%s %s %04o %s", rel, kind, info.Mode().Perm(), content)
		return nil
	})
	if err != nil {
		t.Fatal("enumerate installed-tree manifest")
	}
	return entries
}

func manifestDelta(before, after map[string]string) []byte {
	paths := map[string]bool{}
	for path := range before {
		paths[path] = true
	}
	for path := range after {
		paths[path] = true
	}
	var ordered []string
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	var out strings.Builder
	for _, path := range ordered {
		old, existed := before[path]
		current, exists := after[path]
		switch {
		case !exists:
			fmt.Fprintf(&out, "- %s\n", path)
		case !existed:
			fmt.Fprintf(&out, "+ %s\n", current)
		case old != current:
			fmt.Fprintf(&out, "~ %s\n", current)
		}
	}
	return []byte(out.String())
}

func (f *fixture) checkManifest(t *testing.T, name, root string, before map[string]string) {
	t.Helper()
	oracle.Check(t, oracle.Artifact, name, manifestDelta(before, f.manifest(t, root)))
}

func (f *fixture) gitSnapshot(t *testing.T) []byte {
	t.Helper()
	var lines []string
	refs := f.gitRun(t, "for-each-ref", "--format=%(refname) %(objectname) %(subject)")
	for _, line := range strings.Split(refs, "\n") {
		fields := strings.SplitN(line, " ", 3)
		if len(fields) != 3 {
			t.Fatal("fixture ref observation malformed")
		}
		name := "unnamed " + fields[2]
		if fields[1] == f.commit {
			name = "fixture"
		}
		lines = append(lines, "ref "+fields[0]+" "+name)
	}
	exclude := readOptional(t, f.project, ".git/info/exclude")
	for _, line := range strings.Split(string(exclude), "\n") {
		if line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, "exclude "+line)
		}
	}
	hooks, err := os.ReadDir(filepath.Join(f.project, ".git", "hooks"))
	if err != nil {
		t.Fatal("read fixture hooks")
	}
	for _, hook := range hooks {
		if strings.HasSuffix(hook.Name(), ".sample") {
			continue
		}
		info, err := hook.Info()
		if err != nil {
			t.Fatal("stat fixture hook")
		}
		lines = append(lines, fmt.Sprintf("hook %s %04o", hook.Name(), info.Mode().Perm()))
	}
	for _, line := range strings.Split(f.gitRun(t, "config", "--local", "--list"), "\n") {
		lines = append(lines, "config "+line)
	}
	sort.Strings(lines)
	return []byte(strings.Join(lines, "\n") + "\n")
}

func (f *fixture) checkActivations(t *testing.T, name string) {
	t.Helper()
	data := readOptional(t, f.project, ".git/liza-provider-activations.json")
	oracle.Check(t, oracle.Artifact, name+".activations", []byte(f.pathOnly(string(data))))
}
