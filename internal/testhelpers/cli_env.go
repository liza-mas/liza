package testhelpers

import (
	"testing"

	"github.com/liza-mas/liza/internal/brand"
)

// ClearCLIEnv isolates the default CLI chain from the developer's shell.
func ClearCLIEnv(t *testing.T) {
	t.Helper()
	for _, suffix := range []string{"DEFAULT_CLI", "DEFAULT_DOER_CLI", "DEFAULT_REVIEWER_CLI"} {
		t.Setenv(brand.EnvName(suffix), "")
		t.Setenv(brand.LegacyEnvName(suffix), "")
	}
}
