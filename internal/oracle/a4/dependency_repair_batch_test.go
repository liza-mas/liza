package a4

import "testing"

// TestOracleApplyDependencyRepairBatch pins the decisions
// TestOracleApplyDependencyRepair leaves open for the declarative batch: the
// stale expected list, the complete-candidate refusal, the terminal update, the
// missing request, a batch that leaves the source untouched, the executing
// consumer, the replay of an identified request and the operator path.
func TestOracleApplyDependencyRepairBatch(t *testing.T) {
	apply := []string{"apply-dependency-repair", "b1", "--reason", "Apply"}
	for _, c := range []oracleCase{
		{name: "apply-dependency-repair/stale_refused", fixture: "batch-stale", identity: "orch", json: true, wantExit: 1,
			contains: "task t2 dependencies changed since the repair request was created", args: apply},
		{name: "apply-dependency-repair/invalid_candidate_refused", fixture: "batch-cycle", identity: "orch", json: true, wantExit: 1,
			contains: "failed to apply dependency repair", args: apply},
		{name: "apply-dependency-repair/terminal_refused", fixture: "batch-terminal", identity: "orch", json: true, wantExit: 1,
			contains: "cannot apply dependency repair to terminal task m1 (MERGED)", args: apply},
		{name: "apply-dependency-repair/no_request_refused", fixture: "batch-no-request", identity: "orch", json: true, wantExit: 1,
			contains: "blocked task b1 has no repair request", args: apply},
		{name: "apply-dependency-repair/batch_without_source", fixture: "batch-without-source", identity: "orch", json: true,
			contains: "t3", args: apply},
		{name: "apply-dependency-repair/executing_refused", fixture: "batch-executing", identity: "orch", json: true, wantExit: 1,
			contains: "is executing (IMPLEMENTING_CODE)", args: apply},
		{name: "apply-dependency-repair/operator", fixture: "repair-request", identity: "op", json: true,
			contains: "t2", args: apply},
	} {
		t.Run(c.name, func(t *testing.T) { runCase(t, c) })
	}
	t.Run("apply-dependency-repair/replay", func(t *testing.T) {
		runReplayCase(t, "apply-dependency-repair/replay", "repair-request", "b1", apply...)
	})
}
