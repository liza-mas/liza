package ops

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// interruptMergeAfterReceipt runs a merge that advances the integration ref and
// persists its mutation receipt, then dies before the MERGED write — the OP-046
// boundary. It returns the review commit the task was approved at.
func interruptMergeAfterReceipt(t *testing.T, projectRoot, stateFile, taskID string, authority models.AgentAuthority) string {
	t.Helper()
	state := readStateForTest(t, stateFile)
	task := state.FindTask(taskID)
	if task == nil || task.ReviewCommit == nil {
		t.Fatal("approved task or review_commit missing")
	}
	reviewCommit := *task.ReviewCommit

	previous := mergeFinalStateTestHook
	t.Cleanup(func() { mergeFinalStateTestHook = previous })
	const interrupted = "interrupt before publication"
	mergeFinalStateTestHook = func() { panic(interrupted) }
	func() {
		defer func() {
			if recovered := recover(); recovered != interrupted {
				t.Fatalf("interrupted merge panic = %v, want %q", recovered, interrupted)
			}
		}()
		_, _ = MergeWorktreeWithAuthority(projectRoot, taskID, authority)
	}()
	mergeFinalStateTestHook = previous

	interruptedState := readStateForTest(t, stateFile)
	if got := interruptedState.FindTask(taskID).Status; got != models.TaskStatusApproved {
		t.Fatalf("status after interrupted publication = %s, want APPROVED", got)
	}
	if interruptedState.Goal.Integration == nil || len(interruptedState.Goal.Integration.MutationReceipts) == 0 {
		t.Fatal("interrupted merge recorded no integration mutation receipt")
	}
	return reviewCommit
}

func mergeTestAuthority(t *testing.T, stateFile, agentID, generation string) models.AgentAuthority {
	t.Helper()
	if err := db.For(stateFile).Modify(func(state *models.State) error {
		agent := state.Agents[agentID]
		agent.Generation = generation
		state.Agents[agentID] = agent
		return nil
	}); err != nil {
		t.Fatalf("install merge test authority: %v", err)
	}
	return models.AgentAuthority{ID: agentID, Generation: generation}
}

// TestInterruptedMergeResumesOnProvenEffect is the OP-046 convergence case: the
// integration ref moved and the mutation receipt proves this task moved it, so a
// retry in the same generation must finish the merge rather than requery
// forever. Before this, only a new process could resolve it.
func TestInterruptedMergeResumesOnProvenEffect(t *testing.T) {
	const taskID = "resume-proven"
	const agentID = "code-reviewer-1"
	projectRoot, stateFile := setupMergeTestRepo(t, taskID, agentID)
	authority := mergeTestAuthority(t, stateFile, agentID, "same-generation")

	reviewCommit := interruptMergeAfterReceipt(t, projectRoot, stateFile, taskID, authority)

	result, err := MergeWorktreeWithAuthority(projectRoot, taskID, authority)
	if err != nil {
		t.Fatalf("same-generation retry after a proven merge = %v, want convergence", err)
	}
	if result == nil || result.MergeCommit != reviewCommit {
		t.Fatalf("merge result = %+v, want merge_commit %s", result, reviewCommit)
	}

	state := readStateForTest(t, stateFile)
	merged := state.FindTask(taskID)
	if merged.Status != models.TaskStatusMerged {
		t.Fatalf("status = %s, want MERGED", merged.Status)
	}
	if merged.MergeCommit == nil {
		t.Fatalf("merge_commit is unset, want %s", reviewCommit)
	}
	if *merged.MergeCommit != reviewCommit {
		t.Fatalf("merge_commit = %s, want %s", *merged.MergeCommit, reviewCommit)
	}
	mergedEvents := 0
	for _, entry := range merged.History {
		if entry.Event == models.TaskEventMerged {
			mergedEvents++
		}
	}
	if mergedEvents != 1 {
		t.Fatalf("merged history events = %d, want exactly 1", mergedEvents)
	}
	// Cleanup runs downstream of publication, so a resumed merge must not leak
	// the worktree the interrupted attempt left behind.
	if merged.Worktree != nil {
		t.Errorf("worktree = %v, want nil after a resumed merge", merged.Worktree)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".worktrees", taskID)); !os.IsNotExist(err) {
		t.Errorf("worktree directory still present after a resumed merge (stat err = %v)", err)
	}
}

// TestInterruptedMergeAttributesRecordedCommit covers the window between the
// interrupted publication and the resume: if another task merges in it, the
// integration ref no longer points at this task's merge commit, and only the
// recorded receipt carries the right attribution.
func TestInterruptedMergeAttributesRecordedCommit(t *testing.T) {
	const taskID = "resume-attribution"
	const agentID = "code-reviewer-1"
	projectRoot, stateFile := setupMergeTestRepo(t, taskID, agentID)
	authority := mergeTestAuthority(t, stateFile, agentID, "same-generation")

	reviewCommit := interruptMergeAfterReceipt(t, projectRoot, stateFile, taskID, authority)

	// Another commit lands on integration while this merge is unresolved.
	testhelpers.MustGit(t, projectRoot, "checkout", "integration")
	if err := os.WriteFile(filepath.Join(projectRoot, "other-task.txt"), []byte("other work"), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, projectRoot, "add", ".")
	testhelpers.MustGit(t, projectRoot, "commit", "-m", "another task merged meanwhile")
	head := testhelpers.MustGit(t, projectRoot, "rev-parse", "refs/heads/integration")
	if head == reviewCommit {
		t.Fatal("fixture did not advance the integration ref past this task")
	}

	if _, err := MergeWorktreeWithAuthority(projectRoot, taskID, authority); err != nil {
		t.Fatalf("resume after an intervening merge = %v, want convergence", err)
	}

	merged := readStateForTest(t, stateFile).FindTask(taskID)
	if merged.MergeCommit == nil {
		t.Fatalf("merge_commit is unset, want this task's commit %s", reviewCommit)
	}
	if *merged.MergeCommit != reviewCommit {
		t.Fatalf("merge_commit = %s, want this task's commit %s (integration HEAD is %s)",
			*merged.MergeCommit, reviewCommit, head)
	}
}

// TestInterruptedMergeResumeIsExactlyOnce puts a same-generation resume and a
// replacement generation on the same interrupted merge at once. Exactly-once
// never rested on the preparation fence — the locked approved-status recheck
// before publication refuses the loser — and that must hold now that the fence
// admits the resume.
func TestInterruptedMergeResumeIsExactlyOnce(t *testing.T) {
	const taskID = "resume-exactly-once"
	const agentID = "code-reviewer-1"
	projectRoot, stateFile := setupMergeTestRepo(t, taskID, agentID)
	authority := mergeTestAuthority(t, stateFile, agentID, "same-generation")

	reviewCommit := interruptMergeAfterReceipt(t, projectRoot, stateFile, taskID, authority)
	replacement := mergeTestAuthority(t, stateFile, agentID, "replacement-generation")

	var wg sync.WaitGroup
	outcomes := make(chan error, 2)
	for _, caller := range []models.AgentAuthority{authority, replacement} {
		wg.Add(1)
		go func(a models.AgentAuthority) {
			defer wg.Done()
			_, err := MergeWorktreeWithAuthority(projectRoot, taskID, a)
			outcomes <- err
		}(caller)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for concurrent resumes")
	}
	close(outcomes)

	successes := 0
	for err := range outcomes {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful merges = %d, want exactly 1", successes)
	}

	merged := readStateForTest(t, stateFile).FindTask(taskID)
	if merged.Status != models.TaskStatusMerged || merged.MergeCommit == nil || *merged.MergeCommit != reviewCommit {
		t.Fatalf("final task = %+v, want MERGED at %s", merged, reviewCommit)
	}
	mergedEvents := 0
	for _, entry := range merged.History {
		if entry.Event == models.TaskEventMerged {
			mergedEvents++
		}
	}
	if mergedEvents != 1 {
		t.Fatalf("merged history events = %d, want exactly 1", mergedEvents)
	}
}

// TestInterruptedMergeResumeRevalidatesIntegration verifies a resume is not a
// shortcut to publication: the admitted retry re-runs the required validation
// the interrupted attempt never reached, and a failure there refuses completion
// and rewinds the integration ref to what the receipt records as preceding this
// task's merge. The current ref cannot serve as that baseline on a resume — it
// already carries the merge — so the assertion is on the ref itself.
func TestInterruptedMergeResumeRevalidatesIntegration(t *testing.T) {
	const taskID = "resume-revalidates"
	const agentID = "code-reviewer-1"
	projectRoot, stateFile := setupMergeTestRepo(t, taskID, agentID)
	authority := mergeTestAuthority(t, stateFile, agentID, "same-generation")

	beforeMerge := testhelpers.MustGit(t, projectRoot, "rev-parse", "refs/heads/integration")
	reviewCommit := interruptMergeAfterReceipt(t, projectRoot, stateFile, taskID, authority)

	// Integration tests now fail — the interrupted attempt never ran them.
	scriptsDir := filepath.Join(projectRoot, "scripts")
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		t.Fatalf("create scripts directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "integration-test.sh"), []byte("#!/bin/sh\nexit 1\n"), 0755); err != nil {
		t.Fatalf("write integration test: %v", err)
	}

	_, err := MergeWorktreeWithAuthority(projectRoot, taskID, authority)
	var integrationErr *IntegrationFailedError
	if !errors.As(err, &integrationErr) {
		t.Fatalf("resumed merge error = %T %v, want an integration failure", err, err)
	}

	task := readStateForTest(t, stateFile).FindTask(taskID)
	if task.Status == models.TaskStatusMerged {
		t.Fatal("resumed merge published MERGED despite failing integration tests")
	}
	if task.MergeCommit != nil && task.IntegrationFailure == nil {
		t.Fatalf("task recorded a merge commit without an integration failure: %+v", task)
	}

	// The task is failed; Git must agree. Either the ref was rewound to what
	// preceded this task's merge, or the diagnostic says plainly that
	// integration kept it. Silently keeping it while reporting a rollback is the
	// divergence this assertion exists to catch.
	head := testhelpers.MustGit(t, projectRoot, "rev-parse", "refs/heads/integration")
	if head == beforeMerge {
		return
	}
	if head != reviewCommit {
		t.Fatalf("integration HEAD = %s, want either the pre-merge commit %s or this task's merge %s",
			head, beforeMerge, reviewCommit)
	}
	detail, _ := task.IntegrationFailure["detail"].(string)
	if !strings.Contains(detail, "retains") {
		t.Fatalf("integration still carries %s but the failure claims a rollback: detail = %q, failure = %+v",
			reviewCommit, detail, task.IntegrationFailure)
	}
}

// TestRolledBackMergeDoesNotResume covers the condition that separates proof
// from a bare record: a merge that was rolled back leaves a reverse receipt
// whose recorded commit does not carry the approved review commit. Without that
// check a rolled-back merge would re-publish itself automatically.
func TestRolledBackMergeDoesNotResume(t *testing.T) {
	const taskID = "resume-rolled-back"
	const agentID = "code-reviewer-1"
	projectRoot, stateFile := setupMergeTestRepo(t, taskID, agentID)

	state := readStateForTest(t, stateFile)
	task := state.FindTask(taskID)
	if task == nil || task.ReviewCommit == nil {
		t.Fatal("approved task or review_commit missing")
	}
	reviewCommit := *task.ReviewCommit
	before := testhelpers.MustGit(t, projectRoot, "rev-parse", "refs/heads/integration")

	// Forward merge followed by its rollback, as a failed integration leaves it.
	if state.Goal.Integration == nil {
		state.Goal.Integration = &models.IntegrationLifecycle{}
	}
	state.Goal.Integration.MutationReceipts = append(state.Goal.Integration.MutationReceipts,
		models.IntegrationMutationReceipt{TaskID: taskID, BeforeCommit: before, AfterCommit: reviewCommit},
		models.IntegrationMutationReceipt{TaskID: taskID, BeforeCommit: reviewCommit, AfterCommit: before},
	)
	testhelpers.WriteInitialState(t, stateFile, state)
	db.ResetInstance(stateFile)

	gitWrapper := git.New(projectRoot)
	proven, err := provenMergeEffect(readStateForTest(t, stateFile), gitWrapper, "refs/heads/integration", taskID, reviewCommit)
	if err != nil {
		t.Fatalf("provenMergeEffect() error = %v", err)
	}
	if proven != nil {
		t.Fatalf("rolled-back merge presented as proven: %+v", proven)
	}
}

// TestReceiptLockTimeoutRetiresRolledBackMergePreparation is the D77 case: the
// state lock times out while the merge records its mutation receipt, and the
// merge rewinds the integration ref it moved. The invocation knows its ref
// effect is undone, so it must resolve its own preparation; otherwise every
// same-generation retry requeries until a restart. The retirement write follows
// a lock timeout, so it must wait for the patient budget rather than fail the
// same way. Not parallel: it shortens the ordinary state-lock timeout.
func TestReceiptLockTimeoutRetiresRolledBackMergePreparation(t *testing.T) {
	const taskID = "receipt-lock-timeout"
	const agentID = "code-reviewer-1"
	const ordinaryLockTimeout = 200 * time.Millisecond
	projectRoot, stateFile := setupMergeTestRepo(t, taskID, agentID)
	authority := mergeTestAuthority(t, stateFile, agentID, "same-generation")
	t.Cleanup(db.SetDefaultLockTimeoutForTest(ordinaryLockTimeout))
	db.ResetInstance(stateFile) // Recreate the shared instance with the short timeout.

	reviewCommit := *readStateForTest(t, stateFile).FindTask(taskID).ReviewCommit
	before := testhelpers.MustGit(t, projectRoot, "rev-parse", "refs/heads/integration")

	// Saturate the state lock for the receipt write only. The first lifecycle
	// write after it is the failure retirement; release the hold only once that
	// write has waited past the ordinary timeout.
	var release func()
	modifiesAfterHold := 0
	previousReceiptHook := integrationMutationReceiptPersistTestHook
	previousModifyHook := lifecycleBeforeModifyTestHook
	t.Cleanup(func() {
		integrationMutationReceiptPersistTestHook = previousReceiptHook
		lifecycleBeforeModifyTestHook = previousModifyHook
	})
	integrationMutationReceiptPersistTestHook = func(models.IntegrationMutationReceipt) {
		integrationMutationReceiptPersistTestHook = nil
		release = testhelpers.HoldFileLock(t, stateFile)
	}
	lifecycleBeforeModifyTestHook = func() {
		if release == nil {
			return
		}
		if modifiesAfterHold++; modifiesAfterHold == 2 {
			time.AfterFunc(3*ordinaryLockTimeout, release)
		}
	}

	_, err := MergeWorktreeWithAuthority(projectRoot, taskID, authority)
	lifecycleBeforeModifyTestHook = previousModifyHook
	if release == nil {
		t.Fatal("receipt persistence hook never ran; the merge did not reach the receipt write")
	}
	release() // Unmodified code never retires, so release before inspecting state.
	if err == nil || !strings.Contains(err.Error(), "failed to persist integration mutation receipt") {
		t.Fatalf("first merge error = %v, want a receipt persistence failure", err)
	}
	if got := testhelpers.MustGit(t, projectRoot, "rev-parse", "refs/heads/integration"); got != before {
		t.Fatalf("integration HEAD after the failed receipt write = %s, want rolled back to %s", got, before)
	}

	failed := readStateForTest(t, stateFile).FindTask(taskID)
	if failed.Status != models.TaskStatusApproved {
		t.Fatalf("status after the rolled-back merge = %s, want APPROVED", failed.Status)
	}
	for _, entry := range failed.History {
		if entry.Event == models.TaskEventMerged {
			t.Fatal("rolled-back merge recorded a merged history event")
		}
	}
	if failed.Lifecycle != nil {
		for _, receipt := range failed.Lifecycle.Receipts {
			if receipt.Operation == integrationOperationWTMerge {
				t.Fatalf("rolled-back merge recorded a completion receipt: %+v", receipt)
			}
		}
		if p := failed.Lifecycle.Preparation; p != nil {
			t.Errorf("rolled-back merge retained its preparation (operation %s, actor %s)", p.Operation, p.Actor)
		}
	}

	result, err := MergeWorktreeWithAuthority(projectRoot, taskID, authority)
	if err != nil {
		t.Fatalf("same-generation retry after a rolled-back merge = %v, want convergence", err)
	}
	if result == nil || result.MergeCommit != reviewCommit {
		t.Fatalf("merge result = %+v, want merge_commit %s", result, reviewCommit)
	}
	merged := readStateForTest(t, stateFile).FindTask(taskID)
	if merged.Status != models.TaskStatusMerged {
		t.Fatalf("status after retry = %s, want MERGED", merged.Status)
	}
	mergedEvents := 0
	for _, entry := range merged.History {
		if entry.Event == models.TaskEventMerged {
			mergedEvents++
		}
	}
	if mergedEvents != 1 {
		t.Fatalf("merged history events = %d, want exactly 1", mergedEvents)
	}
}

// TestReceiptLockTimeoutKeepsFenceUnlessRollbackRewound covers the two false
// branches of the D77 retirement predicate: when the receipt write fails and
// the merge cannot vouch that it rewound the ref it moved, the preparation
// fence must hold and a same-generation retry must requery. Not parallel: it
// shortens the ordinary state-lock timeout.
func TestReceiptLockTimeoutKeepsFenceUnlessRollbackRewound(t *testing.T) {
	cases := []struct {
		name string
		// interfere runs just before the receipt write, after CAS moved the ref.
		interfere func(t *testing.T, projectRoot string)
	}{
		{
			// Another merge lands on top, so the rollback CAS refuses to rewind.
			name: "rollback skipped because another merge advanced the ref",
			interfere: func(t *testing.T, projectRoot string) {
				head := testhelpers.MustGit(t, projectRoot, "rev-parse", "refs/heads/integration")
				other := testhelpers.MustGit(t, projectRoot, "commit-tree", head+"^{tree}", "-p", head, "-m", "another task merged meanwhile")
				testhelpers.MustGit(t, projectRoot, "update-ref", "refs/heads/integration", other, head)
			},
		},
		{
			// A held ref lock makes the rollback's update-ref fail outright.
			name: "rollback returns an error",
			interfere: func(t *testing.T, projectRoot string) {
				refLock := filepath.Join(projectRoot, ".git", "refs", "heads", "integration.lock")
				if err := os.WriteFile(refLock, nil, 0644); err != nil {
					t.Fatalf("hold integration ref lock: %v", err)
				}
				t.Cleanup(func() { _ = os.Remove(refLock) })
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const taskID = "receipt-lock-timeout-fence"
			const agentID = "code-reviewer-1"
			projectRoot, stateFile := setupMergeTestRepo(t, taskID, agentID)
			authority := mergeTestAuthority(t, stateFile, agentID, "same-generation")
			t.Cleanup(db.SetDefaultLockTimeoutForTest(200 * time.Millisecond))
			db.ResetInstance(stateFile) // Recreate the shared instance with the short timeout.

			// Release the hold as soon as any lifecycle write follows the failed
			// receipt write, so a wrongful retirement would succeed and be caught
			// rather than time out and leave the fence standing by accident.
			var release func()
			modifiesAfterHold := 0
			previousReceiptHook := integrationMutationReceiptPersistTestHook
			previousModifyHook := lifecycleBeforeModifyTestHook
			t.Cleanup(func() {
				integrationMutationReceiptPersistTestHook = previousReceiptHook
				lifecycleBeforeModifyTestHook = previousModifyHook
			})
			integrationMutationReceiptPersistTestHook = func(models.IntegrationMutationReceipt) {
				integrationMutationReceiptPersistTestHook = nil
				tc.interfere(t, projectRoot)
				release = testhelpers.HoldFileLock(t, stateFile)
			}
			lifecycleBeforeModifyTestHook = func() {
				if release == nil {
					return
				}
				if modifiesAfterHold++; modifiesAfterHold == 2 {
					release()
				}
			}

			_, err := MergeWorktreeWithAuthority(projectRoot, taskID, authority)
			lifecycleBeforeModifyTestHook = previousModifyHook
			if release == nil {
				t.Fatal("receipt persistence hook never ran; the merge did not reach the receipt write")
			}
			release()
			if err == nil || !strings.Contains(err.Error(), "failed to persist integration mutation receipt") {
				t.Fatalf("first merge error = %v, want a receipt persistence failure", err)
			}

			failed := readStateForTest(t, stateFile).FindTask(taskID)
			if failed.Status != models.TaskStatusApproved {
				t.Fatalf("status after the failed merge = %s, want APPROVED", failed.Status)
			}
			if failed.Lifecycle == nil || failed.Lifecycle.Preparation == nil {
				t.Fatal("preparation retired although the merge could not vouch that it rewound the ref")
			}
			_, err = MergeWorktreeWithAuthority(projectRoot, taskID, authority)
			var pending *LifecycleError
			if !errors.As(err, &pending) || pending.Outcome.Outcome != models.LifecycleStateChanged || pending.Outcome.SafeAction != "requery" {
				t.Fatalf("same-generation retry behind the kept fence = %v, want requery", err)
			}
		})
	}
}
