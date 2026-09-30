package statevalidate

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

func TestValidateHandoffEvents_Valid(t *testing.T) {
	t.Parallel()
	now := time.Now()

	tests := []struct {
		name  string
		state *models.State
	}{
		{
			name: "task with no handoff events in pre-submission state",
			state: &models.State{
				Tasks: []models.Task{
					{ID: "t1", Status: models.TaskStatusImplementing},
				},
			},
		},
		{
			name: "task with valid context_exhaustion event",
			state: &models.State{
				Tasks: []models.Task{
					{
						ID:     "t1",
						Status: models.TaskStatusImplementing,
						HandoffEvents: []models.HandoffEvent{
							{Timestamp: now, Agent: "coder-1", Trigger: models.HandoffTriggerContextExhaustion},
						},
					},
				},
			},
		},
		{
			name: "submitted task with submission event",
			state: &models.State{
				Tasks: []models.Task{
					{
						ID:     "t1",
						Status: models.TaskStatusReadyForReview,
						HandoffEvents: []models.HandoffEvent{
							{Timestamp: now, Agent: "coder-1", Trigger: models.HandoffTriggerSubmission},
						},
					},
				},
			},
		},
		{
			name: "merged task with submission and completion events",
			state: &models.State{
				Tasks: []models.Task{
					{
						ID:     "t1",
						Status: models.TaskStatusMerged,
						HandoffEvents: []models.HandoffEvent{
							{Timestamp: now, Agent: "coder-1", Trigger: models.HandoffTriggerSubmission},
							{Timestamp: now, Agent: "reviewer-1", Trigger: models.HandoffTriggerCompletion},
						},
					},
				},
			},
		},
		{
			name: "empty tasks list",
			state: &models.State{
				Tasks: []models.Task{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := handoffEventsErr(tt.state, "/tmp", false); err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
		})
	}
}

func TestValidateHandoffEvents_MissingFields(t *testing.T) {
	t.Parallel()
	now := time.Now()

	tests := []struct {
		name    string
		event   models.HandoffEvent
		wantMsg string
	}{
		{
			name:    "zero timestamp",
			event:   models.HandoffEvent{Agent: "coder-1", Trigger: models.HandoffTriggerSubmission},
			wantMsg: "zero timestamp",
		},
		{
			name:    "empty agent",
			event:   models.HandoffEvent{Timestamp: now, Trigger: models.HandoffTriggerSubmission},
			wantMsg: "empty agent",
		},
		{
			name:    "invalid trigger",
			event:   models.HandoffEvent{Timestamp: now, Agent: "coder-1", Trigger: "bogus"},
			wantMsg: "invalid trigger",
		},
		{
			name:    "empty trigger",
			event:   models.HandoffEvent{Timestamp: now, Agent: "coder-1"},
			wantMsg: "invalid trigger",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &models.State{
				Tasks: []models.Task{
					{
						ID:            "t1",
						Status:        models.TaskStatusImplementing,
						HandoffEvents: []models.HandoffEvent{tt.event},
					},
				},
			}
			err := handoffEventsErr(state, "/tmp", false)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error %q should contain %q", err.Error(), tt.wantMsg)
			}
			if !strings.Contains(err.Error(), "t1") {
				t.Errorf("error %q should mention task ID", err.Error())
			}
		})
	}
}

func TestValidateHandoffEvents_MissingEvents(t *testing.T) {
	t.Parallel()
	now := time.Now()

	tests := []struct {
		name    string
		task    models.Task
		wantMsg string
	}{
		{
			name: "submitted state without submission event",
			task: models.Task{
				ID:     "t1",
				Status: models.TaskStatusReadyForReview,
			},
			wantMsg: "submission",
		},
		{
			name: "reviewing state without submission event",
			task: models.Task{
				ID:     "t1",
				Status: models.TaskStatusReviewing,
			},
			wantMsg: "submission",
		},
		{
			name: "approved state without submission event",
			task: models.Task{
				ID:     "t1",
				Status: models.TaskStatusApproved,
			},
			wantMsg: "submission",
		},
		{
			name: "merged state without completion event",
			task: models.Task{
				ID:     "t1",
				Status: models.TaskStatusMerged,
				HandoffEvents: []models.HandoffEvent{
					{Timestamp: now, Agent: "coder-1", Trigger: models.HandoffTriggerSubmission},
				},
			},
			wantMsg: "completion",
		},
		{
			name: "merged state without submission event",
			task: models.Task{
				ID:     "t1",
				Status: models.TaskStatusMerged,
				HandoffEvents: []models.HandoffEvent{
					{Timestamp: now, Agent: "reviewer-1", Trigger: models.HandoffTriggerCompletion},
				},
			},
			wantMsg: "submission",
		},
		{
			name: "integration_failed without submission event",
			task: models.Task{
				ID:     "t1",
				Status: models.TaskStatusIntegrationFailed,
			},
			wantMsg: "submission",
		},
		{
			name: "coding plan review state without submission event",
			task: models.Task{
				ID:     "t1",
				Status: models.TaskStatusCodingPlanToReview,
			},
			wantMsg: "submission",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &models.State{
				Tasks: []models.Task{tt.task},
			}
			err := handoffEventsErr(state, "/tmp", false)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error %q should contain %q", err.Error(), tt.wantMsg)
			}
		})
	}
}
