//go:build windows

package procscan

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestProcessCommandLineMatchesOwnArgs checks the reader against the one
// command line the test can know independently: its own.
func TestProcessCommandLineMatchesOwnArgs(t *testing.T) {
	argv, err := ProcessCommandLine(os.Getpid())
	if err != nil {
		t.Fatalf("ProcessCommandLine(self): %v", err)
	}
	if len(argv) != len(os.Args) {
		t.Fatalf("ProcessCommandLine(self) = %q (%d args), want %d args to match os.Args %q",
			argv, len(argv), len(os.Args), os.Args)
	}
	if filepath.Base(argv[0]) != filepath.Base(os.Args[0]) {
		t.Fatalf("ProcessCommandLine(self)[0] = %q, want the same image as os.Args[0] %q", argv[0], os.Args[0])
	}
	for i := 1; i < len(argv); i++ {
		if argv[i] != os.Args[i] {
			t.Fatalf("ProcessCommandLine(self)[%d] = %q, want %q", i, argv[i], os.Args[i])
		}
	}
}

// TestProcessCommandLineReadsAnotherProcess covers the case the identity check
// actually uses: a process this one did not start from Go's own arguments.
func TestProcessCommandLineReadsAnotherProcess(t *testing.T) {
	cmd := exec.Command("ping", "-n", "60", "127.0.0.1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start process: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	argv, err := ProcessCommandLine(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("ProcessCommandLine(%d): %v", cmd.Process.Pid, err)
	}
	if len(argv) != 4 {
		t.Fatalf("ProcessCommandLine(%d) = %q, want 4 arguments", cmd.Process.Pid, argv)
	}
	if !strings.EqualFold(filepath.Base(argv[0]), "ping.exe") && !strings.EqualFold(filepath.Base(argv[0]), "ping") {
		t.Fatalf("argv[0] = %q, want the ping image", argv[0])
	}
	for i, want := range []string{"-n", "60", "127.0.0.1"} {
		if argv[i+1] != want {
			t.Fatalf("argv[%d] = %q, want %q", i+1, argv[i+1], want)
		}
	}
}

func TestProcessCommandLineRejectsInvalidPID(t *testing.T) {
	if _, err := ProcessCommandLine(0); err == nil {
		t.Fatal("ProcessCommandLine(0) = nil error, want a rejection")
	}
	if _, err := ProcessCommandLine(-1); err == nil {
		t.Fatal("ProcessCommandLine(-1) = nil error, want a rejection")
	}
}
