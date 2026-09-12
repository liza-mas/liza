//go:build windows

package sessionvalidation

import (
	"os"
	"os/exec"

	"github.com/liza-mas/liza/internal/procscan"
)

// Probes use the same process-tree termination as other bounded commands.
func configureProbeCancellation(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		return procscan.KillProcessTree(cmd.Process.Pid)
	}
}
