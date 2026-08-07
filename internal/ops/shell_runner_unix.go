//go:build !windows

package ops

import (
	"context"
	"os/exec"
)

// shellCommand builds an exec.Cmd that runs cmdStr through the platform shell.
//
// On Unix this is `sh -c`, matching the documented behavior of
// post_worktree_cmd and other shell-invoked configuration strings.
func shellCommand(cmdStr, dir string) *exec.Cmd {
	cmd := exec.Command("sh", "-c", cmdStr)
	cmd.Dir = dir
	return cmd
}

// shellCommandContext is shellCommand bound to a context, for callers that
// enforce a timeout.
func shellCommandContext(ctx context.Context, cmdStr, dir string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	cmd.Dir = dir
	return cmd
}

// shellMissingError explains (for test/diagnostic clarity) when the platform
// shell is unavailable. On Unix this never happens in practice, so it returns
// nil.
func shellMissingError() error { return nil }

// formatShellMissingHelp returns a platform-specific hint. Empty on Unix.
func formatShellMissingHelp() string { return "" }
