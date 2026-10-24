//go:build linux

package a9

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// signalGone reports whether pid no longer exists, a zombie included: the test
// process, the subreaper, has reaped it.
func signalGone(pid int) bool { return errors.Is(unix.Kill(pid, 0), unix.ESRCH) }

// TestSignalGroupReap shows the subreaper bar of D45: a descendant that
// outlives its group leader becomes a child of the test process, and the reap
// kills and reaps it, which the kit's direct-child cancellation does not.
func TestSignalGroupReap(t *testing.T) {
	if err := signalSubreaper(); err != nil {
		t.Fatal("A9 signal group: child subreaper unavailable")
	}
	cmd := exec.CommandContext(context.Background(), "/bin/sh", "-c", "/bin/sleep 300 >/dev/null 2>&1 & echo ready; wait")
	cmd.Env = []string{}
	signalIsolate(cmd)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal("A9 signal group: pipe unavailable")
	}
	if err := cmd.Start(); err != nil {
		t.Fatal("A9 signal group: start failed")
	}
	supervisor := cmd.Process.Pid
	t.Cleanup(func() {
		if !signalReap(supervisor) {
			t.Error("A9 signal group: cleanup left a child")
		}
	})
	if line, _ := bufio.NewReader(out).ReadString('\n'); line != "ready\n" {
		t.Fatal("A9 signal group: descendant not started")
	}
	below, err := signalChildrenOf(supervisor)
	if err != nil || len(below) != 1 {
		t.Fatal("A9 signal group: expected one descendant")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal("A9 signal group: leader kill failed")
	}
	_ = cmd.Wait()
	mine, err := signalChildrenOf(os.Getpid())
	if err != nil || !signalHasChild(mine, below[0].pid) {
		t.Fatal("A9 signal group: the orphaned descendant must reparent to the test process")
	}
	if !signalReap(supervisor) || !signalGone(below[0].pid) {
		t.Fatal("A9 signal group: reap must remove and reap the descendant")
	}
}

// TestSignalUnattributedReap guards D48's refusal to filter children by session
// or HOME. The child has no environment and shares the test's session, with its
// own group. ForkExec leaves Wait4 ownership with the reaper, unlike exec.Cmd.
func TestSignalUnattributedReap(t *testing.T) {
	if err := signalSubreaper(); err != nil {
		t.Fatal("A9 signal child: child subreaper unavailable")
	}
	pid, err := syscall.ForkExec("/bin/sleep", []string{"sleep", "300"}, &syscall.ProcAttr{
		Env: []string{}, Sys: &syscall.SysProcAttr{Setpgid: true},
	})
	if err != nil {
		t.Fatal("A9 signal child: start failed")
	}
	t.Cleanup(func() {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		_, _ = syscall.Wait4(pid, nil, 0, nil)
	})
	if !signalReap(pid) || !signalGone(pid) {
		t.Fatal("A9 signal child: a child without scenario attribution survived")
	}
}

func signalHasChild(kids []signalChild, pid int) bool {
	for _, c := range kids {
		if c.pid == pid {
			return true
		}
	}
	return false
}

// TestSignalProviderReap holds a real scenario stub provider, which the
// supervisor runs in a process group of its own, and ends the scenario while it is
// held: through the cleanup every signal scenario registers, and through the
// cancellation the kit's command deadline performs before any cleanup. The
// supervisor and the provider must be gone and reaped either way; the cleanup
// is the same on a failing test, which t.Cleanup runs too.
func TestSignalProviderReap(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		name := "cleanup"
		if cancelled {
			name = "cancelled"
		}
		var started []int
		t.Run(name, func(t *testing.T) {
			p := newScenario(t, "i19-held")
			a := signalStart(t, p, signalCoderArgs("claude")...)
			p.await(t, "held provider", func() bool {
				_, err := os.Stat(filepath.Join(p.recordDir, "launch-1", "state.yaml"))
				return err == nil
			})
			supervisor := a.cmd.Process.Pid
			started = append(started, supervisor)
			detached := false
			below, err := signalChildrenOf(supervisor)
			if err != nil {
				t.Fatal("A9 signal provider: processes unobservable")
			}
			for _, c := range below {
				started = append(started, c.pid)
				detached = detached || c.pgid == c.pid
			}
			if !detached {
				t.Fatal("I-19 the held provider must run in a group of its own")
			}
			if cancelled {
				a.cancel()
				select {
				case <-a.done:
				case <-time.After(10 * time.Second):
					t.Fatal("I-19 cancelled supervisor did not exit")
				}
				signalRequireGone(t, started)
			}
		})
		signalRequireGone(t, started)
	}
}

// signalRequireGone fails the test unless every process is gone and reaped.
func signalRequireGone(t *testing.T, pids []int) {
	t.Helper()
	if len(pids) < 2 {
		t.Fatal("A9 signal provider: no provider observed")
	}
	for _, pid := range pids {
		if !signalGone(pid) {
			t.Fatal("A9 signal provider: a started process outlived its scenario")
		}
	}
}
