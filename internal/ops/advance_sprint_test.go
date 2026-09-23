package ops

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/statevalidate"
	"github.com/liza-mas/liza/internal/testhelpers"
	"gopkg.in/yaml.v3"
)

func setupAdvanceTest(t *testing.T) (tmpDir, stateFile string) {
	t.Helper()
	tmpDir = t.TempDir()
	stateFile, _ = testhelpers.SetupLizaDir(t, tmpDir)
	archiveDir := filepath.Join(tmpDir, paths.ProjectDirName(), "archive")
	if err := os.MkdirAll(archiveDir, 0755); err != nil {
		t.Fatalf("Failed to create archive dir: %v", err)
	}
	return tmpDir, stateFile
}

func TestAdvanceSprint(t *testing.T) {
	t.Parallel()

	tmpDir, stateFile := setupAdvanceTest(t)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusCheckpoint
	state.Sprint.Number = 1
	state.Sprint.Metrics.TasksDone = 2

	// Merged task (terminal) in planned scope
	mergedTask := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now)
	// Non-terminal task not in planned scope
	readyTask := testhelpers.BuildTaskByStatus("task-2", models.TaskStatusReady, now)
	state.Tasks = []models.Task{mergedTask, readyTask}
	state.Sprint.Scope.Planned = []string{"task-1"}

	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := AdvanceSprint(tmpDir)
	if err != nil {
		t.Fatalf("AdvanceSprint() error: %v", err)
	}

	if result.ArchivedSprintID != "sprint-1" {
		t.Errorf("ArchivedSprintID = %q, want %q", result.ArchivedSprintID, "sprint-1")
	}
	if result.NewSprintID != "sprint-2" {
		t.Errorf("NewSprintID = %q, want %q", result.NewSprintID, "sprint-2")
	}
	if result.NewSprintNumber != 2 {
		t.Errorf("NewSprintNumber = %d, want 2", result.NewSprintNumber)
	}
	if len(result.CarriedTasks) != 1 || result.CarriedTasks[0] != "task-2" {
		t.Errorf("CarriedTasks = %v, want [task-2]", result.CarriedTasks)
	}

	// Verify persisted state
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	if readState.Sprint.ID != "sprint-2" {
		t.Errorf("Sprint.ID = %q, want %q", readState.Sprint.ID, "sprint-2")
	}
	if readState.Sprint.Number != 2 {
		t.Errorf("Sprint.Number = %d, want 2", readState.Sprint.Number)
	}
	if readState.Sprint.Status != models.SprintStatusInProgress {
		t.Errorf("Sprint.Status = %v, want IN_PROGRESS", readState.Sprint.Status)
	}
	if len(readState.Sprint.Scope.Planned) != 1 || readState.Sprint.Scope.Planned[0] != "task-2" {
		t.Errorf("Sprint.Scope.Planned = %v, want [task-2]", readState.Sprint.Scope.Planned)
	}

	// Verify sprint history
	if len(readState.SprintHistory) != 1 {
		t.Fatalf("SprintHistory length = %d, want 1", len(readState.SprintHistory))
	}
	summary := readState.SprintHistory[0]
	if summary.ID != "sprint-1" {
		t.Errorf("SprintHistory[0].ID = %q, want %q", summary.ID, "sprint-1")
	}
	if summary.Number != 1 {
		t.Errorf("SprintHistory[0].Number = %d, want 1", summary.Number)
	}
	if summary.Status != models.SprintStatusCompleted {
		t.Errorf("SprintHistory[0].Status = %v, want COMPLETED", summary.Status)
	}
	if summary.TasksDone != 2 {
		t.Errorf("SprintHistory[0].TasksDone = %d, want 2", summary.TasksDone)
	}
}

func TestAdvanceSprint_ArchiveFileCreated(t *testing.T) {
	t.Parallel()

	tmpDir, stateFile := setupAdvanceTest(t)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusCheckpoint
	state.Sprint.Number = 1
	state.Sprint.Metrics.TasksDone = 1

	mergedTask := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now)
	state.Tasks = []models.Task{mergedTask}
	state.Sprint.Scope.Planned = []string{"task-1"}

	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := AdvanceSprint(tmpDir)
	if err != nil {
		t.Fatalf("AdvanceSprint() error: %v", err)
	}

	// Verify archive file exists
	archivePath := result.ArchivePath
	if _, err := os.Stat(archivePath); os.IsNotExist(err) {
		t.Fatalf("Archive file not created at %s", archivePath)
	}

	// Verify archive content
	data, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("Failed to read archive file: %v", err)
	}
	var archived models.Sprint
	if err := yaml.Unmarshal(data, &archived); err != nil {
		t.Fatalf("Failed to parse archive YAML: %v", err)
	}
	if archived.ID != "sprint-1" {
		t.Errorf("Archived sprint ID = %q, want %q", archived.ID, "sprint-1")
	}
	if archived.Number != 1 {
		t.Errorf("Archived sprint Number = %d, want 1", archived.Number)
	}
	// Archive should store COMPLETED status, not CHECKPOINT
	if archived.Status != models.SprintStatusCompleted {
		t.Errorf("Archived sprint Status = %v, want COMPLETED", archived.Status)
	}
	if archived.Timeline.Ended == nil {
		t.Error("Archived sprint Timeline.Ended should be set")
	}
}

func TestAdvanceSprint_NotCheckpoint(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusInProgress
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := AdvanceSprint(tmpDir)
	if err == nil {
		t.Fatal("Expected error when sprint is not at CHECKPOINT")
	}
}

func TestAdvanceSprint_NotAllTerminal(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusCheckpoint
	state.Sprint.Number = 1

	readyTask := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now)
	state.Tasks = []models.Task{readyTask}
	state.Sprint.Scope.Planned = []string{"task-1"}

	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := AdvanceSprint(tmpDir)
	if err == nil {
		t.Fatal("Expected error when not all planned tasks terminal")
	}
}

func TestAdvanceSprint_LegacyZeroNumber(t *testing.T) {
	t.Parallel()

	tmpDir, stateFile := setupAdvanceTest(t)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusCheckpoint
	state.Sprint.Number = 0 // legacy state

	mergedTask := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now)
	state.Tasks = []models.Task{mergedTask}
	state.Sprint.Scope.Planned = []string{"task-1"}

	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := AdvanceSprint(tmpDir)
	if err != nil {
		t.Fatalf("AdvanceSprint() error: %v", err)
	}

	// Legacy Number=0 should produce sprint-2 (normalized to 1, then +1)
	if result.NewSprintNumber != 2 {
		t.Errorf("NewSprintNumber = %d, want 2 (legacy zero guard)", result.NewSprintNumber)
	}

	// Verify history entry has normalized Number=1, not 0
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if len(readState.SprintHistory) != 1 {
		t.Fatalf("SprintHistory length = %d, want 1", len(readState.SprintHistory))
	}
	if readState.SprintHistory[0].Number != 1 {
		t.Errorf("SprintHistory[0].Number = %d, want 1 (normalized from legacy 0)", readState.SprintHistory[0].Number)
	}
}

// TestAdvanceSprint_LegacyPassesValidation is a regression test: advancing a
// legacy sprint (Number=0) must produce state that passes full validation.
func TestAdvanceSprint_LegacyPassesValidation(t *testing.T) {
	t.Parallel()

	tmpDir, stateFile := setupAdvanceTest(t)

	// Create spec files referenced by state and tasks
	specsDir := filepath.Join(tmpDir, "specs")
	if err := os.MkdirAll(specsDir, 0755); err != nil {
		t.Fatalf("Failed to create specs dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(specsDir, "vision.md"), []byte("# Vision\n"), 0644); err != nil {
		t.Fatalf("Failed to create spec file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("# README\n"), 0644); err != nil {
		t.Fatalf("Failed to create README.md: %v", err)
	}

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusCheckpoint
	state.Sprint.Number = 0 // legacy

	mergedTask := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now)
	state.Tasks = []models.Task{mergedTask}
	state.Sprint.Scope.Planned = []string{"task-1"}

	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := AdvanceSprint(tmpDir)
	if err != nil {
		t.Fatalf("AdvanceSprint() error: %v", err)
	}

	// Full validation should pass on the resulting state
	if err := statevalidate.ValidateStateFile(stateFile, false, nil); err != nil {
		t.Fatalf("Validation failed after legacy advance: %v", err)
	}
}

func TestResumeWithSprintAdvance(t *testing.T) {
	t.Parallel()

	tmpDir, stateFile := setupAdvanceTest(t)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModeRunning
	state.Sprint.Status = models.SprintStatusCheckpoint
	state.Sprint.Number = 1

	// All planned tasks are terminal
	mergedTask := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now)
	state.Tasks = []models.Task{mergedTask}
	state.Sprint.Scope.Planned = []string{"task-1"}

	testhelpers.WriteInitialState(t, stateFile, state)

	// Step 1: Resume from CHECKPOINT with all terminal → marks COMPLETED
	result1, err := Resume(tmpDir, "human")
	if err != nil {
		t.Fatalf("Resume() step 1 error: %v", err)
	}
	if result1.SprintAdvanced != nil {
		t.Error("Step 1: Expected SprintAdvanced to be nil (should mark COMPLETED, not advance)")
	}

	// Verify intermediate COMPLETED state
	bb := db.New(stateFile)
	midState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read mid-state: %v", err)
	}
	if midState.Sprint.Status != models.SprintStatusCompleted {
		t.Errorf("After step 1: Sprint.Status = %v, want COMPLETED", midState.Sprint.Status)
	}
	if midState.Sprint.ID != "sprint-1" {
		t.Errorf("After step 1: Sprint.ID = %q, want %q (should not advance yet)", midState.Sprint.ID, "sprint-1")
	}

	// Step 2: Resume from COMPLETED → advances to new sprint
	result2, err := Resume(tmpDir, "human")
	if err != nil {
		t.Fatalf("Resume() step 2 error: %v", err)
	}

	if result2.SprintAdvanced == nil {
		t.Fatal("Step 2: Expected SprintAdvanced to be non-nil")
	}
	if result2.SprintAdvanced.NewSprintID != "sprint-2" {
		t.Errorf("SprintAdvanced.NewSprintID = %q, want %q", result2.SprintAdvanced.NewSprintID, "sprint-2")
	}

	// Verify final persisted state
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if readState.Sprint.ID != "sprint-2" {
		t.Errorf("Sprint.ID = %q, want %q", readState.Sprint.ID, "sprint-2")
	}
	if readState.Sprint.Number != 2 {
		t.Errorf("Sprint.Number = %d, want 2", readState.Sprint.Number)
	}
	if readState.Sprint.Status != models.SprintStatusInProgress {
		t.Errorf("Sprint.Status = %v, want IN_PROGRESS", readState.Sprint.Status)
	}
	if len(readState.SprintHistory) != 1 {
		t.Errorf("SprintHistory length = %d, want 1", len(readState.SprintHistory))
	}
}

// TestAdvanceSprint_CarriesMergedPlanningWithUnconsumedOutput is a regression test:
// a merged planning task with Output[] but no TransitionsExecuted must be carried
// into the new sprint so the orchestrator can fire PLANNING_COMPLETE.
func TestAdvanceSprint_CarriesMergedPlanningWithUnconsumedOutput(t *testing.T) {
	t.Parallel()

	tmpDir, stateFile := setupAdvanceTest(t)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusCheckpoint
	state.Sprint.Number = 1

	// Merged planning task with unconsumed output (no transitions executed)
	planningTask := testhelpers.BuildTaskByStatus("code-planning-1", models.TaskStatusMerged, now)
	planningTask.RolePair = "code-planning-pair"
	planningTask.Output = []models.OutputEntry{
		{Desc: "implement feature X", DoneWhen: "tests pass", Scope: "internal/", SpecRef: "README.md"},
	}
	// TransitionsExecuted is nil — output not yet consumed

	state.Tasks = []models.Task{planningTask}
	state.Sprint.Scope.Planned = []string{"code-planning-1"}

	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := AdvanceSprint(tmpDir)
	if err != nil {
		t.Fatalf("AdvanceSprint() error: %v", err)
	}

	// The planning task must be carried forward
	found := false
	for _, id := range result.CarriedTasks {
		if id == "code-planning-1" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("CarriedTasks = %v, want to include code-planning-1 (merged planning with unconsumed output)", result.CarriedTasks)
	}

	// Verify persisted state
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	found = false
	for _, id := range readState.Sprint.Scope.Planned {
		if id == "code-planning-1" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Sprint.Scope.Planned = %v, want to include code-planning-1", readState.Sprint.Scope.Planned)
	}
}

// TestResumeWithSprintAdvance_CarriesMergedPlanning is a regression test for the
// COMPLETED/resume path: a merged planning task with unconsumed output must survive
// sprint advance via Resume.
func TestResumeWithSprintAdvance_CarriesMergedPlanning(t *testing.T) {
	t.Parallel()

	tmpDir, stateFile := setupAdvanceTest(t)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModeRunning
	state.Sprint.Status = models.SprintStatusCheckpoint
	state.Sprint.Number = 1

	// Merged planning task with unconsumed output
	planningTask := testhelpers.BuildTaskByStatus("code-planning-1", models.TaskStatusMerged, now)
	planningTask.RolePair = "code-planning-pair"
	planningTask.Output = []models.OutputEntry{
		{Desc: "implement feature X", DoneWhen: "tests pass", Scope: "internal/", SpecRef: "README.md"},
	}

	state.Tasks = []models.Task{planningTask}
	state.Sprint.Scope.Planned = []string{"code-planning-1"}

	testhelpers.WriteInitialState(t, stateFile, state)

	// Step 1: Resume from CHECKPOINT with all terminal → marks COMPLETED
	result1, err := Resume(tmpDir, "human")
	if err != nil {
		t.Fatalf("Resume() step 1 error: %v", err)
	}
	if result1.SprintAdvanced != nil {
		t.Error("Step 1: Expected SprintAdvanced to be nil (should mark COMPLETED, not advance)")
	}

	// Step 2: Resume from COMPLETED → advances to new sprint
	result2, err := Resume(tmpDir, "human")
	if err != nil {
		t.Fatalf("Resume() step 2 error: %v", err)
	}
	if result2.SprintAdvanced == nil {
		t.Fatal("Step 2: Expected SprintAdvanced to be non-nil")
	}

	// Verify the planning task was carried into the new sprint
	found := false
	for _, id := range result2.SprintAdvanced.CarriedTasks {
		if id == "code-planning-1" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("CarriedTasks = %v, want to include code-planning-1", result2.SprintAdvanced.CarriedTasks)
	}

	// Verify persisted state
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if readState.Sprint.ID != "sprint-2" {
		t.Errorf("Sprint.ID = %q, want %q", readState.Sprint.ID, "sprint-2")
	}
	found = false
	for _, id := range readState.Sprint.Scope.Planned {
		if id == "code-planning-1" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Sprint.Scope.Planned = %v, want to include code-planning-1", readState.Sprint.Scope.Planned)
	}
}

func TestResumeWithSprintAdvance_BlocksApprovedUnmergedPlanning(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                   string
		taskID                 string
		rolePair               string
		status                 models.TaskStatus
		removePipelineConfig   bool
		transitionsExecuted    map[string]bool
		wantTransitionsPersist bool
	}{
		{
			name:                 "legacy code planning source",
			taskID:               "code-planning-1",
			rolePair:             "code-planning-pair",
			status:               models.TaskStatusCodingPlanApproved,
			removePipelineConfig: true,
		},
		{
			name:     "configured epic planning source",
			taskID:   "epic-planning-1",
			rolePair: "epic-planning-pair",
			status:   models.TaskStatus("EPIC_PLAN_APPROVED"),
		},
		{
			name:                   "configured source with transition marker",
			taskID:                 "code-planning-1",
			rolePair:               "code-planning-pair",
			status:                 models.TaskStatusCodingPlanApproved,
			transitionsExecuted:    map[string]bool{"code-plan-to-coding": true},
			wantTransitionsPersist: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir, stateFile := setupAdvanceTest(t)
			if tt.removePipelineConfig {
				pipelinePath := filepath.Join(tmpDir, paths.ProjectDirName(), "pipeline.yaml")
				if err := os.Remove(pipelinePath); err != nil {
					t.Fatalf("failed to remove pipeline config for legacy fallback test: %v", err)
				}
			}

			now := time.Now().UTC()
			state := testhelpers.CreateValidState()
			state.Config.Mode = models.SystemModeRunning
			state.Sprint.Status = models.SprintStatusCompleted
			state.Sprint.Number = 1

			approvedBy := "code-reviewer-1"
			reviewCommit := "review123"
			planningTask := testhelpers.BuildTaskByStatus(tt.taskID, tt.status, now)
			planningTask.Type = models.TaskTypePlanning
			planningTask.RolePair = tt.rolePair
			planningTask.ApprovedBy = &approvedBy
			planningTask.ReviewCommit = &reviewCommit
			planningTask.Output = []models.OutputEntry{
				{Desc: "implement feature X", DoneWhen: "tests pass", Scope: "internal/", SpecRef: "README.md"},
			}
			planningTask.TransitionsExecuted = tt.transitionsExecuted

			state.Tasks = []models.Task{planningTask}
			state.Sprint.Scope.Planned = []string{tt.taskID}

			testhelpers.WriteInitialState(t, stateFile, state)

			result, err := Resume(tmpDir, "human")
			if err == nil {
				t.Fatal("Resume() error = nil, want approved planning merge precondition failure")
			}
			if result != nil {
				t.Fatalf("Resume() result = %+v, want nil on precondition failure", result)
			}
			if !strings.Contains(err.Error(), "approved planning task(s) must be merged") ||
				!strings.Contains(err.Error(), tt.taskID) {
				t.Fatalf("Resume() error = %q, want merge-first message naming %s", err, tt.taskID)
			}

			archivePath := filepath.Join(tmpDir, paths.ProjectDirName(), "archive", "sprint-1.yaml")
			if _, statErr := os.Stat(archivePath); !os.IsNotExist(statErr) {
				t.Fatalf("archive path stat error = %v, want archive not written at %s", statErr, archivePath)
			}

			bb := db.New(stateFile)
			readState, err := bb.Read()
			if err != nil {
				t.Fatalf("Failed to read state: %v", err)
			}
			if readState.Sprint.Status != models.SprintStatusCompleted {
				t.Errorf("Sprint.Status = %s, want %s", readState.Sprint.Status, models.SprintStatusCompleted)
			}
			if readState.Sprint.Number != 1 {
				t.Errorf("Sprint.Number = %d, want 1", readState.Sprint.Number)
			}
			if len(readState.SprintHistory) != 0 {
				t.Errorf("SprintHistory length = %d, want 0", len(readState.SprintHistory))
			}
			if got := readState.Sprint.Scope.Planned; len(got) != 1 || got[0] != tt.taskID {
				t.Errorf("Sprint.Scope.Planned = %v, want [%s]", got, tt.taskID)
			}
			readTask := readState.FindTask(tt.taskID)
			if readTask == nil {
				t.Fatalf("task %q not found after failed resume", tt.taskID)
			}
			if tt.wantTransitionsPersist {
				if !readTask.TransitionsExecuted["code-plan-to-coding"] {
					t.Errorf("TransitionsExecuted = %v, want code-plan-to-coding marker preserved", readTask.TransitionsExecuted)
				}
			} else if len(readTask.TransitionsExecuted) != 0 {
				t.Errorf("TransitionsExecuted = %v, want empty", readTask.TransitionsExecuted)
			}
			if len(readState.Tasks) != 1 {
				t.Errorf("Tasks length = %d, want 1", len(readState.Tasks))
			}
		})
	}
}

func TestApprovedPlanningTasksWithUnmergedOutput(t *testing.T) {
	t.Parallel()

	planningPairs := map[string]bool{
		"code-planning-pair": true,
		"epic-planning-pair": true,
	}
	approvedStatuses := map[string]models.TaskStatus{
		"code-planning-pair": models.TaskStatusCodingPlanApproved,
		"epic-planning-pair": models.TaskStatus("EPIC_PLAN_APPROVED"),
	}
	output := []models.OutputEntry{{Desc: "planned child"}}

	t.Run("one eligible task", func(t *testing.T) {
		state := testhelpers.CreateValidState()
		state.Tasks = []models.Task{{
			ID:       "code-plan-1",
			RolePair: "code-planning-pair",
			Status:   models.TaskStatusCodingPlanApproved,
			Output:   output,
		}}
		state.Sprint.Scope.Planned = []string{"code-plan-1"}

		got := ApprovedPlanningTasksWithUnmergedOutput(state, planningPairs, approvedStatuses)
		if !slices.Equal(got, []string{"code-plan-1"}) {
			t.Fatalf("ApprovedPlanningTasksWithUnmergedOutput() = %v, want [code-plan-1]", got)
		}
	})

	t.Run("multiple eligible tasks preserve sprint order and exclude ineligible tasks", func(t *testing.T) {
		state := testhelpers.CreateValidState()
		state.Tasks = []models.Task{
			{ID: "code-plan-1", RolePair: "code-planning-pair", Status: models.TaskStatusCodingPlanApproved, Output: output},
			{ID: "epic-plan-1", RolePair: "epic-planning-pair", Status: models.TaskStatus("EPIC_PLAN_APPROVED"), Output: output},
			{ID: "non-planning", RolePair: "coding-pair", Status: models.TaskStatusCodingPlanApproved, Output: output},
			{ID: "no-output", RolePair: "code-planning-pair", Status: models.TaskStatusCodingPlanApproved},
			{ID: "merged", RolePair: "code-planning-pair", Status: models.TaskStatusMerged, Output: output},
			{ID: "wrong-status", RolePair: "code-planning-pair", Status: models.TaskStatusCodePlanning, Output: output},
			{ID: "out-of-scope", RolePair: "code-planning-pair", Status: models.TaskStatusCodingPlanApproved, Output: output},
		}
		state.Sprint.Scope.Planned = []string{
			"epic-plan-1",
			"non-planning",
			"no-output",
			"merged",
			"wrong-status",
			"code-plan-1",
		}
		before, err := yaml.Marshal(state)
		if err != nil {
			t.Fatalf("yaml.Marshal(state) before query: %v", err)
		}

		got := ApprovedPlanningTasksWithUnmergedOutput(state, planningPairs, approvedStatuses)
		if !slices.Equal(got, []string{"epic-plan-1", "code-plan-1"}) {
			t.Fatalf("ApprovedPlanningTasksWithUnmergedOutput() = %v, want [epic-plan-1 code-plan-1]", got)
		}

		after, err := yaml.Marshal(state)
		if err != nil {
			t.Fatalf("yaml.Marshal(state) after query: %v", err)
		}
		if string(after) != string(before) {
			t.Fatal("ApprovedPlanningTasksWithUnmergedOutput() mutated its input state")
		}
	})
}

// TestResumeWithSprintAdvance_AfterMergeExecutesPlanningHandoff is a regression
// test for fdcb19a.
// When advancing from COMPLETED, Resume must execute available transitions so
// child tasks are created in the new sprint. Without this, merged planning tasks
// with unconsumed output[] are carried forward indefinitely without creating children.
func TestResumeWithSprintAdvance_AfterMergeExecutesPlanningHandoff(t *testing.T) {
	t.Parallel()

	tmpDir, stateFile := setupAdvanceTest(t)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModeRunning
	state.Sprint.Status = models.SprintStatusCompleted
	state.Sprint.Number = 1

	// Merged epic-planning task with unconsumed output (2 entries → 2 US writing tasks)
	planningTask := testhelpers.BuildTaskByStatus("epic-planning-1", models.TaskStatusMerged, now)
	planningTask.RolePair = "epic-planning-pair"
	planningTask.Output = []models.OutputEntry{
		{Desc: "US 1", DoneWhen: "done", Scope: "pkg/a", SpecRef: "README.md"},
		{Desc: "US 2", DoneWhen: "done", Scope: "pkg/b", SpecRef: "README.md"},
	}

	state.Tasks = []models.Task{planningTask}
	state.Sprint.Scope.Planned = []string{"epic-planning-1"}
	if got := ApprovedPlanningTasksWithUnmergedOutput(
		state,
		map[string]bool{"epic-planning-pair": true},
		map[string]models.TaskStatus{"epic-planning-pair": models.TaskStatus("EPIC_PLAN_APPROVED")},
	); len(got) != 0 {
		t.Fatalf("ApprovedPlanningTasksWithUnmergedOutput() after merge = %v, want []", got)
	}

	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Resume(tmpDir, "human")
	if err != nil {
		t.Fatalf("Resume() error: %v", err)
	}
	if result.SprintAdvanced == nil {
		t.Fatal("Expected SprintAdvanced to be non-nil")
	}
	if result.TransitionsExecuted != 1 {
		t.Errorf("TransitionsExecuted = %d, want 1", result.TransitionsExecuted)
	}
	if result.TransitionError != "" {
		t.Errorf("TransitionError = %q, want empty", result.TransitionError)
	}

	// Verify child tasks were created by post-advance transition execution
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	// Should have 3 tasks total: the parent + 2 children
	if len(readState.Tasks) < 3 {
		t.Errorf("Expected at least 3 tasks (parent + 2 children), got %d", len(readState.Tasks))
	}

	// Parent should have TransitionsExecuted set
	parent := readState.FindTask("epic-planning-1")
	if parent == nil {
		t.Fatal("Parent task epic-planning-1 not found")
	}
	if len(parent.TransitionsExecuted) == 0 {
		t.Error("Parent task TransitionsExecuted should be non-empty after transition")
	}

	// Children should be in the new sprint's planned scope
	childCount := 0
	for _, task := range readState.Tasks {
		if slices.Contains(task.EffectiveParentTasks(), "epic-planning-1") {
			childCount++
		}
	}
	if childCount != 2 {
		t.Errorf("Expected 2 child tasks from output[], got %d", childCount)
	}
}

func TestCollectMergedPlanningWithUnconsumedOutput_ConfiguredPairs(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()

	// Non-legacy planning pair with unconsumed output
	planningTask := testhelpers.BuildTaskByStatus("epic-plan-1", models.TaskStatusMerged, now)
	planningTask.RolePair = "epic-planning-pair"
	planningTask.Output = []models.OutputEntry{
		{Desc: "epic subtask", DoneWhen: "done", Scope: "pkg/", SpecRef: "README.md"},
	}

	state.Tasks = []models.Task{planningTask}
	state.Sprint.Scope.Planned = []string{"epic-plan-1"}

	// With configured pairs that include this role-pair
	configuredPairs := map[string]bool{"epic-planning-pair": true}
	carried := collectMergedPlanningWithUnconsumedOutput(state, configuredPairs)
	if len(carried) != 1 || carried[0] != "epic-plan-1" {
		t.Errorf("carried = %v, want [epic-plan-1] with configured planning pairs", carried)
	}

	// Without configured pairs (nil → legacy fallback to "code-planning-pair" only)
	carried = collectMergedPlanningWithUnconsumedOutput(state, nil)
	if len(carried) != 0 {
		t.Errorf("carried = %v, want [] — epic-planning-pair should not match legacy fallback", carried)
	}
}

func TestCollectMergedPlanningWithUnconsumedOutput_ConsumedOutputNotCarried(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()

	// Merged planning task with output BUT transitions already executed
	planningTask := testhelpers.BuildTaskByStatus("code-planning-1", models.TaskStatusMerged, now)
	planningTask.RolePair = "code-planning-pair"
	planningTask.Output = []models.OutputEntry{
		{Desc: "implement feature X", DoneWhen: "tests pass", Scope: "internal/", SpecRef: "README.md"},
	}
	planningTask.TransitionsExecuted = map[string]bool{"child-task-1": true}

	state.Tasks = []models.Task{planningTask}
	state.Sprint.Scope.Planned = []string{"code-planning-1"}

	carried := collectMergedPlanningWithUnconsumedOutput(state, nil)
	if len(carried) != 0 {
		t.Errorf("carried = %v, want [] — consumed output should not be carried", carried)
	}
}

func TestResumeWithoutSprintAdvance(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModeRunning
	state.Sprint.Status = models.SprintStatusCheckpoint
	state.Sprint.Number = 1

	// Not all planned tasks are terminal (mid-sprint checkpoint)
	readyTask := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now)
	state.Tasks = []models.Task{readyTask}
	state.Sprint.Scope.Planned = []string{"task-1"}

	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Resume(tmpDir, "human")
	if err != nil {
		t.Fatalf("Resume() error: %v", err)
	}

	if result.SprintAdvanced != nil {
		t.Error("Expected SprintAdvanced to be nil for mid-sprint checkpoint")
	}

	// Verify sprint stays the same
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if readState.Sprint.ID != "sprint-1" {
		t.Errorf("Sprint.ID = %q, want %q (should stay same)", readState.Sprint.ID, "sprint-1")
	}
	if readState.Sprint.Status != models.SprintStatusInProgress {
		t.Errorf("Sprint.Status = %v, want IN_PROGRESS", readState.Sprint.Status)
	}
}

func ptrS(s string) *string { return &s }

func TestIsManyToOneReady(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()

	m2oTransitions := []ManyToOneTransitionInfo{
		{Name: "us-to-coding", SourceRolePair: "us-writing-pair"},
	}

	t.Run("CompleteCohort", func(t *testing.T) {
		state := testhelpers.CreateValidState()
		parentID := "epic-plan-1"

		us1 := testhelpers.BuildTaskByStatus("us-1", models.TaskStatusMerged, now)
		us1.RolePair = "us-writing-pair"
		us1.ParentTask = ptrS(parentID)

		us2 := testhelpers.BuildTaskByStatus("us-2", models.TaskStatusMerged, now)
		us2.RolePair = "us-writing-pair"
		us2.ParentTask = ptrS(parentID)

		us3 := testhelpers.BuildTaskByStatus("us-3", models.TaskStatusMerged, now)
		us3.RolePair = "us-writing-pair"
		us3.ParentTask = ptrS(parentID)

		state.Tasks = []models.Task{us1, us2, us3}

		if !IsManyToOneReady(&state.Tasks[0], state, m2oTransitions) {
			t.Error("IsManyToOneReady() = false, want true for complete cohort")
		}
	})

	t.Run("IncompleteCohort", func(t *testing.T) {
		state := testhelpers.CreateValidState()
		parentID := "epic-plan-1"

		us1 := testhelpers.BuildTaskByStatus("us-1", models.TaskStatusMerged, now)
		us1.RolePair = "us-writing-pair"
		us1.ParentTask = ptrS(parentID)

		us2 := testhelpers.BuildTaskByStatus("us-2", models.TaskStatusMerged, now)
		us2.RolePair = "us-writing-pair"
		us2.ParentTask = ptrS(parentID)

		// us3 is not MERGED yet
		us3 := testhelpers.BuildTaskByStatus("us-3", models.TaskStatus("US_APPROVED"), now)
		us3.RolePair = "us-writing-pair"
		us3.ParentTask = ptrS(parentID)

		state.Tasks = []models.Task{us1, us2, us3}

		if IsManyToOneReady(&state.Tasks[0], state, m2oTransitions) {
			t.Error("IsManyToOneReady() = true, want false for incomplete cohort")
		}
	})

	t.Run("AlreadyExecuted", func(t *testing.T) {
		state := testhelpers.CreateValidState()
		parentID := "epic-plan-1"

		us1 := testhelpers.BuildTaskByStatus("us-1", models.TaskStatusMerged, now)
		us1.RolePair = "us-writing-pair"
		us1.ParentTask = ptrS(parentID)
		us1.TransitionsExecuted = map[string]bool{"us-to-coding": true}

		us2 := testhelpers.BuildTaskByStatus("us-2", models.TaskStatusMerged, now)
		us2.RolePair = "us-writing-pair"
		us2.ParentTask = ptrS(parentID)
		us2.TransitionsExecuted = map[string]bool{"us-to-coding": true}

		us3 := testhelpers.BuildTaskByStatus("us-3", models.TaskStatusMerged, now)
		us3.RolePair = "us-writing-pair"
		us3.ParentTask = ptrS(parentID)
		us3.TransitionsExecuted = map[string]bool{"us-to-coding": true}

		state.Tasks = []models.Task{us1, us2, us3}

		if IsManyToOneReady(&state.Tasks[0], state, m2oTransitions) {
			t.Error("IsManyToOneReady() = true, want false when transitions already executed")
		}
	})
}

func TestIsManyToOneReady_SupersededMember(t *testing.T) {
	t.Parallel()
	m2oTransitions := []ManyToOneTransitionInfo{{Name: "us-to-coding", SourceRolePair: "us-writing-pair"}}
	for _, tc := range []struct {
		name               string
		replacementLineage bool
		wantReady          bool
	}{
		{name: "replacement carries lineage", replacementLineage: true, wantReady: true},
		{name: "replacement lost lineage", wantReady: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// GIVEN a merged story whose sibling was replaced
			state := testhelpers.CreateValidState()
			state.Tasks = supersededCohort("epic-plan-1", tc.replacementLineage)

			// WHEN readiness is evaluated from the untouched story
			ready := IsManyToOneReady(state.FindTask("epic-plan-1-us-1"), state, m2oTransitions)

			// THEN the superseded original no longer holds the cohort, but a lineage gap does
			if ready != tc.wantReady {
				t.Fatalf("IsManyToOneReady() = %v, want %v", ready, tc.wantReady)
			}
		})
	}
}

func TestCollectMergedManyToOneWithUnfiredTransition(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()

	m2oTransitions := []ManyToOneTransitionInfo{
		{Name: "us-to-coding", SourceRolePair: "us-writing-pair"},
	}

	state := testhelpers.CreateValidState()
	parentID := "epic-plan-1"

	us1 := testhelpers.BuildTaskByStatus("us-1", models.TaskStatusMerged, now)
	us1.RolePair = "us-writing-pair"
	us1.ParentTask = ptrS(parentID)

	us2 := testhelpers.BuildTaskByStatus("us-2", models.TaskStatusMerged, now)
	us2.RolePair = "us-writing-pair"
	us2.ParentTask = ptrS(parentID)

	state.Tasks = []models.Task{us1, us2}
	state.Sprint.Scope.Planned = []string{"us-1", "us-2"}

	carried := collectMergedManyToOneWithUnfiredTransition(state, m2oTransitions)

	if len(carried) != 2 {
		t.Fatalf("carried = %v, want 2 items", carried)
	}
	if !slices.Contains(carried, "us-1") {
		t.Errorf("carried = %v, want to include us-1", carried)
	}
	if !slices.Contains(carried, "us-2") {
		t.Errorf("carried = %v, want to include us-2", carried)
	}

	// Verify dedup: add duplicate IDs to planned scope
	state.Sprint.Scope.Planned = []string{"us-1", "us-2", "us-1"}
	carried = collectMergedManyToOneWithUnfiredTransition(state, m2oTransitions)
	if len(carried) != 2 {
		t.Errorf("carried = %v (len %d), want exactly 2 (dedup should prevent duplicates)", carried, len(carried))
	}
}

func TestBuildSprintAdvancePlan_CarriesManyToOne(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()

	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusCheckpoint
	state.Sprint.Number = 1

	parentID := "epic-plan-1"

	us1 := testhelpers.BuildTaskByStatus("us-1", models.TaskStatusMerged, now)
	us1.RolePair = "us-writing-pair"
	us1.ParentTask = ptrS(parentID)

	us2 := testhelpers.BuildTaskByStatus("us-2", models.TaskStatusMerged, now)
	us2.RolePair = "us-writing-pair"
	us2.ParentTask = ptrS(parentID)

	state.Tasks = []models.Task{us1, us2}
	state.Sprint.Scope.Planned = []string{"us-1", "us-2"}

	detCtx := &advanceDetectionContext{
		planningPairs: map[string]bool{"code-planning-pair": true},
		m2oTransitions: []ManyToOneTransitionInfo{
			{Name: "us-to-coding", SourceRolePair: "us-writing-pair"},
		},
	}

	plan, err := buildSprintAdvancePlan(state, now, detCtx)
	if err != nil {
		t.Fatalf("buildSprintAdvancePlan() error: %v", err)
	}

	if !slices.Contains(plan.carriedTasks, "us-1") {
		t.Errorf("carriedTasks = %v, want to include us-1 (many-to-one cohort member)", plan.carriedTasks)
	}
	if !slices.Contains(plan.carriedTasks, "us-2") {
		t.Errorf("carriedTasks = %v, want to include us-2 (many-to-one cohort member)", plan.carriedTasks)
	}
}

func TestAdvanceSprint_CycleBlockedPlanningTaskCarriedForward(t *testing.T) {
	t.Parallel()

	tmpDir, stateFile := setupAdvanceTest(t)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusCheckpoint
	state.Sprint.Number = 1

	// Cycle-blocked planning task: MERGED with output, no transitions, but has cycle-blocked event
	cycledTask := testhelpers.BuildTaskByStatus("plan-cycled", models.TaskStatusMerged, now)
	cycledTask.RolePair = "code-planning-pair"
	cycledTask.Output = []models.OutputEntry{
		{Desc: "blocked work", DoneWhen: "done", Scope: "s", SpecRef: "README.md"},
	}
	cycledTask.History = append(cycledTask.History, models.TaskHistoryEntry{
		Time:  now,
		Event: models.TaskEventTransitionCycleBlocked,
		Extra: map[string]any{"transition": "code-plan-to-coding", "cycle_members": []string{"plan-cycled"}},
	})

	state.Tasks = []models.Task{cycledTask}
	state.Sprint.Scope.Planned = []string{"plan-cycled"}

	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := AdvanceSprint(tmpDir)
	if err != nil {
		t.Fatalf("AdvanceSprint() error: %v", err)
	}

	// Cycle-blocked task uses IsUnconsumedPlanningOutput (not IsPlanningCompleteEligible),
	// so it SHOULD be carried forward
	found := false
	for _, id := range result.CarriedTasks {
		if id == "plan-cycled" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("CarriedTasks = %v, want to include plan-cycled (cycle-blocked should carry forward)", result.CarriedTasks)
	}
}

// TestAdvanceSprint_PipelineTerminalNotCarried verifies that tasks in
// pipeline-defined terminal states (e.g. INTEGRATION_ANALYSIS_CLEAN) are NOT
// carried forward to the next sprint. Previously, only MERGED/ABANDONED/SUPERSEDED
// were recognized as terminal, causing pipeline-terminal tasks to be carried
// indefinitely and triggering an auto-resume sprint-advance loop.
func TestAdvanceSprint_PipelineTerminalNotCarried(t *testing.T) {
	t.Parallel()

	tmpDir, stateFile := setupAdvanceTest(t)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusCheckpoint
	state.Sprint.Number = 1

	// Task in a pipeline-defined terminal state (not universally terminal)
	integrationTask := testhelpers.BuildTaskByStatus("integration-1", "INTEGRATION_ANALYSIS_CLEAN", now)
	integrationTask.RolePair = "integration-pair"

	// Also a universally terminal task for comparison
	mergedTask := testhelpers.BuildTaskByStatus("coding-1", models.TaskStatusMerged, now)

	state.Tasks = []models.Task{integrationTask, mergedTask}
	state.Sprint.Scope.Planned = []string{"integration-1", "coding-1"}

	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := AdvanceSprint(tmpDir)
	if err != nil {
		t.Fatalf("AdvanceSprint() error: %v", err)
	}

	// Neither task should be carried — both are terminal
	if len(result.CarriedTasks) > 0 {
		t.Errorf("CarriedTasks = %v, want empty (pipeline-terminal tasks should not be carried)", result.CarriedTasks)
	}
}
