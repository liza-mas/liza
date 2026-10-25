package a4

import "testing"

// TestOracleCheckpointPaths pins the sprint-checkpoint decisions the success and
// refused cases leave open: the trigger detected when none is given, the report
// sections a populated sprint produces, the previous checkpoint carried into the
// pending summary, the COMPLETED and ABORTED refusals, and the completion
// precondition a SPRINT_COMPLETE checkpoint is refused by.
func TestOracleCheckpointPaths(t *testing.T) {
	for _, c := range []oracleCase{
		{name: "sprint-checkpoint/trigger_planning_complete", fixture: "checkpoint-planning-complete", identity: "orch", args: []string{"sprint-checkpoint"}, json: true, contains: "sprint_summary.md"},
		{name: "sprint-checkpoint/trigger_many_to_one", fixture: "checkpoint-many-to-one", identity: "orch", args: []string{"sprint-checkpoint"}, json: true, contains: "sprint_summary.md"},
		{name: "sprint-checkpoint/report_sections", fixture: "checkpoint-report-sections", identity: "orch", args: []string{"sprint-checkpoint"}, json: true, contains: "sprint_summary.md"},
		{name: "sprint-checkpoint/previous_checkpoint", fixture: "checkpoint-previous", identity: "orch", args: []string{"sprint-checkpoint"}, json: true, contains: "sprint_summary.md"},
		{name: "sprint-checkpoint/completed_refused", fixture: "checkpoint-completed", identity: "orch", args: []string{"sprint-checkpoint"}, json: true, wantExit: 1, contains: "cannot checkpoint: sprint is COMPLETED"},
		{name: "sprint-checkpoint/aborted_refused", fixture: "checkpoint-aborted", identity: "orch", args: []string{"sprint-checkpoint"}, json: true, wantExit: 1, contains: "cannot checkpoint: sprint is ABORTED"},
		{name: "sprint-checkpoint/sprint_complete_refused", fixture: "checkpoint-planning-complete", helper: "checkpoint-sprint-complete", summary: "refused: failed to update sprint status: validation failed: integration is not effectively complete: planning_unsettled", contains: "planning_unsettled"},
	} {
		t.Run(c.name[len("sprint-checkpoint/"):], func(t *testing.T) { runCase(t, c) })
	}
}
