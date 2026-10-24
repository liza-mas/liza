//go:build windows

package a9

import "os/exec"

func orchDetach(*exec.Cmd) {}
func orchKillGroup(int)    {}
