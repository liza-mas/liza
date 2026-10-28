package present

import (
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func TestOracleCompletion(t *testing.T) {
	skipUnlessLinux(t)
	var cases []invocation
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		cases = append(cases, invocation{shell, oracle.Artifact, []string{"completion", shell}})
	}
	for _, line := range fixtureLines(t, "complete-cases.txt") {
		fields := strings.Fields(line)
		args := append([]string{"__complete"}, fields[1:]...)
		args = append(args, "")
		cases = append(cases, invocation{fields[0] + ".txt", oracle.Human, args})
	}
	checkInvocations(t, "completion", cases)
}
