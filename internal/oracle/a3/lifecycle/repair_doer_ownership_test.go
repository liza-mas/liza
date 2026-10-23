package lifecycle

import "testing"

func TestOracleRepairDoerOwnership(t *testing.T) {
	linuxOnly(t)
	// L15 owns the repair's state effect; validate's output belongs to A1.
	runCases(t, "validate-repair",
		lifecycleCase{name: "success", fixture: "executing-dead", args: []string{"validate", "--repair"}, modes: jsonMode, mutating: true, stateOnly: true})
}
