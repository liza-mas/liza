package a4

import (
	"bytes"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

// TestOracleDirectEdgeRepairs pins the decisions TestOracleRetargetDependency
// and TestOracleRepairSupersededDependencies leave open: the cycle and
// executing-consumer refusals a retarget reaches after its edge checks, the
// terminal lineage repair, the repair-request clearing, the operator path, the
// no-illegal-edge refusal of the superseded repair, and the replay of an
// identified request for both operations.
func TestOracleDirectEdgeRepairs(t *testing.T) {
	for _, c := range []oracleCase{
		{name: "retarget-dependency/cycle_refused", fixture: "direct-cycle", identity: "orch", json: true, wantExit: 1,
			contains: "dependency cycle", args: []string{"retarget-dependency", "t1", "m1", "t2", "--reason", "Moved"}},
		{name: "retarget-dependency/executing_refused", fixture: "direct-executing", identity: "orch", json: true, wantExit: 1,
			contains: "is executing (IMPLEMENTING_CODE)", args: []string{"retarget-dependency", "consumer", "m1", "pending", "--reason", "Moved"}},
		{name: "retarget-dependency/lineage_repair", fixture: "direct-lineage", identity: "orch", json: true,
			contains: "cp-1-replan-1", args: []string{"retarget-dependency", "cp-2", "cp-1", "cp-1-replan-1", "--reason", "Repair lineage"}},
		{name: "retarget-dependency/repair_request_cleared", fixture: "direct-repair-request", identity: "orch", json: true,
			contains: "t3", args: []string{"retarget-dependency", "t2", "t1", "t3", "--reason", "Moved"}},
		{name: "retarget-dependency/operator", fixture: "graph", identity: "op", json: true,
			contains: "t3", args: []string{"retarget-dependency", "t2", "t1", "t3", "--reason", "Moved"}},
		{name: "repair-superseded-dependencies/none_refused", fixture: "direct-superseded-clean", identity: "orch", json: true, wantExit: 1,
			contains: "task p1 has no illegal downstream dependencies", args: []string{"repair-superseded-dependencies", "p1", "--reason", "Repair"}},
	} {
		t.Run(c.name, func(t *testing.T) { runCase(t, c) })
	}
	t.Run("retarget-dependency/replay", func(t *testing.T) {
		runReplayCase(t, "retarget-dependency/replay", "graph", "t2",
			"retarget-dependency", "t2", "t1", "t3", "--reason", "Moved")
	})
	t.Run("repair-superseded-dependencies/replay", func(t *testing.T) {
		runReplayCase(t, "repair-superseded-dependencies/replay", d35Fixture, "p1",
			"repair-superseded-dependencies", "p1", "--reason", "Repair")
	})
}

// runReplayCase runs one identified orchestrator request twice against the same
// fixture and freezes the second outcome with the state both runs left, so the
// golden shows the original completion replayed and a single history entry.
// The expected transition is read before the first run, as a retrying caller
// preserves it.
func runReplayCase(t *testing.T, name, fixtureName, taskID string, args ...string) {
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
		request := append(append([]string(nil), args...), "--request-id", "replay-1", "--expected-transition", expected)
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
