package lifecycle

import "testing"

func TestOracleHandoff(t *testing.T) {
	linuxOnly(t)
	runCases(t, "handoff",
		lifecycleCase{name: "success", fixture: "executing", caller: "coder-1", args: []string{"handoff", "task-1", "oracle summary", "oracle next"}, mutating: true},
		lifecycleCase{name: "refusal-other-doer", fixture: "executing-second-doer", caller: "coder-2", args: []string{"handoff", "task-1", "oracle summary", "oracle next"}, mutating: true})
}
