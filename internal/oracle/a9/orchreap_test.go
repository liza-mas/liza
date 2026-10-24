package a9

import (
	"sync"
	"testing"
)

// Decision D45: cancelling a supervisor leaves a provider it started in a detached
// process group running after the project is removed. Every supervisor these
// tests own is therefore tracked, and its descendants are signalled and reaped
// when the test ends on any path. The helper is task-owned and small by D45;
// consolidating it into the kit is recorded debt (payback: the first A9-area
// refactor task). Linux makes this test process the reaper of orphans through a
// child subreaper (orchreap_linux_test.go). Other Unix hosts have no subreaper:
// cleanup is best-effort termination of each supervisor's process group
// (orchreap_other_test.go) and detached providers are not tracked.

// orchOwner is the process set one scenario owns.
type orchOwner struct {
	home string
	mu   sync.Mutex
	// leaders are the supervisor pids; their exec.Cmd owns their wait.
	leaders []int
	// seen maps every descendant observed so far to its start time, so a
	// process that exits before it is adopted is still reaped by identity.
	seen map[int]string
}

var orchOwners sync.Map // scenario HOME -> *orchOwner

func (o *orchOwner) addLeader(pid int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.leaders = append(o.leaders, pid)
}

// orchGuard makes this test process the reaper of the scenario's orphaned
// descendants before its first supervisor starts, and registers their reaping for
// the end of the test. It registers first so its cleanup runs after the
// supervisor's own.
func orchGuard(t *testing.T, p *scenario) *orchOwner {
	t.Helper()
	orchSubreaper(t)
	value, loaded := orchOwners.LoadOrStore(p.home, &orchOwner{home: p.home, seen: map[int]string{}})
	o := value.(*orchOwner)
	if !loaded {
		t.Cleanup(func() {
			orchReap(t, o)
			orchOwners.Delete(p.home)
		})
	}
	return o
}
