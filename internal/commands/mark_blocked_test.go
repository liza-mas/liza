package commands

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/payloadschema"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestMarkBlockedCommand(t *testing.T) {
	tests := []struct {
		name          string
		taskID        string
		reason        string
		questions     []string
		agentID       string
		setupState    func(*models.State)
		wantErr       bool
		wantErrMsg    string
		validateState func(*testing.T, *models.State)
	}{
		{
			name:      "Success - IMPLEMENTING to BLOCKED with valid inputs",
			taskID:    "task-1",
			reason:    "Test blocker reason",
			questions: []string{"Question 1?", "Question 2?"},
			agentID:   "coder-1",
			wantErr:   false,
			setupState: func(s *models.State) {
				now := time.Now().UTC()
				assignedTo := "coder-1"
				leaseExpires := now.Add(30 * time.Minute)
				s.Tasks = []models.Task{
					{
						ID:           "task-1",
						Description:  "Test task",
						Status:       models.TaskStatusImplementing,
						AssignedTo:   &assignedTo,
						LeaseExpires: &leaseExpires,
						Created:      now,
						History:      []models.TaskHistoryEntry{},
					},
				}
			},
			validateState: func(t *testing.T, s *models.State) {
				task := &s.Tasks[0]
				if task.Status != models.TaskStatusBlocked {
					t.Errorf("expected status BLOCKED, got %s", task.Status)
				}
				if task.BlockedReason == nil || *task.BlockedReason != "Test blocker reason" {
					t.Errorf("expected blocked_reason 'Test blocker reason', got %v", task.BlockedReason)
				}
				if len(task.BlockedQuestions) != 2 {
					t.Errorf("expected 2 blocked_questions, got %d", len(task.BlockedQuestions))
				}
				if task.AssignedTo != nil {
					t.Errorf("expected assigned_to to be nil, got %v", task.AssignedTo)
				}
				if task.LeaseExpires != nil {
					t.Errorf("expected lease_expires to be nil, got %v", task.LeaseExpires)
				}
				if len(task.History) != 1 {
					t.Fatalf("expected 1 history entry, got %d", len(task.History))
				}
				if task.History[0].Event != "blocked" {
					t.Errorf("expected event blocked, got %s", task.History[0].Event)
				}
				if task.History[0].Agent == nil || *task.History[0].Agent != "coder-1" {
					t.Errorf("expected agent coder-1 in history, got %v", task.History[0].Agent)
				}
				if task.History[0].Reason == nil || *task.History[0].Reason != "Test blocker reason" {
					t.Errorf("expected reason in history, got %v", task.History[0].Reason)
				}
			},
		},
		{
			name:       "Error - Task not found",
			taskID:     "nonexistent-task",
			reason:     "Test reason",
			questions:  []string{"Question?"},
			agentID:    "coder-1",
			wantErr:    true,
			wantErrMsg: "task not found",
			setupState: func(s *models.State) {
				s.Tasks = []models.Task{}
			},
		},
		{
			name:       "Error - Task not in IMPLEMENTING status",
			taskID:     "task-1",
			reason:     "Test reason",
			questions:  []string{"Question?"},
			agentID:    "coder-1",
			wantErr:    true,
			wantErrMsg: "must be in an executing status",
			setupState: func(s *models.State) {
				now := time.Now().UTC()
				s.Tasks = []models.Task{
					{
						ID:          "task-1",
						Description: "Test task",
						Status:      models.TaskStatusReadyForReview,
						Created:     now,
						History:     []models.TaskHistoryEntry{},
					},
				}
			},
		},
		{
			name:       "Error - Empty reason",
			taskID:     "task-1",
			reason:     "",
			questions:  []string{"Question?"},
			agentID:    "coder-1",
			wantErr:    true,
			wantErrMsg: "reason is required",
		},
		{
			name:       "Error - Empty questions array",
			taskID:     "task-1",
			reason:     "Test reason",
			questions:  []string{},
			agentID:    "coder-1",
			wantErr:    true,
			wantErrMsg: "at least 1 question is required",
		},
		{
			name:       "Error - More than 3 questions",
			taskID:     "task-1",
			reason:     "Test reason",
			questions:  []string{"Q1?", "Q2?", "Q3?", "Q4?"},
			agentID:    "coder-1",
			wantErr:    true,
			wantErrMsg: "maximum 3 questions allowed",
		},
		{
			name:       "Error - Agent ID doesn't match task.AssignedTo",
			taskID:     "task-1",
			reason:     "Test reason",
			questions:  []string{"Question?"},
			agentID:    "different-agent",
			wantErr:    true,
			wantErrMsg: "only the assigned agent can mark task as blocked",
			setupState: func(s *models.State) {
				now := time.Now().UTC()
				assignedTo := "coder-1"
				leaseExpires := now.Add(30 * time.Minute)
				s.Tasks = []models.Task{
					{
						ID:           "task-1",
						Description:  "Test task",
						Status:       models.TaskStatusImplementing,
						AssignedTo:   &assignedTo,
						LeaseExpires: &leaseExpires,
						Created:      now,
						History:      []models.TaskHistoryEntry{},
					},
				}
			},
		},
		{
			name:       "Error - Empty task ID",
			taskID:     "",
			reason:     "Test reason",
			questions:  []string{"Question?"},
			agentID:    "coder-1",
			wantErr:    true,
			wantErrMsg: "task ID is required",
		},
		{
			name:       "Error - Empty agent ID",
			taskID:     "task-1",
			reason:     "Test reason",
			questions:  []string{"Question?"},
			agentID:    "",
			wantErr:    true,
			wantErrMsg: "agent ID is required",
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
			err := MarkBlockedCommand(tmpDir, tt.taskID, tt.reason, tt.questions, tt.agentID)

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

func TestMarkBlockedPayloadCanonicalObject(t *testing.T) {
	t.Parallel()

	repairRequest := &models.RepairRequest{Operation: "add-task", Target: "architecture-2"}
	got := MarkBlockedPayload("task-1", "blocked", []string{"q1"}, ops.MarkBlockedOptions{
		DependsOn:     []string{"dep-1"},
		RepairRequest: repairRequest,
	})

	want := payloadschema.MarkBlockedPayload{
		TaskID:        "task-1",
		Reason:        "blocked",
		Questions:     []string{"q1"},
		DependsOn:     []string{"dep-1"},
		RepairRequest: repairRequest,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MarkBlockedPayload() = %#v, want %#v", got, want)
	}

	// Caller identity is not payload: nothing in the canonical object names the
	// agent that would authorize the call.
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal canonical object: %v", err)
	}
	if strings.Contains(string(encoded), "agent") {
		t.Fatalf("canonical object = %s, want no agent identity", encoded)
	}
}
