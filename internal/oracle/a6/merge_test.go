package a6

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func TestOracleWtMerge(t *testing.T) {
	platformGate(t)
	t.Run("success", func(t *testing.T) {
		runCase(t, caseSpec{
			setup: func(t *testing.T) *fixture { return mergeFixture(t, false) },
			args:  []string{"wt-merge", "task-1"}, kinds: []oracle.Kind{oracle.JSON, oracle.Human, oracle.State},
			extra: []string{"LIZA_AGENT_ID=code-reviewer-1", "LIZA_AGENT_GENERATION=oracle-reviewer"},
			check: func(t *testing.T, f *fixture, _ observation) {
				assertIsolated(t, f)
				got, err := os.ReadFile(filepath.Join(f.root, "approved.txt"))
				if err != nil || string(got) != "oracle-approved-change\n" {
					t.Fatal("merge did not publish the approved task content")
				}
				if _, err := os.Stat(filepath.Join(f.root, ".worktrees", "task-1")); !os.IsNotExist(err) {
					t.Fatal("successful merge left its task worktree")
				}
			},
		})
	})
	t.Run("conflict", func(t *testing.T) {
		runCase(t, caseSpec{
			setup: func(t *testing.T) *fixture { return mergeFixture(t, true) },
			args:  []string{"wt-merge", "task-1"}, kinds: []oracle.Kind{oracle.JSON, oracle.Human, oracle.State},
			extra: []string{"LIZA_AGENT_ID=code-reviewer-1", "LIZA_AGENT_GENERATION=oracle-reviewer"},
			exit:  1,
			check: func(t *testing.T, f *fixture, o observation) {
				assertIsolated(t, f)
				if !bytes.Contains(append(o.stdout, o.stderr...), []byte("merge conflict")) {
					t.Fatal("conflicting branches did not reach merge-conflict handling")
				}
				state, err := os.ReadFile(filepath.Join(f.root, ".liza", "state.yaml"))
				if err != nil {
					t.Fatal(err)
				}
				for _, field := range []string{"INTEGRATION_FAILED", "integration_failed", "integration_failure:", "failed_by:", "lifecycle:", "handoff_events:"} {
					if !bytes.Contains(state, []byte(field)) {
						t.Errorf("merge conflict did not persist %s", field)
					}
				}
			},
		})
	})
}

func mergeFixture(t *testing.T, conflict bool) *fixture {
	t.Helper()
	platformGate(t)
	f := baseFixture(t)
	f.write(t, "conflict.txt", "oracle-base\n")
	f.git(t, "add", "conflict.txt")
	f.git(t, "commit", "-m", "fixture: merge ancestor")
	f.tokens["@BASE@"] = strings.TrimSpace(f.git(t, "rev-parse", "HEAD"))
	wt := filepath.Join(f.root, ".worktrees", "task-1")
	f.git(t, "worktree", "add", wt, "-b", "task/task-1", "integration")
	name, content := "approved.txt", "oracle-approved-change\n"
	if conflict {
		name, content = "conflict.txt", "oracle-task-change\n"
	}
	f.write(t, filepath.Join(".worktrees", "task-1", name), content)
	f.git(t, "-C", wt, "add", name)
	f.git(t, "-C", wt, "commit", "-m", "fixture: approved task change")
	f.tokens["@REVIEW@"] = strings.TrimSpace(f.git(t, "-C", wt, "rev-parse", "HEAD"))
	if conflict {
		f.write(t, "conflict.txt", "oracle-integration-change\n")
		f.git(t, "add", "conflict.txt")
		f.git(t, "commit", "-m", "fixture: conflicting integration change")
		f.tokens["@INTEGRATION@"] = strings.TrimSpace(f.git(t, "rev-parse", "HEAD"))
	}
	f.install(t, "merge/approved.yaml")
	assertIsolated(t, f)
	return f
}

func TestOracleReconcileMerged(t *testing.T) {
	platformGate(t)
	t.Run("success", func(t *testing.T) {
		runCase(t, caseSpec{
			setup: reconcileFixture,
			args:  []string{"reconcile-merged", "task-1", "--merge-commit", "integration", "--reason", "oracle external merge"},
			kinds: []oracle.Kind{oracle.JSON, oracle.Human, oracle.State},
			check: func(t *testing.T, f *fixture, _ observation) {
				assertIsolated(t, f)
				if _, err := os.Stat(filepath.Join(f.root, ".worktrees", "task-1")); !os.IsNotExist(err) {
					t.Fatal("reconciliation left the stale task worktree")
				}
				if f.git(t, "branch", "--list", "task/task-1") != "" {
					t.Fatal("reconciliation left the stale task branch")
				}
			},
		})
	})
	t.Run("refused", func(t *testing.T) {
		runCase(t, caseSpec{
			setup: reconcileFixture,
			args:  []string{"reconcile-merged", "task-1", "--merge-commit", "oracle-missing-ref", "--reason", "oracle external merge"},
			kinds: []oracle.Kind{oracle.JSON, oracle.Human, oracle.State}, exit: 1, noDelta: true,
			check: func(t *testing.T, f *fixture, o observation) {
				assertIsolated(t, f)
				if !bytes.Contains(append(o.stdout, o.stderr...), []byte("could not be resolved locally; fetch the merged PR first")) {
					t.Fatal("reconciliation did not reach the local-object precondition")
				}
			},
		})
	})
}

func reconcileFixture(t *testing.T) *fixture {
	t.Helper()
	platformGate(t)
	f := baseFixture(t)
	f.git(t, "worktree", "add", filepath.Join(f.root, ".worktrees", "task-1"), "-b", "task/task-1", "integration")
	f.install(t, "merge/integration-failed.yaml")
	assertIsolated(t, f)
	return f
}
