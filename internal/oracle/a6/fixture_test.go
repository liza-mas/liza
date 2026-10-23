package a6

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

type fixture struct {
	root, home, tmp string
	env             []string
	tokens          map[string]string
	norm            *oracle.Normaliser
	ids             identities
	release         func()
}

func baseFixture(t *testing.T) *fixture {
	t.Helper()
	platformGate(t)
	dir := t.TempDir()
	f := &fixture{root: filepath.Join(dir, "project"), home: filepath.Join(dir, "home"), tmp: filepath.Join(dir, "tmp"), tokens: make(map[string]string), norm: oracle.NewNormaliser()}
	for _, p := range []string{f.root, f.home, f.tmp} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	f.norm.Dir(f.root, oracle.TokenProject)
	f.norm.Dir(f.home, oracle.TokenHome)
	f.norm.Dir(f.tmp, oracle.TokenTmp)
	f.tokens["@ROOT@"] = f.root
	f.tokens["@WORKTREE@"] = filepath.Join(f.root, ".worktrees", "t1")
	f.env = f.envFor(t)
	f.git(t, "init", "--initial-branch=integration")
	f.git(t, "config", "user.name", "Oracle")
	f.git(t, "config", "user.email", "oracle@example.invalid")
	f.git(t, "config", "commit.gpgsign", "false")
	f.write(t, ".gitignore", ".liza/\n.worktrees/\nstacklit.json\n")
	f.write(t, "README.md", "# Oracle fixture\n")
	f.git(t, "add", "README.md", ".gitignore")
	f.git(t, "commit", "-m", "test: oracle baseline")
	f.tokens["@BASE@"] = f.git(t, "rev-parse", "HEAD")
	pipeline, err := os.ReadFile("testdata/fixtures/common/pipeline.yaml")
	if err != nil {
		t.Fatal(err)
	}
	f.write(t, ".liza/pipeline.yaml", string(pipeline))
	return f
}

func (f *fixture) envFor(t testing.TB, extras ...string) []string {
	t.Helper()
	pairs := []string{"TZ=Etc/GMT-2", "GIT_CONFIG_NOSYSTEM=1"}
	pairs = append(pairs, extras...)
	env, err := oracle.ChildEnv(oracle.EnvBase{Home: f.home, Path: os.Getenv("PATH"), Tmp: f.tmp}, pairs...)
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func (f *fixture) git(t *testing.T, args ...string) string {
	t.Helper()
	o := runProcess(t, f, "git", args, nil)
	if o.exit != 0 {
		t.Fatalf("fixture git %v: exit %d\n%s", args, o.exit, o.stderr)
	}
	return strings.TrimSpace(string(o.stdout))
}

func (f *fixture) write(t testing.TB, rel, content string) {
	t.Helper()
	p := filepath.Join(f.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) install(t *testing.T, rel string) {
	t.Helper()
	common, err := os.ReadFile("testdata/fixtures/common/state.yaml")
	if err != nil {
		t.Fatal(err)
	}
	delta, err := os.ReadFile(filepath.Join("testdata/fixtures", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	merged, err := fixtureState(common, delta)
	if err != nil {
		t.Fatal(err)
	}
	s := string(merged)
	// Stable names rather than map iteration determine known SHA identity.
	keys := make([]string, 0, len(f.tokens))
	for key := range f.tokens {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := f.tokens[key]
		s = strings.ReplaceAll(s, key, value)
		f.ids.register(value, strings.Trim(key, "@"))
	}
	f.write(t, ".liza/state.yaml", s)
}

func (f *fixture) state(t testing.TB) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.root, ".liza", "state.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func (f *fixture) preprocess(s string) string { return f.ids.text(s) }

// These guards prevent the production paths that launch detached children.
func assertIsolated(t *testing.T, f *fixture) {
	t.Helper()
	if err := fixtureHasNoDeferredCommands(f.state(t)); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{".liza/deferred-validation", ".git/hooks/liza-index.sh", ".git/hooks/omni-ee-index.sh", ".git/liza-index-refresh.log", ".git/omni-ee-index-refresh.log"} {
		if _, err := os.Stat(filepath.Join(f.root, rel)); !os.IsNotExist(err) {
			t.Fatalf("A6 isolation guard: unexpected %s", rel)
		}
	}
}

func TestA6ExplicitEnvironment(t *testing.T) {
	f := baseFixture(t)
	want := map[string]bool{"HOME": true, "PATH": true, "TMPDIR": true, "TZ": true, "GIT_CONFIG_NOSYSTEM": true}
	for _, pair := range f.env {
		name, _, _ := strings.Cut(pair, "=")
		if !want[name] {
			t.Fatalf("unexpected child environment name %s", name)
		}
		delete(want, name)
	}
	if len(want) != 0 {
		t.Fatal("child environment policy names missing")
	}
}
