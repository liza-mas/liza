package lifecycle

import "testing"

func TestOracleCheckCommitAllowed(t *testing.T) {
	linuxOnly(t)
	args := []string{"check-commit-allowed", "task-1"}
	runCases(t, "check-commit-allowed",
		lifecycleCase{name: "allowed-executing", fixture: "executing", caller: "coder-1", args: args},
		lifecycleCase{name: "allowed-blocked", fixture: "blocked", caller: "coder-1", args: args},
		lifecycleCase{name: "denied-review", fixture: "review", caller: "coder-1", args: args})
}
