package ops

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// blockPreservedFixture turns the fixture's ready task into one blocked in the
// middle of an iteration, with its preserved worktree intact.
func blockPreservedFixture(t *testing.T, fixture *preservedInitialClaimFixture, iteration int, mutate ...func(*models.Task)) {
	t.Helper()
	state := readClaimStateForTest(t, fixture.stateFile)
	task := state.FindTask(fixture.taskID)
	task.Status = models.TaskStatusBlocked
	task.Iteration = iteration
	reason := "provider quota exhausted mid-iteration"
	task.BlockedReason = &reason
	for _, fn := range mutate {
		fn(task)
	}
	testhelpers.WriteInitialState(t, fixture.stateFile, state)
}

func lastHistoryEntry(t *testing.T, task *models.Task, event string) models.TaskHistoryEntry {
	t.Helper()
	for i := len(task.History) - 1; i >= 0; i-- {
		if task.History[i].Event == event {
			return task.History[i]
		}
	}
	t.Fatalf("task %s has no %s history entry", task.ID, event)
	return models.TaskHistoryEntry{}
}

func countHistoryEntries(task *models.Task, event string) int {
	count := 0
	for _, entry := range task.History {
		if entry.Event == event {
			count++
		}
	}
	return count
}

func lastReceiptIteration(t *testing.T, task *models.Task) int {
	t.Helper()
	if task.Lifecycle == nil || len(task.Lifecycle.Receipts) == 0 {
		t.Fatalf("task %s has no lifecycle receipt", task.ID)
	}
	return task.Lifecycle.Receipts[len(task.Lifecycle.Receipts)-1].Projection.Iteration
}

func TestUnblockTask_DefaultIsContinuationOfTheCurrentIteration(t *testing.T) {
	t.Parallel()
	fixture := newPreservedInitialClaimFixture(t)
	blockPreservedFixture(t, fixture, 2)

	// WHEN the orchestrator unblocks without options
	result, err := UnblockTaskWithOptions(fixture.projectRoot, fixture.taskID, "quota restored", "orchestrator-1", UnblockTaskOptions{})
	if err != nil {
		t.Fatalf("UnblockTaskWithOptions() error: %v", err)
	}

	// THEN the task is claimable by any doer and carries the entitlement
	if result.NewIteration || result.AssignedTo != "" || result.ToStatus != models.TaskStatusReady {
		t.Fatalf("result = %+v, want an unassigned READY continuation", result)
	}
	unblocked := mustReadTask(t, fixture.stateFile, fixture.taskID)
	if !unblocked.Continuation || unblocked.Iteration != 2 {
		t.Fatalf("after unblock: continuation=%v iteration=%d, want true/2", unblocked.Continuation, unblocked.Iteration)
	}
	if got := lastHistoryEntry(t, unblocked, models.TaskEventUnblocked).Extra["new_iteration"]; got != false {
		t.Fatalf("unblocked history new_iteration = %v, want false", got)
	}

	// WHEN a doer claims the preserved work
	if _, err := ClaimTask(fixture.projectRoot, fixture.taskID, fixture.agentID); err != nil {
		t.Fatalf("ClaimTask() error: %v", err)
	}

	// THEN the claim resumes iteration 2 and consumes the entitlement
	claimed := mustReadTask(t, fixture.stateFile, fixture.taskID)
	if claimed.Iteration != 2 || claimed.Continuation {
		t.Fatalf("after claim: iteration=%d continuation=%v, want 2/false", claimed.Iteration, claimed.Continuation)
	}
	if got := lastHistoryEntry(t, claimed, models.TaskEventClaimed).Extra["continuation"]; got != true {
		t.Fatalf("claimed history continuation = %v, want true", got)
	}
	if got := lastReceiptIteration(t, claimed); got != 2 {
		t.Fatalf("claim receipt iteration = %d, want 2", got)
	}
}

func TestUnblockTask_ContinuationDoesNotWaiveExhaustedReviewBudget(t *testing.T) {
	t.Parallel()
	fixture := newPreservedInitialClaimFixture(t)
	wantReason := reviewBudgetExhaustedReason(3, 3)
	blockPreservedFixture(t, fixture, 2, func(task *models.Task) {
		task.Attempt, task.MaxIterations = 2, 2
		task.ReviewCyclesCurrent = 3
		task.BlockedReason = &wantReason
	})
	if err := db.For(fixture.stateFile).Modify(func(state *models.State) error {
		state.Config.MaxReviewCycles = 3
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Default unblock grants a continuation, without resetting the real review
	// budget. A continuation grants no review-budget reset of its own.
	if _, err := UnblockTaskWithOptions(fixture.projectRoot, fixture.taskID, "resume preserved work", "orchestrator-1", UnblockTaskOptions{}); err != nil {
		t.Fatal(err)
	}
	unblocked := mustReadTask(t, fixture.stateFile, fixture.taskID)
	if !unblocked.Continuation || unblocked.Iteration != 2 || unblocked.ReviewCyclesCurrent != 3 {
		t.Fatalf("default unblock changed the budgets: %+v", unblocked)
	}
	if _, err := ClaimTask(fixture.projectRoot, fixture.taskID, fixture.agentID); err == nil || !strings.Contains(err.Error(), wantReason) {
		t.Fatalf("continuation bypassed review cap: %v", err)
	}
	blocked := mustReadTask(t, fixture.stateFile, fixture.taskID)
	if blocked.Status != models.TaskStatusBlocked || blocked.BlockedReason == nil || *blocked.BlockedReason != wantReason || blocked.Iteration != 2 || blocked.ReviewCyclesCurrent != 3 || blocked.AssignedTo != nil {
		t.Fatalf("review-only escalation changed work accounting or granted ownership: %+v", blocked)
	}
}

func TestUnblockTask_NewIterationMakesTheNextClaimCount(t *testing.T) {
	t.Parallel()
	fixture := newPreservedInitialClaimFixture(t)
	blockPreservedFixture(t, fixture, 2)

	result, err := UnblockTaskWithOptions(fixture.projectRoot, fixture.taskID, "product correction", "orchestrator-1", UnblockTaskOptions{NewIteration: true})
	if err != nil {
		t.Fatalf("UnblockTaskWithOptions() error: %v", err)
	}
	if !result.NewIteration {
		t.Fatal("result.NewIteration = false, want true")
	}
	unblocked := mustReadTask(t, fixture.stateFile, fixture.taskID)
	if unblocked.Continuation {
		t.Fatal("continuation set by an unblock with --new-iteration")
	}
	if got := lastHistoryEntry(t, unblocked, models.TaskEventUnblocked).Extra["new_iteration"]; got != true {
		t.Fatalf("unblocked history new_iteration = %v, want true", got)
	}

	if _, err := ClaimTask(fixture.projectRoot, fixture.taskID, fixture.agentID); err != nil {
		t.Fatalf("ClaimTask() error: %v", err)
	}
	claimed := mustReadTask(t, fixture.stateFile, fixture.taskID)
	if claimed.Iteration != 3 {
		t.Fatalf("Iteration after claim = %d, want 3", claimed.Iteration)
	}
	if _, ok := lastHistoryEntry(t, claimed, models.TaskEventClaimed).Extra["continuation"]; ok {
		t.Fatal("a counted claim recorded continuation")
	}
}

// The entitlement continues preserved work only: a claim that starts from a
// fresh worktree, or a task never claimed, starts an iteration regardless.
func TestUnblockTask_ContinuationNeverAppliesToAFreshClaim(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		iteration     int
		wantIteration int
	}{
		{name: "never claimed", iteration: 0, wantIteration: 1},
		{name: "worktree gone", iteration: 2, wantIteration: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tmpDir := t.TempDir()
			testhelpers.SetupTestGitRepo(t, tmpDir)
			testhelpers.SetupPipelineConfig(t, tmpDir)
			stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

			state := testhelpers.CreateValidState()
			task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, time.Now().UTC())
			task.RolePair = "code-planning-pair"
			task.Worktree = nil
			task.BaseCommit = nil
			task.Iteration = tc.iteration
			state.Tasks = []models.Task{task}
			testhelpers.WriteInitialState(t, stateFile, state)

			if _, err := UnblockTaskWithOptions(tmpDir, "task-1", "repair verified", "orchestrator-1", UnblockTaskOptions{}); err != nil {
				t.Fatalf("UnblockTaskWithOptions() error: %v", err)
			}
			bb := db.New(stateFile)
			testhelpers.RegisterTestAgent(t, bb, "code-planner-1", "code-planner")
			setAgentPID(t, bb, "code-planner-1", os.Getpid())
			if _, err := ClaimTask(tmpDir, "task-1", "code-planner-1"); err != nil {
				t.Fatalf("ClaimTask() error: %v", err)
			}

			claimed := mustReadTask(t, stateFile, "task-1")
			if claimed.Iteration != tc.wantIteration || claimed.Continuation {
				t.Fatalf("after fresh claim: iteration=%d continuation=%v, want %d/false", claimed.Iteration, claimed.Continuation, tc.wantIteration)
			}
		})
	}
}

func TestUnblockTask_RefusedClaimLeavesTheContinuationUnconsumed(t *testing.T) {
	t.Parallel()
	fixture := newPreservedInitialClaimFixture(t)
	blockPreservedFixture(t, fixture, 2, func(task *models.Task) {
		task.DependsOn = []string{"dependency-1"}
	})
	state := readClaimStateForTest(t, fixture.stateFile)
	state.Tasks = append(state.Tasks, testhelpers.BuildTaskByStatus("dependency-1", models.TaskStatusReady, time.Now().UTC()))
	testhelpers.WriteInitialState(t, fixture.stateFile, state)

	// GIVEN a continuation restore that is still dependency-held
	if _, err := UnblockTaskWithOptions(fixture.projectRoot, fixture.taskID, "quota restored", "orchestrator-1", UnblockTaskOptions{}); err != nil {
		t.Fatalf("UnblockTaskWithOptions() error: %v", err)
	}

	// WHEN a claim is refused
	if _, err := ClaimTask(fixture.projectRoot, fixture.taskID, fixture.agentID); err == nil || !strings.Contains(err.Error(), "unmet dependencies") {
		t.Fatalf("ClaimTask() error = %v, want unmet dependencies", err)
	}

	// THEN the entitlement survives for the claim that succeeds
	refused := mustReadTask(t, fixture.stateFile, fixture.taskID)
	if !refused.Continuation || refused.Iteration != 2 {
		t.Fatalf("after refused claim: continuation=%v iteration=%d, want true/2", refused.Continuation, refused.Iteration)
	}
	state = readClaimStateForTest(t, fixture.stateFile)
	state.FindTask(fixture.taskID).DependsOn = nil
	testhelpers.WriteInitialState(t, fixture.stateFile, state)
	if _, err := ClaimTask(fixture.projectRoot, fixture.taskID, fixture.agentID); err != nil {
		t.Fatalf("ClaimTask() error: %v", err)
	}
	claimed := mustReadTask(t, fixture.stateFile, fixture.taskID)
	if claimed.Iteration != 2 || claimed.Continuation {
		t.Fatalf("after claim: iteration=%d continuation=%v, want 2/false", claimed.Iteration, claimed.Continuation)
	}
}

func TestUnblockTask_AssignToOnlyPicksTheDoer(t *testing.T) {
	t.Parallel()

	t.Run("default assignment continues the iteration", func(t *testing.T) {
		t.Parallel()
		fixture := newPreservedInitialClaimFixture(t)
		blockPreservedFixture(t, fixture, 2, func(task *models.Task) { task.Continuation = true })

		if _, err := UnblockTaskWithOptions(fixture.projectRoot, fixture.taskID, "quota restored", "orchestrator-1", UnblockTaskOptions{AssignTo: fixture.agentID}); err != nil {
			t.Fatalf("UnblockTaskWithOptions() error: %v", err)
		}
		assigned := mustReadTask(t, fixture.stateFile, fixture.taskID)
		if assigned.Iteration != 2 || assigned.Continuation {
			t.Fatalf("after assign: iteration=%d continuation=%v, want 2/false", assigned.Iteration, assigned.Continuation)
		}
	})

	t.Run("assignment with --new-iteration counts once, replay included", func(t *testing.T) {
		t.Parallel()
		fixture := newPreservedInitialClaimFixture(t)
		blockPreservedFixture(t, fixture, 2)
		opts := UnblockTaskOptions{
			AssignTo:     fixture.agentID,
			NewIteration: true,
			Request:      ownershipRequestOptions(t, db.New(fixture.stateFile), "assign-new-iteration"),
		}

		result, err := UnblockTaskWithOptions(fixture.projectRoot, fixture.taskID, "product correction", "orchestrator-1", opts)
		if err != nil {
			t.Fatalf("UnblockTaskWithOptions() error: %v", err)
		}
		if result.Outcome != models.LifecycleCompleted || !result.NewIteration {
			t.Fatalf("result = %+v, want a completed new-iteration restore", result)
		}
		replayed, err := UnblockTaskWithOptions(fixture.projectRoot, fixture.taskID, "product correction", "orchestrator-1", opts)
		if err != nil || replayed.Outcome != models.LifecycleAlreadyCompleted {
			t.Fatalf("replay = %+v, %v; want ALREADY_COMPLETED", replayed, err)
		}

		assigned := mustReadTask(t, fixture.stateFile, fixture.taskID)
		if assigned.Iteration != 3 {
			t.Fatalf("Iteration = %d, want 3 — counted once across the replay", assigned.Iteration)
		}
		if got := countHistoryEntries(assigned, models.TaskEventUnblocked); got != 1 {
			t.Fatalf("unblocked history entries = %d, want 1", got)
		}
		if got := lastHistoryEntry(t, assigned, models.TaskEventUnblocked).Extra["new_iteration"]; got != true {
			t.Fatalf("unblocked history new_iteration = %v, want true", got)
		}
	})
}

// Fresh recovery discards the preserved work the entitlement was granted for.
func TestUnblockTask_FreshRecoveryInvalidatesTheContinuation(t *testing.T) {
	t.Parallel()
	fixture := newPreservedInitialClaimFixture(t)
	blockPreservedFixture(t, fixture, 2)
	if _, err := UnblockTaskWithOptions(fixture.projectRoot, fixture.taskID, "quota restored", "orchestrator-1", UnblockTaskOptions{}); err != nil {
		t.Fatalf("UnblockTaskWithOptions() error: %v", err)
	}

	if _, err := RecoverTaskWithOptions(fixture.projectRoot, fixture.taskID, "discard preserved work", RecoverTaskOptions{Fresh: true}); err != nil {
		t.Fatalf("RecoverTaskWithOptions(fresh) error: %v", err)
	}
	recovered := mustReadTask(t, fixture.stateFile, fixture.taskID)
	if recovered.Continuation {
		t.Fatal("continuation survived a fresh recovery")
	}

	if _, err := ClaimTask(fixture.projectRoot, fixture.taskID, fixture.agentID); err != nil {
		t.Fatalf("ClaimTask() error: %v", err)
	}
	if claimed := mustReadTask(t, fixture.stateFile, fixture.taskID); claimed.Iteration != 3 {
		t.Fatalf("Iteration after claim = %d, want 3 — a recreated worktree starts an iteration", claimed.Iteration)
	}
}

// Not parallel: it installs the package-level recover-task test hook.
func TestUnblockTask_FailedFreshRecreationInvalidatesTheContinuation(t *testing.T) {
	fixture := newPreservedInitialClaimFixture(t)
	blockPreservedFixture(t, fixture, 2)
	if _, err := UnblockTaskWithOptions(fixture.projectRoot, fixture.taskID, "quota restored", "orchestrator-1", UnblockTaskOptions{}); err != nil {
		t.Fatalf("UnblockTaskWithOptions() error: %v", err)
	}
	testRecoverTaskHooks = &recoverTaskTestHooks{
		beforeFreshCreate: func() {
			if err := os.MkdirAll(fixture.worktreeDir, 0755); err != nil {
				panic(err)
			}
		},
	}
	defer func() { testRecoverTaskHooks = nil }()

	if _, err := RecoverTaskWithOptions(fixture.projectRoot, fixture.taskID, "fresh creation race", RecoverTaskOptions{Fresh: true}); err == nil {
		t.Fatal("RecoverTaskWithOptions(fresh) error = nil, want create failure")
	}
	failed := mustReadTask(t, fixture.stateFile, fixture.taskID)
	if failed.Status != models.TaskStatusBlocked || failed.Continuation {
		t.Fatalf("after failed recreation: status=%s continuation=%v, want BLOCKED/false", failed.Status, failed.Continuation)
	}
}

func TestClearAttemptStateInvalidatesTheContinuation(t *testing.T) {
	t.Parallel()
	for _, profile := range []attemptStateCleanupProfile{
		attemptStateReviewRejection,
		attemptStateClaimReleaseReset,
		attemptStateInitialReset,
		attemptStateRetire,
		attemptStateIntegrationFixClaim,
	} {
		task := models.Task{ID: "task-1", Continuation: true}
		clearAttemptState(&task, profile)
		if task.Continuation {
			t.Errorf("profile %d kept the continuation", profile)
		}
	}
}

func TestUnblockTask_PreserveOnlyRecoveryKeepsTheContinuation(t *testing.T) {
	t.Parallel()
	fixture := newPreservedInitialClaimFixture(t)
	blockPreservedFixture(t, fixture, 2)
	if _, err := UnblockTaskWithOptions(fixture.projectRoot, fixture.taskID, "quota restored", "orchestrator-1", UnblockTaskOptions{}); err != nil {
		t.Fatalf("UnblockTaskWithOptions() error: %v", err)
	}

	if _, err := RecoverTaskWithOptions(fixture.projectRoot, fixture.taskID, "keep preserved work", RecoverTaskOptions{Force: true}); err != nil {
		t.Fatalf("RecoverTaskWithOptions(preserve) error: %v", err)
	}
	if recovered := mustReadTask(t, fixture.stateFile, fixture.taskID); !recovered.Continuation {
		t.Fatal("preserve-only recovery dropped the continuation")
	}

	if _, err := ClaimTask(fixture.projectRoot, fixture.taskID, fixture.agentID); err != nil {
		t.Fatalf("ClaimTask() error: %v", err)
	}
	if claimed := mustReadTask(t, fixture.stateFile, fixture.taskID); claimed.Iteration != 2 {
		t.Fatalf("Iteration after claim = %d, want 2", claimed.Iteration)
	}
}
