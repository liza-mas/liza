package a4

import "testing"

// TestOracleProviderWaits pins the provider-wait paths the D5/D6 success and
// first-refusal cases leave open: reservation release and cap, deferral
// buckets, and the retirement-holder and stale-routing policy that
// supersede-task and cancel-task reach through rewriteActiveDependents.
func TestOracleProviderWaits(t *testing.T) {
	reserve := func(task string, extra ...string) []string {
		return append([]string{"reserve-provider", task, "cp-p", "--transition", "code-plan-to-coding", "--reason", "Writer order"}, extra...)
	}
	for _, c := range []oracleCase{
		{name: "wait/reserve/release", fixture: "wait-reserved", identity: "orch", json: true, args: reserve("t1", "--release")},
		{name: "wait/reserve/release_absent", fixture: "provider", identity: "orch", json: true, args: reserve("t1", "--release")},
		{name: "wait/reserve/held_again", fixture: "wait-reserved", identity: "orch", json: true, args: reserve("t1")},
		{name: "wait/reserve/cap", fixture: "provider", identity: "orch", json: true, args: reserve("t1", "--provider-max-outputs", "2")},
		{name: "wait/reserve/cap_raise_refused", fixture: "wait-capped", identity: "orch", json: true, wantExit: 1,
			contains: "provider cp-p max_outputs is 1; reserve-provider only tightens a cap, never raises it", args: reserve("t1", "--provider-max-outputs", "2")},
		{name: "wait/reserve/started_refused", fixture: "wait-started", identity: "orch", json: true, wantExit: 1,
			contains: "writer w1 is IMPLEMENTING_CODE and assigned or leased", args: reserve("w1")},
		{name: "wait/defer/bucket_append", fixture: "wait-bucket", identity: "op", json: true,
			args: []string{"defer-provider-dependency", "cp-x", "--provider-task", "cp-p", "--transition", "code-plan-to-coding", "--reason", "Hold only writers"}},
		{name: "wait/defer/cross_stage_refused", fixture: "wait-cross-stage", identity: "op", json: true, wantExit: 1,
			contains: "only a wait between tasks of the same stage can be deferred",
			args:     []string{"defer-provider-dependency", "cp-x", "--provider-task", "cm-p", "--transition", "code-plan-decompose", "--reason", "Hold only writers"}},
		{name: "wait/retire/multi_holder_refused", fixture: "wait-multi-holder", identity: "orch", json: true, wantExit: 1,
			contains: "cannot retire provider cp-p while cx-1 descendant_dependencies, cx-2 descendant_dependencies have live provider_dependencies declarations",
			args:     []string{"supersede-task", "cp-p", "cp-r", "--reason", "Rescoped"}},
		{name: "wait/retire/selected_child_refused", fixture: "wait-selected-child", identity: "orch", json: true, wantExit: 1,
			contains: "cannot retire provider cp-p-code-0 while cx-1 descendant_dependencies has a live provider_dependencies declaration",
			args:     []string{"cancel-task", "cp-p-code-0", "No longer needed"}},
		{name: "wait/retire/reservation_refused", fixture: "wait-reserved", identity: "orch", json: true, wantExit: 1,
			contains: "cannot retire provider cp-p while t1 provider_reservations has a live provider_dependencies declaration",
			args:     []string{"cancel-task", "cp-p", "No longer needed"}},
		{name: "wait/retire/stale_routing", fixture: "wait-stale", identity: "orch", json: true,
			args: []string{"cancel-task", "cp-p", "No longer needed"}},
	} {
		t.Run(c.name[len("wait/"):], func(t *testing.T) { runCase(t, c) })
	}
}
