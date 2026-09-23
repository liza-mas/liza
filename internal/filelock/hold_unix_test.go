//go:build darwin || dragonfly || freebsd || illumos || linux || netbsd || openbsd

package filelock

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A child that inherits the held descriptor keeps the lock after the holder's
// own copy is gone, which is what happens when the holder dies mid-run.
func TestTryHoldInheritedDescriptorOutlivesTheHolder(t *testing.T) {
	fl := New(filepath.Join(t.TempDir(), "index-refresh"))
	held, acquired, err := fl.TryHold("owner")
	if err != nil || !acquired {
		t.Fatalf("TryHold() = (%v, %v), want acquired", acquired, err)
	}

	// The child blocks reading stdin, so closing the pipe releases it, and a
	// failing test cannot leave it running.
	child := exec.Command("sh", "-c", "read _ || true")
	child.ExtraFiles = []*os.File{held.File()}
	stdin, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	waited := false
	t.Cleanup(func() {
		_ = stdin.Close()
		if !waited {
			_ = child.Wait()
		}
	})
	// Drop the holder's copy without unlocking, as process death would.
	if err := held.File().Close(); err != nil {
		t.Fatalf("close holder descriptor: %v", err)
	}

	if _, again, err := fl.TryHold("contender"); err != nil || again {
		t.Fatalf("TryHold() while child holds = (%v, %v), want busy", again, err)
	}

	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	waited = true
	if err := child.Wait(); err != nil {
		t.Fatalf("child: %v", err)
	}
	next, acquired, err := fl.TryHold("after child")
	if err != nil || !acquired {
		t.Fatalf("TryHold() after child exit = (%v, %v), want acquired", acquired, err)
	}
	if err := next.Release(); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
}
