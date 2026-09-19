package ops

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func setupOwnershipLifecycleClaim(t *testing.T) (string, string, *db.Blackboard, models.AgentAuthority) {
	t.Helper()
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	authority := models.AgentAuthority{ID: "coder-1", Generation: "ownership-generation-1"}
	agent := state.Agents[authority.ID]
	agent.Generation = authority.Generation
	state.Agents[authority.ID] = agent
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, time.Now().UTC())}
	bb := testhelpers.WriteInitialState(t, statePath, state)
	return root, statePath, bb, authority
}

func ownershipRequestOptions(t *testing.T, bb *db.Blackboard, requestID string) LifecycleRequestOptions {
	t.Helper()
	task, err := bb.GetTask("task-1")
	if err != nil {
		t.Fatal(err)
	}
	return LifecycleRequestOptions{RequestID: requestID, ExpectedTransition: models.TaskTransitionID(task)}
}

func ownershipStateBytes(t *testing.T, statePath string) []byte {
	t.Helper()
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestOwnershipKnownSetupFailureRequiresFreshBoundary(t *testing.T) {
	root, statePath, bb, authority := setupOwnershipLifecycleClaim(t)
	if err := bb.Modify(func(state *models.State) error {
		command := "exit 1"
		state.Config.PostWorktreeCmd = &command
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	opts := ownershipRequestOptions(t, bb, "failed-setup")
	_, err := ClaimTaskWithRequest(root, "task-1", authority.ID, &authority, opts)
	var setupErr *PostWorktreeSetupError
	if !errors.As(err, &setupErr) || !errors.Is(err, ErrAgentDegraded) {
		t.Fatalf("expected setup failure and degraded agent, got %v", err)
	}
	task, err := bb.GetTask("task-1")
	if err != nil {
		t.Fatal(err)
	}
	if task.Lifecycle == nil || task.Lifecycle.Preparation != nil || len(task.Lifecycle.Receipts) != 0 ||
		models.TaskTransitionID(task) == opts.ExpectedTransition || task.Status != models.TaskStatusReady || task.AssignedTo != nil {
		t.Fatalf("known setup failure did not retire only its reservation: %+v", task)
	}
	if _, err := os.Stat(filepath.Join(root, ".worktrees", "task-1")); err != nil {
		t.Fatalf("failed setup worktree was not preserved: %v", err)
	}
	before := ownershipStateBytes(t, statePath)
	_, err = ClaimTaskWithRequest(root, "task-1", authority.ID, &authority, opts)
	requireLifecycleError(t, err, models.LifecycleStateChanged, "requery", "none")
	if !bytes.Equal(before, ownershipStateBytes(t, statePath)) {
		t.Fatal("stale setup request repeated effects or rewrote state")
	}
	if err := bb.Modify(func(state *models.State) error {
		command := "touch .fixed-setup"
		state.Config.PostWorktreeCmd = &command
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	fresh := ownershipRequestOptions(t, bb, "fixed-setup")
	result, err := ClaimTaskWithRequest(root, "task-1", authority.ID, &authority, fresh)
	if err != nil || result.Outcome != models.LifecycleCompleted {
		t.Fatalf("fresh request after setup repair failed: result=%+v err=%v", result, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".worktrees", "task-1", ".fixed-setup")); err != nil {
		t.Fatalf("repaired setup did not execute: %v", err)
	}
}

func TestOwnershipFailureObservesCommittedState(t *testing.T) {
	for _, replaced := range []bool{false, true} {
		name := "discarded-mutation"
		if replaced {
			name = "authority-replaced"
		}
		t.Run(name, func(t *testing.T) {
			root, _, bb, authority := setupOwnershipLifecycleClaim(t)
			before, err := bb.GetTask("task-1")
			if err != nil {
				t.Fatal(err)
			}
			invocation := &ownershipInvocation{operation: "claim-task", authority: &authority}
			writeFailure := errors.New("failed state write")
			err = bb.Modify(func(state *models.State) error {
				task := state.FindTask("task-1")
				invocation.observe(task)
				task.Status = models.TaskStatusImplementing
				task.AssignedTo = &authority.ID
				models.AdvanceLifecycle(task)
				return writeFailure
			})
			if replaced {
				if err := bb.Modify(func(state *models.State) error {
					agent := state.Agents[authority.ID]
					agent.Generation = "replacement-generation"
					state.Agents[authority.ID] = agent
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			err = invocation.finish(root, err)
			var lifecycleErr *LifecycleError
			if !errors.Is(err, writeFailure) || !errors.As(err, &lifecycleErr) {
				t.Fatalf("lost write failure: %v", err)
			}
			outcome := lifecycleErr.Outcome
			if replaced {
				if outcome.TaskStatus != "UNKNOWN" || outcome.TransitionID != "" || outcome.CurrentAssignee != "" {
					t.Fatalf("stale caller observed ownership: %+v", outcome)
				}
			} else if outcome.TaskStatus != before.Status || outcome.TransitionID != models.TaskTransitionID(before) || outcome.CurrentAssignee != "" {
				t.Fatalf("error advertised an uncommitted claim: %+v", outcome)
			}
		})
	}
}

func TestOwnershipLifecycleClaimReplayAfterReleaseAndReclaim(t *testing.T) {
	root, statePath, bb, authority := setupOwnershipLifecycleClaim(t)
	claimOpts := ownershipRequestOptions(t, bb, "claim-first")
	first, err := ClaimTaskWithRequest(root, "task-1", authority.ID, &authority, claimOpts)
	if err != nil {
		t.Fatal(err)
	}
	if first.Outcome != models.LifecycleCompleted {
		t.Fatalf("initial outcome = %s", first.Outcome)
	}
	before := ownershipStateBytes(t, statePath)
	replay, err := ClaimTaskWithRequest(root, "task-1", authority.ID, &authority, claimOpts)
	if err != nil {
		t.Fatal(err)
	}
	if replay.Outcome != models.LifecycleAlreadyCompleted || replay.SafeAction != "continue" || replay.BaseCommit != first.BaseCommit || !replay.LeaseExpires.Equal(first.LeaseExpires) {
		t.Fatalf("invalid claim replay: %+v", replay)
	}
	if !bytes.Equal(before, ownershipStateBytes(t, statePath)) {
		t.Fatal("claim replay rewrote state.yaml")
	}
	releaseOpts := ownershipRequestOptions(t, bb, "release-first")
	released, err := ReleaseClaimWithRequest(root, "task-1", "doer", true, "operator release", "human", nil, releaseOpts)
	if err != nil {
		t.Fatal(err)
	}
	if !released.ReleasedDoer || released.Outcome != models.LifecycleCompleted {
		t.Fatalf("invalid release: %+v", released)
	}
	before = ownershipStateBytes(t, statePath)
	releaseReplay, err := ReleaseClaimWithRequest(root, "task-1", "doer", true, "operator release", "human", nil, releaseOpts)
	if err != nil {
		t.Fatal(err)
	}
	if releaseReplay.Outcome != models.LifecycleAlreadyCompleted || releaseReplay.SafeAction != "stop" || !releaseReplay.ReleasedDoer {
		t.Fatalf("invalid release replay: %+v", releaseReplay)
	}
	if !bytes.Equal(before, ownershipStateBytes(t, statePath)) {
		t.Fatal("release replay rewrote state.yaml")
	}
	if _, err := ClaimTaskWithRequest(root, "task-1", authority.ID, &authority, ownershipRequestOptions(t, bb, "claim-second")); err != nil {
		t.Fatal(err)
	}
	before = ownershipStateBytes(t, statePath)
	replay, err = ClaimTaskWithRequest(root, "task-1", authority.ID, &authority, claimOpts)
	if err != nil {
		t.Fatal(err)
	}
	if replay.Outcome != models.LifecycleAlreadyCompleted || replay.SafeAction != "stop" || replay.CompletedTransitionID != first.CompletedTransitionID || replay.TransitionID == first.TransitionID {
		t.Fatalf("historical claim authorized another interval: %+v", replay)
	}
	if !bytes.Equal(before, ownershipStateBytes(t, statePath)) {
		t.Fatal("historical claim replay rewrote state.yaml")
	}
}

func TestOwnershipLifecycleClaimRestartsAfterGenerationReplacement(t *testing.T) {
	root, statePath, bb, old := setupOwnershipLifecycleClaim(t)
	oldOpts := ownershipRequestOptions(t, bb, "interrupted-claim")
	current := models.AgentAuthority{ID: old.ID, Generation: "ownership-generation-2"}
	previousHooks := testClaimTaskHooks
	t.Cleanup(func() { testClaimTaskHooks = previousHooks })
	testClaimTaskHooks = &claimTaskTestHooks{beforePhase3Modify: func() {
		if err := bb.Modify(func(state *models.State) error {
			agent := state.Agents[old.ID]
			agent.Generation = current.Generation
			state.Agents[old.ID] = agent
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}}
	_, err := ClaimTaskWithRequest(root, "task-1", old.ID, &old, oldOpts)
	requireLifecycleError(t, err, models.LifecycleStaleCaller, "stop", "unknown")
	testClaimTaskHooks = previousHooks
	task, err := bb.GetTask("task-1")
	if err != nil {
		t.Fatal(err)
	}
	if task.Lifecycle == nil || task.Lifecycle.Preparation == nil || len(task.Lifecycle.Receipts) != 0 {
		t.Fatal("interrupted claim did not retain only its preparation")
	}
	result, err := ClaimTaskWithRequest(root, "task-1", current.ID, &current, ownershipRequestOptions(t, bb, "restart-claim"))
	if err != nil {
		t.Fatalf("current generation could not reclaim: %v", err)
	}
	if result.Outcome != models.LifecycleCompleted {
		t.Fatalf("restart outcome = %s", result.Outcome)
	}
	task, err = bb.GetTask("task-1")
	if err != nil {
		t.Fatal(err)
	}
	if task.Lifecycle.Preparation != nil || len(task.Lifecycle.Receipts) != 1 || task.Iteration != 1 {
		t.Fatal("restart fabricated or duplicated a claim completion")
	}
	before := ownershipStateBytes(t, statePath)
	_, err = ClaimTaskWithRequest(root, "task-1", old.ID, &old, oldOpts)
	requireLifecycleError(t, err, models.LifecycleStaleCaller, "stop", "none")
	if !bytes.Equal(before, ownershipStateBytes(t, statePath)) {
		t.Fatal("old generation mutated the recovered task")
	}
}

func TestOwnershipLifecycleConcurrentClaimHasOneCompletion(t *testing.T) {
	root, _, bb, authority := setupOwnershipLifecycleClaim(t)
	opts := ownershipRequestOptions(t, bb, "concurrent-claim")
	start := make(chan struct{})
	results := make(chan *ClaimResult, 2)
	failures := make(chan error, 2)
	var workers sync.WaitGroup
	for i := 0; i < 2; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			result, err := ClaimTaskWithRequest(root, "task-1", authority.ID, &authority, opts)
			if err != nil {
				failures <- err
				return
			}
			results <- result
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	close(failures)
	completed := 0
	for result := range results {
		if result.Outcome == models.LifecycleCompleted {
			completed++
		} else if result.Outcome != models.LifecycleAlreadyCompleted {
			t.Fatalf("unexpected concurrent outcome: %s", result.Outcome)
		}
	}
	for err := range failures {
		// Observing the first caller's live preparation cannot prove its outcome.
		requireLifecycleError(t, err, models.LifecycleStateChanged, "requery", "unknown")
	}
	if completed != 1 {
		t.Fatalf("completed calls = %d, want exactly one", completed)
	}
	task, err := bb.GetTask("task-1")
	if err != nil {
		t.Fatal(err)
	}
	if task.Iteration != 1 || len(task.Lifecycle.Receipts) != 1 || task.Lifecycle.Preparation != nil {
		t.Fatal("concurrent calls duplicated assignment effects")
	}
}

func TestOwnershipLifecycleRecoveryRetiresPreparationAndReplays(t *testing.T) {
	root, statePath := setupImplementingTask(t, 999999)
	testhelpers.SetupTestGitRepo(t, root)
	bb := db.For(statePath)
	var abandoned LifecycleRequest
	if err := bb.Modify(func(state *models.State) error {
		task := state.FindTask("task-1")
		var err error
		abandoned, err = NewLifecycleRequest("submit-for-review", task, "coder-1", nil, LifecycleRequestOptions{}, nil)
		if err != nil {
			return err
		}
		return PrepareLifecycleRequest(task, abandoned, nil)
	}); err != nil {
		t.Fatal(err)
	}
	opts := RecoverTaskOptions{RequestOptions: ownershipRequestOptions(t, bb, "recover-abandoned")}
	result, err := RecoverTaskWithOptions(root, "task-1", "operator recovery", opts)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != models.LifecycleCompleted || !result.ClaimReleased {
		t.Fatalf("recovery failed: %+v", result)
	}
	task, err := bb.GetTask("task-1")
	if err != nil {
		t.Fatal(err)
	}
	if task.Lifecycle.Preparation != nil || len(task.Lifecycle.Receipts) != 1 || task.Lifecycle.Receipts[0].Operation != "recover-task" {
		t.Fatal("recovery retained abandoned work or fabricated its completion")
	}
	requireLifecycleError(t, ValidateLifecyclePreparation(task, abandoned), models.LifecycleStateChanged, "requery", "unknown")
	before := ownershipStateBytes(t, statePath)
	replay, err := RecoverTaskWithOptions(root, "task-1", "operator recovery", opts)
	if err != nil {
		t.Fatal(err)
	}
	if replay.Outcome != models.LifecycleAlreadyCompleted || replay.SafeAction != "stop" || replay.CompletedTransitionID != result.CompletedTransitionID {
		t.Fatalf("invalid recovery replay: %+v", replay)
	}
	if !bytes.Equal(before, ownershipStateBytes(t, statePath)) {
		t.Fatal("recovery replay rewrote state.yaml or notes")
	}
}

func TestOwnershipLifecycleAttemptRolloverInvalidatesPreparation(t *testing.T) {
	root, statePath := setupTransitionTest(t)
	bb := db.For(statePath)
	var abandoned LifecycleRequest
	if err := bb.Modify(func(state *models.State) error {
		task := state.FindTask("task-1")
		var err error
		abandoned, err = NewLifecycleRequest("claim-task", task, "coder-1", nil, LifecycleRequestOptions{}, nil)
		if err != nil {
			return err
		}
		return PrepareLifecycleRequest(task, abandoned, nil)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := TransitionToNewAttempt(root, "task-1", "attempt exhausted"); err != nil {
		t.Fatal(err)
	}
	task, err := bb.GetTask("task-1")
	if err != nil {
		t.Fatal(err)
	}
	if task.Attempt != 2 || task.Lifecycle.Preparation != nil || len(task.Lifecycle.Receipts) != 0 || task.Lifecycle.CompletionSequence != 0 {
		t.Fatal("rollover failed to invalidate or fabricated a completion")
	}
	requireLifecycleError(t, ValidateLifecyclePreparation(task, abandoned), models.LifecycleStateChanged, "requery", "unknown")
}

func TestOwnershipLifecycleVerdictRolloverChecksCompletedBoundary(t *testing.T) {
	for _, interveningClaim := range []bool{false, true} {
		name := "lease renewal preserves completed boundary"
		if interveningClaim {
			name = "intervening claim prevents delayed rollover"
		}
		t.Run(name, func(t *testing.T) {
			root, statePath := setupTransitionTest(t)
			bb := db.For(statePath)
			completed := ownershipRequestOptions(t, bb, "verdict").ExpectedTransition
			if err := bb.Modify(func(state *models.State) error {
				task := state.FindTask("task-1")
				lease := time.Now().UTC().Add(time.Hour)
				task.LeaseExpires = &lease
				if interveningClaim {
					owner := "coder-2"
					task.AssignedTo = &owner
					task.Status = models.TaskStatusImplementing
					task.Iteration++
					models.AdvanceLifecycle(task)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			before := ownershipStateBytes(t, statePath)
			var result *TransitionAttemptResult
			err := WithProjectLifecycleSharedLock(root, "verdict-follow-up-test", func() error {
				var inner error
				result, inner = transitionToNewAttemptAfterVerdict(root, "task-1", "review cycle limit", nil, completed)
				return inner
			})
			if interveningClaim {
				testhelpers.RequireErrorContains(t, err, "task boundary changed after verdict completion")
				if result != nil {
					t.Fatal("rejected follow-up returned a completed rollover")
				}
				if !bytes.Equal(before, ownershipStateBytes(t, statePath)) {
					t.Fatal("delayed rollover changed the new claim")
				}
				return
			}
			if err != nil {
				t.Fatalf("lease renewal invalidated verdict boundary: %v", err)
			}
			task, err := bb.GetTask("task-1")
			if err != nil {
				t.Fatal(err)
			}
			if task.Attempt != 2 || task.Status != models.TaskStatusReady || result.TransitionID != models.TaskTransitionID(task) {
				t.Fatal("matching verdict boundary did not complete rollover")
			}
		})
	}
}
