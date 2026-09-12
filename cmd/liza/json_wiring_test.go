package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/commands"
	activitylog "github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/prompts"
	"github.com/liza-mas/liza/internal/statehygiene"
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
	var buf bytes.Buffer
	copyDone := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(&buf, r)
		copyDone <- copyErr
	}()

	cmdErr := executeRootCommand(t, projectRoot, args...)

	os.Stdout = oldStdout
	if closeErr := w.Close(); closeErr != nil {
		t.Fatalf("failed to close captured stdout: %v", closeErr)
	}
	if copyErr := <-copyDone; copyErr != nil {
		t.Fatalf("failed to read captured stdout: %v", copyErr)
	}
	if closeErr := r.Close(); closeErr != nil {
		t.Fatalf("failed to close captured stdout reader: %v", closeErr)
	}

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

func TestJSON_ConfigReadWrite(t *testing.T) {
	projectRoot, statePath := setupMutationTestProject(t, nil)
	for _, query := range [][]string{{"config", "get", ops.PostWorktreeConfigKey, "--json"}, {"get", ops.PostWorktreeConfigKey, "--json"}} {
		stdout, err := executeRootCommandCapture(t, projectRoot, query...)
		if err != nil {
			t.Fatal(err)
		}
		env := parseEnvelope(t, stdout)
		if env["ok"] != true || env["result"] != nil {
			t.Fatalf("unset read = %s", stdout)
		}
	}
	for _, tc := range []struct {
		command, outcome string
		flags            []string
	}{
		{"make setup", "set", nil},
		{"make setup", "unchanged", nil},
		{"make corrected", "replaced", []string{"--replace", "--reason", "Correct bootstrap"}},
	} {
		args := append([]string{"config", "set", ops.PostWorktreeConfigKey, tc.command, "--json"}, tc.flags...)
		stdout, err := executeRootCommandCapture(t, projectRoot, args...)
		if err != nil {
			t.Fatal(err)
		}
		env := parseEnvelope(t, stdout)
		result, ok := env["result"].(map[string]any)
		if env["ok"] != true || !ok || result["key"] != ops.PostWorktreeConfigKey || result["outcome"] != tc.outcome {
			t.Fatalf("set = %s", stdout)
		}
	}
	for _, query := range [][]string{{"config", "get", ops.PostWorktreeConfigKey, "--json"}, {"get", ops.PostWorktreeConfigKey, "--json"}} {
		stdout, err := executeRootCommandCapture(t, projectRoot, query...)
		if err != nil {
			t.Fatal(err)
		}
		if parseEnvelope(t, stdout)["result"] != "make corrected" {
			t.Fatalf("configured read = %s", stdout)
		}
	}
	stdout, err := executeRootCommandCapture(t, projectRoot, "config", "set", ops.PostWorktreeConfigKey, "make conflict", "--json")
	if err == nil {
		t.Fatal("conflicting set returned success")
	}
	assertJSONError(t, stdout, "validation", "already set", "--replace")
	details := parseEnvelope(t, stdout)["error"].(map[string]any)["details"].(map[string]any)
	if details["key"] != ops.PostWorktreeConfigKey || details["conflict"] != "existing_value" {
		t.Fatalf("conflict details = %v", details)
	}
	if got := readState(t, statePath).Config.PostWorktreeCmd; got == nil || *got != "make corrected" {
		t.Fatal("conflict changed config")
	}
}

func TestJSON_ConfigInvalidInput(t *testing.T) {
	projectRoot, statePath := setupMutationTestProject(t, nil)
	for _, args := range [][]string{
		{"config", "get"},
		{"config", "get", "config.mode"},
		{"config", "set", ops.PostWorktreeConfigKey},
		{"config", "set", "post-worktree-cmd", "make setup"},
		{"config", "set", ops.PostWorktreeConfigKey, ""},
		{"config", "set", ops.PostWorktreeConfigKey, "make setup\nnext"},
		{"config", "set", ops.PostWorktreeConfigKey, "make setup", "--replace"},
		{"config", "set", ops.PostWorktreeConfigKey, "make setup", "--replace", "--reason", "--json"},
	} {
		stdout, err := executeRootCommandCapture(t, projectRoot, append(args, "--json")...)
		if err == nil {
			t.Fatalf("invalid invocation succeeded: %v", args)
		}
		assertJSONError(t, stdout, "validation")
	}
	if readState(t, statePath).Config.PostWorktreeCmd != nil {
		t.Fatal("invalid invocation changed config")
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

func TestJSON_SubmitVerdict_OversizedReasonIsActionableAndSideEffectFree(t *testing.T) {
	projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
		now := time.Now().UTC()
		state.Tasks = []models.Task{
			testhelpers.BuildTaskByStatus("task-json-review", models.TaskStatusReviewing, now),
		}
	})
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("ReadFile() before submit-verdict: %v", err)
	}
	reason := strings.Repeat("x", statehygiene.MaxStateTextBytes+1)

	stdout, err := executeRootCommandCapture(t, projectRoot,
		"submit-verdict", "task-json-review", "REJECTED",
		"--review-commit", quarantinedVerdictTestCommit,
		"--reason", reason,
		"--agent-id", "code-reviewer-1",
		"--json",
	)
	if err == nil {
		t.Fatal("expected oversized rejection reason error, got nil")
	}
	assertJSONError(t, stdout, "validation",
		"4097 bytes",
		"4096-byte maximum",
		paths.ProjectDirName()+"/agent-outputs/",
		"bounded summary",
		"artifact reference",
	)

	after, readErr := os.ReadFile(statePath)
	if readErr != nil {
		t.Fatalf("ReadFile() after submit-verdict: %v", readErr)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("oversized rejection changed state")
	}
	if _, statErr := os.Stat(filepath.Join(projectRoot, paths.ProjectDirName(), "log.yaml")); !os.IsNotExist(statErr) {
		t.Fatalf("oversized rejection created activity log: %v", statErr)
	}
}

func TestJSON_SubmitVerdict_ReasonConsumedJSONFlagIsActionableAndSideEffectFree(t *testing.T) {
	projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
		now := time.Now().UTC()
		state.Tasks = []models.Task{
			testhelpers.BuildTaskByStatus("task-json-consumed-reason", models.TaskStatusReviewing, now),
		}
	})
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("ReadFile() before submit-verdict: %v", err)
	}

	// Reproduces an empty unquoted reason where --json becomes the consumed
	// value. The root hook must still honor the intended JSON output mode.
	stdout, err := executeRootCommandCapture(t, projectRoot,
		"submit-verdict", "task-json-consumed-reason", "REJECTED",
		"--reason", "--json",
		"--agent-id", "code-reviewer-1",
	)
	if err == nil {
		t.Fatal("expected consumed --json reason error, got nil")
	}
	assertJSONError(t, stdout, "validation",
		"--reason",
		"registered flag --json",
		"empty shell expansion",
		"Only registered flag tokens are detected",
	)

	after, readErr := os.ReadFile(statePath)
	if readErr != nil {
		t.Fatalf("ReadFile() after submit-verdict: %v", readErr)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("consumed --json rejection reason changed state")
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

func TestJSON_AddTasks_DegradedStateIncludesItemWarning(t *testing.T) {
	projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
		now := time.Now().UTC()
		state.Tasks = append(state.Tasks, models.Task{
			ID:          "invalid-existing-task",
			Description: "Invalid existing task",
			Status:      models.TaskStatusImplementing,
			RolePair:    "coding-pair",
			Priority:    1,
			SpecRef:     "specs/vision.md",
			DoneWhen:    "done",
			Scope:       "scope",
			Created:     now,
			History:     []models.TaskHistoryEntry{},
		})
	})
	testhelpers.CreateSpecFile(t, projectRoot, "vision.md", "# Vision\n")
	testhelpers.CreateSpecFile(t, projectRoot, "feature.md", "# Feature\n")

	tasks := []map[string]any{
		{
			"id":        "repair-task",
			"desc":      "Repair degraded state",
			"spec":      "specs/feature.md",
			"done":      "repair task exists",
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
		t.Fatalf("expected ok=true for degraded-state repair add, got %v", env["ok"])
	}
	result, ok := env["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result to be object, got %T", env["result"])
	}
	results, ok := result["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("result.results = %v, want one item", result["results"])
	}
	item, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result item has type %T, want object", results[0])
	}
	warnings, ok := item["warnings"].([]any)
	if !ok || len(warnings) == 0 {
		t.Fatalf("item warnings = %v, want degraded-state warning", item["warnings"])
	}
	if warning, _ := warnings[0].(string); !strings.Contains(warning, "state remains degraded after add-task") {
		t.Fatalf("warning = %q, want degraded-state warning", warning)
	}
	state := readState(t, statePath)
	if state.FindTask("repair-task") == nil {
		t.Fatal("repair-task was not persisted")
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
	projectRoot, _ := setupMutationTestProject(t, func(state *models.State) {
		delete(state.Agents, "orchestrator-1")
	})
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

func TestJSON_MarkBlocked_InvalidRepairEvidenceReportsValidExample(t *testing.T) {
	projectRoot, _ := setupMutationTestProject(t, func(state *models.State) {
		now := time.Now().UTC()
		state.Tasks = []models.Task{
			testhelpers.BuildTaskByStatus("task-invalid-repair-evidence", models.TaskStatusImplementing, now),
		}
	})

	stdout, err := executeRootCommandCapture(t, projectRoot,
		"mark-blocked", "task-invalid-repair-evidence",
		"--agent-id", "coder-1",
		"--reason", "Required state repair is orchestrator-only",
		"--questions", "Can the orchestrator repair the state?",
		"--repair-operation", "retarget-dependency",
		"--repair-target", "task-invalid-repair-evidence",
		"--repair-command", "liza retarget-dependency task-invalid-repair-evidence old-dep new-dep --json",
		"--repair-evidence", "retarget failed",
		"--repair-validation", "liza validate --json",
		"--json",
	)
	if err == nil {
		t.Fatalf("expected invalid repair evidence validation error, got nil")
	}
	assertJSONError(t, stdout, "validation", "valid examples", "exit_code=1 stderr=", "error=provider session thread not found")
}

func writeRepairRequestFile(t *testing.T, projectRoot string, request models.RepairRequest) string {
	t.Helper()
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal repair request: %v", err)
	}
	path := filepath.Join(projectRoot, "repair-request.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write repair request: %v", err)
	}
	return path
}

func declarativeRepairRequestFor(taskID string) models.RepairRequest {
	return models.RepairRequest{
		Operation: "apply-dependency-repair",
		Target:    taskID,
		DependencyUpdates: []models.DependencyUpdate{{
			TaskID:            "consumer-1",
			ExpectedDependsOn: []string{},
			DesiredDependsOn:  []string{"producer-1"},
		}},
		Evidence:   []string{"error=dependency repair requires orchestrator authority"},
		Validation: []string{"liza validate --json"},
	}
}

func TestJSON_AssessBlocked_ReconcilesCanonicalMetadata(t *testing.T) {
	resetFlagIfPresent(assessBlockedCmd, "question")
	t.Cleanup(func() { resetFlagIfPresent(assessBlockedCmd, "question") })
	projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
		now := time.Now().UTC()
		reason := "obsolete blocker"
		task := testhelpers.BuildTaskByStatus("task-assess-repair", models.TaskStatusBlocked, now)
		task.AssignedTo = nil
		task.Worktree = nil
		task.BlockedReason = &reason
		task.BlockedQuestions = []string{"obsolete question"}
		state.Tasks = []models.Task{task}
		state.Agents["orchestrator-1"] = testhelpers.RegisteredTestAgent("orchestrator")
	})
	testhelpers.CreateSpecFile(t, projectRoot, "vision.md", "# Vision\n")

	commandRepairWant := models.RepairRequest{
		Operation:  "retarget-dependency",
		Target:     "task-assess-repair",
		Command:    "liza retarget-dependency task-assess-repair stale replacement --json",
		Evidence:   []string{"command=retarget exit_code=1 stderr=orchestrator authority required"},
		Validation: []string{"liza validate --json"},
	}
	stdout, err := executeRootCommandCapture(t, projectRoot,
		"assess-blocked", "task-assess-repair",
		"--agent-id", "orchestrator-1",
		"--reason", "current command blocker",
		"--question", "Can the orchestrator retarget the dependency?",
		"--repair-operation", commandRepairWant.Operation,
		"--repair-target", commandRepairWant.Target,
		"--repair-command", commandRepairWant.Command,
		"--repair-evidence", commandRepairWant.Evidence[0],
		"--repair-validation", commandRepairWant.Validation[0],
		"--json",
	)
	if err != nil {
		t.Fatalf("command-style assess-blocked failed: %v\n%s", err, stdout)
	}
	commandResult := parseEnvelope(t, stdout)["result"].(map[string]any)
	if commandResult["reason"] != "current command blocker" {
		t.Fatalf("reason = %v, want current command blocker", commandResult["reason"])
	}
	if !reflect.DeepEqual(commandResult["questions"], []any{"Can the orchestrator retarget the dependency?"}) {
		t.Fatalf("questions = %#v, want exact current question", commandResult["questions"])
	}
	commandRepair := commandResult["repair_request"].(map[string]any)
	wantCommandRepairJSON := map[string]any{
		"operation":  commandRepairWant.Operation,
		"target":     commandRepairWant.Target,
		"command":    commandRepairWant.Command,
		"evidence":   []any{commandRepairWant.Evidence[0]},
		"validation": []any{commandRepairWant.Validation[0]},
	}
	if !reflect.DeepEqual(commandRepair, wantCommandRepairJSON) {
		t.Fatalf("repair_request = %#v, want %#v", commandRepair, wantCommandRepairJSON)
	}
	stored := readState(t, statePath).FindTask("task-assess-repair")
	if !reflect.DeepEqual(stored.RepairRequest, &commandRepairWant) {
		t.Fatalf("stored repair_request = %#v, want %#v", stored.RepairRequest, &commandRepairWant)
	}

	declarativeRepairWant := declarativeRepairRequestFor("task-assess-repair")
	requestPath := writeRepairRequestFile(t, projectRoot, declarativeRepairWant)
	resetFlagIfPresent(assessBlockedCmd, "question")
	stdout, err = executeRootCommandCapture(t, projectRoot,
		"assess-blocked", "task-assess-repair",
		"--agent-id", "orchestrator-1",
		"--reason", "current declarative blocker",
		"--question", "Can the orchestrator apply the dependency update?",
		"--question", "Can it validate the repaired graph?",
		"--repair-request-file", requestPath,
		"--json",
	)
	if err != nil {
		t.Fatalf("declarative assess-blocked failed: %v\n%s", err, stdout)
	}
	declarativeResult := parseEnvelope(t, stdout)["result"].(map[string]any)
	declarativeRepair := declarativeResult["repair_request"].(map[string]any)
	wantDeclarativeRepairJSON := map[string]any{
		"operation": declarativeRepairWant.Operation,
		"target":    declarativeRepairWant.Target,
		"dependency_updates": []any{map[string]any{
			"task_id":             "consumer-1",
			"expected_depends_on": []any{},
			"desired_depends_on":  []any{"producer-1"},
		}},
		"evidence":   []any{declarativeRepairWant.Evidence[0]},
		"validation": []any{declarativeRepairWant.Validation[0]},
	}
	if !reflect.DeepEqual(declarativeRepair, wantDeclarativeRepairJSON) {
		t.Fatalf("declarative repair_request = %#v, want %#v", declarativeRepair, wantDeclarativeRepairJSON)
	}
	stored = readState(t, statePath).FindTask("task-assess-repair")
	if stored.BlockedReason == nil || *stored.BlockedReason != "current declarative blocker" {
		t.Fatalf("stored blocked_reason = %v, want current declarative blocker", stored.BlockedReason)
	}
	if !reflect.DeepEqual(stored.BlockedQuestions, []string{"Can the orchestrator apply the dependency update?", "Can it validate the repaired graph?"}) {
		t.Fatalf("stored blocked_questions = %#v, want exact current questions", stored.BlockedQuestions)
	}
	if !reflect.DeepEqual(stored.RepairRequest, &declarativeRepairWant) {
		t.Fatalf("stored repair_request = %#v, want %#v", stored.RepairRequest, &declarativeRepairWant)
	}

	resetFlagIfPresent(assessBlockedCmd, "question")
	stdout, err = executeRootCommandCapture(t, projectRoot,
		"assess-blocked", "task-assess-repair",
		"--agent-id", "orchestrator-1",
		"--reason", "current blocker needs no repair request",
		"--question", "Can the task remain blocked pending an answer?",
		"--json",
	)
	if err != nil {
		t.Fatalf("repair-clearing assess-blocked failed: %v\n%s", err, stdout)
	}
	clearResult := parseEnvelope(t, stdout)["result"].(map[string]any)
	if _, present := clearResult["repair_request"]; present {
		t.Fatalf("repair-clearing result unexpectedly contains repair_request: %#v", clearResult)
	}
	if stored := readState(t, statePath).FindTask("task-assess-repair"); stored.RepairRequest != nil {
		t.Fatalf("stored repair_request = %#v, want cleared request", stored.RepairRequest)
	}

	t.Run("validation rejects malformed input before mutation", func(t *testing.T) {
		tests := []struct {
			name      string
			prepare   func(*testing.T, string) []string
			wantParts []string
		}{
			{
				name: "reason without question",
				prepare: func(_ *testing.T, _ string) []string {
					return []string{"--reason", "current blocker"}
				},
				wantParts: []string{"--question is required"},
			},
			{
				name: "question without reason",
				prepare: func(_ *testing.T, _ string) []string {
					return []string{"--question", "What remains blocked?"}
				},
				wantParts: []string{"--reason is required"},
			},
			{
				name: "too many questions",
				prepare: func(_ *testing.T, _ string) []string {
					return []string{"--reason", "current blocker", "--question", "one", "--question", "two", "--question", "three", "--question", "four"}
				},
				wantParts: []string{"at most 3 times"},
			},
			{
				name: "partial command-style repair",
				prepare: func(_ *testing.T, _ string) []string {
					return []string{"--reason", "current blocker", "--question", "What remains blocked?", "--repair-operation", "retarget-dependency"}
				},
				wantParts: []string{"--repair-target is required"},
			},
			{
				name: "empty request file",
				prepare: func(t *testing.T, root string) []string {
					path := filepath.Join(root, "empty-repair.json")
					if err := os.WriteFile(path, nil, 0o600); err != nil {
						t.Fatalf("write empty repair request: %v", err)
					}
					return []string{"--reason", "current blocker", "--question", "What remains blocked?", "--repair-request-file", path}
				},
				wantParts: []string{"repair request file is empty"},
			},
			{
				name: "unreadable request file",
				prepare: func(_ *testing.T, root string) []string {
					return []string{"--reason", "current blocker", "--question", "What remains blocked?", "--repair-request-file", filepath.Join(root, "missing-repair.json")}
				},
				wantParts: []string{"reading repair request file", "missing-repair.json"},
			},
			{
				name: "invalid request file",
				prepare: func(t *testing.T, root string) []string {
					path := filepath.Join(root, "invalid-repair.json")
					if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
						t.Fatalf("write invalid repair request: %v", err)
					}
					return []string{"--reason", "current blocker", "--question", "What remains blocked?", "--repair-request-file", path}
				},
				wantParts: []string{"parsing repair request file"},
			},
			{
				name: "incomplete request file",
				prepare: func(t *testing.T, root string) []string {
					path := filepath.Join(root, "incomplete-repair.json")
					if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
						t.Fatalf("write incomplete repair request: %v", err)
					}
					return []string{"--reason", "current blocker", "--question", "What remains blocked?", "--repair-request-file", path}
				},
				wantParts: []string{"repair request operation is required"},
			},
			{
				name: "mixed file and fields",
				prepare: func(t *testing.T, root string) []string {
					path := writeRepairRequestFile(t, root, declarativeRepairRequestFor("task-invalid-assess"))
					return []string{"--reason", "current blocker", "--question", "What remains blocked?", "--repair-request-file", path, "--repair-operation", "apply-dependency-repair"}
				},
				wantParts: []string{"--repair-request-file cannot be combined with --repair-"},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				root, invalidStatePath := setupMutationTestProject(t, func(state *models.State) {
					now := time.Now().UTC()
					reason := "unchanged blocker"
					task := testhelpers.BuildTaskByStatus("task-invalid-assess", models.TaskStatusBlocked, now)
					task.AssignedTo = nil
					task.Worktree = nil
					task.BlockedReason = &reason
					task.BlockedQuestions = []string{"unchanged question"}
					state.Tasks = []models.Task{task}
					state.Agents["orchestrator-1"] = testhelpers.RegisteredTestAgent("orchestrator")
				})
				testhelpers.CreateSpecFile(t, root, "vision.md", "# Vision\n")
				extraArgs := tt.prepare(t, root)
				before, readErr := os.ReadFile(invalidStatePath)
				if readErr != nil {
					t.Fatalf("read state before invalid assess: %v", readErr)
				}
				args := append([]string{"assess-blocked", "task-invalid-assess", "--agent-id", "orchestrator-1"}, extraArgs...)
				args = append(args, "--json")
				resetFlagIfPresent(assessBlockedCmd, "question")
				stdout, err := executeRootCommandCapture(t, root, args...)
				if err == nil {
					t.Fatal("expected validation error")
				}
				assertJSONError(t, stdout, "validation", tt.wantParts...)
				after, readErr := os.ReadFile(invalidStatePath)
				if readErr != nil {
					t.Fatalf("read state after invalid assess: %v", readErr)
				}
				if !bytes.Equal(after, before) {
					t.Fatal("invalid assess-blocked input changed state")
				}
			})
		}
	})
}

func TestJSON_MarkBlocked_RepairRequestFile(t *testing.T) {
	legacyCommand := "liza add-task --id architecture-2 --agent-id orchestrator-1 --json"
	projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
		now := time.Now().UTC()
		legacyTask := testhelpers.BuildTaskByStatus("task-legacy-repair", models.TaskStatusBlocked, now)
		legacyTask.AssignedTo = nil
		legacyTask.Worktree = nil
		legacyTask.RepairRequest = &models.RepairRequest{
			Operation:  "add-task",
			Target:     "architecture-2",
			Command:    legacyCommand,
			Evidence:   []string{"command requires role type [orchestrator]"},
			Validation: []string{"go test ./cmd/liza"},
		}
		state.Tasks = []models.Task{
			testhelpers.BuildTaskByStatus("task-file-repair", models.TaskStatusImplementing, now),
			legacyTask,
		}
		state.Agents["coder-1"] = testhelpers.RegisteredTestAgent("coder")
	})
	requestPath := writeRepairRequestFile(t, projectRoot, declarativeRepairRequestFor("task-file-repair"))
	stdout, err := executeRootCommandCapture(t, projectRoot,
		"mark-blocked", "task-file-repair",
		"--agent-id", "coder-1",
		"--reason", "Dependency graph repair is orchestrator-only",
		"--questions", "Can the orchestrator apply the stored repair?",
		"--repair-request-file", requestPath,
		"--json",
	)
	if err != nil {
		t.Fatalf("mark-blocked --repair-request-file failed: %v\n%s", err, stdout)
	}
	env := parseEnvelope(t, stdout)
	result, ok := env["result"].(map[string]any)
	if !ok {
		t.Fatalf("result = %T, want object", env["result"])
	}
	repairRequest, ok := result["repair_request"].(map[string]any)
	if !ok {
		t.Fatalf("repair_request = %T, want object", result["repair_request"])
	}
	if _, present := repairRequest["command"]; present {
		t.Fatalf("repair_request unexpectedly contains command: %#v", repairRequest)
	}
	state := readState(t, statePath)
	stored := state.FindTask("task-file-repair").RepairRequest
	if stored == nil || len(stored.DependencyUpdates) != 1 {
		t.Fatalf("stored RepairRequest = %#v, want one dependency update", stored)
	}
	if stored.DependencyUpdates[0].ExpectedDependsOn == nil {
		t.Fatal("expected_depends_on must remain an explicit empty list")
	}

	stdout, err = executeRootCommandCapture(t, projectRoot,
		"mark-blocked", "task-file-repair",
		"--agent-id", "coder-1",
		"--reason", "Dependency graph repair is orchestrator-only",
		"--questions", "Can the orchestrator apply the stored repair?",
		"--repair-request-file", requestPath,
		"--repair-operation", "apply-dependency-repair",
		"--json",
	)
	if err == nil {
		t.Fatal("expected mixed repair flag validation error")
	}
	assertJSONError(t, stdout, "validation", "--repair-request-file cannot be combined with --repair-")

	stdout, err = executeRootCommandCapture(t, projectRoot, "get", "task-legacy-repair", "--json")
	if err != nil {
		t.Fatalf("inspect legacy repair request failed: %v\n%s", err, stdout)
	}
	legacyEnv := parseEnvelope(t, stdout)
	legacyResult, ok := legacyEnv["result"].(map[string]any)
	if !ok {
		t.Fatalf("legacy result = %T, want object", legacyEnv["result"])
	}
	legacyRepairRequest, ok := legacyResult["repair_request"].(map[string]any)
	if !ok {
		t.Fatalf("legacy repair_request = %T, want object", legacyResult["repair_request"])
	}
	if legacyRepairRequest["command"] != legacyCommand {
		t.Fatalf("legacy repair_request.command = %v, want %q", legacyRepairRequest["command"], legacyCommand)
	}
}

func TestJSON_RetargetDependency_Success(t *testing.T) {
	projectRoot, _ := setupMutationTestProject(t, func(state *models.State) {
		now := time.Now().UTC()
		state.Goal.SpecRef = "README.md"
		task := testhelpers.BuildTaskByStatus("task-json-retarget", models.TaskStatusBlocked, now)
		task.DependsOn = []string{"old-dep"}
		state.Tasks = []models.Task{
			task,
			testhelpers.BuildTaskByStatus("old-dep", models.TaskStatusMerged, now),
			testhelpers.BuildTaskByStatus("new-dep", models.TaskStatusMerged, now),
		}
	})

	stdout, err := executeRootCommandCapture(t, projectRoot,
		"retarget-dependency", "task-json-retarget", "old-dep", "new-dep",
		"--reason", "Correct dependency edge",
		"--agent-id", "orchestrator-1",
		"--json",
	)
	if err != nil {
		t.Fatalf("retarget-dependency --json failed: %v\n%s", err, stdout)
	}

	env := parseEnvelope(t, stdout)
	if env["ok"] != true {
		t.Fatalf("expected ok=true, got %v", env["ok"])
	}
	result, ok := env["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object, got %T", env["result"])
	}
	if result["task_id"] != "task-json-retarget" {
		t.Fatalf("task_id = %v, want task-json-retarget", result["task_id"])
	}
	if result["old_dependency"] != "old-dep" {
		t.Fatalf("old_dependency = %v, want old-dep", result["old_dependency"])
	}
}

func TestJSON_RetargetDependency_RejectsTransitiveCycle(t *testing.T) {
	const sentinel = "retarget-cycle-cli-secret"
	t.Setenv("RETARGET_TOKEN", sentinel)

	projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
		now := time.Now().UTC()
		state.Goal.SpecRef = "README.md"
		taskA := testhelpers.BuildTaskByStatus("A", models.TaskStatusBlocked, now)
		taskA.DependsOn = []string{"old-dep"}
		taskA.RepairRequest = &models.RepairRequest{
			Operation:  "retarget-dependency",
			Target:     "A",
			Command:    "liza retarget-dependency A old-dep B --json",
			Evidence:   []string{"error=dependency graph needs repair"},
			Validation: []string{"liza validate --json"},
		}
		taskB := testhelpers.BuildTaskByStatus("B", models.TaskStatusReady, now)
		taskB.DependsOn = []string{"C"}
		taskC := testhelpers.BuildTaskByStatus("C", models.TaskStatusReady, now)
		taskC.DependsOn = []string{"A"}
		state.Tasks = []models.Task{
			taskA,
			testhelpers.BuildTaskByStatus("old-dep", models.TaskStatusMerged, now),
			taskB,
			taskC,
		}
	})
	before := mustFindTask(t, readState(t, statePath), "A")

	oldStderr := os.Stderr
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stderr pipe: %v", err)
	}
	os.Stderr = stderrW

	stdout, cmdErr := executeRootCommandCapture(t, projectRoot,
		"retarget-dependency", "A", "old-dep", "B",
		"--reason", "token="+sentinel+" prove transitive cycle rejection",
		"--agent-id", "orchestrator-1",
		"--json", "-v",
	)

	stderrW.Close()
	os.Stderr = oldStderr
	var stderrBuf bytes.Buffer
	if _, copyErr := io.Copy(&stderrBuf, stderrR); copyErr != nil {
		t.Fatalf("failed to read stderr: %v", copyErr)
	}
	stderrR.Close()
	stderr := stderrBuf.String()

	if cmdErr == nil {
		t.Fatal("expected dependency-cycle rejection, got nil")
	}
	envDecoder := json.NewDecoder(strings.NewReader(stdout))
	var env map[string]any
	if err := envDecoder.Decode(&env); err != nil {
		t.Fatalf("decode stdout envelope: %v\nraw output: %s", err, stdout)
	}
	if err := assertJSONStreamEOF(envDecoder); err != nil {
		t.Fatalf("stdout contains output beyond one JSON envelope: %v", err)
	}
	if env["ok"] != false {
		t.Fatalf("expected ok=false, got %v", env["ok"])
	}
	errObj, ok := env["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %T", env["error"])
	}
	if errObj["code"] != "validation" {
		t.Fatalf("error.code = %v, want validation", errObj["code"])
	}
	const wantMessage = "retarget dependency rejected because the candidate state contains a dependency cycle"
	if errObj["message"] != wantMessage {
		t.Fatalf("error.message = %q, want %q", errObj["message"], wantMessage)
	}
	wantDetails := map[string]any{
		"operation":         "retarget-dependency",
		"task_id":           "A",
		"old_dependency":    "old-dep",
		"new_dependencies":  []any{"B"},
		"phase":             "candidate-state-validation",
		"cycle_path":        []any{"A", "B", "C", "A"},
		"diagnostic_action": "retarget_dependency_rejected",
	}
	if !reflect.DeepEqual(errObj["details"], wantDetails) {
		t.Fatalf("error.details = %#v, want %#v", errObj["details"], wantDetails)
	}

	stderrDecoder := json.NewDecoder(strings.NewReader(stderr))
	var diagnostic map[string]any
	if err := stderrDecoder.Decode(&diagnostic); err != nil {
		t.Fatalf("decode verbose stderr diagnostic: %v\nraw output: %s", err, stderr)
	}
	if err := assertJSONStreamEOF(stderrDecoder); err != nil {
		t.Fatalf("stderr contains output beyond one safe diagnostic: %v", err)
	}
	if len(diagnostic) != 2 || diagnostic["message"] != wantMessage || !reflect.DeepEqual(diagnostic["details"], wantDetails) {
		t.Fatalf("verbose stderr diagnostic = %#v, want safe message and details", diagnostic)
	}
	if strings.Contains(stdout, sentinel) || strings.Contains(stderr, sentinel) {
		t.Fatalf("CLI output leaked sentinel: stdout=%q stderr=%q", stdout, stderr)
	}

	after := mustFindTask(t, readState(t, statePath), "A")
	if !reflect.DeepEqual(after.DependsOn, before.DependsOn) {
		t.Fatalf("DependsOn changed after rejected retarget: got %v, want %v", after.DependsOn, before.DependsOn)
	}
	if !reflect.DeepEqual(after.History, before.History) {
		t.Fatalf("History changed after rejected retarget: got %#v, want %#v", after.History, before.History)
	}
	if !reflect.DeepEqual(after.RepairRequest, before.RepairRequest) {
		t.Fatalf("RepairRequest changed after rejected retarget: got %#v, want %#v", after.RepairRequest, before.RepairRequest)
	}

	entries, err := activitylog.New(filepath.Join(projectRoot, paths.ProjectDirName(), "log.yaml")).Read()
	if err != nil {
		t.Fatalf("read activity log: %v", err)
	}
	var rejected []activitylog.Entry
	for _, entry := range entries {
		switch entry.Action {
		case "retarget_dependency_rejected":
			rejected = append(rejected, entry)
		case "retarget-dependency":
			t.Fatalf("rejected retarget recorded success activity: %#v", entry)
		}
	}
	if len(rejected) != 1 {
		t.Fatalf("retarget_dependency_rejected activity count = %d, want 1; entries=%#v", len(rejected), entries)
	}
	if rejected[0].Task == nil || *rejected[0].Task != "A" || !strings.Contains(rejected[0].Detail, "cycle_path=A -> B -> C -> A") {
		t.Fatalf("rejection activity = %#v, want task A and complete cycle path", rejected[0])
	}
	if strings.Contains(rejected[0].Detail, sentinel) {
		t.Fatalf("rejection activity leaked sentinel: %q", rejected[0].Detail)
	}
}

func TestJSON_UnblockTask_PendingDependencyReportsNotClaimable(t *testing.T) {
	projectRoot, _ := setupMutationTestProject(t, func(state *models.State) {
		now := time.Now().UTC()
		task := testhelpers.BuildTaskByStatus("task-json-unblock", models.TaskStatusBlocked, now)
		task.RolePair = "code-planning-pair"
		task.Worktree = nil
		task.BaseCommit = nil
		task.AssignedTo = nil
		task.LeaseExpires = nil
		task.DependsOn = []string{"task-json-pending-dependency"}
		dependency := testhelpers.BuildTaskByStatus("task-json-pending-dependency", models.TaskStatusImplementing, now)
		dependency.RolePair = "code-planning-pair"
		state.Tasks = []models.Task{task, dependency}
	})

	stdout, err := executeRootCommandCapture(t, projectRoot,
		"unblock-task", "task-json-unblock",
		"--reason", "repair verified",
		"--agent-id", "orchestrator-1",
		"--json",
	)
	if err != nil {
		t.Fatalf("unblock-task --json failed: %v\n%s", err, stdout)
	}

	env := parseEnvelope(t, stdout)
	if env["ok"] != true {
		t.Fatalf("expected ok=true, got %v", env["ok"])
	}
	result, ok := env["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object, got %T", env["result"])
	}
	if result["claimable"] != false {
		t.Fatalf("claimable = %v, want false", result["claimable"])
	}
	if result["to_status"] != string(models.TaskStatusDraftCodingPlan) {
		t.Fatalf("to_status = %v, want %s", result["to_status"], models.TaskStatusDraftCodingPlan)
	}
}

func TestJSON_UnblockTask_AssignToRejectsPendingDependency(t *testing.T) {
	projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
		now := time.Now().UTC()
		task := testhelpers.BuildTaskByStatus("task-json-unblock-direct", models.TaskStatusBlocked, now)
		task.RolePair = "code-planning-pair"
		task.Worktree = nil
		task.BaseCommit = nil
		task.AssignedTo = nil
		task.LeaseExpires = nil
		task.DependsOn = []string{"task-json-pending-dependency"}
		dependency := testhelpers.BuildTaskByStatus("task-json-pending-dependency", models.TaskStatusImplementing, now)
		dependency.RolePair = "code-planning-pair"
		state.Tasks = []models.Task{task, dependency}
	})

	stdout, err := executeRootCommandCapture(t, projectRoot,
		"unblock-task", "task-json-unblock-direct",
		"--assign-to", "code-planner-1",
		"--reason", "repair verified",
		"--agent-id", "orchestrator-1",
		"--json",
	)
	if err == nil {
		t.Fatal("expected --assign-to rejection while dependency is pending")
	}
	assertJSONError(t, stdout, "validation", "has unmet dependencies")

	task := mustFindTask(t, readState(t, statePath), "task-json-unblock-direct")
	if task.Status != models.TaskStatusBlocked || task.AssignedTo != nil {
		t.Fatalf("rejected task changed: status=%s assigned_to=%v", task.Status, task.AssignedTo)
	}
}

func TestJSON_RetargetDependency_RejectsNonOrchestrator(t *testing.T) {
	projectRoot, _ := setupMutationTestProject(t, func(state *models.State) {
		now := time.Now().UTC()
		state.Goal.SpecRef = "README.md"
		task := testhelpers.BuildTaskByStatus("task-json-retarget-rbac", models.TaskStatusBlocked, now)
		task.DependsOn = []string{"old-dep"}
		state.Tasks = []models.Task{
			task,
			testhelpers.BuildTaskByStatus("old-dep", models.TaskStatusMerged, now),
			testhelpers.BuildTaskByStatus("new-dep", models.TaskStatusMerged, now),
		}
	})

	stdout, err := executeRootCommandCapture(t, projectRoot,
		"retarget-dependency", "task-json-retarget-rbac", "old-dep", "new-dep",
		"--reason", "Correct dependency edge",
		"--agent-id", "coder-1",
		"--json",
	)
	if err == nil {
		t.Fatalf("expected RBAC error, got nil")
	}
	assertJSONError(t, stdout, "permission_denied", `operation "retarget-dependency" not allowed for role "coder"`)
}

func TestJSON_ApplyDependencyRepair_ReportsEveryCanonicalUpdate(t *testing.T) {
	projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
		now := time.Now().UTC()
		state.Goal.SpecRef = "README.md"
		source := testhelpers.BuildTaskByStatus("repair-source", models.TaskStatusBlocked, now)
		source.DependsOn = []string{"old-source"}
		source.RepairRequest = &models.RepairRequest{
			Operation: models.RepairOperationApplyDependencyRepair,
			Target:    "repair-source",
			DependencyUpdates: []models.DependencyUpdate{
				{TaskID: "repair-source", ExpectedDependsOn: []string{"old-source"}, DesiredDependsOn: []string{"replacement-old"}},
				{TaskID: "consumer", ExpectedDependsOn: []string{"old-consumer"}, DesiredDependsOn: []string{}},
			},
			Evidence:   []string{"command=blocked-operation exit_code=1 stderr=orchestrator repair required"},
			Validation: []string{"validate repaired dependency graph"},
		}
		consumer := testhelpers.BuildTaskByStatus("consumer", models.TaskStatusReady, now)
		consumer.DependsOn = []string{"old-consumer"}
		replacementOld := testhelpers.BuildTaskByStatus("replacement-old", models.TaskStatusSuperseded, now)
		replacementOld.RolePair = "coding-pair"
		replacementOld.SupersededBy = []string{"replacement-new"}
		replacementOld.RescopeReason = testhelpers.StringPtr("Canonical replacement")
		state.Tasks = []models.Task{
			source,
			consumer,
			testhelpers.BuildTaskByStatus("old-source", models.TaskStatusMerged, now),
			testhelpers.BuildTaskByStatus("old-consumer", models.TaskStatusMerged, now),
			replacementOld,
			testhelpers.BuildTaskByStatus("replacement-new", models.TaskStatusMerged, now),
		}
	})

	stdout, err := executeRootCommandCapture(t, projectRoot,
		"apply-dependency-repair", "repair-source",
		"--reason", "Apply stored graph repair",
		"--agent-id", "orchestrator-1",
		"--json",
	)
	if err != nil {
		t.Fatalf("apply-dependency-repair --json failed: %v\n%s", err, stdout)
	}

	env := parseEnvelope(t, stdout)
	if env["ok"] != true {
		t.Fatalf("expected ok=true, got %v", env["ok"])
	}
	result, ok := env["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object, got %T", env["result"])
	}
	if result["source_task_id"] != "repair-source" {
		t.Fatalf("source_task_id = %v, want repair-source", result["source_task_id"])
	}
	updates, ok := result["updates"].([]any)
	if !ok || len(updates) != 2 {
		t.Fatalf("updates = %#v, want two entries", result["updates"])
	}
	first, ok := updates[0].(map[string]any)
	if !ok || first["task_id"] != "repair-source" {
		t.Fatalf("updates[0] = %#v, want repair-source", updates[0])
	}
	firstDeps, ok := first["canonical_dependencies"].([]any)
	if !ok || len(firstDeps) != 1 || firstDeps[0] != "replacement-new" {
		t.Fatalf("updates[0].canonical_dependencies = %#v, want [replacement-new]", first["canonical_dependencies"])
	}
	second, ok := updates[1].(map[string]any)
	if !ok || second["task_id"] != "consumer" {
		t.Fatalf("updates[1] = %#v, want consumer", updates[1])
	}
	secondDeps, ok := second["canonical_dependencies"].([]any)
	if !ok || len(secondDeps) != 0 {
		t.Fatalf("updates[1].canonical_dependencies = %#v, want []", second["canonical_dependencies"])
	}

	updatedState := readState(t, statePath)
	updatedSource := mustFindTask(t, updatedState, "repair-source")
	if updatedSource.Status != models.TaskStatusBlocked || updatedSource.RepairRequest != nil {
		t.Fatalf("source status/request = %s/%#v, want BLOCKED/nil", updatedSource.Status, updatedSource.RepairRequest)
	}
	if got := mustFindTask(t, updatedState, "consumer").DependsOn; len(got) != 0 {
		t.Fatalf("consumer DependsOn = %v, want empty", got)
	}
}

func TestJSON_ApplyDependencyRepair_RBAC(t *testing.T) {
	projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
		now := time.Now().UTC()
		source := testhelpers.BuildTaskByStatus("repair-source", models.TaskStatusBlocked, now)
		source.DependsOn = []string{"old-source"}
		source.RepairRequest = &models.RepairRequest{
			Operation: models.RepairOperationApplyDependencyRepair,
			Target:    "repair-source",
			DependencyUpdates: []models.DependencyUpdate{
				{TaskID: "repair-source", ExpectedDependsOn: []string{"old-source"}, DesiredDependsOn: []string{"new-source"}},
			},
			Evidence:   []string{"command=blocked-operation exit_code=1 stderr=orchestrator repair required"},
			Validation: []string{"validate repaired dependency graph"},
		}
		state.Tasks = []models.Task{
			source,
			testhelpers.BuildTaskByStatus("old-source", models.TaskStatusMerged, now),
			testhelpers.BuildTaskByStatus("new-source", models.TaskStatusMerged, now),
		}
	})

	stdout, err := executeRootCommandCapture(t, projectRoot,
		"apply-dependency-repair", "repair-source",
		"--reason", "Apply stored graph repair",
		"--agent-id", "coder-1",
		"--json",
	)
	if err == nil {
		t.Fatal("expected RBAC error, got nil")
	}
	assertJSONError(t, stdout, "permission_denied", `operation "apply-dependency-repair" not allowed for role "coder"`)
	unchangedSource := mustFindTask(t, readState(t, statePath), "repair-source")
	if len(unchangedSource.DependsOn) != 1 || unchangedSource.DependsOn[0] != "old-source" || unchangedSource.RepairRequest == nil {
		t.Fatalf("source changed after rejected command: %#v", unchangedSource)
	}
}

func TestJSON_RepairSupersededDependencies_Success(t *testing.T) {
	projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
		now := time.Now().UTC()
		state.Goal.SpecRef = "README.md"
		target := testhelpers.BuildTaskByStatus("plan-old", models.TaskStatusSuperseded, now)
		target.RolePair = "code-planning-pair"
		target.DependsOn = []string{"coding-a", "legal-plan", "coding-b"}
		target.SupersededBy = []string{"replacement-plan"}
		target.RescopeReason = testhelpers.StringPtr("Replaced invalid plan")
		state.Tasks = []models.Task{
			target,
			testhelpers.BuildTaskByStatus("coding-a", models.TaskStatusReady, now),
			testhelpers.BuildTaskByStatus("legal-plan", models.TaskStatusDraftCodingPlan, now),
			testhelpers.BuildTaskByStatus("coding-b", models.TaskStatusReady, now),
			testhelpers.BuildTaskByStatus("replacement-plan", models.TaskStatusDraftCodingPlan, now),
		}
	})
	logPath := filepath.Join(projectRoot, paths.ProjectDirName(), "log.yaml")
	if err := os.RemoveAll(logPath); err != nil {
		t.Fatalf("remove log path: %v", err)
	}
	if err := os.Mkdir(logPath, 0o755); err != nil {
		t.Fatalf("mkdir log path: %v", err)
	}

	stdout, err := executeRootCommandCapture(t, projectRoot,
		"repair-superseded-dependencies", "plan-old",
		"--reason", "Repair terminal dependency metadata",
		"--agent-id", "orchestrator-1",
		"--json",
	)
	if err != nil {
		t.Fatalf("repair-superseded-dependencies --json failed: %v\n%s", err, stdout)
	}

	env := parseEnvelope(t, stdout)
	if env["ok"] != true {
		t.Fatalf("expected ok=true, got %v", env["ok"])
	}
	result, ok := env["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object, got %T", env["result"])
	}
	if result["task_id"] != "plan-old" {
		t.Fatalf("task_id = %v, want plan-old", result["task_id"])
	}
	removed, ok := result["removed_dependencies"].([]any)
	if !ok || len(removed) != 2 || removed[0] != "coding-a" || removed[1] != "coding-b" {
		t.Fatalf("removed_dependencies = %#v, want [coding-a coding-b]", result["removed_dependencies"])
	}
	retained, ok := result["retained_dependencies"].([]any)
	if !ok || len(retained) != 1 || retained[0] != "legal-plan" {
		t.Fatalf("retained_dependencies = %#v, want [legal-plan]", result["retained_dependencies"])
	}
	warnings, ok := env["warnings"].([]any)
	if !ok || len(warnings) != 1 || !strings.Contains(warnings[0].(string), "activity log write failed") {
		t.Fatalf("warnings = %#v, want activity log write failure", env["warnings"])
	}

	task := mustFindTask(t, readState(t, statePath), "plan-old")
	last := task.History[len(task.History)-1]
	if last.Agent == nil || *last.Agent != "orchestrator-1" {
		t.Fatalf("history agent = %v, want orchestrator-1", last.Agent)
	}
}

func TestJSON_RepairSupersededDependencies_RejectsNonOrchestrator(t *testing.T) {
	projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
		now := time.Now().UTC()
		target := testhelpers.BuildTaskByStatus("plan-old", models.TaskStatusSuperseded, now)
		target.RolePair = "code-planning-pair"
		target.DependsOn = []string{"coding-a"}
		state.Tasks = []models.Task{
			target,
			testhelpers.BuildTaskByStatus("coding-a", models.TaskStatusReady, now),
		}
	})

	stdout, err := executeRootCommandCapture(t, projectRoot,
		"repair-superseded-dependencies", "plan-old",
		"--reason", "Repair terminal dependency metadata",
		"--agent-id", "coder-1",
		"--json",
	)
	if err == nil {
		t.Fatal("expected RBAC error, got nil")
	}
	assertJSONError(t, stdout, "permission_denied", `operation "repair-superseded-dependencies" not allowed for role "coder"`)
	if got := mustFindTask(t, readState(t, statePath), "plan-old").DependsOn; len(got) != 1 || got[0] != "coding-a" {
		t.Fatalf("task DependsOn changed after rejected command: %v", got)
	}
}

func TestJSON_MarkBlocked_AlertWriteFailureReturnsWarning(t *testing.T) {
	projectRoot, _ := setupMutationTestProject(t, func(state *models.State) {
		now := time.Now().UTC()
		state.Tasks = []models.Task{
			testhelpers.BuildTaskByStatus("task-alert-warning", models.TaskStatusImplementing, now),
		}
		state.Agents["coder-1"] = testhelpers.RegisteredTestAgent("coder")
	})
	alertsPath := filepath.Join(projectRoot, paths.ProjectDirName(), "alerts.log")
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
	pipelinePath := filepath.Join(projectRoot, paths.ProjectDirName(), "pipeline.yaml")
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
	pipelinePath := filepath.Join(projectRoot, paths.ProjectDirName(), "pipeline.yaml")
	content, err := os.ReadFile(pipelinePath)
	if err != nil {
		t.Fatalf("read pipeline.yaml: %v", err)
	}
	withoutMasterMarkers := strings.ReplaceAll(string(content), "      decomposition-root: true\n      decomposition-output-ref: plan_ref\n", "")
	withoutMasterMarkers = strings.ReplaceAll(withoutMasterMarkers, "      decomposition-root: true\n      decomposition-output-ref: arch_ref\n", "")
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

func TestJSON_Validate_DefaultStatePathAcceptsOwnTaskWorktreeRoot(t *testing.T) {
	projectRoot, _ := setupMutationTestProject(t, nil)
	taskID := "task-validate-worktree"
	testhelpers.CreateTestWorktree(t, projectRoot, taskID)
	worktreeDir := filepath.Join(projectRoot, ".worktrees", taskID)

	stdout, err := executeRootCommandCapture(t, worktreeDir, "validate", "--json", "--skip-spec-check")
	if err != nil {
		t.Fatalf("validate from own task worktree failed: %v\n%s", err, stdout)
	}

	env := parseEnvelope(t, stdout)
	if env["ok"] != true {
		t.Fatalf("expected ok=true, got %v", env["ok"])
	}
}

func TestJSON_Validate_ProjectRootFlagFromUnrelatedDirectory(t *testing.T) {
	projectRoot, _ := setupMutationTestProject(t, nil)
	unrelatedDir := t.TempDir()

	stdout, err := executeRootCommandCapture(t, unrelatedDir, "-C", projectRoot, "validate", "--json", "--skip-spec-check")
	if err != nil {
		t.Fatalf("validate -C from unrelated directory failed: %v\n%s", err, stdout)
	}

	env := parseEnvelope(t, stdout)
	if env["ok"] != true {
		t.Fatalf("expected ok=true, got %v", env["ok"])
	}
}

func TestJSON_Validate_ProjectRootFlagRejectsNonGitTarget(t *testing.T) {
	projectRoot, _ := setupMutationTestProject(t, nil)
	nonGitDir := t.TempDir()

	stdout, err := executeRootCommandCapture(t, projectRoot, "-C", nonGitDir, "validate", "--json", "--skip-spec-check")
	if err == nil {
		t.Fatalf("expected project root error for non-git -C target")
	}

	assertJSONError(t, stdout, "project_root", "not a git repository")
}

func TestJSON_Validate_ProjectRootFlagRejectsGitRepoWithoutLizaMarker(t *testing.T) {
	projectRoot, _ := setupMutationTestProject(t, nil)
	nonLizaGitRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, nonLizaGitRoot)

	stdout, err := executeRootCommandCapture(t, projectRoot, "-C", nonLizaGitRoot, "validate", "--json", "--skip-spec-check")
	if err == nil {
		t.Fatalf("expected project root error for non-Liza -C target")
	}

	assertJSONError(t, stdout, "project_root", "missing "+paths.ProjectDirName()+" directory")
}

func TestJSON_Validate_DefaultStatePathRejectsExternalLinkedWorktree(t *testing.T) {
	projectRoot, _ := setupMutationTestProject(t, nil)
	externalWorktree := filepath.Join(t.TempDir(), "external-task")
	testhelpers.MustGit(t, projectRoot, "worktree", "add", externalWorktree, "integration", "-b", "task/external-linked")

	stdout, err := executeRootCommandCapture(t, externalWorktree, "validate", "--json", "--skip-spec-check")
	if err == nil {
		t.Fatalf("expected project root error from external linked worktree, got nil")
	}

	assertJSONError(t, stdout, "project_root", "must be run from project root")
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
	statePath := filepath.Join(projectRoot, paths.ProjectDirName(), "state.yaml")
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
	pipelinePath := filepath.Join(projectRoot, paths.ProjectDirName(), "pipeline.yaml")
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

func TestJSON_SubmitForReviewFromOwnTaskWorktreeRoot(t *testing.T) {
	projectRoot, statePath, taskID, agentID := setupSubmitForReviewCLIProject(t)

	worktreeDir := filepath.Join(projectRoot, ".worktrees", taskID)
	stdout, err := executeRootCommandCapture(t, worktreeDir,
		"submit-for-review", taskID, "HEAD", "--agent-id", agentID, "--json")
	if err != nil {
		t.Fatalf("submit-for-review from own task worktree failed: %v\n%s", err, stdout)
	}

	env := parseEnvelope(t, stdout)
	if env["ok"] != true {
		t.Fatalf("expected ok=true, got %v", env["ok"])
	}

	state := readState(t, statePath)
	task := state.FindTask(taskID)
	if task == nil {
		t.Fatalf("task %s not found", taskID)
	}
	if task.Status != models.TaskStatusReadyForReview {
		t.Fatalf("task status = %s, want READY_FOR_REVIEW", task.Status)
	}
	if task.ReviewCommit == nil || *task.ReviewCommit == "" {
		t.Fatalf("ReviewCommit = %v, want non-empty", task.ReviewCommit)
	}
}

func TestJSON_GetZombieWarningsAreIncluded(t *testing.T) {
	projectRoot, _ := setupMutationTestProject(t, nil)

	originalInspect := inspectCommand
	t.Cleanup(func() { inspectCommand = originalInspect })

	const warning = "WARNING: Live liza agent process scan partial: unable to verify project scope for pid 3456 role architect (cwd_unreadable); these processes were not classified as zombies"
	inspectCommand = func(args []string, opts commands.InspectOptions) (string, error) {
		if !opts.Zombies {
			t.Fatal("InspectOptions.Zombies = false, want true")
		}
		if opts.WarnWriter == nil {
			t.Fatal("InspectOptions.WarnWriter = nil, want buffered warning writer")
		}
		if _, err := io.WriteString(opts.WarnWriter, warning+"\n"); err != nil {
			t.Fatalf("write warning: %v", err)
		}
		return "[]", nil
	}

	stdout, err := executeRootCommandCapture(t, projectRoot, "get", "agents", "--zombies", "--json")
	if err != nil {
		t.Fatalf("get agents --zombies --json failed: %v", err)
	}

	env := parseEnvelope(t, stdout)
	warnings, ok := env["warnings"].([]any)
	if !ok {
		t.Fatalf("warnings = %T, want array", env["warnings"])
	}
	if len(warnings) != 1 || warnings[0] != warning {
		t.Fatalf("warnings = %#v, want [%q]", warnings, warning)
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

func TestJSON_GetTaskExposesRCARequired(t *testing.T) {
	for _, tc := range []struct {
		name        string
		rcaRequired bool
		wantPresent bool
	}{
		{name: "defect task exposes the flag", rcaRequired: true, wantPresent: true},
		{name: "feature task omits the flag", rcaRequired: false, wantPresent: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			projectRoot, _ := setupMutationTestProject(t, func(state *models.State) {
				now := time.Now().UTC()
				task := testhelpers.BuildTaskByStatus("task-rca", models.TaskStatusImplementing, now)
				task.RCARequired = tc.rcaRequired
				state.Tasks = []models.Task{task}
			})

			stdout, err := executeRootCommandCapture(t, projectRoot, "get", "task-rca", "--json")
			if err != nil {
				t.Fatalf("get task-rca --json failed: %v", err)
			}

			env := parseEnvelope(t, stdout)
			if env["ok"] != true {
				t.Fatalf("expected ok=true, got %v", env["ok"])
			}
			task, ok := env["result"].(map[string]any)
			if !ok {
				t.Fatalf("expected task object, got %T", env["result"])
			}
			value, present := task["rca_required"]
			if present != tc.wantPresent {
				t.Fatalf("rca_required present = %v, want %v (task: %v)", present, tc.wantPresent, task)
			}
			if tc.wantPresent && value != true {
				t.Errorf("rca_required = %v, want true", value)
			}
		})
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
				Generation:   testhelpers.TestAgentGeneration,
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
			taskSlug:   "ep",
			transition: "epic-decompose",
		},
		{
			root:       "architecture-main-pair",
			target:     "architecture-pair",
			from:       "architecture-main-pair.approved",
			to:         "architecture-pair.initial",
			taskSlug:   "ar",
			transition: "arch-decompose",
		},
		{
			root:       "code-planning-main-pair",
			target:     "code-planning-pair",
			from:       "code-planning-main-pair.approved",
			to:         "code-planning-pair.initial",
			taskSlug:   "cp",
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
	testhelpers.CreateCommittedPreCommitConfig(t, projectRoot)
	testhelpers.MustGit(t, projectRoot, "branch", "-f", "integration", "HEAD")
	embeddedPipelinePath, err := filepath.Abs(filepath.Join("..", "..", "internal", "embedded", "pipeline.yaml"))
	if err != nil {
		t.Fatalf("resolve embedded pipeline path: %v", err)
	}

	if err := executeRootCommand(t, projectRoot, "init", "--config", embeddedPipelinePath, "--spec", "specs/vision.md", "--entry-point", entryPoint, "Master planning route goal"); err != nil {
		t.Fatalf("init with entry-point %q failed: %v", entryPoint, err)
	}

	return projectRoot, filepath.Join(projectRoot, paths.ProjectDirName(), "state.yaml")
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
