package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// TestReviewerWaitForWorkWakesForInheritedMerge is the D106 wake half: an idle
// reviewer with no review work becomes the owner of an approved, unmerged task
// when the approver that owned the merge leaves the registry. Merges run only
// in PreWork, so the normal wait must end for that inherited merge instead of
// parking for reviewer_max_wait while downstream work waits on it.
func TestReviewerWaitForWorkWakesForInheritedMerge(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	pr, err := ops.LoadResolverForModels(tmpDir)
	if err != nil {
		t.Fatalf("LoadResolverForModels() error = %v", err)
	}

	const approver = "code-reviewer-1"
	const inheritor = "code-reviewer-2"
	tasks := []models.Task{{
		ID:        "approved-unmerged",
		Status:    models.TaskStatusApproved,
		RolePair:  "coding-pair",
		Approvals: []models.Approval{{Agent: approver, Provider: "claude"}},
	}}
	state := testhelpers.CreateValidState()
	state.Tasks = tasks
	registerEligibleMergeTestAgents(state, tasks, inheritor, pr)
	testhelpers.WriteInitialState(t, statePath, state)

	bb := db.New(statePath)
	config := SupervisorConfig{
		AgentID:     inheritor,
		Role:        models.RoleCodeReviewer,
		ProjectRoot: tmpDir,
		Authority:   testSupervisorAuthority(t, bb, inheritor),
	}
	strategy, err := NewRoleStrategy("code-reviewer", testResolver(t))
	if err != nil {
		t.Fatalf("NewRoleStrategy() error = %v", err)
	}
	if hasPendingMerges(bb, inheritor, pr) {
		t.Fatal("fixture: the inheritor owns the merge before the approver leaves")
	}

	// GIVEN the inheritor is parked in its normal wait: the watcher factory
	// signals once the initial no-work check has run and the real watcher is
	// installed, so the mutation below lands after that check. The fallback
	// tick is shortened well below maxWait so a missed fsnotify event cannot
	// run the wait to its deadline.
	withAbortTickInterval(t, 200*time.Millisecond)
	watcherReady := make(chan struct{})
	originalWatcher := newStateWatcher
	newStateWatcher = func(bb *db.Blackboard) (stateWatcher, error) {
		watcher, err := originalWatcher(bb)
		close(watcherReady)
		return watcher, err
	}
	t.Cleanup(func() { newStateWatcher = originalWatcher })

	const maxWait = 5 * time.Second
	type waitResult struct {
		hasWork bool
		err     error
		elapsed time.Duration
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan waitResult, 1)
	start := time.Now()
	go func() {
		hasWork, err := strategy.WaitForWork(ctx, bb, config, 50*time.Millisecond, maxWait)
		done <- waitResult{hasWork, err, time.Since(start)}
	}()
	select {
	case <-watcherReady:
	case result := <-done:
		t.Fatalf("WaitForWork() returned before parking: %+v", result)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for the reviewer to park")
	}

	// WHEN the approver that owned the merge leaves the registry
	if err := bb.Modify(func(state *models.State) error {
		delete(state.Agents, approver)
		return nil
	}); err != nil {
		t.Fatalf("unregister approver: %v", err)
	}
	if !hasPendingMerges(bb, inheritor, pr) {
		t.Fatal("fixture: the inheritor does not own the merge after the approver left")
	}

	// THEN the inheritor's wait ends for the merge it now owns
	result := <-done
	if result.err != nil {
		t.Fatalf("WaitForWork() error = %v", result.err)
	}
	if !result.hasWork {
		t.Fatalf("WaitForWork() = no work after %v, want a wake for the inherited merge", result.elapsed)
	}
	if result.elapsed >= maxWait {
		t.Fatalf("WaitForWork() woke after %v, want well before maxWait %v", result.elapsed, maxWait)
	}
}

// TestReviewerWaitForWorkIgnoresUnchangedStalledMerge is R5: once PreWork gave
// up on a merge, the same owned merge must not end the wait again — that would
// spin the bounded retries forever — while a change in the owned set must.
func TestReviewerWaitForWorkIgnoresUnchangedStalledMerge(t *testing.T) {
	withPendingMergeWake(t, 50*time.Millisecond, 1)
	withAbortTickInterval(t, 50*time.Millisecond)
	reviewer, bb, config, _ := pendingMergeReviewer(t)

	// GIVEN PreWork gave up on the owned merge
	reviewer.mergeRetries = reviewer.effectiveMaxRetries()
	reviewer.mergeStallRounds = maxPendingMergeStallRounds
	if shouldContinue, err := reviewer.PreWork(context.Background(), bb, config); err != nil || shouldContinue {
		t.Fatalf("PreWork() = (%v, %v), want the stall to release the reviewer", shouldContinue, err)
	}

	// WHEN the reviewer waits with that merge unchanged
	hasWork, err := reviewer.WaitForWork(context.Background(), bb, config, 10*time.Millisecond, 300*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitForWork() error = %v", err)
	}
	// THEN nothing wakes it
	if hasWork || reviewer.preWorkWake {
		t.Fatalf("WaitForWork() = %v (preWorkWake %v), want no wake for the unchanged stalled merge", hasWork, reviewer.preWorkWake)
	}

	// WHEN it comes to own a further merge, THEN the wait ends for merge handling
	if err := bb.Modify(func(state *models.State) error {
		state.Tasks = append(state.Tasks, models.Task{
			ID: "approved-unmerged-2", Status: models.TaskStatusApproved, RolePair: "coding-pair",
			Approvals: []models.Approval{{Agent: config.AgentID, Provider: "claude"}},
		})
		return nil
	}); err != nil {
		t.Fatalf("add a second owned merge: %v", err)
	}
	hasWork, err = reviewer.WaitForWork(context.Background(), bb, config, 10*time.Millisecond, 5*time.Second)
	if err != nil {
		t.Fatalf("WaitForWork() error = %v", err)
	}
	if !hasWork || !reviewer.preWorkWake {
		t.Fatalf("WaitForWork() = %v (preWorkWake %v), want a merge wake for the changed owned set", hasWork, reviewer.preWorkWake)
	}
}

// TestReviewerClaimTaskDeclinesAfterMergeWake pins the routing: a wait that
// ended for merge handling makes the claim step decline without claiming, and
// the decline is neither paced nor counted by the claim breaker, so the loop
// goes straight back to PreWork.
func TestReviewerClaimTaskDeclinesAfterMergeWake(t *testing.T) {
	reviewable := testhelpers.BuildTaskByStatus("needs-review", models.TaskStatusReadyForReview, time.Now().UTC())
	reviewer, bb, config, _ := pendingMergeReviewer(t, reviewable)

	// GIVEN a wait that ended for the owned merge, although review work exists
	hasWork, err := reviewer.WaitForWork(context.Background(), bb, config, 10*time.Millisecond, 5*time.Second)
	if err != nil || !hasWork || !reviewer.preWorkWake {
		t.Fatalf("WaitForWork() = (%v, %v) preWorkWake %v, want a merge wake", hasWork, err, reviewer.preWorkWake)
	}

	// WHEN the supervisor reaches the claim step
	taskID, _, err := reviewer.ClaimTask(config, bb)

	// THEN it declines, claims nothing, and the breaker ignores the decline
	if !errors.Is(err, errPreWorkWake) || taskID != "" {
		t.Fatalf("ClaimTask() = (%q, %v), want the pre-work decline", taskID, err)
	}
	if reviewer.preWorkWake {
		t.Fatal("preWorkWake still set after the decline")
	}
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if task := state.FindTask("needs-review"); task.Status != models.TaskStatusReadyForReview || task.ReviewingBy != nil {
		t.Fatalf("needs-review = status %s reviewing_by %v, want it left unclaimed", task.Status, task.ReviewingBy)
	}
	if decision := reviewer.ObserveClaimFailure(err); decision.Stop || decision.Delay != 0 || len(decision.Opened) != 0 {
		t.Fatalf("ObserveClaimFailure(decline) = %+v, want an immediate, uncounted retry", decision)
	}
}

// TestReviewerYieldToReviewWorkParksOwnedMerge guards against a livelock: after
// PreWork yields a pending merge to review work, the following wait must return
// for that review work, not wake again for the merge it just stepped away from.
func TestReviewerYieldToReviewWorkParksOwnedMerge(t *testing.T) {
	withPendingMergeWake(t, 30*time.Second, 20)
	reviewable := testhelpers.BuildTaskByStatus("needs-review", models.TaskStatusReadyForReview, time.Now().UTC())
	reviewer, bb, config, _ := pendingMergeReviewer(t, reviewable)

	// GIVEN PreWork yielded the pending merge to review work
	reviewer.mergeRetries = reviewer.effectiveMaxRetries()
	if shouldContinue, err := reviewer.PreWork(context.Background(), bb, config); err != nil || shouldContinue {
		t.Fatalf("PreWork() = (%v, %v), want a yield to review work", shouldContinue, err)
	}

	// WHEN the reviewer waits, THEN it wakes for the review work
	hasWork, err := reviewer.WaitForWork(context.Background(), bb, config, 10*time.Millisecond, 5*time.Second)
	if err != nil || !hasWork {
		t.Fatalf("WaitForWork() = (%v, %v), want the review work", hasWork, err)
	}
	if reviewer.preWorkWake {
		t.Fatal("WaitForWork() woke for the merge PreWork just yielded, want the review work")
	}
}
