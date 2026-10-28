//go:build linux

package a9

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
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
// it with a blocking wait bounded by a deadline, and repeats the enumeration
// until no child remains; it fails the test if one outlives the deadline. The
// supervisor has already been waited for by its own cleanup.
func orchReap(t testing.TB) {
	t.Helper()
	ourGroup := syscall.Getpgrp()
	deadline := time.After(10 * time.Second)
	for {
		children := orchChildren()
		if len(children) == 0 {
			return
		}
		reaped := make(chan struct{}, len(children))
		for _, child := range children {
			// Even a zombie can lead a group with live descendants.
			if child.pgrp > 0 && child.pgrp != ourGroup {
				_ = syscall.Kill(-child.pgrp, syscall.SIGKILL)
			}
			_ = syscall.Kill(child.pid, syscall.SIGKILL)
			go func(pid int) {
				var status syscall.WaitStatus
				_, _ = syscall.Wait4(pid, &status, 0, nil)
				reaped <- struct{}{}
			}(child.pid)
		}
		for range children {
			select {
			case <-reaped:
			case <-deadline:
				t.Errorf("A9 cleanup: %d children of the test process outlive the test", len(orchChildren()))
				return
			}
		}
	}
}

func orchKillPID(pid int) { _ = syscall.Kill(pid, syscall.SIGKILL) }

// The helper leaves an exited, unwaited child before exiting itself. The outer
// test waits for that parent before its first process scan: the zombie is already
// adopted, and no tracker or environment attribution can have registered it.
func TestOracleA9OrchestratorAdoptedZombie(t *testing.T) {
	const helper = "ORACLE_A9_ORCH_ZOMBIE"
	if pidFile := os.Getenv(helper); pidFile != "" {
		child := exec.Command("/bin/sh", "-c", "exit 0")
		var err error
		child.Env, err = oracle.ChildEnv(oracle.EnvBase{
			Home: os.Getenv("HOME"), Path: os.Getenv("PATH"), Tmp: os.Getenv("TMPDIR"),
		})
		if err != nil {
			t.Fatal("A9 zombie helper: environment")
		}
		orchDetach(child)
		if err := child.Start(); err != nil {
			t.Fatal("A9 zombie helper: child start")
		}
		// WNOWAIT observes exit but deliberately leaves the child waitable.
		if err := unix.Waitid(unix.P_PID, child.Process.Pid, nil, unix.WEXITED|unix.WNOWAIT, nil); err != nil {
			t.Fatal("A9 zombie helper: child exit")
		}
		if err := os.WriteFile(pidFile, []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
			t.Fatal("A9 zombie helper: child pid")
		}
		return
	}
	root := t.TempDir()
	orchGuard(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal("A9 zombie setup: test executable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	parent := exec.CommandContext(ctx, executable, "-test.run=^TestOracleA9OrchestratorAdoptedZombie$")
	pidFile := filepath.Join(root, "zombie-pid")
	parent.Env, err = oracle.ChildEnv(oracle.EnvBase{Home: root, Path: root, Tmp: root}, helper+"="+pidFile)
	if err != nil {
		t.Fatal("A9 zombie setup: environment")
	}
	if err := parent.Run(); err != nil {
		t.Fatal("A9 zombie setup: parent exit")
	}
	pid, err := strconv.Atoi(string(mustRead(t, pidFile)))
	if err != nil {
		t.Fatal("A9 zombie setup: child pid")
	}
	found := false
	for _, child := range orchChildren() {
		if child.pid == pid {
			found = true
			if !child.zombie {
				t.Fatal("A9 zombie setup: child must have exited before discovery")
			}
		}
	}
	if !found {
		t.Fatal("A9 zombie setup: exited child must be adopted before discovery")
	}
	orchReap(t)
	if left := orchChildren(); len(left) != 0 {
		t.Fatalf("A9 cleanup: %d children remain after reaping the undiscovered zombie", len(left))
	}
}
