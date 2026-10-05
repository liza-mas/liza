package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestPlanHandoffFailureDoesNotRewakeOrComplete(t *testing.T) {
	for _, activeWork := range []bool{false, true} {
		t.Run(map[bool]string{false: "all merged", true: "other work active"}[activeWork], func(t *testing.T) {
			root := t.TempDir()
			statePath, _ := testhelpers.SetupLizaDir(t, root)
			plan := withDisposition(handoffPlanTask("failed-plan"), models.PlanCheckPassed, "")
			plan.Output[0].InheritInputs = &models.InheritInputs{
				Mode:       models.InheritModeSelected,
				Selections: []models.InputSelection{{UpstreamTask: "unavailable-plan", Outputs: []int{0}}},
			}
			state := handoffState(plan)
			if activeWork {
				active := testhelpers.BuildTaskByStatus("active", models.TaskStatusCodePlanning, time.Now().UTC())
				active.RolePair = "code-planning-pair"
				state.Tasks = append(state.Tasks, active)
				state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, active.ID)
			}
			testhelpers.WriteInitialState(t, statePath, state)
			report, err := ops.ExecuteTransitionsReportWith(root, "manual", ops.AdmitReviewed)
			if err != nil || len(report.Failures) != 1 || !strings.Contains(report.Failures[0].Error, "supplies no inherited children") {
				t.Fatalf("initial refusal = %+v, %v", report, err)
			}
			persisted, err := db.For(statePath).Read()
			if err != nil {
				t.Fatal(err)
			}
			ctx, err := ops.LoadDetectionContext(root)
			if err != nil {
				t.Fatal(err)
			}
			wake := DetectOrchestratorWakeTriggersForProject(root, persisted, ctx.SprintTerminals, ctx.PlanningPairs, ctx.ManyToOneTransitions)
			if wake.ShouldWake() {
				t.Fatalf("persisted unchanged failure wakes %s; want no repeat planning or premature completion", wake.Trigger)
			}
			blocked := testhelpers.BuildTaskByStatus("blocked", models.TaskStatusBlocked, time.Now().UTC())
			blocked.DependsOn = []string{plan.ID}
			persisted.Tasks = append(persisted.Tasks, blocked)
			persisted.Sprint.Scope.Planned = append(persisted.Sprint.Scope.Planned, blocked.ID)
			wake = DetectOrchestratorWakeTriggersForProject(root, persisted, ctx.SprintTerminals, ctx.PlanningPairs, ctx.ManyToOneTransitions)
			if wake.Trigger != WakeTriggerBlocked {
				t.Fatalf("blocked triage redirected to failed handoff: %s", wake.Trigger)
			}
			persisted.FindTask(blocked.ID).Status = models.TaskStatusAbandoned
			persisted.Tasks = append(persisted.Tasks, withDisposition(handoffPlanTask("fresh"), models.PlanCheckPassed, ""))
			persisted.Sprint.Scope.Planned = append(persisted.Sprint.Scope.Planned, "fresh")
			wake = DetectOrchestratorWakeTriggersForProject(root, persisted, ctx.SprintTerminals, ctx.PlanningPairs, ctx.ManyToOneTransitions)
			if wake.Trigger != WakeTriggerPlanningComplete || wake.Count != 1 {
				t.Fatalf("fresh never-attempted plan lost wake: %+v", wake)
			}
		})
	}
}
