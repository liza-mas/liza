package a6

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func TestOracleWtCreate(t *testing.T) {
	platformGate(t)
	t.Run("success", func(t *testing.T) {
		runCase(t, caseSpec{
			setup: createSuccessFixture,
			args:  []string{"wt-create", "task-1"},
			kinds: []oracle.Kind{oracle.JSON, oracle.Human, oracle.State},
			extra: []string{"LIZA_ENABLE_STACKLIT=1"},
			check: func(t *testing.T, f *fixture, _ observation) {
				for _, name := range []string{"post-worktree.done", "stacklit.json"} {
					got, err := os.ReadFile(filepath.Join(f.root, ".worktrees", "task-1", name))
					if err != nil {
						t.Fatalf("worktree setup effect %s missing: %v", name, err)
					}
					want := "oracle-post-worktree\n"
					if name == "stacklit.json" {
						want = "{\"oracle\":\"repo-root-index\"}\n"
					}
					if string(got) != want {
						t.Errorf("worktree setup effect %s differs from fixture", name)
					}
				}
			},
		})
	})
	t.Run("not_executing", func(t *testing.T) {
		runCase(t, caseSpec{
			setup: func(t *testing.T) *fixture {
				f := baseFixture(t)
				f.install(t, "worktree/not-executing.yaml")
				return f
			},
			args: []string{"wt-create", "task-1"}, kinds: []oracle.Kind{oracle.JSON, oracle.Human, oracle.State},
			exit: 1, noDelta: true,
			check: func(t *testing.T, _ *fixture, o observation) {
				if !bytes.Contains(append(o.stdout, o.stderr...), []byte("not in an executing state")) {
					t.Fatal("wt-create did not reach the executing-status refusal")
				}
			},
		})
	})
	t.Run("index_missing", func(t *testing.T) {
		runCase(t, caseSpec{
			setup: func(t *testing.T) *fixture {
				f := baseFixture(t)
				f.install(t, "worktree/executing.yaml")
				return f
			},
			args: []string{"wt-create", "task-1"}, kinds: []oracle.Kind{oracle.JSON},
			extra: []string{"LIZA_ENABLE_STACKLIT=1"},
			check: func(t *testing.T, f *fixture, o observation) {
				if !bytes.Contains(o.stdout, []byte("repo-root index")) || !bytes.Contains(o.stdout, []byte("not found")) {
					t.Fatalf("missing root Stacklit index did not produce its warning:\nstdout: %s\nstderr: %s", o.stdout, o.stderr)
				}
				if _, err := os.Stat(filepath.Join(f.root, ".worktrees", "task-1", "stacklit.json")); !os.IsNotExist(err) {
					t.Fatal("missing-index fixture unexpectedly produced a worktree index")
				}
			},
		})
	})
}

func createSuccessFixture(t *testing.T) *fixture {
	t.Helper()
	platformGate(t)
	f := baseFixture(t)
	for _, name := range []string{"post-worktree.sh", "stacklit.json"} {
		data, err := os.ReadFile(filepath.Join("testdata", "fixtures", "worktree", name))
		if err != nil {
			t.Fatal(err)
		}
		f.write(t, name, string(data))
	}
	f.git(t, "add", "post-worktree.sh")
	f.git(t, "commit", "-m", "fixture: worktree setup inputs")
	f.tokens["@BASE@"] = strings.TrimSpace(f.git(t, "rev-parse", "HEAD"))
	f.install(t, "worktree/create.yaml")
	return f
}

func TestOracleWtDelete(t *testing.T) {
	platformGate(t)
	t.Run("success", func(t *testing.T) {
		runCase(t, caseSpec{
			setup: func(t *testing.T) *fixture { return deleteFixture(t, "worktree/blocked.yaml") },
			args:  []string{"wt-delete", "task-1"}, kinds: []oracle.Kind{oracle.JSON, oracle.Human, oracle.State},
			check: func(t *testing.T, f *fixture, _ observation) {
				if _, err := os.Stat(filepath.Join(f.root, ".worktrees", "task-1")); !os.IsNotExist(err) {
					t.Fatal("successful deletion left its worktree on disk")
				}
				if f.git(t, "branch", "--list", "task/task-1") != "" {
					t.Fatal("successful deletion left its task branch")
				}
			},
		})
	})
	t.Run("executing", func(t *testing.T) {
		runCase(t, caseSpec{
			setup: func(t *testing.T) *fixture { return deleteFixture(t, "worktree/executing.yaml") },
			args:  []string{"wt-delete", "task-1"}, kinds: []oracle.Kind{oracle.JSON, oracle.Human, oracle.State},
			exit: 1, noDelta: true,
			check: func(t *testing.T, f *fixture, o observation) {
				if !bytes.Contains(append(o.stdout, o.stderr...), []byte("cannot delete worktree")) {
					t.Fatal("wt-delete did not reach the executing-status refusal")
				}
				if _, err := os.Stat(filepath.Join(f.root, ".worktrees", "task-1")); err != nil {
					t.Fatal("refused deletion removed the executing task worktree")
				}
			},
		})
	})
}

func deleteFixture(t *testing.T, input string) *fixture {
	t.Helper()
	platformGate(t)
	f := baseFixture(t)
	f.git(t, "worktree", "add", filepath.Join(f.root, ".worktrees", "task-1"), "-b", "task/task-1", "integration")
	f.install(t, input)
	return f
}
