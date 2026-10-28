package a4

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

const (
	driftSourcePath  = "specs/drift/source.md"
	driftCarrierPath = "specs/drift/plan.md"
	driftSourceV1    = "# Source\n\n## Counters\nCount observations.\n\n## Other\nUnrelated.\n"
	driftSourceV2    = "# Source\n\n## Counters\nCount observations.\nAlso count coverage.\n\n## Other\nUnrelated.\n"
)

// driftCarrier is a plan carrier whose one obligation rests on a Counters
// section pinned at revision.
func driftCarrier(revision string) string {
	return fmt.Sprintf("# Plan\n\n## Task 1\nDo the work.\n\n## Source References\nSource revision: %q\n\n"+
		"### Direct References\n- %q: %q @ %q\n\n### Obligation Coverage\n- %q -> %q\n",
		revision, "counters", driftSourcePath+"#Counters", revision, "AC-1", "counters")
}

// driftCommit commits one file with the fixture's deterministic identity and
// records its commit under label, so the normaliser names it.
func driftCommit(t *testing.T, f *fixture, label, path, content string) string {
	t.Helper()
	writeFile(t, filepath.Join(f.root, path), []byte(content))
	f.git(t, "add", path)
	f.git(t, "commit", "-m", label)
	f.commits[label] = f.git(t, "rev-parse", "HEAD")
	return f.commits[label]
}

// TestOracleObligationDrift pins the anomaly records RecordObligationContentDrift
// writes for a live child whose acceptance source names a committed carrier:
// a section edited under an unchanged pin, a carrier re-pinned onto changed
// content, and a merge that moves nothing the obligation rests on. The driver
// commits the carriers after the fixture is built, because their pins are the
// commits it creates; the integration ref is advanced to the last of them
// before the snapshot, never by the operation under test.
func TestOracleObligationDrift(t *testing.T) {
	gate(t)
	for _, c := range []struct {
		name, summary string
		integrate     func(t *testing.T, f *fixture, pinned string) string
	}{
		{"stale", "recorded: [specs/drift/source.md#Counters (stale)]", func(t *testing.T, f *fixture, _ string) string {
			return driftCommit(t, f, "edited", driftSourcePath, driftSourceV2)
		}},
		{"repinned", "recorded: [specs/drift/source.md#Counters (repinned)]", func(t *testing.T, f *fixture, _ string) string {
			moved := driftCommit(t, f, "moved", driftSourcePath, driftSourceV2)
			return driftCommit(t, f, "repinned", driftCarrierPath, driftCarrier(moved))
		}},
		{"none", "recorded: []", func(t *testing.T, f *fixture, _ string) string {
			return driftCommit(t, f, "unrelated", "specs/drift/unrelated.md", "Nothing the carrier rests on.\n")
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t, "obligation-drift")
			pinned := driftCommit(t, f, "pinned", driftSourcePath, driftSourceV1)
			reviewed := driftCommit(t, f, "reviewed", driftCarrierPath, driftCarrier(pinned))
			f.git(t, "branch", "-f", "integration", c.integrate(t, f, pinned))
			current := string(readFile(t, f.statePath()))
			state := strings.Replace(current, `parent_review_commit: "`+f.commits["base"]+`"`, `parent_review_commit: "`+reviewed+`"`, 1)
			if state == current {
				t.Fatal("fixture review commit was not substituted")
			}
			writeFile(t, f.statePath(), []byte(state))

			before := readFile(t, f.statePath())
			snapshot := projectFiles(t, f.root)
			res := f.helper(t, "obligation-drift", "op")
			if res.code != 0 || strings.TrimSpace(string(res.stdout)) != c.summary {
				t.Fatalf("helper exit %d, summary %s; %s", res.code, res.stdout, res.stderr)
			}
			b, a := stateDocument(t, f, before, snapshot)
			f.harness().Check(t, oracle.Human, "obligation-drift/"+c.name+".txt", humanDocument(t, f, res))
			f.harness().CheckState(t, "obligation-drift/"+c.name+".txt", b, a)
		})
	}
}
