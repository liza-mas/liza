package ops

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// --- Proceed: happy path ---

func TestProceed_CreatesChildTasks(t *testing.T) {
	tmpDir, stateFile := setupPipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted

	now := time.Now().UTC()
	parentID := "plan-task-1"
	reviewCommit := "abc123"
	task := models.Task{
		ID:           parentID,
		Type:         models.TaskTypeCoding,
		RolePair:     "code-planning-pair",
		Description:  "Plan the auth module",
		Status:       models.TaskStatus("CODING_PLAN_APPROVED"),
		Priority:     1,
		Created:      now,
		SpecRef:      "README.md",
		DoneWhen:     "Plan approved",
		Scope:        "auth module",
		ReviewCommit: &reviewCommit,
		Output: []models.OutputEntry{
			{Desc: "Implement login", DoneWhen: "POST /login works", Scope: "auth", SpecRef: "specs/auth.md#login"},
			{Desc: "Implement refresh", DoneWhen: "POST /refresh works", Scope: "auth", SpecRef: "specs/auth.md#refresh"},
		},
		History: []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, task)
	state.Sprint.Scope.Planned = []string{parentID}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Proceed(tmpDir, parentID, "code-plan-to-coding")
	if err != nil {
		t.Fatalf("Proceed() error: %v", err)
	}

	if result.SourceTaskID != parentID {
		t.Errorf("SourceTaskID = %q, want %q", result.SourceTaskID, parentID)
	}
	if result.TransitionName != "code-plan-to-coding" {
		t.Errorf("TransitionName = %q, want %q", result.TransitionName, "code-plan-to-coding")
	}
	if len(result.ChildTaskIDs) != 2 {
		t.Fatalf("ChildTaskIDs count = %d, want 2", len(result.ChildTaskIDs))
	}

	expectedID0 := "plan-task-1-code-plan-to-coding-0"
	expectedID1 := "plan-task-1-code-plan-to-coding-1"
	if result.ChildTaskIDs[0] != expectedID0 {
		t.Errorf("ChildTaskIDs[0] = %q, want %q", result.ChildTaskIDs[0], expectedID0)
	}
	if result.ChildTaskIDs[1] != expectedID1 {
		t.Errorf("ChildTaskIDs[1] = %q, want %q", result.ChildTaskIDs[1], expectedID1)
	}

	// Verify persisted state
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	// Source task unchanged status, transitions_executed set
	srcTask := readState.FindTask(parentID)
	if srcTask == nil {
		t.Fatal("Source task not found")
	}
	if srcTask.Status != models.TaskStatus("CODING_PLAN_APPROVED") {
		t.Errorf("Source status = %v, want CODING_PLAN_APPROVED", srcTask.Status)
	}
	if !srcTask.TransitionsExecuted["code-plan-to-coding"] {
		t.Error("transitions_executed should contain code-plan-to-coding")
	}

	// Child task 0
	child0 := readState.FindTask(expectedID0)
	if child0 == nil {
		t.Fatal("Child task 0 not found")
	}
	if child0.Status != models.TaskStatus("DRAFT_CODE") {
		t.Errorf("Child 0 status = %v, want DRAFT_CODE", child0.Status)
	}
	if !slices.Contains(child0.ParentTasks, parentID) {
		t.Errorf("Child 0 parent_tasks = %v, want to contain %q", child0.ParentTasks, parentID)
	}
	if child0.Description != "Implement login" {
		t.Errorf("Child 0 desc = %q, want %q", child0.Description, "Implement login")
	}
	if child0.DoneWhen != "POST /login works" {
		t.Errorf("Child 0 done_when = %q", child0.DoneWhen)
	}
	if child0.Scope != "auth" {
		t.Errorf("Child 0 scope = %q", child0.Scope)
	}
	if child0.SpecRef != "specs/auth.md#login" {
		t.Errorf("Child 0 spec_ref = %q", child0.SpecRef)
	}

	// Child task 1
	child1 := readState.FindTask(expectedID1)
	if child1 == nil {
		t.Fatal("Child task 1 not found")
	}
	if child1.Status != models.TaskStatus("DRAFT_CODE") {
		t.Errorf("Child 1 status = %v, want DRAFT_CODE", child1.Status)
	}
	if !slices.Contains(child1.ParentTasks, parentID) {
		t.Errorf("Child 1 parent_tasks = %v, want to contain %q", child1.ParentTasks, parentID)
	}
}

// --- Proceed: idempotency rejection ---

func TestProceed_RejectsRepeatedTransition(t *testing.T) {
	tmpDir, stateFile := setupPipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted

	now := time.Now().UTC()
	parentID := "plan-1"
	reviewCommit := "abc123"
	task := models.Task{
		ID:           parentID,
		Type:         models.TaskTypeCoding,
		RolePair:     "code-planning-pair",
		Description:  "Plan task",
		Status:       models.TaskStatus("CODING_PLAN_APPROVED"),
		Priority:     1,
		Created:      now,
		SpecRef:      "README.md",
		DoneWhen:     "Approved",
		Scope:        "scope",
		ReviewCommit: &reviewCommit,
		Output: []models.OutputEntry{
			{Desc: "Child", DoneWhen: "Done", Scope: "s", SpecRef: "README.md"},
		},
		TransitionsExecuted: map[string]bool{"code-plan-to-coding": true},
		History:             []models.TaskHistoryEntry{},
	}
	// Child already exists — transition was fully completed
	child := models.Task{
		ID:          "plan-1-code-plan-to-coding-0",
		Type:        models.TaskTypeCoding,
		RolePair:    "coding-pair",
		Description: "Child",
		Status:      models.TaskStatus("DRAFT_CODE"),
		Priority:    1,
		Created:     now,
		ParentTasks: []string{parentID},
		SpecRef:     "README.md",
		DoneWhen:    "Done",
		Scope:       "s",
		History:     []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, task, child)
	state.Sprint.Scope.Planned = []string{parentID}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := Proceed(tmpDir, parentID, "code-plan-to-coding")
	if err == nil {
		t.Fatal("Expected error for repeated transition")
	}
	if !strings.Contains(err.Error(), "already executed") {
		t.Errorf("Error = %q, want to contain 'already executed'", err.Error())
	}
}

// --- Proceed: sprint not COMPLETED ---

func TestProceed_RejectsIfSprintNotCompleted(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusInProgress

	now := time.Now().UTC()
	reviewCommit := "abc123"
	task := models.Task{
		ID:           "plan-1",
		Type:         models.TaskTypeCoding,
		Description:  "Plan task",
		Status:       models.TaskStatusMerged,
		Priority:     1,
		Created:      now,
		SpecRef:      "README.md",
		DoneWhen:     "Approved",
		Scope:        "scope",
		ReviewCommit: &reviewCommit,
		Output: []models.OutputEntry{
			{Desc: "Child", DoneWhen: "Done", Scope: "s", SpecRef: "README.md"},
		},
		History: []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, task)
	state.Sprint.Scope.Planned = []string{"plan-1"}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := Proceed(tmpDir, "plan-1", "code-plan-to-coding")
	if err == nil {
		t.Fatal("Expected error for non-COMPLETED sprint")
	}
	if !strings.Contains(err.Error(), "COMPLETED") {
		t.Errorf("Error = %q, want to contain 'COMPLETED'", err.Error())
	}
}

// --- Proceed: unknown transition ---

func TestProceed_RejectsUnknownTransition(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusCompleted

	now := time.Now().UTC()
	reviewCommit := "abc123"
	task := models.Task{
		ID:           "plan-1",
		Type:         models.TaskTypeCoding,
		Description:  "Plan task",
		Status:       models.TaskStatusMerged,
		Priority:     1,
		Created:      now,
		SpecRef:      "README.md",
		DoneWhen:     "Approved",
		Scope:        "scope",
		ReviewCommit: &reviewCommit,
		Output: []models.OutputEntry{
			{Desc: "Child", DoneWhen: "Done", Scope: "s", SpecRef: "README.md"},
		},
		History: []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, task)
	state.Sprint.Scope.Planned = []string{"plan-1"}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := Proceed(tmpDir, "plan-1", "unknown-transition")
	if err == nil {
		t.Fatal("Expected error for unknown transition")
	}
	if !strings.Contains(err.Error(), "unknown transition") {
		t.Errorf("Error = %q, want to contain 'unknown transition'", err.Error())
	}
}

// --- Proceed: task not found ---

func TestProceed_RejectsNonexistentTask(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusCompleted
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := Proceed(tmpDir, "nonexistent", "code-plan-to-coding")
	if err == nil {
		t.Fatal("Expected error for nonexistent task")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("Error = %q, want to contain 'not found'", err.Error())
	}
}

// --- Proceed: wrong status ---

func TestProceed_RejectsWrongStatus(t *testing.T) {
	tmpDir, stateFile := setupPipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted

	now := time.Now().UTC()
	task := models.Task{
		ID:           "plan-1",
		Type:         models.TaskTypeCoding,
		RolePair:     "code-planning-pair",
		Description:  "Plan task",
		Status:       models.TaskStatus("CODE_PLANNING"),
		Priority:     1,
		Created:      now,
		SpecRef:      "README.md",
		DoneWhen:     "Done",
		Scope:        "scope",
		AssignedTo:   testhelpers.StringPtr("coder-1"),
		Worktree:     testhelpers.StringPtr(".worktrees/plan-1"),
		BaseCommit:   testhelpers.StringPtr("abc123"),
		LeaseExpires: testhelpers.TimePtr(now.Add(30 * time.Minute)),
		History:      []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, task)
	state.Sprint.Scope.Planned = []string{"plan-1"}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := Proceed(tmpDir, "plan-1", "code-plan-to-coding")
	if err == nil {
		t.Fatal("Expected error for wrong status")
	}
	if !strings.Contains(err.Error(), "CODING_PLAN_APPROVED") {
		t.Errorf("Error = %q, want to contain 'CODING_PLAN_APPROVED'", err.Error())
	}
}

// --- Proceed: empty output ---

func TestProceed_RejectsEmptyOutput(t *testing.T) {
	tmpDir, stateFile := setupPipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted

	now := time.Now().UTC()
	reviewCommit := "abc123"
	task := models.Task{
		ID:           "plan-1",
		Type:         models.TaskTypeCoding,
		RolePair:     "code-planning-pair",
		Description:  "Plan task",
		Status:       models.TaskStatus("CODING_PLAN_APPROVED"),
		Priority:     1,
		Created:      now,
		SpecRef:      "README.md",
		DoneWhen:     "Approved",
		Scope:        "scope",
		ReviewCommit: &reviewCommit,
		Output:       []models.OutputEntry{},
		History:      []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, task)
	state.Sprint.Scope.Planned = []string{"plan-1"}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := Proceed(tmpDir, "plan-1", "code-plan-to-coding")
	if err == nil {
		t.Fatal("Expected error for empty output")
	}
	if !strings.Contains(err.Error(), "output") {
		t.Errorf("Error = %q, want to contain 'output'", err.Error())
	}
}

// --- Proceed: crash recovery ---

func TestProceed_CrashRecovery_CreatesMissingChildren(t *testing.T) {
	tmpDir, stateFile := setupPipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted

	now := time.Now().UTC()
	parentID := "plan-1"
	reviewCommit := "abc123"
	parentTask := models.Task{
		ID:           parentID,
		Type:         models.TaskTypeCoding,
		RolePair:     "code-planning-pair",
		Description:  "Plan task",
		Status:       models.TaskStatus("CODING_PLAN_APPROVED"),
		Priority:     1,
		Created:      now,
		SpecRef:      "README.md",
		DoneWhen:     "Approved",
		Scope:        "scope",
		ReviewCommit: &reviewCommit,
		Output: []models.OutputEntry{
			{Desc: "Child 0", DoneWhen: "Done 0", Scope: "s0", SpecRef: "README.md"},
			{Desc: "Child 1", DoneWhen: "Done 1", Scope: "s1", SpecRef: "README.md"},
		},
		TransitionsExecuted: map[string]bool{"code-plan-to-coding": true},
		History:             []models.TaskHistoryEntry{},
	}

	// Simulate crash: transition marked but only first child created
	child0 := models.Task{
		ID:          "plan-1-code-plan-to-coding-0",
		Type:        models.TaskTypeCoding,
		RolePair:    "coding-pair",
		Description: "Child 0",
		Status:      models.TaskStatus("DRAFT_CODE"),
		Priority:    1,
		Created:     now,
		ParentTasks: []string{parentID},
		SpecRef:     "README.md",
		DoneWhen:    "Done 0",
		Scope:       "s0",
		History:     []models.TaskHistoryEntry{},
	}

	state.Tasks = append(state.Tasks, parentTask, child0)
	state.Sprint.Scope.Planned = []string{parentID}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Proceed(tmpDir, parentID, "code-plan-to-coding")
	if err != nil {
		t.Fatalf("Proceed() error (crash recovery): %v", err)
	}

	// Should only create the missing child
	if len(result.ChildTaskIDs) != 1 {
		t.Fatalf("ChildTaskIDs count = %d, want 1 (only missing)", len(result.ChildTaskIDs))
	}
	if result.ChildTaskIDs[0] != "plan-1-code-plan-to-coding-1" {
		t.Errorf("ChildTaskIDs[0] = %q, want %q", result.ChildTaskIDs[0], "plan-1-code-plan-to-coding-1")
	}

	// Verify child 1 now exists
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	child1 := readState.FindTask("plan-1-code-plan-to-coding-1")
	if child1 == nil {
		t.Fatal("Child task 1 not found after crash recovery")
	}
	if child1.Description != "Child 1" {
		t.Errorf("Child 1 desc = %q, want %q", child1.Description, "Child 1")
	}
}

// --- Proceed: crash recovery with all children present ---

func TestProceed_CrashRecovery_AllChildrenExist(t *testing.T) {
	tmpDir, stateFile := setupPipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted

	now := time.Now().UTC()
	parentID := "plan-1"
	reviewCommit := "abc123"
	parentTask := models.Task{
		ID:           parentID,
		Type:         models.TaskTypeCoding,
		RolePair:     "code-planning-pair",
		Description:  "Plan task",
		Status:       models.TaskStatus("CODING_PLAN_APPROVED"),
		Priority:     1,
		Created:      now,
		SpecRef:      "README.md",
		DoneWhen:     "Approved",
		Scope:        "scope",
		ReviewCommit: &reviewCommit,
		Output: []models.OutputEntry{
			{Desc: "Child 0", DoneWhen: "Done 0", Scope: "s0", SpecRef: "README.md"},
		},
		TransitionsExecuted: map[string]bool{"code-plan-to-coding": true},
		History:             []models.TaskHistoryEntry{},
	}

	child0 := models.Task{
		ID:          "plan-1-code-plan-to-coding-0",
		Type:        models.TaskTypeCoding,
		RolePair:    "coding-pair",
		Description: "Child 0",
		Status:      models.TaskStatus("DRAFT_CODE"),
		Priority:    1,
		Created:     now,
		ParentTasks: []string{parentID},
		SpecRef:     "README.md",
		DoneWhen:    "Done 0",
		Scope:       "s0",
		History:     []models.TaskHistoryEntry{},
	}

	state.Tasks = append(state.Tasks, parentTask, child0)
	state.Sprint.Scope.Planned = []string{parentID}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := Proceed(tmpDir, parentID, "code-plan-to-coding")
	if err == nil {
		t.Fatal("Expected error when all children already exist")
	}
	if !strings.Contains(err.Error(), "already executed") {
		t.Errorf("Error = %q, want to contain 'already executed'", err.Error())
	}
}

// --- Proceed: output entry validation ---

func TestProceed_RejectsOutputMissingFields(t *testing.T) {
	tmpDir, stateFile := setupPipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted

	now := time.Now().UTC()
	reviewCommit := "abc123"
	task := models.Task{
		ID:           "plan-1",
		Type:         models.TaskTypeCoding,
		RolePair:     "code-planning-pair",
		Description:  "Plan task",
		Status:       models.TaskStatus("CODING_PLAN_APPROVED"),
		Priority:     1,
		Created:      now,
		SpecRef:      "README.md",
		DoneWhen:     "Approved",
		Scope:        "scope",
		ReviewCommit: &reviewCommit,
		Output: []models.OutputEntry{
			{Desc: "", DoneWhen: "Done", Scope: "s", SpecRef: "README.md"}, // missing desc
		},
		History: []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, task)
	state.Sprint.Scope.Planned = []string{"plan-1"}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := Proceed(tmpDir, "plan-1", "code-plan-to-coding")
	if err == nil {
		t.Fatal("Expected error for output entry missing desc")
	}
	if !strings.Contains(err.Error(), "output") {
		t.Errorf("Error = %q, want to contain 'output'", err.Error())
	}
}

// --- Pipeline-aware Proceed tests ---

// setupPipelineProceedTest creates a test dir with frozen pipeline config.
func setupPipelineProceedTest(t *testing.T) (string, string) {
	t.Helper()
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	// Copy the valid pipeline YAML to .liza/pipeline.yaml (frozen config).
	src, err := os.ReadFile(filepath.Join(testhelpers.FindRepoRoot(t), "internal", "pipeline", "testdata", "valid-coding-subpipeline.yaml"))
	if err != nil {
		t.Fatalf("Failed to read pipeline testdata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, ".liza", "pipeline.yaml"), src, 0o644); err != nil {
		t.Fatalf("Failed to write frozen pipeline config: %v", err)
	}

	return tmpDir, stateFile
}

func TestProceed_PipelineCreatesChildTasksWithRolePair(t *testing.T) {
	tmpDir, stateFile := setupPipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted

	now := time.Now().UTC()
	parentID := "plan-task-1"
	reviewCommit := "abc123"
	task := models.Task{
		ID:           parentID,
		Type:         models.TaskTypeCoding,
		RolePair:     "code-planning-pair",
		Description:  "Plan the auth module",
		Status:       models.TaskStatus("CODING_PLAN_APPROVED"),
		Priority:     1,
		Created:      now,
		SpecRef:      "README.md",
		DoneWhen:     "Plan approved",
		Scope:        "auth module",
		ReviewCommit: &reviewCommit,
		Output: []models.OutputEntry{
			{Desc: "Implement login", DoneWhen: "POST /login works", Scope: "auth", SpecRef: "specs/auth.md#login"},
			{Desc: "Implement refresh", DoneWhen: "POST /refresh works", Scope: "auth", SpecRef: "specs/auth.md#refresh"},
		},
		History: []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, task)
	state.Sprint.Scope.Planned = []string{parentID}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Proceed(tmpDir, parentID, "code-plan-to-coding")
	if err != nil {
		t.Fatalf("Proceed() error: %v", err)
	}

	if len(result.ChildTaskIDs) != 2 {
		t.Fatalf("ChildTaskIDs count = %d, want 2", len(result.ChildTaskIDs))
	}

	// Verify persisted state
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	// Child task 0: should have pipeline status and role_pair
	child0 := readState.FindTask(result.ChildTaskIDs[0])
	if child0 == nil {
		t.Fatal("Child task 0 not found")
	}
	if child0.Status != models.TaskStatus("DRAFT_CODE") {
		t.Errorf("Child 0 status = %v, want DRAFT_CODE", child0.Status)
	}
	if child0.RolePair != "coding-pair" {
		t.Errorf("Child 0 role_pair = %q, want %q", child0.RolePair, "coding-pair")
	}
	if !slices.Contains(child0.ParentTasks, parentID) {
		t.Errorf("Child 0 parent_tasks = %v, want to contain %q", child0.ParentTasks, parentID)
	}
	if child0.Description != "Implement login" {
		t.Errorf("Child 0 desc = %q, want %q", child0.Description, "Implement login")
	}
	if child0.Type != models.TaskTypeCoding {
		t.Errorf("Child 0 type = %q, want %q", child0.Type, models.TaskTypeCoding)
	}

	// Child task 1
	child1 := readState.FindTask(result.ChildTaskIDs[1])
	if child1 == nil {
		t.Fatal("Child task 1 not found")
	}
	if child1.Status != models.TaskStatus("DRAFT_CODE") {
		t.Errorf("Child 1 status = %v, want DRAFT_CODE", child1.Status)
	}
	if child1.RolePair != "coding-pair" {
		t.Errorf("Child 1 role_pair = %q, want %q", child1.RolePair, "coding-pair")
	}
	if !slices.Contains(child1.ParentTasks, parentID) {
		t.Errorf("Child 1 parent_tasks = %v, want to contain %q", child1.ParentTasks, parentID)
	}

	// Source task status should be unchanged
	srcTask := readState.FindTask(parentID)
	if srcTask.Status != models.TaskStatus("CODING_PLAN_APPROVED") {
		t.Errorf("Source status = %v, want CODING_PLAN_APPROVED", srcTask.Status)
	}
	if !srcTask.TransitionsExecuted["code-plan-to-coding"] {
		t.Error("transitions_executed should contain code-plan-to-coding")
	}
}

func TestAvailableManualTransitions_PipelineTask(t *testing.T) {
	tmpDir, stateFile := setupPipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	now := time.Now().UTC()
	task := models.Task{
		ID:          "plan-1",
		Type:        models.TaskTypeCoding,
		RolePair:    "code-planning-pair",
		Description: "Plan task",
		Status:      models.TaskStatus("CODING_PLAN_APPROVED"),
		Priority:    1,
		Created:     now,
		SpecRef:     "README.md",
		DoneWhen:    "Done",
		Scope:       "scope",
		History:     []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, task)
	testhelpers.WriteInitialState(t, stateFile, state)

	avail := AvailableManualTransitions(&state.Tasks[len(state.Tasks)-1], tmpDir)
	if len(avail) != 1 || avail[0] != "code-plan-to-coding" {
		t.Errorf("AvailableTransitions = %v, want [code-plan-to-coding]", avail)
	}
}

func TestProceed_PipelineRejectsAutoTransition(t *testing.T) {
	// Create a pipeline config with an auto transition to verify proceed rejects it.
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	autoYAML := `pipeline:
  roles:
    code-planner:
      type: doer
      display-name: "Code Planner"
    code-plan-reviewer:
      type: reviewer
      display-name: "Code Plan Reviewer"
    coder:
      type: doer
      display-name: "Coder"
    code-reviewer:
      type: reviewer
      display-name: "Code Reviewer"

  role-pairs:
    code-planning-pair:
      doer: code-planner
      reviewer: code-plan-reviewer
      states:
        initial: DRAFT_CODING_PLAN
        executing: CODE_PLANNING
        submitted: CODING_PLAN_TO_REVIEW
        reviewing: REVIEWING_CODING_PLAN
        approved: CODING_PLAN_APPROVED
        rejected: CODING_PLAN_REJECTED

    coding-pair:
      doer: coder
      reviewer: code-reviewer
      states:
        initial: DRAFT_CODE
        executing: IMPLEMENTING_CODE
        submitted: CODE_READY_FOR_REVIEW
        reviewing: REVIEWING_CODE
        approved: CODE_APPROVED
        rejected: CODE_REJECTED

  sub-pipelines:
    coding-subpipeline:
      steps:
        - code-planning-pair
        - coding-pair
      transitions:
        - name: code-plan-to-coding
          from: code-planning-pair.approved
          to: coding-pair.initial
          trigger: auto
          cardinality: per-subtask

  entry-points:
    detailed-spec: coding-subpipeline.code-planning-pair
`
	if err := os.WriteFile(filepath.Join(tmpDir, ".liza", "pipeline.yaml"), []byte(autoYAML), 0o644); err != nil {
		t.Fatalf("Failed to write pipeline config: %v", err)
	}

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted

	now := time.Now().UTC()
	parentID := "plan-1"
	reviewCommit := "abc123"
	task := models.Task{
		ID:           parentID,
		Type:         models.TaskTypeCoding,
		RolePair:     "code-planning-pair",
		Description:  "Plan task",
		Status:       models.TaskStatus("CODING_PLAN_APPROVED"),
		Priority:     1,
		Created:      now,
		SpecRef:      "README.md",
		DoneWhen:     "Plan approved",
		Scope:        "scope",
		ReviewCommit: &reviewCommit,
		Output: []models.OutputEntry{
			{Desc: "Child", DoneWhen: "Done", Scope: "s", SpecRef: "README.md"},
		},
		History: []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, task)
	state.Sprint.Scope.Planned = []string{parentID}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := Proceed(tmpDir, parentID, "code-plan-to-coding")
	if err == nil {
		t.Fatal("Expected error for auto transition via proceed")
	}
	if !strings.Contains(err.Error(), "manual") {
		t.Errorf("Error = %q, want to mention 'manual'", err.Error())
	}
	if !strings.Contains(err.Error(), "auto") {
		t.Errorf("Error = %q, want to mention 'auto'", err.Error())
	}
}

// --- Phase 2 pipeline test helpers ---

// setupPhase2PipelineProceedTest creates a test dir with the full Phase 2 pipeline config
// (including pipeline-transitions with us-to-coding one-to-one transition).
func setupPhase2PipelineProceedTest(t *testing.T) (string, string) {
	t.Helper()
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	// Copy the full Phase 2 pipeline YAML to .liza/pipeline.yaml (frozen config).
	src, err := os.ReadFile(filepath.Join(testhelpers.FindRepoRoot(t), "internal", "pipeline", "testdata", "valid-phase2-full.yaml"))
	if err != nil {
		t.Fatalf("Failed to read pipeline testdata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, ".liza", "pipeline.yaml"), src, 0o644); err != nil {
		t.Fatalf("Failed to write frozen pipeline config: %v", err)
	}

	return tmpDir, stateFile
}

// --- Proceed: one-to-one cardinality ---

func TestProceed_OneToOne_CreatesSingleChild(t *testing.T) {
	// us-to-coding is now many-to-one (CP1). Test single-member cohort creates one child.
	tmpDir, stateFile := setupPhase2PipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted

	now := time.Now().UTC()
	cohortParentID := "epic-plan-1"
	taskID := "us-task-1"
	reviewCommit := "abc123"
	task := models.Task{
		ID:           taskID,
		Type:         models.TaskTypeCoding,
		RolePair:     "us-writing-pair",
		Description:  "User authentication story",
		Status:       models.TaskStatus("US_APPROVED"),
		Priority:     1,
		Created:      now,
		ParentTasks:  []string{cohortParentID},
		SpecRef:      "specs/auth.md",
		DoneWhen:     "US approved",
		Scope:        "auth module",
		ReviewCommit: &reviewCommit,
		History:      []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, task)
	state.Sprint.Scope.Planned = []string{taskID}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Proceed(tmpDir, taskID, "us-to-coding")
	if err != nil {
		t.Fatalf("Proceed() error: %v", err)
	}

	// Should create exactly one child
	if len(result.ChildTaskIDs) != 1 {
		t.Fatalf("ChildTaskIDs count = %d, want 1", len(result.ChildTaskIDs))
	}

	// Child ID follows <cohort-parent>-<transition-name> pattern
	expectedChildID := "epic-plan-1-us-to-coding"
	if result.ChildTaskIDs[0] != expectedChildID {
		t.Errorf("ChildTaskIDs[0] = %q, want %q", result.ChildTaskIDs[0], expectedChildID)
	}

	// Verify persisted state
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	child := readState.FindTask(expectedChildID)
	if child == nil {
		t.Fatal("Child task not found")
	}

	// Child has correct status (architecture-pair initial)
	if child.Status != models.TaskStatus("DRAFT_ARCHITECTURE") {
		t.Errorf("Child status = %v, want DRAFT_ARCHITECTURE", child.Status)
	}
	// Child has correct role_pair (architecture-pair)
	if child.RolePair != "architecture-pair" {
		t.Errorf("Child role_pair = %q, want %q", child.RolePair, "architecture-pair")
	}
	// Child has parent_tasks set
	if !slices.Contains(child.ParentTasks, taskID) {
		t.Errorf("Child parent_tasks = %v, want to contain %q", child.ParentTasks, taskID)
	}
	// Child desc contains doer display name
	if !strings.Contains(child.Description, "Architect") {
		t.Errorf("Child desc = %q, want to contain 'Architect'", child.Description)
	}
	// Child spec_ref is parent's spec_ref
	if child.SpecRef != "specs/auth.md" {
		t.Errorf("Child spec_ref = %q, want %q", child.SpecRef, "specs/auth.md")
	}
	// Child type is architecture (target is architecture-pair)
	if child.Type != models.TaskTypeArchitecture {
		t.Errorf("Child type = %q, want %q", child.Type, models.TaskTypeArchitecture)
	}

	// Source task unchanged status, transitions_executed set
	srcTask := readState.FindTask(taskID)
	if srcTask.Status != models.TaskStatus("US_APPROVED") {
		t.Errorf("Source status = %v, want US_APPROVED", srcTask.Status)
	}
	if !srcTask.TransitionsExecuted["us-to-coding"] {
		t.Error("transitions_executed should contain us-to-coding")
	}
}

func TestProceed_EpicToUS_ChildRetainsCodingType(t *testing.T) {
	tmpDir, stateFile := setupPhase2PipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted

	now := time.Now().UTC()
	parentID := "epic-task-1"
	reviewCommit := "abc123"
	task := models.Task{
		ID:           parentID,
		Type:         models.TaskTypeEpicPlanning,
		RolePair:     "epic-planning-pair",
		Description:  "Epic for auth module",
		Status:       models.TaskStatus("EPIC_PLAN_APPROVED"),
		Priority:     1,
		Created:      now,
		SpecRef:      "specs/auth.md",
		DoneWhen:     "Epic approved",
		Scope:        "auth module",
		ReviewCommit: &reviewCommit,
		Output: []models.OutputEntry{
			{Desc: "Login story", DoneWhen: "Login works", Scope: "auth", SpecRef: "specs/auth.md#login"},
		},
		History: []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, task)
	state.Sprint.Scope.Planned = []string{parentID}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Proceed(tmpDir, parentID, "epic-to-us")
	if err != nil {
		t.Fatalf("Proceed() error: %v", err)
	}
	if len(result.ChildTaskIDs) != 1 {
		t.Fatalf("ChildTaskIDs count = %d, want 1", len(result.ChildTaskIDs))
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	child := readState.FindTask(result.ChildTaskIDs[0])
	if child == nil {
		t.Fatal("Child task not found")
	}
	// us-writing-pair resolves to TaskTypeUSWriting via TaskTypeForRole
	if child.Type != models.TaskTypeUSWriting {
		t.Errorf("Child type = %q, want %q", child.Type, models.TaskTypeUSWriting)
	}
	if child.RolePair != "us-writing-pair" {
		t.Errorf("Child role_pair = %q, want %q", child.RolePair, "us-writing-pair")
	}
}

func TestProceed_OneToOne_CrashRecovery_ChildExists(t *testing.T) {
	tmpDir, stateFile := setupPhase2PipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted

	now := time.Now().UTC()
	cohortParentID := "epic-plan-1"
	taskID := "us-task-1"
	reviewCommit := "abc123"
	parentTask := models.Task{
		ID:                  taskID,
		Type:                models.TaskTypeCoding,
		RolePair:            "us-writing-pair",
		Description:         "User authentication story",
		Status:              models.TaskStatus("US_APPROVED"),
		Priority:            1,
		Created:             now,
		ParentTasks:         []string{cohortParentID},
		SpecRef:             "specs/auth.md",
		DoneWhen:            "US approved",
		Scope:               "auth module",
		ReviewCommit:        &reviewCommit,
		TransitionsExecuted: map[string]bool{"us-to-coding": true},
		History:             []models.TaskHistoryEntry{},
	}

	// Child already exists — transition was fully completed
	childID := "epic-plan-1-us-to-coding"
	child := models.Task{
		ID:          childID,
		Type:        models.TaskTypeArchitecture,
		RolePair:    "architecture-pair",
		Description: "Architect task consolidating 1 approved tasks from epic-plan-1",
		Status:      models.TaskStatus("DRAFT_ARCHITECTURE"),
		Priority:    1,
		Created:     now,
		ParentTasks: []string{taskID},
		SpecRef:     "specs/auth.md",
		DoneWhen:    "done",
		Scope:       "scope",
		History:     []models.TaskHistoryEntry{},
	}

	state.Tasks = append(state.Tasks, parentTask, child)
	state.Sprint.Scope.Planned = []string{taskID}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := Proceed(tmpDir, taskID, "us-to-coding")
	if err == nil {
		t.Fatal("Expected error for repeated many-to-one transition")
	}
	if !strings.Contains(err.Error(), "already executed") {
		t.Errorf("Error = %q, want to contain 'already executed'", err.Error())
	}
}

func TestProceed_OneToOne_CrashRecovery_ChildMissing(t *testing.T) {
	// us-to-coding is now many-to-one. Test crash recovery: transition marked, child missing.
	tmpDir, stateFile := setupPhase2PipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted

	now := time.Now().UTC()
	cohortParentID := "epic-plan-1"
	taskID := "us-task-1"
	reviewCommit := "abc123"
	parentTask := models.Task{
		ID:                  taskID,
		Type:                models.TaskTypeCoding,
		RolePair:            "us-writing-pair",
		Description:         "User authentication story",
		Status:              models.TaskStatus("US_APPROVED"),
		Priority:            1,
		Created:             now,
		ParentTasks:         []string{cohortParentID},
		SpecRef:             "specs/auth.md",
		DoneWhen:            "US approved",
		Scope:               "auth module",
		ReviewCommit:        &reviewCommit,
		TransitionsExecuted: map[string]bool{"us-to-coding": true},
		History:             []models.TaskHistoryEntry{},
	}

	// Crash scenario: transition marked but child NOT created
	state.Tasks = append(state.Tasks, parentTask)
	state.Sprint.Scope.Planned = []string{taskID}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Proceed(tmpDir, taskID, "us-to-coding")
	if err != nil {
		t.Fatalf("Proceed() error (crash recovery): %v", err)
	}

	// Should create the missing child
	if len(result.ChildTaskIDs) != 1 {
		t.Fatalf("ChildTaskIDs count = %d, want 1", len(result.ChildTaskIDs))
	}
	expectedChildID := "epic-plan-1-us-to-coding"
	if result.ChildTaskIDs[0] != expectedChildID {
		t.Errorf("ChildTaskIDs[0] = %q, want %q", result.ChildTaskIDs[0], expectedChildID)
	}

	// Verify child now exists
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	child := readState.FindTask(expectedChildID)
	if child == nil {
		t.Fatal("Child task not found after crash recovery")
	}
}

// --- resolvePhaseRef: 3-part refs ---

func TestResolvePhaseRef_3PartRef(t *testing.T) {
	tmpDir, _ := setupPhase2PipelineProceedTest(t)

	resolver, _, err := loadResolver(tmpDir)
	if err != nil {
		t.Fatalf("loadResolver error: %v", err)
	}

	// 3-part ref: epic-spec-subpipeline.us-writing-pair.approved → US_APPROVED
	status, err := resolvePhaseRef(resolver, "epic-spec-subpipeline.us-writing-pair.approved")
	if err != nil {
		t.Fatalf("resolvePhaseRef error: %v", err)
	}
	if status != models.TaskStatus("US_APPROVED") {
		t.Errorf("resolvePhaseRef = %v, want US_APPROVED", status)
	}
}

func TestResolvePhaseRef_3PartRef_Initial(t *testing.T) {
	tmpDir, _ := setupPhase2PipelineProceedTest(t)

	resolver, _, err := loadResolver(tmpDir)
	if err != nil {
		t.Fatalf("loadResolver error: %v", err)
	}

	// 3-part ref: coding-subpipeline.code-planning-pair.initial → DRAFT_CODING_PLAN
	status, err := resolvePhaseRef(resolver, "coding-subpipeline.code-planning-pair.initial")
	if err != nil {
		t.Fatalf("resolvePhaseRef error: %v", err)
	}
	if status != models.TaskStatus("DRAFT_CODING_PLAN") {
		t.Errorf("resolvePhaseRef = %v, want DRAFT_CODING_PLAN", status)
	}
}

func TestResolvePhaseRef_2PartRef_StillWorks(t *testing.T) {
	tmpDir, _ := setupPhase2PipelineProceedTest(t)

	resolver, _, err := loadResolver(tmpDir)
	if err != nil {
		t.Fatalf("loadResolver error: %v", err)
	}

	// 2-part ref: code-planning-pair.approved → CODING_PLAN_APPROVED
	status, err := resolvePhaseRef(resolver, "code-planning-pair.approved")
	if err != nil {
		t.Fatalf("resolvePhaseRef error: %v", err)
	}
	if status != models.TaskStatus("CODING_PLAN_APPROVED") {
		t.Errorf("resolvePhaseRef = %v, want CODING_PLAN_APPROVED", status)
	}
}

// --- AvailableTransitions: pipeline-transitions ---

func TestAvailableManualTransitions_PipelineTransition_USApproved(t *testing.T) {
	tmpDir, stateFile := setupPhase2PipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	now := time.Now().UTC()
	task := models.Task{
		ID:          "us-1",
		Type:        models.TaskTypeCoding,
		RolePair:    "us-writing-pair",
		Description: "US task",
		Status:      models.TaskStatus("US_APPROVED"),
		Priority:    1,
		Created:     now,
		SpecRef:     "README.md",
		DoneWhen:    "Done",
		Scope:       "scope",
		History:     []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, task)
	testhelpers.WriteInitialState(t, stateFile, state)

	avail := AvailableManualTransitions(&state.Tasks[len(state.Tasks)-1], tmpDir)
	if len(avail) != 1 || avail[0] != "us-to-coding" {
		t.Errorf("AvailableTransitions = %v, want [us-to-coding]", avail)
	}
}

func TestAvailableManualTransitions_PipelineExcludesExecuted(t *testing.T) {
	tmpDir, stateFile := setupPipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	now := time.Now().UTC()
	task := models.Task{
		ID:                  "plan-1",
		Type:                models.TaskTypeCoding,
		RolePair:            "code-planning-pair",
		Description:         "Plan task",
		Status:              models.TaskStatus("CODING_PLAN_APPROVED"),
		Priority:            1,
		Created:             now,
		SpecRef:             "README.md",
		DoneWhen:            "Done",
		Scope:               "scope",
		TransitionsExecuted: map[string]bool{"code-plan-to-coding": true},
		History:             []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, task)
	testhelpers.WriteInitialState(t, stateFile, state)

	avail := AvailableManualTransitions(&state.Tasks[len(state.Tasks)-1], tmpDir)
	if len(avail) != 0 {
		t.Errorf("AvailableTransitions = %v, want [] (already executed)", avail)
	}
}

// --- ExecuteAvailableTransitions tests ---

func TestExecuteAvailableTransitions_CreatesChildrenForMergedTasks(t *testing.T) {
	// us-to-coding is now many-to-one. Test EAT with single-member cohort.
	tmpDir, stateFile := setupPhase2PipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusInProgress

	now := time.Now().UTC()
	cohortParentID := "epic-plan-1"
	taskID := "us-task-1"
	reviewCommit := "abc123"
	mergeCommit := "def456"
	task := models.Task{
		ID:           taskID,
		Type:         models.TaskTypeCoding,
		RolePair:     "us-writing-pair",
		Description:  "User authentication story",
		Status:       models.TaskStatusMerged,
		Priority:     1,
		Created:      now,
		ParentTasks:  []string{cohortParentID},
		SpecRef:      "specs/auth.md",
		DoneWhen:     "US approved",
		Scope:        "auth module",
		ReviewCommit: &reviewCommit,
		MergeCommit:  &mergeCommit,
		History:      []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, task)
	state.Sprint.Scope.Planned = []string{taskID}
	testhelpers.WriteInitialState(t, stateFile, state)

	results, err := ExecuteAvailableTransitions(tmpDir, "manual")
	if err != nil {
		t.Fatalf("ExecuteAvailableTransitions() error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("results count = %d, want 1", len(results))
	}
	if results[0].SourceTaskID != taskID {
		t.Errorf("SourceTaskID = %q, want %q", results[0].SourceTaskID, taskID)
	}
	if results[0].TransitionName != "us-to-coding" {
		t.Errorf("TransitionName = %q, want %q", results[0].TransitionName, "us-to-coding")
	}
	if len(results[0].ChildTaskIDs) != 1 {
		t.Fatalf("ChildTaskIDs count = %d, want 1", len(results[0].ChildTaskIDs))
	}

	// Verify child exists in state
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	childID := results[0].ChildTaskIDs[0]
	child := readState.FindTask(childID)
	if child == nil {
		t.Fatal("Child task not found in state.Tasks")
	}
	if child.RolePair != "architecture-pair" {
		t.Errorf("Child role_pair = %q, want %q", child.RolePair, "architecture-pair")
	}
	if child.Status != models.TaskStatus("DRAFT_ARCHITECTURE") {
		t.Errorf("Child status = %v, want DRAFT_ARCHITECTURE", child.Status)
	}

	// Children MUST be in Sprint.Scope.Planned
	if !slices.Contains(readState.Sprint.Scope.Planned, childID) {
		t.Errorf("Child %q should be in Sprint.Scope.Planned", childID)
	}

	// Source task should have transition marked
	srcTask := readState.FindTask(taskID)
	if !srcTask.TransitionsExecuted["us-to-coding"] {
		t.Error("transitions_executed should contain us-to-coding")
	}
}

func TestExecuteAvailableTransitions_PerSubtask(t *testing.T) {
	tmpDir, stateFile := setupPhase2PipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusInProgress

	now := time.Now().UTC()
	parentID := "plan-task-1"
	reviewCommit := "abc123"
	mergeCommit := "def456"
	task := models.Task{
		ID:           parentID,
		Type:         models.TaskTypeCoding,
		RolePair:     "code-planning-pair",
		Description:  "Plan the auth module",
		Status:       models.TaskStatusMerged,
		Priority:     1,
		Created:      now,
		SpecRef:      "README.md",
		DoneWhen:     "Plan approved",
		Scope:        "auth module",
		ReviewCommit: &reviewCommit,
		MergeCommit:  &mergeCommit,
		Output: []models.OutputEntry{
			{Desc: "Implement login", DoneWhen: "POST /login works", Scope: "auth", SpecRef: "specs/auth.md#login"},
			{Desc: "Implement refresh", DoneWhen: "POST /refresh works", Scope: "auth", SpecRef: "specs/auth.md#refresh"},
		},
		History: []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, task)
	state.Sprint.Scope.Planned = []string{parentID}
	testhelpers.WriteInitialState(t, stateFile, state)

	results, err := ExecuteAvailableTransitions(tmpDir, "manual")
	if err != nil {
		t.Fatalf("ExecuteAvailableTransitions() error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("results count = %d, want 1", len(results))
	}
	if len(results[0].ChildTaskIDs) != 2 {
		t.Fatalf("ChildTaskIDs count = %d, want 2", len(results[0].ChildTaskIDs))
	}

	// Verify children exist
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	for _, childID := range results[0].ChildTaskIDs {
		child := readState.FindTask(childID)
		if child == nil {
			t.Fatalf("Child task %q not found in state.Tasks", childID)
		}
		if child.RolePair != "coding-pair" {
			t.Errorf("Child %q role_pair = %q, want %q", childID, child.RolePair, "coding-pair")
		}
		// Must be in sprint scope
		childFound := false
		for _, id := range readState.Sprint.Scope.Planned {
			if id == childID {
				childFound = true
				break
			}
		}
		if !childFound {
			t.Errorf("Child %q should be in Sprint.Scope.Planned", childID)
		}
	}
}

func TestExecuteAvailableTransitions_NoopWhenNoTransitions(t *testing.T) {
	tmpDir, stateFile := setupPhase2PipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusInProgress

	now := time.Now().UTC()
	reviewCommit := "abc123"
	mergeCommit := "def456"
	// coding-pair has no outgoing transitions — should produce no results
	task := models.Task{
		ID:           "code-task-1",
		Type:         models.TaskTypeCoding,
		RolePair:     "coding-pair",
		Description:  "Implement login",
		Status:       models.TaskStatusMerged,
		Priority:     1,
		Created:      now,
		SpecRef:      "README.md",
		DoneWhen:     "Done",
		Scope:        "scope",
		ReviewCommit: &reviewCommit,
		MergeCommit:  &mergeCommit,
		History:      []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, task)
	testhelpers.WriteInitialState(t, stateFile, state)

	results, err := ExecuteAvailableTransitions(tmpDir, "manual")
	if err != nil {
		t.Fatalf("ExecuteAvailableTransitions() error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("results count = %d, want 0 (no transitions for coding-pair)", len(results))
	}
}

func TestExecuteAvailableTransitions_Idempotent(t *testing.T) {
	tmpDir, stateFile := setupPhase2PipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusInProgress

	now := time.Now().UTC()
	parentID := "us-task-1"
	reviewCommit := "abc123"
	mergeCommit := "def456"
	childID := "us-task-1-us-to-coding"
	task := models.Task{
		ID:                  parentID,
		Type:                models.TaskTypeCoding,
		RolePair:            "us-writing-pair",
		Description:         "User authentication story",
		Status:              models.TaskStatusMerged,
		Priority:            1,
		Created:             now,
		SpecRef:             "specs/auth.md",
		DoneWhen:            "US approved",
		Scope:               "auth module",
		ReviewCommit:        &reviewCommit,
		MergeCommit:         &mergeCommit,
		TransitionsExecuted: map[string]bool{"us-to-coding": true},
		History:             []models.TaskHistoryEntry{},
	}
	child := models.Task{
		ID:          childID,
		Type:        models.TaskTypeCoding,
		RolePair:    "code-planning-pair",
		Description: "Code Planner task for: User authentication story",
		Status:      models.TaskStatus("DRAFT_CODING_PLAN"),
		Priority:    1,
		Created:     now,
		ParentTasks: []string{parentID},
		SpecRef:     "specs/auth.md",
		DoneWhen:    "done",
		Scope:       "scope",
		History:     []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, task, child)
	testhelpers.WriteInitialState(t, stateFile, state)

	results, err := ExecuteAvailableTransitions(tmpDir, "manual")
	if err != nil {
		t.Fatalf("ExecuteAvailableTransitions() error: %v", err)
	}
	// Transition already executed and child exists → skipped
	if len(results) != 0 {
		t.Errorf("results count = %d, want 0 (idempotent)", len(results))
	}
}

func TestExecuteAvailableTransitions_NoSprintGate(t *testing.T) {
	// Verify ExecuteAvailableTransitions works regardless of sprint status
	tmpDir, stateFile := setupPhase2PipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusInProgress // NOT COMPLETED

	now := time.Now().UTC()
	cohortParentID := "epic-plan-1"
	taskID := "us-task-1"
	reviewCommit := "abc123"
	mergeCommit := "def456"
	task := models.Task{
		ID:           taskID,
		Type:         models.TaskTypeCoding,
		RolePair:     "us-writing-pair",
		Description:  "User authentication story",
		Status:       models.TaskStatusMerged,
		Priority:     1,
		Created:      now,
		ParentTasks:  []string{cohortParentID},
		SpecRef:      "specs/auth.md",
		DoneWhen:     "US approved",
		Scope:        "auth module",
		ReviewCommit: &reviewCommit,
		MergeCommit:  &mergeCommit,
		History:      []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, task)
	state.Sprint.Scope.Planned = []string{taskID}
	testhelpers.WriteInitialState(t, stateFile, state)

	// Should succeed even though sprint is IN_PROGRESS (not COMPLETED)
	results, err := ExecuteAvailableTransitions(tmpDir, "manual")
	if err != nil {
		t.Fatalf("ExecuteAvailableTransitions() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results count = %d, want 1", len(results))
	}
}

// TestExecuteAvailableTransitions_ScopeDedupGuard verifies the defensive dedup
// guard when adding children to Sprint.Scope.Planned.
//
// Note: crash recovery through ExecuteAvailableTransitions is unreachable because
// AvailableTransitions filters out transitions already in TransitionsExecuted.
// The recoverCrashedTransition path is only reachable through direct Proceed calls.
// This test validates the dedup guard against a pre-seeded scope entry.
func TestExecuteAvailableTransitions_ScopeDedupGuard(t *testing.T) {
	tmpDir, stateFile := setupPhase2PipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusInProgress

	now := time.Now().UTC()
	parentID := "plan-task-1"
	reviewCommit := "abc123"
	mergeCommit := "def456"

	// Predictable child IDs based on the naming convention
	child0ID := "plan-task-1-code-plan-to-coding-0"
	child1ID := "plan-task-1-code-plan-to-coding-1"

	task := models.Task{
		ID:           parentID,
		Type:         models.TaskTypeCoding,
		RolePair:     "code-planning-pair",
		Description:  "Plan the auth module",
		Status:       models.TaskStatusMerged,
		Priority:     1,
		Created:      now,
		SpecRef:      "README.md",
		DoneWhen:     "Plan approved",
		Scope:        "auth module",
		ReviewCommit: &reviewCommit,
		MergeCommit:  &mergeCommit,
		Output: []models.OutputEntry{
			{Desc: "Implement login", DoneWhen: "POST /login works", Scope: "auth", SpecRef: "specs/auth.md#login"},
			{Desc: "Implement refresh", DoneWhen: "POST /refresh works", Scope: "auth", SpecRef: "specs/auth.md#refresh"},
		},
		History: []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, task)
	// Artificially pre-seed a child ID in scope to validate the dedup guard
	state.Sprint.Scope.Planned = []string{parentID, child0ID}
	testhelpers.WriteInitialState(t, stateFile, state)

	results, err := ExecuteAvailableTransitions(tmpDir, "manual")
	if err != nil {
		t.Fatalf("ExecuteAvailableTransitions() error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("results count = %d, want 1", len(results))
	}
	if len(results[0].ChildTaskIDs) != 2 {
		t.Fatalf("ChildTaskIDs count = %d, want 2", len(results[0].ChildTaskIDs))
	}

	// Verify sprint scope: each child ID exactly once, no duplicates
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	child0Count := 0
	child1Count := 0
	for _, id := range readState.Sprint.Scope.Planned {
		if id == child0ID {
			child0Count++
		}
		if id == child1ID {
			child1Count++
		}
	}
	if child0Count != 1 {
		t.Errorf("child %q appears %d times in Sprint.Scope.Planned, want 1 (dedup failed)", child0ID, child0Count)
	}
	if child1Count != 1 {
		t.Errorf("child %q appears %d times in Sprint.Scope.Planned, want 1", child1ID, child1Count)
	}
}

// --- Proceed: sprint scope update ---

func TestProceed_AddsChildrenToSprintScope(t *testing.T) {
	tmpDir, stateFile := setupPipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted

	now := time.Now().UTC()
	parentID := "plan-task-1"
	reviewCommit := "abc123"
	task := models.Task{
		ID:           parentID,
		Type:         models.TaskTypeCoding,
		RolePair:     "code-planning-pair",
		Description:  "Plan the auth module",
		Status:       models.TaskStatus("CODING_PLAN_APPROVED"),
		Priority:     1,
		Created:      now,
		SpecRef:      "README.md",
		DoneWhen:     "Plan approved",
		Scope:        "auth module",
		ReviewCommit: &reviewCommit,
		Output: []models.OutputEntry{
			{Desc: "Implement login", DoneWhen: "POST /login works", Scope: "auth", SpecRef: "specs/auth.md#login"},
			{Desc: "Implement refresh", DoneWhen: "POST /refresh works", Scope: "auth", SpecRef: "specs/auth.md#refresh"},
		},
		History: []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, task)
	state.Sprint.Scope.Planned = []string{parentID}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Proceed(tmpDir, parentID, "code-plan-to-coding")
	if err != nil {
		t.Fatalf("Proceed() error: %v", err)
	}

	// Verify children are in sprint scope
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	for _, childID := range result.ChildTaskIDs {
		found := false
		for _, id := range readState.Sprint.Scope.Planned {
			if id == childID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Child %q not found in Sprint.Scope.Planned", childID)
		}
	}
}

// --- DependsOn tests ---

func TestProceed_DependsOnResolvesToChildTaskIDs(t *testing.T) {
	tmpDir, stateFile := setupPipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted

	now := time.Now().UTC()
	parentID := "plan-deps-1"
	reviewCommit := "abc123"
	task := models.Task{
		ID:           parentID,
		Type:         models.TaskTypeCoding,
		RolePair:     "code-planning-pair",
		Description:  "Plan with dependencies",
		Status:       models.TaskStatus("CODING_PLAN_APPROVED"),
		Priority:     1,
		Created:      now,
		SpecRef:      "README.md",
		DoneWhen:     "Plan approved",
		Scope:        "deps test",
		ReviewCommit: &reviewCommit,
		Output: []models.OutputEntry{
			{Desc: "Setup DB", DoneWhen: "DB ready", Scope: "db", SpecRef: "specs/db.md"},
			{Desc: "Build API", DoneWhen: "API works", Scope: "api", SpecRef: "specs/api.md", DependsOn: []string{"0"}},
			{Desc: "Build UI", DoneWhen: "UI works", Scope: "ui", SpecRef: "specs/ui.md", DependsOn: []string{"0", "1"}},
		},
		History: []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, task)
	state.Sprint.Scope.Planned = []string{parentID}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Proceed(tmpDir, parentID, "code-plan-to-coding")
	if err != nil {
		t.Fatalf("Proceed() error: %v", err)
	}

	if len(result.ChildTaskIDs) != 3 {
		t.Fatalf("ChildTaskIDs count = %d, want 3", len(result.ChildTaskIDs))
	}

	// Verify persisted child tasks have correct DependsOn
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	child0ID := parentID + "-code-plan-to-coding-0"
	child1ID := parentID + "-code-plan-to-coding-1"
	child2ID := parentID + "-code-plan-to-coding-2"

	child0 := readState.FindTask(child0ID)
	child1 := readState.FindTask(child1ID)
	child2 := readState.FindTask(child2ID)

	if child0 == nil || child1 == nil || child2 == nil {
		t.Fatal("One or more child tasks not found")
	}

	// Child 0: no dependencies
	if len(child0.DependsOn) != 0 {
		t.Errorf("child0.DependsOn = %v, want empty", child0.DependsOn)
	}

	// Child 1: depends on child 0
	if len(child1.DependsOn) != 1 || child1.DependsOn[0] != child0ID {
		t.Errorf("child1.DependsOn = %v, want [%s]", child1.DependsOn, child0ID)
	}

	// Child 2: depends on child 0 and child 1
	if len(child2.DependsOn) != 2 || child2.DependsOn[0] != child0ID || child2.DependsOn[1] != child1ID {
		t.Errorf("child2.DependsOn = %v, want [%s, %s]", child2.DependsOn, child0ID, child1ID)
	}
}

func TestProceed_DependsOnInvalidIndex(t *testing.T) {
	tmpDir, stateFile := setupPipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted

	now := time.Now().UTC()
	parentID := "plan-bad-dep"
	reviewCommit := "abc123"
	task := models.Task{
		ID:           parentID,
		Type:         models.TaskTypeCoding,
		RolePair:     "code-planning-pair",
		Description:  "Plan with bad dep",
		Status:       models.TaskStatus("CODING_PLAN_APPROVED"),
		Priority:     1,
		Created:      now,
		SpecRef:      "README.md",
		DoneWhen:     "Plan approved",
		Scope:        "bad dep test",
		ReviewCommit: &reviewCommit,
		Output: []models.OutputEntry{
			{Desc: "Task A", DoneWhen: "Done", Scope: "s", SpecRef: "specs/a.md"},
			{Desc: "Task B", DoneWhen: "Done", Scope: "s", SpecRef: "specs/b.md", DependsOn: []string{"5"}},
		},
		History: []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, task)
	state.Sprint.Scope.Planned = []string{parentID}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := Proceed(tmpDir, parentID, "code-plan-to-coding")
	if err == nil {
		t.Fatal("Expected error for out-of-range DependsOn index")
	}
	if !strings.Contains(err.Error(), "out of range") {
		t.Errorf("Error = %q, want to contain 'out of range'", err.Error())
	}
}

func TestProceed_DependsOnNonNumeric(t *testing.T) {
	tmpDir, stateFile := setupPipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted

	now := time.Now().UTC()
	parentID := "plan-nan-dep"
	reviewCommit := "abc123"
	task := models.Task{
		ID:           parentID,
		Type:         models.TaskTypeCoding,
		RolePair:     "code-planning-pair",
		Description:  "Plan with non-numeric dep",
		Status:       models.TaskStatus("CODING_PLAN_APPROVED"),
		Priority:     1,
		Created:      now,
		SpecRef:      "README.md",
		DoneWhen:     "Plan approved",
		Scope:        "nan dep test",
		ReviewCommit: &reviewCommit,
		Output: []models.OutputEntry{
			{Desc: "Task A", DoneWhen: "Done", Scope: "s", SpecRef: "specs/a.md", DependsOn: []string{"abc"}},
		},
		History: []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, task)
	state.Sprint.Scope.Planned = []string{parentID}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := Proceed(tmpDir, parentID, "code-plan-to-coding")
	if err == nil {
		t.Fatal("Expected error for non-numeric DependsOn")
	}
	if !strings.Contains(err.Error(), "non-numeric") {
		t.Errorf("Error = %q, want to contain 'non-numeric'", err.Error())
	}
}

func TestProceed_DependsOnSelfReference(t *testing.T) {
	tmpDir, stateFile := setupPipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted

	now := time.Now().UTC()
	parentID := "plan-self-dep"
	reviewCommit := "abc123"
	task := models.Task{
		ID:           parentID,
		Type:         models.TaskTypeCoding,
		RolePair:     "code-planning-pair",
		Description:  "Plan with self dep",
		Status:       models.TaskStatus("CODING_PLAN_APPROVED"),
		Priority:     1,
		Created:      now,
		SpecRef:      "README.md",
		DoneWhen:     "Plan approved",
		Scope:        "self dep test",
		ReviewCommit: &reviewCommit,
		Output: []models.OutputEntry{
			{Desc: "Task A", DoneWhen: "Done", Scope: "s", SpecRef: "specs/a.md", DependsOn: []string{"0"}},
		},
		History: []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, task)
	state.Sprint.Scope.Planned = []string{parentID}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := Proceed(tmpDir, parentID, "code-plan-to-coding")
	if err == nil {
		t.Fatal("Expected error for self-referencing DependsOn")
	}
	if !strings.Contains(err.Error(), "references itself") {
		t.Errorf("Error = %q, want to contain 'references itself'", err.Error())
	}
}

func TestProceed_ChildTasksGetPlanRefFromOutputEntry(t *testing.T) {
	tmpDir, stateFile := setupPipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted

	now := time.Now().UTC()
	parentID := "plan-planref-1"
	reviewCommit := "abc123"
	task := models.Task{
		ID:           parentID,
		Type:         models.TaskTypeCoding,
		RolePair:     "code-planning-pair",
		Description:  "Plan with plan_ref",
		Status:       models.TaskStatus("CODING_PLAN_APPROVED"),
		Priority:     1,
		Created:      now,
		SpecRef:      "README.md",
		DoneWhen:     "Plan approved",
		Scope:        "auth module",
		ReviewCommit: &reviewCommit,
		Output: []models.OutputEntry{
			{Desc: "Task A", DoneWhen: "A works", Scope: "a", SpecRef: "specs/a.md", PlanRef: "specs/plans/20260317-plan.md"},
			{Desc: "Task B", DoneWhen: "B works", Scope: "b", SpecRef: "specs/b.md", PlanRef: "specs/plans/20260317-plan.md"},
		},
		History: []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, task)
	state.Sprint.Scope.Planned = []string{parentID}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Proceed(tmpDir, parentID, "code-plan-to-coding")
	if err != nil {
		t.Fatalf("Proceed() error: %v", err)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	for i, childID := range result.ChildTaskIDs {
		child := readState.FindTask(childID)
		if child == nil {
			t.Fatalf("Child task %d not found", i)
		}
		if child.PlanRef != "specs/plans/20260317-plan.md" {
			t.Errorf("Child %d plan_ref = %q, want %q", i, child.PlanRef, "specs/plans/20260317-plan.md")
		}
	}
}

func TestProceed_OneToOne_InheritsPlanRefFromParent(t *testing.T) {
	// us-to-coding is now many-to-one (CP1). Many-to-one creates architecture
	// tasks which don't inherit PlanRef — they produce arch_ref instead.
	// This test verifies the child is created correctly.
	tmpDir, stateFile := setupPhase2PipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted

	now := time.Now().UTC()
	cohortParentID := "epic-plan-1"
	taskID := "us-planref-1"
	reviewCommit := "abc123"
	task := models.Task{
		ID:           taskID,
		Type:         models.TaskTypeCoding,
		RolePair:     "us-writing-pair",
		Description:  "US with epic_ref",
		Status:       models.TaskStatus("US_APPROVED"),
		Priority:     1,
		Created:      now,
		ParentTasks:  []string{cohortParentID},
		SpecRef:      "specs/feature.md",
		EpicRef:      "specs/epics/auth-epic.md#capability-cap-001---authentication",
		DoneWhen:     "US approved",
		Scope:        "auth module",
		ReviewCommit: &reviewCommit,
		History:      []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, task)
	state.Sprint.Scope.Planned = []string{taskID}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Proceed(tmpDir, taskID, "us-to-coding")
	if err != nil {
		t.Fatalf("Proceed() error: %v", err)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	child := readState.FindTask(result.ChildTaskIDs[0])
	if child == nil {
		t.Fatal("Child task not found")
	}
	// Many-to-one child (architecture task) does not inherit PlanRef
	if child.PlanRef != "" {
		t.Errorf("Child plan_ref = %q, want empty (many-to-one architecture tasks don't inherit PlanRef)", child.PlanRef)
	}
	// Inherits SpecRef
	if child.SpecRef != "specs/feature.md" {
		t.Errorf("Child spec_ref = %q, want %q", child.SpecRef, "specs/feature.md")
	}
	// Inherits EpicRef doc-only (section anchor stripped at many-to-one boundary)
	if child.EpicRef != "specs/epics/auth-epic.md" {
		t.Errorf("Child epic_ref = %q, want %q (section anchor should be stripped)", child.EpicRef, "specs/epics/auth-epic.md")
	}
}

// --- arch_ref propagation tests ---

func TestProceed_PerSubtask_PropagatesArchRef(t *testing.T) {
	tmpDir, stateFile := setupPipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted

	now := time.Now().UTC()
	parentID := "arch-archref-1"
	reviewCommit := "abc123"
	task := models.Task{
		ID:           parentID,
		Type:         models.TaskTypeCoding,
		RolePair:     "architecture-pair",
		Description:  "Arch with arch_ref on entries",
		Status:       models.TaskStatus("ARCHITECTURE_APPROVED"),
		Priority:     1,
		Created:      now,
		SpecRef:      "README.md",
		DoneWhen:     "Architecture approved",
		Scope:        "auth module",
		ReviewCommit: &reviewCommit,
		Output: []models.OutputEntry{
			{Desc: "Task A", DoneWhen: "A works", Scope: "a", SpecRef: "specs/a.md", ArchRef: "specs/arch-plan/feature.md"},
			{Desc: "Task B", DoneWhen: "B works", Scope: "b", SpecRef: "specs/b.md", ArchRef: "specs/arch-plan/feature.md"},
		},
		History: []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, task)
	state.Sprint.Scope.Planned = []string{parentID}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Proceed(tmpDir, parentID, "architecture-to-code-plan")
	if err != nil {
		t.Fatalf("Proceed() error: %v", err)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	for i, childID := range result.ChildTaskIDs {
		child := readState.FindTask(childID)
		if child == nil {
			t.Fatalf("Child task %d not found", i)
		}
		if child.ArchRef != "specs/arch-plan/feature.md" {
			t.Errorf("Child %d arch_ref = %q, want %q", i, child.ArchRef, "specs/arch-plan/feature.md")
		}
	}
}

func TestProceed_PerSubtask_InheritsParentArchRef(t *testing.T) {
	tmpDir, stateFile := setupPipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted

	now := time.Now().UTC()
	parentID := "plan-archref-inherit-1"
	reviewCommit := "abc123"
	task := models.Task{
		ID:           parentID,
		Type:         models.TaskTypeCoding,
		RolePair:     "code-planning-pair",
		Description:  "Plan with parent arch_ref",
		Status:       models.TaskStatus("CODING_PLAN_APPROVED"),
		Priority:     1,
		Created:      now,
		SpecRef:      "README.md",
		ArchRef:      "specs/arch-plan/feature.md",
		DoneWhen:     "Plan approved",
		Scope:        "auth module",
		ReviewCommit: &reviewCommit,
		Output: []models.OutputEntry{
			{Desc: "Task A", DoneWhen: "A works", Scope: "a", SpecRef: "specs/a.md"},
			{Desc: "Task B", DoneWhen: "B works", Scope: "b", SpecRef: "specs/b.md"},
		},
		History: []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, task)
	state.Sprint.Scope.Planned = []string{parentID}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Proceed(tmpDir, parentID, "code-plan-to-coding")
	if err != nil {
		t.Fatalf("Proceed() error: %v", err)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	for i, childID := range result.ChildTaskIDs {
		child := readState.FindTask(childID)
		if child == nil {
			t.Fatalf("Child task %d not found", i)
		}
		if child.ArchRef != "specs/arch-plan/feature.md" {
			t.Errorf("Child %d arch_ref = %q, want %q (should inherit from parent)", i, child.ArchRef, "specs/arch-plan/feature.md")
		}
	}
}

// --- epic_ref propagation tests ---

func TestProceed_PerSubtask_InheritsParentEpicRef(t *testing.T) {
	tmpDir, stateFile := setupPipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted

	now := time.Now().UTC()
	parentID := "arch-epicref-inherit-1"
	reviewCommit := "abc123"
	task := models.Task{
		ID:           parentID,
		Type:         models.TaskTypeCoding,
		RolePair:     "architecture-pair",
		Description:  "Arch with parent epic_ref",
		Status:       models.TaskStatus("ARCHITECTURE_APPROVED"),
		Priority:     1,
		Created:      now,
		SpecRef:      "README.md",
		EpicRef:      "specs/epics/ep-001.md",
		DoneWhen:     "Architecture approved",
		Scope:        "auth module",
		ReviewCommit: &reviewCommit,
		Output: []models.OutputEntry{
			{Desc: "Task A", DoneWhen: "A works", Scope: "a", SpecRef: "specs/a.md", ArchRef: "specs/arch-plan/feature.md"},
			{Desc: "Task B", DoneWhen: "B works", Scope: "b", SpecRef: "specs/b.md", ArchRef: "specs/arch-plan/feature.md"},
		},
		History: []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, task)
	state.Sprint.Scope.Planned = []string{parentID}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Proceed(tmpDir, parentID, "architecture-to-code-plan")
	if err != nil {
		t.Fatalf("Proceed() error: %v", err)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	for i, childID := range result.ChildTaskIDs {
		child := readState.FindTask(childID)
		if child == nil {
			t.Fatalf("Child task %d not found", i)
		}
		if child.EpicRef != "specs/epics/ep-001.md" {
			t.Errorf("Child %d epic_ref = %q, want %q (should inherit from parent)", i, child.EpicRef, "specs/epics/ep-001.md")
		}
	}
}

func TestProceed_OneToOne_InheritsArchRef(t *testing.T) {
	tmpDir, stateFile := setupPipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusInProgress

	now := time.Now().UTC()
	parentID := "plan-archref-oto-1"
	reviewCommit := "abc123"
	task := models.Task{
		ID:           parentID,
		Type:         models.TaskTypeCoding,
		RolePair:     "code-planning-pair",
		Description:  "Plan with arch_ref for one-to-one",
		Status:       models.TaskStatusMerged,
		Priority:     1,
		Created:      now,
		SpecRef:      "README.md",
		ArchRef:      "specs/arch-plan/feature.md",
		PlanRef:      "specs/plans/plan.md",
		DoneWhen:     "Plan approved",
		Scope:        "auth module",
		ReviewCommit: &reviewCommit,
		History:      []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, task)
	state.Sprint.Scope.Planned = []string{parentID}
	testhelpers.WriteInitialState(t, stateFile, state)

	results, err := ExecuteAvailableTransitions(tmpDir, "auto")
	if err != nil {
		t.Fatalf("ExecuteAvailableTransitions(auto) error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("results count = %d, want 1", len(results))
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	child := readState.FindTask(results[0].ChildTaskIDs[0])
	if child == nil {
		t.Fatal("Child task not found")
	}
	if child.ArchRef != "specs/arch-plan/feature.md" {
		t.Errorf("Child arch_ref = %q, want %q", child.ArchRef, "specs/arch-plan/feature.md")
	}
}

func TestProceed_PerSubtask_EntryArchRefOverridesParent(t *testing.T) {
	tmpDir, stateFile := setupPipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted

	now := time.Now().UTC()
	parentID := "plan-archref-override-1"
	reviewCommit := "abc123"
	task := models.Task{
		ID:           parentID,
		Type:         models.TaskTypeCoding,
		RolePair:     "code-planning-pair",
		Description:  "Plan with override arch_ref",
		Status:       models.TaskStatus("CODING_PLAN_APPROVED"),
		Priority:     1,
		Created:      now,
		SpecRef:      "README.md",
		ArchRef:      "specs/arch-plan/old.md",
		DoneWhen:     "Plan approved",
		Scope:        "auth module",
		ReviewCommit: &reviewCommit,
		Output: []models.OutputEntry{
			{Desc: "Task A", DoneWhen: "A works", Scope: "a", SpecRef: "specs/a.md", ArchRef: "specs/arch-plan/new.md"},
		},
		History: []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, task)
	state.Sprint.Scope.Planned = []string{parentID}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Proceed(tmpDir, parentID, "code-plan-to-coding")
	if err != nil {
		t.Fatalf("Proceed() error: %v", err)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	child := readState.FindTask(result.ChildTaskIDs[0])
	if child == nil {
		t.Fatal("Child task not found")
	}
	if child.ArchRef != "specs/arch-plan/new.md" {
		t.Errorf("Child arch_ref = %q, want %q (entry should override parent)", child.ArchRef, "specs/arch-plan/new.md")
	}
}

// --- computeInheritedDeps tests ---

func TestComputeInheritedDeps_UpstreamExecutedSameTransition(t *testing.T) {
	tmpDir, _ := setupPipelineProceedTest(t)
	resolver, _, err := loadResolver(tmpDir)
	if err != nil {
		t.Fatalf("loadResolver: %v", err)
	}

	// Upstream plan-1 executed code-plan-to-coding with 3 output entries → 3 children exist
	upstreamID := "plan-1"
	transitionName := "code-plan-to-coding"
	s := &models.State{
		Tasks: []models.Task{
			{
				ID:       upstreamID,
				Status:   models.TaskStatusMerged,
				RolePair: "code-planning-pair",
				Output: []models.OutputEntry{
					{Desc: "a", DoneWhen: "a", Scope: "a", SpecRef: "s.md"},
					{Desc: "b", DoneWhen: "b", Scope: "b", SpecRef: "s.md"},
					{Desc: "c", DoneWhen: "c", Scope: "c", SpecRef: "s.md"},
				},
				TransitionsExecuted: map[string]bool{transitionName: true},
			},
			// The 3 children that were created by the transition
			{ID: perSubtaskChildID(upstreamID, transitionName, 0), Status: models.TaskStatus("DRAFT_CODE")},
			{ID: perSubtaskChildID(upstreamID, transitionName, 1), Status: models.TaskStatus("DRAFT_CODE")},
			{ID: perSubtaskChildID(upstreamID, transitionName, 2), Status: models.TaskStatus("DRAFT_CODE")},
			// Downstream plan-2 depends on plan-1
			{
				ID:        "plan-2",
				Status:    models.TaskStatusMerged,
				RolePair:  "code-planning-pair",
				DependsOn: []string{upstreamID},
			},
		},
	}

	downstream := s.FindTask("plan-2")
	inherited, err := computeInheritedDeps(s, downstream, transitionName, resolver)
	if err != nil {
		t.Fatalf("computeInheritedDeps: %v", err)
	}

	if len(inherited) != 3 {
		t.Fatalf("inherited count = %d, want 3", len(inherited))
	}
	for i := 0; i < 3; i++ {
		expected := perSubtaskChildID(upstreamID, transitionName, i)
		if inherited[i] != expected {
			t.Errorf("inherited[%d] = %q, want %q", i, inherited[i], expected)
		}
	}
}

func TestComputeInheritedDeps_UpstreamDidNotExecuteTransition(t *testing.T) {
	tmpDir, _ := setupPipelineProceedTest(t)
	resolver, _, err := loadResolver(tmpDir)
	if err != nil {
		t.Fatalf("loadResolver: %v", err)
	}

	s := &models.State{
		Tasks: []models.Task{
			{
				ID:       "plan-1",
				Status:   models.TaskStatusMerged,
				RolePair: "code-planning-pair",
				// No TransitionsExecuted — hasn't been transitioned yet
			},
			{
				ID:        "plan-2",
				Status:    models.TaskStatusMerged,
				RolePair:  "code-planning-pair",
				DependsOn: []string{"plan-1"},
			},
		},
	}

	downstream := s.FindTask("plan-2")
	inherited, err := computeInheritedDeps(s, downstream, "code-plan-to-coding", resolver)
	if err != nil {
		t.Fatalf("computeInheritedDeps: %v", err)
	}
	if len(inherited) != 0 {
		t.Errorf("inherited count = %d, want 0 (upstream hasn't executed transition)", len(inherited))
	}
}

func TestComputeInheritedDeps_UpstreamExecutedDifferentTransition(t *testing.T) {
	tmpDir, _ := setupPipelineProceedTest(t)
	resolver, _, err := loadResolver(tmpDir)
	if err != nil {
		t.Fatalf("loadResolver: %v", err)
	}

	// Upstream executed auto-code-plan-to-coding (one-to-one), not code-plan-to-coding
	s := &models.State{
		Tasks: []models.Task{
			{
				ID:                  "plan-1",
				Status:              models.TaskStatusMerged,
				RolePair:            "code-planning-pair",
				TransitionsExecuted: map[string]bool{"auto-code-plan-to-coding": true},
			},
			{ID: oneToOneChildID("plan-1", "auto-code-plan-to-coding"), Status: models.TaskStatus("DRAFT_CODE")},
			{
				ID:        "plan-2",
				Status:    models.TaskStatusMerged,
				RolePair:  "code-planning-pair",
				DependsOn: []string{"plan-1"},
			},
		},
	}

	downstream := s.FindTask("plan-2")
	inherited, err := computeInheritedDeps(s, downstream, "code-plan-to-coding", resolver)
	if err != nil {
		t.Fatalf("computeInheritedDeps: %v", err)
	}
	if len(inherited) != 0 {
		t.Errorf("inherited count = %d, want 0 (upstream executed different transition)", len(inherited))
	}
}

func TestComputeInheritedDeps_MissingUpstreamChild_ReturnsError(t *testing.T) {
	tmpDir, _ := setupPipelineProceedTest(t)
	resolver, _, err := loadResolver(tmpDir)
	if err != nil {
		t.Fatalf("loadResolver: %v", err)
	}

	// Upstream says transition executed with 2 output entries, but child-1 is missing
	s := &models.State{
		Tasks: []models.Task{
			{
				ID:       "plan-1",
				Status:   models.TaskStatusMerged,
				RolePair: "code-planning-pair",
				Output: []models.OutputEntry{
					{Desc: "a", DoneWhen: "a", Scope: "a", SpecRef: "s.md"},
					{Desc: "b", DoneWhen: "b", Scope: "b", SpecRef: "s.md"},
				},
				TransitionsExecuted: map[string]bool{"code-plan-to-coding": true},
			},
			// Only child-0 exists, child-1 is missing (crash)
			{ID: perSubtaskChildID("plan-1", "code-plan-to-coding", 0), Status: models.TaskStatus("DRAFT_CODE")},
			{
				ID:        "plan-2",
				Status:    models.TaskStatusMerged,
				RolePair:  "code-planning-pair",
				DependsOn: []string{"plan-1"},
			},
		},
	}

	downstream := s.FindTask("plan-2")
	_, err = computeInheritedDeps(s, downstream, "code-plan-to-coding", resolver)
	if err == nil {
		t.Fatal("expected error for missing upstream child")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("error = %q, want to contain 'missing'", err.Error())
	}
}

// --- Topological ordering tests ---

func TestEAT_TopoOrdering_UpstreamBeforeDownstream(t *testing.T) {
	tmpDir, stateFile := setupPipelineProceedTest(t)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusInProgress

	// plan-2 depends on plan-1, but plan-2 appears FIRST in state.Tasks
	plan2 := testhelpers.BuildTaskByStatus("plan-2", models.TaskStatusMerged, now)
	plan2.RolePair = "code-planning-pair"
	plan2.Output = []models.OutputEntry{
		{Desc: "d", DoneWhen: "d", Scope: "d", SpecRef: "s.md"},
	}
	plan2.DependsOn = []string{"plan-1"}

	plan1 := testhelpers.BuildTaskByStatus("plan-1", models.TaskStatusMerged, now)
	plan1.RolePair = "code-planning-pair"
	plan1.Output = []models.OutputEntry{
		{Desc: "a", DoneWhen: "a", Scope: "a", SpecRef: "s.md"},
		{Desc: "b", DoneWhen: "b", Scope: "b", SpecRef: "s.md"},
	}

	state.Tasks = append(state.Tasks, plan2, plan1)
	state.Sprint.Scope.Planned = []string{"plan-2", "plan-1"}
	testhelpers.WriteInitialState(t, stateFile, state)

	results, err := ExecuteAvailableTransitions(tmpDir, "manual")
	if err != nil {
		t.Fatalf("ExecuteAvailableTransitions: %v", err)
	}

	// plan-1 should execute first despite appearing second in array
	if len(results) < 2 {
		t.Fatalf("results count = %d, want >= 2", len(results))
	}
	if results[0].SourceTaskID != "plan-1" {
		t.Errorf("results[0].SourceTaskID = %q, want plan-1 (upstream first)", results[0].SourceTaskID)
	}
	if results[1].SourceTaskID != "plan-2" {
		t.Errorf("results[1].SourceTaskID = %q, want plan-2 (downstream second)", results[1].SourceTaskID)
	}
}

func TestEAT_StableOrdering_UnrelatedTasksPreserveArrayOrder(t *testing.T) {
	tmpDir, stateFile := setupPipelineProceedTest(t)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusInProgress

	// Two unrelated tasks — no dep relationship, should preserve original order
	planA := testhelpers.BuildTaskByStatus("plan-a", models.TaskStatusMerged, now)
	planA.RolePair = "code-planning-pair"
	planA.Output = []models.OutputEntry{
		{Desc: "a", DoneWhen: "a", Scope: "a", SpecRef: "s.md"},
	}

	planB := testhelpers.BuildTaskByStatus("plan-b", models.TaskStatusMerged, now)
	planB.RolePair = "code-planning-pair"
	planB.Output = []models.OutputEntry{
		{Desc: "b", DoneWhen: "b", Scope: "b", SpecRef: "s.md"},
	}

	state.Tasks = append(state.Tasks, planA, planB)
	state.Sprint.Scope.Planned = []string{"plan-a", "plan-b"}
	testhelpers.WriteInitialState(t, stateFile, state)

	results, err := ExecuteAvailableTransitions(tmpDir, "manual")
	if err != nil {
		t.Fatalf("ExecuteAvailableTransitions: %v", err)
	}

	if len(results) < 2 {
		t.Fatalf("results count = %d, want >= 2", len(results))
	}
	// plan-a appears first in Tasks array, should execute first
	if results[0].SourceTaskID != "plan-a" {
		t.Errorf("results[0].SourceTaskID = %q, want plan-a (stable order)", results[0].SourceTaskID)
	}
	if results[1].SourceTaskID != "plan-b" {
		t.Errorf("results[1].SourceTaskID = %q, want plan-b (stable order)", results[1].SourceTaskID)
	}
}

func TestEAT_CycleDetection_CyclicTasksGetHistoryEvent(t *testing.T) {
	tmpDir, stateFile := setupPipelineProceedTest(t)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusInProgress

	// Circular dependency: plan-a depends on plan-b, plan-b depends on plan-a
	planA := testhelpers.BuildTaskByStatus("plan-a", models.TaskStatusMerged, now)
	planA.RolePair = "code-planning-pair"
	planA.Output = []models.OutputEntry{
		{Desc: "a", DoneWhen: "a", Scope: "a", SpecRef: "s.md"},
	}
	planA.DependsOn = []string{"plan-b"}

	planB := testhelpers.BuildTaskByStatus("plan-b", models.TaskStatusMerged, now)
	planB.RolePair = "code-planning-pair"
	planB.Output = []models.OutputEntry{
		{Desc: "b", DoneWhen: "b", Scope: "b", SpecRef: "s.md"},
	}
	planB.DependsOn = []string{"plan-a"}

	state.Tasks = append(state.Tasks, planA, planB)
	state.Sprint.Scope.Planned = []string{"plan-a", "plan-b"}
	testhelpers.WriteInitialState(t, stateFile, state)

	results, err := ExecuteAvailableTransitions(tmpDir, "manual")
	if err != nil {
		t.Fatalf("ExecuteAvailableTransitions: %v", err)
	}

	// Cyclic tasks should NOT execute
	if len(results) != 0 {
		t.Errorf("results count = %d, want 0 (cyclic tasks skipped)", len(results))
	}

	// Verify history events on both tasks
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	for _, taskID := range []string{"plan-a", "plan-b"} {
		task := readState.FindTask(taskID)
		if task == nil {
			t.Fatalf("task %s not found", taskID)
		}
		found := false
		for _, h := range task.History {
			if h.Event == models.TaskEventTransitionCycleBlocked {
				found = true
				if trans, ok := h.Extra["transition"].(string); !ok || trans != "code-plan-to-coding" {
					t.Errorf("task %s cycle event transition = %v, want code-plan-to-coding", taskID, h.Extra["transition"])
				}
			}
		}
		if !found {
			t.Errorf("task %s missing transition_cycle_blocked history event", taskID)
		}
	}
}

func TestEAT_CycleDetection_Idempotent(t *testing.T) {
	tmpDir, stateFile := setupPipelineProceedTest(t)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusInProgress

	planA := testhelpers.BuildTaskByStatus("plan-a", models.TaskStatusMerged, now)
	planA.RolePair = "code-planning-pair"
	planA.Output = []models.OutputEntry{
		{Desc: "a", DoneWhen: "a", Scope: "a", SpecRef: "s.md"},
	}
	planA.DependsOn = []string{"plan-b"}

	planB := testhelpers.BuildTaskByStatus("plan-b", models.TaskStatusMerged, now)
	planB.RolePair = "code-planning-pair"
	planB.Output = []models.OutputEntry{
		{Desc: "b", DoneWhen: "b", Scope: "b", SpecRef: "s.md"},
	}
	planB.DependsOn = []string{"plan-a"}

	state.Tasks = append(state.Tasks, planA, planB)
	state.Sprint.Scope.Planned = []string{"plan-a", "plan-b"}
	testhelpers.WriteInitialState(t, stateFile, state)

	// Run twice
	_, _ = ExecuteAvailableTransitions(tmpDir, "manual")
	_, _ = ExecuteAvailableTransitions(tmpDir, "manual")

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	for _, taskID := range []string{"plan-a", "plan-b"} {
		task := readState.FindTask(taskID)
		count := 0
		for _, h := range task.History {
			if h.Event == models.TaskEventTransitionCycleBlocked {
				count++
			}
		}
		if count != 1 {
			t.Errorf("task %s has %d cycle-blocked events, want 1 (idempotent)", taskID, count)
		}
	}
}

func TestEAT_CycleDetection_DownstreamBlockedTransitively(t *testing.T) {
	tmpDir, stateFile := setupPipelineProceedTest(t)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusInProgress

	// True cycle: plan-a <-> plan-b. Downstream: plan-c -> plan-a, plan-d -> plan-c.
	planA := testhelpers.BuildTaskByStatus("plan-a", models.TaskStatusMerged, now)
	planA.RolePair = "code-planning-pair"
	planA.Output = []models.OutputEntry{{Desc: "a", DoneWhen: "a", Scope: "a", SpecRef: "s.md"}}
	planA.DependsOn = []string{"plan-b"}

	planB := testhelpers.BuildTaskByStatus("plan-b", models.TaskStatusMerged, now)
	planB.RolePair = "code-planning-pair"
	planB.Output = []models.OutputEntry{{Desc: "b", DoneWhen: "b", Scope: "b", SpecRef: "s.md"}}
	planB.DependsOn = []string{"plan-a"}

	planC := testhelpers.BuildTaskByStatus("plan-c", models.TaskStatusMerged, now)
	planC.RolePair = "code-planning-pair"
	planC.Output = []models.OutputEntry{{Desc: "c", DoneWhen: "c", Scope: "c", SpecRef: "s.md"}}
	planC.DependsOn = []string{"plan-a"}

	planD := testhelpers.BuildTaskByStatus("plan-d", models.TaskStatusMerged, now)
	planD.RolePair = "code-planning-pair"
	planD.Output = []models.OutputEntry{{Desc: "d", DoneWhen: "d", Scope: "d", SpecRef: "s.md"}}
	planD.DependsOn = []string{"plan-c"}

	state.Tasks = append(state.Tasks, planA, planB, planC, planD)
	state.Sprint.Scope.Planned = []string{"plan-a", "plan-b", "plan-c", "plan-d"}
	testhelpers.WriteInitialState(t, stateFile, state)

	results, err := ExecuteAvailableTransitions(tmpDir, "manual")
	if err != nil {
		t.Fatalf("ExecuteAvailableTransitions: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("results count = %d, want 0 (cycle and downstream tasks skipped)", len(results))
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	for _, taskID := range []string{"plan-a", "plan-b"} {
		task := readState.FindTask(taskID)
		if task == nil {
			t.Fatalf("task %s not found", taskID)
		}
		found := false
		for _, h := range task.History {
			if h.Event == models.TaskEventTransitionCycleBlocked {
				found = true
				members := extraToStringSlice(h.Extra["cycle_members"])
				wantMembers := []string{"plan-a", "plan-b"}
				if !slices.Equal(members, wantMembers) {
					t.Errorf("task %s cycle_members = %v, want %v", taskID, members, wantMembers)
				}
			}
		}
		if !found {
			t.Errorf("task %s missing transition_cycle_blocked event", taskID)
		}
	}

	planningPairs := map[string]bool{"code-planning-pair": true}
	for _, taskID := range []string{"plan-c", "plan-d"} {
		task := readState.FindTask(taskID)
		if task == nil {
			t.Fatalf("task %s not found", taskID)
		}
		for _, h := range task.History {
			if h.Event == models.TaskEventTransitionCycleBlocked {
				t.Errorf("task %s has transition_cycle_blocked event but is only downstream of a cycle", taskID)
			}
		}
		if IsPlanningCompleteEligible(task, planningPairs, readState) {
			t.Errorf("%s should not be planning-complete eligible while upstream cycle remains unresolved", taskID)
		}
	}
}

func TestEAT_CycleDetection_IndependentCyclesSeparated(t *testing.T) {
	tmpDir, stateFile := setupPipelineProceedTest(t)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusInProgress

	// Two independent cycles: {plan-a <-> plan-b} and {plan-c <-> plan-d}.
	planA := testhelpers.BuildTaskByStatus("plan-a", models.TaskStatusMerged, now)
	planA.RolePair = "code-planning-pair"
	planA.Output = []models.OutputEntry{{Desc: "a", DoneWhen: "a", Scope: "a", SpecRef: "s.md"}}
	planA.DependsOn = []string{"plan-b"}

	planB := testhelpers.BuildTaskByStatus("plan-b", models.TaskStatusMerged, now)
	planB.RolePair = "code-planning-pair"
	planB.Output = []models.OutputEntry{{Desc: "b", DoneWhen: "b", Scope: "b", SpecRef: "s.md"}}
	planB.DependsOn = []string{"plan-a"}

	planC := testhelpers.BuildTaskByStatus("plan-c", models.TaskStatusMerged, now)
	planC.RolePair = "code-planning-pair"
	planC.Output = []models.OutputEntry{{Desc: "c", DoneWhen: "c", Scope: "c", SpecRef: "s.md"}}
	planC.DependsOn = []string{"plan-d"}

	planD := testhelpers.BuildTaskByStatus("plan-d", models.TaskStatusMerged, now)
	planD.RolePair = "code-planning-pair"
	planD.Output = []models.OutputEntry{{Desc: "d", DoneWhen: "d", Scope: "d", SpecRef: "s.md"}}
	planD.DependsOn = []string{"plan-c"}

	state.Tasks = append(state.Tasks, planA, planB, planC, planD)
	state.Sprint.Scope.Planned = []string{"plan-a", "plan-b", "plan-c", "plan-d"}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := ExecuteAvailableTransitions(tmpDir, "manual")
	if err != nil {
		t.Fatalf("ExecuteAvailableTransitions: %v", err)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	for _, tc := range []struct {
		taskID      string
		wantMembers []string
	}{
		{taskID: "plan-a", wantMembers: []string{"plan-a", "plan-b"}},
		{taskID: "plan-b", wantMembers: []string{"plan-a", "plan-b"}},
		{taskID: "plan-c", wantMembers: []string{"plan-c", "plan-d"}},
		{taskID: "plan-d", wantMembers: []string{"plan-c", "plan-d"}},
	} {
		task := readState.FindTask(tc.taskID)
		if task == nil {
			t.Fatalf("task %s not found", tc.taskID)
		}
		found := false
		for _, h := range task.History {
			if h.Event == models.TaskEventTransitionCycleBlocked {
				found = true
				members := extraToStringSlice(h.Extra["cycle_members"])
				if !slices.Equal(members, tc.wantMembers) {
					t.Errorf("task %s cycle_members = %v, want %v", tc.taskID, members, tc.wantMembers)
				}
			}
		}
		if !found {
			t.Errorf("task %s missing transition_cycle_blocked event", tc.taskID)
		}
	}
}

func TestEAT_CrashRecoveryThroughEAT(t *testing.T) {
	tmpDir, stateFile := setupPipelineProceedTest(t)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusInProgress

	// Upstream task with TransitionsExecuted set but child-1 missing (crash)
	plan1 := testhelpers.BuildTaskByStatus("plan-1", models.TaskStatusMerged, now)
	plan1.RolePair = "code-planning-pair"
	plan1.Output = []models.OutputEntry{
		{Desc: "a", DoneWhen: "a", Scope: "a", SpecRef: "s.md"},
		{Desc: "b", DoneWhen: "b", Scope: "b", SpecRef: "s.md"},
	}
	plan1.TransitionsExecuted = map[string]bool{"code-plan-to-coding": true}

	// Only child-0 exists
	child0 := testhelpers.BuildTaskByStatus(perSubtaskChildID("plan-1", "code-plan-to-coding", 0), models.TaskStatus("DRAFT_CODE"), now)
	child0.RolePair = "coding-pair"

	state.Tasks = append(state.Tasks, plan1, child0)
	state.Sprint.Scope.Planned = []string{"plan-1", child0.ID}
	testhelpers.WriteInitialState(t, stateFile, state)

	results, err := ExecuteAvailableTransitions(tmpDir, "manual")
	if err != nil {
		t.Fatalf("ExecuteAvailableTransitions: %v", err)
	}

	// Should recover the missing child
	if len(results) != 1 {
		t.Fatalf("results count = %d, want 1 (crash recovery)", len(results))
	}
	if len(results[0].ChildTaskIDs) != 1 {
		t.Fatalf("recovered children = %d, want 1", len(results[0].ChildTaskIDs))
	}
	expectedChild1 := perSubtaskChildID("plan-1", "code-plan-to-coding", 1)
	if results[0].ChildTaskIDs[0] != expectedChild1 {
		t.Errorf("recovered child = %q, want %q", results[0].ChildTaskIDs[0], expectedChild1)
	}
}

// --- Phase-gate propagation tests ---

func TestEAT_PhaseGatePropagation(t *testing.T) {
	tmpDir, stateFile := setupPipelineProceedTest(t)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusInProgress

	// plan-1: MERGED with transition already executed and 3 children
	plan1 := testhelpers.BuildTaskByStatus("plan-1", models.TaskStatusMerged, now)
	plan1.RolePair = "code-planning-pair"
	plan1.Output = []models.OutputEntry{
		{Desc: "a", DoneWhen: "a", Scope: "a", SpecRef: "s.md"},
		{Desc: "b", DoneWhen: "b", Scope: "b", SpecRef: "s.md"},
		{Desc: "c", DoneWhen: "c", Scope: "c", SpecRef: "s.md"},
	}
	plan1.TransitionsExecuted = map[string]bool{"code-plan-to-coding": true}

	child0 := testhelpers.BuildTaskByStatus(perSubtaskChildID("plan-1", "code-plan-to-coding", 0), models.TaskStatus("DRAFT_CODE"), now)
	child0.RolePair = "coding-pair"
	child1 := testhelpers.BuildTaskByStatus(perSubtaskChildID("plan-1", "code-plan-to-coding", 1), models.TaskStatus("DRAFT_CODE"), now)
	child1.RolePair = "coding-pair"
	child2 := testhelpers.BuildTaskByStatus(perSubtaskChildID("plan-1", "code-plan-to-coding", 2), models.TaskStatus("DRAFT_CODE"), now)
	child2.RolePair = "coding-pair"

	// plan-2: MERGED depends on plan-1, with 2 output entries — NOT yet transitioned
	plan2 := testhelpers.BuildTaskByStatus("plan-2", models.TaskStatusMerged, now)
	plan2.RolePair = "code-planning-pair"
	plan2.Output = []models.OutputEntry{
		{Desc: "d", DoneWhen: "d", Scope: "d", SpecRef: "s.md"},
		{Desc: "e", DoneWhen: "e", Scope: "e", SpecRef: "s.md"},
	}
	plan2.DependsOn = []string{"plan-1"}

	state.Tasks = append(state.Tasks, plan1, child0, child1, child2, plan2)
	state.Sprint.Scope.Planned = []string{"plan-1", child0.ID, child1.ID, child2.ID, "plan-2"}
	testhelpers.WriteInitialState(t, stateFile, state)

	results, err := ExecuteAvailableTransitions(tmpDir, "manual")
	if err != nil {
		t.Fatalf("ExecuteAvailableTransitions: %v", err)
	}

	// Only plan-2 should produce results (plan-1 already transitioned)
	if len(results) != 1 {
		t.Fatalf("results count = %d, want 1", len(results))
	}
	if results[0].SourceTaskID != "plan-2" {
		t.Fatalf("result source = %q, want plan-2", results[0].SourceTaskID)
	}
	if len(results[0].ChildTaskIDs) != 2 {
		t.Fatalf("children count = %d, want 2", len(results[0].ChildTaskIDs))
	}

	// Verify children have inherited deps from plan-1's children
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}

	expectedInherited := []string{child0.ID, child1.ID, child2.ID}
	for _, childID := range results[0].ChildTaskIDs {
		child := readState.FindTask(childID)
		if child == nil {
			t.Fatalf("child %s not found", childID)
		}
		for _, expected := range expectedInherited {
			if !slices.Contains(child.DependsOn, expected) {
				t.Errorf("child %s missing inherited dep %s; DependsOn = %v", childID, expected, child.DependsOn)
			}
		}
	}
}

func TestProceedInner_InheritedDepsAppendedAfterSiblingDeps(t *testing.T) {
	now := time.Now().UTC()

	s := &models.State{
		Tasks: []models.Task{
			{
				ID:       "plan-1",
				Status:   models.TaskStatusMerged,
				RolePair: "code-planning-pair",
				Output: []models.OutputEntry{
					{Desc: "a", DoneWhen: "a", Scope: "a", SpecRef: "s.md"},
					{Desc: "b", DoneWhen: "b", Scope: "b", SpecRef: "s.md", DependsOn: []string{"0"}},
				},
			},
		},
	}

	tDef := transitionDef{
		requiredStatus: models.TaskStatusMerged,
		targetStatus:   models.TaskStatus("DRAFT_CODE"),
		cardinality:    "per-subtask",
		targetRolePair: "coding-pair",
		taskSlug:       "code-plan-to-coding",
	}

	inheritedDeps := []string{"upstream-child-0", "upstream-child-1"}
	result := &ProceedResult{SourceTaskID: "plan-1", TransitionName: "code-plan-to-coding"}

	err := proceedInner(s, "plan-1", "code-plan-to-coding", tDef, inheritedDeps, now, result)
	if err != nil {
		t.Fatalf("proceedInner: %v", err)
	}

	// Child 1 (output[1]) depends on output[0] (sibling) AND inherited deps
	child1 := s.FindTask(result.ChildTaskIDs[1])
	if child1 == nil {
		t.Fatal("child 1 not found")
	}

	// First dep should be sibling (from output DependsOn)
	siblingID := perSubtaskChildID("plan-1", "code-plan-to-coding", 0)
	if len(child1.DependsOn) < 1 || child1.DependsOn[0] != siblingID {
		t.Errorf("child1 DependsOn[0] = %v, want sibling %s", child1.DependsOn, siblingID)
	}
	// Then inherited deps
	for _, dep := range inheritedDeps {
		if !slices.Contains(child1.DependsOn, dep) {
			t.Errorf("child1 missing inherited dep %s; DependsOn = %v", dep, child1.DependsOn)
		}
	}
}

func TestProceed_InheritedDepsViaManualPath(t *testing.T) {
	tmpDir, stateFile := setupPipelineProceedTest(t)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted

	// plan-1 already transitioned with 2 children
	plan1 := testhelpers.BuildTaskByStatus("plan-1", models.TaskStatusMerged, now)
	plan1.RolePair = "code-planning-pair"
	plan1.Output = []models.OutputEntry{
		{Desc: "a", DoneWhen: "a", Scope: "a", SpecRef: "s.md"},
		{Desc: "b", DoneWhen: "b", Scope: "b", SpecRef: "s.md"},
	}
	plan1.TransitionsExecuted = map[string]bool{"code-plan-to-coding": true}

	child0 := testhelpers.BuildTaskByStatus(perSubtaskChildID("plan-1", "code-plan-to-coding", 0), models.TaskStatus("DRAFT_CODE"), now)
	child0.RolePair = "coding-pair"
	child1 := testhelpers.BuildTaskByStatus(perSubtaskChildID("plan-1", "code-plan-to-coding", 1), models.TaskStatus("DRAFT_CODE"), now)
	child1.RolePair = "coding-pair"

	// plan-2 depends on plan-1, at CODING_PLAN_APPROVED (manual transition source)
	plan2 := testhelpers.BuildTaskByStatus("plan-2", models.TaskStatus("CODING_PLAN_APPROVED"), now)
	plan2.RolePair = "code-planning-pair"
	plan2.Output = []models.OutputEntry{
		{Desc: "c", DoneWhen: "c", Scope: "c", SpecRef: "s.md"},
	}
	plan2.DependsOn = []string{"plan-1"}

	state.Tasks = append(state.Tasks, plan1, child0, child1, plan2)
	state.Sprint.Scope.Planned = []string{"plan-1", child0.ID, child1.ID, "plan-2"}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Proceed(tmpDir, "plan-2", "code-plan-to-coding")
	if err != nil {
		t.Fatalf("Proceed: %v", err)
	}

	if len(result.ChildTaskIDs) != 1 {
		t.Fatalf("children = %d, want 1", len(result.ChildTaskIDs))
	}

	// Verify the child inherits plan-1's children as deps
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}

	child := readState.FindTask(result.ChildTaskIDs[0])
	if child == nil {
		t.Fatal("child not found")
	}
	for _, expectedDep := range []string{child0.ID, child1.ID} {
		if !slices.Contains(child.DependsOn, expectedDep) {
			t.Errorf("child missing inherited dep %s; DependsOn = %v", expectedDep, child.DependsOn)
		}
	}
}

func TestEAT_CrashRecovery_PatchesExistingChildrenWithInheritedDeps(t *testing.T) {
	tmpDir, stateFile := setupPipelineProceedTest(t)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusInProgress

	// plan-1 already transitioned with 1 child
	plan1 := testhelpers.BuildTaskByStatus("plan-1", models.TaskStatusMerged, now)
	plan1.RolePair = "code-planning-pair"
	plan1.Output = []models.OutputEntry{
		{Desc: "a", DoneWhen: "a", Scope: "a", SpecRef: "s.md"},
	}
	plan1.TransitionsExecuted = map[string]bool{"code-plan-to-coding": true}
	upstreamChild := testhelpers.BuildTaskByStatus(perSubtaskChildID("plan-1", "code-plan-to-coding", 0), models.TaskStatus("DRAFT_CODE"), now)
	upstreamChild.RolePair = "coding-pair"

	// plan-2 depends on plan-1, transition marked but crashed mid-creation:
	// child-0 exists WITHOUT inherited deps, child-1 is missing
	plan2 := testhelpers.BuildTaskByStatus("plan-2", models.TaskStatusMerged, now)
	plan2.RolePair = "code-planning-pair"
	plan2.Output = []models.OutputEntry{
		{Desc: "b", DoneWhen: "b", Scope: "b", SpecRef: "s.md"},
		{Desc: "c", DoneWhen: "c", Scope: "c", SpecRef: "s.md"},
	}
	plan2.DependsOn = []string{"plan-1"}
	plan2.TransitionsExecuted = map[string]bool{"code-plan-to-coding": true}

	// Existing child created before crash — no inherited deps
	existingChild := testhelpers.BuildTaskByStatus(perSubtaskChildID("plan-2", "code-plan-to-coding", 0), models.TaskStatus("DRAFT_CODE"), now)
	existingChild.RolePair = "coding-pair"
	// child-1 is missing (crash)

	state.Tasks = append(state.Tasks, plan1, upstreamChild, plan2, existingChild)
	state.Sprint.Scope.Planned = []string{"plan-1", upstreamChild.ID, "plan-2", existingChild.ID}
	testhelpers.WriteInitialState(t, stateFile, state)

	results, err := ExecuteAvailableTransitions(tmpDir, "manual")
	if err != nil {
		t.Fatalf("ExecuteAvailableTransitions: %v", err)
	}

	// plan-2 should recover the missing child
	if len(results) != 1 {
		t.Fatalf("results count = %d, want 1", len(results))
	}
	if len(results[0].ChildTaskIDs) != 1 {
		t.Fatalf("recovered children = %d, want 1", len(results[0].ChildTaskIDs))
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}

	// Existing child should be patched with inherited deps
	patched := readState.FindTask(existingChild.ID)
	if patched == nil {
		t.Fatal("existing child not found")
	}
	if !slices.Contains(patched.DependsOn, upstreamChild.ID) {
		t.Errorf("existing child should be patched with inherited dep %s; DependsOn = %v", upstreamChild.ID, patched.DependsOn)
	}

	// Recovered child should have inherited deps
	recovered := readState.FindTask(results[0].ChildTaskIDs[0])
	if recovered == nil {
		t.Fatal("recovered child not found")
	}
	if !slices.Contains(recovered.DependsOn, upstreamChild.ID) {
		t.Errorf("recovered child missing inherited dep %s; DependsOn = %v", upstreamChild.ID, recovered.DependsOn)
	}
}

// --- ExecuteAvailableTransitions: trigger filter ---

// setupIntegrationPipelineProceedTest creates a temp dir with the valid-with-clean.yaml
// pipeline config (includes integration-subpipeline with auto transition).
func setupIntegrationPipelineProceedTest(t *testing.T) (string, string) {
	t.Helper()
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	src, err := os.ReadFile(filepath.Join(testhelpers.FindRepoRoot(t), "internal", "pipeline", "testdata", "valid-with-clean.yaml"))
	if err != nil {
		t.Fatalf("Failed to read pipeline testdata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, ".liza", "pipeline.yaml"), src, 0o644); err != nil {
		t.Fatalf("Failed to write frozen pipeline config: %v", err)
	}

	return tmpDir, stateFile
}

func TestExecuteAvailableTransitions_AutoTransition(t *testing.T) {
	tmpDir, stateFile := setupIntegrationPipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusInProgress

	now := time.Now().UTC()
	parentID := "integration-task-1"
	reviewCommit := "abc123"
	mergeCommit := "def456"
	task := models.Task{
		ID:           parentID,
		Type:         models.TaskTypeIntegration,
		RolePair:     "integration-pair",
		Description:  "Integration analysis for goal",
		Status:       models.TaskStatusMerged,
		Priority:     1,
		Created:      now,
		SpecRef:      "specs/goals/test.md",
		DoneWhen:     "Analysis approved",
		Scope:        "full branch",
		ReviewCommit: &reviewCommit,
		MergeCommit:  &mergeCommit,
		Output: []models.OutputEntry{
			{Desc: "Fix type alignment in auth", DoneWhen: "Types match across modules", Scope: "internal/auth", SpecRef: "specs/goals/test.md"},
			{Desc: "Fix error mapping in handler", DoneWhen: "All errors propagated", Scope: "internal/handler", SpecRef: "specs/goals/test.md"},
		},
		History: []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, task)
	state.Sprint.Scope.Planned = []string{parentID}
	testhelpers.WriteInitialState(t, stateFile, state)

	results, err := ExecuteAvailableTransitions(tmpDir, "auto")
	if err != nil {
		t.Fatalf("ExecuteAvailableTransitions(auto) error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("results count = %d, want 1", len(results))
	}
	if results[0].TransitionName != "integration-to-fix" {
		t.Errorf("TransitionName = %q, want %q", results[0].TransitionName, "integration-to-fix")
	}
	if len(results[0].ChildTaskIDs) != 2 {
		t.Fatalf("ChildTaskIDs count = %d, want 2", len(results[0].ChildTaskIDs))
	}

	// Verify children
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	for i, childID := range results[0].ChildTaskIDs {
		child := readState.FindTask(childID)
		if child == nil {
			t.Fatalf("Child task %q not found", childID)
		}
		if child.Status != "DRAFT_CODE" {
			t.Errorf("Child[%d] status = %q, want DRAFT_CODE", i, child.Status)
		}
		if child.RolePair != "coding-pair" {
			t.Errorf("Child[%d] role_pair = %q, want coding-pair", i, child.RolePair)
		}
		// Verify child is in sprint scope
		if !slices.Contains(readState.Sprint.Scope.Planned, childID) {
			t.Errorf("Child %q not in Sprint.Scope.Planned", childID)
		}
	}

	// Verify parent's TransitionsExecuted includes integration-to-fix
	parent := readState.FindTask(parentID)
	if parent == nil {
		t.Fatal("Parent task not found after transition")
	}
	if !parent.TransitionsExecuted["integration-to-fix"] {
		t.Errorf("Parent TransitionsExecuted missing 'integration-to-fix': %v", parent.TransitionsExecuted)
	}
}

func TestExecuteAvailableTransitions_ManualFilterIgnoresAuto(t *testing.T) {
	tmpDir, stateFile := setupIntegrationPipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusInProgress

	now := time.Now().UTC()

	// Integration task with auto transition available
	reviewCommit := "abc123"
	mergeCommit := "def456"
	integrationTask := models.Task{
		ID:           "integration-task-1",
		Type:         models.TaskTypeIntegration,
		RolePair:     "integration-pair",
		Description:  "Integration analysis",
		Status:       models.TaskStatusMerged,
		Priority:     1,
		Created:      now,
		SpecRef:      "specs/goals/test.md",
		DoneWhen:     "Analysis approved",
		Scope:        "full branch",
		ReviewCommit: &reviewCommit,
		MergeCommit:  &mergeCommit,
		Output: []models.OutputEntry{
			{Desc: "Fix type alignment", DoneWhen: "Types match", Scope: "internal/auth", SpecRef: "specs/goals/test.md"},
		},
		History: []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, integrationTask)
	state.Sprint.Scope.Planned = []string{"integration-task-1"}
	testhelpers.WriteInitialState(t, stateFile, state)

	results, err := ExecuteAvailableTransitions(tmpDir, "manual")
	if err != nil {
		t.Fatalf("ExecuteAvailableTransitions(manual) error: %v", err)
	}

	// Manual filter should produce no results since integration-to-fix is auto
	if len(results) != 0 {
		t.Errorf("results count = %d, want 0 (manual filter should ignore auto transitions)", len(results))
	}

	// Verify no children were created
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	parent := readState.FindTask("integration-task-1")
	if parent == nil {
		t.Fatal("Parent task not found")
	}
	if parent.TransitionsExecuted["integration-to-fix"] {
		t.Error("integration-to-fix should NOT be in TransitionsExecuted with manual filter")
	}
}

// setupPhase2GitProceedTest creates a test environment with both a real git repo
// (with integration branch) and the Phase 2 pipeline config. This is needed for
// tests that exercise goal.BaseCommit snapshotting, which calls git rev-parse.
func setupPhase2GitProceedTest(t *testing.T) (string, string) {
	t.Helper()
	tmpDir := t.TempDir()

	// Initialize a real git repo with an integration branch.
	testhelpers.SetupTestGitRepo(t, tmpDir)

	// Set up .liza dir (state.yaml, lock file).
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	// Copy the Phase 2 pipeline YAML (has code-plan-to-coding targeting coding-pair).
	src, err := os.ReadFile(filepath.Join(testhelpers.FindRepoRoot(t), "internal", "pipeline", "testdata", "valid-phase2-full.yaml"))
	if err != nil {
		t.Fatalf("Failed to read pipeline testdata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, ".liza", "pipeline.yaml"), src, 0o644); err != nil {
		t.Fatalf("Failed to write frozen pipeline config: %v", err)
	}

	return tmpDir, stateFile
}

func TestExecuteAvailableTransitions_SnapshotsGoalBaseCommit(t *testing.T) {
	tmpDir, stateFile := setupPhase2GitProceedTest(t)

	// Get actual integration branch HEAD SHA for assertion.
	expectedSHA := testhelpers.MustGit(t, tmpDir, "rev-parse", "integration")

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusInProgress
	// goal.BaseCommit is nil (default from CreateValidState).

	now := time.Now().UTC()
	parentID := "plan-task-1"
	reviewCommit := "abc123"
	mergeCommit := "def456"
	task := models.Task{
		ID:           parentID,
		Type:         models.TaskTypePlanning,
		RolePair:     "code-planning-pair",
		Description:  "Code planning task",
		Status:       models.TaskStatusMerged,
		Priority:     1,
		Created:      now,
		SpecRef:      "specs/goals/test.md",
		DoneWhen:     "Plan approved",
		Scope:        "internal/ops",
		ReviewCommit: &reviewCommit,
		MergeCommit:  &mergeCommit,
		Output: []models.OutputEntry{
			{Desc: "Implement feature A", DoneWhen: "Tests pass", Scope: "internal/ops", SpecRef: "specs/goals/test.md"},
		},
		History: []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, task)
	state.Sprint.Scope.Planned = []string{parentID}
	testhelpers.WriteInitialState(t, stateFile, state)

	results, err := ExecuteAvailableTransitions(tmpDir, "")
	if err != nil {
		t.Fatalf("ExecuteAvailableTransitions() error: %v", err)
	}

	// Verify transition executed and created coding-pair children.
	if len(results) != 1 {
		t.Fatalf("results count = %d, want 1", len(results))
	}
	if results[0].TransitionName != "code-plan-to-coding" {
		t.Errorf("TransitionName = %q, want %q", results[0].TransitionName, "code-plan-to-coding")
	}

	// Verify goal.BaseCommit was set to integration HEAD SHA.
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	if readState.Goal.BaseCommit == nil {
		t.Fatal("goal.BaseCommit should be set after first coding-pair children created")
	}
	if *readState.Goal.BaseCommit != expectedSHA {
		t.Errorf("goal.BaseCommit = %q, want %q (integration HEAD)", *readState.Goal.BaseCommit, expectedSHA)
	}
}

func TestExecuteAvailableTransitions_NoBaseCommitOverwrite(t *testing.T) {
	tmpDir, stateFile := setupPhase2GitProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusInProgress

	// Pre-set goal.BaseCommit to an existing value.
	existingSHA := "abcdef1234567890abcdef1234567890abcdef12"
	state.Goal.BaseCommit = &existingSHA

	now := time.Now().UTC()
	parentID := "plan-task-2"
	reviewCommit := "abc123"
	mergeCommit := "def456"
	task := models.Task{
		ID:           parentID,
		Type:         models.TaskTypePlanning,
		RolePair:     "code-planning-pair",
		Description:  "Another code planning task",
		Status:       models.TaskStatusMerged,
		Priority:     1,
		Created:      now,
		SpecRef:      "specs/goals/test.md",
		DoneWhen:     "Plan approved",
		Scope:        "internal/ops",
		ReviewCommit: &reviewCommit,
		MergeCommit:  &mergeCommit,
		Output: []models.OutputEntry{
			{Desc: "Implement feature B", DoneWhen: "Tests pass", Scope: "internal/ops", SpecRef: "specs/goals/test.md"},
		},
		History: []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, task)
	state.Sprint.Scope.Planned = []string{parentID}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := ExecuteAvailableTransitions(tmpDir, "")
	if err != nil {
		t.Fatalf("ExecuteAvailableTransitions() error: %v", err)
	}

	// Verify goal.BaseCommit was NOT overwritten.
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	if readState.Goal.BaseCommit == nil {
		t.Fatal("goal.BaseCommit should still be set")
	}
	if *readState.Goal.BaseCommit != existingSHA {
		t.Errorf("goal.BaseCommit = %q, want %q (should not be overwritten)", *readState.Goal.BaseCommit, existingSHA)
	}
}

// --- Many-to-one transition tests ---

// makeManyToOneCohort creates N sibling tasks sharing a parent_task with the same role_pair.
func makeManyToOneCohort(parentID, rolePair string, status models.TaskStatus, specRef string, n int) []models.Task {
	tasks := make([]models.Task, n)
	for i := range n {
		rc := "abc123"
		tasks[i] = models.Task{
			ID:           fmt.Sprintf("%s-us-%d", parentID, i),
			Type:         models.TaskTypeCoding,
			RolePair:     rolePair,
			Description:  fmt.Sprintf("User story %d", i),
			Status:       status,
			Priority:     1,
			ParentTasks:  []string{parentID},
			SpecRef:      specRef,
			DoneWhen:     "US approved",
			Scope:        "auth module",
			ReviewCommit: &rc,
			Created:      time.Now().UTC(),
			History:      []models.TaskHistoryEntry{},
		}
	}
	return tasks
}

func TestProceedManyToOne_HappyPath(t *testing.T) {
	tmpDir, stateFile := setupPhase2PipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted

	parentID := "epic-plan-1"
	cohort := makeManyToOneCohort(parentID, "us-writing-pair", models.TaskStatusMerged, "specs/goal.md", 3)
	for _, task := range cohort {
		state.Tasks = append(state.Tasks, task)
		state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, task.ID)
	}

	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Proceed(tmpDir, cohort[0].ID, "us-to-coding")
	if err != nil {
		t.Fatalf("Proceed() error: %v", err)
	}

	// Should create exactly one child
	if len(result.ChildTaskIDs) != 1 {
		t.Fatalf("ChildTaskIDs count = %d, want 1", len(result.ChildTaskIDs))
	}

	// Deterministic child ID: <parent>-<transition>
	expectedChildID := "epic-plan-1-us-to-coding"
	if result.ChildTaskIDs[0] != expectedChildID {
		t.Errorf("ChildTaskIDs[0] = %q, want %q", result.ChildTaskIDs[0], expectedChildID)
	}

	// CohortTaskIDs contains all 3 sibling IDs
	if len(result.CohortTaskIDs) != 3 {
		t.Fatalf("CohortTaskIDs count = %d, want 3", len(result.CohortTaskIDs))
	}

	// Verify persisted state
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	child := readState.FindTask(expectedChildID)
	if child == nil {
		t.Fatal("Child task not found")
	}

	// Child has ParentTasks containing all cohort member IDs
	if len(child.ParentTasks) != 3 {
		t.Fatalf("Child ParentTasks count = %d, want 3", len(child.ParentTasks))
	}
	for _, c := range cohort {
		if !slices.Contains(child.ParentTasks, c.ID) {
			t.Errorf("Child ParentTasks = %v, want to contain %q", child.ParentTasks, c.ID)
		}
	}

	// Child has correct status (DRAFT_ARCHITECTURE)
	if child.Status != models.TaskStatus("DRAFT_ARCHITECTURE") {
		t.Errorf("Child status = %v, want DRAFT_ARCHITECTURE", child.Status)
	}

	// Child has correct role_pair
	if child.RolePair != "architecture-pair" {
		t.Errorf("Child role_pair = %q, want %q", child.RolePair, "architecture-pair")
	}

	// Child has correct task type
	if child.Type != models.TaskTypeArchitecture {
		t.Errorf("Child type = %q, want %q", child.Type, models.TaskTypeArchitecture)
	}

	// Child inherits spec_ref
	if child.SpecRef != "specs/goal.md" {
		t.Errorf("Child spec_ref = %q, want %q", child.SpecRef, "specs/goal.md")
	}

	// Child description contains doer display name
	if !strings.Contains(child.Description, "Architect") {
		t.Errorf("Child desc = %q, want to contain 'Architect'", child.Description)
	}

	// transitions_executed set on ALL cohort members
	for _, c := range cohort {
		srcTask := readState.FindTask(c.ID)
		if !srcTask.TransitionsExecuted["us-to-coding"] {
			t.Errorf("Task %s: transitions_executed should contain us-to-coding", c.ID)
		}
	}
}

func TestProceedManyToOne_CohortIncomplete(t *testing.T) {
	tmpDir, stateFile := setupPhase2PipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted

	parentID := "epic-plan-1"
	// 2 MERGED, 1 still in progress
	cohort := makeManyToOneCohort(parentID, "us-writing-pair", models.TaskStatusMerged, "specs/goal.md", 3)
	cohort[2].Status = models.TaskStatus("WRITING_US")
	cohort[2].ReviewCommit = nil
	for _, task := range cohort {
		state.Tasks = append(state.Tasks, task)
		state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, task.ID)
	}

	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := Proceed(tmpDir, cohort[0].ID, "us-to-coding")
	if err == nil {
		t.Fatal("Proceed() should fail with incomplete cohort")
	}
	if !strings.Contains(err.Error(), "cohort incomplete") {
		t.Errorf("error = %q, want to contain 'cohort incomplete'", err.Error())
	}
}

func TestProceedManyToOne_MixedMergedAndApproved(t *testing.T) {
	tmpDir, stateFile := setupPhase2PipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted

	parentID := "epic-plan-1"
	// 2 MERGED, 1 US_APPROVED — all should be accepted
	cohort := makeManyToOneCohort(parentID, "us-writing-pair", models.TaskStatusMerged, "specs/goal.md", 3)
	cohort[1].Status = models.TaskStatus("US_APPROVED")
	for _, task := range cohort {
		state.Tasks = append(state.Tasks, task)
		state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, task.ID)
	}

	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Proceed(tmpDir, cohort[0].ID, "us-to-coding")
	if err != nil {
		t.Fatalf("Proceed() error: %v", err)
	}

	if len(result.ChildTaskIDs) != 1 {
		t.Fatalf("ChildTaskIDs count = %d, want 1", len(result.ChildTaskIDs))
	}
}

func TestProceedManyToOne_Idempotent(t *testing.T) {
	tmpDir, stateFile := setupPhase2PipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted

	parentID := "epic-plan-1"
	cohort := makeManyToOneCohort(parentID, "us-writing-pair", models.TaskStatusMerged, "specs/goal.md", 3)
	for _, task := range cohort {
		state.Tasks = append(state.Tasks, task)
		state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, task.ID)
	}

	testhelpers.WriteInitialState(t, stateFile, state)

	// First execution
	_, err := Proceed(tmpDir, cohort[0].ID, "us-to-coding")
	if err != nil {
		t.Fatalf("First Proceed() error: %v", err)
	}

	// Second execution — should return errTransitionAlreadyExecuted
	_, err = Proceed(tmpDir, cohort[0].ID, "us-to-coding")
	if err == nil {
		t.Fatal("Second Proceed() should return error")
	}
	if !strings.Contains(err.Error(), "transition already executed") {
		t.Errorf("error = %q, want to contain 'transition already executed'", err.Error())
	}
}

func TestProceedManyToOne_NoCohortParent(t *testing.T) {
	tmpDir, stateFile := setupPhase2PipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted

	// Task with no parent
	rc := "abc123"
	task := models.Task{
		ID:           "orphan-us",
		Type:         models.TaskTypeCoding,
		RolePair:     "us-writing-pair",
		Description:  "Orphan US",
		Status:       models.TaskStatusMerged,
		Priority:     1,
		SpecRef:      "specs/goal.md",
		DoneWhen:     "done",
		Scope:        "scope",
		ReviewCommit: &rc,
		Created:      time.Now().UTC(),
		History:      []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, task)
	state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, task.ID)

	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := Proceed(tmpDir, "orphan-us", "us-to-coding")
	if err == nil {
		t.Fatal("Proceed() should fail for task with no parent")
	}
	if !strings.Contains(err.Error(), "no parent_task") {
		t.Errorf("error = %q, want to contain 'no parent_task'", err.Error())
	}
}

func TestProceedManyToOne_SpecRefInheritance(t *testing.T) {
	tmpDir, stateFile := setupPhase2PipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted

	parentID := "epic-plan-1"
	specRef := "specs/goals/20260405-architecture-step.md"
	cohort := makeManyToOneCohort(parentID, "us-writing-pair", models.TaskStatusMerged, specRef, 2)
	for _, task := range cohort {
		state.Tasks = append(state.Tasks, task)
		state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, task.ID)
	}

	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Proceed(tmpDir, cohort[0].ID, "us-to-coding")
	if err != nil {
		t.Fatalf("Proceed() error: %v", err)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	child := readState.FindTask(result.ChildTaskIDs[0])
	if child == nil {
		t.Fatal("Child task not found")
	}
	if child.SpecRef != specRef {
		t.Errorf("Child spec_ref = %q, want %q", child.SpecRef, specRef)
	}
}

func TestComputeInheritedDeps_ManyToOne(t *testing.T) {
	tmpDir, _ := setupPhase2PipelineProceedTest(t)

	resolver, _, err := loadResolver(tmpDir)
	if err != nil {
		t.Fatalf("loadResolver: %v", err)
	}

	parentID := "epic-plan-1"
	childID := manyToOneChildID(parentID, "us-to-coding")

	// Create state with cohort members that have executed the transition
	// and the resulting child task
	state := &models.State{
		Tasks: []models.Task{
			{
				ID:          "us-1",
				RolePair:    "us-writing-pair",
				ParentTasks: []string{parentID},
				TransitionsExecuted: map[string]bool{
					"us-to-coding": true,
				},
			},
			{
				ID:          "us-2",
				RolePair:    "us-writing-pair",
				ParentTasks: []string{parentID},
				TransitionsExecuted: map[string]bool{
					"us-to-coding": true,
				},
			},
			{
				ID:       childID,
				RolePair: "architecture-pair",
			},
			// Downstream task that depends on both us-1 and us-2
			{
				ID:        "downstream-task",
				DependsOn: []string{"us-1", "us-2"},
			},
		},
	}

	downstreamTask := state.FindTask("downstream-task")
	inherited, err := computeInheritedDeps(state, downstreamTask, "us-to-coding", resolver)
	if err != nil {
		t.Fatalf("computeInheritedDeps: %v", err)
	}

	// Both us-1 and us-2 are in the same cohort, so they produce the same child ID
	// Dedup should result in only one inherited dep
	if len(inherited) != 1 {
		t.Fatalf("inherited deps count = %d, want 1 (dedup same cohort child)", len(inherited))
	}
	if inherited[0] != childID {
		t.Errorf("inherited[0] = %q, want %q", inherited[0], childID)
	}
}

func TestComputeInheritedDeps_ManyToOne_IntraCohortSkipsSelfDep(t *testing.T) {
	tmpDir, _ := setupPhase2PipelineProceedTest(t)

	resolver, _, err := loadResolver(tmpDir)
	if err != nil {
		t.Fatalf("loadResolver: %v", err)
	}

	parentID := "epic-plan-1"
	childID := manyToOneChildID(parentID, "us-to-coding")

	// Cohort members us-1 and us-2 share the same parent. us-2 depends on us-1.
	// When us-1 has already executed the transition, computeInheritedDeps for us-2
	// must NOT produce the many-to-one child as an inherited dep — that child IS
	// the task being created, which would be a circular self-dependency.
	state := &models.State{
		Tasks: []models.Task{
			{
				ID:          "us-1",
				RolePair:    "us-writing-pair",
				ParentTasks: []string{parentID},
				TransitionsExecuted: map[string]bool{
					"us-to-coding": true,
				},
			},
			{
				ID:          "us-2",
				RolePair:    "us-writing-pair",
				ParentTasks: []string{parentID},
				DependsOn:   []string{"us-1"},
			},
			{
				ID:       childID,
				RolePair: "architecture-pair",
			},
		},
	}

	triggerTask := state.FindTask("us-2")
	inherited, err := computeInheritedDeps(state, triggerTask, "us-to-coding", resolver)
	if err != nil {
		t.Fatalf("computeInheritedDeps: %v", err)
	}

	if len(inherited) != 0 {
		t.Errorf("inherited deps = %v, want empty (intra-cohort dep should be skipped to avoid self-reference)", inherited)
	}
}

func TestProceedManyToOne_CrashRecovery_MissingChild(t *testing.T) {
	tmpDir, stateFile := setupPhase2PipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted

	parentID := "epic-plan-1"
	cohort := makeManyToOneCohort(parentID, "us-writing-pair", models.TaskStatusMerged, "specs/goal.md", 3)

	// Simulate crash: set transitions_executed on first 2 members only, no child created
	cohort[0].TransitionsExecuted = map[string]bool{"us-to-coding": true}
	cohort[1].TransitionsExecuted = map[string]bool{"us-to-coding": true}
	// cohort[2] has NO transitions_executed (partial crash)

	for _, task := range cohort {
		state.Tasks = append(state.Tasks, task)
		state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, task.ID)
	}

	testhelpers.WriteInitialState(t, stateFile, state)

	// Proceed should detect partial transitions_executed and create child (crash recovery)
	result, err := Proceed(tmpDir, cohort[0].ID, "us-to-coding")
	if err != nil {
		t.Fatalf("Proceed() error: %v", err)
	}

	// Should create exactly one child
	if len(result.ChildTaskIDs) != 1 {
		t.Fatalf("ChildTaskIDs count = %d, want 1", len(result.ChildTaskIDs))
	}

	expectedChildID := "epic-plan-1-us-to-coding"
	if result.ChildTaskIDs[0] != expectedChildID {
		t.Errorf("ChildTaskIDs[0] = %q, want %q", result.ChildTaskIDs[0], expectedChildID)
	}

	// Verify persisted state
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	child := readState.FindTask(expectedChildID)
	if child == nil {
		t.Fatal("Child task not found after crash recovery")
	}

	// Child has ParentTasks containing all cohort member IDs
	if len(child.ParentTasks) != 3 {
		t.Fatalf("Child ParentTasks count = %d, want 3", len(child.ParentTasks))
	}
	for _, c := range cohort {
		if !slices.Contains(child.ParentTasks, c.ID) {
			t.Errorf("Child ParentTasks = %v, want to contain %q", child.ParentTasks, c.ID)
		}
	}

	// transitions_executed repaired on ALL cohort members (including member 2)
	for _, c := range cohort {
		srcTask := readState.FindTask(c.ID)
		if !srcTask.TransitionsExecuted["us-to-coding"] {
			t.Errorf("Task %s: transitions_executed should contain us-to-coding (crash recovery repair)", c.ID)
		}
	}
}

func TestProceedManyToOne_CrashRecovery_ChildExists(t *testing.T) {
	tmpDir, _ := setupPhase2PipelineProceedTest(t)

	resolver, _, err := loadResolver(tmpDir)
	if err != nil {
		t.Fatalf("loadResolver: %v", err)
	}
	tDef, err := buildTransitionDefFromPipeline(resolver, "us-to-coding")
	if err != nil {
		t.Fatalf("buildTransitionDefFromPipeline: %v", err)
	}

	parentID := "epic-plan-1"
	cohort := makeManyToOneCohort(parentID, "us-writing-pair", models.TaskStatusMerged, "specs/goal.md", 3)

	// Simulate crash: transitions_executed on first 2 members, child exists
	cohort[0].TransitionsExecuted = map[string]bool{"us-to-coding": true}
	cohort[1].TransitionsExecuted = map[string]bool{"us-to-coding": true}
	// cohort[2] missing transitions_executed (partial crash, but child was created)

	childID := "epic-plan-1-us-to-coding"
	state := &models.State{Tasks: append(cohort, models.Task{
		ID:          childID,
		Type:        models.TaskTypeArchitecture,
		RolePair:    "architecture-pair",
		Description: "Existing child",
		Status:      models.TaskStatus("DRAFT_ARCHITECTURE"),
		ParentTasks: []string{cohort[0].ID, cohort[1].ID, cohort[2].ID},
		Created:     time.Now().UTC(),
		History:     []models.TaskHistoryEntry{},
	})}

	now := time.Now().UTC()
	result := &ProceedResult{}
	err = proceedInner(state, cohort[0].ID, "us-to-coding", tDef, nil, now, result)

	// Should return errTransitionAlreadyExecuted
	if err == nil {
		t.Fatal("proceedInner should return error when child exists")
	}
	if !strings.Contains(err.Error(), "transition already executed") {
		t.Errorf("error = %q, want to contain 'transition already executed'", err.Error())
	}

	// Verify transitions_executed repaired on ALL members (in-memory)
	for i := range cohort {
		member := state.FindTask(cohort[i].ID)
		if !member.TransitionsExecuted["us-to-coding"] {
			t.Errorf("Task %s: transitions_executed should be repaired", cohort[i].ID)
		}
	}
}

func TestIsTransitionIncomplete_ManyToOne(t *testing.T) {
	tmpDir, _ := setupPhase2PipelineProceedTest(t)

	resolver, _, err := loadResolver(tmpDir)
	if err != nil {
		t.Fatalf("loadResolver: %v", err)
	}

	parentID := "epic-plan-1"
	childID := manyToOneChildID(parentID, "us-to-coding")

	t.Run("child missing returns true", func(t *testing.T) {
		state := &models.State{
			Tasks: []models.Task{
				{
					ID:          "us-1",
					RolePair:    "us-writing-pair",
					ParentTasks: []string{parentID},
					TransitionsExecuted: map[string]bool{
						"us-to-coding": true,
					},
				},
			},
		}
		task := state.FindTask("us-1")
		if !isTransitionIncomplete(state, task, "us-to-coding", resolver) {
			t.Error("isTransitionIncomplete = false, want true (child missing)")
		}
	})

	t.Run("child present returns false", func(t *testing.T) {
		state := &models.State{
			Tasks: []models.Task{
				{
					ID:          "us-1",
					RolePair:    "us-writing-pair",
					ParentTasks: []string{parentID},
					TransitionsExecuted: map[string]bool{
						"us-to-coding": true,
					},
				},
				{
					ID:       childID,
					RolePair: "architecture-pair",
				},
			},
		}
		task := state.FindTask("us-1")
		if isTransitionIncomplete(state, task, "us-to-coding", resolver) {
			t.Error("isTransitionIncomplete = true, want false (child present)")
		}
	})
}

func TestExecuteAvailableTransitions_SkipsReplannedTasks(t *testing.T) {
	tmpDir, stateFile := setupPhase2PipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusInProgress

	now := time.Now().UTC()
	reviewCommit := "abc123"
	mergeCommit := "def456"

	// Original US task: MERGED but marked as replanned.
	// Without the defensive check, EAT would fire us-to-coding on this task.
	originalUS := models.Task{
		ID:           "us-task-1",
		Type:         models.TaskTypeCoding,
		RolePair:     "us-writing-pair",
		Description:  "Original US (replanned)",
		Status:       models.TaskStatusMerged,
		Priority:     1,
		Created:      now,
		ParentTasks:  []string{"epic-plan-1"},
		SpecRef:      "specs/auth.md",
		DoneWhen:     "US approved",
		Scope:        "auth module",
		ReviewCommit: &reviewCommit,
		MergeCommit:  &mergeCommit,
		TransitionsExecuted: map[string]bool{
			"replanned":    true,
			"us-to-coding": true, // preventive layer from replan.go
		},
		History: []models.TaskHistoryEntry{},
	}

	// Replan US task: MERGED without replanned marker — should fire normally.
	replanUS := models.Task{
		ID:           "us-task-2",
		Type:         models.TaskTypeCoding,
		RolePair:     "us-writing-pair",
		Description:  "Replan US",
		Status:       models.TaskStatusMerged,
		Priority:     1,
		Created:      now,
		ParentTasks:  []string{"epic-plan-1-replan-1"},
		SpecRef:      "specs/auth.md",
		DoneWhen:     "US approved",
		Scope:        "auth module",
		ReviewCommit: &reviewCommit,
		MergeCommit:  &mergeCommit,
		History:      []models.TaskHistoryEntry{},
	}

	state.Tasks = append(state.Tasks, originalUS, replanUS)
	state.Sprint.Scope.Planned = []string{"us-task-1", "us-task-2"}
	testhelpers.WriteInitialState(t, stateFile, state)

	results, err := ExecuteAvailableTransitions(tmpDir, "")
	if err != nil {
		t.Fatalf("ExecuteAvailableTransitions() error: %v", err)
	}

	// Only the replan US should produce a child — the original is skipped.
	if len(results) != 1 {
		t.Fatalf("results count = %d, want 1 (only replan US should fire)", len(results))
	}
	if results[0].SourceTaskID != "us-task-2" {
		t.Errorf("SourceTaskID = %q, want us-task-2", results[0].SourceTaskID)
	}

	// Verify only one architecture child exists.
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	var archTasks []string
	for _, task := range readState.Tasks {
		if task.RolePair == "architecture-pair" {
			archTasks = append(archTasks, task.ID)
		}
	}
	if len(archTasks) != 1 {
		t.Errorf("Expected 1 architecture task, got %d: %v", len(archTasks), archTasks)
	}
}

func TestExecuteAvailableTransitions_DefensiveSkipsReplannedEvenWithoutTransitionBlock(t *testing.T) {
	// Tests the defensive layer alone: a replanned task where the preventive
	// layer (blocking real transition names) was somehow missed. The "replanned"
	// marker in TransitionsExecuted should still prevent EAT from firing.
	tmpDir, stateFile := setupPhase2PipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusInProgress

	now := time.Now().UTC()
	reviewCommit := "abc123"
	mergeCommit := "def456"

	// Only "replanned" marker, no "us-to-coding" block.
	task := models.Task{
		ID:           "us-task-1",
		Type:         models.TaskTypeCoding,
		RolePair:     "us-writing-pair",
		Description:  "Replanned US (defensive test)",
		Status:       models.TaskStatusMerged,
		Priority:     1,
		Created:      now,
		ParentTasks:  []string{"epic-plan-1"},
		SpecRef:      "specs/auth.md",
		DoneWhen:     "US approved",
		Scope:        "auth module",
		ReviewCommit: &reviewCommit,
		MergeCommit:  &mergeCommit,
		TransitionsExecuted: map[string]bool{
			"replanned": true,
		},
		History: []models.TaskHistoryEntry{},
	}

	state.Tasks = append(state.Tasks, task)
	state.Sprint.Scope.Planned = []string{"us-task-1"}
	testhelpers.WriteInitialState(t, stateFile, state)

	results, err := ExecuteAvailableTransitions(tmpDir, "")
	if err != nil {
		t.Fatalf("ExecuteAvailableTransitions() error: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("results count = %d, want 0 (replanned task should be skipped)", len(results))
	}
}

// --- Task-slug tests ---

func setupTaskSlugProceedTest(t *testing.T) (string, string) {
	t.Helper()
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	src, err := os.ReadFile(filepath.Join(testhelpers.FindRepoRoot(t), "internal", "pipeline", "testdata", "valid-with-task-slugs.yaml"))
	if err != nil {
		t.Fatalf("Failed to read pipeline testdata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, ".liza", "pipeline.yaml"), src, 0o644); err != nil {
		t.Fatalf("Failed to write frozen pipeline config: %v", err)
	}
	return tmpDir, stateFile
}

func TestProceed_TaskSlug_ChildIDsUseSlug(t *testing.T) {
	tmpDir, stateFile := setupTaskSlugProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 3
	state.Sprint.Status = models.SprintStatusCompleted

	now := time.Now().UTC()
	parentID := "plan-task-1"
	reviewCommit := "abc123"
	task := models.Task{
		ID:           parentID,
		Type:         models.TaskTypeCoding,
		RolePair:     "code-planning-pair",
		Description:  "Plan the auth module",
		Status:       models.TaskStatus("CODING_PLAN_APPROVED"),
		Priority:     1,
		Created:      now,
		SpecRef:      "README.md",
		DoneWhen:     "Plan approved",
		Scope:        "auth module",
		ReviewCommit: &reviewCommit,
		Output: []models.OutputEntry{
			{Desc: "Implement login", DoneWhen: "POST /login works", Scope: "auth", SpecRef: "specs/auth.md#login"},
			{Desc: "Implement refresh", DoneWhen: "POST /refresh works", Scope: "auth", SpecRef: "specs/auth.md#refresh"},
		},
	}
	state.Tasks = append(state.Tasks, task)
	state.Sprint.Scope.Planned = []string{parentID}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Proceed(tmpDir, parentID, "code-plan-to-coding")
	if err != nil {
		t.Fatalf("Proceed() error: %v", err)
	}

	// Child IDs should use task-slug "coding", not transition name "code-plan-to-coding"
	expectedID0 := "plan-task-1-coding-0"
	expectedID1 := "plan-task-1-coding-1"
	if result.ChildTaskIDs[0] != expectedID0 {
		t.Errorf("ChildTaskIDs[0] = %q, want %q (task-slug should be used)", result.ChildTaskIDs[0], expectedID0)
	}
	if result.ChildTaskIDs[1] != expectedID1 {
		t.Errorf("ChildTaskIDs[1] = %q, want %q (task-slug should be used)", result.ChildTaskIDs[1], expectedID1)
	}
}

func TestProceed_TaskSlug_CrashRecoveryUsesSlug(t *testing.T) {
	tmpDir, stateFile := setupTaskSlugProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 3
	state.Sprint.Status = models.SprintStatusCompleted

	now := time.Now().UTC()
	parentID := "plan-task-1"
	reviewCommit := "abc123"

	// Parent task with transition already executed but child missing (crash scenario)
	task := models.Task{
		ID:           parentID,
		Type:         models.TaskTypeCoding,
		RolePair:     "code-planning-pair",
		Description:  "Plan the auth module",
		Status:       models.TaskStatus("CODING_PLAN_APPROVED"),
		Priority:     1,
		Created:      now,
		SpecRef:      "README.md",
		DoneWhen:     "Plan approved",
		Scope:        "auth module",
		ReviewCommit: &reviewCommit,
		Output: []models.OutputEntry{
			{Desc: "Implement login", DoneWhen: "POST /login works", Scope: "auth", SpecRef: "specs/auth.md#login"},
			{Desc: "Implement refresh", DoneWhen: "POST /refresh works", Scope: "auth", SpecRef: "specs/auth.md#refresh"},
		},
		TransitionsExecuted: map[string]bool{"code-plan-to-coding": true},
	}

	// Only child 0 exists (child 1 missing — simulates crash)
	child0 := models.Task{
		ID:          "plan-task-1-coding-0",
		Type:        models.TaskTypeCoding,
		RolePair:    "coding-pair",
		Description: "Implement login",
		Status:      models.TaskStatus("DRAFT_CODE"),
		Priority:    1,
		Created:     now,
		SpecRef:     "specs/auth.md#login",
		DoneWhen:    "POST /login works",
		Scope:       "auth",
		ParentTask:  &parentID,
	}

	state.Tasks = append(state.Tasks, task, child0)
	state.Sprint.Scope.Planned = []string{parentID, child0.ID}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Proceed(tmpDir, parentID, "code-plan-to-coding")
	if err != nil {
		t.Fatalf("Proceed() error: %v", err)
	}

	// Crash recovery should create the missing child using task-slug
	if len(result.ChildTaskIDs) != 1 {
		t.Fatalf("ChildTaskIDs count = %d, want 1 (only missing child)", len(result.ChildTaskIDs))
	}
	expectedRecovered := "plan-task-1-coding-1"
	if result.ChildTaskIDs[0] != expectedRecovered {
		t.Errorf("recovered child = %q, want %q", result.ChildTaskIDs[0], expectedRecovered)
	}
}

// --- Kind-based dedup (per-subtask) ---

// makeKindDedupTriggerTask builds a trigger task in CODING_PLAN_APPROVED state
// suitable for firing the per-subtask code-plan-to-coding transition.
func makeKindDedupTriggerTask(id string, output []models.OutputEntry, now time.Time) models.Task {
	reviewCommit := "trigger-rev"
	return models.Task{
		ID:           id,
		Type:         models.TaskTypeCoding,
		RolePair:     "code-planning-pair",
		Description:  "Kind-dedup trigger",
		Status:       models.TaskStatus("CODING_PLAN_APPROVED"),
		Priority:     1,
		Created:      now,
		SpecRef:      "README.md",
		DoneWhen:     "Plan approved",
		Scope:        "dedup scope",
		ReviewCommit: &reviewCommit,
		Output:       output,
		History:      []models.TaskHistoryEntry{},
	}
}

// findTransitionExecutedEvent returns the last TransitionExecuted history entry
// for the given task, or nil if none exist.
func findTransitionExecutedEvent(task *models.Task) *models.TaskHistoryEntry {
	for i := len(task.History) - 1; i >= 0; i-- {
		if task.History[i].Event == models.TaskEventTransitionExecuted {
			return &task.History[i]
		}
	}
	return nil
}

// extractSkippedEntries normalizes the skipped_entries extra into a canonical
// []map[string]any. After YAML round-trip the slice is []any of map[string]any.
func extractSkippedEntries(extra map[string]any) []map[string]any {
	v, ok := extra["skipped_entries"]
	if !ok {
		return nil
	}
	switch typed := v.(type) {
	case []map[string]any:
		return typed
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}

// skippedIndex returns the output_index field as int, normalizing YAML round-trip.
func skippedIndex(entry map[string]any) int {
	switch v := entry["output_index"].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return -1
}

func TestProceed_PerSubtask_KindDedup_Hit(t *testing.T) {
	tmpDir, stateFile := setupPipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted

	now := time.Now().UTC()
	incumbentDeps := []string{"some-prior-dep"}
	incumbent := models.Task{
		ID:          "bootstrap-incumbent",
		Type:        models.TaskTypeCoding,
		RolePair:    "coding-pair",
		Description: "Existing bootstrap",
		Status:      models.TaskStatusReady, // DRAFT_CODE (non-terminal)
		Priority:    1,
		Created:     now,
		SpecRef:     "README.md",
		DoneWhen:    "bootstrap done",
		Scope:       "bootstrap scope",
		Kind:        "bootstrap-precommit",
		DependsOn:   slices.Clone(incumbentDeps),
		History:     []models.TaskHistoryEntry{},
	}

	triggerID := "plan-hit"
	trigger := makeKindDedupTriggerTask(triggerID, []models.OutputEntry{
		{Desc: "Bootstrap child", DoneWhen: "boot", Scope: "boot", SpecRef: "README.md", Kind: "bootstrap-precommit"},
		{Desc: "Follow-up", DoneWhen: "follow", Scope: "follow", SpecRef: "README.md", DependsOn: []string{"0"}},
	}, now)

	state.Tasks = append(state.Tasks, incumbent, trigger)
	state.Sprint.Scope.Planned = []string{triggerID}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Proceed(tmpDir, triggerID, "code-plan-to-coding")
	if err != nil {
		t.Fatalf("Proceed() error: %v", err)
	}

	if len(result.ChildTaskIDs) != 1 {
		t.Fatalf("ChildTaskIDs count = %d, want 1 (entry 0 dedup'd)", len(result.ChildTaskIDs))
	}
	expectedEntry1ID := "plan-hit-code-plan-to-coding-1"
	if result.ChildTaskIDs[0] != expectedEntry1ID {
		t.Errorf("ChildTaskIDs[0] = %q, want %q", result.ChildTaskIDs[0], expectedEntry1ID)
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("Read state: %v", err)
	}

	if readState.FindTask("plan-hit-code-plan-to-coding-0") != nil {
		t.Error("entry 0 child should NOT exist (dedup'd to incumbent)")
	}

	entry1Child := readState.FindTask(expectedEntry1ID)
	if entry1Child == nil {
		t.Fatal("entry 1 child not found")
	}
	if !slices.Contains(entry1Child.DependsOn, "bootstrap-incumbent") {
		t.Errorf("entry 1 DependsOn = %v, want to contain incumbent ID %q", entry1Child.DependsOn, "bootstrap-incumbent")
	}

	// Incumbent must not be mutated.
	gotIncumbent := readState.FindTask("bootstrap-incumbent")
	if gotIncumbent == nil {
		t.Fatal("incumbent task disappeared")
	}
	if !slices.Equal(gotIncumbent.DependsOn, incumbentDeps) {
		t.Errorf("incumbent DependsOn mutated: got %v, want %v", gotIncumbent.DependsOn, incumbentDeps)
	}
	if gotIncumbent.Status != models.TaskStatusReady {
		t.Errorf("incumbent status changed: got %v, want DRAFT_CODE", gotIncumbent.Status)
	}

	trig := readState.FindTask(triggerID)
	if !trig.TransitionsExecuted["code-plan-to-coding"] {
		t.Error("trigger transitions_executed[code-plan-to-coding] must be true")
	}
	ev := findTransitionExecutedEvent(trig)
	if ev == nil {
		t.Fatal("no TransitionExecuted history event found")
	}
	if children, _ := ev.Extra["children"].(int); children != 1 {
		t.Errorf("history children = %v, want 1", ev.Extra["children"])
	}
	skipped := extractSkippedEntries(ev.Extra)
	if len(skipped) != 1 {
		t.Fatalf("skipped_entries count = %d, want 1", len(skipped))
	}
	if got := skippedIndex(skipped[0]); got != 0 {
		t.Errorf("skipped[0].output_index = %d, want 0", got)
	}
	if kind, _ := skipped[0]["kind"].(string); kind != "bootstrap-precommit" {
		t.Errorf("skipped[0].kind = %q, want bootstrap-precommit", skipped[0]["kind"])
	}
	if rm, _ := skipped[0]["remapped_to"].(string); rm != "bootstrap-incumbent" {
		t.Errorf("skipped[0].remapped_to = %q, want bootstrap-incumbent", skipped[0]["remapped_to"])
	}
	if reason, _ := skipped[0]["reason"].(string); reason == "" {
		t.Error("skipped[0].reason should be non-empty")
	}
}

func TestProceed_PerSubtask_KindDedup_Miss(t *testing.T) {
	tmpDir, stateFile := setupPipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted

	now := time.Now().UTC()
	// Three terminal-status bootstraps — all ignored by collectNonTerminalByKind.
	terminals := []models.TaskStatus{models.TaskStatusMerged, models.TaskStatusAbandoned, models.TaskStatusSuperseded}
	for i, st := range terminals {
		state.Tasks = append(state.Tasks, models.Task{
			ID:          fmt.Sprintf("boot-terminal-%d", i),
			Type:        models.TaskTypeCoding,
			RolePair:    "coding-pair",
			Description: "terminal bootstrap",
			Status:      st,
			Priority:    1,
			Created:     now,
			SpecRef:     "README.md",
			DoneWhen:    "done",
			Scope:       "scope",
			Kind:        "bootstrap-precommit",
			History:     []models.TaskHistoryEntry{},
		})
	}

	triggerID := "plan-miss"
	trigger := makeKindDedupTriggerTask(triggerID, []models.OutputEntry{
		{Desc: "Boot", DoneWhen: "b", Scope: "s", SpecRef: "README.md", Kind: "bootstrap-precommit"},
		{Desc: "Other", DoneWhen: "o", Scope: "s", SpecRef: "README.md"},
	}, now)
	state.Tasks = append(state.Tasks, trigger)
	state.Sprint.Scope.Planned = []string{triggerID}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Proceed(tmpDir, triggerID, "code-plan-to-coding")
	if err != nil {
		t.Fatalf("Proceed() error: %v", err)
	}
	if len(result.ChildTaskIDs) != 2 {
		t.Fatalf("ChildTaskIDs count = %d, want 2 (no dedup)", len(result.ChildTaskIDs))
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("Read state: %v", err)
	}
	entry0 := readState.FindTask("plan-miss-code-plan-to-coding-0")
	if entry0 == nil {
		t.Fatal("entry 0 child missing")
	}
	if entry0.Kind != "bootstrap-precommit" {
		t.Errorf("entry 0 Kind = %q, want bootstrap-precommit", entry0.Kind)
	}

	trig := readState.FindTask(triggerID)
	ev := findTransitionExecutedEvent(trig)
	if ev == nil {
		t.Fatal("no TransitionExecuted event")
	}
	if _, ok := ev.Extra["skipped_entries"]; ok {
		t.Errorf("skipped_entries key should be ABSENT when no skips, got %v", ev.Extra["skipped_entries"])
	}
	if children, _ := ev.Extra["children"].(int); children != 2 {
		t.Errorf("history children = %v, want 2", ev.Extra["children"])
	}
}

func TestProceed_PerSubtask_KindDedup_BlockedInFlight(t *testing.T) {
	tmpDir, stateFile := setupPipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted

	now := time.Now().UTC()
	blockedReason := "awaiting external dep"
	blockedDeps := []string{"external-dep"}
	blocked := models.Task{
		ID:            "boot-blocked",
		Type:          models.TaskTypeCoding,
		RolePair:      "coding-pair",
		Description:   "blocked bootstrap",
		Status:        models.TaskStatusBlocked,
		Priority:      1,
		Created:       now,
		SpecRef:       "README.md",
		DoneWhen:      "done",
		Scope:         "scope",
		Kind:          "bootstrap-precommit",
		BlockedReason: &blockedReason,
		DependsOn:     slices.Clone(blockedDeps),
		History:       []models.TaskHistoryEntry{},
	}

	triggerID := "plan-blocked"
	trigger := makeKindDedupTriggerTask(triggerID, []models.OutputEntry{
		{Desc: "Boot", DoneWhen: "b", Scope: "s", SpecRef: "README.md", Kind: "bootstrap-precommit"},
		{Desc: "After boot", DoneWhen: "a", Scope: "s", SpecRef: "README.md", DependsOn: []string{"0"}},
	}, now)
	state.Tasks = append(state.Tasks, blocked, trigger)
	state.Sprint.Scope.Planned = []string{triggerID}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Proceed(tmpDir, triggerID, "code-plan-to-coding")
	if err != nil {
		t.Fatalf("Proceed() error: %v", err)
	}
	if len(result.ChildTaskIDs) != 1 {
		t.Fatalf("ChildTaskIDs count = %d, want 1 (BLOCKED is non-terminal → in-flight)", len(result.ChildTaskIDs))
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("Read state: %v", err)
	}
	follower := readState.FindTask("plan-blocked-code-plan-to-coding-1")
	if follower == nil {
		t.Fatal("follower child missing")
	}
	if !slices.Contains(follower.DependsOn, "boot-blocked") {
		t.Errorf("follower DependsOn = %v, want to contain boot-blocked", follower.DependsOn)
	}

	got := readState.FindTask("boot-blocked")
	if got.Status != models.TaskStatusBlocked {
		t.Errorf("blocked task status changed: got %v", got.Status)
	}
	if !slices.Equal(got.DependsOn, blockedDeps) {
		t.Errorf("blocked task DependsOn mutated: got %v, want %v", got.DependsOn, blockedDeps)
	}

	// BLOCKED is neither MERGED nor SUPERSEDED, so downstream cannot be
	// claimed until the blocker resolves. Express the invariant directly:
	// the follower's DependsOn includes a non-satisfying dep.
	if got.Status == models.TaskStatusMerged || got.Status == models.TaskStatusSuperseded {
		t.Errorf("invariant broken: BLOCKED should not count as dependency-satisfying")
	}
}

func TestProceed_PerSubtask_KindDedup_SupersededUnblocks(t *testing.T) {
	t.Run("only_superseded", func(t *testing.T) {
		tmpDir, stateFile := setupPipelineProceedTest(t)
		state := testhelpers.CreateValidState()
		state.PipelineVersion = 2
		state.Sprint.Status = models.SprintStatusCompleted
		now := time.Now().UTC()

		oldBoot := models.Task{
			ID:          "boot-old",
			Type:        models.TaskTypeCoding,
			RolePair:    "coding-pair",
			Description: "old bootstrap",
			Status:      models.TaskStatusSuperseded,
			Priority:    1,
			Created:     now,
			SpecRef:     "README.md",
			DoneWhen:    "done",
			Scope:       "scope",
			Kind:        "bootstrap-precommit",
			History:     []models.TaskHistoryEntry{},
		}
		triggerID := "plan-supersede"
		trigger := makeKindDedupTriggerTask(triggerID, []models.OutputEntry{
			{Desc: "Fresh boot", DoneWhen: "b", Scope: "s", SpecRef: "README.md", Kind: "bootstrap-precommit"},
		}, now)
		state.Tasks = append(state.Tasks, oldBoot, trigger)
		state.Sprint.Scope.Planned = []string{triggerID}
		testhelpers.WriteInitialState(t, stateFile, state)

		result, err := Proceed(tmpDir, triggerID, "code-plan-to-coding")
		if err != nil {
			t.Fatalf("Proceed() error: %v", err)
		}
		if len(result.ChildTaskIDs) != 1 {
			t.Fatalf("ChildTaskIDs count = %d, want 1 (SUPERSEDED is terminal)", len(result.ChildTaskIDs))
		}
		readState, err := db.New(stateFile).Read()
		if err != nil {
			t.Fatalf("Read state: %v", err)
		}
		fresh := readState.FindTask("plan-supersede-code-plan-to-coding-0")
		if fresh == nil {
			t.Fatal("fresh bootstrap child missing")
		}
		if fresh.Kind != "bootstrap-precommit" {
			t.Errorf("fresh Kind = %q, want bootstrap-precommit", fresh.Kind)
		}
		trig := readState.FindTask(triggerID)
		ev := findTransitionExecutedEvent(trig)
		if _, ok := ev.Extra["skipped_entries"]; ok {
			t.Errorf("skipped_entries should be absent when SUPERSEDED is the only incumbent")
		}
	})

	t.Run("two_superseded_plus_one_live", func(t *testing.T) {
		tmpDir, stateFile := setupPipelineProceedTest(t)
		state := testhelpers.CreateValidState()
		state.PipelineVersion = 2
		state.Sprint.Status = models.SprintStatusCompleted
		now := time.Now().UTC()

		var tasks []models.Task
		for i := 0; i < 2; i++ {
			tasks = append(tasks, models.Task{
				ID:          fmt.Sprintf("boot-old-%d", i),
				Type:        models.TaskTypeCoding,
				RolePair:    "coding-pair",
				Description: "old bootstrap",
				Status:      models.TaskStatusSuperseded,
				Priority:    1,
				Created:     now,
				SpecRef:     "README.md",
				DoneWhen:    "done",
				Scope:       "scope",
				Kind:        "bootstrap-precommit",
				History:     []models.TaskHistoryEntry{},
			})
		}
		live := models.Task{
			ID:          "boot-live",
			Type:        models.TaskTypeCoding,
			RolePair:    "coding-pair",
			Description: "live bootstrap",
			Status:      models.TaskStatusReady,
			Priority:    1,
			Created:     now,
			SpecRef:     "README.md",
			DoneWhen:    "done",
			Scope:       "scope",
			Kind:        "bootstrap-precommit",
			History:     []models.TaskHistoryEntry{},
		}
		tasks = append(tasks, live)

		triggerID := "plan-mixed"
		trigger := makeKindDedupTriggerTask(triggerID, []models.OutputEntry{
			{Desc: "Boot", DoneWhen: "b", Scope: "s", SpecRef: "README.md", Kind: "bootstrap-precommit"},
		}, now)
		tasks = append(tasks, trigger)
		state.Tasks = append(state.Tasks, tasks...)
		state.Sprint.Scope.Planned = []string{triggerID}
		testhelpers.WriteInitialState(t, stateFile, state)

		result, err := Proceed(tmpDir, triggerID, "code-plan-to-coding")
		if err != nil {
			t.Fatalf("Proceed() error: %v", err)
		}
		if len(result.ChildTaskIDs) != 0 {
			t.Fatalf("ChildTaskIDs count = %d, want 0 (live bootstrap wins over superseded)", len(result.ChildTaskIDs))
		}
		readState, err := db.New(stateFile).Read()
		if err != nil {
			t.Fatalf("Read state: %v", err)
		}
		trig := readState.FindTask(triggerID)
		ev := findTransitionExecutedEvent(trig)
		skipped := extractSkippedEntries(ev.Extra)
		if len(skipped) != 1 {
			t.Fatalf("skipped_entries count = %d, want 1", len(skipped))
		}
		if rm, _ := skipped[0]["remapped_to"].(string); rm != "boot-live" {
			t.Errorf("remapped_to = %q, want boot-live", rm)
		}
	})
}

func TestProceed_PerSubtask_KindDedup_DuplicateWithinBatch(t *testing.T) {
	tmpDir, stateFile := setupPipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted

	now := time.Now().UTC()
	triggerID := "plan-dupbatch"
	trigger := makeKindDedupTriggerTask(triggerID, []models.OutputEntry{
		{Desc: "Boot A", DoneWhen: "a", Scope: "s", SpecRef: "README.md", Kind: "bootstrap-precommit"},
		{Desc: "Boot B", DoneWhen: "b", Scope: "s", SpecRef: "README.md", Kind: "bootstrap-precommit"},
		{Desc: "After boot", DoneWhen: "c", Scope: "s", SpecRef: "README.md", DependsOn: []string{"0"}},
	}, now)
	state.Tasks = append(state.Tasks, trigger)
	state.Sprint.Scope.Planned = []string{triggerID}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Proceed(tmpDir, triggerID, "code-plan-to-coding")
	if err != nil {
		t.Fatalf("Proceed() error: %v", err)
	}
	if len(result.ChildTaskIDs) != 2 {
		t.Fatalf("ChildTaskIDs count = %d, want 2 (entry 1 dedup'd)", len(result.ChildTaskIDs))
	}
	firstBootID := "plan-dupbatch-code-plan-to-coding-0"
	if result.ChildTaskIDs[0] != firstBootID {
		t.Errorf("ChildTaskIDs[0] = %q, want %q", result.ChildTaskIDs[0], firstBootID)
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("Read state: %v", err)
	}
	if readState.FindTask("plan-dupbatch-code-plan-to-coding-1") != nil {
		t.Error("entry 1 should NOT exist (dedup'd within batch)")
	}
	after := readState.FindTask("plan-dupbatch-code-plan-to-coding-2")
	if after == nil {
		t.Fatal("after-boot child missing")
	}
	if !slices.Contains(after.DependsOn, firstBootID) {
		t.Errorf("after.DependsOn = %v, want to contain %q (first-occurrence remap)", after.DependsOn, firstBootID)
	}

	trig := readState.FindTask(triggerID)
	ev := findTransitionExecutedEvent(trig)
	skipped := extractSkippedEntries(ev.Extra)
	if len(skipped) != 1 {
		t.Fatalf("skipped_entries count = %d, want 1", len(skipped))
	}
	if got := skippedIndex(skipped[0]); got != 1 {
		t.Errorf("skipped[0].output_index = %d, want 1", got)
	}
	if rm, _ := skipped[0]["remapped_to"].(string); rm != firstBootID {
		t.Errorf("skipped[0].remapped_to = %q, want %q", rm, firstBootID)
	}
}

func TestProceed_PerSubtask_KindDedup_CrossGoal(t *testing.T) {
	tmpDir, stateFile := setupPipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted

	now := time.Now().UTC()
	// Goal A bootstrap: distinct SpecRef / parent chain.
	goalABoot := models.Task{
		ID:          "goalA-boot",
		Type:        models.TaskTypeCoding,
		RolePair:    "coding-pair",
		Description: "goal A bootstrap",
		Status:      models.TaskStatusReady,
		Priority:    1,
		Created:     now,
		SpecRef:     "specs/goals/goal-A.md",
		ParentTasks: []string{"goalA-arch"},
		DoneWhen:    "done",
		Scope:       "scope",
		Kind:        "bootstrap-precommit",
		DependsOn:   []string{"goalA-arch"},
		History:     []models.TaskHistoryEntry{},
	}
	originalA := slices.Clone(goalABoot.DependsOn)

	triggerID := "goalB-plan"
	trigger := makeKindDedupTriggerTask(triggerID, []models.OutputEntry{
		{Desc: "goal B boot", DoneWhen: "b", Scope: "s", SpecRef: "specs/goals/goal-B.md", Kind: "bootstrap-precommit"},
	}, now)
	trigger.SpecRef = "specs/goals/goal-B.md"
	trigger.ParentTasks = []string{"goalB-arch"}

	state.Tasks = append(state.Tasks, goalABoot, trigger)
	state.Sprint.Scope.Planned = []string{triggerID}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Proceed(tmpDir, triggerID, "code-plan-to-coding")
	if err != nil {
		t.Fatalf("Proceed() error: %v", err)
	}
	if len(result.ChildTaskIDs) != 0 {
		t.Fatalf("ChildTaskIDs count = %d, want 0 (cross-goal skip)", len(result.ChildTaskIDs))
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("Read state: %v", err)
	}
	gotA := readState.FindTask("goalA-boot")
	if gotA == nil {
		t.Fatal("goal A bootstrap vanished")
	}
	if !slices.Equal(gotA.DependsOn, originalA) {
		t.Errorf("cross-goal mutation: goalA-boot.DependsOn = %v, want %v", gotA.DependsOn, originalA)
	}

	trig := readState.FindTask(triggerID)
	ev := findTransitionExecutedEvent(trig)
	skipped := extractSkippedEntries(ev.Extra)
	if len(skipped) != 1 {
		t.Fatalf("skipped_entries count = %d, want 1", len(skipped))
	}
	if rm, _ := skipped[0]["remapped_to"].(string); rm != "goalA-boot" {
		t.Errorf("remapped_to = %q, want goalA-boot", rm)
	}
}

func TestProceed_PerSubtask_KindDedup_CrashRecovery(t *testing.T) {
	tmpDir, stateFile := setupPipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted

	now := time.Now().UTC()
	foreignDeps := []string{"goalA-arch", "extra-prior"}
	foreign := models.Task{
		ID:          "foreign-boot",
		Type:        models.TaskTypeCoding,
		RolePair:    "coding-pair",
		Description: "foreign bootstrap",
		Status:      models.TaskStatusReady,
		Priority:    1,
		Created:     now,
		SpecRef:     "specs/goals/goal-A.md",
		ParentTasks: []string{"goalA-arch"},
		DoneWhen:    "done",
		Scope:       "scope",
		Kind:        "bootstrap-precommit",
		DependsOn:   slices.Clone(foreignDeps),
		History:     []models.TaskHistoryEntry{},
	}

	triggerID := "goalB-plan-crash"
	trigger := makeKindDedupTriggerTask(triggerID, []models.OutputEntry{
		{Desc: "Boot dedup'd", DoneWhen: "b", Scope: "s", SpecRef: "README.md", Kind: "bootstrap-precommit"},
		{Desc: "Pre-crash child", DoneWhen: "p", Scope: "s", SpecRef: "README.md"},
		{Desc: "Missing after crash", DoneWhen: "m", Scope: "s", SpecRef: "README.md"},
	}, now)
	trigger.TransitionsExecuted = map[string]bool{"code-plan-to-coding": true}
	trigger.SpecRef = "specs/goals/goal-B.md"
	trigger.ParentTasks = []string{"goalB-arch"}

	preCrashChild := models.Task{
		ID:          "goalB-plan-crash-code-plan-to-coding-1",
		Type:        models.TaskTypeCoding,
		RolePair:    "coding-pair",
		Description: "Pre-crash child",
		Status:      models.TaskStatusReady,
		Priority:    1,
		Created:     now,
		ParentTasks: []string{triggerID},
		SpecRef:     "README.md",
		DoneWhen:    "p",
		Scope:       "s",
		History:     []models.TaskHistoryEntry{},
	}

	state.Tasks = append(state.Tasks, foreign, trigger, preCrashChild)
	state.Sprint.Scope.Planned = []string{triggerID}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Proceed(tmpDir, triggerID, "code-plan-to-coding")
	if err != nil {
		t.Fatalf("Proceed() error (crash recovery): %v", err)
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("Read state: %v", err)
	}

	// Entry 0 child must NOT exist — skip decision re-applied.
	if readState.FindTask("goalB-plan-crash-code-plan-to-coding-0") != nil {
		t.Error("entry 0 child exists but should have been skipped in recovery")
	}
	// Entry 1 child must still exist (was pre-crash).
	if readState.FindTask("goalB-plan-crash-code-plan-to-coding-1") == nil {
		t.Error("pre-crash entry 1 child missing after recovery")
	}
	// Entry 2 child must now exist (recovered).
	if readState.FindTask("goalB-plan-crash-code-plan-to-coding-2") == nil {
		t.Error("entry 2 child not recovered")
	}
	if len(result.ChildTaskIDs) != 1 || result.ChildTaskIDs[0] != "goalB-plan-crash-code-plan-to-coding-2" {
		t.Errorf("ChildTaskIDs = %v, want [goalB-plan-crash-code-plan-to-coding-2]", result.ChildTaskIDs)
	}

	// SAFETY INVARIANT: the foreign incumbent's DependsOn must be byte-equal
	// before and after recovery. This witnesses the skipped? → existing? →
	// create? precedence: a swap would call patchInheritedDeps on the foreign
	// task and append inheritedDeps into its DependsOn list.
	gotForeign := readState.FindTask("foreign-boot")
	if gotForeign == nil {
		t.Fatal("foreign bootstrap vanished")
	}
	if !slices.Equal(gotForeign.DependsOn, foreignDeps) {
		t.Errorf("foreign.DependsOn mutated by recovery: got %v, want %v (patchInheritedDeps was called on foreign incumbent)", gotForeign.DependsOn, foreignDeps)
	}

	// History has TransitionCrashRecov with recovered_children == 1.
	trig := readState.FindTask(triggerID)
	var recovEv *models.TaskHistoryEntry
	for i := len(trig.History) - 1; i >= 0; i-- {
		if trig.History[i].Event == models.TaskEventTransitionCrashRecov {
			recovEv = &trig.History[i]
			break
		}
	}
	if recovEv == nil {
		t.Fatal("no TransitionCrashRecov history event")
	}
	switch rc := recovEv.Extra["recovered_children"].(type) {
	case int:
		if rc != 1 {
			t.Errorf("recovered_children = %d, want 1", rc)
		}
	case int64:
		if rc != 1 {
			t.Errorf("recovered_children = %d, want 1", rc)
		}
	default:
		t.Errorf("recovered_children has unexpected type %T (%v)", recovEv.Extra["recovered_children"], recovEv.Extra["recovered_children"])
	}
}

func TestProceed_PerSubtask_NoKind_UnaffectedByDedup(t *testing.T) {
	tmpDir, stateFile := setupPipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted

	now := time.Now().UTC()
	otherDeps := []string{"upstream-a"}
	somethingElse := models.Task{
		ID:          "boot-unrelated",
		Type:        models.TaskTypeCoding,
		RolePair:    "coding-pair",
		Description: "unrelated bootstrap",
		Status:      models.TaskStatusReady,
		Priority:    1,
		Created:     now,
		SpecRef:     "README.md",
		DoneWhen:    "done",
		Scope:       "scope",
		Kind:        "bootstrap-precommit",
		DependsOn:   slices.Clone(otherDeps),
		History:     []models.TaskHistoryEntry{},
	}

	triggerID := "plan-nokind"
	trigger := makeKindDedupTriggerTask(triggerID, []models.OutputEntry{
		{Desc: "A", DoneWhen: "a", Scope: "s", SpecRef: "README.md"},
		{Desc: "B", DoneWhen: "b", Scope: "s", SpecRef: "README.md"},
		{Desc: "C", DoneWhen: "c", Scope: "s", SpecRef: "README.md"},
	}, now)
	state.Tasks = append(state.Tasks, somethingElse, trigger)
	state.Sprint.Scope.Planned = []string{triggerID}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Proceed(tmpDir, triggerID, "code-plan-to-coding")
	if err != nil {
		t.Fatalf("Proceed() error: %v", err)
	}
	if len(result.ChildTaskIDs) != 3 {
		t.Fatalf("ChildTaskIDs count = %d, want 3 (no-kind entries never dedup)", len(result.ChildTaskIDs))
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("Read state: %v", err)
	}
	trig := readState.FindTask(triggerID)
	ev := findTransitionExecutedEvent(trig)
	if _, ok := ev.Extra["skipped_entries"]; ok {
		t.Errorf("skipped_entries must be absent when all entries have empty Kind")
	}

	gotUnrelated := readState.FindTask("boot-unrelated")
	if gotUnrelated == nil {
		t.Fatal("unrelated bootstrap vanished")
	}
	if !slices.Equal(gotUnrelated.DependsOn, otherDeps) {
		t.Errorf("unrelated DependsOn mutated: got %v, want %v", gotUnrelated.DependsOn, otherDeps)
	}
	if gotUnrelated.Status != models.TaskStatusReady {
		t.Errorf("unrelated status changed: got %v", gotUnrelated.Status)
	}
}
