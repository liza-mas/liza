package a6

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

// setupOutputDigest matches the worktree-path digest naming the private output
// file; it differs per fixture directory, so the driver registers it as a token.
var setupOutputDigest = regexp.MustCompile(`worktree-setup/task-1-([0-9a-f]{16})\.log`)

// TestOracleWtCreatePostWorktreeGaps covers the failure outcome of the K-WT
// split that W1 leaves unmapped: a configured post_worktree_cmd that exits
// non-zero makes wt-create fail closed with the masked, bounded diagnostic,
// leaving the worktree on disk and the recorded base commit in state. The
// configured-language scip and functional-clusters refresh need external
// indexers and Windows is outside the proof environment, so they stay with
// their white-box tests.
func TestOracleWtCreatePostWorktreeGaps(t *testing.T) {
	platformGate(t)
	t.Run("post_worktree_failed", func(t *testing.T) {
		runCase(t, caseSpec{
			setup: func(t *testing.T) *fixture {
				f := baseFixture(t)
				data, err := os.ReadFile(filepath.Join("testdata", "fixtures", "worktree", "post-worktree-fail.sh"))
				if err != nil {
					t.Fatal(err)
				}
				f.write(t, "post-worktree-fail.sh", string(data))
				f.git(t, "add", "post-worktree-fail.sh")
				f.git(t, "commit", "-m", "fixture: failing worktree setup input")
				f.tokens["@BASE@"] = strings.TrimSpace(f.git(t, "rev-parse", "HEAD"))
				f.install(t, "worktree/create-failing.yaml")
				return f
			},
			args: []string{"wt-create", "task-1"}, kinds: []oracle.Kind{oracle.JSON, oracle.Human, oracle.State},
			exit: 1,
			check: func(t *testing.T, f *fixture, o observation) {
				// --json classifies the unclassified setup error as internal and
				// withholds its text; the human transcript carries the diagnostic.
				want := []byte("post-worktree setup command failed")
				if bytes.HasPrefix(bytes.TrimSpace(o.stdout), []byte("{")) {
					want = []byte(`"ok":false`)
				}
				if !bytes.Contains(append(o.stdout, o.stderr...), want) {
					t.Fatalf("failing post_worktree_cmd did not reach the fail-closed refusal:\nstdout: %s\nstderr: %s", o.stdout, o.stderr)
				}
				if m := setupOutputDigest.FindSubmatch(o.stderr); m != nil {
					f.norm.Literal(string(m[1]), "<SETUP-DIGEST>")
				}
				if _, err := os.Stat(filepath.Join(f.root, ".worktrees", "task-1")); err != nil {
					t.Fatal("fail-closed creation removed the worktree it left for inspection")
				}
			},
		})
	})
}
