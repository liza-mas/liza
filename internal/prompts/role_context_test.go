package prompts

import (
	"testing"

	"github.com/liza-mas/liza/internal/models"
)

func TestRoleContextData_CoderPopulation(t *testing.T) {
	handoff := &models.HandoffEvent{
		Agent:     "coder-0",
		Trigger:   models.HandoffTriggerContextExhaustion,
		Succeeded: []string{"Completed phase 1"},
		NextStep:  "Continue with phase 2",
	}

	data := RoleContextData{
		// Identity
		Role:     "coder",
		AgentID:  "coder-1",
		RoleType: "doer",

		// Task
		TaskID:         "task-42",
		Description:    "Implement feature X",
		DoneWhen:       "Tests pass and feature works",
		Scope:          "internal/prompts/role_context.go",
		SpecRef:        "specs/build/3.md",
		Worktree:       "/project/.worktrees/task-42",
		IterationNum:   2,
		AttemptNum:     1,
		PriorRejection: "Missing error handling",

		// Plan scoping
		GoalSpecRef: "specs/vision.md",
		SiblingTasks: []SiblingTaskSummary{
			{ID: "task-41", Description: "Setup infrastructure", Status: "MERGED"},
		},
		TotalPlanTasks: 5,
		TaskOrdinal:    2,

		// Coder-specific
		IntegrationBranch: "integration",
		HandoffNote:       handoff,

		// Config/state
		ProjectRoot: "/project",
		StatePath:   "/project/.liza/state.yaml",
		SpecsDir:    "/project/specs",
		GoalDesc:    "Build a web API",

		// Declarative
		MandatoryDocs: []string{"docs/arch.md"},
		Skills:        []string{"debugging", "testing", "clean-code"},
	}

	// Verify identity
	if data.Role != "coder" {
		t.Errorf("Role = %q, want %q", data.Role, "coder")
	}
	if data.AgentID != "coder-1" {
		t.Errorf("AgentID = %q, want %q", data.AgentID, "coder-1")
	}
	if data.RoleType != "doer" {
		t.Errorf("RoleType = %q, want %q", data.RoleType, "doer")
	}

	// Verify task fields
	if data.TaskID != "task-42" {
		t.Errorf("TaskID = %q, want %q", data.TaskID, "task-42")
	}
	if data.IterationNum != 2 {
		t.Errorf("IterationNum = %d, want %d", data.IterationNum, 2)
	}
	if data.AttemptNum != 1 {
		t.Errorf("AttemptNum = %d, want %d", data.AttemptNum, 1)
	}
	if data.PriorRejection != "Missing error handling" {
		t.Errorf("PriorRejection = %q, want %q", data.PriorRejection, "Missing error handling")
	}
	if data.Worktree != "/project/.worktrees/task-42" {
		t.Errorf("Worktree = %q, want %q", data.Worktree, "/project/.worktrees/task-42")
	}

	// Verify coder-specific
	if data.IntegrationBranch != "integration" {
		t.Errorf("IntegrationBranch = %q, want %q", data.IntegrationBranch, "integration")
	}
	if data.HandoffNote == nil {
		t.Fatal("HandoffNote is nil, want non-nil")
	}
	if len(data.HandoffNote.Succeeded) != 1 || data.HandoffNote.Succeeded[0] != "Completed phase 1" {
		t.Errorf("HandoffNote.Succeeded = %v, want [Completed phase 1]", data.HandoffNote.Succeeded)
	}
	if data.HandoffNote.NextStep != "Continue with phase 2" {
		t.Errorf("HandoffNote.NextStep = %q, want %q", data.HandoffNote.NextStep, "Continue with phase 2")
	}

	// Verify plan scoping
	if data.GoalSpecRef != "specs/vision.md" {
		t.Errorf("GoalSpecRef = %q, want %q", data.GoalSpecRef, "specs/vision.md")
	}
	if len(data.SiblingTasks) != 1 {
		t.Fatalf("SiblingTasks length = %d, want 1", len(data.SiblingTasks))
	}
	if data.SiblingTasks[0].ID != "task-41" {
		t.Errorf("SiblingTasks[0].ID = %q, want %q", data.SiblingTasks[0].ID, "task-41")
	}
	if data.TotalPlanTasks != 5 {
		t.Errorf("TotalPlanTasks = %d, want %d", data.TotalPlanTasks, 5)
	}
	if data.TaskOrdinal != 2 {
		t.Errorf("TaskOrdinal = %d, want %d", data.TaskOrdinal, 2)
	}

	// Verify declarative
	if len(data.Skills) != 3 {
		t.Errorf("Skills length = %d, want 3", len(data.Skills))
	}
	if len(data.MandatoryDocs) != 1 {
		t.Errorf("MandatoryDocs length = %d, want 1", len(data.MandatoryDocs))
	}

	// Verify orchestrator fields are zero-valued for coder
	if data.DashboardOutput != "" {
		t.Errorf("DashboardOutput should be empty for coder, got %q", data.DashboardOutput)
	}
	if data.WakeInstruction != "" {
		t.Errorf("WakeInstruction should be empty for coder, got %q", data.WakeInstruction)
	}

	// Verify review fields are zero-valued for coder
	if data.ReviewCycles != 0 {
		t.Errorf("ReviewCycles should be 0 for coder, got %d", data.ReviewCycles)
	}
	if data.ScopeExtensions != nil {
		t.Errorf("ScopeExtensions should be nil for coder, got %v", data.ScopeExtensions)
	}
}

func TestRoleContextData_CodeReviewerPopulation(t *testing.T) {
	data := RoleContextData{
		// Identity
		Role:     "code-reviewer",
		AgentID:  "code-reviewer-1",
		RoleType: "reviewer",

		// Task
		TaskID:         "task-42",
		Description:    "Implement feature X",
		DoneWhen:       "Tests pass and feature works",
		Scope:          "internal/prompts/role_context.go",
		SpecRef:        "specs/build/3.md",
		Worktree:       "/project/.worktrees/task-42",
		IterationNum:   1,
		AttemptNum:     1,
		PriorRejection: "",

		// Review
		ReviewCycles: 2,
		ScopeExtensions: []map[string]string{
			{"file": "go.mod", "justification": "New dependency required"},
		},

		// Plan scoping
		GoalSpecRef: "specs/vision.md",
		SiblingTasks: []SiblingTaskSummary{
			{ID: "task-41", Description: "Setup infrastructure", Status: "MERGED"},
			{ID: "task-43", Description: "Write docs", Status: "DRAFT_CODE"},
		},
		TotalPlanTasks: 5,
		TaskOrdinal:    2,

		// Config/state
		ProjectRoot: "/project",
		StatePath:   "/project/.liza/state.yaml",
		SpecsDir:    "/project/specs",
		GoalDesc:    "Build a web API",

		// Declarative
		MandatoryDocs: []string{},
		Skills:        []string{"code-review", "systemic-thinking", "software-architecture-review"},
	}

	// Verify identity
	if data.Role != "code-reviewer" {
		t.Errorf("Role = %q, want %q", data.Role, "code-reviewer")
	}
	if data.RoleType != "reviewer" {
		t.Errorf("RoleType = %q, want %q", data.RoleType, "reviewer")
	}

	// Verify review-specific fields
	if data.ReviewCycles != 2 {
		t.Errorf("ReviewCycles = %d, want %d", data.ReviewCycles, 2)
	}
	if len(data.ScopeExtensions) != 1 {
		t.Fatalf("ScopeExtensions length = %d, want 1", len(data.ScopeExtensions))
	}
	if data.ScopeExtensions[0]["file"] != "go.mod" {
		t.Errorf("ScopeExtensions[0][file] = %q, want %q", data.ScopeExtensions[0]["file"], "go.mod")
	}

	// Verify plan scoping
	if len(data.SiblingTasks) != 2 {
		t.Errorf("SiblingTasks length = %d, want 2", len(data.SiblingTasks))
	}

	// Verify coder-specific fields are zero-valued for reviewer
	if data.IntegrationBranch != "" {
		t.Errorf("IntegrationBranch should be empty for reviewer, got %q", data.IntegrationBranch)
	}
	if data.HandoffNote != nil {
		t.Errorf("HandoffNote should be nil for reviewer, got %v", data.HandoffNote)
	}

	// Verify orchestrator fields are zero-valued for reviewer
	if data.DashboardOutput != "" {
		t.Errorf("DashboardOutput should be empty for reviewer, got %q", data.DashboardOutput)
	}

	// Verify declarative
	if len(data.Skills) != 3 {
		t.Errorf("Skills length = %d, want 3", len(data.Skills))
	}
	if data.MandatoryDocs == nil {
		t.Error("MandatoryDocs should be non-nil (empty slice), got nil")
	}
}

func TestRoleContextData_OrchestratorPopulation(t *testing.T) {
	data := RoleContextData{
		// Identity
		Role:     "orchestrator",
		AgentID:  "orchestrator-1",
		RoleType: "orchestrator",

		// Orchestrator-specific
		DashboardOutput:   "Sprint 3: 5/10 tasks complete",
		WakeInstruction:   "Plan next sprint",
		AgentStates:       "coder-1: active, reviewer-1: idle",
		SprintMetrics:     "velocity: 3.2 tasks/day",
		ActivePolicies:    "max-parallel: 3",
		BlockedTasks:      "task-99: missing spec",
		CheckpointSummary: "Last checkpoint: 2h ago",
		PipelineConfig:    "pipeline v2 loaded",

		// Config/state
		ProjectRoot: "/project",
		StatePath:   "/project/.liza/state.yaml",
		SpecsDir:    "/project/specs",
		GoalDesc:    "Build a web API",

		// Declarative
		MandatoryDocs: []string{},
		Skills:        []string{"systemic-thinking"},
	}

	// Verify identity
	if data.Role != "orchestrator" {
		t.Errorf("Role = %q, want %q", data.Role, "orchestrator")
	}
	if data.RoleType != "orchestrator" {
		t.Errorf("RoleType = %q, want %q", data.RoleType, "orchestrator")
	}

	// Verify orchestrator-specific fields
	if data.DashboardOutput != "Sprint 3: 5/10 tasks complete" {
		t.Errorf("DashboardOutput = %q, want %q", data.DashboardOutput, "Sprint 3: 5/10 tasks complete")
	}
	if data.WakeInstruction != "Plan next sprint" {
		t.Errorf("WakeInstruction = %q, want %q", data.WakeInstruction, "Plan next sprint")
	}
	if data.AgentStates != "coder-1: active, reviewer-1: idle" {
		t.Errorf("AgentStates = %q, want %q", data.AgentStates, "coder-1: active, reviewer-1: idle")
	}
	if data.SprintMetrics != "velocity: 3.2 tasks/day" {
		t.Errorf("SprintMetrics = %q, want %q", data.SprintMetrics, "velocity: 3.2 tasks/day")
	}
	if data.ActivePolicies != "max-parallel: 3" {
		t.Errorf("ActivePolicies = %q, want %q", data.ActivePolicies, "max-parallel: 3")
	}
	if data.BlockedTasks != "task-99: missing spec" {
		t.Errorf("BlockedTasks = %q, want %q", data.BlockedTasks, "task-99: missing spec")
	}
	if data.CheckpointSummary != "Last checkpoint: 2h ago" {
		t.Errorf("CheckpointSummary = %q, want %q", data.CheckpointSummary, "Last checkpoint: 2h ago")
	}
	if data.PipelineConfig != "pipeline v2 loaded" {
		t.Errorf("PipelineConfig = %q, want %q", data.PipelineConfig, "pipeline v2 loaded")
	}

	// Verify task fields are zero-valued for orchestrator
	if data.TaskID != "" {
		t.Errorf("TaskID should be empty for orchestrator, got %q", data.TaskID)
	}
	if data.Worktree != "" {
		t.Errorf("Worktree should be empty for orchestrator, got %q", data.Worktree)
	}
	if data.IterationNum != 0 {
		t.Errorf("IterationNum should be 0 for orchestrator, got %d", data.IterationNum)
	}

	// Verify coder-specific fields are zero-valued
	if data.IntegrationBranch != "" {
		t.Errorf("IntegrationBranch should be empty for orchestrator, got %q", data.IntegrationBranch)
	}
	if data.HandoffNote != nil {
		t.Errorf("HandoffNote should be nil for orchestrator, got %v", data.HandoffNote)
	}

	// Verify review fields are zero-valued
	if data.ReviewCycles != 0 {
		t.Errorf("ReviewCycles should be 0 for orchestrator, got %d", data.ReviewCycles)
	}

	// Verify declarative
	if len(data.Skills) != 1 {
		t.Errorf("Skills length = %d, want 1", len(data.Skills))
	}
}

func TestRoleContextData_Architect(t *testing.T) {
	data := RoleContextData{
		// Identity
		Role:     "architect",
		AgentID:  "architect-1",
		RoleType: "doer",

		// Task
		TaskID:      "arch-task-1",
		Description: "Design component boundaries for feature X",
		DoneWhen:    "Architecture document produced",
		Scope:       "specs/arch-plan/",
		SpecRef:     "specs/goals/feature-x.md",
		Worktree:    "/project/.worktrees/arch-task-1",

		// Architecture-specific
		ArchRef: "/project/.worktrees/arch-task-1/specs/arch-plan/feature-x.md",
		ParentTaskContexts: []ParentTaskContext{
			{
				ID:          "us-1",
				Description: "User can sign up",
				DoneWhen:    "Signup flow works end-to-end",
				SpecRef:     "specs/goals/feature-x.md",
				PlanRef:     "specs/plans/signup.md",
			},
			{
				ID:          "us-2",
				Description: "User can reset password",
				DoneWhen:    "Password reset sends email",
				SpecRef:     "specs/goals/feature-x.md",
				PlanRef:     "",
			},
		},

		// Config/state
		ProjectRoot: "/project",
		StatePath:   "/project/.liza/state.yaml",
		SpecsDir:    "/project/specs",
		GoalDesc:    "Build user management",
	}

	// Verify ArchRef is populated
	if data.ArchRef != "/project/.worktrees/arch-task-1/specs/arch-plan/feature-x.md" {
		t.Errorf("ArchRef = %q, want %q", data.ArchRef, "/project/.worktrees/arch-task-1/specs/arch-plan/feature-x.md")
	}

	// Verify ParentTaskContexts populated with 2 entries
	if len(data.ParentTaskContexts) != 2 {
		t.Fatalf("ParentTaskContexts length = %d, want 2", len(data.ParentTaskContexts))
	}
	if data.ParentTaskContexts[0].ID != "us-1" {
		t.Errorf("ParentTaskContexts[0].ID = %q, want %q", data.ParentTaskContexts[0].ID, "us-1")
	}
	if data.ParentTaskContexts[0].Description != "User can sign up" {
		t.Errorf("ParentTaskContexts[0].Description = %q, want %q", data.ParentTaskContexts[0].Description, "User can sign up")
	}
	if data.ParentTaskContexts[0].DoneWhen != "Signup flow works end-to-end" {
		t.Errorf("ParentTaskContexts[0].DoneWhen = %q, want %q", data.ParentTaskContexts[0].DoneWhen, "Signup flow works end-to-end")
	}
	if data.ParentTaskContexts[0].SpecRef != "specs/goals/feature-x.md" {
		t.Errorf("ParentTaskContexts[0].SpecRef = %q, want %q", data.ParentTaskContexts[0].SpecRef, "specs/goals/feature-x.md")
	}
	if data.ParentTaskContexts[0].PlanRef != "specs/plans/signup.md" {
		t.Errorf("ParentTaskContexts[0].PlanRef = %q, want %q", data.ParentTaskContexts[0].PlanRef, "specs/plans/signup.md")
	}
	if data.ParentTaskContexts[1].ID != "us-2" {
		t.Errorf("ParentTaskContexts[1].ID = %q, want %q", data.ParentTaskContexts[1].ID, "us-2")
	}
	if data.ParentTaskContexts[1].PlanRef != "" {
		t.Errorf("ParentTaskContexts[1].PlanRef = %q, want empty", data.ParentTaskContexts[1].PlanRef)
	}

	// Verify orchestrator and review fields are zero-valued for architect
	if data.DashboardOutput != "" {
		t.Errorf("DashboardOutput should be empty for architect, got %q", data.DashboardOutput)
	}
	if data.ReviewCycles != 0 {
		t.Errorf("ReviewCycles should be 0 for architect, got %d", data.ReviewCycles)
	}
}

func TestRoleContextData_PlanRefAndValidationPlan(t *testing.T) {
	// Coder gets PlanRef but not ValidationPlan
	coderData := RoleContextData{
		Role:     "coder",
		RoleType: "doer",
		PlanRef:  "specs/plans/20260317-plan.md",
	}
	if coderData.PlanRef != "specs/plans/20260317-plan.md" {
		t.Errorf("Coder PlanRef = %q, want %q", coderData.PlanRef, "specs/plans/20260317-plan.md")
	}
	if coderData.ValidationPlan != "" {
		t.Errorf("Coder ValidationPlan should be empty, got %q", coderData.ValidationPlan)
	}

	// Reviewer gets both PlanRef and ValidationPlan
	reviewerData := RoleContextData{
		Role:           "code-reviewer",
		RoleType:       "reviewer",
		PlanRef:        "specs/plans/20260317-plan.md",
		ValidationPlan: "run go test ./... and verify all pass",
	}
	if reviewerData.PlanRef != "specs/plans/20260317-plan.md" {
		t.Errorf("Reviewer PlanRef = %q, want %q", reviewerData.PlanRef, "specs/plans/20260317-plan.md")
	}
	if reviewerData.ValidationPlan != "run go test ./... and verify all pass" {
		t.Errorf("Reviewer ValidationPlan = %q, want %q", reviewerData.ValidationPlan, "run go test ./... and verify all pass")
	}
}
