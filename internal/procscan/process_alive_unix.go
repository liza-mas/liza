//go:build !windows

package procscan

import (
	"errors"
	"os"
	"syscall"
)

// ProcessAlive reports whether pid identifies a running process.
//
// Returns:
//   - alive: true if the process exists.
//   - permDenied: true if the process exists but the caller lacks permission
//     to signal it (EPERM). When permDenied is true, alive is also true.
//   - err: a non-nil error only if the probe itself could not be attempted
//     (e.g. os.FindProcess failed). A dead process is reported as
//     alive=false, permDenied=false, err=nil.
//
// Implementation: send signal 0 via os.FindProcess().Signal, the portable
// Unix way to test for process existence without affecting the target.
func ProcessAlive(pid int) (alive bool, permDenied bool, err error) {
	process, findErr := os.FindProcess(pid)
	if findErr != nil {
		return false, false, findErr
	}

	if sigErr := process.Signal(syscall.Signal(0)); sigErr == nil {
		return true, false, nil
	} else if errors.Is(sigErr, syscall.EPERM) {
		return true, true, nil
	}
	// ESRCH ("no such process") or any other error: treat as dead.
	return false, false, nil
}

// processProbeSource is the human-readable source label for the platform's
// existence probe, used in AgentProcessStatus.Detail diagnostics.
func processProbeSource() string {
	return "signal(0)"
}
