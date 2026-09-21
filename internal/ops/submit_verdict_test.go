package ops

import (
	"bytes"
	stderrors "errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/git"
	activitylog "github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/payloadschema"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/statehygiene"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestSubmitVerdict_Validation(t *testing.T) {
	tests := []struct {
		name        string
		taskID      string
		verdict     string
		reason      string
		agentID     string
		errContains string
	}{
		{
			name: "empty task ID", verdict: "APPROVED", agentID: "r1",
			errContains: "task ID is required",
		},
		{
			name: "empty verdict", taskID: "t1", agentID: "r1",
			errContains: "verdict is required",
		},
		{
			name: "empty agent ID", taskID: "t1", verdict: "APPROVED",
			errContains: brand.EnvName("AGENT_ID") + " is required",
		},
		{
			name: "invalid verdict", taskID: "t1", verdict: "MAYBE", agentID: "r1",
			errContains: "must be APPROVED or REJECTED",
		},
		{
			name: "rejection without reason", taskID: "t1", verdict: "REJECTED", agentID: "r1",
			errContains: "rejection reason is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := SubmitVerdict("/nonexistent", tt.taskID, tt.verdict, tt.reason, tt.agentID, "")
			testhelpers.RequireErrorContains(t, err, tt.errContains)
		})
	}
}

// TestSubmitVerdict_LowercaseVerdictRejected pins the boundary that replaced
// case normalization: the canonical object is validated as the caller sent it,
// so a lowercase verdict is the structural rejection validate-payload already
// reports rather than a value the mutation boundary silently upcases.
func TestSubmitVerdict_LowercaseVerdictRejected(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now),
	}
	state.Agents["code-reviewer-1"] = models.Agent{
		Role:   "code-reviewer",
		Status: models.AgentStatusWorking,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := SubmitVerdict(tmpDir, "task-1", "approved", "", "code-reviewer-1", "")
	requireVerdictDiagnostic(t, err, "/verdict", "must be APPROVED or REJECTED", models.FieldValueClassUnknownEnum)

	readState, readErr := db.New(stateFile).Read()
	if readErr != nil {
		t.Fatalf("Read() error = %v", readErr)
	}
	if status := taskStatus(readState.FindTask("task-1")); status != models.TaskStatusReviewing {
		t.Errorf("Status = %v, want REVIEWING", status)
	}
}

func TestSubmitVerdict_Approved(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now),
	}
	state.Agents["code-reviewer-1"] = models.Agent{
		Role:   "code-reviewer",
		Status: models.AgentStatusWorking,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := SubmitVerdict(tmpDir, "task-1", "APPROVED", "", "code-reviewer-1", "")
	if err != nil {
		t.Fatalf("SubmitVerdict() error: %v", err)
	}

	if result.TaskID != "task-1" {
		t.Errorf("TaskID = %q, want %q", result.TaskID, "task-1")
	}
	if result.Verdict != "APPROVED" {
		t.Errorf("Verdict = %q, want %q", result.Verdict, "APPROVED")
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
	if task.Status != models.TaskStatusApproved {
		t.Errorf("Status = %v, want APPROVED", task.Status)
	}
	if task.ApprovedBy == nil || *task.ApprovedBy != "code-reviewer-1" {
		t.Error("ApprovedBy should be code-reviewer-1")
	}
	if task.RejectionReason != nil {
		t.Error("RejectionReason should be nil after approval")
	}
	if task.ReviewingBy != nil {
		t.Error("ReviewingBy should be cleared")
	}
	if task.ReviewLeaseExpires != nil {
		t.Error("ReviewLeaseExpires should be cleared")
	}

	lastHistory := task.History[len(task.History)-1]
	if lastHistory.Event != models.TaskEventApproved {
		t.Errorf("History event = %q, want %q", lastHistory.Event, models.TaskEventApproved)
	}
	if task.ReviewCommit == nil {
		t.Fatal("ReviewCommit is nil")
	}
	if lastHistory.Commit == nil || *lastHistory.Commit != *task.ReviewCommit {
		t.Fatalf("History commit = %v, want %s", lastHistory.Commit, *task.ReviewCommit)
	}
}

func TestSubmitVerdict_ApprovedClearsStaleIntegrationFailure(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now)
	staleMergeCommit := "stale-merge"
	task.MergeCommit = &staleMergeCommit
	task.IntegrationFailure = map[string]any{
		"operation": "wt-merge",
		"reason":    "merge conflict",
	}
	state.Tasks = []models.Task{task}
	state.Agents["code-reviewer-1"] = models.Agent{
		Role:   "code-reviewer",
		Status: models.AgentStatusWorking,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	if _, err := SubmitVerdict(tmpDir, "task-1", "APPROVED", "", "code-reviewer-1", ""); err != nil {
		t.Fatalf("SubmitVerdict() error: %v", err)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	readTask := readState.FindTask("task-1")
	if readTask == nil {
		t.Fatal("Task not found")
	}
	if readTask.Status != models.TaskStatusApproved {
		t.Fatalf("Status = %s, want %s", readTask.Status, models.TaskStatusApproved)
	}
	if readTask.ReviewCommit == nil {
		t.Fatal("ReviewCommit was cleared")
	}
	if readTask.MergeCommit != nil {
		t.Fatalf("MergeCommit = %v, want nil stale merge metadata", *readTask.MergeCommit)
	}
	if readTask.IntegrationFailure != nil {
		t.Fatalf("IntegrationFailure = %v, want nil stale failure metadata", readTask.IntegrationFailure)
	}
}

func TestSubmitVerdict_Rejected(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	taskWithStaleAttempt := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now)
	approvedBy := "code-reviewer-0"
	mergeCommit := "merge-stale"
	taskWithStaleAttempt.ApprovedBy = &approvedBy
	taskWithStaleAttempt.Approvals = []models.Approval{{
		Agent:     approvedBy,
		Provider:  "codex",
		Timestamp: now,
	}}
	taskWithStaleAttempt.MergeCommit = &mergeCommit
	taskWithStaleAttempt.IntegrationFailure = map[string]any{"detail": "stale"}
	taskWithStaleAttempt.Output = []models.OutputEntry{
		{
			Desc:     "plan child",
			DoneWhen: "child complete",
			Scope:    "internal/ops",
			SpecRef:  "README.md",
			PlanRef:  "specs/plans/stale.md",
		},
	}
	state.Tasks = []models.Task{taskWithStaleAttempt}
	state.Agents["code-reviewer-1"] = models.Agent{
		Role:   "code-reviewer",
		Status: models.AgentStatusWorking,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := SubmitVerdict(tmpDir, "task-1", "REJECTED", "Missing error handling", "code-reviewer-1", "")
	if err != nil {
		t.Fatalf("SubmitVerdict() error: %v", err)
	}

	if result.Verdict != "REJECTED" {
		t.Errorf("Verdict = %q, want %q", result.Verdict, "REJECTED")
	}
	if result.Reason != "Missing error handling" {
		t.Errorf("Reason = %q, want %q", result.Reason, "Missing error handling")
	}
	if result.EscalatedToBlocked {
		t.Error("EscalatedToBlocked = true, want false for normal rejection")
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
	if task.Status != models.TaskStatusRejected {
		t.Errorf("Status = %v, want REJECTED", task.Status)
	}
	if task.RejectionReason == nil || *task.RejectionReason != "Missing error handling" {
		t.Error("RejectionReason not set correctly")
	}
	if task.ReviewCyclesCurrent != 1 {
		t.Errorf("ReviewCyclesCurrent = %d, want 1", task.ReviewCyclesCurrent)
	}
	if task.ReviewCyclesTotal != 1 {
		t.Errorf("ReviewCyclesTotal = %d, want 1", task.ReviewCyclesTotal)
	}

	lastHistory := task.History[len(task.History)-1]
	if lastHistory.Event != models.TaskEventRejected {
		t.Errorf("History event = %q, want %q", lastHistory.Event, models.TaskEventRejected)
	}
	if task.ReviewCommit != nil {
		t.Fatalf("ReviewCommit = %v, want nil after rejection", *task.ReviewCommit)
	}
	if lastHistory.Commit == nil || *lastHistory.Commit != "review123" {
		t.Fatalf("History commit = %v, want review123", lastHistory.Commit)
	}
	if task.ApprovedBy != nil {
		t.Fatalf("ApprovedBy = %v, want nil after rejection", *task.ApprovedBy)
	}
	if len(task.Approvals) != 0 {
		t.Fatalf("Approvals = %v, want cleared after rejection", task.Approvals)
	}
	if task.MergeCommit != nil {
		t.Fatalf("MergeCommit = %v, want nil after rejection", *task.MergeCommit)
	}
	if task.IntegrationFailure != nil {
		t.Fatalf("IntegrationFailure = %v, want nil after rejection", task.IntegrationFailure)
	}
	if len(task.Output) != 1 || task.Output[0].PlanRef != "specs/plans/stale.md" {
		t.Fatalf("Output = %v, want preserved as rework context", task.Output)
	}
}

func TestSubmitVerdict_RejectionReasonByteLimit(t *testing.T) {
	setup := func(t *testing.T) (string, string) {
		t.Helper()
		projectRoot := t.TempDir()
		stateFile, _ := testhelpers.SetupLizaDir(t, projectRoot)
		now := time.Now().UTC()
		state := testhelpers.CreateValidState()
		state.Tasks = []models.Task{
			testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now),
		}
		state.Agents["code-reviewer-1"] = models.Agent{
			Role:   "code-reviewer",
			Status: models.AgentStatusWorking,
		}
		testhelpers.WriteInitialState(t, stateFile, state)
		return projectRoot, stateFile
	}

	t.Run("maximum accepted", func(t *testing.T) {
		projectRoot, stateFile := setup(t)
		reason := strings.Repeat("x", statehygiene.MaxStateTextBytes)

		if _, err := SubmitVerdict(projectRoot, "task-1", "REJECTED", reason, "code-reviewer-1", ""); err != nil {
			t.Fatalf("SubmitVerdict() error: %v", err)
		}

		state, err := db.New(stateFile).Read()
		if err != nil {
			t.Fatalf("Read() error: %v", err)
		}
		task := state.FindTask("task-1")
		if task == nil || task.RejectionReason == nil || *task.RejectionReason != reason {
			t.Fatal("4096-byte rejection reason was not persisted")
		}
	})

	t.Run("oversized rejected before side effects", func(t *testing.T) {
		projectRoot, stateFile := setup(t)
		before, err := os.ReadFile(stateFile)
		if err != nil {
			t.Fatalf("ReadFile() before SubmitVerdict: %v", err)
		}
		reason := strings.Repeat("x", statehygiene.MaxStateTextBytes+1)

		_, err = SubmitVerdict(projectRoot, "task-1", "REJECTED", reason, "code-reviewer-1", "")
		var precondition *PreconditionError
		if !stderrors.As(err, &precondition) {
			t.Fatalf("SubmitVerdict() error = %T %v, want *PreconditionError", err, err)
		}
		var lifecycleErr *LifecycleError
		if !stderrors.As(err, &lifecycleErr) || lifecycleErr.Outcome.Outcome != models.LifecycleInvalidInput ||
			lifecycleErr.Outcome.SafeAction != "correct_input" || lifecycleErr.Outcome.Effects != "none" {
			t.Fatalf("oversized verdict must remain a pre-effect hard failure: %v", err)
		}
		for _, part := range []string{"4097 bytes", "4096-byte maximum", paths.ProjectDirName() + "/agent-outputs/", "bounded summary", "artifact reference"} {
			if !strings.Contains(precondition.Reason, part) {
				t.Errorf("PreconditionError.Reason = %q, want substring %q", precondition.Reason, part)
			}
		}

		after, readErr := os.ReadFile(stateFile)
		if readErr != nil {
			t.Fatalf("ReadFile() after SubmitVerdict: %v", readErr)
		}
		if !bytes.Equal(after, before) {
			t.Fatal("oversized rejection changed state")
		}
		if _, statErr := os.Stat(filepath.Join(projectRoot, paths.ProjectDirName(), "log.yaml")); !os.IsNotExist(statErr) {
			t.Fatalf("oversized rejection created activity log: %v", statErr)
		}
	})
}

func TestSubmitVerdict_RejectionThenResubmissionUsesFreshReviewMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now)
	oldMergeCommit := "old-merge"
	task.MergeCommit = &oldMergeCommit
	task.IntegrationFailure = map[string]any{"detail": "old failure"}
	state.Tasks = []models.Task{task}
	state.Agents["code-reviewer-1"] = models.Agent{
		Role:   "code-reviewer",
		Status: models.AgentStatusWorking,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	if _, err := SubmitVerdict(tmpDir, "task-1", "REJECTED", "needs changes", "code-reviewer-1", ""); err != nil {
		t.Fatalf("SubmitVerdict(REJECTED) error: %v", err)
	}

	bb := db.New(stateFile)
	newReviewCommit := "fresh-review"
	reviewLease := now.Add(30 * time.Minute)
	if err := bb.Modify(func(state *models.State) error {
		task := state.FindTask("task-1")
		if task == nil {
			t.Fatal("task not found")
		}
		task.Status = models.TaskStatusReviewing
		task.ReviewCommit = &newReviewCommit
		task.ReviewingBy = testhelpers.StringPtr("code-reviewer-1")
		task.ReviewLeaseExpires = &reviewLease
		state.Agents["code-reviewer-1"] = models.Agent{
			Role:        "code-reviewer",
			Status:      models.AgentStatusReviewing,
			CurrentTask: testhelpers.StringPtr("task-1"),
		}
		return nil
	}); err != nil {
		t.Fatalf("resubmit test setup error: %v", err)
	}

	if _, err := SubmitVerdict(tmpDir, "task-1", "APPROVED", "", "code-reviewer-1", ""); err != nil {
		t.Fatalf("SubmitVerdict(APPROVED) error: %v", err)
	}

	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("read state error: %v", err)
	}
	task = *readState.FindTask("task-1")
	if task.Status != models.TaskStatusApproved {
		t.Fatalf("Status = %s, want %s", task.Status, models.TaskStatusApproved)
	}
	if task.ReviewCommit == nil || *task.ReviewCommit != newReviewCommit {
		t.Fatalf("ReviewCommit = %v, want %s", task.ReviewCommit, newReviewCommit)
	}
	lastHistory := task.History[len(task.History)-1]
	if lastHistory.Event != models.TaskEventApproved || lastHistory.Commit == nil || *lastHistory.Commit != newReviewCommit {
		t.Fatalf("last history = %+v, want approval for fresh review commit", lastHistory)
	}
	if task.MergeCommit != nil {
		t.Fatalf("MergeCommit = %v, want nil old merge metadata", *task.MergeCommit)
	}
	if task.IntegrationFailure != nil {
		t.Fatalf("IntegrationFailure = %v, want nil old failure metadata", task.IntegrationFailure)
	}
}

func TestSubmitVerdict_TaskNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := SubmitVerdict(tmpDir, "nonexistent", "APPROVED", "", "code-reviewer-1", "")
	if err == nil {
		t.Fatal("Expected error for nonexistent task")
	}
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFoundError, got %T: %v", err, err)
	}
}

func TestSubmitVerdict_WrongStatus(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
	}
	taskRef := "task-1"
	state.Agents["code-reviewer-1"] = models.Agent{
		Role:        models.RoleCodeReviewer,
		Status:      models.AgentStatusWaiting,
		CurrentTask: &taskRef,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := SubmitVerdict(tmpDir, "task-1", "REJECTED", "late finding", "code-reviewer-1", "")
	testhelpers.RequireErrorContains(t, err, "not in a reviewing state")

	bb := db.New(stateFile)
	readState, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("read state error: %v", readErr)
	}
	if len(readState.Anomalies) != 0 {
		t.Fatal("late verdict must not mutate anomaly history")
	}
	var late *LifecycleError
	if !stderrors.As(err, &late) || late.Outcome.Outcome != models.LifecycleAlreadyTransitioned || late.Outcome.SafeAction != "stop" {
		t.Fatalf("late verdict recovery = %v", err)
	}
	agent := readState.Agents["code-reviewer-1"]
	if agent.CurrentTask == nil || *agent.CurrentTask != taskRef {
		t.Fatal("late verdict changed reviewer ownership")
	}
}

func TestSubmitVerdict_RequeriesWhenTaskLeavesReviewBeforeModify(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now),
	}
	taskRef := "task-1"
	state.Agents["code-reviewer-1"] = models.Agent{
		Role:        models.RoleCodeReviewer,
		Status:      models.AgentStatusWorking,
		CurrentTask: &taskRef,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	bb := db.New(stateFile)
	testSubmitVerdictHooks = &submitVerdictTestHooks{
		beforeModify: func() {
			if err := bb.Modify(func(state *models.State) error {
				task := state.FindTask("task-1")
				if task == nil {
					t.Fatal("task not found")
				}
				task.Status = models.TaskStatusImplementing
				task.ReviewingBy = nil
				task.ReviewLeaseExpires = nil
				return nil
			}); err != nil {
				t.Fatalf("hook modify state: %v", err)
			}
		},
	}
	t.Cleanup(func() { testSubmitVerdictHooks = nil })

	_, err := SubmitVerdict(tmpDir, "task-1", "REJECTED", "late finding", "code-reviewer-1", "")
	var changed *LifecycleError
	if !stderrors.As(err, &changed) || changed.Outcome.Outcome != models.LifecycleStateChanged || changed.Outcome.SafeAction != "requery" {
		t.Fatalf("raced verdict recovery = %v", err)
	}

	readState, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("read state error: %v", readErr)
	}
	if len(readState.Anomalies) != 0 {
		t.Fatal("raced verdict mutated anomaly history")
	}
	agent := readState.Agents["code-reviewer-1"]
	if agent.CurrentTask == nil || *agent.CurrentTask != taskRef {
		t.Fatal("raced verdict changed reviewer ownership")
	}
}

func TestRecordStaleVerdictAnomaly_SkipsReviewingTask(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now),
	}
	taskRef := "task-1"
	state.Agents["code-reviewer-1"] = models.Agent{
		Role:        models.RoleCodeReviewer,
		Status:      models.AgentStatusReviewing,
		CurrentTask: &taskRef,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	bb := db.New(stateFile)
	err := recordStaleVerdictAnomaly(
		bb,
		"task-1",
		"code-reviewer-1",
		nil,
		"REJECTED",
		"late finding",
		"",
		models.TaskStatusReviewing,
		models.TaskStatusReviewingCode2,
	)
	if err != nil {
		t.Fatalf("recordStaleVerdictAnomaly() error: %v", err)
	}

	readState, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("read state error: %v", readErr)
	}
	if len(readState.Anomalies) != 0 {
		t.Fatalf("anomaly count = %d, want 0", len(readState.Anomalies))
	}
	agent := readState.Agents["code-reviewer-1"]
	if agent.CurrentTask == nil || *agent.CurrentTask != "task-1" {
		t.Fatalf("reviewer CurrentTask = %v, want task-1 preserved", agent.CurrentTask)
	}
}

func TestSubmitVerdict_AgentReleased(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now),
	}
	taskRef := "task-1"
	state.Agents["code-reviewer-1"] = models.Agent{
		Role:        "code-reviewer",
		Status:      models.AgentStatusWorking,
		CurrentTask: &taskRef,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := SubmitVerdict(tmpDir, "task-1", "APPROVED", "", "code-reviewer-1", "")
	if err != nil {
		t.Fatalf("SubmitVerdict() error: %v", err)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	agent := readState.Agents["code-reviewer-1"]
	if agent.Status != models.AgentStatusIdle {
		t.Errorf("Agent status = %v, want idle", agent.Status)
	}
	if agent.CurrentTask != nil {
		t.Error("Agent CurrentTask should be nil after verdict")
	}
}

func TestSubmitVerdict_RejectedLimitEscalationTransitionsToBlocked(t *testing.T) {
	tests := []struct {
		name               string
		rejectionReason    string
		configureStateTask func(*models.State, *models.Task)
		wantReasonContains string
		wantQuestionHint   string
		wantReviewCurrent  int
		wantReviewTotal    int
	}{
		{
			name:            "review cycle limit",
			rejectionReason: "Still failing",
			configureStateTask: func(state *models.State, task *models.Task) {
				state.Config.MaxReviewCycles = 2
				task.ReviewCyclesCurrent = 1
				task.ReviewCyclesTotal = 1
				task.Attempt = 2
			},
			wantReasonContains: "review budget exhausted",
			wantQuestionHint:   "review cycle",
			wantReviewCurrent:  2,
			wantReviewTotal:    2,
		},
		{
			name:            "task iteration limit",
			rejectionReason: "Needs redesign",
			configureStateTask: func(state *models.State, task *models.Task) {
				state.Config.MaxReviewCycles = 5
				state.Config.MaxCoderIterations = 10
				task.Iteration = 2
				task.MaxIterations = 2
				task.Attempt = 2
			},
			wantReasonContains: "max iterations",
			wantQuestionHint:   "max iterations were exhausted",
			wantReviewCurrent:  1,
			wantReviewTotal:    1,
		},
		{
			name:            "combined limits",
			rejectionReason: "Needs rescope",
			configureStateTask: func(state *models.State, task *models.Task) {
				state.Config.MaxReviewCycles = 2
				state.Config.MaxCoderIterations = 10
				task.ReviewCyclesCurrent = 1
				task.ReviewCyclesTotal = 4
				task.Iteration = 2
				task.MaxIterations = 2
				task.Attempt = 2
			},
			wantReasonContains: "review budget and iteration limits exhausted",
			wantQuestionHint:   "both review cycles and iterations",
			wantReviewCurrent:  2,
			wantReviewTotal:    5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

			now := time.Now().UTC()
			state := testhelpers.CreateValidState()
			task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now)
			tt.configureStateTask(state, &task)
			state.Tasks = []models.Task{task}

			taskRef := "task-1"
			state.Agents["coder-1"] = models.Agent{
				Role:        "coder",
				Status:      models.AgentStatusWaiting,
				CurrentTask: &taskRef,
			}
			state.Agents["code-reviewer-1"] = models.Agent{
				Role:        "code-reviewer",
				Status:      models.AgentStatusReviewing,
				CurrentTask: &taskRef,
			}

			testhelpers.WriteInitialState(t, stateFile, state)

			result, err := SubmitVerdict(tmpDir, "task-1", "REJECTED", tt.rejectionReason, "code-reviewer-1", "")
			if err != nil {
				t.Fatalf("SubmitVerdict() error: %v", err)
			}
			if !result.EscalatedToBlocked {
				t.Error("EscalatedToBlocked = false, want true")
			}
			if !strings.Contains(result.BlockedReason, tt.wantReasonContains) {
				t.Errorf("BlockedReason = %q, want to contain %q", result.BlockedReason, tt.wantReasonContains)
			}

			bb := db.New(stateFile)
			readState, err := bb.Read()
			if err != nil {
				t.Fatalf("Failed to read state: %v", err)
			}

			blockedTask := readState.FindTask("task-1")
			if blockedTask == nil {
				t.Fatal("Task not found")
			}
			if blockedTask.Status != models.TaskStatusBlocked {
				t.Errorf("Status = %v, want BLOCKED", blockedTask.Status)
			}
			if blockedTask.BlockedReason == nil || !strings.Contains(*blockedTask.BlockedReason, tt.wantReasonContains) {
				t.Errorf("BlockedReason = %v, want to contain %q", blockedTask.BlockedReason, tt.wantReasonContains)
			}
			if len(blockedTask.BlockedQuestions) == 0 || !strings.Contains(blockedTask.BlockedQuestions[0], tt.wantQuestionHint) {
				t.Errorf("BlockedQuestions = %v, want first question to contain %q", blockedTask.BlockedQuestions, tt.wantQuestionHint)
			}
			if blockedTask.ReviewCyclesCurrent != tt.wantReviewCurrent {
				t.Errorf("ReviewCyclesCurrent = %d, want %d", blockedTask.ReviewCyclesCurrent, tt.wantReviewCurrent)
			}
			if blockedTask.ReviewCyclesTotal != tt.wantReviewTotal {
				t.Errorf("ReviewCyclesTotal = %d, want %d", blockedTask.ReviewCyclesTotal, tt.wantReviewTotal)
			}
			if blockedTask.AssignedTo != nil {
				t.Error("AssignedTo should be cleared after escalation")
			}
			if blockedTask.ReviewingBy != nil || blockedTask.ReviewLeaseExpires != nil {
				t.Error("Review lease fields should be cleared")
			}

			assertReleasedAgent(t, readState, "coder-1")
			assertReleasedAgent(t, readState, "code-reviewer-1")
		})
	}
}

func TestSubmitVerdict_MissingReviewCommit(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now)
	task.ReviewCommit = nil // Corrupt: REVIEWING without review_commit
	state.Tasks = []models.Task{task}
	state.Agents["code-reviewer-1"] = models.Agent{
		Role:   "code-reviewer",
		Status: models.AgentStatusWorking,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := SubmitVerdict(tmpDir, "task-1", "APPROVED", "", "code-reviewer-1", "")
	if err == nil {
		t.Fatal("Expected error for missing review_commit, got nil")
	}
	if !strings.Contains(err.Error(), "no review_commit") {
		t.Errorf("Error = %q, want to contain 'no review_commit'", err.Error())
	}
}

func TestSubmitVerdict_ReviewCommitMismatch(t *testing.T) {
	tmpDir := t.TempDir()

	// Setup git repo + liza dir
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	// Create worktree
	g := git.New(tmpDir)
	_, err := g.CreateWorktree("task-1", "integration")
	if err != nil {
		t.Fatalf("Failed to create worktree: %v", err)
	}
	wtPath := g.GetWorktreePath("task-1")

	// Make a commit in the worktree so HEAD diverges from integration
	implFile := filepath.Join(wtPath, "feature.go")
	if err := os.WriteFile(implFile, []byte("package feature\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, wtPath, "add", "feature.go")
	testhelpers.MustGit(t, wtPath, "commit", "-m", "Add feature")

	// Record a stale ReviewCommit (integration HEAD, not worktree HEAD)
	staleCommit := testhelpers.MustGit(t, tmpDir, "rev-parse", "integration")

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now)
	task.ReviewCommit = &staleCommit
	worktreeRel := g.GetWorktreeRelPath("task-1")
	task.Worktree = &worktreeRel
	state.Tasks = []models.Task{task}
	state.Agents["code-reviewer-1"] = models.Agent{
		Role:   "code-reviewer",
		Status: models.AgentStatusWorking,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err = SubmitVerdict(tmpDir, "task-1", "APPROVED", "", "code-reviewer-1", "")
	if err == nil {
		t.Fatal("Expected error for ReviewCommit vs worktree HEAD mismatch")
	}
	if !strings.Contains(err.Error(), "does not match worktree HEAD") {
		t.Fatalf("Expected mismatch error, got: %v", err)
	}

	// Verify task state unchanged
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	readTask := readState.FindTask("task-1")
	if readTask.Status != models.TaskStatusReviewing {
		t.Errorf("Status = %v, want REVIEWING (unchanged)", readTask.Status)
	}
}

func TestSubmitVerdict_StatErrorNotSilenced(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	reviewCommit := "abc123def456"
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now)
	task.ReviewCommit = &reviewCommit
	state.Tasks = []models.Task{task}
	state.Agents["code-reviewer-1"] = models.Agent{
		Role:   "code-reviewer",
		Status: models.AgentStatusWorking,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	// The stat has to fail for a reason other than absence. A regular file at
	// .worktrees gives ENOTDIR on POSIX, but Windows reports that same layout as
	// "path not found" — os.IsNotExist is true — and an unprivileged process
	// cannot deny itself access to a path it owns. So the error is injected.
	originalStat := statWorktreePath
	statWorktreePath = func(string) (os.FileInfo, error) { return nil, os.ErrPermission }
	t.Cleanup(func() { statWorktreePath = originalStat })

	_, err := SubmitVerdict(tmpDir, "task-1", "APPROVED", "", "code-reviewer-1", "")
	if err == nil {
		t.Fatal("Expected stat error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to stat worktree") {
		t.Fatalf("Expected 'failed to stat worktree' error, got: %v", err)
	}

	// Verify task state unchanged
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	readTask := readState.FindTask("task-1")
	if readTask.Status != models.TaskStatusReviewing {
		t.Errorf("Status = %v, want REVIEWING (unchanged)", readTask.Status)
	}
}

func TestSubmitVerdictApprovals(t *testing.T) {
	t.Run("approved builds approval and sets derived approved_by", func(t *testing.T) {
		tmpDir := t.TempDir()
		stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

		now := time.Now().UTC()
		state := testhelpers.CreateValidState()
		state.Tasks = []models.Task{
			testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now),
		}
		state.Agents["code-reviewer-1"] = models.Agent{
			Role:     "code-reviewer",
			Status:   models.AgentStatusWorking,
			Provider: "claude",
		}
		testhelpers.WriteInitialState(t, stateFile, state)

		result, err := SubmitVerdict(tmpDir, "task-1", "APPROVED", "", "code-reviewer-1", "")
		if err != nil {
			t.Fatalf("SubmitVerdict() error: %v", err)
		}
		if result.Verdict != "APPROVED" {
			t.Errorf("Verdict = %q, want %q", result.Verdict, "APPROVED")
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

		// Verify approvals list
		if task.ApprovalCount() != 1 {
			t.Fatalf("ApprovalCount() = %d, want 1", task.ApprovalCount())
		}
		approval := task.Approvals[0]
		if approval.Agent != "code-reviewer-1" {
			t.Errorf("Approval.Agent = %q, want %q", approval.Agent, "code-reviewer-1")
		}
		if approval.Provider != "claude" {
			t.Errorf("Approval.Provider = %q, want %q", approval.Provider, "claude")
		}
		if approval.Timestamp.IsZero() {
			t.Error("Approval.Timestamp is zero")
		}

		// Verify derived ApprovedBy for backward compat
		if task.ApprovedBy == nil || *task.ApprovedBy != "code-reviewer-1" {
			t.Error("ApprovedBy (derived) should be code-reviewer-1")
		}

		// Verify LastApprover matches
		if task.LastApprover() != "code-reviewer-1" {
			t.Errorf("LastApprover() = %q, want %q", task.LastApprover(), "code-reviewer-1")
		}
	})

	t.Run("rejected clears approvals", func(t *testing.T) {
		tmpDir := t.TempDir()
		stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

		now := time.Now().UTC()
		state := testhelpers.CreateValidState()
		task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now)
		// Pre-populate approvals and derived ApprovedBy (simulating a partially-approved task re-entering review)
		task.Approvals = []models.Approval{
			{Agent: "code-reviewer-2", Provider: "codex", Timestamp: now.Add(-10 * time.Minute)},
		}
		priorApprover := "code-reviewer-2"
		task.ApprovedBy = &priorApprover
		state.Tasks = []models.Task{task}
		state.Agents["code-reviewer-1"] = models.Agent{
			Role:     "code-reviewer",
			Status:   models.AgentStatusWorking,
			Provider: "claude",
		}
		testhelpers.WriteInitialState(t, stateFile, state)

		result, err := SubmitVerdict(tmpDir, "task-1", "REJECTED", "Needs rework", "code-reviewer-1", "")
		if err != nil {
			t.Fatalf("SubmitVerdict() error: %v", err)
		}
		if result.Verdict != "REJECTED" {
			t.Errorf("Verdict = %q, want %q", result.Verdict, "REJECTED")
		}

		bb := db.New(stateFile)
		readState, err := bb.Read()
		if err != nil {
			t.Fatalf("Failed to read state: %v", err)
		}

		rejTask := readState.FindTask("task-1")
		if rejTask == nil {
			t.Fatal("Task not found")
		}
		if rejTask.Approvals != nil {
			t.Errorf("Approvals = %v, want nil after rejection", rejTask.Approvals)
		}
		if rejTask.ApprovedBy != nil {
			t.Errorf("ApprovedBy = %v, want nil after rejection (derived field must be cleared with approvals)", *rejTask.ApprovedBy)
		}
	})

	t.Run("approved with empty provider falls back gracefully", func(t *testing.T) {
		tmpDir := t.TempDir()
		stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

		now := time.Now().UTC()
		state := testhelpers.CreateValidState()
		state.Tasks = []models.Task{
			testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now),
		}
		// Agent without provider set (backward compat scenario)
		state.Agents["code-reviewer-1"] = models.Agent{
			Role:   "code-reviewer",
			Status: models.AgentStatusWorking,
		}
		testhelpers.WriteInitialState(t, stateFile, state)

		result, err := SubmitVerdict(tmpDir, "task-1", "APPROVED", "", "code-reviewer-1", "")
		if err != nil {
			t.Fatalf("SubmitVerdict() error: %v", err)
		}
		if result.Verdict != "APPROVED" {
			t.Errorf("Verdict = %q, want %q", result.Verdict, "APPROVED")
		}

		bb := db.New(stateFile)
		readState, err := bb.Read()
		if err != nil {
			t.Fatalf("Failed to read state: %v", err)
		}

		task := readState.FindTask("task-1")
		if task.ApprovalCount() != 1 {
			t.Fatalf("ApprovalCount() = %d, want 1", task.ApprovalCount())
		}
		// Provider should be empty string, not cause a crash
		if task.Approvals[0].Provider != "" {
			t.Errorf("Approval.Provider = %q, want empty string", task.Approvals[0].Provider)
		}
	})
}

func TestSubmitVerdict_ApprovedFromReviewing2(t *testing.T) {
	// Verifies that a verdict can be submitted from REVIEWING_CODE_2 state
	// (second review in quorum flow). The task should transition to APPROVED.
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	reviewCommit := "review123"
	staleMergeCommit := "stale-merge"
	worktree := ".worktrees/task-1"
	reviewingBy := "code-reviewer-2"
	reviewLease := now.Add(30 * time.Minute)
	state.Tasks = []models.Task{
		{
			ID:           "task-1",
			Status:       models.TaskStatusReviewingCode2,
			RolePair:     "coding-pair",
			Priority:     1,
			ReviewCommit: &reviewCommit,
			MergeCommit:  &staleMergeCommit,
			IntegrationFailure: map[string]any{
				"operation": "wt-merge",
				"reason":    "merge conflict",
			},
			Worktree:           &worktree,
			ReviewingBy:        &reviewingBy,
			ReviewLeaseExpires: &reviewLease,
			History:            []models.TaskHistoryEntry{},
			Created:            now,
			Approvals: []models.Approval{
				{Agent: "code-reviewer-1", Provider: "anthropic", Timestamp: now},
			},
		},
	}
	state.Agents["code-reviewer-2"] = models.Agent{
		Role:   "code-reviewer",
		Status: models.AgentStatusReviewing,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := SubmitVerdict(tmpDir, "task-1", "APPROVED", "", "code-reviewer-2", "")
	if err != nil {
		t.Fatalf("SubmitVerdict() error: %v", err)
	}
	if result.Verdict != "APPROVED" {
		t.Errorf("Verdict = %q, want %q", result.Verdict, "APPROVED")
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
	if task.Status != models.TaskStatusApproved {
		t.Errorf("Status = %v, want CODE_APPROVED", task.Status)
	}
	if task.ReviewCommit == nil || *task.ReviewCommit != reviewCommit {
		t.Fatalf("ReviewCommit = %v, want %s", task.ReviewCommit, reviewCommit)
	}
	if task.MergeCommit != nil {
		t.Fatalf("MergeCommit = %v, want nil stale merge metadata", *task.MergeCommit)
	}
	if task.IntegrationFailure != nil {
		t.Fatalf("IntegrationFailure = %v, want nil stale failure metadata", task.IntegrationFailure)
	}
	if task.ApprovedBy == nil || *task.ApprovedBy != "code-reviewer-2" {
		t.Error("ApprovedBy should be code-reviewer-2")
	}
	if task.ReviewingBy != nil {
		t.Error("ReviewingBy should be cleared")
	}
}

func TestSubmitVerdict_RejectedFromReviewing2(t *testing.T) {
	// Verifies that a rejection can be submitted from REVIEWING_CODE_2 state.
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	reviewCommit := "review123"
	worktree := ".worktrees/task-1"
	reviewingBy := "code-reviewer-2"
	reviewLease := now.Add(30 * time.Minute)
	state.Tasks = []models.Task{
		{
			ID:                 "task-1",
			Status:             models.TaskStatusReviewingCode2,
			RolePair:           "coding-pair",
			Priority:           1,
			ReviewCommit:       &reviewCommit,
			Worktree:           &worktree,
			ReviewingBy:        &reviewingBy,
			ReviewLeaseExpires: &reviewLease,
			History:            []models.TaskHistoryEntry{},
			Created:            now,
			Approvals: []models.Approval{
				{Agent: "code-reviewer-1", Provider: "anthropic", Timestamp: now},
			},
		},
	}
	state.Agents["code-reviewer-2"] = models.Agent{
		Role:   "code-reviewer",
		Status: models.AgentStatusReviewing,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := SubmitVerdict(tmpDir, "task-1", "REJECTED", "Needs improvement", "code-reviewer-2", "")
	if err != nil {
		t.Fatalf("SubmitVerdict() error: %v", err)
	}
	if result.Verdict != "REJECTED" {
		t.Errorf("Verdict = %q, want %q", result.Verdict, "REJECTED")
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
	if task.Status != models.TaskStatusRejected {
		t.Errorf("Status = %v, want CODE_REJECTED", task.Status)
	}
	if task.RejectionReason == nil || *task.RejectionReason != "Needs improvement" {
		t.Error("RejectionReason not set correctly")
	}
}

func TestResolveEffectiveImpact(t *testing.T) {
	tests := []struct {
		name    string
		history []models.TaskHistoryEntry
		want    string
	}{
		{
			name:    "no impact declared returns standard",
			history: nil,
			want:    "standard",
		},
		{
			name: "checkpoint-only impact",
			history: []models.TaskHistoryEntry{
				{Event: models.TaskEventPreExecutionCheckpoint, Extra: map[string]any{"impact": "significant"}},
			},
			want: "significant",
		},
		{
			name: "verdict upgrades checkpoint impact",
			history: []models.TaskHistoryEntry{
				{Event: models.TaskEventPreExecutionCheckpoint, Extra: map[string]any{"impact": "significant"}},
				{Event: models.TaskEventApproved, Extra: map[string]any{"impact": "architecture"}},
			},
			want: "architecture",
		},
		{
			name: "rejection resets cycle — post-rejection checkpoint starts fresh",
			history: []models.TaskHistoryEntry{
				{Event: models.TaskEventPreExecutionCheckpoint, Extra: map[string]any{"impact": "architecture"}},
				{Event: models.TaskEventRejected},
				{Event: models.TaskEventPreExecutionCheckpoint, Extra: map[string]any{"impact": "standard"}},
			},
			want: "standard",
		},
		{
			name: "entries without impact are ignored",
			history: []models.TaskHistoryEntry{
				{Event: models.TaskEventPreExecutionCheckpoint, Extra: map[string]any{"impact": "significant"}},
				{Event: models.TaskEventSubmittedForReview},
			},
			want: "significant",
		},
		{
			name: "only checkpoint and verdict events contribute impact",
			history: []models.TaskHistoryEntry{
				{Event: models.TaskEventPreExecutionCheckpoint, Extra: map[string]any{"impact": "standard"}},
				{Event: models.TaskEventApproved, Extra: map[string]any{"impact": "significant"}},
				{Event: models.TaskEventBlocked},
			},
			want: "significant",
		},
		{
			name: "empty extra on checkpoint defaults to standard",
			history: []models.TaskHistoryEntry{
				{Event: models.TaskEventPreExecutionCheckpoint},
			},
			want: "standard",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveEffectiveImpact(tt.history)
			if got != tt.want {
				t.Errorf("ResolveEffectiveImpact() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestQuorumEvaluation(t *testing.T) {
	setupQuorumEnv := func(t *testing.T, task models.Task, agents map[string]models.Agent, pipelineYAML string) (string, string) {
		t.Helper()
		tmpDir := t.TempDir()
		stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

		// Write custom pipeline config
		pipelinePath := filepath.Join(tmpDir, paths.ProjectDirName(), "pipeline.yaml")
		if err := os.WriteFile(pipelinePath, []byte(pipelineYAML), 0644); err != nil {
			t.Fatalf("Failed to write pipeline config: %v", err)
		}

		state := testhelpers.CreateValidState()
		state.Tasks = []models.Task{task}
		for id, agent := range agents {
			state.Agents[id] = agent
		}
		testhelpers.WriteInitialState(t, stateFile, state)
		return tmpDir, stateFile
	}

	// Pipeline with quorum 1 (standard) but quorum 2 for architecture
	quorum2Pipeline := `pipeline:
  roles:
    coder:
      type: doer
      display-name: Coder
      timeouts: {execution: 2h, poll-interval: 30s, max-wait: 30m}
      context-sections: [assigned-task]
      allowed-operations: [write-checkpoint, submit-for-review]
    code-reviewer:
      type: reviewer
      display-name: Code Reviewer
      timeouts: {execution: 30m, poll-interval: 30s, max-wait: 30m}
      context-sections: [review-task]
      allowed-operations: [submit-verdict]
    orchestrator:
      type: orchestrator
      display-name: Orchestrator
      max-instances: 1
      timeouts: {execution: 4h, poll-interval: 60s, max-wait: 30m}
      context-sections: [orchestrator-dashboard]
      allowed-operations: [add-tasks]
  role-pairs:
    coding-pair:
      doer: coder
      reviewer: code-reviewer
      review-policy:
        quorum: 1
        significant-change:
          quorum: 2
          provider-diversity: preferred
        architecture-impact:
          quorum: 2
          provider-diversity: preferred
      states:
        initial: DRAFT_CODE
        executing: IMPLEMENTING_CODE
        submitted: CODE_READY_FOR_REVIEW
        reviewing: REVIEWING_CODE
        approved: CODE_APPROVED
        rejected: CODE_REJECTED
        partially-approved: CODE_PARTIALLY_APPROVED
        reviewing-2: REVIEWING_CODE_2
  sub-pipelines:
    coding:
      steps: [coding-pair]
`

	t.Run("quorum-1 standard path — single approval transitions to approved", func(t *testing.T) {
		now := time.Now().UTC()
		task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now)
		// Checkpoint with standard impact
		task.History = append(task.History, models.TaskHistoryEntry{
			Time:  now.Add(-5 * time.Minute),
			Event: models.TaskEventPreExecutionCheckpoint,
			Extra: map[string]any{"impact": "standard"},
		})

		tmpDir, stateFile := setupQuorumEnv(t, task, map[string]models.Agent{
			"code-reviewer-1": {Role: "code-reviewer", Status: models.AgentStatusWorking, Provider: "claude"},
		}, quorum2Pipeline)

		result, err := SubmitVerdict(tmpDir, "task-1", "APPROVED", "", "code-reviewer-1", "")
		if err != nil {
			t.Fatalf("SubmitVerdict() error: %v", err)
		}
		if result.Verdict != "APPROVED" {
			t.Errorf("Verdict = %q, want %q", result.Verdict, "APPROVED")
		}

		bb := db.New(stateFile)
		readState, _ := bb.Read()
		taskResult := readState.FindTask("task-1")
		if taskResult.Status != models.TaskStatusApproved {
			t.Errorf("Status = %v, want CODE_APPROVED", taskResult.Status)
		}
	})

	t.Run("quorum-2 both reviewers approve — second approval transitions to approved", func(t *testing.T) {
		now := time.Now().UTC()

		// Task already partially approved by reviewer 1, now in reviewing_2
		task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now)
		task.Status = models.TaskStatus("REVIEWING_CODE_2")
		task.Approvals = []models.Approval{
			{Agent: "code-reviewer-1", Provider: "claude", Timestamp: now.Add(-5 * time.Minute)},
		}
		reviewingBy := "code-reviewer-2"
		task.ReviewingBy = &reviewingBy
		// History with architecture impact
		task.History = append(task.History, models.TaskHistoryEntry{
			Time:  now.Add(-10 * time.Minute),
			Event: models.TaskEventPreExecutionCheckpoint,
			Extra: map[string]any{"impact": "architecture"},
		})

		tmpDir, stateFile := setupQuorumEnv(t, task, map[string]models.Agent{
			"code-reviewer-1": {Role: "code-reviewer", Status: models.AgentStatusIdle, Provider: "claude"},
			"code-reviewer-2": {Role: "code-reviewer", Status: models.AgentStatusWorking, Provider: "codex"},
		}, quorum2Pipeline)

		result, err := SubmitVerdict(tmpDir, "task-1", "APPROVED", "", "code-reviewer-2", "")
		if err != nil {
			t.Fatalf("SubmitVerdict() error: %v", err)
		}
		if result.Verdict != "APPROVED" {
			t.Errorf("Verdict = %q, want %q", result.Verdict, "APPROVED")
		}

		bb := db.New(stateFile)
		readState, _ := bb.Read()
		taskResult := readState.FindTask("task-1")
		if taskResult.Status != models.TaskStatusApproved {
			t.Errorf("Status = %v, want CODE_APPROVED", taskResult.Status)
		}
		if taskResult.ApprovalCount() != 2 {
			t.Errorf("ApprovalCount() = %d, want 2", taskResult.ApprovalCount())
		}
	})

	t.Run("impact upgrade triggers partial approval", func(t *testing.T) {
		now := time.Now().UTC()
		task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now)
		// Checkpoint with standard impact
		task.History = append(task.History, models.TaskHistoryEntry{
			Time:  now.Add(-5 * time.Minute),
			Event: models.TaskEventPreExecutionCheckpoint,
			Extra: map[string]any{"impact": "standard"},
		})

		tmpDir, stateFile := setupQuorumEnv(t, task, map[string]models.Agent{
			"code-reviewer-1": {Role: "code-reviewer", Status: models.AgentStatusWorking, Provider: "claude"},
		}, quorum2Pipeline)

		// Reviewer approves with architecture impact — upgrades quorum to 2
		result, err := SubmitVerdict(tmpDir, "task-1", "APPROVED", "", "code-reviewer-1", "architecture")
		if err != nil {
			t.Fatalf("SubmitVerdict() error: %v", err)
		}
		if result.Verdict != "APPROVED" {
			t.Errorf("Verdict = %q, want %q", result.Verdict, "APPROVED")
		}

		bb := db.New(stateFile)
		readState, _ := bb.Read()
		taskResult := readState.FindTask("task-1")
		if taskResult.Status != models.TaskStatus("CODE_PARTIALLY_APPROVED") {
			t.Errorf("Status = %v, want CODE_PARTIALLY_APPROVED", taskResult.Status)
		}
		if taskResult.ApprovalCount() != 1 {
			t.Errorf("ApprovalCount() = %d, want 1", taskResult.ApprovalCount())
		}

		// Verify impact stored in history extra
		found := false
		for i := len(taskResult.History) - 1; i >= 0; i-- {
			if taskResult.History[i].Event == models.TaskEventApproved {
				if v, ok := taskResult.History[i].Extra["impact"].(string); ok && v == "architecture" {
					found = true
				}
				break
			}
		}
		if !found {
			t.Error("Expected impact=architecture in approved history entry Extra")
		}
	})

	t.Run("rejection clears and restarts", func(t *testing.T) {
		now := time.Now().UTC()

		// Task in reviewing_2 with 1 prior approval
		task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now)
		task.Status = models.TaskStatus("REVIEWING_CODE_2")
		task.Approvals = []models.Approval{
			{Agent: "code-reviewer-1", Provider: "claude", Timestamp: now.Add(-5 * time.Minute)},
		}
		priorApprover := "code-reviewer-1"
		task.ApprovedBy = &priorApprover
		reviewingBy := "code-reviewer-2"
		task.ReviewingBy = &reviewingBy
		task.History = append(task.History, models.TaskHistoryEntry{
			Time:  now.Add(-10 * time.Minute),
			Event: models.TaskEventPreExecutionCheckpoint,
			Extra: map[string]any{"impact": "architecture"},
		})

		tmpDir, stateFile := setupQuorumEnv(t, task, map[string]models.Agent{
			"code-reviewer-2": {Role: "code-reviewer", Status: models.AgentStatusWorking, Provider: "codex"},
		}, quorum2Pipeline)

		result, err := SubmitVerdict(tmpDir, "task-1", "REJECTED", "Architectural concerns", "code-reviewer-2", "")
		if err != nil {
			t.Fatalf("SubmitVerdict() error: %v", err)
		}
		if result.Verdict != "REJECTED" {
			t.Errorf("Verdict = %q, want %q", result.Verdict, "REJECTED")
		}

		bb := db.New(stateFile)
		readState, _ := bb.Read()
		taskResult := readState.FindTask("task-1")
		if taskResult.Status != models.TaskStatusRejected {
			t.Errorf("Status = %v, want CODE_REJECTED", taskResult.Status)
		}
		if taskResult.Approvals != nil {
			t.Errorf("Approvals = %v, want nil after rejection", taskResult.Approvals)
		}
		if taskResult.ApprovedBy != nil {
			t.Errorf("ApprovedBy = %v, want nil after rejection", taskResult.ApprovedBy)
		}
	})

	t.Run("impact downgrade rejected", func(t *testing.T) {
		now := time.Now().UTC()
		task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now)
		// Checkpoint declares architecture impact
		task.History = append(task.History, models.TaskHistoryEntry{
			Time:  now.Add(-5 * time.Minute),
			Event: models.TaskEventPreExecutionCheckpoint,
			Extra: map[string]any{"impact": "architecture"},
		})

		tmpDir, _ := setupQuorumEnv(t, task, map[string]models.Agent{
			"code-reviewer-1": {Role: "code-reviewer", Status: models.AgentStatusWorking, Provider: "claude"},
		}, quorum2Pipeline)

		// Reviewer attempts to downgrade to standard — should be rejected
		_, err := SubmitVerdict(tmpDir, "task-1", "APPROVED", "", "code-reviewer-1", "standard")
		if err == nil {
			t.Fatal("Expected error for impact downgrade")
		}
		if !strings.Contains(err.Error(), "cannot downgrade") {
			t.Errorf("Error = %q, want to contain 'cannot downgrade'", err.Error())
		}
	})
}

func TestSubmitVerdict_CleanScanRouting(t *testing.T) {
	// Pipeline with integration-pair declaring a clean state
	cleanPipeline := `pipeline:
  roles:
    coder:
      type: doer
      display-name: Coder
      timeouts: {execution: 2h, poll-interval: 30s, max-wait: 30m}
      context-sections: [assigned-task]
      allowed-operations: [write-checkpoint, submit-for-review, mark-blocked, handoff, set-task-output, await-verdict]
    code-reviewer:
      type: reviewer
      display-name: Code Reviewer
      timeouts: {execution: 30m, poll-interval: 30s, max-wait: 30m}
      context-sections: [review-task]
      allowed-operations: [submit-verdict, await-resubmission]
    integration-analyst:
      type: doer
      display-name: Integration Analyst
      timeouts: {execution: 2h, poll-interval: 30s, max-wait: 30m}
      context-sections: [assigned-task]
      allowed-operations: [write-checkpoint, submit-for-review, mark-blocked, handoff, set-task-output, await-verdict]
    integration-reviewer:
      type: reviewer
      display-name: Integration Reviewer
      timeouts: {execution: 30m, poll-interval: 30s, max-wait: 30m}
      context-sections: [review-task]
      allowed-operations: [submit-verdict, await-resubmission]
    orchestrator:
      type: orchestrator
      display-name: Orchestrator
      max-instances: 1
      timeouts: {execution: 4h, poll-interval: 60s, max-wait: 30m}
      context-sections: [orchestrator-dashboard]
      allowed-operations: [add-tasks, sprint-checkpoint]
  role-pairs:
    coding-pair:
      doer: coder
      reviewer: code-reviewer
      states:
        initial: DRAFT_CODE
        executing: IMPLEMENTING_CODE
        submitted: CODE_READY_FOR_REVIEW
        reviewing: REVIEWING_CODE
        approved: CODE_APPROVED
        rejected: CODE_REJECTED
    integration-pair:
      doer: integration-analyst
      reviewer: integration-reviewer
      states:
        initial: DRAFT_INTEGRATION_ANALYSIS
        executing: ANALYZING_INTEGRATION
        submitted: INTEGRATION_ANALYSIS_TO_REVIEW
        reviewing: REVIEWING_INTEGRATION_ANALYSIS
        approved: INTEGRATION_ANALYSIS_APPROVED
        rejected: INTEGRATION_ANALYSIS_REJECTED
        clean: INTEGRATION_ANALYSIS_CLEAN
  sub-pipelines:
    integration-subpipeline:
      steps: [integration-pair, coding-pair]
      transitions:
        - name: integration-to-fix
          from: integration-pair.approved
          to: coding-pair.initial
          trigger: manual
          cardinality: per-subtask
  entry-points:
    detailed-spec: integration-subpipeline.integration-pair
`

	setupCleanTest := func(t *testing.T, rolePair string, reviewingStatus models.TaskStatus, output []models.OutputEntry) (string, string) {
		t.Helper()
		tmpDir := t.TempDir()
		stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

		// Overwrite with clean-aware pipeline
		pipelinePath := filepath.Join(tmpDir, paths.ProjectDirName(), "pipeline.yaml")
		if err := os.WriteFile(pipelinePath, []byte(cleanPipeline), 0644); err != nil {
			t.Fatalf("Failed to write pipeline config: %v", err)
		}

		now := time.Now().UTC()
		reviewCommit := "abc123"
		state := testhelpers.CreateValidState()
		state.Tasks = []models.Task{
			{
				ID:           "task-1",
				Status:       reviewingStatus,
				RolePair:     rolePair,
				Priority:     1,
				ReviewCommit: &reviewCommit,
				Output:       output,
				History:      []models.TaskHistoryEntry{},
				Created:      now,
				SpecRef:      "README.md",
				DoneWhen:     "Task is complete",
				Scope:        "Test scope",
			},
		}

		reviewerAgent := "integration-reviewer-1"
		if rolePair == "coding-pair" {
			reviewerAgent = "code-reviewer-1"
		}
		state.Tasks[0].ReviewingBy = &reviewerAgent
		lease := now.Add(time.Hour)
		state.Tasks[0].ReviewLeaseExpires = &lease
		state.Agents[reviewerAgent] = models.Agent{
			Role:   strings.TrimSuffix(reviewerAgent, "-1"),
			Status: models.AgentStatusReviewing,
		}
		testhelpers.WriteInitialState(t, stateFile, state)
		return tmpDir, reviewerAgent
	}

	t.Run("empty output with clean-declared pair transitions to clean", func(t *testing.T) {
		tmpDir, agentID := setupCleanTest(t,
			"integration-pair",
			models.TaskStatus("REVIEWING_INTEGRATION_ANALYSIS"),
			nil, // empty output
		)

		result, err := SubmitVerdict(tmpDir, "task-1", "APPROVED", "", agentID, "")
		if err != nil {
			t.Fatalf("SubmitVerdict() error: %v", err)
		}
		if result.Verdict != "APPROVED" {
			t.Errorf("Verdict = %q, want %q", result.Verdict, "APPROVED")
		}

		bb := db.New(filepath.Join(tmpDir, paths.ProjectDirName(), "state.yaml"))
		readState, err := bb.Read()
		if err != nil {
			t.Fatalf("Failed to read state: %v", err)
		}
		task := readState.FindTask("task-1")
		if task == nil {
			t.Fatal("Task not found")
		}
		wantStatus := models.TaskStatus("INTEGRATION_ANALYSIS_CLEAN")
		if task.Status != wantStatus {
			t.Errorf("Status = %v, want %v", task.Status, wantStatus)
		}
	})

	t.Run("non-empty output with clean-declared pair transitions to approved", func(t *testing.T) {
		tmpDir, agentID := setupCleanTest(t,
			"integration-pair",
			models.TaskStatus("REVIEWING_INTEGRATION_ANALYSIS"),
			[]models.OutputEntry{{Desc: "fix type alignment", DoneWhen: "types match", Scope: "pkg/", SpecRef: "spec.md"}},
		)

		result, err := SubmitVerdict(tmpDir, "task-1", "APPROVED", "", agentID, "")
		if err != nil {
			t.Fatalf("SubmitVerdict() error: %v", err)
		}
		if result.Verdict != "APPROVED" {
			t.Errorf("Verdict = %q, want %q", result.Verdict, "APPROVED")
		}

		bb := db.New(filepath.Join(tmpDir, paths.ProjectDirName(), "state.yaml"))
		readState, err := bb.Read()
		if err != nil {
			t.Fatalf("Failed to read state: %v", err)
		}
		task := readState.FindTask("task-1")
		if task == nil {
			t.Fatal("Task not found")
		}
		wantStatus := models.TaskStatus("INTEGRATION_ANALYSIS_APPROVED")
		if task.Status != wantStatus {
			t.Errorf("Status = %v, want %v", task.Status, wantStatus)
		}
	})

	t.Run("no clean state declared transitions to approved regardless of output", func(t *testing.T) {
		tmpDir, agentID := setupCleanTest(t,
			"coding-pair",
			models.TaskStatus("REVIEWING_CODE"),
			nil, // empty output — should still go to approved
		)

		result, err := SubmitVerdict(tmpDir, "task-1", "APPROVED", "", agentID, "")
		if err != nil {
			t.Fatalf("SubmitVerdict() error: %v", err)
		}
		if result.Verdict != "APPROVED" {
			t.Errorf("Verdict = %q, want %q", result.Verdict, "APPROVED")
		}

		bb := db.New(filepath.Join(tmpDir, paths.ProjectDirName(), "state.yaml"))
		readState, err := bb.Read()
		if err != nil {
			t.Fatalf("Failed to read state: %v", err)
		}
		task := readState.FindTask("task-1")
		if task == nil {
			t.Fatal("Task not found")
		}
		wantStatus := models.TaskStatus("CODE_APPROVED")
		if task.Status != wantStatus {
			t.Errorf("Status = %v, want %v", task.Status, wantStatus)
		}
	})
}

func assertReleasedAgent(t *testing.T, state *models.State, agentID string) {
	t.Helper()

	agent := state.Agents[agentID]
	if agent.Status != models.AgentStatusIdle || agent.CurrentTask != nil {
		t.Errorf("%s should be released to IDLE, got status=%v current_task=%v", agentID, agent.Status, agent.CurrentTask)
	}
}

func TestSubmitVerdict_RejectedRefreshesLease(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	leaseDuration := 120
	expiredLease := now.Add(-10 * time.Minute)

	state := testhelpers.CreateValidState()
	state.Config.LeaseDuration = leaseDuration
	coderID := "coder-1"
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now)
	task.AssignedTo = &coderID
	task.LeaseExpires = &expiredLease
	state.Tasks = []models.Task{task}
	state.Agents["code-reviewer-1"] = models.Agent{
		Role:   "code-reviewer",
		Status: models.AgentStatusWorking,
	}
	state.Agents[coderID] = models.Agent{
		Role:   "coder",
		Status: models.AgentStatusWaiting,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	callStart := time.Now().UTC()
	result, err := SubmitVerdict(tmpDir, "task-1", "REJECTED", "Needs work", "code-reviewer-1", "")
	if err != nil {
		t.Fatalf("SubmitVerdict() error: %v", err)
	}
	if result.EscalatedToBlocked {
		t.Fatal("Unexpected escalation — test expects non-escalating rejection")
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	rejTask := readState.FindTask("task-1")
	if rejTask == nil {
		t.Fatal("Task not found")
	}

	// Lease should be refreshed on non-escalating rejection
	expectedMin := callStart.Add(time.Duration(leaseDuration) * time.Second)
	if rejTask.LeaseExpires == nil {
		t.Fatal("LeaseExpires is nil, want refreshed lease")
	}
	if rejTask.LeaseExpires.Before(expectedMin) {
		t.Errorf("LeaseExpires = %v, want >= %v", rejTask.LeaseExpires, expectedMin)
	}
}

func TestSubmitVerdict_EscalationClearsLease(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	coderID := "coder-1"
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now)
	task.AssignedTo = &coderID
	task.ReviewCyclesCurrent = 1
	task.ReviewCyclesTotal = 1
	task.Attempt = 2
	state := testhelpers.CreateValidState()
	state.Config.MaxReviewCycles = 2
	state.Tasks = []models.Task{task}

	taskRef := "task-1"
	state.Agents[coderID] = models.Agent{
		Role:        "coder",
		Status:      models.AgentStatusWaiting,
		CurrentTask: &taskRef,
	}
	state.Agents["code-reviewer-1"] = models.Agent{
		Role:   "code-reviewer",
		Status: models.AgentStatusReviewing,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := SubmitVerdict(tmpDir, "task-1", "REJECTED", "Still broken", "code-reviewer-1", "")
	if err != nil {
		t.Fatalf("SubmitVerdict() error: %v", err)
	}
	if !result.EscalatedToBlocked {
		t.Fatal("Expected escalation to BLOCKED")
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	blockedTask := readState.FindTask("task-1")
	if blockedTask == nil {
		t.Fatal("Task not found")
	}

	// Escalation should clear lease and assignment
	if blockedTask.LeaseExpires != nil {
		t.Errorf("LeaseExpires = %v, want nil after escalation", blockedTask.LeaseExpires)
	}
	if blockedTask.AssignedTo != nil {
		t.Errorf("AssignedTo = %v, want nil after escalation", blockedTask.AssignedTo)
	}
}

func TestSubmitVerdict_RejectionAtReviewCap_Attempt1_TriggersNewAttempt(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	coderID := "coder-1"
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now)
	task.AssignedTo = &coderID
	task.Attempt = 1
	task.Iteration = 3
	task.ReviewCyclesCurrent = 1
	task.ReviewCyclesTotal = 1

	state := testhelpers.CreateValidState()
	state.Config.MaxReviewCycles = 2
	state.Tasks = []models.Task{task}

	taskRef := "task-1"
	state.Agents[coderID] = models.Agent{
		Role:        "coder",
		Status:      models.AgentStatusWaiting,
		CurrentTask: &taskRef,
	}
	state.Agents["code-reviewer-1"] = models.Agent{
		Role:   "code-reviewer",
		Status: models.AgentStatusReviewing,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := SubmitVerdict(tmpDir, "task-1", "REJECTED", "Approach is wrong", "code-reviewer-1", "")
	if err != nil {
		t.Fatalf("SubmitVerdict() error: %v", err)
	}
	if !result.NewAttemptTriggered {
		t.Error("NewAttemptTriggered = false, want true")
	}
	if result.EscalatedToBlocked {
		t.Error("EscalatedToBlocked = true, want false for new attempt")
	}

	// Verify task transitioned to initial status with attempt 2
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	transitioned := readState.FindTask("task-1")
	if transitioned == nil {
		t.Fatal("Task not found")
	}
	if transitioned.Attempt != 2 {
		t.Errorf("Attempt = %d, want 2", transitioned.Attempt)
	}
	if transitioned.Status != models.TaskStatusReady {
		t.Errorf("Status = %v, want %v (initial status)", transitioned.Status, models.TaskStatusReady)
	}
	if transitioned.Iteration != 0 {
		t.Errorf("Iteration = %d, want 0", transitioned.Iteration)
	}
	if transitioned.ReviewCyclesCurrent != 0 {
		t.Errorf("ReviewCyclesCurrent = %d, want 0", transitioned.ReviewCyclesCurrent)
	}
	if transitioned.AssignedTo != nil {
		t.Errorf("AssignedTo = %v, want nil", transitioned.AssignedTo)
	}
	if transitioned.RejectionReason != nil {
		t.Errorf("RejectionReason = %v, want nil after attempt transition", transitioned.RejectionReason)
	}

	// Coder agent should be released by TransitionToNewAttempt
	assertReleasedAgent(t, readState, coderID)
	// Reviewer agent released by SubmitVerdict
	assertReleasedAgent(t, readState, "code-reviewer-1")
}

func TestSubmitVerdict_RejectionAtReviewCap_Attempt1_TransitionFailure_PropagatesError(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	secret := "super-secret-submit-verdict-token"
	t.Setenv("LIZA_TEST_TOKEN", secret)

	now := time.Now().UTC()
	coderID := "coder-1"
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now)
	task.AssignedTo = &coderID
	task.Attempt = 1
	task.Iteration = 3
	task.ReviewCyclesCurrent = 1
	task.ReviewCyclesTotal = 1

	state := testhelpers.CreateValidState()
	state.Config.MaxReviewCycles = 2
	state.Tasks = []models.Task{task}

	taskRef := "task-1"
	state.Agents[coderID] = models.Agent{
		Role:        "coder",
		Status:      models.AgentStatusWaiting,
		CurrentTask: &taskRef,
	}
	state.Agents["code-reviewer-1"] = models.Agent{
		Role:   "code-reviewer",
		Status: models.AgentStatusReviewing,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	// Use testTransitionHooks to replace the sentinel between Phase 1 and Phase 3,
	// causing Phase 3 to fail with a "sentinel replaced" error.
	bb := db.New(stateFile)
	testTransitionHooks = &transitionTestHooks{
		afterPhase1: func() {
			// Replace sentinel with a different value to simulate concurrent modification.
			_ = bb.Modify(func(s *models.State) error {
				t := s.FindTask("task-1")
				if t != nil {
					interloper := "coder-interloper-" + secret
					t.AssignedTo = &interloper
				}
				return nil
			})
		},
	}
	t.Cleanup(func() { testTransitionHooks = nil })

	result, err := SubmitVerdict(tmpDir, "task-1", "REJECTED", "Approach is wrong", "code-reviewer-1", "")

	// SubmitVerdict must return error (not a result) when TransitionToNewAttempt fails.
	if err == nil {
		t.Fatal("SubmitVerdict() returned nil error, want error propagated from TransitionToNewAttempt failure")
	}
	if result != nil {
		t.Errorf("SubmitVerdict() returned non-nil result %+v, want nil on transition failure", result)
	}

	// Error should contain both the SubmitVerdict context and the phase 3 failure.
	if !strings.Contains(err.Error(), "attempt transition failed") {
		t.Errorf("error %q should contain 'attempt transition failed'", err.Error())
	}
	if !strings.Contains(err.Error(), "sentinel replaced") {
		t.Errorf("error %q should contain 'sentinel replaced'", err.Error())
	}

	// TransitionToNewAttempt Phase 1 committed (Attempt=2, counters reset)
	// but Phase 3 failed — task is stuck with the interloper AssignedTo.
	readState, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("Failed to read state: %v", readErr)
	}
	failedTask := readState.FindTask("task-1")
	if failedTask == nil {
		t.Fatal("Task not found")
	}
	if failedTask.Attempt != 2 {
		t.Errorf("Attempt = %d, want 2 (Phase 1 committed)", failedTask.Attempt)
	}
	if failedTask.AssignedTo == nil || *failedTask.AssignedTo != "coder-interloper-"+secret {
		t.Errorf("AssignedTo = %v, want secret-bearing interloper (sentinel was replaced, Phase 3 aborted)", failedTask.AssignedTo)
	}

	entries, logErr := activitylog.New(paths.New(tmpDir).LogPath()).Read()
	if logErr != nil {
		t.Fatalf("failed to read activity log: %v", logErr)
	}
	if len(entries) != 1 {
		t.Fatalf("log entries = %d, want 1", len(entries))
	}
	if entries[0].Action != "submit_verdict_failed" {
		t.Fatalf("log action = %q, want submit_verdict_failed", entries[0].Action)
	}
	if !strings.Contains(entries[0].Detail, "attempt transition failed") ||
		!strings.Contains(entries[0].Detail, "sentinel replaced") {
		t.Fatalf("log detail = %q, want underlying transition failure", entries[0].Detail)
	}
	if strings.Contains(entries[0].Detail, secret) {
		t.Fatalf("log detail leaked secret: %q", entries[0].Detail)
	}
	if !strings.Contains(entries[0].Detail, "***") {
		t.Fatalf("log detail = %q, want redacted secret marker", entries[0].Detail)
	}
	if !strings.Contains(entries[0].Detail, "stack=") ||
		!strings.Contains(entries[0].Detail, "SubmitVerdict") {
		t.Fatalf("log detail = %q, want bounded stack trace", entries[0].Detail)
	}

	readState, readErr = bb.Read()
	if readErr != nil {
		t.Fatalf("Failed to re-read state: %v", readErr)
	}
	if len(readState.Anomalies) != 1 {
		t.Fatalf("anomaly count = %d, want 1", len(readState.Anomalies))
	}
	anomaly := readState.Anomalies[0]
	if anomaly.Type != "submit_verdict_failed" || anomaly.Task != "task-1" || anomaly.Reporter != "code-reviewer-1" {
		t.Fatalf("anomaly = %+v, want submit_verdict_failed for task-1 by code-reviewer-1", anomaly)
	}
	if anomaly.Details["verdict"] != "REJECTED" {
		t.Fatalf("anomaly verdict = %v, want REJECTED", anomaly.Details["verdict"])
	}
	errorDetail, ok := anomaly.Details["error"].(string)
	if !ok {
		t.Fatalf("anomaly error detail = %T, want string", anomaly.Details["error"])
	}
	if !strings.Contains(errorDetail, "attempt transition failed") ||
		!strings.Contains(errorDetail, "sentinel replaced") {
		t.Fatalf("anomaly error = %q, want underlying transition failure", errorDetail)
	}
	if strings.Contains(errorDetail, secret) {
		t.Fatalf("anomaly error leaked secret: %q", errorDetail)
	}
	if !strings.Contains(errorDetail, "***") {
		t.Fatalf("anomaly error = %q, want redacted secret marker", errorDetail)
	}
}

func TestRecordSubmitVerdictFailure_LogsAnomalyRecordingFailure(t *testing.T) {
	tmpDir := t.TempDir()
	_, _ = testhelpers.SetupLizaDir(t, tmpDir)
	secret := "secondary-secret-submit-verdict-token"
	t.Setenv("LIZA_SECONDARY_TOKEN", secret)

	badStatePath := filepath.Join(tmpDir, "missing-dir", "state.yaml")
	recordSubmitVerdictFailure(
		db.New(badStatePath),
		paths.New(tmpDir).LogPath(),
		"task-1",
		"code-reviewer-1",
		nil,
		"REJECTED",
		fmt.Errorf("primary cause contains %s", secret),
	)

	entries, logErr := activitylog.New(paths.New(tmpDir).LogPath()).Read()
	if logErr != nil {
		t.Fatalf("failed to read activity log: %v", logErr)
	}
	if len(entries) != 1 {
		t.Fatalf("log entries = %d, want 1", len(entries))
	}
	detail := entries[0].Detail
	if !strings.Contains(detail, "primary cause contains") {
		t.Fatalf("log detail = %q, want primary cause preserved", detail)
	}
	if !strings.Contains(detail, "anomaly_recording_error=") {
		t.Fatalf("log detail = %q, want secondary anomaly recording failure", detail)
	}
	if strings.Contains(detail, secret) {
		t.Fatalf("log detail leaked secret: %q", detail)
	}
	if !strings.Contains(detail, "***") {
		t.Fatalf("log detail = %q, want redacted secret marker", detail)
	}
}

func TestSubmitVerdict_RejectionAtReviewCap_Attempt2_TriggersBlocked(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	coderID := "coder-1"
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now)
	task.AssignedTo = &coderID
	task.Attempt = 2
	task.Iteration = 3
	task.ReviewCyclesCurrent = 1
	task.ReviewCyclesTotal = 6

	state := testhelpers.CreateValidState()
	state.Config.MaxReviewCycles = 2
	state.Tasks = []models.Task{task}

	taskRef := "task-1"
	state.Agents[coderID] = models.Agent{
		Role:        "coder",
		Status:      models.AgentStatusWaiting,
		CurrentTask: &taskRef,
	}
	state.Agents["code-reviewer-1"] = models.Agent{
		Role:   "code-reviewer",
		Status: models.AgentStatusReviewing,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := SubmitVerdict(tmpDir, "task-1", "REJECTED", "Still wrong", "code-reviewer-1", "")
	if err != nil {
		t.Fatalf("SubmitVerdict() error: %v", err)
	}
	if !result.EscalatedToBlocked {
		t.Error("EscalatedToBlocked = false, want true")
	}
	if result.NewAttemptTriggered {
		t.Error("NewAttemptTriggered = true, want false for attempt 2")
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	blockedTask := readState.FindTask("task-1")
	if blockedTask == nil {
		t.Fatal("Task not found")
	}
	if blockedTask.Status != models.TaskStatusBlocked {
		t.Errorf("Status = %v, want BLOCKED", blockedTask.Status)
	}
	if blockedTask.BlockedReason == nil {
		t.Fatal("BlockedReason is nil, want set")
	}

	assertReleasedAgent(t, readState, coderID)
	assertReleasedAgent(t, readState, "code-reviewer-1")
}

func TestValidateIntegrationAnalysisRolePair(t *testing.T) {
	tests := []struct {
		name     string
		taskID   string
		phase    models.IntegrationAnalysisPhase
		rolePair string
		wantErr  string
	}{
		{
			name:     "slice phase accepts slice role pair",
			taskID:   "task-slice",
			phase:    models.IntegrationAnalysisPhaseSlice,
			rolePair: "slice-integration-pair",
		},
		{
			name:     "global phase accepts global role pair",
			taskID:   "task-global",
			phase:    models.IntegrationAnalysisPhaseGlobal,
			rolePair: "integration-pair",
		},
		{
			name:     "slice phase rejects global role pair",
			taskID:   "task-slice-mismatch",
			phase:    models.IntegrationAnalysisPhaseSlice,
			rolePair: "integration-pair",
			wantErr:  `task task-slice-mismatch integration analysis phase "slice" requires role_pair "slice-integration-pair", got "integration-pair"`,
		},
		{
			name:     "global phase rejects slice role pair",
			taskID:   "task-global-mismatch",
			phase:    models.IntegrationAnalysisPhaseGlobal,
			rolePair: "slice-integration-pair",
			wantErr:  `task task-global-mismatch integration analysis phase "global" requires role_pair "integration-pair", got "slice-integration-pair"`,
		},
		{
			name:     "invalid phase is rejected",
			taskID:   "task-invalid",
			phase:    models.IntegrationAnalysisPhase("future"),
			rolePair: "integration-pair",
			wantErr:  `task task-invalid has invalid integration analysis phase "future"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			task := &models.Task{
				ID:                  tc.taskID,
				RolePair:            tc.rolePair,
				IntegrationAnalysis: &models.IntegrationAnalysisMetadata{Phase: tc.phase},
			}

			err := validateIntegrationAnalysisRolePair(task)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("validateIntegrationAnalysisRolePair() error = %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("validateIntegrationAnalysisRolePair() error = nil, want error")
			}
			if got := err.Error(); got != tc.wantErr {
				t.Fatalf("validateIntegrationAnalysisRolePair() error = %q, want %q", got, tc.wantErr)
			}
		})
	}
}

func TestSubmitVerdictIntegrationLifecycleProjection(t *testing.T) {
	t.Run("final quorum slice approvals append immutable clean and findings reports", func(t *testing.T) {
		for _, tc := range []struct {
			name        string
			output      []models.OutputEntry
			wantVerdict models.IntegrationAnalysisVerdict
			wantStatus  models.TaskStatus
		}{
			{name: "clean", wantVerdict: models.IntegrationAnalysisVerdictClean, wantStatus: models.TaskStatus("SLICE_INTEGRATION_ANALYSIS_CLEAN")},
			{
				name:        "findings",
				output:      []models.OutputEntry{{Desc: "repair slice", DoneWhen: "slice repaired", Scope: "internal/ops", SpecRef: "README.md"}},
				wantVerdict: models.IntegrationAnalysisVerdictFindings,
				wantStatus:  models.TaskStatus("SLICE_INTEGRATION_ANALYSIS_APPROVED"),
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				fixture := newSubmitVerdictIntegrationFixture(t, models.IntegrationAnalysisPhaseSlice, tc.output)
				before := fixture.readState(t)
				priorCoverage := append([]models.IntegrationCoverageRecord(nil), before.Goal.Integration.Coverage...)
				priorReceipts := append([]models.IntegrationMutationReceipt(nil), before.Goal.Integration.MutationReceipts...)

				if _, err := SubmitVerdict(fixture.projectRoot, fixture.taskID, "APPROVED", "", fixture.reviewerID, ""); err != nil {
					t.Fatalf("SubmitVerdict() error = %v", err)
				}

				after := fixture.readState(t)
				task := after.FindTask(fixture.taskID)
				if task == nil || task.Status != tc.wantStatus {
					t.Fatalf("task status = %v, want %s", taskStatus(task), tc.wantStatus)
				}
				if !reflect.DeepEqual(after.Goal.Integration.Coverage[:len(priorCoverage)], priorCoverage) {
					t.Fatalf("prior coverage changed:\n got: %#v\nwant: %#v", after.Goal.Integration.Coverage[:len(priorCoverage)], priorCoverage)
				}
				if !reflect.DeepEqual(after.Goal.Integration.MutationReceipts, priorReceipts) {
					t.Fatalf("mutation receipts changed:\n got: %#v\nwant: %#v", after.Goal.Integration.MutationReceipts, priorReceipts)
				}
				if len(after.Goal.Integration.Coverage) != len(priorCoverage)+1 {
					t.Fatalf("coverage count = %d, want %d", len(after.Goal.Integration.Coverage), len(priorCoverage)+1)
				}
				record := after.Goal.Integration.Coverage[len(priorCoverage)]
				if record.Kind != models.IntegrationCoverageSliceReport || record.PlanTaskID != fixture.planTaskID || record.SliceReport == nil {
					t.Fatalf("slice coverage = %#v, want report for %s", record, fixture.planTaskID)
				}
				report := record.SliceReport
				if report.AnalysisTaskID != fixture.taskID || report.AnalysisKey != fixture.analysisKey || report.Verdict != tc.wantVerdict {
					t.Fatalf("slice report identity = %#v", report)
				}
				if report.SourceCommit != fixture.sourceCommit || report.ReportCommit != fixture.reportCommit || report.SourceCommit == report.ReportCommit {
					t.Fatalf("slice commits = source %q report %q, want distinct %q and %q", report.SourceCommit, report.ReportCommit, fixture.sourceCommit, fixture.reportCommit)
				}
			})
		}
	})

	t.Run("partial slice approval preserves lifecycle", func(t *testing.T) {
		fixture := newSubmitVerdictIntegrationFixture(t, models.IntegrationAnalysisPhaseSlice, nil)
		enableSubmitVerdictIntegrationQuorum(t, fixture.projectRoot, "slice-integration-pair")
		before := fixture.readState(t)

		if _, err := SubmitVerdict(fixture.projectRoot, fixture.taskID, "APPROVED", "", fixture.reviewerID, ""); err != nil {
			t.Fatalf("SubmitVerdict() error = %v", err)
		}

		after := fixture.readState(t)
		if !reflect.DeepEqual(after.Goal.Integration, before.Goal.Integration) {
			t.Fatalf("partial approval lifecycle changed:\n got: %#v\nwant: %#v", after.Goal.Integration, before.Goal.Integration)
		}
		if got := after.FindTask(fixture.taskID).Status; got != models.TaskStatus("SLICE_INTEGRATION_ANALYSIS_PARTIALLY_APPROVED") {
			t.Fatalf("partial approval status = %s", got)
		}
		if analyst := after.Agents[fixture.analystID]; analyst.Status != models.AgentStatusWaiting || analyst.CurrentTask == nil || *analyst.CurrentTask != fixture.taskID {
			t.Fatalf("partial approval analyst ownership = %#v, want waiting on %s", analyst, fixture.taskID)
		}
	})

	t.Run("global findings append contiguous generation without clean closure", func(t *testing.T) {
		fixture := newSubmitVerdictIntegrationFixture(t, models.IntegrationAnalysisPhaseGlobal, []models.OutputEntry{{Desc: "repair global", DoneWhen: "global repaired", Scope: "internal/ops", SpecRef: "README.md"}})
		fixture.installPriorGlobalGeneration(t)
		before := fixture.readState(t)
		prior := append([]models.IntegrationGlobalGeneration(nil), before.Goal.Integration.GlobalGenerations...)

		if _, err := SubmitVerdict(fixture.projectRoot, fixture.taskID, "APPROVED", "", fixture.reviewerID, ""); err != nil {
			t.Fatalf("SubmitVerdict() error = %v", err)
		}

		after := fixture.readState(t)
		if !reflect.DeepEqual(after.Goal.Integration.GlobalGenerations[:len(prior)], prior) {
			t.Fatalf("prior global generations changed")
		}
		if len(after.Goal.Integration.GlobalGenerations) != 2 {
			t.Fatalf("global generation count = %d, want 2", len(after.Goal.Integration.GlobalGenerations))
		}
		generation := after.Goal.Integration.GlobalGenerations[1]
		if generation.Generation != 2 || generation.AnalysisKey != fixture.analysisKey || generation.Verdict != models.IntegrationAnalysisVerdictFindings || generation.SourceCommit != fixture.sourceCommit || generation.ReportCommit != fixture.reportCommit {
			t.Fatalf("global findings generation = %#v", generation)
		}
		if after.Goal.Integration.Closure != nil {
			t.Fatalf("global findings closure = %#v, want nil", after.Goal.Integration.Closure)
		}
	})

	t.Run("global clean verdict serializes closure persistence before competing merge", func(t *testing.T) {
		scenario := setupIntegrationMutationScenario(t)
		verdictTaskID := "global-verdict"
		analystID := "integration-analyst-2"
		reviewerID := "integration-reviewer-1"
		now := time.Now().UTC()
		lease := now.Add(30 * time.Minute)
		verdictTaskRef := verdictTaskID
		state := readStateForTest(t, scenario.stateFile)
		mutationTask := state.FindTask(scenario.taskID)
		if mutationTask == nil {
			t.Fatalf("merge task %s missing", scenario.taskID)
		}
		mutationTask.Type = models.TaskTypeArchitecture
		mutationTask.RolePair = "architecture-pair"
		mutationTask.Status = models.TaskStatus("ARCHITECTURE_APPROVED")
		mutationTask.IntegrationAnalysis = nil
		priorReceipts := append([]models.IntegrationMutationReceipt(nil), state.Goal.Integration.MutationReceipts...)
		state.Tasks = []models.Task{*mutationTask}
		state.Goal.Integration = &models.IntegrationLifecycle{
			ContributingSet:  &models.IntegrationContributingSet{Scopes: []models.IntegrationScopeSnapshot{}},
			MutationReceipts: priorReceipts,
		}
		state.Tasks = append(state.Tasks, models.Task{
			ID: verdictTaskID, Type: models.TaskTypeIntegration, Description: "Global integration analysis",
			Status: models.TaskStatus("REVIEWING_INTEGRATION_ANALYSIS"), RolePair: "integration-pair", Priority: 1,
			Created: now, SpecRef: "README.md", DoneWhen: "Analysis reviewed", Scope: "Integration scope",
			AssignedTo: &analystID, ReviewCommit: &scenario.after, ReviewingBy: &reviewerID, ReviewLeaseExpires: &lease,
			HandoffEvents: []models.HandoffEvent{{Timestamp: now, Agent: analystID, Trigger: models.HandoffTriggerSubmission}},
			IntegrationAnalysis: &models.IntegrationAnalysisMetadata{
				Key: "global:1", Phase: models.IntegrationAnalysisPhaseGlobal, Generation: 1, SourceCommit: scenario.before,
			},
		})
		state.Agents[analystID] = models.Agent{
			Role: "integration-analyst", Status: models.AgentStatusWaiting, CurrentTask: &verdictTaskRef,
			LeaseExpires: &lease, Heartbeat: now, RegisteredAt: now, Provider: "codex", PID: os.Getpid(),
		}
		state.Agents[reviewerID] = models.Agent{
			Role: "integration-reviewer", Status: models.AgentStatusReviewing, CurrentTask: &verdictTaskRef,
			LeaseExpires: &lease, Heartbeat: now, RegisteredAt: now, Provider: "codex", PID: os.Getpid(),
		}
		testhelpers.WriteInitialState(t, scenario.stateFile, state)

		beforeModify := make(chan struct{})
		releaseVerdict := make(chan struct{})
		release := func() {
			select {
			case <-releaseVerdict:
			default:
				close(releaseVerdict)
			}
		}
		t.Cleanup(release)
		previousVerdictHooks := testSubmitVerdictHooks
		testSubmitVerdictHooks = &submitVerdictTestHooks{beforeModify: func() {
			close(beforeModify)
			<-releaseVerdict
		}}
		t.Cleanup(func() { testSubmitVerdictHooks = previousVerdictHooks })

		type verdictCall struct {
			result *VerdictResult
			err    error
		}
		verdictDone := make(chan verdictCall, 1)
		go func() {
			result, err := SubmitVerdict(scenario.projectRoot, verdictTaskID, "APPROVED", "", reviewerID, "")
			verdictDone <- verdictCall{result: result, err: err}
		}()
		select {
		case <-beforeModify:
		case <-time.After(5 * time.Second):
			t.Fatal("clean verdict did not reach the post-verification persistence gap")
		}

		mergeAttempted := make(chan struct{}, 1)
		previousLinearizationHook := beforeEffectiveIntegrationCompletionLinearizationTestHook
		beforeEffectiveIntegrationCompletionLinearizationTestHook = func(operation string) {
			if operation == "forward "+scenario.taskID {
				mergeAttempted <- struct{}{}
			}
		}
		t.Cleanup(func() { beforeEffectiveIntegrationCompletionLinearizationTestHook = previousLinearizationHook })
		type receiptObservation struct {
			receipt models.IntegrationMutationReceipt
			state   *models.State
			err     error
		}
		receiptReached := make(chan receiptObservation, 1)
		previousReceiptHook := integrationMutationReceiptPersistTestHook
		integrationMutationReceiptPersistTestHook = func(receipt models.IntegrationMutationReceipt) {
			state, err := db.For(scenario.stateFile).Read()
			receiptReached <- receiptObservation{receipt: receipt, state: state, err: err}
		}
		t.Cleanup(func() { integrationMutationReceiptPersistTestHook = previousReceiptHook })
		mergeDone := make(chan error, 1)
		go func() {
			_, err := MergeWorktree(scenario.projectRoot, scenario.taskID, scenario.agentID)
			mergeDone <- err
		}()
		select {
		case <-mergeAttempted:
		case <-time.After(5 * time.Second):
			t.Fatal("competing merge did not attempt completion serialization")
		}
		select {
		case observation := <-receiptReached:
			t.Fatalf("competing merge reached receipt persistence before verdict release: %#v", observation.receipt)
		case err := <-mergeDone:
			t.Fatalf("competing merge completed before verdict release: %v", err)
		case <-time.After(250 * time.Millisecond):
		}
		assertIntegrationHead(t, scenario.projectRoot, scenario.before)
		paused := readStateForTest(t, scenario.stateFile)
		if len(paused.Goal.Integration.GlobalGenerations) != 0 || paused.Goal.Integration.Closure != nil {
			t.Fatalf("verdict evidence persisted while verdict was paused: %#v", paused.Goal.Integration)
		}
		if len(paused.Goal.Integration.MutationReceipts) != 1 {
			t.Fatalf("mutation receipt count while verdict paused = %d, want 1", len(paused.Goal.Integration.MutationReceipts))
		}

		release()
		select {
		case call := <-verdictDone:
			if call.err != nil {
				t.Fatalf("SubmitVerdict() error = %v", call.err)
			}
			if call.result == nil {
				t.Fatal("SubmitVerdict() result is nil")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("SubmitVerdict did not complete after release")
		}
		var observation receiptObservation
		select {
		case observation = <-receiptReached:
		case <-time.After(5 * time.Second):
			t.Fatal("MergeWorktree did not reach receipt persistence after verdict completion")
		}
		if observation.err != nil {
			t.Fatalf("read state at receipt persistence: %v", observation.err)
		}
		assertMutationReceipt(t, observation.receipt, scenario.taskID, scenario.before, scenario.after)
		if observation.state == nil || len(observation.state.Goal.Integration.GlobalGenerations) != 1 {
			t.Fatalf("global evidence at receipt persistence = %#v, want one durable generation", observation.state)
		}
		closureAtReceipt := observation.state.Goal.Integration.Closure
		if closureAtReceipt == nil || closureAtReceipt.SourceCommit != scenario.before {
			t.Fatalf("clean closure at receipt persistence = %#v, want source %s", closureAtReceipt, scenario.before)
		}
		if len(observation.state.Goal.Integration.MutationReceipts) != 1 {
			t.Fatalf("mutation receipts before competing persistence = %#v, want prior receipt only", observation.state.Goal.Integration.MutationReceipts)
		}
		select {
		case err := <-mergeDone:
			if err != nil {
				t.Fatalf("MergeWorktree() error = %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("MergeWorktree did not complete after verdict persistence")
		}

		assertIntegrationHead(t, scenario.projectRoot, scenario.after)
		after := readStateForTest(t, scenario.stateFile)
		if len(after.Goal.Integration.GlobalGenerations) != 1 {
			t.Fatalf("global generation count = %d, want 1", len(after.Goal.Integration.GlobalGenerations))
		}
		generation := after.Goal.Integration.GlobalGenerations[0]
		if generation.Verdict != models.IntegrationAnalysisVerdictClean || generation.SourceCommit != scenario.before || generation.ReportCommit != scenario.after {
			t.Fatalf("global clean generation = %#v", generation)
		}
		if len(after.Goal.Integration.MutationReceipts) != 2 {
			t.Fatalf("mutation receipt count = %d, want 2", len(after.Goal.Integration.MutationReceipts))
		}
		assertMutationReceipt(t, after.Goal.Integration.MutationReceipts[1], scenario.taskID, scenario.before, scenario.after)
		decision := evaluateProgress(t, after, pipeline.SlicedIntegrationCapability{Available: true}, scenario.after)
		if decision.IntegrationComplete {
			t.Fatalf("old-source clean closure remained effective after merge: %#v", decision)
		}
	})

	t.Run("stale global clean source records evidence without clean closure", func(t *testing.T) {
		fixture := newSubmitVerdictIntegrationFixture(t, models.IntegrationAnalysisPhaseGlobal, nil)
		previousVerifier := verifyCleanIntegrationSourceForVerdict
		verifyCleanIntegrationSourceForVerdict = func(_ string, sourceCommit string) (cleanIntegrationSourceVerification, error) {
			return cleanIntegrationSourceVerification{SourceCommit: sourceCommit, IntegrationHEAD: "new-head", Effective: false}, nil
		}
		t.Cleanup(func() { verifyCleanIntegrationSourceForVerdict = previousVerifier })

		if _, err := SubmitVerdict(fixture.projectRoot, fixture.taskID, "APPROVED", "", fixture.reviewerID, ""); err != nil {
			t.Fatalf("SubmitVerdict() error = %v", err)
		}
		after := fixture.readState(t)
		if len(after.Goal.Integration.GlobalGenerations) != 1 || after.Goal.Integration.GlobalGenerations[0].Verdict != models.IntegrationAnalysisVerdictClean {
			t.Fatalf("stale clean generation = %#v", after.Goal.Integration.GlobalGenerations)
		}
		if after.Goal.Integration.Closure != nil {
			t.Fatalf("stale clean closure = %#v, want nil", after.Goal.Integration.Closure)
		}
		if analyst := after.Agents[fixture.analystID]; analyst.Status != models.AgentStatusWaiting || analyst.CurrentTask != nil {
			t.Fatalf("completed verdict analyst ownership = %#v, want waiting without current task", analyst)
		}
	})

	t.Run("role phase mismatch and duplicate projection fail closed", func(t *testing.T) {
		t.Run("role phase mismatch", func(t *testing.T) {
			fixture := newSubmitVerdictIntegrationFixture(t, models.IntegrationAnalysisPhaseSlice, nil)
			fixture.mutateState(t, func(state *models.State) {
				task := state.FindTask(fixture.taskID)
				task.RolePair = "integration-pair"
				task.Status = models.TaskStatus("REVIEWING_INTEGRATION_ANALYSIS")
			})
			before := fixture.readState(t)

			_, err := SubmitVerdict(fixture.projectRoot, fixture.taskID, "APPROVED", "", fixture.reviewerID, "")
			testhelpers.RequireErrorContains(t, err, "phase")
			assertSubmitVerdictTransactionUnchanged(t, before, fixture.readState(t), fixture.taskID, fixture.reviewerID)
		})

		t.Run("duplicate slice projection", func(t *testing.T) {
			fixture := newSubmitVerdictIntegrationFixture(t, models.IntegrationAnalysisPhaseSlice, nil)
			fixture.mutateState(t, func(state *models.State) {
				state.Goal.Integration.Coverage = append(state.Goal.Integration.Coverage, models.IntegrationCoverageRecord{
					PlanTaskID: fixture.planTaskID,
					Kind:       models.IntegrationCoverageSliceReport,
					SliceReport: &models.IntegrationSliceReport{
						AnalysisTaskID: fixture.taskID,
						AnalysisKey:    fixture.analysisKey,
						Verdict:        models.IntegrationAnalysisVerdictClean,
						SourceCommit:   fixture.sourceCommit,
						ReportCommit:   fixture.reportCommit,
					},
				})
			})
			before := fixture.readState(t)

			_, err := SubmitVerdict(fixture.projectRoot, fixture.taskID, "APPROVED", "", fixture.reviewerID, "")
			testhelpers.RequireErrorContains(t, err, "duplicate")
			assertSubmitVerdictTransactionUnchanged(t, before, fixture.readState(t), fixture.taskID, fixture.reviewerID)
		})
	})

	t.Run("candidate and transition validation abort the complete verdict transaction", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			corrupt func(*models.State)
			wantErr string
		}{
			{
				name: "malformed newly appended evidence",
				corrupt: func(state *models.State) {
					state.Goal.Integration.Coverage[len(state.Goal.Integration.Coverage)-1].SliceReport.ReportCommit = ""
				},
				wantErr: "report commit is empty",
			},
			{
				name: "prior nested evidence rewrite",
				corrupt: func(state *models.State) {
					state.Goal.Integration.ContributingSet.Scopes[0].RootTaskIDs[0] = "rewritten-root"
				},
				wantErr: "contributing set cannot change",
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				fixture := newSubmitVerdictIntegrationFixture(t, models.IntegrationAnalysisPhaseSlice, nil)
				previousHooks := testSubmitVerdictHooks
				testSubmitVerdictHooks = &submitVerdictTestHooks{beforeValidation: tc.corrupt}
				t.Cleanup(func() { testSubmitVerdictHooks = previousHooks })
				before := fixture.readState(t)

				_, err := SubmitVerdict(fixture.projectRoot, fixture.taskID, "APPROVED", "", fixture.reviewerID, "")
				testhelpers.RequireErrorContains(t, err, tc.wantErr)
				assertSubmitVerdictTransactionUnchanged(t, before, fixture.readState(t), fixture.taskID, fixture.reviewerID)
			})
		}
	})

	t.Run("ordinary non integration approval preserves existing behavior", func(t *testing.T) {
		tmpDir := t.TempDir()
		stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
		now := time.Now().UTC()
		state := testhelpers.CreateValidState()
		state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-ordinary", models.TaskStatusReviewing, now)}
		state.Agents["code-reviewer-1"] = models.Agent{Role: "code-reviewer", Status: models.AgentStatusWorking}
		testhelpers.WriteInitialState(t, stateFile, state)

		if _, err := SubmitVerdict(tmpDir, "task-ordinary", "APPROVED", "", "code-reviewer-1", ""); err != nil {
			t.Fatalf("SubmitVerdict() error = %v", err)
		}
		after, err := db.New(stateFile).Read()
		if err != nil {
			t.Fatalf("Read() error = %v", err)
		}
		if got := after.FindTask("task-ordinary").Status; got != models.TaskStatusApproved {
			t.Fatalf("ordinary approval status = %s, want %s", got, models.TaskStatusApproved)
		}
		if after.Goal.Integration != nil {
			t.Fatalf("ordinary approval lifecycle = %#v, want nil", after.Goal.Integration)
		}
	})
}

func TestSubmitVerdict_CleanGlobalClassificationReadFailureFailsClosed(t *testing.T) {
	fixture := newSubmitVerdictIntegrationFixture(t, models.IntegrationAnalysisPhaseGlobal, nil)
	before := fixture.readState(t)
	forcedErr := fmt.Errorf("forced classification read failure")
	readCalls := 0
	previousReader := readTaskStateForSubmitVerdict
	readTaskStateForSubmitVerdict = func(bb *db.Blackboard, taskID string) (*models.State, *models.Task, error) {
		readCalls++
		if readCalls == 1 {
			return nil, nil, forcedErr
		}
		return readTaskState(bb, taskID)
	}
	t.Cleanup(func() { readTaskStateForSubmitVerdict = previousReader })

	result, err := SubmitVerdict(fixture.projectRoot, fixture.taskID, "APPROVED", "", fixture.reviewerID, "")
	if result != nil {
		t.Fatalf("SubmitVerdict() result = %#v, want nil", result)
	}
	if err == nil || !strings.Contains(err.Error(), forcedErr.Error()) {
		t.Fatalf("SubmitVerdict() error = %v, want %q", err, forcedErr)
	}
	if readCalls != 1 {
		t.Fatalf("task-state reads = %d, want 1", readCalls)
	}
	assertSubmitVerdictTransactionUnchanged(t, before, fixture.readState(t), fixture.taskID, fixture.reviewerID)
	if _, _, err := readTaskStateForSubmitVerdict(db.For(fixture.statePath), fixture.taskID); err != nil {
		t.Fatalf("subsequent task-state read error = %v, want success", err)
	}
	if readCalls != 2 {
		t.Fatalf("task-state reads after subsequent lookup = %d, want 2", readCalls)
	}
}

type submitVerdictIntegrationFixture struct {
	projectRoot  string
	statePath    string
	taskID       string
	reviewerID   string
	analystID    string
	planTaskID   string
	analysisKey  string
	sourceCommit string
	reportCommit string
}

func newSubmitVerdictIntegrationFixture(t *testing.T, phase models.IntegrationAnalysisPhase, output []models.OutputEntry) submitVerdictIntegrationFixture {
	t.Helper()
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.CreateSpecFile(t, projectRoot, "vision.md", "# Test vision\n")

	rolePair := "slice-integration-pair"
	reviewingStatus := models.TaskStatus("REVIEWING_SLICE_INTEGRATION_ANALYSIS")
	planTaskID := "plan-slice"
	analysisKey := "slice:plan-slice"
	generation := 0
	if phase == models.IntegrationAnalysisPhaseGlobal {
		rolePair = "integration-pair"
		reviewingStatus = models.TaskStatus("REVIEWING_INTEGRATION_ANALYSIS")
		planTaskID = ""
		analysisKey = "global:1"
		generation = 1
	}

	taskID := "analysis-task"
	reviewerID := "integration-reviewer-1"
	analystID := "integration-analyst-1"
	sourceCommit := "analyzed-source-commit"
	reportCommit := "analyst-report-commit"
	now := time.Now().UTC()
	lease := now.Add(30 * time.Minute)
	taskRef := taskID
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{{
		ID:                 taskID,
		Type:               models.TaskTypeIntegration,
		Description:        "Integration analysis",
		Status:             reviewingStatus,
		RolePair:           rolePair,
		Priority:           1,
		Created:            now,
		SpecRef:            "README.md",
		DoneWhen:           "Analysis reviewed",
		Scope:              "Integration scope",
		Output:             output,
		AssignedTo:         &analystID,
		ReviewCommit:       &reportCommit,
		ReviewingBy:        &reviewerID,
		ReviewLeaseExpires: &lease,
		HandoffEvents:      []models.HandoffEvent{{Timestamp: now, Agent: analystID, Trigger: models.HandoffTriggerSubmission}},
		IntegrationAnalysis: &models.IntegrationAnalysisMetadata{
			Key:                   analysisKey,
			Phase:                 phase,
			Generation:            generation,
			OriginatingPlanTaskID: planTaskID,
			RootTaskIDs:           sliceRoots(phase),
			SourceCommit:          sourceCommit,
		},
	}}
	state.Agents[analystID] = models.Agent{
		Role:         "integration-analyst",
		Status:       models.AgentStatusWaiting,
		CurrentTask:  &taskRef,
		LeaseExpires: &lease,
		Heartbeat:    now,
		RegisteredAt: now,
		Provider:     "codex",
		PID:          os.Getpid(),
	}
	state.Agents[reviewerID] = models.Agent{
		Role:         "integration-reviewer",
		Status:       models.AgentStatusReviewing,
		CurrentTask:  &taskRef,
		LeaseExpires: &lease,
		Heartbeat:    now,
		RegisteredAt: now,
		Provider:     "codex",
		PID:          os.Getpid(),
	}
	state.Goal.Integration = &models.IntegrationLifecycle{}
	if phase == models.IntegrationAnalysisPhaseSlice {
		state.Goal.Integration.ContributingSet = &models.IntegrationContributingSet{Scopes: []models.IntegrationScopeSnapshot{
			{PlanTaskID: "plan-prior", RootTaskIDs: []string{"root-prior"}},
			{PlanTaskID: planTaskID, RootTaskIDs: sliceRoots(phase)},
		}}
		state.Goal.Integration.Coverage = []models.IntegrationCoverageRecord{{
			PlanTaskID: "plan-prior",
			Kind:       models.IntegrationCoverageApprovalAttestation,
			ApprovalAttestations: []models.IntegrationApprovalAttestation{{
				ReviewedTaskID: "prior-coding", AcceptanceCriteria: "prior done", ReviewedCommit: "prior-review",
				Approver: "prior-reviewer", Validation: []string{"go test ./..."}, MergeCommit: "prior-merge",
			}},
		}}
		state.Goal.Integration.MutationReceipts = []models.IntegrationMutationReceipt{{TaskID: "prior-merge-task", BeforeCommit: "before", AfterCommit: "after"}}
	}
	testhelpers.WriteInitialState(t, statePath, state)
	return submitVerdictIntegrationFixture{
		projectRoot: projectRoot, statePath: statePath, taskID: taskID, reviewerID: reviewerID, analystID: analystID,
		planTaskID: planTaskID, analysisKey: analysisKey, sourceCommit: sourceCommit, reportCommit: reportCommit,
	}
}

func sliceRoots(phase models.IntegrationAnalysisPhase) []string {
	if phase != models.IntegrationAnalysisPhaseSlice {
		return nil
	}
	return []string{"root-a", "root-b"}
}

func (fixture submitVerdictIntegrationFixture) readState(t *testing.T) *models.State {
	t.Helper()
	state, err := db.New(fixture.statePath).Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	return state
}

func (fixture submitVerdictIntegrationFixture) mutateState(t *testing.T, mutate func(*models.State)) {
	t.Helper()
	state := fixture.readState(t)
	mutate(state)
	testhelpers.WriteInitialState(t, fixture.statePath, state)
}

func (fixture *submitVerdictIntegrationFixture) installPriorGlobalGeneration(t *testing.T) {
	t.Helper()
	fixture.mutateState(t, func(state *models.State) {
		current := state.FindTask(fixture.taskID)
		current.IntegrationAnalysis.Key = "global:2"
		current.IntegrationAnalysis.Generation = 2
		fixture.analysisKey = "global:2"
		priorReport := "prior-global-report"
		state.Tasks = append([]models.Task{{
			ID: "global-analysis-1", Type: models.TaskTypeIntegration, Description: "Prior global analysis",
			Status: models.TaskStatus("INTEGRATION_ANALYSIS_APPROVED"), RolePair: "integration-pair", Priority: 1,
			Created: time.Now().UTC(), SpecRef: "README.md", DoneWhen: "Prior analysis reviewed", Scope: "Integration scope",
			ReviewCommit:        &priorReport,
			HandoffEvents:       []models.HandoffEvent{{Timestamp: time.Now().UTC(), Agent: "integration-analyst-1", Trigger: models.HandoffTriggerSubmission}},
			IntegrationAnalysis: &models.IntegrationAnalysisMetadata{Key: "global:1", Phase: models.IntegrationAnalysisPhaseGlobal, Generation: 1, SourceCommit: "prior-global-source"},
		}}, state.Tasks...)
		state.Goal.Integration.GlobalGenerations = []models.IntegrationGlobalGeneration{{
			Generation: 1, AnalysisTaskID: "global-analysis-1", AnalysisKey: "global:1",
			Verdict: models.IntegrationAnalysisVerdictFindings, SourceCommit: "prior-global-source", ReportCommit: priorReport,
		}}
	})
}

func enableSubmitVerdictIntegrationQuorum(t *testing.T, projectRoot, rolePair string) {
	t.Helper()
	pipelinePath := filepath.Join(projectRoot, paths.ProjectDirName(), "pipeline.yaml")
	content, err := os.ReadFile(pipelinePath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", pipelinePath, err)
	}
	pairHeader := "    " + rolePair + ":\n      doer: integration-analyst\n      reviewer: integration-reviewer\n"
	pairWithQuorum := pairHeader + "      review-policy:\n        quorum: 2\n        provider-diversity: preferred\n"
	updated := strings.Replace(string(content), pairHeader, pairWithQuorum, 1)
	cleanState := "        clean: SLICE_INTEGRATION_ANALYSIS_CLEAN\n"
	updated = strings.Replace(updated, cleanState, "        partially-approved: SLICE_INTEGRATION_ANALYSIS_PARTIALLY_APPROVED\n        reviewing-2: REVIEWING_SLICE_INTEGRATION_ANALYSIS_2\n", 1)
	if updated == string(content) {
		t.Fatalf("pipeline role pair %s was not updated", rolePair)
	}
	if err := os.WriteFile(pipelinePath, []byte(updated), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", pipelinePath, err)
	}
}

func assertSubmitVerdictTransactionUnchanged(t *testing.T, before, after *models.State, taskID, reviewerID string) {
	t.Helper()
	if !reflect.DeepEqual(after.FindTask(taskID), before.FindTask(taskID)) {
		t.Fatalf("task changed after rejected verdict transaction:\n got: %#v\nwant: %#v", after.FindTask(taskID), before.FindTask(taskID))
	}
	if !reflect.DeepEqual(after.Goal.Integration, before.Goal.Integration) {
		t.Fatalf("lifecycle changed after rejected verdict transaction:\n got: %#v\nwant: %#v", after.Goal.Integration, before.Goal.Integration)
	}
	if !reflect.DeepEqual(after.Agents[reviewerID], before.Agents[reviewerID]) {
		t.Fatalf("reviewer changed after rejected verdict transaction:\n got: %#v\nwant: %#v", after.Agents[reviewerID], before.Agents[reviewerID])
	}
}

func taskStatus(task *models.Task) models.TaskStatus {
	if task == nil {
		return ""
	}
	return task.Status
}

// verdictPayloadReviewCommit is the immutable boundary the authenticated
// payload fixtures review, so only the field under test is ever malformed.
var verdictPayloadReviewCommit = strings.Repeat("ab", 20)

// setupVerdictPayloadFixture builds a REVIEWING task and a registered reviewer
// whose authority holds, so every rejection below is structural rather than an
// authority or boundary refusal.
func setupVerdictPayloadFixture(t *testing.T) (projectRoot, stateFile string, authority models.AgentAuthority) {
	t.Helper()
	projectRoot = t.TempDir()
	stateFile, _ = testhelpers.SetupLizaDir(t, projectRoot)
	authority = models.AgentAuthority{ID: "code-reviewer-1", Generation: testhelpers.TestAgentGeneration}

	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, time.Now().UTC())
	task.ReviewCommit = &verdictPayloadReviewCommit
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{task}
	state.Agents[authority.ID] = models.Agent{
		Role:       models.RoleCodeReviewer,
		Status:     models.AgentStatusReviewing,
		Generation: authority.Generation,
		CurrentTask: func() *string {
			id := task.ID
			return &id
		}(),
	}
	testhelpers.WriteInitialState(t, stateFile, state)
	return projectRoot, stateFile, authority
}

// requireVerdictDiagnostic asserts err is the INVALID_INPUT lifecycle result
// carrying exactly the named field diagnostic, and that no diagnostic echoes a
// rejected value.
func requireVerdictDiagnostic(t *testing.T, err error, field, constraint, valueClass string) {
	t.Helper()
	if err == nil {
		t.Fatal("SubmitVerdict() error = nil, want INVALID_INPUT")
	}
	var lifecycleErr *LifecycleError
	if !stderrors.As(err, &lifecycleErr) {
		t.Fatalf("error %v is not a *LifecycleError", err)
	}
	outcome := lifecycleErr.Outcome
	if outcome.Outcome != models.LifecycleInvalidInput {
		t.Fatalf("outcome = %q, want %q", outcome.Outcome, models.LifecycleInvalidInput)
	}
	if outcome.SafeAction != "correct_input" {
		t.Errorf("safe_action = %q, want correct_input", outcome.SafeAction)
	}
	if outcome.Effects != "none" {
		t.Errorf("effects = %q, want none", outcome.Effects)
	}
	if len(outcome.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v, want exactly one entry", outcome.Diagnostics)
	}
	got := outcome.Diagnostics[0]
	want := models.FieldDiagnostic{
		SchemaVersion: 1, Field: field, Constraint: constraint,
		ValueClass: valueClass, SafeAction: models.FieldDiagnosticCorrectInput,
	}
	if got != want {
		t.Fatalf("diagnostic = %#v, want %#v", got, want)
	}
}

func TestSubmitVerdictInvalidPayloadDiagnostics(t *testing.T) {
	// The observed run's failure: a verdict reason one byte past the durable
	// state-text limit. It reaches the schema as /reason, not as prose.
	oversizedReason := strings.Repeat("r", statehygiene.MaxStateTextBytes+1)

	tests := []struct {
		name         string
		verdict      string
		reason       string
		reviewCommit string
		field        string
		constraint   string
		valueClass   string
	}{
		{
			name: "oversized rejection reason", verdict: "REJECTED", reason: oversizedReason,
			reviewCommit: verdictPayloadReviewCommit,
			field:        "/reason", constraint: fmt.Sprintf("must be at most %d bytes", statehygiene.MaxStateTextBytes),
			valueClass: models.FieldValueClassOversized,
		},
		{
			name: "lowercase verdict", verdict: "approved", reviewCommit: verdictPayloadReviewCommit,
			field: "/verdict", constraint: "must be APPROVED or REJECTED",
			valueClass: models.FieldValueClassUnknownEnum,
		},
		{
			name: "non-hex review commit", verdict: "APPROVED", reviewCommit: strings.Repeat("a", 39) + "z",
			field: "/review_commit", constraint: "must be the full immutable commit SHA reviewed",
			valueClass: models.FieldValueClassMalformed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectRoot, stateFile, authority := setupVerdictPayloadFixture(t)
			before, err := os.ReadFile(stateFile)
			if err != nil {
				t.Fatalf("ReadFile(%s) error = %v", stateFile, err)
			}

			_, err = SubmitVerdictWithAuthority(projectRoot, "task-1", tt.verdict, tt.reason, authority, "", tt.reviewCommit)
			requireVerdictDiagnostic(t, err, tt.field, tt.constraint, tt.valueClass)

			after, readErr := os.ReadFile(stateFile)
			if readErr != nil {
				t.Fatalf("ReadFile(%s) error = %v", stateFile, readErr)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("state file changed; a structural rejection must happen before the state lock")
			}
		})
	}
}

func TestSubmitVerdictPreflightParity(t *testing.T) {
	// Both boundaries must accept or reject the very same canonical object,
	// so a caller cannot pass validate-payload and fail submit-verdict, or
	// the reverse, for a structural reason (AC-158-3).
	fixtures := []struct {
		name    string
		verdict string
		reason  string
	}{
		{name: "valid fixture", verdict: "REJECTED", reason: "the boundary case is unhandled"},
		{name: "invalid fixture", verdict: "rejected", reason: "the boundary case is unhandled"},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			projectRoot, _, authority := setupVerdictPayloadFixture(t)
			payload := payloadschema.SubmitVerdictPayload(
				"task-1", fixture.verdict, fixture.reason, authority.ID, "standard", verdictPayloadReviewCommit)

			version, preflight, err := payloadschema.Validate(payloadschema.SubmitVerdictOperation, payload)
			if err != nil {
				t.Fatalf("payloadschema.Validate() error = %v", err)
			}
			if version != 1 {
				t.Errorf("schema version = %d, want 1", version)
			}

			_, mutationErr := SubmitVerdictWithAuthority(
				projectRoot, "task-1", fixture.verdict, fixture.reason, authority, "standard", verdictPayloadReviewCommit)

			var lifecycleErr *LifecycleError
			var mutation []models.FieldDiagnostic
			if stderrors.As(mutationErr, &lifecycleErr) && lifecycleErr.Outcome.Outcome == models.LifecycleInvalidInput {
				mutation = lifecycleErr.Outcome.Diagnostics
			}
			if !reflect.DeepEqual(preflight, mutation) {
				t.Fatalf("structural verdict diverged:\npreflight: %#v\nmutation:  %#v", preflight, mutation)
			}
			if len(preflight) == 0 && mutationErr != nil {
				t.Fatalf("valid payload rejected by the mutation boundary: %v", mutationErr)
			}
		})
	}
}

func TestSubmitVerdictValidPayloadUnchanged(t *testing.T) {
	tests := []struct {
		name       string
		verdict    string
		reason     string
		wantStatus models.TaskStatus
		wantEvent  string
	}{
		{
			name: "approved", verdict: "APPROVED",
			wantStatus: models.TaskStatusApproved, wantEvent: models.TaskEventApproved,
		},
		{
			name: "rejected", verdict: "REJECTED", reason: "Missing error handling",
			wantStatus: models.TaskStatusRejected, wantEvent: models.TaskEventRejected,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectRoot, stateFile, _ := setupVerdictPayloadFixture(t)

			result, err := SubmitVerdict(projectRoot, "task-1", tt.verdict, tt.reason, "code-reviewer-1", "")
			if err != nil {
				t.Fatalf("SubmitVerdict() error = %v", err)
			}
			if result.Verdict != tt.verdict {
				t.Errorf("Verdict = %q, want %q", result.Verdict, tt.verdict)
			}
			if result.Reason != tt.reason {
				t.Errorf("Reason = %q, want %q", result.Reason, tt.reason)
			}
			if result.Outcome != models.LifecycleCompleted || result.SafeAction != "continue" {
				t.Errorf("outcome = %q/%q, want COMPLETED/continue", result.Outcome, result.SafeAction)
			}
			if result.EscalatedToBlocked {
				t.Error("EscalatedToBlocked = true, want false: this task carries no gate")
			}

			readState, err := db.New(stateFile).Read()
			if err != nil {
				t.Fatalf("Read() error = %v", err)
			}
			task := readState.FindTask("task-1")
			if task == nil {
				t.Fatal("task-1 not found")
			}
			if task.Status != tt.wantStatus {
				t.Errorf("Status = %v, want %v", task.Status, tt.wantStatus)
			}
			if len(task.History) != 1 {
				t.Fatalf("history = %#v, want exactly one entry", task.History)
			}
			if task.History[0].Event != tt.wantEvent {
				t.Errorf("history event = %q, want %q", task.History[0].Event, tt.wantEvent)
			}
		})
	}
}

// gateVerdictRole names the doer/reviewer pair of one reviewed task type so the
// churn gate is exercised on a coding task and a planning task alike.
type gateVerdictRole struct {
	name         string
	taskType     models.TaskType
	rolePair     string
	reviewing    models.TaskStatus
	rejected     models.TaskStatus
	doer         string
	doerRole     string
	reviewer     string
	reviewerRole string
}

var gateVerdictRoles = []gateVerdictRole{
	{
		name: "coding task", taskType: models.TaskTypeCoding, rolePair: "coding-pair",
		reviewing: models.TaskStatusReviewing, rejected: models.TaskStatusRejected,
		doer: "coder-1", doerRole: models.RoleCoder, reviewer: "code-reviewer-1", reviewerRole: models.RoleCodeReviewer,
	},
	{
		name: "planning task", taskType: models.TaskTypePlanning, rolePair: "code-planning-pair",
		reviewing: models.TaskStatusReviewingCodingPlan, rejected: models.TaskStatusCodingPlanRejected,
		doer: "code-planner-1", doerRole: models.RoleCodePlanner, reviewer: "code-plan-reviewer-1", reviewerRole: models.RoleCodePlanReviewer,
	},
}

const gateVerdictTaskID = "task-1"

// gateVerdictState builds one project whose task is under review after
// priorRejections durable rejections, the earliest one an hour apart from the
// next so first_rejection_at is distinguishable. The mutate hook may adjust
// the state (config threshold, counters, a prior RCA cycle) before it is
// written.
func gateVerdictState(t *testing.T, role gateVerdictRole, priorRejections int, now time.Time, mutate func(*models.State, *models.Task)) (string, string) {
	t.Helper()
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	task := models.Task{
		ID: gateVerdictTaskID, Type: role.taskType, RolePair: role.rolePair, Description: "Gated task",
		Status: role.reviewing, Priority: 1, Created: now, SpecRef: "README.md", DoneWhen: "Task is complete", Scope: "Test scope",
		AssignedTo: testhelpers.StringPtr(role.doer), LeaseExpires: testhelpers.TimePtr(now.Add(30 * time.Minute)),
		BaseCommit: testhelpers.StringPtr("abc1234"), Worktree: testhelpers.StringPtr(".worktrees/" + gateVerdictTaskID),
		ReviewCommit: testhelpers.StringPtr("review123"), ReviewingBy: testhelpers.StringPtr(role.reviewer),
		ReviewLeaseExpires:  testhelpers.TimePtr(now.Add(30 * time.Minute)),
		HandoffEvents:       []models.HandoffEvent{{Timestamp: now, Agent: role.doer, Trigger: models.HandoffTriggerSubmission}},
		ReviewCyclesCurrent: priorRejections, ReviewCyclesTotal: priorRejections,
	}
	for i := 0; i < priorRejections; i++ {
		reason := fmt.Sprintf("rejection %d", i+1)
		task.History = append(task.History, models.TaskHistoryEntry{
			Time: now.Add(-time.Duration(priorRejections-i) * time.Hour), Event: models.TaskEventRejected,
			Agent: testhelpers.StringPtr(role.reviewer), Reason: &reason,
		})
	}
	if mutate != nil {
		mutate(state, &task)
	}
	state.Tasks = []models.Task{task}
	taskRef := gateVerdictTaskID
	state.Agents[role.doer] = models.Agent{Role: role.doerRole, Status: models.AgentStatusWaiting, CurrentTask: &taskRef}
	state.Agents[role.reviewer] = models.Agent{Role: role.reviewerRole, Status: models.AgentStatusReviewing, CurrentTask: &taskRef}
	testhelpers.WriteInitialState(t, stateFile, state)
	return tmpDir, stateFile
}

// requeueForReview puts a rejected task back under review by the same
// reviewer, as a resubmission and reviewer claim would, so a sequence of
// verdicts can be driven against one durable rejection history.
func requeueForReview(t *testing.T, bb *db.Blackboard, role gateVerdictRole) {
	t.Helper()
	if err := bb.Modify(func(state *models.State) error {
		task := state.FindTask(gateVerdictTaskID)
		if task == nil {
			return fmt.Errorf("task %s not found", gateVerdictTaskID)
		}
		now := time.Now().UTC()
		task.Status = role.reviewing
		task.ReviewCommit = testhelpers.StringPtr("review123")
		task.ReviewingBy = testhelpers.StringPtr(role.reviewer)
		task.ReviewLeaseExpires = testhelpers.TimePtr(now.Add(30 * time.Minute))
		taskRef := gateVerdictTaskID
		state.Agents[role.reviewer] = models.Agent{Role: role.reviewerRole, Status: models.AgentStatusReviewing, CurrentTask: &taskRef}
		return nil
	}); err != nil {
		t.Fatalf("requeue for review: %v", err)
	}
}

func readGateVerdictTask(t *testing.T, stateFile string) (*models.State, *models.Task) {
	t.Helper()
	state, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	task := state.FindTask(gateVerdictTaskID)
	if task == nil {
		t.Fatalf("task %s not found", gateVerdictTaskID)
	}
	return state, task
}

// assertGatedTask checks the whole gate escalation: BLOCKED with the typed
// reason, a seeded record for the current cycle, the released doer and
// reviewer, and a TaskEventBlocked entry carrying every declared detail key.
func assertGatedTask(t *testing.T, state *models.State, task *models.Task, role gateVerdictRole, threshold, rejectionCount int, firstRejectionAt time.Time) {
	t.Helper()
	if task.Status != models.TaskStatusBlocked {
		t.Fatalf("Status = %s, want BLOCKED", task.Status)
	}
	if task.BlockedReason == nil || !strings.HasPrefix(*task.BlockedReason, models.BlockedReasonRejectionRCARequired) {
		t.Fatalf("BlockedReason = %v, want prefix %q", task.BlockedReason, models.BlockedReasonRejectionRCARequired)
	}
	if len(task.BlockedQuestions) == 0 {
		t.Fatal("BlockedQuestions empty, want the gate's questions")
	}
	if !task.RejectionRCAGateOpen() {
		t.Fatal("RejectionRCAGateOpen() = false, want an open gate")
	}
	record := task.RejectionRCA
	if record.SchemaVersion != models.RejectionRCASchemaVersion || record.Threshold != threshold || record.RejectionCount != rejectionCount {
		t.Fatalf("seeded record = %+v, want schema %d threshold %d rejection_count %d", record, models.RejectionRCASchemaVersion, threshold, rejectionCount)
	}
	if record.GatedAt.IsZero() || record.GatingCommit != "review123" {
		t.Fatalf("seeded record gated_at/gating_commit = %v/%q, want set/review123", record.GatedAt, record.GatingCommit)
	}
	if record.Fingerprint != "" || record.Summary != "" || len(record.Contributions) != 0 || record.RecordedAt != nil || record.RecordedBy != "" {
		t.Fatalf("seeded record carries caller fields: %+v", record)
	}
	if task.ReviewCyclesTotal != rejectionCount || task.DurableRejectionCount() != rejectionCount {
		t.Fatalf("durable count = %d/%d, want %d", task.ReviewCyclesTotal, task.DurableRejectionCount(), rejectionCount)
	}
	if task.AssignedTo != nil || task.LeaseExpires != nil || task.ReviewingBy != nil || task.ReviewLeaseExpires != nil {
		t.Fatalf("assignment/lease not cleared: assigned_to=%v lease=%v reviewing_by=%v review_lease=%v", task.AssignedTo, task.LeaseExpires, task.ReviewingBy, task.ReviewLeaseExpires)
	}
	assertReleasedAgent(t, state, role.doer)
	assertReleasedAgent(t, state, role.reviewer)

	last := task.History[len(task.History)-1]
	if last.Event != models.TaskEventBlocked {
		t.Fatalf("last history event = %s, want %s", last.Event, models.TaskEventBlocked)
	}
	if last.Agent == nil || *last.Agent != role.reviewer || last.Reason == nil || *last.Reason != *task.BlockedReason {
		t.Fatalf("blocked entry agent/reason = %v/%v, want %s/%q", last.Agent, last.Reason, role.reviewer, *task.BlockedReason)
	}
	want := map[string]string{
		"blocked_class":      models.BlockedReasonRejectionRCARequired,
		"threshold":          fmt.Sprint(threshold),
		"rejection_count":    fmt.Sprint(rejectionCount),
		"gated_at":           record.GatedAt.Format(time.RFC3339),
		"first_rejection_at": firstRejectionAt.Format(time.RFC3339),
	}
	for key, value := range want {
		got, ok := last.Extra[key]
		if !ok || fmt.Sprint(got) != value {
			t.Errorf("blocked entry extra[%q] = %v (present=%v), want %q", key, got, ok, value)
		}
	}
	if len(last.Extra) != len(want) {
		t.Errorf("blocked entry extra keys = %v, want exactly %d declared keys", last.Extra, len(want))
	}
}

func TestSubmitVerdictRejectionRCAGate(t *testing.T) {
	for _, role := range gateVerdictRoles {
		t.Run(role.name+" gates on the threshold-crossing rejection", func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Second)
			tmpDir, stateFile := gateVerdictState(t, role, 3, now, nil)

			result, err := SubmitVerdict(tmpDir, gateVerdictTaskID, "REJECTED", "fourth rejection", role.reviewer, "")
			if err != nil {
				t.Fatalf("SubmitVerdict() error: %v", err)
			}
			state, task := readGateVerdictTask(t, stateFile)
			assertGatedTask(t, state, task, role, models.DefaultHighChurnRejectionThreshold, 4, now.Add(-3*time.Hour))
			if !result.EscalatedToBlocked || !result.RejectionRCAGated || result.BlockedReason != *task.BlockedReason {
				t.Fatalf("result = %+v, want escalated_to_blocked with the task's blocked_reason and rejection_rca_gated", result)
			}
			if result.Outcome != models.LifecycleCompleted || result.SafeAction != "continue" {
				t.Fatalf("outcome = %s/%s, want COMPLETED/continue", result.Outcome, result.SafeAction)
			}
			if task.RejectionReason == nil || *task.RejectionReason != "fourth rejection" {
				t.Fatalf("RejectionReason = %v, want the gating rejection's reason", task.RejectionReason)
			}
			rejected := historyEntries(task, models.TaskEventRejected)
			if len(rejected) != 4 || rejected[3].Commit == nil || *rejected[3].Commit != "review123" {
				t.Fatalf("rejected entries = %d, want 4 with the gating review commit on the last", len(rejected))
			}
		})

		t.Run(role.name+" prior rejection does not gate", func(t *testing.T) {
			now := time.Now().UTC()
			tmpDir, stateFile := gateVerdictState(t, role, 2, now, nil)

			result, err := SubmitVerdict(tmpDir, gateVerdictTaskID, "REJECTED", "third rejection", role.reviewer, "")
			if err != nil {
				t.Fatalf("SubmitVerdict() error: %v", err)
			}
			if result.EscalatedToBlocked || result.RejectionRCAGated || result.BlockedReason != "" {
				t.Fatalf("result = %+v, want a plain rejection", result)
			}
			_, task := readGateVerdictTask(t, stateFile)
			if task.Status != role.rejected || task.RejectionRCA != nil || task.ReviewCyclesTotal != 3 {
				t.Fatalf("status/record/total = %s/%v/%d, want %s/nil/3", task.Status, task.RejectionRCA, task.ReviewCyclesTotal, role.rejected)
			}
			if entries := historyEntries(task, models.TaskEventBlocked); len(entries) != 0 {
				t.Fatalf("blocked entries = %d, want none", len(entries))
			}
		})
	}

	role := gateVerdictRoles[0]
	t.Run("configured threshold of 6 gates at 6", func(t *testing.T) {
		configure := func(state *models.State, task *models.Task) {
			state.Config.HighChurnRejectionThreshold = 6
			task.ReviewCyclesCurrent = 1 // a later attempt: the review budget is not the path that fires
		}
		now := time.Now().UTC().Truncate(time.Second)
		tmpDir, stateFile := gateVerdictState(t, role, 3, now, configure)
		result, err := SubmitVerdict(tmpDir, gateVerdictTaskID, "REJECTED", "fourth rejection", role.reviewer, "")
		if err != nil {
			t.Fatalf("SubmitVerdict() error: %v", err)
		}
		_, task := readGateVerdictTask(t, stateFile)
		if result.EscalatedToBlocked || task.RejectionRCA != nil || task.Status != role.rejected {
			t.Fatalf("fourth rejection under threshold 6 gated: result=%+v status=%s record=%v", result, task.Status, task.RejectionRCA)
		}

		tmpDir, stateFile = gateVerdictState(t, role, 5, now, configure)
		result, err = SubmitVerdict(tmpDir, gateVerdictTaskID, "REJECTED", "sixth rejection", role.reviewer, "")
		if err != nil {
			t.Fatalf("SubmitVerdict() error: %v", err)
		}
		state, task := readGateVerdictTask(t, stateFile)
		assertGatedTask(t, state, task, role, 6, 6, now.Add(-5*time.Hour))
		if !result.EscalatedToBlocked || !result.RejectionRCAGated {
			t.Fatalf("result = %+v, want the gate escalation", result)
		}
	})

	t.Run("already-gated task is not re-seeded", func(t *testing.T) {
		now := time.Now().UTC().Truncate(time.Second)
		tmpDir, stateFile := gateVerdictState(t, role, 3, now, nil)
		bb := db.New(stateFile)

		// The gate fires from another session between this verdict's
		// pre-lock validation and its locked callback.
		gatedAt := now.Add(-time.Minute)
		seeded := &models.RejectionRCARecord{SchemaVersion: models.RejectionRCASchemaVersion, Threshold: 4, RejectionCount: 4, GatedAt: gatedAt, GatingCommit: "other-session"}
		previousHooks := testSubmitVerdictHooks
		testSubmitVerdictHooks = &submitVerdictTestHooks{beforeModify: func() {
			if err := bb.Modify(func(state *models.State) error {
				task := state.FindTask(gateVerdictTaskID)
				task.Status = models.TaskStatusBlocked
				task.BlockedReason = testhelpers.StringPtr(models.BlockedReasonRejectionRCARequired + ": gated by another session")
				task.BlockedQuestions = []string{"Which cause dominated?"}
				task.ReviewCyclesTotal = 4
				task.RejectionRCA = seeded
				task.AssignedTo, task.LeaseExpires, task.ReviewingBy, task.ReviewLeaseExpires = nil, nil, nil, nil
				task.History = append(task.History, models.TaskHistoryEntry{Time: gatedAt, Event: models.TaskEventBlocked, Agent: testhelpers.StringPtr(role.reviewer), Reason: task.BlockedReason})
				return nil
			}); err != nil {
				t.Fatalf("gate from another session: %v", err)
			}
		}}
		t.Cleanup(func() { testSubmitVerdictHooks = previousHooks })

		_, err := SubmitVerdict(tmpDir, gateVerdictTaskID, "REJECTED", "late rejection", role.reviewer, "")
		var lifecycleErr *LifecycleError
		if !stderrors.As(err, &lifecycleErr) || lifecycleErr.Outcome.Outcome != models.LifecycleAlreadyTransitioned || lifecycleErr.Outcome.SafeAction != "stop" {
			t.Fatalf("verdict against a gated task = %v, want ALREADY_TRANSITIONED/stop", err)
		}
		if !strings.Contains(err.Error(), models.BlockedReasonRejectionRCARequired) {
			t.Fatalf("error %q does not name the gate", err)
		}
		_, task := readGateVerdictTask(t, stateFile)
		record := task.RejectionRCA
		if record == nil || !record.GatedAt.Equal(seeded.GatedAt) || record.RejectionCount != seeded.RejectionCount || record.GatingCommit != seeded.GatingCommit || record.Disposition != nil {
			t.Fatalf("record re-seeded: %+v, want %+v", record, seeded)
		}
		if task.ReviewCyclesTotal != 4 || len(historyEntries(task, models.TaskEventBlocked)) != 1 || len(historyEntries(task, models.TaskEventRejected)) != 3 {
			t.Fatalf("gated task mutated: total=%d blocked=%d rejected=%d", task.ReviewCyclesTotal, len(historyEntries(task, models.TaskEventBlocked)), len(historyEntries(task, models.TaskEventRejected)))
		}
	})
}

// resumedRejectionRCACycle installs a completed first gate cycle on the task:
// a recorded and resumed record with its three history entries, at threshold 4
// after four durable rejections, so the next verdicts exercise the re-fire rule.
func resumedRejectionRCACycle(gatedAt time.Time) func(*models.State, *models.Task) {
	return func(state *models.State, task *models.Task) {
		actor := "orchestrator-1"
		request := models.RejectionRCARequest{SchemaVersion: models.RejectionRCASchemaVersion, Summary: "first cycle",
			Contributions: []models.RejectionRCAContribution{{RejectionIndex: 1, Categories: []string{models.RejectionCauseProductDefect}}}}
		recordedAt := gatedAt.Add(time.Minute)
		decidedAt := gatedAt.Add(2 * time.Minute)
		task.RejectionRCA = &models.RejectionRCARecord{
			SchemaVersion: models.RejectionRCASchemaVersion, Threshold: 4, RejectionCount: 4, GatedAt: gatedAt, GatingCommit: "cycle-one",
			Fingerprint: models.RejectionRCAFingerprint(request), RecordedAt: &recordedAt, RecordedBy: actor,
			Summary: request.Summary, Contributions: request.Contributions,
			Disposition: &models.RejectionRCADisposition{RecoveryPath: models.RecoveryImplementationCorrection, RestoreMode: models.RestoreModeClaimable, Actor: actor, DecidedAt: decidedAt},
		}
		task.ReviewCyclesCurrent = 0
		blockedReason := models.BlockedReasonRejectionRCARequired + ": first cycle"
		task.History = append(task.History,
			models.TaskHistoryEntry{Time: gatedAt, Event: models.TaskEventBlocked, Agent: testhelpers.StringPtr("code-reviewer-1"), Reason: &blockedReason,
				Extra: map[string]any{"blocked_class": models.BlockedReasonRejectionRCARequired, "threshold": 4, "rejection_count": 4, "gated_at": gatedAt.Format(time.RFC3339), "first_rejection_at": gatedAt.Add(-4 * time.Hour).Format(time.RFC3339)}},
			models.TaskHistoryEntry{Time: recordedAt, Event: models.TaskEventRejectionRCARecorded, Agent: &actor, Extra: map[string]any{"fingerprint": task.RejectionRCA.Fingerprint, "gated_at": gatedAt.Format(time.RFC3339)}},
			models.TaskHistoryEntry{Time: decidedAt, Event: models.TaskEventRejectionRCAResumed, Agent: &actor, Extra: map[string]any{"recovery_path": models.RecoveryImplementationCorrection, "gated_at": gatedAt.Format(time.RFC3339)}},
		)
	}
}

func TestSubmitVerdictGateReFire(t *testing.T) {
	role := gateVerdictRoles[0]
	now := time.Now().UTC().Truncate(time.Second)
	firstGatedAt := now.Add(-30 * time.Minute)
	tmpDir, stateFile := gateVerdictState(t, role, 4, now, resumedRejectionRCACycle(firstGatedAt))
	bb := db.New(stateFile)
	_, before := readGateVerdictTask(t, stateFile)
	firstCycle := before.RejectionRCA

	reject := func(n int) *VerdictResult {
		t.Helper()
		result, err := SubmitVerdict(tmpDir, gateVerdictTaskID, "REJECTED", fmt.Sprintf("rejection %d", n), role.reviewer, "")
		if err != nil {
			t.Fatalf("rejection %d: SubmitVerdict() error: %v", n, err)
		}
		return result
	}

	// Rejections 5, 6 and 7: below RejectionCount + threshold, so the closed
	// gate does not re-fire and the first cycle's record is untouched.
	for n := 5; n <= 7; n++ {
		result := reject(n)
		_, task := readGateVerdictTask(t, stateFile)
		if result.EscalatedToBlocked || task.Status != role.rejected || task.ReviewCyclesTotal != n {
			t.Fatalf("rejection %d re-gated: result=%+v status=%s total=%d", n, result, task.Status, task.ReviewCyclesTotal)
		}
		if !reflect.DeepEqual(task.RejectionRCA, firstCycle) {
			t.Fatalf("rejection %d changed the closed-gate record: %+v", n, task.RejectionRCA)
		}
		requeueForReview(t, bb, role)
	}

	// Rejection 8 = RejectionCount 4 + threshold 4: the gate re-fires and the
	// record is re-seeded for the new cycle.
	result := reject(8)
	state, task := readGateVerdictTask(t, stateFile)
	assertGatedTask(t, state, task, role, 4, 8, now.Add(-4*time.Hour))
	if !result.EscalatedToBlocked || !result.RejectionRCAGated {
		t.Fatalf("result = %+v, want the gate escalation", result)
	}
	if !task.RejectionRCA.GatedAt.After(firstGatedAt) || task.RejectionRCA.GatingCommit != "review123" {
		t.Fatalf("re-seeded record keeps the first cycle's timing: %+v", task.RejectionRCA)
	}

	// The first cycle survives in history: its blocked, recorded and resumed
	// entries remain, and the new cycle adds exactly one blocked entry.
	blocked := historyEntries(task, models.TaskEventBlocked)
	if len(blocked) != 2 || fmt.Sprint(blocked[0].Extra["gated_at"]) != firstGatedAt.Format(time.RFC3339) {
		t.Fatalf("blocked entries = %+v, want the first cycle's entry followed by the new one", blocked)
	}
	if recorded := historyEntries(task, models.TaskEventRejectionRCARecorded); len(recorded) != 1 || fmt.Sprint(recorded[0].Extra["fingerprint"]) != firstCycle.Fingerprint {
		t.Fatalf("rejection_rca_recorded entries = %+v, want the first cycle's", recorded)
	}
	if resumed := historyEntries(task, models.TaskEventRejectionRCAResumed); len(resumed) != 1 || fmt.Sprint(resumed[0].Extra["recovery_path"]) != models.RecoveryImplementationCorrection {
		t.Fatalf("rejection_rca_resumed entries = %+v, want the first cycle's", resumed)
	}
	if len(historyEntries(task, models.TaskEventRejected)) != 8 {
		t.Fatalf("rejected entries = %d, want 8", len(historyEntries(task, models.TaskEventRejected)))
	}
}

func TestSubmitVerdictGateDefersToExistingLimits(t *testing.T) {
	role := gateVerdictRoles[0]
	tests := []struct {
		name          string
		configure     func(*models.State, *models.Task)
		wantReason    string
		wantQuestions []string
		wantBlocked   bool
	}{
		{
			name: "review budget escalation blocks with its own reason",
			configure: func(state *models.State, task *models.Task) {
				task.Attempt = 2
				task.ReviewCyclesCurrent = 4
			},
			wantReason:    reviewBudgetExhaustedReason(5, 5),
			wantQuestions: defaultReviewBudgetExhaustedQuestions(),
			wantBlocked:   true,
		},
		{
			name: "iteration escalation blocks with its own reason",
			configure: func(state *models.State, task *models.Task) {
				task.Attempt = 2
				task.ReviewCyclesCurrent = 1
				task.Iteration = 2
				task.MaxIterations = 2
			},
			wantReason:    iterationLimitBlockedReason(2, 2),
			wantQuestions: defaultIterationLimitBlockedQuestions(),
			wantBlocked:   true,
		},
		{
			name: "review budget escalation on the first attempt keeps the new-attempt action",
			configure: func(state *models.State, task *models.Task) {
				task.Attempt = 1
				task.ReviewCyclesCurrent = 4
			},
			wantReason:  reviewBudgetExhaustedReason(5, 5),
			wantBlocked: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Now().UTC()
			// Three prior rejections: the fourth crosses the default gate
			// threshold on the same verdict that exhausts the existing limit.
			tmpDir, stateFile := gateVerdictState(t, role, 3, now, tt.configure)

			result, err := SubmitVerdict(tmpDir, gateVerdictTaskID, "REJECTED", "fourth rejection", role.reviewer, "")
			if err != nil {
				t.Fatalf("SubmitVerdict() error: %v", err)
			}
			_, task := readGateVerdictTask(t, stateFile)
			if task.RejectionRCA != nil || result.RejectionRCAGated {
				t.Fatalf("limit escalation seeded a record: task=%+v result=%+v", task.RejectionRCA, result)
			}
			if !task.RejectionRCAGateDue(models.DefaultHighChurnRejectionThreshold) {
				t.Fatal("fixture does not cross the gate threshold; the deferral is not exercised")
			}
			if tt.wantBlocked {
				if !result.EscalatedToBlocked || result.BlockedReason != tt.wantReason || task.Status != models.TaskStatusBlocked {
					t.Fatalf("result=%+v status=%s, want BLOCKED with reason %q", result, task.Status, tt.wantReason)
				}
				if task.BlockedReason == nil || *task.BlockedReason != tt.wantReason || !reflect.DeepEqual(task.BlockedQuestions, tt.wantQuestions) {
					t.Fatalf("blocked_reason/questions = %v/%v, want %q/%v", task.BlockedReason, task.BlockedQuestions, tt.wantReason, tt.wantQuestions)
				}
				blocked := historyEntries(task, models.TaskEventBlocked)
				if len(blocked) != 1 || blocked[0].Extra != nil || blocked[0].Reason == nil || *blocked[0].Reason != tt.wantReason {
					t.Fatalf("blocked entries = %+v, want one plain entry with the limit reason", blocked)
				}
				return
			}
			if result.EscalatedToBlocked || !result.NewAttemptTriggered || result.pendingAttemptReason != tt.wantReason || task.Status == models.TaskStatusBlocked || task.Attempt != 2 {
				t.Fatalf("result=%+v status=%s attempt=%d, want the new-attempt action with reason %q", result, task.Status, task.Attempt, tt.wantReason)
			}
			if len(historyEntries(task, models.TaskEventBlocked)) != 0 {
				t.Fatal("new-attempt escalation appended a blocked entry")
			}
		})
	}
}

func TestSubmitVerdictConcurrentGate(t *testing.T) {
	role := gateVerdictRoles[0]
	now := time.Now().UTC().Truncate(time.Second)
	tmpDir, stateFile := gateVerdictState(t, role, 3, now, nil)

	// The public entry serializes sessions on the per-task review lock before
	// any state is read; both sessions call the transaction directly so the
	// race is settled by the locked read-modify-write alone.
	//
	// Barrier "bothAtMutationBoundary": each reviewer session announces it has
	// passed pre-lock validation and is about to enter modifyLifecycleState;
	// neither is released until both have arrived, so both callbacks run
	// against a task that both sessions observed as under review.
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	previousHooks := testSubmitVerdictHooks
	testSubmitVerdictHooks = &submitVerdictTestHooks{beforeModify: func() {
		arrived <- struct{}{}
		<-release
	}}
	t.Cleanup(func() { testSubmitVerdictHooks = previousHooks })

	type verdictCall struct {
		result *VerdictResult
		err    error
	}
	done := make(chan verdictCall, 2)
	for i := 0; i < 2; i++ {
		go func(session int) {
			reason := fmt.Sprintf("session %d rejection", session)
			result, err := submitVerdict(tmpDir, gateVerdictTaskID, "REJECTED", reason, role.reviewer, nil, "", "", reason, false, LifecycleRequestOptions{})
			done <- verdictCall{result: result, err: err}
		}(i)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-arrived:
		case <-time.After(30 * time.Second):
			t.Fatal("both sessions did not reach the mutation boundary")
		}
	}
	close(release)

	var winners []*VerdictResult
	var losers []error
	for i := 0; i < 2; i++ {
		select {
		case call := <-done:
			if call.err != nil {
				losers = append(losers, call.err)
			} else {
				winners = append(winners, call.result)
			}
		case <-time.After(30 * time.Second):
			t.Fatal("a session did not return")
		}
	}
	if len(winners) != 1 || len(losers) != 1 {
		t.Fatalf("winners=%d losers=%d, want exactly one of each: %v", len(winners), len(losers), losers)
	}
	if !winners[0].EscalatedToBlocked || !winners[0].RejectionRCAGated {
		t.Fatalf("winner = %+v, want the gate escalation", winners[0])
	}
	// Observation: the loser's outcome is produced inside the callback by the
	// gate it observed there — the pre-lock fast-fail reports a different
	// reason — so both sessions entered the mutation callback.
	var lifecycleErr *LifecycleError
	if !stderrors.As(losers[0], &lifecycleErr) || lifecycleErr.Outcome.Outcome != models.LifecycleAlreadyTransitioned || lifecycleErr.Outcome.SafeAction != "stop" {
		t.Fatalf("loser = %v, want ALREADY_TRANSITIONED/stop", losers[0])
	}
	if !strings.Contains(losers[0].Error(), models.BlockedReasonRejectionRCARequired) {
		t.Fatalf("loser error %q does not name the gate observed in the callback", losers[0])
	}

	state, task := readGateVerdictTask(t, stateFile)
	assertGatedTask(t, state, task, role, models.DefaultHighChurnRejectionThreshold, 4, now.Add(-3*time.Hour))
	if len(historyEntries(task, models.TaskEventBlocked)) != 1 || len(historyEntries(task, models.TaskEventRejected)) != 4 {
		t.Fatalf("blocked=%d rejected=%d, want exactly one blocked event and four rejections", len(historyEntries(task, models.TaskEventBlocked)), len(historyEntries(task, models.TaskEventRejected)))
	}
	if len(state.Anomalies) != 0 {
		t.Fatalf("loser recorded anomalies: %+v", state.Anomalies)
	}
}
