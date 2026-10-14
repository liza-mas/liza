package ops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

func shortAdmissionSlotPoll(t *testing.T) {
	t.Helper()
	previous := admissionSlotPollInterval
	admissionSlotPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { admissionSlotPollInterval = previous })
}

func testAdmissionLimit(class string, max int) models.AdmissionLimit {
	return models.AdmissionLimit{Class: class, MaxConcurrent: max, Match: []string{"heavy"}}
}

// holdAdmissionSlots takes limits' slots for the test, released at cleanup.
func holdAdmissionSlots(t *testing.T, root string, limits ...models.AdmissionLimit) *admissionSlots {
	t.Helper()
	slots := &admissionSlots{deadline: time.Now()}
	if err := slots.acquire(context.Background(), root, limits); err != nil {
		t.Fatalf("hold admission slots: %v", err)
	}
	t.Cleanup(slots.release)
	return slots
}

func slotFree(t *testing.T, root, class string, index int) bool {
	t.Helper()
	held, acquired, err := filelock.New(filepath.Join(paths.New(root).AdmissionSlotsDir(), class, fmt.Sprintf("slot-%d", index))).TryHold("test-probe")
	if err != nil {
		t.Fatal(err)
	}
	if acquired {
		_ = held.Release()
	}
	return acquired
}

func queuedTickets(t *testing.T, root, class string) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(paths.New(root).AdmissionSlotsDir(), class, "queue"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".lock" {
			count++
		}
	}
	return count
}

func waitForTickets(t *testing.T, root, class string, want int) {
	t.Helper()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(5 * time.Second)
	for queuedTickets(t, root, class) != want {
		select {
		case <-ticker.C:
		case <-deadline:
			t.Fatalf("class %s queue never reached %d tickets (has %d)", class, want, queuedTickets(t, root, class))
		}
	}
}

func TestAdmissionSlotWaitSharesTheAcceptanceCap(t *testing.T) {
	if got := admissionSlotWait(3600); got != admissionSlotCap-time.Hour {
		t.Fatalf("wait for a 3600 s budget = %s", got)
	}
	if got := admissionSlotWait(0); got != admissionSlotCap-10*time.Minute {
		t.Fatalf("wait for the default budget = %s", got)
	}
	if got := admissionSlotWait(int(admissionSlotCap / time.Second)); got != 0 {
		t.Fatalf("a budget at the cap must get a single try, wait = %s", got)
	}
}

func TestAdmissionSlotsNeverExceedMaxConcurrent(t *testing.T) {
	shortAdmissionSlotPoll(t)
	root := t.TempDir()
	const waiters = 5
	for _, max := range []int{1, 2} {
		limit := testAdmissionLimit(fmt.Sprintf("cap-%d", max), max)
		var holding atomic.Int32
		acquired := make(chan struct{}, waiters)
		proceed := make(chan struct{})
		var wg sync.WaitGroup
		for range waiters {
			wg.Add(1)
			go func() {
				defer wg.Done()
				slots := &admissionSlots{deadline: time.Now().Add(10 * time.Second)}
				err := slots.acquire(context.Background(), root, []models.AdmissionLimit{limit})
				if err != nil {
					t.Error(err)
				} else if holding.Add(1) > int32(max) {
					t.Errorf("max_concurrent %d: more holders at once", max)
				}
				acquired <- struct{}{}
				if err == nil {
					<-proceed
					holding.Add(-1)
					slots.release()
				}
			}()
		}
		// The first max waiters hold; every other one queues behind them.
		for range max {
			<-acquired
		}
		waitForTickets(t, root, limit.Class, waiters-max)
		if got := holding.Load(); got != int32(max) {
			t.Fatalf("max_concurrent %d: %d holders with %d queued", max, got, waiters-max)
		}
		for range waiters - max {
			proceed <- struct{}{}
			<-acquired
		}
		close(proceed)
		wg.Wait()
	}
}

func TestAdmissionSlotsServeWaitersInArrivalOrder(t *testing.T) {
	shortAdmissionSlotPoll(t)
	root := t.TempDir()
	limit := testAdmissionLimit("fifo", 1)
	first := holdAdmissionSlots(t, root, limit)

	order := make(chan string, 2)
	release := map[string]chan struct{}{"early": make(chan struct{}), "late": make(chan struct{})}
	waiter := func(name string) {
		slots := &admissionSlots{deadline: time.Now().Add(10 * time.Second)}
		if err := slots.acquire(context.Background(), root, []models.AdmissionLimit{limit}); err != nil {
			t.Error(err)
			return
		}
		order <- name
		<-release[name]
		slots.release()
	}
	go waiter("early")
	waitForTickets(t, root, "fifo", 1)
	go waiter("late")
	waitForTickets(t, root, "fifo", 2)

	first.release()
	if got := <-order; got != "early" {
		t.Fatalf("freed slot went to %s ahead of the earlier waiter", got)
	}
	select {
	case got := <-order:
		t.Fatalf("%s acquired while the only slot was held", got)
	case <-time.After(100 * time.Millisecond):
	}
	close(release["early"])
	if got := <-order; got != "late" {
		t.Fatalf("second slot holder = %s", got)
	}
	close(release["late"])
}

func TestAdmissionSlotQueueSkipsADeadWaitersTicket(t *testing.T) {
	shortAdmissionSlotPoll(t)
	root := t.TempDir()
	queue := filepath.Join(paths.New(root).AdmissionSlotsDir(), "dead", "queue")
	if err := os.MkdirAll(queue, 0o755); err != nil {
		t.Fatal(err)
	}
	// An earlier ticket whose waiter died: its file remains, its lock is gone.
	held, acquired, err := filelock.New(filepath.Join(queue, "00000000000000000001-dead")).TryHold("test")
	if err != nil || !acquired {
		t.Fatalf("seed ticket: %v %v", acquired, err)
	}
	_ = held.Release()

	slots := &admissionSlots{deadline: time.Now().Add(2 * time.Second)}
	if err := slots.acquire(context.Background(), root, []models.AdmissionLimit{testAdmissionLimit("dead", 1)}); err != nil {
		t.Fatalf("a dead waiter's ticket stalled the queue: %v", err)
	}
	slots.release()
	if got := queuedTickets(t, root, "dead"); got != 0 {
		t.Fatalf("queue keeps %d tickets after the dead one was pruned", got)
	}
}

func TestAdmissionSlotsShareOneDeadlineAcrossClasses(t *testing.T) {
	shortAdmissionSlotPoll(t)
	root := t.TempDir()
	a, b := testAdmissionLimit("a", 1), testAdmissionLimit("b", 1)
	heldA := holdAdmissionSlots(t, root, a)
	holdAdmissionSlots(t, root, b)
	// Wall-clock behavior under test: class a frees after 700ms of a 1s
	// allowance; a restarted allowance for class b would end near 1.7s.
	go func() {
		time.Sleep(700 * time.Millisecond)
		heldA.release()
	}()

	started := time.Now()
	slots := &admissionSlots{deadline: started.Add(time.Second)}
	err := slots.acquire(context.Background(), root, []models.AdmissionLimit{a, b})
	elapsed := time.Since(started)
	var wait *AdmissionSlotWaitError
	if !errors.As(err, &wait) || wait.Class != "b" || wait.Err != nil {
		t.Fatalf("acquire = %v, want class b to stay full", err)
	}
	if elapsed < 700*time.Millisecond || elapsed > 1350*time.Millisecond {
		t.Fatalf("waited %s for a 1s allowance shared by both classes", elapsed)
	}
	if !slotFree(t, root, "a", 0) {
		t.Fatal("class a's slot stayed held after the acquisition failed")
	}
}

func TestAdmissionSlotWaitStopsOnCancelAndTriesOnceAtZeroWait(t *testing.T) {
	shortAdmissionSlotPoll(t)
	root := t.TempDir()
	limit := testAdmissionLimit("busy", 1)
	holdAdmissionSlots(t, root, limit)

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	err := (&admissionSlots{deadline: time.Now().Add(time.Minute)}).acquire(ctx, root, []models.AdmissionLimit{limit})
	var wait *AdmissionSlotWaitError
	if !errors.As(err, &wait) || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled wait = %v", err)
	}
	if refusal := admissionSlotRefusal("t", err); refusal != err {
		t.Fatalf("a cancelled wait must not become an admission refusal: %v", refusal)
	}

	started := time.Now()
	err = (&admissionSlots{deadline: started}).acquire(context.Background(), root, []models.AdmissionLimit{limit})
	if !errors.As(err, &wait) || wait.Err != nil || time.Since(started) > 500*time.Millisecond {
		t.Fatalf("zero wait = %v after %s, want one immediate try", err, time.Since(started))
	}
	var evidence *AcceptanceEvidenceError
	if !errors.As(admissionSlotRefusal("t", err), &evidence) || evidence.Class != AcceptanceFaultAdmissionSlot || evidence.Field != "acceptance.admission_slot" {
		t.Fatalf("full class refusal = %v", admissionSlotRefusal("t", err))
	}
	if got := queuedTickets(t, root, "busy"); got != 0 {
		t.Fatalf("refused waiters left %d tickets", got)
	}
}

func TestOrphanedBaseHealRetriesAFullAdmissionClass(t *testing.T) {
	full := admissionSlotRefusal("t", &AdmissionSlotWaitError{Class: "suite"})
	if deterministicHealRefusal(full) {
		t.Fatal("a full admission class would route the heal to integration recovery")
	}
	if !deterministicHealRefusal(acceptanceError("t", "acceptance.execution", "command failed")) {
		t.Fatal("content refusals must stay deterministic")
	}
}

func TestAdmissionSlotFollowsTheCommandThatInheritedIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("lock descriptor inheritance is Unix-only; filelock.Held.File is nil on Windows")
	}
	requirePosixShell(t)
	root := t.TempDir()
	limit := testAdmissionLimit("inherit", 1)

	// Owner killed: the kernel closes its descriptor without unlocking; the
	// running command keeps the slot until it exits.
	slots := holdAdmissionSlots(t, root, limit)
	cmd := exec.Command("sleep", "1")
	cmd.ExtraFiles = slots.files()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	slots.abandon()
	if slotFree(t, root, "inherit", 0) {
		t.Fatal("slot was free while the inheriting command still ran")
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if !slotFree(t, root, "inherit", 0) {
		t.Fatal("slot stayed held after the inheriting command exited")
	}

	// Normal release unlocks for every holder, so a lingering descendant
	// cannot keep the slot.
	slots = holdAdmissionSlots(t, root, limit)
	lingering := exec.Command("sleep", "5")
	lingering.ExtraFiles = slots.files()
	if err := lingering.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lingering.Process.Kill(); _ = lingering.Wait() })
	slots.release()
	if !slotFree(t, root, "inherit", 0) {
		t.Fatal("released slot stayed held by a lingering inheritor")
	}
}
