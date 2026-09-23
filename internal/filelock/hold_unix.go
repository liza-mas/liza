// The constraint is gofrs/flock's own flock(2) constraint (flock_unix.go).
// Elsewhere it locks with fcntl, which does not exclude flock, so this file
// deliberately leaves such platforms without tryHold: they fail to build
// rather than hold a lock the blocking API cannot see.

//go:build darwin || dragonfly || freebsd || illumos || linux || netbsd || openbsd

package filelock

import (
	"errors"
	"os"
	"syscall"
)

// tryHold opens the lock file itself, rather than through gofrs/flock, because
// the caller needs the locked descriptor to hand to a child process and
// gofrs/flock keeps it private. Both use flock(2) on the same path, so they
// exclude each other.
func tryHold(lockPath string) (*Held, bool, error) {
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return &Held{
		file: file,
		release: func() error {
			unlockErr := syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
			return errors.Join(unlockErr, file.Close())
		},
	}, true, nil
}
