//go:build linux

package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/pairingindex"
)

// Fixture preparation only: no production output assertions or command calls
// belong in this adapter. If these APIs move, retain this body verbatim in an
// oraclestart twin before editing the live call site.
func installIndexActivation(t *testing.T, f *fixture) {
	t.Helper()
	// The adapter's Git helpers read process environment. Run serially in the
	// same explicit environment as the child, then restore every parent value.
	previous := os.Environ()
	defer func() {
		os.Clearenv()
		for _, entry := range previous {
			name, value, _ := strings.Cut(entry, "=")
			must(t, os.Setenv(name, value))
		}
	}()
	env := f.env()
	os.Clearenv()
	for _, entry := range env {
		name, value, _ := strings.Cut(entry, "=")
		must(t, os.Setenv(name, value))
	}
	plan, err := pairingindex.PlanActivation(pairingindex.ActivationPlanOptions{RepoRoot: f.project, EnableStacklit: true})
	must(t, err)
	_, err = pairingindex.InstallActivation(plan.Install)
	must(t, err)
}
