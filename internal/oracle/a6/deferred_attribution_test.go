package a6

import (
	"bytes"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

// The batch-end gap of the deferred-validation surface: a command that passed
// at an ancestor and fails at integration HEAD is bisected to the one merge
// whose predecessor passed and whose own commit failed.
func TestOracleDeferredValidationBisect(t *testing.T) {
	platformGate(t)
	all := []oracle.Kind{oracle.JSON, oracle.Human, oracle.State}
	t.Run("run_bisect", func(t *testing.T) {
		runCase(t, caseSpec{
			setup: bisectFixture,
			args:  []string{"deferred-validation", "run"},
			kinds: all,
			check: func(t *testing.T, f *fixture, got observation) {
				requireDeferredOutcome(t, got, "ran")
				for _, want := range []string{"failed", "oracle-second"} {
					if !bytes.Contains(got.stdout, []byte(want)) {
						t.Fatalf("bisected batch lacks %q", want)
					}
				}
				if bytes.Contains(got.stdout, []byte("oracle-first")) {
					t.Fatal("bisect named the merge whose commit still passed")
				}
			},
		})
	})
	t.Run("status_bisected", func(t *testing.T) {
		runCase(t, caseSpec{
			setup: func(t *testing.T) *fixture {
				f := bisectFixture(t)
				f.install(t, "deferred/bisected.yaml")
				return f
			},
			args:    []string{"deferred-validation", "status"},
			kinds:   []oracle.Kind{oracle.JSON, oracle.Human},
			noDelta: true,
			check: func(t *testing.T, f *fixture, got observation) {
				if !bytes.Contains(got.stdout, []byte("oracle-second")) {
					t.Fatal("status does not name the attributed culprit")
				}
			},
		})
	})
}

func bisectFixture(t *testing.T) *fixture {
	t.Helper()
	f := baseFixture(t)
	for _, change := range []struct{ path, token string }{{"ok.txt", "@C1@"}, {"bad.txt", "@C2@"}} {
		f.write(t, change.path, "oracle\n")
		f.git(t, "add", change.path)
		f.git(t, "commit", "-m", "test: add "+change.path)
		f.tokens[change.token] = f.git(t, "rev-parse", "HEAD")
	}
	f.install(t, "deferred/bisect.yaml")
	return f
}
