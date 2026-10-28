package lifecycle

import "testing"

func TestOracleRecoverAgent(t *testing.T) {
	linuxOnly(t)
	args := []string{"recover-agent", "coder-1"}
	runCases(t, "recover-agent",
		lifecycleCase{name: "success", fixture: "executing-dead", args: args, mutating: true},
		lifecycleCase{name: "refusal-live-agent", fixture: "executing", args: args, mutating: true})
}
