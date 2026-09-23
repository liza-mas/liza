package filelock

import "os"

// Held is an exclusive lock taken by TryHold. It stays held until Release,
// unlike the With*Lock calls, which scope the lock to a callback.
type Held struct {
	file    *os.File
	release func() error
}

// TryHold takes the exclusive lock without waiting. acquired is false, with no
// error, when another holder has it.
//
// Where the platform allows, File returns the locked descriptor so a child
// process can inherit it: see File.
func (fl *FileLock) TryHold(operation string) (held *Held, acquired bool, err error) {
	held, acquired, err = tryHold(fl.lockPath)
	if err != nil {
		return nil, false, ClassifyLockError(err)
	}
	if !acquired {
		return nil, false, nil
	}
	fl.writeOwnerMetadata(operation)
	return held, true, nil
}

// File returns the descriptor that carries the lock, or nil on Windows, where
// a lock cannot pass to a child.
//
// On Unix the lock belongs to the open file description, so a child started
// with this file in exec.Cmd.ExtraFiles keeps the lock held for as long as it,
// or anything it passes the descriptor to, keeps it open, even if this process
// dies. Call Release only after such children have exited: releasing unlocks
// the shared description for them too.
func (h *Held) File() *os.File {
	return h.file
}

// Release unlocks and closes the lock file. It leaves the file in place, for
// the reason given in withLockOperationContext.
func (h *Held) Release() error {
	return h.release()
}
