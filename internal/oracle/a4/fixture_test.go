package a4

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

const d35Fixture = "superseded-downstream"

type fixture struct {
	root, home, tmp string
	commits         map[string]string
}

// gitStep is the optional committed-input recipe consumed before the state commit.
// Later row groups can register recipes in their own files without editing this builder.
type gitStep struct{ Kind, Label, Branch, Path, Source string }

var fixtureRecipes = map[string][]gitStep{}

// The amendment recipe starts before its plan file exists (Fixture Catalogue).
// A later driver registers that omission together with its git recipe.
var fixtureBaseOmissions = map[string][]string{}

func newFixture(t testing.TB, name string) *fixture {
	t.Helper()
	return newFixtureWithRecipe(t, name, fixtureRecipes[name])
}

func newFixtureWithRecipe(t testing.TB, name string, recipe []gitStep) *fixture {
	t.Helper()
	base := t.TempDir()
	f := &fixture{root: filepath.Join(base, "project"), home: filepath.Join(base, "home"), tmp: filepath.Join(base, "tmp"), commits: map[string]string{}}
	for _, dir := range []string{f.root, f.home, f.tmp} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	f.git(t, "init", "-b", "main")
	f.git(t, "config", "user.name", "Oracle")
	f.git(t, "config", "user.email", "oracle@example.invalid")
	copyTree(t, "testdata/fixtures/project", f.root)
	for _, path := range fixtureBaseOmissions[name] {
		if !filepath.IsLocal(path) {
			t.Fatal("fixture omission must be project-relative")
		}
		if err := os.Remove(filepath.Join(f.root, path)); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(f.root, ".liza", "pipeline.yaml"), readFile(t, "testdata/fixtures/pipeline.yaml"))
	f.git(t, "add", "specs", ".gitignore", ".liza/pipeline.yaml")
	f.git(t, "commit", "-m", "fixture base")
	f.commits["base"] = f.git(t, "rev-parse", "HEAD")
	f.git(t, "branch", "integration")
	for _, step := range recipe {
		switch step.Kind {
		case "branch":
			f.git(t, "checkout", "-b", step.Label)
		case "commit":
			writeFile(t, filepath.Join(f.root, step.Path), readFile(t, step.Source))
			f.git(t, "add", step.Path)
			f.git(t, "commit", "-m", step.Label)
			f.commits[step.Label] = f.git(t, "rev-parse", "HEAD")
		case "merge":
			f.git(t, "checkout", "main")
			f.git(t, "merge", "--no-ff", step.Branch, "-m", step.Label)
			f.commits[step.Label] = f.git(t, "rev-parse", "HEAD")
		default:
			t.Fatalf("unknown fixture recipe step %s", step.Kind)
		}
	}
	f.git(t, "checkout", "main")
	state, err := renderState(string(readFile(t, filepath.Join("testdata/fixtures/state", name+".yaml"))), f.commits)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, f.statePath(), []byte(state))
	archive := filepath.Join("testdata/fixtures/archive", name)
	if _, err := os.Stat(archive); err == nil {
		copyTree(t, archive, filepath.Join(f.root, ".liza/archive/objects"))
	}
	f.git(t, "add", ".liza")
	f.git(t, "commit", "-m", "fixture state")
	f.commits["state"] = f.git(t, "rev-parse", "HEAD")
	worktrees := regexp.MustCompile(`(?m)^\s*worktree: ['"]?(\.worktrees/[^\s'"]+)`)
	for _, match := range worktrees.FindAllStringSubmatch(state, -1) {
		path := match[1]
		f.git(t, "worktree", "add", "-b", "task/"+filepath.Base(path), path, f.commits["base"])
	}
	return f
}

func renderState(input string, commits map[string]string) (string, error) {
	for label, sha := range commits {
		input = strings.ReplaceAll(input, "{{COMMIT:"+label+"}}", sha)
	}
	if strings.Contains(input, "{{") {
		return "", fmt.Errorf("unfilled fixture token")
	}
	return input, nil
}

func (f *fixture) statePath() string { return filepath.Join(f.root, ".liza/state.yaml") }

func (f *fixture) git(t testing.TB, args ...string) string {
	t.Helper()
	env := f.environment(t, "op", "GIT_AUTHOR_DATE=2020-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2020-01-01T00:00:00Z")
	r := run(t, f.root, env, nil, "git", args...)
	if r.code != 0 {
		t.Fatalf("fixture git %s failed: %s", strings.Join(args, " "), r.stderr)
	}
	return strings.TrimSpace(string(r.stdout))
}

func readFile(t testing.TB, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func writeFile(t testing.TB, path string, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func copyTree(t testing.TB, source, target string) {
	t.Helper()
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		writeFile(t, filepath.Join(target, rel), readFile(t, path))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) harness() *oracle.Normaliser {
	n := oracle.NewNormaliser()
	n.Dir(f.root, "<FIXTURE>")
	n.Dir(f.home, oracle.TokenHome)
	n.Dir(f.tmp, oracle.TokenTmp)
	return n
}

func (f *fixture) normaliser() *a4Normaliser {
	n := newA4Normaliser(f.harness())
	n.dir(f.root, "<FIXTURE>")
	n.dir(f.home, oracle.TokenHome)
	n.dir(f.tmp, oracle.TokenTmp)
	labels := make([]string, 0, len(f.commits))
	for label := range f.commits {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	for _, label := range labels {
		n.shas[f.commits[label]] = "<SHA " + label + ">"
	}
	return n
}

func (f *fixture) input(t testing.TB, name string) string {
	t.Helper()
	path := filepath.Join(f.tmp, name)
	writeFile(t, path, readFile(t, filepath.Join("testdata/fixtures/inputs", name)))
	return path
}
