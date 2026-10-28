package a4

import (
	"bytes"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

// replaceStep is one CLI invocation of a replace-task case. Arguments may name
// a fixture input (@input:) or a task's starting transition (@transition:);
// both resolve against the case's starting fixture, as a retrying caller keeps
// the transition it first observed.
type replaceStep struct {
	identity string
	args     []string
}

// replaceCase runs steps in order on one fixture and freezes the last one: the
// earlier steps must succeed and only set up what the last step decides on.
type replaceCase struct {
	name, fixture string
	setup         func(testing.TB, *fixture)
	steps         []replaceStep
	wantExit      int
	// contains must appear in both modes; jsonContains names a field the human
	// transcript does not print.
	contains, jsonContains string
	// unchanged asserts the last step left state.yaml as the step before it did.
	unchanged bool
	// refs also freezes the branches and worktrees the last step left behind.
	refs bool
}

func replaceRequest(input, token string) []string {
	return []string{"replace-task", "--replacement-file", "@input:" + input, "--request-id", "replace-1", "--expected-transition", token}
}

func replaceOnce(input, token string) []replaceStep {
	return []replaceStep{{"orch", replaceRequest(input, token)}}
}

// TestOracleReplaceTaskPaths pins the decisions TestOracleReplaceTask leaves
// open: the replay of an identified request and the two identity conflicts it
// names, the replay of an archived source, the preserved base and its refusal,
// the source worktree removed beside the kept branch, the stale consumer, the
// blocked-source recovery rule and the ineligible source.
func TestOracleReplaceTaskPaths(t *testing.T) {
	archive := replaceStep{"op", []string{"archive-terminal-tasks"}}
	for _, c := range []replaceCase{
		{name: "replay", fixture: "graph", unchanged: true,
			steps: append(replaceOnce("replacement.json", "@transition:t1"), replaceStep{"orch", replaceRequest("replacement.json", "@transition:t1")}),
		},
		{name: "conflict_request_id", fixture: "graph", wantExit: 1, unchanged: true, contains: "CONFLICT", jsonContains: "request_id",
			steps: append(replaceOnce("replacement.json", "@transition:t1"), replaceStep{"orch", replaceRequest("replace-reason.json", "@transition:t1")}),
		},
		{name: "conflict_replacement_id", fixture: "graph", wantExit: 1, unchanged: true, contains: "CONFLICT", jsonContains: "replacement.id",
			steps: append(replaceOnce("replacement.json", "@transition:t1"), replaceStep{"orch", replaceRequest("replace-id.json", "@transition:t1")}),
		},
		{name: "archived_replay", fixture: "replace-archival", unchanged: true,
			steps: append(replaceOnce("replacement.json", "@transition:t1"), archive, replaceStep{"orch", replaceRequest("replacement.json", "@transition:t1")}),
		},
		{name: "preserved_base", fixture: "graph", refs: true, contains: "t1b",
			setup: func(t testing.TB, f *fixture) {
				f.git(t, "worktree", "add", "-b", "task/t1b", ".worktrees/t1b", f.commits["base"])
			},
			steps: replaceOnce("replace-preserved.json", "@transition:t1"),
		},
		{name: "preserved_base_refused", fixture: "graph", wantExit: 1, unchanged: true, contains: "canonical replacement worktree is required", jsonContains: "preserved_base.worktree",
			steps: replaceOnce("replace-preserved-refused.json", "@transition:t1"),
		},
		{name: "source_worktree", fixture: "replace-worktree", refs: true, contains: "t1b",
			steps: replaceOnce("replacement.json", "@transition:t1"),
		},
		{name: "consumer_stale_refused", fixture: "graph", wantExit: 1, unchanged: true, contains: "t2",
			steps: replaceOnce("replace-stale.json", "@transition:t1"),
		},
		{name: "blocked_unchanged_refused", fixture: "replace-blocked", wantExit: 1, unchanged: true, contains: "changed",
			steps: replaceOnce("replacement.json", "@transition:t1"),
		},
		{name: "blocked_recovery", fixture: "replace-blocked", contains: "t1b",
			steps: replaceOnce("replace-changed.json", "@transition:t1"),
		},
		{name: "ineligible_refused", fixture: "graph", wantExit: 1, unchanged: true, contains: "ALREADY_TRANSITIONED",
			steps: replaceOnce("replace-merged.json", "@transition:m1"),
		},
	} {
		t.Run(c.name, func(t *testing.T) { runReplaceCase(t, c) })
	}
}

// runReplaceCase is runCase for a sequence of steps. The golden names are
// replace-task/<name>; the json and human modes run on fresh fixtures and must
// end in the same normalised state.
func runReplaceCase(t *testing.T, c replaceCase) {
	t.Helper()
	gate(t)
	name := "replace-task/" + c.name
	var jsonState []byte
	for _, mode := range []string{"json", "human"} {
		f := newFixture(t, c.fixture)
		if c.setup != nil {
			c.setup(t, f)
		}
		before := readFile(t, f.statePath())
		snapshot := projectFiles(t, f.root)
		resolved := make([][]string, len(c.steps))
		for i, step := range c.steps {
			resolved[i] = append([]string(nil), step.args...)
			for j, arg := range resolved[i] {
				if input, ok := strings.CutPrefix(arg, "@input:"); ok {
					resolved[i][j] = f.input(t, input)
				}
				if task, ok := strings.CutPrefix(arg, "@transition:"); ok {
					id, err := transitionID(before, task)
					if err != nil {
						t.Fatal(err)
					}
					resolved[i][j] = id
				}
			}
			if mode == "json" {
				resolved[i] = append(resolved[i], "--json")
			}
		}
		var prior []byte
		var last runResult
		for i, step := range c.steps {
			if i == len(c.steps)-1 {
				prior = readFile(t, f.statePath())
			}
			last = f.cli(t, step.identity, resolved[i]...)
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
			if c.name == "replay" && !strings.Contains(string(last.stdout), `"changed": false`) && !strings.Contains(string(last.stdout), `"changed":false`) {
				t.Fatalf("replay did not report changed=false: %s", last.stdout)
			}
			f.harness().Check(t, oracle.JSON, name+".json", jsonDocument(t, f, last))
		} else {
			f.harness().Check(t, oracle.Human, name+".txt", humanDocument(t, f, last))
		}
		if c.refs && mode == "json" {
			f.harness().Check(t, oracle.Human, name+".refs.txt", []byte(replaceRefs(t, f)))
		}
		b, a := stateDocument(t, f, before, snapshot)
		if mode == "human" {
			if !bytes.Equal(jsonState, a) {
				t.Fatalf("json/human final state differs for %s:\njson:\n%s\nhuman:\n%s", name, jsonState, a)
			}
			continue
		}
		f.harness().CheckState(t, name+".txt", b, a)
		jsonState = a
	}
}

// replaceRefs lists the branch tips and registered worktrees a step left, the
// effects the state golden cannot show: removed directories and kept branches.
func replaceRefs(t testing.TB, f *fixture) string {
	t.Helper()
	var out strings.Builder
	out.WriteString("branches:\n")
	for line := range strings.SplitSeq(f.git(t, "for-each-ref", "--format=%(objectname) %(refname:short)", "refs/heads"), "\n") {
		out.WriteString("  " + line + "\n")
	}
	out.WriteString("worktrees:\n")
	for line := range strings.SplitSeq(f.git(t, "worktree", "list", "--porcelain"), "\n") {
		if strings.HasPrefix(line, "worktree ") || strings.HasPrefix(line, "branch ") {
			out.WriteString("  " + line + "\n")
		}
	}
	return f.normaliser().text(out.String())
}
