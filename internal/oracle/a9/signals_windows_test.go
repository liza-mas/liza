//go:build windows

package a9

import "os/exec"

// The package skips on Windows (unixOnly), so process groups are never used.
func signalIsolate(*exec.Cmd)   {}
func signalKillGroup(int)       {}
func signalGroupAlive(int) bool { return false }
