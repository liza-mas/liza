//go:build linux

package a4

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

// D53 (specs/goals/20261023-architecture-consolidation-decisions.md): this
// scenario runs on Linux only. The repair-superseded-dependencies/success_validate_after
// golden freezes the validation outcome of a host whose procfs process scan
// runs (internal/commands/validate.go, validateNoZombieAgents); a host without
// procfs skips that scan with a warning, as
// internal/commands.TestValidateWarnsWhenProcessScanUnavailable characterizes.
// dependencies_d35_other_test.go skips the scenario elsewhere; the refused case
// stays in dependencies_test.go and runs everywhere.

// D35 deliberately starts with the one illegal p1 -> t3 edge. Validation is
// outside the state pair and only in JSON mode, in the order the plan requires.
func runD35Success(t *testing.T) {
	t.Helper()
	gate(t)
	var jsonState []byte
	for _, mode := range []string{"json", "human"} {
		f := newFixture(t, d35Fixture)
		if mode == "json" {
			checkD35Validation(t, f, false)
		}
		before := readFile(t, f.statePath())
		snapshot := projectFiles(t, f.root)
		args := []string{"repair-superseded-dependencies", "p1", "--reason", "Repair"}
		if mode == "json" {
			args = append(args, "--json")
		}
		r := f.cli(t, "orch", args...)
		if r.code != 0 {
			t.Fatalf("D35 %s repair failed: %s %s", mode, r.stdout, r.stderr)
		}
		name := "repair-superseded-dependencies/success"
		b, a := stateDocument(t, f, before, snapshot)
		if mode == "json" {
			f.harness().Check(t, oracle.JSON, name+".json", jsonDocument(t, f, r))
			f.harness().CheckState(t, name+".txt", b, a)
			jsonState = a
			checkD35Validation(t, f, true)
		} else {
			f.harness().Check(t, oracle.Human, name+".txt", humanDocument(t, f, r))
			if !bytes.Equal(jsonState, a) {
				t.Fatal("D35 human/json final states differ")
			}
		}
	}
}

func checkD35Validation(t *testing.T, f *fixture, after bool) {
	t.Helper()
	before := readFile(t, f.statePath())
	r := f.cli(t, "op", "validate", "--json")
	var e struct {
		OK     bool `json:"ok"`
		Result *struct {
			Valid bool `json:"valid"`
		} `json:"result"`
		Error *struct{ Code, Message string } `json:"error"`
	}
	if err := json.Unmarshal(r.stdout, &e); err != nil {
		t.Fatalf("D35 validation did not return JSON: %v", err)
	}
	phase := "before"
	if after {
		phase = "after"
		if r.code != 0 || !e.OK || e.Result == nil || !e.Result.Valid || e.Error != nil {
			t.Fatalf("D35 after must be valid: %s", r.stdout)
		}
	} else {
		want := "task p1 has downstream dependency t3: role_pair coding-pair is downstream of code-planning-pair"
		if r.code != 1 || e.OK || e.Result != nil || e.Error == nil || e.Error.Code != "validation" || strings.TrimSpace(e.Error.Message) != want {
			t.Fatalf("D35 before must report exactly the p1 -> t3 violation: %s", r.stdout)
		}
	}
	if !bytes.Equal(before, readFile(t, f.statePath())) {
		t.Fatal("D35 validate changed state")
	}
	f.harness().Check(t, oracle.JSON, "repair-superseded-dependencies/success_validate_"+phase+".json", jsonDocument(t, f, r))
}
