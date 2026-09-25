package ops

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// None of these tests is parallel: they share the process-wide deferred
// retirement queue and shorten package-wide lock waits.

func isolateDeferredRetirements(t *testing.T) {
	t.Helper()
	deferredRetirements.Lock()
	saved := deferredRetirements.byState
	deferredRetirements.byState = map[string]map[deferredRetirementKey]deferredRetirement{}
	deferredRetirements.Unlock()
	t.Cleanup(func() {
		deferredRetirements.Lock()
		deferredRetirements.byState = saved
		deferredRetirements.Unlock()
	})
}

func pendingDeferredRetirements(t *testing.T, projectRoot string) int {
	t.Helper()
	statePath, ok := deferredRetirementStatePath(projectRoot)
	if !ok {
		t.Fatal("cannot resolve deferred retirement state path")
	}
	deferredRetirements.Lock()
	defer deferredRetirements.Unlock()
	return len(deferredRetirements.byState[statePath])
}

// shortLockWaits makes the ordinary wait 200ms and the patient wait 500ms, so a
// lock held past both is a test of seconds, not minutes.
func shortLockWaits(t *testing.T) {
	t.Helper()
	t.Cleanup(db.SetDefaultLockTimeoutForTest(200 * time.Millisecond))
	t.Cleanup(db.SetPatientReadLockTimeoutForTest(500 * time.Millisecond))
}

// seedQueuedClaimPreparation records an unresolved claim-task preparation for
// authority at task-1's current boundary and queues its retirement, as a claim
// whose retirement failed to commit leaves them.
func seedQueuedClaimPreparation(t *testing.T, projectRoot string, bb *db.Blackboard, authority models.AgentAuthority) models.LifecyclePreparation {
	t.Helper()
	var preparation models.LifecyclePreparation
	if err := bb.Modify(func(state *models.State) error {
		task := state.FindTask("task-1")
		request, err := NewLifecycleRequest("claim-task", task, authority.ID, &authority, LifecycleRequestOptions{}, nil)
		if err != nil {
			return err
		}
		if err := PrepareLifecycleRequest(task, request, state.Agents); err != nil {
			return err
		}
		preparation = *task.Lifecycle.Preparation
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	statePath, ok := deferredRetirementStatePath(projectRoot)
	if !ok {
		t.Fatal("cannot resolve deferred retirement state path")
	}
	deferLifecycleRetirement(statePath, "task-1", &authority, preparation)
	return preparation
}

func currentPreparation(t *testing.T, bb *db.Blackboard) (*models.LifecyclePreparation, uint64) {
	t.Helper()
	task, err := bb.Patient().GetTask("task-1") // A just-released hold may still be unwinding.
	if err != nil {
		t.Fatal(err)
	}
	if task.Lifecycle == nil {
		return nil, 0
	}
	return task.Lifecycle.Preparation, task.Lifecycle.Revision
}

// RT2: the claim's retirement fails even on the patient wait; the same process's
// next claim retires it from the queue and succeeds, exactly once.
func TestClaimRetriesItsUncommittedRetirementOnNextClaim(t *testing.T) {
	isolateDeferredRetirements(t)
	shortLockWaits(t)
	root, statePath, bb, authority := setupOwnershipLifecycleClaim(t)
	var release func()
	previousHooks := testClaimTaskHooks
	t.Cleanup(func() { testClaimTaskHooks = previousHooks })
	testClaimTaskHooks = &claimTaskTestHooks{beforePhase3Modify: func() {
		if release == nil {
			release = testhelpers.HoldFileLock(t, statePath)
		}
	}}

	_, err := ClaimTaskWithAuthority(root, "task-1", authority)
	testClaimTaskHooks = previousHooks
	if release == nil {
		t.Fatal("phase-3 hook never ran")
	}
	release()
	if err == nil || !strings.Contains(err.Error(), "failed to retire lifecycle preparation") {
		t.Fatalf("first claim error = %v, want an uncommitted retirement", err)
	}
	if p, _ := currentPreparation(t, bb); p == nil {
		t.Fatal("setup: the retirement committed despite the held lock")
	}
	if got := pendingDeferredRetirements(t, root); got != 1 {
		t.Fatalf("queued retirements = %d, want 1", got)
	}

	if _, err := ClaimTaskWithAuthority(root, "task-1", authority); err != nil {
		t.Fatalf("same-process claim after an uncommitted retirement = %v, want success", err)
	}
	task, err := bb.Patient().GetTask("task-1")
	if err != nil {
		t.Fatal(err)
	}
	if task.Iteration != 1 || task.AssignedTo == nil || *task.AssignedTo != authority.ID {
		t.Fatalf("retry claim state: iteration=%d assigned=%v, want exactly one claim by %s", task.Iteration, task.AssignedTo, authority.ID)
	}
	if got := pendingDeferredRetirements(t, root); got != 0 {
		t.Fatalf("queued retirements after the retry = %d, want 0", got)
	}
}

// RT4: a reviewer claim drains the queue before selecting work, so its own
// uncommitted retirement no longer blocks it.
func TestReviewerClaimDrainsDeferredRetirement(t *testing.T) {
	isolateDeferredRetirements(t)
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	registerClaimReviewerTaskTestAgents(state)
	authority := models.AgentAuthority{ID: "code-reviewer-1", Generation: "reviewer-generation-1"}
	agent := state.Agents[authority.ID]
	agent.Generation = authority.Generation
	state.Agents[authority.ID] = agent
	reviewCommit := "abc123"
	state.Tasks = []models.Task{{ID: "task-1", Status: models.TaskStatusReadyForReview, RolePair: "coding-pair", ReviewCommit: &reviewCommit, Created: time.Now().UTC()}}
	bb := testhelpers.WriteInitialState(t, statePath, state)

	var preparation models.LifecyclePreparation
	if err := bb.Modify(func(state *models.State) error {
		task := state.FindTask("task-1")
		request, err := NewLifecycleRequest("claim-reviewer-task", task, authority.ID, &authority, LifecycleRequestOptions{}, nil)
		if err != nil {
			return err
		}
		if err := PrepareLifecycleRequest(task, request, state.Agents); err != nil {
			return err
		}
		preparation = *task.Lifecycle.Preparation
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	input := ClaimReviewerTaskInput{ProjectRoot: root, AgentID: authority.ID, Authority: &authority, TaskID: "task-1"}
	if _, err := ClaimReviewerTask(input); err == nil || !strings.Contains(err.Error(), "unresolved preparation remains") {
		t.Fatalf("reviewer claim over its unqueued marker = %v, want an unresolved-preparation refusal", err)
	}

	statePathKey, _ := deferredRetirementStatePath(root)
	deferLifecycleRetirement(statePathKey, "task-1", &authority, preparation)
	result, err := ClaimReviewerTask(input)
	if err != nil {
		t.Fatalf("reviewer claim after queuing its retirement = %v, want success", err)
	}
	if result.TaskID != "task-1" {
		t.Fatalf("reviewer claimed %q, want task-1", result.TaskID)
	}
	if got := pendingDeferredRetirements(t, root); got != 0 {
		t.Fatalf("queued retirements after the reviewer claim = %d, want 0", got)
	}
}

// RT8: properties of the drain itself.
func TestDeferredRetirementDrain(t *testing.T) {
	t.Run("root isolation", func(t *testing.T) {
		isolateDeferredRetirements(t)
		shortLockWaits(t)
		rootA, statePathA, bbA, authorityA := setupOwnershipLifecycleClaim(t)
		rootB, _, _, _ := setupOwnershipLifecycleClaim(t)
		prepared := seedQueuedClaimPreparation(t, rootA, bbA, authorityA)
		release := testhelpers.HoldFileLock(t, statePathA)

		start := time.Now()
		drainDeferredLifecycleRetirements(rootB)
		if elapsed := time.Since(start); elapsed >= 200*time.Millisecond {
			t.Fatalf("project B's drain waited %v on project A's state lock", elapsed)
		}
		if got := pendingDeferredRetirements(t, rootA); got != 1 {
			t.Fatalf("project A's queued retirements after B's drain = %d, want 1", got)
		}
		release()
		if p, _ := currentPreparation(t, bbA); p == nil || *p != prepared {
			t.Fatal("project B's drain changed project A's marker")
		}
		drainDeferredLifecycleRetirements(rootA)
		if p, _ := currentPreparation(t, bbA); p != nil {
			t.Fatal("project A's own drain did not retire its marker")
		}
	})

	t.Run("stale generation is dropped without a write", func(t *testing.T) {
		isolateDeferredRetirements(t)
		root, statePath, bb, authority := setupOwnershipLifecycleClaim(t)
		seedQueuedClaimPreparation(t, root, bb, authority)
		setLifecycleAgentGeneration(t, bb, authority.ID, "ownership-generation-2")
		before := ownershipStateBytes(t, statePath)

		drainDeferredLifecycleRetirements(root)
		if got := pendingDeferredRetirements(t, root); got != 0 {
			t.Fatalf("queued retirements after a stale-generation drain = %d, want 0", got)
		}
		if after := ownershipStateBytes(t, statePath); string(after) != string(before) {
			t.Fatal("stale-generation drain wrote state")
		}
	})

	t.Run("replaced marker is left untouched", func(t *testing.T) {
		isolateDeferredRetirements(t)
		root, _, bb, authority := setupOwnershipLifecycleClaim(t)
		seedQueuedClaimPreparation(t, root, bb, authority)
		var replacement models.LifecyclePreparation
		if err := bb.Modify(func(state *models.State) error {
			task := state.FindTask("task-1")
			request, err := NewLifecycleRequest("recover-task", task, "human", nil, LifecycleRequestOptions{}, nil)
			if err != nil {
				return err
			}
			if err := prepareOwnerEndingRequest(task, request); err != nil {
				return err
			}
			replacement = *task.Lifecycle.Preparation
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		_, revision := currentPreparation(t, bb)

		drainDeferredLifecycleRetirements(root)
		p, after := currentPreparation(t, bb)
		if p == nil || *p != replacement || after != revision {
			t.Fatalf("drain touched a replacement marker: preparation=%+v revision %d -> %d", p, revision, after)
		}
		if got := pendingDeferredRetirements(t, root); got != 0 {
			t.Fatalf("queued retirements after a mismatch = %d, want 0", got)
		}
	})

	t.Run("still contended stays queued", func(t *testing.T) {
		isolateDeferredRetirements(t)
		shortLockWaits(t)
		root, statePath, bb, authority := setupOwnershipLifecycleClaim(t)
		seedQueuedClaimPreparation(t, root, bb, authority)
		release := testhelpers.HoldFileLock(t, statePath)

		drainDeferredLifecycleRetirements(root)
		if got := pendingDeferredRetirements(t, root); got != 1 {
			t.Fatalf("queued retirements after a contended drain = %d, want 1", got)
		}
		release()
		if p, _ := currentPreparation(t, bb); p == nil {
			t.Fatal("contended drain retired the marker without the lock")
		}
		drainDeferredLifecycleRetirements(root)
		if p, _ := currentPreparation(t, bb); p != nil {
			t.Fatal("uncontended drain did not retire the marker")
		}
		if got := pendingDeferredRetirements(t, root); got != 0 {
			t.Fatalf("queued retirements after the retirement = %d, want 0", got)
		}
	})

	t.Run("concurrent drains retire once", func(t *testing.T) {
		isolateDeferredRetirements(t)
		root, _, bb, authority := setupOwnershipLifecycleClaim(t)
		seedQueuedClaimPreparation(t, root, bb, authority)
		_, revision := currentPreparation(t, bb)

		start := make(chan struct{})
		var drains sync.WaitGroup
		for range 2 {
			drains.Add(1)
			go func() {
				defer drains.Done()
				<-start
				drainDeferredLifecycleRetirements(root)
			}()
		}
		close(start)
		drains.Wait()
		p, after := currentPreparation(t, bb)
		if p != nil || after != revision+1 {
			t.Fatalf("concurrent drains: preparation=%+v revision %d -> %d, want nil and one advance", p, revision, after)
		}
	})
}
