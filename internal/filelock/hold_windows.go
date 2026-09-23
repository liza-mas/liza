//go:build windows

package filelock

import "github.com/gofrs/flock"

// tryHold uses gofrs/flock. A Windows lock belongs to the process that took
// it, so there is no descriptor worth handing to a child and File returns nil.
func tryHold(lockPath string) (*Held, bool, error) {
	lock := flock.New(lockPath)
	acquired, err := lock.TryLock()
	if err != nil || !acquired {
		return nil, false, err
	}
	return &Held{release: lock.Unlock}, true, nil
}
