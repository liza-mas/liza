package testhelpers

import (
	"os"

	"github.com/liza-mas/liza/internal/brand"
)

// indexEnvGateSuffixes names the index feature gates by suffix. testhelpers
// cannot import stacklit, scipsearch or functionalclusters to read their
// constants, since those packages' own tests import testhelpers, so the
// suffixes are repeated here. TestIndexEnvGateNamesMatchPackageGates in
// internal/commands keeps them in step.
var indexEnvGateSuffixes = []string{"ENABLE_STACKLIT", "ENABLE_SCIP_SEARCH", "ENABLE_FUNCTIONAL_CLUSTERS"}

// IndexEnvGateNames returns the branded env names of the index feature gates.
func IndexEnvGateNames() []string {
	names := make([]string, 0, len(indexEnvGateSuffixes))
	for _, suffix := range indexEnvGateSuffixes {
		names = append(names, brand.EnvName(suffix))
	}
	return names
}

// DisableIndexEnvGates switches the index feature gates off for this process
// and returns a function that restores them.
//
// Developer shells set these gates and CI does not, so a suite that leaves them
// alone installs index hooks and plans languages on one machine but not the
// other. Each branded name is set to empty rather than unset: envgate treats a
// present-but-empty branded name as off, whereas an unset one falls back to the
// legacy LIZA_* alias, which a branded build would then read as on.
func DisableIndexEnvGates() (restore func()) {
	type previous struct {
		value   string
		present bool
	}
	names := IndexEnvGateNames()
	saved := make(map[string]previous, len(names))
	for _, name := range names {
		value, present := os.LookupEnv(name)
		saved[name] = previous{value: value, present: present}
		_ = os.Setenv(name, "")
	}
	return func() {
		for name, prev := range saved {
			if prev.present {
				_ = os.Setenv(name, prev.value)
			} else {
				_ = os.Unsetenv(name)
			}
		}
	}
}
