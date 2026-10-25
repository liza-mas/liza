// Command gitshim forwards its arguments to the host Git, so a platform that
// launches only executables can have a Git entry in a git-only PATH directory.
// The entry is a copy of this program; the absolute path of the host Git is
// read from the file git-host-path, one directory above the directory that
// holds the entry. The shim never consults PATH and runs no other program.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const unusable = 127

func main() { os.Exit(run()) }

func run() int {
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "gitshim: cannot locate itself")
		return unusable
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(filepath.Dir(self)), "git-host-path"))
	host := strings.TrimSpace(string(data))
	if err != nil || !filepath.IsAbs(host) {
		fmt.Fprintln(os.Stderr, "gitshim: git-host-path is missing or not an absolute path")
		return unusable
	}
	cmd := exec.Command(host, os.Args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	var exit *exec.ExitError
	switch err := cmd.Run(); {
	case err == nil:
		return 0
	case errors.As(err, &exit):
		return exit.ExitCode()
	default:
		fmt.Fprintln(os.Stderr, "gitshim: cannot start the host git")
		return unusable
	}
}
