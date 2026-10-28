package a4

import (
	"bytes"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func init() {
	fixtureBaseOmissions["amendment-merged"] = []string{"specs/plans/p.md"}
	fixtureRecipes["amendment-merged"] = []gitStep{
		{Kind: "branch", Label: "task/cp-1"},
		{Kind: "commit", Label: "plan", Path: "specs/plans/p.md", Source: "testdata/fixtures/project/specs/plans/p.md"},
		{Kind: "merge", Label: "merge-cp-1", Branch: "task/cp-1"},
		{Kind: "branch", Label: "task/cp-1-amend-1"},
		{Kind: "commit", Label: "amend", Path: "specs/plans/p.md", Source: "testdata/fixtures/inputs/amendment.md"},
		{Kind: "merge", Label: "merge-amend", Branch: "task/cp-1-amend-1"},
	}
}

func TestOracleRepairArchRef(t *testing.T) {
	for _, name := range []string{"success", "refused"} {
		t.Run(name, func(t *testing.T) {
			id, code, message := "cp-p", 0, ""
			if name == "refused" {
				id, code, message = "cp-q", 1, "repair-arch-ref only fills an empty arch_ref"
			}
			runCase(t, oracleCase{name: "repair-arch-ref/" + name, fixture: "arch-ref", identity: "op", json: true, wantExit: code, contains: message, args: []string{"repair-arch-ref", id, "--arch-ref", "specs/architecture/a.md#Scope 1: One", "--reason", "Restore"}})
		})
	}
}

func TestOraclePlanCheck(t *testing.T) {
	for _, mode := range []string{"pass", "hold", "clear", "replaced-by", "retire"} {
		for _, refused := range []bool{false, true} {
			name := mode
			if refused {
				name += "_refused"
			}
			t.Run(name, func(t *testing.T) {
				id, fixture, identity, code, message := "cp-1", "plan-pending", "op", 0, ""
				args := []string{"--" + mode}
				switch mode {
				case "pass":
					identity = "orch"
				case "hold":
					identity, args = "orch", append(args, "Confirm scope")
				case "clear":
					fixture = "plan-held"
				case "replaced-by":
					args = append(args, "cp-c")
				case "retire":
					args = append(args, "--ruling", "Scope dropped")
				}
				if refused {
					id, fixture, code = "t1", "plan-pending", 1
					switch mode {
					case "pass", "hold", "clear":
						message = "task t1 has no plan awaiting hand-off"
					case "replaced-by":
						message = "task t1 must have an unused MERGED reviewed handoff"
					case "retire":
						message = "it is not MERGED"
					}
				}
				runCase(t, oracleCase{name: "plan-check/" + name, fixture: fixture, identity: identity, json: true, wantExit: code, contains: message, args: append([]string{"plan-check", id}, args...)})
			})
		}
	}
}

func TestOracleAmendPlan(t *testing.T) {
	for _, mode := range []string{"reason", "contract", "apply", "replace-pending", "withdraw-pending"} {
		for _, refused := range []bool{false, true} {
			name := mode
			if refused {
				name += "_refused"
			}
			t.Run(name, func(t *testing.T) {
				id, fixture, identity, code, message := "cp-1", "plan-pending", "orch", 0, ""
				args := []string{}
				switch mode {
				case "reason":
					args = []string{"--reason", "Add task two"}
				case "contract":
					id, fixture = "arch-m", "architecture-merged"
					args = []string{"--reason", "Clarify contract", "--contract"}
				case "apply":
					fixture = "amendment-merged"
					args = []string{"--apply", "cp-1-amend-1"}
				case "replace-pending":
					fixture = "amendment-merged"
					args = []string{"--replace-pending", "cp-1-amend-1", "--reason", "Redo"}
				case "withdraw-pending":
					fixture, identity = "amendment-abandoned", "op"
					args = []string{"--withdraw-pending", "cp-1-amend-1", "--reason", "Abandoned before merge"}
				}
				if refused {
					id, fixture, code = "cp-1", "plan-pending", 1
					switch mode {
					case "reason":
						id, message = "t1", "amendment requires a live MERGED planning original"
					case "contract":
						message = "expanded contract correction requires an architecture original"
					default:
						message = mode + " must name the exact pending correction"
					}
				}
				c := oracleCase{name: "amend-plan/" + name, fixture: fixture, identity: identity, json: true, wantExit: code, contains: message, args: append([]string{"amend-plan", id}, args...)}
				if fixture == "amendment-merged" {
					runMergedAmendment(t, c)
				} else {
					runCase(t, c)
				}
			})
		}
	}
}

// The shared recipe records commits and merges on main. These two cases also
// advance the disposable integration ref to the reviewed amendment merge, before
// taking any snapshot. No operation under test is used to set up the fixture.
func runMergedAmendment(t *testing.T, c oracleCase) {
	t.Helper()
	gate(t)
	var jsonState []byte
	for _, mode := range []string{"json", "human"} {
		f := newFixture(t, c.fixture)
		f.git(t, "branch", "-f", "integration", f.commits["merge-amend"])
		before := readFile(t, f.statePath())
		snapshot := projectFiles(t, f.root)
		args := append([]string(nil), c.args...)
		if mode == "json" {
			args = append(args, "--json")
		}
		r := f.cli(t, c.identity, args...)
		if r.code != 0 {
			t.Fatalf("%s %s failed: %s %s", c.name, mode, r.stdout, r.stderr)
		}
		b, a := stateDocument(t, f, before, snapshot)
		if mode == "json" {
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
