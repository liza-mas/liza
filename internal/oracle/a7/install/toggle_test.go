package install

import "testing"

func TestOracleTUIAutoResume(t *testing.T) {
	skipUnlessLinux(t)
	f := newFixture(t)
	f.materialiseRepo(t, true)
	f.stateFixture(t, "clean.yaml")
	before := readOptional(t, f.project, ".liza/state.yaml")
	if err := toggleAutoResume(f.project); err != nil {
		t.Fatal(err)
	}
	f.checkState(t, "tui/false-to-true", before, readOptional(t, f.project, ".liza/state.yaml"))
}
