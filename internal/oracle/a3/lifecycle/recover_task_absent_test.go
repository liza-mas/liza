package lifecycle

import "testing"

// TestOracleRecoverTaskAbsent freezes the absent-task cleanup path: task-9 is
// not in state, so --force cleans leftover artifacts and the unforced call is
// refused. The executing-dead fixture's dead PID satisfies the OC4 guard. The
// -artifacts cases give task-9 a real branch and worktree: --force removes
// them and leaves the task untouched, the unforced refusal preserves them.
func TestOracleRecoverTaskAbsent(t *testing.T) {
	linuxOnly(t)
	runCases(t, "recover-task",
		lifecycleCase{name: "absent-force", fixture: "executing-dead", args: []string{"recover-task", "task-9", "--force"}, mutating: true},
		lifecycleCase{name: "absent-refusal", fixture: "executing-dead", args: []string{"recover-task", "task-9"}, mutating: true},
		lifecycleCase{name: "absent-force-artifacts", fixture: "executing-dead+absent-artifacts", args: []string{"recover-task", "task-9", "--force"}, mutating: true},
		lifecycleCase{name: "absent-refusal-artifacts", fixture: "executing-dead+absent-artifacts", args: []string{"recover-task", "task-9"}, mutating: true})
}
