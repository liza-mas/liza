//go:build !windows

package a9

import (
	"os/exec"
	"syscall"
)

// signalIsolate puts the command in its own process group and makes context
// cancellation kill that whole group, not only the direct child.
func signalIsolate(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
}

func signalKillGroup(pgid int) { _ = syscall.Kill(-pgid, syscall.SIGKILL) }

// signalGroupAlive reports whether any process (zombies included) is left in the group.
func signalGroupAlive(pgid int) bool { return syscall.Kill(-pgid, 0) == nil }
