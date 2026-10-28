package control

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

// The held-slot admit cases need a limit the shared stub-limit fixture cannot
// express: a wrapped command that stays running until the driver releases it,
// so a second admit meets a slot that is actually held. The fixture lives
// beside, not in, fixtures/state so the nine-state validation is unchanged.
const heldSlotFixture = "testdata/fixtures/admit/held-slot.yaml"

// holdStub is the wrapped command: it opens its release fifo read-write before
// announcing itself, so the driver's later write always finds a reader, then
// blocks on that fifo. Only shell builtins are used: the child PATH holds git
// and the stubs alone.
const holdStub = "#!/bin/sh\nexec 7<>\"$1/release\"\n: > \"$1/started\"\nread line <&7\necho \"oracle hold released $2\"\n"

type heldSlotRun struct {
	cmd     *exec.Cmd
	stdout  bytes.Buffer
	stderr  bytes.Buffer
	done    chan error
	started string
	release string
}

func newHeldSlotProject(t *testing.T) *project {
	t.Helper()
	p := newProject(t, "base", false, false, false)
	p.before = bytes.ReplaceAll(mustRead(t, heldSlotFixture), []byte("<live-pid>"), []byte(strconv.Itoa(os.Getpid())))
	mustWrite(t, filepath.Join(p.dir, ".liza", "state.yaml"), p.before, 0o644)
	mustWrite(t, filepath.Join(p.bin, "oracle-hold"), []byte(holdStub), 0o755)
	return p
}

// startAdmit starts an admit of the hold stub without waiting for it. label
// names its control directory and the stub's output.
func (p *project) startAdmit(t *testing.T, label string, admitFlags ...string) *heldSlotRun {
	t.Helper()
	dir := filepath.Join(p.root, "hold-"+label)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal("create hold control directory")
	}
	if err := makeFifo(filepath.Join(dir, "release")); err != nil {
		t.Fatal("create hold release fifo")
	}
	args := append(append([]string{"admit"}, admitFlags...), "--", "oracle-hold", dir, label)
	cmd, cancel := p.command(oracle.BuildCLI(t, "./cmd/liza"), args...)
	t.Cleanup(cancel)
	run := &heldSlotRun{cmd: cmd, done: make(chan error, 1), started: filepath.Join(dir, "started"), release: filepath.Join(dir, "release")}
	cmd.Stdout, cmd.Stderr = &run.stdout, &run.stderr
	p.norm.start = time.Now()
	if err := cmd.Start(); err != nil {
		t.Fatal("fixture CLI failed to start")
	}
	go func() { run.done <- cmd.Wait() }()
	return run
}

// awaitPath returns once the condition holds, failing if the run ends first or
// the bound passes; it observes state, it never decides the outcome.
func (r *heldSlotRun) awaitPath(t *testing.T, what string, holds func() bool) {
	t.Helper()
	poll := time.NewTicker(5 * time.Millisecond)
	defer poll.Stop()
	timeout := time.NewTimer(20 * time.Second)
	defer timeout.Stop()
	for !holds() {
		select {
		case err := <-r.done:
			r.done <- err
			t.Fatalf("admit ended before %s: %v; stderr: %s", what, err, r.stderr.String())
		case <-timeout.C:
			t.Fatalf("timed out waiting for %s", what)
		case <-poll.C:
		}
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (r *heldSlotRun) awaitStarted(t *testing.T) {
	t.Helper()
	r.awaitPath(t, "the wrapped command to start", func() bool { return exists(r.started) })
}

func (r *heldSlotRun) releaseHold(t *testing.T) {
	t.Helper()
	f, err := os.OpenFile(r.release, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal("open hold release fifo")
	}
	if _, err := f.WriteString("\n"); err != nil {
		t.Fatal("write hold release")
	}
	if err := f.Close(); err != nil {
		t.Fatal("close hold release")
	}
}

func (r *heldSlotRun) wait(t *testing.T) invocation {
	t.Helper()
	res := invocation{}
	err := <-r.done
	res.stdout, res.stderr = r.stdout.String(), r.stderr.String()
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatal("fixture CLI failed to run")
		}
		res.code = exit.ExitCode()
		if res.code < 0 {
			t.Fatal("fixture CLI terminated before completion")
		}
	}
	return res
}

// waiterTickets lists the queue entries of the hold class: with the holder
// running, only a waiter's ticket can be there.
func (p *project) waiterTickets(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(p.dir, ".liza", "admission-slots", "hold", "queue"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal("read admission queue")
	}
	var tickets []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".lock") {
			tickets = append(tickets, entry.Name())
		}
	}
	return tickets
}

func (p *project) expect(t *testing.T, name string, res invocation, code int) {
	t.Helper()
	if res.code != code {
		t.Fatalf("%s: exit %d, want %d; %s", name, res.code, code, p.normalise(t, "stderr", []byte(res.stderr)))
	}
	p.human(t, "admit-held-slot/"+name, res)
}

func (p *project) expectStateUnchanged(t *testing.T) {
	t.Helper()
	if !bytes.Equal(p.before, p.state(t)) {
		t.Fatal("admit changed state.yaml")
	}
}

// TestOracleAdmitHeldSlot pins admit against a slot that is held: refused when
// it may not wait, and waiting until the holder releases when it may.
func TestOracleAdmitHeldSlot(t *testing.T) {
	linuxOnly(t)

	t.Run("refused-held-slot", func(t *testing.T) {
		p := newHeldSlotProject(t)
		holder := p.startAdmit(t, "holder")
		holder.awaitStarted(t)

		waiter := p.startAdmit(t, "waiter", "--max-wait", "0")
		res := waiter.wait(t)
		if exists(waiter.started) {
			t.Fatal("refused admit ran its command")
		}
		p.expect(t, "refused-held-slot", res, 1)

		holder.releaseHold(t)
		p.expect(t, "refused-held-slot-holder", holder.wait(t), 0)
		p.expectStateUnchanged(t)
	})

	t.Run("wait-then-run", func(t *testing.T) {
		p := newHeldSlotProject(t)
		holder := p.startAdmit(t, "holder")
		holder.awaitStarted(t)

		waiter := p.startAdmit(t, "waiter", "--max-wait", "60")
		waiter.awaitPath(t, "the waiter's queue entry", func() bool { return len(p.waiterTickets(t)) == 1 })
		if exists(waiter.started) {
			t.Fatal("waiting admit ran its command while the slot was held")
		}

		holder.releaseHold(t)
		p.expect(t, "wait-then-run-holder", holder.wait(t), 0)
		waiter.awaitStarted(t)
		waiter.releaseHold(t)
		p.expect(t, "wait-then-run", waiter.wait(t), 0)
		if tickets := p.waiterTickets(t); len(tickets) != 0 {
			t.Fatalf("queue entries left after the waiter ended: %d", len(tickets))
		}
		p.expectStateUnchanged(t)
	})
}
