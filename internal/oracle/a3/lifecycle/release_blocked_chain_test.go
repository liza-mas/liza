package lifecycle

import "testing"

func TestOracleReleaseBlockedChain(t *testing.T) {
	linuxOnly(t)
	args := []string{"release-blocked-chain", "task-1", "--reason", "oracle-reason"}
	runCases(t, "release-blocked-chain",
		lifecycleCase{name: "success", fixture: "blocked-capped", args: args, mutating: true},
		lifecycleCase{name: "refusal-below-cap", fixture: "blocked", args: args, mutating: true})
}
