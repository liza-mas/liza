package lifecycle

import "testing"

// TestOracleClaimTaskVariants freezes the claim strategies beyond the fresh
// claim of TestOracleClaimTask: integration-fix, preserved-initial and
// rejected, each with a success and a refusal that reaches its own strategy. The rejected
// refusals are the review-budget escalations: a new attempt, then BLOCKED.
func TestOracleClaimTaskVariants(t *testing.T) {
	linuxOnly(t)
	claim := []string{"claim-task", "task-1", "coder-1"}
	runCases(t, "claim-task",
		lifecycleCase{name: "integration-fix-success", fixture: "integration-failed", caller: "coder-1", args: claim, mutating: true},
		lifecycleCase{name: "integration-fix-refusal-worktree-missing", fixture: "integration-failed-no-worktree", caller: "coder-1", args: claim, mutating: true},
		lifecycleCase{name: "preserved-initial-success", fixture: "preserved-initial", caller: "coder-1", args: claim, mutating: true},
		lifecycleCase{name: "preserved-initial-refusal-base-unresolvable", fixture: "preserved-initial-bad-base", caller: "coder-1", args: claim, mutating: true},
		lifecycleCase{name: "rejected-success", fixture: "rejected", caller: "coder-1", args: claim, mutating: true},
		lifecycleCase{name: "rejected-refusal-review-budget-new-attempt", fixture: "rejected-review-budget", caller: "coder-1", args: claim, mutating: true},
		lifecycleCase{name: "rejected-refusal-review-budget-blocked", fixture: "rejected-review-budget-attempt-2", caller: "coder-1", args: claim, mutating: true})
}
