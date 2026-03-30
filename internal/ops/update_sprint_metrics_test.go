package ops

import (
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestComputeSprintMetrics_Empty(t *testing.T) {
	state := testhelpers.CreateValidState()
	metrics := ComputeSprintMetrics(state)

	if metrics.TasksDone != 0 {
		t.Errorf("TasksDone = %d, want 0", metrics.TasksDone)
	}
	if metrics.TasksInProgress != 0 {
		t.Errorf("TasksInProgress = %d, want 0", metrics.TasksInProgress)
	}
	if metrics.TasksBlocked != 0 {
		t.Errorf("TasksBlocked = %d, want 0", metrics.TasksBlocked)
	}
}

func TestComputeSprintMetrics_TaskCounting(t *testing.T) {
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()

	// Add tasks in various statuses
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("t1", models.TaskStatusMerged, now),       // terminal
		testhelpers.BuildTaskByStatus("t2", models.TaskStatusAbandoned, now),    // terminal
		testhelpers.BuildTaskByStatus("t3", models.TaskStatusSuperseded, now),   // terminal
		testhelpers.BuildTaskByStatus("t4", models.TaskStatusImplementing, now), // in progress
		testhelpers.BuildTaskByStatus("t5", models.TaskStatusReviewing, now),    // in progress
		testhelpers.BuildTaskByStatus("t6", models.TaskStatusRejected, now),     // in progress
		testhelpers.BuildTaskByStatus("t7", models.TaskStatusBlocked, now),      // blocked
		testhelpers.BuildTaskByStatus("t8", models.TaskStatusReady, now),        // neither
	}

	metrics := ComputeSprintMetrics(state)

	if metrics.TasksDone != 3 {
		t.Errorf("TasksDone = %d, want 3", metrics.TasksDone)
	}
	if metrics.TasksInProgress != 3 {
		t.Errorf("TasksInProgress = %d, want 3", metrics.TasksInProgress)
	}
	if metrics.TasksBlocked != 1 {
		t.Errorf("TasksBlocked = %d, want 1", metrics.TasksBlocked)
	}
}

func TestComputeSprintMetrics_ReviewVerdicts(t *testing.T) {
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()

	state.Tasks = []models.Task{
		{
			ID:          "t1",
			Description: "Task 1",
			Status:      models.TaskStatusMerged,
			Priority:    1,
			Created:     now,
			SpecRef:     "README.md",
			DoneWhen:    "Done",
			Scope:       "Test",
			History: []models.TaskHistoryEntry{
				{Time: now, Event: models.TaskEventSubmittedForReview},
				{Time: now, Event: models.TaskEventApproved},
			},
		},
		{
			ID:          "t2",
			Description: "Task 2",
			Status:      models.TaskStatusRejected,
			Priority:    1,
			Created:     now,
			SpecRef:     "README.md",
			DoneWhen:    "Done",
			Scope:       "Test",
			History: []models.TaskHistoryEntry{
				{Time: now, Event: models.TaskEventSubmittedForReview},
				{Time: now, Event: models.TaskEventRejected},
				{Time: now, Event: models.TaskEventSubmittedForReview},
				{Time: now, Event: models.TaskEventRejected},
			},
		},
	}

	metrics := ComputeSprintMetrics(state)

	if metrics.ReviewVerdictCount != 3 {
		t.Errorf("ReviewVerdictCount = %d, want 3", metrics.ReviewVerdictCount)
	}
	if metrics.ReviewVerdictApprovals != 1 {
		t.Errorf("ReviewVerdictApprovals = %d, want 1", metrics.ReviewVerdictApprovals)
	}
	if metrics.ReviewVerdictRejections != 2 {
		t.Errorf("ReviewVerdictRejections = %d, want 2", metrics.ReviewVerdictRejections)
	}
	if metrics.TaskSubmittedForReviewCount != 2 {
		t.Errorf("TaskSubmittedForReviewCount = %d, want 2", metrics.TaskSubmittedForReviewCount)
	}
	// Approval rate: 1/3 = 33%
	if metrics.ReviewVerdictApprovalRatePercent != 33 {
		t.Errorf("ReviewVerdictApprovalRatePercent = %d, want 33", metrics.ReviewVerdictApprovalRatePercent)
	}
}

func TestComputeSprintMetrics_TaskOutcomeApprovalRate(t *testing.T) {
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()

	state.Tasks = []models.Task{
		{
			ID: "t1", Description: "T1", Status: models.TaskStatusMerged,
			Priority: 1, Created: now, SpecRef: "README.md", DoneWhen: "Done", Scope: "Test",
			History: []models.TaskHistoryEntry{{Time: now, Event: models.TaskEventSubmittedForReview}},
		},
		{
			ID: "t2", Description: "T2", Status: models.TaskStatusApproved,
			Priority: 1, Created: now, SpecRef: "README.md", DoneWhen: "Done", Scope: "Test",
			History: []models.TaskHistoryEntry{{Time: now, Event: models.TaskEventSubmittedForReview}},
		},
		{
			ID: "t3", Description: "T3", Status: models.TaskStatusRejected,
			Priority: 1, Created: now, SpecRef: "README.md", DoneWhen: "Done", Scope: "Test",
			History: []models.TaskHistoryEntry{{Time: now, Event: models.TaskEventSubmittedForReview}},
		},
	}

	metrics := ComputeSprintMetrics(state)

	// 2 out of 3 submitted tasks are approved/merged = 66%
	if metrics.TaskOutcomeApprovalRatePercent != 66 {
		t.Errorf("TaskOutcomeApprovalRatePercent = %d, want 66", metrics.TaskOutcomeApprovalRatePercent)
	}
}

func TestComputeSprintMetrics_AgentIterations(t *testing.T) {
	state := testhelpers.CreateValidState()
	state.Agents["agent-1"] = models.Agent{IterationsTotal: 5}
	state.Agents["agent-2"] = models.Agent{IterationsTotal: 3}

	metrics := ComputeSprintMetrics(state)

	if metrics.IterationsTotal != 8 {
		t.Errorf("IterationsTotal = %d, want 8", metrics.IterationsTotal)
	}
}

func TestComputeSprintMetrics_ReviewCyclesAggregated(t *testing.T) {
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()

	state.Tasks = []models.Task{
		{
			ID: "t1", Description: "T1", Status: models.TaskStatusReady,
			Priority: 1, Created: now, SpecRef: "README.md", DoneWhen: "Done", Scope: "Test",
			ReviewCyclesTotal: 3, History: []models.TaskHistoryEntry{},
		},
		{
			ID: "t2", Description: "T2", Status: models.TaskStatusReady,
			Priority: 1, Created: now, SpecRef: "README.md", DoneWhen: "Done", Scope: "Test",
			ReviewCyclesTotal: 2, History: []models.TaskHistoryEntry{},
		},
	}

	metrics := ComputeSprintMetrics(state)

	if metrics.ReviewCyclesTotal != 5 {
		t.Errorf("ReviewCyclesTotal = %d, want 5", metrics.ReviewCyclesTotal)
	}
}

func TestCheckSuspiciousRates(t *testing.T) {
	tests := []struct {
		name        string
		metrics     models.SprintMetrics
		wantCount   int
		wantContain string // check first warning contains this
	}{
		{
			name: "no warnings when below threshold",
			metrics: models.SprintMetrics{
				ReviewVerdictCount:               5,
				ReviewVerdictApprovals:           3,
				ReviewVerdictApprovalRatePercent: 60,
			},
			wantCount: 0,
		},
		{
			name: "no warnings when count too low",
			metrics: models.SprintMetrics{
				ReviewVerdictCount:               2, // < 3
				ReviewVerdictApprovals:           2,
				ReviewVerdictApprovalRatePercent: 100,
			},
			wantCount: 0,
		},
		{
			name: "warning for high review verdict rate",
			metrics: models.SprintMetrics{
				ReviewVerdictCount:               10,
				ReviewVerdictApprovals:           10,
				ReviewVerdictApprovalRatePercent: 100,
			},
			wantCount:   1,
			wantContain: "Review verdict approval rate",
		},
		{
			name: "warning for high task outcome rate",
			metrics: models.SprintMetrics{
				TaskSubmittedForReviewCount:    5,
				TaskOutcomeApprovalRatePercent: 100,
			},
			wantCount:   1,
			wantContain: "Task outcome approval rate",
		},
		{
			name: "both warnings when both rates are high",
			metrics: models.SprintMetrics{
				ReviewVerdictCount:               3,
				ReviewVerdictApprovals:           3,
				ReviewVerdictApprovalRatePercent: 100,
				TaskSubmittedForReviewCount:      3,
				TaskOutcomeApprovalRatePercent:   100,
			},
			wantCount: 2,
		},
		{
			name: "no warning at exactly 95%",
			metrics: models.SprintMetrics{
				ReviewVerdictCount:               20,
				ReviewVerdictApprovals:           19,
				ReviewVerdictApprovalRatePercent: 95,
			},
			wantCount: 0,
		},
		{
			name: "warning at 96%",
			metrics: models.SprintMetrics{
				ReviewVerdictCount:               100,
				ReviewVerdictApprovals:           96,
				ReviewVerdictApprovalRatePercent: 96,
			},
			wantCount:   1,
			wantContain: "96%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warnings := CheckSuspiciousRates(tt.metrics)
			if len(warnings) != tt.wantCount {
				t.Errorf("CheckSuspiciousRates() returned %d warnings, want %d: %v", len(warnings), tt.wantCount, warnings)
			}
			if tt.wantContain != "" && tt.wantCount > 0 {
				found := false
				for _, w := range warnings {
					if contains(w, tt.wantContain) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("No warning contains %q, got %v", tt.wantContain, warnings)
				}
			}
		})
	}
}

func TestUpdateSprintMetrics(t *testing.T) {
	now := time.Now().UTC()
	tmpDir := t.TempDir()

	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("t1", models.TaskStatusMerged, now),
		testhelpers.BuildTaskByStatus("t2", models.TaskStatusImplementing, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	metrics, err := UpdateSprintMetrics(tmpDir)
	if err != nil {
		t.Fatalf("UpdateSprintMetrics() error: %v", err)
	}

	if metrics.TasksDone != 1 {
		t.Errorf("TasksDone = %d, want 1", metrics.TasksDone)
	}
	if metrics.TasksInProgress != 1 {
		t.Errorf("TasksInProgress = %d, want 1", metrics.TasksInProgress)
	}

	// Verify metrics were persisted to state
	readState := readStateForTest(t, stateFile)
	if readState.Sprint.Metrics.TasksDone != 1 {
		t.Errorf("Persisted TasksDone = %d, want 1", readState.Sprint.Metrics.TasksDone)
	}
}

// contains is a helper to avoid importing strings in this test file.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsImpl(s, substr))
}

func containsImpl(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
