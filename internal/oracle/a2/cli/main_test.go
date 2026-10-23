//go:build linux

package cli

import (
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

// Goldens are created only by the starting-tree entry point:
// go run ./internal/testhelpers/oracle/cmd/regen -mode write-new -pkg ./internal/oracle/a2/cli/...
// The same filter with -mode check proves their provenance from 96c9ad69a.
func TestMain(m *testing.M) { oracle.RunMain(m) }
