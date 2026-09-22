package ops

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/procscan"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestClearStaleReviewClaims_LeaseFirstOwnership(t *testing.T) {
	for _, passive := range []bool{false, true} {
		mode := "active"
		if passive {
			mode = "passive"
		}
		for _, tt := range []struct {
			name        string
			raw         procscan.AgentProcessState
			mutate      func(*models.Task, *models.Agent)
			wantCleared int
		}{
			{name: "fresh dead PID", raw: procscan.AgentProcessDead},
			{name: "fresh mismatched PID", raw: procscan.AgentProcessMismatched},
			{name: "expired review lease", raw: procscan.AgentProcessDead, wantCleared: 1,
				mutate: func(task *models.Task, _ *models.Agent) {
					task.ReviewLeaseExpires = testhelpers.TimePtr(time.Now().Add(-time.Minute))
				}},
			{name: "expired registration lease", raw: procscan.AgentProcessDead, wantCleared: 1,
				mutate: func(_ *models.Task, agent *models.Agent) {
					agent.LeaseExpires = testhelpers.TimePtr(time.Now().Add(-time.Minute))
				}},
			{name: "missing heartbeat", raw: procscan.AgentProcessDead, wantCleared: 1,
				mutate: func(_ *models.Task, agent *models.Agent) { agent.Heartbeat = time.Time{} }},
			{name: "wrong reviewer role", raw: procscan.AgentProcessMismatched, wantCleared: 1,
				mutate: func(_ *models.Task, agent *models.Agent) { agent.Role = models.RoleCoder }},
			{name: "idle reviewer", raw: procscan.AgentProcessMismatched, wantCleared: 1,
				mutate: func(_ *models.Task, agent *models.Agent) { agent.Status = models.AgentStatusIdle }},
			{name: "different current task", raw: procscan.AgentProcessMismatched, wantCleared: 1,
				mutate: func(_ *models.Task, agent *models.Agent) { agent.CurrentTask = testhelpers.StringPtr("other-task") }},
		} {
			t.Run(mode+"/"+tt.name, func(t *testing.T) {
				root := t.TempDir()
				testhelpers.SetupTestGitRepo(t, root)
				stateFile, _ := testhelpers.SetupLizaDir(t, root)
				setupLogFile(t, root)
				procRoot := t.TempDir()
				t.Cleanup(SetAgentProcessProcRootForTest(procRoot))
				now := time.Now().UTC()
				reviewer := "code-reviewer-1"
				status, agentStatus := models.TaskStatusReviewing, models.AgentStatusReviewing
				if passive {
					status, agentStatus = models.TaskStatusRejected, models.AgentStatusWaiting
				}
				task := testhelpers.BuildTaskByStatus("lease-first-review", status, now)
				task.ReviewingBy = &reviewer
				task.ReviewLeaseExpires = testhelpers.TimePtr(now.Add(30 * time.Minute))
				agent := testhelpers.RegisteredTestAgent(models.RoleCodeReviewer)
				agent.PID = 987654321
				agent.Heartbeat = now
				agent.LeaseExpires = testhelpers.TimePtr(now.Add(30 * time.Minute))
				agent.Status = agentStatus
				agent.CurrentTask = testhelpers.StringPtr(task.ID)
				if tt.mutate != nil {
					tt.mutate(&task, &agent)
				}
				if tt.raw == procscan.AgentProcessMismatched {
					writeAgentProcessCmdline(t, procRoot, agent.PID, []string{"unrelated-process"})
				}
				if got := AgentProcessStatus(reviewer, agent).State; got != tt.raw {
					t.Fatalf("process fixture = %q, want %q", got, tt.raw)
				}
				state := testhelpers.CreateValidState()
				state.Tasks = []models.Task{task}
				state.Agents = map[string]models.Agent{reviewer: agent}
				testhelpers.WriteInitialState(t, stateFile, state)
				before := readStateForTest(t, stateFile)
				cleared, err := ClearStaleReviewClaims(root)
				if err != nil {
					t.Fatal(err)
				}
				if cleared != tt.wantCleared {
					t.Fatalf("cleared = %d, want %d", cleared, tt.wantCleared)
				}
				after := readStateForTest(t, stateFile)
				gotTask := after.FindTask(task.ID)
				gotAgent := after.Agents[reviewer]
				if tt.wantCleared == 0 {
					if !reflect.DeepEqual(gotTask, before.FindTask(task.ID)) || !reflect.DeepEqual(gotAgent, before.Agents[reviewer]) {
						t.Fatal("lease-first preservation changed task or reviewer state")
					}
					return
				}
				wantStatus := models.TaskStatusReadyForReview
				if passive {
					wantStatus = status
				}
				if gotTask.Status != wantStatus || gotTask.ReviewingBy != nil || gotTask.ReviewLeaseExpires != nil {
					t.Fatalf("stale ownership not cleared correctly: %+v", gotTask)
				}
				if *agent.CurrentTask == task.ID {
					if gotAgent.CurrentTask != nil || gotAgent.Status != models.AgentStatusIdle {
						t.Fatalf("stale reviewer not released: %+v", gotAgent)
					}
				} else if !reflect.DeepEqual(gotAgent, before.Agents[reviewer]) {
					t.Fatal("cleanup changed reviewer assigned to another task")
				}
			})
		}
	}
}

func TestClearStaleReviewClaims_NoStale(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	setupLogFile(t, tmpDir)
	t.Cleanup(SetAgentProcessProcRootForTest(filepath.Join(t.TempDir(), "missing-proc")))

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()

	// REVIEWING task with future lease — not stale
	futureLease := now.Add(30 * time.Minute)
	reviewer := "code-reviewer-1"
	state.Tasks = []models.Task{
		{
			ID: "t1", Description: "Active review", Status: models.TaskStatusReviewing,
			Priority: 1, Created: now, SpecRef: "README.md", DoneWhen: "Done", Scope: "Test",
			RolePair:    "coding-pair",
			ReviewingBy: &reviewer, ReviewLeaseExpires: &futureLease,
			History: []models.TaskHistoryEntry{},
		},
	}
	state.Agents[reviewer] = models.Agent{
		Role:        "code-reviewer",
		Status:      models.AgentStatusReviewing,
		CurrentTask: testhelpers.StringPtr("t1"),
		PID:         os.Getpid(),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	cleared, err := ClearStaleReviewClaims(tmpDir)
	if err != nil {
		t.Fatalf("ClearStaleReviewClaims() error: %v", err)
	}
	if cleared != 0 {
		t.Errorf("cleared = %d, want 0", cleared)
	}
}

func TestClearStaleReviewClaims_ExpiredLease(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	setupLogFile(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()

	// REVIEWING task with expired lease
	expiredLease := now.Add(-5 * time.Minute)
	reviewer := "code-reviewer-1"
	coder := "coder-1"
	state.Tasks = []models.Task{
		{
			ID: "t1", Description: "Stale review", Status: models.TaskStatusReviewing,
			Priority: 1, Created: now, SpecRef: "README.md", DoneWhen: "Done", Scope: "Test",
			RolePair:   "coding-pair",
			AssignedTo: &coder, ReviewingBy: &reviewer, ReviewLeaseExpires: &expiredLease,
			History: []models.TaskHistoryEntry{},
		},
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	cleared, err := ClearStaleReviewClaims(tmpDir)
	if err != nil {
		t.Fatalf("ClearStaleReviewClaims() error: %v", err)
	}
	if cleared != 1 {
		t.Errorf("cleared = %d, want 1", cleared)
	}

	// Verify state: should be READY_FOR_REVIEW, reviewer cleared
	readState := readStateForTest(t, stateFile)
	task := readState.FindTask("t1")
	if task == nil {
		t.Fatal("Task not found")
	}
	if task.Status != models.TaskStatusReadyForReview {
		t.Errorf("Status = %v, want CODE_READY_FOR_REVIEW", task.Status)
	}
	if task.ReviewingBy != nil {
		t.Errorf("ReviewingBy should be nil, got %v", *task.ReviewingBy)
	}
	if task.ReviewLeaseExpires != nil {
		t.Error("ReviewLeaseExpires should be nil")
	}
}

func TestClearStaleReviewClaims_MissingLease(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	setupLogFile(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()

	// REVIEWING task with reviewer but no lease (malformed state)
	reviewer := "code-reviewer-1"
	state.Tasks = []models.Task{
		{
			ID: "t1", Description: "Malformed review", Status: models.TaskStatusReviewing,
			Priority: 1, Created: now, SpecRef: "README.md", DoneWhen: "Done", Scope: "Test",
			RolePair:    "coding-pair",
			ReviewingBy: &reviewer, // no ReviewLeaseExpires
			History:     []models.TaskHistoryEntry{},
		},
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	cleared, err := ClearStaleReviewClaims(tmpDir)
	if err != nil {
		t.Fatalf("ClearStaleReviewClaims() error: %v", err)
	}
	if cleared != 1 {
		t.Errorf("cleared = %d, want 1 (malformed lease treated as expired)", cleared)
	}
}

func TestClearStaleReviewClaims_SkipsNonReviewing(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	setupLogFile(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()

	// IMPLEMENTING task should be skipped entirely
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("t1", models.TaskStatusImplementing, now),
		testhelpers.BuildTaskByStatus("t2", models.TaskStatusReady, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	cleared, err := ClearStaleReviewClaims(tmpDir)
	if err != nil {
		t.Fatalf("ClearStaleReviewClaims() error: %v", err)
	}
	if cleared != 0 {
		t.Errorf("cleared = %d, want 0", cleared)
	}
}

func TestClearStaleReviewClaims_MultipleStale(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	setupLogFile(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()

	expiredLease := now.Add(-10 * time.Minute)
	reviewer1 := "code-reviewer-1"
	reviewer2 := "code-reviewer-2"
	state.Tasks = []models.Task{
		{
			ID: "t1", Description: "Stale 1", Status: models.TaskStatusReviewing,
			Priority: 1, Created: now, SpecRef: "README.md", DoneWhen: "Done", Scope: "Test",
			RolePair:    "coding-pair",
			ReviewingBy: &reviewer1, ReviewLeaseExpires: &expiredLease,
			History: []models.TaskHistoryEntry{},
		},
		{
			ID: "t2", Description: "Stale 2", Status: models.TaskStatusReviewing,
			Priority: 1, Created: now, SpecRef: "README.md", DoneWhen: "Done", Scope: "Test",
			RolePair:    "coding-pair",
			ReviewingBy: &reviewer2, ReviewLeaseExpires: &expiredLease,
			History: []models.TaskHistoryEntry{},
		},
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	cleared, err := ClearStaleReviewClaims(tmpDir)
	if err != nil {
		t.Fatalf("ClearStaleReviewClaims() error: %v", err)
	}
	if cleared != 2 {
		t.Errorf("cleared = %d, want 2", cleared)
	}
}

func TestClearStaleReviewingTwo(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	setupLogFile(t, tmpDir)

	now := time.Now().UTC()

	t.Run("expired reviewing_2 reverts to partially_approved", func(t *testing.T) {
		state := testhelpers.CreateValidState()
		expiredLease := now.Add(-5 * time.Minute)
		reviewer := "code-reviewer-2"
		coder := "coder-1"
		state.Tasks = []models.Task{
			{
				ID: "t1", Description: "Second review stale", Status: "REVIEWING_CODE_2",
				Priority: 1, Created: now, SpecRef: "README.md", DoneWhen: "Done", Scope: "Test",
				RolePair:   "coding-pair",
				AssignedTo: &coder, ReviewingBy: &reviewer, ReviewLeaseExpires: &expiredLease,
				History: []models.TaskHistoryEntry{},
			},
		}
		testhelpers.WriteInitialState(t, stateFile, state)

		cleared, err := ClearStaleReviewClaims(tmpDir)
		if err != nil {
			t.Fatalf("ClearStaleReviewClaims() error: %v", err)
		}
		if cleared != 1 {
			t.Errorf("cleared = %d, want 1", cleared)
		}

		readState := readStateForTest(t, stateFile)
		task := readState.FindTask("t1")
		if task == nil {
			t.Fatal("Task not found")
		}
		if task.Status != "CODE_PARTIALLY_APPROVED" {
			t.Errorf("Status = %v, want CODE_PARTIALLY_APPROVED", task.Status)
		}
		if task.ReviewingBy != nil {
			t.Errorf("ReviewingBy should be nil, got %v", *task.ReviewingBy)
		}
		if task.ReviewLeaseExpires != nil {
			t.Error("ReviewLeaseExpires should be nil")
		}
	})

	t.Run("expired reviewing still reverts to submitted", func(t *testing.T) {
		state := testhelpers.CreateValidState()
		expiredLease := now.Add(-5 * time.Minute)
		reviewer := "code-reviewer-1"
		coder := "coder-1"
		state.Tasks = []models.Task{
			{
				ID: "t1", Description: "First review stale", Status: models.TaskStatusReviewing,
				Priority: 1, Created: now, SpecRef: "README.md", DoneWhen: "Done", Scope: "Test",
				RolePair:   "coding-pair",
				AssignedTo: &coder, ReviewingBy: &reviewer, ReviewLeaseExpires: &expiredLease,
				History: []models.TaskHistoryEntry{},
			},
		}
		testhelpers.WriteInitialState(t, stateFile, state)

		cleared, err := ClearStaleReviewClaims(tmpDir)
		if err != nil {
			t.Fatalf("ClearStaleReviewClaims() error: %v", err)
		}
		if cleared != 1 {
			t.Errorf("cleared = %d, want 1", cleared)
		}

		readState := readStateForTest(t, stateFile)
		task := readState.FindTask("t1")
		if task == nil {
			t.Fatal("Task not found")
		}
		if task.Status != models.TaskStatusReadyForReview {
			t.Errorf("Status = %v, want CODE_READY_FOR_REVIEW", task.Status)
		}
	})

	t.Run("mixed reviewing and reviewing_2 both cleared", func(t *testing.T) {
		state := testhelpers.CreateValidState()
		expiredLease := now.Add(-5 * time.Minute)
		reviewer1 := "code-reviewer-1"
		reviewer2 := "code-reviewer-2"
		coder := "coder-1"
		state.Tasks = []models.Task{
			{
				ID: "t1", Description: "First review stale", Status: models.TaskStatusReviewing,
				Priority: 1, Created: now, SpecRef: "README.md", DoneWhen: "Done", Scope: "Test",
				RolePair:   "coding-pair",
				AssignedTo: &coder, ReviewingBy: &reviewer1, ReviewLeaseExpires: &expiredLease,
				History: []models.TaskHistoryEntry{},
			},
			{
				ID: "t2", Description: "Second review stale", Status: "REVIEWING_CODE_2",
				Priority: 1, Created: now, SpecRef: "README.md", DoneWhen: "Done", Scope: "Test",
				RolePair:   "coding-pair",
				AssignedTo: &coder, ReviewingBy: &reviewer2, ReviewLeaseExpires: &expiredLease,
				History: []models.TaskHistoryEntry{},
			},
		}
		testhelpers.WriteInitialState(t, stateFile, state)

		cleared, err := ClearStaleReviewClaims(tmpDir)
		if err != nil {
			t.Fatalf("ClearStaleReviewClaims() error: %v", err)
		}
		if cleared != 2 {
			t.Errorf("cleared = %d, want 2", cleared)
		}

		readState := readStateForTest(t, stateFile)
		t1 := readState.FindTask("t1")
		if t1.Status != models.TaskStatusReadyForReview {
			t.Errorf("t1 Status = %v, want CODE_READY_FOR_REVIEW", t1.Status)
		}
		t2 := readState.FindTask("t2")
		if t2.Status != "CODE_PARTIALLY_APPROVED" {
			t.Errorf("t2 Status = %v, want CODE_PARTIALLY_APPROVED", t2.Status)
		}
	})
}

func TestClearStaleReviewClaims_OrphanedOnRejected(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	setupLogFile(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()

	// REJECTED task with ReviewingBy set and expired lease — orphaned from await_resubmission crash
	expiredLease := now.Add(-5 * time.Minute)
	reviewer := "code-reviewer-1"
	state.Tasks = []models.Task{
		{
			ID: "t1", Description: "Rejected with orphaned reviewer", Status: models.TaskStatusRejected,
			Priority: 1, Created: now, SpecRef: "README.md", DoneWhen: "Done", Scope: "Test",
			RolePair:    "coding-pair",
			ReviewingBy: &reviewer, ReviewLeaseExpires: &expiredLease,
			History: []models.TaskHistoryEntry{},
		},
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	cleared, err := ClearStaleReviewClaims(tmpDir)
	if err != nil {
		t.Fatalf("ClearStaleReviewClaims() error: %v", err)
	}
	if cleared != 1 {
		t.Errorf("cleared = %d, want 1", cleared)
	}

	readState := readStateForTest(t, stateFile)
	task := readState.FindTask("t1")
	if task == nil {
		t.Fatal("Task not found")
	}
	if task.Status != models.TaskStatusRejected {
		t.Errorf("Status = %v, want CODE_REJECTED (status should not change)", task.Status)
	}
	if task.ReviewingBy != nil {
		t.Errorf("ReviewingBy should be nil, got %v", *task.ReviewingBy)
	}
	if task.ReviewLeaseExpires != nil {
		t.Error("ReviewLeaseExpires should be nil")
	}
}

func TestClearStaleReviewClaims_OrphanedOnSubmitted(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	setupLogFile(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()

	// SUBMITTED task with ReviewingBy set and expired lease — orphaned from await_resubmission crash
	expiredLease := now.Add(-5 * time.Minute)
	reviewer := "code-reviewer-1"
	state.Tasks = []models.Task{
		{
			ID: "t1", Description: "Submitted with orphaned reviewer", Status: models.TaskStatusReadyForReview,
			Priority: 1, Created: now, SpecRef: "README.md", DoneWhen: "Done", Scope: "Test",
			RolePair:    "coding-pair",
			ReviewingBy: &reviewer, ReviewLeaseExpires: &expiredLease,
			History: []models.TaskHistoryEntry{},
		},
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	cleared, err := ClearStaleReviewClaims(tmpDir)
	if err != nil {
		t.Fatalf("ClearStaleReviewClaims() error: %v", err)
	}
	if cleared != 1 {
		t.Errorf("cleared = %d, want 1", cleared)
	}

	readState := readStateForTest(t, stateFile)
	task := readState.FindTask("t1")
	if task == nil {
		t.Fatal("Task not found")
	}
	if task.Status != models.TaskStatusReadyForReview {
		t.Errorf("Status = %v, want CODE_READY_FOR_REVIEW (status should not change)", task.Status)
	}
	if task.ReviewingBy != nil {
		t.Errorf("ReviewingBy should be nil, got %v", *task.ReviewingBy)
	}
	if task.ReviewLeaseExpires != nil {
		t.Error("ReviewLeaseExpires should be nil")
	}
}

func TestClearStaleReviewClaims_FutureLeaseMissingReviewerAgent(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	setupLogFile(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()

	// REJECTED task with ReviewingBy set and valid (future) lease, but no
	// reviewer agent row — passive ownership has no live observer.
	futureLease := now.Add(30 * time.Minute)
	reviewer := "code-reviewer-1"
	state.Tasks = []models.Task{
		{
			ID: "t1", Description: "Rejected with active reviewer", Status: models.TaskStatusRejected,
			Priority: 1, Created: now, SpecRef: "README.md", DoneWhen: "Done", Scope: "Test",
			RolePair:    "coding-pair",
			ReviewingBy: &reviewer, ReviewLeaseExpires: &futureLease,
			History: []models.TaskHistoryEntry{},
		},
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	cleared, err := ClearStaleReviewClaims(tmpDir)
	if err != nil {
		t.Fatalf("ClearStaleReviewClaims() error: %v", err)
	}
	if cleared != 1 {
		t.Errorf("cleared = %d, want 1", cleared)
	}

	readState := readStateForTest(t, stateFile)
	task := readState.FindTask("t1")
	if task == nil {
		t.Fatal("Task not found")
	}
	if task.ReviewingBy != nil {
		t.Errorf("ReviewingBy should be nil, got %v", *task.ReviewingBy)
	}
	if task.ReviewLeaseExpires != nil {
		t.Error("ReviewLeaseExpires should be nil")
	}
}

func TestClearStaleReviewClaims_FutureLeaseWithLiveUnknownReviewerProcess(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	setupLogFile(t, tmpDir)
	t.Cleanup(SetAgentProcessProcRootForTest(filepath.Join(t.TempDir(), "missing-proc")))

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()

	futureLease := now.Add(30 * time.Minute)
	reviewer := "code-reviewer-1"
	state.Tasks = []models.Task{
		{
			ID: "t1", Description: "Rejected with active live-unknown reviewer", Status: models.TaskStatusRejected,
			Priority: 1, Created: now, SpecRef: "README.md", DoneWhen: "Done", Scope: "Test",
			RolePair:    "coding-pair",
			ReviewingBy: &reviewer, ReviewLeaseExpires: &futureLease,
			History: []models.TaskHistoryEntry{},
		},
	}
	state.Agents[reviewer] = models.Agent{
		Role:        "code-reviewer",
		Status:      models.AgentStatusWaiting,
		CurrentTask: testhelpers.StringPtr("t1"),
		PID:         os.Getpid(),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	cleared, err := ClearStaleReviewClaims(tmpDir)
	if err != nil {
		t.Fatalf("ClearStaleReviewClaims() error: %v", err)
	}
	if cleared != 0 {
		t.Errorf("cleared = %d, want 0 (live/unknown observer should be preserved)", cleared)
	}

	readState := readStateForTest(t, stateFile)
	task := readState.FindTask("t1")
	if task == nil {
		t.Fatal("Task not found")
	}
	if task.ReviewingBy == nil || *task.ReviewingBy != reviewer {
		t.Fatalf("ReviewingBy = %v, want %v", task.ReviewingBy, reviewer)
	}
	if task.ReviewLeaseExpires == nil {
		t.Fatal("ReviewLeaseExpires should remain set")
	}
}

func TestClearStaleReviewClaims_SubmittedTaskWithLiveWaitingReviewerPreserved(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	setupLogFile(t, tmpDir)
	t.Cleanup(SetAgentProcessProcRootForTest(filepath.Join(t.TempDir(), "missing-proc")))

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()

	futureLease := now.Add(30 * time.Minute)
	reviewer := "code-reviewer-1"
	state.Tasks = []models.Task{
		{
			ID: "t1", Description: "Submitted while reviewer awaits reclaim", Status: models.TaskStatusReadyForReview,
			Priority: 1, Created: now, SpecRef: "README.md", DoneWhen: "Done", Scope: "Test",
			RolePair:    "coding-pair",
			ReviewingBy: &reviewer, ReviewLeaseExpires: &futureLease,
			History: []models.TaskHistoryEntry{},
		},
	}
	state.Agents[reviewer] = models.Agent{
		Role:        "code-reviewer",
		Status:      models.AgentStatusWaiting,
		CurrentTask: testhelpers.StringPtr("t1"),
		PID:         os.Getpid(),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	cleared, err := ClearStaleReviewClaims(tmpDir)
	if err != nil {
		t.Fatalf("ClearStaleReviewClaims() error: %v", err)
	}
	if cleared != 0 {
		t.Errorf("cleared = %d, want 0 (submitted reclaim window should be preserved)", cleared)
	}

	readState := readStateForTest(t, stateFile)
	task := readState.FindTask("t1")
	if task == nil {
		t.Fatal("Task not found")
	}
	if task.ReviewingBy == nil || *task.ReviewingBy != reviewer {
		t.Fatalf("ReviewingBy = %v, want %v", task.ReviewingBy, reviewer)
	}
	if task.ReviewLeaseExpires == nil {
		t.Fatal("ReviewLeaseExpires should remain set")
	}
}

func TestClearStaleReviewClaims_FutureLeaseClearsWhenAgentNotObservingTask(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	setupLogFile(t, tmpDir)
	t.Cleanup(SetAgentProcessProcRootForTest(filepath.Join(t.TempDir(), "missing-proc")))

	now := time.Now().UTC()
	futureLease := now.Add(30 * time.Minute)
	reviewer := "code-reviewer-1"

	tests := []struct {
		name        string
		status      models.TaskStatus
		agentStatus models.AgentStatus
		currentTask *string
	}{
		{
			name:        "passive claim with idle agent",
			status:      models.TaskStatusRejected,
			agentStatus: models.AgentStatusIdle,
			currentTask: testhelpers.StringPtr("t1"),
		},
		{
			name:        "passive claim with mismatched current task",
			status:      models.TaskStatusRejected,
			agentStatus: models.AgentStatusWaiting,
			currentTask: testhelpers.StringPtr("other-task"),
		},
		{
			name:        "active review with mismatched current task",
			status:      models.TaskStatusReviewing,
			agentStatus: models.AgentStatusReviewing,
			currentTask: testhelpers.StringPtr("other-task"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := testhelpers.CreateValidState()
			state.Tasks = []models.Task{
				{
					ID: "t1", Description: "Future lease without matching observer", Status: tt.status,
					Priority: 1, Created: now, SpecRef: "README.md", DoneWhen: "Done", Scope: "Test",
					RolePair:    "coding-pair",
					ReviewingBy: &reviewer, ReviewLeaseExpires: &futureLease,
					History: []models.TaskHistoryEntry{},
				},
			}
			state.Agents[reviewer] = models.Agent{
				Role:        "code-reviewer",
				Status:      tt.agentStatus,
				CurrentTask: tt.currentTask,
				PID:         os.Getpid(),
			}
			testhelpers.WriteInitialState(t, stateFile, state)

			cleared, err := ClearStaleReviewClaims(tmpDir)
			if err != nil {
				t.Fatalf("ClearStaleReviewClaims() error: %v", err)
			}
			if cleared != 1 {
				t.Fatalf("cleared = %d, want 1", cleared)
			}

			readState := readStateForTest(t, stateFile)
			task := readState.FindTask("t1")
			if task == nil {
				t.Fatal("Task not found")
			}
			if task.ReviewingBy != nil {
				t.Errorf("ReviewingBy should be nil, got %v", *task.ReviewingBy)
			}
			if task.ReviewLeaseExpires != nil {
				t.Error("ReviewLeaseExpires should be nil")
			}
		})
	}
}

// setupLogFile creates the log.yaml file that ClearStaleReviewClaims needs.
func setupLogFile(t *testing.T, tmpDir string) {
	t.Helper()
	logPath := filepath.Join(tmpDir, paths.ProjectDirName(), "log.yaml")
	if err := os.WriteFile(logPath, []byte(""), 0644); err != nil {
		t.Fatalf("Failed to create log file: %v", err)
	}
	// Also create the lock file for log
	lockPath := logPath + ".lock"
	if err := os.WriteFile(lockPath, []byte(""), 0644); err != nil {
		t.Fatalf("Failed to create log lock file: %v", err)
	}
}
