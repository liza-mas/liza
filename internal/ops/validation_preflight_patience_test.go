package ops

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D69: the launch-path validation preflight read waits patiently and honors
// cancellation; the claim-path preflight keeps the ordinary lock budget.
// These tests shorten the ordinary timeout, so they must not run in parallel.
const preflightPatienceLockTimeout = 200 * time.Millisecond

func newPreflightPatienceFixture(t *testing.T) (assignmentPreflightFixture, string) {
	t.Helper()
	t.Cleanup(db.SetDefaultLockTimeoutForTest(preflightPatienceLockTimeout))
	f := newAssignmentPreflightFixture(t, models.TaskStatusImplementing)
	return f, paths.New(f.root).StatePath()
}

func TestLaunchValidationPreflightOutlastsStateLockTimeout(t *testing.T) {
	f, statePath := newPreflightPatienceFixture(t)
	release := testhelpers.HoldFileLock(t, statePath)
	time.AfterFunc(600*time.Millisecond, release)

	p, err := PrepareValidationPreflightContext(context.Background(), f.root, "task-1", f.authority.ID, "", f.session(true))
	if err != nil {
		t.Fatalf("launch preflight under a transient state-lock hold: %v", err)
	}
	if p == nil {
		t.Fatal("launch preflight returned no preflight for a task with prerequisites")
	}
}

func TestClaimValidationPreflightKeepsOrdinaryStateLockBudget(t *testing.T) {
	f, statePath := newPreflightPatienceFixture(t)
	testhelpers.HoldFileLock(t, statePath)

	_, err := PrepareValidationPreflight(f.root, "task-1", f.authority.ID, "", f.session(true))
	if !filelock.IsLockErrorType(err, filelock.LockErrorTimeout) {
		t.Fatalf("claim preflight under a held state lock: err=%v, want lock timeout", err)
	}
}

func TestLaunchValidationPreflightStateLockWaitHonorsCancellation(t *testing.T) {
	f, statePath := newPreflightPatienceFixture(t)
	testhelpers.HoldFileLock(t, statePath)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(100*time.Millisecond, cancel)

	start := time.Now()
	_, err := PrepareValidationPreflightContext(ctx, f.root, "task-1", f.authority.ID, "", f.session(true))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("launch preflight after cancellation: err=%v, want context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("launch preflight returned %v after cancellation, want within 1s", elapsed)
	}
}
