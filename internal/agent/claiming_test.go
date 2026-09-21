package agent

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestClaimDoerTask_ResumesOwnedTaskBeforeFreshClaim(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.CreateTestWorktree(t, tmpDir, "task-owned")

	now := time.Now().UTC()
	agentID := "coder-1"
	owned := testhelpers.BuildTaskByStatus("task-owned", models.TaskStatusImplementing, now)
	owned.AssignedTo = &agentID
	fresh := testhelpers.BuildTaskByStatus("task-fresh", models.TaskStatusReady, now)

	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{fresh, owned}
	leaseExpires := now.Add(30 * time.Minute)
	state.Agents[agentID] = models.Agent{
		Role:         models.RoleCoder,
		Status:       models.AgentStatusIdle,
		CurrentTask:  nil,
		LeaseExpires: &leaseExpires,
		Heartbeat:    now,
		Terminal:     "test",
		Provider:     "test",
		PID:          os.Getpid(),
	}
	testhelpers.WriteInitialState(t, statePath, state)

	taskID, worktree, err := claimDoerTask(tmpDir, agentID, models.RoleCoder, db.New(statePath))
	if err != nil {
		t.Fatalf("claimDoerTask() error = %v", err)
	}
	if taskID != "task-owned" {
		t.Fatalf("taskID = %q, want task-owned", taskID)
	}
	if worktree != ".worktrees/task-owned" {
		t.Errorf("worktree = %q, want .worktrees/task-owned", worktree)
	}
}

func TestClaimDoerTask_ResumesHandoffBeforeOwnedTask(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	agentID := "coder-1"
	handoff := testhelpers.BuildTaskByStatus("task-handoff", models.TaskStatusImplementing, now)
	handoff.AssignedTo = &agentID
	handoff.HandoffPending = true
	owned := testhelpers.BuildTaskByStatus("task-owned", models.TaskStatusImplementing, now)
	owned.AssignedTo = &agentID

	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{owned, handoff}
	state.Agents[agentID] = models.Agent{
		Role:        models.RoleCoder,
		Status:      models.AgentStatusWorking,
		CurrentTask: &handoff.ID,
		Heartbeat:   now,
		Terminal:    "test",
	}
	testhelpers.WriteInitialState(t, statePath, state)

	taskID, _, err := claimDoerTask(tmpDir, agentID, models.RoleCoder, db.New(statePath))
	if err != nil {
		t.Fatalf("claimDoerTask() error = %v", err)
	}
	if taskID != "task-handoff" {
		t.Fatalf("taskID = %q, want task-handoff", taskID)
	}
}

func TestClaimDoerTask_ResumesEpicPlannerHandoff(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	agentID := "epic-planner-1"
	worktree := ".worktrees/epic-1"
	task := models.Task{
		ID:             "epic-1",
		Type:           models.TaskTypeEpicPlanning,
		Description:    "Test epic",
		Status:         models.TaskStatus("EPIC_PLANNING"),
		Priority:       1,
		Created:        now,
		SpecRef:        "README.md",
		DoneWhen:       "Epic is planned",
		Scope:          "Test scope",
		RolePair:       "epic-planning-pair",
		AssignedTo:     &agentID,
		Worktree:       &worktree,
		HandoffPending: true,
		History:        []models.TaskHistoryEntry{},
	}
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{task}
	state.Agents[agentID] = models.Agent{
		Role:        models.RoleEpicPlanner,
		Status:      models.AgentStatusWorking,
		CurrentTask: &task.ID,
		Heartbeat:   now,
		Terminal:    "test",
	}
	testhelpers.WriteInitialState(t, statePath, state)

	taskID, gotWorktree, err := claimDoerTask(tmpDir, agentID, models.RoleEpicPlanner, db.New(statePath))
	if err != nil {
		t.Fatalf("claimDoerTask() error = %v", err)
	}
	if taskID != "epic-1" {
		t.Fatalf("taskID = %q, want epic-1", taskID)
	}
	if gotWorktree != worktree {
		t.Errorf("worktree = %q, want %q", gotWorktree, worktree)
	}

	readState, err := db.New(statePath).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	readTask := readState.FindTask("epic-1")
	if readTask == nil {
		t.Fatal("epic-1 not found")
	}
	if readTask.HandoffPending {
		t.Fatal("handoff_pending = true, want false")
	}
	if len(readTask.History) == 0 || readTask.History[len(readTask.History)-1].Event != models.TaskEventHandoffResumed {
		t.Errorf("last history event = %v, want %s", readTask.History, models.TaskEventHandoffResumed)
	}
}

// TestHasPendingMerges tests the hasPendingMerges function
func TestHasPendingMerges(t *testing.T) {
	tests := []struct {
		name     string
		tasks    []models.Task
		agentID  string
		expected bool
	}{
		{
			name:     "no tasks returns false",
			tasks:    []models.Task{},
			agentID:  "code-reviewer-1",
			expected: false,
		},
		{
			name: "approved task by this agent without merge_commit returns true",
			tasks: []models.Task{
				{
					ID:        "task-1",
					Status:    models.TaskStatusApproved,
					RolePair:  "coding-pair",
					Approvals: []models.Approval{{Agent: "code-reviewer-1", Provider: "claude"}},
				},
			},
			agentID:  "code-reviewer-1",
			expected: true,
		},
		{
			name: "approved task with merge_commit set returns false",
			tasks: []models.Task{
				{
					ID:          "task-1",
					Status:      models.TaskStatusApproved,
					RolePair:    "coding-pair",
					Approvals:   []models.Approval{{Agent: "code-reviewer-1", Provider: "claude"}},
					MergeCommit: testhelpers.StringPtr("abc123"),
				},
			},
			agentID:  "code-reviewer-1",
			expected: false,
		},
		{
			name: "approved task with merge_commit and integration_failure returns true",
			tasks: []models.Task{
				{
					ID:                 "task-1",
					Status:             models.TaskStatusApproved,
					RolePair:           "coding-pair",
					Approvals:          []models.Approval{{Agent: "code-reviewer-1", Provider: "claude"}},
					MergeCommit:        testhelpers.StringPtr("abc123"),
					IntegrationFailure: map[string]any{"reason": "post-merge state validation failed"},
				},
			},
			agentID:  "code-reviewer-1",
			expected: true,
		},
		{
			name: "approved task with merge_commit and empty integration_failure returns false",
			tasks: []models.Task{
				{
					ID:                 "task-1",
					Status:             models.TaskStatusApproved,
					RolePair:           "coding-pair",
					Approvals:          []models.Approval{{Agent: "code-reviewer-1", Provider: "claude"}},
					MergeCommit:        testhelpers.StringPtr("abc123"),
					IntegrationFailure: map[string]any{},
				},
			},
			agentID:  "code-reviewer-1",
			expected: false,
		},
		{
			name: "approved task by different agent returns false",
			tasks: []models.Task{
				{
					ID:        "task-1",
					Status:    models.TaskStatusApproved,
					RolePair:  "coding-pair",
					Approvals: []models.Approval{{Agent: "code-reviewer-2", Provider: "codex"}},
				},
			},
			agentID:  "code-reviewer-1",
			expected: false,
		},
		{
			name: "multiple tasks, one pending returns true",
			tasks: []models.Task{
				{
					ID:          "task-1",
					Status:      models.TaskStatusApproved,
					RolePair:    "coding-pair",
					Approvals:   []models.Approval{{Agent: "code-reviewer-1", Provider: "claude"}},
					MergeCommit: testhelpers.StringPtr("abc123"),
				},
				{
					ID:        "task-2",
					Status:    models.TaskStatusApproved,
					RolePair:  "coding-pair",
					Approvals: []models.Approval{{Agent: "code-reviewer-1", Provider: "claude"}},
				},
			},
			agentID:  "code-reviewer-1",
			expected: true,
		},
		{
			name: "coding_plan_approved task by this agent without merge_commit returns true",
			tasks: []models.Task{
				{
					ID:        "task-1",
					Status:    models.TaskStatusCodingPlanApproved,
					RolePair:  "code-planning-pair",
					Approvals: []models.Approval{{Agent: "code-reviewer-1", Provider: "claude"}},
				},
			},
			agentID:  "code-reviewer-1",
			expected: true,
		},
		{
			name: "integration_failed task returns false",
			tasks: []models.Task{
				{
					ID:        "task-1",
					Status:    models.TaskStatusIntegrationFailed,
					Approvals: []models.Approval{{Agent: "code-reviewer-1", Provider: "claude"}},
				},
			},
			agentID:  "code-reviewer-1",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
			pr, _ := ops.LoadResolverForModels(tmpDir)

			state := testhelpers.CreateValidState()
			state.Tasks = tt.tasks
			registerEligibleMergeTestAgents(state, tt.tasks, tt.agentID, pr)

			testhelpers.WriteInitialState(t, statePath, state)

			bb := db.New(statePath)

			result := hasPendingMerges(bb, tt.agentID, pr)
			if result != tt.expected {
				t.Errorf("hasPendingMerges() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

// TestHasPendingMerges_Pipeline tests pipeline-aware merge detection
func TestHasPendingMerges_Pipeline(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	// Install frozen pipeline config
	src, err := os.ReadFile(findPipelineTestdata(t))
	if err != nil {
		t.Fatalf("Failed to read pipeline testdata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, paths.ProjectDirName(), "pipeline.yaml"), src, 0644); err != nil {
		t.Fatalf("Failed to write frozen pipeline config: %v", err)
	}

	tests := []struct {
		name     string
		tasks    []models.Task
		agentID  string
		expected bool
	}{
		{
			name: "CODE_APPROVED pipeline task returns true",
			tasks: []models.Task{
				{
					ID:        "task-1",
					Status:    "CODE_APPROVED",
					RolePair:  "coding-pair",
					Approvals: []models.Approval{{Agent: "code-reviewer-1", Provider: "claude"}},
				},
			},
			agentID:  "code-reviewer-1",
			expected: true,
		},
		{
			name: "CODE_APPROVED pipeline task by different agent returns false",
			tasks: []models.Task{
				{
					ID:        "task-1",
					Status:    "CODE_APPROVED",
					RolePair:  "coding-pair",
					Approvals: []models.Approval{{Agent: "code-reviewer-2", Provider: "codex"}},
				},
			},
			agentID:  "code-reviewer-1",
			expected: false,
		},
		{
			name: "CODING_PLAN_APPROVED pipeline task returns true",
			tasks: []models.Task{
				{
					ID:        "task-1",
					Status:    "CODING_PLAN_APPROVED",
					RolePair:  "code-planning-pair",
					Approvals: []models.Approval{{Agent: "code-plan-reviewer-1", Provider: "claude"}},
				},
			},
			agentID:  "code-plan-reviewer-1",
			expected: true,
		},
	}

	pr, _ := ops.LoadResolverForModels(tmpDir)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := testhelpers.CreateValidState()
			state.Tasks = tt.tasks
			registerEligibleMergeTestAgents(state, tt.tasks, tt.agentID, pr)
			testhelpers.WriteInitialState(t, statePath, state)

			bb := db.New(statePath)
			result := hasPendingMerges(bb, tt.agentID, pr)
			if result != tt.expected {
				t.Errorf("hasPendingMerges() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

// TestHasPendingMerges_Phase2Pipeline tests pipeline-aware merge detection with Phase 2 config
// (epic-planning-pair, us-writing-pair, code-planning-pair, coding-pair).
func TestHasPendingMerges_Phase2Pipeline(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	// Install Phase 2 frozen pipeline config
	src, err := os.ReadFile(findPhase2PipelineTestdata(t))
	if err != nil {
		t.Fatalf("Failed to read Phase 2 pipeline testdata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, paths.ProjectDirName(), "pipeline.yaml"), src, 0644); err != nil {
		t.Fatalf("Failed to write frozen pipeline config: %v", err)
	}

	tests := []struct {
		name     string
		tasks    []models.Task
		agentID  string
		expected bool
	}{
		{
			name: "US_APPROVED us-writing-pair task returns true",
			tasks: []models.Task{
				{
					ID:        "task-1",
					Status:    "US_APPROVED",
					RolePair:  "us-writing-pair",
					Approvals: []models.Approval{{Agent: "us-reviewer-1", Provider: "claude"}},
				},
			},
			agentID:  "us-reviewer-1",
			expected: true,
		},
		{
			name: "EPIC_PLAN_APPROVED epic-planning-pair task returns true",
			tasks: []models.Task{
				{
					ID:        "task-1",
					Status:    "EPIC_PLAN_APPROVED",
					RolePair:  "epic-planning-pair",
					Approvals: []models.Approval{{Agent: "epic-plan-reviewer-1", Provider: "claude"}},
				},
			},
			agentID:  "epic-plan-reviewer-1",
			expected: true,
		},
		{
			name: "US_APPROVED by different agent returns false",
			tasks: []models.Task{
				{
					ID:        "task-1",
					Status:    "US_APPROVED",
					RolePair:  "us-writing-pair",
					Approvals: []models.Approval{{Agent: "us-reviewer-2", Provider: "codex"}},
				},
			},
			agentID:  "us-reviewer-1",
			expected: false,
		},
		{
			name: "US_APPROVED already merged returns false",
			tasks: []models.Task{
				{
					ID:          "task-1",
					Status:      "US_APPROVED",
					RolePair:    "us-writing-pair",
					Approvals:   []models.Approval{{Agent: "us-reviewer-1", Provider: "claude"}},
					MergeCommit: testhelpers.StringPtr("abc123"),
				},
			},
			agentID:  "us-reviewer-1",
			expected: false,
		},
		{
			name: "CODE_APPROVED coding-pair still works in Phase 2 config",
			tasks: []models.Task{
				{
					ID:        "task-1",
					Status:    "CODE_APPROVED",
					RolePair:  "coding-pair",
					Approvals: []models.Approval{{Agent: "code-reviewer-1", Provider: "claude"}},
				},
			},
			agentID:  "code-reviewer-1",
			expected: true,
		},
	}

	pr, _ := ops.LoadResolverForModels(tmpDir)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := testhelpers.CreateValidState()
			state.Tasks = tt.tasks
			registerEligibleMergeTestAgents(state, tt.tasks, tt.agentID, pr)
			testhelpers.WriteInitialState(t, statePath, state)

			bb := db.New(statePath)
			result := hasPendingMerges(bb, tt.agentID, pr)
			if result != tt.expected {
				t.Errorf("hasPendingMerges() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

// TestLogTaskSubmissionIfCompleted_Phase2Pipeline tests that Phase 2 pipeline statuses
// are correctly recognized by logTaskSubmissionIfCompleted.
func TestLogTaskSubmissionIfCompleted_Phase2Pipeline(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	// Install Phase 2 frozen pipeline config
	src, err := os.ReadFile(findPhase2PipelineTestdata(t))
	if err != nil {
		t.Fatalf("Failed to read Phase 2 pipeline testdata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, paths.ProjectDirName(), "pipeline.yaml"), src, 0644); err != nil {
		t.Fatalf("Failed to write frozen pipeline config: %v", err)
	}

	tests := []struct {
		name    string
		task    models.Task
		wantErr bool
	}{
		{
			name: "US_READY_FOR_REVIEW recognized as submitted",
			task: models.Task{
				ID:       "task-1",
				Status:   "US_READY_FOR_REVIEW",
				RolePair: "us-writing-pair",
			},
		},
		{
			name: "WRITING_US recognized as executing",
			task: models.Task{
				ID:       "task-2",
				Status:   "WRITING_US",
				RolePair: "us-writing-pair",
			},
		},
		{
			name: "EPIC_PLAN_TO_REVIEW recognized as submitted",
			task: models.Task{
				ID:       "task-3",
				Status:   "EPIC_PLAN_TO_REVIEW",
				RolePair: "epic-planning-pair",
			},
		},
		{
			name: "EPIC_PLANNING recognized as executing",
			task: models.Task{
				ID:       "task-4",
				Status:   "EPIC_PLANNING",
				RolePair: "epic-planning-pair",
			},
		},
		{
			name: "legacy READY_FOR_REVIEW still works",
			task: models.Task{
				ID:     "task-5",
				Status: models.TaskStatusReadyForReview,
			},
		},
	}

	pr, _ := ops.LoadResolverForModels(tmpDir)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := testhelpers.CreateValidState()
			state.Tasks = []models.Task{tt.task}
			testhelpers.WriteInitialState(t, statePath, state)

			bb := db.New(statePath)
			err := logTaskSubmissionIfCompleted(bb, tt.task.ID, "agent-1", pr)
			if (err != nil) != tt.wantErr {
				t.Errorf("logTaskSubmissionIfCompleted() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestMergeIdentityCheck verifies that an eligible final approver keeps merge
// affinity. Supervisor takeover after that approver exits is covered by
// TestApprovedMergeTakeoverAfterFinalApproverExit.
func TestMergeIdentityCheck(t *testing.T) {
	tests := []struct {
		name     string
		tasks    []models.Task
		agentID  string
		expected bool
	}{
		{
			name: "LastApprover matches agentID returns true",
			tasks: []models.Task{
				{
					ID:       "task-1",
					Status:   models.TaskStatusApproved,
					RolePair: "coding-pair",
					Approvals: []models.Approval{
						{Agent: "code-reviewer-1", Provider: "claude"},
					},
				},
			},
			agentID:  "code-reviewer-1",
			expected: true,
		},
		{
			name: "LastApprover differs from agentID returns false",
			tasks: []models.Task{
				{
					ID:       "task-1",
					Status:   models.TaskStatusApproved,
					RolePair: "coding-pair",
					Approvals: []models.Approval{
						{Agent: "code-reviewer-2", Provider: "codex"},
					},
				},
			},
			agentID:  "code-reviewer-1",
			expected: false,
		},
		{
			name: "empty approvals returns false",
			tasks: []models.Task{
				{
					ID:       "task-1",
					Status:   models.TaskStatusApproved,
					RolePair: "coding-pair",
				},
			},
			agentID:  "code-reviewer-1",
			expected: false,
		},
		{
			name: "multi-approval LastApprover matches",
			tasks: []models.Task{
				{
					ID:       "task-1",
					Status:   models.TaskStatusApproved,
					RolePair: "coding-pair",
					Approvals: []models.Approval{
						{Agent: "code-reviewer-2", Provider: "codex"},
						{Agent: "code-reviewer-1", Provider: "claude"},
					},
				},
			},
			agentID:  "code-reviewer-1",
			expected: true,
		},
		{
			name: "multi-approval LastApprover does not match",
			tasks: []models.Task{
				{
					ID:       "task-1",
					Status:   models.TaskStatusApproved,
					RolePair: "coding-pair",
					Approvals: []models.Approval{
						{Agent: "code-reviewer-1", Provider: "claude"},
						{Agent: "code-reviewer-2", Provider: "codex"},
					},
				},
			},
			agentID:  "code-reviewer-1",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
			pr, _ := ops.LoadResolverForModels(tmpDir)

			state := testhelpers.CreateValidState()
			state.Tasks = tt.tasks
			registerEligibleMergeTestAgents(state, tt.tasks, tt.agentID, pr)
			testhelpers.WriteInitialState(t, statePath, state)

			bb := db.New(statePath)

			result := hasPendingMerges(bb, tt.agentID, pr)
			if result != tt.expected {
				t.Errorf("hasPendingMerges() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func registerEligibleMergeTestAgents(state *models.State, tasks []models.Task, caller string, pr models.PipelineResolver) {
	for i := range tasks {
		reviewerRole, err := pr.ReviewerRole(tasks[i].RolePair)
		if err != nil {
			continue
		}
		agentIDs := map[string]struct{}{caller: {}}
		for _, approval := range tasks[i].Approvals {
			agentIDs[approval.Agent] = struct{}{}
		}
		for agentID := range agentIDs {
			if agentID != "" {
				state.Agents[agentID] = testhelpers.RegisteredTestAgent(reviewerRole)
			}
		}
	}
}

// findPipelineTestdata locates the Phase 1 pipeline testdata YAML in the repo.
func findPipelineTestdata(t *testing.T) string {
	t.Helper()
	return filepath.Join(testhelpers.FindRepoRoot(t), "internal", "pipeline", "testdata", "valid-coding-subpipeline.yaml")
}

// findPhase2PipelineTestdata locates the Phase 2 pipeline testdata YAML in the repo.
func findPhase2PipelineTestdata(t *testing.T) string {
	t.Helper()
	return filepath.Join(testhelpers.FindRepoRoot(t), "internal", "pipeline", "testdata", "valid-phase2-full.yaml")
}

// Resume paths bypass ClaimTask, so without enforcement here a handoff or a
// restart would hand a coder an unprepared worktree — the gap the claim path
// already closes.
func TestClaimDoerTask_ResumePostWorktreeCmdFailureDegradesAgent(t *testing.T) {
	for _, tc := range []struct {
		name           string
		handoffPending bool
	}{
		{name: "owned task resume", handoffPending: false},
		{name: "handoff resume", handoffPending: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			testhelpers.SetupTestGitRepo(t, tmpDir)
			statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
			testhelpers.CreateTestWorktree(t, tmpDir, "task-owned")

			now := time.Now().UTC()
			agentID := "coder-1"
			owned := testhelpers.BuildTaskByStatus("task-owned", models.TaskStatusImplementing, now)
			owned.AssignedTo = &agentID
			owned.HandoffPending = tc.handoffPending

			state := testhelpers.CreateValidState()
			postCmd := "exit 1"
			state.Config.PostWorktreeCmd = &postCmd
			state.Tasks = []models.Task{owned}
			leaseExpires := now.Add(30 * time.Minute)
			state.Agents[agentID] = models.Agent{
				Role:         models.RoleCoder,
				Status:       models.AgentStatusIdle,
				LeaseExpires: &leaseExpires,
				Heartbeat:    now,
				Terminal:     "test",
				Provider:     "test",
				PID:          os.Getpid(),
			}
			testhelpers.WriteInitialState(t, statePath, state)

			taskID, worktree, err := claimDoerTask(tmpDir, agentID, models.RoleCoder, db.New(statePath))
			if err == nil {
				t.Fatal("claimDoerTask() error = nil, want setup failure")
			}
			// The supervisor exits on ErrAgentDegraded before building a prompt or
			// launching a provider, so this is the "no provider invoked" contract.
			if !errors.Is(err, ErrAgentDegraded) {
				t.Fatalf("claimDoerTask() error = %v, want ErrAgentDegraded", err)
			}
			// The sentinel says "stop claiming"; the cause says what to fix. Both
			// must survive to the supervisor, which logs this error on exit.
			var setupErr *ops.PostWorktreeSetupError
			if !errors.As(err, &setupErr) {
				t.Fatalf("claimDoerTask() error = %v, want the setup failure preserved as the cause", err)
			}
			if taskID != "" || worktree != "" {
				t.Errorf("claimDoerTask() = (%q, %q), want empty — no task handed to a provider", taskID, worktree)
			}

			readState, readErr := db.New(statePath).Read()
			if readErr != nil {
				t.Fatalf("Read() error = %v", readErr)
			}
			health, ok := readState.AgentHealth[agentID]
			if !ok {
				t.Fatal("AgentHealth missing entry, want degraded record")
			}
			if health.Reason != ops.AgentDegradedWorktreeSetupFailed {
				t.Errorf("health.Reason = %q, want %q", health.Reason, ops.AgentDegradedWorktreeSetupFailed)
			}
		})
	}
}

// A resume with no configured command must stay a plain resume.
func TestClaimDoerTask_ResumeWithoutPostWorktreeCmdUnaffected(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.CreateTestWorktree(t, tmpDir, "task-owned")

	now := time.Now().UTC()
	agentID := "coder-1"
	owned := testhelpers.BuildTaskByStatus("task-owned", models.TaskStatusImplementing, now)
	owned.AssignedTo = &agentID

	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{owned}
	leaseExpires := now.Add(30 * time.Minute)
	state.Agents[agentID] = models.Agent{
		Role:         models.RoleCoder,
		Status:       models.AgentStatusIdle,
		LeaseExpires: &leaseExpires,
		Heartbeat:    now,
		Terminal:     "test",
		Provider:     "test",
		PID:          os.Getpid(),
	}
	testhelpers.WriteInitialState(t, statePath, state)

	taskID, _, err := claimDoerTask(tmpDir, agentID, models.RoleCoder, db.New(statePath))
	if err != nil {
		t.Fatalf("claimDoerTask() error = %v", err)
	}
	if taskID != "task-owned" {
		t.Errorf("taskID = %q, want task-owned", taskID)
	}
}

// Degraded classification must stop candidate iteration: a fresh-claim setup
// failure returns immediately instead of trying the next candidate task.
func TestClaimDoerTask_PostWorktreeCmdFailureStopsCandidateIteration(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	agentID := "coder-1"
	state := testhelpers.CreateValidState()
	postCmd := "exit 1"
	state.Config.PostWorktreeCmd = &postCmd
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-a", models.TaskStatusReady, now),
		testhelpers.BuildTaskByStatus("task-b", models.TaskStatusReady, now),
		testhelpers.BuildTaskByStatus("task-c", models.TaskStatusReady, now),
	}
	leaseExpires := now.Add(30 * time.Minute)
	state.Agents[agentID] = models.Agent{
		Role:         models.RoleCoder,
		Status:       models.AgentStatusIdle,
		LeaseExpires: &leaseExpires,
		Heartbeat:    now,
		Terminal:     "test",
		Provider:     "test",
		PID:          os.Getpid(),
	}
	testhelpers.WriteInitialState(t, statePath, state)

	taskID, _, err := claimDoerTask(tmpDir, agentID, models.RoleCoder, db.New(statePath))
	if !errors.Is(err, ErrAgentDegraded) {
		t.Fatalf("claimDoerTask() error = %v, want ErrAgentDegraded on the first candidate", err)
	}
	if taskID != "" {
		t.Errorf("taskID = %q, want empty", taskID)
	}

	// Exactly one worktree attempted — iteration stopped rather than churning
	// a worktree per candidate.
	entries, readErr := os.ReadDir(filepath.Join(tmpDir, ".worktrees"))
	if readErr != nil {
		t.Fatalf("ReadDir(.worktrees) error = %v", readErr)
	}
	if len(entries) != 1 {
		t.Errorf("worktree count = %d, want 1 — candidate iteration should stop at the first failure", len(entries))
	}
}

// TestClaimReviewerTaskForRoleLogsClaimErrorAtDebug pins the claim helper's
// log level: the keyed site in strategy_reviewer.go owns the error-level line,
// so a failure repeated on every loop iteration no longer floods the log
// from here.
func TestClaimReviewerTaskForRoleLogsClaimErrorAtDebug(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	state := testhelpers.CreateValidState()
	state.Agents["code-reviewer-1"] = testhelpers.RegisteredTestAgent(models.RoleCodeReviewer)
	state.Tasks = nil
	bb := testhelpers.WriteInitialState(t, statePath, state)

	logs := captureAgentLogsAtLevel(t, slog.LevelDebug)
	_, _, _, err := claimReviewerTaskForRole(tmpDir, "code-reviewer-1", models.RoleCodeReviewer, "", 1800, bb)
	if err == nil {
		t.Fatal("claimReviewerTaskForRole() error = nil, want no reviewable tasks")
	}
	if got := countLogLines(logs.String(), "level=DEBUG", `msg="Review claim error"`); got != 1 {
		t.Fatalf("debug-level Review claim error lines = %d, want 1:\n%s", got, logs.String())
	}
	if got := countLogLines(logs.String(), "level=ERROR", `msg="Review claim error"`); got != 0 {
		t.Fatalf("error-level Review claim error lines = %d, want 0:\n%s", got, logs.String())
	}
}
