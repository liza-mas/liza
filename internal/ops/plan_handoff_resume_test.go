package ops

import (
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D73: a planning checkpoint resumed automatically expands only the plans the
// orchestrator passed; an operator resume is itself the review, except for a
// plan held for a human action.
func TestResume_PlanningCheckpointAdmissionByOrigin(t *testing.T) {
	cases := []struct {
		name   string
		resume func(string, string) (*ResumeResult, error)
		want   map[string]bool // plan ID → expanded
	}{
		{name: "automatic", resume: AutoResume, want: map[string]bool{"passed": true, "undecided": false, "held": false}},
		{name: "operator", resume: Resume, want: map[string]bool{"passed": true, "undecided": true, "held": false}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

			state := testhelpers.CreateValidState()
			state.Config.Mode = models.SystemModeRunning
			state.Config.AutoResume = true
			state.PipelineVersion = 2
			state.Sprint.Status = models.SprintStatusCheckpoint
			state.Sprint.CheckpointTrigger = models.CheckpointTriggerPlanningComplete
			active := testhelpers.BuildTaskByStatus("plan-active", models.TaskStatusCodePlanning, time.Now().UTC())
			active.RolePair = "code-planning-pair"
			state.Tasks = []models.Task{
				withPlanCheck(handoffPlan("passed", "code-planning-pair"), models.PlanCheckPassed, ""),
				handoffPlan("undecided", "code-planning-pair"),
				withPlanCheck(handoffPlan("held", "code-planning-pair"), models.PlanCheckHeld, "inject smoke credentials"),
				active,
			}
			for _, task := range state.Tasks {
				state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, task.ID)
			}
			testhelpers.WriteInitialState(t, stateFile, state)

			if _, err := tc.resume(tmpDir, "resumer"); err != nil {
				t.Fatalf("resume: %v", err)
			}
			after, err := db.For(stateFile).Read()
			if err != nil {
				t.Fatal(err)
			}
			for id, wantExpanded := range tc.want {
				expanded := after.FindTask(id).TransitionsExecuted["code-plan-to-coding"]
				if expanded != wantExpanded {
					t.Errorf("%s expanded = %v, want %v", id, expanded, wantExpanded)
				}
				if child := after.FindTask(id + "-code-0"); (child != nil) != wantExpanded {
					t.Errorf("%s child exists = %v, want %v", id, child != nil, wantExpanded)
				}
			}
		})
	}
}
