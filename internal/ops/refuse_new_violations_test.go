package ops

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/statevalidate"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// These tests pin D84's fix (ADR-0165): a mutation is refused only for
// violations it adds. An invalid record it does not touch no longer vetoes it,
// and no longer hides what the mutation itself got wrong.

// injectLegacyStall persists the D83 record: a retry_loop anomaly missing the
// details its type requires, attached to no task. Modify now refuses to add
// such a record (ADR-0166), so it is written through the unchecked Write, as
// if an older binary had left it.
func injectLegacyStall(t *testing.T, stateFile string) {
	t.Helper()
	bb := db.For(stateFile)
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("read state before injecting legacy stall anomaly: %v", err)
	}
	state.Anomalies = append(state.Anomalies, testhelpers.LegacyPendingMergeStallAnomaly())
	if err := bb.Write(state); err != nil {
		t.Fatalf("inject legacy stall anomaly: %v", err)
	}
}

// requireIntroducedViolation asserts that candidate validation, not an earlier
// precondition, refused the mutation, and that it named the given violation.
func requireIntroducedViolation(t *testing.T, err error, substring string) {
	t.Helper()
	testhelpers.RequireErrorContains(t, err, substring)
	var violations *statevalidate.ViolationList
	if !errors.As(err, &violations) {
		t.Fatalf("error = %T: %v, want a candidate-validation *statevalidate.ViolationList", err, err)
	}
}

// T1: the D83 outage. A rejected task is reclaimed although an unrelated
// invalid anomaly is present.
func TestClaimTask_RejectedReclaimOverUnrelatedInvalidRecord(t *testing.T) {
	fixture := newRejectedHandoffFixture(t, true)
	injectLegacyStall(t, fixture.stateFile)

	if _, err := ClaimTask(fixture.projectRoot, fixture.taskID, "coder-2"); err != nil {
		t.Fatalf("ClaimTask() error = %v, want the reclaim to ignore the unrelated invalid anomaly", err)
	}
	// The helper also validates the whole claimed state; drop the injected
	// record first so that check judges only what the claim wrote.
	if err := db.For(fixture.stateFile).Modify(func(state *models.State) error {
		if len(state.Anomalies) != 1 {
			return fmt.Errorf("anomalies = %d, want the injected record only", len(state.Anomalies))
		}
		state.Anomalies = nil
		return nil
	}); err != nil {
		t.Fatalf("remove injected anomaly: %v", err)
	}
	assertRejectedClaimState(t, fixture, "coder-2", fixture.branchSHA)
}

// T2: supersede goes through over an unrelated invalid task.
func TestSupersedeTask_OverUnrelatedInvalidRecord(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	now := time.Now().UTC()
	target := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
	invalid := testhelpers.BuildTaskByStatus("invalid-task", models.TaskStatusReady, now)
	invalid.DependsOn = []string{"missing-task"}
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{target, invalid}
	setTaskSpecRefs(state)
	testhelpers.WriteInitialState(t, stateFile, state)

	if _, err := SupersedeTask(tmpDir, "task-1", []string{"task-2"}, "Split", "orchestrator-1"); err != nil {
		t.Fatalf("SupersedeTask() error = %v, want the unrelated invalid task ignored", err)
	}
	after, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if got := after.FindTask("task-1").Status; got != models.TaskStatusSuperseded {
		t.Fatalf("task-1 status = %s, want SUPERSEDED", got)
	}
	if got := after.FindTask("invalid-task").DependsOn; !slices.Equal(got, []string{"missing-task"}) {
		t.Fatalf("invalid-task depends_on = %v, want untouched [missing-task]", got)
	}
}

// T3: a pre-existing violation that validation reaches first no longer masks
// the cycle the retarget creates; the refusal carries the cycle diagnostics.
func TestRetargetDependency_CycleReportedOverUnrelatedInvalidRecord(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Goal.SpecRef = "README.md"
	task := testhelpers.BuildTaskByStatus("A", models.TaskStatusBlocked, now)
	task.DependsOn = []string{"old-dep"}
	taskB := testhelpers.BuildTaskByStatus("B", models.TaskStatusReady, now)
	taskB.DependsOn = []string{"C"}
	taskC := testhelpers.BuildTaskByStatus("C", models.TaskStatusReady, now)
	taskC.DependsOn = []string{"A"}
	unrelated := testhelpers.BuildTaskByStatus("Z", models.TaskStatusBlocked, now)
	unrelated.BlockedReason = nil
	state.Tasks = []models.Task{
		task,
		testhelpers.BuildTaskByStatus("old-dep", models.TaskStatusMerged, now),
		taskB,
		taskC,
		unrelated,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := RetargetDependency(tmpDir, "A", "old-dep", []string{"B"}, "repair", "orchestrator-1")
	if err == nil {
		t.Fatal("RetargetDependency() error = nil, want the new cycle refused")
	}
	if strings.Contains(err.Error(), "BLOCKED task without blocked_reason: Z") {
		t.Fatalf("RetargetDependency() error = %v, want the cycle, not task Z's pre-existing violation", err)
	}
	var operational *OperationalError
	if !errors.As(err, &operational) {
		t.Fatalf("RetargetDependency() error = %T: %v, want *OperationalError", err, err)
	}
	cyclePath, ok := operational.SafeDetails()["cycle_path"].([]string)
	if !ok || !slices.Equal(cyclePath, []string{"A", "B", "C", "A"}) {
		t.Fatalf("cycle_path = %#v, want [A B C A]", operational.SafeDetails()["cycle_path"])
	}
}

// T6: repairing one SUPERSEDED task is not vetoed by another SUPERSEDED task
// that still carries illegal downstream edges of its own.
func TestRepairSupersededDependencies_RepairsOneOfTwoInvalidTasks(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")

	now := time.Now().UTC()
	superseded := func(id string, deps ...string) models.Task {
		task := testhelpers.BuildTaskByStatus(id, models.TaskStatusSuperseded, now)
		task.RolePair = "code-planning-pair"
		task.DependsOn = deps
		task.SupersededBy = []string{"replacement-plan"}
		task.RescopeReason = testhelpers.StringPtr("Replaced invalid plan")
		return task
	}
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		superseded("plan-old", "coding-a"),
		superseded("plan-older", "coding-b"),
		testhelpers.BuildTaskByStatus("coding-a", models.TaskStatusReady, now),
		testhelpers.BuildTaskByStatus("coding-b", models.TaskStatusReady, now),
		testhelpers.BuildTaskByStatus("replacement-plan", models.TaskStatusDraftCodingPlan, now),
	}
	setTaskSpecRefs(state)
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := RepairSupersededDependencies(tmpDir, "plan-old", "Repair terminal dependency metadata", "orchestrator-1")
	if err != nil {
		t.Fatalf("RepairSupersededDependencies(plan-old) error = %v, want plan-older's own edges ignored", err)
	}
	if !slices.Equal(result.RemovedDependencies, []string{"coding-a"}) {
		t.Fatalf("RemovedDependencies = %v, want [coding-a]", result.RemovedDependencies)
	}
	after, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if got := after.FindTask("plan-older").DependsOn; !slices.Equal(got, []string{"coding-b"}) {
		t.Fatalf("plan-older depends_on = %v, want untouched [coding-b]", got)
	}
}

// T10a: an integration verdict, which validates its candidate lifecycle, is
// not vetoed by an unrelated invalid record.
func TestSubmitVerdict_IntegrationVerdictOverUnrelatedInvalidRecord(t *testing.T) {
	fixture := newSubmitVerdictIntegrationFixture(t, models.IntegrationAnalysisPhaseSlice, nil)
	fixture.mutateState(t, func(state *models.State) {
		state.Anomalies = append(state.Anomalies, testhelpers.LegacyPendingMergeStallAnomaly())
	})

	if _, err := SubmitVerdict(fixture.projectRoot, fixture.taskID, "APPROVED", "", fixture.reviewerID, ""); err != nil {
		t.Fatalf("SubmitVerdict() error = %v, want the unrelated invalid anomaly ignored", err)
	}
}

// T10b (guard, codex plan-2 note): the verdict's delta baseline is the locked
// pre-image, not the lifecycle snapshot whose Agents map aliases the
// candidate. An agent violation introduced inside the transaction must still
// refuse it; an aliased baseline would see the same violation and wave it
// through.
func TestSubmitVerdict_IntegrationVerdictRefusesViolationAddedInTransaction(t *testing.T) {
	fixture := newSubmitVerdictIntegrationFixture(t, models.IntegrationAnalysisPhaseSlice, nil)
	previousHooks := testSubmitVerdictHooks
	testSubmitVerdictHooks = &submitVerdictTestHooks{beforeValidation: func(state *models.State) {
		state.Agents["coder-9"] = models.Agent{Role: "coder", Status: models.AgentStatusWorking}
	}}
	t.Cleanup(func() { testSubmitVerdictHooks = previousHooks })
	before := fixture.readState(t)

	_, err := SubmitVerdict(fixture.projectRoot, fixture.taskID, "APPROVED", "", fixture.reviewerID, "")
	testhelpers.RequireErrorContains(t, err, "agent coder-9 has status WORKING but no current_task assigned")
	assertSubmitVerdictTransactionUnchanged(t, before, fixture.readState(t), fixture.taskID, fixture.reviewerID)
}

// T9a: replaying a completed recovery is not vetoed by an unrelated invalid
// record.
func TestRecoverIntegrationReplayOverUnrelatedInvalidRecord(t *testing.T) {
	f, id := prematureRecoveryFixture(t)
	if _, err := RecoverIntegration(f.projectRoot, id, "premature freeze", false); err != nil {
		t.Fatal(err)
	}
	f.mutateState(t, func(s *models.State) {
		s.Anomalies = append(s.Anomalies, testhelpers.LegacyPendingMergeStallAnomaly())
	})

	replay, err := RecoverIntegration(f.projectRoot, id, "premature freeze", false)
	if err != nil {
		t.Fatalf("replay error = %v, want the unrelated invalid anomaly ignored", err)
	}
	if !replay.Replayed {
		t.Fatal("replay.Replayed = false, want true")
	}
}

// T9b (guard, codex R3): the replay still refuses when its own recovery
// evidence is corrupt, even though it writes nothing.
func TestRecoverIntegrationReplayRefusesCorruptOwnEvidence(t *testing.T) {
	f, id := prematureRecoveryFixture(t)
	if _, err := RecoverIntegration(f.projectRoot, id, "premature freeze", false); err != nil {
		t.Fatal(err)
	}
	f.mutateState(t, func(s *models.State) {
		s.Goal.Integration.PrematureRecovery.PreservationRef = "refs/elsewhere"
	})

	_, err := RecoverIntegration(f.projectRoot, id, "premature freeze", false)
	if err == nil || !strings.Contains(err.Error(), "invalid integration premature recovery receipt") {
		t.Fatalf("replay error = %v, want the corrupt receipt refused", err)
	}
}
