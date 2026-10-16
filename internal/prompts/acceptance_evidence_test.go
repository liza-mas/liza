package prompts

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/referencecontract"
)

func TestAcceptanceEvidenceReviewerRendering(t *testing.T) {
	withPromptBrandValues(t, func() { brand.BinaryName = "acme" })
	index := 0
	source := &models.AcceptanceSource{Ref: "specs/plan.md#Boundary", Commit: "source-commit", Blob: "source-blob", ParentTask: "plan-1", ParentReviewCommit: "parent-review"}
	now := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	data := RoleContextData{
		Role: "code-reviewer", TaskID: "task-42", AcceptanceSource: source,
		AcceptanceReceipt: &models.AcceptanceReceipt{
			Version: 1, ReviewCommit: "candidate-commit", Source: *source,
			ManifestPath: "acceptance/boundary.json", ManifestBlob: "manifest-blob",
			Mappings: []referencecontract.AcceptanceMapping{
				{ObligationID: "AC-replay", File: "tests/boundary.test", Assertion: "contested replay", CommandIndex: &index},
				{ObligationID: "AC-audit", ApprovedReferenceID: "audit-record"},
			},
			Commands: []models.AcceptanceCommandResult{{Command: "run-tests", ExitCode: 0, StartedAt: now, FinishedAt: now.Add(time.Second), Output: "full output belongs in JSON"}},
		},
	}
	output, err := BuildRoleContext(data.Role, []string{"review-task"}, &data)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"specs/plan.md#Boundary", "source-commit", "source-blob", "plan-1 at parent-review",
		"candidate-commit", "acceptance/boundary.json (blob manifest-blob)",
		"AC-replay: tests/boundary.test :: contested replay; command[0]",
		"AC-audit: approved non-executable reference audit-record",
		"command[0]: run-tests; exit 0", "2026-09-12 10:00:00", "2026-09-12 10:00:01",
		"acme get tasks task-42 --format json",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("review prompt missing %q: %s", want, output)
		}
	}
	for _, absent := range []string{"liza", "Legacy task", "full output belongs in JSON"} {
		if strings.Contains(output, absent) {
			t.Errorf("review summary unexpectedly contains %q", absent)
		}
	}
}

// Executed results keep their canonical index across deferral gaps, and the
// deferred commands are listed so the reviewer knows they did not run.
func TestAcceptanceEvidenceReviewerRendersDeferredCommands(t *testing.T) {
	now := time.Date(2026, 10, 10, 10, 0, 0, 0, time.UTC)
	data := RoleContextData{
		Role: "code-reviewer", TaskID: "task-42",
		AcceptanceReceipt: &models.AcceptanceReceipt{
			ManifestPath: "acceptance/boundary.json",
			Commands: []models.AcceptanceCommandResult{
				{Command: "unit-tests", StartedAt: now, FinishedAt: now},
				{Command: "lint", StartedAt: now, FinishedAt: now},
			},
			Deferred: []models.AcceptanceDeferredCommand{
				{Index: 1, Command: "whole-suite", Authority: models.AcceptanceDeferralContract},
				{Index: 3, Command: "e2e-suite", Authority: models.AcceptanceDeferralOperator},
			},
		},
	}
	output, err := BuildRoleContext(data.Role, []string{"review-task"}, &data)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"command[0]: unit-tests; exit 0", "command[2]: lint; exit 0",
		"DEFERRED (not run at admission",
		"command[1]: whole-suite; deferred by contract", "command[3]: e2e-suite; deferred by operator",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("review prompt missing %q: %s", want, output)
		}
	}
}

func TestAcceptanceEvidenceLegacyAndMissingReceiptRendering(t *testing.T) {
	for _, role := range []string{"coder", "code-reviewer"} {
		t.Run(role, func(t *testing.T) {
			block := "assigned-task"
			if role == "code-reviewer" {
				block = "review-task"
			}
			data := RoleContextData{Role: role, TaskID: "task-42"}
			output, err := BuildRoleContext(role, []string{block}, &data)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output, "Legacy task: acceptance evidence was not machine-validated") || strings.Contains(output, "VALIDATED MAPPINGS") {
				t.Fatalf("legacy evidence classification missing or misleading: %s", output)
			}
			data.AcceptanceSource = &models.AcceptanceSource{Ref: "specs/plan.md#Boundary"}
			output, err = BuildRoleContext(role, []string{block}, &data)
			if err != nil {
				t.Fatal(err)
			}
			want := "manifest path and complete"
			if role == "code-reviewer" {
				want = brand.BinaryName + " update-review-commit task-42 before review"
			}
			if !strings.Contains(output, want) || strings.Contains(output, "Legacy task") {
				t.Fatalf("strict evidence instruction missing or mislabeled: %s", output)
			}
		})
	}
}

func TestAcceptanceEvidencePlannerAndReviewerInstructions(t *testing.T) {
	for _, tc := range []struct{ block, role, want string }{
		{"code-planner-tools", "code-planner", "emit an Acceptance Contract"},
		{"review-instructions", "code-plan-reviewer", "reject a missing\nAcceptance Contract"},
		{"review-instructions", "code-plan-reviewer", "missing from the Reality\nProbes entries of its validation commands"},
		{"review-instructions", "code-reviewer", "A complete mapping plus exit 0 can still"},
		{"submission-phase", "coder", "Missing mappings,"},
	} {
		t.Run(tc.role+tc.block, func(t *testing.T) {
			data := RoleContextData{Role: tc.role, AcceptanceSource: &models.AcceptanceSource{}}
			output, err := BuildRoleContext(tc.role, []string{tc.block}, &data)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output, tc.want) {
				t.Fatalf("missing instruction %q in %s", tc.want, output)
			}
		})
	}
}

// Strict admission runs the canonical list, so a strict coder is told not to
// pre-run it in full; the retry advice must not contradict the gate-only rule
// for single_use runtime inputs. Legacy coders execute nothing at submit.
func TestAcceptanceEvidenceSubmissionValidationAdvice(t *testing.T) {
	const advice = "Before submitting, run focused tests, not the full canonical list."
	const singleUseException = "unless it uses a single_use runtime input: only the gate runs"
	const gateOnly = "Never run a command using a single_use input yourself"
	// D-188: doers read silence past timeout_seconds as a stall and interrupted queued admissions.
	const noInterrupt = "Never interrupt it or start another submit for this task."
	singleUse := []models.RuntimeInput{{ID: "fixture", Commands: []string{"run-tests"}, Consumption: models.RuntimeInputSingleUse}}
	for _, tc := range []struct {
		name         string
		source       *models.AcceptanceSource
		inputs       []models.RuntimeInput
		want, absent []string
	}{
		{name: "strict", source: &models.AcceptanceSource{}, want: []string{advice, singleUseException, noInterrupt}, absent: []string{gateOnly}},
		{name: "legacy", absent: []string{advice, singleUseException, noInterrupt}},
		{name: "strict single_use", source: &models.AcceptanceSource{}, inputs: singleUse, want: []string{advice, singleUseException, gateOnly, noInterrupt}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := RoleContextData{Role: "coder", TaskID: "task-42", AcceptanceSource: tc.source, RuntimeInputs: tc.inputs, ValidationCommands: []string{"run-tests"}}
			output, err := BuildRoleContext("coder", []string{"assigned-task", "submission-phase"}, &data)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tc.want {
				if !strings.Contains(output, want) {
					t.Errorf("missing %q in %s", want, output)
				}
			}
			for _, absent := range tc.absent {
				if strings.Contains(output, absent) {
					t.Errorf("unexpected %q in %s", absent, output)
				}
			}
		})
	}
}
