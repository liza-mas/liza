//go:build !windows

package subprocess

import (
	"os/exec"
	"testing"
)

func TestSetDetachedProcessGroup_ConfiguresNewSession(t *testing.T) {
	cmd := exec.Command("true")

	SetDetachedProcessGroup(cmd)

	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr = nil, want detached attributes")
	}
	if cmd.SysProcAttr.Setpgid {
		t.Fatal("Setpgid = true, want false")
	}
	if !cmd.SysProcAttr.Setsid {
		t.Fatal("Setsid = false, want true")
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
}
