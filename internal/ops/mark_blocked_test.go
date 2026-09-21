package ops

import (
	stderrors "errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestMarkBlocked_Validation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		taskID      string
		reason      string
		questions   []string
		agentID     string
		errContains string
	}{
		{
			name:   "empty task ID",
			reason: "blocked", questions: []string{"q1"}, agentID: "coder-1",
			errContains: "task ID is required",
		},
		{
			name:   "empty reason",
			taskID: "t1", questions: []string{"q1"}, agentID: "coder-1",
			errContains: "reason is required",
		},
		{
			name:   "empty agent ID",
			taskID: "t1", reason: "blocked", questions: []string{"q1"},
			errContains: "agent ID is required",
		},
		{
			name:   "no questions",
			taskID: "t1", reason: "blocked", questions: []string{}, agentID: "coder-1",
			errContains: "at least 1 question",
		},
		{
			name:   "too many questions",
			taskID: "t1", reason: "blocked", questions: []string{"q1", "q2", "q3", "q4"}, agentID: "coder-1",
			errContains: "maximum 3 questions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := MarkBlocked("/nonexistent", tt.taskID, tt.reason, tt.questions, tt.agentID)
			if err == nil {
				t.Fatal("Expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("Error = %q, want to contain %q", err.Error(), tt.errContains)
			}
		})
	}
}

func TestMarkBlocked_Success(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	questions := []string{"What is the API format?", "Where is the config?"}
	result, err := MarkBlocked(tmpDir, "task-1", "Missing API spec", questions, "coder-1")
	if err != nil {
		t.Fatalf("MarkBlocked() error: %v", err)
	}

	if result.TaskID != "task-1" {
		t.Errorf("TaskID = %q, want %q", result.TaskID, "task-1")
	}
	if result.Reason != "Missing API spec" {
		t.Errorf("Reason = %q, want %q", result.Reason, "Missing API spec")
	}

	// Verify state
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	task := readState.FindTask("task-1")
	if task == nil {
		t.Fatal("Task not found")
	}
	if task.Status != models.TaskStatusBlocked {
		t.Errorf("Status = %v, want BLOCKED", task.Status)
	}
	if task.BlockedReason == nil || *task.BlockedReason != "Missing API spec" {
		t.Error("BlockedReason not set correctly")
	}
	if len(task.BlockedQuestions) != 2 {
		t.Errorf("BlockedQuestions len = %d, want 2", len(task.BlockedQuestions))
	}
	if task.AssignedTo != nil {
		t.Error("AssignedTo should be nil after blocking")
	}
	if task.LeaseExpires != nil {
		t.Error("LeaseExpires should be nil after blocking")
	}

	// Verify history entry
	lastHistory := task.History[len(task.History)-1]
	if lastHistory.Event != models.TaskEventBlocked {
		t.Errorf("History event = %q, want %q", lastHistory.Event, models.TaskEventBlocked)
	}
}

func TestMarkBlockedWithOptions_DependsOnAndImmediateAlert(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
		testhelpers.BuildTaskByStatus("dep-1", models.TaskStatusImplementing, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := MarkBlockedWithOptions(
		tmpDir,
		"task-1",
		"Waiting on dep-1",
		[]string{"Should dep-1 merge first?"},
		"coder-1",
		MarkBlockedOptions{DependsOn: []string{" dep-1 "}},
	)
	if err != nil {
		t.Fatalf("MarkBlockedWithOptions() error: %v", err)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("Warnings = %v, want none", result.Warnings)
	}
	if len(result.DependsOn) != 1 || result.DependsOn[0] != "dep-1" {
		t.Fatalf("DependsOn = %v, want [dep-1]", result.DependsOn)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	task := readState.FindTask("task-1")
	if task == nil {
		t.Fatal("Task not found")
	}
	if len(task.DependsOn) != 1 || task.DependsOn[0] != "dep-1" {
		t.Fatalf("task.DependsOn = %v, want [dep-1]", task.DependsOn)
	}

	data, err := os.ReadFile(paths.New(tmpDir).AlertsLogPath())
	if err != nil {
		t.Fatalf("Read alerts.log: %v", err)
	}
	if !strings.Contains(string(data), "BLOCKED: task-1 — Waiting on dep-1") {
		t.Fatalf("alerts.log missing BLOCKED alert:\n%s", string(data))
	}
}

func TestMarkBlockedWithOptions_DependsOnValidation(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()

	setupState := func(t *testing.T) string {
		t.Helper()

		tmpDir := t.TempDir()
		testhelpers.SetupTestGitRepo(t, tmpDir)
		stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

		state := testhelpers.CreateValidState()
		dep := testhelpers.BuildTaskByStatus("dep-1", models.TaskStatusReady, now)
		dep.DependsOn = []string{"task-1"}
		state.Tasks = []models.Task{
			testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
			dep,
		}
		testhelpers.WriteInitialState(t, stateFile, state)

		return tmpDir
	}

	tests := []struct {
		name    string
		deps    []string
		wantErr string
	}{
		{name: "empty", deps: []string{" "}, wantErr: "depends-on entries cannot be empty"},
		{name: "duplicate", deps: []string{"dep-1", "dep-1"}, wantErr: "depends-on entries must be unique"},
		{name: "self", deps: []string{"task-1"}, wantErr: "cannot depend on itself"},
		{name: "missing", deps: []string{"missing"}, wantErr: "non-existent task"},
		{name: "cycle", deps: []string{"dep-1"}, wantErr: "dependency cycle"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := setupState(t)

			_, err := MarkBlockedWithOptions(tmpDir, "task-1", "blocked", []string{"q1"}, "coder-1", MarkBlockedOptions{DependsOn: tt.deps})
			if err == nil {
				t.Fatal("Expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestMarkBlockedWithOptions_RepairRequest(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := MarkBlockedWithOptions(
		tmpDir,
		"task-1",
		"Required state repair is orchestrator-only",
		[]string{"Can the orchestrator restore the missing parent task?"},
		"coder-1",
		MarkBlockedOptions{
			RepairRequest: &models.RepairRequest{
				Operation:  " add-task ",
				Target:     " architecture-2 ",
				Command:    " liza add-task --id architecture-2 --agent-id orchestrator-1 --json ",
				Evidence:   []string{` command=liza add-task --id architecture-2 --agent-id coder-1 --json exit_code=1 stderr=command requires role type [orchestrator] `, ""},
				Validation: []string{" python -m pytest -q tests/backend/test_workflow_contract.py -q "},
			},
		},
	)
	if err != nil {
		t.Fatalf("MarkBlockedWithOptions() error: %v", err)
	}
	if result.RepairRequest == nil {
		t.Fatal("RepairRequest result is nil")
	}
	if result.RepairRequest.Operation != "add-task" {
		t.Errorf("Operation = %q, want add-task", result.RepairRequest.Operation)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	task := readState.FindTask("task-1")
	if task == nil {
		t.Fatal("Task not found")
	}
	if task.RepairRequest == nil {
		t.Fatal("task.RepairRequest is nil")
	}
	if task.RepairRequest.Target != "architecture-2" {
		t.Errorf("Target = %q, want architecture-2", task.RepairRequest.Target)
	}
	if task.RepairRequest.Command != "liza add-task --id architecture-2 --agent-id orchestrator-1 --json" {
		t.Errorf("Command = %q", task.RepairRequest.Command)
	}
	if len(task.RepairRequest.Evidence) != 1 {
		t.Errorf("Evidence len = %d, want 1", len(task.RepairRequest.Evidence))
	}
	if len(task.RepairRequest.Validation) != 1 {
		t.Errorf("Validation len = %d, want 1", len(task.RepairRequest.Validation))
	}
}

func TestMarkBlockedWithOptions_DeclarativeDependencyRepair(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)
	result, err := MarkBlockedWithOptions(
		tmpDir,
		"task-1",
		"Dependency graph repair is orchestrator-only",
		[]string{"Can the orchestrator apply the stored dependency repair?"},
		"coder-1",
		MarkBlockedOptions{RepairRequest: &models.RepairRequest{
			Operation: " apply-dependency-repair ",
			Target:    " task-1 ",
			DependencyUpdates: []models.DependencyUpdate{
				{
					TaskID:            " consumer-1 ",
					ExpectedDependsOn: []string{},
					DesiredDependsOn:  []string{" producer-1 ", "producer-2"},
				},
				{
					TaskID:            "consumer-2",
					ExpectedDependsOn: []string{" producer-1 "},
					DesiredDependsOn:  []string{},
				},
			},
			Evidence:   []string{" error=dependency repair requires orchestrator authority "},
			Validation: []string{" liza validate --json "},
		}},
	)
	if err != nil {
		t.Fatalf("MarkBlockedWithOptions() error: %v", err)
	}
	if result.RepairRequest == nil {
		t.Fatal("RepairRequest result is nil")
	}
	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	request := readState.FindTask("task-1").RepairRequest
	if request == nil {
		t.Fatal("persisted RepairRequest is nil")
	}
	if request.Operation != "apply-dependency-repair" || request.Target != "task-1" {
		t.Fatalf("persisted request identity = %q/%q", request.Operation, request.Target)
	}
	if len(request.DependencyUpdates) != 2 {
		t.Fatalf("DependencyUpdates len = %d, want 2", len(request.DependencyUpdates))
	}
	first := request.DependencyUpdates[0]
	if first.TaskID != "consumer-1" {
		t.Fatalf("first TaskID = %q, want consumer-1", first.TaskID)
	}
	if first.ExpectedDependsOn == nil || len(first.ExpectedDependsOn) != 0 {
		t.Fatalf("first ExpectedDependsOn = %#v, want explicit empty list", first.ExpectedDependsOn)
	}
	if got := strings.Join(first.DesiredDependsOn, ","); got != "producer-1,producer-2" {
		t.Fatalf("first DesiredDependsOn = %q, want producer-1,producer-2", got)
	}
	second := request.DependencyUpdates[1]
	if second.DesiredDependsOn == nil || len(second.DesiredDependsOn) != 0 {
		t.Fatalf("second DesiredDependsOn = %#v, want explicit empty list", second.DesiredDependsOn)
	}

	valid := models.RepairRequest{
		Operation: "apply-dependency-repair",
		Target:    "task-1",
		DependencyUpdates: []models.DependencyUpdate{{
			TaskID:            "consumer-1",
			ExpectedDependsOn: []string{},
			DesiredDependsOn:  []string{},
		}},
		Evidence:   []string{"error=dependency repair requires orchestrator authority"},
		Validation: []string{"liza validate --json"},
	}
	tests := []struct {
		name    string
		request models.RepairRequest
		wantErr string
	}{
		{
			name: "requires explicit expected list",
			request: func() models.RepairRequest {
				request := valid
				request.DependencyUpdates = []models.DependencyUpdate{{
					TaskID:           "consumer-1",
					DesiredDependsOn: []string{},
				}}
				return request
			}(),
			wantErr: "expected_depends_on must be an explicit list",
		},
		{
			name: "rejects duplicate update task",
			request: func() models.RepairRequest {
				request := valid
				request.DependencyUpdates = append(request.DependencyUpdates, models.DependencyUpdate{
					TaskID:            " consumer-1 ",
					ExpectedDependsOn: []string{},
					DesiredDependsOn:  []string{},
				})
				return request
			}(),
			wantErr: "dependency update task_id values must be unique",
		},
	}

	for _, tt := range tests {
		_, err := MarkBlockedWithOptions(
			"/nonexistent",
			"task-1",
			"blocked",
			[]string{"q1"},
			"coder-1",
			MarkBlockedOptions{RepairRequest: &tt.request},
		)
		if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
			t.Fatalf("%s: Error = %v, want substring %q", tt.name, err, tt.wantErr)
		}
	}
}

func TestMarkBlockedWithOptions_RepairRequestRequiresCompleteRequest(t *testing.T) {
	t.Parallel()

	valid := models.RepairRequest{
		Operation:  "add-task",
		Target:     "architecture-2",
		Command:    "liza add-task --id architecture-2 --agent-id orchestrator-1 --json",
		Evidence:   []string{`command=liza add-task --id architecture-2 --agent-id coder-1 --json exit_code=1 stderr=command requires role type [orchestrator]`},
		Validation: []string{"go test ./cmd/liza"},
	}

	tests := []struct {
		name          string
		repairRequest models.RepairRequest
		wantErr       string
	}{
		{
			name:          "missing operation",
			repairRequest: models.RepairRequest{Target: valid.Target, Command: valid.Command, Evidence: valid.Evidence, Validation: valid.Validation},
			wantErr:       "repair request operation is required",
		},
		{
			name:          "missing target",
			repairRequest: models.RepairRequest{Operation: valid.Operation, Command: valid.Command, Evidence: valid.Evidence, Validation: valid.Validation},
			wantErr:       "repair request target is required",
		},
		{
			name:          "missing command",
			repairRequest: models.RepairRequest{Operation: valid.Operation, Target: valid.Target, Evidence: valid.Evidence, Validation: valid.Validation},
			wantErr:       "repair request command is required",
		},
		{
			name:          "missing evidence",
			repairRequest: models.RepairRequest{Operation: valid.Operation, Target: valid.Target, Command: valid.Command, Validation: valid.Validation},
			wantErr:       "repair request evidence is required",
		},
		{
			name:          "missing validation",
			repairRequest: models.RepairRequest{Operation: valid.Operation, Target: valid.Target, Command: valid.Command, Evidence: valid.Evidence},
			wantErr:       "repair request validation is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := MarkBlockedWithOptions(
				"/nonexistent",
				"task-1",
				"blocked",
				[]string{"q1"},
				"coder-1",
				MarkBlockedOptions{RepairRequest: &tt.repairRequest},
			)
			if err == nil {
				t.Fatal("Expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestMarkBlockedWithOptions_RepairRequestRequiresStructuredFailureEvidence(t *testing.T) {
	t.Parallel()

	_, err := MarkBlockedWithOptions(
		"/nonexistent",
		"task-1",
		"Git metadata write access appears broken",
		[]string{"Can the orchestrator restore git metadata write access?"},
		"coder-1",
		MarkBlockedOptions{
			RepairRequest: &models.RepairRequest{
				Operation:  "index_lock_stuck",
				Target:     ".git/worktrees/task-1",
				Command:    "git -C .worktrees/task-1 add file.txt",
				Evidence:   []string{"git add failed with read-only filesystem creating index.lock"},
				Validation: []string{"git -C .worktrees/task-1 add file.txt"},
			},
		},
	)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !strings.Contains(err.Error(), "structured failure evidence") {
		t.Fatalf("Error = %q, want structured failure evidence", err.Error())
	}
}

func TestMarkBlockedWithOptions_RepairRequestAcceptsStructuredFailureEvidence(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := MarkBlockedWithOptions(
		tmpDir,
		"task-1",
		"Git metadata write access appears broken",
		[]string{"Can the orchestrator restore git metadata write access?"},
		"coder-1",
		MarkBlockedOptions{
			RepairRequest: &models.RepairRequest{
				Operation:  "restore_git_write_access",
				Target:     ".git/worktrees/task-1",
				Command:    "git -C .worktrees/task-1 add file.txt",
				Evidence:   []string{"command=git -C .worktrees/task-1 add file.txt exit_code=128 stderr=fatal: Unable to create index.lock: Read-only file system"},
				Validation: []string{"git -C .worktrees/task-1 add file.txt"},
			},
		},
	)
	if err != nil {
		t.Fatalf("MarkBlockedWithOptions() error: %v", err)
	}
}

func TestMarkBlockedWithOptions_RepairRequestAcceptsStandaloneErrorEvidence(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := MarkBlockedWithOptions(
		tmpDir,
		"task-1",
		"Provider repair is orchestrator-only",
		[]string{"Can the orchestrator repair the provider state?"},
		"coder-1",
		MarkBlockedOptions{
			RepairRequest: &models.RepairRequest{
				Operation:  "repair-provider-state",
				Target:     "provider/codex",
				Command:    "provider session repair",
				Evidence:   []string{"error=provider session thread not found"},
				Validation: []string{"liza status --json"},
			},
		},
	)
	if err != nil {
		t.Fatalf("MarkBlockedWithOptions() error: %v", err)
	}
}

func TestMarkBlocked_TaskNotFound(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := MarkBlocked(tmpDir, "nonexistent", "reason", []string{"q1"}, "coder-1")
	if err == nil {
		t.Fatal("Expected error for nonexistent task")
	}
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFoundError, got %T: %v", err, err)
	}
}

func TestMarkBlocked_WrongStatus(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := MarkBlocked(tmpDir, "task-1", "reason", []string{"q1"}, "coder-1")
	if err == nil {
		t.Fatal("Expected error for non-IMPLEMENTING task")
	}
	if !strings.Contains(err.Error(), "executing status") {
		t.Errorf("Error = %q, want to contain 'executing status'", err.Error())
	}
}

func TestMarkBlocked_PipelineExecutingStatus(t *testing.T) {
	t.Parallel()

	tmpDir, stateFile := setupPipelineTest(t)

	now := time.Now().UTC()
	agent := "coder-1"
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		{
			ID:          "task-1",
			Type:        models.TaskTypeCoding,
			Description: "Pipeline task in executing state",
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

	result, err := MarkBlocked(tmpDir, "task-1", "Spec ambiguity", []string{"What should the format be?"}, agent)
	if err != nil {
		t.Fatalf("MarkBlocked() failed for pipeline executing status: %v", err)
	}

	if result.TaskID != "task-1" {
		t.Errorf("TaskID = %q, want %q", result.TaskID, "task-1")
	}

	// Verify state transition
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	task := readState.FindTask("task-1")
	if task == nil {
		t.Fatal("Task not found")
	}
	if task.Status != models.TaskStatusBlocked {
		t.Errorf("Status = %v, want BLOCKED", task.Status)
	}
	if task.AssignedTo != nil {
		t.Error("AssignedTo should be nil after blocking")
	}
}

func TestMarkBlocked_PipelineNonExecutingStatus(t *testing.T) {
	t.Parallel()

	tmpDir, stateFile := setupPipelineTest(t)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		{
			ID:          "task-1",
			Type:        models.TaskTypeCoding,
			Description: "Pipeline task at initial (non-executing) status",
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

	_, err := MarkBlocked(tmpDir, "task-1", "reason", []string{"q1"}, "coder-1")
	if err == nil {
		t.Fatal("Expected error for non-executing pipeline status")
	}
	if !strings.Contains(err.Error(), "executing status") {
		t.Errorf("Error = %q, want to contain 'executing status'", err.Error())
	}
}

func TestMarkBlocked_WrongAgent(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := MarkBlocked(tmpDir, "task-1", "reason", []string{"q1"}, "coder-2")
	if err == nil {
		t.Fatal("Expected error for wrong agent")
	}
	if !strings.Contains(err.Error(), "assigned agent") {
		t.Errorf("Error = %q, want to contain 'assigned agent'", err.Error())
	}
}

// TestMarkBlockedNormalizerSignatures pins the normalizers output 2's
// assess-blocked path calls directly. Routing their predicates through the
// payload schema must change neither their shape nor their verdicts.
func TestMarkBlockedNormalizerSignatures(t *testing.T) {
	t.Parallel()

	// These assignments fail to compile if any signature changes.
	var (
		repairRequest      func(*models.RepairRequest, string) (*models.RepairRequest, error) = normalizeRepairRequest
		dependsOn          func([]string) ([]string, error)                                   = normalizeDependsOn
		dependencyUpdates  func([]models.DependencyUpdate) ([]models.DependencyUpdate, error) = normalizeDependencyUpdates
		explicitDependency func([]string, string, int) ([]string, error)                      = normalizeExplicitDependencyList
		compact            func([]string) []string                                            = compactNonEmpty
	)

	rejections := []struct {
		name    string
		call    func() error
		wantErr string
	}{
		{
			name:    "depends-on rejects an empty entry",
			call:    func() error { _, err := dependsOn([]string{" "}); return err },
			wantErr: "depends-on entries cannot be empty",
		},
		{
			name:    "depends-on rejects a duplicate entry",
			call:    func() error { _, err := dependsOn([]string{"dep-1", "dep-1"}); return err },
			wantErr: "depends-on entries must be unique",
		},
		{
			name: "repair request rejects a missing command",
			call: func() error {
				_, err := repairRequest(&models.RepairRequest{
					Operation:  "add-task",
					Target:     "architecture-2",
					Evidence:   []string{"error=command requires role type [orchestrator]"},
					Validation: []string{"validate --json"},
				}, "task-1")
				return err
			},
			wantErr: "repair request command is required",
		},
		{
			name: "repair request rejects unstructured failure evidence",
			call: func() error {
				_, err := repairRequest(&models.RepairRequest{
					Operation:  "add-task",
					Target:     "architecture-2",
					Command:    "add-task --id architecture-2 --json",
					Evidence:   []string{"the add-task call failed"},
					Validation: []string{"validate --json"},
				}, "task-1")
				return err
			},
			wantErr: "structured failure evidence",
		},
		{
			name: "repair request rejects a declarative repair targeting another task",
			call: func() error {
				_, err := repairRequest(&models.RepairRequest{
					Operation:         models.RepairOperationApplyDependencyRepair,
					Target:            "task-2",
					DependencyUpdates: []models.DependencyUpdate{{TaskID: "consumer-1", ExpectedDependsOn: []string{}, DesiredDependsOn: []string{}}},
					Evidence:          []string{"error=dependency repair requires orchestrator authority"},
					Validation:        []string{"validate --json"},
				}, "task-1")
				return err
			},
			wantErr: "declarative dependency repair target must match blocked task",
		},
		{
			name:    "dependency updates reject an empty batch",
			call:    func() error { _, err := dependencyUpdates(nil); return err },
			wantErr: "declarative dependency repair dependency_updates is required",
		},
		{
			name: "dependency updates reject a duplicate task",
			call: func() error {
				_, err := dependencyUpdates([]models.DependencyUpdate{
					{TaskID: "consumer-1", ExpectedDependsOn: []string{}, DesiredDependsOn: []string{}},
					{TaskID: " consumer-1 ", ExpectedDependsOn: []string{}, DesiredDependsOn: []string{}},
				})
				return err
			},
			wantErr: "dependency update task_id values must be unique",
		},
		{
			name:    "explicit dependency list rejects a missing list",
			call:    func() error { _, err := explicitDependency(nil, "expected_depends_on", 0); return err },
			wantErr: "dependency_updates[0].expected_depends_on must be an explicit list",
		},
		{
			name:    "explicit dependency list rejects an empty entry",
			call:    func() error { _, err := explicitDependency([]string{" "}, "desired_depends_on", 1); return err },
			wantErr: "dependency_updates[1].desired_depends_on entries cannot be empty",
		},
		{
			name: "explicit dependency list rejects a duplicate entry",
			call: func() error {
				_, err := explicitDependency([]string{"producer-1", " producer-1 "}, "desired_depends_on", 0)
				return err
			},
			wantErr: "dependency_updates[0].desired_depends_on entries must be unique",
		},
	}

	for _, tt := range rejections {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.call()
			var precondition *PreconditionError
			if !stderrors.As(err, &precondition) {
				t.Fatalf("error = %v, want a *PreconditionError", err)
			}
			if !strings.Contains(precondition.Reason, tt.wantErr) {
				t.Fatalf("Reason = %q, want to contain %q", precondition.Reason, tt.wantErr)
			}
		})
	}

	t.Run("a nil repair request stays accepted", func(t *testing.T) {
		t.Parallel()

		normalized, err := repairRequest(nil, "task-1")
		if err != nil || normalized != nil {
			t.Fatalf("normalizeRepairRequest(nil) = %#v, %v; want nil, nil", normalized, err)
		}
	})

	t.Run("compactNonEmpty trims and drops blank entries", func(t *testing.T) {
		t.Parallel()

		if got := compact([]string{" a ", " ", "b"}); !reflect.DeepEqual(got, []string{"a", "b"}) {
			t.Fatalf("compactNonEmpty() = %#v, want [a b]", got)
		}
		if got := compact([]string{" "}); got != nil {
			t.Fatalf("compactNonEmpty() = %#v, want nil", got)
		}
	})
}
