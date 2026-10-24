package install

import "testing"

func TestOracleMigrate(t *testing.T) {
	skipUnlessLinux(t)
	for _, name := range []string{"legacy", "clean", "unparseable"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.materialiseRepo(t, true)
			source, expected := name+".yaml", 0
			if name == "unparseable" {
				source, expected = "unparseable.txt", 1
			}
			f.stateFixture(t, source)
			before := readOptional(t, f.project, ".liza/state.yaml")
			// The clean fixture is also the TUI input. Validate it against both
			// live and starting binaries, without any host process scan.
			if name == "clean" {
				f.invoke(t, "validate-clean-fixture", "", false, 0, "validate", "--skip-process-checks")
			}
			f.invoke(t, "migrate", "", false, expected, "migrate")
			f.checkHuman(t, "migrate/"+name)
			f.checkState(t, "migrate/"+name, before, readOptional(t, f.project, ".liza/state.yaml"))
		})
	}
}
