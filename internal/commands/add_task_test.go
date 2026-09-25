package commands

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestAddTaskCommand(t *testing.T) {
	tests := []struct {
		name          string
		taskID        string
		description   string
		specRef       string
		doneWhen      string
		scope         string
		priority      int
		depends       []string
		existingTasks []models.Task
		wantErr       bool
		errContains   string
	}{
		{
			name:        "add basic task",
			taskID:      "task-1",
			description: "Implement feature X",
			specRef:     "specs/vision.md",
			doneWhen:    "Feature X is implemented and tested",
			scope:       "Add feature X to the codebase",
			priority:    1,
			depends:     nil,
			wantErr:     false,
		},
		{
			name:        "add task with dependencies",
			taskID:      "task-2",
			description: "Implement feature Y",
			specRef:     "specs/vision.md",
			doneWhen:    "Feature Y is implemented",
			scope:       "Add feature Y",
			priority:    2,
			depends:     []string{"task-1"},
			existingTasks: []models.Task{
				{
					ID:          "task-1",
					Description: "Prerequisite task",
					Status:      models.TaskStatusReady,
					RolePair:    "coding-pair",
					Priority:    1,
					Created:     time.Now().UTC(),
					SpecRef:     "specs/vision.md",
					DoneWhen:    "Task 1 done",
					Scope:       "Task 1 scope",
					History:     []models.TaskHistoryEntry{},
				},
			},
			wantErr: false,
		},
		{
			name:        "add task with multiline done_when",
			taskID:      "task-3",
			description: "Complex task",
			specRef:     "specs/vision.md",
			doneWhen:    "Line 1\nLine 2\nLine 3",
			scope:       "Multi-line\nscope",
			priority:    1,
			depends:     nil,
			wantErr:     false,
		},
		{
			name:        "duplicate task ID",
			taskID:      "task-1",
			description: "Duplicate task",
			specRef:     "specs/vision.md",
			doneWhen:    "Should fail",
			scope:       "Scope",
			priority:    1,
			depends:     nil,
			existingTasks: []models.Task{
				{
					ID:          "task-1",
					Description: "Existing task",
					Status:      models.TaskStatusReady,
					RolePair:    "coding-pair",
					Priority:    1,
					Created:     time.Now().UTC(),
					SpecRef:     "specs/vision.md",
					DoneWhen:    "Done",
					Scope:       "Scope",
					History:     []models.TaskHistoryEntry{},
				},
			},
			wantErr:     true,
			errContains: "already exists",
		},
		{
			name:        "empty task ID",
			taskID:      "",
			description: "Task",
			specRef:     "specs/vision.md",
			doneWhen:    "Done",
			scope:       "Scope",
			priority:    1,
			wantErr:     true,
			errContains: "task ID cannot be empty",
		},
		{
			name:        "empty description",
			taskID:      "task-x",
			description: "",
			specRef:     "specs/vision.md",
			doneWhen:    "Done",
			scope:       "Scope",
			priority:    1,
			wantErr:     true,
			errContains: "description is required",
		},
		{
			name:        "empty spec_ref",
			taskID:      "task-x",
			description: "Task",
			specRef:     "",
			doneWhen:    "Done",
			scope:       "Scope",
			priority:    1,
			wantErr:     true,
			errContains: "spec_ref is required",
		},
		{
			name:        "empty done_when",
			taskID:      "task-x",
			description: "Task",
			specRef:     "specs/vision.md",
			doneWhen:    "",
			scope:       "Scope",
			priority:    1,
			wantErr:     true,
			errContains: "done_when is required",
		},
		{
			name:        "empty scope",
			taskID:      "task-x",
			description: "Task",
			specRef:     "specs/vision.md",
			doneWhen:    "Done",
			scope:       "",
			priority:    1,
			wantErr:     true,
			errContains: "scope is required",
		},
		{
			name:        "negative priority",
			taskID:      "task-x",
			description: "Task",
			specRef:     "specs/vision.md",
			doneWhen:    "Done",
			scope:       "Scope",
			priority:    -1,
			wantErr:     true,
			errContains: "priority must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directory for test (project root)
			tmpDir := t.TempDir()

			// Setup liza directory and spec file
			testhelpers.SetupTestGitRepo(t, tmpDir)
			stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
			testhelpers.SetupPipelineConfig(t, tmpDir)
			logFile := paths.New(tmpDir).LogPath()
			testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")

			// Create initial state with existing tasks if specified
			now := time.Now().UTC()
			initialState := &models.State{
				Version: 1,
				Goal: models.Goal{
					ID:               "goal-1",
					Description:      "Test goal",
					SpecRef:          "specs/vision.md",
					Created:          now,
					Status:           models.GoalStatusInProgress,
					AlignmentHistory: []models.AlignmentHistory{},
				},
				Tasks:  tt.existingTasks,
				Agents: make(map[string]models.Agent),
				Sprint: models.Sprint{
					ID:      "sprint-1",
					GoalRef: "goal-1",
					Scope: models.SprintScope{
						Planned: []string{},
						Stretch: []string{},
					},
					Timeline: models.SprintTimeline{
						Started: now,
					},
					Status: models.SprintStatusInProgress,
					Metrics: models.SprintMetrics{
						TasksDone:         0,
						TasksInProgress:   0,
						TasksBlocked:      0,
						IterationsTotal:   0,
						ReviewCyclesTotal: 0,
					},
				},
				CircuitBreaker: models.CircuitBreaker{
					Status:  "OK",
					History: []models.CircuitBreakerHistory{},
				},
				Config: models.Config{
					MaxCoderIterations: 10,
					MaxReviewCycles:    5,
					IntegrationBranch:  "integration",
				},
			}

			// Write initial state
			bb := testhelpers.WriteInitialState(t, stateFile, initialState)

			// Create empty log file
			if err := os.WriteFile(logFile, []byte{}, 0644); err != nil {
				t.Fatalf("Failed to create log file: %v", err)
			}

			// Create task input
			input := &TaskInput{
				ID:          tt.taskID,
				RolePair:    "coding-pair",
				Description: tt.description,
				SpecRef:     tt.specRef,
				DoneWhen:    tt.doneWhen,
				Scope:       tt.scope,
				Priority:    tt.priority,
				DependsOn:   tt.depends,
			}

			// Run command
			err := AddTaskCommand(stateFile, logFile, input, "orchestrator-1")

			// Check error
			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error but got none")
					return
				}
				testhelpers.AssertErrorContains(t, err, tt.errContains)
				return
			}

			testhelpers.AssertNoError(t, err)

			// Verify task was added to state
			state, err := bb.Read()
			if err != nil {
				t.Fatalf("Failed to read state: %v", err)
			}

			// Find the added task
			var addedTask *models.Task
			for i := range state.Tasks {
				if state.Tasks[i].ID == tt.taskID {
					addedTask = &state.Tasks[i]
					break
				}
			}

			if addedTask == nil {
				t.Fatalf("Task %s not found in state", tt.taskID)
			}

			// Verify task fields
			if addedTask.Description != tt.description {
				t.Errorf("Description = %q, want %q", addedTask.Description, tt.description)
			}
			if addedTask.Status != models.TaskStatusReady {
				t.Errorf("Status = %v, want %v", addedTask.Status, models.TaskStatusReady)
			}
			if addedTask.Priority != tt.priority {
				t.Errorf("Priority = %d, want %d", addedTask.Priority, tt.priority)
			}
			if addedTask.SpecRef != tt.specRef {
				t.Errorf("SpecRef = %q, want %q", addedTask.SpecRef, tt.specRef)
			}
			if addedTask.DoneWhen != tt.doneWhen {
				t.Errorf("DoneWhen = %q, want %q", addedTask.DoneWhen, tt.doneWhen)
			}
			if addedTask.Scope != tt.scope {
				t.Errorf("Scope = %q, want %q", addedTask.Scope, tt.scope)
			}
			if len(addedTask.DependsOn) != len(tt.depends) {
				t.Errorf("DependsOn length = %d, want %d", len(addedTask.DependsOn), len(tt.depends))
			}
			for i, dep := range tt.depends {
				if addedTask.DependsOn[i] != dep {
					t.Errorf("DependsOn[%d] = %q, want %q", i, addedTask.DependsOn[i], dep)
				}
			}
			if addedTask.Created.IsZero() {
				t.Error("Created timestamp is zero")
			}

			// Verify sprint.scope.planned was updated
			if !slices.Contains(state.Sprint.Scope.Planned, tt.taskID) {
				t.Errorf("Task %s not found in sprint.scope.planned", tt.taskID)
			}

			// Verify goal.alignment_history was updated
			if len(state.Goal.AlignmentHistory) == 0 {
				t.Error("AlignmentHistory is empty")
			} else {
				lastEntry := state.Goal.AlignmentHistory[len(state.Goal.AlignmentHistory)-1]
				if lastEntry.Event != "planning" {
					t.Errorf("AlignmentHistory event = %q, want %q", lastEntry.Event, "planning")
				}
				if !strings.Contains(lastEntry.Summary, tt.taskID) {
					t.Errorf("AlignmentHistory summary does not contain task ID %q", tt.taskID)
				}
			}

			// Verify log entry was added
			logger := log.New(logFile)
			entries, err := logger.Read()
			if err != nil {
				t.Fatalf("Failed to read log: %v", err)
			}

			if len(entries) == 0 {
				t.Error("No log entries found")
			} else {
				lastEntry := entries[len(entries)-1]
				if lastEntry.Action != "task_added" {
					t.Errorf("Log action = %q, want %q", lastEntry.Action, "task_added")
				}
				if lastEntry.Task == nil || *lastEntry.Task != tt.taskID {
					t.Errorf("Log task = %v, want %q", lastEntry.Task, tt.taskID)
				}
				if lastEntry.Detail != tt.description {
					t.Errorf("Log detail = %q, want %q", lastEntry.Detail, tt.description)
				}
			}
		})
	}
}

func TestLoadTaskInputFromFile(t *testing.T) {
	tests := []struct {
		name        string
		fileContent string
		wantErr     bool
		errContains string
		validate    func(*testing.T, *TaskInput)
	}{
		{
			name: "valid task file",
			fileContent: `id: task-1
description: Implement feature X
spec_ref: specs/vision.md
done_when: Feature X is implemented and tested
validation:
  - make test
  - pre-commit run --files specs/vision.md
destructive_db: true
scope: Add feature X to the codebase
priority: 2
depends_on:
  - task-0
`,
			wantErr: false,
			validate: func(t *testing.T, input *TaskInput) {
				if input.ID != "task-1" {
					t.Errorf("ID = %q, want %q", input.ID, "task-1")
				}
				if input.Description != "Implement feature X" {
					t.Errorf("Description = %q, want %q", input.Description, "Implement feature X")
				}
				if input.Priority != 2 {
					t.Errorf("Priority = %d, want %d", input.Priority, 2)
				}
				if len(input.DependsOn) != 1 || input.DependsOn[0] != "task-0" {
					t.Errorf("DependsOn = %v, want [task-0]", input.DependsOn)
				}
				if !input.DestructiveDB {
					t.Errorf("DestructiveDB = false, want true")
				}
				if len(input.Validation) != 2 || input.Validation[0] != "make test" || input.Validation[1] != "pre-commit run --files specs/vision.md" {
					t.Errorf("Validation = %v, want canonical commands", input.Validation)
				}
			},
		},
		{
			name: "multiline fields",
			fileContent: `id: task-2
description: Complex task
spec_ref: specs/vision.md
done_when: |
  Line 1
  Line 2
  Line 3
scope: |
  Multi-line
  scope description
priority: 1
`,
			wantErr: false,
			validate: func(t *testing.T, input *TaskInput) {
				if !strings.Contains(input.DoneWhen, "Line 1") {
					t.Errorf("DoneWhen doesn't contain multiline content")
				}
				if !strings.Contains(input.Scope, "Multi-line") {
					t.Errorf("Scope doesn't contain multiline content")
				}
			},
		},
		{
			name:        "invalid YAML",
			fileContent: "not: valid: yaml: [",
			wantErr:     true,
			errContains: "failed to parse",
		},
		{
			name:        "nonexistent file",
			fileContent: "", // Will not create file
			wantErr:     true,
			errContains: "failed to read",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var filePath string

			if tt.name != "nonexistent file" {
				// Create temp file
				tmpFile, err := os.CreateTemp(t.TempDir(), "task-*.yaml")
				if err != nil {
					t.Fatalf("Failed to create temp file: %v", err)
				}
				defer tmpFile.Close()

				filePath = tmpFile.Name()

				if _, err := tmpFile.WriteString(tt.fileContent); err != nil {
					t.Fatalf("Failed to write temp file: %v", err)
				}
			} else {
				filePath = "/nonexistent/path/to/file.yaml"
			}

			// Load from file
			input, err := LoadTaskInputFromFile(filePath)

			// Check error
			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error but got none")
					return
				}
				testhelpers.AssertErrorContains(t, err, tt.errContains)
				return
			}

			testhelpers.AssertNoError(t, err)

			if tt.validate != nil {
				tt.validate(t, input)
			}
		})
	}
}

func TestAddTaskCommandFromFile(t *testing.T) {
	// Create temp directory (project root)
	tmpDir := t.TempDir()

	// Setup liza directory and spec file
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	logFile := paths.New(tmpDir).LogPath()
	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	// Create initial state
	now := time.Now().UTC()
	initialState := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:               "goal-1",
			Description:      "Test goal",
			SpecRef:          "specs/vision.md",
			Created:          now,
			Status:           models.GoalStatusInProgress,
			AlignmentHistory: []models.AlignmentHistory{},
		},
		Tasks: []models.Task{
			{
				ID:          "task-0",
				Description: "Prerequisite task 0",
				Status:      models.TaskStatusMerged,
				RolePair:    "coding-pair",
				Priority:    1,
				Created:     now,
				SpecRef:     "specs/vision.md",
				DoneWhen:    "Task 0 done",
				Scope:       "Task 0 scope",
				History:     []models.TaskHistoryEntry{},
				HandoffEvents: []models.HandoffEvent{
					{Timestamp: now, Agent: "coder-1", Trigger: models.HandoffTriggerSubmission},
					{Timestamp: now, Agent: "code-reviewer-1", Trigger: models.HandoffTriggerCompletion},
				},
			},
			{
				ID:          "task-1",
				Description: "Prerequisite task 1",
				Status:      models.TaskStatusMerged,
				RolePair:    "coding-pair",
				Priority:    1,
				Created:     now,
				SpecRef:     "specs/vision.md",
				DoneWhen:    "Task 1 done",
				Scope:       "Task 1 scope",
				History:     []models.TaskHistoryEntry{},
				HandoffEvents: []models.HandoffEvent{
					{Timestamp: now, Agent: "coder-1", Trigger: models.HandoffTriggerSubmission},
					{Timestamp: now, Agent: "code-reviewer-1", Trigger: models.HandoffTriggerCompletion},
				},
			},
		},
		Agents: make(map[string]models.Agent),
		Sprint: models.Sprint{
			ID:      "sprint-1",
			GoalRef: "goal-1",
			Scope: models.SprintScope{
				Planned: []string{},
				Stretch: []string{},
			},
			Timeline: models.SprintTimeline{
				Started: now,
			},
			Status: models.SprintStatusInProgress,
			Metrics: models.SprintMetrics{
				TasksDone:         0,
				TasksInProgress:   0,
				TasksBlocked:      0,
				IterationsTotal:   0,
				ReviewCyclesTotal: 0,
			},
		},
		CircuitBreaker: models.CircuitBreaker{
			Status:  "OK",
			History: []models.CircuitBreakerHistory{},
		},
		Config: models.Config{
			MaxCoderIterations: 10,
			MaxReviewCycles:    5,
			IntegrationBranch:  "integration",
		},
	}

	// Write initial state
	bb := testhelpers.WriteInitialState(t, stateFile, initialState)

	// Create empty log file
	if err := os.WriteFile(logFile, []byte{}, 0644); err != nil {
		t.Fatalf("Failed to create log file: %v", err)
	}

	// Create task YAML file
	taskFileContent := `id: task-from-file
description: Task loaded from file
spec_ref: specs/vision.md
role_pair: coding-pair
done_when: |
  This task is done when:
  - All tests pass
  - Code is reviewed
validation:
  - make test
  - pre-commit run --files specs/vision.md
scope: |
  Implement feature from file specification
priority: 3
depends_on:
  - task-0
  - task-1
`
	taskFile := filepath.Join(tmpDir, "task.yaml")
	if err := os.WriteFile(taskFile, []byte(taskFileContent), 0644); err != nil {
		t.Fatalf("Failed to create task file: %v", err)
	}

	// Load task from file
	input, err := LoadTaskInputFromFile(taskFile)
	if err != nil {
		t.Fatalf("Failed to load task from file: %v", err)
	}

	// Run command
	if err := AddTaskCommand(stateFile, logFile, input, "orchestrator-1"); err != nil {
		t.Fatalf("AddTaskCommand failed: %v", err)
	}

	// Verify task was added
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	// Find the added task
	var addedTask *models.Task
	for i := range state.Tasks {
		if state.Tasks[i].ID == "task-from-file" {
			addedTask = &state.Tasks[i]
			break
		}
	}

	if addedTask == nil {
		t.Fatal("Task not found in state")
	}

	// Verify task fields
	if addedTask.Description != "Task loaded from file" {
		t.Errorf("Description = %q, want %q", addedTask.Description, "Task loaded from file")
	}
	if addedTask.Priority != 3 {
		t.Errorf("Priority = %d, want %d", addedTask.Priority, 3)
	}
	if !strings.Contains(addedTask.DoneWhen, "All tests pass") {
		t.Errorf("DoneWhen doesn't contain expected multiline content")
	}
	if len(addedTask.Validation) != 2 || addedTask.Validation[0] != "make test" || addedTask.Validation[1] != "pre-commit run --files specs/vision.md" {
		t.Errorf("Validation = %v, want canonical commands", addedTask.Validation)
	}
	if !strings.Contains(addedTask.Scope, "Implement feature") {
		t.Errorf("Scope doesn't contain expected multiline content")
	}
	if len(addedTask.DependsOn) != 2 {
		t.Errorf("DependsOn length = %d, want 2", len(addedTask.DependsOn))
	}
	if !slices.Contains(addedTask.DependsOn, "task-0") {
		t.Errorf("DependsOn doesn't contain task-0")
	}
	if !slices.Contains(addedTask.DependsOn, "task-1") {
		t.Errorf("DependsOn doesn't contain task-1")
	}
}

func TestAddTaskCommandTaskType(t *testing.T) {
	tests := []struct {
		name        string
		taskType    string
		wantErr     bool
		errContains string
		wantType    models.TaskType
	}{
		{
			name:     "default type when empty",
			taskType: "",
			wantType: models.TaskTypeCoding,
		},
		{
			name:     "explicit coding type",
			taskType: "coding",
			wantType: models.TaskTypeCoding,
		},
		{
			name:        "invalid type rejected",
			taskType:    "unknown",
			wantErr:     true,
			errContains: "unknown task type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			testhelpers.SetupTestGitRepo(t, tmpDir)
			stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
			testhelpers.SetupPipelineConfig(t, tmpDir)
			logFile := paths.New(tmpDir).LogPath()
			testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")

			state := testhelpers.CreateValidState()
			bb := testhelpers.WriteInitialState(t, stateFile, state)

			if err := os.WriteFile(logFile, []byte{}, 0644); err != nil {
				t.Fatalf("Failed to create log file: %v", err)
			}

			input := &TaskInput{
				ID:          "task-1",
				RolePair:    "coding-pair",
				Type:        tt.taskType,
				Description: "Test task",
				SpecRef:     "specs/vision.md",
				DoneWhen:    "Done",
				Scope:       "Scope",
				Priority:    1,
			}

			err := AddTaskCommand(stateFile, logFile, input, "orchestrator-1")

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got none")
					return
				}
				testhelpers.AssertErrorContains(t, err, tt.errContains)
				return
			}

			testhelpers.AssertNoError(t, err)

			readState, err := bb.Read()
			if err != nil {
				t.Fatalf("Failed to read state: %v", err)
			}

			for i := range readState.Tasks {
				if readState.Tasks[i].ID == "task-1" {
					if readState.Tasks[i].Type != tt.wantType {
						t.Errorf("Type = %q, want %q", readState.Tasks[i].Type, tt.wantType)
					}
					return
				}
			}
			t.Fatal("Task not found in state")
		})
	}
}

func TestAddTaskCommandValidation(t *testing.T) {
	// Create temp directory (project root)
	tmpDir := t.TempDir()

	// Setup liza directory
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	logFile := paths.New(tmpDir).LogPath()

	// Create initial state with invalid data to verify validation runs
	now := time.Now().UTC()
	initialState := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:               "goal-1",
			Description:      "Test goal",
			SpecRef:          "nonexistent.md", // Invalid spec ref
			Created:          now,
			Status:           models.GoalStatusInProgress,
			AlignmentHistory: []models.AlignmentHistory{},
		},
		Tasks:  []models.Task{},
		Agents: make(map[string]models.Agent),
		Sprint: models.Sprint{
			ID:      "sprint-1",
			GoalRef: "goal-1",
			Scope: models.SprintScope{
				Planned: []string{},
				Stretch: []string{},
			},
			Timeline: models.SprintTimeline{
				Started: now,
			},
			Status: models.SprintStatusInProgress,
			Metrics: models.SprintMetrics{
				TasksDone:         0,
				TasksInProgress:   0,
				TasksBlocked:      0,
				IterationsTotal:   0,
				ReviewCyclesTotal: 0,
			},
		},
		CircuitBreaker: models.CircuitBreaker{
			Status:  "OK",
			History: []models.CircuitBreakerHistory{},
		},
		Config: models.Config{
			MaxCoderIterations: 10,
			MaxReviewCycles:    5,
			IntegrationBranch:  "integration",
		},
	}

	// Write initial state
	_ = testhelpers.WriteInitialState(t, stateFile, initialState)

	// Create empty log file
	if err := os.WriteFile(logFile, []byte{}, 0644); err != nil {
		t.Fatalf("Failed to create log file: %v", err)
	}

	// Create task input
	input := &TaskInput{
		ID:          "task-1",
		RolePair:    "coding-pair",
		Description: "Test task",
		SpecRef:     "specs/vision.md",
		DoneWhen:    "Done",
		Scope:       "Scope",
		Priority:    1,
		DependsOn:   nil,
	}

	// Try to add a task - should fail validation
	err := AddTaskCommand(stateFile, logFile, input, "orchestrator-1")

	// Should fail because goal spec_ref doesn't exist
	if err == nil {
		t.Error("Expected validation error but got none")
	}
}
