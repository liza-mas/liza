package agent

import (
	"sync"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

// Claim-breaker bounds. They describe the supervisor loop, not the user's
// stack, so they are constants rather than configuration (plan D5).
const (
	// claimBreakerThreshold is the number of consecutive identical failures
	// that quarantine a candidate.
	claimBreakerThreshold = 3
	// claimQuarantineCooldown is the first cooldown; it doubles on every failed
	// re-probe up to claimQuarantineCooldownMax.
	claimQuarantineCooldown    = 5 * time.Minute
	claimQuarantineCooldownMax = 30 * time.Minute
	// claimBackoffBase equals the delay the claim loop used before the breaker,
	// so first-failure behavior is unchanged; it doubles up to claimBackoffMax.
	claimBackoffBase = 5 * time.Second
	claimBackoffMax  = 5 * time.Minute
)

// claimBreakerKey identifies one quarantined condition: a candidate, the class
// of the branch that removed it, and the state-derived boundary version the
// failure was observed against (plan D1).
type claimBreakerKey struct {
	Role            string
	TaskID          string
	Class           string
	BoundaryVersion string
}

// claimBreakerCounters is the observable record behind a key, in the shape the
// durable anomaly reports.
type claimBreakerCounters struct {
	Attempts      int
	FirstFailure  time.Time
	LastFailure   time.Time
	CooldownUntil time.Time
}

// claimBreakerDecision is what the supervisor does after a failed claim: stop
// the loop, or wait Delay before the next iteration. Opened names every key
// this observation quarantined or re-quarantined, once each, so the caller can
// record it durably.
type claimBreakerDecision struct {
	Stop   bool
	Delay  time.Duration
	Opened []claimBreakerKey
}

// shorten keeps the shortest wait any tier asked for.
func (d *claimBreakerDecision) shorten(wait time.Duration) {
	if d.Delay == 0 || wait < d.Delay {
		d.Delay = wait
	}
}

// claimFailureObserver is implemented by a strategy that owns a claimBreaker.
// The supervisor consults it by type assertion; strategies without it keep the
// flat delay.
type claimFailureObserver interface {
	ObserveClaimFailure(err error) claimBreakerDecision
}

// claimBreakerScope is the mutable-key part of claimBreakerKey: one entry per
// scope, whose boundary version is replaced — dropping the counters — whenever
// the candidate is observed against a different version.
type claimBreakerScope struct {
	role   string
	taskID string
	class  string
}

type claimBreakerEntry struct {
	boundaryVersion string
	counters        claimBreakerCounters
	cooldown        time.Duration
}

// claimBackoffState is the per-role schedule for classes that are worth
// retrying: the delay doubles while the class repeats and restarts when the
// class changes or a claim succeeds.
type claimBackoffState struct {
	class string
	delay time.Duration
}

// claimBreaker decides, from typed claim failures, which candidates to stop
// offering and how long to wait. It holds no I/O: quarantine is evaluated from
// in-memory counters and task fields only, so the wait loop's tick stays cheap.
type claimBreaker struct {
	mu      sync.Mutex
	entries map[claimBreakerScope]*claimBreakerEntry
	backoff map[string]*claimBackoffState
}

func newClaimBreaker() *claimBreaker {
	return &claimBreaker{
		entries: make(map[claimBreakerScope]*claimBreakerEntry),
		backoff: make(map[string]*claimBackoffState),
	}
}

// quarantineClass reports whether a candidate class is deterministic and
// repair-required — never worth retrying against unchanged state (plan D3).
func quarantineClass(class string) bool {
	switch class {
	case ops.ReviewClaimClassAcceptanceEvidence,
		ops.ReviewClaimClassReviewBoundaryRepair,
		ops.ReviewClaimClassWorktreeContext:
		return true
	}
	return false
}

// stopClass reports whether a candidate-free class ends the supervisor loop
// instead of scheduling another attempt.
func stopClass(class string) bool {
	return class == ops.ReviewClaimClassAuthority || class == ops.ReviewClaimClassDegraded
}

// Observe records one failed claim and returns the supervisor's next move.
// With candidates present the envelope class is ignored and each candidate is
// dispositioned by its own class; without them the envelope class is the only
// class. The delay is the shortest wait any tier asked for: a grown backoff
// when a retryable candidate was seen, the cooldown when only a key opened,
// and the base delay otherwise.
func (b *claimBreaker) Observe(failure *ops.ReviewClaimFailure, now time.Time) claimBreakerDecision {
	if failure == nil {
		return claimBreakerDecision{}
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(failure.Candidates) == 0 {
		return b.observeCandidateFree(failure)
	}

	decision := claimBreakerDecision{}
	for _, candidate := range failure.Candidates {
		if !quarantineClass(candidate.Class) {
			decision.shorten(b.advanceBackoff(failure.Role, candidate.Class))
			continue
		}
		if key, cooldown, opened := b.observeCandidate(failure.Role, candidate, now); opened {
			decision.Opened = append(decision.Opened, key)
			decision.shorten(cooldown)
		}
	}
	if decision.Delay == 0 {
		decision.Delay = claimBackoffBase
	}
	return decision
}

func (b *claimBreaker) observeCandidateFree(failure *ops.ReviewClaimFailure) claimBreakerDecision {
	switch {
	case stopClass(failure.Class):
		return claimBreakerDecision{Stop: true}
	case failure.Class == ops.ReviewClaimClassNoWork:
		// Nothing to back off from: the normal wait resumes untouched.
		return claimBreakerDecision{Delay: claimBackoffBase}
	default:
		return claimBreakerDecision{Delay: b.advanceBackoff(failure.Role, failure.Class)}
	}
}

// advanceBackoff returns the next delay for the role's schedule: the base on a
// fresh schedule or a class change, otherwise double the last, capped.
func (b *claimBreaker) advanceBackoff(role, class string) time.Duration {
	state := b.backoff[role]
	if state == nil {
		state = &claimBackoffState{}
		b.backoff[role] = state
	}
	if state.class != class || state.delay == 0 {
		state.class = class
		state.delay = claimBackoffBase
		return state.delay
	}
	state.delay = min(state.delay*2, claimBackoffMax)
	return state.delay
}

// observeCandidate counts one failure against the candidate's key. The key
// opens on the threshold; after its cooldown has expired a single further
// failure — the permitted re-probe — reopens it with a doubled cooldown. A
// failure counted while the cooldown is still open neither reopens nor extends
// it, so the durable record is updated at most once per cooldown window.
func (b *claimBreaker) observeCandidate(role string, candidate ops.ReviewClaimCandidateFailure, now time.Time) (claimBreakerKey, time.Duration, bool) {
	scope := claimBreakerScope{role: role, taskID: candidate.TaskID, class: candidate.Class}
	entry := b.entries[scope]
	if entry == nil || entry.boundaryVersion != candidate.BoundaryVersion {
		entry = &claimBreakerEntry{
			boundaryVersion: candidate.BoundaryVersion,
			counters:        claimBreakerCounters{FirstFailure: now},
		}
		b.entries[scope] = entry
	}
	entry.counters.Attempts++
	entry.counters.LastFailure = now
	key := claimBreakerKey{Role: role, TaskID: candidate.TaskID, Class: candidate.Class, BoundaryVersion: candidate.BoundaryVersion}

	switch {
	case entry.counters.Attempts < claimBreakerThreshold:
		return key, 0, false
	case entry.counters.Attempts == claimBreakerThreshold:
		entry.cooldown = claimQuarantineCooldown
	case now.Before(entry.counters.CooldownUntil):
		return key, 0, false
	default:
		entry.cooldown = min(entry.cooldown*2, claimQuarantineCooldownMax)
	}
	entry.counters.CooldownUntil = now.Add(entry.cooldown)
	return key, entry.cooldown, true
}

// ObserveSuccess clears the task's keys and the role's backoff after a claim
// succeeded.
func (b *claimBreaker) ObserveSuccess(role, taskID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.backoff, role)
	for scope := range b.entries {
		if scope.role == role && scope.taskID == taskID {
			delete(b.entries, scope)
		}
	}
}

// Quarantined reports whether the task is currently excluded for the role: a
// key for it is open, its cooldown has not expired, and the task's boundary
// version is still the one the failures were observed against. An expired
// cooldown releases the task for as many reads as it takes the next claim to
// probe it; a changed boundary releases it immediately.
func (b *claimBreaker) Quarantined(task *models.Task, role string, now time.Time) bool {
	if task == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.quarantinedLocked(task, role, now)
}

func (b *claimBreaker) quarantinedLocked(task *models.Task, role string, now time.Time) bool {
	version := ops.ReviewClaimBoundaryVersion(task)
	for scope, entry := range b.entries {
		if scope.role != role || scope.taskID != task.ID {
			continue
		}
		if entry.counters.Attempts >= claimBreakerThreshold &&
			now.Before(entry.counters.CooldownUntil) &&
			entry.boundaryVersion == version {
			return true
		}
	}
	return false
}

// ClaimableAfterQuarantine counts the tasks the agent could claim for the role
// once quarantined candidates are set aside. It repeats the two predicates of
// models.CountReviewableTasksForAgent, which takes no extra filter.
func (b *claimBreaker) ClaimableAfterQuarantine(state *models.State, role, agentID string, pr models.PipelineResolver, now time.Time) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	count := 0
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if !task.IsClaimable(role, state.Tasks, pr) || task.HasApprovalFromAgent(agentID) {
			continue
		}
		if b.quarantinedLocked(task, role, now) {
			continue
		}
		count++
	}
	return count
}

// Counters returns the record behind an exact key, for the caller that writes
// the durable anomaly.
func (b *claimBreaker) Counters(key claimBreakerKey) (claimBreakerCounters, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	entry := b.entries[claimBreakerScope{role: key.Role, taskID: key.TaskID, class: key.Class}]
	if entry == nil || entry.boundaryVersion != key.BoundaryVersion {
		return claimBreakerCounters{}, false
	}
	return entry.counters, true
}
