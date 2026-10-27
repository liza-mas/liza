package a9

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// stdLogLineRE matches a line of the standard library logger, which stamps the
// local time without a zone. Log lines are free (frozen-surface table), and the
// kit's timestamp rules cover only structured records, so the new rows drop
// these lines from the compared console text.
var stdLogLineRE = regexp.MustCompile(`^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2} [A-Z]+: `)

func withoutStdLogLines(r invocation) invocation {
	keep := func(stream string) string {
		var kept []string
		for _, line := range strings.Split(stream, "\n") {
			if !stdLogLineRE.MatchString(line) {
				kept = append(kept, line)
			}
		}
		return strings.Join(kept, "\n")
	}
	r.stdout, r.stderr = keep(r.stdout), keep(r.stderr)
	return r
}

// installVersionProbeHook wraps the scenario's claude executable so that the
// first --version probe after coder-1 holds the claim runs the given CLI
// command. The runtime probes the version while it builds the provider command,
// after the claim committed and before the launch gate, so the command lands in
// the window no external caller can otherwise reach. The wrapper uses shell
// builtins only: the scenario PATH holds no other tool.
func (p *scenario) installVersionProbeHook(t testing.TB, command string) {
	t.Helper()
	testBin, err := os.Executable()
	if err != nil {
		t.Fatal("A9 setup: test executable")
	}
	flag := filepath.Join(p.recordDir, "probe-hook-ran")
	state := filepath.Join(p.dir, ".liza", "state.yaml")
	wrapper := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--version\" ] && [ ! -e " + shellQuote(flag) + " ]; then\n" +
		"  claimed=0\n" +
		"  while IFS= read -r line; do case $line in *'assigned_to: coder-1'*) claimed=1;; esac; done < " + shellQuote(state) + "\n" +
		"  if [ \"$claimed\" = 1 ]; then\n" +
		"    : > " + shellQuote(flag) + "\n" +
		"    " + shellQuote(p.cli) + " -C " + shellQuote(p.dir) + " " + command + " >/dev/null 2>&1\n" +
		"  fi\n" +
		"fi\n" +
		"export ORACLE_A9_STUB=claude\nexport ORACLE_A9_RECORD=" + shellQuote(p.recordDir) + "\nexec " + shellQuote(testBin) + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(p.bin, "claude"), []byte(wrapper), 0o755); err != nil {
		t.Fatal("A9 setup: probe hook wrapper")
	}
}

// The three exit-release rows start a supervisor and a held provider, so they
// follow the D48 isolation of the orchestrator rows: they are top-level and
// serial (never t.Parallel: orchGuard's reap asserts that no other scenario owns
// a child of the test process), and exitReleaseStart gives the supervisor its
// own process group and the test process the subreaper role before launch. The
// cleanup waits for the direct supervisor first, then signals, reaps and
// asserts the absence of every remaining child on success, assertion failure,
// cancellation and deadline alike (D45 group termination elsewhere).
// exit_release_cleanup_test.go proves that path with a held provider.

// exitReleaseStart starts the scenario's supervisor under D48 ownership.
func exitReleaseStart(t *testing.T, p *scenario, args ...string) *runningAgent {
	t.Helper()
	a, _ := orchStart(t, p, args...)
	return a
}

// exitReleaseRunLimit bounds a supervisor run; it is below the scenario command
// context (100 s) so that the deadline failure path is the one observed.
const exitReleaseRunLimit = 90 * time.Second

// TestOracleA9HaltRelease freezes the release of a claim whose provider start a
// halt refused: the pause commits between the claim and the launch gate, the
// gate refuses the start, and the supervisor releases the doer's claim as a
// continuation of the same iteration and idles at the pause gate. The launch
// golden is the state observed while parked, before the stop that ends the run.
func TestOracleA9HaltRelease(t *testing.T) {
	p := newScenario(t, "i05-halt-release")
	p.installVersionProbeHook(t, "pause")
	a := exitReleaseStart(t, p, "agent", "coder", "task-1", "--cli", "claude", "--agent-id", "coder-1")
	p.await(t, "halt release", func() bool {
		s := p.state(t)
		return bytes.Contains(s, []byte("mode: PAUSED")) && bytes.Contains(s, []byte("event: claim_released")) &&
			!bytes.Contains(s, []byte("current_task: task-1")) && bytes.Contains(s, []byte("coder-1:"))
	})
	p.observeLaunch(t)
	parked := p.state(t)
	for _, want := range []string{"continuation: true", "status: DRAFT_CODE"} {
		if !bytes.Contains(parked, []byte(want)) {
			t.Fatalf("I-05 halted start must release the claim as %q", want)
		}
	}
	if bytes.Contains(parked, []byte("assigned_to: coder-1")) || len(p.records(t)) != 0 {
		t.Fatal("I-05 halted start must leave the task unassigned and run no provider")
	}
	if r := p.run(t, "stop"); r.code != 0 {
		t.Fatal("I-05 stop failed")
	}
	r := p.finish(t, a, 40*time.Second)
	requireCode(t, p, r, 0)
	p.check(t, withoutStdLogLines(r))
}

// TestOracleA9BlockReleasedStale freezes the stale outcome of the block that
// follows a crash exit: the provider releases its own claim before it crashes,
// so the exit releases nothing and the crash-loop block, which may only act on
// the continuation that exit released, leaves the task unblocked. The crash
// threshold is 1, so the second crash reaches the block decision.
func TestOracleA9BlockReleasedStale(t *testing.T) {
	p := newScenario(t, "i05-block-released-stale")
	// Two crash exits (5 s and 10 s restart delays), then a third provider call
	// that stops the run.
	a := exitReleaseStart(t, p, "agent", "coder", "task-1", "--cli", "claude", "--agent-id", "coder-1")
	r := p.finish(t, a, exitReleaseRunLimit)
	requireCode(t, p, r, 0)
	after := p.state(t)
	if len(p.records(t)) != 3 || bytes.Count(after, []byte("event: doer_claim_released")) != 2 {
		t.Fatal("I-05 must run two crashes and the stopping call")
	}
	if bytes.Contains(after, []byte("status: BLOCKED")) || bytes.Contains(after, []byte("crash restart loop")) {
		t.Fatal("I-05 a crash that released no claim must not block the task")
	}
	p.check(t, withoutStdLogLines(r))
}

// TestOracleA9BlockDoerStale freezes the stale outcome of the block that
// follows a repeated observed tool failure: the provider releases its own claim
// before it exits, so by the time the failure loop reaches its threshold (1) the
// task is no longer owned by the agent and the block leaves it untouched.
func TestOracleA9BlockDoerStale(t *testing.T) {
	p := newScenario(t, "i05-block-doer-stale")
	a := exitReleaseStart(t, p, "agent", "coder", "task-1", "--cli", "claude", "--agent-id", "coder-1")
	// A run names two files by the UTC second: its saved prompt and, set up
	// separately, its agent-outputs stream (newAgentOutputLogs in
	// cli_agent_streams.go), which truncates a same-named file. Both stamps are
	// fixed before the provider writes call i's record, so each call is released
	// only after the clock has left the prompt's second and the second in which
	// that record was observed: no two runs share a prompt or an output name.
	for i := 1; i <= 3; i++ {
		index := i
		p.await(t, fmt.Sprintf("failing call %d", i), func() bool {
			paths, _ := filepath.Glob(filepath.Join(p.recordDir, "call-*.json"))
			return len(paths) >= index
		})
		observed := time.Now().UTC().Format("20060102-150405")
		prompts, _ := filepath.Glob(filepath.Join(p.dir, ".liza", "agent-prompts", "*.txt"))
		if len(prompts) == 0 {
			t.Fatal("I-05 provider launched without saved prompt")
		}
		stamp := a9StampRE.FindString(filepath.Base(prompts[len(prompts)-1]))
		p.await(t, "next prompt and output second", func() bool {
			now := time.Now().UTC().Format("20060102-150405")
			return now != stamp && now != observed
		})
		p.write(t, fmt.Sprintf(".liza/oracle-release-%d", i), []byte("release\n"))
	}
	r := p.finish(t, a, 40*time.Second)
	requireCode(t, p, r, 0)
	after := p.state(t)
	if len(p.records(t)) != 3 || bytes.Count(after, []byte("event: doer_claim_released")) != 2 {
		t.Fatal("I-05 must run two failing calls and the stopping call")
	}
	if bytes.Contains(after, []byte("status: BLOCKED")) || bytes.Contains(after, []byte("retry loop")) {
		t.Fatal("I-05 a failure loop on a task the agent no longer owns must not block it")
	}
	p.check(t, withoutStdLogLines(r))
}
