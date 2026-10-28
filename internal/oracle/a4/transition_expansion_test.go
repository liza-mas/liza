package a4

import "testing"

func init() {
	// One-to-one has no transition in the starting pipeline; the variant turns
	// code-plan-to-coding into one and is committed over the base pipeline.
	fixtureRecipes["proceed-one-to-one"] = []gitStep{
		{Kind: "commit", Label: "one-to-one-pipeline", Path: ".liza/pipeline.yaml", Source: "testdata/fixtures/pipeline-one-to-one.yaml"},
	}
}

// TestOracleProceedExpansion freezes the generation and crash-recovery paths of
// `proceed` beyond the per-subtask success of TestOracleProceed: the other
// cardinalities, selective inheritance, kind deduplication and recovery.
func TestOracleProceedExpansion(t *testing.T) {
	for _, c := range []oracleCase{
		{name: "proceed/one_to_one", fixture: "proceed-one-to-one", identity: "orch", args: []string{"proceed", "cp-1", "code-plan-to-coding"}, contains: "cp-1-code"},
		{name: "proceed/many_to_one", fixture: "proceed-many-to-one", identity: "orch", args: []string{"proceed", "us-1", "us-to-coding"}, contains: "ep-1-arm"},
		{name: "proceed/many_to_one_incomplete", fixture: "proceed-many-to-one-incomplete", identity: "orch", args: []string{"proceed", "us-1", "us-to-coding"}, wantExit: 1, contains: "many-to-one cohort incomplete"},
		{name: "proceed/selective_inheritance", fixture: "proceed-selective-inheritance", identity: "orch", args: []string{"proceed", "cp-b", "code-plan-to-coding"}, contains: "cp-b-code-0"},
		{name: "proceed/kind_dedup", fixture: "proceed-kind-dedup", identity: "orch", args: []string{"proceed", "cp-1", "code-plan-to-coding"}, contains: "cp-1-code-1"},
		{name: "proceed/recovery_missing_child", fixture: "proceed-recovery-missing-child", identity: "orch", args: []string{"proceed", "cp-1", "code-plan-to-coding"}, contains: "cp-1-code-1"},
		{name: "proceed/recovery_complete", fixture: "proceed-recovery-complete", identity: "orch", args: []string{"proceed", "cp-1", "code-plan-to-coding"}, wantExit: 1, contains: "transition already executed"},
	} {
		t.Run(c.name[len("proceed/"):], func(t *testing.T) { runCase(t, c) })
	}
}
