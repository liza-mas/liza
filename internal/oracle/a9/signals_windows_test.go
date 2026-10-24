//go:build windows

package a9

import "os/exec"

// The package skips on Windows (unixOnly), so none of this is reached.
type signalProc struct{ pid, ppid, pgid int }

func signalIsolate(*exec.Cmd)            {}
func signalReap(int) bool                { return true }
func signalDescendants(int) []signalProc { return nil }
func signalPidLive(int) bool             { return false }
