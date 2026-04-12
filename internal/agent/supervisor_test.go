package agent

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// MockCLIExecutor for testing CLI execution
type MockCLIExecutor struct {
	mu               sync.Mutex
	Calls            []MockCLICall
	InteractiveCalls []MockCLICall
	ExitCode         int
	ExitError        error
}

type MockCLICall struct {
	CLIName string
	AgentID string
	Prompt  string
}

func (m *MockCLIExecutor) Execute(ctx context.Context, cliName string, agentID string, prompt string, projectRoot string) (int, error) {
	m.mu.Lock()
	m.Calls = append(m.Calls, MockCLICall{CLIName: cliName, AgentID: agentID, Prompt: prompt})
	m.mu.Unlock()
	return m.ExitCode, m.ExitError
}

func (m *MockCLIExecutor) ExecuteInteractive(ctx context.Context, cliName string, projectRoot string) (int, error) {
	m.mu.Lock()
	m.InteractiveCalls = append(m.InteractiveCalls, MockCLICall{CLIName: cliName})
	m.mu.Unlock()
	return m.ExitCode, m.ExitError
}

// GetCalls returns a copy of the calls slice in a thread-safe manner
func (m *MockCLIExecutor) GetCalls() []MockCLICall {
	m.mu.Lock()
	defer m.mu.Unlock()
	calls := make([]MockCLICall, len(m.Calls))
	copy(calls, m.Calls)
	return calls
}

// GetInteractiveCalls returns a copy of the interactive calls slice in a thread-safe manner
func (m *MockCLIExecutor) GetInteractiveCalls() []MockCLICall {
	m.mu.Lock()
	defer m.mu.Unlock()
	calls := make([]MockCLICall, len(m.InteractiveCalls))
	copy(calls, m.InteractiveCalls)
	return calls
}

// TestMockCLIExecution tests CLI executor mock
func TestMockCLIExecution(t *testing.T) {
	mock := &MockCLIExecutor{
		ExitCode: 0,
	}

	ctx := context.Background()
	exitCode, err := mock.Execute(ctx, "claude", "claude-1", "test prompt", "/tmp/test-project")

	if err != nil {
		t.Errorf("Execute() error = %v", err)
	}

	if exitCode != 0 {
		t.Errorf("Execute() exitCode = %d, want 0", exitCode)
	}

	calls := mock.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("Expected 1 call, got %d", len(calls))
	}

	call := calls[0]
	if call.CLIName != "claude" {
		t.Errorf("CLIName = %s, want claude", call.CLIName)
	}
	if call.Prompt != "test prompt" {
		t.Errorf("Prompt = %s, want 'test prompt'", call.Prompt)
	}
}

func TestExit42RestartTracker_ExponentialBackoffAndCap(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	now := time.Now().UTC()

	agentID := "coder-1"
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now)
	task.AssignedTo = &agentID

	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{task}
	state.Config.Exit42RestartThreshold = 99
	state.Config.Exit42MaxBackoffSeconds = 8
	state.Agents[agentID] = models.Agent{Role: "coder", Status: models.AgentStatusWorking}

	bb := testhelpers.WriteInitialState(t, statePath, state)
	tracker := newExit42RestartTracker()

	var delays []time.Duration
	for i := 0; i < 4; i++ {
		outcome, err := tracker.Handle(bb, tmpDir, "coder", task.ID, agentID)
		if err != nil {
			t.Fatalf("Handle() error on attempt %d: %v", i+1, err)
		}
		if outcome.BlockedTask {
			t.Fatalf("Handle() blocked task unexpectedly on attempt %d", i+1)
		}
		delays = append(delays, outcome.Delay)
	}

	wantDelays := []time.Duration{
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		8 * time.Second,
	}
	for i, want := range wantDelays {
		if delays[i] != want {
			t.Errorf("delay[%d] = %v, want %v", i, delays[i], want)
		}
	}

	updatedState, err := bb.Read()
	if err != nil {
		t.Fatalf("failed to read state: %v", err)
	}
	updatedTask := updatedState.FindTask(task.ID)
	if updatedTask == nil {
		t.Fatalf("task %q not found", task.ID)
	}

	if updatedTask.BlockedReason != nil && *updatedTask.BlockedReason != "" {
		t.Errorf("task should not be blocked yet, got reason: %s", *updatedTask.BlockedReason)
	}
}

func TestExit42RestartTracker_Blocking(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	now := time.Now().UTC()

	agentID := "coder-1"
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now)
	task.AssignedTo = &agentID

	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{task}
	state.Config.Exit42RestartThreshold = 2
	state.Agents[agentID] = models.Agent{Role: "coder", Status: models.AgentStatusWorking}

	bb := testhelpers.WriteInitialState(t, statePath, state)
	tracker := newExit42RestartTracker()

	// First attempt
	outcome, err := tracker.Handle(bb, tmpDir, "coder", task.ID, agentID)
	if err != nil {
		t.Fatalf("Handle() error on attempt 1: %v", err)
	}
	if outcome.BlockedTask {
		t.Fatalf("Handle() should not block on first attempt")
	}

	// Second attempt (at threshold)
	outcome, err = tracker.Handle(bb, tmpDir, "coder", task.ID, agentID)
	if err != nil {
		t.Fatalf("Handle() error on attempt 2: %v", err)
	}
	if outcome.BlockedTask {
		t.Fatalf("Handle() should not block at threshold")
	}

	// Third attempt (over threshold)
	outcome, err = tracker.Handle(bb, tmpDir, "coder", task.ID, agentID)
	if err != nil {
		t.Fatalf("Handle() error on attempt 3: %v", err)
	}
	if !outcome.BlockedTask {
		t.Fatalf("Handle() should block when over threshold")
	}

	updatedState, err := bb.Read()
	if err != nil {
		t.Fatalf("failed to read state: %v", err)
	}
	updatedTask := updatedState.FindTask(task.ID)
	if updatedTask == nil {
		t.Fatalf("task %q not found", task.ID)
	}

	wantReason := "exit code 42 restart loop detected"
	if updatedTask.BlockedReason == nil || !strings.Contains(*updatedTask.BlockedReason, wantReason) {
		got := "<nil>"
		if updatedTask.BlockedReason != nil {
			got = *updatedTask.BlockedReason
		}
		t.Errorf("blocked reason = %q, want containing %q", got, wantReason)
	}
}

func TestExit42RestartTracker_BlocksNonCoderRoles(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	now := time.Now().UTC()

	agentID := "code-reviewer-1"
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now)
	task.AssignedTo = &agentID
	task.ReviewingBy = &agentID

	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{task}
	state.Config.Exit42RestartThreshold = 2
	state.Agents[agentID] = models.Agent{Role: "code-reviewer", Status: models.AgentStatusReviewing}

	bb := testhelpers.WriteInitialState(t, statePath, state)
	tracker := newExit42RestartTracker()

	// Exhaust the threshold.
	for i := 0; i < 3; i++ {
		tracker.Handle(bb, tmpDir, "code-reviewer", task.ID, agentID)
	}

	// Read updated state — task should be BLOCKED.
	updatedState, err := bb.Read()
	if err != nil {
		t.Fatalf("failed to read state: %v", err)
	}
	updatedTask := updatedState.FindTask(task.ID)
	if updatedTask == nil {
		t.Fatalf("task %q not found", task.ID)
	}
	if updatedTask.Status != models.TaskStatusBlocked {
		t.Errorf("task status = %q, want BLOCKED", updatedTask.Status)
	}
}

func TestCrashRestartTracker_BlocksAfterThreshold(t *testing.T) {
	tracker := newCrashRestartTracker()
	threshold := 3

	// Same signature (no progress) — count accumulates.
	for i := 1; i <= threshold; i++ {
		count := tracker.Increment("task-1", "same-sig")
		if count != i {
			t.Fatalf("Increment() = %d, want %d", count, i)
		}
	}

	// Over threshold.
	count := tracker.Increment("task-1", "same-sig")
	if count != threshold+1 {
		t.Fatalf("Increment() = %d, want %d", count, threshold+1)
	}

	// Reset clears.
	tracker.reset("task-1")
	count = tracker.Increment("task-1", "same-sig")
	if count != 1 {
		t.Fatalf("after reset, Increment() = %d, want 1", count)
	}
}

func TestCrashRestartTracker_ResetsOnProgress(t *testing.T) {
	tracker := newCrashRestartTracker()

	tracker.Increment("task-1", "sig-a")
	tracker.Increment("task-1", "sig-a")

	// Signature changes — progress detected, counter resets.
	count := tracker.Increment("task-1", "sig-b")
	if count != 1 {
		t.Fatalf("Increment() after progress = %d, want 1", count)
	}
}

func TestSpinningTracker_BlocksAfterThreshold(t *testing.T) {
	tracker := newSpinningTracker()
	threshold := 5

	for i := 1; i <= threshold+1; i++ {
		count := tracker.Track("task-1", "same-sig")
		if count != i {
			t.Fatalf("Track() = %d, want %d", count, i)
		}
	}
}

func TestSpinningTracker_ResetsOnProgress(t *testing.T) {
	tracker := newSpinningTracker()

	tracker.Track("task-1", "sig-a")
	tracker.Track("task-1", "sig-a")
	tracker.Track("task-1", "sig-a")

	// Progress detected.
	count := tracker.Track("task-1", "sig-b")
	if count != 1 {
		t.Fatalf("Track() after progress = %d, want 1", count)
	}
}

func TestRunAgent_ExtractedOps_Integration(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	now := time.Now().UTC()

	state := testhelpers.CreateValidState()

	// Create a task ready for review
	taskID := "task-1"
	task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusReadyForReview, now)
	state.Tasks = []models.Task{task}

	testhelpers.WriteInitialState(t, statePath, state)

	// Test ClaimReviewerTask operation
	input := ops.ClaimReviewerTaskInput{
		ProjectRoot:   tmpDir,
		AgentID:       "code-reviewer-1",
		LeaseDuration: 300, // 5 minutes in seconds
	}
	result, err := ops.ClaimReviewerTask(input)
	if err != nil {
		t.Fatalf("ClaimReviewerTask failed: %v", err)
	}
	if result == nil {
		t.Fatalf("ClaimReviewerTask returned nil result")
	}
	if result.TaskID != taskID {
		t.Errorf("result.TaskID = %s, want %s", result.TaskID, taskID)
	}
}

func TestResumeHandoff_ExtractedOp_Integration(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	now := time.Now().UTC()

	state := testhelpers.CreateValidState()

	// Create a task with handoff pending
	taskID := "task-1"
	task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusImplementing, now)
	task.HandoffPending = true
	agentID := "coder-1"
	task.AssignedTo = &agentID
	task.Worktree = &tmpDir
	state.Tasks = []models.Task{task}
	state.Agents[agentID] = models.Agent{
		Role:   "coder",
		Status: models.AgentStatusHandoff,
	}

	testhelpers.WriteInitialState(t, statePath, state)

	// Test ResumeHandoff operation
	input := ops.ResumeHandoffInput{
		ProjectRoot: tmpDir,
		AgentID:     agentID,
	}
	result, err := ops.ResumeHandoff(input)
	if err != nil {
		t.Fatalf("ResumeHandoff failed: %v", err)
	}
	if result == nil {
		t.Fatalf("ResumeHandoff returned nil result")
	}
	if !result.Found {
		t.Errorf("ResumeHandoff should find handoff task")
	}
	if result.TaskID != taskID {
		t.Errorf("result.TaskID = %s, want %s", result.TaskID, taskID)
	}
}

func TestResumeHandoff_NotFound_Integration(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	now := time.Now().UTC()

	state := testhelpers.CreateValidState()

	// Create a task WITHOUT handoff pending
	taskID := "task-1"
	task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusImplementing, now)
	task.HandoffPending = false // Not pending
	agentID := "coder-1"
	task.AssignedTo = &agentID
	state.Tasks = []models.Task{task}

	testhelpers.WriteInitialState(t, statePath, state)

	// Test ResumeHandoff operation - should not find anything
	input := ops.ResumeHandoffInput{
		ProjectRoot: tmpDir,
		AgentID:     agentID,
	}
	result, err := ops.ResumeHandoff(input)
	if err != nil {
		t.Fatalf("ResumeHandoff failed: %v", err)
	}
	if result == nil {
		t.Fatalf("ResumeHandoff returned nil result")
	}
	if result.Found {
		t.Errorf("ResumeHandoff should NOT find handoff task when HandoffPending=false")
	}
}

// TestExtractedOps_BehavioralParity tests that the extracted ops functions
// maintain the same behavior as the original inline closures
func TestExtractedOps_BehavioralParity(t *testing.T) {
	t.Run("ClaimReviewerTask finds highest priority task", func(t *testing.T) {
		tmpDir := t.TempDir()
		statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
		testhelpers.SetupPipelineConfig(t, tmpDir)
		now := time.Now().UTC()

		state := testhelpers.CreateValidState()

		// Create multiple tasks with different priorities
		task1 := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReadyForReview, now)
		task1.Priority = 2
		task2 := testhelpers.BuildTaskByStatus("task-2", models.TaskStatusReadyForReview, now)
		task2.Priority = 1 // Higher priority (lower number)

		state.Tasks = []models.Task{task1, task2}

		testhelpers.WriteInitialState(t, statePath, state)

		input := ops.ClaimReviewerTaskInput{
			ProjectRoot:   tmpDir,
			AgentID:       "code-reviewer-1",
			LeaseDuration: 300,
		}
		result, err := ops.ClaimReviewerTask(input)
		if err != nil {
			t.Fatalf("ClaimReviewerTask failed: %v", err)
		}

		// Should claim the highest priority task (task-2 with priority 1)
		if result.TaskID != "task-2" {
			t.Errorf("expected task-2 (priority 1), got %s", result.TaskID)
		}
	})

	t.Run("ResumeHandoff uses correct worktree", func(t *testing.T) {
		tmpDir := t.TempDir()
		statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
		testhelpers.SetupPipelineConfig(t, tmpDir)
		now := time.Now().UTC()

		state := testhelpers.CreateValidState()

		taskID := "task-1"
		task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusImplementing, now)
		task.HandoffPending = true
		agentID := "coder-1"
		task.AssignedTo = &agentID
		expectedWorktree := "/worktrees/task-1"
		task.Worktree = &expectedWorktree
		state.Tasks = []models.Task{task}
		state.Agents[agentID] = models.Agent{
			Role:   "coder",
			Status: models.AgentStatusHandoff,
		}

		testhelpers.WriteInitialState(t, statePath, state)

		input := ops.ResumeHandoffInput{
			ProjectRoot: tmpDir,
			AgentID:     agentID,
		}
		result, err := ops.ResumeHandoff(input)
		if err != nil {
			t.Fatalf("ResumeHandoff failed: %v", err)
		}

		if result.Worktree != expectedWorktree {
			t.Errorf("worktree = %s, want %s", result.Worktree, expectedWorktree)
		}
	})
}

func BenchmarkClaimReviewerTask(b *testing.B) {
	tmpDir := b.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(&testing.T{}, tmpDir)
	testhelpers.SetupPipelineConfig(&testing.T{}, tmpDir)
	now := time.Now().UTC()

	state := testhelpers.CreateValidState()
	taskID := "task-1"
	task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusReadyForReview, now)
	state.Tasks = []models.Task{task}

	testhelpers.WriteInitialState(&testing.T{}, statePath, state)

	input := ops.ClaimReviewerTaskInput{
		ProjectRoot:   tmpDir,
		AgentID:       "code-reviewer-1",
		LeaseDuration: 300,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ops.ClaimReviewerTask(input)
	}
}

func BenchmarkResumeHandoff(b *testing.B) {
	tmpDir := b.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(&testing.T{}, tmpDir)
	testhelpers.SetupPipelineConfig(&testing.T{}, tmpDir)
	now := time.Now().UTC()

	state := testhelpers.CreateValidState()
	taskID := "task-1"
	task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusImplementing, now)
	task.HandoffPending = true
	agentID := "coder-1"
	task.AssignedTo = &agentID
	state.Tasks = []models.Task{task}
	state.Agents[agentID] = models.Agent{
		Role:   "coder",
		Status: models.AgentStatusHandoff,
	}

	testhelpers.WriteInitialState(&testing.T{}, statePath, state)

	input := ops.ResumeHandoffInput{
		ProjectRoot: tmpDir,
		AgentID:     agentID,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ops.ResumeHandoff(input)
	}
}

func TestCLISupportsStdin(t *testing.T) {
	tests := []struct {
		cli  string
		want bool
	}{
		{"claude", true},
		{"kimi", true},
		{"codex", true},
		{"gemini", true},
		{"vibe", false},
	}
	for _, tc := range tests {
		t.Run(tc.cli, func(t *testing.T) {
			if got := cliSupportsStdin(tc.cli); got != tc.want {
				t.Errorf("cliSupportsStdin(%q) = %v, want %v", tc.cli, got, tc.want)
			}
		})
	}
}

func TestBuildCodexArgs(t *testing.T) {
	t.Run("stdin without logging uses full-auto", func(t *testing.T) {
		args := buildCodexArgs("/tmp/project", "ignored", true, "")

		if !slices.Contains(args, "--full-auto") {
			t.Fatalf("args = %v, want --full-auto flag", args)
		}
		if slices.Contains(args, "--dangerously-bypass-approvals-and-sandbox") {
			t.Fatalf("args = %v, did not expect bypass flag", args)
		}
		if !slices.Contains(args, "exec") || !slices.Contains(args, "-") {
			t.Fatalf("args = %v, want stdin exec invocation", args)
		}
		if slices.Contains(args, "--json") {
			t.Fatalf("args = %v, did not expect --json without logging", args)
		}
		for _, a := range args {
			if strings.Contains(a, "mcp_servers") {
				t.Fatalf("args = %v, did not expect mcp_servers config", args)
			}
		}
	})

	t.Run("prompt with logging emits json", func(t *testing.T) {
		args := buildCodexArgs("/tmp/project", "do the thing", false, "/tmp/logs")

		if !slices.Contains(args, "do the thing") {
			t.Fatalf("args = %v, want prompt argument", args)
		}
		if !slices.Contains(args, "--json") {
			t.Fatalf("args = %v, want --json when logging enabled", args)
		}
		if !slices.Contains(args, "--full-auto") {
			t.Fatalf("args = %v, want --full-auto flag", args)
		}
		for _, a := range args {
			if strings.Contains(a, "mcp_servers") {
				t.Fatalf("args = %v, did not expect mcp_servers config", args)
			}
		}
	})
}

func TestNewDefaultCLIExecutor(t *testing.T) {
	t.Run("empty outputsDir disables logging", func(t *testing.T) {
		e := NewDefaultCLIExecutor("")
		if e.outputsDir != "" {
			t.Errorf("outputsDir should be empty, got %q", e.outputsDir)
		}
	})

	t.Run("non-empty outputsDir enables logging", func(t *testing.T) {
		dir := t.TempDir()
		e := NewDefaultCLIExecutor(dir)
		if e.outputsDir != dir {
			t.Errorf("outputsDir = %q, want %q", e.outputsDir, dir)
		}
	})
}
