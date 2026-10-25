package a4

import (
	"bytes"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

// deleteCase runs one `delete task` invocation with empty stdin on a fresh
// fixture, as TestOracleDeleteTask does; golden is the delete-task/<case> name.
type deleteCase struct {
	golden, fixture string
	args            []string
	wantExit        int
	contains        string
	// unchanged asserts the invocation left state.yaml as it found it.
	unchanged bool
	// refs also freezes the branches and worktrees the invocation left behind.
	refs bool
}

// TestOracleDeletePaths pins the delete-task decisions the success and
// refused cases leave open: forced deletion of a MERGED task and of a task
// with dependents, the dependents, parent-reference and under-review refusals,
// the worktree and branch deleted beside the task or preserved, and the agent
// released from a deleted task.
func TestOracleDeletePaths(t *testing.T) {
	for _, c := range []deleteCase{
		{golden: "force_merged", fixture: "graph", args: []string{"m1", "--force"}, contains: "Deleted task m1 (was MERGED)"},
		{golden: "force_dependents", fixture: "graph", args: []string{"t1", "--force"}, contains: "Deleted task t1"},
		{golden: "dependents_refused", fixture: "graph", args: []string{"t1"}, wantExit: 1, unchanged: true, contains: "depend on it"},
		{golden: "parent_refused", fixture: "delete-parent", args: []string{"t1"}, wantExit: 1, unchanged: true, contains: "references it via parent_task"},
		{golden: "review_refused", fixture: "delete-review", args: []string{"t1"}, wantExit: 1, unchanged: true, contains: "under review"},
		{golden: "delete_worktree", fixture: "delete-worktree", args: []string{"t1", "--delete-worktree"}, refs: true, contains: "Also deleted worktree and branch"},
		{golden: "worktree_preserved", fixture: "delete-worktree", args: []string{"t1"}, refs: true, contains: "preserved"},
		{golden: "assigned", fixture: "delete-assigned", args: []string{"t1"}, refs: true, contains: "Deleted task t1 (was IMPLEMENTING_CODE)"},
	} {
		t.Run(c.golden, func(t *testing.T) { runDeleteCase(t, c) })
	}
}

func runDeleteCase(t *testing.T, c deleteCase) {
	t.Helper()
	gate(t)
	f := newFixture(t, c.fixture)
	before := readFile(t, f.statePath())
	snapshot := projectFiles(t, f.root)
	res := f.cli(t, "orch", append([]string{"delete", "task"}, c.args...)...)
	if res.code != c.wantExit {
		t.Fatalf("exit %d, want %d: %s %s", res.code, c.wantExit, f.normaliser().text(string(res.stdout)), f.normaliser().text(string(res.stderr)))
	}
	if !strings.Contains(string(res.stdout)+string(res.stderr), c.contains) {
		t.Fatalf("did not reach named outcome %q: %s %s", c.contains, res.stdout, res.stderr)
	}
	if c.unchanged && !bytes.Equal(before, readFile(t, f.statePath())) {
		t.Fatal("refused deletion changed state")
	}
	name := "delete-task/" + c.golden
	f.harness().Check(t, oracle.Human, name+".txt", humanDocument(t, f, res))
	if c.refs {
		f.harness().Check(t, oracle.Human, name+".refs.txt", []byte(replaceRefs(t, f)))
	}
	b, a := stateDocument(t, f, before, snapshot)
	f.harness().CheckState(t, name+".txt", b, a)
}
