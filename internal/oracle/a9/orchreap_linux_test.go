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

// Decision D45: the supervisor starts providers in a detached process group, so
// cancelling the supervisor leaves a held provider running after its project is
// removed. A Linux subreaper makes every orphaned descendant a child of this test
// process, where it can be found, signalled and reaped whatever its leader did.
// The helper is task-owned and small by D45; consolidating it into the kit is
// recorded debt (payback: the first A9-area refactor task).

var (
	orchSubreaperOnce sync.Once
	orchSubreaperErr  error
)

// orchSubreaper makes this process the reaper of its orphaned descendants. It
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

// orchChildren lists the live children of this process that belong to the
// scenario whose HOME is home: the scenario environment is inherited by every
// descendant, so it attributes a reparented process to its test without touching
// the children of parallel scenarios. Zombies carry no environment and are
// reaped by orchReap through the pids it signalled.
func orchChildren(home string) []int {
	self := os.Getpid()
	entries, _ := filepath.Glob("/proc/[0-9]*")
	want := []byte("HOME=" + home)
	var pids []int
	for _, entry := range entries {
		pid, err := strconv.Atoi(filepath.Base(entry))
		if err != nil || pid == self {
			continue
		}
		stat, err := os.ReadFile(filepath.Join(entry, "stat"))
		if err != nil {
			continue
		}
		// "pid (comm) state ppid ...": comm may hold spaces and parentheses.
		fields := strings.Fields(string(stat[bytes.LastIndexByte(stat, ')')+1:]))
		if len(fields) < 2 || fields[1] != strconv.Itoa(self) {
			continue
		}
		environ, err := os.ReadFile(filepath.Join(entry, "environ"))
		if err != nil {
			continue
		}
		for _, variable := range bytes.Split(environ, []byte{0}) {
			if bytes.Equal(variable, want) {
				pids = append(pids, pid)
				break
			}
		}
	}
	return pids
}

// orchReap signals every remaining scenario child and its process group, reaps
// them, and fails the test if one outlives the deadline.
func orchReap(t testing.TB, home string) {
	t.Helper()
	signalled := map[int]bool{}
	deadline := time.Now().Add(10 * time.Second)
	for {
		for _, pid := range orchChildren(home) {
			if group, err := syscall.Getpgid(pid); err == nil && group != syscall.Getpgrp() {
				_ = syscall.Kill(-group, syscall.SIGKILL)
			}
			_ = syscall.Kill(pid, syscall.SIGKILL)
			signalled[pid] = true
		}
		for pid := range signalled {
			var status syscall.WaitStatus
			if reaped, err := syscall.Wait4(pid, &status, syscall.WNOHANG, nil); reaped == pid || err == syscall.ECHILD {
				delete(signalled, pid)
			}
		}
		if len(signalled) == 0 && len(orchChildren(home)) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Errorf("A9 cleanup: %d scenario children outlive the test", len(signalled)+len(orchChildren(home)))
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// orchAlive reports whether pid still names a process.
func orchAlive(pid int) bool { return syscall.Kill(pid, 0) == nil }
