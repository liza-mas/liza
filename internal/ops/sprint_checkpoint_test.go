package ops

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestSprintCheckpoint_Success(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusInProgress
	state.Sprint.Timeline.Started = now.Add(-2 * time.Hour)
	state.Sprint.Timeline.Deadline = now.Add(6 * time.Hour)
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now),
		testhelpers.BuildTaskByStatus("task-2", models.TaskStatusImplementing, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := SprintCheckpoint(tmpDir, "")
	if err != nil {
		t.Fatalf("SprintCheckpoint() error: %v", err)
	}

	if result.CheckpointAt.IsZero() {
		t.Error("CheckpointAt should not be zero")
	}
	if result.ReportPath == "" {
		t.Error("ReportPath should not be empty")
	}

	// Verify report file was written
	content, err := os.ReadFile(result.ReportPath)
	if err != nil {
		t.Fatalf("Failed to read report: %v", err)
	}
	if !strings.Contains(string(content), "Sprint Summary") {
		t.Error("Report should contain 'Sprint Summary'")
	}
	if !strings.Contains(string(content), "MERGED") {
		t.Error("Report should contain task status table")
	}

	// Verify state updated
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if readState.Sprint.Status != models.SprintStatusCheckpoint {
		t.Errorf("Sprint status = %v, want CHECKPOINT", readState.Sprint.Status)
	}
	if readState.Sprint.Timeline.CheckpointAt == nil {
		t.Error("CheckpointAt should be set in state")
	}
}

func TestSprintCheckpoint_StoresTrigger(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusInProgress
	state.Sprint.Timeline.Started = time.Now().UTC().Add(-1 * time.Hour)
	state.Sprint.Timeline.Deadline = time.Now().UTC().Add(5 * time.Hour)
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := SprintCheckpoint(tmpDir, "PLANNING_COMPLETE")
	if err != nil {
		t.Fatalf("SprintCheckpoint() error: %v", err)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if readState.Sprint.CheckpointTrigger != "PLANNING_COMPLETE" {
		t.Errorf("CheckpointTrigger = %q, want %q", readState.Sprint.CheckpointTrigger, "PLANNING_COMPLETE")
	}
}

func TestSprintCheckpoint_EmptyTrigger(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusInProgress
	state.Sprint.Timeline.Started = time.Now().UTC().Add(-1 * time.Hour)
	state.Sprint.Timeline.Deadline = time.Now().UTC().Add(5 * time.Hour)
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := SprintCheckpoint(tmpDir, "")
	if err != nil {
		t.Fatalf("SprintCheckpoint() error: %v", err)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if readState.Sprint.CheckpointTrigger != "" {
		t.Errorf("CheckpointTrigger = %q, want empty", readState.Sprint.CheckpointTrigger)
	}
}

func TestSprintCheckpoint_AutoDetectsPlanningComplete(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusInProgress
	state.Sprint.Timeline.Started = now.Add(-1 * time.Hour)
	state.Sprint.Timeline.Deadline = now.Add(5 * time.Hour)

	// Add a merged planning task with unconsumed output
	task := models.Task{
		ID:          "plan-1",
		Status:      models.TaskStatusMerged,
		Description: "Plan feature X",
		Created:     now,
		SpecRef:     "specs/x.md",
		DoneWhen:    "Plan approved",
		Scope:       "specs/",
		RolePair:    "code-planning-pair",
		Output: []models.OutputEntry{
			{Desc: "impl X", DoneWhen: "tests pass", Scope: "pkg/x"},
		},
		History: []models.TaskHistoryEntry{},
	}
	state.Sprint.Scope.Planned = []string{"plan-1"}
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	// Pass empty trigger — should auto-detect PLANNING_COMPLETE
	_, err := SprintCheckpoint(tmpDir, "")
	if err != nil {
		t.Fatalf("SprintCheckpoint() error: %v", err)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if readState.Sprint.CheckpointTrigger != "PLANNING_COMPLETE" {
		t.Errorf("CheckpointTrigger = %q, want %q (auto-detect)", readState.Sprint.CheckpointTrigger, "PLANNING_COMPLETE")
	}
}

func TestSprintCheckpoint_AutoDetectsManyToOneReady(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusInProgress
	state.Sprint.Timeline.Started = now.Add(-1 * time.Hour)
	state.Sprint.Timeline.Deadline = now.Add(5 * time.Hour)

	cohort := makeManyToOneCohort("epic-plan-1", "us-writing-pair", models.TaskStatusMerged, "README.md", 2)
	state.Tasks = []models.Task{cohort[0], cohort[1]}
	state.Sprint.Scope.Planned = []string{cohort[0].ID, cohort[1].ID}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := SprintCheckpoint(tmpDir, "")
	if err != nil {
		t.Fatalf("SprintCheckpoint() error: %v", err)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if readState.Sprint.CheckpointTrigger != models.CheckpointTriggerManyToOneReady {
		t.Errorf("CheckpointTrigger = %q, want %q (auto-detect)", readState.Sprint.CheckpointTrigger, models.CheckpointTriggerManyToOneReady)
	}
}

func TestSprintCheckpoint_AlreadyAtCheckpoint(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusCheckpoint
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := SprintCheckpoint(tmpDir, "")
	if err == nil {
		t.Fatal("Expected error when already at CHECKPOINT")
	}
	if !errors.Is(err, ErrSprintAlreadyCheckpoint) {
		t.Fatalf("error = %v, want errors.Is(..., ErrSprintAlreadyCheckpoint)", err)
	}
}

func TestSprintCheckpoint_CompletedSprint(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusCompleted
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := SprintCheckpoint(tmpDir, "")
	if err == nil {
		t.Fatal("Expected error for COMPLETED sprint")
	}
	if !strings.Contains(err.Error(), "COMPLETED") {
		t.Errorf("Error = %q, want to contain 'COMPLETED'", err.Error())
	}
}

func TestSprintCheckpoint_AbortedSprint(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusAborted
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := SprintCheckpoint(tmpDir, "")
	if err == nil {
		t.Fatal("Expected error for ABORTED sprint")
	}
	if !strings.Contains(err.Error(), "ABORTED") {
		t.Errorf("Error = %q, want to contain 'ABORTED'", err.Error())
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{name: "minutes only", duration: 45 * time.Minute, expected: "45m"},
		{name: "hours and minutes", duration: 3*time.Hour + 15*time.Minute, expected: "3h 15m"},
		{name: "days hours minutes", duration: 50 * time.Hour, expected: "2d 2h 0m"},
		{name: "zero", duration: 0, expected: "0m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatDuration(tt.duration)
			if result != tt.expected {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.duration, result, tt.expected)
			}
		})
	}
}

func TestGenerateSprintSummary_WithAnomalies(t *testing.T) {
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Sprint.Timeline.Started = now.Add(-1 * time.Hour)
	state.Sprint.Timeline.Deadline = now.Add(5 * time.Hour)
	state.Anomalies = []models.Anomaly{
		{Type: "retry_loop", Task: "task-1", Reporter: "coder-1", Details: map[string]any{"error_pattern": "test anomaly"}},
		{Type: "retry_loop", Task: "task-1", Reporter: "coder-1", Details: map[string]any{"error_pattern": "another"}},
	}

	report := generateSprintSummary(state, now)

	if !strings.Contains(report, "Anomalies") {
		t.Error("Report should contain Anomalies section")
	}
	if !strings.Contains(report, "retry_loop") {
		t.Error("Report should contain anomaly type")
	}
}

func TestGenerateSprintSummary_Overdue(t *testing.T) {
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Sprint.Timeline.Started = now.Add(-10 * time.Hour)
	state.Sprint.Timeline.Deadline = now.Add(-2 * time.Hour) // Overdue

	report := generateSprintSummary(state, now)

	if !strings.Contains(report, "Overdue") {
		t.Error("Report should contain 'Overdue' for past deadlines")
	}
}

func TestGenerateSprintSummary_WithAgents(t *testing.T) {
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Sprint.Timeline.Started = now.Add(-1 * time.Hour)
	state.Sprint.Timeline.Deadline = now.Add(5 * time.Hour)
	taskRef := "task-1"
	state.Agents["coder-1"] = models.Agent{
		Role:        "coder",
		Status:      models.AgentStatusWorking,
		CurrentTask: &taskRef,
	}

	report := generateSprintSummary(state, now)

	if !strings.Contains(report, "coder-1") {
		t.Error("Report should contain agent ID")
	}
	if !strings.Contains(report, "task-1") {
		t.Error("Report should contain agent's current task")
	}
}

func TestGenerateSprintSummary_CircuitBreakerTriggered(t *testing.T) {
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Sprint.Timeline.Started = now.Add(-1 * time.Hour)
	state.Sprint.Timeline.Deadline = now.Add(5 * time.Hour)
	state.CircuitBreaker.Status = "TRIGGERED"
	state.CircuitBreaker.CurrentTrigger = &models.CircuitBreakerTrigger{
		Pattern:  "retry_cluster",
		Severity: "HIGH",
	}

	report := generateSprintSummary(state, now)

	if !strings.Contains(report, "TRIGGERED") {
		t.Error("Report should contain circuit breaker status")
	}
	if !strings.Contains(report, "retry_cluster") {
		t.Error("Report should contain trigger pattern")
	}
}
