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
func shellCommand(cmdStr, dir string) (*exec.Cmd, error) {
	cmd := exec.Command("sh", "-c", cmdStr)
	cmd.Dir = dir
	return cmd, nil
}

// shellCommandContext is shellCommand bound to a context, for callers that
// enforce a timeout.
func shellCommandContext(ctx context.Context, cmdStr, dir string) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	cmd.Dir = dir
	return cmd, nil
}
