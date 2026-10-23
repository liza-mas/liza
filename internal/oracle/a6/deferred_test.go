package a6

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func TestOracleDeferredValidation(t *testing.T) {
	platformGate(t)
	all := []oracle.Kind{oracle.JSON, oracle.Human, oracle.State}
	output := []oracle.Kind{oracle.JSON, oracle.Human}
	cases := []struct {
		name, input, action, outcome, refusal string
		args                                  []string
		kinds                                 []oracle.Kind
		locked, operatorGuard                 bool
	}{
		{name: "status", input: "status", kinds: output},
		{name: "rule", input: "empty", action: "rule", outcome: "set", args: []string{"--command", "true", "--ruling", "oracle ruling"}, kinds: all},
		{name: "rule_refused", input: "empty", action: "rule", refusal: "command[0] must not have leading or trailing whitespace", args: []string{"--command", " true", "--ruling", "oracle ruling"}, kinds: all},
		{name: "unrule", input: "ruled", action: "unrule", outcome: "removed", args: []string{"--command", "true"}, kinds: all},
		{name: "unrule_refused", input: "ruled", action: "unrule", refusal: "command[0] must not have leading or trailing whitespace", args: []string{"--command", " true"}, kinds: all},
		{name: "retire", input: "command", action: "retire", outcome: "retired", args: []string{"--command", "true", "--ruling", "oracle ruling"}, kinds: all},
		{name: "retire_refused", input: "command", action: "retire", refusal: "command[0] must not have leading or trailing whitespace", args: []string{"--command", " true", "--ruling", "oracle ruling"}, kinds: all},
		{name: "settle", input: "unsettled", action: "settle", outcome: "settled", args: []string{"--ruling", "oracle confirmed no runner"}, kinds: all},
		{name: "settle_refused", input: "unsettled", action: "settle", refusal: "a deferred-validation runner holds the lock; it is live and cannot be settled", args: []string{"--ruling", "oracle confirmed no runner"}, kinds: all, locked: true},
		{name: "abort", input: "runner", action: "abort", outcome: "requested", args: []string{"--ruling", "oracle abort ruling"}, kinds: all, locked: true},
		{name: "abort_refused", input: "runner", action: "abort", refusal: "no deferred-validation runner is live", args: []string{"--ruling", "oracle abort ruling"}, kinds: all},
		{name: "run", input: "command", action: "run", outcome: "ran", kinds: all},
		{name: "run_locked", input: "runner", action: "run", outcome: "skipped", kinds: all, locked: true},
		{name: "operator_only", input: "empty", action: "rule", refusal: "is operator-only; agent sessions cannot change deferred-validation authority", args: []string{"--command", "true", "--ruling", "oracle ruling"}, kinds: output, operatorGuard: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			action := tc.action
			if action == "" {
				action = "status"
			}
			c := caseSpec{
				setup: func(t *testing.T) *fixture {
					f := baseFixture(t)
					f.install(t, "deferred/"+tc.input+".yaml")
					if tc.locked {
						f.release = holdOracleRunnerLock(t, f.root)
					}
					return f
				},
				args:    append([]string{"deferred-validation", action}, tc.args...),
				kinds:   tc.kinds,
				noDelta: tc.refusal != "" || tc.name == "run_locked",
				check: func(t *testing.T, _ *fixture, got observation) {
					if tc.refusal != "" {
						if !bytes.Contains(append(bytes.Clone(got.stdout), got.stderr...), []byte(tc.refusal)) {
							t.Fatalf("did not reach deferred-validation %s refusal", tc.name)
						}
						return
					}
					if tc.outcome != "" {
						requireDeferredOutcome(t, got, tc.outcome)
					}
					if tc.name == "run_locked" && !bytes.Contains(got.stdout, []byte("runner_active")) {
						t.Fatal("held runner lock did not determine the no-op")
					}
					if tc.name == "run" {
						if !bytes.Contains(got.stdout, []byte("passed")) {
							t.Fatal("deferred command did not produce a passed batch")
						}
					}
				},
			}
			if tc.refusal != "" {
				c.exit = 1
			}
			if tc.operatorGuard {
				c.extra = []string{"LIZA_AGENT_ID=oracle-agent"}
			}
			runCase(t, c)
		})
	}
}

func requireDeferredOutcome(t *testing.T, got observation, want string) {
	t.Helper()
	var envelope struct {
		OK     bool `json:"ok"`
		Result struct {
			Outcome string `json:"outcome"`
		} `json:"result"`
	}
	if json.Unmarshal(got.stdout, &envelope) == nil {
		if !envelope.OK || envelope.Result.Outcome != want {
			t.Fatalf("deferred-validation outcome differs from %q", want)
		}
		return
	}
	if !strings.Contains(string(got.stdout), "outcome: "+want) {
		t.Fatalf("human deferred-validation outcome differs from %q", want)
	}
}
