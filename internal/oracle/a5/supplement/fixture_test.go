package supplement

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

const fixtureDate = "2020-01-01T00:00:00Z"

// deliveredFixtures holds the delivered A5 suite's committed base projects.
const deliveredFixtures = "../testdata/fixtures"

type baseInput struct {
	State  json.RawMessage `json:"state"`
	Recipe []recipeStep    `json:"recipe"`
}

type fixture struct {
	root, project, home, tmp string
	labels                   map[string]string
}

// materialise builds a disposable project: a Git repository from the base
// recipe plus the case's extra commits, one task worktree holding a candidate
// commit, and the merged state. Each mode runs on its own materialisation.
func materialise(t *testing.T, c caseInput) *fixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{root: root, project: filepath.Join(root, "project"), home: filepath.Join(root, "home"), tmp: filepath.Join(root, "tmp"), labels: map[string]string{}}
	for _, dir := range []string{f.project, f.home, f.tmp} {
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	var base baseInput
	if err := json.Unmarshal(read(t, deliveredFixtures+"/base-"+c.Base+"/project.json"), &base); err != nil {
		t.Fatal(err)
	}
	f.git(t, f.project, "init", "-b", "integration")
	f.git(t, f.project, "config", "user.name", "Oracle")
	f.git(t, f.project, "config", "user.email", "oracle@example.invalid")
	write(t, filepath.Join(f.project, ".gitignore"), []byte(".liza/\n.worktrees/\n"))
	for _, r := range append(base.Recipe, c.RecipeExtra...) {
		paths := []string{".gitignore"}
		for path, contents := range r.Files {
			write(t, filepath.Join(f.project, path), []byte(f.resolve(contents)))
			paths = append(paths, path)
		}
		sort.Strings(paths)
		f.git(t, f.project, append([]string{"add"}, paths...)...)
		if len(r.Remove) > 0 {
			f.git(t, f.project, append([]string{"rm", "-q", "--"}, r.Remove...)...)
		}
		f.git(t, f.project, "commit", "--allow-empty", "-m", r.Label)
		if r.Label != "" {
			f.labels[r.Label] = f.git(t, f.project, "rev-parse", "HEAD")
		}
	}
	wt := filepath.Join(f.project, ".worktrees/task-1")
	f.git(t, f.project, "worktree", "add", "-b", "task/task-1", wt, "integration")
	for _, path := range []string{"feature.go", "feature_test.go"} {
		write(t, filepath.Join(wt, path), []byte("package fixture\n"))
	}
	f.git(t, wt, "add", "feature.go", "feature_test.go")
	f.git(t, wt, "commit", "-m", "candidate")
	f.labels["candidate"] = f.git(t, wt, "rev-parse", "HEAD")
	state := mergeFixture(t, []byte(f.resolve(string(base.State))), []byte(f.resolve(string(c.Delta))))
	writeFixtureState(t, f, state)
	return f
}

// resolve substitutes the project root and the labelled commits.
func (f *fixture) resolve(s string) string {
	s = strings.ReplaceAll(s, "{{ROOT}}", f.project)
	for label, sha := range f.labels {
		s = strings.ReplaceAll(s, "{{commit:"+label+"}}", sha)
	}
	return s
}

func gitEnvironment() []string {
	return []string{"GIT_AUTHOR_NAME=Oracle", "GIT_AUTHOR_EMAIL=oracle@example.invalid", "GIT_COMMITTER_NAME=Oracle", "GIT_COMMITTER_EMAIL=oracle@example.invalid", "GIT_AUTHOR_DATE=" + fixtureDate, "GIT_COMMITTER_DATE=" + fixtureDate}
}

func (f *fixture) envBase() oracle.EnvBase {
	return oracle.EnvBase{Home: f.home, Path: os.Getenv("PATH"), Tmp: f.tmp}
}

func (f *fixture) git(t testing.TB, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	env, err := oracle.ChildEnv(f.envBase(), gitEnvironment()...)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Env = env
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fixture git %v failed: %v: %s", args, err, b)
	}
	return strings.TrimSpace(string(b))
}

func (f *fixture) statePath() string {
	return filepath.Join(f.project, runtimeDirectory(), "state.yaml")
}

// invoke runs the case's step: the CLI as the registered agent, or the adapter.
func (f *fixture) invoke(t *testing.T, s step, mode string) oracle.Result {
	t.Helper()
	if s.Adapter != "" {
		run, ok := adapters[s.Adapter]
		if !ok {
			t.Fatalf("unknown adapter %q", s.Adapter)
		}
		run(t, f, s)
		return oracle.Result{}
	}
	args := make([]string, len(s.Args))
	for i, arg := range s.Args {
		args[i] = f.resolve(arg)
	}
	args = append(args, "--agent-id", s.Agent)
	if mode == "json" {
		args = append(args, "--json")
	}
	env := append(gitEnvironment(), generationVariable()+"=generation-1")
	return oracle.RunInProject(t, oracle.BuildCLI(t, "./cmd/liza"), args, f.project, f.envBase(), oracle.RunOptions{Env: env})
}

func (f *fixture) normaliser() *oracle.Normaliser {
	n := oracle.NewNormaliser()
	n.Dir(f.root, "<ROOT>")
	for label, sha := range f.labels {
		n.Literal(sha, "<commit:"+label+">")
		n.Literal(sha[:7], "<commit:"+label+">")
	}
	return n
}
