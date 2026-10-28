//go:build windows

package a9

import (
	"os/exec"
	"sync/atomic"
)

// The package skips on Windows (unixOnly), so none of this is reached.
func signalIsolate(*exec.Cmd) *atomic.Bool { return new(atomic.Bool) }
func signalSubreaper() error               { return nil }
func signalReap(int) bool                  { return true }
