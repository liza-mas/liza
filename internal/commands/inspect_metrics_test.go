package commands

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/usage"
)

func TestInspectMetrics(t *testing.T) {
	// Create test state with sprint metrics
	now := time.Now()
	state := &models.State{
		Sprint: models.Sprint{
			Metrics: models.SprintMetrics{
				TasksDone:                        5,
				TasksInProgress:                  2,
				TasksBlocked:                     1,
				IterationsTotal:                  15,
				ReviewCyclesTotal:                8,
				ReviewVerdictApprovals:           6,
				ReviewVerdictRejections:          2,
				ReviewVerdictCount:               8,
				ReviewVerdictApprovalRatePercent: 75,
				TaskSubmittedForReviewCount:      7,
				TaskOutcomeApprovalRatePercent:   85,
			},
		},
		Tasks: []models.Task{
			{ID: "t1", Status: models.TaskStatusMerged},
			{ID: "t2", Status: models.TaskStatusImplementing},
		},
		Agents: map[string]models.Agent{
			"coder-1": {
				Role:            "coder",
				Status:          models.AgentStatusWorking,
				Heartbeat:       now,
				Terminal:        "terminal1",
				IterationsTotal: 10,
				ContextPercent:  50,
			},
		},
	}

	tests := []struct {
		name       string
		opts       inspectMetricsOptions
		wantFormat string // "json", "yaml", "value", or "internal"
		wantErr    bool
	}{
		{
			name:       "get all metrics",
			opts:       inspectMetricsOptions{},
			wantFormat: "value",
		},
		{
			name:       "JSON format",
			opts:       inspectMetricsOptions{Format: "json"},
			wantFormat: "json",
		},
		{
			name:       "YAML format",
			opts:       inspectMetricsOptions{Format: "yaml"},
			wantFormat: "yaml",
		},
		{
			name:       "internal flag returns structured data",
			opts:       inspectMetricsOptions{Internal: true},
			wantFormat: "internal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := inspectMetrics(state, tt.opts)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			// Validate result based on format
			switch tt.wantFormat {
			case "internal":
				// Should return metricsInfo
				metricsInfo, ok := result.(metricsInfo)
				if !ok {
					t.Errorf("expected metricsInfo, got %T", result)
					return
				}
				// Verify metrics match state
				if metricsInfo.TasksDone != 5 {
					t.Errorf("expected TasksDone=5, got %d", metricsInfo.TasksDone)
				}
				if metricsInfo.TasksInProgress != 2 {
					t.Errorf("expected TasksInProgress=2, got %d", metricsInfo.TasksInProgress)
				}
				if metricsInfo.TasksBlocked != 1 {
					t.Errorf("expected TasksBlocked=1, got %d", metricsInfo.TasksBlocked)
				}
			case "json":
				output, ok := result.(string)
				if !ok {
					t.Errorf("expected string output, got %T", result)
					return
				}
				// Validate JSON
				var metricsInfo metricsInfo
				if err := json.Unmarshal([]byte(output), &metricsInfo); err != nil {
					t.Errorf("invalid JSON output: %v", err)
				}
				if metricsInfo.TasksDone != 5 {
					t.Errorf("expected TasksDone=5 in JSON, got %d", metricsInfo.TasksDone)
				}
			case "yaml":
				output, ok := result.(string)
				if !ok {
					t.Errorf("expected string output, got %T", result)
					return
				}
				// Just check it's not empty and contains expected values
				if output == "" {
					t.Errorf("expected non-empty YAML output")
				}
				if !strings.Contains(output, "tasks_done: 5") {
					t.Errorf("expected YAML to contain 'tasks_done: 5'")
				}
			case "value":
				output, ok := result.(string)
				if !ok {
					t.Errorf("expected string output, got %T", result)
					return
				}
				// Check output contains key metrics
				if !strings.Contains(output, "Tasks Done: 5") {
					t.Errorf("expected output to contain 'Tasks Done: 5'")
				}
				if !strings.Contains(output, "Tasks In Progress: 2") {
					t.Errorf("expected output to contain 'Tasks In Progress: 2'")
				}
			}
		})
	}
}

func TestInspectAgentMetrics(t *testing.T) {
	now := time.Now()
	agent1 := "coder-1"
	agent2 := "coder-2"

	state := &models.State{
		Agents: map[string]models.Agent{
			"coder-1": {
				Role:            "coder",
				Status:          models.AgentStatusWorking,
				IterationsTotal: 10,
				ContextPercent:  50,
			},
			"coder-2": {
				Role:            "coder",
				Status:          models.AgentStatusIdle,
				IterationsTotal: 5,
				ContextPercent:  30,
			},
		},
		Tasks: []models.Task{
			{
				ID:         "t1",
				Status:     models.TaskStatusMerged,
				AssignedTo: &agent1,
				Iteration:  2,
				History: []models.TaskHistoryEntry{
					{Time: now.Add(-2 * time.Hour), Event: "claimed", Agent: &agent1},
					{Time: now.Add(-1 * time.Hour), Event: "merged", Agent: &agent1},
				},
			},
			{
				ID:         "t2",
				Status:     models.TaskStatusMerged,
				AssignedTo: &agent1,
				Iteration:  1,
				History: []models.TaskHistoryEntry{
					{Time: now.Add(-3 * time.Hour), Event: "claimed", Agent: &agent1},
					{Time: now.Add(-2 * time.Hour), Event: "merged", Agent: &agent1},
				},
			},
			{
				ID:         "t3",
				Status:     models.TaskStatusImplementing,
				AssignedTo: &agent1,
				Iteration:  1,
				History: []models.TaskHistoryEntry{
					{Time: now.Add(-30 * time.Minute), Event: "claimed", Agent: &agent1},
				},
			},
			{
				ID:         "t4",
				Status:     models.TaskStatusMerged,
				AssignedTo: &agent2,
				Iteration:  1,
				History: []models.TaskHistoryEntry{
					{Time: now.Add(-2 * time.Hour), Event: "claimed", Agent: &agent2},
					{Time: now.Add(-1 * time.Hour), Event: "merged", Agent: &agent2},
				},
			},
			{
				ID:         "t5",
				Status:     models.TaskStatusAbandoned,
				AssignedTo: &agent2,
				Iteration:  3,
				History: []models.TaskHistoryEntry{
					{Time: now.Add(-4 * time.Hour), Event: "claimed", Agent: &agent2},
					{Time: now.Add(-3 * time.Hour), Event: "abandoned", Agent: &agent2},
				},
			},
		},
	}

	tests := []struct {
		name       string
		opts       inspectMetricsOptions
		wantFormat string
		wantCount  int // Expected number of agents in metrics
		wantErr    bool
	}{
		{
			name:       "get all agent metrics",
			opts:       inspectMetricsOptions{AgentMetrics: true},
			wantFormat: "table",
			wantCount:  2,
		},
		{
			name:       "agent metrics JSON format",
			opts:       inspectMetricsOptions{AgentMetrics: true, Format: "json"},
			wantFormat: "json",
			wantCount:  2,
		},
		{
			name:       "agent metrics YAML format",
			opts:       inspectMetricsOptions{AgentMetrics: true, Format: "yaml"},
			wantFormat: "yaml",
			wantCount:  2,
		},
		{
			name:       "agent metrics internal",
			opts:       inspectMetricsOptions{AgentMetrics: true, Internal: true},
			wantFormat: "internal",
			wantCount:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := inspectMetrics(state, tt.opts)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			// Validate result based on format
			switch tt.wantFormat {
			case "internal":
				// Should return []AgentMetricsInfo
				agentMetrics, ok := result.([]AgentMetricsInfo)
				if !ok {
					t.Errorf("expected []AgentMetricsInfo, got %T", result)
					return
				}
				if len(agentMetrics) != tt.wantCount {
					t.Errorf("expected %d agent metrics, got %d", tt.wantCount, len(agentMetrics))
				}

				// Find coder-1 metrics and verify calculations
				var coder1Metrics *AgentMetricsInfo
				for i := range agentMetrics {
					if agentMetrics[i].AgentID == "coder-1" {
						coder1Metrics = &agentMetrics[i]
						break
					}
				}
				if coder1Metrics == nil {
					t.Fatalf("expected to find coder-1 in agent metrics")
				}

				// coder-1 has 2 completed tasks and 1 in progress
				if coder1Metrics.TasksCompleted != 2 {
					t.Errorf("expected coder-1 TasksCompleted=2, got %d", coder1Metrics.TasksCompleted)
				}
				if coder1Metrics.TasksInProgress != 1 {
					t.Errorf("expected coder-1 TasksInProgress=1, got %d", coder1Metrics.TasksInProgress)
				}
				// Total iterations for coder-1 tasks: 2 + 1 + 1 = 4
				if coder1Metrics.TotalIterations != 4 {
					t.Errorf("expected coder-1 TotalIterations=4, got %d", coder1Metrics.TotalIterations)
				}
				// Success rate: 2/3 ≈ 66.67%
				if coder1Metrics.SuccessRatePercent < 66 || coder1Metrics.SuccessRatePercent > 67 {
					t.Errorf("expected coder-1 SuccessRatePercent≈67, got %d", coder1Metrics.SuccessRatePercent)
				}

			case "json":
				output, ok := result.(string)
				if !ok {
					t.Errorf("expected string output, got %T", result)
					return
				}
				// Validate JSON
				var agentMetrics []AgentMetricsInfo
				if err := json.Unmarshal([]byte(output), &agentMetrics); err != nil {
					t.Errorf("invalid JSON output: %v", err)
				}
				if len(agentMetrics) != tt.wantCount {
					t.Errorf("expected %d agent metrics in JSON, got %d", tt.wantCount, len(agentMetrics))
				}
			case "yaml":
				output, ok := result.(string)
				if !ok {
					t.Errorf("expected string output, got %T", result)
					return
				}
				if output == "" {
					t.Errorf("expected non-empty YAML output")
				}
			case "table":
				output, ok := result.(string)
				if !ok {
					t.Errorf("expected string output, got %T", result)
					return
				}
				// Check that agent IDs appear in output
				if !strings.Contains(output, "coder-1") {
					t.Errorf("expected output to contain coder-1")
				}
				if !strings.Contains(output, "coder-2") {
					t.Errorf("expected output to contain coder-2")
				}
			}
		})
	}
}

func TestBuildMetricsInfo(t *testing.T) {
	metrics := models.SprintMetrics{
		TasksDone:                        10,
		TasksInProgress:                  3,
		TasksBlocked:                     2,
		IterationsTotal:                  25,
		ReviewCyclesTotal:                12,
		ReviewVerdictApprovals:           8,
		ReviewVerdictRejections:          4,
		ReviewVerdictCount:               12,
		ReviewVerdictApprovalRatePercent: 67,
		TaskSubmittedForReviewCount:      11,
		TaskOutcomeApprovalRatePercent:   72,
	}

	info := buildMetricsInfo(metrics)

	// Verify all fields are copied correctly
	if info.TasksDone != 10 {
		t.Errorf("expected TasksDone=10, got %d", info.TasksDone)
	}
	if info.TasksInProgress != 3 {
		t.Errorf("expected TasksInProgress=3, got %d", info.TasksInProgress)
	}
	if info.TasksBlocked != 2 {
		t.Errorf("expected TasksBlocked=2, got %d", info.TasksBlocked)
	}
	if info.IterationsTotal != 25 {
		t.Errorf("expected IterationsTotal=25, got %d", info.IterationsTotal)
	}
	if info.ReviewCyclesTotal != 12 {
		t.Errorf("expected ReviewCyclesTotal=12, got %d", info.ReviewCyclesTotal)
	}
	if info.ReviewVerdictApprovalRatePercent != 67 {
		t.Errorf("expected ReviewVerdictApprovalRatePercent=67, got %d", info.ReviewVerdictApprovalRatePercent)
	}
	if info.TaskOutcomeApprovalRatePercent != 72 {
		t.Errorf("expected TaskOutcomeApprovalRatePercent=72, got %d", info.TaskOutcomeApprovalRatePercent)
	}
}

func TestCalculateAgentMetrics(t *testing.T) {
	now := time.Now()
	agent1 := "coder-1"
	agent2 := "coder-2"

	tasks := []models.Task{
		{
			ID:         "t1",
			Status:     models.TaskStatusMerged,
			AssignedTo: &agent1,
			Iteration:  2,
		},
		{
			ID:         "t2",
			Status:     models.TaskStatusMerged,
			AssignedTo: &agent1,
			Iteration:  1,
		},
		{
			ID:         "t3",
			Status:     models.TaskStatusImplementing,
			AssignedTo: &agent1,
			Iteration:  1,
		},
		{
			ID:         "t4",
			Status:     models.TaskStatusMerged,
			AssignedTo: &agent2,
			Iteration:  1,
		},
		{
			ID:         "t5",
			Status:     models.TaskStatusAbandoned,
			AssignedTo: &agent2,
			Iteration:  3,
		},
		{
			ID:         "t6",
			Status:     models.TaskStatusBlocked,
			AssignedTo: &agent2,
			Iteration:  1,
		},
	}

	agents := map[string]models.Agent{
		"coder-1": {
			Role:            "coder",
			Status:          models.AgentStatusWorking,
			Heartbeat:       now,
			IterationsTotal: 10,
		},
		"coder-2": {
			Role:            "coder",
			Status:          models.AgentStatusIdle,
			Heartbeat:       now,
			IterationsTotal: 5,
		},
	}

	metrics := calculateAgentMetrics(tasks, agents)

	// Should have metrics for both agents
	if len(metrics) != 2 {
		t.Errorf("expected 2 agent metrics, got %d", len(metrics))
	}

	// Find coder-1 metrics
	var coder1 *AgentMetricsInfo
	for i := range metrics {
		if metrics[i].AgentID == "coder-1" {
			coder1 = &metrics[i]
			break
		}
	}
	if coder1 == nil {
		t.Fatalf("expected to find coder-1 metrics")
	}

	// coder-1: 2 completed (MERGED), 1 in progress (IMPLEMENTING), 0 failed
	if coder1.TasksCompleted != 2 {
		t.Errorf("expected coder-1 TasksCompleted=2, got %d", coder1.TasksCompleted)
	}
	if coder1.TasksInProgress != 1 {
		t.Errorf("expected coder-1 TasksInProgress=1, got %d", coder1.TasksInProgress)
	}
	if coder1.TasksFailed != 0 {
		t.Errorf("expected coder-1 TasksFailed=0, got %d", coder1.TasksFailed)
	}
	// Total iterations: 2 + 1 + 1 = 4
	if coder1.TotalIterations != 4 {
		t.Errorf("expected coder-1 TotalIterations=4, got %d", coder1.TotalIterations)
	}
	// Success rate: 2/3 = 66.67%
	if coder1.SuccessRatePercent < 66 || coder1.SuccessRatePercent > 67 {
		t.Errorf("expected coder-1 SuccessRatePercent≈67, got %d", coder1.SuccessRatePercent)
	}

	// Find coder-2 metrics
	var coder2 *AgentMetricsInfo
	for i := range metrics {
		if metrics[i].AgentID == "coder-2" {
			coder2 = &metrics[i]
			break
		}
	}
	if coder2 == nil {
		t.Fatalf("expected to find coder-2 metrics")
	}

	// coder-2: 1 completed (MERGED), 1 in progress (BLOCKED), 1 failed (ABANDONED)
	if coder2.TasksCompleted != 1 {
		t.Errorf("expected coder-2 TasksCompleted=1, got %d", coder2.TasksCompleted)
	}
	if coder2.TasksInProgress != 1 {
		t.Errorf("expected coder-2 TasksInProgress=1, got %d", coder2.TasksInProgress)
	}
	if coder2.TasksFailed != 1 {
		t.Errorf("expected coder-2 TasksFailed=1, got %d", coder2.TasksFailed)
	}
	// Total iterations: 1 + 3 + 1 = 5
	if coder2.TotalIterations != 5 {
		t.Errorf("expected coder-2 TotalIterations=5, got %d", coder2.TotalIterations)
	}
	// Success rate: 1/3 = 33.33%
	if coder2.SuccessRatePercent < 33 || coder2.SuccessRatePercent > 34 {
		t.Errorf("expected coder-2 SuccessRatePercent≈33, got %d", coder2.SuccessRatePercent)
	}
}

// usageFixtureT0 anchors the usage fixtures; the sprint starts here and every
// record starts after it.
var usageFixtureT0 = time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)

func usageFixtureState() *models.State {
	at := func(hours int) time.Time { return usageFixtureT0.Add(time.Duration(hours) * time.Hour) }
	return &models.State{
		Sprint: models.Sprint{
			Timeline: models.SprintTimeline{Started: usageFixtureT0},
			Metrics: models.SprintMetrics{
				TasksDone: 1, TasksInProgress: 1, IterationsTotal: 3, ReviewCyclesTotal: 2,
				ReviewVerdictApprovals: 1, ReviewVerdictCount: 1, ReviewVerdictApprovalRatePercent: 100,
				TaskSubmittedForReviewCount: 1, TaskOutcomeApprovalRatePercent: 100,
			},
		},
		Tasks: []models.Task{
			{ID: "t1", Status: models.TaskStatusMerged, Lifecycle: &models.TaskLifecycle{Revision: 3}, History: []models.TaskHistoryEntry{
				{Time: at(0), Event: models.TaskEventCreated},
				{Time: at(1), Event: models.TaskEventClaimed},
				{Time: at(6), Event: models.TaskEventMerged},
			}},
			{ID: "t2", Status: models.TaskStatusImplementing, Lifecycle: &models.TaskLifecycle{Revision: 1}, History: []models.TaskHistoryEntry{
				{Time: at(0), Event: models.TaskEventCreated},
				{Time: at(2), Event: models.TaskEventClaimed},
			}},
		},
	}
}

// usageFixtureStore writes three authoritative records (two on merged t1, one
// on active t2) and one unknown-provenance record on t2 through the real store.
func usageFixtureStore(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	rec := func(taskID, runID string, hour, fresh, cacheRead, output int, provenance usage.Provenance) usage.Record {
		started := usageFixtureT0.Add(time.Duration(hour) * time.Hour)
		return usage.Record{
			TaskID: taskID, Role: "coder", AgentID: "coder-1", SupervisorRunID: runID, SessionID: taskID, Provider: "cli",
			StartedAt: started, EndedAt: started.Add(time.Minute),
			FreshInputTokens: fresh, CacheReadTokens: cacheRead, OutputTokens: output, Provenance: provenance,
		}
	}
	for _, r := range []usage.Record{
		rec("t1", "run-a", 2, 100, 900, 10, usage.ProvenanceTerminalAuthoritative),
		rec("t1", "run-b", 3, 50, 450, 5, usage.ProvenanceTerminalAuthoritative),
		rec("t2", "run-c", 3, 20, 80, 2, usage.ProvenanceTerminalAuthoritative),
		rec("t2", "run-d", 4, 0, 0, 0, usage.ProvenanceUnknown),
	} {
		if err := usage.Append(root, r); err != nil {
			t.Fatalf("usage.Append: %v", err)
		}
	}
	return root
}

func inspectMetricsInfo(t *testing.T, state *models.State, opts inspectMetricsOptions) metricsInfo {
	t.Helper()
	opts.Internal = true
	result, err := inspectMetrics(state, opts)
	if err != nil {
		t.Fatalf("inspectMetrics: %v", err)
	}
	info, ok := result.(metricsInfo)
	if !ok {
		t.Fatalf("expected metricsInfo, got %T", result)
	}
	return info
}

func TestInspectMetricsUsageSummary(t *testing.T) {
	// GIVEN a fixture store under the project root and a state whose sprint started before the records
	state := usageFixtureState()
	root := usageFixtureStore(t)

	// WHEN sprint metrics are inspected with the project root
	info := inspectMetricsInfo(t, state, inspectMetricsOptions{ProjectRoot: root})

	// THEN the usage block is populated from the store
	if info.Usage == nil {
		t.Fatalf("expected usage block, got nil (warnings: %v)", info.UsageWarnings)
	}
	if len(info.UsageWarnings) != 0 {
		t.Errorf("expected no usage warnings, got %v", info.UsageWarnings)
	}
	if !info.Usage.Window.Since.Equal(usageFixtureT0) {
		t.Errorf("expected window since %s, got %s", usageFixtureT0, info.Usage.Window.Since)
	}
	want := map[usage.OutcomeClass]usage.SummaryOutcome{
		usage.OutcomeMerged: {Outcome: usage.OutcomeMerged, Tasks: 1, FreshTokens: 150, CacheReadTokens: 1350, OutputTokens: 15},
		usage.OutcomeActive: {Outcome: usage.OutcomeActive, Tasks: 1, FreshTokens: 20, CacheReadTokens: 80, OutputTokens: 2},
	}
	if len(info.Usage.Outcomes) != len(want) {
		t.Fatalf("expected %d outcome rows, got %+v", len(want), info.Usage.Outcomes)
	}
	for _, row := range info.Usage.Outcomes {
		if row != want[row.Outcome] {
			t.Errorf("outcome %s: got %+v, want %+v", row.Outcome, row, want[row.Outcome])
		}
	}
	if info.Usage.CacheReadTokensPerMergedTask == nil || *info.Usage.CacheReadTokensPerMergedTask != 1350 {
		t.Errorf("expected cache_read_tokens_per_merged_task 1350, got %v", info.Usage.CacheReadTokensPerMergedTask)
	}
	if info.Usage.CacheHitPercent == nil || *info.Usage.CacheHitPercent != 89.375 {
		t.Errorf("expected cache_hit_percent 89.375, got %v", info.Usage.CacheHitPercent)
	}
	wantProvenance := usage.ProvenanceCounts{TerminalAuthoritative: 3, Unknown: 1}
	if info.Usage.Provenance == nil || *info.Usage.Provenance != wantProvenance {
		t.Errorf("expected provenance %+v, got %+v", wantProvenance, info.Usage.Provenance)
	}

	// AND the JSON and value renderings carry the block
	jsonOut, err := formatMetricsOutput(info, "json")
	if err != nil {
		t.Fatalf("formatMetricsOutput json: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(jsonOut), &decoded); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}
	if _, ok := decoded["usage"]; !ok {
		t.Errorf("expected usage key in JSON output: %s", jsonOut)
	}
	if _, ok := decoded["usage_warnings"]; ok {
		t.Errorf("expected no usage_warnings key in JSON output: %s", jsonOut)
	}
	valueOut, err := formatMetricsOutput(info, "value")
	if err != nil {
		t.Fatalf("formatMetricsOutput value: %v", err)
	}
	for _, fragment := range []string{"Usage: available", "merged: tasks 1, fresh 150, cache-read 1350, output 15", "Cache-read tokens per merged task: 1350", "Cache hit: 89.4%", "Provenance: terminal_authoritative 3, partial 0, unknown 1, conflicting 0"} {
		if !strings.Contains(valueOut, fragment) {
			t.Errorf("expected value output to contain %q:\n%s", fragment, valueOut)
		}
	}
}

func TestInspectMetricsWithoutUsageStore(t *testing.T) {
	// GIVEN a project root with no usage store
	state := usageFixtureState()
	root := t.TempDir()

	// WHEN sprint metrics are inspected
	info := inspectMetricsInfo(t, state, inspectMetricsOptions{ProjectRoot: root})

	// THEN the block is omitted and the warning is recorded
	if info.Usage != nil {
		t.Errorf("expected usage block omitted, got %+v", info.Usage)
	}
	if len(info.UsageWarnings) == 0 || !strings.Contains(info.UsageWarnings[0], "usage store unavailable") {
		t.Errorf("expected usage store unavailable warning, got %v", info.UsageWarnings)
	}

	// AND every pre-existing metrics field is byte-identical to today's output
	today, err := formatMetricsOutput(buildMetricsInfo(state.Sprint.Metrics), "json")
	if err != nil {
		t.Fatalf("formatMetricsOutput today: %v", err)
	}
	stripped := info
	stripped.Usage, stripped.UsageWarnings = nil, nil
	got, err := formatMetricsOutput(stripped, "json")
	if err != nil {
		t.Fatalf("formatMetricsOutput stripped: %v", err)
	}
	if got != today {
		t.Errorf("pre-existing metrics changed:\nwant %s\ngot  %s", today, got)
	}
	jsonOut, err := formatMetricsOutput(info, "json")
	if err != nil {
		t.Fatalf("formatMetricsOutput json: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(jsonOut), &decoded); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}
	if _, ok := decoded["usage"]; ok {
		t.Errorf("expected usage key omitted from JSON output: %s", jsonOut)
	}
	if _, ok := decoded["usage_warnings"]; !ok {
		t.Errorf("expected usage_warnings key in JSON output: %s", jsonOut)
	}

	// AND the value rendering states unavailability with the warning, after the unchanged existing lines
	valueOut, err := formatMetricsOutput(info, "value")
	if err != nil {
		t.Fatalf("formatMetricsOutput value: %v", err)
	}
	todayValue, err := formatMetricsOutput(buildMetricsInfo(state.Sprint.Metrics), "value")
	if err != nil {
		t.Fatalf("formatMetricsOutput today value: %v", err)
	}
	if !strings.HasPrefix(valueOut, todayValue) {
		t.Errorf("expected value output to start with today's output:\n%s", valueOut)
	}
	if !strings.Contains(valueOut, "Usage: unavailable") || !strings.Contains(valueOut, "Warning: usage store unavailable") {
		t.Errorf("expected unavailable usage block with warning:\n%s", valueOut)
	}
}
