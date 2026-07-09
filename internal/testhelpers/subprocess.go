package testhelpers

import (
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

// AddSlowCommandToPath installs a fake executable that emits one line, then
// sleeps long enough for short-timeout tests to kill it.
func AddSlowCommandToPath(t *testing.T, name string) {
	t.Helper()
	binDir := t.TempDir()
	commandName, content := slowCommandFile(name)
	path := filepath.Join(binDir, commandName)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if runtime.GOOS == "windows" {
		t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")
	}
}

func slowCommandFile(name string) (string, string) {
	if runtime.GOOS == "windows" {
		return name + ".cmd", "@echo off\r\necho started\r\nping -n 3 127.0.0.1 >NUL\r\necho late\r\n"
	}
	return name, "#!/bin/sh\nprintf 'started\\n'\nsleep 2\nprintf 'late\\n'\n"
}
