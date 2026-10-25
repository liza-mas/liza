package a4

import "testing"

// TestOracleTransitionPass freezes the pass-level behaviour of
// ExecuteTransitionsReportWith that TestOracleAutoTransitions does not reach:
// per-transition refusal inside a pass, cycle recording, admission skips,
// recovery through the pass and the goal base-commit snapshot.
//
// A dependency cycle cannot exist in a valid state, so cycle_blocked loads its
// fixture from testdata/fixtures/defects, outside the catalogue that
// TestA4FixturesValidate requires to validate.
func TestOracleTransitionPass(t *testing.T) {
	for _, c := range []oracleCase{
		{name: "transition-pass/partial_pass", fixture: "pass-partial", helper: "pass-operator", summary: "results: 1 failures: 1 warnings: 0\nresult cp-1 code-plan-to-coding children=[cp-1-code-0] cohort=[] retired=[]\nfailure task cp-b transition code-plan-to-coding: output[0].inherit_inputs.selections[0]: upstream task \"cp-a\" has 2 output(s), cannot select index 5"},
		{name: "transition-pass/cycle_blocked", fixture: "../defects/pass-cycle", helper: "pass-operator", summary: "results: 0 failures: 2 warnings: 0\nfailure task cp-2 transition code-plan-to-coding: blocked by dependency cycle among [cp-1 cp-2]\nfailure task cp-1 transition code-plan-to-coding: blocked by dependency cycle among [cp-1 cp-2]"},
		{name: "transition-pass/reviewed_skip", fixture: "pass-reviewed-skip", helper: "pass-reviewed", summary: "results: 1 failures: 0 warnings: 0\nresult cp-2 code-plan-to-coding children=[cp-2-code-0] cohort=[] retired=[]"},
		{name: "transition-pass/recovery_pass", fixture: "proceed-recovery-missing-child", helper: "pass-operator", summary: "results: 1 failures: 0 warnings: 0\nresult cp-1 code-plan-to-coding children=[cp-1-code-1] cohort=[] retired=[]"},
		{name: "transition-pass/coding_base_commit", fixture: "plan-completed", helper: "pass-operator", summary: "results: 1 failures: 0 warnings: 0\nresult cp-1 code-plan-to-coding children=[cp-1-code-0] cohort=[] retired=[]"},
	} {
		t.Run(c.name[len("transition-pass/"):], func(t *testing.T) { runCase(t, c) })
	}
}
