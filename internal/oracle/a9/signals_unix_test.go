//go:build !windows

package a9

import (
	"errors"
	"os/exec"
	"syscall"
	"time"
)

// D43 ruling 2 / D45: no process a signal scenario starts outlives its test.
//
// On Linux the test process becomes a child subreaper (signalSubreaper) before
// its first start, so every orphaned descendant, the provider that runs in a
// process group of its own included, reparents to it. Cleanup then signals the
// children of the scenario and their groups, reaps them, and fails the test
// unless none is left (signals_linux_test.go). Elsewhere there is no subreaper:
// cleanup is a best-effort kill of the supervisor's process group
// (signals_other_test.go) and a provider in a group of its own can survive.
//
// Debt (D45): this helper is task-owned and duplicated per A9 oracle task; it
// consolidates into the kit in the first A9-area refactor task, which is its
// payback trigger.

// signalOwner identifies the processes of one scenario: its supervisor's
// session (the supervisor is its leader) and its HOME, which every descendant
// inherits; a provider that detaches into a group or session of its own is
// still found by it.
type signalOwner struct {
	sid  int
	home string
}

// signalIsolate puts the command in a session of its own and makes context
// cancellation (the kit's command deadline included) reap the command with
// everything the scenario started, and bounds the wait for output handles a
// surviving descendant could hold open.
func signalIsolate(cmd *exec.Cmd, home string) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error {
		if !signalReap(signalOwner{sid: cmd.Process.Pid, home: home}) {
			return errors.New("scenario processes survived cancellation")
		}
		return nil
	}
	cmd.WaitDelay = 2 * time.Second
}
