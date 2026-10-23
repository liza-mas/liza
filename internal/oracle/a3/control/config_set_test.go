package control

import "testing"

func TestOracleConfigSet(t *testing.T) {
	cases := []controlCase{}
	for _, key := range []struct{ name, key, value string }{
		{"post-worktree-cmd", "post_worktree_cmd", "oracle-setup"},
		{"auto-resume", "auto_resume", "true"},
		{"max-instances", "max_instances", "5"},
		{"doer-max-wait", "doer_max_wait", "900"},
		{"reviewer-max-wait", "reviewer_max_wait", "900"},
		{"await-poll-interval", "await_poll_interval", "20"},
		{"runtime-input-registry", "runtime_input_registry", "specs/runtime-inputs.yaml"},
		{"agent-env-deny", "agent_env_deny", "ORACLE_DENY_A,ORACLE_DENY_B"},
	} {
		cases = append(cases, controlCase{name: key.name, fixture: "base", args: []string{"config", "set", "config." + key.key, key.value}, json: true, state: true})
	}
	cases = append(cases,
		controlCase{name: "replace", fixture: "max-instances", args: []string{"config", "set", "config.max_instances", "5", "--replace", "--reason", "wider"}, json: true, state: true},
		controlCase{name: "refused-existing", fixture: "max-instances", args: []string{"config", "set", "config.max_instances", "5"}, code: 1, json: true, state: true},
		controlCase{name: "refused-unsupported", fixture: "base", args: []string{"config", "set", "config.nope", "1"}, code: 1, json: true, state: true},
		controlCase{name: "refused-agent", fixture: "base", args: []string{"config", "set", "config.max_instances", "5"}, code: 1, json: true, state: true, agent: true},
	)
	// runCases asserts each fresh human after-state equals its JSON counterpart.
	runCases(t, "config-set", cases)
}
