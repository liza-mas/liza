package a4

import "testing"

func TestOracleProceed(t *testing.T) {
	for _, c := range []oracleCase{
		{name: "proceed/success", fixture: "plan-completed", identity: "orch", args: []string{"proceed", "cp-1", "code-plan-to-coding"}, contains: "cp-1-code-0"},
		{name: "proceed/refused", fixture: "plan-in-progress", identity: "orch", args: []string{"proceed", "cp-1", "code-plan-to-coding"}, wantExit: 1, contains: "sprint must be COMPLETED before proceeding (current: IN_PROGRESS)"},
	} {
		t.Run(c.name[len("proceed/"):], func(t *testing.T) { runCase(t, c) })
	}
}

func TestOracleAutoTransitions(t *testing.T) {
	t.Run("executes", func(t *testing.T) {
		runCase(t, oracleCase{name: "auto-transitions/executes", fixture: "auto-transition", helper: "auto-transitions", summary: "children: [cpm-1-cp-0]"})
	})
	t.Run("none_eligible", func(t *testing.T) {
		runCase(t, oracleCase{name: "auto-transitions/none_eligible", fixture: "base", helper: "auto-transitions", summary: "children: []"})
	})
}

func TestOracleReplan(t *testing.T) {
	for _, c := range []oracleCase{
		{name: "replan/success", fixture: "plan-in-progress", identity: "orch", args: []string{"replan", "cp-1", "--reason", "Scope changed"}, contains: "cp-1-replan-1"},
		{name: "replan/refused", fixture: "plan-with-children", identity: "orch", args: []string{"replan", "cp-1", "--reason", "Scope changed"}, wantExit: 1, contains: "child tasks already created from task cp-1 output"},
		{name: "replan/preserve", fixture: "plan-in-progress", identity: "orch", args: []string{"replan", "cp-1", "--preserve-output-identity", "--reason", "Review again"}, contains: "cp-1-replan-1"},
		{name: "replan/preserve_refused", fixture: "plan-completed", identity: "orch", args: []string{"replan", "cp-1", "--preserve-output-identity", "--reason", "Review again"}, wantExit: 1, contains: "sprint must be at CHECKPOINT or IN_PROGRESS"},
	} {
		t.Run(c.name[len("replan/"):], func(t *testing.T) { runCase(t, c) })
	}
}
