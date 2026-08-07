package testhelpers

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// WriteShellStub installs a fake command that production code can find on PATH
// and execute by name.
//
// Tests stand in for wezterm, codex, acpx, stacklit and friends with a small
// POSIX shell script. That works directly on Unix, but on Windows a file has to
// carry a PATHEXT extension for exec.LookPath to consider it, and the OS cannot
// execute a shebang script in any case. So on Windows the script is written at
// the exact path given — a POSIX shell looking up the bare name still finds it —
// and a "<name>.exe" relay hands it to Git Bash so exec.LookPath finds it too.
// Callers keep writing one portable shell script and pass the extensionless path.
//
// The relay is a real executable rather than a .cmd wrapper because cmd.exe
// cannot carry a newline inside a single argument: a stub handed a multi-line
// payload would receive only its first line.
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

	// The script keeps the bare name so a POSIX shell can still resolve it:
	// production code reached through Go needs the PATHEXT wrapper below, but a
	// stub invoked from inside a shell script — a git hook calling pre-commit,
	// say — is looked up by the exact name, and sh appends .exe at most, never
	// .cmd. Writing both forms serves either caller.
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		t.Fatalf("write shell stub %s: %v", path, err)
	}

	// The relay reads the shell to use from a file beside the script, so the
	// resolution done here — Git Bash, not the WSL launcher — is the one that
	// applies at run time, whatever PATH the test installs.
	if err := os.WriteFile(path+".bash", []byte(ResolveBashForScripts(t)), 0o644); err != nil {
		t.Fatalf("write shell stub shell path %s: %v", path+".bash", err)
	}

	relayPath := path + ".exe"
	if err := copyFile(stubRelayBinary(t), relayPath); err != nil {
		t.Fatalf("install shell stub relay %s: %v", relayPath, err)
	}
	return relayPath
}

// ShellArg renders a value the way the generated shell scripts do: bare unless
// it carries a metacharacter, quoted otherwise.
//
// Tests that assert on a generated script cannot hardcode either form. A native
// Windows path carries a backslash, so the same value appears quoted there and
// bare on Unix, and an expectation built by concatenation matches neither.
func ShellArg(value string) string {
	if value != "" && !strings.ContainsAny(value, " \t\n'\"\\$`;&|<>*?!()[]{}") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

var stubRelay struct {
	once sync.Once
	path string
	err  error
}

// stubRelayBinary builds the relay once per test binary and returns its path.
func stubRelayBinary(t *testing.T) string {
	t.Helper()

	repoRoot := FindRepoRoot(t)

	stubRelay.once.Do(func() {
		var dir string
		dir, stubRelay.err = os.MkdirTemp("", "liza-stub-relay-*")
		if stubRelay.err != nil {
			return
		}
		binary := filepath.Join(dir, "stubrelay.exe")
		build := exec.Command("go", "build", "-o", binary, "./internal/testhelpers/stubrelay")
		build.Dir = repoRoot
		if out, err := build.CombinedOutput(); err != nil {
			stubRelay.err = fmt.Errorf("build stub relay: %w\n%s", err, out)
			return
		}
		stubRelay.path = binary
	})

	if stubRelay.err != nil {
		t.Fatalf("shell stub relay unavailable: %v", stubRelay.err)
	}
	return stubRelay.path
}

func copyFile(src, dst string) error {
	content, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, content, 0o755)
}
