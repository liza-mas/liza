package ops

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/referencecontract"
)

// admissionSlotPollInterval is how often a waiter re-reads its class queue;
// tests shorten it.
var admissionSlotPollInterval = time.Second

// defaultAdmissionLimitRefreshInterval is the initial admissionLimitRefreshInterval.
const defaultAdmissionLimitRefreshInterval = 10 * time.Second

// admissionLimitRefreshInterval spaces the re-reads of a full class's limit
// by its head waiter, so a changed max_concurrent reaches waiters already
// queued (D-180); tests shorten it.
var admissionLimitRefreshInterval = defaultAdmissionLimitRefreshInterval

// Lock operation labels of the slot protocol, recorded in the lock owner
// metadata. A ticket's lock is held while it waits; a slot's while it runs.
const (
	admissionQueueLockOperation = "admission-slot-queue"
	admissionWaitLockOperation  = "admission-slot-wait"
	admissionSlotLockOperation  = "admission-slot"
	admissionPruneLockOperation = "admission-slot-prune"
)

// admissionQueueDirName is the per-class directory of waiter tickets.
const admissionQueueDirName = "queue"

// admissionDirMode is the mode of the per-class queue directory.
const admissionDirMode = 0o755

// admissionTicketNonceBytes is the random part of a ticket name, which keeps
// tickets created in the same nanosecond apart.
const admissionTicketNonceBytes = 8

// Lock-file names filelock derives from a protected path (filelock.go): the
// lock itself and its owner metadata. filelock exports no constant for them.
const (
	admissionLockFileSuffix  = ".lock"
	admissionOwnerFileSuffix = ".lock.owner.json"
)

// admissionSlotCap bounds a slot wait plus the execution budget behind it;
// tests shorten it.
var admissionSlotCap = referencecontract.AcceptanceMaxTimeoutSeconds * time.Second

// SubmitSlotWaitFloor is the least a submit may wait for its slots, whatever
// its budget; MaxInflightSubmitGrace includes it (D-178).
const SubmitSlotWaitFloor = 2 * time.Hour

// submitSlotWaitFloor is SubmitSlotWaitFloor; tests shorten it.
var submitSlotWaitFloor = SubmitSlotWaitFloor

// admissionSlotWait is the longest a command budgeted budgetSeconds may wait
// for its slots: a wait plus its execution never exceeds the engine's
// acceptance cap (ADR-0209). It bounds callers no in-flight submit grace
// covers: update-review-commit and the orphaned-base heal.
func admissionSlotWait(budgetSeconds int) time.Duration {
	return max(0, admissionSlotCap-acceptanceBatchTimeout(budgetSeconds))
}

// submitAdmissionSlotWait is a submit's slot wait: never under the floor, so a
// batch budgeted at the cap still queues. Wait plus batch stay within the cap
// plus the floor, which the in-flight submit grace covers (D-178).
func submitAdmissionSlotWait(budgetSeconds int) time.Duration {
	return max(submitSlotWaitFloor, admissionSlotWait(budgetSeconds))
}

// AdmissionSlotWaitError reports that a class had no free slot before the
// wait deadline, or that the wait was cancelled (Err holds the cause). No
// command ran under it.
type AdmissionSlotWaitError struct {
	Class  string
	Waited time.Duration
	// Dir holds the class's slot locks, where a holder can be found.
	Dir string
	Err error
}

func (e *AdmissionSlotWaitError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("admission slot wait for class %s stopped after %s: %v", e.Class, e.Waited.Round(time.Second), e.Err)
	}
	return fmt.Sprintf("no admission slot for class %s within %s (slot locks: %s)", e.Class, e.Waited.Round(time.Second), e.Dir)
}

func (e *AdmissionSlotWaitError) Unwrap() error { return e.Err }

// admissionSlotRefusal reports a class that stayed full as an acceptance
// refusal of its own fault class. A cancelled wait or a slot I/O failure is
// returned as is, so the caller's ownership fence decides the former.
func admissionSlotRefusal(taskID string, err error) error {
	var wait *AdmissionSlotWaitError
	if errors.As(err, &wait) && wait.Err == nil {
		return &AcceptanceEvidenceError{TaskID: taskID, Field: "acceptance.admission_slot", Reason: wait.Error(), Class: AcceptanceFaultAdmissionSlot}
	}
	return err
}

// admissionTier ranks a class's waiters. Its value leads the ticket name, so
// name order serves canonical waiters, then deferred probes, then local runs
// (D-218).
type admissionTier int

const (
	admissionTierCanonical admissionTier = iota // submit, update-review-commit, the heal
	admissionTierDeferred                       // deferred-validation probes
	admissionTierLocal                          // admit
)

// usableSlots is how many of a class's maxConcurrent slots a waiter of tier
// may take: a local run never takes the last slot of a class that has more
// than one, so canonical and deferred work always keeps a slot (D-218).
func (tier admissionTier) usableSlots(maxConcurrent int) int {
	if tier == admissionTierLocal && maxConcurrent > 1 {
		return maxConcurrent - 1
	}
	return maxConcurrent
}

// admissionSlots is the set of slots one execution holds, at most one per
// class, and the single deadline every acquisition for it shares; a zero
// deadline waits until a slot frees or the context ends. The zero tier is
// canonical.
type admissionSlots struct {
	deadline time.Time
	tier     admissionTier
	classes  []string
	held     []*filelock.Held
	// progress, when a submit owns the slots, hears their queue wait and the
	// batch they admit.
	progress *submitProgress
}

// newAdmissionSlots fixes the shared wait deadline for a batch budgeted
// budgetSeconds.
func newAdmissionSlots(budgetSeconds int) *admissionSlots {
	return &admissionSlots{deadline: time.Now().Add(admissionSlotWait(budgetSeconds))}
}

// newSubmitAdmissionSlots fixes a submit's shared wait deadline.
func newSubmitAdmissionSlots(budgetSeconds int) *admissionSlots {
	return &admissionSlots{deadline: time.Now().Add(submitAdmissionSlotWait(budgetSeconds))}
}

// newQueuedAdmissionSlots waits with no deadline: for an execution no
// in-flight grace bounds, the deferred runner's probes (D-223). They queue
// behind every canonical waiter (D-218).
func newQueuedAdmissionSlots() *admissionSlots {
	return &admissionSlots{tier: admissionTierDeferred}
}

// newLocalAdmissionSlots waits up to maxWait for a local run (admit), behind
// every canonical and deferred waiter.
func newLocalAdmissionSlots(maxWait time.Duration) *admissionSlots {
	return &admissionSlots{deadline: time.Now().Add(maxWait), tier: admissionTierLocal}
}

// acquire takes a slot of every class in limits not already held, in class
// order, waiting in each class's FIFO queue until the shared deadline. On
// failure every slot held, including earlier ones, is released.
func (s *admissionSlots) acquire(ctx context.Context, projectRoot string, limits []models.AdmissionLimit) error {
	statePath := paths.New(projectRoot).StatePath()
	for _, limit := range limits {
		if slices.Contains(s.classes, limit.Class) {
			continue
		}
		queued := func(ahead int) {
			s.progress.set(fmt.Sprintf("queued for admission class %s, %d waiter(s) ahead; the batch budget has not started", limit.Class, ahead), s.deadline)
		}
		current := func() (int, bool) { return currentAdmissionMax(statePath, limit.Class) }
		held, err := acquireAdmissionSlot(ctx, filepath.Join(paths.New(projectRoot).AdmissionSlotsDir(), limit.Class), limit, s.deadline, s.tier, queued, current)
		if err != nil {
			s.release()
			return err
		}
		s.classes = append(s.classes, limit.Class)
		s.held = append(s.held, held)
	}
	return nil
}

// files are the locked descriptors a command inherits, so the slots stay held
// while it runs even if their owner is killed. Nil where locks cannot pass
// to a child.
func (s *admissionSlots) files() []*os.File {
	var files []*os.File
	for _, held := range s.held {
		if file := held.File(); file != nil {
			files = append(files, file)
		}
	}
	return files
}

// release unlocks every slot, also for any command still holding an
// inherited descriptor.
func (s *admissionSlots) release() {
	for _, held := range s.held {
		if err := held.Release(); err != nil {
			log.Printf("WARNING: admission slot not released: %v", err)
		}
	}
	s.classes, s.held = nil, nil
}

// abandon closes this process's descriptors without unlocking, leaving each
// slot with the commands that inherited it.
func (s *admissionSlots) abandon() {
	for _, file := range s.files() {
		_ = file.Close()
	}
	s.classes, s.held = nil, nil
}

// currentAdmissionMax reads class's stored max_concurrent without the state
// lock. It reports none when the state cannot be read or holds no valid entry
// for class, so the waiter keeps the limit it knew: a removed class's waiters
// wait for the slots they knew (ADR-0209).
func currentAdmissionMax(statePath, class string) (int, bool) {
	limits, err := db.For(statePath).ReadAdmissionLimits()
	if err != nil {
		log.Printf("WARNING: admission limit for class %s not re-read, keeping the last known one: %v", class, err)
		return 0, false
	}
	for _, limit := range limits {
		if limit.Class == class && models.ValidateAdmissionLimit(limit) == nil {
			return limit.MaxConcurrent, true
		}
	}
	return 0, false
}

// acquireAdmissionSlot queues a ticket in dir and, once the ticket is the
// earliest live one, takes the first free of the slots its tier may use.
// Tickets and slots are flocks, so a dead waiter or holder releases its own.
// Ticket names lead with the tier digit, so name order serves canonical (0),
// deferred (1), then local (2) waiters. An older binary's names (zero-padded
// time, no tier) sort between canonical and deferred. queued, when set, hears
// how many tickets are ahead before each wait. current, when set, re-reads the class's
// max_concurrent: once the head waiter has found every slot held, on its next
// poll and then at most every admissionLimitRefreshInterval, keeping the last
// known value when it reports none (D-180). A zero deadline never expires.
func acquireAdmissionSlot(ctx context.Context, dir string, limit models.AdmissionLimit, deadline time.Time, tier admissionTier, queued func(ahead int), current func() (int, bool)) (*filelock.Held, error) {
	started := time.Now()
	queue := filepath.Join(dir, admissionQueueDirName)
	if err := os.MkdirAll(queue, admissionDirMode); err != nil {
		return nil, err
	}
	// Creation and pruning share this lock, so no ticket is ever seen
	// between its creation and its lock.
	queueLock := filelock.New(queue)
	nonce := make([]byte, admissionTicketNonceBytes)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	ticketPath := filepath.Join(queue, fmt.Sprintf("%d-%020d-%s", tier, time.Now().UnixNano(), hex.EncodeToString(nonce)))
	var ticket *filelock.Held
	err := queueLock.WithLockOperation(admissionQueueLockOperation, func() error {
		held, acquired, err := filelock.New(ticketPath).TryHold(admissionWaitLockOperation)
		if err == nil && !acquired {
			err = fmt.Errorf("admission slot ticket %s already held", ticketPath)
		}
		ticket = held
		return err
	})
	if err != nil {
		return nil, err
	}
	defer removeAdmissionTicket(ticketPath, ticket)

	logged := false
	full := false // the head found every slot held
	var nextRefresh time.Time
	for {
		ahead, err := admissionQueueAhead(queueLock, queue, ticketPath)
		if err != nil {
			return nil, err
		}
		if ahead == 0 {
			if full && current != nil && !time.Now().Before(nextRefresh) {
				nextRefresh = time.Now().Add(admissionLimitRefreshInterval)
				if maxConcurrent, ok := current(); ok && maxConcurrent != limit.MaxConcurrent {
					log.Printf("admission limit for class %s is now %d at once", limit.Class, maxConcurrent)
					limit.MaxConcurrent = maxConcurrent
				}
			}
			full = true
			for i := range tier.usableSlots(limit.MaxConcurrent) {
				held, acquired, err := filelock.New(filepath.Join(dir, fmt.Sprintf("slot-%d", i))).TryHold(admissionSlotLockOperation)
				if err != nil {
					return nil, err
				}
				if acquired {
					if logged {
						log.Printf("admission slot for class %s acquired after %s", limit.Class, time.Since(started).Round(time.Second))
					}
					return held, nil
				}
			}
		}
		now := time.Now()
		poll := admissionSlotPollInterval
		if !deadline.IsZero() {
			if !now.Before(deadline) {
				return nil, &AdmissionSlotWaitError{Class: limit.Class, Waited: now.Sub(started), Dir: dir}
			}
			poll = min(poll, deadline.Sub(now))
		}
		if !logged {
			until := "until one frees"
			if !deadline.IsZero() {
				until = "at most until " + deadline.UTC().Format(time.RFC3339)
			}
			usable := ""
			if n := tier.usableSlots(limit.MaxConcurrent); n < limit.MaxConcurrent {
				usable = fmt.Sprintf(", local runs use at most %d", n)
			}
			log.Printf("waiting for an admission slot of class %s (max %d at once%s), %s", limit.Class, limit.MaxConcurrent, usable, until)
			logged = true
		}
		if queued != nil {
			queued(ahead)
		}
		select {
		case <-ctx.Done():
			return nil, &AdmissionSlotWaitError{Class: limit.Class, Waited: time.Since(started), Dir: dir, Err: context.Cause(ctx)}
		case <-time.After(poll):
		}
	}
}

// admissionQueueAhead counts the tickets ahead of ticketPath; zero means it is
// the earliest live ticket. Earlier tickets whose waiter is gone (their lock
// is free) are removed up to the first live one; later ones are counted
// unprobed.
func admissionQueueAhead(queueLock *filelock.FileLock, queue, ticketPath string) (int, error) {
	ahead := 0
	err := queueLock.WithLockOperation(admissionQueueLockOperation, func() error {
		entries, err := os.ReadDir(queue)
		if err != nil {
			return err
		}
		for _, entry := range entries { // ReadDir sorts by name: arrival order.
			name, ok := strings.CutSuffix(entry.Name(), admissionLockFileSuffix)
			if !ok {
				continue
			}
			path := filepath.Join(queue, name)
			if path == ticketPath {
				return nil
			}
			if ahead == 0 {
				held, acquired, err := filelock.New(path).TryHold(admissionPruneLockOperation)
				if err != nil {
					return err
				}
				if acquired {
					removeAdmissionTicket(path, held)
					continue
				}
			}
			ahead++ // An earlier waiter is live, or queued behind one.
		}
		return errors.New("admission slot ticket vanished from its queue")
	})
	return ahead, err
}

func removeAdmissionTicket(path string, held *filelock.Held) {
	// Release before removing: Windows refuses to unlink an open file, and
	// releasing first also avoids a re-creator flocking a different inode.
	_ = held.Release()
	for _, file := range []string{path + admissionLockFileSuffix, path + admissionOwnerFileSuffix} {
		if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
			log.Printf("WARNING: admission slot ticket not removed: %v", err)
		}
	}
}
