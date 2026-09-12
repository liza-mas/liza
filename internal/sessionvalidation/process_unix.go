//go:build !windows

package sessionvalidation

import (
	"os"
	"os/exec"
	"syscall"
)

// Probes have their own process group so the deadline also stops descendants.
func configureProbeCancellation(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if err == syscall.ESRCH {
			return os.ErrProcessDone
		}
		return err
	}
}
