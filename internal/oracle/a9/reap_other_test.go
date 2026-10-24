//go:build !linux

package a9

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// launchAwaitsExit reports that launchAwaitExit cannot wait for the exit of a
// scenario process on this platform.
const launchAwaitsExit = false

// launchSubreaper does nothing: only Linux lets a process adopt its orphaned
// descendants (D45), so on other platforms a scenario process whose parent exits
// leaves the test's reach and its exit cannot be waited for.
func launchSubreaper() error { return nil }

// launchAwaitExit waits for nothing: the exit of a reparented descendant cannot
// be observed here without polling (see launchReapChildren).
func launchAwaitExit([]int, time.Duration) int { return 0 }

// awaitRefusalDecision adds nothing to the entry barrier (D50): only Linux
// exposes the supervisor's inotify descriptor, the post-decision observable. The
// residual gap is that a supervisor descheduled between publishing its lock owner
// record and evaluating the claim can be stopped before a wrongly admitted claim
// surfaces. The eligibility logic is platform-independent, so the Linux runs
// carry the regression coverage.
func awaitRefusalDecision(*testing.T, *scenario, int) {}

// D45 accepts best-effort group termination on non-Linux platforms. There is
// no portable subreaper or way to wait for a reparented descendant here, so this
// makes one pass with no wait loop or claim that all descendants were collected.
func launchReapChildren(o *launchOwner) (int, error) {
	if o == nil {
		return 0, nil // The deadline path already called launchKillTree.
	}
	live, err := o.scan()
	if err != nil {
		return 0, err
	}
	groups := map[int]bool{}
	for _, child := range live {
		if child.pgid > 1 && !groups[child.pgid] {
			groups[child.pgid] = true
			cmd := exec.Command("kill", "-KILL", "-"+strconv.Itoa(child.pgid))
			cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
			_ = cmd.Run()
		}
		if proc, err := os.FindProcess(child.pid); err == nil {
			_ = proc.Kill()
		}
	}
	return len(live), nil
}

// scan is the non-Linux best-effort fallback. Linux must use all direct
// children, including zombies, instead of these observed scenario members.
func (o *launchOwner) scan() ([]launchProc, error) {
	procs, err := launchProcesses()
	if err != nil {
		return nil, err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	self, parent, selfGroup := os.Getpid(), os.Getppid(), 0
	for _, p := range procs {
		if p.pid == self {
			selfGroup = p.pgid
		}
	}
	for grown := true; grown; {
		grown = false
		for _, p := range procs {
			if p.pid == self || p.pid == parent {
				continue
			}
			known := o.members[p.pid]
			if !known && (strings.Contains(p.command, o.root+"/") || o.members[p.ppid] || (o.groups[p.pgid] && p.pgid != selfGroup)) {
				o.members[p.pid], grown = true, true
				known = true
			}
			if known && p.pgid != selfGroup && !o.groups[p.pgid] {
				o.groups[p.pgid], grown = true, true
			}
		}
	}
	var live []launchProc
	for _, p := range procs {
		if o.members[p.pid] {
			live = append(live, p)
		}
	}
	return live, nil
}
