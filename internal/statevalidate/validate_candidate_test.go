package statevalidate

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// candidateFixture returns a valid baseline holding the given tasks. Tests
// derive the candidate from a deep copy, so baseline and candidate share
// nothing a mutation could alias.
func candidateFixture(tasks ...models.Task) *models.State {
	state := testhelpers.CreateValidState()
	state.Tasks = append(state.Tasks, tasks...)
	return state
}

func pipelineRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	testhelpers.SetupPipelineConfig(t, root)
	return root
}

// blockedTask is a valid non-terminal task past its initial status, so it
// must carry done_when and spec_ref (initial statuses are drafts and need
// neither).
func blockedTask(id string, now time.Time) models.Task {
	task := testhelpers.BuildTaskByStatus(id, models.TaskStatusBlocked, now)
	reason := "waiting on a dependency"
	task.BlockedReason = &reason
	return task
}

func baselineOf(state *models.State) func() (*models.State, error) {
	return func() (*models.State, error) { return state, nil }
}

func assertMentions(t *testing.T, label, got string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("%s = %q, want it to mention %q", label, got, w)
		}
	}
}

func assertOmits(t *testing.T, label, got string, unwanted ...string) {
	t.Helper()
	for _, u := range unwanted {
		if strings.Contains(got, u) {
			t.Errorf("%s = %q, want it not to mention %q", label, got, u)
		}
	}
}

// TestValidateState_ReportsEveryViolation (T4/T7): validation keeps going
// after the first violation, across validators, across records and within one
// record, so `validate` shows all the damage and a later check cannot hide
// behind an earlier one.
func TestValidateState_ReportsEveryViolation(t *testing.T) {
	now := time.Now().UTC()
	task := blockedTask("task-a", now)
	task.DoneWhen = ""
	task.SpecRef = ""
	orphan := blockedTask("task-b", now)
	orphan.DependsOn = []string{"task-missing"}
	state := candidateFixture(task, orphan)
	state.Anomalies = []models.Anomaly{
		{Timestamp: now, Reporter: "coder-1", Type: "retry_loop", Details: map[string]any{}},
		{Timestamp: now, Reporter: "coder-1", Type: "not_a_type"},
	}

	err := ValidateState(state, pipelineRoot(t), true, nil)
	if err == nil {
		t.Fatal("ValidateState() = nil, want every violation reported")
	}
	assertMentions(t, "ValidateState()", err.Error(),
		"missing done_when: task-a",
		"missing spec_ref: task-a",
		"task-missing",
		"retry_loop anomaly at index 0",
		"unknown anomaly type 'not_a_type' at index 1",
	)
}

// TestValidateCandidate_RefusesOnlyNewViolation (T5a): a record the mutation
// did not break does not veto it; the record it did break does, and the
// refusal names only that one while the old one is reported as a warning.
func TestValidateCandidate_RefusesOnlyNewViolation(t *testing.T) {
	now := time.Now().UTC()
	broken := blockedTask("task-a", now)
	broken.DoneWhen = ""
	baseline := candidateFixture(broken, blockedTask("task-b", now))
	candidate := db.CloneState(baseline)
	candidate.FindTask("task-b").DoneWhen = ""

	var warnings bytes.Buffer
	err := ValidateCandidate(candidate, baselineOf(baseline), pipelineRoot(t), true, &warnings)
	if err == nil {
		t.Fatal("ValidateCandidate() = nil, want the new task-b violation refused")
	}
	assertMentions(t, "refusal", err.Error(), "task-b")
	assertOmits(t, "refusal", err.Error(), "task-a")
	assertMentions(t, "warnings", warnings.String(), "pre-existing", "task-a")
}

// TestValidateCandidate_AllowsUnrelatedMutationOverInvalidRecord is the D84
// shape: an invalid anomaly nobody touches must not block a valid change.
func TestValidateCandidate_AllowsUnrelatedMutationOverInvalidRecord(t *testing.T) {
	now := time.Now().UTC()
	baseline := candidateFixture(blockedTask("task-a", now))
	baseline.Anomalies = []models.Anomaly{testhelpers.LegacyPendingMergeStallAnomaly()}
	candidate := db.CloneState(baseline)
	unassigned := blockedTask("task-b", now)
	unassigned.AssignedTo = nil
	candidate.Tasks = append(candidate.Tasks, unassigned)

	var warnings bytes.Buffer
	if err := ValidateCandidate(candidate, baselineOf(baseline), pipelineRoot(t), true, &warnings); err != nil {
		t.Fatalf("ValidateCandidate() = %v, want the unrelated change allowed", err)
	}
	assertMentions(t, "warnings", warnings.String(), "pre-existing", "retry_loop anomaly at index 0")
}

// TestValidateCandidate_PartialRepairAllowed (T5b, human decision
// 2026-09-30): filling one of two missing fields adds no violation, so it
// passes; the field still missing is warned about, not enforced.
func TestValidateCandidate_PartialRepairAllowed(t *testing.T) {
	now := time.Now().UTC()
	broken := blockedTask("task-a", now)
	broken.DoneWhen = ""
	broken.SpecRef = ""
	baseline := candidateFixture(broken)
	candidate := db.CloneState(baseline)
	candidate.FindTask("task-a").DoneWhen = "it works"

	var warnings bytes.Buffer
	if err := ValidateCandidate(candidate, baselineOf(baseline), pipelineRoot(t), true, &warnings); err != nil {
		t.Fatalf("ValidateCandidate() = %v, want the partial repair allowed", err)
	}
	assertMentions(t, "warnings", warnings.String(), "missing spec_ref: task-a")
	assertOmits(t, "warnings", warnings.String(), "missing done_when")
}

// TestValidateCandidate_NewViolationBehindOldOneRefused (T5b, codex's masking
// example): a record that already misses done_when must still refuse a
// mutation that clears its spec_ref.
func TestValidateCandidate_NewViolationBehindOldOneRefused(t *testing.T) {
	now := time.Now().UTC()
	broken := blockedTask("task-a", now)
	broken.DoneWhen = ""
	baseline := candidateFixture(broken)
	candidate := db.CloneState(baseline)
	candidate.FindTask("task-a").SpecRef = ""

	err := ValidateCandidate(candidate, baselineOf(baseline), pipelineRoot(t), true, nil)
	if err == nil {
		t.Fatal("ValidateCandidate() = nil, want the cleared spec_ref refused")
	}
	assertMentions(t, "refusal", err.Error(), "missing spec_ref: task-a")
	assertOmits(t, "refusal", err.Error(), "missing done_when")
}

// TestValidateCandidate_CrossRecordViolationRefused (T5c): a record with an
// old violation of its own still refuses a change to another record that makes
// its dependency on that record illegal.
func TestValidateCandidate_CrossRecordViolationRefused(t *testing.T) {
	now := time.Now().UTC()
	consumer := blockedTask("task-r", now)
	consumer.DoneWhen = ""
	consumer.DependsOn = []string{"task-d"}
	baseline := candidateFixture(consumer, blockedTask("task-d", now))
	candidate := db.CloneState(baseline)
	abandoned := testhelpers.BuildTaskByStatus("task-d", models.TaskStatusAbandoned, now)
	abandoned.RolePair = consumer.RolePair
	*candidate.FindTask("task-d") = abandoned

	if err := ValidateState(db.CloneState(baseline), pipelineRoot(t), true, nil); err == nil || !strings.Contains(err.Error(), "missing done_when: task-r") {
		t.Fatalf("baseline ValidateState() = %v, want only task-r's own violation", err)
	}
	err := ValidateCandidate(candidate, baselineOf(baseline), pipelineRoot(t), true, nil)
	if err == nil {
		t.Fatal("ValidateCandidate() = nil, want the dependency violation on task-d refused")
	}
	assertMentions(t, "refusal", err.Error(), "non-terminal task task-r depends on terminal non-merged task task-d")
	assertOmits(t, "refusal", err.Error(), "missing done_when")
}

// TestValidateCandidate_RetryLoopPartialRepairAllowed (T11): grouped detail
// requirements are independent violations, so adding one missing detail is a
// repair, not a new violation.
func TestValidateCandidate_RetryLoopPartialRepairAllowed(t *testing.T) {
	now := time.Now().UTC()
	baseline := candidateFixture()
	baseline.Anomalies = []models.Anomaly{{Timestamp: now, Reporter: "coder-1", Type: "retry_loop", Details: map[string]any{}}}
	candidate := db.CloneState(baseline)
	candidate.Anomalies[0].Details["count"] = 3

	var warnings bytes.Buffer
	if err := ValidateCandidate(candidate, baselineOf(baseline), pipelineRoot(t), true, &warnings); err != nil {
		t.Fatalf("ValidateCandidate() = %v, want adding count allowed", err)
	}
	assertMentions(t, "warnings", warnings.String(), "error_pattern")
}

// TestValidateCandidate_RepeatedOccurrenceIsNew (T13): identities are counted,
// not deduplicated, so a second occurrence of an existing violation is new.
func TestValidateCandidate_RepeatedOccurrenceIsNew(t *testing.T) {
	now := time.Now().UTC()
	consumer := blockedTask("task-r", now)
	consumer.DependsOn = []string{"task-d", "task-d"}
	baseline := candidateFixture(consumer, blockedTask("task-d", now))
	candidate := db.CloneState(baseline)
	candidate.FindTask("task-r").DependsOn = []string{"task-d", "task-d", "task-d"}

	err := ValidateCandidate(candidate, baselineOf(baseline), pipelineRoot(t), true, nil)
	if err == nil {
		t.Fatal("ValidateCandidate() = nil, want the extra duplicate entry refused")
	}
	assertMentions(t, "refusal", err.Error(), "duplicate depends_on entry \"task-d\"")
}

// TestValidateCandidate_IdentityStableAcrossStatusChange (T12): a message may
// name the status for the operator, but the identity must not, or a task that
// moves between two statuses with the same requirement would trade its old
// violation for a "new" one and refuse the transition.
func TestValidateCandidate_IdentityStableAcrossStatusChange(t *testing.T) {
	now := time.Now().UTC()
	approved := testhelpers.BuildTaskByStatus("task-a", models.TaskStatusApproved, now)
	approved.HandoffEvents = nil
	baseline := candidateFixture(approved)
	rejected := testhelpers.BuildTaskByStatus("task-a", models.TaskStatusRejected, now)
	rejected.HandoffEvents = nil
	candidate := db.CloneState(baseline)
	*candidate.FindTask("task-a") = rejected

	if err := ValidateState(db.CloneState(baseline), pipelineRoot(t), true, nil); err == nil || strings.Count(err.Error(), "\n") != 0 {
		t.Fatalf("baseline ValidateState() = %v, want only the missing submission event", err)
	}
	var warnings bytes.Buffer
	if err := ValidateCandidate(candidate, baselineOf(baseline), pipelineRoot(t), true, &warnings); err != nil {
		t.Fatalf("ValidateCandidate() = %v, want the unchanged violation treated as pre-existing", err)
	}
	assertMentions(t, "warnings", warnings.String(), "pre-existing",
		"in status "+string(models.TaskStatusRejected)+" has no handoff event with trigger \"submission\"")
}

// TestValidateCandidate_OneClockForBothPasses (T8, codex R2): an agent whose
// lease expires between two clock reads must be judged at one instant in both
// passes; otherwise its unchanged violation looks new and refuses an
// unrelated change.
//
// The instant is historical, so the lease is active only at the injected time
// and long expired at wall time. The pre-existing warning therefore proves the
// injected instant reached the nested agent check in both passes: ignoring the
// clock finds no violation (no warning), and a second read lands after the
// lease so the baseline lacks the violation (refusal, and calls == 2).
func TestValidateCandidate_OneClockForBothPasses(t *testing.T) {
	t0 := time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC)
	lease := t0.Add(time.Second)
	baseline := candidateFixture()
	baseline.Agents["coder-1"] = models.Agent{Role: "coder", Status: models.AgentStatusIdle, PID: 42, LeaseExpires: &lease}
	candidate := db.CloneState(baseline)
	candidate.Tasks = append(candidate.Tasks, blockedTask("task-b", t0))

	calls := 0
	clock := func() time.Time {
		calls++
		return t0.Add(time.Duration(calls-1) * 2 * time.Second)
	}
	var warnings bytes.Buffer
	if err := validateCandidate(candidate, baselineOf(baseline), pipelineRoot(t), true, &warnings, clock); err != nil {
		t.Fatalf("validateCandidate() = %v, want the unchanged provider violation treated as pre-existing", err)
	}
	assertMentions(t, "warnings", warnings.String(), "pre-existing", "agent coder-1 has active lease but no provider")
	if calls != 1 {
		t.Fatalf("clock read %d times, want exactly 1 shared by both passes", calls)
	}
}

// The tests below pin code review round 1 (codex C1-C4, from its probes):
// every independently evaluable constraint, nested helpers included, is its
// own violation, identified by its owner and not by incidental values. Each
// pair shows a new defect refused behind an old one and a partial repair
// allowed.

func requireRefused(t *testing.T, candidate, baseline *models.State, want string) {
	t.Helper()
	err := ValidateCandidate(candidate, baselineOf(baseline), pipelineRoot(t), true, nil)
	if err == nil {
		t.Fatalf("ValidateCandidate() = nil, want %q refused", want)
	}
	assertMentions(t, "refusal", err.Error(), want)
}

func requireAllowed(t *testing.T, candidate, baseline *models.State) {
	t.Helper()
	if err := ValidateCandidate(candidate, baselineOf(baseline), pipelineRoot(t), true, nil); err != nil {
		t.Fatalf("ValidateCandidate() = %v, want the partial repair allowed", err)
	}
}

// C1: validation safety is checked per command.
func TestValidateCandidate_DestructiveCommandsCheckedEach(t *testing.T) {
	marked := models.CurrentDestructiveDBAllowMarker() + " make test"
	task := blockedTask("A", time.Now().UTC())
	task.DestructiveDB = true
	task.Validation = []string{"echo legacy", marked}
	baseline := candidateFixture(task)

	unmarked := db.CloneState(baseline)
	unmarked.FindTask("A").Validation[1] = "make test"
	requireRefused(t, unmarked, baseline, "validation[1] destructive_db requires")

	repaired := db.CloneState(unmarked)
	repaired.FindTask("A").Validation[0] = marked
	requireAllowed(t, repaired, unmarked)
}

// C1: validation prerequisites are checked per entry.
func TestValidateCandidate_PrerequisitesCheckedEach(t *testing.T) {
	task := blockedTask("A", time.Now().UTC())
	task.Validation = []string{"check"}
	task.ValidationPrerequisites = []models.ValidationPrerequisite{{Command: "check", Env: []string{"bad-name", "GOOD"}}}
	baseline := candidateFixture(task)

	worse := db.CloneState(baseline)
	worse.FindTask("A").ValidationPrerequisites[0].Env[1] = "also-bad"
	requireRefused(t, worse, baseline, "validation_prerequisites[0].env[1]")

	repaired := db.CloneState(worse)
	repaired.FindTask("A").ValidationPrerequisites[0].Env[0] = "GOOD_NAME"
	requireAllowed(t, repaired, worse)
}

// C1: a clean closure's key and source commit are compared separately.
func TestValidateCandidate_ClosureFieldsCheckedEach(t *testing.T) {
	baseline := validIntegrationState(t)
	baseline.Goal.Integration.Closure.AnalysisKey = "wrong"

	worse := db.CloneState(baseline)
	worse.Goal.Integration.Closure.SourceCommit = "wrong-source"
	requireRefused(t, worse, baseline, "clean integration closure does not match generation 2 (source commit)")

	repaired := db.CloneState(worse)
	repaired.Goal.Integration.Closure.AnalysisKey = baseline.Goal.Integration.GlobalGenerations[1].AnalysisKey
	requireAllowed(t, repaired, worse)
}

// C2: the same defect on another attestation of the same plan is new.
func TestValidateCandidate_AttestationDefectOwnedByItsAttestation(t *testing.T) {
	baseline := validIntegrationState(t)
	baseline.Goal.Integration.Coverage[0].ApprovalAttestations[0].Approver = ""

	moved := db.CloneState(baseline)
	moved.Goal.Integration.Coverage[0].ApprovalAttestations[0].Approver = "code-reviewer-1"
	moved.Goal.Integration.Coverage[0].ApprovalAttestations[1].Approver = ""
	requireRefused(t, moved, baseline, "approval attestation approver is empty")
}

// C1: doer ownership reports every defect of the owner row.
func TestValidateCandidate_DoerOwnershipDefectsCheckedEach(t *testing.T) {
	now := time.Now().UTC()
	task := testhelpers.BuildTaskByStatus("A", models.TaskStatusImplementing, now)
	task.Worktree = testhelpers.StringPtr(".")
	expired := now.Add(-time.Hour)
	baseline := candidateFixture(task)
	baseline.Agents["coder-1"] = models.Agent{Role: "coder", Status: models.AgentStatusIdle, LeaseExpires: &expired}

	repaired := db.CloneState(baseline)
	agent := repaired.Agents["coder-1"]
	agent.Provider = "claude"
	repaired.Agents["coder-1"] = agent
	requireAllowed(t, repaired, baseline)

	withPID := db.CloneState(baseline)
	agent = withPID.Agents["coder-1"]
	agent.PID = 42
	withPID.Agents["coder-1"] = agent
	cleared := db.CloneState(withPID)
	agent = cleared.Agents["coder-1"]
	agent.PID = 0
	cleared.Agents["coder-1"] = agent
	requireRefused(t, cleared, withPID, "has agent without pid")
}

// C4: a duplicate assignment is one violation per conflicting pair.
func TestValidateCandidate_DuplicateAssignmentsByPair(t *testing.T) {
	now := time.Now().UTC()
	baseline := candidateFixture()
	for _, id := range []string{"A", "B", "C"} {
		task := testhelpers.BuildTaskByStatus(id, models.TaskStatusImplementing, now)
		task.Worktree = testhelpers.StringPtr(".")
		baseline.Tasks = append(baseline.Tasks, task)
	}

	released := db.CloneState(baseline)
	released.Tasks = released.Tasks[:2]
	requireAllowed(t, released, baseline)

	added := db.CloneState(baseline)
	extra := testhelpers.BuildTaskByStatus("D", models.TaskStatusImplementing, now)
	extra.Worktree = testhelpers.StringPtr(".")
	added.Tasks = append(added.Tasks, extra)
	requireRefused(t, added, baseline, "assigned to multiple active tasks simultaneously: [A D]")
}

func TestValidateCandidate_DormantAssignmentsByPair(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	baseline := candidateFixture()
	for _, id := range []string{"A", "B", "C"} {
		baseline.Tasks = append(baseline.Tasks, testhelpers.BuildTaskByStatus(id, models.TaskStatusRejected, now))
	}
	partial := db.CloneState(baseline)
	partial.FindTask("A").AssignedTo = nil
	partial.FindTask("A").LeaseExpires = nil
	partial.FindTask("A").Worktree = nil
	requireAllowed(t, partial, baseline)

	reordered := db.CloneState(baseline)
	reordered.Tasks[0], reordered.Tasks[2] = reordered.Tasks[2], reordered.Tasks[0]
	// Changing a dormant phase does not create new conflicting pairs.
	reordered.FindTask("A").Status = models.TaskStatusApproved
	requireAllowed(t, reordered, baseline)

	added := db.CloneState(baseline)
	added.Tasks = append(added.Tasks, testhelpers.BuildTaskByStatus("D", models.TaskStatusRejected, now))
	requireRefused(t, added, baseline, "assigned to multiple active tasks simultaneously: [A D]")

	// A repaired old pair cannot pay for a newly introduced different pair.
	partial.Tasks = append(partial.Tasks, testhelpers.BuildTaskByStatus("D", models.TaskStatusRejected, now))
	requireRefused(t, partial, baseline, "assigned to multiple active tasks simultaneously: [B D]")
}

// C3: a new cycle through tasks already on other cycles is refused; breaking
// an existing cycle is not.
func TestValidateCandidate_CycleThroughCyclicTasksRefused(t *testing.T) {
	now := time.Now().UTC()
	baseline := candidateFixture(blockedTask("A", now), blockedTask("B", now), blockedTask("C", now), blockedTask("D", now))
	baseline.FindTask("A").DependsOn = []string{"B"}
	baseline.FindTask("B").DependsOn = []string{"A"}
	baseline.FindTask("C").DependsOn = []string{"D"}
	baseline.FindTask("D").DependsOn = []string{"C"}

	joined := db.CloneState(baseline)
	joined.FindTask("B").DependsOn = []string{"A", "C"}
	joined.FindTask("D").DependsOn = []string{"C", "A"}
	requireRefused(t, joined, baseline, "circular dependency detected: B eventually depends on itself")

	broken := db.CloneState(baseline)
	broken.FindTask("D").DependsOn = nil
	requireAllowed(t, broken, baseline)
}

// Code review round 2 (codex, from its probes).

func repeated(value string, n int) []string {
	values := make([]string, n)
	for i := range values {
		values[i] = value
	}
	return values
}

// C1: each prerequisite list has its own bound.
func TestValidateCandidate_PrerequisiteListBoundsCheckedEach(t *testing.T) {
	task := blockedTask("A", time.Now().UTC())
	task.Validation = []string{"check"}
	task.ValidationPrerequisites = []models.ValidationPrerequisite{{Command: "check", Env: repeated("GOOD", 65), Executables: []string{"tool"}}}
	baseline := candidateFixture(task)

	worse := db.CloneState(baseline)
	worse.FindTask("A").ValidationPrerequisites[0].Executables = repeated("tool", 65)
	requireRefused(t, worse, baseline, "exceeds the 64 checks per list limit (executables)")

	repaired := db.CloneState(worse)
	repaired.FindTask("A").ValidationPrerequisites[0].Env = repeated("GOOD", 64)
	requireAllowed(t, repaired, worse)
}

// C1 (round 3): the command ceiling and the one-declaration-per-command
// count are separate constraints.
func TestValidateCandidate_PrerequisiteCommandCeilingCheckedApart(t *testing.T) {
	commands := make([]string, 64)
	for i := range commands {
		commands[i] = fmt.Sprintf("check-%d", i)
	}
	task := blockedTask("A", time.Now().UTC())
	task.Validation = commands
	task.ValidationPrerequisites = []models.ValidationPrerequisite{{Command: commands[0], Executables: []string{"tool"}}}
	baseline := candidateFixture(task)

	over := db.CloneState(baseline)
	over.FindTask("A").Validation = append(slices.Clone(commands), "check-64")
	requireRefused(t, over, baseline, "validation has more than 64 commands")

	repaired := db.CloneState(over)
	repaired.FindTask("A").Validation = []string{commands[0]}
	requireAllowed(t, repaired, over)
}

// C1: a fingerprint's uniqueness is judged apart from its shape.
func TestValidateCandidate_FingerprintDuplicateBehindMalformedRefused(t *testing.T) {
	baseline := candidateFixture(blockedTask("task-1", time.Now().UTC()))
	finding := quarantinedVerdictState().QuarantinedVerdicts[0]
	finding.GenerationFingerprints = []string{"x", "y"}
	baseline.QuarantinedVerdicts = []models.QuarantinedVerdict{finding}

	duplicated := db.CloneState(baseline)
	duplicated.QuarantinedVerdicts[0].GenerationFingerprints[1] = "x"
	requireRefused(t, duplicated, baseline, "(fingerprint 1 duplicate)")
}

// C2: a finding's defects are owned by the finding, not by its index, so
// pruning an earlier finding does not make them new.
func TestValidateCandidate_QuarantineDefectSurvivesPruning(t *testing.T) {
	baseline := candidateFixture(blockedTask("task-1", time.Now().UTC()))
	finding := quarantinedVerdictState().QuarantinedVerdicts[0]
	second := finding
	second.ID = "finding-2"
	second.ReviewerID = ""
	baseline.QuarantinedVerdicts = []models.QuarantinedVerdict{finding, second}

	pruned := db.CloneState(baseline)
	pruned.QuarantinedVerdicts = pruned.QuarantinedVerdicts[1:]
	requireAllowed(t, pruned, baseline)

	moved := db.CloneState(baseline)
	moved.QuarantinedVerdicts[0].ReviewerID = ""
	moved.QuarantinedVerdicts[1].ReviewerID = "code-reviewer-1"
	requireRefused(t, moved, baseline, "quarantined_verdicts[0] requires a reviewer")
}

// C3 (round 2): the cycle graph follows a duplicated ID to its first task,
// as every operation does.
func TestValidateCandidate_CycleThroughDuplicatedIDRefused(t *testing.T) {
	now := time.Now().UTC()
	baseline := candidateFixture(blockedTask("A", now), blockedTask("A", now), blockedTask("B", now))
	baseline.FindTask("B").DependsOn = []string{"A"}

	cyclic := db.CloneState(baseline)
	cyclic.FindTask("A").DependsOn = []string{"B"}
	requireRefused(t, cyclic, baseline, "circular dependency detected: A eventually depends on itself")
}
