package process

import (
	"os"
	"testing"

	"github.com/liza-mas/liza/internal/agentlaunch"
	"github.com/liza-mas/liza/internal/providers"
)

// TestMain pins the provider catalog that provider-key derivation reads, so
// tests do not depend on the on-disk cache or the network.
func TestMain(m *testing.M) {
	agentlaunch.UseRuntimeCatalog(providers.EmbeddedCatalog())
	os.Exit(m.Run())
}
