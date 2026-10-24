//go:build linux

package a9

import (
	"bytes"
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
		signalSubreaperErr = unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0)
	})
	return signalSubreaperErr
}

type signalChild struct {
	pid, pgid, sid int
	zombie         bool
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
		sid, err3 := strconv.Atoi(fields[3])
		if err1 != nil || err2 != nil || err3 != nil {
			return nil, errors.New("unreadable process stat")
		}
		if parent == ppid {
			kids = append(kids, signalChild{pid, pgid, sid, fields[0] == "Z"})
		}
	}
	return kids, nil
}

// owns reports whether the child belongs to the scenario: it is in the
// supervisor's session, or its environment carries the scenario's HOME (the
// name and value are matched, never reported). A zombie has no environment, so
// a child is attributed while it lives.
func (o signalOwner) owns(c signalChild) bool {
	if c.sid == o.sid {
		return true
	}
	env, err := os.ReadFile("/proc/" + strconv.Itoa(c.pid) + "/environ")
	return err == nil && bytes.Contains(append([]byte{0}, env...), []byte("\x00HOME="+o.home+"\x00"))
}

// signalReap kills the scenario's supervisor session and every child of this
// test process the scenario owns, with its process group, and reaps them, until
// none is left. Because the test process is the subreaper, an orphan is a child
// whatever the exit order of its ancestors, but only once its parent is dead:
// the supervisor, which stays with its exec.Cmd to be reaped, counts as left
// until it is a zombie or gone, since a descendant is not yet ours before that.
// It reports whether none was left before the deadline; a failed observation
// reports false.
func signalReap(o signalOwner) bool {
	owned := map[int]bool{}
	deadline := time.Now().Add(5 * time.Second)
	for {
		_ = unix.Kill(-o.sid, unix.SIGKILL)
		kids, err := signalChildrenOf(os.Getpid())
		if err != nil {
			return false
		}
		left := 0
		for _, c := range kids {
			if c.pid == o.sid {
				if !c.zombie {
					left++
				}
				continue
			}
			if !owned[c.pid] && !o.owns(c) {
				continue
			}
			owned[c.pid] = true
			left++
			if c.pgid > 1 && c.pgid != unix.Getpgrp() {
				_ = unix.Kill(-c.pgid, unix.SIGKILL)
			}
			_ = unix.Kill(c.pid, unix.SIGKILL)
			_, _ = unix.Wait4(c.pid, nil, unix.WNOHANG, nil)
		}
		if left == 0 {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
}
