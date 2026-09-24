package commands

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestReplanCommand_HappyPath(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")
	testhelpers.CreateSpecFile(t, tmpDir, "plan.md", "# Plan\n")
	os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("# README\n"), 0644)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusCheckpoint
	state.Sprint.CheckpointTrigger = models.CheckpointTriggerPlanningComplete

	planningTask := testhelpers.BuildTaskByStatus("code-planning-1", models.TaskStatusMerged, now)
	planningTask.RolePair = "code-planning-pair"
	planningTask.PlanRef = "specs/plan.md"
	planningTask.Output = []models.OutputEntry{
		{Desc: "implement feature", DoneWhen: "tests pass", Scope: "internal/", SpecRef: "README.md"},
	}
	state.Tasks = []models.Task{planningTask}
	state.Sprint.Scope.Planned = []string{"code-planning-1"}

	testhelpers.WriteInitialState(t, stateFile, state)

	err := ReplanCommand(tmpDir, "code-planning-1", "human", "")
	if err != nil {
		t.Fatalf("ReplanCommand() error: %v", err)
	}

	// Verify state
	bb := db.New(stateFile)
	readState, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("Failed to read state: %v", readErr)
	}

	if readState.Sprint.Status != models.SprintStatusInProgress {
		t.Errorf("Sprint.Status = %v, want IN_PROGRESS", readState.Sprint.Status)
	}

	newTask := readState.FindTask("code-planning-1-replan-1")
	if newTask == nil {
		t.Fatal("New task code-planning-1-replan-1 not found")
	}
	if newTask.RolePair != "code-planning-pair" {
		t.Errorf("New task RolePair = %q, want %q", newTask.RolePair, "code-planning-pair")
	}
}

func TestReplanCommand_AutoDetect(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")
	os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("# README\n"), 0644)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusCheckpoint

	planningTask := testhelpers.BuildTaskByStatus("code-planning-1", models.TaskStatusMerged, now)
	planningTask.RolePair = "code-planning-pair"
	planningTask.Output = []models.OutputEntry{
		{Desc: "implement feature", DoneWhen: "tests pass", Scope: "internal/", SpecRef: "README.md"},
	}
	state.Tasks = []models.Task{planningTask}
	state.Sprint.Scope.Planned = []string{"code-planning-1"}

	testhelpers.WriteInitialState(t, stateFile, state)

	// No task ID — auto-detect
	err := ReplanCommand(tmpDir, "", "human", "")
	if err != nil {
		t.Fatalf("ReplanCommand() error: %v", err)
	}

	bb := db.New(stateFile)
	readState, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("Failed to read state: %v", readErr)
	}

	newTask := readState.FindTask("code-planning-1-replan-1")
	if newTask == nil {
		t.Fatal("New task not created by auto-detect")
	}
}

// Replan is allowed at CHECKPOINT and IN_PROGRESS (D73: the orchestrator
// replans before auto-resume can close a checkpoint); COMPLETED stays refused.
func TestReplanCommand_ErrorWrongStatus(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusCompleted

	planningTask := testhelpers.BuildTaskByStatus("code-planning-1", models.TaskStatusMerged, now)
	planningTask.RolePair = "code-planning-pair"
	planningTask.Output = []models.OutputEntry{
		{Desc: "feature", DoneWhen: "done", Scope: ".", SpecRef: "README.md"},
	}
	state.Tasks = []models.Task{planningTask}
	state.Sprint.Scope.Planned = []string{"code-planning-1"}

	testhelpers.WriteInitialState(t, stateFile, state)

	err := ReplanCommand(tmpDir, "code-planning-1", "human", "")
	if err == nil {
		t.Fatal("Expected error for wrong sprint status")
	}
	testhelpers.AssertErrorContains(t, err, "CHECKPOINT or IN_PROGRESS")
}
