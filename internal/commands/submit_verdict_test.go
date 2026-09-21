package commands

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestSubmitVerdictCommand(t *testing.T) {
	tests := []struct {
		name          string
		taskID        string
		verdict       string
		reason        string
		agentID       string
		setupState    func(*models.State)
		wantErr       bool
		wantErrMsg    string
		validateState func(*testing.T, *models.State)
	}{
		{
			name:    "successful APPROVED verdict",
			taskID:  "t1",
			verdict: "APPROVED",
			reason:  "",
			agentID: "code-reviewer-1",
			wantErr: false,
			setupState: func(s *models.State) {
				now := time.Now().UTC()
				reviewCommit := "abc123"
				reviewingBy := "code-reviewer-1"
				reviewLeaseExpires := now.Add(30 * time.Minute)
				currentTask := "t1"
				s.Tasks = []models.Task{
					{
						ID:                 "t1",
						Description:        "Test task",
						Status:             models.TaskStatusReviewing,
						RolePair:           "coding-pair",
						ReviewCommit:       &reviewCommit,
						ReviewingBy:        &reviewingBy,
						ReviewLeaseExpires: &reviewLeaseExpires,
						Created:            now,
						History:            []models.TaskHistoryEntry{},
					},
				}
				s.Agents["code-reviewer-1"] = models.Agent{
					Role:         "code-reviewer",
					Status:       models.AgentStatusReviewing,
					CurrentTask:  &currentTask,
					LeaseExpires: &reviewLeaseExpires,
					Heartbeat:    now,
				}
			},
			validateState: func(t *testing.T, s *models.State) {
				task := &s.Tasks[0]
				if task.Status != models.TaskStatusApproved {
					t.Errorf("expected status APPROVED, got %s", task.Status)
				}
				if task.ApprovedBy == nil || *task.ApprovedBy != "code-reviewer-1" {
					t.Errorf("expected approved_by code-reviewer-1, got %v", task.ApprovedBy)
				}
				if task.RejectionReason != nil {
					t.Errorf("expected rejection_reason to be nil, got %v", task.RejectionReason)
				}
				if task.ReviewingBy != nil {
					t.Errorf("expected reviewing_by to be nil, got %v", task.ReviewingBy)
				}
				if task.ReviewLeaseExpires != nil {
					t.Errorf("expected review_lease_expires to be nil, got %v", task.ReviewLeaseExpires)
				}
				if len(task.History) != 1 {
					t.Fatalf("expected 1 history entry, got %d", len(task.History))
				}
				if task.History[0].Event != "approved" {
					t.Errorf("expected event approved, got %s", task.History[0].Event)
				}
				if task.History[0].Agent == nil || *task.History[0].Agent != "code-reviewer-1" {
					t.Errorf("expected agent code-reviewer-1 in history, got %v", task.History[0].Agent)
				}

				agent := s.Agents["code-reviewer-1"]
				if agent.Status != models.AgentStatusIdle {
					t.Errorf("expected reviewer status IDLE, got %s", agent.Status)
				}
				if agent.CurrentTask != nil {
					t.Errorf("expected reviewer current_task nil, got %v", agent.CurrentTask)
				}
			},
		},
		{
			name:    "successful REJECTED verdict",
			taskID:  "t2",
			verdict: "REJECTED",
			reason:  "Code doesn't meet requirements",
			agentID: "code-reviewer-1",
			wantErr: false,
			setupState: func(s *models.State) {
				now := time.Now().UTC()
				reviewCommit := "def456"
				reviewingBy := "code-reviewer-1"
				reviewLeaseExpires := now.Add(30 * time.Minute)
				currentTask := "t2"
				s.Tasks = []models.Task{
					{
						ID:                  "t2",
						Description:         "Test task",
						Status:              models.TaskStatusReviewing,
						RolePair:            "coding-pair",
						ReviewCommit:        &reviewCommit,
						ReviewingBy:         &reviewingBy,
						ReviewLeaseExpires:  &reviewLeaseExpires,
						ReviewCyclesCurrent: 0,
						ReviewCyclesTotal:   0,
						Created:             now,
						History:             []models.TaskHistoryEntry{},
					},
				}
				s.Agents["code-reviewer-1"] = models.Agent{
					Role:         "code-reviewer",
					Status:       models.AgentStatusReviewing,
					CurrentTask:  &currentTask,
					LeaseExpires: &reviewLeaseExpires,
					Heartbeat:    now,
				}
			},
			validateState: func(t *testing.T, s *models.State) {
				task := &s.Tasks[0]
				if task.Status != models.TaskStatusRejected {
					t.Errorf("expected status REJECTED, got %s", task.Status)
				}
				if task.RejectionReason == nil || *task.RejectionReason != "Code doesn't meet requirements" {
					t.Errorf("expected rejection_reason, got %v", task.RejectionReason)
				}
				if task.ReviewingBy != nil {
					t.Errorf("expected reviewing_by to be nil, got %v", task.ReviewingBy)
				}
				if task.ReviewLeaseExpires != nil {
					t.Errorf("expected review_lease_expires to be nil, got %v", task.ReviewLeaseExpires)
				}
				if task.ReviewCyclesCurrent != 1 {
					t.Errorf("expected review_cycles_current 1, got %d", task.ReviewCyclesCurrent)
				}
				if task.ReviewCyclesTotal != 1 {
					t.Errorf("expected review_cycles_total 1, got %d", task.ReviewCyclesTotal)
				}
				if len(task.History) != 1 {
					t.Fatalf("expected 1 history entry, got %d", len(task.History))
				}
				if task.History[0].Event != "rejected" {
					t.Errorf("expected event rejected, got %s", task.History[0].Event)
				}
				if task.History[0].Agent == nil || *task.History[0].Agent != "code-reviewer-1" {
					t.Errorf("expected agent code-reviewer-1 in history, got %v", task.History[0].Agent)
				}
				if task.History[0].Reason == nil || *task.History[0].Reason != "Code doesn't meet requirements" {
					t.Errorf("expected reason in history, got %v", task.History[0].Reason)
				}

				agent := s.Agents["code-reviewer-1"]
				if agent.Status != models.AgentStatusIdle {
					t.Errorf("expected reviewer status IDLE, got %s", agent.Status)
				}
				if agent.CurrentTask != nil {
					t.Errorf("expected reviewer current_task nil, got %v", agent.CurrentTask)
				}
			},
		},
		{
			name:    "REJECTED increments review cycles",
			taskID:  "t3",
			verdict: "REJECTED",
			reason:  "Try again",
			agentID: "code-reviewer-1",
			wantErr: false,
			setupState: func(s *models.State) {
				reviewCommit := "ghi789"
				reviewingBy := "code-reviewer-1"
				reviewLeaseExpires := time.Now().UTC().Add(30 * time.Minute)
				s.Tasks = []models.Task{
					{
						ID:                  "t3",
						Description:         "Test task",
						Status:              models.TaskStatusReviewing,
						RolePair:            "coding-pair",
						ReviewCommit:        &reviewCommit,
						ReviewingBy:         &reviewingBy,
						ReviewLeaseExpires:  &reviewLeaseExpires,
						ReviewCyclesCurrent: 2,
						ReviewCyclesTotal:   5,
						Created:             time.Now().UTC(),
						History:             []models.TaskHistoryEntry{},
					},
				}
			},
			validateState: func(t *testing.T, s *models.State) {
				task := &s.Tasks[0]
				if task.ReviewCyclesCurrent != 3 {
					t.Errorf("expected review_cycles_current 3, got %d", task.ReviewCyclesCurrent)
				}
				if task.ReviewCyclesTotal != 6 {
					t.Errorf("expected review_cycles_total 6, got %d", task.ReviewCyclesTotal)
				}
			},
		},
		{
			name:       "missing task ID",
			taskID:     "",
			verdict:    "APPROVED",
			reason:     "",
			agentID:    "code-reviewer-1",
			wantErr:    true,
			wantErrMsg: "task ID is required",
		},
		{
			name:       "missing verdict",
			taskID:     "t1",
			verdict:    "",
			reason:     "",
			agentID:    "code-reviewer-1",
			wantErr:    true,
			wantErrMsg: "verdict is required",
		},
		{
			name:       "invalid verdict",
			taskID:     "t1",
			verdict:    "MAYBE",
			reason:     "",
			agentID:    "code-reviewer-1",
			wantErr:    true,
			wantErrMsg: "verdict must be APPROVED or REJECTED",
		},
		{
			name:       "missing agent ID",
			taskID:     "t1",
			verdict:    "APPROVED",
			reason:     "",
			agentID:    "",
			wantErr:    true,
			wantErrMsg: brand.EnvName("AGENT_ID") + " is required",
		},
		{
			name:       "REJECTED without reason",
			taskID:     "t1",
			verdict:    "REJECTED",
			reason:     "",
			agentID:    "code-reviewer-1",
			wantErr:    true,
			wantErrMsg: "rejection reason is required for REJECTED verdict",
		},
		{
			name:       "task not found",
			taskID:     "nonexistent",
			verdict:    "APPROVED",
			reason:     "",
			agentID:    "code-reviewer-1",
			wantErr:    true,
			wantErrMsg: "task not found",
			setupState: func(s *models.State) {
				s.Tasks = []models.Task{}
			},
		},
		{
			name:       "task not in REVIEWING status",
			taskID:     "t1",
			verdict:    "APPROVED",
			reason:     "",
			agentID:    "code-reviewer-1",
			wantErr:    true,
			wantErrMsg: "task t1 is not in a reviewing state",
			setupState: func(s *models.State) {
				s.Tasks = []models.Task{
					{
						ID:          "t1",
						Description: "Test task",
						Status:      models.TaskStatusImplementing,
						RolePair:    "coding-pair",
						Created:     time.Now().UTC(),
						History:     []models.TaskHistoryEntry{},
					},
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory for test
			tmpDir := t.TempDir()
			statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
			testhelpers.SetupPipelineConfig(t, tmpDir)

			// Initialize state
			initialState := &models.State{
				Config: models.Config{
					IntegrationBranch: "integration",
					LeaseDuration:     1800,
				},
				Tasks:  []models.Task{},
				Agents: make(map[string]models.Agent),
			}

			// Setup state if provided
			if tt.setupState != nil {
				tt.setupState(initialState)
			}

			// Write initial state
			bb := testhelpers.WriteInitialState(t, statePath, initialState)

			// Execute command
			err := SubmitVerdictCommand(tmpDir, tt.taskID, tt.verdict, tt.reason, tt.agentID, "")

			// Check error
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				} else {
					testhelpers.AssertErrorContains(t, err, tt.wantErrMsg)
				}
			} else {
				testhelpers.AssertNoError(t, err)
			}

			// Validate state if no error expected
			if !tt.wantErr && tt.validateState != nil {
				state, err := bb.Read()
				if err != nil {
					t.Fatalf("failed to read state: %v", err)
				}
				tt.validateState(t, state)
			}
		})
	}
}

func TestPrintVerdictResultGated(t *testing.T) {
	blockedReason := models.BlockedReasonRejectionRCARequired + ": 4 durable rejections reached the high-churn threshold 4"
	gated := &ops.VerdictResult{
		LifecycleOutcome:   models.LifecycleOutcome{Operation: "submit-verdict", TaskID: "t1", Outcome: models.LifecycleCompleted, SafeAction: "continue"},
		TaskID:             "t1",
		Verdict:            "REJECTED",
		AgentID:            "code-reviewer-1",
		Reason:             "fourth rejection",
		EscalatedToBlocked: true,
		RejectionRCAGated:  true,
		BlockedReason:      blockedReason,
	}
	output := captureStdout(t, func() { printVerdictResult(gated) })

	for _, want := range []string{
		"REJECTED: t1",
		"  escalated_to: BLOCKED",
		"  blocked_reason: " + blockedReason,
		"  rejection_rca_gate: open",
		brand.Command("record-rejection-rca", "t1", "--rca-file", "<file>"),
		brand.Command("resume-rejection-rca", "t1", "--disposition-file", "<file>"),
	} {
		if !strings.Contains(output, want) {
			t.Errorf("gated verdict output missing %q:\n%s", want, output)
		}
	}

	limit := *gated
	limit.RejectionRCAGated = false
	limit.BlockedReason = "review budget exhausted: max review cycles reached (5/5)"
	output = captureStdout(t, func() { printVerdictResult(&limit) })
	if strings.Contains(output, "rejection_rca_gate") || strings.Contains(output, "record-rejection-rca") || strings.Contains(output, "resume-rejection-rca") {
		t.Errorf("limit escalation output names the gate:\n%s", output)
	}
	if !strings.Contains(output, "  escalated_to: BLOCKED\n  blocked_reason: "+limit.BlockedReason) {
		t.Errorf("limit escalation output changed:\n%s", output)
	}
}
