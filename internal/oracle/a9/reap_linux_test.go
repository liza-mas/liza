//go:build linux

package a9

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
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

// awaitRefusalDecision waits until the supervisor pid has decided to refuse a
// claim (D50). waitForWorkEventDriven creates its state watcher, an inotify
// instance, only after checkWork has refused, so an inotify descriptor under
// /proc/<pid>/fd is a post-decision observable. A wrongly admitted claim returns
// before any watcher exists, so this await fails instead of passing. The
// supervisor holds no inotify descriptor before that point.
func awaitRefusalDecision(t *testing.T, p *scenario, pid int) {
	t.Helper()
	dir := filepath.Join("/proc", strconv.Itoa(pid), "fd")
	p.await(t, "refusal decision", func() bool {
		fds, err := os.ReadDir(dir)
		if err != nil {
			return false
		}
		for _, fd := range fds {
			if target, err := os.Readlink(filepath.Join(dir, fd.Name())); err == nil && target == "anon_inode:inotify" {
				return true
			}
		}
		return false
	})
}

// launchReapChildren uses D48's isolation: supervisor tests are top-level,
// nonparallel tests without parallel subtests. Every child belongs to the current
// test, including zombies and detached descendants adopted by the subreaper.
// No scenario ownership or environment filter may exclude a child.
func launchReapChildren(_ *launchOwner) (int, error) {
	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	killed := map[int]bool{}
	for {
		select {
		case <-timer.C:
			return len(killed), errors.New("child reaping deadline exceeded")
		default:
		}
		children, err := launchChildren()
		if err != nil {
			return len(killed), err
		}
		if len(children) == 0 {
			// Assert the postcondition with a fresh enumeration, after collecting
			// every exit. No surviving child can create a later orphan.
			remaining, err := launchChildren()
			if err != nil {
				return len(killed), err
			}
			if len(remaining) != 0 {
				return len(killed), errors.New("child remained after reaping")
			}
			return len(killed), nil
		}
		for _, child := range children {
			if child.pgid != unix.Getpgrp() {
				if err := unix.Kill(-child.pgid, unix.SIGKILL); err != nil && !errors.Is(err, unix.ESRCH) {
					return len(killed), errors.New("cannot signal child process group")
				}
			}
			if err := unix.Kill(child.pid, unix.SIGKILL); err != nil && !errors.Is(err, unix.ESRCH) {
				return len(killed), errors.New("cannot signal child process")
			}
			if !child.zombie {
				killed[child.pid] = true
			}
		}
		collected := make(chan error, len(children))
		for _, child := range children {
			go func() { collected <- launchReapPid(child.pid) }()
		}
		for range children {
			select {
			case err := <-collected:
				if err != nil {
					return len(killed), err
				}
			case <-timer.C:
				return len(killed), errors.New("child reaping deadline exceeded")
			}
		}
		// Killing a leader reparents its descendants; enumerate again to reap
		// them too, regardless of when they exited or which group they used.
	}
}

func launchReapPid(pid int) error {
	var status syscall.WaitStatus
	for {
		_, err := syscall.Wait4(pid, &status, 0, nil)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		// exec.Cmd.Wait may already own a direct child's status on a deadline
		// path. The final enumeration still proves the child is gone.
		if errors.Is(err, syscall.ECHILD) {
			return nil
		}
		return err
	}
}

type launchChild struct {
	pid, pgid int
	zombie    bool
}

func launchChildren() ([]launchChild, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, errors.New("child process listing unavailable")
	}
	var children []launchChild
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || !entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		// A process can exit between listing and reading its stat: the open then
		// fails with ENOENT, or the read fails with ESRCH once the task is gone.
		// Any other errno is a real failure and is reported.
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, unix.ESRCH) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("A9 cleanup: child process stat unavailable: %w", err)
		}
		// comm may contain spaces and parentheses. Fields after its closing
		// parenthesis are state, ppid, pgrp, ...; zombies retain all three.
		end := strings.LastIndexByte(string(data), ')')
		fields := strings.Fields(string(data)[end+1:])
		if end < 0 || len(fields) < 3 {
			return nil, errors.New("invalid child process stat")
		}
		ppid, err := strconv.Atoi(fields[1])
		if err != nil {
			return nil, errors.New("invalid child parent pid")
		}
		if ppid != os.Getpid() {
			continue
		}
		pgid, err := strconv.Atoi(fields[2])
		if err != nil || pgid <= 0 {
			return nil, errors.New("invalid child process group")
		}
		children = append(children, launchChild{pid, pgid, fields[0] == "Z"})
	}
	return children, nil
}

// A zombie has no command or environment to correlate with a scenario. D48
// requires cleanup to reap it even when no tracker ever observed it.
func TestOracleA9LaunchReapsZombie(t *testing.T) {
	own := newLaunchOwner(t.TempDir())
	t.Cleanup(func() { launchCleanup(t, "zombie", own) })
	cmd := exec.Command("sh", "-c", "exit 0")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	spawnDetach(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal("A9 zombie probe: start child")
	}
	defer cmd.Process.Release()
	// Observe exit without consuming the status, leaving a real zombie that was
	// never registered in own.members or own.groups.
	var info unix.Siginfo
	if err := unix.Waitid(unix.P_PID, cmd.Process.Pid, &info, unix.WEXITED|unix.WNOWAIT, nil); err != nil {
		t.Fatal("A9 zombie probe: wait for zombie")
	}
	launchReap(t, own)
	var status syscall.WaitStatus
	if _, err := syscall.Wait4(cmd.Process.Pid, &status, syscall.WNOHANG, nil); !errors.Is(err, syscall.ECHILD) {
		t.Fatal("A9 zombie probe: cleanup left an unobserved zombie child")
	}
}
