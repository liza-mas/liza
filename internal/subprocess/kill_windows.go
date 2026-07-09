//go:build windows

package subprocess

import "os/exec"

// configProcessGroupKill is a no-op on Windows. exec.CommandContext's default
// cancellation kills the direct child on deadline; WaitDelay bounds pipe waits
// if descendants outlive the canceled child.
func configProcessGroupKill(cmd *exec.Cmd) {
	_ = cmd
}
