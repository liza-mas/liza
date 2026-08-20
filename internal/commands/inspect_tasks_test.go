package commands

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/render"
)

func TestInspectTasks(t *testing.T) {
	// Create test state with various tasks
	now := time.Now()
	assignedTo := "coder-1"
	blockedReason := "waiting for input"

	state := &models.State{
		Tasks: []models.Task{
			{
				ID:          "task-1",
				Description: "Implement feature A",
				Status:      models.TaskStatusImplementing,
				Priority:    1,
				AssignedTo:  &assignedTo,
				Created:     now.Add(-2 * time.Hour),
				History: []models.TaskHistoryEntry{
					{Time: now.Add(-1 * time.Hour), Event: models.TaskEventClaimed},
				},
			},
			{
				ID:            "task-2",
				Description:   "Fix bug B",
				Status:        models.TaskStatusBlocked,
				Priority:      2,
				BlockedReason: &blockedReason,
				Created:       now.Add(-24 * time.Hour),
				History: []models.TaskHistoryEntry{
					{Time: now.Add(-23 * time.Hour), Event: models.TaskEventBlocked},
				},
			},
			{
				ID:          "task-3",
				Description: "Add tests C",
				Status:      models.TaskStatusMerged,
				Priority:    3,
				Created:     now.Add(-48 * time.Hour),
			},
			{
				ID:          "task-4",
				Description: "Unclaimed task",
				Status:      models.TaskStatusReady,
				Priority:    4,
				Created:     now.Add(-1 * time.Hour),
			},
		},
	}

	tests := []struct {
		name       string
		opts       inspectTasksOptions
		wantCount  int
		wantIDs    []string
		wantFormat string // "json", "yaml", "table", or ""
		wantErr    bool
	}{
		{
			name:       "list all tasks",
			opts:       inspectTasksOptions{},
			wantCount:  4,
			wantIDs:    []string{"task-1", "task-2", "task-3", "task-4"},
			wantFormat: "table",
		},
		{
			name: "filter by status IMPLEMENTING",
			opts: inspectTasksOptions{
				StatusFilter: string(models.TaskStatusImplementing),
			},
			wantCount:  1,
			wantIDs:    []string{"task-1"},
			wantFormat: "table",
		},
		{
			name: "filter by status BLOCKED",
			opts: inspectTasksOptions{
				StatusFilter: string(models.TaskStatusBlocked),
			},
			wantCount:  1,
			wantIDs:    []string{"task-2"},
			wantFormat: "table",
		},
		{
			name: "filter by assigned_to",
			opts: inspectTasksOptions{
				AssignedToFilter: "coder-1",
			},
			wantCount:  1,
			wantIDs:    []string{"task-1"},
			wantFormat: "table",
		},
		{
			name: "filter by blocked=true",
			opts: inspectTasksOptions{
				BlockedFilter: true,
			},
			wantCount:  1,
			wantIDs:    []string{"task-2"},
			wantFormat: "table",
		},
		{
			name: "JSON format",
			opts: inspectTasksOptions{
				Format: "json",
			},
			wantCount:  4,
			wantFormat: "json",
		},
		{
			name: "YAML format",
			opts: inspectTasksOptions{
				Format: "yaml",
			},
			wantCount:  4,
			wantFormat: "yaml",
		},
		{
			name: "internal flag returns structured data",
			opts: inspectTasksOptions{
				Internal: true,
			},
			wantCount:  4,
			wantFormat: "internal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := inspectTasks(state, tt.opts)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			// Validate result based on format
			switch tt.wantFormat {
			case "internal":
				// Should return []taskInfo
				tasks, ok := result.([]taskInfo)
				if !ok {
					t.Errorf("expected []taskInfo, got %T", result)
					return
				}
				if len(tasks) != tt.wantCount {
					t.Errorf("expected %d tasks, got %d", tt.wantCount, len(tasks))
				}
				// Check IDs if specified
				if tt.wantIDs != nil {
					for i, id := range tt.wantIDs {
						if tasks[i].ID != id {
							t.Errorf("expected task %d to be %s, got %s", i, id, tasks[i].ID)
						}
					}
				}
			case "json":
				output, ok := result.(string)
				if !ok {
					t.Errorf("expected string output, got %T", result)
					return
				}
				// Validate JSON
				var tasks []taskInfo
				if err := json.Unmarshal([]byte(output), &tasks); err != nil {
					t.Errorf("invalid JSON output: %v", err)
				}
				if len(tasks) != tt.wantCount {
					t.Errorf("expected %d tasks in JSON, got %d", tt.wantCount, len(tasks))
				}
			case "yaml":
				output, ok := result.(string)
				if !ok {
					t.Errorf("expected string output, got %T", result)
					return
				}
				// Just check it's not empty
				if output == "" {
					t.Errorf("expected non-empty YAML output")
				}
			case "table":
				output, ok := result.(string)
				if !ok {
					t.Errorf("expected string output, got %T", result)
					return
				}
				// Check that all expected IDs appear in output
				for _, id := range tt.wantIDs {
					if !strings.Contains(output, id) {
						t.Errorf("expected output to contain %s", id)
					}
				}
			}
		})
	}
}

func TestInspectTasksSummaryActive(t *testing.T) {
	now := time.Now()
	assignedTo := "coder-1"
	blockedReason := "waiting for input"
	rejectionReason := "needs a narrower scope"

	state := &models.State{
		Tasks: []models.Task{
			{
				ID:                  "task-1",
				Description:         "Implement feature A",
				RolePair:            "coding-pair",
				Status:              models.TaskStatusImplementing,
				Priority:            1,
				AssignedTo:          &assignedTo,
				Attempt:             2,
				ReviewCyclesCurrent: 1,
				Created:             now,
				DoneWhen:            strings.Repeat("verbose done when ", 20),
				Scope:               strings.Repeat("verbose scope ", 20),
				Decomposition:       inspectTasksTestDecomposition(),
				Output: []models.OutputEntry{
					{Kind: "code-task", Desc: "child 1"},
					{Kind: "code-task", Desc: "child 2"},
					{Kind: "review-task", Desc: "child 3"},
				},
			},
			{
				ID:               "task-2",
				Description:      "Blocked task",
				Status:           models.TaskStatusBlocked,
				Priority:         2,
				BlockedReason:    &blockedReason,
				BlockedQuestions: []string{"Which API?"},
				Created:          now,
			},
			{
				ID:              "task-3",
				Description:     "Rejected task",
				Status:          models.TaskStatusRejected,
				Priority:        3,
				RejectionReason: &rejectionReason,
				FailedBy:        []string{"integration-analyst-1"},
				Created:         now,
			},
			{
				ID:          "task-4",
				Description: "Merged task",
				Status:      models.TaskStatusMerged,
				Priority:    4,
				Created:     now,
			},
		},
	}

	result, err := inspectTasks(state, inspectTasksOptions{
		Format:  "json",
		Summary: true,
		Active:  true,
	})
	if err != nil {
		t.Fatalf("inspectTasks() error = %v", err)
	}

	output := result.(string)
	var summaries []taskSummaryInfo
	if err := json.Unmarshal([]byte(output), &summaries); err != nil {
		t.Fatalf("summary JSON invalid: %v", err)
	}

	if len(summaries) != 3 {
		t.Fatalf("summary count = %d, want 3 active tasks", len(summaries))
	}
	if summaries[0].ID != "task-1" || summaries[0].RolePair != "coding-pair" {
		t.Fatalf("first summary = %+v, want task-1 coding-pair", summaries[0])
	}
	if summaries[0].Attempt != 2 {
		t.Errorf("attempt = %d, want 2", summaries[0].Attempt)
	}
	if summaries[0].OutputCount != 3 {
		t.Errorf("output_count = %d, want 3", summaries[0].OutputCount)
	}
	if got := strings.Join(summaries[0].OutputKinds, ","); got != "code-task,review-task" {
		t.Errorf("output_kinds = %q, want code-task,review-task", got)
	}
	if summaries[1].BlockedReason == nil || *summaries[1].BlockedReason != blockedReason {
		t.Errorf("blocked_reason = %v, want %q", summaries[1].BlockedReason, blockedReason)
	}
	if summaries[2].RejectionReason == nil || *summaries[2].RejectionReason != rejectionReason {
		t.Errorf("rejection_reason = %v, want %q", summaries[2].RejectionReason, rejectionReason)
	}
	if strings.Contains(output, "verbose done when") || strings.Contains(output, "verbose scope") {
		t.Errorf("summary output leaked verbose fields:\n%s", output)
	}
	var raw []map[string]any
	if err := json.Unmarshal([]byte(output), &raw); err != nil {
		t.Fatalf("summary JSON invalid on raw decode: %v", err)
	}
	if _, exists := raw[0]["output"]; exists {
		t.Errorf("summary output includes full output field: %v", raw[0])
	}
	if _, exists := raw[0]["decomposition"]; exists {
		t.Errorf("summary output includes decomposition: %v", raw[0])
	}
}

func TestInspectTasksActiveUsesOperationalTerminalStates(t *testing.T) {
	state := &models.State{Tasks: []models.Task{
		{ID: "clean", RolePair: "integration-pair", Status: "INTEGRATION_ANALYSIS_CLEAN"},
		{ID: "approved", RolePair: "integration-pair", Status: "INTEGRATION_ANALYSIS_APPROVED"},
	}}

	result, err := inspectTasks(state, inspectTasksOptions{
		Internal:         true,
		Summary:          true,
		Active:           true,
		PipelineResolver: operationalTerminalResolver(),
	})
	if err != nil {
		t.Fatalf("inspectTasks() error = %v", err)
	}
	summaries := result.([]taskSummaryInfo)
	if len(summaries) != 1 || summaries[0].ID != "approved" {
		t.Fatalf("active summaries = %#v, want approved transition source only", summaries)
	}
}

func TestInspectTasksFullTaskDecomposition(t *testing.T) {
	state := &models.State{
		Tasks: []models.Task{{
			ID:            "task-with-decomposition",
			Description:   "Task description",
			Status:        models.TaskStatusMerged,
			Priority:      1,
			Created:       time.Now(),
			Decomposition: inspectTasksTestDecomposition(),
		}},
	}

	result, err := inspectTask(state, "task-with-decomposition", inspectTasksOptions{
		Format: "json",
	})
	if err != nil {
		t.Fatalf("inspectTask() error = %v", err)
	}

	var task map[string]any
	if err := json.Unmarshal([]byte(result.(string)), &task); err != nil {
		t.Fatalf("task JSON invalid: %v", err)
	}

	decomposition, ok := task["decomposition"].(map[string]any)
	if !ok {
		t.Fatalf("decomposition = %T, want object: %v", task["decomposition"], task["decomposition"])
	}
	if got := decomposition["coverage_notes"]; got != "covers the inspect projection boundary" {
		t.Errorf("coverage_notes = %v, want inspect coverage note", got)
	}
	ownedFiles, ok := decomposition["owned_files"].([]any)
	if !ok || len(ownedFiles) != 1 || ownedFiles[0] != "internal/commands/inspect_tasks.go" {
		t.Errorf("owned_files = %v, want inspect_tasks.go", decomposition["owned_files"])
	}
	interfacesOwned, ok := decomposition["interfaces_owned"].([]any)
	if !ok || len(interfacesOwned) != 1 || interfacesOwned[0] != "inspect-task-json" {
		t.Errorf("interfaces_owned = %v, want inspect-task-json", decomposition["interfaces_owned"])
	}
}

func TestInspectTasksOutputSummary(t *testing.T) {
	now := time.Now()
	worktree := "/tmp/worktree"
	mergeCommit := "abc123"

	state := &models.State{
		Tasks: []models.Task{
			{
				ID:          "task-with-output",
				Description: "Parent description should not appear",
				RolePair:    "planning-pair",
				Status:      models.TaskStatusMerged,
				Priority:    1,
				Created:     now,
				DoneWhen:    "Parent done_when should not appear",
				Scope:       "Parent scope should not appear",
				SpecRef:     "specs/parent.md",
				Worktree:    &worktree,
				MergeCommit: &mergeCommit,
				IntegrationFailure: map[string]any{
					"stderr": "verbose failure should not appear",
				},
				Output: []models.OutputEntry{
					{
						Desc:    "Prepare API foundation",
						SpecRef: "specs/foundation.md",
						Kind:    "code-task",
					},
					{
						Desc:          "Implement API",
						DoneWhen:      "Child done_when should not appear",
						Scope:         "Child scope should not appear",
						SpecRef:       "specs/api.md",
						EpicRef:       "specs/epics/api.md#api",
						PlanRef:       "specs/plans/api.md",
						ArchRef:       "specs/arch-plan/api.md",
						Kind:          "code-task",
						Validation:    []string{"make test ./internal/api"},
						DestructiveDB: true,
						DependsOn:     []string{"0"},
						TaskDependsOn: []string{"existing-task"},
						Decomposition: inspectTasksTestDecomposition(),
					},
				},
			},
		},
	}

	result, err := inspectTasks(state, inspectTasksOptions{
		Format:        "json",
		OutputSummary: true,
	})
	if err != nil {
		t.Fatalf("inspectTasks() error = %v", err)
	}

	output := result.(string)
	var summaries []map[string]any
	if err := json.Unmarshal([]byte(output), &summaries); err != nil {
		t.Fatalf("output summary JSON invalid: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("summary count = %d, want 1", len(summaries))
	}

	task := summaries[0]
	for _, key := range []string{"description", "done_when", "scope", "spec_ref", "worktree", "merge_commit", "integration_failure"} {
		if _, exists := task[key]; exists {
			t.Fatalf("output summary includes parent blob %q: %v", key, task)
		}
	}
	if task["id"] != "task-with-output" || task["status"] != string(models.TaskStatusMerged) || task["role_pair"] != "planning-pair" {
		t.Fatalf("unexpected task envelope: %v", task)
	}

	entries, ok := task["output"].([]any)
	if !ok || len(entries) != 2 {
		t.Fatalf("output entries = %T %v, want two entries", task["output"], task["output"])
	}
	entry, ok := entries[1].(map[string]any)
	if !ok {
		t.Fatalf("output entry = %T, want object", entries[1])
	}
	if entry["index"] != float64(1) {
		t.Errorf("index = %v, want 1", entry["index"])
	}
	for key, want := range map[string]string{
		"desc":     "Implement API",
		"kind":     "code-task",
		"spec_ref": "specs/api.md",
		"epic_ref": "specs/epics/api.md#api",
		"plan_ref": "specs/plans/api.md",
		"arch_ref": "specs/arch-plan/api.md",
	} {
		if entry[key] != want {
			t.Errorf("%s = %v, want %q", key, entry[key], want)
		}
	}
	if _, exists := entry["done_when"]; exists {
		t.Fatalf("output summary includes child done_when: %v", entry)
	}
	if _, exists := entry["scope"]; exists {
		t.Fatalf("output summary includes child scope: %v", entry)
	}
	if got := entry["depends_on"].([]any); len(got) != 1 || got[0] != "0" {
		t.Errorf("depends_on = %v, want [0]", got)
	}
	if got := entry["task_depends_on"].([]any); len(got) != 1 || got[0] != "existing-task" {
		t.Errorf("task_depends_on = %v, want [existing-task]", got)
	}
	if got := entry["validation"].([]any); len(got) != 1 || got[0] != "make test ./internal/api" {
		t.Errorf("validation = %v, want [make test ./internal/api]", got)
	}
	if got := entry["destructive_db"]; got != true {
		t.Errorf("destructive_db = %v, want true", got)
	}
	decomposition, ok := entry["decomposition"].(map[string]any)
	if !ok {
		t.Fatalf("decomposition = %T, want object: %v", entry["decomposition"], entry["decomposition"])
	}
	if got := decomposition["coverage_notes"]; got != "covers the inspect projection boundary" {
		t.Errorf("coverage_notes = %v, want inspect coverage note", got)
	}
	ownedFiles, ok := decomposition["owned_files"].([]any)
	if !ok || len(ownedFiles) != 1 || ownedFiles[0] != "internal/commands/inspect_tasks.go" {
		t.Errorf("owned_files = %v, want inspect_tasks.go", decomposition["owned_files"])
	}
	if strings.Contains(output, "Parent done_when") || strings.Contains(output, "Parent scope") ||
		strings.Contains(output, "Child done_when") || strings.Contains(output, "Child scope") {
		t.Fatalf("output summary leaked verbose blobs:\n%s", output)
	}
}

func inspectTasksTestDecomposition() *models.DecompositionManifest {
	return &models.DecompositionManifest{
		OwnedFiles:            []string{"internal/commands/inspect_tasks.go"},
		OwnedModules:          []string{"internal/commands"},
		ReadOnlyDependsOn:     []int{0},
		ReadOnlyTaskDependsOn: []string{"architecture-4-code-planning-0-a-coding-0-repair-2"},
		InterfacesOwned:       []string{"inspect-task-json"},
		InterfacesConsumed:    []string{"task-model"},
		CoverageNotes:         "covers the inspect projection boundary",
	}
}

func TestInspectTasksOutputSummaryEmptyOutput(t *testing.T) {
	state := &models.State{
		Tasks: []models.Task{{
			ID:          "task-without-output",
			Description: "No output yet",
			Status:      models.TaskStatusMerged,
			Created:     time.Now(),
		}},
	}

	result, err := inspectTask(state, "task-without-output", inspectTasksOptions{
		Format:        "json",
		OutputSummary: true,
	})
	if err != nil {
		t.Fatalf("inspectTask() error = %v", err)
	}

	output := result.(string)
	var summary map[string]any
	if err := json.Unmarshal([]byte(output), &summary); err != nil {
		t.Fatalf("output summary JSON invalid: %v", err)
	}
	entries, ok := summary["output"].([]any)
	if !ok {
		t.Fatalf("output field = %T, want empty array", summary["output"])
	}
	if len(entries) != 0 {
		t.Fatalf("output entries = %v, want empty array", entries)
	}

	listResult, err := inspectTasks(state, inspectTasksOptions{
		Format:        "json",
		OutputSummary: true,
	})
	if err != nil {
		t.Fatalf("inspectTasks() error = %v", err)
	}
	var listSummary []map[string]any
	if err := json.Unmarshal([]byte(listResult.(string)), &listSummary); err != nil {
		t.Fatalf("list output summary JSON invalid: %v", err)
	}
	listEntries, ok := listSummary[0]["output"].([]any)
	if !ok || len(listEntries) != 0 {
		t.Fatalf("list output entries = %T %v, want empty array", listSummary[0]["output"], listSummary[0]["output"])
	}
}

func TestInspectTask(t *testing.T) {
	now := time.Now()
	assignedTo := "coder-1"
	blockedReason := "waiting for approval"

	state := &models.State{
		Tasks: []models.Task{
			{
				ID:          "task-1",
				Description: "Implement feature A",
				Status:      models.TaskStatusImplementing,
				Priority:    1,
				AssignedTo:  &assignedTo,
				Created:     now.Add(-2 * time.Hour),
				History: []models.TaskHistoryEntry{
					{Time: now.Add(-30 * time.Minute), Event: models.TaskEventCreated},
					{Time: now.Add(-1 * time.Hour), Event: models.TaskEventClaimed},
				},
			},
			{
				ID:            "task-2",
				Description:   "Blocked task",
				Status:        models.TaskStatusBlocked,
				Priority:      2,
				BlockedReason: &blockedReason,
				Created:       now.Add(-5 * time.Hour),
				History: []models.TaskHistoryEntry{
					{Time: now.Add(-5 * time.Hour), Event: models.TaskEventCreated},
					{Time: now.Add(-4 * time.Hour), Event: models.TaskEventClaimed},
					{Time: now.Add(-2 * time.Hour), Event: models.TaskEventBlocked},
				},
			},
		},
	}

	tests := []struct {
		name         string
		taskID       string
		opts         inspectTasksOptions
		wantTaskID   string
		wantErr      bool
		wantNotFound bool
	}{
		{
			name:       "get task by ID",
			taskID:     "task-1",
			opts:       inspectTasksOptions{},
			wantTaskID: "task-1",
		},
		{
			name:       "get task with JSON format",
			taskID:     "task-1",
			opts:       inspectTasksOptions{Format: "json"},
			wantTaskID: "task-1",
		},
		{
			name:       "get task with YAML format",
			taskID:     "task-2",
			opts:       inspectTasksOptions{Format: "yaml"},
			wantTaskID: "task-2",
		},
		{
			name:       "get task with value format",
			taskID:     "task-1",
			opts:       inspectTasksOptions{Format: "value"},
			wantTaskID: "task-1",
		},
		{
			name:         "task not found",
			taskID:       "nonexistent",
			opts:         inspectTasksOptions{},
			wantErr:      true,
			wantNotFound: true,
		},
		{
			name:       "internal flag returns taskInfo",
			taskID:     "task-1",
			opts:       inspectTasksOptions{Internal: true},
			wantTaskID: "task-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := inspectTask(state, tt.taskID, tt.opts)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
					return
				}
				if tt.wantNotFound && !errors.IsNotFound(err) {
					t.Errorf("expected NotFoundError, got %T: %v", err, err)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			// Validate result based on format
			if tt.opts.Internal {
				taskInfo, ok := result.(taskInfo)
				if !ok {
					t.Errorf("expected taskInfo, got %T", result)
					return
				}
				if taskInfo.ID != tt.wantTaskID {
					t.Errorf("expected task ID %s, got %s", tt.wantTaskID, taskInfo.ID)
				}
				// Verify computed fields are present
				if taskInfo.Age == "" {
					t.Errorf("expected Age to be computed")
				}
				if taskInfo.TimeInStatus == "" {
					t.Errorf("expected TimeInStatus to be computed")
				}
			} else {
				output, ok := result.(string)
				if !ok {
					t.Errorf("expected string output, got %T", result)
					return
				}
				// Check output contains task ID
				if !strings.Contains(output, tt.wantTaskID) {
					t.Errorf("expected output to contain task ID %s", tt.wantTaskID)
				}
			}
		})
	}
}

func TestTaskInfo_ComputedFields(t *testing.T) {
	now := time.Now()
	assignedTo := "coder-1"

	task := models.Task{
		ID:          "task-1",
		Description: "Test task",
		Status:      models.TaskStatusImplementing,
		Priority:    1,
		AssignedTo:  &assignedTo,
		Created:     now.Add(-2 * time.Hour),
		History: []models.TaskHistoryEntry{
			{Time: now.Add(-2 * time.Hour), Event: models.TaskEventCreated},
			{Time: now.Add(-1 * time.Hour), Event: models.TaskEventClaimed},
		},
	}

	info := buildTaskInfo(&task, "")

	// Check that computed fields are set
	if info.Age == "" {
		t.Errorf("expected Age to be set")
	}
	if info.TimeInStatus == "" {
		t.Errorf("expected TimeInStatus to be set")
	}

	// Age should be approximately 2 hours
	if !strings.Contains(info.Age, "2h") {
		t.Errorf("expected Age to contain '2h', got %s", info.Age)
	}

	// TimeInStatus should be approximately 1 hour (time since "claimed" event)
	if !strings.Contains(info.TimeInStatus, "1h") {
		t.Errorf("expected TimeInStatus to contain '1h', got %s", info.TimeInStatus)
	}
}

func TestBuildTaskInfo_IncludesBlockedRepairRequest(t *testing.T) {
	reason := "Required state repair is orchestrator-only"
	task := models.Task{
		ID:               "task-repair",
		Description:      "Repair missing parent",
		Status:           models.TaskStatusBlocked,
		Priority:         1,
		BlockedReason:    &reason,
		BlockedQuestions: []string{"Can the orchestrator restore architecture-2?"},
		RepairRequest: &models.RepairRequest{
			Operation:  "add-task",
			Target:     "architecture-2",
			Command:    "liza add-task --id architecture-2 --agent-id orchestrator-1 --json",
			Evidence:   []string{`command requires role type [orchestrator] but agent "coder-1" has type "doer"`},
			Validation: []string{"python -m pytest -q tests/backend/test_workflow_contract.py -q"},
		},
		Created: time.Now().UTC(),
	}

	info := buildTaskInfo(&task, "")

	if len(info.BlockedQuestions) != 1 {
		t.Fatalf("BlockedQuestions len = %d, want 1", len(info.BlockedQuestions))
	}
	if info.RepairRequest == nil {
		t.Fatal("RepairRequest is nil")
	}
	if info.RepairRequest.Operation != "add-task" {
		t.Fatalf("RepairRequest.Operation = %q, want add-task", info.RepairRequest.Operation)
	}
	if info.RepairRequest.Target != "architecture-2" {
		t.Fatalf("RepairRequest.Target = %q, want architecture-2", info.RepairRequest.Target)
	}
}

func TestTaskInfo_MultipleFilters(t *testing.T) {
	now := time.Now()
	assignedTo1 := "coder-1"
	assignedTo2 := "coder-2"
	blockedReason := "waiting"

	state := &models.State{
		Tasks: []models.Task{
			{
				ID:         "task-1",
				Status:     models.TaskStatusImplementing,
				AssignedTo: &assignedTo1,
				Created:    now,
			},
			{
				ID:         "task-2",
				Status:     models.TaskStatusImplementing,
				AssignedTo: &assignedTo2,
				Created:    now,
			},
			{
				ID:            "task-3",
				Status:        models.TaskStatusBlocked,
				AssignedTo:    &assignedTo1,
				BlockedReason: &blockedReason,
				Created:       now,
			},
		},
	}

	// Filter by status AND assigned_to
	opts := inspectTasksOptions{
		StatusFilter:     string(models.TaskStatusImplementing),
		AssignedToFilter: "coder-1",
		Internal:         true,
	}

	result, err := inspectTasks(state, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tasks := result.([]taskInfo)
	if len(tasks) != 1 {
		t.Errorf("expected 1 task, got %d", len(tasks))
	}
	if len(tasks) > 0 && tasks[0].ID != "task-1" {
		t.Errorf("expected task-1, got %s", tasks[0].ID)
	}
}

func TestCalculateTimeInStatus(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name             string
		task             *models.Task
		expectedContains string // what the output should contain
	}{
		{
			name: "time since claimed",
			task: &models.Task{
				Status:  models.TaskStatusImplementing,
				Created: now.Add(-5 * time.Hour),
				History: []models.TaskHistoryEntry{
					{Time: now.Add(-5 * time.Hour), Event: models.TaskEventCreated},
					{Time: now.Add(-2 * time.Hour), Event: models.TaskEventClaimed},
				},
			},
			expectedContains: "2h",
		},
		{
			name: "time since blocked",
			task: &models.Task{
				Status:  models.TaskStatusBlocked,
				Created: now.Add(-10 * time.Hour),
				History: []models.TaskHistoryEntry{
					{Time: now.Add(-10 * time.Hour), Event: models.TaskEventCreated},
					{Time: now.Add(-8 * time.Hour), Event: models.TaskEventClaimed},
					{Time: now.Add(-3 * time.Hour), Event: models.TaskEventBlocked},
				},
			},
			expectedContains: "3h",
		},
		{
			name: "no history - use created time",
			task: &models.Task{
				Status:  models.TaskStatusReady,
				Created: now.Add(-1 * time.Hour),
				History: []models.TaskHistoryEntry{},
			},
			expectedContains: "1h",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			duration := calculateTimeInStatus(tt.task)
			formatted := render.FormatDuration(duration)
			if !strings.Contains(formatted, tt.expectedContains) {
				t.Errorf("expected duration to contain '%s', got '%s'", tt.expectedContains, formatted)
			}
		})
	}
}

func TestTaskInfo_DependenciesField(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name          string
		task          *models.Task
		wantDepsCount int
		wantDepsIDs   []string
	}{
		{
			name: "task with no dependencies",
			task: &models.Task{
				ID:        "task-1",
				Status:    models.TaskStatusReady,
				Created:   now,
				DependsOn: nil,
			},
			wantDepsCount: 0,
			wantDepsIDs:   nil,
		},
		{
			name: "task with empty dependencies",
			task: &models.Task{
				ID:        "task-2",
				Status:    models.TaskStatusReady,
				Created:   now,
				DependsOn: []string{},
			},
			wantDepsCount: 0,
			wantDepsIDs:   []string{},
		},
		{
			name: "task with single dependency",
			task: &models.Task{
				ID:        "task-3",
				Status:    models.TaskStatusReady,
				Created:   now,
				DependsOn: []string{"task-1"},
			},
			wantDepsCount: 1,
			wantDepsIDs:   []string{"task-1"},
		},
		{
			name: "task with multiple dependencies",
			task: &models.Task{
				ID:        "task-4",
				Status:    models.TaskStatusReady,
				Created:   now,
				DependsOn: []string{"task-1", "task-2", "task-3"},
			},
			wantDepsCount: 3,
			wantDepsIDs:   []string{"task-1", "task-2", "task-3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := buildTaskInfo(tt.task, "")

			if len(info.DependsOn) != tt.wantDepsCount {
				t.Errorf("expected %d dependencies, got %d", tt.wantDepsCount, len(info.DependsOn))
			}

			if tt.wantDepsIDs != nil {
				for i, id := range tt.wantDepsIDs {
					if info.DependsOn[i] != id {
						t.Errorf("expected dependency %d to be %s, got %s", i, id, info.DependsOn[i])
					}
				}
			}
		})
	}
}

func TestFormatTasksTable_WithDependencies(t *testing.T) {
	tasks := []taskInfo{
		{
			ID:           "task-1",
			Description:  "No dependencies",
			Status:       "DRAFT_CODE",
			Priority:     1,
			DependsOn:    nil,
			Age:          "1h ago",
			TimeInStatus: "1h ago",
			AttemptNum:   1,
		},
		{
			ID:           "task-2",
			Description:  "Single dependency",
			Status:       "DRAFT_CODE",
			Priority:     2,
			DependsOn:    []string{"task-1"},
			Age:          "30m ago",
			TimeInStatus: "30m ago",
			AttemptNum:   1,
		},
		{
			ID:           "task-3",
			Description:  "Multiple dependencies",
			Status:       "DRAFT_CODE",
			Priority:     3,
			DependsOn:    []string{"task-1", "task-2"},
			Age:          "15m ago",
			TimeInStatus: "15m ago",
			AttemptNum:   2,
			Iteration:    3,
		},
	}

	output := formatTasksTable(tasks)

	// Check header includes ATTEMPT and DEPS columns
	if !strings.Contains(output, "ATTEMPT") {
		t.Errorf("expected table header to contain 'ATTEMPT'")
	}
	if !strings.Contains(output, "DEPS") {
		t.Errorf("expected table header to contain 'DEPS'")
	}

	// Check attempt values appear in output
	if !strings.Contains(output, "2.3") {
		t.Errorf("expected output to contain '2.3' for task-3 (attempt 2, round 3)")
	}

	// Check dependency counts appear in output
	if !strings.Contains(output, "-") { // task with no deps should show "-"
		t.Errorf("expected output to contain '-' for no dependencies")
	}
	if !strings.Contains(output, "1") { // task with 1 dep should show "1"
		t.Errorf("expected output to contain '1' for single dependency")
	}
	if !strings.Contains(output, "2") { // task with 2 deps should show "2"
		t.Errorf("expected output to contain '2' for multiple dependencies")
	}
}

func TestBuildTaskInfo_AttemptNum_UsesEffectiveAttempt(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name           string
		attempt        int
		wantAttemptNum int
	}{
		{
			name:           "Attempt 2 returns 2",
			attempt:        2,
			wantAttemptNum: 2,
		},
		{
			name:           "Attempt 0 (legacy/unset) returns 1 via EffectiveAttempt",
			attempt:        0,
			wantAttemptNum: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := &models.Task{
				ID:        "test-task",
				Status:    models.TaskStatusImplementing,
				Created:   now,
				Attempt:   tt.attempt,
				Iteration: 3,
			}
			info := buildTaskInfo(task, "")
			if info.AttemptNum != tt.wantAttemptNum {
				t.Errorf("AttemptNum = %d, want %d", info.AttemptNum, tt.wantAttemptNum)
			}
		})
	}
}

func TestFormatTaskValue_WithDependencies(t *testing.T) {
	tests := []struct {
		name             string
		task             taskInfo
		expectContains   []string
		notExpectContain []string
	}{
		{
			name: "task with no dependencies",
			task: taskInfo{
				ID:           "task-1",
				Description:  "Test task",
				Status:       "DRAFT_CODE",
				Priority:     1,
				DependsOn:    nil,
				Age:          "1h ago",
				TimeInStatus: "1h ago",
			},
			expectContains: []string{
				"ID: task-1",
				"Dependencies: none",
			},
		},
		{
			name: "task with empty dependencies slice",
			task: taskInfo{
				ID:           "task-2",
				Description:  "Test task",
				Status:       "DRAFT_CODE",
				Priority:     1,
				DependsOn:    []string{},
				Age:          "1h ago",
				TimeInStatus: "1h ago",
			},
			expectContains: []string{
				"ID: task-2",
				"Dependencies: none",
			},
		},
		{
			name: "task with single dependency",
			task: taskInfo{
				ID:           "task-3",
				Description:  "Test task",
				Status:       "DRAFT_CODE",
				Priority:     1,
				DependsOn:    []string{"task-1"},
				Age:          "1h ago",
				TimeInStatus: "1h ago",
			},
			expectContains: []string{
				"ID: task-3",
				"Dependencies: task-1",
			},
			notExpectContain: []string{
				"Dependencies: none",
			},
		},
		{
			name: "task with canonical validation",
			task: taskInfo{
				ID:            "task-validation",
				Description:   "Test task",
				Status:        "DRAFT_CODE",
				Priority:      1,
				DoneWhen:      "Tests pass",
				Validation:    []string{"make test", "pre-commit run --files docs/USAGE.md"},
				DestructiveDB: true,
				Age:           "1h ago",
				TimeInStatus:  "1h ago",
			},
			expectContains: []string{
				"ID: task-validation",
				"Validation: make test; pre-commit run --files docs/USAGE.md",
				"Destructive DB: true",
			},
		},
		{
			name: "task with multiple dependencies",
			task: taskInfo{
				ID:           "task-4",
				Description:  "Test task",
				Status:       "DRAFT_CODE",
				Priority:     1,
				DependsOn:    []string{"task-1", "task-2", "task-3"},
				Age:          "1h ago",
				TimeInStatus: "1h ago",
			},
			expectContains: []string{
				"ID: task-4",
				"Dependencies: task-1, task-2, task-3",
			},
			notExpectContain: []string{
				"Dependencies: none",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := formatTaskValue(tt.task)

			for _, expected := range tt.expectContains {
				if !strings.Contains(output, expected) {
					t.Errorf("expected output to contain %q, but it didn't.\nOutput:\n%s", expected, output)
				}
			}

			for _, notExpected := range tt.notExpectContain {
				if strings.Contains(output, notExpected) {
					t.Errorf("expected output NOT to contain %q, but it did.\nOutput:\n%s", notExpected, output)
				}
			}
		})
	}
}

func TestFormatTaskOutputSummaryValue_IncludesDestructiveDB(t *testing.T) {
	task := taskOutputSummaryInfo{
		ID:       "task-with-output",
		Status:   "MERGED",
		RolePair: "planning-pair",
		Output: []outputEntrySummaryInfo{
			{
				Index:         1,
				Kind:          "code-task",
				SpecRef:       "specs/api.md",
				Validation:    []string{"LIZA_ALLOW_DESTRUCTIVE_DB=1 make test ./internal/api"},
				DestructiveDB: true,
				Desc:          "Implement API",
			},
		},
	}

	output := formatTaskOutputSummaryValue(task)
	for _, expected := range []string{
		"validation=LIZA_ALLOW_DESTRUCTIVE_DB=1 make test ./internal/api",
		"destructive_db=true",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output missing %q:\n%s", expected, output)
		}
	}
}

func TestInspectTasks_IncludesLatestIntegrationFailureDiagnostic(t *testing.T) {
	now := time.Now()
	state := &models.State{
		Tasks: []models.Task{
			{
				ID:          "task-1",
				Description: "Failed integration",
				Status:      models.TaskStatusIntegrationFailed,
				Priority:    1,
				Created:     now.Add(-time.Hour),
				History: []models.TaskHistoryEntry{
					{
						Time:  now.Add(-30 * time.Minute),
						Event: models.TaskEventIntegrationFailed,
						Extra: map[string]any{
							"diagnostic": map[string]any{
								"reason":        "merge conflict",
								"operation":     "wt-merge",
								"recovery_hint": "resolve the integration conflict",
							},
						},
					},
				},
			},
		},
	}

	result, err := inspectTasks(state, inspectTasksOptions{Internal: true})
	if err != nil {
		t.Fatalf("inspectTasks() error = %v", err)
	}
	infos := result.([]taskInfo)
	if len(infos) != 1 {
		t.Fatalf("len(infos) = %d, want 1", len(infos))
	}
	info := infos[0]
	if info.IntegrationFailure == nil {
		t.Fatal("IntegrationFailure is nil")
	}
	if info.IntegrationFailure["operation"] != "wt-merge" {
		t.Errorf("operation = %v, want wt-merge", info.IntegrationFailure["operation"])
	}
	if info.IntegrationFailure["recovery_hint"] == "" {
		t.Error("recovery_hint is empty")
	}
}

func TestInspectTasks_SynthesizesLegacyIntegrationFailureDiagnostic(t *testing.T) {
	tmpDir := t.TempDir()
	now := time.Now()
	worktree := ".worktrees/task-1"
	state := &models.State{
		Tasks: []models.Task{
			{
				ID:          "task-1",
				Description: "Legacy failed integration",
				Status:      models.TaskStatusIntegrationFailed,
				Priority:    1,
				Created:     now.Add(-time.Hour),
				Worktree:    &worktree,
			},
		},
	}

	result, err := inspectTasks(state, inspectTasksOptions{Internal: true, ProjectRoot: tmpDir})
	if err != nil {
		t.Fatalf("inspectTasks() error = %v", err)
	}
	infos := result.([]taskInfo)
	if len(infos) != 1 {
		t.Fatalf("len(infos) = %d, want 1", len(infos))
	}
	diagnostic := infos[0].IntegrationFailure
	if diagnostic == nil {
		t.Fatal("IntegrationFailure is nil")
	}
	if diagnostic["operation"] != "unknown" {
		t.Errorf("operation = %v, want unknown", diagnostic["operation"])
	}
	if diagnostic["worktree_state"] != "missing" {
		t.Errorf("worktree_state = %v, want missing", diagnostic["worktree_state"])
	}
	if diagnostic["worktree_missing"] != true {
		t.Errorf("worktree_missing = %v, want true", diagnostic["worktree_missing"])
	}
	hint, _ := diagnostic["recovery_hint"].(string)
	if !strings.Contains(hint, "reconcile-merged") {
		t.Errorf("recovery_hint = %q, want reconcile-merged guidance", hint)
	}
}

func TestInspectTasks_IncludesLatestPRURL(t *testing.T) {
	now := time.Now()
	state := &models.State{
		Tasks: []models.Task{
			{
				ID:          "task-1",
				Description: "Externally merged",
				Status:      models.TaskStatusMerged,
				Priority:    1,
				Created:     now.Add(-time.Hour),
				History: []models.TaskHistoryEntry{
					{
						Time:  now.Add(-30 * time.Minute),
						Event: models.TaskEventMerged,
						Extra: map[string]any{
							"pr_url": "https://github.com/example/repo/pull/414",
						},
					},
					{
						Time:  now.Add(-10 * time.Minute),
						Event: models.TaskEventBlocked,
						Extra: map[string]any{
							"pr_url": "not-an-integration-pr-url",
						},
					},
				},
			},
		},
	}

	result, err := inspectTasks(state, inspectTasksOptions{Internal: true})
	if err != nil {
		t.Fatalf("inspectTasks() error = %v", err)
	}
	infos := result.([]taskInfo)
	if len(infos) != 1 {
		t.Fatalf("len(infos) = %d, want 1", len(infos))
	}
	if infos[0].PRURL == nil || *infos[0].PRURL != "https://github.com/example/repo/pull/414" {
		t.Errorf("PRURL = %v, want PR URL", infos[0].PRURL)
	}
}
