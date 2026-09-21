//go:build windows

package subprocess

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"time"
)

// configProcessGroupKill terminates descendants as well as the direct child:
// otherwise they can retain output pipes and the command's working directory.
func configProcessGroupKill(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		// Cancellation itself must stay bounded if taskkill cannot make progress.
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		kill := exec.CommandContext(ctx, "taskkill.exe", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
		if err := kill.Run(); err != nil {
			// Retain CommandContext's direct-child guarantee if tree termination
			// is unavailable, while exposing the tree cleanup failure.
			killErr := cmd.Process.Kill()
			if errors.Is(killErr, os.ErrProcessDone) {
				return os.ErrProcessDone
			}
			return errors.Join(fmt.Errorf("terminate process tree: %w", err), killErr)
		}
		return nil
	}
}
