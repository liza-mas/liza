package ops

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestLifecycleVerdictConcurrentReplay(t *testing.T) {
	root := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	taskID, actor := "task-1", "code-reviewer-1"
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus(taskID, models.TaskStatusReviewing, time.Now().UTC())}
	reviewCommit := strings.Repeat("a", 40)
	state.Tasks[0].ReviewCommit = &reviewCommit
	authority := models.AgentAuthority{ID: actor, Generation: "review-generation"}
	state.Agents[actor] = models.Agent{Role: models.RoleCodeReviewer, Status: models.AgentStatusReviewing, Generation: authority.Generation, CurrentTask: &taskID}
	bb := testhelpers.WriteInitialState(t, statePath, state)
	current, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	opts := LifecycleRequestOptions{RequestID: "verdict-1", ExpectedTransition: models.TaskTransitionID(current.FindTask(taskID))}
	arrived, release := make(chan struct{}, 2), make(chan struct{})
	previous := testSubmitVerdictHooks
	testSubmitVerdictHooks = &submitVerdictTestHooks{beforeModify: func() { arrived <- struct{}{}; <-release }}
	t.Cleanup(func() { testSubmitVerdictHooks = previous })
	type callResult struct {
		result *VerdictResult
		err    error
	}
	results := make(chan callResult, 2)
	for range 2 {
		go func() {
			r, err := SubmitVerdictWithAuthorityAndOptions(root, taskID, "APPROVED", "", authority, "", reviewCommit, opts)
			results <- callResult{r, err}
		}()
	}
	// The review lock serializes the callers; the second must replay once
	// the first is allowed to commit, without reaching its own mutation hook.
	select {
	case <-arrived:
	case result := <-results:
		close(release)
		t.Fatalf("verdict failed before transaction: %v", result.err)
	case <-time.After(10 * time.Second):
		close(release)
		t.Fatal("verdict did not reach transaction")
	}
	close(release)
	counts := map[string]int{}
	for range 2 {
		call := <-results
		if call.err != nil {
			t.Fatal(call.err)
		}
		counts[call.result.Outcome]++
	}
	if counts[models.LifecycleCompleted] != 1 || counts[models.LifecycleAlreadyCompleted] != 1 {
		t.Fatalf("outcomes = %v", counts)
	}
	after, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(after.FindTask(taskID).Approvals) != 1 {
		t.Fatal("duplicate approval persisted")
	}
	if len(after.Anomalies) != 0 {
		t.Fatal("duplicate produced a failure anomaly")
	}
}

func TestLifecycleMergeReplayAfterCleanup(t *testing.T) {
	root, statePath := setupMergeTestRepo(t, "merge-replay", "coder-1")
	bb := db.New(statePath)
	authority := models.AgentAuthority{ID: "orchestrator-1", Generation: "merge-generation"}
	if err := bb.Modify(func(state *models.State) error {
		state.Agents[authority.ID] = models.Agent{Role: models.RoleOrchestrator, Status: models.AgentStatusIdle, Generation: authority.Generation}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	state, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	opts := LifecycleRequestOptions{RequestID: "merge-1", ExpectedTransition: models.TaskTransitionID(state.FindTask("merge-replay"))}
	first, err := MergeWorktreeWithAuthorityAndOptions(root, "merge-replay", authority, opts)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(statePath)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := MergeWorktreeWithAuthorityAndOptions(root, "merge-replay", authority, opts)
	if err != nil || replay.Outcome != models.LifecycleAlreadyCompleted || replay.MergeCommit != first.MergeCommit || replay.SafeAction != "stop" {
		t.Fatalf("merge replay = %+v, %v", replay, err)
	}
	after, err := os.Stat(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("merge replay rewrote state")
	}
}

func TestLifecycleMergeFailureReportsCommittedBoundary(t *testing.T) {
	root, statePath := setupMergeTestRepo(t, "merge-boundary", "coder-1")
	bb := db.New(statePath)
	mismatch := testhelpers.MustGit(t, root, "rev-parse", "integration")
	if err := bb.Modify(func(state *models.State) error {
		state.FindTask("merge-boundary").ReviewCommit = &mismatch
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_, err := MergeWorktree(root, "merge-boundary", "coder-1")
	var failed *LifecycleError
	if !errors.As(err, &failed) || failed.Outcome.Effects != "committed" ||
		failed.Outcome.Outcome != models.LifecycleStateChanged || failed.Outcome.SafeAction != "requery" {
		t.Fatalf("failure outcome = %v", err)
	}
	state, readErr := bb.Read()
	if readErr != nil {
		t.Fatal(readErr)
	}
	task := state.FindTask("merge-boundary")
	if failed.Outcome.TaskStatus != task.Status || failed.Outcome.TransitionID != models.TaskTransitionID(task) || task.Status != models.TaskStatusIntegrationFailed {
		t.Fatalf("failure reported stale pre-transition state: %+v, current=%+v", failed.Outcome, task)
	}
}

func TestLifecycleBlockedReplayAndConflictingPayload(t *testing.T) {
	root := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, time.Now().UTC())}
	bb := testhelpers.WriteInitialState(t, statePath, state)
	current, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	opts := MarkBlockedOptions{Request: LifecycleRequestOptions{RequestID: "blocked-1", ExpectedTransition: models.TaskTransitionID(current.FindTask("task-1"))}}
	if _, err := MarkBlockedWithOptions(root, "task-1", "needs decision", []string{"Which behavior?"}, "coder-1", opts); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(statePath)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := MarkBlockedWithOptions(root, "task-1", "needs decision", []string{"Which behavior?"}, "coder-1", opts)
	if err != nil || replay.Outcome != models.LifecycleAlreadyCompleted || replay.SafeAction != "stop" {
		t.Fatalf("block replay = %+v, %v", replay, err)
	}
	_, err = MarkBlockedWithOptions(root, "task-1", "different reason", []string{"Which behavior?"}, "coder-1", opts)
	var conflict *LifecycleError
	if !errors.As(err, &conflict) || conflict.Outcome.Outcome != models.LifecycleInvalidInput {
		t.Fatalf("changed payload = %v", err)
	}
	after, err := os.Stat(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("block replay/conflict rewrote state")
	}
}

func TestLifecycleBlockedRetiresOnlyAuthorizedPreparation(t *testing.T) {
	root := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, time.Now().UTC())}
	owner := models.AgentAuthority{ID: "coder-1", Generation: "owner-generation"}
	other := models.AgentAuthority{ID: "coder-2", Generation: "other-generation"}
	for _, authority := range []models.AgentAuthority{owner, other} {
		agent := testhelpers.RegisteredTestAgent("coder")
		agent.Generation = authority.Generation
		state.Agents[authority.ID] = agent
	}
	bb := testhelpers.WriteInitialState(t, statePath, state)
	var unfinished LifecycleRequest
	if err := bb.Modify(func(current *models.State) error {
		task := current.FindTask("task-1")
		var err error
		unfinished, err = NewLifecycleRequest("submit-for-review", task, owner.ID, &owner, LifecycleRequestOptions{
			RequestID: "pending-submission", ExpectedTransition: models.TaskTransitionID(task),
		}, strings.Repeat("a", 40))
		if err != nil {
			return err
		}
		return PrepareLifecycleRequest(task, unfinished, nil)
	}); err != nil {
		t.Fatal(err)
	}
	current, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	opts := MarkBlockedOptions{Request: LifecycleRequestOptions{RequestID: "owner-block", ExpectedTransition: models.TaskTransitionID(current.FindTask("task-1"))}}
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, denied := range []struct {
		name      string
		authority models.AgentAuthority
		dependsOn []string
		outcome   string
	}{
		{name: "wrong owner", authority: other, outcome: models.LifecycleStaleCaller},
		{name: "invalid dependency", authority: owner, dependsOn: []string{"missing-task"}, outcome: models.LifecycleInvalidInput},
	} {
		t.Run(denied.name, func(t *testing.T) {
			deniedOptions := opts
			deniedOptions.DependsOn = denied.dependsOn
			_, err := MarkBlockedWithAuthority(root, "task-1", "needs decision", []string{"Which behavior?"}, denied.authority, deniedOptions)
			var failure *LifecycleError
			if !errors.As(err, &failure) || failure.Outcome.Outcome != denied.outcome {
				t.Fatalf("denied block = %v, want %s", err, denied.outcome)
			}
			after, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("ineligible block changed pending preparation or task state")
			}
		})
	}
	blocked, err := MarkBlockedWithAuthority(root, "task-1", "needs decision", []string{"Which behavior?"}, owner, opts)
	if err != nil || blocked == nil || blocked.Outcome != models.LifecycleCompleted {
		t.Fatalf("authorized block = %+v, %v", blocked, err)
	}
	current, err = bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	task := current.FindTask("task-1")
	if task.Status != models.TaskStatusBlocked || task.AssignedTo != nil || task.Lifecycle == nil || task.Lifecycle.Preparation != nil {
		t.Fatal("authorized block did not retire ownership and unfinished preparation")
	}
	if len(task.Lifecycle.Receipts) != 1 || task.Lifecycle.Receipts[0].Operation != "mark-blocked" || task.Lifecycle.Receipts[0].RequestID != opts.Request.RequestID {
		t.Fatalf("completion receipts = %+v, want only block receipt", task.Lifecycle.Receipts)
	}
	var superseded *LifecycleError
	if err := ValidateLifecyclePreparation(task, unfinished); !errors.As(err, &superseded) || superseded.Outcome.Outcome != models.LifecycleStateChanged || superseded.Outcome.SafeAction != "requery" {
		t.Fatalf("unfinished submission validation = %v, want STATE_CHANGED/requery", err)
	}
	for _, event := range task.History {
		if event.Event == models.TaskEventSubmittedForReview {
			t.Fatal("retiring preparation recorded a successful submission")
		}
	}
}
