package lifecycle

import "testing"

// TestOracleAttemptRollover freezes the claim-triggered attempt rollover: a
// rejected task at its coder iteration limit moves to attempt 2 (sentinel,
// finalisation, worktree cleanup); on attempt 2 the same limit blocks instead.
func TestOracleAttemptRollover(t *testing.T) {
	linuxOnly(t)
	claim := []string{"claim-task", "task-1", "coder-1"}
	runCases(t, "claim-task",
		lifecycleCase{name: "rollover-attempt-one", fixture: "rejected-limit", caller: "coder-1", args: claim, mutating: true},
		lifecycleCase{name: "rollover-refusal-attempt-two", fixture: "rejected-limit-attempt-two", caller: "coder-1", args: claim, mutating: true})
}
