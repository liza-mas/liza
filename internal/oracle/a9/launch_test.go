package a9

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

// Launch and spawn-gate rows I-22 to I-25 (R8, R12, R21). Prompt bytes belong to
// A8: the stub records the delivered prompt only as equality (D-9), and every
// invocation document is checked for prompt text before it is compared.

// launchCatalog is the built-in catalog at the starting commit (R8); the launch
// tests assert it against `providers list` so a tool added later fails loudly.
var launchCatalog = []string{
	"claude", "codex", "codex-acp", "copilot", "cursor", "cursor-acp", "devin", "devin-acp",
	"gemini", "kimi", "mistral", "opencode", "opencode-acp", "pi", "qwen", "qwen-acp",
}

// launchStateFixture is the claimable-task state shared by the I-22 and I-23
// scenarios; I-24 and I-25 use the common (task-less) state.
const launchStateFixture = "testdata/fixtures/i22-common/state.yaml"

// launchScenario is newScenario plus the process hygiene of decisions D43-D48:
// top-level, nonparallel tests own all children of the test process. The cleanup,
// registered after the kit's temporary-directory cleanup, runs first so the
// fixture state outlives it.
func launchScenario(t *testing.T, name string) *scenario {
	t.Helper()
	p := newScenario(t, name)
	own := newLaunchOwner(p.root)
	launchOwners.Store(p, own)
	t.Cleanup(func() { launchCleanup(t, p.name, own) })
	return p
}

// launchOwners maps a scenario to its process tracker.
var launchOwners launchRegistry

type launchRegistry struct{ m sync.Map }

func (r *launchRegistry) Store(p *scenario, o *launchOwner) { r.m.Store(p, o) }

// owner returns the tracker of a scenario made by launchScenario.
func (r *launchRegistry) owner(p *scenario) *launchOwner {
	o, ok := r.m.Load(p)
	if !ok {
		panic("A9 launch: scenario without a process tracker")
	}
	return o.(*launchOwner)
}

// launchRun runs the CLI like scenario.run, but registers the process with the
// scenario's tracker as soon as it starts and, if the run's deadline expires,
// kills the whole process tree rather than the leader alone.
func launchRun(t *testing.T, p *scenario, args ...string) invocation {
	t.Helper()
	cmd, cancel := p.command(p.cli, append([]string{"-C", p.dir}, args...)...)
	defer cancel()
	launchBind(cmd)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := launchOwners.owner(p).start(cmd); err != nil {
		t.Fatalf("A9 %s run: cannot start the CLI", p.name)
	}
	code := 0
	if err := cmd.Wait(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatalf("A9 %s run: CLI wait", p.name)
		}
		code = exit.ExitCode()
	}
	return invocation{code, out.String(), errOut.String()}
}

// launchBind makes a context deadline stop the command's whole process tree,
// enumerated while the leader is still alive.
func launchBind(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		launchKillTree(cmd.Process.Pid)
		// D48: on Linux the subreaper owns even descendants whose leaders
		// exited before the tree snapshot. Reap those on the deadline path too.
		_, err := launchReapChildren(nil)
		return err
	}
	cmd.WaitDelay = 5 * time.Second
}

// launchState replaces the scenario's state with the shared claimable-task state.
func launchState(t *testing.T, p *scenario) {
	t.Helper()
	p.write(t, ".liza/state.yaml", mustRead(t, launchStateFixture))
	p.before = p.state(t)
}

// launchRuntime installs fixtures/<scenario>/runtime/* as pre-existing .liza
// files (signals, alerts) and refreshes the before-side runtime bundle.
func launchRuntime(t *testing.T, p *scenario) {
	t.Helper()
	dir := filepath.Join("testdata", "fixtures", p.name, "runtime")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatalf("A9 %s setup: runtime fixtures", p.name)
	}
	for _, entry := range entries {
		p.write(t, ".liza/"+entry.Name(), mustRead(t, filepath.Join(dir, entry.Name())))
	}
	p.beforeFiles = p.runtimeFiles(t)
	p.beforeListing = p.listing(t)
}

// launchHuman records exit code and non-log output of a run under the scenario's
// agent.txt golden, in the form scenario.check uses.
func launchHuman(t *testing.T, p *scenario, r invocation) {
	t.Helper()
	var human strings.Builder
	fmt.Fprintf(&human, "exit_code: %d\n[stdout]\n", r.code)
	for i, stream := range []string{r.stdout, r.stderr} {
		for _, line := range strings.Split(stream, "\n") {
			if strings.HasPrefix(line, "time=") && strings.Contains(line, " level=") {
				continue
			}
			human.WriteString(line + "\n")
		}
		if i == 0 {
			human.WriteString("[stderr]\n")
		}
	}
	shas := p.fixedSHAs(t)
	p.harness(t, shas).Check(t, oracle.Human, p.name+"/agent.txt", []byte(newA9Normaliser(shas).text(human.String())))
}

// launchUnchanged requires that the run mutated nothing and called no provider.
func launchUnchanged(t *testing.T, p *scenario, what string) {
	t.Helper()
	if !bytes.Equal(p.before, p.state(t)) || !bytes.Equal(p.beforeListing, p.listing(t)) || len(p.records(t)) != 0 {
		t.Fatalf("A9 %s: %s must leave state and listings unchanged and call no provider", p.name, what)
	}
}

// The launch rows run sequentially: each scenario writes and executes stub
// wrappers, and a concurrent fork elsewhere in the package can hold such a file
// open for writing (ETXTBSY) between its write and its first execution.
func TestOracleA9LaunchLines(t *testing.T) {
	unixOnly(t)
	launchRequireCatalog(t)
	for _, tool := range launchCatalog {
		t.Run(tool, func(t *testing.T) {
			p := launchScenario(t, "i23-"+tool)
			launchState(t, p)
			r := launchRun(t, p, "agent", "coder", "task-1", "--cli", tool, "--agent-id", "coder-1")
			requireCode(t, p, r, 0)
			var script stubScript
			if err := readJSON(filepath.Join("testdata", "fixtures", p.name, "stub.json"), &script); err != nil {
				t.Fatalf("A9 %s collect: stub script", p.name)
			}
			records := p.records(t)
			if len(records) != len(script.Calls) || len(records) == 0 {
				t.Fatalf("A9 %s collect: %d provider calls for %d scripted", p.name, len(records), len(script.Calls))
			}
			launchCheckInvocations(t, p, tool, records)
			p.check(t, r)
		})
	}
}

func TestOracleA9ExplainLaunch(t *testing.T) {
	unixOnly(t)
	for _, tool := range launchCatalog {
		t.Run(tool, func(t *testing.T) {
			p := launchScenario(t, "i24-"+tool)
			r := launchRun(t, p, "agent", "coder", "task-1", "--cli", tool, "--agent-id", "coder-1", "--explain-launch")
			requireCode(t, p, r, 0)
			if !strings.Contains(r.stdout, "tool: "+tool+"\n") {
				t.Fatalf("A9 %s run: explain output does not name the tool", p.name)
			}
			launchUnchanged(t, p, "--explain-launch")
			launchHuman(t, p, r)
		})
	}
}

// TestOracleA9LaunchRefused: an ACP tool without its runner is refused before
// registration. The underlying tool executable is reachable; only acpx is absent.
func TestOracleA9LaunchRefused(t *testing.T) {
	unixOnly(t)
	p := launchScenario(t, "i25-codex-acp")
	launchState(t, p)
	if _, err := os.Stat(filepath.Join(p.bin, "acpx")); !os.IsNotExist(err) {
		t.Fatalf("A9 %s setup: acpx must be absent from the scenario PATH", p.name)
	}
	if _, err := os.Stat(filepath.Join(p.bin, "codex")); err != nil {
		t.Fatalf("A9 %s setup: the underlying tool must be on the scenario PATH", p.name)
	}
	r := launchRun(t, p, "agent", "coder", "task-1", "--cli", "codex-acp", "--agent-id", "coder-1")
	requireCode(t, p, r, 1)
	if !strings.Contains(r.stderr, "codex-acp requires acpx on PATH") {
		t.Fatalf("A9 %s run: refusal does not name the missing runner", p.name)
	}
	launchUnchanged(t, p, "runner refusal")
	p.check(t, r)
}

// launchRequireCatalog pins the 16 tools the launch rows cover to the catalog the
// starting binary reports with an empty HOME.
func launchRequireCatalog(t *testing.T) {
	t.Helper()
	p := launchScenario(t, "i24-claude")
	r := launchRun(t, p, "providers", "list")
	requireCode(t, p, r, 0)
	var got []string
	for _, line := range strings.Split(strings.TrimSpace(r.stdout), "\n") {
		got = append(got, strings.Split(line, "\t")[0])
	}
	want := append([]string(nil), launchCatalog...)
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("A9 launch rows: catalog tools %v differ from covered tools %v", got, want)
	}
}

type launchInvocation struct {
	Executable      string   `json:"executable"`
	Arguments       []string `json:"arguments"`
	Directory       string   `json:"directory"`
	Stdin           string   `json:"stdin,omitempty"`
	PromptTransport string   `json:"prompt_transport"`
	PromptEqual     bool     `json:"prompt_equal"`
	ContainsPrompt  bool     `json:"contains_prompt,omitempty"`
}

// launchTransport names how the saved prompt reached the invocation, from the
// stub's redaction markers; "none" is any call that carried no prompt.
func launchTransport(r stubRecord) string {
	if r.Stdin == "<PROMPT>" {
		return "stdin"
	}
	for _, arg := range r.Args {
		if arg == "<PROMPT>" || arg == "<CONTAINS_PROMPT>" {
			return "argument"
		}
	}
	return "none"
}

// launchCheckInvocations compares the recorded invocation sequence with the
// i23 json golden after proving no prompt text was recorded.
func launchCheckInvocations(t *testing.T, p *scenario, tool string, records []stubRecord) {
	t.Helper()
	doc := struct {
		Tool        string             `json:"tool"`
		Invocations []launchInvocation `json:"invocations"`
	}{Tool: tool}
	delivered := 0
	for _, r := range records {
		doc.Invocations = append(doc.Invocations, launchInvocation{
			Executable: r.Executable, Arguments: r.Args, Directory: r.Directory, Stdin: r.Stdin,
			PromptTransport: launchTransport(r), PromptEqual: r.PromptEqual, ContainsPrompt: r.ContainsPrompt,
		})
		if r.PromptEqual && !r.ContainsPrompt {
			delivered++
		}
	}
	if delivered != 1 {
		t.Fatalf("A9 %s collect: %d invocations deliver the saved prompt unchanged, want 1", p.name, delivered)
	}
	var data bytes.Buffer
	enc := json.NewEncoder(&data)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		t.Fatalf("A9 %s collect: encode invocations", p.name)
	}
	launchRejectPrompt(t, p, data.Bytes())
	shas := p.fixedSHAs(t)
	n := p.harness(t, shas)
	n.Dir(filepath.Dir(p.cli), "<CLI>")
	n.Check(t, oracle.JSON, p.name+"/invocations.json", []byte(newA9Normaliser(shas).text(data.String())))
}

// launchRejectPrompt fails when any substantial line of the newest saved prompt
// appears in the document that is about to become a golden.
func launchRejectPrompt(t *testing.T, p *scenario, doc []byte) {
	t.Helper()
	paths, _ := filepath.Glob(filepath.Join(p.dir, ".liza", "agent-prompts", "*.txt"))
	sort.Strings(paths)
	if len(paths) == 0 {
		t.Fatalf("A9 %s collect: no saved prompt", p.name)
	}
	prompt := mustRead(t, paths[len(paths)-1])
	checked := 0
	for _, line := range strings.Split(string(prompt), "\n") {
		if line = strings.TrimSpace(line); len(line) >= 24 {
			checked++
			if bytes.Contains(doc, []byte(line)) {
				t.Fatalf("A9 %s collect: invocation document records prompt content", p.name)
			}
		}
	}
	if checked == 0 {
		t.Fatalf("A9 %s collect: saved prompt has no line to guard against", p.name)
	}
}

// Spawn gate (I-22). The adapter runs in a helper process: this test binary
// re-executed under oracle.ChildEnv plus one helper variable, so the supervisor
// the gate spawns inherits no host variable (it gains only the PWD Go appends
// for cmd.Dir, which is not asserted).

const (
	spawnHelperVar   = "ORACLE_A9_SPAWN_HELPER"
	spawnSentinelVar = "ORACLE_A9_SPAWN_SENTINEL"
	spawnLinePrefix  = "A9SPAWN "
	spawnNamePrefix  = "A9ENV "
)

type spawnRequest struct {
	Configured bool
	Project    string
	Role, CLI  string
	Args       []string
}

type spawnCase struct {
	name       string
	configured bool
	// refusal is the expected error text; empty means the gate passes.
	refusal string
	// alert is the category the refusal must append to alerts.log.
	alert string
}

// Each refusal is its own scenario, hence its own alerts.log document, so no
// document holds two run-written timestamps whose second relation depends on
// timing (D-4). The quota alert quotes the signal's expiry; its fixture
// alerts.log therefore opens with a line carrying that same instant, which
// numbers it before the run-written alert time and keeps both tokens fixed.
var spawnCases = []spawnCase{
	{"i22-quota-agent", false, "provider quota exhausted for claude; refusing to spawn coder", "PROVIDER QUOTA SPAWN BLOCKED"},
	{"i22-quota-configured", true, "provider quota exhausted for claude; refusing to spawn coder", "PROVIDER QUOTA SPAWN BLOCKED"},
	{"i22-unavailable-agent", false, "provider unavailable for claude; refusing to spawn coder", "PROVIDER UNAVAILABLE SPAWN BLOCKED"},
	{"i22-unavailable-configured", true, "provider unavailable for claude; refusing to spawn coder", "PROVIDER UNAVAILABLE SPAWN BLOCKED"},
	{"i22-pass-agent", false, "", ""},
	{"i22-pass-configured", true, "", ""},
}

// TestOracleA9SpawnGate is not parallel: it sets an unrelated sentinel variable
// in this process and proves it reaches neither the helper nor the stub.
func TestOracleA9SpawnGate(t *testing.T) {
	unixOnly(t)
	t.Setenv(spawnSentinelVar, "fixture-only")
	for _, c := range spawnCases {
		t.Run(c.name, func(t *testing.T) { spawnRun(t, c) })
	}
}

func spawnRun(t *testing.T, c spawnCase) {
	t.Helper()
	p := launchScenario(t, c.name)
	launchState(t, p)
	launchRuntime(t, p)
	// The gate spawns `liza` from the helper's PATH. It is on the scenario PATH
	// in every case, so a refusal that failed to hold would visibly spawn.
	if err := os.Symlink(p.cli, filepath.Join(p.bin, "liza")); err != nil {
		t.Fatalf("A9 %s setup: liza link", p.name)
	}
	req := spawnRequest{Configured: c.configured, Project: p.dir, Role: "coder", CLI: "claude", Args: []string{"task-1", "--agent-id", "coder-1"}}
	result, names, lines := spawnHelper(t, p, req)
	want := append(environmentNames(p.env), spawnHelperVar)
	sort.Strings(want)
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("A9 %s run: helper environment names %v, want %v", p.name, names, want)
	}
	for _, name := range names {
		if name == spawnSentinelVar {
			t.Fatalf("A9 %s run: helper inherited the unrelated sentinel", p.name)
		}
	}
	if c.refusal != "" {
		if !reflect.DeepEqual(lines, []string{"started: false", "error: " + c.refusal}) {
			t.Fatalf("A9 %s run: refusal lines %q", p.name, lines)
		}
		alerts := string(p.read(t, ".liza/alerts.log"))
		if !strings.Contains(alerts, c.alert) {
			t.Fatalf("A9 %s collect: no %s alert", p.name, c.alert)
		}
		launchUnchanged(t, p, "spawn refusal")
		p.check(t, result)
		return
	}
	if !reflect.DeepEqual(lines, []string{"started: true", "error: none", "exited: true"}) {
		t.Fatalf("A9 %s run: pass lines %q", p.name, lines)
	}
	records := p.records(t)
	if len(records) != 1 {
		t.Fatalf("A9 %s collect: %d provider calls, want 1", p.name, len(records))
	}
	for _, name := range records[0].EnvNames {
		if name == spawnSentinelVar {
			t.Fatalf("A9 %s collect: the stub inherited the unrelated sentinel", p.name)
		}
	}
	state := p.state(t)
	if !bytes.Contains(state, []byte("mode: STOPPED")) || bytes.Contains(state, []byte("coder-1:")) || !bytes.Contains(state, []byte("event: claim_released")) {
		t.Fatalf("A9 %s collect: the spawned supervisor did not run its claim to release and exit", p.name)
	}
	spawnCheckLogs(t, p)
	p.check(t, result)
}

// spawnHelper runs the helper process and returns its non-name output as a
// scenario result, the environment names it recorded and its result lines.
func spawnHelper(t *testing.T, p *scenario, req spawnRequest) (invocation, []string, []string) {
	t.Helper()
	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("A9 %s setup: helper request", p.name)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("A9 %s setup: test executable", p.name)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestOracleA9SpawnGateHelper$", "-test.count=1")
	cmd.Dir = p.dir
	cmd.Env = append(append([]string{}, p.env...), spawnHelperVar+"="+string(payload))
	launchBind(cmd)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := launchOwners.owner(p).start(cmd); err != nil {
		t.Fatalf("A9 %s run: cannot start the spawn helper", p.name)
	}
	code := 0
	if err := cmd.Wait(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatalf("A9 %s run: spawn helper wait", p.name)
		}
		code = exit.ExitCode()
	}
	var names, lines []string
	for _, line := range strings.Split(out.String(), "\n") {
		switch {
		case strings.HasPrefix(line, spawnNamePrefix):
			names = append(names, strings.TrimPrefix(line, spawnNamePrefix))
		case strings.HasPrefix(line, spawnLinePrefix):
			lines = append(lines, strings.TrimPrefix(line, spawnLinePrefix))
		}
	}
	if code != 0 {
		t.Fatalf("A9 %s run: spawn helper exit %d, result lines %q", p.name, code, lines)
	}
	return invocation{code, strings.Join(lines, "\n") + "\n", ""}, names, lines
}

// TestOracleA9SpawnGateHelper is the helper process of TestOracleA9SpawnGate; it
// does nothing unless its helper variable is set.
func TestOracleA9SpawnGateHelper(t *testing.T) {
	payload := os.Getenv(spawnHelperVar)
	if payload == "" {
		t.Skip("helper process of TestOracleA9SpawnGate")
	}
	var req spawnRequest
	if err := json.Unmarshal([]byte(payload), &req); err != nil {
		t.Fatal("A9 spawn helper: request")
	}
	for _, name := range environmentNames(os.Environ()) {
		fmt.Println(spawnNamePrefix + name)
	}
	if err := launchSubreaper(); err != nil {
		t.Fatal("A9 spawn helper: child subreaper unavailable")
	}
	t.Cleanup(func() { launchCleanup(t, "spawn helper", nil) })
	cmd, err := spawnAdapter(req.Configured, req.Project, req.Role, req.CLI, req.Args...)
	fmt.Printf("%sstarted: %t\n", spawnLinePrefix, cmd != nil)
	if err != nil {
		fmt.Println(spawnLinePrefix + "error: " + err.Error())
		return
	}
	fmt.Println(spawnLinePrefix + "error: none")
	// The gate waits on the supervisor itself, so the command's own Wait is spent:
	// a second Wait on it would race the gate's. A fresh handle on the child waits
	// for its exit instead, whichever of the two collects the status, and the
	// process is gone once signalling it fails.
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		if child, err := os.FindProcess(cmd.Process.Pid); err == nil {
			_, _ = child.Wait()
		}
	}()
	deadline := time.NewTimer(60 * time.Second)
	defer deadline.Stop()
	select {
	case <-exited:
	case <-deadline.C:
		// The supervisor is detached from this process; stop its whole tree
		// while the leader is alive, so no child is orphaned.
		launchKillTree(cmd.Process.Pid)
		t.Fatal("A9 spawn helper: supervisor exit deadline")
	}
	if cmd.Process.Signal(syscall.Signal(0)) == nil {
		t.Fatal("A9 spawn helper: supervisor still signalable after its exit")
	}
	fmt.Println(spawnLinePrefix + "exited: true")
}

var spawnLogRE = regexp.MustCompile(`^supervisor-coder-\d{8}-\d{6}\.\d{9}\.(stderr|stdout)\.log$`)
var spawnLogStampRE = regexp.MustCompile(`\d{8}-\d{6}\.\d{9}`)

// spawnCheckLogs proves the gate left exactly one stdout and one stderr
// supervisor log. Their names end in a nanosecond stamp that the kit's stamp rule
// does not cover, so they are recorded here, tokenised, and moved out of the
// agent-outputs listing that scenario.check compares.
func spawnCheckLogs(t *testing.T, p *scenario) {
	t.Helper()
	outputs := filepath.Join(p.dir, ".liza", "agent-outputs")
	entries, err := os.ReadDir(outputs)
	if err != nil {
		t.Fatalf("A9 %s collect: agent-outputs", p.name)
	}
	keep := filepath.Join(p.root, "supervisor-logs")
	if err := os.Mkdir(keep, 0o755); err != nil {
		t.Fatalf("A9 %s collect: supervisor log directory", p.name)
	}
	var names []string
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "supervisor-") {
			continue
		}
		if !spawnLogRE.MatchString(entry.Name()) {
			t.Fatalf("A9 %s collect: unexpected supervisor artifact name", p.name)
		}
		if err := os.Rename(filepath.Join(outputs, entry.Name()), filepath.Join(keep, entry.Name())); err != nil {
			t.Fatalf("A9 %s collect: move supervisor log", p.name)
		}
		names = append(names, "agent-outputs/"+spawnLogStampRE.ReplaceAllString(entry.Name(), "<STAMP>"))
	}
	sort.Strings(names)
	shas := p.fixedSHAs(t)
	p.harness(t, shas).Check(t, oracle.Human, p.name+"/supervisor-logs.txt", []byte(strings.Join(names, "\n")+"\n"))
}

// Process hygiene (D48). Linux ownership comes from nonparallel test isolation
// and the subreaper, never process observations. The portable process listing
// and remembered groups below support probes and non-Linux best-effort cleanup.

type launchProc struct {
	pid, ppid, pgid int
	command         string
}

// launchProcesses lists the live (non-zombie) processes of the host.
func launchProcesses() ([]launchProc, error) {
	cmd := exec.Command("ps", "-axww", "-o", "pid=,ppid=,pgid=,stat=,command=")
	env, err := oracle.ChildEnv(oracle.EnvBase{Home: os.TempDir(), Path: os.Getenv("PATH"), Tmp: os.TempDir()})
	if err != nil {
		return nil, err
	}
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var procs []launchProc
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || strings.HasPrefix(fields[3], "Z") {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		pgid, err3 := strconv.Atoi(fields[2])
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		rest := strings.TrimSpace(line)
		for i := 0; i < 4; i++ {
			rest = strings.TrimLeft(rest[strings.IndexAny(rest, " \t"):], " \t")
		}
		procs = append(procs, launchProc{pid, ppid, pgid, rest})
	}
	return procs, nil
}

// launchKillTree kills pid and every process below it, enumerated while pid is
// still alive. It never signals the test process.
func launchKillTree(pid int) {
	tree := map[int]bool{pid: true}
	if procs, err := launchProcesses(); err == nil {
		for grown := true; grown; {
			grown = false
			for _, p := range procs {
				if !tree[p.pid] && tree[p.ppid] && p.pid != os.Getpid() {
					tree[p.pid], grown = true, true
				}
			}
		}
	}
	for member := range tree {
		if proc, err := os.FindProcess(member); err == nil && member != os.Getpid() {
			_ = proc.Kill()
		}
	}
}

// launchOwner remembers every process of one scenario for the scenario's whole
// lifetime: pids and groups stay known after their leader has exited.
type launchOwner struct {
	root    string
	mu      sync.Mutex
	members map[int]bool
	groups  map[int]bool
}

// newLaunchOwner returns an owner that follows no process until told to.
func newLaunchOwner(root string) *launchOwner {
	// Before any process of the scenario starts, so none can be orphaned to init.
	if err := launchSubreaper(); err != nil {
		panic("A9 launch: child subreaper unavailable")
	}
	return &launchOwner{root: root, members: map[int]bool{}, groups: map[int]bool{}}
}

// start launches cmd as the leader of its own session and process group (see
// spawnDetach) and records that group before returning. The group id is the leader's pid, so every process that stays
// in the group is owned even if the leader exits before anything observes it.
func (o *launchOwner) start(cmd *exec.Cmd) error {
	spawnDetach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	o.mu.Lock()
	o.members[cmd.Process.Pid], o.groups[cmd.Process.Pid] = true, true
	o.mu.Unlock()
	return nil
}

// launchReap stops the remaining children and their groups, reaps their exits,
// and asserts that none remains (D48). Non-Linux cleanup is best effort (D45).
// The count excludes zombies: their exit status must be collected, but they
// were already stopped before cleanup.
func launchReap(t testing.TB, o *launchOwner) int {
	t.Helper()
	n, err := launchReapChildren(o)
	if err != nil {
		t.Errorf("A9 cleanup: %v", err)
	}
	return n
}

// launchCleanup stops every process left in the scenario and fails the test when
// any outlived it.
func launchCleanup(t testing.TB, name string, o *launchOwner) {
	t.Helper()
	if n := launchReap(t, o); n > 0 {
		t.Errorf("A9 %s cleanup: %d scenario process(es) outlived the test and were stopped", name, n)
	}
}

const launchProbeVar = "ORACLE_A9_CLEANUP_PROBE"

// TestOracleA9LaunchCleanup proves the cleanup runs on a failing path: it
// re-executes the probe, which fails on purpose after its leader has exited and
// left a child that names no root, and requires that the probe failed, that its
// cleanup stopped the orphan, and that neither the orphan nor any process naming
// the probe root remains.
func TestOracleA9LaunchCleanup(t *testing.T) {
	unixOnly(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal("A9 cleanup probe: test executable")
	}
	dir := t.TempDir()
	own := newLaunchOwner(dir)
	t.Cleanup(func() { launchCleanup(t, "cleanup probe", own) })
	env, err := oracle.ChildEnv(oracle.EnvBase{Home: dir, Path: os.Getenv("PATH"), Tmp: dir}, launchProbeVar+"=1")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestOracleA9LaunchCleanupProbe$", "-test.count=1")
	cmd.Dir, cmd.Env = dir, env
	spawnDetach(cmd)
	launchBind(cmd)
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatal("A9 cleanup probe: the probe must fail on purpose with exit code 1")
	}
	text := string(out)
	root, orphan := "", 0
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "A9PROBE root: ") {
			root = strings.TrimPrefix(line, "A9PROBE root: ")
		}
		if strings.HasPrefix(line, "A9PROBE orphan: ") {
			orphan, _ = strconv.Atoi(strings.TrimPrefix(line, "A9PROBE orphan: "))
		}
	}
	if root == "" || orphan == 0 || !strings.Contains(text, "controlled failure") {
		t.Fatal("A9 cleanup probe: the probe did not reach its controlled failure")
	}
	if !strings.Contains(text, "scenario process(es) outlived the test and were stopped") {
		t.Fatal("A9 cleanup probe: cleanup did not stop the orphan on the failure path")
	}
	procs, err := launchProcesses()
	if err != nil {
		t.Fatal("A9 cleanup probe: process listing unavailable")
	}
	for _, p := range procs {
		if p.pid == orphan || strings.Contains(p.command, root+"/") {
			t.Fatal("A9 cleanup probe: a probe process survived the cleanup")
		}
	}
}

// TestOracleA9LaunchCleanupProbe is the failing test of TestOracleA9LaunchCleanup;
// it does nothing unless its variable is set.
func TestOracleA9LaunchCleanupProbe(t *testing.T) {
	if os.Getenv(launchProbeVar) == "" {
		t.Skip("probe of TestOracleA9LaunchCleanup")
	}
	root := t.TempDir()
	// No observer: Linux cleanup owns every child, including this rootless orphan.
	own := newLaunchOwner(root)
	t.Cleanup(func() { launchCleanup(t, "probe", own) })
	// The leader names the root and exits at once after forking a child that names
	// nothing, so nothing can observe the child while the leader lives.
	cmd := exec.Command("sh", "-c", `sleep 300 & exit 0`, filepath.Join(root, "hold"))
	env, err := oracle.ChildEnv(oracle.EnvBase{Home: root, Path: os.Getenv("PATH"), Tmp: root})
	if err != nil {
		t.Fatal(err)
	}
	cmd.Env = env
	if err := own.start(cmd); err != nil {
		t.Fatal("A9 probe: start leader")
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal("A9 probe: leader wait")
	}
	procs, err := launchProcesses()
	if err != nil {
		t.Fatal("A9 probe: process listing unavailable")
	}
	orphan := 0
	for _, p := range procs {
		if p.pid == cmd.Process.Pid {
			t.Fatal("A9 probe: the leader must have exited")
		}
		if p.pgid == cmd.Process.Pid && p.command == "sleep 300" {
			orphan = p.pid
		}
	}
	if orphan == 0 {
		t.Fatal("A9 probe: the rootless child must outlive its leader in the leader's group")
	}
	fmt.Println("A9PROBE root: " + root)
	fmt.Printf("A9PROBE orphan: %d\n", orphan)
	t.Fatal("controlled failure")
}
