//go:build !windows

package ops

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// configProcessGroupKill sets up the command to run in its own process group
// and kill the entire group on context cancellation. This prevents child
// processes from holding stdout/stderr pipes open after the parent is killed.
func configProcessGroupKill(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

// signalProcessGroup delivers sig to every member of the process group pid
// leads (a configProcessGroupKill command).
func signalProcessGroup(pid int, sig os.Signal) error {
	number, ok := sig.(syscall.Signal)
	if !ok {
		number = syscall.SIGTERM
	}
	return syscall.Kill(-pid, number)
}

// processGroupPollInterval spaces the probes for a killed process group's
// members to be gone.
const processGroupPollInterval = 20 * time.Millisecond

// reapProcessGroup kills what remains of the process group pid led (a
// configProcessGroupKill command that already exited) and waits up to wait
// for every member to be gone. It reports whether none survives.
func reapProcessGroup(pid int, wait time.Duration) bool {
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	deadline := time.Now().Add(wait)
	for {
		if err := syscall.Kill(-pid, 0); errors.Is(err, syscall.ESRCH) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(processGroupPollInterval)
	}
}
