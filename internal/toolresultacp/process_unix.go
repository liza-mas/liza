//go:build !windows

package toolresultacp

import (
	"os"
	"os/exec"
	"syscall"
)

// configureProcess owns a separate group, including shell grandchildren.
func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killProcess(cmd) }
}

func killProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if err == syscall.ESRCH {
		return os.ErrProcessDone
	}
	return err
}
