package ops

import (
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestWriteCheckpoint_Validation(t *testing.T) {
	tests := []struct {
		name        string
		input       WriteCheckpointInput
		errContains string
	}{
		{
			name:        "empty task ID",
			input:       WriteCheckpointInput{AgentID: "coder-1", Intent: "i", ValidationPlan: "v", FilesToModify: []string{"f"}},
			errContains: "task_id is required",
		},
		{
			name:        "empty agent ID",
			input:       WriteCheckpointInput{TaskID: "t1", Intent: "i", ValidationPlan: "v", FilesToModify: []string{"f"}},
			errContains: "agent_id is required",
		},
		{
			name:        "empty intent",
			input:       WriteCheckpointInput{TaskID: "t1", AgentID: "coder-1", ValidationPlan: "v", FilesToModify: []string{"f"}},
			errContains: "intent is required",
		},
		{
			name:        "empty validation plan",
			input:       WriteCheckpointInput{TaskID: "t1", AgentID: "coder-1", Intent: "i", FilesToModify: []string{"f"}},
			errContains: "validation_plan is required",
		},
		// files_to_modify is optional (read-only analysis tasks modify no files)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := WriteCheckpoint("/nonexistent", &tt.input)
			testhelpers.RequireErrorContains(t, err, tt.errContains)
		})
	}
}

func TestWriteCheckpoint_TaskNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	testhelpers.WriteInitialState(t, stateFile, state)

	err := WriteCheckpoint(tmpDir, &WriteCheckpointInput{
		TaskID:         "nonexistent",
		AgentID:        "coder-1",
		Intent:         "test",
		ValidationPlan: "test",
		FilesToModify:  []string{"file.go"},
	})
	testhelpers.RequireErrorContains(t, err, "not found")
}

func TestWriteCheckpoint_WrongStatus(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	err := WriteCheckpoint(tmpDir, &WriteCheckpointInput{
		TaskID:         "task-1",
		AgentID:        "coder-1",
		Intent:         "test",
		ValidationPlan: "test",
		FilesToModify:  []string{"file.go"},
	})
	testhelpers.RequireErrorContains(t, err, "not in an executing state")
}

func TestWriteCheckpoint_PipelineExecutingStatus(t *testing.T) {
	tmpDir, stateFile := setupPipelineTest(t)

	now := time.Now().UTC()
	agent := "coder-1"
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		{
			ID:          "task-1",
			Type:        models.TaskTypeCoding,
			Description: "Pipeline task",
			Status:      "IMPLEMENTING_CODE",
			RolePair:    "coding-pair",
			Priority:    1,
			Created:     now,
			AssignedTo:  &agent,
			SpecRef:     "README.md",
			DoneWhen:    "Done",
			Scope:       "Test",
			History:     []models.TaskHistoryEntry{},
		},
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	err := WriteCheckpoint(tmpDir, &WriteCheckpointInput{
		TaskID:         "task-1",
		AgentID:        "coder-1",
		Intent:         "Implement feature via pipeline",
		ValidationPlan: "go test ./...",
		FilesToModify:  []string{"main.go"},
	})
	if err != nil {
		t.Fatalf("WriteCheckpoint failed for pipeline executing status: %v", err)
	}

	bb := db.For(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if !HasCheckpoint(readState.FindTask("task-1").History, "coder-1") {
		t.Fatal("Expected checkpoint in task history")
	}
}

func TestWriteCheckpoint_PipelineNonExecutingStatus(t *testing.T) {
	tmpDir, stateFile := setupPipelineTest(t)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		{
			ID:          "task-1",
			Type:        models.TaskTypeCoding,
			Description: "Pipeline task at initial",
			Status:      "DRAFT_CODE",
			RolePair:    "coding-pair",
			Priority:    1,
			Created:     now,
			SpecRef:     "README.md",
			DoneWhen:    "Done",
			Scope:       "Test",
			History:     []models.TaskHistoryEntry{},
		},
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	err := WriteCheckpoint(tmpDir, &WriteCheckpointInput{
		TaskID:         "task-1",
		AgentID:        "coder-1",
		Intent:         "Should not work",
		ValidationPlan: "go test ./...",
		FilesToModify:  []string{"main.go"},
	})
	testhelpers.RequireErrorContains(t, err, "not in an executing state")
}

func TestWriteCheckpoint_WrongAgent(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	err := WriteCheckpoint(tmpDir, &WriteCheckpointInput{
		TaskID:         "task-1",
		AgentID:        "coder-2",
		Intent:         "test",
		ValidationPlan: "test",
		FilesToModify:  []string{"file.go"},
	})
	testhelpers.RequireErrorContains(t, err, "not assigned to agent")
}

func TestWriteCheckpoint_Success(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	err := WriteCheckpoint(tmpDir, &WriteCheckpointInput{
		TaskID:         "task-1",
		AgentID:        "coder-1",
		Intent:         "Implement greeting function",
		ValidationPlan: "python -m hello --name Test outputs 'Hello, Test!'",
		FilesToModify:  []string{"hello/__main__.py", "hello/__init__.py"},
		Assumptions:    []string{"argparse is preferred per spec"},
		Risks:          "None identified",
	})
	if err != nil {
		t.Fatalf("WriteCheckpoint failed: %v", err)
	}

	// Verify the checkpoint was written to history
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	task := readState.FindTask("task-1")
	if task == nil {
		t.Fatal("Task not found after checkpoint")
	}

	if !HasCheckpoint(task.History, "coder-1") {
		t.Fatal("Expected checkpoint in task history")
	}

	// Verify the last history entry
	lastEntry := task.History[len(task.History)-1]
	if lastEntry.Event != models.TaskEventPreExecutionCheckpoint {
		t.Errorf("Expected event %q, got %q", models.TaskEventPreExecutionCheckpoint, lastEntry.Event)
	}
	if lastEntry.Agent == nil || *lastEntry.Agent != "coder-1" {
		t.Error("Expected agent 'coder-1' in checkpoint entry")
	}
	if lastEntry.Extra["intent"] != "Implement greeting function" {
		t.Errorf("Expected intent in extra, got %v", lastEntry.Extra["intent"])
	}
}

func TestWriteCheckpoint_SuccessWithoutOptionalFields(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	err := WriteCheckpoint(tmpDir, &WriteCheckpointInput{
		TaskID:         "task-1",
		AgentID:        "coder-1",
		Intent:         "Implement feature",
		ValidationPlan: "go test ./...",
		FilesToModify:  []string{"main.go"},
	})
	if err != nil {
		t.Fatalf("WriteCheckpoint failed: %v", err)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	task := readState.FindTask("task-1")
	lastEntry := task.History[len(task.History)-1]

	// Optional fields should not be present
	if _, ok := lastEntry.Extra["assumptions"]; ok {
		t.Error("Expected no assumptions key when none provided")
	}
	if _, ok := lastEntry.Extra["risks"]; ok {
		t.Error("Expected no risks key when none provided")
	}
}

func TestWriteCheckpoint_TDDNotRequired(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	err := WriteCheckpoint(tmpDir, &WriteCheckpointInput{
		TaskID:         "task-1",
		AgentID:        "coder-1",
		Intent:         "Fix typo in comment",
		ValidationPlan: "go build ./...",
		FilesToModify:  []string{"main.go"},
		TDDNotRequired: "cosmetic-only: comment typo fix",
	})
	if err != nil {
		t.Fatalf("WriteCheckpoint failed: %v", err)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	task := readState.FindTask("task-1")
	lastEntry := task.History[len(task.History)-1]

	val, ok := lastEntry.Extra["tdd_not_required"].(string)
	if !ok {
		t.Fatal("Expected tdd_not_required in Extra")
	}
	if val != "cosmetic-only: comment typo fix" {
		t.Errorf("Expected 'cosmetic-only: comment typo fix', got %q", val)
	}
}

func TestWriteCheckpoint_TDDNotRequired_OmittedWhenEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	err := WriteCheckpoint(tmpDir, &WriteCheckpointInput{
		TaskID:         "task-1",
		AgentID:        "coder-1",
		Intent:         "Add feature",
		ValidationPlan: "go test ./...",
		FilesToModify:  []string{"main.go"},
		// TDDNotRequired intentionally empty
	})
	if err != nil {
		t.Fatalf("WriteCheckpoint failed: %v", err)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	task := readState.FindTask("task-1")
	lastEntry := task.History[len(task.History)-1]

	if _, ok := lastEntry.Extra["tdd_not_required"]; ok {
		t.Error("Expected no tdd_not_required key when field is empty")
	}
}

func TestWriteCheckpoint_ScopeExtensions(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	err := WriteCheckpoint(tmpDir, &WriteCheckpointInput{
		TaskID:         "task-1",
		AgentID:        "coder-1",
		Intent:         "Add helper to shared package",
		ValidationPlan: "go test ./...",
		FilesToModify:  []string{"internal/ops/main.go"},
		ScopeExtensions: []ScopeExtensionEntry{
			{File: "internal/utils/helpers.go", Justification: "Need to add shared helper used by main implementation"},
		},
	})
	if err != nil {
		t.Fatalf("WriteCheckpoint failed: %v", err)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	task := readState.FindTask("task-1")
	lastEntry := task.History[len(task.History)-1]

	raw, ok := lastEntry.Extra["scope_extensions"]
	if !ok {
		t.Fatal("Expected scope_extensions in Extra")
	}

	// After storage, the value should be recoverable via GetLatestScopeExtensions
	extensions := GetLatestScopeExtensions(task.History, "coder-1")
	if len(extensions) != 1 {
		t.Fatalf("Expected 1 scope extension, got %d (raw: %v)", len(extensions), raw)
	}
	if extensions[0]["file"] != "internal/utils/helpers.go" {
		t.Errorf("Expected file 'internal/utils/helpers.go', got %q", extensions[0]["file"])
	}
	if extensions[0]["justification"] != "Need to add shared helper used by main implementation" {
		t.Errorf("Expected justification mismatch, got %q", extensions[0]["justification"])
	}
}

func TestWriteCheckpoint_ScopeExtensions_OmittedWhenEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	err := WriteCheckpoint(tmpDir, &WriteCheckpointInput{
		TaskID:         "task-1",
		AgentID:        "coder-1",
		Intent:         "Simple change",
		ValidationPlan: "go test ./...",
		FilesToModify:  []string{"main.go"},
		// ScopeExtensions intentionally empty
	})
	if err != nil {
		t.Fatalf("WriteCheckpoint failed: %v", err)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	task := readState.FindTask("task-1")
	lastEntry := task.History[len(task.History)-1]

	if _, ok := lastEntry.Extra["scope_extensions"]; ok {
		t.Error("Expected no scope_extensions key when none provided")
	}
}

func TestGetLatestScopeExtensions(t *testing.T) {
	agent := "coder-1"
	otherAgent := "coder-2"

	tests := []struct {
		name    string
		history []models.TaskHistoryEntry
		agentID string
		want    int // expected count
	}{
		{
			name:    "empty history",
			history: nil,
			agentID: "coder-1",
			want:    0,
		},
		{
			name: "checkpoint without scope extensions",
			history: []models.TaskHistoryEntry{
				{Event: models.TaskEventPreExecutionCheckpoint, Agent: &agent, Extra: map[string]any{"intent": "test"}},
			},
			agentID: "coder-1",
			want:    0,
		},
		{
			name: "checkpoint with scope extensions",
			history: []models.TaskHistoryEntry{
				{Event: models.TaskEventPreExecutionCheckpoint, Agent: &agent, Extra: map[string]any{
					"intent": "test",
					"scope_extensions": []map[string]string{
						{"file": "pkg/util.go", "justification": "shared helper"},
					},
				}},
			},
			agentID: "coder-1",
			want:    1,
		},
		{
			name: "checkpoint from different agent ignored",
			history: []models.TaskHistoryEntry{
				{Event: models.TaskEventPreExecutionCheckpoint, Agent: &otherAgent, Extra: map[string]any{
					"scope_extensions": []map[string]string{
						{"file": "other.go", "justification": "other"},
					},
				}},
			},
			agentID: "coder-1",
			want:    0,
		},
		{
			name: "latest checkpoint wins",
			history: []models.TaskHistoryEntry{
				{Event: models.TaskEventPreExecutionCheckpoint, Agent: &agent, Extra: map[string]any{
					"scope_extensions": []map[string]string{
						{"file": "old.go", "justification": "old"},
					},
				}},
				{Event: models.TaskEventPreExecutionCheckpoint, Agent: &agent, Extra: map[string]any{
					"intent": "new checkpoint without extensions",
				}},
			},
			agentID: "coder-1",
			want:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetLatestScopeExtensions(tt.history, tt.agentID)
			if len(got) != tt.want {
				t.Errorf("GetLatestScopeExtensions() returned %d entries, want %d", len(got), tt.want)
			}
		})
	}
}

func TestGetTDDWaiver(t *testing.T) {
	agent := "coder-1"
	otherAgent := "coder-2"

	tests := []struct {
		name    string
		history []models.TaskHistoryEntry
		agentID string
		want    string
	}{
		{
			name:    "empty history",
			history: nil,
			agentID: "coder-1",
			want:    "",
		},
		{
			name: "checkpoint without waiver",
			history: []models.TaskHistoryEntry{
				{Event: models.TaskEventPreExecutionCheckpoint, Agent: &agent, Extra: map[string]any{"intent": "test"}},
			},
			agentID: "coder-1",
			want:    "",
		},
		{
			name: "checkpoint with waiver",
			history: []models.TaskHistoryEntry{
				{Event: models.TaskEventPreExecutionCheckpoint, Agent: &agent, Extra: map[string]any{
					"intent":           "test",
					"tdd_not_required": "cosmetic-only change",
				}},
			},
			agentID: "coder-1",
			want:    "cosmetic-only change",
		},
		{
			name: "checkpoint from different agent ignored",
			history: []models.TaskHistoryEntry{
				{Event: models.TaskEventPreExecutionCheckpoint, Agent: &otherAgent, Extra: map[string]any{
					"tdd_not_required": "waiver from other",
				}},
			},
			agentID: "coder-1",
			want:    "",
		},
		{
			name: "latest checkpoint wins",
			history: []models.TaskHistoryEntry{
				{Event: models.TaskEventPreExecutionCheckpoint, Agent: &agent, Extra: map[string]any{
					"tdd_not_required": "old waiver",
				}},
				{Event: models.TaskEventPreExecutionCheckpoint, Agent: &agent, Extra: map[string]any{
					"intent": "new checkpoint without waiver",
				}},
			},
			agentID: "coder-1",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetTDDWaiver(tt.history, tt.agentID)
			if got != tt.want {
				t.Errorf("GetTDDWaiver() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWriteCheckpointImpact(t *testing.T) {
	t.Run("valid values accepted", func(t *testing.T) {
		validValues := []string{"", "standard", "significant", "architecture"}
		for _, impact := range validValues {
			t.Run("impact_"+impact, func(t *testing.T) {
				tmpDir := t.TempDir()
				stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

				now := time.Now().UTC()
				state := testhelpers.CreateValidState()
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
				}
				testhelpers.WriteInitialState(t, stateFile, state)

				err := WriteCheckpoint(tmpDir, &WriteCheckpointInput{
					TaskID:         "task-1",
					AgentID:        "coder-1",
					Intent:         "Implement feature",
					ValidationPlan: "go test ./...",
					FilesToModify:  []string{"main.go"},
					Impact:         impact,
				})
				if err != nil {
					t.Fatalf("WriteCheckpoint failed for impact %q: %v", impact, err)
				}

				bb := db.New(stateFile)
				readState, err := bb.Read()
				if err != nil {
					t.Fatalf("Failed to read state: %v", err)
				}

				task := readState.FindTask("task-1")
				lastEntry := task.History[len(task.History)-1]

				if impact == "" {
					// Empty impact should not be stored in Extra
					if _, ok := lastEntry.Extra["impact"]; ok {
						t.Error("Expected no impact key when field is empty")
					}
				} else {
					val, ok := lastEntry.Extra["impact"].(string)
					if !ok {
						t.Fatalf("Expected impact in Extra, got %v", lastEntry.Extra["impact"])
					}
					if val != impact {
						t.Errorf("Expected impact %q, got %q", impact, val)
					}
				}
			})
		}
	})

	t.Run("invalid values rejected", func(t *testing.T) {
		invalidValues := []string{"critical", "high", "low", "STANDARD", "Architecture"}
		for _, impact := range invalidValues {
			t.Run("impact_"+impact, func(t *testing.T) {
				err := WriteCheckpoint("/nonexistent", &WriteCheckpointInput{
					TaskID:         "task-1",
					AgentID:        "coder-1",
					Intent:         "test",
					ValidationPlan: "test",
					FilesToModify:  []string{"f"},
					Impact:         impact,
				})
				testhelpers.RequireErrorContains(t, err, "invalid impact value")
			})
		}
	})
}

func TestGetCheckpointImpact(t *testing.T) {
	agent := "coder-1"
	otherAgent := "coder-2"

	tests := []struct {
		name    string
		history []models.TaskHistoryEntry
		agentID string
		want    string
	}{
		{
			name:    "empty history",
			history: nil,
			agentID: "coder-1",
			want:    "",
		},
		{
			name: "checkpoint without impact",
			history: []models.TaskHistoryEntry{
				{Event: models.TaskEventPreExecutionCheckpoint, Agent: &agent, Extra: map[string]any{"intent": "test"}},
			},
			agentID: "coder-1",
			want:    "",
		},
		{
			name: "checkpoint with impact",
			history: []models.TaskHistoryEntry{
				{Event: models.TaskEventPreExecutionCheckpoint, Agent: &agent, Extra: map[string]any{
					"intent": "test",
					"impact": "significant",
				}},
			},
			agentID: "coder-1",
			want:    "significant",
		},
		{
			name: "checkpoint from different agent ignored",
			history: []models.TaskHistoryEntry{
				{Event: models.TaskEventPreExecutionCheckpoint, Agent: &otherAgent, Extra: map[string]any{
					"impact": "architecture",
				}},
			},
			agentID: "coder-1",
			want:    "",
		},
		{
			name: "latest checkpoint wins",
			history: []models.TaskHistoryEntry{
				{Event: models.TaskEventPreExecutionCheckpoint, Agent: &agent, Extra: map[string]any{
					"impact": "architecture",
				}},
				{Event: models.TaskEventPreExecutionCheckpoint, Agent: &agent, Extra: map[string]any{
					"intent": "new checkpoint without impact",
				}},
			},
			agentID: "coder-1",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetCheckpointImpact(tt.history, tt.agentID)
			if got != tt.want {
				t.Errorf("GetCheckpointImpact() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHasCheckpoint(t *testing.T) {
	agent := "coder-1"
	otherAgent := "coder-2"

	tests := []struct {
		name    string
		history []models.TaskHistoryEntry
		agentID string
		want    bool
	}{
		{
			name:    "empty history",
			history: nil,
			agentID: "coder-1",
			want:    false,
		},
		{
			name: "no checkpoint events",
			history: []models.TaskHistoryEntry{
				{Event: "claimed", Agent: &agent},
			},
			agentID: "coder-1",
			want:    false,
		},
		{
			name: "checkpoint from different agent",
			history: []models.TaskHistoryEntry{
				{Event: models.TaskEventPreExecutionCheckpoint, Agent: &otherAgent},
			},
			agentID: "coder-1",
			want:    false,
		},
		{
			name: "checkpoint from correct agent",
			history: []models.TaskHistoryEntry{
				{Event: models.TaskEventPreExecutionCheckpoint, Agent: &agent},
			},
			agentID: "coder-1",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasCheckpoint(tt.history, tt.agentID)
			if got != tt.want {
				t.Errorf("HasCheckpoint() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetValidationPlan(t *testing.T) {
	agent := "coder-1"
	otherAgent := "coder-2"

	tests := []struct {
		name    string
		history []models.TaskHistoryEntry
		agentID string
		want    string
	}{
		{
			name:    "empty history",
			history: nil,
			agentID: "coder-1",
			want:    "",
		},
		{
			name: "checkpoint without validation_plan",
			history: []models.TaskHistoryEntry{
				{Event: models.TaskEventPreExecutionCheckpoint, Agent: &agent, Extra: map[string]any{"intent": "test"}},
			},
			agentID: "coder-1",
			want:    "",
		},
		{
			name: "checkpoint with validation_plan",
			history: []models.TaskHistoryEntry{
				{Event: models.TaskEventPreExecutionCheckpoint, Agent: &agent, Extra: map[string]any{
					"intent":          "test",
					"validation_plan": "run go test ./... and verify all pass",
				}},
			},
			agentID: "coder-1",
			want:    "run go test ./... and verify all pass",
		},
		{
			name: "checkpoint from different agent ignored",
			history: []models.TaskHistoryEntry{
				{Event: models.TaskEventPreExecutionCheckpoint, Agent: &otherAgent, Extra: map[string]any{
					"validation_plan": "plan from other agent",
				}},
			},
			agentID: "coder-1",
			want:    "",
		},
		{
			name: "latest checkpoint wins",
			history: []models.TaskHistoryEntry{
				{Event: models.TaskEventPreExecutionCheckpoint, Agent: &agent, Extra: map[string]any{
					"validation_plan": "old plan",
				}},
				{Event: models.TaskEventPreExecutionCheckpoint, Agent: &agent, Extra: map[string]any{
					"intent":          "new checkpoint",
					"validation_plan": "new plan",
				}},
			},
			agentID: "coder-1",
			want:    "new plan",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetValidationPlan(tt.history, tt.agentID)
			if got != tt.want {
				t.Errorf("GetValidationPlan() = %q, want %q", got, tt.want)
			}
		})
	}
}
