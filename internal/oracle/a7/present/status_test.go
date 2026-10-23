package present

import (
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func TestOracleStatus(t *testing.T) {
	skipUnlessLinux(t)
	checkInvocations(t, "status", []invocation{
		{"default.txt", oracle.Human, []string{"status"}},
		{"json.json", oracle.JSON, []string{"status", "--json"}},
		{"format-json.json", oracle.JSON, []string{"status", "--format", "json"}},
		{"format-yaml.txt", oracle.Human, []string{"status", "--format", "yaml"}},
		{"detailed.txt", oracle.Human, []string{"status", "--detailed"}},
		{"detailed-json.json", oracle.JSON, []string{"status", "--detailed", "--json"}},
	})
	t.Run("refusal-non-repository", func(t *testing.T) {
		e := newEnvironment(t)
		checkResult(t, e, oracle.JSON, "status/refusal-non-repository.json", runCLI(t, e, e.project, nil, "-C", e.project, "status", "--json"))
	})
}
