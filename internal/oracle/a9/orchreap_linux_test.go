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
		go orchTrack()
	})
	if orchSubreaperErr != nil {
		t.Fatal("A9 setup: child subreaper unavailable")
	}
}

type orchProc struct {
	ppid, pgrp int
	start      string
	zombie     bool
}

// orchTable reads every process's identity. A zombie keeps these fields, unlike
// its environment, which is why descendants are recorded by identity while they
// live and reaped by identity afterwards.
func orchTable() map[int]orchProc {
	table := map[int]orchProc{}
	entries, _ := filepath.Glob("/proc/[0-9]*")
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
		if len(fields) < 20 {
			continue
		}
		ppid, _ := strconv.Atoi(fields[1])
		pgrp, _ := strconv.Atoi(fields[2])
		table[pid] = orchProc{ppid: ppid, pgrp: pgrp, start: fields[19], zombie: fields[0] == "Z"}
	}
	return table
}

// orchRecord adds to the owner's set every live descendant of its supervisors and
// every live child of this process whose environment carries the scenario HOME
// (the scenario environment is inherited by all its descendants), skipping the
// supervisors themselves: their exec.Cmd waits for them. Other scenarios'
// processes are never added.
func orchRecord(o *orchOwner, table map[int]orchProc) {
	o.mu.Lock()
	defer o.mu.Unlock()
	leader := map[int]bool{}
	frontier := append([]int(nil), o.leaders...)
	for _, pid := range o.leaders {
		leader[pid] = true
	}
	self := os.Getpid()
	for len(frontier) > 0 {
		parent := frontier[0]
		frontier = frontier[1:]
		for pid, proc := range table {
			if proc.ppid == parent && !leader[pid] {
				if _, ok := o.seen[pid]; !ok {
					o.seen[pid] = proc.start
					frontier = append(frontier, pid)
				}
			}
		}
	}
	want := []byte("HOME=" + o.home)
	for pid, proc := range table {
		if proc.ppid != self || proc.zombie || leader[pid] || o.seen[pid] != "" {
			continue
		}
		environ, _ := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/environ")
		for _, variable := range bytes.Split(environ, []byte{0}) {
			if bytes.Equal(variable, want) {
				o.seen[pid] = proc.start
				break
			}
		}
	}
}

// orchTrack records descendants of every scenario while it runs, so a provider is
// known before its supervisor dies and it is orphaned.
func orchTrack() {
	for range time.Tick(20 * time.Millisecond) {
		var owners []*orchOwner
		orchOwners.Range(func(_, value any) bool { owners = append(owners, value.(*orchOwner)); return true })
		if len(owners) == 0 {
			continue
		}
		table := orchTable()
		for _, o := range owners {
			orchRecord(o, table)
		}
	}
}

// orchSnapshot records the owner's descendants at this instant. Cleanup takes it
// just before cancelling a supervisor, which closes the window between two
// tracker ticks.
func orchSnapshot(o *orchOwner) { orchRecord(o, orchTable()) }

// orchReap kills every recorded process that still exists with its recorded
// identity, reaps the ones adopted by this process (zombies included) and fails
// the test if one remains after the deadline.
func orchReap(t testing.TB, o *orchOwner) {
	t.Helper()
	self, ourGroup := os.Getpid(), syscall.Getpgrp()
	// A supervisor's own group first; the kernel never reuses a pid that still
	// names a group, so this reaches only its members.
	o.mu.Lock()
	for _, leader := range o.leaders {
		orchKillGroup(leader)
	}
	o.mu.Unlock()
	deadline := time.Now().Add(10 * time.Second)
	for {
		table := orchTable()
		orchRecord(o, table)
		o.mu.Lock()
		remaining := 0
		for pid, start := range o.seen {
			proc, ok := table[pid]
			if !ok || proc.start != start {
				delete(o.seen, pid)
				continue
			}
			remaining++
			if !proc.zombie {
				if proc.pgrp != ourGroup {
					_ = syscall.Kill(-proc.pgrp, syscall.SIGKILL)
				}
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
			if proc.ppid == self {
				var status syscall.WaitStatus
				_, _ = syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
			}
		}
		o.mu.Unlock()
		if remaining == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Errorf("A9 cleanup: %d scenario processes outlive the test", remaining)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// orchExists reports whether pid names the process recorded with this start time.
func orchExists(pid int, start string) bool {
	proc, ok := orchTable()[pid]
	return ok && proc.start == start
}

// orchZombie reports whether the recorded process has exited and awaits its reaper.
func orchZombie(pid int, start string) bool {
	proc, ok := orchTable()[pid]
	return ok && proc.start == start && proc.zombie
}

func orchKillPID(pid int) { _ = syscall.Kill(pid, syscall.SIGKILL) }
