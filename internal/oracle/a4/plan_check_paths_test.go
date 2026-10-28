package a4

import "testing"

// TestOraclePlanCheckPaths pins the plan-check decisions TestOraclePlanCheck
// leaves open: the admission refusals a pass reaches before it records
// anything, a pass carrying validation notes, a hold replayed or contradicted,
// and the release of a hold on a plan that no longer awaits hand-off.
func TestOraclePlanCheckPaths(t *testing.T) {
	for _, c := range []oracleCase{
		{name: "plan-check/pass_upstream_refused", fixture: "plan-pass-upstream", identity: "orch", json: true, wantExit: 1,
			contains: "task cp-1 cannot pass while upstream cp-c", args: []string{"plan-check", "cp-1", "--pass"}},
		{name: "plan-check/pass_replacement_refused", fixture: "plan-pass-superseding", identity: "orch", json: true, wantExit: 1,
			contains: "output[0] supersedes t1, which is already MERGED", args: []string{"plan-check", "cp-1", "--pass"}},
		{name: "plan-check/pass_notes", fixture: "plan-pending", identity: "orch", json: true,
			contains: "passed", args: []string{"plan-check", "cp-1", "--pass", "--notes-file", "@input:plan-notes.json"}},
		{name: "plan-check/hold_replay", fixture: "plan-held", identity: "orch", json: true,
			contains: "Confirm scope", args: []string{"plan-check", "cp-1", "--hold", "Confirm scope"}},
		{name: "plan-check/hold_conflict_refused", fixture: "plan-held", identity: "orch", json: true, wantExit: 1,
			contains: "is already held for", args: []string{"plan-check", "cp-1", "--hold", "Other ask"}},
		{name: "plan-check/clear_not_source", fixture: "plan-held-expanded", identity: "op", json: true,
			contains: "cp-1", args: []string{"plan-check", "cp-1", "--clear"}},
	} {
		t.Run(c.name[len("plan-check/"):], func(t *testing.T) { runCase(t, c) })
	}
}
