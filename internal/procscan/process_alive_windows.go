//go:build windows

package procscan

import (
	"syscall"
)

// processAlive reports whether pid identifies a running process on Windows.
//
// Signal 0 is not supported on Windows (os.Process.Signal returns "not
// supported"), so the existence probe uses OpenProcess with
// PROCESS_QUERY_LIMITED_INFORMATION. A successful handle means the process
// exists; ERROR_INVALID_PARAMETER (returned for PIDs that don't correspond to
// any process, or for pid 0/System) is treated as "not found".
//
// permDenied is true when the caller can see the process exists but lacks
// permission to open it (ERROR_ACCESS_DENIED). In that case alive is also true.
//
// A handle is not on its own evidence that the process runs: Windows keeps a
// PID resolvable for as long as anything holds a handle to it, and os/exec
// holds one from Start until Wait so the PID cannot be recycled. Every agent is
// spawned that way, so the exit code decides.
func ProcessAlive(pid int) (alive bool, permDenied bool, err error) {
	const PROCESS_QUERY_LIMITED_INFORMATION = 0x1000

	// syscall.OpenProcess(handle, inherit, pid)
	handle, e := syscall.OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if e == nil {
		defer syscall.CloseHandle(handle)
		return processStillActive(handle)
	}

	// syscall.Errno is the raw Windows error code. ERROR_ACCESS_DENIED (5) and
	// ERROR_INVALID_PARAMETER (87) are not all exported by name in the syscall
	// package, so compare numerically.
	const (
		errAccessDenied     = syscall.Errno(5)
		errInvalidParameter = syscall.Errno(87)
	)
	switch e {
	case errAccessDenied:
		// Process exists but we can't open it: treat as alive.
		return true, true, nil
	case errInvalidParameter:
		// No process for this PID (also returned for the Idle process / pid 0).
		return false, false, nil
	default:
		// Unknown error: be conservative and report dead rather than holding a
		// phantom agent row. Callers (signalProcessStatus) surface err.Error()
		// in the status detail for diagnosis.
		return false, false, e
	}
}

// processStillActive reports whether an already-opened process handle refers to
// a process that is still running.
//
// STILL_ACTIVE (259) is what GetExitCodeProcess reports while a process runs.
// The well-known cost is that a process which genuinely exits with code 259
// reads as alive; that is inherent to the API, and the alternative — asking for
// SYNCHRONIZE so WaitForSingleObject can answer instead — widens the access
// rights we request and so widens the cases that come back ERROR_ACCESS_DENIED.
//
// A failure here reports alive rather than dead. The handle opened, so the
// process is known to the OS and only the exit code is in doubt; callers use
// this to decide whether to terminate before dropping an agent row, and a false
// "dead" there orphans a running agent and its provider. That is the opposite
// trade from the unknown-OpenProcess-error branch above, where nothing is known
// about the PID at all.
func processStillActive(handle syscall.Handle) (alive bool, permDenied bool, err error) {
	const stillActive = 259

	var code uint32
	if e := syscall.GetExitCodeProcess(handle, &code); e != nil {
		return true, false, e
	}
	return code == stillActive, false, nil
}

// processProbeSource is the human-readable source label for the platform's
// existence probe, used in AgentProcessStatus.Detail diagnostics.
func processProbeSource() string {
	return "OpenProcess+GetExitCodeProcess"
}
