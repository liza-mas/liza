//go:build !linux

package a9

import "testing"

// Hosts without a child subreaper cannot adopt orphans (decision D45); orchStart
// terminates each supervisor's process group instead. Windows skips every A9 test
// (AC4); these keep the package compiling there.

type orchChild struct {
	pid    int
	zombie bool
}

func orchSubreaper(testing.TB)  {}
func orchReap(testing.TB)       {}
func orchChildren() []orchChild { return nil }
func orchKillPID(int)           {}
