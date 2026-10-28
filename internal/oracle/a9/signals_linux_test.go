//go:build linux

package a9

import (
	"errors"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

var (
	signalSubreaperOnce sync.Once
	signalSubreaperErr  error
)

// signalSubreaper makes this test process the reaper of every orphaned
// descendant. It is called before the first supervisor start (D45).
func signalSubreaper() error {
	signalSubreaperOnce.Do(func() {
		signalSubreaperErr = launchSubreaper()
	})
	return signalSubreaperErr
}

type signalChild struct {
	pid, pgid int
}

// signalChildrenOf lists the processes whose parent is ppid, zombies included,
// from /proc. A failed observation is an error: it never reads as no children.
func signalChildrenOf(ppid int) ([]signalChild, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	var kids []signalChild
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		raw, err := os.ReadFile("/proc/" + entry.Name() + "/stat")
		if errors.Is(err, fs.ErrNotExist) {
			continue // exited since the listing
		}
		if err != nil {
			return nil, err
		}
		// pid (comm) state ppid pgrp session ...; comm may hold spaces and parentheses.
		stat := string(raw)
		fields := strings.Fields(stat[strings.LastIndexByte(stat, ')')+1:])
		if len(fields) < 4 {
			return nil, errors.New("unreadable process stat")
		}
		parent, err1 := strconv.Atoi(fields[1])
		pgid, err2 := strconv.Atoi(fields[2])
		if err1 != nil || err2 != nil {
			return nil, errors.New("unreadable process stat")
		}
		if parent == ppid {
			kids = append(kids, signalChild{pid, pgid})
		}
	}
	return kids, nil
}

// signalReap uses D48 isolation: the top-level signal tests and their subtests
// are serial, so every child belongs to this test. No session, environment or
// prior observation is needed, including for already-dead adopted children.
// The supervisor has already been waited by exec.Cmd before this is called.
func signalReap(_ int) bool {
	deadline := time.Now().Add(5 * time.Second)
	for {
		kids, err := signalChildrenOf(os.Getpid())
		if err != nil {
			return false
		}
		if len(kids) == 0 {
			return true
		}
		pids := make([]int, 0, len(kids))
		for _, c := range kids {
			if c.pgid > 1 && c.pgid != unix.Getpgrp() {
				_ = unix.Kill(-c.pgid, unix.SIGKILL)
			}
			_ = unix.Kill(c.pid, unix.SIGKILL)
			pids = append(pids, c.pid)
		}
		if launchAwaitExit(pids, time.Until(deadline)) != len(pids) {
			return false
		}
	}
}
