package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestRecoverAgent_Validation(t *testing.T) {
	t.Parallel()

	_, err := RecoverAgent("/nonexistent", "", false, "reason")
	if err == nil {
		t.Fatal("Expected error for empty agent ID")
	}
	if !strings.Contains(err.Error(), "agent ID required") {
		t.Errorf("Error = %q, want to contain 'agent ID required'", err.Error())
	}
}

func TestRecoverAgent_NotFound_Idempotent(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := RecoverAgent(tmpDir, "nonexistent", false, "reason")
	if err != nil {
		t.Fatalf("RecoverAgent() error: %v", err)
	}
	if !result.AlreadyClean {
		t.Error("Expected AlreadyClean=true for nonexistent agent")
	}
	if result.AgentDeleted {
		t.Error("AgentDeleted should be false for nonexistent agent")
	}
}

func TestRecoverAgent_MissingAgentReleasesTaskSideClaims(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	leaseExpires := now.Add(30 * time.Minute)
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		{
			ID:           "task-doer",
			Description:  "orphaned doer claim",
			Status:       models.TaskStatusImplementing,
			RolePair:     "coding-pair",
			Priority:     1,
			AssignedTo:   strPtr("ghost-1"),
			LeaseExpires: &leaseExpires,
			SpecRef:      "spec.md",
			DoneWhen:     "done",
			Scope:        "scope",
			Created:      now,
		},
		{
			ID:                 "task-reviewer",
			Description:        "orphaned reviewer claim",
			Status:             models.TaskStatusReviewing,
			RolePair:           "coding-pair",
			Priority:           1,
			AssignedTo:         strPtr("coder-1"),
			ReviewCommit:       strPtr("abc123"),
			ReviewingBy:        strPtr("ghost-1"),
			ReviewLeaseExpires: &leaseExpires,
			SpecRef:            "spec.md",
			DoneWhen:           "done",
			Scope:              "scope",
			Created:            now,
		},
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := RecoverAgent(tmpDir, "ghost-1", true, "missing row cleanup")
	if err != nil {
		t.Fatalf("RecoverAgent() error: %v", err)
	}
	if result.AlreadyClean {
		t.Fatal("RecoverAgent should not report clean while task-side claims exist")
	}
	if !result.ClaimReleased {
		t.Fatal("Expected orphaned task-side claims to be released")
	}
	if result.AgentDeleted {
		t.Fatal("AgentDeleted should be false when the row was already missing")
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	doerTask := readState.FindTask("task-doer")
	if doerTask == nil {
		t.Fatal("task-doer should still exist")
	}
	if doerTask.AssignedTo != nil || doerTask.LeaseExpires != nil {
		t.Fatalf("Doer claim still present: assigned=%v lease=%v", doerTask.AssignedTo, doerTask.LeaseExpires)
	}
	if doerTask.Status != models.TaskStatusReady {
		t.Fatalf("Doer task status = %s, want READY", doerTask.Status)
	}

	reviewerTask := readState.FindTask("task-reviewer")
	if reviewerTask == nil {
		t.Fatal("task-reviewer should still exist")
	}
	if reviewerTask.ReviewingBy != nil || reviewerTask.ReviewLeaseExpires != nil {
		t.Fatalf("Reviewer claim still present: reviewing_by=%v lease=%v", reviewerTask.ReviewingBy, reviewerTask.ReviewLeaseExpires)
	}
	if reviewerTask.Status != models.TaskStatusReadyForReview {
		t.Fatalf("Reviewer task status = %s, want READY_FOR_REVIEW", reviewerTask.Status)
	}
}

func TestRecoverAgent_ReleasesTaskSideDoerClaimWhenAgentCurrentTaskMissing(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	leaseExpires := now.Add(30 * time.Minute)
	state := testhelpers.CreateValidState()
	state.Agents["coder-1"] = models.Agent{
		Status:       models.AgentStatusIdle,
		LeaseExpires: &leaseExpires,
	}
	state.Tasks = []models.Task{
		{
			ID:           "task-1",
			Description:  "orphaned claim",
			Status:       models.TaskStatusImplementing,
			RolePair:     "coding-pair",
			Priority:     1,
			AssignedTo:   strPtr("coder-1"),
			LeaseExpires: &leaseExpires,
			SpecRef:      "spec.md",
			DoneWhen:     "done",
			Scope:        "scope",
			Created:      now,
		},
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := RecoverAgent(tmpDir, "coder-1", true, "ghost cleanup")
	if err != nil {
		t.Fatalf("RecoverAgent() error: %v", err)
	}
	if !result.ClaimReleased {
		t.Fatal("Expected task-side doer claim to be released")
	}
	if result.Role != "" {
		t.Fatalf("Result role = %q, want empty role for ghost agent", result.Role)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if _, exists := readState.Agents["coder-1"]; exists {
		t.Fatal("Agent should be deleted")
	}
	task := readState.FindTask("task-1")
	if task == nil {
		t.Fatal("Task should still exist")
	}
	if task.AssignedTo != nil || task.LeaseExpires != nil {
		t.Fatalf("Task claim still present: assigned=%v lease=%v", task.AssignedTo, task.LeaseExpires)
	}
	if task.Status != models.TaskStatusReady {
		t.Fatalf("Task status = %s, want READY", task.Status)
	}
}

func TestRecoverAgent_CoderWithImplementingTask(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	taskRef := "task-1"
	now := time.Now().UTC()
	leaseExpires := now.Add(-10 * time.Minute) // expired
	worktreeRef := ".worktrees/task-1"
	state := testhelpers.CreateValidState()
	state.Agents["coder-1"] = models.Agent{
		Role:         "coder",
		Status:       models.AgentStatusWorking,
		CurrentTask:  &taskRef,
		LeaseExpires: &leaseExpires,
		PID:          999999, // dead PID
	}
	state.Tasks = append(state.Tasks, models.Task{
		ID:           "task-1",
		Description:  "test task",
		Status:       models.TaskStatusImplementing,
		RolePair:     "coding-pair",
		Priority:     1,
		AssignedTo:   strPtr("coder-1"),
		Worktree:     &worktreeRef,
		LeaseExpires: &leaseExpires,
		SpecRef:      "spec.md",
		DoneWhen:     "tests pass",
		Scope:        "small",
	})
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := RecoverAgent(tmpDir, "coder-1", false, "crashed")
	if err != nil {
		t.Fatalf("RecoverAgent() error: %v", err)
	}

	if result.AlreadyClean {
		t.Error("Expected AlreadyClean=false")
	}
	if result.Role != "coder" {
		t.Errorf("Role = %q, want %q", result.Role, "coder")
	}
	if result.TaskID != "task-1" {
		t.Errorf("TaskID = %q, want %q", result.TaskID, "task-1")
	}
	if !result.ClaimReleased {
		t.Error("Expected ClaimReleased=true")
	}
	if !result.AgentDeleted {
		t.Error("Expected AgentDeleted=true")
	}

	// Verify state
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	// Agent should be gone
	if _, exists := readState.Agents["coder-1"]; exists {
		t.Error("Agent should be removed from state")
	}

	// Task should be READY with no assignment
	task := readState.FindTask("task-1")
	if task == nil {
		t.Fatal("Task should still exist")
	}
	if task.Status != models.TaskStatusReady {
		t.Errorf("Task status = %s, want %s", task.Status, models.TaskStatusReady)
	}
	if task.AssignedTo != nil {
		t.Error("Task AssignedTo should be nil")
	}
	if task.LeaseExpires != nil {
		t.Error("Task LeaseExpires should be nil")
	}
	if task.Worktree != nil {
		t.Error("Task Worktree should be nil")
	}

	// Human note should exist
	if len(readState.HumanNotes) == 0 {
		t.Fatal("Expected human note to be added")
	}
	lastNote := readState.HumanNotes[len(readState.HumanNotes)-1]
	if !strings.Contains(lastNote.Message, "coder-1") {
		t.Errorf("Note message = %q, want to contain agent ID", lastNote.Message)
	}
	if !lastNote.SeenByOrchestrator() {
		t.Error("audit note must be stamped seen so it does not wake the orchestrator")
	}
}

func TestRecoverAgent_ReviewerWithReviewingTask(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	taskRef := "task-2"
	now := time.Now().UTC()
	leaseExpires := now.Add(-10 * time.Minute)
	state := testhelpers.CreateValidState()
	state.Agents["code-reviewer-1"] = models.Agent{
		Role:         "code-reviewer",
		Status:       models.AgentStatusReviewing,
		CurrentTask:  &taskRef,
		LeaseExpires: &leaseExpires,
	}
	state.Tasks = append(state.Tasks, models.Task{
		ID:                 "task-2",
		Description:        "test task",
		Status:             models.TaskStatusReviewing,
		RolePair:           "coding-pair",
		Priority:           1,
		ReviewingBy:        strPtr("code-reviewer-1"),
		ReviewLeaseExpires: &leaseExpires,
		SpecRef:            "spec.md",
		DoneWhen:           "tests pass",
		Scope:              "small",
	})
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := RecoverAgent(tmpDir, "code-reviewer-1", false, "crashed")
	if err != nil {
		t.Fatalf("RecoverAgent() error: %v", err)
	}

	if result.Role != "code-reviewer" {
		t.Errorf("Role = %q, want %q", result.Role, "code-reviewer")
	}
	if !result.ClaimReleased {
		t.Error("Expected ClaimReleased=true")
	}
	if result.WorktreeRemoved {
		t.Error("Expected WorktreeRemoved=false for reviewer")
	}
	if !result.AgentDeleted {
		t.Error("Expected AgentDeleted=true")
	}

	// Verify state
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	// Agent should be gone
	if _, exists := readState.Agents["code-reviewer-1"]; exists {
		t.Error("Agent should be removed from state")
	}

	// Task should be READY_FOR_REVIEW
	task := readState.FindTask("task-2")
	if task == nil {
		t.Fatal("Task should still exist")
	}
	if task.Status != models.TaskStatusReadyForReview {
		t.Errorf("Task status = %s, want %s", task.Status, models.TaskStatusReadyForReview)
	}
	if task.ReviewingBy != nil {
		t.Error("Task ReviewingBy should be nil")
	}
	if task.ReviewLeaseExpires != nil {
		t.Error("Task ReviewLeaseExpires should be nil")
	}
}

func TestRecoverAgent_NoCurrentTask(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Agents["coder-1"] = models.Agent{
		Role:   "coder",
		Status: models.AgentStatusIdle,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := RecoverAgent(tmpDir, "coder-1", false, "cleanup")
	if err != nil {
		t.Fatalf("RecoverAgent() error: %v", err)
	}

	if !result.AgentDeleted {
		t.Error("Expected AgentDeleted=true")
	}
	if result.ClaimReleased {
		t.Error("Expected ClaimReleased=false (no task)")
	}
	if result.WorktreeRemoved {
		t.Error("Expected WorktreeRemoved=false (no task)")
	}
	if result.TaskID != "" {
		t.Errorf("TaskID = %q, want empty", result.TaskID)
	}

	// Verify agent removed
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if _, exists := readState.Agents["coder-1"]; exists {
		t.Error("Agent should be removed from state")
	}
}

func TestRecoverAgent_PIDAlive_NoForce(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Agents["coder-1"] = models.Agent{
		Role:   "coder",
		Status: models.AgentStatusWorking,
		PID:    os.Getpid(), // alive PID
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := RecoverAgent(tmpDir, "coder-1", false, "reason")
	if err == nil {
		t.Fatal("Expected error for alive PID without force")
	}
	if !strings.Contains(err.Error(), "still running") {
		t.Errorf("Error = %q, want to contain 'still running'", err.Error())
	}
}

func TestRecoverAgent_PIDAlive_WithForce(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Agents["coder-1"] = models.Agent{
		Role:   "coder",
		Status: models.AgentStatusIdle,
		PID:    os.Getpid(), // alive PID
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := RecoverAgent(tmpDir, "coder-1", true, "forced recovery")
	if err != nil {
		t.Fatalf("RecoverAgent() with force error: %v", err)
	}
	if !result.AgentDeleted {
		t.Error("Expected AgentDeleted=true with force")
	}
}

func TestRecoverAgent_DoubleRecover(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Agents["coder-1"] = models.Agent{
		Role:   "coder",
		Status: models.AgentStatusIdle,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	// First recover
	result1, err := RecoverAgent(tmpDir, "coder-1", false, "first recovery")
	if err != nil {
		t.Fatalf("First RecoverAgent() error: %v", err)
	}
	if result1.AlreadyClean {
		t.Error("First recovery should not be AlreadyClean")
	}
	if !result1.AgentDeleted {
		t.Error("First recovery should delete agent")
	}

	// Second recover — idempotent
	result2, err := RecoverAgent(tmpDir, "coder-1", false, "second recovery")
	if err != nil {
		t.Fatalf("Second RecoverAgent() error: %v", err)
	}
	if !result2.AlreadyClean {
		t.Error("Second recovery should be AlreadyClean")
	}
	if result2.AgentDeleted {
		t.Error("Second recovery should not report AgentDeleted")
	}
}

// TestRecoverAgent_CustomDoerRole_WorktreeRemoval verifies that a custom doer role
// (data-engineer) triggers worktree removal during recovery, proving the condition
// is resolver-based rather than hardcoded to "coder".
func TestRecoverAgent_CustomDoerRole_WorktreeRemoval(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	// Override with custom pipeline that defines data-engineer as a doer role.
	customPipeline := []byte(`pipeline:
  roles:
    orchestrator:
      type: orchestrator
      display-name: "Orchestrator"
    data-engineer:
      type: doer
      display-name: "Data Engineer"
    data-reviewer:
      type: reviewer
      display-name: "Data Reviewer"
  role-pairs:
    data-pair:
      doer: data-engineer
      reviewer: data-reviewer
      states:
        initial: DRAFT_CODE
        executing: IMPLEMENTING_CODE
        submitted: CODE_READY_FOR_REVIEW
        reviewing: REVIEWING_CODE
        approved: CODE_APPROVED
        rejected: CODE_REJECTED
  sub-pipelines:
    data-subpipeline:
      steps:
        - data-pair
  entry-points:
    default: data-subpipeline.data-pair
`)
	pipelinePath := filepath.Join(tmpDir, paths.ProjectDirName(), "pipeline.yaml")
	if err := os.WriteFile(pipelinePath, customPipeline, 0644); err != nil {
		t.Fatalf("Failed to write custom pipeline: %v", err)
	}

	taskRef := "task-de-1"
	now := time.Now().UTC()
	leaseExpires := now.Add(-10 * time.Minute)
	worktreeRef := ".worktrees/task-de-1"
	state := testhelpers.CreateValidState()
	state.Agents["data-engineer-1"] = models.Agent{
		Role:         "data-engineer",
		Status:       models.AgentStatusWorking,
		CurrentTask:  &taskRef,
		LeaseExpires: &leaseExpires,
		PID:          999999, // dead PID
	}
	state.Tasks = append(state.Tasks, models.Task{
		ID:           "task-de-1",
		Description:  "data engineering task",
		Status:       models.TaskStatusImplementing,
		RolePair:     "data-pair",
		Priority:     1,
		AssignedTo:   strPtr("data-engineer-1"),
		Worktree:     &worktreeRef,
		LeaseExpires: &leaseExpires,
		SpecRef:      "spec.md",
		DoneWhen:     "tests pass",
		Scope:        "small",
	})
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := RecoverAgent(tmpDir, "data-engineer-1", false, "crashed")
	if err != nil {
		t.Fatalf("RecoverAgent() error: %v", err)
	}

	// Worktree removal should have been attempted (will fail since no real git repo,
	// but the attempt proves the resolver-based path was taken).
	worktreeAttempted := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "worktree removal") {
			worktreeAttempted = true
			break
		}
	}
	if !worktreeAttempted {
		t.Error("Expected worktree removal to be attempted for custom doer role data-engineer")
	}

	if !result.ClaimReleased {
		t.Error("Expected ClaimReleased=true")
	}
	if !result.AgentDeleted {
		t.Error("Expected AgentDeleted=true")
	}

	// Verify task state was properly released
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	task := readState.FindTask("task-de-1")
	if task == nil {
		t.Fatal("Task should still exist")
	}
	if task.AssignedTo != nil {
		t.Error("Task AssignedTo should be nil after recovery")
	}
}

// TestRecoverAgent_NilResolver_WarningLogLine verifies that when the resolver is nil
// during agent recovery, a warning log line is emitted indicating claim release was
// skipped due to missing resolver and the claim-owning agent is preserved.
func TestRecoverAgent_NilResolver_WarningLogLine(t *testing.T) {
	// captureLogOutput temporarily redirects the process-global logger, so this
	// test must not overlap parallel tests that may log concurrently.

	tmpDir := t.TempDir()

	// Manually create the project runtime directory WITHOUT pipeline config so resolver is nil.
	lizaDir := filepath.Join(tmpDir, paths.ProjectDirName())
	if err := os.MkdirAll(lizaDir, 0755); err != nil {
		t.Fatalf("failed to create project runtime directory: %v", err)
	}
	lockPath := filepath.Join(lizaDir, "state.yaml.lock")
	if err := os.WriteFile(lockPath, []byte{}, 0644); err != nil {
		t.Fatalf("Failed to create lock file: %v", err)
	}

	stateFile := filepath.Join(lizaDir, "state.yaml")
	taskRef := "task-nil-1"
	now := time.Now().UTC()
	leaseExpires := now.Add(-10 * time.Minute)
	state := testhelpers.CreateValidState()
	state.Agents["coder-1"] = models.Agent{
		Role:         "coder",
		Status:       models.AgentStatusWorking,
		CurrentTask:  &taskRef,
		LeaseExpires: &leaseExpires,
		PID:          999999,
	}
	state.Tasks = append(state.Tasks, models.Task{
		ID:           "task-nil-1",
		Description:  "test task",
		Status:       models.TaskStatusImplementing,
		RolePair:     "coding-pair",
		Priority:     1,
		AssignedTo:   strPtr("coder-1"),
		LeaseExpires: &leaseExpires,
		SpecRef:      "spec.md",
		DoneWhen:     "tests pass",
		Scope:        "small",
	})
	testhelpers.WriteInitialState(t, stateFile, state)

	// Capture log output during recovery.
	logOutput := captureLogOutput(t, func() {
		result, err := RecoverAgent(tmpDir, "coder-1", false, "crashed")
		if err != nil {
			t.Fatalf("RecoverAgent() error: %v", err)
		}

		// result.Warnings should mention claim release skipped
		found := false
		for _, w := range result.Warnings {
			if strings.Contains(w, "claim release skipped") && strings.Contains(w, "resolver not loaded") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected warning about claim release skipped, got: %v", result.Warnings)
		}

		if result.ClaimReleased {
			t.Error("ClaimReleased should be false when resolver is nil")
		}
		if result.AgentDeleted {
			t.Error("AgentDeleted should be false when resolver is nil and a claim remains attached")
		}

		readState, err := db.New(stateFile).Read()
		if err != nil {
			t.Fatalf("read state after RecoverAgent: %v", err)
		}
		if _, exists := readState.Agents["coder-1"]; !exists {
			t.Fatal("agent should be preserved when claim release is skipped")
		}
		task := readState.FindTask("task-nil-1")
		if task == nil {
			t.Fatal("task-nil-1 not found")
		}
		if task.AssignedTo == nil || *task.AssignedTo != "coder-1" {
			t.Fatalf("task AssignedTo = %v, want coder-1", task.AssignedTo)
		}
	})

	// Verify the actual log line was emitted.
	if !strings.Contains(logOutput, "claim release skipped") {
		t.Errorf("Expected log output to contain 'claim release skipped', got: %q", logOutput)
	}
	if !strings.Contains(logOutput, "resolver not loaded") {
		t.Errorf("Expected log output to contain 'resolver not loaded', got: %q", logOutput)
	}
}

func strPtr(s string) *string {
	return &s
}
