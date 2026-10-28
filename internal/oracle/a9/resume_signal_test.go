package a9

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Resume signal cleanup. `liza resume` on a paused system sweeps the provider
// signal files: an expired quota signal is cleared, an unexpired one is kept and
// reported, and a provider-unavailable signal is cleared. The fixture holds one
// of each, so the human output, the state and the remaining signal listing pin
// the presentation-side cleanup that the signal extraction touches.
func TestOracleA9ResumeSignalCleanup(t *testing.T) {
	unixOnly(t)
	p := newScenario(t, "i27-resume-signals")
	signalSeed(t, p)
	p.beforeListing = p.listing(t)
	r := p.run(t, "resume")
	requireCode(t, p, r, 0)
	for _, want := range []string{
		"Cleared expired quota signal for provider: claude",
		"Kept quota signal for provider: codex until ",
		"Cleared provider-unavailable signal for provider: codex",
	} {
		if !strings.Contains(r.stdout, want) {
			t.Fatalf("A9 %s run: resume output lacks %q", p.name, want)
		}
	}
	remaining := signalFiles(p)
	sort.Strings(remaining)
	if len(remaining) != 1 || remaining[0] != "provider-quota-exhausted-codex" {
		t.Fatalf("A9 %s collect: remaining signal files %v", p.name, remaining)
	}
	if _, err := os.Stat(filepath.Join(p.dir, ".liza", "provider-quota-exhausted-codex")); err != nil {
		t.Fatalf("A9 %s collect: the unexpired quota signal must remain", p.name)
	}
	launchHuman(t, p, r)
	p.check(t, r)
}
