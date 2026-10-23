package control

import "testing"

func TestOracleDeleteAgent(t *testing.T) {
	runCases(t, "delete-agent", []controlCase{
		{name: "delete", fixture: "base", args: []string{"delete", "agent", "coder-2"}, state: true},
		{name: "refused-active-lease", fixture: "active-lease", args: []string{"delete", "agent", "coder-3"}, code: 1, state: true},
	})
}
