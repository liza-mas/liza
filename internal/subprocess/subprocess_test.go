package subprocess

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCombinedOutputWithTimeoutKillsHungCommand(t *testing.T) {
	const timeout = 5 * time.Second

	binDir := t.TempDir()
	commandName, content := slowCommandFile("slow-tool")
	fakeCommand := filepath.Join(binDir, commandName)
	if err := os.WriteFile(fakeCommand, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", fakeCommand, err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if runtime.GOOS == "windows" {
		t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")
	}
	previousWaitDelay := DefaultWaitDelay
	DefaultWaitDelay = 500 * time.Millisecond
	t.Cleanup(func() { DefaultWaitDelay = previousWaitDelay })

	start := time.Now()
	output, err := CombinedOutputWithTimeout(timeout, "slow-tool", []string{"arg"}, t.TempDir())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("CombinedOutputWithTimeout() error = nil, want timeout")
	}
	var timeoutErr *TimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("CombinedOutputWithTimeout() error = %T %v, want *TimeoutError", err, err)
	}
	if elapsed >= timeout+2*time.Second {
		t.Fatalf("CombinedOutputWithTimeout() elapsed = %s, want under %s", elapsed, timeout+2*time.Second)
	}
	if !strings.Contains(output, "started") {
		t.Fatalf("output = %q, want pre-timeout output", output)
	}
	if strings.Contains(output, "late") {
		t.Fatalf("output = %q, want process killed before late output", output)
	}
	if timeoutErr.Name != "slow-tool" || timeoutErr.Timeout != timeout {
		t.Fatalf("timeout error = %#v, want command facts", timeoutErr)
	}
}

func slowCommandFile(name string) (string, string) {
	if runtime.GOOS == "windows" {
		return name + ".cmd", "@echo off\r\necho started\r\nping -n 16 127.0.0.1 >NUL\r\necho late\r\n"
	}
	return name, "#!/bin/sh\nprintf 'started\\n'\nsleep 15\nprintf 'late\\n'\n"
}
