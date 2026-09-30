package ops

import (
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	activitylog "github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/statevalidate"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestRepairSupersededDependencies_RemovesAllIllegalEdgesAtomically(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	now := time.Now().UTC()
	target := testhelpers.BuildTaskByStatus("plan-old", models.TaskStatusSuperseded, now)
	target.RolePair = "code-planning-pair"
	target.DependsOn = []string{"coding-a", "legal-plan", "coding-b"}
	target.SupersededBy = []string{"replacement-plan"}
	target.RescopeReason = testhelpers.StringPtr("Replaced invalid plan")
	initialHistoryLen := len(target.History)

	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		target,
		testhelpers.BuildTaskByStatus("coding-a", models.TaskStatusReady, now),
		testhelpers.BuildTaskByStatus("legal-plan", models.TaskStatusDraftCodingPlan, now),
		testhelpers.BuildTaskByStatus("coding-b", models.TaskStatusReady, now),
		testhelpers.BuildTaskByStatus("replacement-plan", models.TaskStatusDraftCodingPlan, now),
	}
	setTaskSpecRefs(state)
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := RepairSupersededDependencies(tmpDir, "plan-old", "Repair terminal dependency metadata", "orchestrator-1")
	if err != nil {
		t.Fatalf("RepairSupersededDependencies() error: %v", err)
	}
	if result.TaskID != "plan-old" {
		t.Fatalf("TaskID = %q, want plan-old", result.TaskID)
	}
	if !slices.Equal(result.RemovedDependencies, []string{"coding-a", "coding-b"}) {
		t.Fatalf("RemovedDependencies = %v, want [coding-a coding-b]", result.RemovedDependencies)
	}
	if !slices.Equal(result.RetainedDependencies, []string{"legal-plan"}) {
		t.Fatalf("RetainedDependencies = %v, want [legal-plan]", result.RetainedDependencies)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("Warnings = %v, want none", result.Warnings)
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
	if !slices.Equal(updated.DependsOn, []string{"legal-plan"}) {
		t.Fatalf("depends_on = %v, want [legal-plan]", updated.DependsOn)
	}
	if !slices.Equal(updated.SupersededBy, []string{"replacement-plan"}) {
		t.Fatalf("superseded_by = %v, want [replacement-plan]", updated.SupersededBy)
	}
	if updated.RescopeReason == nil || *updated.RescopeReason != "Replaced invalid plan" {
		t.Fatalf("rescope_reason = %v, want Replaced invalid plan", updated.RescopeReason)
	}
	if len(updated.History) != initialHistoryLen+1 {
		t.Fatalf("history length = %d, want %d", len(updated.History), initialHistoryLen+1)
	}
	last := updated.History[len(updated.History)-1]
	if last.Event != models.TaskEventDependenciesRewritten {
		t.Fatalf("history event = %q, want %q", last.Event, models.TaskEventDependenciesRewritten)
	}
	if last.Agent == nil || *last.Agent != "orchestrator-1" {
		t.Fatalf("history agent = %v, want orchestrator-1", last.Agent)
	}
	if last.Reason == nil || *last.Reason != "Repair terminal dependency metadata" {
		t.Fatalf("history reason = %v, want repair reason", last.Reason)
	}
	if last.Extra["operation"] != "repair-superseded-dependencies" {
		t.Fatalf("history operation = %v, want repair-superseded-dependencies", last.Extra["operation"])
	}
	if !reflect.DeepEqual(last.Extra["removed_dependencies"], []any{"coding-a", "coding-b"}) {
		t.Fatalf("history removed_dependencies = %#v", last.Extra["removed_dependencies"])
	}
	if !reflect.DeepEqual(last.Extra["retained_dependencies"], []any{"legal-plan"}) {
		t.Fatalf("history retained_dependencies = %#v", last.Extra["retained_dependencies"])
	}

	entries, err := activitylog.New(paths.New(tmpDir).LogPath()).Read()
	if err != nil {
		t.Fatalf("read activity log: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("activity log entries = %d, want 1", len(entries))
	}
	entry := entries[0]
	if entry.Action != "repair-superseded-dependencies" || entry.Agent != "orchestrator-1" {
		t.Fatalf("activity log action/agent = %q/%q", entry.Action, entry.Agent)
	}
	if entry.Task == nil || *entry.Task != "plan-old" {
		t.Fatalf("activity log task = %v, want plan-old", entry.Task)
	}
	for _, want := range []string{"coding-a", "coding-b", "legal-plan", "Repair terminal dependency metadata"} {
		if !strings.Contains(entry.Detail, want) {
			t.Fatalf("activity log detail = %q, want %q", entry.Detail, want)
		}
	}
	if err := statevalidate.ValidateState(readState, tmpDir, false, io.Discard); err != nil {
		t.Fatalf("persisted state validation failed: %v", err)
	}
}

func TestRepairSupersededDependencies_PrunesSupersessionPathDownstreamDependency(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	now := time.Now().UTC()
	target := testhelpers.BuildTaskByStatus("plan-old", models.TaskStatusSuperseded, now)
	target.RolePair = "code-planning-pair"
	target.DependsOn = []string{"retired-downstream-plan", "retired-legal-plan"}
	target.SupersededBy = []string{"replacement-plan"}
	target.RescopeReason = testhelpers.StringPtr("Replaced invalid plan")
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

	result, err := RepairSupersededDependencies(tmpDir, "plan-old", "Repair terminal dependency metadata", "orchestrator-1")
	if err != nil {
		t.Fatalf("RepairSupersededDependencies() error: %v", err)
	}
	if !slices.Equal(result.RemovedDependencies, []string{"retired-downstream-plan"}) {
		t.Fatalf("RemovedDependencies = %v, want original direct dependency", result.RemovedDependencies)
	}
	if !slices.Equal(result.RetainedDependencies, []string{"retired-legal-plan"}) {
		t.Fatalf("RetainedDependencies = %v, want original legal path", result.RetainedDependencies)
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
	if updated.RescopeReason == nil || *updated.RescopeReason != "Replaced invalid plan" {
		t.Fatalf("rescope_reason = %v, want Replaced invalid plan", updated.RescopeReason)
	}
	lastHistory := updated.History[len(updated.History)-1]
	if !reflect.DeepEqual(lastHistory.Extra["removed_dependencies"], []any{"retired-downstream-plan"}) {
		t.Fatalf("history removed_dependencies = %#v, want original direct dependency", lastHistory.Extra["removed_dependencies"])
	}
	if !reflect.DeepEqual(lastHistory.Extra["retained_dependencies"], []any{"retired-legal-plan"}) {
		t.Fatalf("history retained_dependencies = %#v, want original legal path", lastHistory.Extra["retained_dependencies"])
	}
	entries, err := activitylog.New(paths.New(tmpDir).LogPath()).Read()
	if err != nil {
		t.Fatalf("read activity log: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("activity log entries = %d, want 1", len(entries))
	}
	if !strings.Contains(entries[0].Detail, "retired-downstream-plan") || strings.Contains(entries[0].Detail, "coding-a") {
		t.Fatalf("activity log detail = %q, want original direct dependency only", entries[0].Detail)
	}
	if err := statevalidate.ValidateState(readState, tmpDir, false, io.Discard); err != nil {
		t.Fatalf("persisted state validation failed: %v", err)
	}
}

// Removing a terminal task's edges cannot add a violation, so an unrelated
// invalid record, which used to refuse this repair, is now left in place
// (ADR-0165). Rollback on refusal is pinned by the precondition tests below.
func TestRepairSupersededDependencies_SupersessionPathIgnoresUnrelatedInvalidRecord(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	now := time.Now().UTC()
	target := testhelpers.BuildTaskByStatus("plan-old", models.TaskStatusSuperseded, now)
	target.RolePair = "code-planning-pair"
	target.DependsOn = []string{"retired-downstream-plan"}
	target.SupersededBy = []string{"replacement-plan"}
	target.RescopeReason = testhelpers.StringPtr("Replaced invalid plan")
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

	result, err := RepairSupersededDependencies(tmpDir, "plan-old", "Repair terminal dependency metadata", "orchestrator-1")
	if err != nil {
		t.Fatalf("RepairSupersededDependencies() error = %v, want the unrelated invalid task ignored", err)
	}
	if !slices.Equal(result.RemovedDependencies, []string{"retired-downstream-plan"}) {
		t.Fatalf("RemovedDependencies = %v, want [retired-downstream-plan]", result.RemovedDependencies)
	}
	after, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if got := after.FindTask("invalid-task").DependsOn; !slices.Equal(got, []string{"missing-task"}) {
		t.Fatalf("invalid-task depends_on = %v, want untouched [missing-task]", got)
	}
}

func TestRepairSupersededDependencies_RejectsNonSupersededTaskWithoutMutation(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	target := testhelpers.BuildTaskByStatus("plan-old", models.TaskStatusBlocked, now)
	target.RolePair = "code-planning-pair"
	target.DependsOn = []string{"coding-a"}
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		target,
		testhelpers.BuildTaskByStatus("coding-a", models.TaskStatusReady, now),
	}

	assertRepairSupersededDependenciesRejectedWithoutMutation(t, state, "cannot repair dependencies on task plan-old in status BLOCKED")
}

func TestRepairSupersededDependencies_RejectsAlreadyValidTaskWithoutMutation(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	target := testhelpers.BuildTaskByStatus("plan-old", models.TaskStatusSuperseded, now)
	target.RolePair = "code-planning-pair"
	target.DependsOn = []string{"legal-plan"}
	target.RescopeReason = testhelpers.StringPtr("Already replaced")
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		target,
		testhelpers.BuildTaskByStatus("legal-plan", models.TaskStatusDraftCodingPlan, now),
	}

	assertRepairSupersededDependenciesRejectedWithoutMutation(t, state, "has no illegal downstream dependencies")
}

func assertRepairSupersededDependenciesRejectedWithoutMutation(t *testing.T, state *models.State, wantError string) {
	t.Helper()
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")
	setTaskSpecRefs(state)
	testhelpers.WriteInitialState(t, stateFile, state)

	bb := db.New(stateFile)
	before, err := bb.Read()
	if err != nil {
		t.Fatalf("read initial state: %v", err)
	}
	_, err = RepairSupersededDependencies(tmpDir, "plan-old", "Repair terminal dependency metadata", "orchestrator-1")
	testhelpers.RequireErrorContains(t, err, wantError)
	after, err := bb.Read()
	if err != nil {
		t.Fatalf("read state after rejected repair: %v", err)
	}
	if !reflect.DeepEqual(after.Tasks, before.Tasks) {
		t.Fatalf("tasks changed after rejected repair\nbefore: %#v\nafter:  %#v", before.Tasks, after.Tasks)
	}
}

func setTaskSpecRefs(state *models.State) {
	for i := range state.Tasks {
		state.Tasks[i].SpecRef = state.Goal.SpecRef
	}
}
