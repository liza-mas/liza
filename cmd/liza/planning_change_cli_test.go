package main

import (
	"reflect"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestAddTaskRecordsExplicitPlanningChangeAndRefusesMissingOriginal(t *testing.T) {
	root := setupPlanCheckCLI(t)
	t.Setenv(brand.EnvName("AGENT_GENERATION"), testhelpers.TestAgentGeneration)
	statePath := paths.New(root).StatePath()
	state, err := db.For(statePath).ReadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	state.FindTask("plan-1").Type = models.TaskTypePlanning
	testhelpers.WriteInitialState(t, statePath, state)
	args := []string{"add-task", "--id", "corrective-plan", "--desc", "repair consumer contract", "--spec", root + "/README.md",
		"--done", "reviewed correction", "--scope", "contract", "--role-pair", "code-planning-pair", "--agent-id", "orchestrator-1",
		"--planning-change-kind", "correction", "--planning-change-original", "plan-1", "--trigger", "consumer-gap", "--json"}
	output, err := executeRootCommandCapture(t, root, args...)
	if err != nil {
		t.Fatalf("corrective commissioning: %v (%s)", err, output)
	}
	state, err = db.For(statePath).ReadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	task := state.FindTask("corrective-plan")
	want := models.NewPlanningChange(models.PlanningChangeCorrection, "consumer-gap", "plan-1")
	if task == nil || !reflect.DeepEqual(task.PlanningChange, want) || task.Created.IsZero() {
		t.Fatalf("commissioning lost typed creation metadata: %#v", task)
	}
	before := len(state.Tasks)
	resetRootCmdForTest(t)
	args[2] = "invalid-correction"
	for i := range args {
		if args[i] == "plan-1" {
			args[i] = "missing-original"
		}
	}
	output, err = executeRootCommandCapture(t, root, args...)
	if err == nil {
		t.Fatalf("missing original accepted: %s", output)
	}
	state, err = db.For(statePath).ReadSnapshot()
	if err != nil || len(state.Tasks) != before || state.FindTask("invalid-correction") != nil {
		t.Fatalf("refused commissioning changed state: %v", err)
	}
}
