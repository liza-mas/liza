//go:build !windows

package toolresult

import (
	"os/exec"
	"syscall"
	"testing"
)

func TestRunExitCodeOfSignaledProcessAddsSignalBase(t *testing.T) {
	if signalExitCodeBase != 128 {
		t.Fatalf("signalExitCodeBase = %d, want 128", signalExitCodeBase)
	}
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("sleep unavailable: %v", err)
	}
	if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if got, want := runExitCode(cmd), 128+int(syscall.SIGKILL); got != want {
		t.Fatalf("runExitCode = %d, want %d", got, want)
	}
}
