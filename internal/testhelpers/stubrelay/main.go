// Command stubrelay runs the shell script a test stub stands for.
//
// Windows needs a PATHEXT extension for exec.LookPath to consider a file, and
// cannot execute a shebang script at all, so a test stub has to be fronted by
// something the OS can start. A .cmd wrapper forwarding %* was the obvious
// choice and is wrong: cmd.exe cannot carry a newline inside a single argument,
// so a multi-line payload — a pane script, an agent prompt — arrives truncated
// at its first line. A real executable receives argv untouched.
//
// The relay is copied next to the script it serves and named after it, so one
// build serves every stub: given <name>.exe it runs <name>, with the shell
// named in <name>.bash.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "stub relay: locate self:", err)
		os.Exit(127)
	}
	script := strings.TrimSuffix(self, filepath.Ext(self))

	shell, err := os.ReadFile(script + ".bash")
	if err != nil {
		fmt.Fprintln(os.Stderr, "stub relay: read shell path:", err)
		os.Exit(127)
	}

	args := append([]string{filepath.ToSlash(script)}, os.Args[1:]...)
	cmd := exec.Command(strings.TrimSpace(string(shell)), args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		fmt.Fprintln(os.Stderr, "stub relay: run script:", err)
		os.Exit(127)
	}
}
