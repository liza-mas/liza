//go:build windows

package toolresultacp

import (
	"os"
	"os/exec"
	"syscall"

	"github.com/liza-mas/liza/internal/procscan"
)

func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
	cmd.Cancel = func() error { return killProcess(cmd) }
}
func killProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	return procscan.KillProcessTree(cmd.Process.Pid)
}
