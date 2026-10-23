package lifecycle

import "testing"

func TestOracleClaimTask(t *testing.T) {
	linuxOnly(t)
	runCases(t, "claim-task",
		lifecycleCase{name: "success", fixture: "draft", caller: "coder-1", args: []string{"claim-task", "task-1", "coder-1"}, mutating: true},
		lifecycleCase{name: "refusal-not-claimable", fixture: "executing", caller: "coder-1", args: []string{"claim-task", "task-1", "coder-1"}, mutating: true})
}
