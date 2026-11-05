//go:build !windows

package subprocess

import (
	"os"
	"os/exec"
	"syscall"
)

func configProcessGroupKill(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

// Start starts cmd. Its process group, set by ConfigureCancellation, already
// lets cancellation reach every descendant.
func Start(cmd *exec.Cmd) error {
	return cmd.Start()
}
