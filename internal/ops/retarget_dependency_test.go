package ops

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/liza-mas/liza/internal/db"
	activitylog "github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestRetargetDependency_ReplacesEdgeAndClearsMatchingRepairRequest(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Goal.SpecRef = "README.md"
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
	task.DependsOn = []string{"old-dep", "replacement-a"}
	task.RepairRequest = &models.RepairRequest{
		Operation:  "retarget-dependency",
		Target:     "task-1",
		Command:    "liza retarget-dependency task-1 old-dep replacement-a,replacement-b --reason repair --agent-id orchestrator-1 --json",
		Evidence:   []string{"command=liza claim-task task-1 coder-1 exit_code=1 stderr=task has invalid dependency old-dep"},
		Validation: []string{"liza validate --json"},
	}
	state.Tasks = []models.Task{
		task,
		testhelpers.BuildTaskByStatus("replacement-a", models.TaskStatusMerged, now),
		testhelpers.BuildTaskByStatus("replacement-b", models.TaskStatusMerged, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := RetargetDependency(tmpDir, "task-1", "old-dep", []string{"replacement-a", "replacement-b"}, "Correct stale dependency", "orchestrator-1")
	if err != nil {
		t.Fatalf("RetargetDependency() error: %v", err)
	}
	if result.TaskID != "task-1" {
		t.Fatalf("TaskID = %q, want task-1", result.TaskID)
	}
	if !slices.Equal(result.NewDependencies, []string{"replacement-a", "replacement-b"}) {
		t.Fatalf("NewDependencies = %v", result.NewDependencies)
	}
	if !slices.Equal(result.CanonicalDependencies, []string{"replacement-a", "replacement-b"}) {
		t.Fatalf("CanonicalDependencies = %v", result.CanonicalDependencies)
	}
	if !result.RepairRequestCleared {
		t.Fatal("RepairRequestCleared = false, want true")
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	updated := readState.FindTask("task-1")
	if updated == nil {
		t.Fatal("task-1 not found")
	}
	if !slices.Equal(updated.DependsOn, []string{"replacement-a", "replacement-b"}) {
		t.Fatalf("DependsOn = %v, want [replacement-a replacement-b]", updated.DependsOn)
	}
	if updated.RepairRequest != nil {
		t.Fatal("RepairRequest should be cleared")
	}
	last := updated.History[len(updated.History)-1]
	if last.Event != models.TaskEventDependenciesRewritten {
		t.Fatalf("History event = %q, want %q", last.Event, models.TaskEventDependenciesRewritten)
	}
	if last.Extra["operation"] != "retarget-dependency" {
		t.Fatalf("history operation = %v, want retarget-dependency", last.Extra["operation"])
	}
	if last.Extra["repair_request_cleared"] != true {
		t.Fatalf("repair_request_cleared = %v, want true", last.Extra["repair_request_cleared"])
	}
}

func TestRetargetDependency_ClearsRepairRequestAfterEquivalentCanonicalRepair(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Goal.SpecRef = "README.md"
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
	task.DependsOn = []string{"old-dep"}
	task.RepairRequest = &models.RepairRequest{
		Operation:  retargetDependencyOperation,
		Target:     "task-1",
		Command:    "liza retarget-dependency task-1 old-dep replacement-new --reason repair --agent-id orchestrator-1 --json",
		Evidence:   []string{"command=liza claim-task task-1 coder-1 exit_code=1 stderr=task has invalid dependency old-dep"},
		Validation: []string{"liza validate --json"},
	}
	originalRepairRequest := *task.RepairRequest
	originalBlockedReason := *task.BlockedReason
	originalBlockedQuestions := slices.Clone(task.BlockedQuestions)
	superseded := testhelpers.BuildTaskByStatus("replacement-old", models.TaskStatusSuperseded, now)
	superseded.RolePair = "coding-pair"
	superseded.SupersededBy = []string{"replacement-new"}
	superseded.RescopeReason = testhelpers.StringPtr("replaced")
	state.Tasks = []models.Task{
		task,
		testhelpers.BuildTaskByStatus("old-dep", models.TaskStatusMerged, now),
		superseded,
		testhelpers.BuildTaskByStatus("replacement-new", models.TaskStatusMerged, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := RetargetDependency(tmpDir, "task-1", "old-dep", []string{"replacement-old"}, "Follow canonical replacement", "orchestrator-1")
	if err != nil {
		t.Fatalf("RetargetDependency() error: %v", err)
	}
	if !result.RepairRequestCleared {
		t.Fatal("RepairRequestCleared = false, want true")
	}
	if !slices.Equal(result.CanonicalDependencies, []string{"replacement-new"}) {
		t.Fatalf("CanonicalDependencies = %v, want [replacement-new]", result.CanonicalDependencies)
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	updated := readState.FindTask("task-1")
	if updated == nil {
		t.Fatal("task-1 not found")
	}
	if updated.RepairRequest != nil {
		t.Fatal("RepairRequest should be cleared after the requested stale edge is removed")
	}
	if updated.BlockedReason == nil || *updated.BlockedReason != originalBlockedReason {
		t.Fatalf("BlockedReason = %v, want %q", updated.BlockedReason, originalBlockedReason)
	}
	if !slices.Equal(updated.BlockedQuestions, originalBlockedQuestions) {
		t.Fatalf("BlockedQuestions = %v, want %v", updated.BlockedQuestions, originalBlockedQuestions)
	}
	last := updated.History[len(updated.History)-1]
	if last.Event != models.TaskEventDependenciesRewritten {
		t.Fatalf("History event = %q, want %q", last.Event, models.TaskEventDependenciesRewritten)
	}
	if last.Extra["repair_request_cleared"] != true {
		t.Fatalf("repair_request_cleared = %v, want true", last.Extra["repair_request_cleared"])
	}
	for key, want := range map[string]string{
		"repair_operation": originalRepairRequest.Operation,
		"repair_target":    originalRepairRequest.Target,
		"repair_command":   originalRepairRequest.Command,
	} {
		if last.Extra[key] != want {
			t.Errorf("history %s = %#v, want %#v", key, last.Extra[key], want)
		}
	}
	if !reflect.DeepEqual(last.Extra["repair_evidence"], []any{originalRepairRequest.Evidence[0]}) {
		t.Errorf("history repair_evidence = %#v, want retired request evidence", last.Extra["repair_evidence"])
	}
	if !reflect.DeepEqual(last.Extra["repair_validation"], []any{originalRepairRequest.Validation[0]}) {
		t.Errorf("history repair_validation = %#v, want retired request validation", last.Extra["repair_validation"])
	}
}

func TestRetargetDependency_CanonicalizesSupersededReplacement(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Goal.SpecRef = "README.md"
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
	task.DependsOn = []string{"old-dep"}
	superseded := testhelpers.BuildTaskByStatus("replacement-old", models.TaskStatusSuperseded, now)
	superseded.RolePair = "coding-pair"
	superseded.SupersededBy = []string{"replacement-new"}
	superseded.RescopeReason = testhelpers.StringPtr("replaced")
	state.Tasks = []models.Task{
		task,
		superseded,
		testhelpers.BuildTaskByStatus("replacement-new", models.TaskStatusMerged, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := RetargetDependency(tmpDir, "task-1", "old-dep", []string{"replacement-old"}, "Follow replacement", "orchestrator-1")
	if err != nil {
		t.Fatalf("RetargetDependency() error: %v", err)
	}
	if !slices.Equal(result.CanonicalDependencies, []string{"replacement-new"}) {
		t.Fatalf("CanonicalDependencies = %v, want [replacement-new]", result.CanonicalDependencies)
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	updated := readState.FindTask("task-1")
	if !slices.Equal(updated.DependsOn, []string{"replacement-new"}) {
		t.Fatalf("DependsOn = %v, want [replacement-new]", updated.DependsOn)
	}
}

func TestRetargetDependency_RejectsCanonicalizationToEmptyDependencies(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Goal.SpecRef = "README.md"
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
	task.DependsOn = []string{"old-dep"}
	abandoned := testhelpers.BuildTaskByStatus("abandoned-dep", models.TaskStatusAbandoned, now)
	abandoned.RolePair = "coding-pair"
	state.Tasks = []models.Task{
		task,
		testhelpers.BuildTaskByStatus("old-dep", models.TaskStatusMerged, now),
		abandoned,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := RetargetDependency(tmpDir, "task-1", "old-dep", []string{"abandoned-dep"}, "Should fail", "orchestrator-1")
	testhelpers.RequireErrorContains(t, err, "retarget-dependency cannot remove all dependencies")

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	updated := readState.FindTask("task-1")
	if !slices.Equal(updated.DependsOn, []string{"old-dep"}) {
		t.Fatalf("DependsOn = %v, want [old-dep]", updated.DependsOn)
	}
}

func TestRetargetDependency_DoesNotClearRepairRequestForDifferentTargetOrEdge(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		target  string
		command string
	}{
		{
			name:    "different target",
			target:  "other-task",
			command: "liza retarget-dependency task-1 old-dep new-dep --reason repair --agent-id orchestrator-1 --json",
		},
		{
			name:    "different old edge",
			target:  "task-1",
			command: "liza retarget-dependency task-1 other-old new-dep --reason repair --agent-id orchestrator-1 --json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			testhelpers.SetupTestGitRepo(t, tmpDir)
			stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

			now := time.Now().UTC()
			state := testhelpers.CreateValidState()
			state.Goal.SpecRef = "README.md"
			task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
			task.DependsOn = []string{"old-dep"}
			task.RepairRequest = &models.RepairRequest{
				Operation:  retargetDependencyOperation,
				Target:     tt.target,
				Command:    tt.command,
				Evidence:   []string{"command=liza claim-task task-1 coder-1 exit_code=1 stderr=task has an unrelated stale dependency"},
				Validation: []string{"liza validate --json"},
			}
			originalRepairRequest := *task.RepairRequest
			state.Tasks = []models.Task{
				task,
				testhelpers.BuildTaskByStatus("old-dep", models.TaskStatusMerged, now),
				testhelpers.BuildTaskByStatus("new-dep", models.TaskStatusMerged, now),
			}
			testhelpers.WriteInitialState(t, stateFile, state)

			result, err := RetargetDependency(tmpDir, "task-1", "old-dep", []string{"new-dep"}, "Correct dependency", "orchestrator-1")
			if err != nil {
				t.Fatalf("RetargetDependency() error: %v", err)
			}
			if result.RepairRequestCleared {
				t.Fatal("RepairRequestCleared = true, want false")
			}

			readState, err := db.New(stateFile).Read()
			if err != nil {
				t.Fatalf("read state: %v", err)
			}
			updated := readState.FindTask("task-1")
			if updated == nil {
				t.Fatal("task-1 not found")
			}
			if !reflect.DeepEqual(updated.RepairRequest, &originalRepairRequest) {
				t.Fatalf("RepairRequest = %#v, want unrelated request %#v", updated.RepairRequest, originalRepairRequest)
			}
			last := updated.History[len(updated.History)-1]
			if last.Extra["repair_request_cleared"] != false {
				t.Fatalf("repair_request_cleared = %v, want false", last.Extra["repair_request_cleared"])
			}
		})
	}
}

func TestRetargetDependency_DoesNotClearRepairRequestWhenStaleEdgeRemains(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Goal.SpecRef = "README.md"
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
	task.DependsOn = []string{"old-dep"}
	task.RepairRequest = &models.RepairRequest{
		Operation:  retargetDependencyOperation,
		Target:     "task-1",
		Command:    "liza retarget-dependency task-1 old-dep new-dep --reason repair --agent-id orchestrator-1 --json",
		Evidence:   []string{"command=liza claim-task task-1 coder-1 exit_code=1 stderr=task has invalid dependency old-dep"},
		Validation: []string{"liza validate --json"},
	}
	originalRepairRequest := *task.RepairRequest
	state.Tasks = []models.Task{
		task,
		testhelpers.BuildTaskByStatus("old-dep", models.TaskStatusMerged, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := RetargetDependency(tmpDir, "task-1", "old-dep", []string{"old-dep"}, "No-op replacement", "orchestrator-1")
	if err != nil {
		t.Fatalf("RetargetDependency() error: %v", err)
	}
	if result.RepairRequestCleared {
		t.Fatal("RepairRequestCleared = true, want false while the stale edge remains")
	}
	if !slices.Equal(result.CanonicalDependencies, []string{"old-dep"}) {
		t.Fatalf("CanonicalDependencies = %v, want [old-dep]", result.CanonicalDependencies)
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	updated := readState.FindTask("task-1")
	if updated == nil {
		t.Fatal("task-1 not found")
	}
	if !slices.Equal(updated.DependsOn, []string{"old-dep"}) {
		t.Fatalf("DependsOn = %v, want [old-dep]", updated.DependsOn)
	}
	if !reflect.DeepEqual(updated.RepairRequest, &originalRepairRequest) {
		t.Fatalf("RepairRequest = %#v, want actionable request %#v", updated.RepairRequest, originalRepairRequest)
	}
	last := updated.History[len(updated.History)-1]
	if last.Extra["repair_request_cleared"] != false {
		t.Fatalf("repair_request_cleared = %v, want false", last.Extra["repair_request_cleared"])
	}
}

func TestRetargetDependency_RejectsTerminalDependentTask(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Goal.SpecRef = "README.md"
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now)
	task.DependsOn = []string{"old-dep"}
	state.Tasks = []models.Task{
		task,
		testhelpers.BuildTaskByStatus("old-dep", models.TaskStatusMerged, now),
		testhelpers.BuildTaskByStatus("new-dep", models.TaskStatusMerged, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := RetargetDependency(tmpDir, "task-1", "old-dep", []string{"new-dep"}, "Should fail", "orchestrator-1")
	testhelpers.RequireErrorContains(t, err, "cannot retarget dependencies on terminal task task-1")
}

func TestRetargetDependency_RejectsMissingOldEdge(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Goal.SpecRef = "README.md"
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
	task.DependsOn = []string{"actual-dep"}
	state.Tasks = []models.Task{
		task,
		testhelpers.BuildTaskByStatus("actual-dep", models.TaskStatusMerged, now),
		testhelpers.BuildTaskByStatus("new-dep", models.TaskStatusMerged, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := RetargetDependency(tmpDir, "task-1", "old-dep", []string{"new-dep"}, "Should fail", "orchestrator-1")
	testhelpers.RequireErrorContains(t, err, "task task-1 does not depend on old-dep")
}

func TestRetargetDependency_RequiresAtLeastOneNewDependency(t *testing.T) {
	t.Parallel()
	_, err := RetargetDependency("/nonexistent", "task-1", "old-dep", nil, "Should fail", "orchestrator-1")
	testhelpers.RequireErrorContains(t, err, "at least one new dependency is required")
}

func TestRetargetDependency_CycleDiagnostics(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	const sentinel = "retarget-cycle-secret-sentinel"
	t.Setenv("RETARGET_TOKEN", sentinel)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Goal.SpecRef = "README.md"
	task := testhelpers.BuildTaskByStatus("A", models.TaskStatusBlocked, now)
	task.DependsOn = []string{"old-dep"}
	task.RepairRequest = &models.RepairRequest{
		Operation:  retargetDependencyOperation,
		Target:     "A",
		Command:    "liza retarget-dependency A old-dep B --reason repair --agent-id orchestrator-1 --json",
		Evidence:   []string{"error=dependency graph needs repair"},
		Validation: []string{"liza validate --json"},
	}
	taskB := testhelpers.BuildTaskByStatus("B", models.TaskStatusReady, now)
	taskB.DependsOn = []string{"C"}
	taskC := testhelpers.BuildTaskByStatus("C", models.TaskStatusReady, now)
	taskC.DependsOn = []string{"A"}
	state.Tasks = []models.Task{
		task,
		testhelpers.BuildTaskByStatus("old-dep", models.TaskStatusMerged, now),
		taskB,
		taskC,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	beforeState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("read state before retarget: %v", err)
	}
	before := beforeState.FindTask("A")
	if before == nil {
		t.Fatal("task A not found before retarget")
	}

	reason := "token=" + sentinel + " " + strings.Repeat("é", 1400)
	_, err = RetargetDependency(tmpDir, "A", "old-dep", []string{"B"}, reason, "orchestrator-1")
	if err == nil {
		t.Fatal("RetargetDependency() error = nil, want dependency-cycle rejection")
	}
	var operational *OperationalError
	if !errors.As(err, &operational) {
		t.Fatalf("RetargetDependency() error = %T: %v, want *OperationalError", err, err)
	}
	if operational.Code != "validation" {
		t.Fatalf("OperationalError.Code = %q, want validation", operational.Code)
	}
	if operational.Phase != "candidate-state-validation" {
		t.Fatalf("OperationalError.Phase = %q, want candidate-state-validation", operational.Phase)
	}
	details := operational.SafeDetails()
	for key, want := range map[string]string{
		"operation":         retargetDependencyOperation,
		"task_id":           "A",
		"old_dependency":    "old-dep",
		"phase":             "candidate-state-validation",
		"diagnostic_action": "retarget_dependency_rejected",
	} {
		if details[key] != want {
			t.Errorf("OperationalError detail %s = %v, want %q", key, details[key], want)
		}
	}
	newDependencies, ok := details["new_dependencies"].([]string)
	if !ok || !slices.Equal(newDependencies, []string{"B"}) {
		t.Fatalf("new_dependencies = %#v, want []string{\"B\"}", details["new_dependencies"])
	}
	cyclePath, ok := details["cycle_path"].([]string)
	if !ok || !slices.Equal(cyclePath, []string{"A", "B", "C", "A"}) {
		t.Fatalf("cycle_path = %#v, want []string{\"A\", \"B\", \"C\", \"A\"}", details["cycle_path"])
	}

	readState, readErr := db.New(stateFile).Read()
	if readErr != nil {
		t.Fatalf("read state: %v", readErr)
	}
	unchanged := readState.FindTask("A")
	if unchanged == nil {
		t.Fatal("task A not found after rejected retarget")
	}
	if !slices.Equal(unchanged.DependsOn, before.DependsOn) {
		t.Fatalf("DependsOn persisted after failed validation: got %v, want %v", unchanged.DependsOn, before.DependsOn)
	}
	if !reflect.DeepEqual(unchanged.RepairRequest, before.RepairRequest) {
		t.Fatalf("RepairRequest changed after failed validation: got %#v, want %#v", unchanged.RepairRequest, before.RepairRequest)
	}
	if !reflect.DeepEqual(unchanged.History, before.History) {
		t.Fatalf("History changed after failed validation: got %#v, want %#v", unchanged.History, before.History)
	}

	entries, logErr := activitylog.New(paths.New(tmpDir).LogPath()).Read()
	if logErr != nil {
		t.Fatalf("read activity log: %v", logErr)
	}
	var rejected []activitylog.Entry
	for _, entry := range entries {
		switch entry.Action {
		case "retarget_dependency_rejected":
			rejected = append(rejected, entry)
		case retargetDependencyOperation:
			t.Fatalf("rejected retarget recorded success activity: %#v", entry)
		}
	}
	if len(rejected) != 1 {
		t.Fatalf("retarget_dependency_rejected activity count = %d, want 1; entries=%#v", len(rejected), entries)
	}
	detail := rejected[0].Detail
	if !utf8.ValidString(detail) {
		t.Fatalf("rejection Detail is not valid UTF-8: %q", detail)
	}
	if len([]byte(detail)) > 2048 {
		t.Fatalf("rejection Detail byte length = %d, want <= 2048", len([]byte(detail)))
	}
	if strings.Contains(detail, sentinel) {
		t.Fatalf("rejection Detail leaked sentinel secret: %q", detail)
	}
	if !strings.HasSuffix(detail, "... [truncated]") {
		t.Fatalf("rejection Detail was not truncated: %q", detail)
	}
	for _, want := range []string{
		"operation=retarget-dependency",
		"phase=candidate-state-validation",
		"task_id=A",
		"old_dependency=old-dep",
		"new_dependencies=B",
		"cycle_path=A -> B -> C -> A",
		"token=***",
	} {
		if !strings.Contains(detail, want) {
			t.Errorf("rejection Detail = %q, want substring %q", detail, want)
		}
	}
}
