package testhelpers

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteShellStubPreservesMultiLineArgument is the promise the relay exists
// for: the previous .cmd wrapper delivered only the first line of a multi-line
// argument, which is how a pane script or an agent prompt reaches a stub.
func TestWriteShellStubPreservesMultiLineArgument(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "captured")

	stub := WriteShellStub(t, filepath.Join(dir, "capture"), `#!/bin/sh
printf '%s' "$2" > "$CAPTURE_OUT"
`)

	payload := "set -e\nsecond line\nthird 'quoted' line\n"
	cmd := exec.Command(stub, "-lc", payload)
	cmd.Env = append(os.Environ(), "CAPTURE_OUT="+filepath.ToSlash(outPath))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("run stub: %v\n%s", err, out)
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read captured argument: %v", err)
	}
	if strings.TrimRight(string(got), "\r\n") != strings.TrimRight(payload, "\n") {
		t.Fatalf("stub received %q, want %q", string(got), payload)
	}
}

// TestWriteShellStubReportsExitCode keeps the stub usable for failure paths.
func TestWriteShellStubReportsExitCode(t *testing.T) {
	dir := t.TempDir()
	stub := WriteShellStub(t, filepath.Join(dir, "failing"), "#!/bin/sh\nexit 3\n")

	err := exec.Command(stub).Run()

	var exitErr *exec.ExitError
	if !asExitError(err, &exitErr) {
		t.Fatalf("run stub: got %v, want an exit error", err)
	}
	if exitErr.ExitCode() != 3 {
		t.Fatalf("exit code = %d, want 3", exitErr.ExitCode())
	}
}

func asExitError(err error, target **exec.ExitError) bool {
	exitErr, ok := err.(*exec.ExitError)
	if ok {
		*target = exitErr
	}
	return ok
}
