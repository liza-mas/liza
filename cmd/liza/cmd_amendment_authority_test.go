package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestAmendmentAdoptionChecksScalarOnlyPlanningProse(t *testing.T) {
	for _, drift := range []bool{false, true} {
		name := "unreferenced edit remains admissible"
		if drift {
			name = "scalar-only corrected prose drift refuses adoption"
		}
		t.Run(name, func(t *testing.T) {
			root, statePath := setupAmendmentCLI(t)
			if err := db.For(statePath).Modify(func(state *models.State) error {
				// Only this scalar reference covers the corrected handoff prose;
				// output Task 1 retains its exact acceptance allocation.
				state.FindTask("original").PlanRef = "specs/acceptance-plan.md"
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			runAmendmentCLI(t, root, "replan", "original", "--preserve-output-identity", "--reason", "Correct scalar handoff prose")
			pending := readState(t, statePath)
			correction := pending.FindTask("original").PlanAmendment.Pending
			mergeAmendmentCLI(t, root, statePath, correction, pending.FindTask("original").Output)
			testhelpers.MustGit(t, root, "checkout", "integration")
			if drift {
				path := filepath.Join(root, "specs/acceptance-plan.md")
				plan, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				changed := strings.Replace(string(plan), "Use the reviewed scheduling metadata.", "Use unreviewed scheduling metadata.", 1)
				if changed == string(plan) {
					t.Fatal("fixture lacks scalar-only corrected prose")
				}
				writeAmendmentFile(t, root, "specs/acceptance-plan.md", changed)
				testhelpers.MustGit(t, root, "add", "specs/acceptance-plan.md")
			} else {
				writeAmendmentFile(t, root, "README.md", "# Independent documentation change\n")
				testhelpers.MustGit(t, root, "add", "README.md")
			}
			testhelpers.MustGit(t, root, "commit", "-m", "test: integration advances after correction review")
			before := readStateBytes(t, statePath)
			stdout, err := executeRootCommandCapture(t, root, "amend-plan", "original", "--apply", correction, "--agent-id", "orchestrator-1", "--json")
			if drift {
				if err == nil {
					t.Fatalf("unreviewed scalar prose adopted: %s", stdout)
				}
				envelope := parseEnvelope(t, stdout)
				if envelope["ok"] != false || !strings.Contains(envelope["error"].(map[string]any)["message"].(string), "plan_ref artifact") || !strings.Contains(stdout, "drifted after correction review") {
					t.Fatalf("adoption failed without the scalar drift diagnostic: %s", stdout)
				}
				if readStateBytes(t, statePath) != before || readState(t, statePath).FindTask("original").PlanAmendment.Pending != correction {
					t.Fatal("refused scalar drift cleared pending or changed state")
				}
			} else if err != nil || readState(t, statePath).FindTask("original").PlanAmendment.Pending != "" {
				t.Fatalf("unreferenced integration edit blocked adoption: %v\n%s", err, stdout)
			}
		})
	}
}
