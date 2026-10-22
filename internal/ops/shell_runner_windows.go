//go:build windows

package ops

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/liza-mas/liza/internal/gitbash"
)

// shellCommandContext builds an exec.Cmd bound to ctx that runs cmdStr through
// a shell on Windows.
//
// Git Bash is required because the configured commands use the project's
// documented POSIX shell syntax; cmd.exe is not a compatible fallback.
func shellCommandContext(ctx context.Context, cmdStr, dir string) (*exec.Cmd, error) {
	path, err := gitbash.Resolve()
	if err != nil {
		return nil, fmt.Errorf("POSIX shell unavailable: %w", err)
	}
	cmd := exec.CommandContext(ctx, path, "-c", cmdStr)
	cmd.Dir = dir
	return cmd, nil
}
