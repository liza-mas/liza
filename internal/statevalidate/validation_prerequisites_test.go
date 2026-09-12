package statevalidate

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestStateValidationPrerequisitesRejectStaleCommands(t *testing.T) {
	task := testhelpers.BuildTaskByStatus("protected", models.TaskStatusReady, time.Now().UTC())
	task.Validation = []string{"check first", "check other"}
	task.ValidationPrerequisites = []models.ValidationPrerequisite{
		{Command: "check first", Env: []string{"URL"}},
		{Command: "check other", Executables: []string{"tool"}},
	}
	if err := validateTaskInvariants(stateWithTasks(task), "", true, nil, nil); err != nil {
		t.Fatalf("valid contract rejected: %v", err)
	}
	task.Validation = []string{"check other", "check first"}
	if err := validateTaskInvariants(stateWithTasks(task), "", true, nil, nil); err != nil {
		t.Fatalf("command reorder broke exact associations: %v", err)
	}
	task.Validation[0] = "check third"
	if err := validateTaskInvariants(stateWithTasks(task), "", true, nil, nil); err == nil || !strings.Contains(err.Error(), "validation_prerequisites") {
		t.Fatalf("stale persisted prerequisite accepted: %v", err)
	}
	task.ValidationPrerequisites = nil
	if err := validateTaskInvariants(stateWithTasks(task), "", true, nil, nil); err != nil {
		t.Fatalf("legacy undeclared task rejected: %v", err)
	}
}

func TestStateOutputValidationPrerequisitesCheckedWithoutArtifactChecks(t *testing.T) {
	task := models.Task{ID: "parent", Output: []models.OutputEntry{{
		Desc: "child", DoneWhen: "checks pass", Scope: "feature", SpecRef: "specs/feature.md",
		Validation:              []string{"check"},
		ValidationPrerequisites: []models.ValidationPrerequisite{{Command: "check", Env: []string{"URL"}}},
	}}}
	if err := validateTaskOutput(&task, false); err != nil {
		t.Fatalf("valid output contract rejected: %v", err)
	}
	task.Output[0].ValidationPrerequisites[0].Env = nil
	if err := validateTaskOutput(&task, false); err == nil || !strings.Contains(err.Error(), "output[0]: validation_prerequisites[0] requires at least one check") {
		t.Fatalf("vacuous output contract accepted: %v", err)
	}
}
