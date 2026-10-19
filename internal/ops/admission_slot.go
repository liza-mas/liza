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

	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/referencecontract"
)

// admissionSlotPollInterval is how often a waiter re-reads its class queue;
// tests shorten it.
var admissionSlotPollInterval = time.Second

// admissionSlotCap bounds a slot wait plus the execution budget behind it;
// tests shorten it.
var admissionSlotCap = referencecontract.AcceptanceMaxTimeoutSeconds * time.Second

// admissionSlotWait is the longest a command budgeted budgetSeconds may wait
// for its slots: a wait plus its execution never exceeds the engine's
// acceptance cap, so the in-flight submit grace still bounds both (ADR-0209).
func admissionSlotWait(budgetSeconds int) time.Duration {
	return max(0, admissionSlotCap-acceptanceBatchTimeout(budgetSeconds))
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

// admissionSlots is the set of slots one execution holds, at most one per
// class, and the single deadline every acquisition for it shares; a zero
// deadline waits until a slot frees or the context ends. A local execution
// (admit) queues behind every canonical waiter of a class.
type admissionSlots struct {
	deadline time.Time
	local    bool
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

// newQueuedAdmissionSlots waits with no deadline: for an execution no
// in-flight grace bounds, the deferred runner's probes (D-223).
func newQueuedAdmissionSlots() *admissionSlots {
	return &admissionSlots{}
}

// acquire takes a slot of every class in limits not already held, in class
// order, waiting in each class's FIFO queue until the shared deadline. On
// failure every slot held, including earlier ones, is released.
func (s *admissionSlots) acquire(ctx context.Context, projectRoot string, limits []models.AdmissionLimit) error {
	for _, limit := range limits {
		if slices.Contains(s.classes, limit.Class) {
			continue
		}
		queued := func(ahead int) {
			s.progress.set(fmt.Sprintf("queued for admission class %s, %d waiter(s) ahead; the batch budget has not started", limit.Class, ahead), s.deadline)
		}
		held, err := acquireAdmissionSlot(ctx, filepath.Join(paths.New(projectRoot).AdmissionSlotsDir(), limit.Class), limit, s.deadline, s.local, queued)
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

// acquireAdmissionSlot queues a ticket in dir and, once the ticket is the
// earliest live one, takes the first free of limit.MaxConcurrent slots.
// Tickets and slots are flocks, so a dead waiter or holder releases its own.
// Ticket names lead with a priority digit, canonical 0 and local 1, so name
// order serves canonical waiters first. An older binary's names (zero-padded
// time, no priority) sort between the two. queued, when set, hears how many
// tickets are ahead before each wait. A zero deadline never expires.
func acquireAdmissionSlot(ctx context.Context, dir string, limit models.AdmissionLimit, deadline time.Time, local bool, queued func(ahead int)) (*filelock.Held, error) {
	started := time.Now()
	queue := filepath.Join(dir, "queue")
	if err := os.MkdirAll(queue, 0o755); err != nil {
		return nil, err
	}
	// Creation and pruning share this lock, so no ticket is ever seen
	// between its creation and its lock.
	queueLock := filelock.New(queue)
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	priority := 0
	if local {
		priority = 1
	}
	ticketPath := filepath.Join(queue, fmt.Sprintf("%d-%020d-%s", priority, time.Now().UnixNano(), hex.EncodeToString(nonce)))
	var ticket *filelock.Held
	err := queueLock.WithLockOperation("admission-slot-queue", func() error {
		held, acquired, err := filelock.New(ticketPath).TryHold("admission-slot-wait")
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
	for {
		ahead, err := admissionQueueAhead(queueLock, queue, ticketPath)
		if err != nil {
			return nil, err
		}
		if ahead == 0 {
			for i := range limit.MaxConcurrent {
				held, acquired, err := filelock.New(filepath.Join(dir, fmt.Sprintf("slot-%d", i))).TryHold("admission-slot")
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
			log.Printf("waiting for an admission slot of class %s (max %d at once), %s", limit.Class, limit.MaxConcurrent, until)
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
	err := queueLock.WithLockOperation("admission-slot-queue", func() error {
		entries, err := os.ReadDir(queue)
		if err != nil {
			return err
		}
		for _, entry := range entries { // ReadDir sorts by name: arrival order.
			name, ok := strings.CutSuffix(entry.Name(), ".lock")
			if !ok {
				continue
			}
			path := filepath.Join(queue, name)
			if path == ticketPath {
				return nil
			}
			if ahead == 0 {
				held, acquired, err := filelock.New(path).TryHold("admission-slot-prune")
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
	for _, file := range []string{path + ".lock", path + ".lock.owner.json"} {
		if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
			log.Printf("WARNING: admission slot ticket not removed: %v", err)
		}
	}
}
