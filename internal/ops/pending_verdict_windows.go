//go:build windows

package ops

import (
	"errors"
	"syscall"
	"time"
)

const (
	pendingVerdictRetryBudget   = time.Second
	pendingVerdictRetryInterval = 5 * time.Millisecond

	errAccessDenied     = syscall.Errno(5)
	errSharingViolation = syscall.Errno(32)
)

// retrySharingViolation tolerates a transient Windows sharing collision on a
// pending verdict envelope. Identical concurrent submissions share one
// content-addressed file; while one renames over or removes it, that handle
// holds delete access, which an open by another caller cannot share. Only
// ERROR_SHARING_VIOLATION is retried: ERROR_ACCESS_DENIED also reports real
// permission failures and surfaces immediately.
func retrySharingViolation(op func() error) error {
	return retryPendingVerdictCollision(op, errSharingViolation)
}

// retryReplaceCollision tolerates a rename onto an envelope another caller
// holds open or is deleting: Windows reports both as ERROR_ACCESS_DENIED,
// indistinguishable from a real permission failure, which therefore spends the
// budget before surfacing. Retrying is safe: the temp file is unique per call
// and the envelope name derives from its bytes, so any replacement is identical.
func retryReplaceCollision(op func() error) error {
	return retryPendingVerdictCollision(op, errAccessDenied, errSharingViolation)
}

func retryPendingVerdictCollision(op func() error, transient ...syscall.Errno) error {
	deadline := time.Now().Add(pendingVerdictRetryBudget)
	for {
		err := op()
		if err == nil || !isTransientErrno(err, transient) || !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(pendingVerdictRetryInterval)
	}
}

func isTransientErrno(err error, transient []syscall.Errno) bool {
	for _, errno := range transient {
		if errors.Is(err, errno) {
			return true
		}
	}
	return false
}
