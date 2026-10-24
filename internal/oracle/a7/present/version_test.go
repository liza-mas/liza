package present

import (
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func TestOracleVersion(t *testing.T) {
	skipUnlessLinux(t)
	checkInvocations(t, "version", []invocation{
		{"default.txt", oracle.Human, []string{"version"}},
		{"json.json", oracle.JSON, []string{"version", "--json"}},
	})
}
