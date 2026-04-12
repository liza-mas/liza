package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestMutationCommandWiring(t *testing.T) {
	t.Run("claim-task wires positional args to handler", func(t *testing.T) {
		projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
			now := time.Now().UTC()
			state.Tasks = []models.Task{
				testhelpers.BuildTaskByStatus("task-claim-alpha", models.TaskStatusReady, now),
			}
		})

		err := executeRootCommand(t, projectRoot, "claim-task", "task-claim-alpha", "coder-42")
		if err != nil {
			t.Fatalf("claim-task execute failed: %v", err)
		}

		state := readState(t, statePath)
		task := mustFindTask(t, state, "task-claim-alpha")
		if task.Status != models.TaskStatusImplementing {
			t.Fatalf("task status = %s, want %s", task.Status, models.TaskStatusImplementing)
		}
		if task.AssignedTo == nil || *task.AssignedTo != "coder-42" {
			t.Fatalf("task assigned_to = %v, want coder-42", task.AssignedTo)
		}

		agent, ok := state.Agents["coder-42"]
		if !ok {
			t.Fatalf("agent coder-42 not created")
		}
		if agent.CurrentTask == nil || *agent.CurrentTask != "task-claim-alpha" {
			t.Fatalf("agent current_task = %v, want task-claim-alpha", agent.CurrentTask)
		}
	})

	t.Run("submit-verdict uses --agent-id flag", func(t *testing.T) {
		projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
			now := time.Now().UTC()
			state.Tasks = []models.Task{
				testhelpers.BuildTaskByStatus("task-review-flag", models.TaskStatusReviewing, now),
			}
		})

		err := executeRootCommand(t, projectRoot, "submit-verdict", "task-review-flag", "APPROVED", "--agent-id", "code-reviewer-9")
		if err != nil {
			t.Fatalf("submit-verdict execute failed: %v", err)
		}

		state := readState(t, statePath)
		task := mustFindTask(t, state, "task-review-flag")
		if task.Status != models.TaskStatusApproved {
			t.Fatalf("task status = %s, want %s", task.Status, models.TaskStatusApproved)
		}
		if task.ApprovedBy == nil || *task.ApprovedBy != "code-reviewer-9" {
			t.Fatalf("approved_by = %v, want code-reviewer-9", task.ApprovedBy)
		}
	})

	t.Run("submit-verdict falls back to LIZA_AGENT_ID and forwards reason", func(t *testing.T) {
		projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
			now := time.Now().UTC()
			state.Tasks = []models.Task{
				testhelpers.BuildTaskByStatus("task-review-env", models.TaskStatusReviewing, now),
			}
		})

		t.Setenv("LIZA_AGENT_ID", "code-reviewer-8")
		err := executeRootCommand(t, projectRoot, "submit-verdict", "task-review-env", "REJECTED", "needs-work")
		if err != nil {
			t.Fatalf("submit-verdict execute failed: %v", err)
		}

		state := readState(t, statePath)
		task := mustFindTask(t, state, "task-review-env")
		if task.Status != models.TaskStatusRejected {
			t.Fatalf("task status = %s, want %s", task.Status, models.TaskStatusRejected)
		}
		if task.RejectionReason == nil || *task.RejectionReason != "needs-work" {
			t.Fatalf("rejection_reason = %v, want needs-work", task.RejectionReason)
		}
		if len(task.History) == 0 {
			t.Fatalf("expected history entry for verdict")
		}
		last := task.History[len(task.History)-1]
		if last.Event != "rejected" {
			t.Fatalf("history event = %s, want rejected", last.Event)
		}
		if last.Agent == nil || *last.Agent != "code-reviewer-8" {
			t.Fatalf("history agent = %v, want code-reviewer-8", last.Agent)
		}
		if last.Reason == nil || *last.Reason != "needs-work" {
			t.Fatalf("history reason = %v, want needs-work", last.Reason)
		}
	})

	t.Run("wt-merge routes parsed args to merge handler", func(t *testing.T) {
		projectRoot, _ := setupMutationTestProject(t, func(state *models.State) {
			now := time.Now().UTC()
			state.Tasks = []models.Task{
				testhelpers.BuildTaskByStatus("task-wt-merge", models.TaskStatusReady, now),
			}
		})

		err := executeRootCommand(t, projectRoot, "wt-merge", "task-wt-merge", "--agent-id", "code-reviewer-3")
		if err == nil {
			t.Fatalf("expected wt-merge error, got nil")
		}
		if !strings.Contains(err.Error(), "task must be in an approved state to merge (current status: DRAFT_CODE)") {
			t.Fatalf("unexpected wt-merge error: %v", err)
		}
	})

	t.Run("supersede-task with replacements and --reason flag", func(t *testing.T) {
		projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
			now := time.Now().UTC()
			state.Tasks = []models.Task{
				testhelpers.BuildTaskByStatus("task-supersede-repl", models.TaskStatusBlocked, now),
			}
		})

		err := executeRootCommand(t, projectRoot, "supersede-task", "task-supersede-repl", "task-new-1,task-new-2", "--reason", "Split into smaller tasks", "--agent-id", "orchestrator-1")
		if err != nil {
			t.Fatalf("supersede-task execute failed: %v", err)
		}

		state := readState(t, statePath)
		task := mustFindTask(t, state, "task-supersede-repl")
		if task.Status != models.TaskStatusSuperseded {
			t.Fatalf("task status = %s, want %s", task.Status, models.TaskStatusSuperseded)
		}
		if len(task.SupersededBy) != 2 || task.SupersededBy[0] != "task-new-1" || task.SupersededBy[1] != "task-new-2" {
			t.Fatalf("superseded_by = %v, want [task-new-1 task-new-2]", task.SupersededBy)
		}
		if task.RescopeReason == nil || *task.RescopeReason != "Split into smaller tasks" {
			t.Fatalf("rescope_reason = %v, want 'Split into smaller tasks'", task.RescopeReason)
		}
	})

	t.Run("supersede-task without replacements", func(t *testing.T) {
		projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
			now := time.Now().UTC()
			state.Tasks = []models.Task{
				testhelpers.BuildTaskByStatus("task-supersede-norep", models.TaskStatusBlocked, now),
			}
		})

		err := executeRootCommand(t, projectRoot, "supersede-task", "task-supersede-norep", "--reason", "Work already merged", "--agent-id", "orchestrator-1")
		if err != nil {
			t.Fatalf("supersede-task without replacements failed: %v", err)
		}

		state := readState(t, statePath)
		task := mustFindTask(t, state, "task-supersede-norep")
		if task.Status != models.TaskStatusSuperseded {
			t.Fatalf("task status = %s, want %s", task.Status, models.TaskStatusSuperseded)
		}
		if len(task.SupersededBy) != 0 {
			t.Fatalf("superseded_by = %v, want empty", task.SupersededBy)
		}
		if task.RescopeReason == nil || *task.RescopeReason != "Work already merged" {
			t.Fatalf("rescope_reason = %v, want 'Work already merged'", task.RescopeReason)
		}
	})

	t.Run("handoff rejects code-reviewer agent via RBAC", func(t *testing.T) {
		projectRoot, _ := setupMutationTestProject(t, func(state *models.State) {
			now := time.Now().UTC()
			state.Tasks = []models.Task{
				testhelpers.BuildTaskByStatus("task-handoff-rbac", models.TaskStatusImplementing, now),
			}
		})

		err := executeRootCommand(t, projectRoot, "handoff", "task-handoff-rbac", "summary", "next", "--agent-id", "code-reviewer-1")
		if err == nil {
			t.Fatalf("expected RBAC error for code-reviewer calling handoff, got nil")
		}
		if !strings.Contains(err.Error(), `operation "handoff" not allowed for role "code-reviewer"`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("claim-task rejects orchestrator agent via RBAC", func(t *testing.T) {
		projectRoot, _ := setupMutationTestProject(t, func(state *models.State) {
			now := time.Now().UTC()
			state.Tasks = []models.Task{
				testhelpers.BuildTaskByStatus("task-claim-rbac", models.TaskStatusReady, now),
			}
		})

		err := executeRootCommand(t, projectRoot, "claim-task", "task-claim-rbac", "orchestrator-1")
		if err == nil {
			t.Fatalf("expected RBAC error for orchestrator calling claim-task, got nil")
		}
		if !strings.Contains(err.Error(), "command requires role type [doer]") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("submit-verdict rejects coder agent via RBAC", func(t *testing.T) {
		projectRoot, _ := setupMutationTestProject(t, func(state *models.State) {
			now := time.Now().UTC()
			state.Tasks = []models.Task{
				testhelpers.BuildTaskByStatus("task-verdict-rbac", models.TaskStatusReviewing, now),
			}
		})

		err := executeRootCommand(t, projectRoot, "submit-verdict", "task-verdict-rbac", "APPROVED", "--agent-id", "coder-1")
		if err == nil {
			t.Fatalf("expected RBAC error for coder calling submit-verdict, got nil")
		}
		if !strings.Contains(err.Error(), `operation "submit-verdict" not allowed for role "coder"`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("add-task rejects non-orchestrator via env-var RBAC", func(t *testing.T) {
		projectRoot, _ := setupMutationTestProject(t, nil)

		t.Setenv("LIZA_AGENT_ID", "coder-1")
		err := executeRootCommand(t, projectRoot, "add-task", "--id", "new-task", "--desc", "test", "--spec", "s", "--done", "d", "--scope", "sc")
		if err == nil {
			t.Fatalf("expected RBAC error for coder calling add-task via env-var, got nil")
		}
		if !strings.Contains(err.Error(), "command requires role type [orchestrator]") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("release-claim uses --changed-by over env fallback", func(t *testing.T) {
		projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
			now := time.Now().UTC()
			state.Tasks = []models.Task{
				testhelpers.BuildTaskByStatus("task-release-claim", models.TaskStatusImplementing, now),
			}
		})

		t.Setenv("LIZA_AGENT_ID", "coder-99")
		err := executeRootCommand(t, projectRoot, "release-claim", "task-release-claim", "--role", "doer", "--force", "--changed-by", "auditor-7")
		if err != nil {
			t.Fatalf("release-claim execute failed: %v", err)
		}

		state := readState(t, statePath)
		task := mustFindTask(t, state, "task-release-claim")
		if task.Status != models.TaskStatusReady {
			t.Fatalf("task status = %s, want %s", task.Status, models.TaskStatusReady)
		}
		if len(task.History) == 0 {
			t.Fatalf("expected history entry for released claim")
		}
		last := task.History[len(task.History)-1]
		if last.Event != "doer_claim_released" {
			t.Fatalf("history event = %s, want doer_claim_released", last.Event)
		}
		if last.Agent == nil || *last.Agent != "auditor-7" {
			t.Fatalf("history agent = %v, want auditor-7", last.Agent)
		}
	})
}

func setupMutationTestProject(t *testing.T, mutateState func(*models.State)) (string, string) {
	t.Helper()

	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.SetupPipelineConfig(t, projectRoot)

	state := testhelpers.CreateValidState()
	if mutateState != nil {
		mutateState(state)
	}
	testhelpers.WriteInitialState(t, statePath, state)

	return projectRoot, statePath
}

// executeRootCommand runs a CLI command against the given project root.
// NOTE: os.Chdir is process-global state, which prevents t.Parallel() in this
// package. rootCmd resolves the project root from CWD; until it accepts an
// explicit --project-root flag, Chdir is the only option.
func executeRootCommand(t *testing.T, projectRoot string, args ...string) error {
	t.Helper()

	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	defer func() {
		if chdirErr := os.Chdir(oldDir); chdirErr != nil {
			t.Fatalf("failed to restore working directory: %v", chdirErr)
		}
	}()

	if err := os.Chdir(projectRoot); err != nil {
		t.Fatalf("failed to chdir to project root: %v", err)
	}

	// rootCmd and db singletons are process globals; reset before each command
	// execution so tests don't leak state across runs.
	resetRootCmdForTest(t)
	rootCmd.SetArgs(args)
	return rootCmd.Execute()
}

func readState(t *testing.T, statePath string) *models.State {
	t.Helper()
	state, err := db.For(statePath).Read()
	if err != nil {
		t.Fatalf("failed to read state: %v", err)
	}
	return state
}

func mustFindTask(t *testing.T, state *models.State, taskID string) *models.Task {
	t.Helper()
	task := state.FindTask(taskID)
	if task == nil {
		t.Fatalf("task %s not found", taskID)
	}
	return task
}
