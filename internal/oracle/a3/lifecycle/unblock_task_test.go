package lifecycle

import "testing"

func TestOracleUnblockTask(t *testing.T) {
	linuxOnly(t)
	args := []string{"unblock-task", "task-1", "--reason", "oracle-reason"}
	runCases(t, "unblock-task",
		lifecycleCase{name: "success", fixture: "blocked", args: args, mutating: true},
		lifecycleCase{name: "refusal-not-blocked", fixture: "executing", args: args, mutating: true})
}
