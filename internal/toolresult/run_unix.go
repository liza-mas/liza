//go:build !windows

package toolresult

import (
	"os"
	"os/exec"
	"syscall"
)

// configureRunCancellation keeps the command in the caller's process group, so
// a native client's timeout or interrupt, which signals its own group, reaches
// the command and its jobs exactly as it would without the runner.
func configureRunCancellation(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		return cmd.Process.Kill()
	}
}

func runExitCode(cmd *exec.Cmd) int {
	if status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal())
	}
	return cmd.ProcessState.ExitCode()
}
