package lifecycle

import "testing"

// TestOracleReleaseReviewer freezes the reviewer branch of release-claim and
// of recover-agent for a task under review by a dead reviewer.
func TestOracleReleaseReviewer(t *testing.T) {
	linuxOnly(t)
	runCases(t, "release-claim",
		lifecycleCase{name: "reviewer-release", fixture: "reviewing-dead", args: []string{"release-claim", "task-1", "--role", "reviewer", "--force"}, mutating: true})
	runCases(t, "recover-agent",
		lifecycleCase{name: "reviewer-release", fixture: "reviewing-dead", args: []string{"recover-agent", "code-reviewer-1"}, mutating: true})
}
