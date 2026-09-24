package db_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func newPatientReadFixture(t *testing.T) (*db.Blackboard, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.yaml")
	bb := db.New(path).WithLockTimeout(200 * time.Millisecond)
	if err := bb.Write(&models.State{Goal: models.Goal{Description: "patient"}}); err != nil {
		t.Fatal(err)
	}
	return bb, path
}

func TestReadContextPatientOutlastsLockTimeout(t *testing.T) {
	t.Parallel()
	bb, path := newPatientReadFixture(t)
	release := testhelpers.HoldFileLock(t, path)
	time.AfterFunc(600*time.Millisecond, release)

	state, err := bb.ReadContextPatient(context.Background())
	if err != nil {
		t.Fatalf("patient read under a transient lock hold: %v", err)
	}
	if state.Goal.Description != "patient" {
		t.Fatalf("patient read returned goal %q", state.Goal.Description)
	}
}

func TestReadContextPatientHonorsCancellation(t *testing.T) {
	t.Parallel()
	bb, path := newPatientReadFixture(t)
	testhelpers.HoldFileLock(t, path)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(100*time.Millisecond, cancel)

	start := time.Now()
	_, err := bb.ReadContextPatient(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("patient read after cancellation: err=%v, want context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("patient read returned %v after cancellation, want within 1s", elapsed)
	}
}

// Not parallel: it shortens the package-wide patient wait.
func TestReadContextPatientReturnsLockTimeoutOnceWaitElapses(t *testing.T) {
	t.Cleanup(db.SetPatientReadLockTimeoutForTest(300 * time.Millisecond))
	bb, path := newPatientReadFixture(t)
	testhelpers.HoldFileLock(t, path)

	start := time.Now()
	_, err := bb.ReadContextPatient(context.Background())
	if !filelock.IsLockErrorType(err, filelock.LockErrorTimeout) {
		t.Fatalf("patient read after its wait elapsed: err=%v, want lock timeout", err)
	}
	if elapsed := time.Since(start); elapsed < 300*time.Millisecond {
		t.Fatalf("patient read gave up after %v, before its 300ms wait", elapsed)
	}
}
