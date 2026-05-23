//go:build e2e

package integration

// full_sprint_test.go contains an end-to-end integration test that exercises
// the full sprint pipeline (epic-planning → US-writing → architecture-main →
// architecture → code-planning → coding) using the real RunSupervisor loop with
// a mock CLI executor.
//
// The SmartMockCLIExecutor replaces the LLM CLI by calling ops.* functions
// directly to simulate what each agent role does, without any real LLM calls.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/db"
	gitpkg "github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/identity"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// ---------------------------------------------------------------------------
// SmartMockCLIExecutor — implements agent.CLIExecutor
// ---------------------------------------------------------------------------

// MockExecution records what the mock did for a single Execute call.
type MockExecution struct {
	AgentID string
	TaskID  string
	Role    string
	Action  string // "doer" or "reviewer"
}

// SmartMockCLIExecutor replaces the real LLM CLI. It reads the blackboard to
// find which task is assigned to the calling agent, then performs the expected
// ops calls (checkpoint, output, commit, submit/verdict) directly.
type SmartMockCLIExecutor struct {
	mu    sync.Mutex
	calls []MockExecution
}

func (m *SmartMockCLIExecutor) Execute(ctx context.Context, cliName, agentID, prompt, projectRoot string, _ []string, _ models.Config) (agent.CLIExecutionResult, error) {
	runtimeRole, err := identity.ExtractRole(agentID)
	if err != nil {
		return agent.CLIExecutionResult{ExitCode: 1}, fmt.Errorf("extract role from %s: %w", agentID, err)
	}

	// Find the task assigned to this agent.
	// Doers use AssignedTo; reviewers use ReviewingBy.
	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())
	state, err := bb.Read()
	if err != nil {
		return agent.CLIExecutionResult{ExitCode: 1}, fmt.Errorf("read state: %w", err)
	}

	// Load pipeline resolver for executing-state checks.
	pr, prErr := ops.LoadResolverForModels(projectRoot)
	if prErr != nil {
		return agent.CLIExecutionResult{ExitCode: 1}, fmt.Errorf("load pipeline resolver: %w", prErr)
	}

	// Load full pipeline resolver for role-type queries.
	pipeCfg, pipeErr := pipeline.LoadFrozen(projectRoot)
	if pipeErr != nil {
		return agent.CLIExecutionResult{ExitCode: 1}, fmt.Errorf("load pipeline config: %w", pipeErr)
	}
	pipeResolver := pipeline.NewResolver(pipeCfg)
	roleType, rtErr := pipeResolver.RoleType(runtimeRole)
	if rtErr != nil {
		return agent.CLIExecutionResult{ExitCode: 1}, fmt.Errorf("resolve role type for %s: %w", runtimeRole, rtErr)
	}

	var taskID string
	var taskRolePair string
	var taskArchRef string
	isReviewer := roleType == "reviewer"
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if isReviewer {
			if task.ReviewingBy != nil && *task.ReviewingBy == agentID {
				taskID = task.ID
				taskRolePair = task.RolePair
				taskArchRef = task.ArchRef
				break
			}
		} else {
			// Only match tasks in an executing state — skip tasks already
			// submitted for review that still have AssignedTo set.
			if task.AssignedTo != nil && *task.AssignedTo == agentID && models.IsExecutingStatus(task, pr) {
				taskID = task.ID
				taskRolePair = task.RolePair
				taskArchRef = task.ArchRef
				break
			}
		}
	}
	if taskID == "" {
		return agent.CLIExecutionResult{ExitCode: 1}, fmt.Errorf("no task assigned to %s (reviewer=%v)", agentID, isReviewer)
	}

	if roleType == "doer" {
		if err := m.executeDoer(ctx, projectRoot, agentID, taskID, runtimeRole, taskRolePair, taskArchRef); err != nil {
			return agent.CLIExecutionResult{ExitCode: 1}, err
		}
	} else if roleType == "reviewer" {
		if err := m.executeReviewer(projectRoot, agentID, taskID, runtimeRole); err != nil {
			return agent.CLIExecutionResult{ExitCode: 1}, err
		}
	} else {
		return agent.CLIExecutionResult{ExitCode: 1}, fmt.Errorf("unsupported role type: %s (role: %s)", roleType, runtimeRole)
	}

	return agent.CLIExecutionResult{ExitCode: 0}, nil
}

func (m *SmartMockCLIExecutor) ExecuteInteractive(_ context.Context, _, _ string, _ []string) (int, error) {
	return 0, fmt.Errorf("interactive mode not supported in mock")
}

// executeDoer simulates what a doer agent (planner or coder) does:
//  1. Write a pre-execution checkpoint
//  2. Set task output (planners only — needed for per-subtask transitions)
//  3. Create a file and commit in the worktree
//  4. Submit the task for review
func (m *SmartMockCLIExecutor) executeDoer(ctx context.Context, projectRoot, agentID, taskID, role, rolePair, archRef string) error {
	// 1. Write checkpoint
	if err := ops.WriteCheckpoint(projectRoot, &ops.WriteCheckpointInput{
		TaskID:         taskID,
		AgentID:        agentID,
		Intent:         fmt.Sprintf("Mock %s work on %s", role, taskID),
		ValidationPlan: "mock validation passes",
		FilesToModify:  []string{fmt.Sprintf("mock-%s.txt", taskID)},
		TDDNotRequired: "integration test mock — no real code changes",
	}); err != nil {
		return fmt.Errorf("WriteCheckpoint: %w", err)
	}

	// 2. Set output for planner roles (needed for per-subtask transitions).
	// epic-planner produces one output[] entry per capability (2 capabilities
	// in this test), each becoming a US Writer child task.
	// code-planner produces one output[] entry per coding task.
	// us-writer uses a one-to-one transition, but setting output is harmless.
	if role == "epic-planner" {
		if err := ops.SetTaskOutput(projectRoot, &ops.SetTaskOutputInput{
			TaskID:  taskID,
			AgentID: agentID,
			Output: []models.OutputEntry{
				{
					Desc:     fmt.Sprintf("Capability 1 from %s", taskID),
					DoneWhen: "Capability 1 stories complete",
					Scope:    "CAP-001 Authentication",
					SpecRef:  "specs/feature.md",
					EpicRef:  "specs/epics/ep-001-auth.md#capability-cap-001---authentication",
				},
				{
					Desc:     fmt.Sprintf("Capability 2 from %s", taskID),
					DoneWhen: "Capability 2 stories complete",
					Scope:    "CAP-002 Authorization",
					SpecRef:  "specs/feature.md",
					EpicRef:  "specs/epics/ep-001-auth.md#capability-cap-002---authorization",
				},
			},
		}); err != nil {
			return fmt.Errorf("SetTaskOutput: %w", err)
		}
	} else if role == "architect" && rolePair == "architecture-main-pair" {
		if err := ops.SetTaskOutput(projectRoot, &ops.SetTaskOutputInput{
			TaskID:  taskID,
			AgentID: agentID,
			Output:  fullSprintMasterArchitectureOutput(),
		}); err != nil {
			return fmt.Errorf("SetTaskOutput: %w", err)
		}
	} else if role == "architect" {
		if err := ops.SetTaskOutput(projectRoot, &ops.SetTaskOutputInput{
			TaskID:  taskID,
			AgentID: agentID,
			Output: []models.OutputEntry{{
				Desc:     fmt.Sprintf("Code plan from %s", taskID),
				DoneWhen: "Implementation plan complete",
				Scope:    "Component architecture",
				SpecRef:  "specs/feature.md",
				ArchRef:  archRef,
			}},
		}); err != nil {
			return fmt.Errorf("SetTaskOutput: %w", err)
		}
	} else if role == "us-writer" ||
		role == "code-planner" {
		if err := ops.SetTaskOutput(projectRoot, &ops.SetTaskOutputInput{
			TaskID:  taskID,
			AgentID: agentID,
			Output: []models.OutputEntry{{
				Desc:     fmt.Sprintf("Child task from %s", taskID),
				DoneWhen: "Implementation complete",
				Scope:    "Full scope",
				SpecRef:  "specs/feature.md",
			}},
		}); err != nil {
			return fmt.Errorf("SetTaskOutput: %w", err)
		}
	}

	// 3. Create a file and commit in the worktree.
	g := gitpkg.New(projectRoot)
	wtPath := g.GetWorktreePath(taskID)

	// Use a unique filename per task to avoid merge conflicts when
	// multiple worktrees merge to the same integration branch.
	filesToAdd := []string{}
	if role == "epic-planner" {
		epicDocRel := filepath.Join("specs", "epics", "ep-001-auth.md")
		epicDoc := filepath.Join(wtPath, epicDocRel)
		if err := os.MkdirAll(filepath.Dir(epicDoc), 0755); err != nil {
			return fmt.Errorf("create epic doc directory: %w", err)
		}
		if err := os.WriteFile(epicDoc, []byte("# Authentication Epic\n"), 0644); err != nil {
			return fmt.Errorf("write epic doc: %w", err)
		}
		filesToAdd = append(filesToAdd, filepath.ToSlash(epicDocRel))
	}
	if role == "architect" && rolePair == "architecture-main-pair" {
		archDocRel := filepath.Join("specs", "arch-plan", "full-sprint-architecture.md")
		archDoc := filepath.Join(wtPath, archDocRel)
		if err := os.MkdirAll(filepath.Dir(archDoc), 0755); err != nil {
			return fmt.Errorf("create architecture doc directory: %w", err)
		}
		if err := os.WriteFile(archDoc, []byte("# Full Sprint Architecture\n"), 0644); err != nil {
			return fmt.Errorf("write architecture doc: %w", err)
		}
		filesToAdd = append(filesToAdd, filepath.ToSlash(archDocRel))
	}

	mockFileName := fmt.Sprintf("mock-%s.txt", taskID)
	mockFile := filepath.Join(wtPath, mockFileName)
	content := fmt.Sprintf("Work by %s on %s at %s\n", agentID, taskID, time.Now().Format(time.RFC3339Nano))
	if err := os.WriteFile(mockFile, []byte(content), 0644); err != nil {
		return fmt.Errorf("write mock file: %w", err)
	}

	filesToAdd = append(filesToAdd, mockFileName)
	gitAddArgs := append([]string{"-C", wtPath, "add"}, filesToAdd...)
	if err := exec.CommandContext(ctx, "git", gitAddArgs...).Run(); err != nil {
		return fmt.Errorf("git add in worktree %s: %w", wtPath, err)
	}
	if err := exec.CommandContext(ctx, "git", "-C", wtPath, "commit", "-m", fmt.Sprintf("Mock work by %s", agentID)).Run(); err != nil {
		return fmt.Errorf("git commit in worktree %s: %w", wtPath, err)
	}

	// 4. Get HEAD SHA and submit for review.
	headOutput, err := exec.CommandContext(ctx, "git", "-C", wtPath, "rev-parse", "HEAD").Output()
	if err != nil {
		return fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	commitSHA := strings.TrimSpace(string(headOutput))

	if _, err := ops.SubmitForReview(projectRoot, taskID, commitSHA, agentID); err != nil {
		return fmt.Errorf("SubmitForReview: %w", err)
	}

	// Record the call for assertions.
	m.mu.Lock()
	m.calls = append(m.calls, MockExecution{
		AgentID: agentID,
		TaskID:  taskID,
		Role:    role,
		Action:  "doer",
	})
	m.mu.Unlock()

	return nil
}

// executeReviewer simulates what a reviewer agent does: approve the task.
func (m *SmartMockCLIExecutor) executeReviewer(projectRoot, agentID, taskID, role string) error {
	if _, err := ops.SubmitVerdict(projectRoot, taskID, "APPROVED", "", agentID, ""); err != nil {
		return fmt.Errorf("SubmitVerdict: %w", err)
	}

	m.mu.Lock()
	m.calls = append(m.calls, MockExecution{
		AgentID: agentID,
		TaskID:  taskID,
		Role:    role,
		Action:  "reviewer",
	})
	m.mu.Unlock()

	return nil
}

func fullSprintMasterArchitectureOutput() []models.OutputEntry {
	return []models.OutputEntry{
		{
			Desc:     "Authentication architecture",
			DoneWhen: "Authentication architecture is approved",
			Scope:    "internal/auth",
			SpecRef:  "specs/feature.md",
			ArchRef:  "specs/arch-plan/full-sprint-architecture.md",
			Decomposition: &models.DecompositionManifest{
				OwnedFiles:         []string{"internal/auth/handler.go"},
				OwnedModules:       []string{"internal/auth"},
				InterfacesOwned:    []string{"auth.Handler"},
				InterfacesConsumed: []string{"storage.Repository"},
				CoverageNotes:      "Owns authentication request handling.",
			},
		},
		{
			Desc:     "Authorization architecture",
			DoneWhen: "Authorization architecture is approved",
			Scope:    "internal/authz",
			SpecRef:  "specs/feature.md",
			ArchRef:  "specs/arch-plan/full-sprint-architecture.md",
			Decomposition: &models.DecompositionManifest{
				OwnedFiles:      []string{"internal/authz/policy.go"},
				OwnedModules:    []string{"internal/authz"},
				InterfacesOwned: []string{"authz.Policy"},
				CoverageNotes:   "Owns authorization policy decisions.",
			},
		},
	}
}

// ---------------------------------------------------------------------------
// TestFullSprintSequence
// ---------------------------------------------------------------------------

// TestFullSprintSequence runs the real RunSupervisor loop through a complete
// pipeline: epic-planning → US-writing → architecture-main → architecture →
// code-planning → coding.
//
// Each supervisor is run sequentially. Each claims one task, the mock executor
// does the work (checkpoint, commit, submit/verdict), the supervisor loops
// back, finds no more work, and exits after a short timeout.
//
// The test verifies:
//   - All 10 tasks reach MERGED status
//   - 9 child tasks are created by pipeline transitions
//   - Each mock CLI was called the expected number of times per role
func TestFullSprintSequence(t *testing.T) {
	// ── Setup ──────────────────────────────────────────────────────────

	// Clean any leftover db singletons from other tests.
	db.ResetInstances()
	t.Cleanup(db.ResetInstances)

	testhelpers.SetupGlobalLiza(t)

	projectDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectDir)

	// Save original CWD — InitCommandWithConfig resolves paths relative to CWD.
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(projectDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalDir) })

	// Create spec file (required by AddTask validation).
	testhelpers.CreateCommittedSpecFileOnIntegration(t, projectDir, "feature.md", "# E2E Sprint Feature\nTest feature for full sprint sequence.")

	// Write the production pipeline.yaml to a temp location so
	// InitCommandWithConfig can read it.
	// go test CWD is internal/integration/; go up one level to reach internal/embedded/.
	pipelineSrc := filepath.Join(originalDir, "..", "embedded", "pipeline.yaml")
	pipelineData, err := os.ReadFile(pipelineSrc)
	if err != nil {
		t.Fatalf("Failed to read pipeline.yaml from %s: %v", pipelineSrc, err)
	}
	pipelineDst := filepath.Join(projectDir, "pipeline.yaml")
	if err := os.WriteFile(pipelineDst, pipelineData, 0644); err != nil {
		t.Fatalf("Failed to write pipeline config: %v", err)
	}

	// Initialize workspace with pipeline config.
	if err := commands.InitCommandWithConfig(commands.InitParams{
		Description: "E2E sprint test",
		SpecRef:     "specs/feature.md",
		ConfigPath:  pipelineDst,
		EntryPoint:  "general-objective",
	}); err != nil {
		t.Fatalf("InitCommandWithConfig failed: %v", err)
	}

	statePath := filepath.Join(projectDir, ".liza", "state.yaml")
	logPath := filepath.Join(projectDir, ".liza", "log.yaml")
	specsDir := filepath.Join(projectDir, "specs")

	// Add the initial epic-planning task.
	if _, err := ops.AddTask(statePath, logPath, &ops.AddTaskInput{
		ID:          "epic-1",
		Description: "Plan the epic feature",
		DoneWhen:    "Epic plan approved",
		Scope:       "Full feature",
		Priority:    1,
		SpecRef:     "specs/feature.md",
		RolePair:    "epic-planning-pair",
	}, "orchestrator-1"); err != nil {
		t.Fatalf("AddTask failed: %v", err)
	}

	// Configure fast poll/wait times for quick test execution.
	bb := db.New(statePath)
	if err := bb.Modify(func(s *models.State) error {
		s.Config.CoderPollInterval = 1
		s.Config.DoerMaxWait = 5
		s.Config.ReviewerPollInterval = 1
		s.Config.ReviewerMaxWait = 5
		s.Config.OrchestratorPollInterval = 1
		s.Config.OrchestratorMaxWait = 3
		s.Config.LeaseDuration = 300 // 5 min — generous for test
		return nil
	}); err != nil {
		t.Fatalf("Failed to set config: %v", err)
	}

	mock := &SmartMockCLIExecutor{}

	// Helper: run a supervisor for a specific agent, blocking until it exits.
	runSupervisor := func(agentID, role string) {
		t.Helper()
		t.Logf("▶ Running supervisor: %s (role: %s)", agentID, role)

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		cfg := agent.SupervisorConfig{
			AgentID:          agentID,
			Role:             role,
			ProjectRoot:      projectDir,
			StatePath:        statePath,
			LogPath:          logPath,
			SpecsDir:         specsDir,
			CLIName:          "claude",
			Executor:         mock,
			ExecutionTimeout: 60 * time.Second,
		}

		if err := agent.RunSupervisor(ctx, cfg); err != nil {
			t.Fatalf("RunSupervisor(%s) failed: %v", agentID, err)
		}
		t.Logf("  ✓ %s exited normally", agentID)
	}

	// simulateOrchestratorTransitions drives the real checkpoint-resume cycle
	// that production uses to fire pipeline transitions after planning work is
	// merged. Transition checkpoints resume directly to IN_PROGRESS and execute
	// available transitions immediately; older completion checkpoints still need
	// a second resume to advance the sprint and fire transitions.
	simulateOrchestratorTransitions := func(phase string) {
		t.Helper()
		t.Logf("▶ Running checkpoint-resume transition cycle (%s)", phase)

		cpResult, err := ops.SprintCheckpoint(projectDir, "")
		if err != nil {
			t.Fatalf("SprintCheckpoint(%s) failed: %v", phase, err)
		}
		t.Logf("  Checkpoint created at %s", cpResult.CheckpointAt.Format(time.RFC3339))

		res1, err := ops.Resume(projectDir, "test-human")
		if err != nil {
			t.Fatalf("Resume[1](%s) failed: %v", phase, err)
		}
		t.Logf("  Resumed from: %s", res1.ResumedFrom)
		if res1.TransitionsExecuted > 0 {
			stateAfterResume, readErr := db.For(statePath).Read()
			if readErr != nil {
				t.Fatalf("Read state after Resume[1](%s) failed: %v", phase, readErr)
			}
			if stateAfterResume.Sprint.Status != models.SprintStatusInProgress {
				t.Fatalf("Resume[1](%s): expected sprint IN_PROGRESS after transition checkpoint, got %s",
					phase, stateAfterResume.Sprint.Status)
			}
			t.Logf("  Transitions executed: %d", res1.TransitionsExecuted)
			return
		}

		stateAfterResume, readErr := db.For(statePath).Read()
		if readErr != nil {
			t.Fatalf("Read state after Resume[1](%s) failed: %v", phase, readErr)
		}
		if stateAfterResume.Sprint.Status != models.SprintStatusCompleted {
			t.Fatalf("Resume[1](%s): expected transitions or COMPLETED sprint, got status %s",
				phase, stateAfterResume.Sprint.Status)
		}

		res2, err := ops.Resume(projectDir, "test-human")
		if err != nil {
			t.Fatalf("Resume[2](%s) failed: %v", phase, err)
		}
		if res2.SprintAdvanced == nil {
			t.Fatalf("Resume[2](%s): expected sprint advance, got nil", phase)
		}
		t.Logf("  Sprint advanced: %s → %s (carried %d tasks, %d transitions)",
			res2.SprintAdvanced.ArchivedSprintID,
			res2.SprintAdvanced.NewSprintID,
			len(res2.SprintAdvanced.CarriedTasks),
			res2.TransitionsExecuted)
	}

	// ── Phase 1: Epic Planning ─────────────────────────────────────────
	t.Log("=== Phase 1: Epic Planning ===")
	runSupervisor("epic-planner-1", "epic-planner")
	runSupervisor("epic-plan-reviewer-1", "epic-plan-reviewer")
	simulateOrchestratorTransitions("epic → US")

	// ── Phase 2: US Writing ────────────────────────────────────────────
	t.Log("=== Phase 2: US Writing ===")
	runSupervisor("us-writer-1", "us-writer")
	runSupervisor("us-reviewer-1", "us-reviewer")
	simulateOrchestratorTransitions("US → architecture")

	// ── Phase 3: Architecture Master ───────────────────────────────────
	t.Log("=== Phase 3: Architecture Master ===")
	runSupervisor("architect-1", "architect")

	registerReviewer := func(agentID, provider string) {
		t.Helper()
		testhelpers.RegisterTestAgent(t, bb, agentID, "architecture-reviewer")
		if err := bb.Modify(func(s *models.State) error {
			agent := s.Agents[agentID]
			agent.Provider = provider
			s.Agents[agentID] = agent
			return nil
		}); err != nil {
			t.Fatalf("set provider for %s failed: %v", agentID, err)
		}
	}
	approveArchitectureMaster := func(agentID string, reviewingStatus models.TaskStatus) {
		t.Helper()
		claimReviewManually(t, bb, "epic-1-architecture", agentID, reviewingStatus)
		if err := commands.SubmitVerdictCommand(projectDir, "epic-1-architecture", "APPROVED", "", agentID, ""); err != nil {
			t.Fatalf("SubmitVerdict architecture master by %s failed: %v", agentID, err)
		}
	}

	registerReviewer("architecture-reviewer-1", "codex")
	approveArchitectureMaster("architecture-reviewer-1", models.TaskStatus("REVIEWING_ARCHITECTURE_MAIN"))
	stateAfterFirstArchitectureReview, err := bb.Read()
	if err != nil {
		t.Fatalf("read after first architecture master approval failed: %v", err)
	}
	if task := stateAfterFirstArchitectureReview.FindTask("epic-1-architecture"); task == nil || task.Status != models.TaskStatus("ARCHITECTURE_MAIN_PARTIALLY_APPROVED") {
		t.Fatalf("first architecture master approval status = %v, want ARCHITECTURE_MAIN_PARTIALLY_APPROVED", task)
	}

	registerReviewer("architecture-reviewer-2", "test")
	approveArchitectureMaster("architecture-reviewer-2", models.TaskStatus("REVIEWING_ARCHITECTURE_MAIN_2"))
	os.Setenv("LIZA_AGENT_ID", "architecture-reviewer-2")
	if err := commands.WtMergeCommand(projectDir, "epic-1-architecture", "architecture-reviewer-2"); err != nil {
		t.Fatalf("WtMerge architecture master failed: %v", err)
	}
	os.Unsetenv("LIZA_AGENT_ID")
	simulateOrchestratorTransitions("architecture master → specialized architecture")

	// ── Phase 4: Architecture ──────────────────────────────────────────
	t.Log("=== Phase 4: Architecture ===")
	runSupervisor("architect-2", "architect")
	runSupervisor("architecture-reviewer-3", "architecture-reviewer")
	simulateOrchestratorTransitions("architecture → code-planning")

	// ── Phase 5: Code Planning ─────────────────────────────────────────
	t.Log("=== Phase 5: Code Planning ===")
	runSupervisor("code-planner-1", "code-planner")
	runSupervisor("code-plan-reviewer-1", "code-plan-reviewer")
	simulateOrchestratorTransitions("code-planning → coding")

	// ── Phase 6: Coding ────────────────────────────────────────────────
	t.Log("=== Phase 6: Coding ===")
	runSupervisor("coder-1", "coder")
	runSupervisor("code-reviewer-1", "code-reviewer")

	// ── Assertions ─────────────────────────────────────────────────────
	t.Log("=== Assertions ===")

	state, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}

	// Log all tasks for debugging.
	for _, task := range state.Tasks {
		t.Logf("  Task %-55s  status=%-25s  role_pair=%s", task.ID, task.Status, task.RolePair)
	}

	// Expected tasks (1 original + 9 created by transitions):
	//   epic-1                                                                  (epic-planning-pair)
	//   epic-1-us-writing-0                                                     (us-writing-pair, CAP-001)
	//   epic-1-us-writing-1                                                     (us-writing-pair, CAP-002)
	//   epic-1-architecture                                                     (architecture-main-pair)
	//   epic-1-architecture-architecture-0                                      (architecture-pair)
	//   epic-1-architecture-architecture-1                                      (architecture-pair)
	//   epic-1-architecture-architecture-0-code-planning-0                      (code-planning-pair)
	//   epic-1-architecture-architecture-1-code-planning-0                      (code-planning-pair)
	//   epic-1-architecture-architecture-0-code-planning-0-coding-0             (coding-pair)
	//   epic-1-architecture-architecture-1-code-planning-0-coding-0             (coding-pair)
	if len(state.Tasks) != 10 {
		t.Errorf("Expected 10 tasks, got %d", len(state.Tasks))
	}

	// All tasks should be MERGED.
	mergedCount := 0
	for _, task := range state.Tasks {
		if task.Status == models.TaskStatusMerged {
			mergedCount++
		}
	}
	if mergedCount != 10 {
		t.Errorf("Expected 10 MERGED tasks, got %d", mergedCount)
	}

	// Verify transitions_executed on source tasks.
	assertTransitionExecuted(t, state, "epic-1", "epic-to-us")

	// Both US tasks should have us-to-coding executed (many-to-one).
	for _, suffix := range []string{"0", "1"} {
		usTaskID := "epic-1-us-writing-" + suffix
		assertTransitionExecuted(t, state, usTaskID, "us-to-coding")
	}

	// Architecture master requires quorum 2 and auto-decomposes to specialized architecture tasks.
	archMasterTaskID := "epic-1-architecture"
	archMasterTask := state.FindTask(archMasterTaskID)
	if archMasterTask == nil {
		t.Fatalf("architecture master task %s not found", archMasterTaskID)
	}
	if archMasterTask.RolePair != "architecture-main-pair" {
		t.Errorf("Architecture master role_pair = %q, want architecture-main-pair", archMasterTask.RolePair)
	}
	if len(archMasterTask.Approvals) != 2 {
		t.Errorf("Architecture master approvals = %d, want 2", len(archMasterTask.Approvals))
	}
	assertTransitionExecuted(t, state, archMasterTaskID, "arch-decompose")

	// Specialized architecture tasks bypass code-planning-main-pair and create code-planning-pair tasks.
	for _, suffix := range []string{"0", "1"} {
		archTaskID := archMasterTaskID + "-architecture-" + suffix
		archTask := state.FindTask(archTaskID)
		if archTask == nil {
			t.Fatalf("specialized architecture task %s not found", archTaskID)
		}
		if archTask.RolePair != "architecture-pair" {
			t.Errorf("Specialized architecture task %s role_pair = %q, want architecture-pair", archTaskID, archTask.RolePair)
		}
		assertTransitionExecuted(t, state, archTaskID, "architecture-to-code-plan")
	}

	// Each code-planning task fans out to a coding task.
	for _, suffix := range []string{"0", "1"} {
		codePlanTaskID := archMasterTaskID + "-architecture-" + suffix + "-code-planning-0"
		assertTransitionExecuted(t, state, codePlanTaskID, "code-plan-to-coding")
	}

	// Verify capability scoping: each US task has the right scope, spec_ref (goal spec),
	// and epic_ref (epic document with section anchor) from output[].
	usTask0 := state.FindTask("epic-1-us-writing-0")
	usTask1 := state.FindTask("epic-1-us-writing-1")
	if usTask0 != nil {
		if usTask0.Scope != "CAP-001 Authentication" {
			t.Errorf("US task 0 scope = %q, want %q", usTask0.Scope, "CAP-001 Authentication")
		}
		if usTask0.SpecRef != "specs/feature.md" {
			t.Errorf("US task 0 spec_ref = %q, want %q", usTask0.SpecRef, "specs/feature.md")
		}
		if usTask0.EpicRef != "specs/epics/ep-001-auth.md#capability-cap-001---authentication" {
			t.Errorf("US task 0 epic_ref = %q, want %q", usTask0.EpicRef, "specs/epics/ep-001-auth.md#capability-cap-001---authentication")
		}
	}
	if usTask1 != nil {
		if usTask1.Scope != "CAP-002 Authorization" {
			t.Errorf("US task 1 scope = %q, want %q", usTask1.Scope, "CAP-002 Authorization")
		}
		if usTask1.SpecRef != "specs/feature.md" {
			t.Errorf("US task 1 spec_ref = %q, want %q", usTask1.SpecRef, "specs/feature.md")
		}
		if usTask1.EpicRef != "specs/epics/ep-001-auth.md#capability-cap-002---authorization" {
			t.Errorf("US task 1 epic_ref = %q, want %q", usTask1.EpicRef, "specs/epics/ep-001-auth.md#capability-cap-002---authorization")
		}
	}

	// Verify EpicRef propagation across many-to-one and master decomposition boundaries.
	if archMasterTask != nil {
		wantEpicRef := "specs/epics/ep-001-auth.md"
		if archMasterTask.EpicRef != wantEpicRef {
			t.Errorf("Architecture master epic_ref = %q, want %q (section should be stripped at many-to-one)", archMasterTask.EpicRef, wantEpicRef)
		}
	}

	// Verify EpicRef and ArchRef propagate to specialized architecture, code-planning, and coding tasks.
	wantArchRef := "specs/arch-plan/full-sprint-architecture.md"
	for _, suffix := range []string{"0", "1"} {
		archTaskID := archMasterTaskID + "-architecture-" + suffix
		archTask := state.FindTask(archTaskID)
		if archTask != nil {
			wantEpicRef := "specs/epics/ep-001-auth.md"
			if archTask.EpicRef != wantEpicRef {
				t.Errorf("Architecture task %s epic_ref = %q, want %q", archTask.ID, archTask.EpicRef, wantEpicRef)
			}
			if archTask.ArchRef != wantArchRef {
				t.Errorf("Architecture task %s arch_ref = %q, want %q", archTask.ID, archTask.ArchRef, wantArchRef)
			}
			if archTask.Decomposition == nil {
				t.Errorf("Architecture task %s decomposition is nil", archTask.ID)
			}
		}

		cpTask := state.FindTask(archTaskID + "-code-planning-0")
		if cpTask != nil {
			wantEpicRef := "specs/epics/ep-001-auth.md"
			if cpTask.EpicRef != wantEpicRef {
				t.Errorf("Code-plan task %s epic_ref = %q, want %q", cpTask.ID, cpTask.EpicRef, wantEpicRef)
			}
			if cpTask.RolePair != "code-planning-pair" {
				t.Errorf("Code-plan task %s role_pair = %q, want code-planning-pair", cpTask.ID, cpTask.RolePair)
			}
			if cpTask.ArchRef != wantArchRef {
				t.Errorf("Code-plan task %s arch_ref = %q, want %q", cpTask.ID, cpTask.ArchRef, wantArchRef)
			}
		}

		codingTask := state.FindTask(archTaskID + "-code-planning-0-coding-0")
		if codingTask != nil && codingTask.ArchRef != wantArchRef {
			t.Errorf("Coding task %s arch_ref = %q, want %q", codingTask.ID, codingTask.ArchRef, wantArchRef)
		}
	}

	for _, task := range state.Tasks {
		if task.RolePair == "code-planning-main-pair" {
			t.Fatalf("unexpected code-planning-main-pair task created: %s", task.ID)
		}
	}

	// Verify mock call count and role coverage.
	mock.mu.Lock()
	defer mock.mu.Unlock()

	// 2 (epic) + 4 (US x2) + 3 (architecture doers) +
	// 2 (specialized architecture reviewers) + 4 (code-plan x2) + 4 (coding x2) = 19.
	if len(mock.calls) != 19 {
		t.Errorf("Expected 19 mock calls, got %d", len(mock.calls))
		for i, call := range mock.calls {
			t.Logf("  Call %d: %s (%s) on %s [%s]", i, call.AgentID, call.Role, call.TaskID, call.Action)
		}
	}

	// Epic roles called once; architecture includes one master doer plus two
	// specialized architecture doer/reviewer calls; downstream roles run twice.
	expectedRoleCounts := map[string]int{
		"epic-planner":          1,
		"epic-plan-reviewer":    1,
		"us-writer":             2,
		"us-reviewer":           2,
		"architect":             3,
		"architecture-reviewer": 2,
		"code-planner":          2,
		"code-plan-reviewer":    2,
		"coder":                 2,
		"code-reviewer":         2,
	}
	roleCounts := make(map[string]int)
	for _, call := range mock.calls {
		roleCounts[call.Role]++
	}
	for role, want := range expectedRoleCounts {
		if roleCounts[role] != want {
			t.Errorf("Expected role %s called %d times, got %d", role, want, roleCounts[role])
		}
	}
}

// assertTransitionExecuted verifies that the given task has the named
// transition in its transitions_executed map.
func assertTransitionExecuted(t *testing.T, state *models.State, taskID, transitionName string) {
	t.Helper()
	task := state.FindTask(taskID)
	if task == nil {
		t.Errorf("task %s not found", taskID)
		return
	}
	if !task.TransitionsExecuted[transitionName] {
		t.Errorf("task %s missing transition %s in transitions_executed", taskID, transitionName)
	}
}
