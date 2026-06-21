package envgate

import (
	"os"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
)

// Truthy reports whether a user-facing environment gate is enabled.
func Truthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true":
		return true
	default:
		return false
	}
}

// Value returns the value for a user-facing LIZA_* env var, honoring the
// branded namespace first and the legacy LIZA_* name as a compatibility alias.
func Value(legacyName string) string {
	return Lookup(legacyName).Value
}

// Lookup returns detailed alias resolution for a user-facing LIZA_* env var.
func Lookup(legacyName string) brand.EnvLookup {
	return brand.LookupEnv(os.Getenv, strings.TrimPrefix(legacyName, "LIZA_"))
}

// TruthyEnv reports whether a user-facing LIZA_* env var is enabled through the
// branded namespace or legacy compatibility alias.
func TruthyEnv(legacyName string) bool {
	return Truthy(Value(legacyName))
}
