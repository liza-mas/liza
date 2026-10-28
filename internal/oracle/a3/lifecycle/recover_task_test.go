package lifecycle

import "testing"

func TestOracleRecoverTask(t *testing.T) {
	linuxOnly(t)
	args := []string{"recover-task", "task-1"}
	runCases(t, "recover-task",
		lifecycleCase{name: "success", fixture: "executing-dead", args: args, mutating: true},
		lifecycleCase{name: "refusal-live-assignee", fixture: "executing", args: args, mutating: true})
}
