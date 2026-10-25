package a4

import "testing"

// TestOracleVocabularyPaths pins the analyze, disposition, note and metrics
// outcomes the success and refused cases leave open, before the A4 K7 inventory
// names the values that decide them: the provider-audit CHECKPOINT response and
// its active rerun, the dismissed and task-reference dispositions and the
// malformed-disposition refusal, the broadcast note target, and the metrics run
// whose approval rates stay unwarned below the sample threshold.
func TestOracleVocabularyPaths(t *testing.T) {
	for _, c := range []oracleCase{
		{name: "analyze/checkpoint_response", fixture: "vocab-analyze-checkpoint", identity: "orch", args: []string{"analyze"}, json: true, contains: "provider_audit_degradation"},
		{name: "analyze/active_checkpoint_rerun", fixture: "vocab-analyze-active-checkpoint", identity: "orch", args: []string{"analyze"}, json: true, contains: "provider_audit_degradation"},
		{name: "set-discovery-disposition/dismissed", fixture: "discovery", identity: "orch", args: []string{"set-discovery-disposition", "disc-1", "dismissed"}, json: true},
		{name: "set-discovery-disposition/task_ref", fixture: "discovery", identity: "orch", args: []string{"set-discovery-disposition", "disc-1", "t1"}, json: true},
		// Same discovery and operator-orchestrator identity; only the disposition is
		// malformed (neither a sentinel nor a task ID), so the first refusal is the
		// disposition syntax check that names both sentinels.
		{name: "set-discovery-disposition/invalid_refused", fixture: "discovery", identity: "orch", args: []string{"set-discovery-disposition", "disc-1", "Not A Disposition"}, json: true, wantExit: 1, contains: "must be a valid task ID"},
		{name: "add-human-note/all", fixture: "base", identity: "op", args: []string{"add-human-note", "all", "--note-file", "@input:note.txt"}, json: true, contains: "all"},
		{name: "update-sprint-metrics/below_threshold", fixture: "vocab-metrics-below", identity: "orch", args: []string{"update-sprint-metrics"}, json: true},
	} {
		t.Run(c.name, func(t *testing.T) { runCase(t, c) })
	}
}
