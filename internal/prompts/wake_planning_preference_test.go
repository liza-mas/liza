package prompts

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D45: the snapshot dashboard path must agree with the live selector. A
// blocker that depends on untransitioned planning output renders the
// PLANNING_COMPLETE instructions, not a BLOCKED_TASKS turn that forbids
// sprint-checkpoint.
func TestRenderOrchestratorDashboard_BlockedOnPlannerRendersPlanningComplete(t *testing.T) {
	mergedAt := time.Date(2026, time.September, 24, 7, 0, 0, 0, time.UTC)
	planner := testhelpers.BuildTaskByStatus("provider-plan", models.TaskStatusMerged, mergedAt.Add(-time.Hour))
	planner.RolePair = "code-planning-pair"
	planner.Output = []models.OutputEntry{{Desc: "Implement provider", DoneWhen: "Provider contract passes", Scope: "provider"}}
	planner.History = append(planner.History, models.TaskHistoryEntry{Time: mergedAt, Event: models.TaskEventMerged})
	consumer := testhelpers.BuildTaskByStatus("consumer", models.TaskStatusBlocked, mergedAt.Add(-2*time.Hour))
	consumer.DependsOn = []string{planner.ID}
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{planner, consumer}
	state.Sprint.Scope.Planned = []string{planner.ID, consumer.ID}

	dashboard, instruction, err := RenderOrchestratorDashboard(state, setupPipelineConfig(t), "orchestrator-1")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(dashboard, "WAKE TRIGGER: PLANNING_COMPLETE\n") {
		t.Errorf("expected PLANNING_COMPLETE for a blocker waiting on planning output; dashboard: %s", dashboard)
	}
	if !strings.Contains(instruction, "Create checkpoint for human review") {
		t.Errorf("instructions must permit the checkpoint that materializes the children; got: %s", instruction)
	}
}
