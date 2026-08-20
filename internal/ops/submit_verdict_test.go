package ops

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/git"
	activitylog "github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
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
			errContains: "LIZA_AGENT_ID is required",
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

func TestSubmitVerdict_VerdictNormalization(t *testing.T) {
	// Lowercase "approved" should be accepted and normalized
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

	result, err := SubmitVerdict(tmpDir, "task-1", "approved", "", "code-reviewer-1", "")
	if err != nil {
		t.Fatalf("SubmitVerdict() error: %v", err)
	}
	if result.Verdict != "APPROVED" {
		t.Errorf("Verdict = %q, want %q", result.Verdict, "APPROVED")
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
		precondition, ok := err.(*PreconditionError)
		if !ok {
			t.Fatalf("SubmitVerdict() error = %T %v, want *PreconditionError", err, err)
		}
		for _, part := range []string{"4097 bytes", "4096-byte maximum", ".liza/agent-outputs/", "bounded summary", "artifact reference"} {
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
		if _, statErr := os.Stat(filepath.Join(projectRoot, ".liza", "log.yaml")); !os.IsNotExist(statErr) {
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
	if len(readState.Anomalies) != 1 {
		t.Fatalf("anomaly count = %d, want 1", len(readState.Anomalies))
	}
	anomaly := readState.Anomalies[0]
	if anomaly.Type != "stale_verdict" || anomaly.Task != "task-1" || anomaly.Reporter != "code-reviewer-1" {
		t.Fatalf("anomaly = %+v, want stale_verdict for task-1 by code-reviewer-1", anomaly)
	}
	if anomaly.Details["attempted_verdict"] != "REJECTED" {
		t.Fatalf("attempted_verdict = %v, want REJECTED", anomaly.Details["attempted_verdict"])
	}
	if anomaly.Details["current_status"] != string(models.TaskStatusImplementing) {
		t.Fatalf("current_status = %v, want %s", anomaly.Details["current_status"], models.TaskStatusImplementing)
	}
	if anomaly.Details["reason"] != "late finding" {
		t.Fatalf("reason = %v, want late finding", anomaly.Details["reason"])
	}
	agent := readState.Agents["code-reviewer-1"]
	if agent.CurrentTask != nil {
		t.Fatalf("reviewer CurrentTask = %q, want nil after stale verdict", *agent.CurrentTask)
	}
}

func TestSubmitVerdict_RecordsStaleVerdictWhenTaskLeavesReviewBeforeModify(t *testing.T) {
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
	testhelpers.RequireErrorContains(t, err, "not in a reviewing state")

	readState, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("read state error: %v", readErr)
	}
	if len(readState.Anomalies) != 1 {
		t.Fatalf("anomaly count = %d, want 1", len(readState.Anomalies))
	}
	anomaly := readState.Anomalies[0]
	if anomaly.Type != "stale_verdict" || anomaly.Task != "task-1" || anomaly.Reporter != "code-reviewer-1" {
		t.Fatalf("anomaly = %+v, want stale_verdict for task-1 by code-reviewer-1", anomaly)
	}
	if anomaly.Details["current_status"] != string(models.TaskStatusImplementing) {
		t.Fatalf("current_status = %v, want %s", anomaly.Details["current_status"], models.TaskStatusImplementing)
	}
	if anomaly.Details["attempted_verdict"] != "REJECTED" {
		t.Fatalf("attempted_verdict = %v, want REJECTED", anomaly.Details["attempted_verdict"])
	}
	agent := readState.Agents["code-reviewer-1"]
	if agent.CurrentTask != nil {
		t.Fatalf("reviewer CurrentTask = %q, want nil after stale verdict", *agent.CurrentTask)
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

	// Create a regular file at .worktrees so os.Stat(.worktrees/task-1)
	// returns ENOTDIR instead of ENOENT.
	wtParent := filepath.Join(tmpDir, ".worktrees")
	if err := os.WriteFile(wtParent, []byte("not-a-directory"), 0644); err != nil {
		t.Fatalf("Failed to create fixture: %v", err)
	}

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
		pipelinePath := filepath.Join(tmpDir, ".liza", "pipeline.yaml")
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
		pipelinePath := filepath.Join(tmpDir, ".liza", "pipeline.yaml")
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

		bb := db.New(filepath.Join(tmpDir, ".liza", "state.yaml"))
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

		bb := db.New(filepath.Join(tmpDir, ".liza", "state.yaml"))
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

		bb := db.New(filepath.Join(tmpDir, ".liza", "state.yaml"))
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

	t.Run("global clean verifies source before atomic projection", func(t *testing.T) {
		fixture := newSubmitVerdictIntegrationFixture(t, models.IntegrationAnalysisPhaseGlobal, nil)
		called := make(chan string, 1)
		release := make(chan struct{})
		previousVerifier := verifyCleanIntegrationSourceForVerdict
		verifyCleanIntegrationSourceForVerdict = func(_ string, sourceCommit string) (cleanIntegrationSourceVerification, error) {
			called <- sourceCommit
			select {
			case <-release:
				return cleanIntegrationSourceVerification{SourceCommit: sourceCommit, IntegrationHEAD: sourceCommit, Effective: true}, nil
			case <-time.After(5 * time.Second):
				return cleanIntegrationSourceVerification{}, fmt.Errorf("timed out waiting to release clean-source verification")
			}
		}
		t.Cleanup(func() { verifyCleanIntegrationSourceForVerdict = previousVerifier })

		type verdictCall struct {
			result *VerdictResult
			err    error
		}
		completed := make(chan verdictCall, 1)
		go func() {
			result, err := SubmitVerdict(fixture.projectRoot, fixture.taskID, "APPROVED", "", fixture.reviewerID, "")
			completed <- verdictCall{result: result, err: err}
		}()

		select {
		case sourceCommit := <-called:
			if sourceCommit != fixture.sourceCommit {
				t.Fatalf("verified source = %q, want %q", sourceCommit, fixture.sourceCommit)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("clean-source verification was not called")
		}
		blockedState := fixture.readState(t)
		if got := blockedState.FindTask(fixture.taskID).Status; got != models.TaskStatus("REVIEWING_INTEGRATION_ANALYSIS") {
			t.Fatalf("status changed before verification returned: %s", got)
		}
		if len(blockedState.Goal.Integration.GlobalGenerations) != 0 {
			t.Fatalf("global evidence persisted before verification returned: %#v", blockedState.Goal.Integration.GlobalGenerations)
		}

		close(release)
		select {
		case call := <-completed:
			if call.err != nil {
				t.Fatalf("SubmitVerdict() error = %v", call.err)
			}
			if call.result == nil {
				t.Fatal("SubmitVerdict() result is nil")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("SubmitVerdict did not complete after verification")
		}

		after := fixture.readState(t)
		if len(after.Goal.Integration.GlobalGenerations) != 1 {
			t.Fatalf("global generation count = %d, want 1", len(after.Goal.Integration.GlobalGenerations))
		}
		generation := after.Goal.Integration.GlobalGenerations[0]
		if generation.Verdict != models.IntegrationAnalysisVerdictClean || generation.SourceCommit != fixture.sourceCommit || generation.ReportCommit != fixture.reportCommit {
			t.Fatalf("global clean generation = %#v", generation)
		}
		closure := after.Goal.Integration.Closure
		if closure == nil || closure.Status != models.IntegrationClosureStatusClean || closure.Generation != 1 || closure.AnalysisKey != fixture.analysisKey || closure.SourceCommit != fixture.sourceCommit {
			t.Fatalf("global clean closure = %#v", closure)
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
	pipelinePath := filepath.Join(projectRoot, ".liza", "pipeline.yaml")
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
