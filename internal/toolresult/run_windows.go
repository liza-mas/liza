//go:build windows

package toolresult

import "os/exec"

func configureRunCancellation(cmd *exec.Cmd) {}

func runExitCode(cmd *exec.Cmd) int {
	code := cmd.ProcessState.ExitCode()
	if code < 0 {
		return 1
	}
	return code
}
