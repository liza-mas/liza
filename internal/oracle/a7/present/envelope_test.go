package present

import (
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func TestOracleEnvelopeClassification(t *testing.T) {
	skipUnlessLinux(t)
	for _, name := range fixtureLines(t, "classification-cases.txt") {
		t.Run(name, func(t *testing.T) {
			data, ok := envelopeCase(name)
			if !ok {
				t.Fatalf("unknown classification fixture %s", name)
			}
			oracle.Check(t, oracle.JSON, "envelope-classification/"+name+".json", data)
		})
	}
}

func TestOracleEnvelopeCLI(t *testing.T) {
	skipUnlessLinux(t)
	t.Run("project-root", func(t *testing.T) {
		e := newEnvironment(t)
		checkResult(t, e, oracle.JSON, "envelope-cli/project-root.json", runCLI(t, e, e.project, nil, "status", "--json"))
	})
	checkInvocations(t, "envelope-cli", []invocation{
		{"not-found.json", oracle.JSON, []string{"get", "task-unknown", "--json"}},
		{"validation.json", oracle.JSON, []string{"get", "config.mode", "extra", "--json"}},
	})
}
