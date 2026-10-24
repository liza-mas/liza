package lifecycle

import "testing"

func TestOracleAssessHypothesisExhausted(t *testing.T) {
	linuxOnly(t)
	args := []string{"assess-hypothesis-exhausted", "task-1"}
	runCases(t, "assess-hypothesis-exhausted",
		lifecycleCase{name: "success", fixture: "blocked-failed-two", caller: "orchestrator-1", args: args, mutating: true},
		lifecycleCase{name: "refusal-one-failure", fixture: "blocked-failed-one", caller: "orchestrator-1", args: args, mutating: true})
}
