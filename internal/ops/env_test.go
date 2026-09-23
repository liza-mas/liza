package ops

import (
	"os"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/envgate"
	"github.com/liza-mas/liza/internal/functionalclusters"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/scipsearch"
	"github.com/liza-mas/liza/internal/semble"
	"github.com/liza-mas/liza/internal/stacklit"
)

func TestMain(m *testing.M) {
	clearAmbientFeatureGates()
	os.Exit(m.Run())
}

func clearAmbientFeatureGates() {
	for _, gateName := range []string{
		models.EnvEnableCopyWorktreeEnvFiles,
		scipsearch.EnvEnableScipSearch,
		semble.EnvEnableSemble,
		stacklit.EnvEnableStacklit,
		functionalclusters.EnvEnableFunctionalClusters,
	} {
		suffix := strings.TrimPrefix(gateName, brand.RuntimeValues().EnvPrefix+"_")
		suffix = strings.TrimPrefix(suffix, brand.LegacyEnvName(""))
		_ = os.Unsetenv(brand.EnvName(suffix))
		_ = os.Unsetenv(brand.LegacyEnvName(suffix))
		_ = os.Unsetenv(gateName)
	}
}

// requireFeatureGateUnset checks that TestMain left a feature gate unset, so a
// test can rely on the disabled default without t.Setenv and still run under
// t.Parallel.
func requireFeatureGateUnset(t *testing.T, name string) {
	t.Helper()
	if lookup := envgate.Lookup(name); lookup.Source != "" {
		t.Fatalf("%s is set via %s; TestMain must clear ambient feature gates", name, lookup.Source)
	}
}
