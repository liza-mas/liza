//go:build windows

package subprocess

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"
)

// firstWrite closes ready on the command's first output.
type firstWrite struct {
	once  sync.Once
	ready chan struct{}
}

func (w *firstWrite) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.ready) })
	return len(p), nil
}

// A grandchild that holds the output pipe would keep Wait blocked until
// WaitDelay: cancellation must kill it through the job, without taskkill.
func TestStartCancellationTerminatesTheGrandchildThroughTheJob(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// cmd.exe prints, then waits for ping.exe, its child, for ~30s.
	cmd := exec.CommandContext(ctx, "cmd.exe", "/c", "echo ready& ping -n 30 127.0.0.1")
	ConfigureCancellation(cmd)
	cmd.Cancel = func() error {
		t.Error("cancellation fell back to taskkill: the job did not terminate the tree")
		return os.ErrProcessDone
	}
	out := &firstWrite{ready: make(chan struct{})}
	cmd.Stdout = out

	if err := Start(cmd); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	select {
	case <-out.ready:
	case <-time.After(10 * time.Second):
		t.Fatal("cmd.exe printed nothing within 10s")
	}
	cancel()
	start := time.Now()
	err := cmd.Wait()
	if errors.Is(err, exec.ErrWaitDelay) {
		t.Fatalf("Wait() error = %v: the grandchild kept the output pipe open", err)
	}
	if err == nil {
		t.Fatal("Wait() error = nil, want the cancellation error")
	}
	if elapsed := time.Since(start); elapsed >= DefaultWaitDelay {
		t.Fatalf("Wait() took %s, want under WaitDelay %s", elapsed, DefaultWaitDelay)
	}
}

// Without CommandContext there is nothing to cancel: Start must not set Cancel,
// which exec.Cmd.Start rejects on such a command.
func TestStartWithoutContextStartsPlainly(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "exit", "0")
	if err := Run(cmd); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if cmd.Cancel != nil {
		t.Fatal("Cancel set on a command without a context")
	}
}
