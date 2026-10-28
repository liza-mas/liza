package a9

import (
	"os"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func TestMain(m *testing.M) {
	if os.Getenv("ORACLE_A9_STUB") != "" {
		os.Exit(stubMain())
	}
	oracle.RunMain(m)
}
