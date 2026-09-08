//go:build windows

package ops

import (
	"os"
	"os/exec"

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
