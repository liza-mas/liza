package lifecycle

import (
	"strings"
	"testing"
)

// Observed from get task-1 on the draft and stranded-holder fixtures; constant
// so a change in transition derivation is visible rather than learned at run time.
const (
	draftTransition    = "3fc6675ebc987be1ef20110bdd136ebbb6aee4dc72e4e28b13cbbcf6cce9e7d9"
	strandedTransition = "231bb8739866c39c5cc00858cc5156ae38290087857932ddf2e424b5a83a7948"
)

// TestOracleClaimTaskVariants freezes the claim strategies beyond the fresh
// claim of TestOracleClaimTask: integration-fix, preserved-initial and
// rejected, each with a success and a refusal that reaches its own strategy. The rejected
// refusals are the review-budget escalations: a new attempt, then BLOCKED.
// Stranded takeover (an expired, unregistered-liveness holder) and the retained
// claim receipt (exact replay, stale expected transition) are frozen here too.
func TestOracleClaimTaskVariants(t *testing.T) {
	linuxOnly(t)
	claim := []string{"claim-task", "task-1", "coder-1"}
	pinned := func(transition string) []string {
		return append(append([]string(nil), claim...), "--request-id", "req-1", "--expected-transition", transition)
	}
	runCases(t, "claim-task",
		lifecycleCase{name: "integration-fix-success", fixture: "integration-failed", caller: "coder-1", args: claim, mutating: true},
		lifecycleCase{name: "integration-fix-refusal-worktree-missing", fixture: "integration-failed-no-worktree", caller: "coder-1", args: claim, mutating: true},
		lifecycleCase{name: "preserved-initial-success", fixture: "preserved-initial", caller: "coder-1", args: claim, mutating: true},
		lifecycleCase{name: "preserved-initial-refusal-base-unresolvable", fixture: "preserved-initial-bad-base", caller: "coder-1", args: claim, mutating: true},
		lifecycleCase{name: "rejected-success", fixture: "rejected", caller: "coder-1", args: claim, mutating: true},
		lifecycleCase{name: "rejected-refusal-review-budget-new-attempt", fixture: "rejected-review-budget", caller: "coder-1", args: claim, mutating: true},
		lifecycleCase{name: "rejected-refusal-review-budget-blocked", fixture: "rejected-review-budget-attempt-2", caller: "coder-1", args: claim, mutating: true},
		lifecycleCase{name: "stranded-takeover-success", fixture: "stranded-holder", caller: "coder-1", args: claim, mutating: true, maskBoundaries: true},
		lifecycleCase{name: "stranded-takeover-refusal-pinned-transition", fixture: "stranded-holder", caller: "coder-1", args: pinned(strandedTransition), modes: jsonMode, mutating: true},
		lifecycleCase{name: "receipt-replay", fixture: "draft", caller: "coder-1", invocations: [][]string{pinned(draftTransition), pinned(draftTransition)}, modes: jsonMode, mutating: true},
		lifecycleCase{name: "receipt-stale-transition", fixture: "draft", caller: "coder-1", args: pinned(strings.Repeat("0", 64)), modes: jsonMode, mutating: true})
}
