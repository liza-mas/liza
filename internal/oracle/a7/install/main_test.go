// Package install freezes default-brand installation and state effects on Linux.
// Drivers use disposable roots, restricted PATH and explicit child environments.
// Setup uses --yes; workspace init requires a prior setup; re-init refusal omits
// --yes so that stdin can decline deletion. Kimi setup records its refusal.
// Codex and Cursor activation omit host-dependent Codex sandbox stderr (AC5).
//
// Artifacts use path-only substitution before hashing. Runtime state retains a
// state-golden marker (D29); its bytes are asserted by a separate state pair.
// Locks, log.yaml, alerts.log and the home .liza/cache tree are excluded (AC6).
// Goldens are generated only by regen write-new against the starting tree;
// live tests compare and never create them. Umask 022 fixes installed modes.
// D37 collapses horizontal border runs only on box-drawing-only human lines,
// after harness path normalization; text and all other characters stay intact.
package install

import (
	"runtime"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func TestMain(m *testing.M) {
	setUmask()
	oracle.RunMain(m)
}

func skipUnlessLinux(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("A7 installed-tree modes and symlinks are frozen on Linux only (AC1)")
	}
}

var providers = []string{"claude", "codex", "cursor", "opencode", "copilot", "gemini", "mistral", "pi"}
