//go:build windows

package db

import (
	"errors"
	"os"
	"syscall"
	"time"
)

// readStateAttempt lets tests release a conflicting handle after it has
// actually blocked a read, without relying on scheduling or timers.
var readStateAttempt = os.ReadFile

// readStateFile tolerates a transient Windows sharing collision while a
// publication or another filesystem user holds a conflicting handle. It does
// not acquire the state lock: each successful attempt reads one whole file,
// closes it, and only then lets the caller decode the bytes.
func readStateFile(statePath string) ([]byte, error) {
	const errSharingViolation = syscall.Errno(32)
	deadline := time.Now().Add(publishRetryBudget)
	for {
		data, err := readStateAttempt(statePath)
		if err == nil || !errors.Is(err, errSharingViolation) {
			return data, err
		}
		if !time.Now().Before(deadline) {
			return nil, err
		}
		time.Sleep(publishRetryInterval)
	}
}
