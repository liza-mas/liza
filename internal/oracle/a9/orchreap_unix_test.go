//go:build !windows

package a9

import (
	"os/exec"
	"syscall"
)

// orchDetach gives the supervisor its own process group, so its group can be
// terminated without touching the test process or a parallel scenario.
func orchDetach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

func orchKillGroup(leader int) { _ = syscall.Kill(-leader, syscall.SIGKILL) }
