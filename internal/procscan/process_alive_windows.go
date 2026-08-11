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
func ProcessAlive(pid int) (alive bool, permDenied bool, err error) {
	const PROCESS_QUERY_LIMITED_INFORMATION = 0x1000

	// syscall.OpenProcess(handle, inherit, pid)
	handle, e := syscall.OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if e == nil {
		syscall.CloseHandle(handle)
		return true, false, nil
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

// processProbeSource is the human-readable source label for the platform's
// existence probe, used in AgentProcessStatus.Detail diagnostics.
func processProbeSource() string {
	return "OpenProcess"
}
