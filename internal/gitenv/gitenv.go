// Package gitenv provides locale-safe environment for git subprocesses.
package gitenv

import (
	"os"
	"os/exec"
	"strings"
)

// Env returns the current environment with LC_ALL=C forced, so git output
// is always in English regardless of the system locale.
func Env() []string {
	env := os.Environ()
	for i, e := range env {
		if strings.HasPrefix(e, "LC_ALL=") {
			env[i] = "LC_ALL=C"
			return env
		}
	}
	return append(env, "LC_ALL=C")
}

// Command creates an exec.Cmd for git with LC_ALL=C forced.
func Command(args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	cmd.Env = Env()
	return cmd
}
