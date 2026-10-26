package a6

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

// TestOracleWtMergeFailureGaps covers the failure outcome of the K-MRG split
// that W7 leaves unmapped: an integration test script that exits non-zero
// rewinds the merge the invocation published and records INTEGRATION_FAILED
// with the script's output. A worktree HEAD mismatch and a post-merge state
// validation failure are not built here: their diagnostics print seven-character
// SHAs the A6 determinism layer cannot tokenise, or need a corrupted artifact
// ref, so they stay with their white-box tests.
func TestOracleWtMergeFailureGaps(t *testing.T) {
	platformGate(t)
	t.Run("tests_failed", func(t *testing.T) {
		runCase(t, caseSpec{
			setup: func(t *testing.T) *fixture {
				f := mergeFixture(t, false)
				script := filepath.Join("scripts", "integration-test.sh")
				f.write(t, script, "#!/bin/sh\necho oracle-integration-output\nexit 1\n")
				if err := os.Chmod(filepath.Join(f.root, script), 0o755); err != nil {
					t.Fatal(err)
				}
				return f
			},
			args: []string{"wt-merge", "task-1"}, kinds: []oracle.Kind{oracle.JSON, oracle.Human, oracle.State},
			extra: []string{"LIZA_AGENT_ID=code-reviewer-1", "LIZA_AGENT_GENERATION=oracle-reviewer"},
			exit:  1,
			check: func(t *testing.T, f *fixture, o observation) {
				assertIsolated(t, f)
				if !bytes.Contains(append(o.stdout, o.stderr...), []byte("integration tests failed")) {
					t.Fatal("failing integration script did not reach tests-failed handling")
				}
				if f.git(t, "rev-parse", "integration") != f.tokens["@BASE@"] {
					t.Fatal("failed integration tests left the merge on the integration branch")
				}
				state, err := os.ReadFile(filepath.Join(f.root, ".liza", "state.yaml"))
				if err != nil {
					t.Fatal(err)
				}
				for _, field := range []string{"INTEGRATION_FAILED", "integration_failure:", "oracle-integration-output", "failing_command"} {
					if !bytes.Contains(state, []byte(field)) {
						t.Errorf("failed integration tests did not persist %s", field)
					}
				}
			},
		})
	})
}
