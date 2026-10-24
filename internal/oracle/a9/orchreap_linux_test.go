//go:build linux

package a9

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

var (
	orchSubreaperOnce sync.Once
	orchSubreaperErr  error
)

// orchSubreaper makes this process the parent of its orphaned descendants. It
// must run before the first supervisor start; it is process-wide and idempotent.
func orchSubreaper(t testing.TB) {
	t.Helper()
	orchSubreaperOnce.Do(func() {
		orchSubreaperErr = unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0)
	})
	if orchSubreaperErr != nil {
		t.Fatal("A9 setup: child subreaper unavailable")
	}
}

type orchChild struct {
	pid, pgrp int
	zombie    bool
}

// orchChildren lists the actual children of this process, adopted orphans and
// zombies included: ownership needs no environment or earlier observation.
func orchChildren() []orchChild {
	self := os.Getpid()
	entries, _ := filepath.Glob("/proc/[0-9]*")
	var children []orchChild
	for _, entry := range entries {
		pid, err := strconv.Atoi(filepath.Base(entry))
		if err != nil {
			continue
		}
		stat, err := os.ReadFile(filepath.Join(entry, "stat"))
		if err != nil {
			continue
		}
		// "pid (comm) state ppid pgrp ...": comm may hold spaces and parentheses.
		fields := strings.Fields(string(stat[bytes.LastIndexByte(stat, ')')+1:]))
		if len(fields) < 3 {
			continue
		}
		if ppid, _ := strconv.Atoi(fields[1]); ppid == self {
			pgrp, _ := strconv.Atoi(fields[2])
			children = append(children, orchChild{pid: pid, pgrp: pgrp, zombie: fields[0] == "Z"})
		}
	}
	return children
}

// orchReap signals every child of the test process and its process group, reaps
// it, and fails the test if one remains after the deadline. The supervisor has
// already been waited for by its own cleanup.
func orchReap(t testing.TB) {
	t.Helper()
	ourGroup := syscall.Getpgrp()
	deadline := time.Now().Add(10 * time.Second)
	for {
		children := orchChildren()
		if len(children) == 0 {
			return
		}
		for _, child := range children {
			if !child.zombie {
				if child.pgrp != ourGroup {
					_ = syscall.Kill(-child.pgrp, syscall.SIGKILL)
				}
				_ = syscall.Kill(child.pid, syscall.SIGKILL)
			}
			var status syscall.WaitStatus
			_, _ = syscall.Wait4(child.pid, &status, syscall.WNOHANG, nil)
		}
		if time.Now().After(deadline) {
			t.Errorf("A9 cleanup: %d children of the test process outlive the test", len(orchChildren()))
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func orchKillPID(pid int) { _ = syscall.Kill(pid, syscall.SIGKILL) }
