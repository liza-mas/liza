//go:build windows

package ops

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/liza-mas/liza/internal/gitbash"
)

// shellCommand builds an exec.Cmd that runs cmdStr through a shell on Windows.
//
// Git Bash is required because the configured commands use the project's
// documented POSIX shell syntax; cmd.exe is not a compatible fallback.
func shellCommand(cmdStr, dir string) (*exec.Cmd, error) {
	path, err := gitbash.Resolve()
	if err != nil {
		return nil, fmt.Errorf("POSIX shell unavailable: %w", err)
	}
	cmd := exec.Command(path, "-c", cmdStr)
	cmd.Dir = dir
	return cmd, nil
}

// shellCommandContext is shellCommand bound to a context, for callers that
// enforce a timeout.
func shellCommandContext(ctx context.Context, cmdStr, dir string) (*exec.Cmd, error) {
	path, err := gitbash.Resolve()
	if err != nil {
		return nil, fmt.Errorf("POSIX shell unavailable: %w", err)
	}
	cmd := exec.CommandContext(ctx, path, "-c", cmdStr)
	cmd.Dir = dir
	return cmd, nil
}
