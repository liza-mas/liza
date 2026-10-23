package lifecycle

import (
	"strings"
	"testing"
)

// Observed from get task-1 on the executing fixture. Kept constant so a
// change in transition derivation is visible rather than learned at run time.
const executingTransition = "d97b8e2dade34ed006a5ad08fcf0eaa5d8da4d627b0ad43094738fe76959a312"

func TestOracleLifecycleReceipts(t *testing.T) {
	linuxOnly(t)
	base := []string{"mark-blocked", "task-1", "--reason", "oracle-reason", "--questions", "oracle-question"}
	replay := append(append([]string(nil), base...), "--request-id", "req-1", "--expected-transition", executingTransition)
	stale := append(append([]string(nil), base...), "--request-id", "req-2", "--expected-transition", strings.Repeat("0", 64))
	runCases(t, "lifecycle-receipts",
		lifecycleCase{name: "replay", fixture: "executing", caller: "coder-1", invocations: [][]string{replay, replay}, modes: jsonMode, mutating: true},
		lifecycleCase{name: "stale", fixture: "executing", caller: "coder-1", args: stale, modes: jsonMode, mutating: true})
}
