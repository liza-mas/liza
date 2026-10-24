package a4

import "testing"

func TestOracleOperatorOnly(t *testing.T) {
	// Each command has well-formed success-path flags and a registered orch
	// identity. Match the operator-only text as well as the frozen exit, code
	// and unchanged state, so another precondition failure cannot satisfy O1.
	for _, c := range []struct {
		name string
		args []string
	}{
		{"archive-terminal-tasks", []string{"archive-terminal-tasks"}},
		{"archive-terminal-tasks_restore-inline", []string{"archive-terminal-tasks", "--restore-inline"}},
		{"defer-provider-dependency", []string{"defer-provider-dependency", "cp-1", "--provider-task", "cp-1", "--transition", "code-plan-to-coding", "--reason", "Hold only writers"}},
		{"repair-arch-ref", []string{"repair-arch-ref", "cp-1", "--arch-ref", "specs/architecture/a.md#Scope 1: One", "--reason", "Repair"}},
		{"add-human-note", []string{"add-human-note", "t1", "--note-file", "@input:note.txt"}},
		{"plan-check_clear", []string{"plan-check", "cp-1", "--clear"}},
		{"plan-check_replaced-by", []string{"plan-check", "cp-1", "--replaced-by", "cp-1"}},
		{"plan-check_retire", []string{"plan-check", "cp-1", "--retire", "--ruling", "No replacement"}},
		{"amend-plan_withdraw-pending", []string{"amend-plan", "cp-1", "--withdraw-pending", "cp-1-amend-1", "--reason", "Withdraw"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			runCase(t, oracleCase{name: "operator-only/" + c.name, fixture: "plan-in-progress", identity: "orch", args: c.args, json: true, wantExit: 1, contains: "operator-only"})
		})
	}
}
