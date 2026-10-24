package present

import (
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func nonemptyLines(text string) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func TestOracleHelp(t *testing.T) {
	skipUnlessLinux(t)
	cases := []invocation{
		{"root.txt", oracle.Human, []string{"--help"}},
		{"help-command.txt", oracle.Human, []string{"help"}},
	}
	for _, path := range fixtureLines(t, "help-paths.txt") {
		cases = append(cases, invocation{strings.ReplaceAll(path, " ", "-") + ".txt", oracle.Human, append(strings.Fields(path), "--help")})
	}
	checkInvocations(t, "help", cases)
}
