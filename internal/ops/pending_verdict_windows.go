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
)

// retrySharingViolation tolerates a transient Windows sharing collision on a
// pending verdict envelope. Identical concurrent submissions share one
// content-addressed file; while one renames over or removes it, that handle
// holds delete access, which an open by another caller cannot share. Only
// ERROR_SHARING_VIOLATION is retried: ERROR_ACCESS_DENIED also reports real
// permission failures and surfaces immediately.
func retrySharingViolation(op func() error) error {
	const errSharingViolation = syscall.Errno(32)
	deadline := time.Now().Add(pendingVerdictRetryBudget)
	for {
		err := op()
		if err == nil || !errors.Is(err, errSharingViolation) {
			return err
		}
		if !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(pendingVerdictRetryInterval)
	}
}
