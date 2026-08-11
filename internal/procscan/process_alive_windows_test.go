//go:build windows

package procscan

import (
	"os/exec"
	"testing"
	"time"
)

// TestProcessAliveReportsExitedProcessWithSurvivingHandle covers the Windows
// case that has no Unix counterpart: a PID stays openable for as long as any
// handle to the process is held, so OpenProcess succeeding says nothing about
// whether the process still runs.
//
// The scenario is not contrived. os/exec holds a handle from Start until Wait,
// precisely so the PID cannot be recycled underneath it, and every liza agent
// is spawned that way.
func TestProcessAliveReportsExitedProcessWithSurvivingHandle(t *testing.T) {
	cmd := exec.Command("cmd", "/c", "exit", "0")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start short-lived process: %v", err)
	}
	// No Wait: the handle stays open, which is what keeps the PID resolvable
	// after the process has gone.
	t.Cleanup(func() { _ = cmd.Wait() })

	pid := cmd.Process.Pid
	deadline := time.Now().Add(30 * time.Second)
	for {
		alive, permDenied, err := ProcessAlive(pid)
		if err != nil {
			t.Fatalf("ProcessAlive(%d) error: %v", pid, err)
		}
		if permDenied {
			t.Fatalf("ProcessAlive(%d) reported permission denied for a child process", pid)
		}
		if !alive {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("ProcessAlive(%d) still reports alive after the process exited", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestProcessAliveReportsRunningProcess is the other half: the exit-code gate
// must not report a running process as dead.
func TestProcessAliveReportsRunningProcess(t *testing.T) {
	// A ping to the loopback address with a large count outlives the test
	// without needing a shell that stays attached to a console.
	cmd := exec.Command("ping", "-n", "60", "127.0.0.1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start long-lived process: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	alive, permDenied, err := ProcessAlive(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("ProcessAlive(%d) error: %v", cmd.Process.Pid, err)
	}
	if permDenied {
		t.Fatalf("ProcessAlive(%d) reported permission denied for a child process", cmd.Process.Pid)
	}
	if !alive {
		t.Fatalf("ProcessAlive(%d) = dead, want alive for a running process", cmd.Process.Pid)
	}
}
