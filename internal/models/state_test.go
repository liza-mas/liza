package models

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/roles"
	"gopkg.in/yaml.v3"
)

func TestTaskStatusConstants(t *testing.T) {
	validStatuses := []TaskStatus{
		TaskStatusDraft,
		TaskStatusReady,
		TaskStatusImplementing,
		TaskStatusReadyForReview,
		TaskStatusReviewing,
		TaskStatusRejected,
		TaskStatusApproved,
		TaskStatusMerged,
		TaskStatusBlocked,
		TaskStatusAbandoned,
		TaskStatusSuperseded,
		TaskStatusIntegrationFailed,
		TaskStatusDraftCodingPlan,
		TaskStatusCodePlanning,
		TaskStatusCodingPlanToReview,
		TaskStatusReviewingCodingPlan,
		TaskStatusCodingPlanApproved,
		TaskStatusCodingPlanRejected,
	}

	for _, status := range validStatuses {
		if !status.IsValid() {
			t.Errorf("Status %s should be valid", status)
		}
	}

	invalidStatus := TaskStatus("INVALID")
	if invalidStatus.IsValid() {
		t.Errorf("Status %s should be invalid", invalidStatus)
	}
}

func TestTaskTerminalStates(t *testing.T) {
	terminalStates := []TaskStatus{
		TaskStatusMerged,
		TaskStatusAbandoned,
		TaskStatusSuperseded,
	}

	for _, status := range terminalStates {
		if !status.IsTerminal() {
			t.Errorf("Status %s should be terminal", status)
		}
	}

	nonTerminalStates := []TaskStatus{
		TaskStatusDraft,
		TaskStatusReady,
		TaskStatusImplementing,
		TaskStatusReadyForReview,
		TaskStatusReviewing,
		TaskStatusRejected,
		TaskStatusApproved,
		TaskStatusBlocked,
		TaskStatusIntegrationFailed,
	}

	for _, status := range nonTerminalStates {
		if status.IsTerminal() {
			t.Errorf("Status %s should not be terminal", status)
		}
	}
}

func TestComputeSprintMetricsWithTerminalStates_CountsPipelineCleanAsDone(t *testing.T) {
	state := &State{
		Tasks: []Task{
			{ID: "integration-clean", Status: TaskStatus("INTEGRATION_ANALYSIS_CLEAN")},
		},
	}

	metrics := state.ComputeSprintMetricsWithTerminalStates([]TaskStatus{"INTEGRATION_ANALYSIS_CLEAN"})

	if metrics.TasksDone != 1 {
		t.Errorf("TasksDone = %d, want 1", metrics.TasksDone)
	}
	if metrics.TasksBlocked != 0 {
		t.Errorf("TasksBlocked = %d, want 0", metrics.TasksBlocked)
	}
}

func TestAgentStatusConstants(t *testing.T) {
	validStatuses := []AgentStatus{
		AgentStatusStarting,
		AgentStatusIdle,
		AgentStatusWorking,
		AgentStatusReviewing,
		AgentStatusWaiting,
		AgentStatusHandoff,
		AgentStatusPlanning,
	}

	for _, status := range validStatuses {
		if !status.IsValid() {
			t.Errorf("Agent status %s should be valid", status)
		}
	}

	invalidStatus := AgentStatus("INVALID")
	if invalidStatus.IsValid() {
		t.Errorf("Agent status %s should be invalid", invalidStatus)
	}
}

func TestAgentStatusPlanning(t *testing.T) {
	// Test that PLANNING constant exists and has correct value
	if AgentStatusPlanning != "PLANNING" {
		t.Errorf("AgentStatusPlanning value = %s, want PLANNING", AgentStatusPlanning)
	}

	// Test that PLANNING is valid
	if !AgentStatusPlanning.IsValid() {
		t.Error("AgentStatusPlanning should be valid")
	}

	// Test string representation
	status := AgentStatusPlanning
	if string(status) != "PLANNING" {
		t.Errorf("String representation = %s, want PLANNING", string(status))
	}
}

func TestGoalStatusConstants(t *testing.T) {
	validStatuses := []GoalStatus{
		GoalStatusInProgress,
		GoalStatusCompleted,
		GoalStatusAborted,
	}

	for _, status := range validStatuses {
		if !status.IsValid() {
			t.Errorf("Goal status %s should be valid", status)
		}
	}
}

func TestSprintStatusConstants(t *testing.T) {
	validStatuses := []SprintStatus{
		SprintStatusInProgress,
		SprintStatusCheckpoint,
		SprintStatusCompleted,
		SprintStatusAborted,
	}

	for _, status := range validStatuses {
		if !status.IsValid() {
			t.Errorf("Sprint status %s should be valid", status)
		}
	}
}

func TestTaskYAMLMarshaling(t *testing.T) {
	created, _ := time.Parse(time.RFC3339, "2025-01-17T14:05:00Z")
	task := Task{
		ID:          "task-1",
		Description: "Test task",
		Status:      TaskStatusReady,
		Priority:    1,
		Created:     created,
		SpecRef:     "specs/test.md",
		DoneWhen:    "Tests pass",
		DependsOn:   []string{},
		History: []TaskHistoryEntry{
			{
				Time:  created,
				Event: "created",
			},
		},
	}

	// Marshal to YAML
	data, err := yaml.Marshal(&task)
	if err != nil {
		t.Fatalf("Failed to marshal task: %v", err)
	}

	// Unmarshal back
	var unmarshaled Task
	err = yaml.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal task: %v", err)
	}

	// Verify fields
	if unmarshaled.ID != task.ID {
		t.Errorf("ID mismatch: got %s, want %s", unmarshaled.ID, task.ID)
	}
	if unmarshaled.Status != task.Status {
		t.Errorf("Status mismatch: got %s, want %s", unmarshaled.Status, task.Status)
	}
	if unmarshaled.Description != task.Description {
		t.Errorf("Description mismatch: got %s, want %s", unmarshaled.Description, task.Description)
	}
}

func TestAgentYAMLMarshaling(t *testing.T) {
	heartbeat, _ := time.Parse(time.RFC3339, "2025-01-17T14:50:00Z")
	leaseExpires, _ := time.Parse(time.RFC3339, "2025-01-17T15:00:00Z")

	agent := Agent{
		Role:            "coder",
		Status:          AgentStatusWorking,
		CurrentTask:     strPtr("task-1"),
		LeaseExpires:    &leaseExpires,
		Heartbeat:       heartbeat,
		Terminal:        "/dev/pts/2",
		IterationsTotal: 10,
		ContextPercent:  34,
	}

	// Marshal to YAML
	data, err := yaml.Marshal(&agent)
	if err != nil {
		t.Fatalf("Failed to marshal agent: %v", err)
	}

	// Unmarshal back
	var unmarshaled Agent
	err = yaml.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal agent: %v", err)
	}

	// Verify fields
	if unmarshaled.Role != agent.Role {
		t.Errorf("Role mismatch: got %s, want %s", unmarshaled.Role, agent.Role)
	}
	if unmarshaled.Status != agent.Status {
		t.Errorf("Status mismatch: got %s, want %s", unmarshaled.Status, agent.Status)
	}
	if *unmarshaled.CurrentTask != *agent.CurrentTask {
		t.Errorf("CurrentTask mismatch: got %s, want %s", *unmarshaled.CurrentTask, *agent.CurrentTask)
	}
}

func TestConfigLegacyCoderMaxWaitYAML(t *testing.T) {
	data := []byte("doer_max_wait: 120\ncoder_max_wait: 600\n")

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatalf("Failed to unmarshal config: %v", err)
	}

	if config.DoerMaxWait != 120 {
		t.Errorf("DoerMaxWait = %d, want 120", config.DoerMaxWait)
	}
	if config.DeprecatedCoderMaxWait != 600 {
		t.Errorf("DeprecatedCoderMaxWait = %d, want 600", config.DeprecatedCoderMaxWait)
	}
}

func TestStateYAMLMarshaling(t *testing.T) {
	created, _ := time.Parse(time.RFC3339, "2025-01-17T14:00:00Z")
	state := State{
		Version: 1,
		Goal: Goal{
			ID:          "goal-1",
			Description: "Test goal",
			SpecRef:     "specs/vision.md",
			Created:     created,
			Status:      GoalStatusInProgress,
			AlignmentHistory: []AlignmentHistory{
				{
					Timestamp: created,
					Event:     "initialization",
					Summary:   "Initial setup",
				},
			},
		},
		Tasks: []Task{
			{
				ID:          "task-1",
				Description: "Test task",
				Status:      TaskStatusReady,
				Priority:    1,
				Created:     created,
				SpecRef:     "specs/test.md",
				DoneWhen:    "Tests pass",
				History:     []TaskHistoryEntry{},
			},
		},
		Agents: map[string]Agent{
			"coder-1": {
				Role:      "coder",
				Status:    AgentStatusIdle,
				Heartbeat: created,
			},
		},
		Discovered:  []Discovery{},
		HumanNotes:  []HumanNote{},
		SpecChanges: []SpecChange{},
		Anomalies:   []Anomaly{},
		Sprint: Sprint{
			ID:      "sprint-1",
			GoalRef: "goal-1",
			Status:  SprintStatusInProgress,
		},
		CircuitBreaker: CircuitBreaker{
			Status: "OK",
		},
		Config: Config{
			MaxCoderIterations: 10,
			MaxReviewCycles:    5,
			HeartbeatInterval:  60,
			LeaseDuration:      1800,
			CoderPollInterval:  30,
			DoerMaxWait:        300,
			IntegrationBranch:  "integration",
		},
	}

	// Marshal to YAML
	data, err := yaml.Marshal(&state)
	if err != nil {
		t.Fatalf("Failed to marshal state: %v", err)
	}

	// Unmarshal back
	var unmarshaled State
	err = yaml.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal state: %v", err)
	}

	// Verify top-level fields
	if unmarshaled.Version != state.Version {
		t.Errorf("Version mismatch: got %d, want %d", unmarshaled.Version, state.Version)
	}
	if unmarshaled.Goal.ID != state.Goal.ID {
		t.Errorf("Goal ID mismatch: got %s, want %s", unmarshaled.Goal.ID, state.Goal.ID)
	}
	if len(unmarshaled.Tasks) != len(state.Tasks) {
		t.Errorf("Tasks count mismatch: got %d, want %d", len(unmarshaled.Tasks), len(state.Tasks))
	}
	if len(unmarshaled.Agents) != len(state.Agents) {
		t.Errorf("Agents count mismatch: got %d, want %d", len(unmarshaled.Agents), len(state.Agents))
	}
}

func TestTaskTypeIsValid(t *testing.T) {
	if !TaskTypeCoding.IsValid() {
		t.Error("TaskTypeCoding should be valid")
	}
	if TaskType("unknown").IsValid() {
		t.Error("unknown task type should be invalid")
	}
	if TaskType("").IsValid() {
		t.Error("empty task type should be invalid (not in registry)")
	}
}

func TestRoleConstants(t *testing.T) {
	if RoleCoder != "coder" {
		t.Errorf("RoleCoder = %q, want %q", RoleCoder, "coder")
	}
	if RoleCodeReviewer != "code-reviewer" {
		t.Errorf("RoleCodeReviewer = %q, want %q", RoleCodeReviewer, "code-reviewer")
	}
	if RoleOrchestrator != "orchestrator" {
		t.Errorf("RoleOrchestrator = %q, want %q", RoleOrchestrator, "orchestrator")
	}
}

func TestTaskTypeRoleWorkflow(t *testing.T) {
	workflow := TaskTypeCoding.RoleWorkflow()
	if len(workflow) != 2 {
		t.Fatalf("coding workflow should have 2 roles, got %d", len(workflow))
	}
	if workflow[0] != RoleCoder || workflow[1] != RoleCodeReviewer {
		t.Errorf("coding workflow = %v, want [coder code_reviewer]", workflow)
	}
}

func TestTaskTypeHasRole(t *testing.T) {
	if !TaskTypeCoding.HasRole(RoleCoder) {
		t.Error("coding type should have coder role")
	}
	if !TaskTypeCoding.HasRole(RoleCodeReviewer) {
		t.Error("coding type should have code_reviewer role")
	}
	if TaskTypeCoding.HasRole(RoleOrchestrator) {
		t.Error("coding type should not have orchestrator role")
	}

	// Planning type has code-planner roles, not coder roles
	if !TaskTypePlanning.HasRole(RoleCodePlanner) {
		t.Error("planning type should have code-planner role")
	}
	if !TaskTypePlanning.HasRole(RoleCodePlanReviewer) {
		t.Error("planning type should have code-plan-reviewer role")
	}
	if TaskTypePlanning.HasRole(RoleCoder) {
		t.Error("planning type should not have coder role")
	}
	if TaskTypePlanning.HasRole(RoleCodeReviewer) {
		t.Error("planning type should not have code-reviewer role")
	}
}

func TestTaskTypePlanningIsValid(t *testing.T) {
	if !TaskTypePlanning.IsValid() {
		t.Error("planning should be a valid task type")
	}
	if !TaskTypeCoding.IsValid() {
		t.Error("coding should be a valid task type")
	}
	if TaskType("bogus").IsValid() {
		t.Error("bogus should not be a valid task type")
	}
}

func TestEffectiveType(t *testing.T) {
	// Empty type defaults to coding
	task := Task{Type: ""}
	if task.EffectiveType() != TaskTypeCoding {
		t.Errorf("EffectiveType() for empty = %s, want %s", task.EffectiveType(), TaskTypeCoding)
	}

	// Explicit coding type
	task = Task{Type: TaskTypeCoding}
	if task.EffectiveType() != TaskTypeCoding {
		t.Errorf("EffectiveType() for coding = %s, want %s", task.EffectiveType(), TaskTypeCoding)
	}

	// Explicit planning type
	task = Task{Type: TaskTypePlanning}
	if task.EffectiveType() != TaskTypePlanning {
		t.Errorf("EffectiveType() for planning = %s, want %s", task.EffectiveType(), TaskTypePlanning)
	}
}

func TestTaskTypeBackwardCompat(t *testing.T) {
	// YAML without type field should unmarshal with empty Type
	yamlData := `id: task-1
description: Test task
status: READY
priority: 1
`
	var task Task
	if err := yaml.Unmarshal([]byte(yamlData), &task); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}
	if task.Type != "" {
		t.Errorf("Type should be empty for backward compat, got %q", task.Type)
	}
	if task.EffectiveType() != TaskTypeCoding {
		t.Errorf("EffectiveType() = %s, want %s", task.EffectiveType(), TaskTypeCoding)
	}
}

func TestTaskTypeEpicPlanningIsValid(t *testing.T) {
	if !TaskTypeEpicPlanning.IsValid() {
		t.Error("TaskTypeEpicPlanning should be valid")
	}
}

func TestTaskTypeEpicPlanningHasRoles(t *testing.T) {
	if !TaskTypeEpicPlanning.HasRole(RoleEpicPlanner) {
		t.Error("epic-planning type should have epic-planner role")
	}
	if !TaskTypeEpicPlanning.HasRole(RoleEpicPlanReviewer) {
		t.Error("epic-planning type should have epic-plan-reviewer role")
	}
	if TaskTypeEpicPlanning.HasRole(RoleCoder) {
		t.Error("epic-planning type should not have coder role")
	}
	if TaskTypeEpicPlanning.HasRole(RoleCodeReviewer) {
		t.Error("epic-planning type should not have code-reviewer role")
	}
}

func TestTaskTypeEpicPlanningNotCoding(t *testing.T) {
	if TaskTypeEpicPlanning == TaskTypeCoding {
		t.Error("TaskTypeEpicPlanning must differ from TaskTypeCoding (TDD bypass condition)")
	}
}

func TestTaskTypeUSWritingIsValid(t *testing.T) {
	if !TaskTypeUSWriting.IsValid() {
		t.Error("TaskTypeUSWriting should be valid")
	}
}

func TestTaskTypeUSWritingHasRoles(t *testing.T) {
	if !TaskTypeUSWriting.HasRole(RoleUSWriter) {
		t.Error("us-writing type should have us-writer role")
	}
	if !TaskTypeUSWriting.HasRole(RoleUSReviewer) {
		t.Error("us-writing type should have us-reviewer role")
	}
	if TaskTypeUSWriting.HasRole(RoleCoder) {
		t.Error("us-writing type should not have coder role")
	}
	if TaskTypeUSWriting.HasRole(RoleCodeReviewer) {
		t.Error("us-writing type should not have code-reviewer role")
	}
}

func TestTaskTypeUSWritingNotCoding(t *testing.T) {
	if TaskTypeUSWriting == TaskTypeCoding {
		t.Error("TaskTypeUSWriting must differ from TaskTypeCoding (TDD bypass condition)")
	}
}

func TestTaskTypeIntegrationIsValid(t *testing.T) {
	if !TaskTypeIntegration.IsValid() {
		t.Error("TaskTypeIntegration should be valid")
	}
}

func TestTaskTypeIntegrationHasRoles(t *testing.T) {
	if !TaskTypeIntegration.HasRole("integration-analyst") {
		t.Error("integration type should have integration-analyst role")
	}
	if !TaskTypeIntegration.HasRole("integration-reviewer") {
		t.Error("integration type should have integration-reviewer role")
	}
	// Should not have coding roles
	if TaskTypeIntegration.HasRole(RoleCoder) {
		t.Error("integration type should not have coder role")
	}
	if TaskTypeIntegration.HasRole(RoleCodeReviewer) {
		t.Error("integration type should not have code-reviewer role")
	}
}

func TestTaskTypeIntegrationNotCoding(t *testing.T) {
	// Verifies TDD bypass condition: integration != coding
	if TaskTypeIntegration == TaskTypeCoding {
		t.Error("TaskTypeIntegration must differ from TaskTypeCoding (TDD bypass condition)")
	}
}

func TestTaskTypeArchitectureIsValid(t *testing.T) {
	if !TaskTypeArchitecture.IsValid() {
		t.Error("TaskTypeArchitecture should be valid")
	}
}

func TestTaskTypeArchitectureHasRoles(t *testing.T) {
	if !TaskTypeArchitecture.HasRole(RoleArchitect) {
		t.Error("architecture type should have architect role")
	}
	if !TaskTypeArchitecture.HasRole(RoleArchitectureReviewer) {
		t.Error("architecture type should have architecture-reviewer role")
	}
	if TaskTypeArchitecture.HasRole(RoleCoder) {
		t.Error("architecture type should not have coder role")
	}
	if TaskTypeArchitecture.HasRole(RoleCodeReviewer) {
		t.Error("architecture type should not have code-reviewer role")
	}
}

func TestTaskTypeArchitectureNotCoding(t *testing.T) {
	if TaskTypeArchitecture == TaskTypeCoding {
		t.Error("TaskTypeArchitecture must differ from TaskTypeCoding")
	}
}

func TestEffectiveTypeBackwardCompat(t *testing.T) {
	// Empty type still defaults to coding (not integration)
	task := Task{Type: ""}
	if task.EffectiveType() != TaskTypeCoding {
		t.Errorf("EffectiveType() for empty type = %s, want %s", task.EffectiveType(), TaskTypeCoding)
	}
}

func TestValidTaskTypeNamesIncludesIntegration(t *testing.T) {
	names := ValidTaskTypeNames()
	if !slices.Contains(names, "integration") {
		t.Errorf("ValidTaskTypeNames() = %v, should include 'integration'", names)
	}
}

func TestTaskTypeForRole(t *testing.T) {
	tests := []struct {
		role string
		want TaskType
	}{
		{RoleCoder, TaskTypeCoding},
		{RoleCodeReviewer, TaskTypeCoding},
		{RoleCodePlanner, TaskTypePlanning},
		{RoleCodePlanReviewer, TaskTypePlanning},
		{RoleEpicPlanner, TaskTypeEpicPlanning},
		{RoleEpicPlanReviewer, TaskTypeEpicPlanning},
		{RoleUSWriter, TaskTypeUSWriting},
		{RoleUSReviewer, TaskTypeUSWriting},
		{RoleIntegrationAnalyst, TaskTypeIntegration},
		{RoleIntegrationReviewer, TaskTypeIntegration},
		{RoleArchitect, TaskTypeArchitecture},
		{RoleArchitectureReviewer, TaskTypeArchitecture},
		{"unknown-role", TaskTypeCoding}, // fallback
	}
	for _, tt := range tests {
		got := TaskTypeForRole(tt.role)
		if got != tt.want {
			t.Errorf("TaskTypeForRole(%q) = %q, want %q", tt.role, got, tt.want)
		}
	}
}

func TestIsClaimableWithRole(t *testing.T) {
	pr := &mockPipelineResolver{
		doer:      "coder",         // runtime form
		reviewer:  "code-reviewer", // runtime form
		initial:   TaskStatusReady,
		rejected:  TaskStatusRejected,
		submitted: TaskStatusReadyForReview,
		reviewing: TaskStatusReviewing,
		executing: TaskStatusImplementing,
		approved:  TaskStatusApproved,
	}

	tests := []struct {
		name      string
		task      Task
		role      string
		claimable bool
	}{
		{
			name:      "coder can claim READY coding task",
			task:      Task{Status: TaskStatusReady, Type: TaskTypeCoding, RolePair: "coding-pair"},
			role:      RoleCoder,
			claimable: true,
		},
		{
			name:      "coder can claim REJECTED coding task",
			task:      Task{Status: TaskStatusRejected, Type: TaskTypeCoding, RolePair: "coding-pair"},
			role:      RoleCoder,
			claimable: true,
		},
		{
			name:      "coder cannot claim READY_FOR_REVIEW task",
			task:      Task{Status: TaskStatusReadyForReview, Type: TaskTypeCoding, RolePair: "coding-pair"},
			role:      RoleCoder,
			claimable: false,
		},
		{
			name:      "code_reviewer can claim READY_FOR_REVIEW coding task",
			task:      Task{Status: TaskStatusReadyForReview, Type: TaskTypeCoding, RolePair: "coding-pair", ReviewCommit: strPtr("abc123")},
			role:      "code-reviewer", // runtime form — matches pipeline resolver
			claimable: true,
		},
		{
			name:      "code_reviewer cannot claim READY task",
			task:      Task{Status: TaskStatusReady, Type: TaskTypeCoding, RolePair: "coding-pair"},
			role:      "code-reviewer", // runtime form
			claimable: false,
		},
		{
			name:      "orchestrator cannot claim any task",
			task:      Task{Status: TaskStatusReady, Type: TaskTypeCoding, RolePair: "coding-pair"},
			role:      "orchestrator", // runtime form
			claimable: false,
		},
		{
			name:      "unknown type is not claimable",
			task:      Task{Status: TaskStatusReady, Type: TaskType("unknown")},
			role:      RoleCoder,
			claimable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.task.IsClaimable(tt.role, nil, pr)
			if result != tt.claimable {
				t.Errorf("IsClaimable(%q) = %v, want %v", tt.role, result, tt.claimable)
			}
		})
	}
}

func TestTaskClaimability(t *testing.T) {
	pr := &mockPipelineResolver{
		doer:      "coder",         // runtime form
		reviewer:  "code-reviewer", // runtime form
		initial:   TaskStatusReady,
		rejected:  TaskStatusRejected,
		submitted: TaskStatusReadyForReview,
		reviewing: TaskStatusReviewing,
		executing: TaskStatusImplementing,
		approved:  TaskStatusApproved,
	}

	tests := []struct {
		name      string
		task      Task
		claimable bool
	}{
		{
			name: "READY with no dependencies",
			task: Task{
				Status:    TaskStatusReady,
				RolePair:  "coding-pair",
				DependsOn: []string{},
			},
			claimable: true,
		},
		{
			name: "READY with nil dependencies",
			task: Task{
				Status:   TaskStatusReady,
				RolePair: "coding-pair",
			},
			claimable: true,
		},
		{
			name: "REJECTED is claimable",
			task: Task{
				Status:    TaskStatusRejected,
				RolePair:  "coding-pair",
				DependsOn: []string{},
			},
			claimable: true,
		},
		{
			name: "INTEGRATION_FAILED is claimable",
			task: Task{
				Status:    TaskStatusIntegrationFailed,
				RolePair:  "coding-pair",
				DependsOn: []string{},
			},
			claimable: true,
		},
		{
			name: "DRAFT is not claimable",
			task: Task{
				Status:   TaskStatusDraft,
				RolePair: "coding-pair",
			},
			claimable: false,
		},
		{
			name: "IMPLEMENTING is not claimable",
			task: Task{
				Status:   TaskStatusImplementing,
				RolePair: "coding-pair",
			},
			claimable: false,
		},
		{
			name: "MERGED is not claimable",
			task: Task{
				Status:   TaskStatusMerged,
				RolePair: "coding-pair",
			},
			claimable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.task.IsClaimable(RoleCoder, nil, pr) // nil allTasks means all dependencies are satisfied
			if result != tt.claimable {
				t.Errorf("IsClaimable() = %v, want %v", result, tt.claimable)
			}
		})
	}
}

func TestTaskClaimabilityWithDependencies(t *testing.T) {
	pr := &mockPipelineResolver{
		doer:      "coder",         // runtime form
		reviewer:  "code-reviewer", // runtime form
		initial:   TaskStatusReady,
		rejected:  TaskStatusRejected,
		submitted: TaskStatusReadyForReview,
		reviewing: TaskStatusReviewing,
		executing: TaskStatusImplementing,
		approved:  TaskStatusApproved,
	}

	// Mock function that checks if dependencies are satisfied
	allTasks := []Task{
		{ID: "task-1", Status: TaskStatusMerged, RolePair: "coding-pair"},
		{ID: "task-2", Status: TaskStatusReady, RolePair: "coding-pair"},
		{ID: "task-3", Status: TaskStatusMerged, RolePair: "coding-pair"},
	}

	tests := []struct {
		name      string
		task      Task
		claimable bool
	}{
		{
			name: "READY with all dependencies MERGED",
			task: Task{
				ID:        "task-4",
				Status:    TaskStatusReady,
				RolePair:  "coding-pair",
				DependsOn: []string{"task-1", "task-3"},
			},
			claimable: true,
		},
		{
			name: "READY with unmet dependencies",
			task: Task{
				ID:        "task-5",
				Status:    TaskStatusReady,
				RolePair:  "coding-pair",
				DependsOn: []string{"task-1", "task-2"}, // task-2 is READY
			},
			claimable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.task.IsClaimable(RoleCoder, allTasks, pr)
			if result != tt.claimable {
				t.Errorf("IsClaimable() = %v, want %v", result, tt.claimable)
			}
		})
	}
}

func TestDiscoverySeverity(t *testing.T) {
	validSeverities := []DiscoverySeverity{"critical", "high", "medium", "low"}
	for _, sev := range validSeverities {
		d := Discovery{Severity: sev}
		if !d.IsValidSeverity() {
			t.Errorf("Severity %s should be valid", sev)
		}
	}

	d := Discovery{Severity: "invalid"}
	if d.IsValidSeverity() {
		t.Error("Invalid severity should not be valid")
	}
}

func TestDiscoveryUrgency(t *testing.T) {
	validUrgencies := []DiscoveryUrgency{"immediate", "deferred"}
	for _, urg := range validUrgencies {
		d := Discovery{Urgency: urg}
		if !d.IsValidUrgency() {
			t.Errorf("Urgency %s should be valid", urg)
		}
	}

	d := Discovery{Urgency: "invalid"}
	if d.IsValidUrgency() {
		t.Error("Invalid urgency should not be valid")
	}
}

func TestAnomalyTypes(t *testing.T) {
	validTypes := []AnomalyType{
		"retry_loop",
		"trade_off",
		"spec_ambiguity",
		"external_blocker",
		"assumption_violated",
		"scope_deviation",
		"workaround",
		"debt_created",
		"spec_changed",
		"hypothesis_exhaustion",
		"spec_gap",
		"review_budget_exhausted",
		"review_exhaustion",
		"reviewer_loop",
		"stale_verdict",
		"system_ambiguity",
		"provider_audit_degraded",
		"agent_degraded",
		"submit_verdict_failed",
	}

	for _, typ := range validTypes {
		a := Anomaly{Type: typ}
		if !a.IsValidType() {
			t.Errorf("Anomaly type %s should be valid", typ)
		}
	}

	a := Anomaly{Type: "invalid"}
	if a.IsValidType() {
		t.Error("Invalid anomaly type should not be valid")
	}
}

func TestCircuitBreakerStatus(t *testing.T) {
	validStatuses := []string{"OK", "TRIGGERED"}
	for _, status := range validStatuses {
		cb := CircuitBreaker{Status: status}
		if !cb.IsValidStatus() {
			t.Errorf("CircuitBreaker status %s should be valid", status)
		}
	}

	cb := CircuitBreaker{Status: "invalid"}
	if cb.IsValidStatus() {
		t.Error("Invalid circuit breaker status should not be valid")
	}
}

func TestCircuitBreakerTriggerYAML(t *testing.T) {
	now := time.Date(2025, 1, 18, 17, 30, 0, 0, time.UTC)

	trigger := CircuitBreakerTrigger{
		Timestamp:  now,
		Pattern:    "retry_cluster",
		Severity:   "ARCHITECTURE_FLAW",
		ReportFile: paths.ProjectDirName() + "/circuit_breaker_report.md",
	}

	yamlData, err := yaml.Marshal(&trigger)
	if err != nil {
		t.Fatalf("Failed to marshal CircuitBreakerTrigger: %v", err)
	}

	var unmarshaledTrigger CircuitBreakerTrigger
	err = yaml.Unmarshal(yamlData, &unmarshaledTrigger)
	if err != nil {
		t.Fatalf("Failed to unmarshal CircuitBreakerTrigger: %v", err)
	}

	if unmarshaledTrigger.Pattern != trigger.Pattern {
		t.Errorf("Pattern mismatch: got %s, want %s", unmarshaledTrigger.Pattern, trigger.Pattern)
	}

	if unmarshaledTrigger.Severity != trigger.Severity {
		t.Errorf("Severity mismatch: got %s, want %s", unmarshaledTrigger.Severity, trigger.Severity)
	}

	if unmarshaledTrigger.ReportFile != trigger.ReportFile {
		t.Errorf("ReportFile mismatch: got %s, want %s", unmarshaledTrigger.ReportFile, trigger.ReportFile)
	}
}

func TestCircuitBreakerHistoryYAML(t *testing.T) {
	now := time.Date(2025, 1, 18, 17, 30, 0, 0, time.UTC)
	resolvedAt := time.Date(2025, 1, 18, 19, 0, 0, 0, time.UTC)

	pattern := "retry_cluster"
	severity := "ARCHITECTURE_FLAW"
	resolution := "ADR-003 created, specs updated"

	history := CircuitBreakerHistory{
		Timestamp:  now,
		Pattern:    &pattern,
		Severity:   &severity,
		Result:     "TRIGGERED",
		Resolution: &resolution,
		ResolvedAt: &resolvedAt,
	}

	yamlData, err := yaml.Marshal(&history)
	if err != nil {
		t.Fatalf("Failed to marshal CircuitBreakerHistory: %v", err)
	}

	var unmarshaledHistory CircuitBreakerHistory
	err = yaml.Unmarshal(yamlData, &unmarshaledHistory)
	if err != nil {
		t.Fatalf("Failed to unmarshal CircuitBreakerHistory: %v", err)
	}

	if *unmarshaledHistory.Pattern != *history.Pattern {
		t.Errorf("Pattern mismatch: got %s, want %s", *unmarshaledHistory.Pattern, *history.Pattern)
	}

	if *unmarshaledHistory.Severity != *history.Severity {
		t.Errorf("Severity mismatch: got %s, want %s", *unmarshaledHistory.Severity, *history.Severity)
	}

	if unmarshaledHistory.Result != history.Result {
		t.Errorf("Result mismatch: got %s, want %s", unmarshaledHistory.Result, history.Result)
	}

	if *unmarshaledHistory.Resolution != *history.Resolution {
		t.Errorf("Resolution mismatch: got %s, want %s", *unmarshaledHistory.Resolution, *history.Resolution)
	}
}

func TestSprintStalled(t *testing.T) {
	now := time.Now().UTC()
	mkTask := func(id string, status TaskStatus) Task {
		return Task{ID: id, Status: status, Created: now, Priority: 1, Iteration: 1}
	}

	tests := []struct {
		name    string
		planned []string
		tasks   []Task
		want    bool
	}{
		{
			name:    "empty planned list",
			planned: []string{},
			tasks:   []Task{mkTask("task-1", TaskStatusBlocked)},
			want:    false,
		},
		{
			name:    "all terminal — complete not stalled",
			planned: []string{"task-1", "task-2"},
			tasks:   []Task{mkTask("task-1", TaskStatusMerged), mkTask("task-2", TaskStatusAbandoned)},
			want:    false,
		},
		{
			name:    "all blocked",
			planned: []string{"task-1", "task-2"},
			tasks:   []Task{mkTask("task-1", TaskStatusBlocked), mkTask("task-2", TaskStatusBlocked)},
			want:    true,
		},
		{
			name:    "mix of terminal and blocked",
			planned: []string{"task-1", "task-2", "task-3"},
			tasks: []Task{
				mkTask("task-1", TaskStatusMerged),
				mkTask("task-2", TaskStatusBlocked),
				mkTask("task-3", TaskStatusSuperseded),
			},
			want: true,
		},
		{
			name:    "blocked plus in-progress — not stalled",
			planned: []string{"task-1", "task-2"},
			tasks:   []Task{mkTask("task-1", TaskStatusBlocked), mkTask("task-2", TaskStatusImplementing)},
			want:    false,
		},
		{
			name:    "single blocked single ready — not stalled",
			planned: []string{"task-1", "task-2"},
			tasks:   []Task{mkTask("task-1", TaskStatusBlocked), mkTask("task-2", TaskStatusReady)},
			want:    false,
		},
		{
			name:    "planned task missing from task list",
			planned: []string{"task-1", "task-2"},
			tasks:   []Task{mkTask("task-1", TaskStatusBlocked)},
			want:    false,
		},
		{
			name:    "extra non-planned tasks dont affect result",
			planned: []string{"task-1"},
			tasks: []Task{
				mkTask("task-1", TaskStatusBlocked),
				mkTask("task-2", TaskStatusImplementing),
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &State{
				Sprint: Sprint{Scope: SprintScope{Planned: tt.planned}},
				Tasks:  tt.tasks,
			}
			got := state.SprintStalled()
			if got != tt.want {
				t.Errorf("SprintStalled() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Helper function for creating string pointers
func strPtr(s string) *string {
	return &s
}

func TestAgentPIDMarshaling(t *testing.T) {
	heartbeat, _ := time.Parse(time.RFC3339, "2025-01-17T14:50:00Z")

	tests := []struct {
		name      string
		agent     Agent
		checkPID  bool
		wantPID   int
		hasPIDTag bool
	}{
		{
			name: "Agent with PID set",
			agent: Agent{
				Role:      "coder",
				Status:    AgentStatusIdle,
				Heartbeat: heartbeat,
				Terminal:  "/dev/pts/1",
				PID:       12345,
			},
			checkPID:  true,
			wantPID:   12345,
			hasPIDTag: true,
		},
		{
			name: "Agent with PID zero (not set)",
			agent: Agent{
				Role:      "coder",
				Status:    AgentStatusIdle,
				Heartbeat: heartbeat,
				Terminal:  "/dev/pts/1",
				PID:       0,
			},
			checkPID:  true,
			wantPID:   0,
			hasPIDTag: false, // omitempty should exclude it
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal to YAML
			data, err := yaml.Marshal(&tt.agent)
			if err != nil {
				t.Fatalf("Failed to marshal agent: %v", err)
			}

			// Check if PID appears in YAML when expected
			yamlStr := string(data)
			hasPID := containsField(yamlStr, "pid:")
			if hasPID != tt.hasPIDTag {
				t.Errorf("PID in YAML = %v, want %v\nYAML:\n%s", hasPID, tt.hasPIDTag, yamlStr)
			}

			// Unmarshal back
			var unmarshaled Agent
			err = yaml.Unmarshal(data, &unmarshaled)
			if err != nil {
				t.Fatalf("Failed to unmarshal agent: %v", err)
			}

			// Verify PID field
			if tt.checkPID && unmarshaled.PID != tt.wantPID {
				t.Errorf("PID mismatch: got %d, want %d", unmarshaled.PID, tt.wantPID)
			}
		})
	}
}

func TestAgentBackwardCompatibility(t *testing.T) {
	// YAML without PID field (backward compatibility test)
	yamlData := `role: coder
status: IDLE
heartbeat: 2025-01-17T14:50:00Z
terminal: /dev/pts/1
iterations_total: 0
context_percent: 0
`

	var agent Agent
	err := yaml.Unmarshal([]byte(yamlData), &agent)
	if err != nil {
		t.Fatalf("Failed to unmarshal agent without PID: %v", err)
	}

	// PID should be zero (default value)
	if agent.PID != 0 {
		t.Errorf("PID should be 0 for backward compatibility, got %d", agent.PID)
	}

	// Verify other fields were parsed correctly
	if agent.Role != "coder" {
		t.Errorf("Role mismatch: got %s, want coder", agent.Role)
	}
	if agent.Status != AgentStatusIdle {
		t.Errorf("Status mismatch: got %s, want IDLE", agent.Status)
	}
}

// Helper function to check if a YAML string contains a field
func containsField(yamlStr, field string) bool {
	// Simple check - look for the field name followed by a colon
	for i := 0; i < len(yamlStr)-len(field); i++ {
		if yamlStr[i:i+len(field)] == field {
			return true
		}
	}
	return false
}

func TestFindTask(t *testing.T) {
	now := time.Now().UTC()
	mkTask := func(id string) Task {
		return Task{ID: id, Status: TaskStatusReady, Created: now, Priority: 1, Iteration: 1}
	}

	state := &State{
		Tasks: []Task{mkTask("task-1"), mkTask("task-2"), mkTask("task-3")},
	}

	tests := []struct {
		name   string
		taskID string
		wantID string // empty means expect nil
	}{
		{"found first", "task-1", "task-1"},
		{"found middle", "task-2", "task-2"},
		{"found last", "task-3", "task-3"},
		{"not found", "task-99", ""},
		{"empty ID", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := state.FindTask(tt.taskID)
			if tt.wantID == "" {
				if got != nil {
					t.Errorf("FindTask(%q) = %v, want nil", tt.taskID, got)
				}
			} else {
				if got == nil {
					t.Fatalf("FindTask(%q) = nil, want task %q", tt.taskID, tt.wantID)
				}
				if got.ID != tt.wantID {
					t.Errorf("FindTask(%q).ID = %q, want %q", tt.taskID, got.ID, tt.wantID)
				}
			}
		})
	}

	// Verify returned pointer refers to slice element (mutations apply)
	t.Run("returned pointer is mutable", func(t *testing.T) {
		task := state.FindTask("task-1")
		task.Status = TaskStatusImplementing
		if state.Tasks[0].Status != TaskStatusImplementing {
			t.Error("mutation via FindTask pointer did not apply to slice element")
		}
		state.Tasks[0].Status = TaskStatusReady // restore
	})

	// Empty state
	t.Run("empty state", func(t *testing.T) {
		empty := &State{}
		if got := empty.FindTask("task-1"); got != nil {
			t.Errorf("FindTask on empty state = %v, want nil", got)
		}
	})
}

func TestFindTaskIndex(t *testing.T) {
	now := time.Now().UTC()
	mkTask := func(id string) Task {
		return Task{ID: id, Status: TaskStatusReady, Created: now, Priority: 1, Iteration: 1}
	}

	state := &State{
		Tasks: []Task{mkTask("task-1"), mkTask("task-2"), mkTask("task-3")},
	}

	tests := []struct {
		name   string
		taskID string
		want   int
	}{
		{"found first", "task-1", 0},
		{"found middle", "task-2", 1},
		{"found last", "task-3", 2},
		{"not found", "task-99", -1},
		{"empty ID", "", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := state.FindTaskIndex(tt.taskID)
			if got != tt.want {
				t.Errorf("FindTaskIndex(%q) = %d, want %d", tt.taskID, got, tt.want)
			}
		})
	}

	// Empty state
	t.Run("empty state", func(t *testing.T) {
		empty := &State{}
		if got := empty.FindTaskIndex("task-1"); got != -1 {
			t.Errorf("FindTaskIndex on empty state = %d, want -1", got)
		}
	})
}

func TestCodingPlanApprovedIsNotSprintTerminal(t *testing.T) {
	// CODING_PLAN_APPROVED is no longer sprint-terminal — MERGED is the universal terminal.
	if TaskStatusCodingPlanApproved.IsSprintTerminal() {
		t.Error("CODING_PLAN_APPROVED should NOT be sprint-terminal (MERGED is)")
	}
	if TaskStatusCodingPlanApproved.IsTerminal() {
		t.Error("CODING_PLAN_APPROVED should NOT be globally terminal")
	}
}

func TestIsPipelineValid(t *testing.T) {
	// Simulate pipeline-declared states (e.g., from a coding-pair config)
	pipelineDeclared := []TaskStatus{
		TaskStatus("DRAFT_CODE"),
		TaskStatus("IMPLEMENTING_CODE"),
		TaskStatus("CODE_READY_FOR_REVIEW"),
		TaskStatus("REVIEWING_CODE"),
		TaskStatus("CODE_APPROVED"),
		TaskStatus("CODE_REJECTED"),
	}

	tests := []struct {
		name   string
		status TaskStatus
		want   bool
	}{
		// Hardcoded statuses are always valid
		{"hardcoded DRAFT", TaskStatusDraft, true},
		{"hardcoded MERGED", TaskStatusMerged, true},
		{"hardcoded BLOCKED", TaskStatusBlocked, true},
		{"hardcoded IMPLEMENTING", TaskStatusImplementing, true},

		// Pipeline-declared statuses
		{"pipeline DRAFT_CODE", TaskStatus("DRAFT_CODE"), true},
		{"pipeline IMPLEMENTING_CODE", TaskStatus("IMPLEMENTING_CODE"), true},
		{"pipeline CODE_APPROVED", TaskStatus("CODE_APPROVED"), true},

		// Unknown status not in either set
		{"unknown FOOBAR", TaskStatus("FOOBAR"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.status.IsPipelineValid(pipelineDeclared)
			if got != tt.want {
				t.Errorf("IsPipelineValid(%s) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestCanPipelineTransition(t *testing.T) {
	// Simulate a pipeline transition map
	transitions := map[TaskStatus][]TaskStatus{
		TaskStatus("DRAFT_CODE"):            {TaskStatus("IMPLEMENTING_CODE"), TaskStatusAbandoned},
		TaskStatus("IMPLEMENTING_CODE"):     {TaskStatus("CODE_READY_FOR_REVIEW"), TaskStatusBlocked, TaskStatus("DRAFT_CODE")},
		TaskStatus("CODE_READY_FOR_REVIEW"): {TaskStatus("REVIEWING_CODE")},
		TaskStatus("REVIEWING_CODE"):        {TaskStatus("CODE_APPROVED"), TaskStatus("CODE_REJECTED")},
		TaskStatus("CODE_APPROVED"):         {TaskStatusMerged, TaskStatusIntegrationFailed},
		TaskStatus("CODE_REJECTED"):         {TaskStatus("DRAFT_CODE")},
		TaskStatusMerged:                    {},
	}

	tests := []struct {
		from TaskStatus
		to   TaskStatus
		want bool
	}{
		// Valid pipeline transitions
		{TaskStatus("DRAFT_CODE"), TaskStatus("IMPLEMENTING_CODE"), true},
		{TaskStatus("DRAFT_CODE"), TaskStatusAbandoned, true},
		{TaskStatus("IMPLEMENTING_CODE"), TaskStatus("CODE_READY_FOR_REVIEW"), true},
		{TaskStatus("REVIEWING_CODE"), TaskStatus("CODE_APPROVED"), true},
		{TaskStatus("CODE_APPROVED"), TaskStatusMerged, true},
		{TaskStatus("CODE_REJECTED"), TaskStatus("DRAFT_CODE"), true},

		// Invalid pipeline transitions
		{TaskStatus("DRAFT_CODE"), TaskStatus("CODE_APPROVED"), false},
		{TaskStatus("IMPLEMENTING_CODE"), TaskStatusMerged, false},
		{TaskStatusMerged, TaskStatus("DRAFT_CODE"), false},

		// Status not in transition map
		{TaskStatus("UNKNOWN"), TaskStatus("DRAFT_CODE"), false},
	}

	for _, tt := range tests {
		name := string(tt.from) + "→" + string(tt.to)
		t.Run(name, func(t *testing.T) {
			got := tt.from.CanPipelineTransition(tt.to, transitions)
			if got != tt.want {
				t.Errorf("CanPipelineTransition(%s, %s) = %v, want %v", tt.from, tt.to, got, tt.want)
			}
		})
	}
}

func TestTransitionWith(t *testing.T) {
	transitions := map[TaskStatus][]TaskStatus{
		TaskStatus("DRAFT_CODE"):            {TaskStatus("IMPLEMENTING_CODE"), TaskStatusAbandoned},
		TaskStatus("IMPLEMENTING_CODE"):     {TaskStatus("CODE_READY_FOR_REVIEW"), TaskStatusBlocked},
		TaskStatusBlocked:                   {TaskStatusAbandoned},
		TaskStatusAbandoned:                 {},
		TaskStatus("CODE_READY_FOR_REVIEW"): {},
	}

	t.Run("valid transition to declared target", func(t *testing.T) {
		task := Task{ID: "t1", Status: TaskStatus("DRAFT_CODE")}
		err := task.TransitionWith(TaskStatus("IMPLEMENTING_CODE"), transitions)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if task.Status != TaskStatus("IMPLEMENTING_CODE") {
			t.Errorf("status = %s, want IMPLEMENTING_CODE", task.Status)
		}
	})

	t.Run("invalid edge rejected", func(t *testing.T) {
		task := Task{ID: "t2", Status: TaskStatus("DRAFT_CODE")}
		err := task.TransitionWith(TaskStatus("CODE_READY_FOR_REVIEW"), transitions)
		if err == nil {
			t.Fatal("expected error for invalid transition edge")
		}
		if task.Status != TaskStatus("DRAFT_CODE") {
			t.Errorf("status should be unchanged, got %s", task.Status)
		}
	})

	t.Run("valid edge to undeclared target rejected", func(t *testing.T) {
		// DRAFT_CODE → ABANDONED is a valid edge, but if ABANDONED were missing
		// as a key in the map, it should be rejected. Simulate with a map that
		// has the edge but not the target key.
		incomplete := map[TaskStatus][]TaskStatus{
			TaskStatus("DRAFT_CODE"): {TaskStatus("GHOST_STATUS")},
		}
		task := Task{ID: "t3", Status: TaskStatus("DRAFT_CODE")}
		err := task.TransitionWith(TaskStatus("GHOST_STATUS"), incomplete)
		if err == nil {
			t.Fatal("expected error for undeclared target status")
		}
		if task.Status != TaskStatus("DRAFT_CODE") {
			t.Errorf("status should be unchanged, got %s", task.Status)
		}
	})
}

func TestRolePairField(t *testing.T) {
	task := Task{
		ID:       "task-1",
		Status:   TaskStatusDraftCodingPlan,
		RolePair: "code-planning-pair",
	}

	data, err := yaml.Marshal(&task)
	if err != nil {
		t.Fatalf("Failed to marshal task with role_pair: %v", err)
	}

	yamlStr := string(data)
	if !containsField(yamlStr, "role_pair:") {
		t.Error("YAML output should contain role_pair field")
	}

	var unmarshaled Task
	err = yaml.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal task with role_pair: %v", err)
	}
	if unmarshaled.RolePair != "code-planning-pair" {
		t.Errorf("RolePair = %q, want %q", unmarshaled.RolePair, "code-planning-pair")
	}
}

func TestRolePairFieldOmittedWhenEmpty(t *testing.T) {
	task := Task{
		ID:     "task-1",
		Status: TaskStatusDraft,
	}

	data, err := yaml.Marshal(&task)
	if err != nil {
		t.Fatalf("Failed to marshal task: %v", err)
	}

	if containsField(string(data), "role_pair:") {
		t.Error("role_pair should be omitted when empty")
	}
}

func TestRoleConstants_CodePlanning(t *testing.T) {
	if RoleCodePlanner != "code-planner" {
		t.Errorf("RoleCodePlanner = %q, want %q", RoleCodePlanner, "code-planner")
	}
	if RoleCodePlanReviewer != "code-plan-reviewer" {
		t.Errorf("RoleCodePlanReviewer = %q, want %q", RoleCodePlanReviewer, "code-plan-reviewer")
	}
}

func TestIsClaimable_CodePlanningRoles(t *testing.T) {
	pr := &mockPipelineResolver{
		doer:      "code-planner",       // runtime form
		reviewer:  "code-plan-reviewer", // runtime form
		initial:   TaskStatusDraftCodingPlan,
		rejected:  TaskStatusCodingPlanRejected,
		submitted: TaskStatusCodingPlanToReview,
		reviewing: TaskStatusReviewingCodingPlan,
		executing: TaskStatusCodePlanning,
		approved:  TaskStatusCodingPlanApproved,
	}

	tests := []struct {
		name      string
		task      Task
		role      string
		claimable bool
	}{
		{
			name:      "code_planner can claim DRAFT_CODING_PLAN",
			task:      Task{Status: TaskStatusDraftCodingPlan, RolePair: "code-planning-pair"},
			role:      "code-planner", // runtime form — matches pipeline resolver
			claimable: true,
		},
		{
			name:      "code_planner can claim CODING_PLAN_REJECTED",
			task:      Task{Status: TaskStatusCodingPlanRejected, RolePair: "code-planning-pair"},
			role:      "code-planner", // runtime form
			claimable: true,
		},
		{
			name:      "code_plan_reviewer can claim CODING_PLAN_TO_REVIEW",
			task:      Task{Status: TaskStatusCodingPlanToReview, RolePair: "code-planning-pair", ReviewCommit: strPtr("abc123")},
			role:      "code-plan-reviewer", // runtime form
			claimable: true,
		},
		{
			name:      "code_plan_reviewer cannot claim DRAFT_CODING_PLAN",
			task:      Task{Status: TaskStatusDraftCodingPlan, RolePair: "code-planning-pair"},
			role:      "code-plan-reviewer", // runtime form
			claimable: false,
		},
		{
			name:      "code_planner cannot claim CODING_PLAN_TO_REVIEW",
			task:      Task{Status: TaskStatusCodingPlanToReview, RolePair: "code-planning-pair"},
			role:      "code-planner", // runtime form
			claimable: false,
		},
		{
			name:      "coder cannot claim DRAFT_CODING_PLAN",
			task:      Task{Status: TaskStatusDraftCodingPlan, RolePair: "code-planning-pair"},
			role:      "coder", // runtime form
			claimable: false,
		},
		{
			name:      "code_reviewer cannot claim CODING_PLAN_TO_REVIEW",
			task:      Task{Status: TaskStatusCodingPlanToReview, RolePair: "code-planning-pair"},
			role:      "code-reviewer", // runtime form
			claimable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.task.IsClaimable(tt.role, nil, pr)
			if result != tt.claimable {
				t.Errorf("IsClaimable(%q) = %v, want %v", tt.role, result, tt.claimable)
			}
		})
	}
}

// mockPipelineResolver implements PipelineResolver for testing.
type mockPipelineResolver struct {
	doer              string
	reviewer          string
	initial           TaskStatus
	rejected          TaskStatus
	submitted         TaskStatus
	reviewing         TaskStatus
	executing         TaskStatus
	approved          TaskStatus
	partiallyApproved TaskStatus
	reviewing2        TaskStatus
}

func (m *mockPipelineResolver) DoerRole(string) (string, error)     { return m.doer, nil }
func (m *mockPipelineResolver) ReviewerRole(string) (string, error) { return m.reviewer, nil }
func (m *mockPipelineResolver) RoleType(role string) (roles.RoleType, error) {
	switch role {
	case m.doer:
		return roles.RoleTypeDoer, nil
	case m.reviewer:
		return roles.RoleTypeReviewer, nil
	default:
		return "", fmt.Errorf("unknown role %q", role)
	}
}
func (m *mockPipelineResolver) AllRoleNames() []string                     { return []string{m.doer, m.reviewer} }
func (m *mockPipelineResolver) InitialStatus(string) (TaskStatus, error)   { return m.initial, nil }
func (m *mockPipelineResolver) RejectedStatus(string) (TaskStatus, error)  { return m.rejected, nil }
func (m *mockPipelineResolver) SubmittedStatus(string) (TaskStatus, error) { return m.submitted, nil }
func (m *mockPipelineResolver) ReviewingStatus(string) (TaskStatus, error) { return m.reviewing, nil }
func (m *mockPipelineResolver) ExecutingStatus(string) (TaskStatus, error) { return m.executing, nil }
func (m *mockPipelineResolver) ApprovedStatus(string) (TaskStatus, error)  { return m.approved, nil }
func (m *mockPipelineResolver) PartiallyApprovedStatus(string) (TaskStatus, error) {
	if m.partiallyApproved == "" {
		return "", fmt.Errorf("no partially-approved state declared")
	}
	return m.partiallyApproved, nil
}
func (m *mockPipelineResolver) Reviewing2Status(string) (TaskStatus, error) {
	if m.reviewing2 == "" {
		return "", fmt.Errorf("no reviewing-2 state declared")
	}
	return m.reviewing2, nil
}

func TestIsClaimable_Pipeline(t *testing.T) {
	pr := &mockPipelineResolver{
		doer:      "coder",         // runtime form
		reviewer:  "code-reviewer", // runtime form
		initial:   "DRAFT_CODE",
		rejected:  "CODE_REJECTED",
		submitted: "CODE_READY_FOR_REVIEW",
		reviewing: "REVIEWING_CODE",
		executing: "IMPLEMENTING_CODE",
	}

	tests := []struct {
		name      string
		task      Task
		role      string // runtime form (hyphenated)
		claimable bool
	}{
		{
			name:      "coder can claim pipeline initial status",
			task:      Task{Status: "DRAFT_CODE", RolePair: "coding-pair"},
			role:      "coder",
			claimable: true,
		},
		{
			name:      "coder can claim pipeline rejected status",
			task:      Task{Status: "CODE_REJECTED", RolePair: "coding-pair"},
			role:      "coder",
			claimable: true,
		},
		{
			name:      "coder can claim INTEGRATION_FAILED pipeline task",
			task:      Task{Status: TaskStatusIntegrationFailed, RolePair: "coding-pair"},
			role:      "coder",
			claimable: true,
		},
		{
			name:      "coder cannot claim pipeline submitted status",
			task:      Task{Status: "CODE_READY_FOR_REVIEW", RolePair: "coding-pair"},
			role:      "coder",
			claimable: false,
		},
		{
			name:      "reviewer can claim pipeline submitted status",
			task:      Task{Status: "CODE_READY_FOR_REVIEW", RolePair: "coding-pair", ReviewCommit: strPtr("abc123")},
			role:      "code-reviewer",
			claimable: true,
		},
		{
			name:      "reviewer cannot claim pipeline initial status",
			task:      Task{Status: "DRAFT_CODE", RolePair: "coding-pair"},
			role:      "code-reviewer",
			claimable: false,
		},
		{
			name:      "orchestrator cannot claim pipeline task",
			task:      Task{Status: "DRAFT_CODE", RolePair: "coding-pair"},
			role:      "orchestrator",
			claimable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.task.IsClaimable(tt.role, nil, pr)
			if result != tt.claimable {
				t.Errorf("IsClaimable(%q) = %v, want %v", tt.role, result, tt.claimable)
			}
		})
	}
}

func TestIsClaimable_Pipeline_WithDependencies(t *testing.T) {
	pr := &mockPipelineResolver{
		doer:      "coder",
		reviewer:  "code-reviewer",
		initial:   "DRAFT_CODE",
		rejected:  "CODE_REJECTED",
		submitted: "CODE_READY_FOR_REVIEW",
		reviewing: "REVIEWING_CODE",
		executing: "IMPLEMENTING_CODE",
	}

	allTasks := []Task{
		{ID: "dep-1", Status: TaskStatusMerged},
		{ID: "dep-2", Status: TaskStatusImplementing},
	}

	t.Run("pipeline task with satisfied deps is claimable", func(t *testing.T) {
		task := Task{Status: "DRAFT_CODE", RolePair: "coding-pair", DependsOn: []string{"dep-1"}}
		if !task.IsClaimable(RoleCoder, allTasks, pr) {
			t.Error("expected claimable with satisfied deps")
		}
	})

	t.Run("pipeline task with unsatisfied deps is not claimable", func(t *testing.T) {
		task := Task{Status: "DRAFT_CODE", RolePair: "coding-pair", DependsOn: []string{"dep-2"}}
		if task.IsClaimable(RoleCoder, allTasks, pr) {
			t.Error("expected not claimable with unsatisfied deps")
		}
	})
}

func TestAllPlannedTasksTerminal_WithCodingPlanApproved(t *testing.T) {
	now := time.Now().UTC()
	mkTask := func(id string, status TaskStatus) Task {
		return Task{ID: id, Status: status, Created: now, Priority: 1, Iteration: 1}
	}

	tests := []struct {
		name    string
		planned []string
		tasks   []Task
		want    bool
	}{
		{
			name:    "CODING_PLAN_APPROVED is not sprint-terminal",
			planned: []string{"task-1", "task-2"},
			tasks:   []Task{mkTask("task-1", TaskStatusCodingPlanApproved), mkTask("task-2", TaskStatusCodingPlanApproved)},
			want:    false,
		},
		{
			name:    "all MERGED is sprint-terminal",
			planned: []string{"task-1", "task-2"},
			tasks:   []Task{mkTask("task-1", TaskStatusMerged), mkTask("task-2", TaskStatusMerged)},
			want:    true,
		},
		{
			name:    "mix MERGED and ABANDONED",
			planned: []string{"task-1", "task-2"},
			tasks:   []Task{mkTask("task-1", TaskStatusMerged), mkTask("task-2", TaskStatusAbandoned)},
			want:    true,
		},
		{
			name:    "CODING_PLAN_APPROVED with non-terminal",
			planned: []string{"task-1", "task-2"},
			tasks:   []Task{mkTask("task-1", TaskStatusCodingPlanApproved), mkTask("task-2", TaskStatusCodePlanning)},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &State{
				Sprint: Sprint{Scope: SprintScope{Planned: tt.planned}},
				Tasks:  tt.tasks,
			}
			got := state.AllPlannedTasksTerminal()
			if got != tt.want {
				t.Errorf("AllPlannedTasksTerminal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAllPlannedTasksTerminal(t *testing.T) {
	now := time.Now().UTC()
	mkTask := func(id string, status TaskStatus) Task {
		return Task{ID: id, Status: status, Created: now, Priority: 1, Iteration: 1}
	}

	tests := []struct {
		name    string
		planned []string
		tasks   []Task
		want    bool
	}{
		{
			name:    "empty planned list",
			planned: []string{},
			tasks:   []Task{mkTask("task-1", TaskStatusMerged)},
			want:    false,
		},
		{
			name:    "all merged",
			planned: []string{"task-1", "task-2"},
			tasks:   []Task{mkTask("task-1", TaskStatusMerged), mkTask("task-2", TaskStatusMerged)},
			want:    true,
		},
		{
			name:    "mixed terminal states",
			planned: []string{"task-1", "task-2", "task-3"},
			tasks: []Task{
				mkTask("task-1", TaskStatusMerged),
				mkTask("task-2", TaskStatusAbandoned),
				mkTask("task-3", TaskStatusSuperseded),
			},
			want: true,
		},
		{
			name:    "one still in progress",
			planned: []string{"task-1", "task-2"},
			tasks:   []Task{mkTask("task-1", TaskStatusMerged), mkTask("task-2", TaskStatusImplementing)},
			want:    false,
		},
		{
			name:    "planned task missing from task list",
			planned: []string{"task-1", "task-2"},
			tasks:   []Task{mkTask("task-1", TaskStatusMerged)},
			want:    false,
		},
		{
			name:    "extra non-planned tasks don't affect result",
			planned: []string{"task-1"},
			tasks: []Task{
				mkTask("task-1", TaskStatusMerged),
				mkTask("task-2", TaskStatusImplementing),
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &State{
				Sprint: Sprint{Scope: SprintScope{Planned: tt.planned}},
				Tasks:  tt.tasks,
			}
			got := state.AllPlannedTasksTerminal()
			if got != tt.want {
				t.Errorf("AllPlannedTasksTerminal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTopPriorityTier(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name       string
		candidates []*Task
		wantIDs    []string
	}{
		{
			name:       "nil input",
			candidates: nil,
			wantIDs:    nil,
		},
		{
			name:       "empty slice",
			candidates: []*Task{},
			wantIDs:    nil,
		},
		{
			name:       "single candidate",
			candidates: []*Task{{ID: "a", Priority: 5, Created: now}},
			wantIDs:    []string{"a"},
		},
		{
			name: "distinct priorities returns top only",
			candidates: []*Task{
				{ID: "p3", Priority: 3, Created: now},
				{ID: "p1", Priority: 1, Created: now},
				{ID: "p2", Priority: 2, Created: now},
			},
			wantIDs: []string{"p1"},
		},
		{
			name: "tied top priority returns all tied",
			candidates: []*Task{
				{ID: "a", Priority: 1, Created: now},
				{ID: "b", Priority: 2, Created: now},
				{ID: "c", Priority: 1, Created: now},
				{ID: "d", Priority: 3, Created: now},
			},
			wantIDs: []string{"a", "c"},
		},
		{
			name: "all same priority returns all",
			candidates: []*Task{
				{ID: "x", Priority: 2, Created: now},
				{ID: "y", Priority: 2, Created: now},
				{ID: "z", Priority: 2, Created: now},
			},
			wantIDs: []string{"x", "y", "z"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TopPriorityTier(tt.candidates)
			if len(got) != len(tt.wantIDs) {
				t.Fatalf("len = %d, want %d", len(got), len(tt.wantIDs))
			}
			gotIDs := make(map[string]bool, len(got))
			for _, task := range got {
				gotIDs[task.ID] = true
			}
			for _, id := range tt.wantIDs {
				if !gotIDs[id] {
					t.Errorf("missing expected ID %q in result", id)
				}
			}
		})
	}
}

func TestGoalBaseCommitYAMLRoundTrip(t *testing.T) {
	t.Run("with BaseCommit set", func(t *testing.T) {
		sha := "abc123def456"
		goal := Goal{
			ID:          "goal-1",
			Description: "Test goal",
			SpecRef:     "specs/test.md",
			Created:     time.Date(2026, 4, 5, 10, 0, 0, 0, time.UTC),
			Status:      GoalStatusInProgress,
			BaseCommit:  &sha,
		}

		data, err := yaml.Marshal(&goal)
		if err != nil {
			t.Fatalf("Failed to marshal goal: %v", err)
		}

		yamlStr := string(data)
		if !strings.Contains(yamlStr, "base_commit:") {
			t.Errorf("YAML output should contain 'base_commit:' field, got:\n%s", yamlStr)
		}

		var unmarshaled Goal
		if err := yaml.Unmarshal(data, &unmarshaled); err != nil {
			t.Fatalf("Failed to unmarshal goal: %v", err)
		}
		if unmarshaled.BaseCommit == nil {
			t.Fatal("BaseCommit should not be nil after round-trip")
		}
		if *unmarshaled.BaseCommit != sha {
			t.Errorf("BaseCommit mismatch: got %q, want %q", *unmarshaled.BaseCommit, sha)
		}
	})

	t.Run("without BaseCommit", func(t *testing.T) {
		goal := Goal{
			ID:          "goal-2",
			Description: "Test goal without base commit",
			SpecRef:     "specs/test.md",
			Created:     time.Date(2026, 4, 5, 10, 0, 0, 0, time.UTC),
			Status:      GoalStatusInProgress,
		}

		data, err := yaml.Marshal(&goal)
		if err != nil {
			t.Fatalf("Failed to marshal goal: %v", err)
		}

		yamlStr := string(data)
		if strings.Contains(yamlStr, "base_commit") {
			t.Errorf("YAML output should NOT contain 'base_commit' when nil (omitempty), got:\n%s", yamlStr)
		}

		var unmarshaled Goal
		if err := yaml.Unmarshal(data, &unmarshaled); err != nil {
			t.Fatalf("Failed to unmarshal goal: %v", err)
		}
		if unmarshaled.BaseCommit != nil {
			t.Errorf("BaseCommit should be nil after round-trip, got %q", *unmarshaled.BaseCommit)
		}
	})
}

func TestEffectiveParentTasks(t *testing.T) {
	parentID := "parent-1"
	parentID2 := "parent-2"

	tests := []struct {
		name string
		task Task
		want []string
	}{
		{
			name: "no parents returns nil",
			task: Task{ID: "t1"},
			want: nil,
		},
		{
			name: "legacy ParentTask returns single-element slice",
			task: Task{ID: "t1", ParentTask: &parentID},
			want: []string{"parent-1"},
		},
		{
			name: "ParentTasks returns ParentTasks",
			task: Task{ID: "t1", ParentTasks: []string{"parent-1", "parent-2"}},
			want: []string{"parent-1", "parent-2"},
		},
		{
			name: "both set returns ParentTasks (precedence)",
			task: Task{ID: "t1", ParentTask: &parentID2, ParentTasks: []string{"parent-1"}},
			want: []string{"parent-1"},
		},
		{
			name: "empty ParentTask string returns nil",
			task: func() Task {
				empty := ""
				return Task{ID: "t1", ParentTask: &empty}
			}(),
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.task.EffectiveParentTasks()
			if len(got) != len(tt.want) {
				t.Fatalf("EffectiveParentTasks() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("EffectiveParentTasks()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestArchRefYAMLRoundTrip(t *testing.T) {
	t.Run("Task with ArchRef", func(t *testing.T) {
		task := Task{
			ID:          "task-1",
			Description: "test task",
			Status:      "DRAFT_CODE",
			ArchRef:     "specs/arch-plan/my-feature.md",
		}

		data, err := yaml.Marshal(&task)
		if err != nil {
			t.Fatalf("Failed to marshal task: %v", err)
		}

		yamlStr := string(data)
		if !strings.Contains(yamlStr, "arch_ref:") {
			t.Errorf("YAML output should contain 'arch_ref:' field, got:\n%s", yamlStr)
		}

		var unmarshaled Task
		if err := yaml.Unmarshal(data, &unmarshaled); err != nil {
			t.Fatalf("Failed to unmarshal task: %v", err)
		}
		if unmarshaled.ArchRef != "specs/arch-plan/my-feature.md" {
			t.Errorf("ArchRef mismatch: got %q, want %q", unmarshaled.ArchRef, "specs/arch-plan/my-feature.md")
		}
	})

	t.Run("Task without ArchRef omits field", func(t *testing.T) {
		task := Task{
			ID:          "task-2",
			Description: "test task without arch_ref",
			Status:      "DRAFT_CODE",
		}

		data, err := yaml.Marshal(&task)
		if err != nil {
			t.Fatalf("Failed to marshal task: %v", err)
		}

		yamlStr := string(data)
		if strings.Contains(yamlStr, "arch_ref:") {
			t.Errorf("YAML output should NOT contain 'arch_ref:' when empty, got:\n%s", yamlStr)
		}
	})

	t.Run("OutputEntry with ArchRef", func(t *testing.T) {
		entry := OutputEntry{
			Desc:    "subtask-1",
			ArchRef: "specs/arch-plan/my-feature.md",
		}

		data, err := yaml.Marshal(&entry)
		if err != nil {
			t.Fatalf("Failed to marshal OutputEntry: %v", err)
		}

		yamlStr := string(data)
		if !strings.Contains(yamlStr, "arch_ref:") {
			t.Errorf("YAML output should contain 'arch_ref:' field, got:\n%s", yamlStr)
		}

		var unmarshaled OutputEntry
		if err := yaml.Unmarshal(data, &unmarshaled); err != nil {
			t.Fatalf("Failed to unmarshal OutputEntry: %v", err)
		}
		if unmarshaled.ArchRef != "specs/arch-plan/my-feature.md" {
			t.Errorf("ArchRef mismatch: got %q, want %q", unmarshaled.ArchRef, "specs/arch-plan/my-feature.md")
		}
	})

	t.Run("OutputEntry without ArchRef omits field", func(t *testing.T) {
		entry := OutputEntry{
			Desc: "subtask-2",
		}

		data, err := yaml.Marshal(&entry)
		if err != nil {
			t.Fatalf("Failed to marshal OutputEntry: %v", err)
		}

		yamlStr := string(data)
		if strings.Contains(yamlStr, "arch_ref:") {
			t.Errorf("YAML output should NOT contain 'arch_ref:' when empty, got:\n%s", yamlStr)
		}
	})
}
