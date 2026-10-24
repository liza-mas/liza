package a9

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Orchestrator rows I-12 to I-15. Every scenario runs the orchestrator
// supervisor with the claude stub and ends by a stub-issued `liza stop`, except
// the consumed-summary refusal, which the test ends after observing the
// checkpoint gate poll. Two cells are reached through the nearest binary-visible
// path, recorded here rather than weakened:
//   - I-13 "a different checkpoint": PreWork compares the resumed checkpoint
//     twice, once at its gate and once inside the write callback. The callback
//     comparison needs a checkpoint replaced between PreWork's snapshot and its
//     write, which no process outside the supervisor can interleave, so the
//     refusal is a resumed hard checkpoint (SPRINT_COMPLETE) that the gate keeps.
//   - I-15 "summary already pending": a second obligation cannot be raised while
//     a summary runs, so the refusal is a checkpoint whose obligation was already
//     claimed (no pending_checkpoint_summary): nothing is emitted.
func TestOracleA9OrchestratorTurn(t *testing.T) {
	t.Parallel()
	t.Run("i12-success", func(t *testing.T) {
		t.Parallel()
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
		t.Parallel()
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
	t.Parallel()
	t.Run("i13-cleared", func(t *testing.T) {
		t.Parallel()
		p, r, launch := orchRun(t, "i13-cleared")
		if !bytes.Contains(p.before, []byte("checkpoint_trigger: PLANNING_COMPLETE")) ||
			bytes.Contains(launch, []byte("checkpoint_trigger")) || bytes.Contains(p.state(t), []byte("checkpoint_trigger")) {
			t.Fatal("I-13 a resumed transition checkpoint must have its trigger cleared before the turn launches")
		}
		p.check(t, r)
	})
	t.Run("i13-kept", func(t *testing.T) {
		t.Parallel()
		p, r, launch := orchRun(t, "i13-kept")
		for name, state := range map[string][]byte{"before": p.before, "launch": launch, "after": p.state(t)} {
			if !bytes.Contains(state, []byte("checkpoint_trigger: SPRINT_COMPLETE")) {
				t.Fatalf("I-13 a hard checkpoint must keep its trigger (%s)", name)
			}
		}
		p.check(t, r)
	})
	t.Run("i14-undecided", func(t *testing.T) {
		t.Parallel()
		p, r, _ := orchRun(t, "i14-undecided")
		after := p.state(t)
		alerts := string(p.runtimeFiles(t)["alerts.log"])
		if bytes.Count(after, []byte("event: plan_handoff_undecided")) != 1 || !strings.Contains(alerts, "PLAN DISPOSITION MISSING") {
			t.Fatal("I-14 an undispositioned merged plan must be recorded undecided with its alert")
		}
		p.check(t, r)
	})
	t.Run("i14-decided", func(t *testing.T) {
		t.Parallel()
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
	t.Parallel()
	t.Run("i15-emitted", func(t *testing.T) {
		t.Parallel()
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
		t.Parallel()
		p, r, _ := orchRun(t, "i15-failed")
		if len(p.records(t)) != 1 || !strings.Contains(string(p.runtimeFiles(t)["alerts.log"]), "CHECKPOINT SUMMARY FAILED") || bytes.Contains(p.state(t), []byte("pending_checkpoint_summary")) {
			t.Fatal("I-15 a failed summary must raise its alert and keep the obligation claimed")
		}
		p.check(t, r)
	})
	t.Run("i15-consumed", func(t *testing.T) {
		t.Parallel()
		p := newScenario(t, "i15-consumed")
		a, polled := orchStart(t, p, "agent", "orchestrator", "--cli", "claude", "--agent-id", "orchestrator-1")
		// The gate claims a pending summary before it logs its first wait, so
		// that log line proves a full poll found nothing to emit.
		p.await(t, "checkpoint gate poll", func() bool { return strings.Contains(polled.String(), "System paused, waiting for resume") })
		if len(p.records(t)) != 0 {
			t.Fatal("I-15 a consumed obligation must emit no summary")
		}
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

// orchRun runs the scenario's orchestrator to its scripted stop and returns the
// at-launch state the stub copied.
func orchRun(t *testing.T, name string) (*scenario, invocation, []byte) {
	t.Helper()
	p := newScenario(t, name)
	r := p.run(t, "agent", "orchestrator", "--cli", "claude", "--agent-id", "orchestrator-1")
	requireCode(t, p, r, 0)
	return p, r, mustRead(t, filepath.Join(p.recordDir, "launch-1", "state.yaml"))
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
	a := &runningAgent{done: make(chan error, 1)}
	seen := &orchBuffer{}
	a.cmd, a.cancel = p.command(p.cli, append([]string{"-C", p.dir}, args...)...)
	a.cmd.Stdout, a.cmd.Stderr = io.MultiWriter(&a.stdout, seen), &a.stderr
	if err := a.cmd.Start(); err != nil {
		t.Fatalf("A9 %s run: agent start", p.name)
	}
	go func() { a.done <- a.cmd.Wait(); close(a.done) }()
	t.Cleanup(func() { a.cancel(); <-a.done })
	return a, seen
}
