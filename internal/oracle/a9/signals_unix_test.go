//go:build !windows

package a9

import (
	"os/exec"
	"sync/atomic"
	"syscall"
	"time"
)

// D43 ruling 2 / D45 / D48: no process a signal scenario starts outlives its test.
//
// On Linux the test process becomes a child subreaper (signalSubreaper) before
// its first start, so every orphaned descendant, the provider that runs in a
// process group of its own included, reparents to it. Cleanup then signals the
// children of the test process and their groups, reaps them, and fails the test
// unless none is left (signals_linux_test.go). Elsewhere there is no subreaper:
// cleanup is a best-effort kill of the supervisor's process group
// (signals_other_test.go) and a provider in a group of its own can survive.
//
// Debt (D45): this helper is task-owned and duplicated per A9 oracle task; it
// consolidates into the kit in the first A9-area refactor task, which is its
// payback trigger.

// signalIsolate terminates the supervisor on cancellation, including the kit's
// deadline. exec.Cmd retains ownership of its Wait; signalStart reaps adopted
// children only after that Wait completes, before publishing a.done. Otherwise
// Wait4 could steal exec.Cmd's child, or run before its descendants are adopted.
func signalIsolate(cmd *exec.Cmd) *atomic.Bool {
	cancelled := new(atomic.Bool)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error {
		cancelled.Store(true)
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		return cmd.Process.Kill()
	}
	cmd.WaitDelay = 2 * time.Second
	return cancelled
}
