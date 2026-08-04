package testhelpers

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/subprocess"
)

// WithShortSubprocessTimeout temporarily lowers advisory subprocess limits so
// timeout behavior can be tested without slowing the suite.
func WithShortSubprocessTimeout(t *testing.T, timeout time.Duration) {
	t.Helper()
	previousTimeout := subprocess.DefaultCommandTimeout
	previousWaitDelay := subprocess.DefaultWaitDelay
	subprocess.DefaultCommandTimeout = timeout
	subprocess.DefaultWaitDelay = timeout
	t.Cleanup(func() {
		subprocess.DefaultCommandTimeout = previousTimeout
		subprocess.DefaultWaitDelay = previousWaitDelay
	})
}

// AddSlowCommandToPathWithDelay installs a fake executable with a caller-set
// delay before its late output so timeout tests can allow for suite load.
func AddSlowCommandToPathWithDelay(t *testing.T, name string, delay time.Duration) {
	t.Helper()
	binDir := t.TempDir()
	commandName, content := slowCommandFile(name, delay)
	path := filepath.Join(binDir, commandName)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if runtime.GOOS == "windows" {
		t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")
	}
}

func slowCommandFile(name string, delay time.Duration) (string, string) {
	delaySeconds := int((delay + time.Second - 1) / time.Second)
	if delaySeconds < 1 {
		delaySeconds = 1
	}
	if runtime.GOOS == "windows" {
		return name + ".cmd", fmt.Sprintf("@echo off\r\necho started\r\nping -n %d 127.0.0.1 >NUL\r\necho late\r\n", delaySeconds+1)
	}
	return name, fmt.Sprintf("#!/bin/sh\nprintf 'started\\n'\nsleep %d\nprintf 'late\\n'\n", delaySeconds)
}
