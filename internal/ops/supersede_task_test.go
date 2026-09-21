package ops

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/statevalidate"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestSupersedeTask_Validation(t *testing.T) {
	tests := []struct {
		name           string
		taskID         string
		replacementIDs []string
		reason         string
		wantErr        string
	}{
		{
			name: "empty task ID", replacementIDs: []string{"r1"}, reason: "r",
			wantErr: "task ID is required",
		},
		{
			name: "no replacements and no reason", taskID: "t1", replacementIDs: []string{},
			wantErr: "rescope reason is required",
		},
		{
			name: "empty reason", taskID: "t1", replacementIDs: []string{"r1"},
			wantErr: "rescope reason is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := SupersedeTask("/nonexistent", tt.taskID, tt.replacementIDs, tt.reason, "orchestrator-1")
			testhelpers.RequireErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestSupersedeTask_FromBlocked(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
	reviewCommit := "stale-review"
	approvedBy := "code-reviewer-1"
	mergeCommit := "stale-merge"
	task.ReviewCommit = &reviewCommit
	task.ApprovedBy = &approvedBy
	task.Approvals = []models.Approval{{Agent: approvedBy, Provider: "codex", Timestamp: now}}
	task.MergeCommit = &mergeCommit
	task.IntegrationFailure = map[string]any{"detail": "stale"}
	task.FailedBy = []string{"coder-1"}
	task.Output = []models.OutputEntry{{Desc: "retired output", DoneWhen: "done", Scope: "scope", SpecRef: "README.md"}}
	state.Tasks = []models.Task{
		task,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := SupersedeTask(tmpDir, "task-1", []string{"task-2", "task-3"}, "Split into smaller tasks", "orchestrator-1")
	if err != nil {
		t.Fatalf("SupersedeTask() error: %v", err)
	}

	if result.TaskID != "task-1" {
		t.Errorf("TaskID = %q, want %q", result.TaskID, "task-1")
	}
	if result.OriginalStatus != models.TaskStatusBlocked {
		t.Errorf("OriginalStatus = %v, want BLOCKED", result.OriginalStatus)
	}
	if len(result.ReplacementIDs) != 2 {
		t.Errorf("ReplacementIDs len = %d, want 2", len(result.ReplacementIDs))
	}

	// Worktree cleanup is best-effort — directory doesn't exist so no warnings
	// (RemoveWorktreeDir gracefully handles missing directories)

	// Verify state
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	updatedTask := readState.FindTask("task-1")
	if updatedTask == nil {
		t.Fatal("Task not found")
	}
	if updatedTask.Status != models.TaskStatusSuperseded {
		t.Errorf("Status = %v, want SUPERSEDED", updatedTask.Status)
	}
	if len(updatedTask.SupersededBy) != 2 || updatedTask.SupersededBy[0] != "task-2" {
		t.Errorf("SupersededBy = %v, want [task-2 task-3]", updatedTask.SupersededBy)
	}
	if updatedTask.RescopeReason == nil || *updatedTask.RescopeReason != "Split into smaller tasks" {
		t.Error("RescopeReason not set correctly")
	}
	if updatedTask.AssignedTo != nil {
		t.Error("AssignedTo should be nil after superseding")
	}
	if updatedTask.Worktree != nil {
		t.Error("Worktree should be nil after superseding")
	}
	if updatedTask.ReviewCommit != nil {
		t.Errorf("ReviewCommit = %v, want nil after superseding", *updatedTask.ReviewCommit)
	}
	if updatedTask.ApprovedBy != nil {
		t.Errorf("ApprovedBy = %v, want nil after superseding", *updatedTask.ApprovedBy)
	}
	if len(updatedTask.Approvals) != 0 {
		t.Errorf("Approvals = %v, want cleared after superseding", updatedTask.Approvals)
	}
	if updatedTask.MergeCommit != nil {
		t.Errorf("MergeCommit = %v, want nil after superseding", *updatedTask.MergeCommit)
	}
	if updatedTask.IntegrationFailure != nil {
		t.Errorf("IntegrationFailure = %v, want nil after superseding", updatedTask.IntegrationFailure)
	}
	if len(updatedTask.FailedBy) != 1 || updatedTask.FailedBy[0] != "coder-1" {
		t.Errorf("FailedBy = %v, want preserved", updatedTask.FailedBy)
	}
	if len(updatedTask.Output) != 1 || updatedTask.Output[0].Desc != "retired output" {
		t.Errorf("Output = %v, want preserved as terminal audit context", updatedTask.Output)
	}

	lastHistory := updatedTask.History[len(updatedTask.History)-1]
	if lastHistory.Event != models.TaskEventSuperseded {
		t.Errorf("History event = %q, want %q", lastHistory.Event, models.TaskEventSuperseded)
	}
}

func TestSupersedeTask_RewritesActiveDependentDependencies(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	active := testhelpers.BuildTaskByStatus("active-dependent", models.TaskStatusReady, now)
	active.DependsOn = []string{"prep", "task-1", "replacement-a"}
	terminal := testhelpers.BuildTaskByStatus("terminal-dependent", models.TaskStatusMerged, now)
	terminal.DependsOn = []string{"task-1"}
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now),
		testhelpers.BuildTaskByStatus("prep", models.TaskStatusMerged, now),
		testhelpers.BuildTaskByStatus("replacement-a", models.TaskStatusMerged, now),
		testhelpers.BuildTaskByStatus("replacement-b", models.TaskStatusMerged, now),
		active,
		terminal,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := SupersedeTask(tmpDir, "task-1", []string{"replacement-a", "replacement-b"}, "Split into replacements", "orchestrator-1")
	if err != nil {
		t.Fatalf("SupersedeTask() error: %v", err)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	active = *readState.FindTask("active-dependent")
	wantDeps := []string{"prep", "replacement-a", "replacement-b"}
	if !slices.Equal(active.DependsOn, wantDeps) {
		t.Fatalf("active depends_on = %v, want %v", active.DependsOn, wantDeps)
	}
	lastHistory := active.History[len(active.History)-1]
	if lastHistory.Event != models.TaskEventDependenciesRewritten {
		t.Fatalf("active last history event = %q, want %q", lastHistory.Event, models.TaskEventDependenciesRewritten)
	}

	terminal = *readState.FindTask("terminal-dependent")
	if !slices.Equal(terminal.DependsOn, []string{"task-1"}) {
		t.Fatalf("terminal depends_on = %v, want historical dependency preserved", terminal.DependsOn)
	}
}

func TestSupersedeTask_PrunesOwnDownstreamDependencies(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	now := time.Now().UTC()
	target := testhelpers.BuildTaskByStatus("plan-old", models.TaskStatusBlocked, now)
	target.RolePair = "code-planning-pair"
	target.DependsOn = []string{"coding-a", "legal-plan", "coding-b"}
	codingA := testhelpers.BuildTaskByStatus("coding-a", models.TaskStatusReady, now)
	codingB := testhelpers.BuildTaskByStatus("coding-b", models.TaskStatusReady, now)
	legalPlan := testhelpers.BuildTaskByStatus("legal-plan", models.TaskStatusDraftCodingPlan, now)
	replacement := testhelpers.BuildTaskByStatus("replacement-plan", models.TaskStatusDraftCodingPlan, now)
	consumer := testhelpers.BuildTaskByStatus("active-consumer", models.TaskStatusReady, now)
	consumer.DependsOn = []string{"plan-old"}
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{target, codingA, legalPlan, codingB, replacement, consumer}
	for i := range state.Tasks {
		state.Tasks[i].SpecRef = state.Goal.SpecRef
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := SupersedeTask(tmpDir, "plan-old", []string{"replacement-plan"}, "Replace invalid plan", "orchestrator-1")
	if err != nil {
		t.Fatalf("SupersedeTask() error: %v", err)
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	updated := readState.FindTask("plan-old")
	if updated.Status != models.TaskStatusSuperseded {
		t.Fatalf("status = %s, want SUPERSEDED", updated.Status)
	}
	if !slices.Equal(updated.DependsOn, []string{"legal-plan"}) {
		t.Fatalf("depends_on = %v, want legal dependency preserved", updated.DependsOn)
	}
	if !slices.Equal(updated.SupersededBy, []string{"replacement-plan"}) {
		t.Fatalf("superseded_by = %v, want [replacement-plan]", updated.SupersededBy)
	}
	consumer = *readState.FindTask("active-consumer")
	if !slices.Equal(consumer.DependsOn, []string{"replacement-plan"}) {
		t.Fatalf("active consumer depends_on = %v, want [replacement-plan]", consumer.DependsOn)
	}

	lastHistory := updated.History[len(updated.History)-1]
	removed, ok := lastHistory.Extra["removed_dependencies"].([]any)
	if !ok || !reflect.DeepEqual(removed, []any{"coding-a", "coding-b"}) {
		t.Fatalf("removed_dependencies = %#v, want [coding-a coding-b]", lastHistory.Extra["removed_dependencies"])
	}
	if err := statevalidate.ValidateState(readState, tmpDir, false, io.Discard); err != nil {
		t.Fatalf("persisted state validation failed: %v", err)
	}
}

func TestSupersedeTask_PrunesSupersessionPathDownstreamDependency(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	now := time.Now().UTC()
	target := testhelpers.BuildTaskByStatus("plan-old", models.TaskStatusBlocked, now)
	target.RolePair = "code-planning-pair"
	target.DependsOn = []string{"retired-downstream-plan", "retired-legal-plan"}
	downstreamPath := testhelpers.BuildTaskByStatus("retired-downstream-plan", models.TaskStatusSuperseded, now)
	downstreamPath.RolePair = "code-planning-pair"
	downstreamPath.SupersededBy = []string{"coding-a", "coding-b"}
	downstreamPath.RescopeReason = testhelpers.StringPtr("Split into coding replacements")
	legalPath := testhelpers.BuildTaskByStatus("retired-legal-plan", models.TaskStatusSuperseded, now)
	legalPath.RolePair = "code-planning-pair"
	legalPath.SupersededBy = []string{"same-role-plan", "upstream-architecture"}
	legalPath.RescopeReason = testhelpers.StringPtr("Replaced by legal prerequisites")
	upstream := testhelpers.BuildTaskByStatus("upstream-architecture", models.TaskStatus("DRAFT_ARCHITECTURE"), now)
	upstream.RolePair = "architecture-pair"
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		target,
		downstreamPath,
		legalPath,
		testhelpers.BuildTaskByStatus("coding-a", models.TaskStatusReady, now),
		testhelpers.BuildTaskByStatus("coding-b", models.TaskStatusReady, now),
		testhelpers.BuildTaskByStatus("same-role-plan", models.TaskStatusDraftCodingPlan, now),
		upstream,
		testhelpers.BuildTaskByStatus("replacement-plan", models.TaskStatusDraftCodingPlan, now),
	}
	setTaskSpecRefs(state)
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := SupersedeTask(tmpDir, "plan-old", []string{"replacement-plan"}, "Replace invalid plan", "orchestrator-1")
	if err != nil {
		t.Fatalf("SupersedeTask() error: %v", err)
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	updated := readState.FindTask("plan-old")
	if updated == nil {
		t.Fatal("plan-old not found")
	}
	if updated.Status != models.TaskStatusSuperseded {
		t.Fatalf("status = %s, want SUPERSEDED", updated.Status)
	}
	if !slices.Equal(updated.DependsOn, []string{"retired-legal-plan"}) {
		t.Fatalf("depends_on = %v, want original legal path retained", updated.DependsOn)
	}
	if !slices.Equal(updated.SupersededBy, []string{"replacement-plan"}) {
		t.Fatalf("superseded_by = %v, want [replacement-plan]", updated.SupersededBy)
	}
	lastHistory := updated.History[len(updated.History)-1]
	if !reflect.DeepEqual(lastHistory.Extra["removed_dependencies"], []any{"retired-downstream-plan"}) {
		t.Fatalf("removed_dependencies = %#v, want original direct dependency", lastHistory.Extra["removed_dependencies"])
	}
	if err := statevalidate.ValidateState(readState, tmpDir, false, io.Discard); err != nil {
		t.Fatalf("persisted state validation failed: %v", err)
	}
}

func TestSupersedeTask_SupersessionPathInvalidCandidateLeavesStateUnchanged(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	now := time.Now().UTC()
	target := testhelpers.BuildTaskByStatus("plan-old", models.TaskStatusBlocked, now)
	target.RolePair = "code-planning-pair"
	target.DependsOn = []string{"retired-downstream-plan"}
	downstreamPath := testhelpers.BuildTaskByStatus("retired-downstream-plan", models.TaskStatusSuperseded, now)
	downstreamPath.RolePair = "code-planning-pair"
	downstreamPath.SupersededBy = []string{"coding-a"}
	downstreamPath.RescopeReason = testhelpers.StringPtr("Split into coding replacement")
	invalid := testhelpers.BuildTaskByStatus("invalid-task", models.TaskStatusReady, now)
	invalid.DependsOn = []string{"missing-task"}
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		target,
		downstreamPath,
		testhelpers.BuildTaskByStatus("coding-a", models.TaskStatusReady, now),
		testhelpers.BuildTaskByStatus("replacement-plan", models.TaskStatusDraftCodingPlan, now),
		invalid,
	}
	setTaskSpecRefs(state)
	testhelpers.WriteInitialState(t, stateFile, state)

	bb := db.New(stateFile)
	before, err := bb.Read()
	if err != nil {
		t.Fatalf("read initial state: %v", err)
	}
	_, err = SupersedeTask(tmpDir, "plan-old", []string{"replacement-plan"}, "Replace invalid plan", "orchestrator-1")
	testhelpers.RequireErrorContains(t, err, "missing-task")
	after, err := bb.Read()
	if err != nil {
		t.Fatalf("read state after rejected candidate: %v", err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("state changed after candidate validation failure\nbefore: %#v\nafter:  %#v", before, after)
	}
}

func TestSupersedeTask_InvalidCandidateLeavesStateUnchanged(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	now := time.Now().UTC()
	target := testhelpers.BuildTaskByStatus("plan-old", models.TaskStatusBlocked, now)
	target.RolePair = "code-planning-pair"
	target.DependsOn = []string{"coding-a", "legal-plan", "coding-b"}
	codingA := testhelpers.BuildTaskByStatus("coding-a", models.TaskStatusReady, now)
	codingB := testhelpers.BuildTaskByStatus("coding-b", models.TaskStatusReady, now)
	legalPlan := testhelpers.BuildTaskByStatus("legal-plan", models.TaskStatusDraftCodingPlan, now)
	replacement := testhelpers.BuildTaskByStatus("replacement-plan", models.TaskStatusDraftCodingPlan, now)
	consumer := testhelpers.BuildTaskByStatus("active-consumer", models.TaskStatusReady, now)
	consumer.DependsOn = []string{"plan-old"}
	invalid := testhelpers.BuildTaskByStatus("invalid-task", models.TaskStatusReady, now)
	invalid.DependsOn = []string{"missing-task"}
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{target, codingA, legalPlan, codingB, replacement, consumer, invalid}
	for i := range state.Tasks {
		state.Tasks[i].SpecRef = state.Goal.SpecRef
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	bb := db.New(stateFile)
	before, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read initial state: %v", err)
	}
	_, err = SupersedeTask(tmpDir, "plan-old", []string{"replacement-plan"}, "Replace invalid plan", "orchestrator-1")
	testhelpers.RequireErrorContains(t, err, "missing-task")
	after, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state after rejected candidate: %v", err)
	}
	if !reflect.DeepEqual(after.Tasks, before.Tasks) {
		t.Fatalf("tasks changed after candidate validation failure\nbefore: %#v\nafter:  %#v", before.Tasks, after.Tasks)
	}
}

func TestSupersedeTask_RewritesOperationalOutputTaskDependsOn(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	parent := testhelpers.BuildTaskByStatus("parent-plan", models.TaskStatusMerged, now)
	parent.RolePair = "architecture-pair"
	parent.Output = []models.OutputEntry{{
		Desc:          "Plan child",
		DoneWhen:      "child planned",
		Scope:         "internal/ops",
		SpecRef:       "specs/plan.md",
		TaskDependsOn: []string{"old-dep", "replacement-a"},
	}}
	oldDep := testhelpers.BuildTaskByStatus("old-dep", models.TaskStatusBlocked, now)
	oldDep.RolePair = "code-planning-pair"
	replacementA := testhelpers.BuildTaskByStatus("replacement-a", models.TaskStatusMerged, now)
	replacementA.RolePair = "code-planning-pair"
	replacementB := testhelpers.BuildTaskByStatus("replacement-b", models.TaskStatusMerged, now)
	replacementB.RolePair = "code-planning-pair"
	state.Tasks = []models.Task{parent, oldDep, replacementA, replacementB}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := SupersedeTask(tmpDir, "old-dep", []string{"replacement-a", "replacement-b"}, "Split into replacements", "orchestrator-1")
	if err != nil {
		t.Fatalf("SupersedeTask() error: %v", err)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	parent = *readState.FindTask("parent-plan")
	wantDeps := []string{"replacement-a", "replacement-b"}
	if !slices.Equal(parent.Output[0].TaskDependsOn, wantDeps) {
		t.Fatalf("output task_depends_on = %v, want %v", parent.Output[0].TaskDependsOn, wantDeps)
	}
}

func TestSupersedeTask_InvalidOutputReplacementLeavesStateUnchanged(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	parent := testhelpers.BuildTaskByStatus("parent-plan", models.TaskStatusMerged, now)
	parent.RolePair = "architecture-pair"
	parent.DependsOn = []string{"old-dep"}
	parent.Output = []models.OutputEntry{{
		Desc:          "Plan child",
		DoneWhen:      "child planned",
		Scope:         "internal/ops",
		SpecRef:       "specs/plan.md",
		TaskDependsOn: []string{"old-dep"},
	}}
	oldDep := testhelpers.BuildTaskByStatus("old-dep", models.TaskStatusBlocked, now)
	oldDep.RolePair = "coding-pair"
	replacement := testhelpers.BuildTaskByStatus("replacement-coding", models.TaskStatusMerged, now)
	replacement.RolePair = "coding-pair"
	state.Tasks = []models.Task{parent, oldDep, replacement}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := SupersedeTask(tmpDir, "old-dep", []string{"replacement-coding"}, "Replace with coding work", "orchestrator-1")
	testhelpers.RequireErrorContains(t, err, "downstream dependency")

	bb := db.New(stateFile)
	readState, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("Failed to read state: %v", readErr)
	}

	parent = *readState.FindTask("parent-plan")
	if !slices.Equal(parent.DependsOn, []string{"old-dep"}) {
		t.Fatalf("parent depends_on = %v, want unchanged [old-dep]", parent.DependsOn)
	}
	if !slices.Equal(parent.Output[0].TaskDependsOn, []string{"old-dep"}) {
		t.Fatalf("output task_depends_on = %v, want unchanged [old-dep]", parent.Output[0].TaskDependsOn)
	}
	oldDep = *readState.FindTask("old-dep")
	if oldDep.Status != models.TaskStatusBlocked {
		t.Fatalf("old dep status = %s, want BLOCKED", oldDep.Status)
	}
}

func TestSupersedeTask_NoReplacements(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := SupersedeTaskWithOptions(tmpDir, "task-1", nil, "Work already merged in prior sprint", "orchestrator-1", SupersedeTaskOptions{
		RecoverabilityCommand: "liza recover-task task-1",
	})
	if err != nil {
		t.Fatalf("SupersedeTask() error: %v", err)
	}

	if result.TaskID != "task-1" {
		t.Errorf("TaskID = %q, want %q", result.TaskID, "task-1")
	}
	if len(result.ReplacementIDs) != 0 {
		t.Errorf("ReplacementIDs = %v, want empty", result.ReplacementIDs)
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
	if task.Status != models.TaskStatusSuperseded {
		t.Errorf("Status = %v, want SUPERSEDED", task.Status)
	}
	if len(task.SupersededBy) != 0 {
		t.Errorf("SupersededBy = %v, want empty", task.SupersededBy)
	}
	if task.RescopeReason == nil || *task.RescopeReason != "Work already merged in prior sprint" {
		t.Error("RescopeReason not set correctly")
	}

	lastHistory := task.History[len(task.History)-1]
	if lastHistory.Note == nil || *lastHistory.Note != "superseded without replacements" {
		t.Errorf("History note = %v, want 'superseded without replacements'", lastHistory.Note)
	}
	if lastHistory.Extra["recoverability_command"] != "liza recover-task task-1" {
		t.Errorf("recoverability_command = %v, want command", lastHistory.Extra["recoverability_command"])
	}
	pre, ok := lastHistory.Extra["pre_supersession"].(map[string]any)
	if !ok {
		t.Fatalf("pre_supersession = %T, want map", lastHistory.Extra["pre_supersession"])
	}
	if pre["status"] != string(models.TaskStatusBlocked) {
		t.Errorf("pre_supersession.status = %v, want %s", pre["status"], models.TaskStatusBlocked)
	}
	if pre["branch"] != "task/task-1" {
		t.Errorf("pre_supersession.branch = %v, want task/task-1", pre["branch"])
	}
	if pre["branch_exists"] != false {
		t.Errorf("pre_supersession.branch_exists = %v, want false", pre["branch_exists"])
	}
	if pre["worktree_exists"] != false {
		t.Errorf("pre_supersession.worktree_exists = %v, want false", pre["worktree_exists"])
	}
}

func TestSupersedeTask_NoReplacementsRequiresRecoverabilityCommand(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := SupersedeTask(tmpDir, "task-1", nil, "Work already merged in prior sprint", "orchestrator-1")
	testhelpers.RequireErrorContains(t, err, "recoverability command is required when superseding without replacements")
}

func TestSupersedeTask_RejectsRecoverabilityCommandWithReplacements(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := SupersedeTaskWithOptions(tmpDir, "task-1", []string{"task-2"}, "Split into replacement", "orchestrator-1", SupersedeTaskOptions{
		RecoverabilityCommand: "liza recover-task task-1",
	})
	testhelpers.RequireErrorContains(t, err, "recoverability command is only valid when superseding without replacements")
}

func TestSupersedeTask_MasksRecoverabilityCommandBeforeHistory(t *testing.T) {
	t.Setenv("RECOVER_TOKEN", "secret-value-123")

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := SupersedeTaskWithOptions(tmpDir, "task-1", nil, "Work already merged", "orchestrator-1", SupersedeTaskOptions{
		RecoverabilityCommand: "RECOVER_TOKEN=secret-value-123 liza recover-task task-1",
	})
	if err != nil {
		t.Fatalf("SupersedeTaskWithOptions() error: %v", err)
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
	lastHistory := task.History[len(task.History)-1]
	if got := lastHistory.Extra["recoverability_command"]; got != "RECOVER_TOKEN=*** liza recover-task task-1" {
		t.Errorf("recoverability_command = %v, want masked command", got)
	}
}

func TestSupersedeTask_RejectsDownstreamReplacement(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	planningTask := testhelpers.BuildTaskByStatus("plan-1", models.TaskStatusBlocked, now)
	planningTask.RolePair = "code-planning-pair"
	codingTask := testhelpers.BuildTaskByStatus("coding-1", models.TaskStatusMerged, now)
	codingTask.RolePair = "coding-pair"

	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{planningTask, codingTask}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := SupersedeTask(tmpDir, "plan-1", []string{"coding-1"}, "Coding work already exists", "orchestrator-1")
	testhelpers.RequireErrorContains(t, err, "role_pair coding-pair is downstream of code-planning-pair")
}

func TestSupersedeTask_NoReplacements_DeletesBranch(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	// Create a branch for the task
	gw := git.New(tmpDir)
	baseCommit, err := gw.CreateWorktree("task-1", "main")
	if err != nil {
		t.Fatalf("CreateWorktree() error: %v", err)
	}

	// Verify branch exists
	exists, err := gw.BranchExists("task/task-1")
	if err != nil {
		t.Fatalf("BranchExists error: %v", err)
	}
	if !exists {
		t.Fatal("branch should exist before supersede")
	}

	// Set up state with BLOCKED task that has a worktree
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
	worktree := ".worktrees/task-1"
	task.Worktree = &worktree
	task.BaseCommit = &baseCommit
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := SupersedeTaskWithOptions(tmpDir, "task-1", nil, "Completed externally", "orchestrator-1", SupersedeTaskOptions{
		RecoverabilityCommand: "liza recover-task task-1",
	})
	if err != nil {
		t.Fatalf("SupersedeTask() error: %v", err)
	}

	// No warnings expected
	if len(result.Warnings) > 0 {
		t.Errorf("unexpected warnings: %v", result.Warnings)
	}

	// Verify worktree directory removed
	wtPath := filepath.Join(tmpDir, ".worktrees", "task-1")
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Error("worktree directory should be removed")
	}

	// Verify branch deleted — no successors to preserve it for
	exists, err = gw.BranchExists("task/task-1")
	if err != nil {
		t.Fatalf("BranchExists error: %v", err)
	}
	if exists {
		t.Error("branch should be deleted when superseding with no replacements")
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	updated := readState.FindTask("task-1")
	if updated == nil {
		t.Fatal("Task not found")
	}
	lastHistory := updated.History[len(updated.History)-1]
	pre, ok := lastHistory.Extra["pre_supersession"].(map[string]any)
	if !ok {
		t.Fatalf("pre_supersession = %T, want map", lastHistory.Extra["pre_supersession"])
	}
	if pre["branch_exists"] != true {
		t.Errorf("pre_supersession.branch_exists = %v, want true", pre["branch_exists"])
	}
	if pre["branch_head"] == "" {
		t.Error("pre_supersession.branch_head should be recorded")
	}
	if pre["worktree_exists"] != true {
		t.Errorf("pre_supersession.worktree_exists = %v, want true", pre["worktree_exists"])
	}
	if pre["worktree_head"] == "" {
		t.Error("pre_supersession.worktree_head should be recorded")
	}
	if pre["worktree_status"] != "" {
		t.Errorf("pre_supersession.worktree_status = %v, want clean status", pre["worktree_status"])
	}
	if pre["base_commit"] != baseCommit {
		t.Errorf("pre_supersession.base_commit = %v, want %s", pre["base_commit"], baseCommit)
	}
}

func TestSupersedeTask_FromRejected(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := SupersedeTask(tmpDir, "task-1", []string{"task-2"}, "Rewrite needed", "orchestrator-1")
	if err != nil {
		t.Fatalf("SupersedeTask() error: %v", err)
	}
	if result.OriginalStatus != models.TaskStatusRejected {
		t.Errorf("OriginalStatus = %v, want REJECTED", result.OriginalStatus)
	}
}

func TestSupersedeTask_FromIntegrationFailedMissingWorktree(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusIntegrationFailed, now)
	worktree := ".worktrees/task-1"
	task.Worktree = &worktree
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := SupersedeTaskWithOptions(tmpDir, "task-1", nil, "Completed externally by merged PR", "orchestrator-1", SupersedeTaskOptions{
		RecoverabilityCommand: "liza recover-task task-1",
	})
	if err != nil {
		t.Fatalf("SupersedeTask() error: %v", err)
	}
	if result.OriginalStatus != models.TaskStatusIntegrationFailed {
		t.Errorf("OriginalStatus = %v, want INTEGRATION_FAILED", result.OriginalStatus)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	updated := readState.FindTask("task-1")
	if updated == nil {
		t.Fatal("Task not found")
	}
	if updated.Status != models.TaskStatusSuperseded {
		t.Errorf("Status = %v, want SUPERSEDED", updated.Status)
	}
	if updated.Worktree != nil {
		t.Errorf("Worktree = %v, want nil", *updated.Worktree)
	}
}

func TestSupersedeTask_FromReady(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := SupersedeTask(tmpDir, "task-1", []string{"task-2"}, "No longer needed", "orchestrator-1")
	if err != nil {
		t.Fatalf("SupersedeTask() error: %v", err)
	}
}

func TestSupersedeTask_FromPipelineDraftState(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	// DRAFT_ARCHITECTURE is a pipeline-declared initial state, not hardcoded.
	task := models.Task{
		ID:          "task-1",
		Type:        models.TaskTypeCoding,
		Description: "Architecture task",
		Status:      "DRAFT_ARCHITECTURE",
		Priority:    1,
		Created:     now,
		SpecRef:     "README.md",
		DoneWhen:    "Architecture defined",
		Scope:       "Test scope",
		History:     []models.TaskHistoryEntry{},
		RolePair:    "architecture-pair",
	}
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := SupersedeTask(tmpDir, "task-1", []string{"task-2"}, "Scope changed", "orchestrator-1")
	if err != nil {
		t.Fatalf("SupersedeTask() error: %v", err)
	}
	if result.OriginalStatus != "DRAFT_ARCHITECTURE" {
		t.Errorf("OriginalStatus = %v, want DRAFT_ARCHITECTURE", result.OriginalStatus)
	}
}

func TestSupersedeTask_LegacyTaskNoRolePair(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	// Legacy task with no role_pair — must still be supersedeable from DRAFT_CODE.
	task := models.Task{
		ID:          "task-1",
		Type:        models.TaskTypeCoding,
		Description: "Legacy task",
		Status:      models.TaskStatusReady, // DRAFT_CODE
		Priority:    1,
		Created:     now,
		SpecRef:     "README.md",
		DoneWhen:    "Done",
		Scope:       "Test scope",
		History:     []models.TaskHistoryEntry{},
		RolePair:    "", // no role-pair — legacy
	}
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := SupersedeTask(tmpDir, "task-1", []string{"task-2"}, "Legacy rescope", "orchestrator-1")
	if err != nil {
		t.Fatalf("SupersedeTask() error: %v", err)
	}
	if result.OriginalStatus != models.TaskStatusReady {
		t.Errorf("OriginalStatus = %v, want DRAFT_CODE", result.OriginalStatus)
	}
}

func TestSupersedeTask_WrongStatus(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := SupersedeTask(tmpDir, "task-1", []string{"task-2"}, "reason", "orchestrator-1")
	testhelpers.RequireErrorContains(t, err, "cannot supersede")
}

func TestSupersedeTask_TaskNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := SupersedeTask(tmpDir, "nonexistent", []string{"task-2"}, "reason", "orchestrator-1")
	if err == nil {
		t.Fatal("Expected error for nonexistent task")
	}
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFoundError, got %T: %v", err, err)
	}
}

func TestSupersedeTask_EmptyAgentIDReturnsError(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	// Empty agentID should now return an error
	_, err := SupersedeTask(tmpDir, "task-1", []string{"task-2"}, "reason", "")
	if err == nil {
		t.Fatal("expected error for empty agentID, got nil")
	}
	testhelpers.AssertErrorContains(t, err, "orchestrator agent ID is required")
}

func TestSupersedeTask_CleansUpWorktree(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	// Create a real git worktree
	gw := git.New(tmpDir)
	_, err := gw.CreateWorktree("task-1", "main")
	if err != nil {
		t.Fatalf("CreateWorktree() error: %v", err)
	}

	// Verify worktree directory exists
	wtPath := filepath.Join(tmpDir, ".worktrees", "task-1")
	if _, err := os.Stat(wtPath); err != nil {
		t.Fatalf("worktree directory should exist: %v", err)
	}

	// Set up state with BLOCKED task that has a worktree
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
	worktree := ".worktrees/task-1"
	task.Worktree = &worktree
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := SupersedeTask(tmpDir, "task-1", []string{"task-2"}, "Split into smaller tasks", "orchestrator-1")
	if err != nil {
		t.Fatalf("SupersedeTask() error: %v", err)
	}

	// No warnings expected — cleanup should succeed with real git repo
	if len(result.Warnings) > 0 {
		t.Errorf("unexpected warnings: %v", result.Warnings)
	}

	// Verify worktree directory removed
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Error("worktree directory should be removed after supersede")
	}

	// Verify state: Worktree field cleared
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	updatedTask := readState.FindTask("task-1")
	if updatedTask.Worktree != nil {
		t.Error("Worktree should be nil in state after supersede")
	}

	// Verify branch preserved — successors may need it via git show
	exists, brErr := gw.BranchExists("task/task-1")
	if brErr != nil {
		t.Fatalf("BranchExists error: %v", brErr)
	}
	if !exists {
		t.Error("task branch should be preserved after supersede for successor access")
	}
}

func TestInTransactionCores_SupersedeTaskInStateMatchesCommand(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	seeded := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	fixture := func() *models.State {
		target := testhelpers.BuildTaskByStatus("plan-old", models.TaskStatusBlocked, seeded)
		target.RolePair = "code-planning-pair"
		target.DependsOn = []string{"coding-a", "legal-plan"}
		consumer := testhelpers.BuildTaskByStatus("active-consumer", models.TaskStatusReady, seeded)
		consumer.DependsOn = []string{"plan-old"}
		state := testhelpers.CreateValidState()
		state.Tasks = []models.Task{
			target,
			testhelpers.BuildTaskByStatus("coding-a", models.TaskStatusReady, seeded),
			testhelpers.BuildTaskByStatus("legal-plan", models.TaskStatusDraftCodingPlan, seeded),
			testhelpers.BuildTaskByStatus("replacement-plan", models.TaskStatusDraftCodingPlan, seeded),
			consumer,
		}
		for i := range state.Tasks {
			state.Tasks[i].SpecRef = state.Goal.SpecRef
		}
		return state
	}

	testhelpers.WriteInitialState(t, stateFile, fixture())
	if _, err := SupersedeTask(tmpDir, "plan-old", []string{"replacement-plan"}, "Replace invalid plan", "orchestrator-1"); err != nil {
		t.Fatalf("SupersedeTask() error: %v", err)
	}
	commandState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}

	pb, err := loadPipelineBundle(tmpDir)
	if err != nil {
		t.Fatalf("loadPipelineBundle() error: %v", err)
	}
	coreState := fixture()
	target := coreState.FindTask("plan-old")
	revisionBefore := uint64(0)
	if target.Lifecycle != nil {
		revisionBefore = target.Lifecycle.Revision
	}
	removed, err := supersedeTaskInState(coreState, pb, target, []string{"replacement-plan"}, "Replace invalid plan", "orchestrator-1", nil, seeded)
	if err != nil {
		t.Fatalf("supersedeTaskInState() error: %v", err)
	}
	if !slices.Equal(removed, []string{"coding-a"}) {
		t.Fatalf("removed dependencies = %v, want [coding-a]", removed)
	}
	// The core advances the lifecycle once; the caller's CompleteLifecycleRequest
	// adds the second advance and the receipt outside this boundary.
	if target.Lifecycle == nil || target.Lifecycle.Revision != revisionBefore+1 {
		t.Fatalf("core lifecycle revision = %v, want %d", target.Lifecycle, revisionBefore+1)
	}

	// Pinned independently of the command, so a core-only regression cannot hide
	// behind the differential comparison below.
	if target.Status != models.TaskStatusSuperseded {
		t.Fatalf("core status = %s, want SUPERSEDED", target.Status)
	}
	if !slices.Equal(target.SupersededBy, []string{"replacement-plan"}) {
		t.Fatalf("core superseded_by = %v, want [replacement-plan]", target.SupersededBy)
	}
	if !slices.Equal(target.DependsOn, []string{"legal-plan"}) {
		t.Fatalf("core depends_on = %v, want the legal dependency retained", target.DependsOn)
	}
	if target.AssignedTo != nil || target.Worktree != nil || target.LeaseExpires != nil {
		t.Fatalf("core left ownership on the superseded task: %+v", target)
	}
	audit := target.History[len(target.History)-1]
	if audit.Event != models.TaskEventSuperseded || audit.Note == nil || *audit.Note != "replaced by: replacement-plan" {
		t.Fatalf("core audit entry = %+v, want a superseded entry naming the replacement", audit)
	}
	if !reflect.DeepEqual(audit.Extra["removed_dependencies"], []string{"coding-a"}) {
		t.Fatalf("core removed_dependencies = %#v, want [coding-a]", audit.Extra["removed_dependencies"])
	}
	if consumer := coreState.FindTask("active-consumer"); !slices.Equal(consumer.DependsOn, []string{"replacement-plan"}) {
		t.Fatalf("core consumer depends_on = %v, want [replacement-plan]", consumer.DependsOn)
	}

	for _, taskID := range []string{"plan-old", "active-consumer", "coding-a", "legal-plan", "replacement-plan"} {
		assertCoreMatchesCommand(t, coreState, commandState, taskID)
	}
	if err := statevalidate.ValidateState(coreState, tmpDir, true, io.Discard); err != nil {
		t.Fatalf("core candidate state validation failed: %v", err)
	}
}
