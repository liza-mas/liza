package a9

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

const fixedDate = "2020-01-01T00:00:00Z"

var scenarioExtras = []string{
	"TZ=Etc/GMT-2", "LIZA_SKIP_AUTO_UPDATE=1", "LIZA_PROVIDER_CATALOG_URL=http://catalog.invalid/",
	"GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=Oracle", "GIT_AUTHOR_EMAIL=oracle@example.invalid",
	"GIT_AUTHOR_DATE=" + fixedDate, "GIT_COMMITTER_NAME=Oracle", "GIT_COMMITTER_EMAIL=oracle@example.invalid", "GIT_COMMITTER_DATE=" + fixedDate,
}

type scenario struct {
	name, root, dir, home, tmp, bin, recordDir, cli, named string
	env                                                    []string
	before                                                 []byte
	beforeFiles                                            map[string][]byte
	beforeListing                                          []byte
	recipeSHAs                                             []string
}
type invocation struct {
	code           int
	stdout, stderr string
}
type runningAgent struct {
	cmd            *exec.Cmd
	cancel         context.CancelFunc
	stdout, stderr bytes.Buffer
	done           chan error
}
type gitRecipe struct {
	Files       map[string]string `json:"files"`
	Message     string            `json:"message"`
	ExpectedSHA string            `json:"expected_sha"`
}

func TestOracleA9Kit(t *testing.T) {
	unixOnly(t)
	t.Setenv("ORACLE_A9_UNRELATED_SENTINEL", "fixture-only")
	p := newScenario(t, "i01-success")
	wantNames := []string{"HOME", "PATH", "TMPDIR", "TZ", "LIZA_SKIP_AUTO_UPDATE", "LIZA_PROVIDER_CATALOG_URL", "GIT_CONFIG_NOSYSTEM", "GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_AUTHOR_DATE", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL", "GIT_COMMITTER_DATE"}
	sort.Strings(wantNames)
	if !reflect.DeepEqual(environmentNames(p.env), wantNames) {
		t.Fatalf("runner environment names: %v", environmentNames(p.env))
	}
	entries, err := os.ReadDir(p.home)
	if err != nil || len(entries) != 0 {
		t.Fatal("scenario HOME must begin empty")
	}
	if !bytes.Contains(p.before, []byte("heartbeat_interval: 60")) || !bytes.Contains(p.before, []byte("lease_duration: 1800")) {
		t.Fatal("common fixture changed default heartbeat/lease")
	}
	// Probe the actual wrapper, rather than a derived environment map.
	p.installStub(t, stubScript{Executables: map[string][]stubProbe{"claude": {}}, Calls: []stubAction{{}}})
	if r := p.execute(t, filepath.Join(p.bin, "claude")); r.code != 0 {
		t.Fatal("isolation stub failed")
	}
	for _, name := range p.records(t)[0].EnvNames {
		if name == "ORACLE_A9_UNRELATED_SENTINEL" {
			t.Fatal("stub inherited unrelated sentinel")
		}
	}
	var path string
	for _, entry := range p.env {
		if name, value, ok := strings.Cut(entry, "="); ok && name == "PATH" {
			path = value
		}
	}
	if path != p.bin || strings.Contains(path, string(os.PathListSeparator)) {
		t.Fatal("child PATH is not the scenario bin alone")
	}
	for _, name := range []string{"codex", "acpx", "gemini"} {
		if _, err := os.Stat(filepath.Join(p.bin, name)); !os.IsNotExist(err) {
			t.Fatalf("unexpected provider reachable: %s", name)
		}
	}
	shas := p.fixedSHAs(t)
	if !reflect.DeepEqual(shas, p.recipeSHAs) {
		t.Fatal("registered set differs from fixed recipe set")
	}
	n := p.harness(t, shas)
	sha := shas[0]
	other := "0" + sha[1:]
	if other == sha {
		other = "1" + sha[1:]
	}
	text := newA9Normaliser(shas).text("base_commit: " + sha + "\nunknown: " + other)
	if got := string(n.StateForm(nil, []byte(text))); !strings.Contains(got, sha) || strings.Contains(got, other) || !strings.Contains(got, "<V1>") {
		t.Fatal("fixed SHA StateForm isolation failed")
	}
	for _, kind := range []oracle.Kind{oracle.JSON, oracle.Human} {
		input := sha + " " + other
		if kind == oracle.JSON {
			input = fmt.Sprintf(`{"fixed":%q,"other":%q}`, sha, other)
		}
		got, err := n.Form(kind, []byte(newA9Normaliser(shas).text(input)))
		if err != nil || !bytes.Contains(got, []byte(sha)) || bytes.Contains(got, []byte(other)) {
			t.Fatal("fixed SHA Form isolation failed")
		}
	}
	var recipe gitRecipe
	if err := readJSON("testdata/fixtures/common/recipe.json", &recipe); err != nil {
		t.Fatal(err)
	}
	recipe.ExpectedSHA = other
	if err := verifyRecipeSHA(recipe, sha); err == nil || !strings.Contains(err.Error(), "recipe SHA mismatch") {
		t.Fatal("recipe mismatch accepted")
	}
}

func unixOnly(t testing.TB) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("A9: Linux baseline goldens use Unix process semantics, paths and executable PATH stubs; Windows remains covered by white-box tests")
	}
}

func newScenario(t *testing.T, name string) *scenario {
	t.Helper()
	unixOnly(t)
	p := &scenario{name: name, root: t.TempDir(), cli: oracle.BuildCLI(t, "./cmd/liza")}
	p.dir = filepath.Join(p.root, "project")
	p.home = filepath.Join(p.root, "home")
	p.tmp = filepath.Join(p.root, "tmp")
	p.bin = filepath.Join(p.root, "bin")
	p.recordDir = filepath.Join(p.root, "records")
	for _, dir := range []string{p.dir, p.home, p.tmp, p.bin, p.recordDir} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal("A9 setup: create scenario directory")
		}
	}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal("A9 setup: git prerequisite unavailable")
	}
	if err := os.Symlink(git, filepath.Join(p.bin, "git")); err != nil {
		t.Fatal("A9 setup: git symlink")
	}
	// Supervisors run under the configured binary name with "agent" as the first
	// argument, the identity `liza delete agent --force` verifies before signalling.
	// The link lives outside bin, so no CLI is reachable through the child PATH.
	if err := os.Mkdir(filepath.Join(p.root, "launch"), 0o755); err != nil {
		t.Fatal("A9 setup: create launch directory")
	}
	p.named = filepath.Join(p.root, "launch", "liza")
	if err := os.Symlink(p.cli, p.named); err != nil {
		t.Fatal("A9 setup: canonical-name link")
	}
	p.env, err = oracle.ChildEnv(oracle.EnvBase{Home: p.home, Path: p.bin, Tmp: p.tmp}, scenarioExtras...)
	if err != nil {
		t.Fatal(err)
	}
	var recipe gitRecipe
	if err := readJSON("testdata/fixtures/common/recipe.json", &recipe); err != nil {
		t.Fatal("A9 setup: recipe unavailable")
	}
	p.git(t, "init", "-q", "-b", "main")
	// Repository-local settings remove host Git defaults from the recipe.
	p.git(t, "config", "core.autocrlf", "false")
	p.git(t, "config", "commit.gpgsign", "false")
	names := make([]string, 0, len(recipe.Files))
	for name := range recipe.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		p.write(t, name, []byte(recipe.Files[name]))
	}
	p.git(t, append([]string{"add", "--"}, names...)...)
	p.git(t, "commit", "-q", "-m", recipe.Message)
	sha := strings.TrimSpace(p.git(t, "rev-parse", "HEAD"))
	if err := verifyRecipeSHA(recipe, sha); err != nil {
		t.Fatal(err)
	}
	p.recipeSHAs = []string{sha}
	p.git(t, "branch", "integration")
	p.write(t, ".liza/pipeline.yaml", mustRead(t, "testdata/fixtures/common/pipeline.yaml"))
	state := mustRead(t, "testdata/fixtures/common/state.yaml")
	if local, err := os.ReadFile(filepath.Join("testdata", "fixtures", name, "state.yaml")); err == nil {
		state = local
	} else if !os.IsNotExist(err) {
		t.Fatal("A9 setup: scenario fixture read")
	}
	state = bytes.ReplaceAll(state, []byte("<live-pid>"), []byte(fmt.Sprint(os.Getpid())))
	state = bytes.ReplaceAll(state, []byte("<base-sha>"), []byte(sha))
	p.write(t, ".liza/state.yaml", state)
	var script stubScript
	if err := readJSON(filepath.Join("testdata", "fixtures", name, "stub.json"), &script); err != nil {
		t.Fatal("A9 setup: scenario stub script unavailable")
	}
	p.installStub(t, script)
	p.before = p.state(t)
	p.beforeFiles = p.runtimeFiles(t)
	p.beforeListing = p.listing(t)
	return p
}

func verifyRecipeSHA(recipe gitRecipe, actual string) error {
	if recipe.ExpectedSHA != actual {
		return fmt.Errorf("A9 setup: recipe SHA mismatch: expected %s, got %s", recipe.ExpectedSHA, actual)
	}
	return nil
}
func mustRead(t testing.TB, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("A9 read %s: %v", filepath.Base(path), err)
	}
	return b
}
func (p *scenario) write(t testing.TB, path string, b []byte) {
	t.Helper()
	full := filepath.Join(p.dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal("A9 setup: file parent")
	}
	if err := os.WriteFile(full, b, 0o644); err != nil {
		t.Fatalf("A9 setup: write %s", path)
	}
}
func (p *scenario) read(t testing.TB, path string) []byte {
	t.Helper()
	return mustRead(t, filepath.Join(p.dir, path))
}
func (p *scenario) state(t testing.TB) []byte { t.Helper(); return p.read(t, ".liza/state.yaml") }
func (p *scenario) command(bin string, args ...string) (*exec.Cmd, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir, cmd.Env = p.dir, p.env
	return cmd, cancel
}
func (p *scenario) execute(t testing.TB, bin string, args ...string) invocation {
	t.Helper()
	cmd, cancel := p.command(bin, args...)
	defer cancel()
	var out, errout bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errout
	err := cmd.Run()
	code := 0
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatalf("A9 %s run: cannot start %s", p.name, filepath.Base(bin))
	}
	return invocation{code, out.String(), errout.String()}
}
func (p *scenario) run(t testing.TB, args ...string) invocation {
	t.Helper()
	return p.execute(t, p.cli, append([]string{"-C", p.dir}, args...)...)
}
func (p *scenario) git(t testing.TB, args ...string) string {
	t.Helper()
	r := p.execute(t, filepath.Join(p.bin, "git"), args...)
	if r.code != 0 {
		t.Fatalf("A9 %s setup: git %s exit %d: %s", p.name, args[0], r.code, r.stderr)
	}
	return r.stdout
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'" }
func (p *scenario) installStub(t testing.TB, script stubScript) {
	t.Helper()
	scriptPath := filepath.Join(p.recordDir, "script.json")
	if err := writeJSON(scriptPath, script); err != nil {
		t.Fatal("A9 setup: write stub script")
	}
	if err := writeJSON(filepath.Join(p.recordDir, "context.json"), stubContext{p.dir, p.cli, scriptPath}); err != nil {
		t.Fatal("A9 setup: write stub context")
	}
	testBin, err := os.Executable()
	if err != nil {
		t.Fatal("A9 setup: test executable")
	}
	for name := range script.Executables {
		wrapper := "#!/bin/sh\nexport ORACLE_A9_STUB=" + shellQuote(name) + "\nexport ORACLE_A9_RECORD=" + shellQuote(p.recordDir) + "\nexec " + shellQuote(testBin) + " \"$@\"\n"
		if err := os.WriteFile(filepath.Join(p.bin, name), []byte(wrapper), 0o755); err != nil {
			t.Fatal("A9 setup: provider wrapper")
		}
	}
}
func (p *scenario) records(t testing.TB) []stubRecord {
	t.Helper()
	paths, _ := filepath.Glob(filepath.Join(p.recordDir, "call-*.json"))
	sort.Strings(paths)
	records := make([]stubRecord, 0, len(paths))
	for _, path := range paths {
		var r stubRecord
		if err := readJSON(path, &r); err != nil {
			t.Fatal("A9 collect: invocation record")
		}
		records = append(records, r)
	}
	return records
}

func (p *scenario) start(t *testing.T, args ...string) *runningAgent {
	t.Helper()
	a := &runningAgent{done: make(chan error, 1)}
	a.cmd, a.cancel = p.command(p.named, args...)
	a.cmd.Stdout, a.cmd.Stderr = &a.stdout, &a.stderr
	if err := a.cmd.Start(); err != nil {
		t.Fatalf("A9 %s run: agent start", p.name)
	}
	go func() { a.done <- a.cmd.Wait(); close(a.done) }()
	t.Cleanup(func() { a.cancel(); <-a.done })
	return a
}
func (p *scenario) finish(t *testing.T, a *runningAgent, deadline time.Duration) invocation {
	t.Helper()
	timer := time.NewTimer(deadline)
	defer timer.Stop()
	select {
	case err := <-a.done:
		code := 0
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			code = exit.ExitCode()
		} else if err != nil {
			t.Fatalf("A9 %s run: agent wait", p.name)
		}
		return invocation{code, a.stdout.String(), a.stderr.String()}
	case <-timer.C:
		t.Fatalf("A9 %s run: agent exit deadline (%s)", p.name, deadline)
		return invocation{}
	}
}
func (p *scenario) await(t testing.TB, stage string, predicate func() bool) {
	t.Helper()
	timer := time.NewTimer(15 * time.Second)
	tick := time.NewTicker(10 * time.Millisecond)
	defer timer.Stop()
	defer tick.Stop()
	for {
		if predicate() {
			return
		}
		select {
		case <-timer.C:
			t.Fatalf("A9 %s %s: observation deadline; golden state/%s", p.name, stage, p.name)
		case <-tick.C:
		}
	}
}
