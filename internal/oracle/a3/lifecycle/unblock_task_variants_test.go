package lifecycle

import (
	"strings"
	"testing"
)

// Observed from get task-1 on the blocked fixture. Constant so a change in
// transition derivation is visible rather than learned at run time.
const blockedTransition = "036b86fce65ff556a73c3194b6335ed9f95ae066ac3646088edd9352b0bc8eca"

const scopeExceptionFile = `{"files":["internal/example/helper.go"],"change":"add a fixture helper","unchanged":"production code and merged contracts"}`

// TestOracleUnblockTaskVariants freezes the unblock-task options that
// TestOracleUnblockTask does not reach: scope exception, budget exception,
// rebase-on, assign-to and new-iteration, each with a success and a refusal at
// the gate that option owns, then the retained receipt (exact replay, stale
// expected transition). These scenarios use Git and CLI state only, no Linux
// process facility (D53), so none is Linux-scoped.
func TestOracleUnblockTaskVariants(t *testing.T) {
	base := []string{"unblock-task", "task-1", "--reason", "oracle-reason"}
	with := func(extra ...string) []string { return append(append([]string(nil), base...), extra...) }
	scope := []setupFile{{name: "scope.json", data: []byte(scopeExceptionFile)}}
	pinned := func(transition string) []string {
		return with("--request-id", "req-1", "--expected-transition", transition)
	}
	runCases(t, "unblock-task",
		lifecycleCase{name: "scope-exception-success", fixture: "blocked", args: with("--scope-exception-file", "{{TMPDIR}}/scope.json"), setupFiles: scope, mutating: true},
		lifecycleCase{name: "scope-exception-refusal-non-coding-task", fixture: "blocked-planning", args: with("--scope-exception-file", "{{TMPDIR}}/scope.json"), setupFiles: scope, mutating: true},
		lifecycleCase{name: "budget-exception-success", fixture: "blocked", args: with("--budget-exception", "7200", "--budget-ruling", "D-oracle-1"), mutating: true},
		lifecycleCase{name: "budget-exception-refusal-non-coding-task", fixture: "blocked-planning", args: with("--budget-exception", "7200", "--budget-ruling", "D-oracle-1"), mutating: true},
		lifecycleCase{name: "rebase-on-success", fixture: "blocked+integration-ahead", args: with("--rebase-on", "integration"), mutating: true},
		lifecycleCase{name: "rebase-on-refusal-tracked-changes", fixture: "blocked+dirty-worktree", args: with("--rebase-on", "integration"), mutating: true},
		lifecycleCase{name: "rebase-on-refusal-conflict", fixture: "blocked+integration-conflict", args: with("--rebase-on", "integration"), mutating: true},
		lifecycleCase{name: "assign-to-success", fixture: "blocked", args: with("--assign-to", "coder-1"), mutating: true},
		lifecycleCase{name: "assign-to-refusal-unregistered-agent", fixture: "blocked", args: with("--assign-to", "coder-9"), mutating: true},
		lifecycleCase{name: "new-iteration-success", fixture: "blocked", args: with("--new-iteration"), mutating: true},
		lifecycleCase{name: "new-iteration-assign-to-success", fixture: "blocked", args: with("--new-iteration", "--assign-to", "coder-1"), mutating: true},
		lifecycleCase{name: "new-iteration-refusal-rca-continues-iteration", fixture: "blocked-rca-assign", args: with("--new-iteration"), mutating: true},
		lifecycleCase{name: "receipt-replay", fixture: "blocked", caller: "orchestrator-1", invocations: [][]string{pinned(blockedTransition), pinned(blockedTransition)}, modes: jsonMode, mutating: true},
		lifecycleCase{name: "receipt-stale-transition", fixture: "blocked", caller: "orchestrator-1", args: pinned(strings.Repeat("0", 64)), modes: jsonMode, mutating: true})
}
