package a6

import (
	"os"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func TestMain(m *testing.M) {
	if os.Getenv("ORACLE_A6_HELPER") == "integration" {
		if len(os.Args) != 2 {
			os.Exit(2)
		}
		os.Exit(integrationHelper(os.Args[1]))
	}
	oracle.RunMain(m)
}
