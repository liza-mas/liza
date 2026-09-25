package commands

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/referencecontract"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestInspectAcceptanceEvidenceThroughTaskDispatch(t *testing.T) {
	projectRoot := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.SetupPipelineConfig(t, projectRoot)
	index := 0
	now := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	source := models.AcceptanceSource{
		Ref: "specs/plan.md#Boundary", Commit: strings.Repeat("1", 40), Blob: strings.Repeat("2", 40),
		ParentTask: "plan-1", ParentReviewCommit: strings.Repeat("3", 40),
	}
	reviewCommit := strings.Repeat("4", 40)
	receipt := models.AcceptanceReceipt{
		Version: 1, ReviewCommit: reviewCommit, Source: source,
		ManifestPath: "acceptance/boundary.json", ManifestBlob: strings.Repeat("5", 40),
		Mappings: []referencecontract.AcceptanceMapping{{ObligationID: "AC-replay", File: "tests/boundary.test", Assertion: "contested replay", CommandIndex: &index}},
		Commands: []models.AcceptanceCommandResult{{Command: "run-tests", ExitCode: 0, StartedAt: now, FinishedAt: now.Add(time.Second), Output: "case one passed\ncase two passed; credential=***REDACTED***\n"}},
	}
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		{ID: "task-42", Status: models.TaskStatusReadyForReview, Created: now, ReviewCommit: &reviewCommit, AcceptanceSource: &source, AcceptanceReceipt: &receipt},
		{ID: "legacy", Status: models.TaskStatusReady, Created: now},
	}
	testhelpers.WriteInitialState(t, statePath, state)

	output, err := InspectCommand([]string{"tasks", "task-42"}, InspectOptions{Format: "json", ProjectRoot: projectRoot})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Source  models.AcceptanceSource  `json:"acceptance_source"`
		Receipt models.AcceptanceReceipt `json:"acceptance_receipt"`
	}
	if err := json.Unmarshal([]byte(output), &got); err != nil {
		t.Fatalf("task inspection was not JSON: %v", err)
	}
	if got.Source != source || got.Receipt.Source != source || got.Receipt.ReviewCommit != reviewCommit || got.Receipt.ManifestBlob != receipt.ManifestBlob {
		t.Fatalf("task inspection lost evidence identity: %s", output)
	}
	if len(got.Receipt.Mappings) != 1 || got.Receipt.Mappings[0].Assertion != "contested replay" || len(got.Receipt.Commands) != 1 || got.Receipt.Commands[0].Output != receipt.Commands[0].Output {
		t.Fatalf("task inspection omitted mappings or full execution output: %s", output)
	}
	legacy, err := InspectCommand([]string{"tasks", "legacy"}, InspectOptions{Format: "json", ProjectRoot: projectRoot})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(legacy, "acceptance_receipt") || strings.Contains(legacy, "acceptance_source") {
		t.Fatalf("legacy inspection invented evidence: %s", legacy)
	}
}

// D63: before a claim adopts acceptance_source, the allocation inputs a claim
// refusal is judged on — plan_ref and the parent tasks — must be inspectable.
func TestInspectTaskShowsAcceptanceAllocationInputs(t *testing.T) {
	projectRoot := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.SetupPipelineConfig(t, projectRoot)
	parent := "plan-7"
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{{
		ID: "task-7", Status: models.TaskStatusReady, Created: time.Date(2026, 9, 24, 9, 44, 0, 0, time.UTC),
		SpecRef: "specs/goal.md", PlanRef: "specs/plan.md#Task 2", ParentTask: &parent,
		Validation: []string{"make check"},
	}}
	testhelpers.WriteInitialState(t, statePath, state)

	output, err := InspectCommand([]string{"tasks", "task-7"}, InspectOptions{Format: "json", ProjectRoot: projectRoot})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		PlanRef     string   `json:"plan_ref"`
		ParentTasks []string `json:"parent_tasks"`
	}
	if err := json.Unmarshal([]byte(output), &got); err != nil {
		t.Fatalf("task inspection was not JSON: %v", err)
	}
	if got.PlanRef != "specs/plan.md#Task 2" || len(got.ParentTasks) != 1 || got.ParentTasks[0] != parent {
		t.Fatalf("task inspection hides the allocation inputs (plan_ref=%q parent_tasks=%v): %s", got.PlanRef, got.ParentTasks, output)
	}
}
