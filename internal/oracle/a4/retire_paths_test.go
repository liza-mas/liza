package a4

import (
	"bytes"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

// retireCase runs steps in order on one fixture and freezes the last one, as
// replaceCase does; golden is the <operation>/<case> name of the frozen step.
type retireCase struct {
	golden, fixture string
	setup           func(testing.TB, *fixture)
	steps           []replaceStep
	wantExit        int
	// contains must appear in both modes; jsonContains names a field the human
	// transcript does not print.
	contains, jsonContains string
	// unchanged asserts the last step left state.yaml as the step before it did.
	unchanged bool
	// replay asserts the last step reports changed=false.
	replay bool
	// refs also freezes the branches and worktrees the last step left behind.
	refs bool
}

func supersedeRequest(args ...string) replaceStep {
	return replaceStep{"orch", append([]string{"supersede-task", "t1"}, args...)}
}

func supersedeIdentified(args ...string) replaceStep {
	return supersedeRequest(append(args, "--request-id", "supersede-1", "--expected-transition", "@transition:t1")...)
}

func cancelRequest(id string, args ...string) replaceStep {
	return replaceStep{"orch", append([]string{"cancel-task", id, "No longer needed"}, args...)}
}

func cancelIdentified(id string) replaceStep {
	return cancelRequest(id, "--request-id", "cancel-1", "--expected-transition", "@transition:"+id)
}

// branchBeside gives the predecessor p0 the branch its supersession left, which
// the fixture builder creates only for tasks holding a worktree.
func branchBeside(t testing.TB, f *fixture) { f.git(t, "branch", "task/p0", f.commits["base"]) }

// TestOracleRetirePaths pins the supersede-task and cancel-task decisions the
// success and refused cases leave open: the replay of an identified request,
// the replay of an archived source, the salvage and branch deletion of a
// supersession without replacements, the worktree removed beside the kept or
// deleted branch, the blocked recovery rule, the predecessor branch released
// by the last terminal successor and the consumer edge a cancellation removes.
func TestOracleRetirePaths(t *testing.T) {
	archive := replaceStep{"op", []string{"archive-terminal-tasks"}}
	const operation = "supersede-task/"
	const cancellation = "cancel-task/"
	for _, c := range []retireCase{
		{golden: operation + "replay", fixture: "graph", unchanged: true, replay: true,
			steps: []replaceStep{supersedeIdentified("t3", "--reason", "Rescoped"), supersedeIdentified("t3", "--reason", "Rescoped")},
		},
		{golden: operation + "archived_replay", fixture: "retire-archival", unchanged: true, replay: true,
			steps: []replaceStep{supersedeIdentified("t3", "--reason", "Rescoped"), archive, supersedeIdentified("t3", "--reason", "Rescoped")},
		},
		{golden: operation + "no_replacements", fixture: "retire-worktree", refs: true, contains: "t1",
			steps: []replaceStep{supersedeRequest("--reason", "Already merged", "--recoverability-command", "git show task/t1")},
		},
		{golden: operation + "no_replacements_refused", fixture: "graph", wantExit: 1, unchanged: true, contains: "recoverability command is required",
			steps: []replaceStep{supersedeRequest("--reason", "Already merged")},
		},
		{golden: operation + "source_worktree", fixture: "retire-worktree", refs: true, contains: "t3",
			steps: []replaceStep{supersedeRequest("t3", "--reason", "Rescoped")},
		},
		{golden: operation + "blocked_recovery", fixture: "retire-blocked", contains: "t3",
			steps: []replaceStep{supersedeRequest("t3", "--reason", "Rescoped", "--changed", "Split the decomposition")},
		},
		{golden: operation + "blocked_unchanged_refused", fixture: "retire-blocked", wantExit: 1, unchanged: true, contains: "changed",
			steps: []replaceStep{supersedeRequest("t3", "--reason", "Rescoped")},
		},
		{golden: operation + "predecessor_cleanup", fixture: "retire-predecessor", setup: branchBeside, refs: true, contains: "t3",
			steps: []replaceStep{supersedeRequest("t3", "--reason", "Rescoped")},
		},
		{golden: cancellation + "replay", fixture: "graph", unchanged: true, replay: true,
			steps: []replaceStep{cancelIdentified("t3"), cancelIdentified("t3")},
		},
		{golden: cancellation + "archived_replay", fixture: "retire-archival", unchanged: true, replay: true,
			steps: []replaceStep{cancelIdentified("t3"), archive, cancelIdentified("t3")},
		},
		{golden: cancellation + "worktree", fixture: "retire-worktree", refs: true, contains: "t1",
			steps: []replaceStep{cancelRequest("t1")},
		},
		{golden: cancellation + "predecessor_cleanup", fixture: "retire-predecessor", setup: branchBeside, refs: true, contains: "t1",
			steps: []replaceStep{cancelRequest("t1")},
		},
		{golden: cancellation + "dependents_rewritten", fixture: "graph", contains: "t1",
			steps: []replaceStep{cancelRequest("t1")},
		},
	} {
		t.Run(c.golden, func(t *testing.T) { runRetireCase(t, c) })
	}
}

// runRetireCase is runReplaceCase for the retirement operations: the json and
// human modes run on fresh fixtures and must end in the same normalised state.
func runRetireCase(t *testing.T, c retireCase) {
	t.Helper()
	gate(t)
	var jsonState []byte
	for _, mode := range []string{"json", "human"} {
		f := newFixture(t, c.fixture)
		if c.setup != nil {
			c.setup(t, f)
		}
		before := readFile(t, f.statePath())
		snapshot := projectFiles(t, f.root)
		var prior []byte
		var last runResult
		for i, step := range c.steps {
			args := append([]string(nil), step.args...)
			for j, arg := range args {
				if task, ok := strings.CutPrefix(arg, "@transition:"); ok {
					id, err := transitionID(before, task)
					if err != nil {
						t.Fatal(err)
					}
					args[j] = id
				}
			}
			if mode == "json" {
				args = append(args, "--json")
			}
			if i == len(c.steps)-1 {
				prior = readFile(t, f.statePath())
			}
			last = f.cli(t, step.identity, args...)
			if i < len(c.steps)-1 && last.code != 0 {
				t.Fatalf("%s step %d exit %d: %s %s", mode, i, last.code, last.stdout, last.stderr)
			}
		}
		if last.code != c.wantExit {
			t.Fatalf("%s exit %d, want %d: %s %s", mode, last.code, c.wantExit, f.normaliser().text(string(last.stdout)), f.normaliser().text(string(last.stderr)))
		}
		if c.contains != "" && !strings.Contains(string(last.stdout)+string(last.stderr), c.contains) {
			t.Fatalf("%s did not reach named outcome %q: %s %s", mode, c.contains, last.stdout, last.stderr)
		}
		if mode == "json" && !strings.Contains(string(last.stdout), c.jsonContains) {
			t.Fatalf("json did not name %q: %s", c.jsonContains, last.stdout)
		}
		if c.unchanged && !bytes.Equal(prior, readFile(t, f.statePath())) {
			t.Fatalf("%s last step changed state", mode)
		}
		if mode == "json" {
			if c.replay && !strings.Contains(string(last.stdout), `"changed": false`) && !strings.Contains(string(last.stdout), `"changed":false`) {
				t.Fatalf("replay did not report changed=false: %s", last.stdout)
			}
			f.harness().Check(t, oracle.JSON, c.golden+".json", jsonDocument(t, f, last))
		} else {
			f.harness().Check(t, oracle.Human, c.golden+".txt", humanDocument(t, f, last))
		}
		if c.refs && mode == "json" {
			f.harness().Check(t, oracle.Human, c.golden+".refs.txt", []byte(replaceRefs(t, f)))
		}
		b, a := stateDocument(t, f, before, snapshot)
		if mode == "human" {
			if !bytes.Equal(jsonState, a) {
				t.Fatalf("json/human final state differs for %s:\njson:\n%s\nhuman:\n%s", c.golden, jsonState, a)
			}
			continue
		}
		f.harness().CheckState(t, c.golden+".txt", b, a)
		jsonState = a
	}
}
