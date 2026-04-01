package statevalidate

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// loadTestConfig loads the pipeline config from the test fixture YAML.
func loadTestConfig(t *testing.T) *pipeline.PipelineConfig {
	t.Helper()
	repoRoot := testhelpers.FindRepoRoot(t)
	yamlPath := filepath.Join(repoRoot, "internal", "pipeline", "testdata", "valid-coding-subpipeline.yaml")
	cfg, err := pipeline.Load(yamlPath)
	if err != nil {
		t.Fatalf("Failed to load test pipeline config: %v", err)
	}
	return cfg
}

// loadTestResolver loads a pipeline resolver from the test fixture YAML.
func loadTestResolver(t *testing.T) *pipeline.Resolver {
	t.Helper()
	cfg := loadTestConfig(t)
	return pipeline.NewResolver(cfg)
}

func TestValidateTaskStates_RejectsMissingRolePair_PipelineGoal(t *testing.T) {
	resolver := loadTestResolver(t)
	state := testhelpers.CreateValidState()
	now := time.Now().UTC()

	// Task in a cross-cutting status (MERGED) but missing role_pair
	state.Tasks = []models.Task{
		{
			ID:          "task-1",
			Description: "Missing role_pair",
			Status:      models.TaskStatusMerged,
			Priority:    1,
			Created:     now,
			RolePair:    "", // missing
		},
	}
	state.Sprint.Scope.Planned = []string{"task-1"}

	err := validateTaskStates(state, "", true, resolver)
	if err == nil {
		t.Fatal("Expected error for pipeline-goal task missing role_pair")
	}
	if !strings.Contains(err.Error(), "missing role_pair") {
		t.Errorf("Error = %q, want to contain 'missing role_pair'", err.Error())
	}
}

func TestValidateTaskStates_RejectsMissingRolePair_BlockedStatus(t *testing.T) {
	resolver := loadTestResolver(t)
	state := testhelpers.CreateValidState()
	now := time.Now().UTC()

	// BLOCKED task missing role_pair — should still be rejected for pipeline goals
	blockedReason := "blocked"
	state.Tasks = []models.Task{
		{
			ID:               "task-2",
			Description:      "Blocked without role_pair",
			Status:           models.TaskStatusBlocked,
			Priority:         1,
			Created:          now,
			RolePair:         "",
			BlockedReason:    &blockedReason,
			BlockedQuestions: []string{"q1"},
		},
	}
	state.Sprint.Scope.Planned = []string{"task-2"}

	err := validateTaskStates(state, "", true, resolver)
	if err == nil {
		t.Fatal("Expected error for BLOCKED pipeline-goal task missing role_pair")
	}
	if !strings.Contains(err.Error(), "missing role_pair") {
		t.Errorf("Error = %q, want to contain 'missing role_pair'", err.Error())
	}
}

func TestValidateTaskStates_RejectsInvalidRolePair(t *testing.T) {
	resolver := loadTestResolver(t)
	state := testhelpers.CreateValidState()
	now := time.Now().UTC()

	state.Tasks = []models.Task{
		{
			ID:          "task-3",
			Description: "Invalid role_pair",
			Status:      models.TaskStatusMerged,
			Priority:    1,
			Created:     now,
			RolePair:    "nonexistent-pair",
		},
	}
	state.Sprint.Scope.Planned = []string{"task-3"}

	err := validateTaskStates(state, "", true, resolver)
	if err == nil {
		t.Fatal("Expected error for task with invalid role_pair")
	}
	if !strings.Contains(err.Error(), "invalid role_pair") {
		t.Errorf("Error = %q, want to contain 'invalid role_pair'", err.Error())
	}
}

func TestValidateTaskStates_AcceptsValidRolePair(t *testing.T) {
	resolver := loadTestResolver(t)
	state := testhelpers.CreateValidState()
	now := time.Now().UTC()

	state.Tasks = []models.Task{
		{
			ID:          "task-4",
			Description: "Valid pipeline task",
			Status:      models.TaskStatusMerged,
			Priority:    1,
			Created:     now,
			RolePair:    "coding-pair",
		},
	}
	state.Sprint.Scope.Planned = []string{"task-4"}

	err := validateTaskStates(state, "", true, resolver)
	if err != nil {
		t.Fatalf("Unexpected error for valid pipeline task: %v", err)
	}
}

func TestValidateTaskStates_AcceptsPipelineDeclaredStatus(t *testing.T) {
	resolver := loadTestResolver(t)
	state := testhelpers.CreateValidState()
	now := time.Now().UTC()

	// DRAFT_CODE is not a hardcoded valid status — it comes from the pipeline config
	state.Tasks = []models.Task{
		{
			ID:          "task-5",
			Description: "Pipeline status task",
			Status:      models.TaskStatus("DRAFT_CODE"),
			Priority:    1,
			Created:     now,
			RolePair:    "coding-pair",
		},
	}
	state.Sprint.Scope.Planned = []string{"task-5"}

	err := validateTaskStates(state, "", true, resolver)
	if err != nil {
		t.Fatalf("Unexpected error for pipeline-declared status: %v", err)
	}
}

func TestValidateTaskStates_RejectsUnknownStatusInPipelineGoal(t *testing.T) {
	resolver := loadTestResolver(t)
	state := testhelpers.CreateValidState()
	now := time.Now().UTC()

	state.Tasks = []models.Task{
		{
			ID:          "task-6",
			Description: "Unknown status task",
			Status:      models.TaskStatus("TOTALLY_UNKNOWN"),
			Priority:    1,
			Created:     now,
			RolePair:    "coding-pair",
		},
	}
	state.Sprint.Scope.Planned = []string{"task-6"}

	err := validateTaskStates(state, "", true, resolver)
	if err == nil {
		t.Fatal("Expected error for unknown status in pipeline goal")
	}
	if !strings.Contains(err.Error(), "unknown task status") {
		t.Errorf("Error = %q, want to contain 'unknown task status'", err.Error())
	}
}

func TestValidateTaskStates_LegacyGoalNoRolePairRequired(t *testing.T) {
	// No resolver → legacy goal. Tasks without role_pair are fine.
	state := testhelpers.CreateValidState()
	now := time.Now().UTC()

	state.Tasks = []models.Task{
		{
			ID:          "task-7",
			Description: "Legacy task",
			Status:      models.TaskStatusMerged,
			Priority:    1,
			Created:     now,
			RolePair:    "", // no role_pair, but that's fine for legacy
		},
	}
	state.Sprint.Scope.Planned = []string{"task-7"}

	err := validateTaskStates(state, "", true, nil)
	if err != nil {
		t.Fatalf("Unexpected error for legacy goal task without role_pair: %v", err)
	}
}

func TestValidateDependencies_PipelineExecutingUnmetDeps(t *testing.T) {
	cfg := loadTestConfig(t)
	resolver := pipeline.NewResolver(cfg)
	state := testhelpers.CreateValidState()
	now := time.Now().UTC()
	assignee := "coder-1"
	worktree := ".worktrees/task-exec"
	baseCommit := "abc123"
	leaseExpires := now.Add(30 * time.Minute)

	// dep-task is NOT merged — it's still DRAFT_CODE
	state.Tasks = []models.Task{
		{
			ID:          "dep-task",
			Description: "Dependency task",
			Status:      models.TaskStatus("DRAFT_CODE"),
			Priority:    1,
			Created:     now,
			RolePair:    "coding-pair",
		},
		{
			ID:           "task-exec",
			Description:  "Executing pipeline task with unmet dep",
			Status:       models.TaskStatus("IMPLEMENTING_CODE"), // pipeline executing status
			Priority:     1,
			Created:      now,
			RolePair:     "coding-pair",
			AssignedTo:   &assignee,
			Worktree:     &worktree,
			BaseCommit:   &baseCommit,
			LeaseExpires: &leaseExpires,
			DependsOn:    []string{"dep-task"},
		},
	}
	state.Sprint.Scope.Planned = []string{"dep-task", "task-exec"}

	err := validateDependencies(state, "", true, resolver, cfg)
	if err == nil {
		t.Fatal("Expected error for pipeline executing task with unmet dependencies")
	}
	if !strings.Contains(err.Error(), "unmet dependencies") {
		t.Errorf("Error = %q, want to contain 'unmet dependencies'", err.Error())
	}
	if !strings.Contains(err.Error(), "task-exec") {
		t.Errorf("Error = %q, want to contain task ID 'task-exec'", err.Error())
	}
}

func TestValidateDependencies_PipelineExecutingMetDeps(t *testing.T) {
	cfg := loadTestConfig(t)
	resolver := pipeline.NewResolver(cfg)
	state := testhelpers.CreateValidState()
	now := time.Now().UTC()
	assignee := "coder-1"
	worktree := ".worktrees/task-exec"
	baseCommit := "abc123"
	leaseExpires := now.Add(30 * time.Minute)

	// dep-task IS merged
	state.Tasks = []models.Task{
		{
			ID:          "dep-task",
			Description: "Dependency task",
			Status:      models.TaskStatusMerged,
			Priority:    1,
			Created:     now,
			RolePair:    "coding-pair",
		},
		{
			ID:           "task-exec",
			Description:  "Executing pipeline task with met dep",
			Status:       models.TaskStatus("IMPLEMENTING_CODE"),
			Priority:     1,
			Created:      now,
			RolePair:     "coding-pair",
			AssignedTo:   &assignee,
			Worktree:     &worktree,
			BaseCommit:   &baseCommit,
			LeaseExpires: &leaseExpires,
			DependsOn:    []string{"dep-task"},
		},
	}
	state.Sprint.Scope.Planned = []string{"dep-task", "task-exec"}

	err := validateDependencies(state, "", true, resolver, cfg)
	if err != nil {
		t.Fatalf("Unexpected error for pipeline executing task with met dependencies: %v", err)
	}
}

func TestValidateDependencies_ExecutingUnmetDeps(t *testing.T) {
	cfg := loadTestConfig(t)
	resolver := pipeline.NewResolver(cfg)
	state := testhelpers.CreateValidState()
	now := time.Now().UTC()
	assignee := "coder-1"
	worktree := ".worktrees/task-impl"
	baseCommit := "abc123"
	leaseExpires := now.Add(30 * time.Minute)

	state.Tasks = []models.Task{
		{
			ID:          "dep-task",
			Description: "Dependency task",
			Status:      models.TaskStatusDraft,
			Priority:    1,
			Created:     now,
			RolePair:    "coding-pair",
		},
		{
			ID:           "task-impl",
			Description:  "Executing task with unmet dep",
			Status:       models.TaskStatusImplementing,
			Priority:     1,
			Created:      now,
			RolePair:     "coding-pair",
			AssignedTo:   &assignee,
			Worktree:     &worktree,
			BaseCommit:   &baseCommit,
			LeaseExpires: &leaseExpires,
			DependsOn:    []string{"dep-task"},
		},
	}
	state.Sprint.Scope.Planned = []string{"dep-task", "task-impl"}

	err := validateDependencies(state, "", true, resolver, cfg)
	if err == nil {
		t.Fatal("Expected error for IMPLEMENTING task with unmet dependencies")
	}
	if !strings.Contains(err.Error(), "unmet dependencies") {
		t.Errorf("Error = %q, want to contain 'unmet dependencies'", err.Error())
	}
}

func TestValidateDependencies_SupersededDepSatisfied(t *testing.T) {
	cfg := loadTestConfig(t)
	resolver := pipeline.NewResolver(cfg)
	state := testhelpers.CreateValidState()
	now := time.Now().UTC()
	assignee := "coder-1"
	worktree := ".worktrees/task-exec"
	baseCommit := "abc123"
	leaseExpires := now.Add(30 * time.Minute)

	// dep-task is SUPERSEDED — should satisfy dependency
	state.Tasks = []models.Task{
		{
			ID:          "dep-task",
			Description: "Superseded dependency task",
			Status:      models.TaskStatusSuperseded,
			Priority:    1,
			Created:     now,
			RolePair:    "coding-pair",
		},
		{
			ID:           "task-exec",
			Description:  "Executing task depending on superseded task",
			Status:       models.TaskStatus("IMPLEMENTING_CODE"),
			Priority:     1,
			Created:      now,
			RolePair:     "coding-pair",
			AssignedTo:   &assignee,
			Worktree:     &worktree,
			BaseCommit:   &baseCommit,
			LeaseExpires: &leaseExpires,
			DependsOn:    []string{"dep-task"},
		},
	}
	state.Sprint.Scope.Planned = []string{"dep-task", "task-exec"}

	err := validateDependencies(state, "", true, resolver, cfg)
	if err != nil {
		t.Fatalf("SUPERSEDED dependency should satisfy requirement, got: %v", err)
	}
}
