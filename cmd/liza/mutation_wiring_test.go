package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestMutationCommandWiring(t *testing.T) {
	t.Run("config set preserves value unless explicitly replaced", func(t *testing.T) {
		projectRoot, statePath := setupMutationTestProject(t, nil)
		for _, args := range [][]string{
			{"config", "set", "config.post_worktree_cmd", "make setup"},
			{"config", "set", "config.post_worktree_cmd", "make setup"},
			{"config", "set", "config.post_worktree_cmd", "make other", "--replace", "--reason", "Correct bootstrap"},
		} {
			if err := executeRootCommand(t, projectRoot, args...); err != nil {
				t.Fatal(err)
			}
		}
		if err := executeRootCommand(t, projectRoot, "config", "set", "config.post_worktree_cmd", "make third"); err == nil {
			t.Fatal("conflicting set succeeded (or --replace leaked from a prior invocation)")
		}
		state := readState(t, statePath)
		if state.Config.PostWorktreeCmd == nil || *state.Config.PostWorktreeCmd != "make other" {
			t.Fatal("config set did not persist replacement")
		}
	})
	t.Run("claim-task wires positional args to handler", func(t *testing.T) {
		projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
			now := time.Now().UTC()
			state.Tasks = []models.Task{
				testhelpers.BuildTaskByStatus("task-claim-alpha", models.TaskStatusReady, now),
			}
			state.Agents["coder-42"] = mutationTestAgent("coder")
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
			t.Fatalf("agent coder-42 missing")
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
			state.Tasks[0].ReviewCommit = testhelpers.StringPtr(quarantinedVerdictTestCommit)
			state.Agents["code-reviewer-9"] = mutationTestAgent("code-reviewer")
		})

		err := executeRootCommand(t, projectRoot, "submit-verdict", "task-review-flag", "APPROVED", "--review-commit", quarantinedVerdictTestCommit, "--agent-id", "code-reviewer-9")
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
			state.Tasks[0].ReviewCommit = testhelpers.StringPtr(quarantinedVerdictTestCommit)
			state.Agents["code-reviewer-8"] = mutationTestAgent("code-reviewer")
		})

		t.Setenv("LIZA_AGENT_ID", "code-reviewer-8")
		err := executeRootCommand(t, projectRoot, "submit-verdict", "task-review-env", "REJECTED", "needs-work", "--review-commit", quarantinedVerdictTestCommit)
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

	t.Run("submit-verdict --reason flag overrides positional arg", func(t *testing.T) {
		projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
			now := time.Now().UTC()
			state.Tasks = []models.Task{
				testhelpers.BuildTaskByStatus("task-reason-flag", models.TaskStatusReviewing, now),
			}
			state.Tasks[0].ReviewCommit = testhelpers.StringPtr(quarantinedVerdictTestCommit)
			state.Agents["code-reviewer-8"] = mutationTestAgent("code-reviewer")
		})

		// Pass both positional reason and --reason flag; flag should win
		t.Setenv("LIZA_AGENT_ID", "code-reviewer-8")
		err := executeRootCommand(t, projectRoot, "submit-verdict", "task-reason-flag", "REJECTED", "positional-reason", "--review-commit", quarantinedVerdictTestCommit, "--reason", "---\n# Blockers\nArchitecture plan missing")
		if err != nil {
			t.Fatalf("submit-verdict execute failed: %v", err)
		}

		state := readState(t, statePath)
		task := mustFindTask(t, state, "task-reason-flag")
		if task.RejectionReason == nil || *task.RejectionReason != "---\n# Blockers\nArchitecture plan missing" {
			t.Fatalf("rejection_reason = %v, want markdown content from --reason flag", task.RejectionReason)
		}
	})

	t.Run("submit-verdict reads a multiline reason from stdin", func(t *testing.T) {
		projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
			now := time.Now().UTC()
			state.Tasks = []models.Task{
				testhelpers.BuildTaskByStatus("task-reason-stdin", models.TaskStatusReviewing, now),
			}
			state.Tasks[0].ReviewCommit = testhelpers.StringPtr(quarantinedVerdictTestCommit)
			state.Agents["code-reviewer-8"] = mutationTestAgent("code-reviewer")
		})

		const reason = "---\n# Blockers\nArchitecture plan missing\n"
		rootCmd.SetIn(strings.NewReader(reason))
		defer rootCmd.SetIn(nil)
		t.Setenv("LIZA_AGENT_ID", "code-reviewer-8")
		err := executeRootCommand(t, projectRoot,
			"submit-verdict", "task-reason-stdin", "REJECTED",
			"--review-commit", quarantinedVerdictTestCommit, "--reason-file", "-",
		)
		if err != nil {
			t.Fatalf("submit-verdict execute failed: %v", err)
		}

		state := readState(t, statePath)
		task := mustFindTask(t, state, "task-reason-stdin")
		wantReason := strings.TrimSpace(reason)
		if task.RejectionReason == nil {
			t.Fatal("rejection_reason = nil, want stdin content")
		}
		if *task.RejectionReason != wantReason {
			t.Fatalf("rejection_reason = %q, want stdin content %q", *task.RejectionReason, wantReason)
		}
	})

	t.Run("submit-verdict rejects a registered flag consumed as reason", func(t *testing.T) {
		projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
			now := time.Now().UTC()
			state.Tasks = []models.Task{
				testhelpers.BuildTaskByStatus("task-reason-consumed-flag", models.TaskStatusReviewing, now),
			}
		})
		before := readState(t, statePath)
		beforeTask := mustFindTask(t, before, "task-reason-consumed-flag")
		beforeHistoryLen := len(beforeTask.History)

		// Reproduces: --reason $empty --agent-id code-reviewer-8.
		// pflag consumes --agent-id as the reason and leaves its value positional.
		t.Setenv("LIZA_AGENT_ID", "code-reviewer-8")
		err := executeRootCommand(t, projectRoot,
			"submit-verdict", "task-reason-consumed-flag", "REJECTED",
			"--reason", "--agent-id", "code-reviewer-8",
		)
		if err == nil {
			t.Fatal("expected consumed registered flag to be rejected")
		}
		for _, want := range []string{"--reason", "registered flag --agent-id", "empty shell expansion"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("error = %q, want substring %q", err, want)
			}
		}

		after := readState(t, statePath)
		afterTask := mustFindTask(t, after, "task-reason-consumed-flag")
		if afterTask.Status != models.TaskStatusReviewing {
			t.Fatalf("task status = %s, want unchanged %s", afterTask.Status, models.TaskStatusReviewing)
		}
		if afterTask.RejectionReason != nil {
			t.Fatalf("rejection_reason = %v, want nil", afterTask.RejectionReason)
		}
		if len(afterTask.History) != beforeHistoryLen {
			t.Fatalf("history length = %d, want unchanged %d", len(afterTask.History), beforeHistoryLen)
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

	t.Run("supersede-task without replacements requires recoverability command", func(t *testing.T) {
		projectRoot, _ := setupMutationTestProject(t, func(state *models.State) {
			now := time.Now().UTC()
			state.Tasks = []models.Task{
				testhelpers.BuildTaskByStatus("task-supersede-norep", models.TaskStatusBlocked, now),
			}
		})

		err := executeRootCommand(t, projectRoot, "supersede-task", "task-supersede-norep", "--reason", "Work already merged", "--agent-id", "orchestrator-1")
		if err == nil {
			t.Fatal("expected supersede-task without replacements to require recoverability command")
		}
		if !strings.Contains(err.Error(), "recoverability command is required when superseding without replacements") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("supersede-task with replacements rejects recoverability command", func(t *testing.T) {
		projectRoot, _ := setupMutationTestProject(t, func(state *models.State) {
			now := time.Now().UTC()
			state.Tasks = []models.Task{
				testhelpers.BuildTaskByStatus("task-supersede-repl", models.TaskStatusBlocked, now),
			}
		})

		err := executeRootCommand(t, projectRoot, "supersede-task", "task-supersede-repl", "task-new-1", "--reason", "Split into smaller tasks", "--recoverability-command", "liza recover-task task-supersede-repl", "--agent-id", "orchestrator-1")
		if err == nil {
			t.Fatal("expected supersede-task with replacements to reject recoverability command")
		}
		if !strings.Contains(err.Error(), "recoverability command is only valid when superseding without replacements") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("supersede-task without replacements", func(t *testing.T) {
		projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
			now := time.Now().UTC()
			state.Tasks = []models.Task{
				testhelpers.BuildTaskByStatus("task-supersede-norep", models.TaskStatusBlocked, now),
			}
		})

		err := executeRootCommand(t, projectRoot, "supersede-task", "task-supersede-norep", "--reason", "Work already merged", "--recoverability-command", "liza recover-task task-supersede-norep", "--agent-id", "orchestrator-1")
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

	t.Run("retarget-dependency updates one task edge", func(t *testing.T) {
		projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
			now := time.Now().UTC()
			state.Goal.SpecRef = "README.md"
			task := testhelpers.BuildTaskByStatus("task-retarget", models.TaskStatusBlocked, now)
			task.DependsOn = []string{"old-dep"}
			state.Tasks = []models.Task{
				task,
				testhelpers.BuildTaskByStatus("old-dep", models.TaskStatusMerged, now),
				testhelpers.BuildTaskByStatus("new-dep", models.TaskStatusMerged, now),
			}
		})

		err := executeRootCommand(
			t,
			projectRoot,
			"retarget-dependency",
			"task-retarget",
			"old-dep",
			"new-dep",
			"--reason",
			"Correct dependency edge",
			"--agent-id",
			"orchestrator-1",
		)
		if err != nil {
			t.Fatalf("retarget-dependency execute failed: %v", err)
		}

		state := readState(t, statePath)
		task := mustFindTask(t, state, "task-retarget")
		if len(task.DependsOn) != 1 || task.DependsOn[0] != "new-dep" {
			t.Fatalf("task DependsOn = %v, want [new-dep]", task.DependsOn)
		}
		last := task.History[len(task.History)-1]
		if last.Event != models.TaskEventDependenciesRewritten {
			t.Fatalf("history event = %s, want dependencies_rewritten", last.Event)
		}
	})

	t.Run("apply-dependency-repair applies the stored batch", func(t *testing.T) {
		projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
			now := time.Now().UTC()
			state.Goal.SpecRef = "README.md"
			source := testhelpers.BuildTaskByStatus("repair-source", models.TaskStatusBlocked, now)
			source.DependsOn = []string{"old-source"}
			source.RepairRequest = &models.RepairRequest{
				Operation: models.RepairOperationApplyDependencyRepair,
				Target:    "repair-source",
				DependencyUpdates: []models.DependencyUpdate{
					{TaskID: "repair-source", ExpectedDependsOn: []string{"old-source"}, DesiredDependsOn: []string{"new-source"}},
				},
				Evidence:   []string{"command=blocked-operation exit_code=1 stderr=orchestrator repair required"},
				Validation: []string{"validate repaired dependency graph"},
			}
			state.Tasks = []models.Task{
				source,
				testhelpers.BuildTaskByStatus("old-source", models.TaskStatusMerged, now),
				testhelpers.BuildTaskByStatus("new-source", models.TaskStatusMerged, now),
			}
		})

		err := executeRootCommand(
			t,
			projectRoot,
			"apply-dependency-repair",
			"repair-source",
			"--reason",
			"Apply stored graph repair",
			"--agent-id",
			"orchestrator-1",
		)
		if err != nil {
			t.Fatalf("apply-dependency-repair execute failed: %v", err)
		}

		task := mustFindTask(t, readState(t, statePath), "repair-source")
		if len(task.DependsOn) != 1 || task.DependsOn[0] != "new-source" {
			t.Fatalf("task DependsOn = %v, want [new-source]", task.DependsOn)
		}
		if task.Status != models.TaskStatusBlocked || task.RepairRequest != nil {
			t.Fatalf("task status/request = %s/%#v, want BLOCKED/nil", task.Status, task.RepairRequest)
		}
		last := task.History[len(task.History)-1]
		if last.Agent == nil || *last.Agent != "orchestrator-1" {
			t.Fatalf("history agent = %v, want orchestrator-1", last.Agent)
		}
		if last.Reason == nil || *last.Reason != "Apply stored graph repair" {
			t.Fatalf("history reason = %v, want repair reason", last.Reason)
		}
	})

	t.Run("repair-superseded-dependencies", func(t *testing.T) {
		t.Run("wires task reason and caller identity", func(t *testing.T) {
			projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
				now := time.Now().UTC()
				state.Goal.SpecRef = "README.md"
				target := testhelpers.BuildTaskByStatus("plan-old", models.TaskStatusSuperseded, now)
				target.RolePair = "code-planning-pair"
				target.DependsOn = []string{"coding-a", "legal-plan", "coding-b"}
				target.SupersededBy = []string{"replacement-plan"}
				target.RescopeReason = testhelpers.StringPtr("Replaced invalid plan")
				state.Tasks = []models.Task{
					target,
					testhelpers.BuildTaskByStatus("coding-a", models.TaskStatusReady, now),
					testhelpers.BuildTaskByStatus("legal-plan", models.TaskStatusDraftCodingPlan, now),
					testhelpers.BuildTaskByStatus("coding-b", models.TaskStatusReady, now),
					testhelpers.BuildTaskByStatus("replacement-plan", models.TaskStatusDraftCodingPlan, now),
				}
			})

			err := executeRootCommand(
				t,
				projectRoot,
				"repair-superseded-dependencies",
				"plan-old",
				"--reason",
				"Repair terminal dependency metadata",
				"--agent-id",
				"orchestrator-1",
			)
			if err != nil {
				t.Fatalf("repair-superseded-dependencies execute failed: %v", err)
			}

			state := readState(t, statePath)
			task := mustFindTask(t, state, "plan-old")
			if len(task.DependsOn) != 1 || task.DependsOn[0] != "legal-plan" {
				t.Fatalf("task DependsOn = %v, want [legal-plan]", task.DependsOn)
			}
			last := task.History[len(task.History)-1]
			if last.Event != models.TaskEventDependenciesRewritten {
				t.Fatalf("history event = %s, want dependencies_rewritten", last.Event)
			}
			if last.Agent == nil || *last.Agent != "orchestrator-1" {
				t.Fatalf("history agent = %v, want orchestrator-1", last.Agent)
			}
			if last.Reason == nil || *last.Reason != "Repair terminal dependency metadata" {
				t.Fatalf("history reason = %v, want repair reason", last.Reason)
			}
		})

		t.Run("rejects missing arguments without mutation", func(t *testing.T) {
			projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
				now := time.Now().UTC()
				target := testhelpers.BuildTaskByStatus("plan-old", models.TaskStatusSuperseded, now)
				target.RolePair = "code-planning-pair"
				target.DependsOn = []string{"coding-a"}
				state.Tasks = []models.Task{
					target,
					testhelpers.BuildTaskByStatus("coding-a", models.TaskStatusReady, now),
				}
			})

			err := executeRootCommand(
				t,
				projectRoot,
				"repair-superseded-dependencies",
				"--reason",
				"Repair terminal dependency metadata",
				"--agent-id",
				"orchestrator-1",
			)
			if err == nil {
				t.Fatal("expected missing task ID to be rejected")
			}
			if !strings.Contains(err.Error(), "accepts 1 arg(s), received 0") {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := mustFindTask(t, readState(t, statePath), "plan-old").DependsOn; len(got) != 1 || got[0] != "coding-a" {
				t.Fatalf("task DependsOn changed after rejected command: %v", got)
			}
		})

		t.Run("rejects missing reason without mutation", func(t *testing.T) {
			projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
				now := time.Now().UTC()
				target := testhelpers.BuildTaskByStatus("plan-old", models.TaskStatusSuperseded, now)
				target.RolePair = "code-planning-pair"
				target.DependsOn = []string{"coding-a"}
				state.Tasks = []models.Task{
					target,
					testhelpers.BuildTaskByStatus("coding-a", models.TaskStatusReady, now),
				}
			})

			err := executeRootCommand(
				t,
				projectRoot,
				"repair-superseded-dependencies",
				"plan-old",
				"--agent-id",
				"orchestrator-1",
			)
			if err == nil {
				t.Fatal("expected missing reason to be rejected")
			}
			if !strings.Contains(err.Error(), `required flag(s) "reason" not set`) {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := mustFindTask(t, readState(t, statePath), "plan-old").DependsOn; len(got) != 1 || got[0] != "coding-a" {
				t.Fatalf("task DependsOn changed after rejected command: %v", got)
			}
		})
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

	t.Run("add-task accepts role-pair from direct flags", func(t *testing.T) {
		projectRoot, statePath := setupMutationTestProject(t, nil)
		testhelpers.CreateSpecFile(t, projectRoot, "vision.md", "# Vision\n")
		testhelpers.CreateSpecFile(t, projectRoot, "feature.md", "# Feature\n")

		err := executeRootCommand(
			t,
			projectRoot,
			"add-task",
			"--id",
			"planning-task",
			"--desc",
			"Plan the change",
			"--spec",
			"specs/feature.md",
			"--done",
			"Plan reviewed",
			"--scope",
			"internal/ops",
			"--role-pair",
			"code-planning-pair",
			"--agent-id",
			"orchestrator-1",
		)
		if err != nil {
			t.Fatalf("add-task execute failed: %v", err)
		}

		state := readState(t, statePath)
		task := mustFindTask(t, state, "planning-task")
		if task.RolePair != "code-planning-pair" {
			t.Fatalf("task role_pair = %s, want code-planning-pair", task.RolePair)
		}
		if task.Type != models.TaskTypePlanning {
			t.Fatalf("task type = %s, want %s", task.Type, models.TaskTypePlanning)
		}
		if task.Status != models.TaskStatusDraftCodingPlan {
			t.Fatalf("task status = %s, want %s", task.Status, models.TaskStatusDraftCodingPlan)
		}
	})

	t.Run("unblock-task rejects non-orchestrator via allowed-operation RBAC", func(t *testing.T) {
		projectRoot, _ := setupMutationTestProject(t, func(state *models.State) {
			now := time.Now().UTC()
			task := testhelpers.BuildTaskByStatus("task-unblock-rbac", models.TaskStatusBlocked, now)
			task.RolePair = "code-planning-pair"
			state.Tasks = []models.Task{task}
		})

		err := executeRootCommand(
			t,
			projectRoot,
			"unblock-task",
			"task-unblock-rbac",
			"--assign-to",
			"code-planner-1",
			"--reason",
			"repair verified",
			"--agent-id",
			"coder-1",
		)
		if err == nil {
			t.Fatalf("expected RBAC error for coder calling unblock-task, got nil")
		}
		if !strings.Contains(err.Error(), `operation "unblock-task" not allowed for role "coder"`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("unblock-task help distinguishes initial status from claimability", func(t *testing.T) {
		if !strings.Contains(unblockTaskCmd.Long, "initial status") {
			t.Fatalf("unblock-task help = %q, want initial status", unblockTaskCmd.Long)
		}
		if !strings.Contains(unblockTaskCmd.Long, "dependency-held") || !strings.Contains(unblockTaskCmd.Long, "not immediately claimable") {
			t.Fatalf("unblock-task help = %q, want dependency-held claimability distinction", unblockTaskCmd.Long)
		}
	})

	t.Run("set-task-output help documents decomposition root RCA classification", func(t *testing.T) {
		for _, want := range []string{
			"rca_required",
			"decomposition",
			"explicit output value overrides",
			"must provide rca_required",
		} {
			if !strings.Contains(setTaskOutputCmd.Long, want) {
				t.Fatalf("set-task-output help = %q, want %q", setTaskOutputCmd.Long, want)
			}
		}
	})

	t.Run("unblock-task without assign-to restores claimable state", func(t *testing.T) {
		projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
			now := time.Now().UTC()
			task := testhelpers.BuildTaskByStatus("task-unblock-claimable", models.TaskStatusBlocked, now)
			task.RolePair = "code-planning-pair"
			task.Worktree = nil
			task.BaseCommit = nil
			state.Tasks = []models.Task{task}
		})

		err := executeRootCommand(
			t,
			projectRoot,
			"unblock-task",
			"task-unblock-claimable",
			"--reason",
			"repair verified",
			"--agent-id",
			"orchestrator-1",
		)
		if err != nil {
			t.Fatalf("unblock-task execute failed: %v", err)
		}

		state := readState(t, statePath)
		task := mustFindTask(t, state, "task-unblock-claimable")
		if task.Status != models.TaskStatusDraftCodingPlan {
			t.Fatalf("task status = %s, want %s", task.Status, models.TaskStatusDraftCodingPlan)
		}
		if task.AssignedTo != nil {
			t.Fatalf("task assigned_to = %v, want nil", *task.AssignedTo)
		}
	})

	t.Run("unblock-task without assign-to restores dependency-held initial state", func(t *testing.T) {
		projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
			now := time.Now().UTC()
			task := testhelpers.BuildTaskByStatus("task-unblock-dependency-held", models.TaskStatusBlocked, now)
			task.RolePair = "code-planning-pair"
			task.Worktree = nil
			task.BaseCommit = nil
			task.AssignedTo = nil
			task.LeaseExpires = nil
			task.DependsOn = []string{"task-pending-dependency"}
			dependency := testhelpers.BuildTaskByStatus("task-pending-dependency", models.TaskStatusImplementing, now)
			dependency.RolePair = "code-planning-pair"
			state.Tasks = []models.Task{task, dependency}
		})

		err := executeRootCommand(
			t,
			projectRoot,
			"unblock-task",
			"task-unblock-dependency-held",
			"--reason",
			"repair verified",
			"--agent-id",
			"orchestrator-1",
		)
		if err != nil {
			t.Fatalf("unblock-task execute failed: %v", err)
		}

		task := mustFindTask(t, readState(t, statePath), "task-unblock-dependency-held")
		if task.Status != models.TaskStatusDraftCodingPlan {
			t.Fatalf("task status = %s, want %s", task.Status, models.TaskStatusDraftCodingPlan)
		}
		if task.AssignedTo != nil {
			t.Fatalf("task assigned_to = %v, want nil", *task.AssignedTo)
		}
	})

	t.Run("unblock-task with assign-to rejects pending dependency", func(t *testing.T) {
		projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
			now := time.Now().UTC()
			task := testhelpers.BuildTaskByStatus("task-unblock-direct-rejected", models.TaskStatusBlocked, now)
			task.RolePair = "code-planning-pair"
			task.Worktree = nil
			task.BaseCommit = nil
			task.AssignedTo = nil
			task.LeaseExpires = nil
			task.DependsOn = []string{"task-pending-dependency"}
			dependency := testhelpers.BuildTaskByStatus("task-pending-dependency", models.TaskStatusImplementing, now)
			dependency.RolePair = "code-planning-pair"
			state.Tasks = []models.Task{task, dependency}
		})

		err := executeRootCommand(
			t,
			projectRoot,
			"unblock-task",
			"task-unblock-direct-rejected",
			"--assign-to",
			"code-planner-1",
			"--reason",
			"repair verified",
			"--agent-id",
			"orchestrator-1",
		)
		if err == nil {
			t.Fatal("expected --assign-to rejection while dependency is pending")
		}
		if !strings.Contains(err.Error(), "has unmet dependencies") {
			t.Fatalf("unexpected error: %v", err)
		}

		task := mustFindTask(t, readState(t, statePath), "task-unblock-direct-rejected")
		if task.Status != models.TaskStatusBlocked || task.AssignedTo != nil {
			t.Fatalf("rejected task changed: status=%s assigned_to=%v", task.Status, task.AssignedTo)
		}
	})

	t.Run("mark-blocked persists orchestrator repair request", func(t *testing.T) {
		projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
			now := time.Now().UTC()
			state.Tasks = []models.Task{
				testhelpers.BuildTaskByStatus("task-repair-request", models.TaskStatusImplementing, now),
			}
			state.Agents["coder-1"] = mutationTestAgent("coder")
		})

		err := executeRootCommand(
			t,
			projectRoot,
			"mark-blocked",
			"task-repair-request",
			"--agent-id",
			"coder-1",
			"--reason",
			"Required state repair is orchestrator-only",
			"--questions",
			"Can the orchestrator restore the missing parent task?",
			"--repair-operation",
			"add-task",
			"--repair-target",
			"architecture-2",
			"--repair-command",
			"liza add-task --id architecture-2 --agent-id orchestrator-1 --json",
			"--repair-evidence",
			`command=liza add-task --id architecture-2 --agent-id coder-1 --json exit_code=1 stderr=command requires role type [orchestrator] but agent "coder-1" has type "doer"`,
			"--repair-validation",
			"python -m pytest -q tests/backend/test_workflow_contract.py -q",
		)
		if err != nil {
			t.Fatalf("mark-blocked execute failed: %v", err)
		}

		state := readState(t, statePath)
		task := mustFindTask(t, state, "task-repair-request")
		if task.Status != models.TaskStatusBlocked {
			t.Fatalf("task status = %s, want %s", task.Status, models.TaskStatusBlocked)
		}
		if task.RepairRequest == nil {
			t.Fatal("repair_request not persisted")
		}
		if task.RepairRequest.Operation != "add-task" {
			t.Fatalf("repair operation = %q, want add-task", task.RepairRequest.Operation)
		}
		if task.RepairRequest.Target != "architecture-2" {
			t.Fatalf("repair target = %q, want architecture-2", task.RepairRequest.Target)
		}
		if len(task.RepairRequest.Evidence) != 1 {
			t.Fatalf("repair evidence len = %d, want 1", len(task.RepairRequest.Evidence))
		}
	})

	t.Run("mark-blocked persists depends-on", func(t *testing.T) {
		projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
			now := time.Now().UTC()
			state.Tasks = []models.Task{
				testhelpers.BuildTaskByStatus("task-blocked-on-dep", models.TaskStatusImplementing, now),
				testhelpers.BuildTaskByStatus("dep-task", models.TaskStatusImplementing, now),
			}
			state.Agents["coder-1"] = mutationTestAgent("coder")
		})

		err := executeRootCommand(
			t,
			projectRoot,
			"mark-blocked",
			"task-blocked-on-dep",
			"--agent-id",
			"coder-1",
			"--reason",
			"Waiting on dep-task",
			"--questions",
			"Should dep-task merge first?",
			"--depends-on",
			"dep-task",
		)
		if err != nil {
			t.Fatalf("mark-blocked execute failed: %v", err)
		}

		state := readState(t, statePath)
		task := mustFindTask(t, state, "task-blocked-on-dep")
		if len(task.DependsOn) != 1 || task.DependsOn[0] != "dep-task" {
			t.Fatalf("task DependsOn = %v, want [dep-task]", task.DependsOn)
		}
	})

	t.Run("mark-blocked validates incomplete repair request flags", func(t *testing.T) {
		projectRoot, _ := setupMutationTestProject(t, func(state *models.State) {
			now := time.Now().UTC()
			state.Tasks = []models.Task{
				testhelpers.BuildTaskByStatus("task-incomplete-repair-request", models.TaskStatusImplementing, now),
			}
		})

		err := executeRootCommand(
			t,
			projectRoot,
			"mark-blocked",
			"task-incomplete-repair-request",
			"--agent-id",
			"coder-1",
			"--reason",
			"Required state repair is orchestrator-only",
			"--questions",
			"Can the orchestrator restore the missing parent task?",
			"--repair-operation",
			"add-task",
		)
		if err == nil {
			t.Fatal("expected incomplete repair request error, got nil")
		}
		if !strings.Contains(err.Error(), "--repair-target is required when repair request fields are provided") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("update-review-commit uses --changed-by and updates state", func(t *testing.T) {
		// This command is RBAC-exempt (--changed-by, same as release-claim):
		// it is an operator recovery action for rebased worktrees, not an
		// agent workflow command. See specs/goals/20260412-cli-native-access-control.md.
		projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
			now := time.Now().UTC()
			state.Tasks = []models.Task{
				testhelpers.BuildTaskByStatus("task-urc", models.TaskStatusReadyForReview, now),
			}
		})

		// Create a worktree and make a commit so HEAD diverges from review_commit
		g := git.New(projectRoot)
		_, err := g.CreateWorktree("task-urc", "integration")
		if err != nil {
			t.Fatalf("Failed to create worktree: %v", err)
		}
		wtPath := g.GetWorktreePath("task-urc")
		implFile := filepath.Join(wtPath, "feature.go")
		if err := os.WriteFile(implFile, []byte("package feature\n"), 0644); err != nil {
			t.Fatal(err)
		}
		testhelpers.MustGit(t, wtPath, "add", "feature.go")
		testhelpers.MustGit(t, wtPath, "commit", "-m", "diverge")

		// Set stale review_commit and worktree path in state
		staleCommit := testhelpers.MustGit(t, projectRoot, "rev-parse", "integration")
		wtHEAD := testhelpers.MustGit(t, wtPath, "rev-parse", "HEAD")
		expectedBase := testhelpers.MustGit(t, projectRoot, "merge-base", wtHEAD, "integration")
		bb := db.For(statePath)
		if err := bb.Modify(func(s *models.State) error {
			task := s.FindTask("task-urc")
			task.ReviewCommit = &staleCommit
			task.BaseCommit = &staleCommit
			worktreeRel := g.GetWorktreeRelPath("task-urc")
			task.Worktree = &worktreeRel
			return nil
		}); err != nil {
			t.Fatalf("Failed to update state: %v", err)
		}

		err = executeRootCommand(t, projectRoot, "update-review-commit", "task-urc", "--changed-by", "operator-1")
		if err != nil {
			t.Fatalf("update-review-commit execute failed: %v", err)
		}

		state := readState(t, statePath)
		task := mustFindTask(t, state, "task-urc")
		if task.ReviewCommit == nil || *task.ReviewCommit != wtHEAD {
			got := "<nil>"
			if task.ReviewCommit != nil {
				got = *task.ReviewCommit
			}
			t.Fatalf("review_commit = %s, want %s", got, wtHEAD)
		}
		if task.BaseCommit == nil || *task.BaseCommit != expectedBase {
			got := "<nil>"
			if task.BaseCommit != nil {
				got = *task.BaseCommit
			}
			t.Fatalf("base_commit = %s, want %s", got, expectedBase)
		}

		// Verify history entry records the operator
		found := false
		for _, entry := range task.History {
			if entry.Event == models.TaskEventReviewCommitUpdated {
				found = true
				if entry.Agent == nil || *entry.Agent != "operator-1" {
					t.Fatalf("history agent = %v, want operator-1", entry.Agent)
				}
				break
			}
		}
		if !found {
			t.Fatal("expected review_commit_updated history entry")
		}
	})

	t.Run("update-review-commit repairs missing review commit with JSON audit", func(t *testing.T) {
		projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
			now := time.Now().UTC()
			state.Tasks = []models.Task{
				testhelpers.BuildTaskByStatus("task-urc-nil", models.TaskStatusReadyForReview, now),
			}
		})

		g := git.New(projectRoot)
		_, err := g.CreateWorktree("task-urc-nil", "integration")
		if err != nil {
			t.Fatalf("Failed to create worktree: %v", err)
		}
		wtPath := g.GetWorktreePath("task-urc-nil")
		implFile := filepath.Join(wtPath, "feature.go")
		if err := os.WriteFile(implFile, []byte("package feature\n"), 0644); err != nil {
			t.Fatal(err)
		}
		testhelpers.MustGit(t, wtPath, "add", "feature.go")
		testhelpers.MustGit(t, wtPath, "commit", "-m", "repair nil boundary")
		wtHEAD := testhelpers.MustGit(t, wtPath, "rev-parse", "HEAD")

		bb := db.For(statePath)
		if err := bb.Modify(func(s *models.State) error {
			task := s.FindTask("task-urc-nil")
			task.ReviewCommit = nil
			task.BaseCommit = nil
			worktreeRel := g.GetWorktreeRelPath("task-urc-nil")
			task.Worktree = &worktreeRel
			return nil
		}); err != nil {
			t.Fatalf("Failed to update state: %v", err)
		}

		stdout, err := executeRootCommandCapture(t, projectRoot, "update-review-commit", "task-urc-nil", "--changed-by", "operator-1", "--json")
		if err != nil {
			t.Fatalf("update-review-commit execute failed: %v\nstdout:\n%s", err, stdout)
		}

		env := parseEnvelope(t, stdout)
		if env["ok"] != true {
			t.Fatalf("expected ok=true, got %v\nstdout:\n%s", env["ok"], stdout)
		}
		data, ok := env["result"].(map[string]any)
		if !ok {
			t.Fatalf("result = %T, want object: %v", env["result"], env["result"])
		}
		if got := data["old_review_commit"]; got != nil {
			t.Fatalf("old_review_commit = %v, want nil", got)
		}
		if data["new_review_commit"] != wtHEAD {
			t.Fatalf("new_review_commit = %v, want %s", data["new_review_commit"], wtHEAD)
		}

		state := readState(t, statePath)
		task := mustFindTask(t, state, "task-urc-nil")
		if task.ReviewCommit == nil || *task.ReviewCommit != wtHEAD {
			t.Fatalf("review_commit = %v, want %s", task.ReviewCommit, wtHEAD)
		}

		found := false
		for _, entry := range task.History {
			if entry.Event == models.TaskEventReviewCommitUpdated {
				found = true
				if got := entry.Extra["old_review_commit"]; got != nil {
					t.Fatalf("history old_review_commit = %v, want nil", got)
				}
				break
			}
		}
		if !found {
			t.Fatal("expected review_commit_updated history entry")
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

func TestVerdictReasonFileValidation(t *testing.T) {
	t.Run("reads stdin byte-exactly", func(t *testing.T) {
		resetRootCmdForTest(t)
		if err := submitVerdictCmd.Flags().Set("reason-file", "-"); err != nil {
			t.Fatal(err)
		}
		const reason = "# Blocker\n`code` and $literal remain unchanged\n"
		submitVerdictCmd.SetIn(strings.NewReader(reason))
		defer submitVerdictCmd.SetIn(nil)

		got, err := verdictReason(submitVerdictCmd, []string{"task-1", "REJECTED"})
		if err != nil {
			t.Fatal(err)
		}
		if got != reason {
			t.Fatalf("verdictReason() = %q, want exact stdin content %q", got, reason)
		}
	})

	t.Run("rejects another reason source", func(t *testing.T) {
		resetRootCmdForTest(t)
		if err := submitVerdictCmd.Flags().Set("reason-file", "-"); err != nil {
			t.Fatal(err)
		}

		_, err := verdictReason(submitVerdictCmd, []string{"task-1", "REJECTED", "positional"})
		if err == nil || !strings.Contains(err.Error(), "cannot be combined") {
			t.Fatalf("verdictReason() error = %v, want mutually exclusive input error", err)
		}
	})

	t.Run("bounds stdin before passing it to state mutation", func(t *testing.T) {
		resetRootCmdForTest(t)
		if err := submitVerdictCmd.Flags().Set("reason-file", "-"); err != nil {
			t.Fatal(err)
		}
		submitVerdictCmd.SetIn(strings.NewReader(strings.Repeat("x", 4097)))
		defer submitVerdictCmd.SetIn(nil)

		_, err := verdictReason(submitVerdictCmd, []string{"task-1", "REJECTED"})
		if err == nil || !strings.Contains(err.Error(), "exceeds 4096-byte limit") {
			t.Fatalf("verdictReason() error = %v, want bounded-input error", err)
		}
	})
}

func setupMutationTestProject(t *testing.T, mutateState func(*models.State)) (string, string) {
	t.Helper()
	t.Setenv(brand.EnvName("AGENT_GENERATION"), testhelpers.TestAgentGeneration)

	projectRoot := t.TempDir()
	projectRoot, err := filepath.EvalSymlinks(projectRoot)
	if err != nil {
		t.Fatalf("failed to resolve temp project root: %v", err)
	}
	testhelpers.SetupTestGitRepo(t, projectRoot)
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.SetupPipelineConfig(t, projectRoot)

	state := testhelpers.CreateValidState()
	orchestrator := mutationTestAgent("orchestrator")
	expired := time.Unix(0, 0).UTC()
	orchestrator.Heartbeat = expired
	orchestrator.LeaseExpires = &expired
	state.Agents["orchestrator-1"] = orchestrator
	if mutateState != nil {
		mutateState(state)
	}
	testhelpers.WriteInitialState(t, statePath, state)

	return projectRoot, statePath
}

func mutationTestAgent(role string) models.Agent {
	agent := testhelpers.RegisteredTestAgent(role)
	agent.Generation = testhelpers.TestAgentGeneration
	return agent
}

// executeRootCommand runs a CLI command against the given project root.
// NOTE: os.Chdir is process-global state, which prevents t.Parallel() in this
// package. Most tests still use Chdir to exercise default root resolution.
func executeRootCommand(t *testing.T, projectRoot string, args ...string) error {
	t.Helper()
	t.Setenv(brand.EnvName("AGENT_GENERATION"), testhelpers.TestAgentGeneration)

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
