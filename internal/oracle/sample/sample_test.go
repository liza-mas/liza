// Package sample holds the harness-proof oracle suite: one binary-driven and
// one adapter-driven golden over surfaces present at the run's starting
// commit. It belongs to no area's surface inventory.
//
// Drivers import only the standard library, the harness and the adapter
// functions of this package; every other import sits in adapter files.
package sample

import (
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func TestMain(m *testing.M) { oracle.RunMain(m) }

// TestOracleSampleHelp drives the built CLI: its help text, human kind.
func TestOracleSampleHelp(t *testing.T) {
	bin := oracle.BuildCLI(t, "./cmd/liza")
	res := oracle.Run(t, bin, []string{"--help"}, oracle.RunOptions{})
	if res.ExitCode != 0 {
		t.Fatalf("--help exited %d", res.ExitCode)
	}
	res.Norm.Check(t, oracle.Human, "help.txt", res.Stdout)
}

// TestOracleSampleErrorEnvelope drives the JSON error envelope through the
// adapter: code and message of a classified untyped error, json kind.
func TestOracleSampleErrorEnvelope(t *testing.T) {
	oracle.Check(t, oracle.JSON, "error-envelope.json", errorEnvelope())
}
