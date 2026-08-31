//go:build windows

package db

import (
	"errors"
	"os"
	"syscall"
	"time"
)

// renameState is the publish primitive. It is a variable so a test can count
// attempts and release the reader that is blocking them.
var renameState = os.Rename

const (
	// publishRetryBudget bounds how long publishState keeps trying.
	//
	// A reader of our own holds the file for well under a millisecond
	// (os.ReadFile over a 116 KB state measured at ~340 µs), so this budget is
	// not sized for those: it is sized for the readers the process does not
	// own — file-sync clients, indexers and scanners walking the project tree —
	// which are the ones that can hold a handle across a whole publish.
	//
	// The budget is also what separates a collision from a real permission
	// failure. Windows reports both as ERROR_ACCESS_DENIED and there is nothing
	// else in the error to tell them apart, so a state file that genuinely
	// cannot be written spends the budget and then surfaces its own error.
	publishRetryBudget = time.Second

	publishRetryInterval = 5 * time.Millisecond
)

// publishState replaces statePath with tmpPath.
//
// On Windows a rename cannot replace a file that another process holds open,
// whatever share mode that process asked for: every share mode was measured to
// fail the same way against the destination, ERROR_ACCESS_DENIED, FILE_SHARE_DELETE
// included. POSIX has no such rule — renaming over an open file always succeeds
// — so publishing state by rename carries a POSIX assumption that does not hold
// here, and a single concurrent reader is enough to fail the write outright.
//
// The rename has a second operand a reader can block: tmpPath itself, freshly
// written and closed microseconds earlier. A handle opened there reports
// ERROR_SHARING_VIOLATION rather than ERROR_ACCESS_DENIED — measured by holding
// tmpPath open across the rename — so both are retried.
//
// Retrying is safe at this point: the caller holds the state lock for the whole
// attempt, which keeps other writers of this file out, and the temp file is
// unique per call, so every retry publishes exactly the bytes the caller
// marshalled. Atomicity is unchanged — one rename still either happens or does
// not.
func publishState(tmpPath, statePath string) error {
	// syscall does not export these by name, so compare numerically, as the
	// process probes in this repository already do.
	const (
		errAccessDenied     = syscall.Errno(5)
		errSharingViolation = syscall.Errno(32)
	)

	deadline := time.Now().Add(publishRetryBudget)
	for {
		err := renameState(tmpPath, statePath)
		if err == nil {
			return nil
		}
		if !errors.Is(err, errAccessDenied) && !errors.Is(err, errSharingViolation) {
			return err
		}
		if !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(publishRetryInterval)
	}
}
