package agent

import (
	"os"
	"testing"

	"github.com/liza-mas/liza/internal/agentlaunch"
	"github.com/liza-mas/liza/internal/providers"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// TestMain pins the provider catalog to the embedded copy for this package.
//
// The runtime catalog is memoised from providers.Load, which resolves through
// the on-disk cache and then the published catalog over the network. Tests that
// assert built-in launch shapes would otherwise pass or fail according to cache
// freshness and what is currently published, rather than according to the
// catalog in this worktree. Pinning it before any test runs keeps the result
// independent of test order and the package off the network. Cache and refresh
// behavior is owned and tested by internal/providers.
//
// It also switches the index env gates off, since developer shells set them and
// CI does not: with them on, every orchestrator supervisor started here would
// install index hooks and plan languages in its temp repository. Tests that
// exercise index activation opt in with t.Setenv.
func TestMain(m *testing.M) {
	agentlaunch.UseRuntimeCatalog(providers.EmbeddedCatalog())
	testhelpers.DisableIndexEnvGates()
	os.Exit(m.Run())
}
