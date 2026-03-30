package commands

import (
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestUpdateSprintMetricsCommand(t *testing.T) {
	tests := []struct {
		name         string
		tasks        []models.Task
		agents       map[string]models.Agent
		wantMetrics  models.SprintMetrics
		wantError    bool
		wantWarnings []string
	}{
		{
			name:   "empty state - all metrics zero",
			tasks:  []models.Task{},
			agents: map[string]models.Agent{},
			wantMetrics: models.SprintMetrics{
				TasksDone:                        0,
				TasksInProgress:                  0,
				TasksBlocked:                     0,
				IterationsTotal:                  0,
				ReviewCyclesTotal:                0,
				ReviewVerdictApprovals:           0,
				ReviewVerdictRejections:          0,
				ReviewVerdictCount:               0,
				ReviewVerdictApprovalRatePercent: 0,
				TaskSubmittedForReviewCount:      0,
				TaskOutcomeApprovalRatePercent:   0,
			},
			wantError: false,
		},
		{
			name: "basic task counts",
			tasks: []models.Task{
				{
					ID:          "task-1",
					Status:      models.TaskStatusMerged,
					Description: "Done task",
					Created:     time.Now(),
				},
				{
					ID:          "task-2",
					Status:      models.TaskStatusImplementing,
					Description: "In progress task",
					Created:     time.Now(),
				},
				{
					ID:          "task-3",
					Status:      models.TaskStatusBlocked,
					Description: "Blocked task",
					Created:     time.Now(),
				},
				{
					ID:          "task-4",
					Status:      models.TaskStatusReady,
					Description: "Unclaimed task",
					Created:     time.Now(),
				},
			},
			agents: map[string]models.Agent{},
			wantMetrics: models.SprintMetrics{
				TasksDone:                        1,
				TasksInProgress:                  1,
				TasksBlocked:                     1,
				IterationsTotal:                  0,
				ReviewCyclesTotal:                0,
				ReviewVerdictApprovals:           0,
				ReviewVerdictRejections:          0,
				ReviewVerdictCount:               0,
				ReviewVerdictApprovalRatePercent: 0,
				TaskSubmittedForReviewCount:      0,
				TaskOutcomeApprovalRatePercent:   0,
			},
			wantError: false,
		},
		{
			name: "iteration and review cycle aggregation",
			tasks: []models.Task{
				{
					ID:                "task-1",
					Status:            models.TaskStatusMerged,
					Description:       "Task 1",
					Iteration:         3,
					ReviewCyclesTotal: 2,
					Created:           time.Now(),
				},
				{
					ID:                "task-2",
					Status:            models.TaskStatusApproved,
					Description:       "Task 2",
					Iteration:         1,
					ReviewCyclesTotal: 1,
					Created:           time.Now(),
				},
				{
					ID:          "task-3",
					Status:      models.TaskStatusImplementing,
					Description: "Task 3",
					Iteration:   2,
					Created:     time.Now(),
				},
			},
			agents: map[string]models.Agent{
				"agent-1": {
					Role:            "coder",
					Status:          models.AgentStatusWorking,
					IterationsTotal: 10,
				},
				"agent-2": {
					Role:            "coder",
					Status:          models.AgentStatusIdle,
					IterationsTotal: 5,
				},
			},
			wantMetrics: models.SprintMetrics{
				TasksDone:                        1,
				TasksInProgress:                  1,
				TasksBlocked:                     0,
				IterationsTotal:                  15, // Sum of agent iterations
				ReviewCyclesTotal:                3,  // 2 + 1
				ReviewVerdictApprovals:           0,
				ReviewVerdictRejections:          0,
				ReviewVerdictCount:               0,
				ReviewVerdictApprovalRatePercent: 0,
				TaskSubmittedForReviewCount:      0,
				TaskOutcomeApprovalRatePercent:   0,
			},
			wantError: false,
		},
		{
			name: "review verdict tracking from history",
			tasks: []models.Task{
				{
					ID:                "task-1",
					Status:            models.TaskStatusMerged,
					Description:       "Task with review history",
					ReviewCyclesTotal: 2,
					Created:           time.Now(),
					History: []models.TaskHistoryEntry{
						{
							Time:  time.Now(),
							Event: "submitted_for_review",
						},
						{
							Time:  time.Now(),
							Event: "rejected",
						},
						{
							Time:  time.Now(),
							Event: "submitted_for_review",
						},
						{
							Time:  time.Now(),
							Event: "approved",
						},
						{
							Time:  time.Now(),
							Event: "merged",
						},
					},
				},
				{
					ID:                "task-2",
					Status:            models.TaskStatusApproved,
					Description:       "Task approved first time",
					ReviewCyclesTotal: 1,
					Created:           time.Now(),
					History: []models.TaskHistoryEntry{
						{
							Time:  time.Now(),
							Event: "submitted_for_review",
						},
						{
							Time:  time.Now(),
							Event: "approved",
						},
					},
				},
			},
			agents: map[string]models.Agent{},
			wantMetrics: models.SprintMetrics{
				TasksDone:                        1,
				TasksInProgress:                  0,
				TasksBlocked:                     0,
				IterationsTotal:                  0,
				ReviewCyclesTotal:                3, // 2 + 1
				ReviewVerdictApprovals:           2,
				ReviewVerdictRejections:          1,
				ReviewVerdictCount:               3,
				ReviewVerdictApprovalRatePercent: 66,  // 2/3 * 100 = 66%
				TaskSubmittedForReviewCount:      2,   // Both tasks submitted
				TaskOutcomeApprovalRatePercent:   100, // 2 approved/merged out of 2 submitted = 100%
			},
			wantError: false,
		},
		{
			name: "suspicious approval rate warning - high review verdict rate",
			tasks: []models.Task{
				{
					ID:                "task-1",
					Status:            models.TaskStatusMerged,
					Description:       "Task 1",
					ReviewCyclesTotal: 1,
					Created:           time.Now(),
					History: []models.TaskHistoryEntry{
						{Event: "submitted_for_review"},
						{Event: "approved"},
					},
				},
				{
					ID:                "task-2",
					Status:            models.TaskStatusMerged,
					Description:       "Task 2",
					ReviewCyclesTotal: 1,
					Created:           time.Now(),
					History: []models.TaskHistoryEntry{
						{Event: "submitted_for_review"},
						{Event: "approved"},
					},
				},
				{
					ID:                "task-3",
					Status:            models.TaskStatusMerged,
					Description:       "Task 3",
					ReviewCyclesTotal: 1,
					Created:           time.Now(),
					History: []models.TaskHistoryEntry{
						{Event: "submitted_for_review"},
						{Event: "approved"},
					},
				},
			},
			agents: map[string]models.Agent{},
			wantMetrics: models.SprintMetrics{
				TasksDone:                        3,
				TasksInProgress:                  0,
				TasksBlocked:                     0,
				IterationsTotal:                  0,
				ReviewCyclesTotal:                3,
				ReviewVerdictApprovals:           3,
				ReviewVerdictRejections:          0,
				ReviewVerdictCount:               3,
				ReviewVerdictApprovalRatePercent: 100,
				TaskSubmittedForReviewCount:      3,
				TaskOutcomeApprovalRatePercent:   100,
			},
			wantError: false,
			wantWarnings: []string{
				"⚠️  Review verdict approval rate is 100% (3/3) - suspiciously high",
				"⚠️  Task outcome approval rate is 100% (3/3) - suspiciously high",
			},
		},
		{
			name: "all terminal task statuses count as done",
			tasks: []models.Task{
				{
					ID:          "task-1",
					Status:      models.TaskStatusMerged,
					Description: "Merged task",
					Created:     time.Now(),
				},
				{
					ID:          "task-2",
					Status:      models.TaskStatusAbandoned,
					Description: "Abandoned task",
					Created:     time.Now(),
				},
				{
					ID:          "task-3",
					Status:      models.TaskStatusSuperseded,
					Description: "Superseded task",
					Created:     time.Now(),
				},
			},
			agents: map[string]models.Agent{},
			wantMetrics: models.SprintMetrics{
				TasksDone:                        3,
				TasksInProgress:                  0,
				TasksBlocked:                     0,
				IterationsTotal:                  0,
				ReviewCyclesTotal:                0,
				ReviewVerdictApprovals:           0,
				ReviewVerdictRejections:          0,
				ReviewVerdictCount:               0,
				ReviewVerdictApprovalRatePercent: 0,
				TaskSubmittedForReviewCount:      0,
				TaskOutcomeApprovalRatePercent:   0,
			},
			wantError: false,
		},
		{
			name: "tasks in progress include claimed and ready for review",
			tasks: []models.Task{
				{
					ID:          "task-1",
					Status:      models.TaskStatusImplementing,
					Description: "Claimed task",
					Created:     time.Now(),
				},
				{
					ID:          "task-2",
					Status:      models.TaskStatusReadyForReview,
					Description: "Ready for review task",
					Created:     time.Now(),
				},
				{
					ID:          "task-3",
					Status:      models.TaskStatusRejected,
					Description: "Rejected task",
					Created:     time.Now(),
				},
			},
			agents: map[string]models.Agent{},
			wantMetrics: models.SprintMetrics{
				TasksDone:                        0,
				TasksInProgress:                  3, // IMPLEMENTING, READY_FOR_REVIEW, REJECTED
				TasksBlocked:                     0,
				IterationsTotal:                  0,
				ReviewCyclesTotal:                0,
				ReviewVerdictApprovals:           0,
				ReviewVerdictRejections:          0,
				ReviewVerdictCount:               0,
				ReviewVerdictApprovalRatePercent: 0,
				TaskSubmittedForReviewCount:      0,
				TaskOutcomeApprovalRatePercent:   0,
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test environment
			tempDir := t.TempDir()

			// Create test state with tasks and agents
			state := testhelpers.CreateValidState()
			state.Tasks = tt.tasks
			state.Agents = tt.agents

			// Setup liza directory and write state
			statePath, _ := testhelpers.SetupLizaDir(t, tempDir)
			testhelpers.SetupPipelineConfig(t, tempDir)
			bb := testhelpers.WriteInitialState(t, statePath, state)

			// Run update-sprint-metrics command
			err := UpdateSprintMetricsCommand(tempDir)

			// Check error
			if (err != nil) != tt.wantError {
				t.Errorf("UpdateSprintMetricsCommand() error = %v, wantError %v", err, tt.wantError)
			}

			if tt.wantError {
				return
			}

			// Read updated state
			updatedState, err := bb.Read()
			if err != nil {
				t.Fatalf("Failed to read updated state: %v", err)
			}

			// Compare metrics
			got := updatedState.Sprint.Metrics
			if got.TasksDone != tt.wantMetrics.TasksDone {
				t.Errorf("TasksDone = %v, want %v", got.TasksDone, tt.wantMetrics.TasksDone)
			}
			if got.TasksInProgress != tt.wantMetrics.TasksInProgress {
				t.Errorf("TasksInProgress = %v, want %v", got.TasksInProgress, tt.wantMetrics.TasksInProgress)
			}
			if got.TasksBlocked != tt.wantMetrics.TasksBlocked {
				t.Errorf("TasksBlocked = %v, want %v", got.TasksBlocked, tt.wantMetrics.TasksBlocked)
			}
			if got.IterationsTotal != tt.wantMetrics.IterationsTotal {
				t.Errorf("IterationsTotal = %v, want %v", got.IterationsTotal, tt.wantMetrics.IterationsTotal)
			}
			if got.ReviewCyclesTotal != tt.wantMetrics.ReviewCyclesTotal {
				t.Errorf("ReviewCyclesTotal = %v, want %v", got.ReviewCyclesTotal, tt.wantMetrics.ReviewCyclesTotal)
			}
			if got.ReviewVerdictApprovals != tt.wantMetrics.ReviewVerdictApprovals {
				t.Errorf("ReviewVerdictApprovals = %v, want %v", got.ReviewVerdictApprovals, tt.wantMetrics.ReviewVerdictApprovals)
			}
			if got.ReviewVerdictRejections != tt.wantMetrics.ReviewVerdictRejections {
				t.Errorf("ReviewVerdictRejections = %v, want %v", got.ReviewVerdictRejections, tt.wantMetrics.ReviewVerdictRejections)
			}
			if got.ReviewVerdictCount != tt.wantMetrics.ReviewVerdictCount {
				t.Errorf("ReviewVerdictCount = %v, want %v", got.ReviewVerdictCount, tt.wantMetrics.ReviewVerdictCount)
			}
			if got.ReviewVerdictApprovalRatePercent != tt.wantMetrics.ReviewVerdictApprovalRatePercent {
				t.Errorf("ReviewVerdictApprovalRatePercent = %v, want %v", got.ReviewVerdictApprovalRatePercent, tt.wantMetrics.ReviewVerdictApprovalRatePercent)
			}
			if got.TaskSubmittedForReviewCount != tt.wantMetrics.TaskSubmittedForReviewCount {
				t.Errorf("TaskSubmittedForReviewCount = %v, want %v", got.TaskSubmittedForReviewCount, tt.wantMetrics.TaskSubmittedForReviewCount)
			}
			if got.TaskOutcomeApprovalRatePercent != tt.wantMetrics.TaskOutcomeApprovalRatePercent {
				t.Errorf("TaskOutcomeApprovalRatePercent = %v, want %v", got.TaskOutcomeApprovalRatePercent, tt.wantMetrics.TaskOutcomeApprovalRatePercent)
			}
		})
	}
}
