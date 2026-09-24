package toolresultacp

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

func TestProcessCancellationReapsShellDescendants(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("requires sh")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", "sleep 30 & wait")
	configureProcess(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancellation succeeded unexpectedly")
		}
	case <-time.After(time.Second * 3):
		t.Fatal("process not reaped")
	}
}
