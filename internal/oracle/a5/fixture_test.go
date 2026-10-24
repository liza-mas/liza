package a5

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

const fixtureDate = "2020-01-01T00:00:00Z"

type recipeStep struct {
	Label string            `json:"label"`
	Files map[string]string `json:"files"`
	Args  []string          `json:"git,omitempty"`
}

type baseInput struct {
	State  json.RawMessage `json:"state"`
	Recipe []recipeStep    `json:"recipe"`
}

type fixture struct {
	root, project, home, tmp string
	labels, captured         map[string]string
	started                  time.Time
}

func materialise(t *testing.T, c caseInput) *fixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{root: root, project: filepath.Join(root, "project"), home: filepath.Join(root, "home"), tmp: filepath.Join(root, "tmp"), labels: map[string]string{}, captured: map[string]string{}}
	for _, dir := range []string{f.project, f.home, f.tmp} {
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	var base baseInput
	if err := json.Unmarshal(read(t, "testdata/fixtures/base-"+c.Base+"/project.json"), &base); err != nil {
		t.Fatal(err)
	}
	f.git(t, f.project, "init", "-b", "integration")
	f.git(t, f.project, "config", "user.name", "Oracle")
	f.git(t, f.project, "config", "user.email", "oracle@example.invalid")
	write(t, filepath.Join(f.project, ".gitignore"), []byte(".liza/\n.worktrees/\n"))
	for _, r := range append(base.Recipe, c.RecipeExtra...) {
		for path, contents := range r.Files {
			write(t, filepath.Join(f.project, path), []byte(f.resolve(contents)))
		}
		if len(r.Args) > 0 {
			args := make([]string, len(r.Args))
			for i, arg := range r.Args {
				args[i] = f.resolve(arg)
			}
			f.git(t, f.project, args...)
		} else {
			paths := []string{".gitignore"}
			for path := range r.Files {
				paths = append(paths, path)
			}
			sort.Strings(paths)
			f.git(t, f.project, append([]string{"add"}, paths...)...)
			f.git(t, f.project, "commit", "--allow-empty", "-m", r.Label)
		}
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
	for _, delta := range c.RepositoryDelta {
		f.commitInWorktree(t, delta)
	}
	for _, name := range []string{"rca.json", "rca-invalid.json", "disposition.json"} {
		write(t, filepath.Join(f.project, name), read(t, "testdata/fixtures/inputs/"+name))
	}
	state := mergeFixture(t, []byte(f.resolve(string(base.State))), []byte(f.resolve(string(c.Delta))))
	writeFixtureState(t, f, state)
	return f
}

// commitInWorktree applies one repository delta to the task worktree: a labelled
// commit of its files, or the explicit git arguments it carries.
func (f *fixture) commitInWorktree(t testing.TB, delta recipeStep) {
	t.Helper()
	wt := filepath.Join(f.project, ".worktrees/task-1")
	for path, contents := range delta.Files {
		write(t, filepath.Join(wt, path), []byte(f.resolve(contents)))
	}
	args := make([]string, len(delta.Args))
	for i, arg := range delta.Args {
		args[i] = f.resolve(arg)
	}
	if len(args) > 0 {
		f.git(t, wt, args...)
	} else {
		var paths []string
		for path := range delta.Files {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		f.git(t, wt, append([]string{"add"}, paths...)...)
		f.git(t, wt, "commit", "-m", delta.Label)
	}
	if delta.Label != "" {
		f.labels[delta.Label] = f.git(t, wt, "rev-parse", "HEAD")
	}
}

func (f *fixture) resolve(s string) string {
	s = strings.ReplaceAll(s, "{{ROOT}}", f.project)
	for label, sha := range f.labels {
		s = strings.ReplaceAll(s, "{{commit:"+label+"}}", sha)
	}
	for label, value := range f.captured {
		s = strings.ReplaceAll(s, "{{capture:"+label+"}}", value)
	}
	return s
}

// remember stores the value a preparation step's Capture selects from its output.
func (f *fixture) remember(t testing.TB, s step, r oracle.Result) {
	t.Helper()
	if s.Capture == nil {
		return
	}
	match := regexp.MustCompile(s.Capture.Pattern).FindSubmatch(append(append([]byte{}, r.Stdout...), r.Stderr...))
	if len(match) != 2 {
		t.Fatalf("capture %s: pattern did not match the step output", s.Capture.Label)
	}
	f.captured[s.Capture.Label] = string(match[1])
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

// environment lists the explicit pairs a CLI step receives beyond ChildEnv's
// policy set: the Git identity and dates, and the acting agent's generation.
// Operator steps carry no agent identity or generation, legacy aliases included.
func (s step) environment() []string {
	env := gitEnvironment()
	if s.Operator {
		return env
	}
	generation := "generation-1"
	if s.StaleGeneration {
		generation = "generation-old"
	}
	return append(env, generationVariable()+"="+generation)
}

func (f *fixture) statePath() string {
	return filepath.Join(f.project, runtimeDirectory(), "state.yaml")
}

func (f *fixture) invoke(t *testing.T, s step, mode string) oracle.Result {
	t.Helper()
	f.started = time.Now()
	if s.Adapter == "worktree-commit" {
		f.commitInWorktree(t, *s.Commit)
		return oracle.Result{}
	}
	if s.Adapter != "" {
		adapterStep(t, f, s)
		return oracle.Result{}
	}
	args := make([]string, len(s.Args))
	for i, arg := range s.Args {
		args[i] = f.resolve(arg)
	}
	if !s.Operator {
		args = append(args, "--agent-id", s.Agent)
	}
	if mode == "json" {
		args = append(args, "--json")
	}
	return oracle.RunInProject(t, oracle.BuildCLI(t, "./cmd/liza"), args, f.project, f.envBase(), oracle.RunOptions{Env: s.environment()})
}
