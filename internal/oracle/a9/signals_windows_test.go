//go:build windows

package a9

import "os/exec"

// The package skips on Windows (unixOnly), so none of this is reached.
type signalOwner struct {
	sid  int
	home string
}

func signalIsolate(*exec.Cmd, string) {}
func signalSubreaper() error          { return nil }
func signalReap(signalOwner) bool     { return true }
