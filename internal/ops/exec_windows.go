//go:build windows

package ops

import (
	"os"
	"os/exec"
	"time"

	"github.com/liza-mas/liza/internal/procscan"
)

// configProcessGroupKill cancels the whole spawned tree and waits for its exit.
func configProcessGroupKill(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		return procscan.KillProcessTree(cmd.Process.Pid)
	}
}

// signalProcessGroup kills the tree pid roots: Windows has no signals to
// forward.
func signalProcessGroup(pid int, _ os.Signal) error {
	return procscan.KillProcessTree(pid)
}

// reapProcessGroup cannot establish that a tree ended: once the root exited,
// its PID may be reused, so killing or probing a tree by it could hit an
// unrelated process. It fails closed; deferred validation refuses to run on
// Windows before reaching it (TECH_DEBT).
func reapProcessGroup(int, time.Duration) bool {
	return false
}
