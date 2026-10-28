package ops

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

const phaseIntegrationOnlyRef = "specs/phase-integration-only.md"

// phaseCaptureStderr runs fn with os.Stderr redirected and returns what it
// wrote. Callers are not parallel: os.Stderr is process-wide.
func phaseCaptureStderr(t *testing.T, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stderr
	os.Stderr = writer
	done := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(reader)
		done <- string(data)
	}()
	func() {
		defer func() {
			os.Stderr = previous
			_ = writer.Close()
		}()
		fn()
	}()
	return <-done
}

// phaseCommitOnIntegrationOnly commits ref on the integration branch of the
// repository at root and returns to main, so the working tree lacks the file
// and only the integration branch resolves it.
func phaseCommitOnIntegrationOnly(t *testing.T, root, ref string) {
	t.Helper()
	testhelpers.MustGit(t, root, "checkout", "-q", "integration")
	writeAndCommit(t, root, ref, "# integration only\n", "Add integration-only spec")
	testhelpers.MustGit(t, root, "checkout", "-q", "main")
	if _, err := os.Stat(filepath.Join(root, ref)); !os.IsNotExist(err) {
		t.Fatalf("%s is visible in the main working tree: %v", ref, err)
	}
}

func phasePointAllSpecRefsAt(t *testing.T, statePath, ref string) {
	t.Helper()
	phaseRewriteState(t, lifecycleGenerationFixture{statePath: statePath}, func(state *models.State) {
		for i := range state.Tasks {
			state.Tasks[i].SpecRef = ref
		}
	})
}

// P7 (B7 and B8): the candidate validation at the end of the assess-blocked
// reconcile transaction and of release-blocked-chain resolves artifact refs
// through the integration branch and reports a violation the baseline already
// had as a warning on stderr; it never turns it into a refusal. A ref present
// only on integration raises nothing. Not parallel (captures os.Stderr).
//
// The plan text says the validation refuses a ref absent from both trees. It
// does not: ValidateCandidate refuses only violations the baseline lacks, and
// neither operation can introduce an artifact-ref violation, so the observed
// contract pinned here is the warning.
func TestPhaseCandidateValidationInEpisodeTransactions(t *testing.T) {
	type operation struct {
		name string
		run  func(t *testing.T) (root string, call func() error)
	}
	operations := []operation{
		{"b7-assess-blocked-reconcile", func(t *testing.T) (string, func() error) {
			fixture := newOrchestratorLifecycleFixture(t, true, models.TaskStatusBlocked)
			phasePointAllSpecRefsAt(t, fixture.statePath, phaseIntegrationOnlyRef)
			return fixture.projectRoot, func() error {
				_, err := AssessBlockedWithAuthority(fixture.projectRoot, "task-1", "assessed", fixture.authorityA, AssessBlockedOptions{
					Reason:    "the shared interface changed",
					Questions: []string{"Which interface wins?"},
				})
				return err
			}
		}},
		{"b8-release-blocked-chain", func(t *testing.T) (string, func() error) {
			root, statePath := releaseFixture(t)
			testhelpers.MustGit(t, root, "init", "-q", "-b", "main")
			testhelpers.MustGit(t, root, "config", "user.email", "test@example.com")
			testhelpers.MustGit(t, root, "config", "user.name", "Test User")
			testhelpers.MustGit(t, root, "config", "commit.gpgsign", "false")
			writeAndCommit(t, root, "README.md", "# project\n", "Initial commit")
			testhelpers.MustGit(t, root, "branch", "integration")
			phasePointAllSpecRefsAt(t, statePath, phaseIntegrationOnlyRef)
			return root, func() error {
				_, err := ReleaseBlockedChain(root, "c", "ruled")
				return err
			}
		}},
	}
	for _, op := range operations {
		t.Run(op.name+"/present-on-integration-only", func(t *testing.T) {
			root, call := op.run(t)
			phaseCommitOnIntegrationOnly(t, root, phaseIntegrationOnlyRef)
			var err error
			stderr := phaseCaptureStderr(t, func() { err = call() })
			if err != nil {
				t.Fatalf("%s: %v", op.name, err)
			}
			if strings.Contains(stderr, "phase-integration-only") {
				t.Fatalf("a ref resolved through the integration branch was reported:\n%s", stderr)
			}
		})
		t.Run(op.name+"/absent-from-both-is-a-pre-existing-warning", func(t *testing.T) {
			_, call := op.run(t)
			var err error
			stderr := phaseCaptureStderr(t, func() { err = call() })
			if err != nil {
				t.Fatalf("%s: a pre-existing artifact-ref violation refused the operation: %v", op.name, err)
			}
			if !strings.Contains(stderr, "WARNING: pre-existing state violation, not caused by this change") || !strings.Contains(stderr, "phase-integration-only") {
				t.Fatalf("stderr does not warn about the pre-existing missing ref:\n%s", stderr)
			}
		})
	}
}
