package lifecycle

import "testing"

func TestOracleReleaseClaim(t *testing.T) {
	linuxOnly(t)
	runCases(t, "release-claim",
		lifecycleCase{name: "success", fixture: "executing-dead", args: []string{"release-claim", "task-1", "--role", "doer", "--force"}, mutating: true},
		lifecycleCase{name: "refusal-unexpired-lease", fixture: "executing", args: []string{"release-claim", "task-1", "--role", "doer"}, mutating: true})
}
