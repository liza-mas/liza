package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

var childExtras = []string{
	"TZ=Etc/GMT-2", "LIZA_SKIP_AUTO_UPDATE=1", "LIZA_PROVIDER_CATALOG_URL=http://catalog.invalid/",
	"GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=Oracle", "GIT_AUTHOR_EMAIL=oracle@example.invalid",
	"GIT_AUTHOR_DATE=2020-01-01T00:00:00Z", "GIT_COMMITTER_NAME=Oracle",
	"GIT_COMMITTER_EMAIL=oracle@example.invalid", "GIT_COMMITTER_DATE=2020-01-01T00:00:00Z",
}

type project struct {
	root, dir, home, tmp, bin string
	env                       []string
	before                    []byte
	norm                      determinism
}

type invocation struct {
	code           int
	stdout, stderr string
}

func mustRead(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read fixture/project file %s: %v", filepath.Base(name), err)
	}
	return b
}

func mustWrite(t *testing.T, name string, b []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal("create fixture parent")
	}
	if err := os.WriteFile(name, b, mode); err != nil {
		t.Fatalf("write fixture/project file %s", filepath.Base(name))
	}
}

func newProject(t *testing.T, fixture string, agent, worktree, stub bool) *project {
	t.Helper()
	p := &project{root: t.TempDir()}
	p.dir, p.home, p.tmp, p.bin = filepath.Join(p.root, "project"), filepath.Join(p.root, "home"), filepath.Join(p.root, "tmp"), filepath.Join(p.root, "bin")
	for _, dir := range []string{p.dir, p.home, p.tmp, p.bin} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal("create disposable directory")
		}
	}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal("git prerequisite missing from parent PATH")
	}
	if err := os.Symlink(git, filepath.Join(p.bin, "git")); err != nil {
		t.Fatal("create git symlink")
	}
	if stub {
		mustWrite(t, filepath.Join(p.bin, "oracle-stub"), []byte("#!/bin/sh\necho \"oracle stub exit $1\"\nexit \"$1\"\n"), 0o755)
	}
	extras := append([]string(nil), childExtras...)
	if agent {
		extras = append(extras, "LIZA_AGENT_ID=coder-1", "LIZA_AGENT_GENERATION=gen-1")
	}
	p.env, err = oracle.ChildEnv(oracle.EnvBase{Home: p.home, Path: p.bin, Tmp: p.tmp}, extras...)
	if err != nil {
		t.Fatal(err)
	}
	p.git(t, "init", "-q", "-b", "main")
	mustWrite(t, filepath.Join(p.dir, "specs", "vision.md"), []byte("# Oracle vision\n"), 0o644)
	p.git(t, "add", "specs/vision.md")
	p.git(t, "commit", "-q", "-m", "fixture")
	p.git(t, "branch", "integration")
	p.norm = determinism{pid: os.Getpid(), fixtureSHA: strings.TrimSpace(p.git(t, "rev-parse", "HEAD"))}
	p.norm.paths = [][2]string{{p.dir, "<PROJECT>"}, {p.home, "<HOME>"}, {p.tmp, "<TMPDIR>"}, {p.root, "<CASE>"}}
	if resolved, err := filepath.EvalSymlinks(p.dir); err == nil {
		p.norm.paths = append(p.norm.paths, [2]string{resolved, "<PROJECT>"})
	}
	p.before = bytes.ReplaceAll(mustRead(t, filepath.Join("testdata", "fixtures", "state", fixture+".yaml")), []byte("<live-pid>"), []byte(strconv.Itoa(os.Getpid())))
	mustWrite(t, filepath.Join(p.dir, ".liza", "state.yaml"), p.before, 0o644)
	mustWrite(t, filepath.Join(p.dir, ".liza", "pipeline.yaml"), mustRead(t, "testdata/fixtures/pipeline.yaml"), 0o644)
	if worktree {
		p.git(t, "worktree", "add", "-q", ".worktrees/task-1", "-b", "task/task-1", "integration")
	}
	return p
}

func (p *project) command(bin string, args ...string) (*exec.Cmd, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir, cmd.Env = p.dir, p.env
	return cmd, cancel
}

func (p *project) git(t *testing.T, args ...string) string {
	t.Helper()
	cmd, cancel := p.command(filepath.Join(p.bin, "git"), args...)
	defer cancel()
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("fixture git %s failed", args[0])
	}
	return string(out)
}

func (p *project) run(t *testing.T, args ...string) invocation {
	t.Helper()
	cmd, cancel := p.command(oracle.BuildCLI(t, "./cmd/liza"), args...)
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	// Stdin nil supplies EOF, including cleanup's unreadable confirmation case.
	p.norm.start = time.Now()
	err := cmd.Run()
	res := invocation{stdout: stdout.String(), stderr: stderr.String()}
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatal("fixture CLI failed to start")
		}
		res.code = exit.ExitCode()
		if res.code < 0 {
			t.Fatal("fixture CLI terminated before completion")
		}
	}
	return res
}

func (p *project) normalise(t *testing.T, stream string, text []byte) []byte {
	t.Helper()
	out, err := p.norm.text(string(text))
	if err != nil {
		t.Fatalf("%s: %v", stream, err)
	}
	return []byte(out)
}

func (p *project) state(t *testing.T) []byte {
	t.Helper()
	return mustRead(t, filepath.Join(p.dir, ".liza", "state.yaml"))
}

func (p *project) human(t *testing.T, name string, res invocation) {
	t.Helper()
	out := fmt.Sprintf("exit %d\n[stdout]\n%s\n[stderr]\n%s\n", res.code, res.stdout, res.stderr)
	oracle.Check(t, oracle.Human, name+".txt", p.normalise(t, "human", []byte(out)))
}

type controlCase struct {
	name, fixture                                 string
	args                                          []string
	code                                          int
	json, state, agent, worktree, stub, unchanged bool
}

func runCases(t *testing.T, surface string, cases []controlCase) {
	t.Helper()
	linuxOnly(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var jsonAfter []byte
			modes := []bool{false}
			if tc.json {
				modes = []bool{true, false}
			}
			for _, jsonMode := range modes {
				p := newProject(t, tc.fixture, tc.agent, tc.worktree, tc.stub)
				args := append([]string(nil), tc.args...)
				if jsonMode {
					args = append(args, "--json")
				}
				res := p.run(t, args...)
				if res.code != tc.code {
					t.Fatalf("%s: exit %d, want %d; %s", tc.name, res.code, tc.code, p.normalise(t, "stderr", []byte(res.stderr)))
				}
				name := surface + "/" + tc.name
				if jsonMode {
					p.jsonGolden(t, name, res)
				} else {
					p.human(t, name, res)
				}
				if tc.worktree {
					oracle.Check(t, oracle.Artifact, name+".manifest", p.manifest(t))
					continue
				}
				after := p.state(t)
				if tc.unchanged && !bytes.Equal(p.before, after) {
					t.Fatal("admit changed state.yaml")
				}
				after = append(after, p.outcomes(t)...)
				normalAfter := p.normalise(t, "state after", after)
				if tc.state && (!tc.json || jsonMode) {
					oracle.CheckState(t, name+".yaml", p.normalise(t, "state before", p.before), normalAfter)
				}
				if jsonMode {
					jsonAfter = normalAfter
				}
				if tc.json && !jsonMode {
					assertParity(t, jsonAfter, normalAfter)
				}
			}
		})
	}
}

func assertParity(t *testing.T, jsonAfter, humanAfter []byte) {
	t.Helper()
	want, got := strings.Split(string(jsonAfter), "\n"), strings.Split(string(humanAfter), "\n")
	for i := 0; i < len(want) || i < len(got); i++ {
		if i >= len(want) || i >= len(got) || want[i] != got[i] {
			t.Fatalf("human/json state parity differs at line %d", i+1)
		}
	}
}

func (p *project) jsonGolden(t *testing.T, name string, res invocation) {
	t.Helper()
	stdout := p.normalise(t, "stdout", []byte(res.stdout))
	dec := json.NewDecoder(bytes.NewReader(stdout))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		t.Fatal("stdout is not a JSON document")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		t.Fatal("stdout is not exactly one JSON document")
	}
	stderr := strings.TrimSuffix(string(p.normalise(t, "stderr", []byte(res.stderr))), "\n")
	lines := []string{}
	if stderr != "" {
		lines = strings.Split(stderr, "\n")
	}
	out, err := json.Marshal(map[string]any{"exit": res.code, "stdout": doc, "stderr": lines})
	if err != nil {
		t.Fatal("marshal JSON golden wrapper")
	}
	oracle.Check(t, oracle.JSON, name+".json", out)
}

func (p *project) outcomes(t *testing.T) []byte {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(p.dir, ".liza", "lifecycle-metrics", "*.json"))
	if err != nil {
		t.Fatal("discover lifecycle outcome files")
	}
	counts := map[string]map[string]uint64{}
	for _, file := range files {
		var doc struct {
			Counts map[string]map[string]uint64 `json:"counts"`
		}
		if err := json.Unmarshal(mustRead(t, file), &doc); err != nil {
			t.Fatal("decode lifecycle outcome file")
		}
		for op, outcomes := range doc.Counts {
			if counts[op] == nil {
				counts[op] = map[string]uint64{}
			}
			for outcome, count := range outcomes {
				counts[op][outcome] += count
			}
		}
	}
	var lines []string
	for op, outcomes := range counts {
		for outcome, count := range outcomes {
			if count != 0 {
				lines = append(lines, fmt.Sprintf("%s %s %d", op, outcome, count))
			}
		}
	}
	sort.Strings(lines)
	return []byte("# lifecycle-outcomes\n" + strings.Join(lines, "\n") + "\n")
}
