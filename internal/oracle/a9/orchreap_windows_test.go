//go:build windows

package a9

import "os/exec"

// Every A9 test skips on Windows (AC4); these keep the package compiling.
func orchDetach(*exec.Cmd) {}
func orchKillGroup(int)    {}
