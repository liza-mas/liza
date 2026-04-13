package ops

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestAddTask_Validation(t *testing.T) {
	tests := []struct {
		name        string
		input       AddTaskInput
		errContains string
	}{
		{
			name:        "empty task ID",
			input:       AddTaskInput{Description: "d", SpecRef: "s", DoneWhen: "w", Scope: "sc", Priority: 1},
			errContains: "invalid task ID",
		},
		{
			name:        "empty description",
			input:       AddTaskInput{ID: "t1", SpecRef: "s", DoneWhen: "w", Scope: "sc", Priority: 1},
			errContains: "description is required",
		},
		{
			name:        "empty spec_ref",
			input:       AddTaskInput{ID: "t1", Description: "d", DoneWhen: "w", Scope: "sc", Priority: 1},
			errContains: "spec_ref is required",
		},
		{
			name:        "empty done_when",
			input:       AddTaskInput{ID: "t1", Description: "d", SpecRef: "s", Scope: "sc", Priority: 1},
			errContains: "done_when is required",
		},
		{
			name:        "empty scope",
			input:       AddTaskInput{ID: "t1", Description: "d", SpecRef: "s", DoneWhen: "w", Priority: 1},
			errContains: "scope is required",
		},
		{
			name:        "zero priority",
			input:       AddTaskInput{ID: "t1", Description: "d", SpecRef: "s", DoneWhen: "w", Scope: "sc", Priority: 0},
			errContains: "priority must be positive",
		},
		{
			name:        "negative priority",
			input:       AddTaskInput{ID: "t1", Description: "d", SpecRef: "s", DoneWhen: "w", Scope: "sc", Priority: -1},
			errContains: "priority must be positive",
		},
		{
			name:        "invalid task type",
			input:       AddTaskInput{ID: "t1", Description: "d", SpecRef: "s", DoneWhen: "w", Scope: "sc", Priority: 1, Type: "invalid"},
			errContains: "unknown task type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := AddTask("/nonexistent", "/dev/null", &tt.input, "orchestrator-1")
			if err == nil {
				t.Fatal("Expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("Error = %q, want to contain %q", err.Error(), tt.errContains)
			}
		})
	}
}

func TestAddTask_Success(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	logFile := filepath.Join(tmpDir, ".liza", "log.jsonl")
	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")
	testhelpers.CreateSpecFile(t, tmpDir, "feature-x.md", "# Feature X\n")

	state := testhelpers.CreateValidState()
	now := time.Now().UTC()
	mergedEvents := []models.HandoffEvent{
		{Timestamp: now, Agent: "coder-1", Trigger: models.HandoffTriggerSubmission},
		{Timestamp: now, Agent: "code-reviewer-1", Trigger: models.HandoffTriggerCompletion},
	}
	state.Tasks = append(state.Tasks,
		models.Task{
			ID:            "dep-1",
			Description:   "Dependency 1",
			Status:        models.TaskStatusMerged,
			RolePair:      "coding-pair",
			Priority:      1,
			SpecRef:       "specs/vision.md",
			DoneWhen:      "done",
			Scope:         "scope",
			Created:       now,
			History:       []models.TaskHistoryEntry{},
			HandoffEvents: mergedEvents,
		},
		models.Task{
			ID:            "dep-2",
			Description:   "Dependency 2",
			Status:        models.TaskStatusMerged,
			RolePair:      "coding-pair",
			Priority:      1,
			SpecRef:       "specs/vision.md",
			DoneWhen:      "done",
			Scope:         "scope",
			Created:       now,
			History:       []models.TaskHistoryEntry{},
			HandoffEvents: mergedEvents,
		},
	)
	testhelpers.WriteInitialState(t, stateFile, state)

	input := &AddTaskInput{
		ID:          "task-1",
		Description: "Implement feature X",
		SpecRef:     "specs/feature-x.md",
		DoneWhen:    "Tests pass",
		Scope:       "internal/ops",
		Priority:    2,
		RolePair:    "coding-pair",
		DependsOn:   []string{"dep-1", " dep-2 ", ""},
	}

	result, err := AddTask(stateFile, logFile, input, "orchestrator-1")
	if err != nil {
		t.Fatalf("AddTask() error: %v", err)
	}

	if result.TaskID != "task-1" {
		t.Errorf("TaskID = %q, want %q", result.TaskID, "task-1")
	}

	// Verify state
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	task := readState.FindTask("task-1")
	if task == nil {
		t.Fatal("Task not found in state")
	}
	if task.Status != models.TaskStatusReady {
		t.Errorf("Task status = %v, want READY", task.Status)
	}
	if task.Priority != 2 {
		t.Errorf("Priority = %d, want 2", task.Priority)
	}
	if task.Type != models.TaskTypeCoding {
		t.Errorf("Type = %v, want coding", task.Type)
	}
	// Verify deps normalized (trimmed, empty removed)
	if len(task.DependsOn) != 2 {
		t.Errorf("DependsOn len = %d, want 2", len(task.DependsOn))
	}
	if task.DependsOn[1] != "dep-2" {
		t.Errorf("DependsOn[1] = %q, want %q", task.DependsOn[1], "dep-2")
	}

	// Verify sprint scope updated
	found := false
	for _, id := range readState.Sprint.Scope.Planned {
		if id == "task-1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Task ID not added to Sprint.Scope.Planned")
	}

	// Verify alignment history updated
	lastAlignment := readState.Goal.AlignmentHistory[len(readState.Goal.AlignmentHistory)-1]
	if !strings.Contains(lastAlignment.Summary, "task-1") {
		t.Errorf("Alignment history summary should mention task ID, got %q", lastAlignment.Summary)
	}
}

func TestAddTask_EmptyOrchestratorIDReturnsError(t *testing.T) {
	input := &AddTaskInput{
		ID: "task-1", Description: "d", SpecRef: "specs/vision.md",
		DoneWhen: "w", Scope: "sc", Priority: 1,
		RolePair: "coding-pair",
	}

	_, err := AddTask("/nonexistent", "/dev/null", input, "")
	if err == nil {
		t.Fatal("expected error for empty orchestrator ID")
	}
	testhelpers.AssertErrorContains(t, err, "orchestrator agent ID is required")
}

// minimalPipelineYAML is a minimal valid pipeline config for testing role_pair validation.
const minimalPipelineYAML = `pipeline:
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
          trigger: manual
          cardinality: per-subtask
  entry-points:
    detailed-spec: coding-subpipeline.code-planning-pair
`

// setupPipelineProject creates a temp dir with .liza/pipeline.yaml, state.yaml, and specs.
func setupPipelineProject(t *testing.T) (stateFile, logFile string) {
	t.Helper()
	tmpDir := t.TempDir()
	stateFile, _ = testhelpers.SetupLizaDir(t, tmpDir)
	logFile = filepath.Join(tmpDir, ".liza", "log.jsonl")
	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")
	testhelpers.CreateSpecFile(t, tmpDir, "feature.md", "# Feature\n")

	// Write pipeline config
	pipelinePath := filepath.Join(tmpDir, ".liza", "pipeline.yaml")
	if err := os.WriteFile(pipelinePath, []byte(minimalPipelineYAML), 0644); err != nil {
		t.Fatalf("Failed to write pipeline.yaml: %v", err)
	}

	state := testhelpers.CreateValidState()
	state.PipelineVersion = 1
	testhelpers.WriteInitialState(t, stateFile, state)

	return stateFile, logFile
}

func TestAddTask_RolePairValidation(t *testing.T) {
	stateFile, logFile := setupPipelineProject(t)

	tests := []struct {
		name        string
		input       AddTaskInput
		errContains []string
	}{
		{
			name: "role_pair required for pipeline goal",
			input: AddTaskInput{
				ID: "t1", Description: "d", SpecRef: "specs/feature.md",
				DoneWhen: "w", Scope: "sc", Priority: 1,
				// RolePair intentionally empty
			},
			errContains: []string{"role_pair is required", "code-planning-pair", "coding-pair"},
		},
		{
			name: "invalid role_pair for pipeline goal",
			input: AddTaskInput{
				ID: "t2", Description: "d", SpecRef: "specs/feature.md",
				DoneWhen: "w", Scope: "sc", Priority: 1,
				RolePair: "nonexistent-pair",
			},
			errContains: []string{"unknown role_pair", "nonexistent-pair", "code-planning-pair", "coding-pair"},
		},
		{
			name: "unknown task type mentions valid types",
			input: AddTaskInput{
				ID: "t3", Description: "d", SpecRef: "specs/feature.md",
				DoneWhen: "w", Scope: "sc", Priority: 1,
				Type: "bogus",
			},
			errContains: []string{"unknown task type", "bogus"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := AddTask(stateFile, logFile, &tt.input, "orchestrator-1")
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			var pe *PreconditionError
			if !errors.As(err, &pe) {
				t.Fatalf("expected PreconditionError, got %T: %v", err, err)
			}

			for _, want := range tt.errContains {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want to contain %q", err.Error(), want)
				}
			}
		})
	}
}

func TestAddTask_PipelineSuccess(t *testing.T) {
	stateFile, logFile := setupPipelineProject(t)

	input := &AddTaskInput{
		ID:          "pipeline-task-1",
		Description: "Implement feature via pipeline",
		SpecRef:     "specs/feature.md",
		DoneWhen:    "Tests pass",
		Scope:       "internal/ops",
		Priority:    1,
		RolePair:    "code-planning-pair",
	}

	result, err := AddTask(stateFile, logFile, input, "orchestrator-1")
	if err != nil {
		t.Fatalf("AddTask() error: %v", err)
	}
	if result.TaskID != "pipeline-task-1" {
		t.Errorf("TaskID = %q, want %q", result.TaskID, "pipeline-task-1")
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	task := readState.FindTask("pipeline-task-1")
	if task == nil {
		t.Fatal("Task not found in state")
	}
	// Pipeline-derived initial status for code-planning-pair is DRAFT_CODING_PLAN
	if task.Status != models.TaskStatusDraftCodingPlan {
		t.Errorf("Task status = %v, want %v", task.Status, models.TaskStatusDraftCodingPlan)
	}
	if task.RolePair != "code-planning-pair" {
		t.Errorf("RolePair = %q, want %q", task.RolePair, "code-planning-pair")
	}
}

func TestAddTask_DuplicateID(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	logFile := filepath.Join(tmpDir, ".liza", "log.jsonl")

	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		{ID: "task-1", Description: "existing", Status: models.TaskStatusReady, RolePair: "coding-pair"},
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	input := &AddTaskInput{
		ID: "task-1", Description: "d", SpecRef: "s",
		DoneWhen: "w", Scope: "sc", Priority: 1,
		RolePair: "coding-pair",
	}

	_, err := AddTask(stateFile, logFile, input, "orchestrator-1")
	if err == nil {
		t.Fatal("Expected error for duplicate task ID")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("Error = %q, want to contain 'already exists'", err.Error())
	}
}

func TestAddTask_PostWriteValidationFailure(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	logFile := filepath.Join(tmpDir, ".liza", "log.jsonl")
	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")
	testhelpers.CreateSpecFile(t, tmpDir, "feature-x.md", "# Feature X\n")

	state := testhelpers.CreateValidState()
	state.Tasks = append(state.Tasks, models.Task{
		ID:          "invalid-existing-task",
		Description: "Invalid existing task",
		Status:      models.TaskStatusImplementing, // missing assigned_to/worktree/base_commit
		RolePair:    "coding-pair",
		Priority:    1,
		SpecRef:     "specs/vision.md",
		DoneWhen:    "done",
		Scope:       "scope",
		Created:     time.Now().UTC(),
		History:     []models.TaskHistoryEntry{},
	})
	testhelpers.WriteInitialState(t, stateFile, state)

	input := &AddTaskInput{
		ID:          "task-added-before-validation-failure",
		Description: "Task to trigger post-write validation",
		SpecRef:     "specs/feature-x.md",
		DoneWhen:    "tests pass",
		Scope:       "internal/ops",
		Priority:    1,
		RolePair:    "coding-pair",
	}

	_, err := AddTask(stateFile, logFile, input, "orchestrator-1")
	if err == nil {
		t.Fatal("expected post-write validation error")
	}

	var validationErr *PostWriteValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected PostWriteValidationError, got %T: %v", err, err)
	}

	if !strings.Contains(err.Error(), "state validation failed") {
		t.Fatalf("error = %q, want to contain %q", err.Error(), "state validation failed")
	}
}

func TestAddTasks_PartialSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	logFile := filepath.Join(tmpDir, ".liza", "log.jsonl")
	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")
	testhelpers.CreateSpecFile(t, tmpDir, "feature.md", "# Feature\n")

	state := testhelpers.CreateValidState()
	// Pre-seed a task so the second input is a duplicate
	state.Tasks = append(state.Tasks, models.Task{
		ID:          "dup-task",
		Description: "Existing task",
		Status:      models.TaskStatusReady,
		RolePair:    "coding-pair",
		Priority:    1,
		SpecRef:     "specs/vision.md",
		DoneWhen:    "done",
		Scope:       "scope",
		Created:     time.Now().UTC(),
		History:     []models.TaskHistoryEntry{},
	})
	testhelpers.WriteInitialState(t, stateFile, state)

	input := &AddTasksInput{
		OrchestratorID: "orchestrator-1",
		Tasks: []AddTaskInput{
			{
				ID:          "new-task",
				Description: "A new task",
				SpecRef:     "specs/feature.md",
				DoneWhen:    "Tests pass",
				Scope:       "internal/ops",
				Priority:    1,
				RolePair:    "coding-pair",
			},
			{
				ID:          "dup-task",
				Description: "Duplicate task",
				SpecRef:     "specs/vision.md",
				DoneWhen:    "done",
				Scope:       "scope",
				Priority:    1,
				RolePair:    "coding-pair",
			},
		},
	}

	result, err := AddTasks(stateFile, logFile, input)
	if err != nil {
		t.Fatalf("AddTasks() returned error: %v", err)
	}

	if len(result.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result.Results))
	}

	// First task should succeed
	if !result.Results[0].Success {
		t.Errorf("first task should succeed, got error: %s", result.Results[0].Error)
	}
	if result.Results[0].TaskID != "new-task" {
		t.Errorf("first task ID = %q, want %q", result.Results[0].TaskID, "new-task")
	}

	// Second task should fail (duplicate)
	if result.Results[1].Success {
		t.Error("second task should fail (duplicate ID)")
	}
	if !strings.Contains(result.Results[1].Error, "already exists") {
		t.Errorf("second task error = %q, want to contain 'already exists'", result.Results[1].Error)
	}

	// Verify the first task was actually added
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if readState.FindTask("new-task") == nil {
		t.Error("new-task should exist in state")
	}
}

func TestAddTask_OptionalPlanRefStoredOnTask(t *testing.T) {
	stateFile, logFile := setupPipelineProject(t)
	// Create plan file so state validation passes
	projectRoot := filepath.Dir(filepath.Dir(stateFile))
	planDir := filepath.Join(projectRoot, "specs", "plans")
	if err := os.MkdirAll(planDir, 0755); err != nil {
		t.Fatalf("Failed to create plans dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(planDir, "20260317-plan.md"), []byte("# Plan\n"), 0644); err != nil {
		t.Fatalf("Failed to create plan file: %v", err)
	}

	input := &AddTaskInput{
		ID:          "task-planref",
		Description: "Task with plan_ref",
		SpecRef:     "specs/vision.md",
		PlanRef:     "specs/plans/20260317-plan.md",
		DoneWhen:    "Tests pass",
		Scope:       "internal/ops",
		Priority:    1,
		RolePair:    "coding-pair",
	}

	result, err := AddTask(stateFile, logFile, input, "orchestrator-1")
	if err != nil {
		t.Fatalf("AddTask() error: %v", err)
	}
	if result.TaskID != "task-planref" {
		t.Errorf("TaskID = %q, want %q", result.TaskID, "task-planref")
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	task := readState.FindTask("task-planref")
	if task == nil {
		t.Fatal("Task not found in state")
	}
	if task.PlanRef != "specs/plans/20260317-plan.md" {
		t.Errorf("PlanRef = %q, want %q", task.PlanRef, "specs/plans/20260317-plan.md")
	}
}

func TestAddTask_PlanRefNormalizesWorktreePrefix(t *testing.T) {
	stateFile, logFile := setupPipelineProject(t)
	// Create plan file so state validation passes after normalization
	projectRoot := filepath.Dir(filepath.Dir(stateFile))
	planDir := filepath.Join(projectRoot, "specs", "plans")
	if err := os.MkdirAll(planDir, 0755); err != nil {
		t.Fatalf("Failed to create plans dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(planDir, "plan.md"), []byte("# Plan\n"), 0644); err != nil {
		t.Fatalf("Failed to create plan file: %v", err)
	}

	input := &AddTaskInput{
		ID:          "task-wt-planref",
		Description: "Task with worktree plan_ref",
		SpecRef:     "specs/vision.md",
		PlanRef:     ".worktrees/planner-1/specs/plans/plan.md",
		DoneWhen:    "Tests pass",
		Scope:       "internal/ops",
		Priority:    1,
		RolePair:    "coding-pair",
	}

	result, err := AddTask(stateFile, logFile, input, "orchestrator-1")
	if err != nil {
		t.Fatalf("AddTask() error: %v", err)
	}
	if result.TaskID != "task-wt-planref" {
		t.Errorf("TaskID = %q, want %q", result.TaskID, "task-wt-planref")
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	task := readState.FindTask("task-wt-planref")
	if task == nil {
		t.Fatal("Task not found in state")
	}
	if task.PlanRef != "specs/plans/plan.md" {
		t.Errorf("PlanRef = %q, want %q (worktree prefix should be stripped)", task.PlanRef, "specs/plans/plan.md")
	}
}

func TestAddTasks_EmptyInput(t *testing.T) {
	input := &AddTasksInput{Tasks: []AddTaskInput{}}
	_, err := AddTasks("/nonexistent", "/dev/null", input)
	if err == nil {
		t.Fatal("expected error for empty tasks")
	}
	if !strings.Contains(err.Error(), "at least one task") {
		t.Errorf("error = %q, want 'at least one task'", err.Error())
	}
}

func TestAddTaskInput_JSONUnmarshal(t *testing.T) {
	// Wire format documented in add-tasks CLI help and wake_initial_planning.tmpl.
	input := `[
		{
			"id": "task-auth-1",
			"desc": "Implement auth",
			"spec": "specs/auth.md",
			"done": "GET /protected returns 401",
			"scope": "internal/auth",
			"priority": 2,
			"depends": ["task-base-1"],
			"type": "code",
			"role_pair": "coding-pair",
			"plan_ref": "specs/plans/plan-1.md"
		}
	]`

	var tasks []AddTaskInput
	if err := json.Unmarshal([]byte(input), &tasks); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("got %d tasks, want 1", len(tasks))
	}
	task := tasks[0]
	if task.ID != "task-auth-1" {
		t.Errorf("ID = %q", task.ID)
	}
	if task.Description != "Implement auth" {
		t.Errorf("Description = %q", task.Description)
	}
	if task.SpecRef != "specs/auth.md" {
		t.Errorf("SpecRef = %q", task.SpecRef)
	}
	if task.DoneWhen != "GET /protected returns 401" {
		t.Errorf("DoneWhen = %q", task.DoneWhen)
	}
	if task.Scope != "internal/auth" {
		t.Errorf("Scope = %q", task.Scope)
	}
	if task.Priority != 2 {
		t.Errorf("Priority = %d", task.Priority)
	}
	if len(task.DependsOn) != 1 || task.DependsOn[0] != "task-base-1" {
		t.Errorf("DependsOn = %v", task.DependsOn)
	}
	if task.Type != "code" {
		t.Errorf("Type = %q", task.Type)
	}
	if task.RolePair != "coding-pair" {
		t.Errorf("RolePair = %q", task.RolePair)
	}
	if task.PlanRef != "specs/plans/plan-1.md" {
		t.Errorf("PlanRef = %q", task.PlanRef)
	}
}
