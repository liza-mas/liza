//go:build !windows

package subprocess

import (
	"os/exec"
	"syscall"
)

// SetDetachedProcessGroup places the command in a new session. setsid(2)
// also creates a new process group, so setting Setpgid as well would ask the
// child to call setpgid after becoming a session leader, which fails with EPERM.
func SetDetachedProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
}
