package a4

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

// AC-budget-2: command-0 wall time is recorded in acceptance-a4-2.json after
// measuring the complete suite, including fixture validation and the CLI build.
func TestOracleRetargetDependency(t *testing.T) {
	for _, name := range []string{"success", "refused"} {
		t.Run(name, func(t *testing.T) {
			id, code, message := "t2", 0, ""
			if name == "refused" {
				id, code, message = "m1", 1, "cannot retarget dependencies on terminal task m1 (MERGED)"
			}
			runCase(t, oracleCase{name: "retarget-dependency/" + name, fixture: "graph", identity: "orch", json: true, wantExit: code, contains: message, args: []string{"retarget-dependency", id, "t1", "t3", "--reason", "Moved"}})
		})
	}
}

func TestOracleNarrowInheritedDependencies(t *testing.T) {
	for _, name := range []string{"success", "refused"} {
		t.Run(name, func(t *testing.T) {
			id, code, message := "cp-b", 0, ""
			if name == "refused" {
				id, code, message = "t1", 1, "producer t1 has no executed per-subtask transition; pass --transition explicitly"
			}
			runCase(t, oracleCase{name: "narrow-inherited-dependencies/" + name, fixture: "inherited", identity: "orch", json: true, wantExit: code, contains: message, args: []string{"narrow-inherited-dependencies", id, "--selections", "@input:narrow.yaml", "--reason", "Only first output"}})
		})
	}
}

func TestOracleApplyDependencyRepair(t *testing.T) {
	for _, name := range []string{"success", "refused"} {
		t.Run(name, func(t *testing.T) {
			id, code, message := "b1", 0, ""
			if name == "refused" {
				id, code, message = "t1", 1, "cannot apply dependency repair from task t1 in status DRAFT_CODE (must be BLOCKED)"
			}
			runCase(t, oracleCase{name: "apply-dependency-repair/" + name, fixture: "repair-request", identity: "orch", json: true, wantExit: code, contains: message, args: []string{"apply-dependency-repair", id, "--reason", "Apply"}})
		})
	}
}

func TestOracleRepairSupersededDependencies(t *testing.T) {
	t.Run("success", runD35Success)
	t.Run("refused", func(t *testing.T) {
		runCase(t, oracleCase{name: "repair-superseded-dependencies/refused", fixture: "graph", identity: "orch", json: true, wantExit: 1, contains: "cannot repair dependencies on task t1 in status DRAFT_CODE (must be SUPERSEDED)", args: []string{"repair-superseded-dependencies", "t1", "--reason", "Repair"}})
	})
}

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

func TestOracleDeferProviderDependency(t *testing.T) {
	for _, name := range []string{"success", "refused"} {
		t.Run(name, func(t *testing.T) {
			id, code, message := "cp-x", 0, ""
			if name == "refused" {
				id, code, message = "m1", 1, "task m1 is not unstarted"
			}
			runCase(t, oracleCase{name: "defer-provider-dependency/" + name, fixture: "provider-wait", identity: "op", json: true, wantExit: code, contains: message, args: []string{"defer-provider-dependency", id, "--provider-task", "cp-p", "--transition", "code-plan-to-coding", "--reason", "Hold only writers"}})
		})
	}
}

func TestOracleReserveProvider(t *testing.T) {
	for _, name := range []string{"success", "refused"} {
		t.Run(name, func(t *testing.T) {
			id, code, message := "t1", 0, ""
			if name == "refused" {
				id, code, message = "m1", 1, "writer m1 is terminal (MERGED); there is nothing left to place"
			}
			runCase(t, oracleCase{name: "reserve-provider/" + name, fixture: "provider", identity: "orch", json: true, wantExit: code, contains: message, args: []string{"reserve-provider", id, "cp-p", "--transition", "code-plan-to-coding", "--reason", "Writer order"}})
		})
	}
}
