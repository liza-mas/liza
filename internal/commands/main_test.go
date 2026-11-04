package commands

import (
	"os"
	"testing"

	"github.com/liza-mas/liza/internal/agentlaunch"
	"github.com/liza-mas/liza/internal/providers"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// TestMain makes init's pre-commit executable check hermetic: developer shells
// usually have pre-commit on PATH and CI runners do not. Tests of the check
// itself install their own lookup. It also pins the provider catalog that
// provider-key derivation reads, so tests do not depend on the on-disk cache or
// the network. It switches the index env gates off, since developer shells set
// them and CI does not: with them on, worktree removals here launch the host's
// real semble binary. Tests that exercise index activation opt in with t.Setenv.
func TestMain(m *testing.M) {
	agentlaunch.UseRuntimeCatalog(providers.EmbeddedCatalog())
	testhelpers.DisableIndexEnvGates()
	restore := SetInitPreCommitLookPathForTest(func(name string) (string, error) { return "/stub/" + name, nil })
	code := m.Run()
	restore()
	os.Exit(code)
}
