//go:build !linux && !windows

package a9

import "syscall"

// signalSubreaper: only Linux has child subreapers (D45).
func signalSubreaper() error { return nil }

// signalReap is the documented best-effort fallback for hosts without a child
// subreaper: it kills the supervisor's process group and cannot see a provider
// that detached into a group of its own, so it reports success without proving
// that nothing survived.
func signalReap(pid int) bool {
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	return true
}
