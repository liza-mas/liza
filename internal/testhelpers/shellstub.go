package testhelpers

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// WriteShellStub installs a fake command that production code can find on PATH
// and execute by name.
//
// Tests stand in for wezterm, codex, acpx, stacklit and friends with a small
// POSIX shell script. That works directly on Unix, but on Windows a file has to
// carry a PATHEXT extension for exec.LookPath to consider it, and the OS cannot
// execute a shebang script in any case. So on Windows the script is written
// beside the command as "<name>.sh" and a "<name>.cmd" wrapper hands it to Git
// Bash, forwarding arguments, stdin and the exit code. Callers keep writing one
// portable shell script and pass the extensionless path.
//
// Pass the command path without an extension, e.g.
// filepath.Join(binDir, "wezterm"). The returned path is the one that can
// actually be executed: identical to path on Unix, the .cmd wrapper on Windows.
// Callers that resolve the stub through PATH can ignore it; callers that hand
// the stub's absolute path to something else must use it, since no file exists
// at the extensionless path on Windows.
func WriteShellStub(t *testing.T, path, script string) string {
	t.Helper()

	if runtime.GOOS != "windows" {
		if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
			t.Fatalf("write shell stub %s: %v", path, err)
		}
		return path
	}

	scriptPath := path + ".sh"
	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		t.Fatalf("write shell stub %s: %v", scriptPath, err)
	}

	// %* forwards the arguments verbatim; cmd.exe propagates the exit code of
	// the last command, so a failing stub still reports failure.
	wrapper := fmt.Sprintf("@echo off\r\n\"%s\" \"%s\" %%*\r\n",
		ResolveBashForScripts(t), filepath.ToSlash(scriptPath))
	wrapperPath := path + ".cmd"
	if err := os.WriteFile(wrapperPath, []byte(wrapper), 0o644); err != nil {
		t.Fatalf("write shell stub wrapper %s: %v", wrapperPath, err)
	}
	return wrapperPath
}
