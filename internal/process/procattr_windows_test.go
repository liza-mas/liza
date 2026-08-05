//go:build windows

package process

import (
	"os/exec"
	"syscall"
	"testing"
)

func TestSetDetachedProcessGroup_SetsNewProcessGroupFlag(t *testing.T) {
	cmd := exec.Command("cmd", "/c", "exit", "0")

	SetDetachedProcessGroup(cmd)

	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr = nil, want detached attributes")
	}
	if cmd.SysProcAttr.CreationFlags&syscall.CREATE_NEW_PROCESS_GROUP == 0 {
		t.Fatalf("CreationFlags = %#x, want CREATE_NEW_PROCESS_GROUP set", cmd.SysProcAttr.CreationFlags)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
}

// TestSetDetachedProcessGroup_PreservesExistingAttributes guards the merge: a
// caller that set its own SysProcAttr should keep it.
func TestSetDetachedProcessGroup_PreservesExistingAttributes(t *testing.T) {
	cmd := exec.Command("cmd", "/c", "exit", "0")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	SetDetachedProcessGroup(cmd)

	if !cmd.SysProcAttr.HideWindow {
		t.Fatal("HideWindow = false, want the caller's value preserved")
	}
	if cmd.SysProcAttr.CreationFlags&syscall.CREATE_NEW_PROCESS_GROUP == 0 {
		t.Fatalf("CreationFlags = %#x, want CREATE_NEW_PROCESS_GROUP set", cmd.SysProcAttr.CreationFlags)
	}
}
