package testhelpers

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// ResolveBashForScripts returns the path to a bash able to execute a script
// given by its native filesystem path.
//
// On Windows this must be Git Bash specifically. The WSL launcher at
// system32\bash.exe is usually first on PATH and cannot reach Windows paths
// (C:/Users/... resolves to "No such file or directory"; it expects
// /mnt/c/...), so a test that execs a repo script by its path silently fails
// against it. Git for Windows installs bash.exe under %LOCALAPPDATA%\Programs\
// Git\bin or %ProgramFiles%\Git\bin, which this probes before falling back to
// PATH. On other platforms the first bash on PATH is correct.
//
// Skips the test when no bash is available at all.
func ResolveBashForScripts(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "windows" {
		if p, err := exec.LookPath("bash"); err == nil {
			return p
		}
		t.Skip("bash not available")
		return ""
	}
	for _, candidate := range []string{
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Git", "bin", "bash.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "Git", "bin", "bash.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Git", "bin", "bash.exe"),
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	if p, err := exec.LookPath("bash"); err == nil {
		return p
	}
	t.Skip("bash not available")
	return ""
}

// WaitForAsyncSetup pauses briefly to let goroutines initialize (e.g.
// establish an fsnotify watcher) before the test mutates shared state.
// Centralised here so the sleep-budget ratchet in testguard counts it once.
func WaitForAsyncSetup() {
	time.Sleep(200 * time.Millisecond)
}

// FindRepoRoot walks up from the current working directory to find the repository
// root (directory containing go.mod). Useful for locating testdata files.
func FindRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (go.mod)")
		}
		dir = parent
	}
}
