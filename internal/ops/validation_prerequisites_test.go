package ops

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestAddTaskValidationPrerequisitesPersistAndReject(t *testing.T) {
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	stateFile, _ := testhelpers.SetupLizaDir(t, root)
	testhelpers.CreateSpecFile(t, root, "vision.md", "# Vision\n")
	testhelpers.CreateSpecFile(t, root, "feature.md", "# Feature\n")
	state := testhelpers.CreateValidState()
	state.Sprint.Scope.Planned = nil
	testhelpers.WriteInitialState(t, stateFile, state)
	var input AddTaskInput
	if err := json.Unmarshal([]byte(`{"id":"protected","desc":"feature","spec":"specs/feature.md","done":"tests pass","scope":"feature","priority":1,"role_pair":"coding-pair","validation":["check"],"validation_prerequisites":[{"command":"check","env":["URL"]}]}`), &input); err != nil {
		t.Fatal(err)
	}
	logFile := filepath.Join(root, paths.ProjectDirName(), "log.jsonl")
	if _, err := AddTask(stateFile, logFile, &input, "orchestrator-1"); err != nil {
		t.Fatal(err)
	}
	after, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatal(err)
	}
	if task := after.FindTask("protected"); task == nil || !reflect.DeepEqual(task.ValidationPrerequisites, input.ValidationPrerequisites) {
		t.Fatal("task lost validation prerequisites")
	}
	input.ID = "invalid"
	input.Validation[0] = "changed"
	if _, err := AddTask(stateFile, logFile, &input, "orchestrator-1"); err == nil || !strings.Contains(err.Error(), "validation_prerequisites") {
		t.Fatalf("stale contract accepted: %v", err)
	}
	after, err = db.New(stateFile).Read()
	if err != nil {
		t.Fatal(err)
	}
	if after.FindTask("invalid") != nil {
		t.Fatal("invalid task persisted")
	}
}

func TestOutputValidationPrerequisitesPersistAndCreateChild(t *testing.T) {
	root := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, root)
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now)}
	testhelpers.WriteInitialState(t, stateFile, state)
	entry := models.OutputEntry{Desc: "feature", DoneWhen: "tests pass", Scope: "feature", SpecRef: "specs/feature.md", Validation: []string{"check"}, ValidationPrerequisites: []models.ValidationPrerequisite{{Command: "check", Probes: [][]string{{"tool", "--check"}}}}}
	input := &SetTaskOutputInput{TaskID: "task-1", AgentID: "coder-1", Output: []models.OutputEntry{entry}}
	if err := SetTaskOutput(root, input); err != nil {
		t.Fatal(err)
	}
	after, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatal(err)
	}
	persisted := after.FindTask("task-1").Output[0]
	if !reflect.DeepEqual(persisted.ValidationPrerequisites, entry.ValidationPrerequisites) {
		t.Fatal("output lost prerequisites")
	}
	child := buildChildTask("child", "task-1", persisted, models.TaskStatusReady, "coding-pair", models.TaskTypeCoding, nil, nil, "", "", false, now)
	if !reflect.DeepEqual(child.ValidationPrerequisites, entry.ValidationPrerequisites) {
		t.Fatal("child lost prerequisites")
	}
	persisted.ValidationPrerequisites[0].Probes[0][0] = "changed"
	if child.ValidationPrerequisites[0].Probes[0][0] != "tool" {
		t.Fatal("child shares mutable probe argv with output")
	}
	input.Output[0].Validation = []string{"changed"}
	if err := SetTaskOutput(root, input); err == nil || !strings.Contains(err.Error(), "validation_prerequisites") {
		t.Fatalf("invalid output accepted: %v", err)
	}
	if err := validateOutputEntry(input.Output[0], 0, 1); err == nil || !strings.Contains(err.Error(), "validation_prerequisites") {
		t.Fatalf("invalid output accepted at transition: %v", err)
	}
	after, err = db.New(stateFile).Read()
	if err != nil {
		t.Fatal(err)
	}
	if after.FindTask("task-1").Output[0].Validation[0] != "check" {
		t.Fatal("invalid output replaced previous output")
	}
}
