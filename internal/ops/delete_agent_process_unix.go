//go:build !windows

package ops

import (
	"fmt"
	"os"
	"syscall"

	"github.com/liza-mas/liza/internal/procscan"
)

// isLizaAgentProcess checks if the process with the given PID is a liza agent
// by reading /proc/<pid>/cmdline. Returns false if the process doesn't exist,
// is unreadable, or isn't a liza agent.
//
// procfs-only: on a Unix without /proc the read fails and the answer is false,
// which callers treat as "do not signal this PID".
func isLizaAgentProcess(pid int) bool {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return false
	}
	return procscan.IsLizaAgentArgv(procscan.ParseCmdlineBytes(data))
}

func signalAgentProcessTree(pid int) error {
	if err := syscall.Kill(-pid, syscall.SIGTERM); err == nil {
		return nil
	}
	return syscall.Kill(pid, syscall.SIGTERM)
}

func killAgentProcessTree(pid int) error {
	if err := syscall.Kill(-pid, syscall.SIGKILL); err == nil {
		return nil
	}
	return syscall.Kill(pid, syscall.SIGKILL)
}
