package a8

import (
	"runtime"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

// TestOracleStatelessWake freezes each wake instruction template rendered
// without run data, the surface the role budget measures.
func TestOracleStatelessWake(t *testing.T) {
	triggers := statelessWakeTriggers()
	if len(triggers) == 0 {
		t.Fatal("no wake triggers to render")
	}
	for _, trigger := range triggers {
		t.Run(trigger, func(t *testing.T) {
			rendered, err := renderStatelessWake(trigger)
			if err != nil {
				t.Fatalf("%s: render: %v", trigger, err)
			}
			if strings.TrimSpace(rendered) == "" {
				t.Fatalf("%s: stateless render is empty", trigger)
			}
			oracle.Check(t, oracle.Prompt, "stateless_"+trigger+".txt", []byte(rendered))
		})
	}
}

// TestOracleBrandedOrchestrator renders the INITIAL_PLANNING and
// PLANNING_COMPLETE orchestrator fixtures under a non-default brand. It must
// not run in parallel: the adapter swaps package-level brand values.
func TestOracleBrandedOrchestrator(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("prompt goldens embed POSIX native paths")
	}
	wanted := map[string]bool{"W01": true, "W06": true}
	rendered := 0
	for _, fixture := range loadFixtures(t) {
		if !wanted[fixture.ID] {
			continue
		}
		rendered++
		t.Run(fixture.ID, func(t *testing.T) {
			restore := useAcmeBrand()
			defer restore()
			prompt := renderFixture(t, fixture)
			for _, marker := range fixture.Markers {
				if !strings.Contains(prompt, marker) {
					t.Fatalf("%s: missing branch marker %q", fixture.ID, marker)
				}
			}
			if !strings.Contains(prompt, "acme ") {
				t.Fatalf("%s: render carries no non-default binary name", fixture.ID)
			}
			oracle.Check(t, oracle.Prompt, "branded_"+fixture.ID+".txt", []byte(prompt))
		})
	}
	if rendered != len(wanted) {
		t.Fatalf("rendered %d branded fixtures, want %d", rendered, len(wanted))
	}
}
