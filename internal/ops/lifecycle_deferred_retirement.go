package ops

import (
	"errors"
	"log"
	"path/filepath"
	"sync"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// A claim that failed after reserving retires its own preparation before
// returning. When that write cannot commit (the state lock is usually saturated
// by the same contention that failed the claim), the invocation still knows its
// reservation is dead, but only until it returns. The supervisor process keeps
// that knowledge here and retries the retirement before its next claim in the
// same project. The retry is the same exact-identity, exact-boundary,
// authority-checked transaction, only later. Process exit discards it: a
// restarted agent's new generation retires the marker instead (D60).

type deferredRetirementKey struct {
	taskID      string
	preparation models.LifecyclePreparation
}

type deferredRetirement struct {
	taskID      string
	authority   *models.AgentAuthority
	preparation models.LifecyclePreparation
}

var deferredRetirements = struct {
	sync.Mutex
	byState map[string]map[deferredRetirementKey]deferredRetirement
}{byState: map[string]map[deferredRetirementKey]deferredRetirement{}}

// errDeferredRetirement stands in for the original failure when a queued
// retirement is retried; the caller never sees it.
var errDeferredRetirement = errors.New("deferred lifecycle preparation retirement")

// deferredRetirementStatePath scopes the queue to one project's state file.
func deferredRetirementStatePath(projectRoot string) (string, bool) {
	statePath, err := filepath.Abs(paths.New(projectRoot).StatePath())
	if err != nil {
		log.Printf("WARNING: lifecycle retirement: cannot resolve state path for %s: %v", projectRoot, err)
		return "", false
	}
	return filepath.Clean(statePath), true
}

// retireOrDeferFailedLifecyclePreparation is retireFailedLifecyclePreparation on
// the patient wait for in-process claim paths, which also queue an uncommitted
// retirement for their process's next claim in this project.
func retireOrDeferFailedLifecyclePreparation(projectRoot, taskID string, authority *models.AgentAuthority, preparation *models.LifecyclePreparation, originalErr error, effects string) error {
	statePath, ok := deferredRetirementStatePath(projectRoot)
	if !ok {
		statePath = paths.New(projectRoot).StatePath()
	}
	err := retireFailedLifecyclePreparation(db.For(statePath).Patient(), taskID, authority, preparation, originalErr, effects)
	if ok && retirementUncommitted(err) {
		deferLifecycleRetirement(statePath, taskID, authority, *preparation)
	}
	return err
}

// retirementUncommitted reports a retirement that failed for a reason a later
// attempt can overcome. Lost authority never becomes retirable again.
func retirementUncommitted(err error) bool {
	var uncommitted *retirementUncommittedError
	return errors.As(err, &uncommitted) && !IsAgentAuthorityError(uncommitted.err)
}

func deferLifecycleRetirement(statePath, taskID string, authority *models.AgentAuthority, preparation models.LifecyclePreparation) {
	entry := deferredRetirement{taskID: taskID, preparation: preparation}
	if authority != nil {
		copied := *authority
		entry.authority = &copied
	}
	deferredRetirements.Lock()
	defer deferredRetirements.Unlock()
	pending := deferredRetirements.byState[statePath]
	if pending == nil {
		pending = map[deferredRetirementKey]deferredRetirement{}
		deferredRetirements.byState[statePath] = pending
	}
	pending[deferredRetirementKey{taskID: taskID, preparation: preparation}] = entry
}

// drainDeferredLifecycleRetirements retries this project's queued retirements.
// Callers hold the project lifecycle shared lock, which the retirement contract
// requires. Entries are detached before any state-lock wait, so concurrent
// drains never retry the same entry and other projects are never touched. A
// failure is logged, never returned: it must not fail the caller's claim.
func drainDeferredLifecycleRetirements(projectRoot string) {
	statePath, ok := deferredRetirementStatePath(projectRoot)
	if !ok {
		return
	}
	deferredRetirements.Lock()
	pending := deferredRetirements.byState[statePath]
	delete(deferredRetirements.byState, statePath)
	deferredRetirements.Unlock()
	if len(pending) == 0 {
		return
	}
	bb := db.For(statePath).Patient()
	for _, entry := range pending {
		err := retireFailedLifecyclePreparation(bb, entry.taskID, entry.authority, &entry.preparation, errDeferredRetirement, "unknown")
		// A committed retirement, a marker that is gone or replaced, and a
		// stale generation all end this entry; only a failed write stays queued.
		if retirementUncommitted(err) {
			log.Printf("WARNING: lifecycle retirement for task %s is still pending: %v", entry.taskID, err)
			deferLifecycleRetirement(statePath, entry.taskID, entry.authority, entry.preparation)
		}
	}
}
