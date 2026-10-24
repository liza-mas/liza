//go:build linux

package a9

import (
	"errors"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// launchAwaitsExit reports that launchAwaitExit can wait for the exit of a
// scenario process.
const launchAwaitsExit = true

// launchSubreaper makes the test process the reaper of its orphaned descendants
// (D45): a process whose parent exits is reparented to the test process instead
// of init, so its exit becomes a status the test can collect. Setting it again is
// harmless. It must run before the scenario's first process starts.
func launchSubreaper() error {
	return unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0)
}

// launchAwaitExit waits for each of pids to exit and collects its status, giving
// up at limit. It returns how many it collected. A pid that is not a child of the
// test process (ECHILD) is not collected, so a caller that gets zero knows there
// was nothing it could wait for or that the limit expired.
//
// It waits for the given pids only: a wait for any child would take the status
// that a concurrent test's exec.Cmd.Wait is blocked on. A wait still pending at
// the limit is abandoned and ends when its process does.
func launchAwaitExit(pids []int, limit time.Duration) int {
	collected := make(chan bool, len(pids))
	for _, pid := range pids {
		go func() { collected <- launchCollect(pid) }()
	}
	timer := time.NewTimer(limit)
	defer timer.Stop()
	n := 0
	for range pids {
		select {
		case ok := <-collected:
			if ok {
				n++
			}
		case <-timer.C:
			return n
		}
	}
	return n
}

// launchCollect blocks until the child pid exits and reports whether it
// collected its status.
func launchCollect(pid int) bool {
	var status syscall.WaitStatus
	for {
		_, err := syscall.Wait4(pid, &status, 0, nil)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		return err == nil
	}
}
