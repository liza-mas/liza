package lifecycle

import "testing"

func TestOracleWriteCheckpoint(t *testing.T) {
	linuxOnly(t)
	args := []string{"write-checkpoint", "task-1", "--intent", "oracle-intent", "--validation-plan", "oracle-plan", "--files-to-modify", "a.go"}
	runCases(t, "write-checkpoint",
		lifecycleCase{name: "success", fixture: "executing", caller: "coder-1", args: args, mutating: true},
		lifecycleCase{name: "refusal-not-executing", fixture: "blocked", caller: "coder-1", args: args, mutating: true})
}
