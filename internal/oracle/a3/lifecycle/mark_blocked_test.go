package lifecycle

import "testing"

func TestOracleMarkBlocked(t *testing.T) {
	linuxOnly(t)
	args := []string{"mark-blocked", "task-1", "--reason", "oracle-reason", "--questions", "oracle-question"}
	runCases(t, "mark-blocked",
		lifecycleCase{name: "success", fixture: "executing", caller: "coder-1", args: args, mutating: true},
		lifecycleCase{name: "refusal-not-executing", fixture: "blocked", caller: "coder-1", args: args, mutating: true})
}
