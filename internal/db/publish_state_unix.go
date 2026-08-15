//go:build !windows

package db

import "os"

// publishState replaces statePath with tmpPath.
//
// A POSIX rename succeeds over a file other processes hold open, so publishing
// needs nothing beyond the rename itself. The Windows build carries the retry
// that its filesystem semantics require.
func publishState(tmpPath, statePath string) error {
	return os.Rename(tmpPath, statePath)
}
