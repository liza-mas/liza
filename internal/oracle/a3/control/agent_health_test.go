package control

import "testing"

func TestOracleAgentHealth(t *testing.T) {
	runCases(t, "agent-health", []controlCase{
		{name: "mark-degraded", fixture: "base", args: []string{"mark-agent-degraded", "coder-2", "--reason", "provider-unavailable", "--role", "coder", "--error", "oracle-error"}, state: true},
		{name: "clear-degraded", fixture: "degraded", args: []string{"clear-agent-degraded", "coder-2"}, state: true},
		{name: "mark-unregistered", fixture: "base", args: []string{"mark-agent-degraded", "nobody", "--reason", "provider-unavailable", "--role", "coder", "--error", "oracle-error"}, state: true},
		{name: "clear-not-degraded", fixture: "base", args: []string{"clear-agent-degraded", "coder-1"}, state: true},
	})
}
