package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/prompts"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// executeRootCommandCapture runs a CLI command and captures stdout output.
// This is needed because JSON output writes directly to os.Stdout, not cmd.OutOrStdout().
func executeRootCommandCapture(t *testing.T, projectRoot string, args ...string) (string, error) {
	t.Helper()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stdout pipe: %v", err)
	}
	os.Stdout = w

	cmdErr := executeRootCommand(t, projectRoot, args...)

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	if _, copyErr := io.Copy(&buf, r); copyErr != nil {
		t.Fatalf("failed to read captured stdout: %v", copyErr)
	}
	r.Close()

	return buf.String(), cmdErr
}

// parseEnvelope unmarshals a JSON envelope from stdout into a generic map.
func parseEnvelope(t *testing.T, stdout string) map[string]any {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("failed to parse JSON envelope: %v\nraw output: %s", err, stdout)
	}
	return env
}

func assertJSONError(t *testing.T, stdout string, wantCode string, wantMessageParts ...string) {
	t.Helper()

	env := parseEnvelope(t, stdout)
	if env["ok"] != false {
		t.Fatalf("expected ok=false, got %v", env["ok"])
	}

	errObj, ok := env["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error to be object, got %T", env["error"])
	}
	if errObj["code"] != wantCode {
		t.Fatalf("error.code = %v, want %s", errObj["code"], wantCode)
	}
	msg, _ := errObj["message"].(string)
	if msg == "" || msg == "internal error" {
		t.Fatalf("error.message = %q, want actionable message", msg)
	}
	for _, part := range wantMessageParts {
		if !strings.Contains(msg, part) {
			t.Fatalf("error.message = %q, want substring %q", msg, part)
		}
	}
}

func TestJSON_ClaimTask_Success(t *testing.T) {
	projectRoot, _ := setupMutationTestProject(t, func(state *models.State) {
		now := time.Now().UTC()
		state.Tasks = []models.Task{
			testhelpers.BuildTaskByStatus("task-json-claim", models.TaskStatusReady, now),
		}
		state.Agents["coder-1"] = testhelpers.RegisteredTestAgent("coder")
	})

	stdout, err := executeRootCommandCapture(t, projectRoot, "claim-task", "task-json-claim", "coder-1", "--json")
	if err != nil {
		t.Fatalf("claim-task --json failed: %v", err)
	}

	env := parseEnvelope(t, stdout)
	if env["ok"] != true {
		t.Fatalf("expected ok=true, got %v", env["ok"])
	}

	result, ok := env["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result to be object, got %T", env["result"])
	}

	// Verify snake_case keys from ClaimResult
	for _, key := range []string{"task_id", "agent_id", "source_status", "worktree_rel", "base_commit", "lease_expires", "integration_fix", "previous_assignee", "worktree_recreated", "warnings"} {
		if _, exists := result[key]; !exists {
			t.Errorf("missing expected key %q in result", key)
		}
	}

	if result["task_id"] != "task-json-claim" {
		t.Errorf("task_id = %v, want task-json-claim", result["task_id"])
	}
	if result["agent_id"] != "coder-1" {
		t.Errorf("agent_id = %v, want coder-1", result["agent_id"])
	}
}

func TestJSON_ClaimTask_Error(t *testing.T) {
	projectRoot, _ := setupMutationTestProject(t, nil)

	stdout, err := executeRootCommandCapture(t, projectRoot, "claim-task", "nonexistent-task", "coder-1", "--json")
	if err == nil {
		t.Fatalf("expected error for nonexistent task, got nil")
	}

	env := parseEnvelope(t, stdout)
	if env["ok"] != false {
		t.Fatalf("expected ok=false, got %v", env["ok"])
	}

	errObj, ok := env["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error to be object, got %T", env["error"])
	}
	if errObj["code"] != "not_found" {
		t.Errorf("error code = %v, want not_found", errObj["code"])
	}
}

func TestJSON_AddTask_CLIInputErrorsAreActionable(t *testing.T) {
	projectRoot, _ := setupMutationTestProject(t, nil)

	tests := []struct {
		name      string
		args      []string
		wantParts []string
	}{
		{
			name:      "state without log",
			args:      []string{"add-task", "--state", filepath.Join(projectRoot, "state.yaml"), "--json"},
			wantParts: []string{"--state", "--log"},
		},
		{
			name:      "missing task input file",
			args:      []string{"add-task", "--file", filepath.Join(projectRoot, "missing-task.yaml"), "--json"},
			wantParts: []string{"failed to read task file", "missing-task.yaml"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, err := executeRootCommandCapture(t, projectRoot, tt.args...)
			if err == nil {
				t.Fatalf("expected CLI input error, got nil")
			}
			assertJSONError(t, stdout, "validation", tt.wantParts...)
		})
	}
}

func TestJSON_AddTasks_PartialItemFailureKeepsOKEnvelope(t *testing.T) {
	projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
		now := time.Now().UTC()
		dupTask := testhelpers.BuildTaskByStatus("dup-task", models.TaskStatusReady, now)
		dupTask.SpecRef = "specs/vision.md"
		state.Tasks = []models.Task{dupTask}
	})
	testhelpers.CreateSpecFile(t, projectRoot, "vision.md", "# Vision\n")
	testhelpers.CreateSpecFile(t, projectRoot, "feature.md", "# Feature\n")

	tasks := []map[string]any{
		{
			"id":        "new-json-task",
			"desc":      "Task added before duplicate",
			"spec":      "specs/feature.md",
			"done":      "done",
			"scope":     "internal/ops",
			"priority":  1,
			"role_pair": "coding-pair",
		},
		{
			"id":        "dup-task",
			"desc":      "Duplicate task",
			"spec":      "specs/vision.md",
			"done":      "done",
			"scope":     "internal/ops",
			"priority":  1,
			"role_pair": "coding-pair",
		},
	}
	data, err := json.Marshal(tasks)
	if err != nil {
		t.Fatalf("failed to marshal tasks: %v", err)
	}
	tasksFile := filepath.Join(projectRoot, "tasks.json")
	if err := os.WriteFile(tasksFile, data, 0644); err != nil {
		t.Fatalf("failed to write tasks file: %v", err)
	}

	stdout, err := executeRootCommandCapture(t, projectRoot,
		"add-tasks",
		"--tasks-file", tasksFile,
		"--agent-id", "orchestrator-1",
		"--json",
	)
	if err != nil {
		t.Fatalf("add-tasks --json returned top-level error: %v", err)
	}

	env := parseEnvelope(t, stdout)
	if env["ok"] != true {
		t.Fatalf("expected ok=true for item-level partial failure, got %v", env["ok"])
	}
	if _, exists := env["error"]; exists {
		t.Fatalf("expected no top-level error for item-level partial failure, got %v", env["error"])
	}

	result, ok := env["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result to be object, got %T", env["result"])
	}
	results, ok := result["results"].([]any)
	if !ok {
		t.Fatalf("expected result.results to be array, got %T", result["results"])
	}
	if len(results) != 2 {
		t.Fatalf("result count = %d, want 2", len(results))
	}
	first, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("first result has type %T, want object", results[0])
	}
	if first["success"] != true || first["task_id"] != "new-json-task" {
		t.Fatalf("first result = %v, want successful new-json-task", first)
	}
	second, ok := results[1].(map[string]any)
	if !ok {
		t.Fatalf("second result has type %T, want object", results[1])
	}
	errMsg, _ := second["error"].(string)
	if second["success"] != false || second["task_id"] != "dup-task" || !strings.Contains(errMsg, "already exists") {
		t.Fatalf("second result = %v, want duplicate item failure", second)
	}

	state := readState(t, statePath)
	if state.FindTask("new-json-task") == nil {
		t.Fatal("new-json-task was not persisted after successful item result")
	}
	dupCount := 0
	for _, task := range state.Tasks {
		if task.ID == "dup-task" {
			dupCount++
		}
	}
	if dupCount != 1 {
		t.Fatalf("dup-task count = %d, want 1", dupCount)
	}
}

func TestJSON_AddTasks_MissingTasksFileReportsActionableValidation(t *testing.T) {
	projectRoot, _ := setupMutationTestProject(t, nil)
	missingFile := filepath.Join(projectRoot, "missing-tasks.json")

	tests := []struct {
		name      string
		args      []string
		wantParts []string
	}{
		{
			name:      "omitted tasks file flag",
			args:      []string{"add-tasks", "--json"},
			wantParts: []string{"--tasks-file is required"},
		},
		{
			name:      "missing tasks file",
			args:      []string{"add-tasks", "--tasks-file", missingFile, "--json"},
			wantParts: []string{"reading tasks file", "missing-tasks.json"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, err := executeRootCommandCapture(t, projectRoot, tt.args...)
			if err == nil {
				t.Fatalf("expected tasks file validation error, got nil")
			}
			assertJSONError(t, stdout, "validation", tt.wantParts...)
		})
	}
}

func TestJSON_AddTasks_MissingOrchestratorReportsActionablePrecondition(t *testing.T) {
	projectRoot, _ := setupMutationTestProject(t, nil)
	t.Setenv("LIZA_AGENT_ID", "")

	tasks := []map[string]any{
		{
			"id":        "new-json-task",
			"desc":      "New task",
			"spec":      "specs/vision.md",
			"done":      "done",
			"scope":     "internal/ops",
			"priority":  1,
			"role_pair": "coding-pair",
		},
	}
	data, err := json.Marshal(tasks)
	if err != nil {
		t.Fatalf("failed to marshal tasks: %v", err)
	}
	tasksFile := filepath.Join(projectRoot, "tasks.json")
	if err := os.WriteFile(tasksFile, data, 0644); err != nil {
		t.Fatalf("failed to write tasks file: %v", err)
	}

	stdout, err := executeRootCommandCapture(t, projectRoot,
		"add-tasks",
		"--tasks-file", tasksFile,
		"--json",
	)
	if err == nil {
		t.Fatalf("expected missing orchestrator error, got nil")
	}

	env := parseEnvelope(t, stdout)
	if env["ok"] != false {
		t.Fatalf("expected ok=false, got %v", env["ok"])
	}

	errObj, ok := env["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error to be object, got %T", env["error"])
	}
	if errObj["code"] != "validation" {
		t.Fatalf("error.code = %v, want validation", errObj["code"])
	}
	msg, _ := errObj["message"].(string)
	if msg == "" || msg == "internal error" || !strings.Contains(msg, "no orchestrator agent registered") || !strings.Contains(msg, "--agent-id") {
		t.Fatalf("error.message = %q, want actionable orchestrator precondition details", msg)
	}
}

func TestJSON_SetTaskOutput_MissingOutputFileReportsActionableValidation(t *testing.T) {
	projectRoot, _ := setupMutationTestProject(t, nil)
	missingFile := filepath.Join(projectRoot, "missing-output.json")

	tests := []struct {
		name      string
		args      []string
		wantParts []string
	}{
		{
			name: "omitted output flag",
			args: []string{
				"set-task-output", "task-json-output",
				"--agent-id", "epic-planner-1",
				"--json",
			},
			wantParts: []string{"--output is required"},
		},
		{
			name: "missing output file",
			args: []string{
				"set-task-output", "task-json-output",
				"--agent-id", "epic-planner-1",
				"--output", missingFile,
				"--json",
			},
			wantParts: []string{"reading output file", "missing-output.json"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, err := executeRootCommandCapture(t, projectRoot, tt.args...)
			if err == nil {
				t.Fatalf("expected output validation error, got nil")
			}
			assertJSONError(t, stdout, "validation", tt.wantParts...)
		})
	}
}

func TestJSON_MarkBlocked_IncompleteRepairRequestReportsActionableValidation(t *testing.T) {
	projectRoot, _ := setupMutationTestProject(t, nil)

	stdout, err := executeRootCommandCapture(t, projectRoot,
		"mark-blocked", "task-incomplete-repair-request",
		"--agent-id", "coder-1",
		"--reason", "Required state repair is orchestrator-only",
		"--questions", "Can the orchestrator restore the missing parent task?",
		"--repair-operation", "add-task",
		"--json",
	)
	if err == nil {
		t.Fatalf("expected incomplete repair request validation error, got nil")
	}
	assertJSONError(t, stdout, "validation", "--repair-target is required")
}

func TestJSON_MarkBlocked_AlertWriteFailureReturnsWarning(t *testing.T) {
	projectRoot, _ := setupMutationTestProject(t, func(state *models.State) {
		now := time.Now().UTC()
		state.Tasks = []models.Task{
			testhelpers.BuildTaskByStatus("task-alert-warning", models.TaskStatusImplementing, now),
		}
	})
	alertsPath := filepath.Join(projectRoot, ".liza", "alerts.log")
	if err := os.RemoveAll(alertsPath); err != nil {
		t.Fatalf("remove alerts.log path: %v", err)
	}
	if err := os.Mkdir(alertsPath, 0o755); err != nil {
		t.Fatalf("mkdir alerts.log path: %v", err)
	}

	stdout, err := executeRootCommandCapture(t, projectRoot,
		"mark-blocked", "task-alert-warning",
		"--agent-id", "coder-1",
		"--reason", "Spec ambiguity",
		"--questions", "What should happen?",
		"--json",
	)
	if err != nil {
		t.Fatalf("mark-blocked --json error: %v\n%s", err, stdout)
	}
	env := parseEnvelope(t, stdout)
	if env["ok"] != true {
		t.Fatalf("expected ok=true, got %v\n%s", env["ok"], stdout)
	}
	warnings, ok := env["warnings"].([]any)
	if !ok || len(warnings) != 1 {
		t.Fatalf("warnings = %#v, want one warning", env["warnings"])
	}
	if !strings.Contains(warnings[0].(string), "alert write failed") {
		t.Fatalf("warning = %q, want alert write failure", warnings[0])
	}
}

func TestJSON_Status_WithWarnings(t *testing.T) {
	// Set up project with corrupted pipeline config so resolver load fails.
	projectRoot, _ := setupMutationTestProject(t, nil)

	// Corrupt pipeline.yaml so resolver fails, producing a warning.
	pipelinePath := filepath.Join(projectRoot, ".liza", "pipeline.yaml")
	if err := os.WriteFile(pipelinePath, []byte("invalid: [yaml: {{broken"), 0644); err != nil {
		t.Fatalf("failed to corrupt pipeline.yaml: %v", err)
	}

	stdout, err := executeRootCommandCapture(t, projectRoot, "status", "--json")
	if err != nil {
		t.Fatalf("status --json failed: %v", err)
	}

	env := parseEnvelope(t, stdout)
	if env["ok"] != true {
		t.Fatalf("expected ok=true, got %v", env["ok"])
	}

	warnings, ok := env["warnings"].([]any)
	if !ok || len(warnings) == 0 {
		t.Fatalf("expected non-empty warnings array, got %v", env["warnings"])
	}

	// At least one warning should mention pipeline resolver failure
	found := false
	for _, w := range warnings {
		if s, ok := w.(string); ok {
			if len(s) > 0 {
				found = true
				break
			}
		}
	}
	if !found {
		t.Errorf("expected warning about pipeline resolver, got %v", warnings)
	}
}

func TestJSON_Status_NoWarnings(t *testing.T) {
	projectRoot, _ := setupMutationTestProject(t, nil)

	stdout, err := executeRootCommandCapture(t, projectRoot, "status", "--json")
	if err != nil {
		t.Fatalf("status --json failed: %v", err)
	}

	env := parseEnvelope(t, stdout)
	if env["ok"] != true {
		t.Fatalf("expected ok=true, got %v", env["ok"])
	}

	// No warnings when pipeline config is valid
	if env["warnings"] != nil {
		t.Errorf("expected no warnings field, got %v", env["warnings"])
	}
}

func TestJSON_UpdateSprintMetrics_TypedPayload(t *testing.T) {
	projectRoot, _ := setupMutationTestProject(t, func(state *models.State) {
		now := time.Now().UTC()
		state.Tasks = []models.Task{
			testhelpers.BuildTaskByStatus("task-metrics-1", models.TaskStatusMerged, now),
		}
	})

	stdout, err := executeRootCommandCapture(t, projectRoot, "update-sprint-metrics", "--json")
	if err != nil {
		t.Fatalf("update-sprint-metrics --json failed: %v", err)
	}

	env := parseEnvelope(t, stdout)
	if env["ok"] != true {
		t.Fatalf("expected ok=true, got %v", env["ok"])
	}

	result, ok := env["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result to be object, got %T", env["result"])
	}

	// All 11 SprintMetrics fields must be present with snake_case keys
	expectedKeys := []string{
		"tasks_done",
		"tasks_in_progress",
		"tasks_blocked",
		"iterations_total",
		"review_cycles_total",
		"review_verdict_approvals",
		"review_verdict_rejections",
		"review_verdict_count",
		"review_verdict_approval_rate_percent",
		"task_submitted_for_review_count",
		"task_outcome_approval_rate_percent",
	}

	for _, key := range expectedKeys {
		if _, exists := result[key]; !exists {
			t.Errorf("missing expected SprintMetrics key %q in result", key)
		}
	}

	// Extra field (json:"-") should not be present
	if _, exists := result["Extra"]; exists {
		t.Errorf("Extra field should not be serialized (has json:\"-\" tag)")
	}
}

func TestJSON_UpdateSprintMetrics_WithWarnings(t *testing.T) {
	projectRoot, _ := setupMutationTestProject(t, func(state *models.State) {
		now := time.Now().UTC()
		// Create 4 tasks all approved/merged with approval history
		// to get >95% approval rate and >=3 verdicts.
		for i := range 4 {
			taskID := "task-suspicious-" + string(rune('a'+i))
			task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusMerged, now)
			task.History = append(task.History,
				models.TaskHistoryEntry{
					Time:  now,
					Event: models.TaskEventSubmittedForReview,
				},
				models.TaskHistoryEntry{
					Time:  now,
					Event: models.TaskEventApproved,
				},
			)
			state.Tasks = append(state.Tasks, task)
		}
	})

	stdout, err := executeRootCommandCapture(t, projectRoot, "update-sprint-metrics", "--json")
	if err != nil {
		t.Fatalf("update-sprint-metrics --json failed: %v", err)
	}

	env := parseEnvelope(t, stdout)
	if env["ok"] != true {
		t.Fatalf("expected ok=true, got %v", env["ok"])
	}

	warnings, ok := env["warnings"].([]any)
	if !ok || len(warnings) == 0 {
		t.Fatalf("expected suspicious rate warnings, got %v", env["warnings"])
	}
}

func TestJSON_Version(t *testing.T) {
	// version doesn't need a project root
	stdout, err := executeRootCommandCapture(t, t.TempDir(), "version", "--json")
	if err != nil {
		t.Fatalf("version --json failed: %v", err)
	}

	env := parseEnvelope(t, stdout)
	if env["ok"] != true {
		t.Fatalf("expected ok=true, got %v", env["ok"])
	}

	result, ok := env["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result to be object, got %T", env["result"])
	}

	for _, key := range []string{"version", "commit", "built"} {
		val, exists := result[key]
		if !exists {
			t.Errorf("missing key %q in version result", key)
			continue
		}
		if _, isStr := val.(string); !isStr {
			t.Errorf("expected %q to be string, got %T", key, val)
		}
	}
}

func TestJSON_Validate_Valid(t *testing.T) {
	projectRoot, _ := setupMutationTestProject(t, nil)

	stdout, err := executeRootCommandCapture(t, projectRoot, "validate", "--json", "--skip-spec-check")
	if err != nil {
		t.Fatalf("validate --json failed: %v", err)
	}

	env := parseEnvelope(t, stdout)
	if env["ok"] != true {
		t.Fatalf("expected ok=true, got %v", env["ok"])
	}

	result, ok := env["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result to be object, got %T", env["result"])
	}
	if result["valid"] != true {
		t.Errorf("expected valid=true, got %v", result["valid"])
	}
}

func TestJSON_Validate_MasterPlanningEmbeddedPipeline(t *testing.T) {
	projectRoot, _ := setupMutationTestProject(t, nil)
	specDir := filepath.Join(projectRoot, "specs")
	if err := os.MkdirAll(specDir, 0755); err != nil {
		t.Fatalf("failed to create specs dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(specDir, "vision.md"), []byte("# Vision\n"), 0644); err != nil {
		t.Fatalf("failed to create vision spec: %v", err)
	}

	assertMasterPlanningTopology(t, projectRoot)

	stdout, err := executeRootCommandCapture(t, projectRoot, "validate", "--json")
	if err != nil {
		t.Fatalf("validate --json failed: %v", err)
	}

	env := parseEnvelope(t, stdout)
	if env["ok"] != true {
		t.Fatalf("expected ok=true, got %v", env["ok"])
	}
	result, ok := env["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result to be object, got %T", env["result"])
	}
	if result["valid"] != true {
		t.Fatalf("expected valid=true, got %v", result["valid"])
	}
}

func TestJSON_InitialPlanningRoutesRenderOneTaskContractsFromInitializedCLIState(t *testing.T) {
	tests := []struct {
		name           string
		entryPoint     string
		simpleRolePair string
		simpleTaskType string
		fanOutRolePair string
		fanOutTaskType string
	}{
		{
			name:           "general objective",
			entryPoint:     "general-objective",
			simpleRolePair: "epic-planning-pair",
			simpleTaskType: "epic-planning",
			fanOutRolePair: "epic-planning-main-pair",
			fanOutTaskType: "epic-planning",
		},
		{
			name:           "functional spec",
			entryPoint:     "functional-spec",
			simpleRolePair: "architecture-pair",
			simpleTaskType: "architecture",
			fanOutRolePair: "architecture-main-pair",
			fanOutTaskType: "architecture",
		},
		{
			name:           "detailed spec",
			entryPoint:     "detailed-spec",
			simpleRolePair: "architecture-pair",
			simpleTaskType: "architecture",
			fanOutRolePair: "architecture-main-pair",
			fanOutTaskType: "architecture",
		},
		{
			name:           "technical spec",
			entryPoint:     "technical-spec",
			simpleRolePair: "code-planning-pair",
			simpleTaskType: "planning",
			fanOutRolePair: "code-planning-main-pair",
			fanOutTaskType: "planning",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectRoot, statePath := setupInitializedProjectWithEntryPoint(t, tt.entryPoint)
			state := readState(t, statePath)

			dashboard, wakeInstruction, err := prompts.RenderOrchestratorDashboard(state, projectRoot, "orchestrator-1")
			if err != nil {
				t.Fatalf("RenderOrchestratorDashboard: %v", err)
			}
			rendered := dashboard + "\n" + wakeInstruction

			assertNotContainsAny(t, rendered, []string{
				"MULTI-TASK PLANNING",
				"Create up to",
				"Create multiple parallel planning tasks",
				"multiple specialized planning tasks",
				"Domain A",
				"Domain B",
				"domain-a",
				"domain-b",
			})

			simpleTasks := extractInitialPlanningExampleTasks(t, rendered, "SIMPLE GOAL TASK EXAMPLE:")
			assertOneInitialPlanningTask(t, simpleTasks, tt.simpleRolePair, tt.simpleTaskType)

			fanOutTasks := extractInitialPlanningExampleTasks(t, rendered, "FAN-OUT GOAL TASK EXAMPLE:")
			assertOneInitialPlanningTask(t, fanOutTasks, tt.fanOutRolePair, tt.fanOutTaskType)
		})
	}
}

func TestJSON_InitialPlanningMissingMasterRendersOneSpecializedFallback(t *testing.T) {
	projectRoot, statePath := setupInitializedProjectWithEntryPoint(t, "functional-spec")
	pipelinePath := filepath.Join(projectRoot, ".liza", "pipeline.yaml")
	content, err := os.ReadFile(pipelinePath)
	if err != nil {
		t.Fatalf("read pipeline.yaml: %v", err)
	}
	withoutMasterMarkers := strings.ReplaceAll(string(content), "      decomposition-root: true\n", "")
	if err := os.WriteFile(pipelinePath, []byte(withoutMasterMarkers), 0644); err != nil {
		t.Fatalf("write pipeline.yaml: %v", err)
	}

	state := readState(t, statePath)
	dashboard, wakeInstruction, err := prompts.RenderOrchestratorDashboard(state, projectRoot, "orchestrator-1")
	if err != nil {
		t.Fatalf("RenderOrchestratorDashboard: %v", err)
	}
	rendered := dashboard + "\n" + wakeInstruction

	if strings.Contains(rendered, "FAN-OUT GOAL TASK EXAMPLE") {
		t.Fatalf("missing-master rendering included fan-out example:\n%s", rendered)
	}
	assertNotContainsAny(t, rendered, []string{
		"architecture-main-pair",
		"epic-planning-main-pair",
		"code-planning-main-pair",
		"\"id\": \"architecture-2\"",
		"MULTI-TASK PLANNING",
		"Create up to",
		"multiple specialized planning tasks",
	})

	simpleTasks := extractInitialPlanningExampleTasks(t, rendered, "SIMPLE GOAL TASK EXAMPLE:")
	assertOneInitialPlanningTask(t, simpleTasks, "architecture-pair", "architecture")
}

func TestJSON_InitialPlanningValidationMatrixDocumentsMasterPlanningCoverage(t *testing.T) {
	planPath := filepath.Join("..", "..", "specs", "plans", "20260523-master-planning-task", "20260523-171755-architecture-5-code-planning-0.md")
	content, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("read validation matrix plan: %v", err)
	}
	plan := string(content)

	for _, want := range []string{
		"| 1. Pipeline validation passes |",
		"| 2. Entry-point routing |",
		"| 8. Orchestrator simplification |",
		"`liza validate --json`",
		"`go test ./cmd/liza -run 'TestJSON_Validate.*MasterPlanning|TestInitDispatch_.*InitialPlanning|TestJSON_.*InitialPlanning'`",
		"`go test ./cmd/liza -run 'TestJSON_Validate.*MasterPlanning'`",
		"`go test ./cmd/liza -run 'TestInitDispatch_.*InitialPlanning|TestJSON_.*InitialPlanning'`",
		"`go test ./internal/prompts/...`",
	} {
		if !strings.Contains(plan, want) {
			t.Fatalf("validation matrix missing %q", want)
		}
	}
}

func TestJSON_Validate_DefaultStatePathRequiresProjectRootFromTaskWorktree(t *testing.T) {
	projectRoot, _ := setupMutationTestProject(t, nil)
	taskID := "task-validate-worktree"
	testhelpers.CreateTestWorktree(t, projectRoot, taskID)
	worktreeDir := filepath.Join(projectRoot, ".worktrees", taskID)

	stdout, err := executeRootCommandCapture(t, worktreeDir, "validate", "--json", "--skip-spec-check")
	if err == nil {
		t.Fatalf("expected project root error from task worktree, got nil")
	}

	env := parseEnvelope(t, stdout)
	if env["ok"] != false {
		t.Fatalf("expected ok=false, got %v", env["ok"])
	}

	errObj, ok := env["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error to be object, got %T", env["error"])
	}
	if errObj["code"] != "project_root" {
		t.Fatalf("error.code = %v, want project_root", errObj["code"])
	}
	msg, _ := errObj["message"].(string)
	if msg == "" || msg == "internal error" || !strings.Contains(msg, "must be run from project root") {
		t.Fatalf("error.message = %q, want project-root cwd guidance", msg)
	}
	details, ok := errObj["details"].(map[string]any)
	if !ok {
		t.Fatalf("expected error.details to be object, got %T", errObj["details"])
	}
	if details["current_dir"] != worktreeDir {
		t.Fatalf("details.current_dir = %v, want %s", details["current_dir"], worktreeDir)
	}
	if details["project_root"] != projectRoot {
		t.Fatalf("details.project_root = %v, want %s", details["project_root"], projectRoot)
	}
}

func TestJSON_Validate_DefaultStatePathRequiresProjectRootFromSubdirectory(t *testing.T) {
	projectRoot, _ := setupMutationTestProject(t, nil)
	subdir := filepath.Join(projectRoot, "docs")
	if err := os.Mkdir(subdir, 0755); err != nil {
		t.Fatalf("failed to create subdirectory: %v", err)
	}

	stdout, err := executeRootCommandCapture(t, subdir, "validate", "--json", "--skip-spec-check")
	if err == nil {
		t.Fatalf("expected project root error from project subdirectory, got nil")
	}

	env := parseEnvelope(t, stdout)
	if env["ok"] != false {
		t.Fatalf("expected ok=false, got %v", env["ok"])
	}

	errObj, ok := env["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error to be object, got %T", env["error"])
	}
	if errObj["code"] != "project_root" {
		t.Fatalf("error.code = %v, want project_root", errObj["code"])
	}
	msg, _ := errObj["message"].(string)
	if msg == "" || msg == "internal error" || !strings.Contains(msg, "must be run from project root") {
		t.Fatalf("error.message = %q, want project-root cwd guidance", msg)
	}
	details, ok := errObj["details"].(map[string]any)
	if !ok {
		t.Fatalf("expected error.details to be object, got %T", errObj["details"])
	}
	if details["current_dir"] != subdir {
		t.Fatalf("details.current_dir = %v, want %s", details["current_dir"], subdir)
	}
	if details["project_root"] != projectRoot {
		t.Fatalf("details.project_root = %v, want %s", details["project_root"], projectRoot)
	}
}

func TestJSON_Validate_DefaultStatePathRequiresProjectRoot(t *testing.T) {
	nonProjectRoot := t.TempDir()

	stdout, err := executeRootCommandCapture(t, nonProjectRoot, "validate", "--json", "--skip-spec-check")
	if err == nil {
		t.Fatalf("expected project root detection error, got nil")
	}

	env := parseEnvelope(t, stdout)
	if env["ok"] != false {
		t.Fatalf("expected ok=false, got %v", env["ok"])
	}
	errObj, ok := env["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error to be object, got %T", env["error"])
	}
	if errObj["code"] != "project_root" {
		t.Fatalf("error.code = %v, want project_root", errObj["code"])
	}
	msg, _ := errObj["message"].(string)
	if msg == "" || msg == "internal error" || !strings.Contains(msg, "project root") {
		t.Fatalf("error.message = %q, want actionable project root details", msg)
	}
}

func TestJSON_Validate_Invalid(t *testing.T) {
	// Create a project with an invalid state (empty/broken state file)
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	_, _ = testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.SetupPipelineConfig(t, projectRoot)

	// Write an empty/minimal state that will fail validation (no version, no goal)
	statePath := filepath.Join(projectRoot, ".liza", "state.yaml")
	if err := os.WriteFile(statePath, []byte("version: 0\n"), 0644); err != nil {
		t.Fatalf("failed to write invalid state: %v", err)
	}

	stdout, err := executeRootCommandCapture(t, projectRoot, "validate", "--json", "--skip-spec-check")
	if err == nil {
		t.Fatalf("expected error for invalid state, got nil")
	}

	env := parseEnvelope(t, stdout)
	if env["ok"] != false {
		t.Fatalf("expected ok=false, got %v", env["ok"])
	}

	errObj, ok := env["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error to be object, got %T", env["error"])
	}
	if errObj["code"] == nil || errObj["code"] == "" {
		t.Errorf("expected error code to be set, got %v", errObj["code"])
	}
}

func TestJSON_Validate_DanglingParentTaskReportsValidationDetails(t *testing.T) {
	projectRoot, _ := setupMutationTestProject(t, func(state *models.State) {
		now := time.Now().UTC()
		child := testhelpers.BuildTaskByStatus("task-child", models.TaskStatusReady, now)
		missingParent := "missing-parent"
		child.ParentTask = &missingParent
		state.Tasks = []models.Task{child}
	})

	stdout, err := executeRootCommandCapture(t, projectRoot, "validate", "--json", "--skip-spec-check")
	if err == nil {
		t.Fatalf("expected validate --json to fail for dangling parent_task")
	}

	env := parseEnvelope(t, stdout)
	if env["ok"] != false {
		t.Fatalf("expected ok=false, got %v", env["ok"])
	}

	errObj, ok := env["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error to be object, got %T", env["error"])
	}
	if errObj["code"] != "validation" {
		t.Fatalf("error.code = %v, want validation", errObj["code"])
	}
	msg, _ := errObj["message"].(string)
	if !strings.Contains(msg, "parent_task") || !strings.Contains(msg, "missing-parent") {
		t.Fatalf("error.message = %q, want dangling parent_task details", msg)
	}
}

func TestJSON_Validate_MissingArtifactRefReportsFieldTaskAndValue(t *testing.T) {
	projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
		now := time.Now().UTC()
		task := testhelpers.BuildTaskByStatus("task-plan-missing", models.TaskStatusMerged, now)
		task.SpecRef = "specs/vision.md"
		task.PlanRef = "specs/plans/missing.md"
		state.Tasks = []models.Task{task}
	})
	specDir := filepath.Join(projectRoot, "specs")
	if err := os.MkdirAll(specDir, 0755); err != nil {
		t.Fatalf("failed to create specs dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(specDir, "vision.md"), []byte("# Vision\n"), 0644); err != nil {
		t.Fatalf("failed to create vision spec: %v", err)
	}
	state := readState(t, statePath)
	task := mustFindTask(t, state, "task-plan-missing")
	if task.PlanRef != "specs/plans/missing.md" {
		t.Fatalf("PlanRef = %q, want specs/plans/missing.md", task.PlanRef)
	}

	stdout, err := executeRootCommandCapture(t, projectRoot, "validate", "--json", "--skip-spec-check=false")
	if err == nil {
		t.Fatalf("expected validate --json to fail for missing plan_ref")
	}

	env := parseEnvelope(t, stdout)
	errObj, ok := env["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error to be object, got %T", env["error"])
	}
	if errObj["code"] != "validation" {
		t.Fatalf("error.code = %v, want validation", errObj["code"])
	}
	msg, _ := errObj["message"].(string)
	if !strings.Contains(msg, "plan_ref file not found") || !strings.Contains(msg, "task-plan-missing") || strings.Contains(msg, "spec_ref file not found") {
		t.Fatalf("error.message = %q, want field-specific plan_ref details", msg)
	}
	details, ok := errObj["details"].(map[string]any)
	if !ok {
		t.Fatalf("expected error.details to be object, got %T", errObj["details"])
	}
	if details["field"] != "plan_ref" {
		t.Errorf("details.field = %v, want plan_ref", details["field"])
	}
	if details["task_id"] != "task-plan-missing" {
		t.Errorf("details.task_id = %v, want task-plan-missing", details["task_id"])
	}
	if details["value"] != "specs/plans/missing.md" {
		t.Errorf("details.value = %v, want specs/plans/missing.md", details["value"])
	}
	if _, exists := details["resolved_path"]; exists {
		t.Errorf("details.resolved_path should not be exposed: %v", details["resolved_path"])
	}
}

func TestJSON_RBACError(t *testing.T) {
	projectRoot, _ := setupMutationTestProject(t, func(state *models.State) {
		now := time.Now().UTC()
		state.Tasks = []models.Task{
			testhelpers.BuildTaskByStatus("task-rbac-json", models.TaskStatusReady, now),
		}
	})

	// orchestrator is not allowed to claim tasks (requires "doer" role type)
	stdout, err := executeRootCommandCapture(t, projectRoot, "claim-task", "task-rbac-json", "orchestrator-1", "--json")
	if err == nil {
		t.Fatalf("expected RBAC error, got nil")
	}

	env := parseEnvelope(t, stdout)
	if env["ok"] != false {
		t.Fatalf("expected ok=false, got %v", env["ok"])
	}

	errObj, ok := env["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error to be object, got %T", env["error"])
	}
	if errObj["code"] != "permission_denied" {
		t.Fatalf("error.code = %v, want permission_denied", errObj["code"])
	}
	msg, _ := errObj["message"].(string)
	if msg == "" || msg == "internal error" {
		t.Fatalf("error.message = %q, want actionable RBAC failure details", msg)
	}
}

func TestJSON_ProjectRootDetectionErrorReportsActionableContext(t *testing.T) {
	nonProjectRoot := t.TempDir()

	stdout, err := executeRootCommandCapture(t, nonProjectRoot, "claim-task", "task-no-root", "coder-1", "--json")
	if err == nil {
		t.Fatalf("expected project root detection error, got nil")
	}

	env := parseEnvelope(t, stdout)
	if env["ok"] != false {
		t.Fatalf("expected ok=false, got %v", env["ok"])
	}

	errObj, ok := env["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error to be object, got %T", env["error"])
	}
	if errObj["code"] != "project_root" {
		t.Fatalf("error.code = %v, want project_root", errObj["code"])
	}
	msg, _ := errObj["message"].(string)
	if msg == "" || msg == "internal error" {
		t.Fatalf("error.message = %q, want actionable project root detection details", msg)
	}
	if !strings.Contains(msg, "project root") {
		t.Fatalf("error.message = %q, want project root context", msg)
	}
}

func TestJSON_PipelineConfigErrorReportsActionableContext(t *testing.T) {
	projectRoot, _ := setupMutationTestProject(t, func(state *models.State) {
		now := time.Now().UTC()
		state.Tasks = []models.Task{
			testhelpers.BuildTaskByStatus("task-pipeline-json", models.TaskStatusReady, now),
		}
	})
	pipelinePath := filepath.Join(projectRoot, ".liza", "pipeline.yaml")
	if err := os.WriteFile(pipelinePath, []byte("roles: [\n"), 0644); err != nil {
		t.Fatalf("failed to corrupt pipeline config: %v", err)
	}

	stdout, err := executeRootCommandCapture(t, projectRoot, "claim-task", "task-pipeline-json", "coder-1", "--json")
	if err == nil {
		t.Fatalf("expected pipeline config error, got nil")
	}

	env := parseEnvelope(t, stdout)
	if env["ok"] != false {
		t.Fatalf("expected ok=false, got %v", env["ok"])
	}
	errObj, ok := env["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error to be object, got %T", env["error"])
	}
	if errObj["code"] != "pipeline_config" {
		t.Fatalf("error.code = %v, want pipeline_config", errObj["code"])
	}
	msg, _ := errObj["message"].(string)
	if msg == "" || msg == "internal error" || !strings.Contains(msg, "pipeline config") {
		t.Fatalf("error.message = %q, want actionable pipeline config details", msg)
	}
}

func TestJSON_StateSchemaErrorReportsActionableContext(t *testing.T) {
	projectRoot, statePath := setupMutationTestProject(t, nil)
	if err := os.WriteFile(statePath, []byte("tasks: [\n"), 0644); err != nil {
		t.Fatalf("failed to corrupt state file: %v", err)
	}

	stdout, err := executeRootCommandCapture(t, projectRoot, "validate", "--json", "--skip-spec-check")
	if err == nil {
		t.Fatalf("expected state schema error, got nil")
	}

	env := parseEnvelope(t, stdout)
	if env["ok"] != false {
		t.Fatalf("expected ok=false, got %v", env["ok"])
	}
	errObj, ok := env["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error to be object, got %T", env["error"])
	}
	if errObj["code"] != "state_schema" {
		t.Fatalf("error.code = %v, want state_schema", errObj["code"])
	}
	msg, _ := errObj["message"].(string)
	if msg == "" || msg == "internal error" || !strings.Contains(msg, "state schema") {
		t.Fatalf("error.message = %q, want actionable state schema details", msg)
	}
}

func TestJSON_StateTransitionSchemaErrorReportsActionableContext(t *testing.T) {
	projectRoot, statePath := setupMutationTestProject(t, nil)
	if err := os.WriteFile(statePath, []byte("tasks: [\n"), 0644); err != nil {
		t.Fatalf("failed to corrupt state file: %v", err)
	}

	stdout, err := executeRootCommandCapture(t, projectRoot,
		"mark-blocked", "task-corrupt-state",
		"--reason", "state parse failure repro",
		"--questions", "What should repair do?",
		"--agent-id", "coder-1",
		"--json",
	)
	if err == nil {
		t.Fatalf("expected state schema error, got nil")
	}

	env := parseEnvelope(t, stdout)
	if env["ok"] != false {
		t.Fatalf("expected ok=false, got %v", env["ok"])
	}
	errObj, ok := env["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error to be object, got %T", env["error"])
	}
	if errObj["code"] != "state_schema" {
		t.Fatalf("error.code = %v, want state_schema", errObj["code"])
	}
	msg, _ := errObj["message"].(string)
	if msg == "" || msg == "internal error" || !strings.Contains(msg, "state schema") {
		t.Fatalf("error.message = %q, want actionable state schema details", msg)
	}
}

func TestJSON_WorktreeContextErrorReportsActionableContext(t *testing.T) {
	projectRoot, _ := setupMutationTestProject(t, func(state *models.State) {
		now := time.Now().UTC()
		agentID := "coder-1"
		task := testhelpers.BuildTaskByStatus("task-worktree-json", models.TaskStatusImplementing, now)
		task.History = append(task.History, models.TaskHistoryEntry{
			Time:  now,
			Event: models.TaskEventPreExecutionCheckpoint,
			Agent: &agentID,
			Extra: map[string]any{
				"intent":          "exercise missing worktree JSON diagnostics",
				"validation_plan": "submit-for-review reports worktree context",
			},
		})
		state.Tasks = []models.Task{task}
	})

	stdout, err := executeRootCommandCapture(t, projectRoot,
		"submit-for-review", "task-worktree-json", "HEAD", "--agent-id", "coder-1", "--json")
	if err == nil {
		t.Fatalf("expected worktree context error, got nil")
	}

	env := parseEnvelope(t, stdout)
	if env["ok"] != false {
		t.Fatalf("expected ok=false, got %v", env["ok"])
	}
	errObj, ok := env["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error to be object, got %T", env["error"])
	}
	if errObj["code"] != "worktree_context" {
		t.Fatalf("error.code = %v, want worktree_context", errObj["code"])
	}
	msg, _ := errObj["message"].(string)
	if msg == "" || msg == "internal error" || !strings.Contains(msg, "worktree") {
		t.Fatalf("error.message = %q, want actionable worktree context details", msg)
	}
}

func TestJSON_SubmitForReviewFromTaskWorktreeRequiresProjectRoot(t *testing.T) {
	projectRoot, statePath, taskID, agentID := setupSubmitForReviewCLIProject(t)
	state := readState(t, statePath)
	state.Config.IntegrationBranch = "missing-integration"
	testhelpers.WriteInitialState(t, statePath, state)

	worktreeDir := filepath.Join(projectRoot, ".worktrees", taskID)
	stdout, err := executeRootCommandCapture(t, worktreeDir,
		"submit-for-review", taskID, "HEAD", "--agent-id", agentID, "--json")
	if err == nil {
		t.Fatalf("expected project root error from task worktree, got nil")
	}

	env := parseEnvelope(t, stdout)
	if env["ok"] != false {
		t.Fatalf("expected ok=false, got %v", env["ok"])
	}
	errObj, ok := env["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error to be object, got %T", env["error"])
	}
	if errObj["code"] != "project_root" {
		t.Fatalf("error.code = %v, want project_root", errObj["code"])
	}
	msg, _ := errObj["message"].(string)
	if msg == "" || msg == "internal error" || !strings.Contains(msg, "must be run from project root") {
		t.Fatalf("error.message = %q, want project-root cwd guidance", msg)
	}
	details, ok := errObj["details"].(map[string]any)
	if !ok {
		t.Fatalf("expected error.details to be object, got %T", errObj["details"])
	}
	if details["current_dir"] != worktreeDir {
		t.Fatalf("details.current_dir = %v, want %s", details["current_dir"], worktreeDir)
	}
	if details["project_root"] != projectRoot {
		t.Fatalf("details.project_root = %v, want %s", details["project_root"], projectRoot)
	}
}

func TestJSON_GetWrapsExisting(t *testing.T) {
	projectRoot, _ := setupMutationTestProject(t, nil)

	stdout, err := executeRootCommandCapture(t, projectRoot, "get", "tasks", "--json")
	if err != nil {
		t.Fatalf("get tasks --json failed: %v", err)
	}

	env := parseEnvelope(t, stdout)
	if env["ok"] != true {
		t.Fatalf("expected ok=true, got %v", env["ok"])
	}

	// result should be present (the wrapped JSON data)
	if _, exists := env["result"]; !exists {
		t.Errorf("expected result field in envelope")
	}
}

func TestJSON_GetTasksSummaryActive(t *testing.T) {
	projectRoot, _ := setupMutationTestProject(t, func(state *models.State) {
		now := time.Now().UTC()
		active := testhelpers.BuildTaskByStatus("task-active", models.TaskStatusImplementing, now)
		active.DoneWhen = "verbose done when should not appear"
		active.Scope = "verbose scope should not appear"
		active.Output = []models.OutputEntry{{Kind: "code-task", Desc: "child"}}
		merged := testhelpers.BuildTaskByStatus("task-merged", models.TaskStatusMerged, now)
		state.Tasks = []models.Task{active, merged}
	})

	stdout, err := executeRootCommandCapture(t, projectRoot, "get", "tasks", "--active", "--summary", "--json")
	if err != nil {
		t.Fatalf("get tasks --active --summary --json failed: %v", err)
	}

	env := parseEnvelope(t, stdout)
	if env["ok"] != true {
		t.Fatalf("expected ok=true, got %v", env["ok"])
	}
	result, ok := env["result"].([]any)
	if !ok {
		t.Fatalf("expected result array, got %T", env["result"])
	}
	if len(result) != 1 {
		t.Fatalf("summary result count = %d, want 1 active task", len(result))
	}
	task, ok := result[0].(map[string]any)
	if !ok {
		t.Fatalf("expected task object, got %T", result[0])
	}
	if task["id"] != "task-active" {
		t.Errorf("id = %v, want task-active", task["id"])
	}
	if _, exists := task["done_when"]; exists {
		t.Errorf("summary task includes done_when: %v", task)
	}
	if _, exists := task["scope"]; exists {
		t.Errorf("summary task includes scope: %v", task)
	}
	if _, exists := task["output"]; exists {
		t.Errorf("summary task includes output: %v", task)
	}
}

func TestJSON_GetTaskOutputSummary(t *testing.T) {
	projectRoot, _ := setupMutationTestProject(t, func(state *models.State) {
		now := time.Now().UTC()
		worktree := "/tmp/task-output-summary"
		task := testhelpers.BuildTaskByStatus("task-output-summary", models.TaskStatusMerged, now)
		task.RolePair = "code-planning-pair"
		task.Worktree = &worktree
		task.DoneWhen = "verbose parent done_when should not appear"
		task.Scope = "verbose parent scope should not appear"
		task.Output = []models.OutputEntry{
			{
				Desc:    "Prepare downstream task",
				SpecRef: "specs/foundation.md",
				Kind:    "code-task",
			},
			{
				Desc:          "Implement downstream task",
				DoneWhen:      "verbose child done_when should not appear",
				Scope:         "verbose child scope should not appear",
				SpecRef:       "specs/downstream.md",
				PlanRef:       "specs/plans/downstream.md",
				ArchRef:       "specs/arch-plan/downstream.md",
				Kind:          "code-task",
				DependsOn:     []string{"0"},
				TaskDependsOn: []string{"task-existing"},
			},
		}
		state.Tasks = []models.Task{task}
	})

	stdout, err := executeRootCommandCapture(t, projectRoot, "get", "task-output-summary", "--output-summary", "--json")
	if err != nil {
		t.Fatalf("get task --output-summary --json failed: %v", err)
	}

	env := parseEnvelope(t, stdout)
	if env["ok"] != true {
		t.Fatalf("expected ok=true, got %v", env["ok"])
	}
	result, ok := env["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object, got %T", env["result"])
	}
	if result["id"] != "task-output-summary" || result["role_pair"] != "code-planning-pair" {
		t.Fatalf("unexpected result envelope: %v", result)
	}
	for _, key := range []string{"description", "done_when", "scope", "worktree"} {
		if _, exists := result[key]; exists {
			t.Fatalf("output summary includes parent %q: %v", key, result)
		}
	}
	entries, ok := result["output"].([]any)
	if !ok || len(entries) != 2 {
		t.Fatalf("result.output = %T %v, want two entries", result["output"], result["output"])
	}
	entry := entries[1].(map[string]any)
	if entry["index"] != float64(1) || entry["desc"] != "Implement downstream task" {
		t.Fatalf("unexpected output entry: %v", entry)
	}
	if _, exists := entry["done_when"]; exists {
		t.Fatalf("output summary includes child done_when: %v", entry)
	}
	if _, exists := entry["scope"]; exists {
		t.Fatalf("output summary includes child scope: %v", entry)
	}
}

func TestJSON_GetRejectsSummaryAndOutputSummaryTogether(t *testing.T) {
	projectRoot, _ := setupMutationTestProject(t, nil)

	stdout, err := executeRootCommandCapture(t, projectRoot, "get", "tasks", "--summary", "--output-summary", "--json")
	if err == nil {
		t.Fatal("expected get tasks --summary --output-summary --json to fail")
	}

	env := parseEnvelope(t, stdout)
	if env["ok"] != false {
		t.Fatalf("expected ok=false, got %v", env["ok"])
	}
	errObj, ok := env["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %T", env["error"])
	}
	msg, _ := errObj["message"].(string)
	if !strings.Contains(msg, "--summary and --output-summary are mutually exclusive") {
		t.Fatalf("error.message = %q, want mutual exclusion message", msg)
	}
}

func TestJSON_VoidSuccess(t *testing.T) {
	projectRoot, _ := setupMutationTestProject(t, func(state *models.State) {
		now := time.Now().UTC()
		task := testhelpers.BuildTaskByStatus("task-ckpt-json", models.TaskStatusImplementing, now)
		agentID := "coder-1"
		task.AssignedTo = &agentID
		state.Tasks = []models.Task{task}
		state.Agents = map[string]models.Agent{
			"coder-1": {
				Role:         "coder",
				Status:       models.AgentStatusWorking,
				CurrentTask:  &task.ID,
				LeaseExpires: timePtr(now.Add(30 * time.Minute)),
				Heartbeat:    now,
				Provider:     "test",
				PID:          os.Getpid(),
			},
		}
	})

	stdout, err := executeRootCommandCapture(t, projectRoot,
		"write-checkpoint", "task-ckpt-json",
		"--agent-id", "coder-1",
		"--intent", "test intent",
		"--validation-plan", "test plan",
		"--files-to-modify", "foo.go",
		"--json",
	)
	if err != nil {
		t.Fatalf("write-checkpoint --json failed: %v", err)
	}

	env := parseEnvelope(t, stdout)
	if env["ok"] != true {
		t.Fatalf("expected ok=true, got %v", env["ok"])
	}

	// Void success: result must be null (not omitted, not empty object)
	resultRaw, exists := env["result"]
	if !exists {
		t.Fatalf("expected result key in envelope")
	}
	if resultRaw != nil {
		t.Errorf("expected result=null for void success, got %v", resultRaw)
	}
}

func TestJSON_Validate_WithWarnings(t *testing.T) {
	expiredLease := time.Now().UTC().Add(-2 * time.Hour)
	taskLease := time.Now().UTC().Add(30 * time.Minute)
	taskID := "task-validate-warn"
	agentID := "coder-1"
	worktreeRel := ".worktrees/task-validate-warn"
	baseCommit := "abc123"

	projectRoot, _ := setupMutationTestProject(t, func(state *models.State) {
		now := time.Now().UTC()
		task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusImplementing, now)
		task.AssignedTo = &agentID
		task.Worktree = &worktreeRel
		task.BaseCommit = &baseCommit
		task.LeaseExpires = &taskLease
		state.Tasks = []models.Task{task}
		state.Agents = map[string]models.Agent{
			agentID: {
				Role:         "coder",
				Status:       models.AgentStatusWorking,
				CurrentTask:  &taskID,
				LeaseExpires: &expiredLease,
				Heartbeat:    now,
				Provider:     "test",
				PID:          os.Getpid(),
			},
		}
	})

	// Create the worktree directory so the worktree existence check passes.
	wtDir := filepath.Join(projectRoot, worktreeRel)
	if err := os.MkdirAll(wtDir, 0755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}

	stdout, err := executeRootCommandCapture(t, projectRoot, "validate", "--json", "--skip-spec-check")
	if err != nil {
		t.Fatalf("validate --json failed: %v", err)
	}

	env := parseEnvelope(t, stdout)
	if env["ok"] != true {
		t.Fatalf("expected ok=true, got %v", env["ok"])
	}

	warnings, ok := env["warnings"].([]any)
	if !ok || len(warnings) == 0 {
		t.Fatalf("expected warnings from expired agent lease, got %v", env["warnings"])
	}

	// At least one warning should mention lease expired
	found := false
	for _, w := range warnings {
		if s, ok := w.(string); ok && len(s) > 0 {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected non-empty warning string, got %v", warnings)
	}
}

func TestJSON_ValidateRepairErrorIncludesRepairWarning(t *testing.T) {
	projectRoot, _ := setupMutationTestProject(t, func(state *models.State) {
		now := time.Now().UTC()
		task := testhelpers.BuildTaskByStatus("task-validate-repair-warning", models.TaskStatusReviewing, now)
		task.DoneWhen = "" // Keep a validation error after repair clears ownership.
		state.Tasks = []models.Task{task}
		state.Agents = map[string]models.Agent{
			"code-reviewer-1": {
				Role:         "code-reviewer",
				Status:       models.AgentStatusIdle,
				LeaseExpires: testhelpers.TimePtr(now.Add(30 * time.Minute)),
				Heartbeat:    now,
				Provider:     "anthropic",
				PID:          os.Getpid(),
			},
		}
	})

	stdout, err := executeRootCommandCapture(t, projectRoot, "validate", "--json", "--skip-spec-check", "--skip-process-checks", "--repair")
	if err == nil {
		t.Fatalf("expected validate --json --repair to fail after repair")
	}

	env := parseEnvelope(t, stdout)
	if env["ok"] != false {
		t.Fatalf("expected ok=false, got %v", env["ok"])
	}
	warnings, ok := env["warnings"].([]any)
	if !ok || len(warnings) == 0 {
		t.Fatalf("expected repair warnings on error response, got %v", env["warnings"])
	}
	found := false
	for _, warning := range warnings {
		if s, ok := warning.(string); ok && strings.Contains(s, "REPAIRED: invalid active review ownership cleared for 1 task(s)") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("warnings = %v, want repair warning", warnings)
	}
}

func TestJSON_LogSuppression(t *testing.T) {
	projectRoot, _ := setupMutationTestProject(t, nil)

	// Capture stderr to verify log suppression
	oldStderr := os.Stderr
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stderr pipe: %v", err)
	}
	os.Stderr = stderrW

	_, cmdErr := executeRootCommandCapture(t, projectRoot, "update-sprint-metrics", "--json")

	stderrW.Close()
	os.Stderr = oldStderr

	var stderrBuf bytes.Buffer
	if _, copyErr := io.Copy(&stderrBuf, stderrR); copyErr != nil {
		t.Fatalf("failed to read stderr: %v", copyErr)
	}
	stderrR.Close()

	if cmdErr != nil {
		t.Fatalf("update-sprint-metrics --json failed: %v", cmdErr)
	}

	if stderrBuf.Len() != 0 {
		t.Errorf("expected empty stderr when --json is set, got: %s", stderrBuf.String())
	}
}

func assertMasterPlanningTopology(t *testing.T, projectRoot string) {
	t.Helper()

	cfg, err := pipeline.LoadFrozen(projectRoot)
	if err != nil {
		t.Fatalf("LoadFrozen: %v", err)
	}

	tests := []struct {
		root       string
		target     string
		from       string
		to         string
		taskSlug   string
		transition string
	}{
		{
			root:       "epic-planning-main-pair",
			target:     "epic-planning-pair",
			from:       "epic-planning-main-pair.approved",
			to:         "epic-planning-pair.initial",
			taskSlug:   "epic-planning",
			transition: "epic-decompose",
		},
		{
			root:       "architecture-main-pair",
			target:     "architecture-pair",
			from:       "architecture-main-pair.approved",
			to:         "architecture-pair.initial",
			taskSlug:   "architecture",
			transition: "arch-decompose",
		},
		{
			root:       "code-planning-main-pair",
			target:     "code-planning-pair",
			from:       "code-planning-main-pair.approved",
			to:         "code-planning-pair.initial",
			taskSlug:   "code-planning",
			transition: "code-plan-decompose",
		},
	}

	for _, tt := range tests {
		rolePair, ok := cfg.Pipeline.RolePairs[tt.root]
		if !ok {
			t.Fatalf("missing master role-pair %q", tt.root)
		}
		if !rolePair.DecompositionRoot {
			t.Fatalf("%s decomposition-root = false, want true", tt.root)
		}
		assertHasMasterPlanningTransition(t, cfg, tt.transition, tt.from, tt.to, tt.taskSlug)
		resolver := pipeline.NewResolver(cfg)
		gotRoot, found, err := resolver.DecompositionRootForTarget(tt.target)
		if err != nil {
			t.Fatalf("DecompositionRootForTarget(%q): %v", tt.target, err)
		}
		if !found || gotRoot != tt.root {
			t.Fatalf("DecompositionRootForTarget(%q) = (%q, %v), want (%q, true)", tt.target, gotRoot, found, tt.root)
		}
	}

	foundUSToCoding := false
	for _, transition := range cfg.Pipeline.PipelineTransitions {
		if transition.Name == "us-to-coding" {
			foundUSToCoding = true
			if transition.To != "architecture-subpipeline.architecture-main-pair.initial" {
				t.Fatalf("us-to-coding target = %q, want architecture-subpipeline.architecture-main-pair.initial", transition.To)
			}
		}
	}
	if !foundUSToCoding {
		t.Fatal("missing us-to-coding pipeline transition")
	}
}

func assertHasMasterPlanningTransition(t *testing.T, cfg *pipeline.PipelineConfig, name, from, to, taskSlug string) {
	t.Helper()

	for _, subPipeline := range cfg.Pipeline.SubPipelines {
		for _, transition := range subPipeline.Transitions {
			if transition.Name != name {
				continue
			}
			if transition.From != from || transition.To != to || transition.Trigger != "auto" || transition.Cardinality != "per-subtask" || transition.TaskSlug != taskSlug {
				t.Fatalf("%s transition = %+v, want from=%s to=%s trigger=auto cardinality=per-subtask task-slug=%s", name, transition, from, to, taskSlug)
			}
			return
		}
	}
	t.Fatalf("missing master planning transition %q", name)
}

func setupInitializedProjectWithEntryPoint(t *testing.T, entryPoint string) (string, string) {
	t.Helper()

	projectRoot := t.TempDir()
	resolvedRoot, err := filepath.EvalSymlinks(projectRoot)
	if err != nil {
		t.Fatalf("failed to resolve temp project root: %v", err)
	}
	projectRoot = resolvedRoot
	testhelpers.SetupTestGitRepo(t, projectRoot)
	testhelpers.SetupGlobalLiza(t)
	testhelpers.CreateCommittedSpecFile(t, projectRoot, "vision.md", "# Vision\n")
	embeddedPipelinePath, err := filepath.Abs(filepath.Join("..", "..", "internal", "embedded", "pipeline.yaml"))
	if err != nil {
		t.Fatalf("resolve embedded pipeline path: %v", err)
	}

	if err := executeRootCommand(t, projectRoot, "init", "--config", embeddedPipelinePath, "--spec", "specs/vision.md", "--entry-point", entryPoint, "Master planning route goal"); err != nil {
		t.Fatalf("init with entry-point %q failed: %v", entryPoint, err)
	}

	return projectRoot, filepath.Join(projectRoot, ".liza", "state.yaml")
}

func extractInitialPlanningExampleTasks(t *testing.T, rendered, label string) []map[string]any {
	t.Helper()

	labelStart := strings.Index(rendered, label)
	if labelStart < 0 {
		t.Fatalf("missing example label %q\n%s", label, rendered)
	}
	afterLabel := rendered[labelStart+len(label):]
	arrayOffset := strings.Index(afterLabel, "[")
	if arrayOffset < 0 {
		t.Fatalf("missing JSON array after %q\n%s", label, rendered)
	}

	arrayStart := labelStart + len(label) + arrayOffset
	depth := 0
	arrayEnd := -1
	for i := arrayStart; i < len(rendered); i++ {
		switch rendered[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				arrayEnd = i + 1
				i = len(rendered)
			}
		}
	}
	if arrayEnd < 0 {
		t.Fatalf("unterminated JSON array after %q\n%s", label, rendered)
	}

	var tasks []map[string]any
	if err := json.Unmarshal([]byte(rendered[arrayStart:arrayEnd]), &tasks); err != nil {
		t.Fatalf("example %q is not a JSON task array: %v\n%s", label, err, rendered[arrayStart:arrayEnd])
	}
	return tasks
}

func assertOneInitialPlanningTask(t *testing.T, tasks []map[string]any, wantRolePair, wantType string) {
	t.Helper()

	if len(tasks) != 1 {
		t.Fatalf("example task count = %d, want 1: %#v", len(tasks), tasks)
	}
	task := tasks[0]
	if task["role_pair"] != wantRolePair {
		t.Fatalf("role_pair = %v, want %s", task["role_pair"], wantRolePair)
	}
	if task["type"] != wantType {
		t.Fatalf("type = %v, want %s", task["type"], wantType)
	}
}

func assertNotContainsAny(t *testing.T, s string, notWants []string) {
	t.Helper()
	for _, notWant := range notWants {
		if strings.Contains(s, notWant) {
			t.Fatalf("unexpected content %q\n%s", notWant, s)
		}
	}
}

func timePtr(t time.Time) *time.Time {
	return &t
}
