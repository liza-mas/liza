package ops

import (
	"errors"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// holdLockJoined holds lock until the returned release runs. Unlike
// testhelpers.HoldFileLock, release returns only once the holder has unlocked,
// so the caller's next step never contends with a hold it already ended.
func holdLockJoined(t *testing.T, lock *filelock.FileLock) (release func()) {
	t.Helper()
	acquired := make(chan struct{})
	releaseCh := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- lock.WithTimeout(5*time.Second).WithLockOperation("d78-test-hold", func() error {
			close(acquired)
			<-releaseCh
			return nil
		})
	}()
	select {
	case <-acquired:
	case err := <-done:
		t.Fatalf("hold lock: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("hold lock: acquisition did not complete")
	}
	released := false
	release = func() {
		if released {
			return
		}
		released = true
		close(releaseCh)
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("lock holder: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("lock holder did not unlock")
		}
	}
	t.Cleanup(release) // Watchdog for an early test failure, never the success path.
	return release
}

// assertMergedOnce asserts the task merged at reviewCommit with one merged event.
func assertMergedOnce(t *testing.T, projectRoot, stateFile, taskID, reviewCommit string) {
	t.Helper()
	merged := readStateForTest(t, stateFile).FindTask(taskID)
	if merged.Status != models.TaskStatusMerged {
		t.Fatalf("status = %s, want MERGED", merged.Status)
	}
	if got := testhelpers.MustGit(t, projectRoot, "rev-parse", "refs/heads/integration"); got != reviewCommit {
		t.Fatalf("integration HEAD = %s, want the review commit %s", got, reviewCommit)
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

// assertUnpublishedFailure asserts the D78 contract for a merge that failed
// before publishing anything: the given outcome with requery and no effects,
// the task still APPROVED and never INTEGRATION_FAILED, its preparation
// retired, and the integration ref where it was.
func assertUnpublishedFailure(t *testing.T, err error, wantOutcome, projectRoot, stateFile, taskID, before string) {
	t.Helper()
	var integrationErr *IntegrationFailedError
	if errors.As(err, &integrationErr) {
		t.Fatalf("merge error = %v, want no INTEGRATION_FAILED for a failure before publication", err)
	}
	var lifecycleErr *LifecycleError
	if !errors.As(err, &lifecycleErr) {
		t.Fatalf("merge error = %T %v, want a lifecycle result", err, err)
	}
	if got := lifecycleErr.Outcome; got.Outcome != wantOutcome || got.SafeAction != "requery" || got.Effects != "none" {
		t.Fatalf("outcome = %s/%s effects=%s, want %s/requery effects=none (error: %v)",
			got.Outcome, got.SafeAction, got.Effects, wantOutcome, err)
	}
	if got := testhelpers.MustGit(t, projectRoot, "rev-parse", "refs/heads/integration"); got != before {
		t.Fatalf("integration HEAD = %s, want unchanged %s", got, before)
	}
	task := readStateForTest(t, stateFile).FindTask(taskID)
	if task.Status != models.TaskStatusApproved {
		t.Fatalf("status = %s, want APPROVED", task.Status)
	}
	if len(task.IntegrationFailure) != 0 {
		t.Fatalf("integration_failure = %v, want none", task.IntegrationFailure)
	}
	if task.Lifecycle != nil && task.Lifecycle.Preparation != nil {
		t.Fatalf("preparation retained (operation %s, actor %s); a same-generation retry would requery forever",
			task.Lifecycle.Preparation.Operation, task.Lifecycle.Preparation.Actor)
	}
}

// TestMergeSurvivesStateLockHeldDuringArtifactGuard is the D78 incident: the
// state lock is saturated exactly while the candidate artifact guard reads
// state. The guard must not need that lock, so the merge completes. Before the
// fix the guard's locked read timed out and the approved task was marked
// INTEGRATION_FAILED. Not parallel: it shortens the state-lock timeout and
// replaces the guard's reader.
func TestMergeSurvivesStateLockHeldDuringArtifactGuard(t *testing.T) {
	const taskID = "guard-state-lock-held"
	const agentID = "code-reviewer-1"
	projectRoot, stateFile := setupMergeTestRepo(t, taskID, agentID)
	authority := mergeTestAuthority(t, stateFile, agentID, "same-generation")
	t.Cleanup(db.SetDefaultLockTimeoutForTest(300 * time.Millisecond))
	db.ResetInstance(stateFile) // Recreate the shared instance with the short timeout.
	reviewCommit := *readStateForTest(t, stateFile).FindTask(taskID).ReviewCommit

	// GIVEN every guard read runs while another holder owns the state lock,
	// released before the merge continues so no later step contends with it.
	previous := artifactGuardReadState
	t.Cleanup(func() { artifactGuardReadState = previous })
	var heldReadErrs []error
	artifactGuardReadState = func(bb *db.Blackboard) (*models.State, error) {
		release := holdLockJoined(t, filelock.New(stateFile))
		state, err := previous(bb)
		release()
		heldReadErrs = append(heldReadErrs, err)
		return state, err
	}

	// WHEN the approved task merges
	_, err := MergeWorktreeWithAuthority(projectRoot, taskID, authority)
	artifactGuardReadState = previous

	// THEN the guard read state under the held lock without contending for it,
	// and the merge published
	if len(heldReadErrs) == 0 {
		t.Fatal("the candidate artifact guard never read state")
	}
	for _, readErr := range heldReadErrs {
		if readErr != nil {
			t.Errorf("guard state read under a held state lock = %v, want a read that needs no lock", readErr)
		}
	}
	if err != nil {
		t.Fatalf("merge while the state lock was held during the guard = %v, want success", err)
	}
	assertMergedOnce(t, projectRoot, stateFile, taskID, reviewCommit)
}

// TestMergeGuardUnavailableIsRetryable covers a guard that cannot reach a
// verdict: its state read fails, or its confirmation re-read fails after an
// initial artifact finding. Neither is a confirmed content defect, and the
// guard runs before the ref moves, so the task must stay approved and the merge
// must resolve its own preparation. Only lock contention is RETRYABLE; any
// other read error is STATE_CHANGED. Not parallel: it replaces the guard's
// reader.
func TestMergeGuardUnavailableIsRetryable(t *testing.T) {
	cases := []struct {
		name string
		// confirmation fails the second read, after a first read that reports
		// an artifact the candidate lacks; otherwise the first read fails.
		confirmation bool
		readErr      error
		want         string
	}{
		{
			name:    "initial state read times out",
			readErr: &filelock.LockError{Type: filelock.LockErrorTimeout, Message: "injected guard state read timeout"},
			want:    models.LifecycleRetryable,
		},
		{
			name:         "confirmation read times out after an artifact finding",
			confirmation: true,
			readErr:      &filelock.LockError{Type: filelock.LockErrorTimeout, Message: "injected guard confirmation read timeout"},
			want:         models.LifecycleRetryable,
		},
		{
			name:    "initial state read fails without contention",
			readErr: errors.New("injected guard state decode failure"),
			want:    models.LifecycleStateChanged,
		},
		{
			name:         "confirmation read fails without contention after an artifact finding",
			confirmation: true,
			readErr:      errors.New("injected guard confirmation decode failure"),
			want:         models.LifecycleStateChanged,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const taskID = "guard-unavailable"
			const agentID = "code-reviewer-1"
			projectRoot, stateFile := setupMergeTestRepo(t, taskID, agentID)
			authority := mergeTestAuthority(t, stateFile, agentID, "same-generation")
			reviewCommit := *readStateForTest(t, stateFile).FindTask(taskID).ReviewCommit
			before := testhelpers.MustGit(t, projectRoot, "rev-parse", "refs/heads/integration")

			// GIVEN a guard that cannot establish a verdict
			previous := artifactGuardReadState
			t.Cleanup(func() { artifactGuardReadState = previous })
			calls := 0
			artifactGuardReadState = func(bb *db.Blackboard) (*models.State, error) {
				calls++
				if !tc.confirmation || calls > 1 {
					return nil, tc.readErr
				}
				state, err := previous(bb)
				if err != nil {
					return nil, err
				}
				// A reference the candidate lacks, seen only by the first read.
				state.Tasks = append(state.Tasks, protectedRefTask("guard-owner", "arch_ref", "specs/missing.md"))
				return state, nil
			}

			// WHEN the approved task merges
			_, err := MergeWorktreeWithAuthority(projectRoot, taskID, authority)
			artifactGuardReadState = previous

			// THEN nothing published, the task stays mergeable, and a
			// same-generation retry merges it once
			if !errors.Is(err, tc.readErr) {
				t.Fatalf("merge error = %v, want the guard's read error %v discoverable", err, tc.readErr)
			}
			assertUnpublishedFailure(t, err, tc.want, projectRoot, stateFile, taskID, before)
			if _, err := MergeWorktreeWithAuthority(projectRoot, taskID, authority); err != nil {
				t.Fatalf("same-generation retry = %v, want the merge to complete", err)
			}
			assertMergedOnce(t, projectRoot, stateFile, taskID, reviewCommit)
		})
	}
}

// TestMergeForwardLockTimeoutIsRetryable covers the forward step's two lock
// acquisitions. A timeout there means the forward callback never ran, so the
// merge published nothing and must report RETRYABLE, not uncertain effects.
// Not parallel: it shortens the integration lock timeout.
func TestMergeForwardLockTimeoutIsRetryable(t *testing.T) {
	for _, purpose := range []string{"integration-completion", "integration-mutation"} {
		t.Run(purpose, func(t *testing.T) {
			const taskID = "forward-lock-timeout"
			const agentID = "code-reviewer-1"
			projectRoot, stateFile := setupMergeTestRepo(t, taskID, agentID)
			authority := mergeTestAuthority(t, stateFile, agentID, "same-generation")
			reviewCommit := *readStateForTest(t, stateFile).FindTask(taskID).ReviewCommit
			before := testhelpers.MustGit(t, projectRoot, "rev-parse", "refs/heads/integration")
			previousTimeout := integrationMutationLockTimeout
			t.Cleanup(func() { integrationMutationLockTimeout = previousTimeout })
			integrationMutationLockTimeout = 200 * time.Millisecond

			// GIVEN another holder owns the forward lock
			lock, err := projectFileLock(projectRoot, purpose)
			if err != nil {
				t.Fatalf("projectFileLock(%s) = %v", purpose, err)
			}
			release := holdLockJoined(t, lock)

			// WHEN the approved task merges
			_, err = MergeWorktreeWithAuthority(projectRoot, taskID, authority)
			release()

			// THEN the timeout survives any wrapping, nothing published, and a
			// same-generation retry merges once
			if !filelock.IsLockErrorType(err, filelock.LockErrorTimeout) {
				t.Fatalf("merge error = %v, want a lock timeout", err)
			}
			assertUnpublishedFailure(t, err, models.LifecycleRetryable, projectRoot, stateFile, taskID, before)
			if _, err := MergeWorktreeWithAuthority(projectRoot, taskID, authority); err != nil {
				t.Fatalf("same-generation retry = %v, want the merge to complete", err)
			}
			assertMergedOnce(t, projectRoot, stateFile, taskID, reviewCommit)
		})
	}
}
