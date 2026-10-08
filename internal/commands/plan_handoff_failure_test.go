package commands

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestPhaseHandoffFailedPlanVisibleButNotReady(t *testing.T) {
	root := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	plan := testhelpers.BuildTaskByStatus("failed-plan", models.TaskStatusMerged, time.Now().UTC())
	plan.RolePair = "code-planning-pair"
	plan.PlanCheck = &models.PlanCheck{Verdict: models.PlanCheckPassed, By: "orchestrator-1", At: time.Now().UTC()}
	plan.Output = []models.OutputEntry{{Desc: "implement", DoneWhen: "tests pass", Scope: "pkg/", SpecRef: "README.md", InheritInputs: &models.InheritInputs{Mode: models.InheritModeSelected, Selections: []models.InputSelection{{UpstreamTask: "absent", Outputs: []int{0}}}}}}
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{plan}
	state.Sprint.Status = models.SprintStatusInProgress
	state.Sprint.Scope.Planned = []string{plan.ID}
	testhelpers.WriteInitialState(t, statePath, state)
	if report, err := ops.ExecuteTransitionsReportWith(root, "manual", ops.AdmitReviewed); err != nil || len(report.Failures) != 1 {
		t.Fatalf("refusal = %+v, %v", report, err)
	}
	state, err := db.For(statePath).Read()
	if err != nil {
		t.Fatal(err)
	}
	handoff := buildPhaseHandoffStatus(state, root)
	if handoff == nil || handoff.State != "REPAIR_REQUIRED" || len(handoff.ReadyPlanningTasks) != 0 || len(handoff.FailedPlanningTasks) != 1 || handoff.FailedPlanningTasks[0].TaskID != plan.ID {
		t.Fatalf("handoff status = %+v", handoff)
	}
	var text strings.Builder
	writePhaseHandoffSection(&text, handoff)
	if strings.Contains(text.String(), "Ready planning tasks:") || !strings.Contains(text.String(), "supplies no inherited children") {
		t.Fatalf("status text = %s", text.String())
	}
}

// D-71: a refused stale-provider plan wakes the orchestrator for its replan,
// but status still reports it only as a failed hand-off, never as ready.
func TestPhaseHandoffStaleProviderRefusalVisibleButNotReady(t *testing.T) {
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	plan := func(id string) models.Task {
		task := testhelpers.BuildTaskByStatus(id, models.TaskStatusMerged, time.Now().UTC())
		task.RolePair = "architecture-pair"
		task.Output = []models.OutputEntry{{Desc: "Plan the shared contract", DoneWhen: "Contract reviewed", Scope: "contract", SpecRef: "README.md"}}
		return task
	}
	consumer := plan("consumer")
	consumer.Output[0].ProviderDependencies = []models.ProviderDependency{{ProviderTask: "provider", Transition: "architecture-to-code-plan", Outputs: []int{0}}}
	state := testhelpers.CreateValidState()
	state.Goal.SpecRef = "README.md"
	state.Tasks = []models.Task{plan("provider"), consumer}
	state.Sprint.Status = models.SprintStatusInProgress
	state.Sprint.Scope.Planned = []string{"provider", "consumer"}
	state.Agents["architect-1"] = testhelpers.RegisteredTestAgent("architect")
	testhelpers.WriteInitialState(t, statePath, state)
	if _, err := ops.Replan(root, &ops.ReplanInput{TaskID: "provider", ChangedBy: "human"}); err != nil {
		t.Fatal(err)
	}
	if report, err := ops.ExecuteTransitionsReportWith(root, "manual", ops.AdmitOperator); err != nil || len(report.Failures) != 1 {
		t.Fatalf("refusal = %+v, %v", report, err)
	}
	state, err := db.For(statePath).Read()
	if err != nil {
		t.Fatal(err)
	}
	handoff := buildPhaseHandoffStatus(state, root)
	if handoff == nil || handoff.State != "REPAIR_REQUIRED" || len(handoff.ReadyPlanningTasks) != 0 || len(handoff.FailedPlanningTasks) != 1 || handoff.FailedPlanningTasks[0].TaskID != consumer.ID {
		t.Fatalf("handoff status = %+v", handoff)
	}
}
