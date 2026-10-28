package lifecycle

import "testing"

func TestOracleAssessBlocked(t *testing.T) {
	linuxOnly(t)
	args := []string{"assess-blocked", "task-1", "--note", "oracle-note"}
	runCases(t, "assess-blocked",
		lifecycleCase{name: "success", fixture: "blocked", caller: "orchestrator-1", args: args, mutating: true},
		lifecycleCase{name: "refusal-not-blocked", fixture: "executing", caller: "orchestrator-1", args: args, mutating: true})
}
