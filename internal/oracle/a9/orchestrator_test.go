package a9

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

// Orchestrator rows I-12 to I-15. Every scenario runs the orchestrator
// supervisor with the claude stub and ends by a stub-issued `liza stop`, except
// the consumed-summary refusal, which the test ends after observing the
// checkpoint gate poll. Decision D44 settles three cells:
//   - I-13 "a different checkpoint": the callback comparison of PreWork differs
//     from its gate only when a checkpoint is replaced between PreWork's
//     snapshot and its write, which no process outside the supervisor can
//     interleave. The binary cell is the reachable gate (a hard checkpoint keeps
//     its trigger); the callback's decision is pinned through an AC2 adapter over
//     the exported ops.SameResumedTransitionCheckpoint (i13-guard).
//   - I-15 success pins the observed starting behaviour: the obligation is
//     claimed before the summary CLI runs and an alert is raised only when the
//     CLI fails.
//   - I-15 "summary already pending" goes through the in-flight guard: the first
//     summary CLI is held on a wait file while resume and sprint-checkpoint raise
//     a second obligation, which stays unclaimed until the first one ends.
func TestOracleA9OrchestratorTurn(t *testing.T) {
	t.Run("i12-success", func(t *testing.T) {
		p, r, launch := orchRun(t, "i12-success")
		after := p.state(t)
		requireLeases(t, launch, 1, false)
		for _, value := range []string{"role: orchestrator", "status: PLANNING", "current_task: planning"} {
			if !bytes.Contains(launch, []byte(value)) {
				t.Fatalf("I-12 at-launch orchestrator entry missing %s", value)
			}
		}
		if bytes.Contains(launch, []byte("orchestrator_seen_at")) || bytes.Count(after, []byte("orchestrator_seen_at:")) != 1 {
			t.Fatal("I-12 a clean turn must mark the unseen note seen after, not before, the turn")
		}
		if len(p.records(t)) != 1 {
			t.Fatal("I-12 the woken turn must launch the provider once")
		}
		p.check(t, r)
	})
	t.Run("i12-refused", func(t *testing.T) {
		p, r, launch := orchRun(t, "i12-refused")
		requireLeases(t, launch, 1, false)
		if !bytes.Contains(launch, []byte("status: PLANNING")) || bytes.Contains(p.state(t), []byte("orchestrator_seen_at")) {
			t.Fatal("I-12 a failed turn must leave the note unseen")
		}
		if len(p.records(t)) != 1 {
			t.Fatal("I-12 the failed turn must not launch the provider again")
		}
		p.check(t, r)
	})
}

func TestOracleA9OrchestratorPlans(t *testing.T) {
	t.Run("i13-cleared", func(t *testing.T) {
		p, r, launch := orchRun(t, "i13-cleared")
		if !bytes.Contains(p.before, []byte("checkpoint_trigger: PLANNING_COMPLETE")) ||
			bytes.Contains(launch, []byte("checkpoint_trigger")) || bytes.Contains(p.state(t), []byte("checkpoint_trigger")) {
			t.Fatal("I-13 a resumed transition checkpoint must have its trigger cleared before the turn launches")
		}
		p.check(t, r)
	})
	t.Run("i13-kept", func(t *testing.T) {
		p, r, launch := orchRun(t, "i13-kept")
		for name, state := range map[string][]byte{"before": p.before, "launch": launch, "after": p.state(t)} {
			if !bytes.Contains(state, []byte("checkpoint_trigger: SPRINT_COMPLETE")) {
				t.Fatalf("I-13 a hard checkpoint must keep its trigger (%s)", name)
			}
		}
		p.check(t, r)
	})
	t.Run("i13-guard", func(t *testing.T) {
		unixOnly(t)
		outcomes, err := orchSameCheckpoint(filepath.Join("testdata", "fixtures", "i13-guard", "sprints.yaml"))
		if err != nil {
			t.Fatalf("A9 i13-guard setup: %v", err)
		}
		var got strings.Builder
		for _, o := range outcomes {
			fmt.Fprintf(&got, "%s: same=%t\n", o.Name, o.Same)
			if want := o.Name == "same-checkpoint"; o.Same != want {
				t.Fatalf("I-13 guard %s: same=%t, want %t", o.Name, o.Same, want)
			}
		}
		if len(outcomes) != 5 {
			t.Fatalf("I-13 guard fixture must hold 5 sprint pairs, has %d", len(outcomes))
		}
		oracle.NewNormaliser().Check(t, oracle.Human, "i13-guard/outcome.txt", []byte(got.String()))
	})
	t.Run("i14-undecided", func(t *testing.T) {
		p, r, _ := orchRun(t, "i14-undecided")
		after := p.state(t)
		alerts := string(p.runtimeFiles(t)["alerts.log"])
		if bytes.Count(after, []byte("event: plan_handoff_undecided")) != 1 || !strings.Contains(alerts, "PLAN DISPOSITION MISSING") {
			t.Fatal("I-14 an undispositioned merged plan must be recorded undecided with its alert")
		}
		p.check(t, r)
	})
	t.Run("i14-decided", func(t *testing.T) {
		p, r, _ := orchRun(t, "i14-decided")
		after := p.state(t)
		alerts := string(p.runtimeFiles(t)["alerts.log"])
		if !bytes.Contains(after, []byte("verdict: held")) || bytes.Contains(after, []byte("plan_handoff_undecided")) || strings.Contains(alerts, "PLAN DISPOSITION MISSING") {
			t.Fatal("I-14 a plan decided during the turn must not be recorded undecided")
		}
		p.check(t, r)
	})
}

func TestOracleA9CheckpointSummary(t *testing.T) {
	t.Run("i15-emitted", func(t *testing.T) {
		p, r, launch := orchRun(t, "i15-emitted")
		records := p.records(t)
		if !bytes.Contains(p.before, []byte("pending_checkpoint_summary:")) || bytes.Contains(launch, []byte("pending_checkpoint_summary")) || bytes.Contains(p.state(t), []byte("pending_checkpoint_summary")) {
			t.Fatal("I-15 a pending summary must be claimed before the summary CLI runs")
		}
		if len(records) != 1 || !strings.Contains(records[0].Stdin, "Use the checkpoint-summary skill.") || !strings.Contains(records[0].Stdin, "(trigger: SPRINT_COMPLETE)") {
			t.Fatal("I-15 the summary CLI must run once with the checkpoint-summary prompt")
		}
		if !bytes.Equal(p.read(t, ".liza/checkpoint-summary.md"), []byte("# Checkpoint summary\n")) || len(p.runtimeFiles(t)["alerts.log"]) != 0 {
			t.Fatal("I-15 a written report must raise no alert")
		}
		p.check(t, r)
	})
	t.Run("i15-failed", func(t *testing.T) {
		p, r, _ := orchRun(t, "i15-failed")
		if len(p.records(t)) != 1 || !strings.Contains(string(p.runtimeFiles(t)["alerts.log"]), "CHECKPOINT SUMMARY FAILED") || bytes.Contains(p.state(t), []byte("pending_checkpoint_summary")) {
			t.Fatal("I-15 a failed summary must raise its alert and keep the obligation claimed")
		}
		p.check(t, r)
	})
	t.Run("i15-pending", func(t *testing.T) {
		p := newScenario(t, "i15-pending")
		a, polled := orchStart(t, p, "agent", "orchestrator", "--cli", "claude", "--agent-id", "orchestrator-1")
		// The first summary CLI is held on a wait file. While it runs, resuming
		// and checkpointing again raises a second obligation.
		p.await(t, "first summary held", func() bool { return len(p.records(t)) == 1 })
		for _, command := range []string{"resume", "sprint-checkpoint"} {
			if r := p.run(t, command); r.code != 0 {
				requireCode(t, p, r, 0)
			}
		}
		// Each gate poll is preceded by one wait line, so two more lines prove a
		// full poll saw the second obligation while the first summary ran.
		waits := func() int { return strings.Count(polled.String(), "System paused, waiting for resume") }
		seen := waits()
		p.await(t, "gate polls with the second obligation", func() bool { return waits() >= seen+2 })
		if !bytes.Contains(p.state(t), []byte("pending_checkpoint_summary:")) || len(p.records(t)) != 1 {
			t.Fatal("I-15 a summary already running must leave the new obligation unclaimed and start no second CLI")
		}
		orchObserve(t, p, 2)
		p.write(t, ".liza/oracle-release", []byte("release\n"))
		r := p.finish(t, a, 30*time.Second)
		requireCode(t, p, r, 0)
		if len(p.records(t)) != 2 || bytes.Contains(p.state(t), []byte("pending_checkpoint_summary")) {
			t.Fatal("I-15 the second obligation must be claimed once the first summary ends")
		}
		p.check(t, r)
	})
	t.Run("i15-consumed", func(t *testing.T) {
		p := newScenario(t, "i15-consumed")
		a, polled := orchStart(t, p, "agent", "orchestrator", "--cli", "claude", "--agent-id", "orchestrator-1")
		// The gate claims a pending summary before it logs its first wait, so
		// that log line proves a full poll found nothing to emit.
		p.await(t, "checkpoint gate poll", func() bool { return strings.Contains(polled.String(), "System paused, waiting for resume") })
		if len(p.records(t)) != 0 {
			t.Fatal("I-15 a consumed obligation must emit no summary")
		}
		if !bytes.Contains(p.state(t), []byte("orchestrator-1:")) {
			t.Fatal("I-15 the observation must catch the registered supervisor")
		}
		p.observeLaunch(t)
		for _, command := range []string{"resume", "stop"} {
			if r := p.run(t, command); r.code != 0 {
				t.Fatalf("I-15 %s failed", command)
			}
		}
		r := p.finish(t, a, 15*time.Second)
		requireCode(t, p, r, 0)
		if len(p.records(t)) != 0 || len(p.runtimeFiles(t)["alerts.log"]) != 0 {
			t.Fatal("I-15 a consumed obligation must emit no summary and no alert")
		}
		if _, err := orchReadReport(p); err == nil {
			t.Fatal("I-15 a consumed obligation must write no report")
		}
		p.check(t, r)
	})
}

// TestOracleA9OrchestratorReap shows the D45 cleanup on a held-provider failure
// path. The supervisor is cancelled while the summary stub is held, which orphans
// the stub; the cleanup must kill, reap and confirm the end of every child. The
// second case kills the orphan first to cover an observed zombie too.
// TestOracleA9OrchestratorAdoptedZombie separately proves exit and adoption before
// the first discovery, without registering a live child.
func TestOracleA9OrchestratorReap(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("A9: the child subreaper (decision D45) is Linux-only")
	}
	for _, exitedFirst := range []bool{false, true} {
		name := map[bool]string{false: "live-provider", true: "exited-before-reap"}[exitedFirst]
		t.Run(name, func(t *testing.T) {
			p := newScenario(t, "i15-pending")
			a, _ := orchStart(t, p, "agent", "orchestrator", "--cli", "claude", "--agent-id", "orchestrator-1")
			p.await(t, "first summary held", func() bool { return len(p.records(t)) == 1 })
			a.cancel()
			<-a.done
			var held []orchChild
			p.await(t, "held provider adopted", func() bool { held = orchChildren(); return len(held) > 0 })
			if exitedFirst {
				for _, child := range held {
					orchKillPID(child.pid)
				}
				p.await(t, "provider exit", func() bool {
					for _, child := range orchChildren() {
						if !child.zombie {
							return false
						}
					}
					return true
				})
			}
			orchReap(t)
			if left := orchChildren(); len(left) != 0 {
				t.Fatalf("A9 cleanup: %d children remain after the reap", len(left))
			}
		})
	}
}

// orchRun runs the scenario's orchestrator to its scripted stop and returns the
// at-launch state the stub copied.
func orchRun(t *testing.T, name string) (*scenario, invocation, []byte) {
	t.Helper()
	p := newScenario(t, name)
	a, _ := orchStart(t, p, "agent", "orchestrator", "--cli", "claude", "--agent-id", "orchestrator-1")
	r := p.finish(t, a, 60*time.Second)
	requireCode(t, p, r, 0)
	return p, r, mustRead(t, filepath.Join(p.recordDir, "launch-1", "state.yaml"))
}

// orchObserve stores the running state as at-launch snapshot n, beside the
// stub's own launch-1, for a scenario whose awaited moment is not a stub call.
func orchObserve(t testing.TB, p *scenario, n int) {
	t.Helper()
	dir := filepath.Join(p.recordDir, fmt.Sprintf("launch-%d", n))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal("A9 observation snapshot parent")
	}
	if err := os.WriteFile(filepath.Join(dir, "state.yaml"), p.state(t), 0o644); err != nil {
		t.Fatal("A9 observation snapshot")
	}
}

func orchReadReport(p *scenario) ([]byte, error) {
	return os.ReadFile(filepath.Join(p.dir, ".liza", "checkpoint-summary.md"))
}

// orchBuffer lets the test read stdout while the supervisor still writes it.
type orchBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *orchBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *orchBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// orchStart is scenario.start with a stdout copy that is safe to read mid-run:
// the supervisor's log records go to stdout.
func orchStart(t *testing.T, p *scenario, args ...string) (*runningAgent, *orchBuffer) {
	t.Helper()
	orchGuard(t)
	a := &runningAgent{done: make(chan error, 1)}
	seen := &orchBuffer{}
	a.cmd, a.cancel = p.command(p.cli, append([]string{"-C", p.dir}, args...)...)
	a.cmd.Stdout, a.cmd.Stderr = io.MultiWriter(&a.stdout, seen), &a.stderr
	orchDetach(a.cmd)
	if err := a.cmd.Start(); err != nil {
		t.Fatalf("A9 %s run: agent start", p.name)
	}
	go func() { a.done <- a.cmd.Wait(); close(a.done) }()
	leader := a.cmd.Process.Pid
	t.Cleanup(func() { a.cancel(); <-a.done; orchKillGroup(leader) })
	return a, seen
}
