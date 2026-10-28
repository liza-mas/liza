package a4

import (
	"bytes"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

// TestOracleNarrowInheritedDependenciesPaths pins the decisions
// TestOracleNarrowInheritedDependencies leaves open: children skipped for their
// status or live lease, a selection that keeps every inherited edge, the
// inherited edges kept because the planner named them, the unresolvable and
// out-of-range selections that write nothing, and the replay of an identified
// request.
func TestOracleNarrowInheritedDependenciesPaths(t *testing.T) {
	narrow := func(input string) []string {
		return []string{"narrow-inherited-dependencies", "cp-b", "--selections", "@input:" + input, "--reason", "Narrow"}
	}
	for _, c := range []oracleCase{
		{name: "narrow-inherited-dependencies/skipped", fixture: "narrow-skipped", identity: "orch", json: true,
			contains: "terminal status MERGED", args: narrow("narrow-skipped.yaml")},
		{name: "narrow-inherited-dependencies/unchanged", fixture: "inherited", identity: "orch", json: true,
			contains: "unchanged", args: narrow("narrow-unchanged.yaml")},
		{name: "narrow-inherited-dependencies/protected_edges", fixture: "narrow-protected", identity: "orch", json: true,
			contains: "cp-a-code-3r", args: narrow("narrow-protected.yaml")},
		{name: "narrow-inherited-dependencies/unresolvable_refused", fixture: "inherited", identity: "orch", json: true, wantExit: 1,
			contains: "supplies no inherited children", args: narrow("narrow-unresolvable.yaml")},
		{name: "narrow-inherited-dependencies/out_of_range_refused", fixture: "inherited", identity: "orch", json: true, wantExit: 1,
			contains: "selection index 3 is out of range: producer cp-b has 1 output(s)", args: narrow("narrow-range.yaml")},
	} {
		t.Run(c.name, func(t *testing.T) { runCase(t, c) })
	}
	t.Run("narrow-inherited-dependencies/replay", func(t *testing.T) {
		runNarrowReplay(t, "narrow-inherited-dependencies/replay", "inherited", "cp-b", narrow("narrow.yaml"))
	})
}

// runNarrowReplay is runReplayCase for a request whose selections file is a
// per-fixture input: the shared helper builds its arguments before the fixture
// exists, so it cannot resolve the @input: reference.
func runNarrowReplay(t *testing.T, name, fixtureName, taskID string, args []string) {
	t.Helper()
	gate(t)
	var jsonState []byte
	for _, mode := range []string{"json", "human"} {
		f := newFixture(t, fixtureName)
		before := readFile(t, f.statePath())
		snapshot := projectFiles(t, f.root)
		expected, err := transitionID(before, taskID)
		if err != nil {
			t.Fatal(err)
		}
		request := append([]string(nil), args...)
		for i, arg := range request {
			if input, ok := strings.CutPrefix(arg, "@input:"); ok {
				request[i] = f.input(t, input)
			}
		}
		request = append(request, "--request-id", "replay-1", "--expected-transition", expected)
		if mode == "json" {
			request = append(request, "--json")
		}
		first := f.cli(t, "orch", request...)
		if first.code != 0 {
			t.Fatalf("%s first run exit %d: %s %s", mode, first.code, first.stdout, first.stderr)
		}
		committed := readFile(t, f.statePath())
		second := f.cli(t, "orch", request...)
		if second.code != 0 {
			t.Fatalf("%s replay exit %d: %s %s", mode, second.code, second.stdout, second.stderr)
		}
		if !bytes.Equal(committed, readFile(t, f.statePath())) {
			t.Fatal("replay changed state")
		}
		if mode == "json" && !strings.Contains(string(second.stdout), `"changed": false`) && !strings.Contains(string(second.stdout), `"changed":false`) {
			t.Fatalf("replay did not report changed=false: %s", second.stdout)
		}
		b, a := stateDocument(t, f, before, snapshot)
		if mode == "json" {
			f.harness().Check(t, oracle.JSON, name+".json", jsonDocument(t, f, second))
			f.harness().CheckState(t, name+".txt", b, a)
			jsonState = a
		} else {
			f.harness().Check(t, oracle.Human, name+".txt", humanDocument(t, f, second))
			if !bytes.Equal(jsonState, a) {
				t.Fatalf("json/human final state differs for %s", name)
			}
		}
	}
}
