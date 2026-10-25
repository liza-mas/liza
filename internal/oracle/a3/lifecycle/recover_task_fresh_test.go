package lifecycle

import "testing"

// TestOracleRecoverTaskFresh freezes the destructive `recover-task --fresh`
// path: integration preflight, worktree cleanup and recreation from the
// integration branch, snapshot agent release and the BLOCKED target.
func TestOracleRecoverTaskFresh(t *testing.T) {
	linuxOnly(t)
	args := []string{"recover-task", "task-1", "--fresh"}
	runCases(t, "recover-task",
		lifecycleCase{name: "fresh-success", fixture: "executing-dead", args: args, mutating: true},
		lifecycleCase{name: "fresh-refusal-live-assignee", fixture: "executing", args: args, mutating: true},
		lifecycleCase{name: "fresh-blocked", fixture: "blocked-dead", args: args, mutating: true})
}
