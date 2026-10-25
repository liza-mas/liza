package a4

import "testing"

// TestOracleReplanPaths pins the default replan paths TestOracleReplan leaves
// open: sprint resume, the first eligibility refusals reached under the lock,
// and the two lineage rewrites replan performs on neighbouring tasks.
func TestOracleReplanPaths(t *testing.T) {
	for _, c := range []oracleCase{
		{name: "replan/checkpoint", fixture: "replan-checkpoint", identity: "orch", args: []string{"replan", "cp-1", "--reason", "Scope changed"}, contains: "cp-1-replan-1"},
		{name: "replan/held_refused", fixture: "plan-held", identity: "orch", args: []string{"replan", "cp-1", "--reason", "Scope changed"}, wantExit: 1,
			contains: "task cp-1 is held for human action (Confirm scope); an operator must run plan-check cp-1 --clear before replanning"},
		{name: "replan/fenced_refused", fixture: "amendment-abandoned", identity: "orch", args: []string{"replan", "cp-1", "--reason", "Scope changed"}, wantExit: 1,
			contains: "cannot replan a correction or an original with a pending amendment"},
		{name: "replan/selection_retired", fixture: "replan-selection", identity: "orch", args: []string{"replan", "cp-a", "--reason", "Scope changed"},
			contains: "selected specific outputs of replanned task cp-a; selection retired to whole-phase inheritance"},
		{name: "replan/successor_dependency", fixture: "replan-successor", identity: "orch", args: []string{"replan", "cp-2", "--reason", "Scope changed"},
			contains: "dependency on replanned cp-1 follows its successor cp-1-replan-1"},
	} {
		t.Run(c.name[len("replan/"):], func(t *testing.T) { runCase(t, c) })
	}
}
