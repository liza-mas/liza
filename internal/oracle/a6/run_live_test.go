package a6

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

// This literal is synthetic fixture data, never a host or user credential.
const oracleReusableValue = "oracle-reusable-fixture-8472"

func TestOracleRunLive(t *testing.T) {
	platformGate(t)
	t.Run("success", func(t *testing.T) {
		runCase(t, caseSpec{
			setup: func(t *testing.T) *fixture {
				f := baseFixture(t)
				f.install(t, "run-live/reusable.yaml")
				installLiveScript(t, f)
				// The envelope and the key the production command creates both live
				// in disposable directories outside the fixture repository.
				envelope := filepath.Join(f.home, "oracle-input.txt")
				if err := os.WriteFile(envelope, []byte("ORACLE_FIXTURE="+oracleReusableValue+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				provision := captureCLI(t, f, nil, "provision", "--record", "--task", "t1", "--input", "fixture", "--file", envelope, "--json")
				if provision.exit != 0 || !bytes.Contains(provision.stdout, []byte(`"recorded"`)) {
					t.Fatal("synthetic reusable input was not recorded")
				}
				return f
			},
			args:  []string{"run-live", "--task", "t1", "--", "sh", "live.sh"},
			kinds: []oracle.Kind{oracle.Human},
			exit:  7,
			check: func(t *testing.T, _ *fixture, got observation) {
				if bytes.Contains(got.stdout, []byte(oracleReusableValue)) || bytes.Contains(got.stderr, []byte(oracleReusableValue)) {
					t.Fatal("run-live exposed the synthetic reusable input")
				}
				if !bytes.Contains(got.stdout, []byte("fixture=***")) || !bytes.Contains(got.stdout, []byte("oracle-live-marker")) {
					t.Fatal("run-live did not deliver and mask the reusable input")
				}
			},
		})
	})
	t.Run("no_inputs", func(t *testing.T) {
		runCase(t, caseSpec{
			setup: func(t *testing.T) *fixture {
				f := baseFixture(t)
				f.install(t, "run-live/no-inputs.yaml")
				installLiveScript(t, f)
				return f
			},
			args:    []string{"run-live", "--task", "t1", "--", "sh", "live.sh"},
			kinds:   []oracle.Kind{oracle.Human},
			exit:    1,
			noDelta: true,
			check: func(t *testing.T, _ *fixture, got observation) {
				if !bytes.Contains(got.stderr, []byte("task t1 declares no runtime inputs; run the command directly")) {
					t.Fatal("run-live did not reach the no-inputs refusal")
				}
				if bytes.Contains(got.stdout, []byte("oracle-live-marker")) {
					t.Fatal("run-live launched a command without declared inputs")
				}
			},
		})
	})
}

func installLiveScript(t *testing.T, f *fixture) {
	t.Helper()
	data, err := os.ReadFile("testdata/fixtures/run-live/live.sh")
	if err != nil {
		t.Fatal(err)
	}
	f.write(t, "live.sh", string(data))
}
