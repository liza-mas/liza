package commands

import (
	"os"
	"testing"

	"github.com/liza-mas/liza/internal/agentlaunch"
	"github.com/liza-mas/liza/internal/providers"
)

// TestMain makes init's pre-commit executable check hermetic: developer shells
// usually have pre-commit on PATH and CI runners do not. Tests of the check
// itself install their own lookup. It also pins the provider catalog that
// provider-key derivation reads, so tests do not depend on the on-disk cache or
// the network.
func TestMain(m *testing.M) {
	agentlaunch.UseRuntimeCatalog(providers.EmbeddedCatalog())
	restore := SetInitPreCommitLookPathForTest(func(name string) (string, error) { return "/stub/" + name, nil })
	code := m.Run()
	restore()
	os.Exit(code)
}
