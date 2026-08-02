//go:build windows

package ops

import (
	"errors"
	"fmt"
	"os/exec"
)

// shellCommand builds an exec.Cmd that runs cmdStr through a shell on Windows.
//
// Preference order:
//  1. sh (Git for Windows) — preferred so existing bash-based post_worktree_cmd
//     scripts keep working unchanged, consistent with the project's
//     "Git for Windows required" stance.
//  2. cmd /c — native fallback when sh is not on PATH.
//
// The chosen shell is recorded so callers can report it in diagnostics.
func shellCommand(cmdStr, dir string) *exec.Cmd {
	if path, err := exec.LookPath("sh"); err == nil {
		cmd := exec.Command(path, "-c", cmdStr)
		cmd.Dir = dir
		return cmd
	}
	cmd := exec.Command("cmd", "/c", cmdStr)
	cmd.Dir = dir
	return cmd
}

// shellMissingError returns a non-nil error only when neither sh nor cmd is
// available (cmd is always present on supported Windows, so this is purely
// defensive).
func shellMissingError() error {
	if _, err := exec.LookPath("sh"); err == nil {
		return nil
	}
	if _, err := exec.LookPath("cmd"); err == nil {
		return nil
	}
	return errors.New("no shell available: neither sh nor cmd found on PATH")
}

// formatShellMissingHelp returns a Windows-specific hint for users who lack sh.
func formatShellMissingHelp() string {
	if _, err := exec.LookPath("sh"); err != nil {
		return fmt.Sprintf("; install Git for Windows to enable bash-based commands (got: %v)", err)
	}
	return ""
}
