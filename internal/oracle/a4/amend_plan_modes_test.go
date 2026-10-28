package a4

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

const (
	amendTask1Input    = "testdata/fixtures/inputs/amendment-task1.md"
	amendArchV1Input   = "testdata/fixtures/inputs/arch-contract-v1.md"
	amendArchV2Input   = "testdata/fixtures/inputs/arch-contract-v2.md"
	amendArchOutsideIn = "testdata/fixtures/inputs/arch-contract-outside.md"
	amendPlanSource    = "testdata/fixtures/project/specs/plans/p.md"
)

func init() {
	fixtureBaseOmissions["amendment-applied"] = []string{"specs/plans/p.md"}
	fixtureRecipes["amendment-applied"] = []gitStep{
		{Kind: "branch", Label: "task/cp-1"},
		{Kind: "commit", Label: "plan", Path: "specs/plans/p.md", Source: amendPlanSource},
		{Kind: "merge", Label: "merge-cp-1", Branch: "task/cp-1"},
		{Kind: "branch", Label: "task/cp-1-amend-1"},
		{Kind: "commit", Label: "amend", Path: "specs/plans/p.md", Source: "testdata/fixtures/inputs/amendment.md"},
		{Kind: "merge", Label: "merge-amend", Branch: "task/cp-1-amend-1"},
	}
	fixtureBaseOmissions["amendment-preserve"] = []string{"specs/plans/p.md"}
	fixtureRecipes["amendment-preserve"] = []gitStep{
		{Kind: "branch", Label: "task/cp-1"},
		{Kind: "commit", Label: "plan", Path: "specs/plans/p.md", Source: amendPlanSource},
		{Kind: "merge", Label: "merge-cp-1", Branch: "task/cp-1"},
		{Kind: "branch", Label: "task/cp-1-replan-1"},
		{Kind: "commit", Label: "replan", Path: "specs/plans/p.md", Source: amendTask1Input},
		{Kind: "merge", Label: "merge-replan", Branch: "task/cp-1-replan-1"},
	}
	fixtureBaseOmissions["amendment-contract"] = []string{"specs/architecture/a.md"}
	fixtureRecipes["amendment-contract"] = amendContractRecipe(amendArchV2Input)
}

// amendContractRecipe reviews an architecture original, then a contract-mode
// correction whose candidate file is the given input.
func amendContractRecipe(candidate string) []gitStep {
	return []gitStep{
		{Kind: "branch", Label: "task/arch-a"},
		{Kind: "commit", Label: "arch", Path: "specs/architecture/a.md", Source: amendArchV1Input},
		{Kind: "merge", Label: "merge-arch", Branch: "task/arch-a"},
		{Kind: "branch", Label: "task/arch-a-amend-1"},
		{Kind: "commit", Label: "amend", Path: "specs/architecture/a.md", Source: candidate},
		{Kind: "merge", Label: "merge-amend", Branch: "task/arch-a-amend-1"},
	}
}

// TestOracleAmendPlanModes pins the first refusal and the adoption paths of the
// reviewed plan correction that TestOracleAmendPlan leaves open.
func TestOracleAmendPlanModes(t *testing.T) {
	apply := func(name, fixture, id, correction, mergeLabel string, recipe []gitStep, code int, message string) {
		t.Run(name, func(t *testing.T) {
			runAdoptedAmendment(t, oracleCase{name: "amend-plan/" + name, fixture: fixture, identity: "orch", wantExit: code, contains: message,
				args: []string{"amend-plan", id, "--apply", correction}}, recipe, mergeLabel)
		})
	}
	t.Run("reason_held_refused", func(t *testing.T) {
		runCase(t, oracleCase{name: "amend-plan/reason_held_refused", fixture: "plan-held", identity: "orch", json: true, wantExit: 1,
			contains: "human hold requires operator plan-check --clear before beginning an amendment",
			args:     []string{"amend-plan", "cp-1", "--reason", "Add task two"}})
	})
	t.Run("reason_pending_refused", func(t *testing.T) {
		runCase(t, oracleCase{name: "amend-plan/reason_pending_refused", fixture: "amendment-merged", identity: "orch", json: true, wantExit: 1,
			contains: "a correction is already pending; apply it or replace-pending after merge/cancellation",
			args:     []string{"amend-plan", "cp-1", "--reason", "Add task two"}})
	})
	apply("apply_contract", "amendment-contract", "arch-a", "arch-a-amend-1", "merge-amend", amendContractRecipe(amendArchV2Input), 0, "")
	apply("apply_contract_outside_refused", "amendment-contract", "arch-a", "arch-a-amend-1", "merge-amend", amendContractRecipe(amendArchOutsideIn), 1,
		"bounded contract amendment: candidate changes text outside referenced contract sections: specs/architecture/a.md")
	apply("apply_preserve_identity", "amendment-preserve", "cp-1", "cp-1-replan-1", "merge-replan", fixtureRecipes["amendment-preserve"], 0, "")
	apply("apply_replay", "amendment-applied", "cp-1", "cp-1-amend-1", "merge-amend", fixtureRecipes["amendment-applied"], 0, "")
}

// runAdoptedAmendment is runMergedAmendment for refusals and for recipes other
// than the registered one: the disposable integration ref is advanced to the
// correction's merge before the snapshot, never by the operation under test.
func runAdoptedAmendment(t *testing.T, c oracleCase, recipe []gitStep, mergeLabel string) {
	t.Helper()
	gate(t)
	var jsonState []byte
	for _, mode := range []string{"json", "human"} {
		f := newFixtureWithRecipe(t, c.fixture, recipe)
		f.git(t, "branch", "-f", "integration", f.commits[mergeLabel])
		before := readFile(t, f.statePath())
		snapshot := projectFiles(t, f.root)
		args := append([]string(nil), c.args...)
		if mode == "json" {
			args = append(args, "--json")
		}
		r := f.cli(t, c.identity, args...)
		if r.code != c.wantExit {
			t.Fatalf("%s %s exit %d, want %d: %s %s", c.name, mode, r.code, c.wantExit, r.stdout, r.stderr)
		}
		if !strings.Contains(string(r.stdout)+string(r.stderr), c.contains) {
			t.Fatalf("%s %s did not reach named outcome %q: %s %s", c.name, mode, c.contains, r.stdout, r.stderr)
		}
		b, a := stateDocument(t, f, before, snapshot)
		if mode == "json" {
			var envelope struct {
				OK bool `json:"ok"`
			}
			if err := json.Unmarshal(r.stdout, &envelope); err != nil || envelope.OK != (c.wantExit == 0) {
				t.Fatal("JSON envelope outcome does not match the named success/refusal")
			}
			f.harness().Check(t, oracle.JSON, c.name+".json", jsonDocument(t, f, r))
			f.harness().CheckState(t, c.name+".txt", b, a)
			jsonState = a
		} else {
			f.harness().Check(t, oracle.Human, c.name+".txt", humanDocument(t, f, r))
			if !bytes.Equal(jsonState, a) {
				t.Fatal("amendment human/json final states differ")
			}
		}
	}
}
