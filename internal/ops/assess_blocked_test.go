package ops

import (
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

func TestAssessBlocked_Validation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		taskID      string
		agentID     string
		errContains string
	}{
		{
			name:        "empty task ID",
			agentID:     "orchestrator-1",
			errContains: "task ID is required",
		},
		{
			name:        "empty agent ID",
			taskID:      "task-1",
			errContains: "agent ID is required",
		},
		{
			name:        "non-orchestrator agent ID",
			taskID:      "task-1",
			agentID:     "coder-1",
			errContains: "only orchestrator agents can assess blocked tasks",
		},
		{
			name:        "reviewer agent ID",
			taskID:      "task-1",
			agentID:     "code-reviewer-1",
			errContains: "only orchestrator agents can assess blocked tasks",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := AssessBlocked("/nonexistent", tt.taskID, "", tt.agentID)
			if err == nil {
				t.Fatal("Expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("Error = %q, want to contain %q", err.Error(), tt.errContains)
			}
		})
	}
}

func TestAssessBlocked_TaskNotFound(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := AssessBlocked(tmpDir, "nonexistent", "", "orchestrator-1")
	if err == nil {
		t.Fatal("Expected error for nonexistent task")
	}
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFoundError, got %T: %v", err, err)
	}
}

func TestAssessBlocked_WrongStatus(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := AssessBlocked(tmpDir, "task-1", "", "orchestrator-1")
	if err == nil {
		t.Fatal("Expected error for non-BLOCKED task")
	}
	if !strings.Contains(err.Error(), "BLOCKED status") {
		t.Errorf("Error = %q, want to contain 'BLOCKED status'", err.Error())
	}
}

func TestAssessBlocked_Success(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := AssessBlocked(tmpDir, "task-1", "Cannot resolve without external API", "orchestrator-1")
	if err != nil {
		t.Fatalf("AssessBlocked() error: %v", err)
	}

	if result.TaskID != "task-1" {
		t.Errorf("TaskID = %q, want %q", result.TaskID, "task-1")
	}

	// Verify history entry
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	task := readState.FindTask("task-1")
	if task == nil {
		t.Fatal("Task not found")
	}
	// Status should remain BLOCKED
	if task.Status != models.TaskStatusBlocked {
		t.Errorf("Status = %v, want BLOCKED", task.Status)
	}

	lastHistory := task.History[len(task.History)-1]
	if lastHistory.Event != models.TaskEventOrchestratorAssessment {
		t.Errorf("History event = %q, want %q", lastHistory.Event, models.TaskEventOrchestratorAssessment)
	}
	if lastHistory.Agent == nil || *lastHistory.Agent != "orchestrator-1" {
		t.Errorf("Expected agent orchestrator-1 in history, got %v", lastHistory.Agent)
	}
	if lastHistory.Note == nil || *lastHistory.Note != "Cannot resolve without external API" {
		t.Errorf("Expected note in history, got %v", lastHistory.Note)
	}

	data, err := os.ReadFile(paths.New(tmpDir).AlertsLogPath())
	if err != nil {
		t.Fatalf("Read alerts.log: %v", err)
	}
	if !strings.Contains(string(data), "UNRESOLVED BLOCKED: task-1 — Cannot resolve without external API") {
		t.Fatalf("alerts.log missing unresolved alert:\n%s", string(data))
	}
}

func TestAssessBlocked_SuccessWithoutNote(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := AssessBlocked(tmpDir, "task-1", "", "orchestrator-1")
	if err != nil {
		t.Fatalf("AssessBlocked() error: %v", err)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	task := readState.FindTask("task-1")
	lastHistory := task.History[len(task.History)-1]
	if lastHistory.Note != nil {
		t.Errorf("Expected nil note, got %v", lastHistory.Note)
	}
}

func TestAssessBlocked_Idempotent(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	// First assessment
	_, err := AssessBlocked(tmpDir, "task-1", "first", "orchestrator-1")
	if err != nil {
		t.Fatalf("First AssessBlocked() error: %v", err)
	}

	// Second assessment
	_, err = AssessBlocked(tmpDir, "task-1", "second", "orchestrator-1")
	if err != nil {
		t.Fatalf("Second AssessBlocked() error: %v", err)
	}

	// Verify two entries
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	task := readState.FindTask("task-1")
	assessmentCount := 0
	for _, entry := range task.History {
		if entry.Event == models.TaskEventOrchestratorAssessment {
			assessmentCount++
		}
	}
	if assessmentCount != 2 {
		t.Errorf("Expected 2 assessment entries, got %d", assessmentCount)
	}
}

func TestAssessBlocked_ReconcilesCanonicalMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		repairRequest     *models.RepairRequest
		wantRepairRequest *models.RepairRequest
	}{
		{
			name: "clears prior repair request",
		},
		{
			name: "normalizes declarative replacement",
			repairRequest: &models.RepairRequest{
				Operation: "  " + models.RepairOperationApplyDependencyRepair + "  ",
				Target:    " task-1 ",
				DependencyUpdates: []models.DependencyUpdate{
					{
						TaskID:            " task-1 ",
						ExpectedDependsOn: []string{" old-dependency "},
						DesiredDependsOn:  []string{" replacement-dependency "},
					},
				},
				Evidence:   []string{"", " error=provider unavailable "},
				Validation: []string{" verify repaired dependency graph "},
			},
			wantRepairRequest: &models.RepairRequest{
				Operation: models.RepairOperationApplyDependencyRepair,
				Target:    "task-1",
				DependencyUpdates: []models.DependencyUpdate{
					{
						TaskID:            "task-1",
						ExpectedDependsOn: []string{"old-dependency"},
						DesiredDependsOn:  []string{"replacement-dependency"},
					},
				},
				Evidence:   []string{"error=provider unavailable"},
				Validation: []string{"verify repaired dependency graph"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
			testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")
			now := time.Now().UTC()
			state := testhelpers.CreateValidState()
			task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
			task.AssignedTo = nil
			oldReason := "obsolete blocker"
			task.BlockedReason = &oldReason
			task.BlockedQuestions = []string{"obsolete question"}
			task.RepairRequest = testAssessBlockedRepairRequest()
			state.Tasks = []models.Task{task}
			setTaskSpecRefs(state)
			testhelpers.WriteInitialState(t, stateFile, state)

			result, err := AssessBlockedWithOptions(
				tmpDir,
				"task-1",
				"remaining blocker reassessed",
				"orchestrator-1",
				AssessBlockedOptions{
					Reason:        "current blocker",
					Questions:     []string{"first current question", "second current question"},
					RepairRequest: tt.repairRequest,
				},
			)
			if err != nil {
				t.Fatalf("AssessBlockedWithOptions() error: %v", err)
			}

			if result.TaskID != "task-1" || result.Reason != "current blocker" {
				t.Fatalf("result identity/reason = %#v, want task-1/current blocker", result)
			}
			if !reflect.DeepEqual(result.Questions, []string{"first current question", "second current question"}) {
				t.Errorf("result questions = %#v", result.Questions)
			}
			if !reflect.DeepEqual(result.RepairRequest, tt.wantRepairRequest) {
				t.Errorf("result repair request = %#v, want %#v", result.RepairRequest, tt.wantRepairRequest)
			}

			readState, err := db.New(stateFile).Read()
			if err != nil {
				t.Fatalf("Read() error: %v", err)
			}
			got := readState.FindTask("task-1")
			if got.Status != models.TaskStatusBlocked {
				t.Errorf("status = %s, want BLOCKED", got.Status)
			}
			if got.BlockedReason == nil || *got.BlockedReason != "current blocker" {
				t.Errorf("blocked reason = %v, want current blocker", got.BlockedReason)
			}
			if !reflect.DeepEqual(got.BlockedQuestions, []string{"first current question", "second current question"}) {
				t.Errorf("blocked questions = %#v", got.BlockedQuestions)
			}
			if !reflect.DeepEqual(got.RepairRequest, tt.wantRepairRequest) {
				t.Errorf("canonical repair request = %#v, want %#v", got.RepairRequest, tt.wantRepairRequest)
			}

			last := got.History[len(got.History)-1]
			if last.Event != models.TaskEventOrchestratorAssessment || last.Reason == nil || *last.Reason != "current blocker" {
				t.Errorf("assessment history = %#v", last)
			}
			if !reflect.DeepEqual(last.Extra["blocked_questions"], []any{"first current question", "second current question"}) {
				t.Errorf("history blocked_questions = %#v", last.Extra["blocked_questions"])
			}
			_, hasRepairRequest := last.Extra["repair_request"]
			if !hasRepairRequest {
				t.Error("assessment history must record resulting repair_request state")
			}
		})
	}
}

func TestAssessBlocked_RecordsDependencyDescendantWakeSnapshot(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")
	baseTime := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	state := testhelpers.CreateValidState()
	blocked := testhelpers.BuildTaskByStatus("blocked-evaluator", models.TaskStatusBlocked, baseTime)
	blocked.AssignedTo = nil
	blocked.DependsOn = []string{"provider-plan"}
	oldReason := "existing reason"
	blocked.BlockedReason = &oldReason
	blocked.BlockedQuestions = []string{"existing question"}
	blocked.RepairRequest = testAssessBlockedRepairRequest()
	provider := testhelpers.BuildTaskByStatus("provider-plan", models.TaskStatusMerged, baseTime)
	zChild := testhelpers.BuildTaskByStatus("z-child", models.TaskStatusReady, baseTime)
	zChild.ParentTasks = []string{"provider-plan"}
	aChild := testhelpers.BuildTaskByStatus("a-child", models.TaskStatusReady, baseTime)
	aChild.ParentTasks = []string{"provider-plan"}
	state.Tasks = []models.Task{blocked, provider, zChild, aChild}
	setTaskSpecRefs(state)
	testhelpers.WriteInitialState(t, stateFile, state)

	wantRaw := BuildDependencyDescendantWakeSnapshot(state, &state.Tasks[0])
	want, ok := NormalizeDependencyDescendantWakeSnapshot(wantRaw)
	if !ok {
		t.Fatalf("NormalizeDependencyDescendantWakeSnapshot(producer value) rejected %#v", wantRaw)
	}
	if len(want) != 2 || want[0].TaskID != "a-child" || want[1].TaskID != "z-child" {
		t.Fatalf("producer snapshot order = %#v, want a-child then z-child", want)
	}

	if _, err := AssessBlocked(tmpDir, "blocked-evaluator", "note only", "orchestrator-1"); err != nil {
		t.Fatalf("AssessBlocked() error: %v", err)
	}
	afterNote := readAssessBlockedTask(t, stateFile, "blocked-evaluator")
	if !reflect.DeepEqual(afterNote.DependsOn, []string{"provider-plan"}) {
		t.Fatalf("note-only depends_on = %#v, want provider-plan", afterNote.DependsOn)
	}
	if afterNote.BlockedReason == nil || *afterNote.BlockedReason != oldReason ||
		!reflect.DeepEqual(afterNote.BlockedQuestions, []string{"existing question"}) ||
		!reflect.DeepEqual(afterNote.RepairRequest, testAssessBlockedRepairRequest()) {
		t.Fatalf("note-only assessment changed canonical blocker fields: %#v", afterNote)
	}
	noteEntry := afterNote.History[len(afterNote.History)-1]
	noteSnapshot, ok := NormalizeDependencyDescendantWakeSnapshot(noteEntry.Extra[DependencyDescendantWakeSnapshotExtraKey])
	if !ok || !reflect.DeepEqual(noteSnapshot, want) {
		t.Fatalf("YAML-decoded note-only snapshot = %#v, ok=%v, want %#v", noteSnapshot, ok, want)
	}

	if _, err := AssessBlockedWithOptions(
		tmpDir,
		"blocked-evaluator",
		"reconciled",
		"orchestrator-1",
		AssessBlockedOptions{Reason: "current reason", Questions: []string{"current question"}},
	); err != nil {
		t.Fatalf("AssessBlockedWithOptions() error: %v", err)
	}
	afterReconcile := readAssessBlockedTask(t, stateFile, "blocked-evaluator")
	if !reflect.DeepEqual(afterReconcile.DependsOn, []string{"provider-plan"}) {
		t.Fatalf("reconciliation depends_on = %#v, want provider-plan", afterReconcile.DependsOn)
	}
	if afterReconcile.BlockedReason == nil || *afterReconcile.BlockedReason != "current reason" ||
		!reflect.DeepEqual(afterReconcile.BlockedQuestions, []string{"current question"}) ||
		afterReconcile.RepairRequest != nil {
		t.Fatalf("reconciled canonical blocker fields = %#v", afterReconcile)
	}
	reconcileEntry := afterReconcile.History[len(afterReconcile.History)-1]
	reconcileSnapshot, ok := NormalizeDependencyDescendantWakeSnapshot(reconcileEntry.Extra[DependencyDescendantWakeSnapshotExtraKey])
	if !ok || !reflect.DeepEqual(reconcileSnapshot, want) {
		t.Fatalf("YAML-decoded reconciliation snapshot = %#v, ok=%v, want %#v", reconcileSnapshot, ok, want)
	}
	if !reflect.DeepEqual(noteSnapshot, reconcileSnapshot) {
		t.Fatalf("note-only snapshot = %#v, reconciliation snapshot = %#v", noteSnapshot, reconcileSnapshot)
	}
}

func TestAssessBlocked_PrunesSupersededWakeSnapshots(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")
	baseTime := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	state := testhelpers.CreateValidState()
	blocked := testhelpers.BuildTaskByStatus("blocked-evaluator", models.TaskStatusBlocked, baseTime)
	blocked.AssignedTo = nil
	blocked.DependsOn = []string{"provider-plan"}
	provider := testhelpers.BuildTaskByStatus("provider-plan", models.TaskStatusMerged, baseTime)
	child := testhelpers.BuildTaskByStatus("child", models.TaskStatusReady, baseTime)
	child.ParentTasks = []string{"provider-plan"}
	state.Tasks = []models.Task{blocked, provider, child}
	setTaskSpecRefs(state)
	testhelpers.WriteInitialState(t, stateFile, state)

	for _, note := range []string{"first", "second", "third"} {
		if _, err := AssessBlocked(tmpDir, "blocked-evaluator", note, "orchestrator-1"); err != nil {
			t.Fatalf("AssessBlocked(%q) error: %v", note, err)
		}
	}

	task := readAssessBlockedTask(t, stateFile, "blocked-evaluator")
	var assessments []int
	for i := range task.History {
		if task.History[i].Event == models.TaskEventOrchestratorAssessment {
			assessments = append(assessments, i)
		}
	}
	if len(assessments) != 3 {
		t.Fatalf("assessment entries = %d, want 3", len(assessments))
	}
	for _, i := range assessments[:len(assessments)-1] {
		if _, ok := task.History[i].Extra[DependencyDescendantWakeSnapshotExtraKey]; ok {
			t.Errorf("superseded assessment at history[%d] still carries a wake snapshot", i)
		}
		if task.History[i].Note == nil {
			t.Errorf("pruning dropped the note of superseded assessment at history[%d]", i)
		}
	}
	last := task.History[assessments[len(assessments)-1]]
	if _, ok := last.Extra[DependencyDescendantWakeSnapshotExtraKey]; !ok {
		t.Fatalf("latest assessment lost its wake snapshot: %#v", last.Extra)
	}

	// Wake suppression depends on the latest snapshot only; pruning the
	// earlier ones must not make an already-triaged task actionable again.
	currentState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	if isTaskActionableSinceAssessment(currentState.FindTask("blocked-evaluator"), currentState) {
		t.Error("task became actionable again after superseded snapshots were pruned")
	}
}

func TestAssessBlocked_CandidateValidationRollback(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
	task.AssignedTo = nil
	task.RepairRequest = testAssessBlockedRepairRequest()
	state.Tasks = []models.Task{task}
	state.Goal.SpecRef = "specs/missing-goal-spec.md"
	setTaskSpecRefs(state)
	testhelpers.WriteInitialState(t, stateFile, state)
	before := readAssessBlockedTask(t, stateFile, "task-1")

	_, err := AssessBlockedWithOptions(
		tmpDir,
		"task-1",
		"must roll back",
		"orchestrator-1",
		AssessBlockedOptions{
			Reason:    "new reason",
			Questions: []string{"new question"},
		},
	)
	if err == nil {
		t.Fatal("AssessBlockedWithOptions() error = nil, want full-state validation failure")
	}

	after := readAssessBlockedTask(t, stateFile, "task-1")
	assertAssessBlockedStateUnchanged(t, before, after)
}

func TestAssessBlockedWithOptions_FailuresPreserveState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		taskID      string
		agentID     string
		status      models.TaskStatus
		opts        AssessBlockedOptions
		errContains string
	}{
		{
			name:        "empty task ID",
			agentID:     "orchestrator-1",
			status:      models.TaskStatusBlocked,
			opts:        AssessBlockedOptions{Reason: "new reason", Questions: []string{"new question"}},
			errContains: "task ID is required",
		},
		{
			name:        "empty agent ID",
			taskID:      "task-1",
			status:      models.TaskStatusBlocked,
			opts:        AssessBlockedOptions{Reason: "new reason", Questions: []string{"new question"}},
			errContains: "agent ID is required",
		},
		{
			name:        "unauthorized role",
			taskID:      "task-1",
			agentID:     "coder-1",
			status:      models.TaskStatusBlocked,
			opts:        AssessBlockedOptions{Reason: "new reason", Questions: []string{"new question"}},
			errContains: "only orchestrator agents",
		},
		{
			name:        "reason without questions",
			taskID:      "task-1",
			agentID:     "orchestrator-1",
			status:      models.TaskStatusBlocked,
			opts:        AssessBlockedOptions{Reason: "new reason"},
			errContains: "at least 1 question",
		},
		{
			name:        "questions without reason",
			taskID:      "task-1",
			agentID:     "orchestrator-1",
			status:      models.TaskStatusBlocked,
			opts:        AssessBlockedOptions{Questions: []string{"new question"}},
			errContains: "reason is required",
		},
		{
			name:        "too many questions",
			taskID:      "task-1",
			agentID:     "orchestrator-1",
			status:      models.TaskStatusBlocked,
			opts:        AssessBlockedOptions{Reason: "new reason", Questions: []string{"one", "two", "three", "four"}},
			errContains: "maximum 3 questions",
		},
		{
			name:    "invalid replacement repair request",
			taskID:  "task-1",
			agentID: "orchestrator-1",
			status:  models.TaskStatusBlocked,
			opts: AssessBlockedOptions{
				Reason:        "new reason",
				Questions:     []string{"new question"},
				RepairRequest: &models.RepairRequest{Operation: "retarget-dependency", Target: "task-1"},
			},
			errContains: "repair request command is required",
		},
		{
			name:        "wrong status",
			taskID:      "task-1",
			agentID:     "orchestrator-1",
			status:      models.TaskStatusReady,
			opts:        AssessBlockedOptions{Reason: "new reason", Questions: []string{"new question"}},
			errContains: "BLOCKED status",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
			task := testhelpers.BuildTaskByStatus("task-1", tt.status, time.Now().UTC())
			task.RepairRequest = testAssessBlockedRepairRequest()
			state := testhelpers.CreateValidState()
			state.Tasks = []models.Task{task}
			testhelpers.WriteInitialState(t, stateFile, state)
			before := readAssessBlockedTask(t, stateFile, "task-1")

			_, err := AssessBlockedWithOptions(tmpDir, tt.taskID, "reassessment", tt.agentID, tt.opts)
			if err == nil || !strings.Contains(err.Error(), tt.errContains) {
				t.Fatalf("AssessBlockedWithOptions() error = %v, want containing %q", err, tt.errContains)
			}

			after := readAssessBlockedTask(t, stateFile, "task-1")
			assertAssessBlockedStateUnchanged(t, before, after)
		})
	}
}

func TestAssessBlocked_NoteOnlyPreservesCanonicalMetadata(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, time.Now().UTC())
	task.RepairRequest = testAssessBlockedRepairRequest()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)
	before := readAssessBlockedTask(t, stateFile, "task-1")

	if _, err := AssessBlocked(tmpDir, "task-1", "history only", "orchestrator-1"); err != nil {
		t.Fatalf("AssessBlocked() error: %v", err)
	}

	after := readAssessBlockedTask(t, stateFile, "task-1")
	if !reflect.DeepEqual(before.BlockedReason, after.BlockedReason) ||
		!reflect.DeepEqual(before.BlockedQuestions, after.BlockedQuestions) ||
		!reflect.DeepEqual(before.RepairRequest, after.RepairRequest) {
		t.Fatalf("note-only assessment changed canonical metadata: before=%#v after=%#v", before, after)
	}
	if len(after.History) != len(before.History)+1 {
		t.Fatalf("history length = %d, want %d", len(after.History), len(before.History)+1)
	}
}

func testAssessBlockedRepairRequest() *models.RepairRequest {
	return &models.RepairRequest{
		Operation:  "retarget-dependency",
		Target:     "task-1",
		Command:    "liza retarget-dependency task-1 old-dependency replacement-dependency",
		Evidence:   []string{"command=retarget exit_code=1 stderr=repair required"},
		Validation: []string{"liza get task-1 --json"},
	}
}

func readAssessBlockedTask(t *testing.T, stateFile, taskID string) *models.Task {
	t.Helper()
	state, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	task := state.FindTask(taskID)
	if task == nil {
		t.Fatalf("task %s not found", taskID)
	}
	return task
}

func assertAssessBlockedStateUnchanged(t *testing.T, before, after *models.Task) {
	t.Helper()
	if !reflect.DeepEqual(before.BlockedReason, after.BlockedReason) ||
		!reflect.DeepEqual(before.BlockedQuestions, after.BlockedQuestions) ||
		!reflect.DeepEqual(before.RepairRequest, after.RepairRequest) ||
		!reflect.DeepEqual(before.History, after.History) {
		t.Fatalf("blocked metadata/history changed after rejected assessment:\nbefore=%#v\nafter=%#v", before, after)
	}
}
