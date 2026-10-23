//go:build !windows

package ops

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

var (
	heavyAdmissionLimit = models.AdmissionLimit{Class: "heavy", MaxConcurrent: 1, Match: []string{"heavy"}}
	suiteClassLimit     = models.AdmissionLimit{Class: "suite", MaxConcurrent: 1, Match: []string{"suite"}}
)

// admitShell runs script under sh with words as its $0.. arguments, so the
// command text carries them for class matching.
func admitShell(script string, words ...string) []string {
	return append([]string{"sh", "-c", script}, words...)
}

// pollUntil fails the test unless done reports true within 5 s.
func pollUntil(t *testing.T, what string, done func() bool) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(5 * time.Second)
	for !done() {
		select {
		case <-ticker.C:
		case <-deadline:
			t.Fatalf("timed out waiting until %s", what)
		}
	}
}

// waitForLine returns the first complete line a command writes to path.
func waitForLine(t *testing.T, path string) string {
	t.Helper()
	var line string
	pollUntil(t, path+" is written", func() bool {
		data, err := os.ReadFile(path)
		line = strings.TrimSpace(string(data))
		return err == nil && strings.HasSuffix(string(data), "\n")
	})
	return line
}

func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

type admitOutcome struct {
	code int
	err  error
}

func runAdmittedAsync(root string, input AdmitInput) <-chan admitOutcome {
	done := make(chan admitOutcome, 1)
	go func() {
		code, err := RunAdmitted(root, input)
		done <- admitOutcome{code, err}
	}()
	return done
}

func TestAdmitRunsACommandMatchingNoClassWithoutASlot(t *testing.T) {
	root := setupAdmitProject(t, heavyAdmissionLimit)

	code, err := RunAdmitted(root, AdmitInput{Argv: admitShell("exit 3", "light"), MaxWait: time.Second})

	if err != nil || code != 3 {
		t.Fatalf("code=%d err=%v, want the command's exit 3", code, err)
	}
	if _, err := os.Stat(filepath.Join(paths.New(root).AdmissionSlotsDir(), "heavy")); !os.IsNotExist(err) {
		t.Fatalf("a non-matching command touched the class's slots: %v", err)
	}
}

func TestAdmitHoldsEveryMatchingClassWhileTheCommandRuns(t *testing.T) {
	root := setupAdmitProject(t, heavyAdmissionLimit, suiteClassLimit)
	dir := t.TempDir()
	started, proceed := filepath.Join(dir, "started"), filepath.Join(dir, "proceed")

	// GIVEN a command whose text matches both classes
	done := runAdmittedAsync(root, AdmitInput{
		Argv:    admitShell(`echo > "$1"; while [ ! -e "$2" ]; do sleep 0.01; done; exit 3`, "heavy-suite", started, proceed),
		MaxWait: time.Second,
	})

	// THEN both slots are held while it runs
	waitForLine(t, started)
	for _, class := range []string{"heavy", "suite"} {
		if slotFree(t, root, class, 0) {
			t.Fatalf("class %s slot free while the matching command ran", class)
		}
	}
	// AND both are released after it exits with its own code
	if err := os.WriteFile(proceed, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	outcome := <-done
	if outcome.err != nil || outcome.code != 3 {
		t.Fatalf("code=%d err=%v, want the command's exit 3", outcome.code, outcome.err)
	}
	for _, class := range []string{"heavy", "suite"} {
		if !slotFree(t, root, class, 0) {
			t.Fatalf("class %s slot still held after the command exited", class)
		}
	}
}

func TestAdmitRefusesAFullClassWithoutRunningAndHoldsNothing(t *testing.T) {
	root := setupAdmitProject(t, heavyAdmissionLimit, suiteClassLimit)
	holdAdmissionSlots(t, root, suiteClassLimit)
	marker := filepath.Join(t.TempDir(), "ran")

	// WHEN the later class stays full past the wait
	code, err := RunAdmitted(root, AdmitInput{Argv: admitShell(`echo > "$1"`, "heavy-suite", marker), MaxWait: 0})

	// THEN nothing ran, the earlier class was released and no ticket remains
	var wait *AdmissionSlotWaitError
	if !errors.As(err, &wait) || wait.Err != nil || wait.Class != "suite" || code != -1 {
		t.Fatalf("code=%d err=%v, want a full-class refusal for suite", code, err)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatal("the command ran although its class stayed full")
	}
	if !slotFree(t, root, "heavy", 0) {
		t.Fatal("the earlier class's slot stayed held after the refusal")
	}
	for _, class := range []string{"heavy", "suite"} {
		if n := queuedTickets(t, root, class); n != 0 {
			t.Fatalf("class %s kept %d tickets", class, n)
		}
	}
}

func TestAdmitCancelledWhileQueuedLaunchesNothing(t *testing.T) {
	root := setupAdmitProject(t, heavyAdmissionLimit, suiteClassLimit)
	holdAdmissionSlots(t, root, suiteClassLimit)
	marker := filepath.Join(t.TempDir(), "ran")
	signals := make(chan os.Signal, 1)

	// GIVEN a command queued for its second class
	done := runAdmittedAsync(root, AdmitInput{Argv: admitShell(`echo > "$1"`, "heavy-suite", marker), MaxWait: 10 * time.Second, Signals: signals})
	waitForTickets(t, root, "suite", 1)

	// WHEN interrupted
	signals <- syscall.SIGTERM
	outcome := <-done

	// THEN the wait stops, nothing ran, and nothing is held or queued
	var wait *AdmissionSlotWaitError
	if !errors.As(outcome.err, &wait) || wait.Err == nil {
		t.Fatalf("err=%v, want a cancelled wait", outcome.err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("the command ran after an interrupted wait")
	}
	if !slotFree(t, root, "heavy", 0) {
		t.Fatal("the earlier class's slot stayed held after the interrupt")
	}
	if n := queuedTickets(t, root, "suite"); n != 0 {
		t.Fatalf("interrupted waiter left %d tickets", n)
	}
}

func TestAdmitInterruptedAsItsSlotIsGrantedLaunchesNothing(t *testing.T) {
	root := setupAdmitProject(t, heavyAdmissionLimit)
	marker := filepath.Join(t.TempDir(), "ran")
	signals := make(chan os.Signal) // unbuffered: a completed send means the wait consumed it
	// GIVEN a free slot, and the class queue lock held so the wait cannot
	// finish before the interrupt is consumed
	queue := filepath.Join(paths.New(root).AdmissionSlotsDir(), "heavy", "queue")
	if err := os.MkdirAll(queue, 0o755); err != nil {
		t.Fatal(err)
	}
	queueLock, acquired, err := filelock.New(queue).TryHold("test-queue")
	if err != nil || !acquired {
		t.Fatalf("hold queue lock: acquired=%v err=%v", acquired, err)
	}
	done := runAdmittedAsync(root, AdmitInput{Argv: admitShell(`echo > "$1"`, "heavy", marker), MaxWait: 10 * time.Second, Signals: signals})

	// WHEN the interrupt is consumed and only then the slot becomes available
	signals <- syscall.SIGTERM
	if err := queueLock.Release(); err != nil {
		t.Fatal(err)
	}
	outcome := <-done

	// THEN nothing launches, and nothing is held or queued
	var wait *AdmissionSlotWaitError
	if !errors.As(outcome.err, &wait) || wait.Err == nil {
		t.Fatalf("code=%d err=%v, want a cancelled wait", outcome.code, outcome.err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("the command launched after its interrupt was consumed")
	}
	if !slotFree(t, root, "heavy", 0) {
		t.Fatal("a slot granted to an interrupted wait stayed held")
	}
	if n := queuedTickets(t, root, "heavy"); n != 0 {
		t.Fatalf("interrupted waiter left %d tickets", n)
	}
}

func TestAdmitKeepsTheSlotUntilTheShellsDescendantStops(t *testing.T) {
	root := setupAdmitProject(t, heavyAdmissionLimit)
	pidFile := filepath.Join(t.TempDir(), "pid")
	signals := make(chan os.Signal, 1)

	// GIVEN a shell whose background descendant ignores SIGTERM
	done := runAdmittedAsync(root, AdmitInput{
		Argv:    admitShell(`(trap "" TERM; exec sleep 30) & echo $! > "$1"; wait`, "heavy", pidFile),
		MaxWait: time.Second, Signals: signals,
	})
	pid, err := strconv.Atoi(waitForLine(t, pidFile))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	if slotFree(t, root, "heavy", 0) {
		t.Fatal("slot free while the command ran")
	}

	// WHEN admit is interrupted: the shell dies, the descendant would survive
	signals <- syscall.SIGTERM
	outcome := <-done

	// THEN the descendant is stopped before the slot frees
	if outcome.err != nil {
		t.Fatalf("err=%v, want the group reaped and the slot released", outcome.err)
	}
	if processAlive(pid) {
		t.Fatalf("descendant %d still alive after admit released its slot", pid)
	}
	if !slotFree(t, root, "heavy", 0) {
		t.Fatal("slot still held after the process group was reaped")
	}
}

func TestAdmitLeavesTheSlotWithASurvivor(t *testing.T) {
	root := setupAdmitProject(t, heavyAdmissionLimit)
	previous := admitReap
	admitReap = func(int, time.Duration) bool { return false }
	t.Cleanup(func() { admitReap = previous })
	dir := t.TempDir()
	pidFile, proceed := filepath.Join(dir, "pid"), filepath.Join(dir, "proceed")

	// GIVEN a command that exits leaving a descendant that inherited the slot
	// and runs until the test lets it go, and a reap that cannot confirm the
	// group is gone
	code, err := RunAdmitted(root, AdmitInput{
		Argv:    admitShell(`while [ ! -e "$2" ]; do sleep 0.01; done & echo $! > "$1"`, "heavy", pidFile, proceed),
		MaxWait: time.Second,
	})

	// THEN admit fails naming the slot directory, and the slot stays held
	// until the survivor exits
	var survivor *AdmitSurvivorError
	if !errors.As(err, &survivor) || code != 1 || !strings.Contains(err.Error(), filepath.Join(paths.New(root).AdmissionSlotsDir(), "heavy")) {
		t.Fatalf("code=%d err=%v, want a survivor error naming the class's slot directory", code, err)
	}
	pid, convErr := strconv.Atoi(waitForLine(t, pidFile))
	if convErr != nil {
		t.Fatal(convErr)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	if !processAlive(pid) {
		t.Fatalf("survivor %d exited before the test released it", pid)
	}
	if slotFree(t, root, "heavy", 0) {
		t.Fatal("slot freed while the survivor still held it")
	}
	if err := os.WriteFile(proceed, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	pollUntil(t, "the slot frees after the survivor exits", func() bool { return slotFree(t, root, "heavy", 0) })
}

func TestAdmitRefusesAnInvalidRequest(t *testing.T) {
	root := setupAdmitProject(t, heavyAdmissionLimit)
	for name, input := range map[string]AdmitInput{
		"no command":    {},
		"negative wait": {Argv: []string{"true"}, MaxWait: -time.Second},
		"wait over cap": {Argv: []string{"true"}, MaxWait: AdmitMaxWait + time.Second},
	} {
		var precondition *PreconditionError
		if _, err := RunAdmitted(root, input); !errors.As(err, &precondition) {
			t.Errorf("%s: err=%v, want a precondition refusal", name, err)
		}
	}
}
