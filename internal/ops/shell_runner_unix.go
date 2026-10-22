//go:build !windows

package ops

import (
	"context"
	"os/exec"
)

// shellCommandContext builds an exec.Cmd bound to ctx that runs cmdStr through
// the platform shell.
//
// On Unix this is `sh -c`, matching the documented behavior of
// post_worktree_cmd and other shell-invoked configuration strings.
func shellCommandContext(ctx context.Context, cmdStr, dir string) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	cmd.Dir = dir
	return cmd, nil
}
