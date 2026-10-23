package present

import (
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func TestOracleUsageErrors(t *testing.T) {
	skipUnlessLinux(t)
	checkInvocations(t, "usage", []invocation{
		{"unknown-command.txt", oracle.Human, []string{"oracle-unknown-command"}},
		{"unknown-flag-json.txt", oracle.Human, []string{"status", "--oracle-unknown-flag", "--json"}},
	})
}
