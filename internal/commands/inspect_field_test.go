package commands

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

func TestGetField(t *testing.T) {
	now := time.Now()
	state := &models.State{
		Version: 1,
		Config: models.Config{
			Mode:                   models.SystemModeRunning,
			MaxCoderIterations:     10,
			MaxReviewCycles:        5,
			DoerMaxWait:            18000,
			DeprecatedCoderMaxWait: 600,
			IntegrationBranch:      "main",
		},
		Sprint: models.Sprint{
			ID:     "sprint-1",
			Status: models.SprintStatusInProgress,
			Timeline: models.SprintTimeline{
				Started:  now.Add(-2 * time.Hour),
				Deadline: now.Add(6 * time.Hour),
			},
			Metrics: models.SprintMetrics{
				TasksDone:       5,
				TasksInProgress: 3,
				TasksBlocked:    1,
			},
		},
		Tasks: []models.Task{
			{
				ID:          "task-1",
				Description: "Implement feature X",
				Status:      models.TaskStatusImplementing,
				Priority:    1,
				Created:     now.Add(-1 * time.Hour),
			},
		},
		Agents: map[string]models.Agent{
			"coder-1": {
				Role:      "coder",
				Status:    models.AgentStatusWorking,
				Heartbeat: now.Add(-5 * time.Second),
			},
		},
	}

	tests := []struct {
		name      string
		fieldPath string
		want      any
		wantErr   bool
	}{
		{
			name:      "config.mode",
			fieldPath: "config.mode",
			want:      "RUNNING",
			wantErr:   false,
		},
		{
			name:      "config.max_coder_iterations",
			fieldPath: "config.max_coder_iterations",
			want:      10,
			wantErr:   false,
		},
		{
			name:      "sprint.status",
			fieldPath: "sprint.status",
			want:      "IN_PROGRESS",
			wantErr:   false,
		},
		{
			name:      "sprint.id",
			fieldPath: "sprint.id",
			want:      "sprint-1",
			wantErr:   false,
		},
		{
			name:      "sprint.metrics.tasks_done",
			fieldPath: "sprint.metrics.tasks_done",
			want:      5,
			wantErr:   false,
		},
		{
			name:      "invalid field",
			fieldPath: "config.nonexistent",
			want:      nil,
			wantErr:   true,
		},
		{
			name:      "legacy coder_max_wait is not inspectable",
			fieldPath: "config.coder_max_wait",
			want:      nil,
			wantErr:   true,
		},
		{
			name:      "invalid entity",
			fieldPath: "nonexistent.field",
			want:      nil,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getField(state, tt.fieldPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("getField() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("getField() = %v (%T), want %v (%T)", got, got, tt.want, tt.want)
			}
		})
	}
}

func TestGetField_DiscoversAllTaggedConfigAndSprintFields(t *testing.T) {
	now := time.Now().UTC().Round(time.Second)
	modeChangedAt := now.Add(-30 * time.Minute)
	modeChangedBy := "orchestrator-1"
	escalationWebhook := "https://example.com/hook"
	checkpointAt := now.Add(2 * time.Hour)
	ended := now.Add(10 * time.Hour)
	retrospective := "Kept quality high under load"

	state := &models.State{
		Version: 7,
		Config: models.Config{
			MaxCoderIterations:       11,
			MaxReviewCycles:          6,
			HeartbeatInterval:        60,
			LeaseDuration:            1800,
			CoderPollInterval:        15,
			DoerMaxWait:              900,
			OrchestratorPollInterval: 45,
			OrchestratorMaxWait:      1200,
			ReviewerPollInterval:     20,
			ReviewerMaxWait:          1000,
			IntegrationBranch:        "integration",
			EscalationWebhook:        &escalationWebhook,
			Mode:                     models.SystemModePaused,
			ModeChangedAt:            &modeChangedAt,
			ModeChangedBy:            &modeChangedBy,
			DiagnosticLogging:        true,
		},
		Sprint: models.Sprint{
			ID:      "sprint-42",
			GoalRef: "specs/current.md",
			Scope: models.SprintScope{
				Planned: []string{"task-1", "task-2"},
				Stretch: []string{"task-99"},
			},
			Timeline: models.SprintTimeline{
				Started:      now.Add(-6 * time.Hour),
				Deadline:     now.Add(6 * time.Hour),
				CheckpointAt: &checkpointAt,
				Ended:        &ended,
			},
			Status: models.SprintStatusCheckpoint,
			Metrics: models.SprintMetrics{
				TasksDone:                        5,
				TasksInProgress:                  3,
				TasksBlocked:                     1,
				IterationsTotal:                  18,
				ReviewCyclesTotal:                9,
				ReviewVerdictApprovals:           7,
				ReviewVerdictRejections:          2,
				ReviewVerdictCount:               9,
				ReviewVerdictApprovalRatePercent: 77,
				TaskSubmittedForReviewCount:      8,
				TaskOutcomeApprovalRatePercent:   63,
				LifecycleOutcomes: &models.LifecycleOutcomeMetrics{
					Available: true, ObservedSince: &now, LastUpdated: &now,
					Counts:  map[string]map[string]uint64{"submit-for-review": {models.LifecycleCompleted: 2}},
					Warning: "observation window began after sprint start",
				},
			},
			Retrospective: &retrospective,
		},
	}

	expectedPaths := []string{"version"}
	expectedPaths = append(expectedPaths, yamlLeafPathsForType("config", reflect.TypeOf(models.Config{}))...)
	expectedPaths = append(expectedPaths, yamlLeafPathsForType("sprint", reflect.TypeOf(models.Sprint{}))...)
	sort.Strings(expectedPaths)

	for _, fieldPath := range expectedPaths {
		fieldPath := fieldPath
		t.Run(fieldPath, func(t *testing.T) {
			want, err := valueAtYAMLPath(state, fieldPath)
			if err != nil {
				t.Fatalf("failed to build expected value for %q: %v", fieldPath, err)
			}

			got, err := getField(state, fieldPath)
			if err != nil {
				t.Fatalf("getField(%q) returned error: %v", fieldPath, err)
			}

			if !reflect.DeepEqual(got, want) {
				t.Fatalf("getField(%q) = %#v (%T), want %#v (%T)", fieldPath, got, got, want, want)
			}
		})
	}
}

func TestGetComputedField(t *testing.T) {
	now := time.Now()
	state := &models.State{
		Sprint: models.Sprint{
			ID:     "sprint-1",
			Status: models.SprintStatusInProgress,
			Timeline: models.SprintTimeline{
				Started:  now.Add(-2 * time.Hour),
				Deadline: now.Add(6 * time.Hour),
			},
			Metrics: models.SprintMetrics{
				TasksDone:       5,
				TasksInProgress: 3,
				TasksBlocked:    1,
			},
		},
		Tasks: []models.Task{
			{ID: "task-1", Status: models.TaskStatusMerged},
			{ID: "task-2", Status: models.TaskStatusMerged},
			{ID: "task-3", Status: models.TaskStatusMerged},
			{ID: "task-4", Status: models.TaskStatusMerged},
			{ID: "task-5", Status: models.TaskStatusMerged},
			{ID: "task-6", Status: models.TaskStatusImplementing},
			{ID: "task-7", Status: models.TaskStatusImplementing},
			{ID: "task-8", Status: models.TaskStatusImplementing},
			{ID: "task-9", Status: models.TaskStatusReady},
		},
		Agents: map[string]models.Agent{
			"coder-1": {
				Role:      "coder",
				Status:    models.AgentStatusWorking,
				Heartbeat: now.Add(-5 * time.Second),
			},
			"coder-2": {
				Role:      "coder",
				Status:    models.AgentStatusIdle,
				Heartbeat: now.Add(-3 * time.Second),
			},
			"code-reviewer-1": {
				Role:      "code-reviewer",
				Status:    models.AgentStatusWorking,
				Heartbeat: now.Add(-2 * time.Second),
			},
		},
	}

	tests := []struct {
		name      string
		fieldPath string
		wantType  string // Type check instead of exact value (for time-based fields)
		wantErr   bool
	}{
		{
			name:      "agents.active_count",
			fieldPath: "agents.active_count",
			wantType:  "int",
			wantErr:   false,
		},
		{
			name:      "agents.utilization",
			fieldPath: "agents.utilization",
			wantType:  "float64",
			wantErr:   false,
		},
		{
			name:      "tasks.completion_rate",
			fieldPath: "tasks.completion_rate",
			wantType:  "float64",
			wantErr:   false,
		},
		{
			name:      "sprint.elapsed",
			fieldPath: "sprint.elapsed",
			wantType:  "string",
			wantErr:   false,
		},
		{
			name:      "sprint.remaining",
			fieldPath: "sprint.remaining",
			wantType:  "string",
			wantErr:   false,
		},
		{
			name:      "sprint.progress_percent",
			fieldPath: "sprint.progress_percent",
			wantType:  "float64",
			wantErr:   false,
		},
		{
			name:      "invalid computed field",
			fieldPath: "sprint.nonexistent",
			wantType:  "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getComputedField(state, tt.fieldPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("getComputedField() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			// Type check
			switch tt.wantType {
			case "int":
				if _, ok := got.(int); !ok {
					t.Errorf("getComputedField() type = %T, want int", got)
				}
			case "float64":
				if _, ok := got.(float64); !ok {
					t.Errorf("getComputedField() type = %T, want float64", got)
				}
			case "string":
				if _, ok := got.(string); !ok {
					t.Errorf("getComputedField() type = %T, want string", got)
				}
			}
		})
	}
}

func TestGetComputedFieldAgentSpecific(t *testing.T) {
	now := time.Now()
	reviewerTask := "review-task-b"
	doerTask := "doer-task-b"
	legacyTask := "legacy-task"
	state := &models.State{
		Agents: map[string]models.Agent{
			"coder-1": {
				Role:        "coder",
				Status:      models.AgentStatusWorking,
				Heartbeat:   now.Add(-30 * time.Second),
				CurrentTask: &doerTask,
			},
			"reviewer-1": {
				Role:        "code-reviewer",
				Status:      models.AgentStatusReviewing,
				Heartbeat:   now,
				CurrentTask: &reviewerTask,
			},
			"legacy-1": {
				Role:        "coder",
				Status:      models.AgentStatusWorking,
				Heartbeat:   now,
				CurrentTask: &legacyTask,
			},
			"idle-1": {
				Role:      "coder",
				Status:    models.AgentStatusIdle,
				Heartbeat: now,
			},
		},
		Tasks: []models.Task{
			{
				ID:         "review-task-a",
				Status:     models.TaskStatusMerged,
				AssignedTo: stringPtr("reviewer-1"),
				History: []models.TaskHistoryEntry{
					{Time: now.Add(-5 * time.Hour), Event: models.TaskEventClaimed, Agent: stringPtr("reviewer-1")},
				},
			},
			{
				ID:          reviewerTask,
				Status:      models.TaskStatusReviewing,
				ReviewingBy: stringPtr("reviewer-1"),
				History: []models.TaskHistoryEntry{
					{Time: now.Add(-2 * time.Hour), Event: models.TaskEventClaimed, Agent: stringPtr("old-doer")},
					{Time: now.Add(-17 * time.Minute), Event: models.TaskEventClaimed, Agent: stringPtr("reviewer-1")},
				},
			},
			{
				ID:         "doer-task-a",
				Status:     models.TaskStatusMerged,
				AssignedTo: stringPtr("coder-1"),
				History: []models.TaskHistoryEntry{
					{Time: now.Add(-4 * time.Hour), Event: models.TaskEventClaimed, Agent: stringPtr("coder-1")},
				},
			},
			{
				ID:         doerTask,
				Status:     models.TaskStatusIntegrationFailed,
				AssignedTo: stringPtr("coder-1"),
				History: []models.TaskHistoryEntry{
					{Time: now.Add(-70 * time.Minute), Event: models.TaskEventClaimed, Agent: stringPtr("coder-1")},
					{Time: now.Add(-11 * time.Minute), Event: models.TaskEventClaimedForIntegrationFix, Agent: stringPtr("coder-1")},
				},
			},
			{
				ID:     legacyTask,
				Status: models.TaskStatusImplementing,
				History: []models.TaskHistoryEntry{
					{Time: now.Add(-45 * time.Minute), Event: models.TaskEventClaimed},
				},
			},
		},
	}

	tests := []struct {
		name      string
		fieldPath string
		want      string
	}{
		{
			name:      "agent.coder-1.time_since_heartbeat",
			fieldPath: "agent.coder-1.time_since_heartbeat",
			want:      "30s",
		},
		{
			name:      "doer current task latest matching assignment",
			fieldPath: "agent.coder-1.time_on_task",
			want:      "11m",
		},
		{
			name:      "reviewer current task latest matching assignment",
			fieldPath: "agent.reviewer-1.time_on_task",
			want:      "17m",
		},
		{
			name:      "active legacy agent without matching start",
			fieldPath: "agent.legacy-1.time_on_task",
			want:      "0s",
		},
		{
			name:      "idle agent",
			fieldPath: "agent.idle-1.time_on_task",
			want:      "0s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getComputedField(state, tt.fieldPath)
			if err != nil {
				t.Errorf("getComputedField() error = %v", err)
				return
			}

			str, ok := got.(string)
			if !ok {
				t.Errorf("getComputedField() type = %T, want string", got)
				return
			}
			if !strings.Contains(str, tt.want) {
				t.Errorf("getComputedField() = %q, want duration containing %q", str, tt.want)
			}
		})
	}
}

// time_in_status measures the current status, not time on task: a task that was
// blocked without ever being claimed reported 0s while it read time-on-task.
func TestGetComputedFieldTimeInStatusIsStatusBased(t *testing.T) {
	now := time.Now()
	state := &models.State{
		Tasks: []models.Task{
			{
				ID:      "task-1",
				Status:  models.TaskStatusBlocked,
				Created: now.Add(-5 * time.Hour),
				History: []models.TaskHistoryEntry{
					{Time: now.Add(-3 * time.Hour), Event: models.TaskEventBlocked},
					{Time: now.Add(-1 * time.Minute), Event: models.TaskEventClaimReleased},
				},
			},
		},
	}

	got, err := getComputedField(state, "task.task-1.time_in_status")
	if err != nil {
		t.Fatalf("getComputedField() error = %v", err)
	}
	str, ok := got.(string)
	if !ok {
		t.Fatalf("getComputedField() type = %T, want string", got)
	}
	if !strings.Contains(str, "3h") {
		t.Errorf("getComputedField() = %q, want duration since the blocked event", str)
	}
}

func TestGetComputedFieldTaskSpecific(t *testing.T) {
	now := time.Now()
	state := &models.State{
		Tasks: []models.Task{
			{
				ID:      "task-1",
				Status:  models.TaskStatusImplementing,
				Created: now.Add(-2 * time.Hour),
				History: []models.TaskHistoryEntry{
					{
						Time:  now.Add(-30 * time.Minute),
						Event: "claimed",
					},
				},
			},
		},
	}

	tests := []struct {
		name      string
		fieldPath string
		wantType  string
	}{
		{
			name:      "task.task-1.age",
			fieldPath: "task.task-1.age",
			wantType:  "string",
		},
		{
			name:      "task.task-1.time_in_status",
			fieldPath: "task.task-1.time_in_status",
			wantType:  "string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getComputedField(state, tt.fieldPath)
			if err != nil {
				t.Errorf("getComputedField() error = %v", err)
				return
			}

			// Check type and that it's a duration string
			if str, ok := got.(string); ok {
				if !strings.Contains(str, "s") && !strings.Contains(str, "m") && !strings.Contains(str, "h") {
					t.Errorf("getComputedField() = %q, doesn't look like a duration", str)
				}
			} else {
				t.Errorf("getComputedField() type = %T, want string", got)
			}
		})
	}
}

// Helper function
func stringPtr(s string) *string {
	return &s
}

func yamlLeafPathsForType(prefix string, t reflect.Type) []string {
	paths := make([]string, 0)
	timeType := reflect.TypeOf(time.Time{})

	var walk func(pathPrefix string, fieldType reflect.Type)
	walk = func(pathPrefix string, fieldType reflect.Type) {
		fieldType = derefType(fieldType)
		if fieldType.Kind() != reflect.Struct || fieldType == timeType {
			paths = append(paths, pathPrefix)
			return
		}

		for i := 0; i < fieldType.NumField(); i++ {
			field := fieldType.Field(i)
			if field.Tag.Get("inspect") == "-" {
				continue
			}
			tagName := yamlTagName(field.Tag.Get("yaml"))
			if tagName == "" {
				continue
			}

			childPath := pathPrefix + "." + tagName
			childType := derefType(field.Type)
			if childType.Kind() == reflect.Struct && childType != timeType {
				walk(childPath, field.Type)
				continue
			}

			paths = append(paths, childPath)
		}
	}

	walk(prefix, t)
	return paths
}

func valueAtYAMLPath(state *models.State, fieldPath string) (any, error) {
	if fieldPath == "" {
		return nil, fmt.Errorf("empty field path")
	}

	parts := strings.Split(fieldPath, ".")
	current := reflect.ValueOf(state)

	for _, part := range parts {
		current = derefValue(current)
		if !current.IsValid() {
			return nil, fmt.Errorf("nil while traversing %q", fieldPath)
		}
		if current.Kind() != reflect.Struct {
			return nil, fmt.Errorf("cannot descend into %q for %q", current.Kind(), fieldPath)
		}

		next, ok := findStructFieldByYAMLTag(current, part)
		if !ok {
			return nil, fmt.Errorf("yaml tag %q not found in %s for %q", part, current.Type(), fieldPath)
		}
		current = next
	}

	return normalizeExpectedValue(current), nil
}

func findStructFieldByYAMLTag(v reflect.Value, tagName string) (reflect.Value, bool) {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.Tag.Get("inspect") == "-" {
			continue
		}
		if yamlTagName(field.Tag.Get("yaml")) == tagName {
			return v.Field(i), true
		}
	}
	return reflect.Value{}, false
}

func normalizeExpectedValue(v reflect.Value) any {
	v = derefValue(v)
	if !v.IsValid() {
		return nil
	}
	if v.Kind() == reflect.String {
		return v.String()
	}
	return v.Interface()
}

func derefValue(v reflect.Value) reflect.Value {
	for v.IsValid() && v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}
	return v
}

func derefType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

func yamlTagName(tag string) string {
	if tag == "" {
		return ""
	}
	base := strings.Split(tag, ",")[0]
	if base == "" || base == "-" {
		return ""
	}
	return base
}
