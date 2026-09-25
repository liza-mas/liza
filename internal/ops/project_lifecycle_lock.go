package ops

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/liza-mas/liza/internal/filelock"
)

const projectLifecycleLockTimeout = 30 * time.Minute

// Project lifecycle locking is intentionally limited to operations that
// establish a live agent or create or recover Git worktree resources. Those
// effects can outlive project-local blackboard state and would become orphaned
// or lost if cleanup raced them. Ordinary blackboard writes stay outside this
// lock because cleanup is explicitly allowed to discard that state.

// WithProjectLifecycleSharedLock prevents project cleanup while fn performs a
// protected lifecycle operation. Shared holders may run concurrently.
func WithProjectLifecycleSharedLock(projectRoot, operation string, fn func() error) error {
	lock, err := projectLifecycleLock(projectRoot)
	if err != nil {
		// Non-repository callers cannot race with project cleanup because cleanup
		// requires an existing Git directory before it can delete any target.
		// Preserve their operation's own validation and error contract.
		if errors.Is(err, os.ErrNotExist) {
			return fn()
		}
		return err
	}
	return withProjectLifecycleLock(lock, operation, true, projectLifecycleLockTimeout, fn)
}

// WithProjectLifecycleExclusiveLock prevents new agent registrations and
// worktree creation while cleanup revalidates and deletes project state.
func WithProjectLifecycleExclusiveLock(projectRoot, operation string, fn func() error) error {
	lock, err := projectLifecycleLock(projectRoot)
	if err != nil {
		return err
	}
	return withProjectLifecycleLock(lock, operation, false, projectLifecycleLockTimeout, fn)
}

func withProjectLifecycleLock(lock *filelock.FileLock, operation string, shared bool, timeout time.Duration, fn func() error) error {
	lock = lock.WithTimeout(timeout)
	// A timeout fn returns belongs to a lock taken inside; naming this lock for
	// it sends the operator after the wrong holder (D60).
	entered := false
	enter := func() error { entered = true; return fn() }
	var err error
	if shared {
		err = lock.WithSharedLockOperation(operation, enter)
	} else {
		err = lock.WithLockOperation(operation, enter)
	}
	if !entered && filelock.IsLockErrorType(err, filelock.LockErrorTimeout) {
		return fmt.Errorf("project lifecycle operation %q could not acquire the lock within %s; one or more cleanup, agent registration, or worktree provisioning/recovery operations are still running; wait for those operations to finish and retry: %w", operation, timeout, err)
	}
	return err
}

func projectLifecycleLock(projectRoot string) (*filelock.FileLock, error) {
	return projectFileLock(projectRoot, "project-lifecycle")
}
