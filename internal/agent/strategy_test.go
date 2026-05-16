package agent

import (
	"context"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// testResolver returns a pipeline.Resolver built from a minimal but complete
// pipeline YAML containing all 9 standard roles. Tests use this instead of
// loading from disk, ensuring deterministic behavior without file I/O.
func testResolver(t *testing.T) *pipeline.Resolver {
	t.Helper()
	cfg, err := pipeline.LoadFromBytes(testPipelineYAML)
	if err != nil {
		t.Fatalf("testResolver: %v", err)
	}
	return pipeline.NewResolver(cfg)
}

// testPipelineYAML is the minimal pipeline config with all 9 standard roles
// and the required role-pairs, sub-pipelines, and entry-points.
var testPipelineYAML = []byte(`
pipeline:
  roles:
    orchestrator:
      type: orchestrator
      display-name: "Orchestrator"
      context-sections:
        - orchestrator-dashboard
        - wake-instructions
        - mandatory-docs
        - skills-affinity
    epic-planner:
      type: doer
      display-name: "Epic Planner"
      context-sections:
        - assigned-task
        - worktree-rules
        - prior-rejection
        - prior-attempt
        - doer-state-transitions
        - epic-planner-tools
        - capability-scoping
        - implementation-phase
        - mandatory-docs
        - skills-affinity
    epic-plan-reviewer:
      type: reviewer
      display-name: "Epic Plan Reviewer"
      context-sections:
        - review-task
        - worktree-rules
        - prior-rejection
        - reviewer-state-transitions
        - epic-plan-reviewer-tools
        - anomaly-logging
        - review-instructions
        - rejection-format
        - verdict-submission
        - mandatory-docs
        - skills-affinity
    us-writer:
      type: doer
      display-name: "US Writer"
      context-sections:
        - assigned-task
        - worktree-rules
        - collective-plan-scoping
        - prior-rejection
        - prior-attempt
        - doer-state-transitions
        - us-writer-tools
        - capability-scoping
        - implementation-phase
        - mandatory-docs
        - skills-affinity
    us-reviewer:
      type: reviewer
      display-name: "US Reviewer"
      context-sections:
        - review-task
        - worktree-rules
        - collective-plan-scoping
        - prior-rejection
        - reviewer-state-transitions
        - us-reviewer-tools
        - anomaly-logging
        - review-instructions
        - rejection-format
        - verdict-submission
        - mandatory-docs
        - skills-affinity
    code-planner:
      type: doer
      display-name: "Code Planner"
      context-sections:
        - assigned-task
        - worktree-rules
        - collective-plan-scoping
        - prior-rejection
        - prior-attempt
        - doer-state-transitions
        - code-planner-tools
        - task-decomposition
        - implementation-phase
        - mandatory-docs
        - skills-affinity
    code-plan-reviewer:
      type: reviewer
      display-name: "Code Plan Reviewer"
      context-sections:
        - review-task
        - worktree-rules
        - collective-plan-scoping
        - prior-rejection
        - reviewer-state-transitions
        - code-plan-reviewer-tools
        - anomaly-logging
        - review-instructions
        - rejection-format
        - verdict-submission
        - mandatory-docs
        - skills-affinity
    coder:
      type: doer
      display-name: "Coder"
      context-sections:
        - assigned-task
        - worktree-rules
        - collective-plan-scoping
        - handoff-resume
        - integration-fix
        - prior-rejection
        - prior-attempt
        - doer-state-transitions
        - coder-tools
        - anomaly-logging
        - blocking-protocol
        - commit-workflow
        - implementation-phase
        - submission-phase
        - mandatory-docs
        - skills-affinity
    code-reviewer:
      type: reviewer
      display-name: "Code Reviewer"
      context-sections:
        - review-task
        - worktree-rules
        - collective-plan-scoping
        - scope-extensions
        - prior-rejection
        - reviewer-state-transitions
        - code-reviewer-tools
        - anomaly-logging
        - review-instructions
        - rejection-format
        - verdict-submission
        - mandatory-docs
        - skills-affinity

  role-pairs:
    coding-pair:
      doer: coder
      reviewer: code-reviewer
      states:
        initial: DRAFT_CODE
        executing: IMPLEMENTING_CODE
        submitted: CODE_READY_FOR_REVIEW
        reviewing: REVIEWING_CODE
        approved: CODE_APPROVED
        rejected: CODE_REJECTED
    code-planning-pair:
      doer: code-planner
      reviewer: code-plan-reviewer
      states:
        initial: DRAFT_CODING_PLAN
        executing: CODE_PLANNING
        submitted: CODING_PLAN_TO_REVIEW
        reviewing: REVIEWING_CODING_PLAN
        approved: CODING_PLAN_APPROVED
        rejected: CODING_PLAN_REJECTED
    epic-planning-pair:
      doer: epic-planner
      reviewer: epic-plan-reviewer
      states:
        initial: DRAFT_EPIC
        executing: PLANNING_EPIC
        submitted: EPIC_TO_REVIEW
        reviewing: REVIEWING_EPIC
        approved: EPIC_APPROVED
        rejected: EPIC_REJECTED
    us-writing-pair:
      doer: us-writer
      reviewer: us-reviewer
      states:
        initial: DRAFT_USER_STORIES
        executing: WRITING_USER_STORIES
        submitted: USER_STORIES_TO_REVIEW
        reviewing: REVIEWING_USER_STORIES
        approved: USER_STORIES_APPROVED
        rejected: USER_STORIES_REJECTED

  sub-pipelines:
    coding-subpipeline:
      steps:
        - code-planning-pair
        - coding-pair
      transitions:
        - name: code-plan-to-coding
          from: code-planning-pair.approved
          to: coding-pair.initial
          trigger: manual
          cardinality: per-subtask

  entry-points:
    detailed-spec: coding-subpipeline.code-planning-pair
`)

// minimalPipelineYAML returns a valid pipeline YAML fixture with the current
// roles-only schema. Timeout values can be overridden per-role via overrides.
func minimalPipelineYAML(coderExec, coderPoll, coderMaxWait, orchExec, orchPoll, orchMaxWait string) string {
	return `pipeline:
  roles:
    coder:
      type: doer
      display-name: "Coder"
      timeouts:
        execution: ` + coderExec + `
        poll-interval: ` + coderPoll + `
        max-wait: ` + coderMaxWait + `
    code-reviewer:
      type: reviewer
      display-name: "Code Reviewer"
      timeouts:
        execution: 30m
        poll-interval: 30s
        max-wait: 30m
    orchestrator:
      type: orchestrator
      display-name: "Orchestrator"
      timeouts:
        execution: ` + orchExec + `
        poll-interval: ` + orchPoll + `
        max-wait: ` + orchMaxWait + `
  role-pairs:
    coding-pair:
      doer: coder
      reviewer: code-reviewer
      states:
        initial: DRAFT_CODE
        executing: IMPLEMENTING_CODE
        submitted: CODE_READY_FOR_REVIEW
        reviewing: REVIEWING_CODE
        approved: CODE_APPROVED
        rejected: CODE_REJECTED
  sub-pipelines:
    coding-subpipeline:
      steps:
        - coding-pair
      transitions: []
  entry-points:
    detailed-spec: coding-subpipeline.coding-pair
`
}

// loadTestResolver creates a pipeline resolver from an inline YAML fixture.
func loadTestResolver(t *testing.T, yaml string) *pipeline.Resolver {
	t.Helper()
	cfg, err := pipeline.LoadFromBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("LoadFromBytes failed: %v", err)
	}
	return pipeline.NewResolver(cfg)
}

// applyResolverTimeouts loads timeouts from the resolver for the given role
// and applies them to the strategy.
func applyResolverTimeouts(t *testing.T, s RoleStrategy, r *pipeline.Resolver, role string) {
	t.Helper()
	timeouts, err := r.RoleTimeouts(role)
	if err != nil {
		t.Fatalf("RoleTimeouts(%q) error: %v", role, err)
	}
	ApplyYAMLTimeouts(s, timeouts.Execution, timeouts.PollInterval, timeouts.MaxWait)
}

// TestNewRoleStrategy verifies the factory creates the correct strategy type
// with correct role for all 9 runtime roles.
func TestNewRoleStrategy(t *testing.T) {
	resolver := testResolver(t)

	tests := []struct {
		role     string
		wantType string // "doer", "reviewer", "orchestrator"
	}{
		{"coder", "doer"},
		{"code-planner", "doer"},
		{"epic-planner", "doer"},
		{"us-writer", "doer"},
		{"code-reviewer", "reviewer"},
		{"code-plan-reviewer", "reviewer"},
		{"epic-plan-reviewer", "reviewer"},
		{"us-reviewer", "reviewer"},
		{"orchestrator", "orchestrator"},
	}

	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			s, err := NewRoleStrategy(tt.role, resolver)
			if err != nil {
				t.Fatalf("NewRoleStrategy(%q) error = %v", tt.role, err)
			}

			switch tt.wantType {
			case "doer":
				ds, ok := s.(*doerStrategy)
				if !ok {
					t.Fatalf("expected *doerStrategy, got %T", s)
				}
				if ds.role != tt.role {
					t.Errorf("role = %q, want %q", ds.role, tt.role)
				}
				if ds.resolver == nil {
					t.Error("resolver should not be nil")
				}
			case "reviewer":
				rs, ok := s.(*reviewerStrategy)
				if !ok {
					t.Fatalf("expected *reviewerStrategy, got %T", s)
				}
				if rs.role != tt.role {
					t.Errorf("role = %q, want %q", rs.role, tt.role)
				}
				if rs.resolver == nil {
					t.Error("resolver should not be nil")
				}
			case "orchestrator":
				if _, ok := s.(*orchestratorStrategy); !ok {
					t.Fatalf("expected *orchestratorStrategy, got %T", s)
				}
			}
		})
	}
}

// TestNewRoleStrategy_UnknownRole verifies the factory returns an error for unknown roles.
func TestNewRoleStrategy_UnknownRole(t *testing.T) {
	resolver := testResolver(t)
	_, err := NewRoleStrategy("nonexistent-role", resolver)
	if err == nil {
		t.Fatal("expected error for unknown role, got nil")
	}
}

// TestNewRoleStrategy_CustomYAMLRole verifies that a hypothetical new doer role
// defined in YAML gets *doerStrategy without modifying the NewRoleStrategy switch.
// This uses the real pipeline resolver (not a mock), exercising the production path.
func TestNewRoleStrategy_CustomYAMLRole(t *testing.T) {
	// Pipeline config with a custom "data-engineer" doer role not in contextBuilders map.
	customYAML := []byte(`
pipeline:
  roles:
    coder:
      type: doer
      display-name: "Coder"
    code-reviewer:
      type: reviewer
      display-name: "Code Reviewer"
    data-engineer:
      type: doer
      display-name: "Data Engineer"
    data-reviewer:
      type: reviewer
      display-name: "Data Reviewer"

  role-pairs:
    coding-pair:
      doer: coder
      reviewer: code-reviewer
      states:
        initial: DRAFT_CODE
        executing: IMPLEMENTING_CODE
        submitted: CODE_READY_FOR_REVIEW
        reviewing: REVIEWING_CODE
        approved: CODE_APPROVED
        rejected: CODE_REJECTED
    data-pair:
      doer: data-engineer
      reviewer: data-reviewer
      states:
        initial: DRAFT_DATA
        executing: IMPLEMENTING_DATA
        submitted: DATA_READY_FOR_REVIEW
        reviewing: REVIEWING_DATA
        approved: DATA_APPROVED
        rejected: DATA_REJECTED

  sub-pipelines:
    coding-subpipeline:
      steps:
        - coding-pair
      transitions: []

  entry-points:
    code: coding-subpipeline.coding-pair
`)

	cfg, err := pipeline.LoadFromBytes(customYAML)
	if err != nil {
		t.Fatalf("LoadFromBytes: %v", err)
	}
	resolver := pipeline.NewResolver(cfg)

	// Custom doer role gets *doerStrategy
	s, err := NewRoleStrategy("data-engineer", resolver)
	if err != nil {
		t.Fatalf("NewRoleStrategy(data-engineer) error = %v", err)
	}
	ds, ok := s.(*doerStrategy)
	if !ok {
		t.Fatalf("expected *doerStrategy for custom doer role, got %T", s)
	}
	if ds.role != "data-engineer" {
		t.Errorf("role = %q, want %q", ds.role, "data-engineer")
	}
	// Custom role gets the shared resolver (not nil)
	if ds.resolver == nil {
		t.Error("custom role should have a resolver")
	}

	// Custom reviewer role gets *reviewerStrategy
	s2, err := NewRoleStrategy("data-reviewer", resolver)
	if err != nil {
		t.Fatalf("NewRoleStrategy(data-reviewer) error = %v", err)
	}
	if _, ok := s2.(*reviewerStrategy); !ok {
		t.Fatalf("expected *reviewerStrategy for custom reviewer role, got %T", s2)
	}
}

// TestDefaultTimeout verifies correct timeout per category.
func TestDefaultTimeout(t *testing.T) {
	resolver := testResolver(t)

	tests := []struct {
		role string
		want time.Duration
	}{
		// Type defaults (no YAML applied): doer=2h, reviewer=30m, orchestrator=4h
		{"coder", 2 * time.Hour},
		{"code-planner", 2 * time.Hour},
		{"epic-planner", 2 * time.Hour},
		{"us-writer", 2 * time.Hour},
		{"code-reviewer", 30 * time.Minute},
		{"code-plan-reviewer", 30 * time.Minute},
		{"epic-plan-reviewer", 30 * time.Minute},
		{"us-reviewer", 30 * time.Minute},
		{"orchestrator", 4 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			s, err := NewRoleStrategy(tt.role, resolver)
			if err != nil {
				t.Fatalf("NewRoleStrategy(%q) error = %v", tt.role, err)
			}
			if got := s.DefaultTimeout(); got != tt.want {
				t.Errorf("DefaultTimeout() = %v, want %v", got, tt.want)
			}
		})
	}

	// Verify YAML-sourced timeout overrides the type default.
	// Uses production path: NewRoleStrategy + resolver + ApplyYAMLTimeouts.
	t.Run("yaml_override/coder_1h", func(t *testing.T) {
		yaml := minimalPipelineYAML("1h", "30s", "30m", "4h", "60s", "30m")
		r := loadTestResolver(t, yaml)

		s, err := NewRoleStrategy("coder", r)
		if err != nil {
			t.Fatalf("NewRoleStrategy error: %v", err)
		}
		applyResolverTimeouts(t, s, r, "coder")

		if got := s.DefaultTimeout(); got != 1*time.Hour {
			t.Errorf("DefaultTimeout() = %v, want 1h (from modified YAML)", got)
		}
	})

	t.Run("yaml_override/orchestrator_6h", func(t *testing.T) {
		yaml := minimalPipelineYAML("2h", "30s", "30m", "6h", "60s", "30m")
		r := loadTestResolver(t, yaml)

		s, err := NewRoleStrategy("orchestrator", r)
		if err != nil {
			t.Fatalf("NewRoleStrategy error: %v", err)
		}
		applyResolverTimeouts(t, s, r, "orchestrator")

		if got := s.DefaultTimeout(); got != 6*time.Hour {
			t.Errorf("DefaultTimeout() = %v, want 6h (from modified YAML)", got)
		}
	})

	t.Run("yaml_default/coder_matches_yaml", func(t *testing.T) {
		// Standard YAML values match type defaults — proves the path works even
		// when values happen to equal the type default.
		yaml := minimalPipelineYAML("2h", "30s", "30m", "4h", "60s", "30m")
		r := loadTestResolver(t, yaml)

		s, err := NewRoleStrategy("coder", r)
		if err != nil {
			t.Fatalf("NewRoleStrategy error: %v", err)
		}
		applyResolverTimeouts(t, s, r, "coder")

		if got := s.DefaultTimeout(); got != 2*time.Hour {
			t.Errorf("DefaultTimeout() = %v, want 2h (from YAML)", got)
		}
	})
}

// TestWaitConfig verifies each role resolves the correct config keys and defaults.
func TestWaitConfig(t *testing.T) {
	resolver := testResolver(t)

	tests := []struct {
		role        string
		wantPoll    time.Duration
		wantMaxWait time.Duration
	}{
		// Doer roles use Coder defaults
		{"coder", 30 * time.Second, 7200 * time.Second},
		{"code-planner", 30 * time.Second, 7200 * time.Second},
		{"epic-planner", 30 * time.Second, 7200 * time.Second},
		{"us-writer", 30 * time.Second, 7200 * time.Second},
		// Reviewer roles use Reviewer defaults
		{"code-reviewer", 30 * time.Second, 7200 * time.Second},
		{"code-plan-reviewer", 30 * time.Second, 7200 * time.Second},
		{"epic-plan-reviewer", 30 * time.Second, 7200 * time.Second},
		{"us-reviewer", 30 * time.Second, 7200 * time.Second},
		// Orchestrator uses Orchestrator defaults
		{"orchestrator", 60 * time.Second, 7200 * time.Second},
	}

	zeroState := &models.State{}

	for _, tt := range tests {
		t.Run(tt.role+"/defaults", func(t *testing.T) {
			s, err := NewRoleStrategy(tt.role, resolver)
			if err != nil {
				t.Fatalf("NewRoleStrategy(%q) error = %v", tt.role, err)
			}
			poll, maxWait := s.WaitConfig(zeroState)
			if poll != tt.wantPoll {
				t.Errorf("WaitConfig() poll = %v, want %v", poll, tt.wantPoll)
			}
			if maxWait != tt.wantMaxWait {
				t.Errorf("WaitConfig() maxWait = %v, want %v", maxWait, tt.wantMaxWait)
			}
		})
	}

	// Verify each category reads the correct config keys (not each other's).
	t.Run("custom_config/doer", func(t *testing.T) {
		state := &models.State{Config: models.Config{CoderPollInterval: 5, CoderMaxWait: 60}}
		s, _ := NewRoleStrategy("coder", resolver)
		poll, maxWait := s.WaitConfig(state)
		if poll != 5*time.Second || maxWait != 60*time.Second {
			t.Errorf("doer WaitConfig() = (%v, %v), want (5s, 1m0s)", poll, maxWait)
		}
	})

	t.Run("custom_config/reviewer", func(t *testing.T) {
		state := &models.State{Config: models.Config{ReviewerPollInterval: 10, ReviewerMaxWait: 120}}
		s, _ := NewRoleStrategy("code-reviewer", resolver)
		poll, maxWait := s.WaitConfig(state)
		if poll != 10*time.Second || maxWait != 120*time.Second {
			t.Errorf("reviewer WaitConfig() = (%v, %v), want (10s, 2m0s)", poll, maxWait)
		}
	})

	t.Run("custom_config/orchestrator", func(t *testing.T) {
		state := &models.State{Config: models.Config{OrchestratorPollInterval: 15, OrchestratorMaxWait: 300}}
		s, _ := NewRoleStrategy("orchestrator", resolver)
		poll, maxWait := s.WaitConfig(state)
		if poll != 15*time.Second || maxWait != 300*time.Second {
			t.Errorf("orchestrator WaitConfig() = (%v, %v), want (15s, 5m0s)", poll, maxWait)
		}
	})

	// Cross-contamination: doer config should NOT affect reviewer or orchestrator
	t.Run("custom_config/isolation", func(t *testing.T) {
		state := &models.State{Config: models.Config{CoderPollInterval: 99, CoderMaxWait: 99}}
		reviewer, _ := NewRoleStrategy("code-reviewer", resolver)
		poll, _ := reviewer.WaitConfig(state)
		if poll == 99*time.Second {
			t.Error("reviewer should not read CoderPollInterval")
		}
		orch, _ := NewRoleStrategy("orchestrator", resolver)
		poll, _ = orch.WaitConfig(state)
		if poll == 99*time.Second {
			t.Error("orchestrator should not read CoderPollInterval")
		}
	})

	// Three-level hierarchy: state.yaml > YAML > type default
	t.Run("hierarchy/yaml_overrides_type_default", func(t *testing.T) {
		// YAML sets coder poll=45s, max-wait=20m — differs from type defaults (30s, 30m)
		yaml := minimalPipelineYAML("2h", "45s", "20m", "4h", "60s", "30m")
		r := loadTestResolver(t, yaml)

		s, _ := NewRoleStrategy("coder", r)
		applyResolverTimeouts(t, s, r, "coder")

		zeroState := &models.State{}
		poll, maxWait := s.WaitConfig(zeroState)
		if poll != 45*time.Second {
			t.Errorf("poll = %v, want 45s (from YAML)", poll)
		}
		if maxWait != 20*time.Minute {
			t.Errorf("maxWait = %v, want 20m (from YAML)", maxWait)
		}
	})

	t.Run("hierarchy/state_overrides_yaml", func(t *testing.T) {
		// YAML sets coder poll=45s, max-wait=20m
		yaml := minimalPipelineYAML("2h", "45s", "20m", "4h", "60s", "30m")
		r := loadTestResolver(t, yaml)

		s, _ := NewRoleStrategy("coder", r)
		applyResolverTimeouts(t, s, r, "coder")

		// state.yaml overrides YAML values
		state := &models.State{Config: models.Config{CoderPollInterval: 10, CoderMaxWait: 120}}
		poll, maxWait := s.WaitConfig(state)
		if poll != 10*time.Second {
			t.Errorf("poll = %v, want 10s (state.yaml overrides YAML)", poll)
		}
		if maxWait != 120*time.Second {
			t.Errorf("maxWait = %v, want 2m (state.yaml overrides YAML)", maxWait)
		}
	})

	t.Run("hierarchy/orchestrator_yaml_overrides_type_default", func(t *testing.T) {
		// YAML sets orchestrator poll=90s, max-wait=45m
		yaml := minimalPipelineYAML("2h", "30s", "30m", "4h", "90s", "45m")
		r := loadTestResolver(t, yaml)

		s, _ := NewRoleStrategy("orchestrator", r)
		applyResolverTimeouts(t, s, r, "orchestrator")

		zeroState := &models.State{}
		poll, maxWait := s.WaitConfig(zeroState)
		if poll != 90*time.Second {
			t.Errorf("poll = %v, want 90s (from YAML)", poll)
		}
		if maxWait != 45*time.Minute {
			t.Errorf("maxWait = %v, want 45m (from YAML)", maxWait)
		}
	})
}

// TestDoerPreWork_IsNoOp verifies doer PreWork returns (false, nil).
func TestDoerPreWork_IsNoOp(t *testing.T) {
	resolver := testResolver(t)
	for _, role := range resolver.DoerRoleNames() {
		t.Run(role, func(t *testing.T) {
			s, err := NewRoleStrategy(role, resolver)
			if err != nil {
				t.Fatalf("NewRoleStrategy(%q) error = %v", role, err)
			}
			shouldContinue, err := s.PreWork(context.Background(), nil, SupervisorConfig{})
			if err != nil {
				t.Errorf("PreWork() error = %v", err)
			}
			if shouldContinue {
				t.Error("PreWork() shouldContinue = true, want false")
			}
		})
	}
}

// TestOrchestratorPreWork_NoTrigger verifies orchestrator PreWork is a no-op when
// there is no checkpoint trigger.
func TestOrchestratorPreWork_NoTrigger(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusInProgress
	state.Sprint.CheckpointTrigger = ""
	testhelpers.WriteInitialState(t, statePath, state)

	bb := db.New(statePath)
	resolver := testResolver(t)
	s, err := NewRoleStrategy("orchestrator", resolver)
	if err != nil {
		t.Fatalf("NewRoleStrategy() error = %v", err)
	}
	shouldContinue, err := s.PreWork(context.Background(), bb, SupervisorConfig{ProjectRoot: tmpDir})
	if err != nil {
		t.Errorf("PreWork() error = %v", err)
	}
	if shouldContinue {
		t.Error("PreWork() shouldContinue = true, want false")
	}
}

// TestOrchestratorPreWork_PlanningComplete_NotResumed verifies the gate does NOT fire
// when checkpoint trigger is PLANNING_COMPLETE but sprint is still at CHECKPOINT.
func TestOrchestratorPreWork_PlanningComplete_NotResumed(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusCheckpoint
	state.Sprint.CheckpointTrigger = "PLANNING_COMPLETE"
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now)
	task.RolePair = "code-planning-pair"
	task.Output = []models.OutputEntry{
		{Desc: "implement X", DoneWhen: "tests pass", Scope: "pkg/x"},
	}
	state.Sprint.Scope.Planned = []string{"task-1"}
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, statePath, state)

	bb := db.New(statePath)
	resolver := testResolver(t)
	s, err := NewRoleStrategy("orchestrator", resolver)
	if err != nil {
		t.Fatalf("NewRoleStrategy() error = %v", err)
	}
	shouldContinue, err := s.PreWork(context.Background(), bb, SupervisorConfig{ProjectRoot: tmpDir})
	if err != nil {
		t.Errorf("PreWork() error = %v", err)
	}
	if shouldContinue {
		t.Error("PreWork() shouldContinue = true, want false")
	}
}

// TestOrchestratorPreWork_ManualCheckpoint verifies the gate does NOT fire
// for a resumed manual checkpoint (no trigger). Differs from NoTrigger by
// simulating a checkpoint that was resumed (CheckpointAt set, IN_PROGRESS).
func TestOrchestratorPreWork_ManualCheckpoint(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusInProgress
	state.Sprint.CheckpointTrigger = ""
	state.Sprint.Timeline.CheckpointAt = &now // manual checkpoint was taken and resumed
	testhelpers.WriteInitialState(t, statePath, state)

	bb := db.New(statePath)
	resolver := testResolver(t)
	s, err := NewRoleStrategy("orchestrator", resolver)
	if err != nil {
		t.Fatalf("NewRoleStrategy() error = %v", err)
	}
	shouldContinue, err := s.PreWork(context.Background(), bb, SupervisorConfig{ProjectRoot: tmpDir})
	if err != nil {
		t.Errorf("PreWork() error = %v", err)
	}
	if shouldContinue {
		t.Error("PreWork() shouldContinue = true, want false")
	}
}

// TestOrchestratorPreWork_PlanningComplete_Resumed verifies the gate FIRES when
// checkpoint trigger is PLANNING_COMPLETE, sprint is IN_PROGRESS, and there's
// unconsumed planning output. After PreWork, the trigger should be cleared.
func TestOrchestratorPreWork_PlanningComplete_Resumed(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusInProgress
	state.Sprint.CheckpointTrigger = "PLANNING_COMPLETE"

	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now)
	task.RolePair = "code-planning-pair"
	task.Output = []models.OutputEntry{
		{Desc: "implement X", DoneWhen: "tests pass", Scope: "pkg/x"},
	}
	state.Sprint.Scope.Planned = []string{"task-1"}
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, statePath, state)

	bb := db.New(statePath)
	resolver := testResolver(t)
	s, err := NewRoleStrategy("orchestrator", resolver)
	if err != nil {
		t.Fatalf("NewRoleStrategy() error = %v", err)
	}
	shouldContinue, err := s.PreWork(context.Background(), bb, SupervisorConfig{ProjectRoot: tmpDir})
	if err != nil {
		t.Errorf("PreWork() error = %v", err)
	}
	if shouldContinue {
		t.Error("PreWork() shouldContinue = true, want false")
	}

	// Verify the gate fired: checkpoint_trigger should be cleared
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if readState.Sprint.CheckpointTrigger != "" {
		t.Errorf("CheckpointTrigger = %q after PreWork, want empty (gate should have fired and cleared it)", readState.Sprint.CheckpointTrigger)
	}
}

// TestOrchestratorPreWork_ManyToOneCohort verifies that PreWork calls
// handleAvailableTransitions when a ready many-to-one cohort exists
// but no planning tasks with output.
func TestOrchestratorPreWork_ManyToOneCohort(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusInProgress
	state.Sprint.CheckpointTrigger = models.CheckpointTriggerManyToOneReady

	// Build m2o-ready tasks: MERGED us-writing-pair tasks sharing a parent, no output
	parentID := "epic-1"
	us1 := testhelpers.BuildTaskByStatus("us-1", models.TaskStatusMerged, now)
	us1.RolePair = "us-writing-pair"
	us1.ParentTask = &parentID
	us2 := testhelpers.BuildTaskByStatus("us-2", models.TaskStatusMerged, now)
	us2.RolePair = "us-writing-pair"
	us2.ParentTask = &parentID
	state.Sprint.Scope.Planned = []string{"us-1", "us-2"}
	state.Tasks = []models.Task{us1, us2}
	testhelpers.WriteInitialState(t, statePath, state)

	bb := db.New(statePath)
	resolver := testResolver(t)
	s, err := NewRoleStrategy("orchestrator", resolver)
	if err != nil {
		t.Fatalf("NewRoleStrategy() error = %v", err)
	}
	shouldContinue, err := s.PreWork(context.Background(), bb, SupervisorConfig{ProjectRoot: tmpDir})
	if err != nil {
		t.Errorf("PreWork() error = %v", err)
	}
	if shouldContinue {
		t.Error("PreWork() shouldContinue = true, want false")
	}

	// Verify the gate fired: checkpoint_trigger should be cleared
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if readState.Sprint.CheckpointTrigger != "" {
		t.Errorf("CheckpointTrigger = %q after PreWork, want empty (gate should have fired and cleared it)", readState.Sprint.CheckpointTrigger)
	}
	childID := "epic-1-architecture"
	child := readState.FindTask(childID)
	if child == nil {
		t.Fatalf("child task %q not found", childID)
	}
	if child.RolePair != "architecture-pair" {
		t.Errorf("child role_pair = %q, want architecture-pair", child.RolePair)
	}
}

// TestOrchestratorClaimTask_ReturnsEmpty verifies orchestrator ClaimTask returns ("", "", nil).
func TestOrchestratorClaimTask_ReturnsEmpty(t *testing.T) {
	resolver := testResolver(t)
	s, err := NewRoleStrategy("orchestrator", resolver)
	if err != nil {
		t.Fatalf("NewRoleStrategy() error = %v", err)
	}
	taskID, claimedID, err := s.ClaimTask(SupervisorConfig{}, nil)
	if err != nil {
		t.Errorf("ClaimTask() error = %v", err)
	}
	if taskID != "" || claimedID != "" {
		t.Errorf("ClaimTask() = (%q, %q), want (\"\", \"\")", taskID, claimedID)
	}
}

// TestReviewerPostExecution_IsNoOp verifies reviewer PostExecution returns nil.
func TestReviewerPostExecution_IsNoOp(t *testing.T) {
	resolver := testResolver(t)
	for _, role := range resolver.ReviewerRoleNames() {
		t.Run(role, func(t *testing.T) {
			s, err := NewRoleStrategy(role, resolver)
			if err != nil {
				t.Fatalf("NewRoleStrategy(%q) error = %v", role, err)
			}
			if err := s.PostExecution(nil, SupervisorConfig{}, "", "", nil); err != nil {
				t.Errorf("PostExecution() error = %v", err)
			}
		})
	}
}

// TestDoerPreExecution_IsNoOp verifies doer PreExecution returns nil.
func TestDoerPreExecution_IsNoOp(t *testing.T) {
	resolver := testResolver(t)
	for _, role := range resolver.DoerRoleNames() {
		t.Run(role, func(t *testing.T) {
			s, err := NewRoleStrategy(role, resolver)
			if err != nil {
				t.Fatalf("NewRoleStrategy(%q) error = %v", role, err)
			}
			if err := s.PreExecution(nil, SupervisorConfig{}); err != nil {
				t.Errorf("PreExecution() error = %v", err)
			}
		})
	}
}

// TestReviewerPreExecution_IsNoOp verifies reviewer PreExecution returns nil.
func TestReviewerPreExecution_IsNoOp(t *testing.T) {
	resolver := testResolver(t)
	for _, role := range resolver.ReviewerRoleNames() {
		t.Run(role, func(t *testing.T) {
			s, err := NewRoleStrategy(role, resolver)
			if err != nil {
				t.Fatalf("NewRoleStrategy(%q) error = %v", role, err)
			}
			if err := s.PreExecution(nil, SupervisorConfig{}); err != nil {
				t.Errorf("PreExecution() error = %v", err)
			}
		})
	}
}

// TestReviewerEffectiveMaxRetries verifies the default and override behavior.
func TestReviewerEffectiveMaxRetries(t *testing.T) {
	// Default (maxRetries = 0) → defaultMaxMergeRetries
	rs := &reviewerStrategy{maxRetries: 0}
	if got := rs.effectiveMaxRetries(); got != defaultMaxMergeRetries {
		t.Errorf("effectiveMaxRetries() = %d, want %d", got, defaultMaxMergeRetries)
	}

	// Override
	rs.maxRetries = 5
	if got := rs.effectiveMaxRetries(); got != 5 {
		t.Errorf("effectiveMaxRetries() = %d, want 5", got)
	}
}

// TestDoerPostExecution_NilClaimedTaskID verifies PostExecution is a no-op when
// claimedTaskID is empty (no task was claimed).
func TestDoerPostExecution_NilClaimedTaskID(t *testing.T) {
	resolver := testResolver(t)
	s, err := NewRoleStrategy("coder", resolver)
	if err != nil {
		t.Fatalf("NewRoleStrategy() error = %v", err)
	}
	// Empty claimedTaskID should return nil without touching bb
	if err := s.PostExecution(nil, SupervisorConfig{}, "", "", nil); err != nil {
		t.Errorf("PostExecution() error = %v", err)
	}
}

// TestAllRolesHaveStrategy ensures every role from resolver.AllRoleNames() has a strategy.
func TestAllRolesHaveStrategy(t *testing.T) {
	resolver := testResolver(t)
	for _, role := range resolver.AllRoleNames() {
		t.Run(role, func(t *testing.T) {
			s, err := NewRoleStrategy(role, resolver)
			if err != nil {
				t.Fatalf("NewRoleStrategy(%q) error = %v", role, err)
			}
			if s == nil {
				t.Fatal("strategy should not be nil")
			}
		})
	}
}
