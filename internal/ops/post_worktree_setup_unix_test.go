//go:build !windows

package ops

import (
	stderrors "errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
)

func TestRunPostWorktreeCmd_ReconcileUsesCallerGeneration(t *testing.T) {
	f := newSetupFixture(t)
	f.seedHealth(t, "code-reviewer-1", AgentDegradedWorktreeSetupFailed, "task-1", time.Now().UTC().Add(-time.Hour))
	authority := models.AgentAuthority{ID: "coder-1", Generation: "gen-a"}
	// FIFOs hand control back and forth with the running setup command:
	// opening one blocks until the other side opens it.
	started := filepath.Join(f.worktree, "started")
	proceed := filepath.Join(f.worktree, "proceed")
	for _, fifo := range []string{started, proceed} {
		if err := syscall.Mkfifo(fifo, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	replaced := make(chan error, 1)
	go func() {
		if _, err := os.ReadFile(started); err != nil {
			replaced <- err
			return
		}
		// The caller's registration is replaced while its setup runs.
		err := db.For(f.statePath).Modify(func(state *models.State) error {
			agent := state.Agents["coder-1"]
			agent.Generation = "gen-z"
			state.Agents["coder-1"] = agent
			return nil
		})
		if writeErr := os.WriteFile(proceed, []byte("go\n"), 0o600); err == nil {
			err = writeErr
		}
		replaced <- err
	}()

	err := RunPostWorktreeCmd(f.root, "echo up > started; cat proceed > /dev/null", f.worktree, testSetupTimeout, &authority)
	if replaceErr := <-replaced; replaceErr != nil {
		t.Fatal(replaceErr)
	}

	if err != nil {
		t.Fatalf("RunPostWorktreeCmd() = %v, want the setup itself to pass", err)
	}
	if got := f.readOutcome(t); !got.OK {
		t.Errorf("outcome = %+v, want ok", got)
	}
	if _, ok := f.health(t)["code-reviewer-1"]; !ok {
		t.Fatal("a replaced caller's reconcile changed agent health")
	}

	if err := RunPostWorktreeCmd(f.root, "true", f.worktree, testSetupTimeout, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.health(t)["code-reviewer-1"]; ok {
		t.Error("an operator-path pass did not reconcile")
	}
}

// processGone reports whether the pid a setup command wrote to file has exited
// and been reaped.
func processGone(t *testing.T, file string) bool {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read pid file: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("parse pid file: %v", err)
	}
	return stderrors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

// D-34: a setup that outlives its bound is killed with every process it
// started, records a failed outcome, is not retried, and degrades the agent.
// "shell exits first" is the case exec's own context kill misses: the shell
// is gone while its child still holds the output pipes.
func TestRunPostWorktreeCmd_TimeoutKillsSetupAndFailsClosed(t *testing.T) {
	for name, cmd := range map[string]string{
		"foreground":        "echo started; echo $$ > pid; exec sleep 30",
		"shell waits":       "sleep 30 & echo $! > pid; wait",
		"shell exits first": "sleep 30 & echo $! > pid; exit 0",
	} {
		t.Run(name, func(t *testing.T) {
			f := newSetupFixture(t)
			start := time.Now()
			setupErr := f.runWithin(t, cmd, 200*time.Millisecond)
			elapsed := time.Since(start)

			if setupErr == nil || !strings.Contains(setupErr.Error(), "timed out after 200ms") {
				t.Fatalf("setup error = %v, want a timeout naming the bound", setupErr)
			}
			if elapsed > 5*time.Second {
				t.Errorf("setup returned after %s, want the bound enforced well before the %s output wait", elapsed, postWorktreeSetupWaitDelay)
			}
			if !processGone(t, filepath.Join(f.worktree, "pid")) {
				t.Error("a process the setup started survived the timeout")
			}
			if got := f.readOutcome(t); got.OK || got.Seq != setupErr.OutcomeSeq {
				t.Errorf("outcome = %+v, want the failure's own failed record (seq %d)", got, setupErr.OutcomeSeq)
			}
			output, err := os.ReadFile(f.output)
			if err != nil {
				t.Fatal(err)
			}
			if n := strings.Count(string(output), "=== attempt "); n != 1 {
				t.Errorf("output file holds %d attempts, want 1: a timed-out setup is not retried", n)
			}
			if !f.degrade(t, "coder-1", setupErr) {
				t.Error("a timed-out setup did not degrade the agent")
			}
		})
	}
}

// A failed attempt's leftover processes are stopped before the retry runs.
func TestRunPostWorktreeCmd_FailedAttemptReapedBeforeRetry(t *testing.T) {
	f := newSetupFixture(t)
	cmd := `if [ -e tried ]; then kill -0 "$(cat pid)" 2>/dev/null && touch survived; exit 0; fi; ` +
		`touch tried; sleep 30 >/dev/null 2>&1 & echo $! > pid; exit 1`

	if setupErr := f.run(t, cmd); setupErr != nil {
		t.Fatalf("setup error = %v, want the retry to pass", setupErr)
	}
	if _, err := os.Stat(filepath.Join(f.worktree, "survived")); err == nil {
		t.Error("the retry ran while the failed attempt's process was still alive")
	}
	if !processGone(t, filepath.Join(f.worktree, "pid")) {
		t.Error("the failed attempt's process survived the setup")
	}
}

// The bound covers the retry pause: expiring there ends the setup with a
// timeout and no second run.
func TestRunPostWorktreeCmd_DeadlineDuringRetryPause(t *testing.T) {
	f := newSetupFixture(t)
	t.Cleanup(SetPostWorktreeSetupRetryDelayForTest(time.Second))

	setupErr := f.runWithin(t, "echo run >> runs; exit 1", 200*time.Millisecond)

	if setupErr == nil || !strings.Contains(setupErr.Error(), "timed out after 200ms before the retry") {
		t.Fatalf("setup error = %v, want a timeout during the retry pause", setupErr)
	}
	runs, err := os.ReadFile(filepath.Join(f.worktree, "runs"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(runs), "run"); n != 1 {
		t.Errorf("command ran %d times, want 1", n)
	}
	if got := f.readOutcome(t); got.OK {
		t.Errorf("outcome = %+v, want failed", got)
	}
}
