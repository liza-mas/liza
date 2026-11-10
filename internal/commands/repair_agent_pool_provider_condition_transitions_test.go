package commands

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/alerts"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/providersignal"
)

// Transitions of the durable provider condition (issue #115), driven at the
// reconciliation boundary so each watcher's observation is scripted: the
// candidates stand for its pre-lock snapshot, block for what it reads under
// the record lock.

var codexCoderWork = ProviderBlockedRoleWork{Role: "coder", CLI: "codex", TaskIDs: []string{"task-a"}}

func blockedAs(blocked bool) ProviderBlock {
	return func(string) (time.Time, bool) { return time.Time{}, blocked }
}

// writeCodexSignal writes a codex quota signal detected at detected, with one
// fixed announced reset, as a raise does.
func writeCodexSignal(t *testing.T, root string, detected time.Time) {
	t.Helper()
	writeCodexSignalResetting(t, root, detected, time.Now().UTC().Add(48*time.Hour).Truncate(time.Hour))
}

func writeCodexSignalResetting(t *testing.T, root string, detected, resetsAt time.Time) {
	t.Helper()
	content := fmt.Sprintf("provider: codex\ndetected: %s\nmessage: You've hit your usage limit\nresets_at: %s\nexpires: %s\n",
		detected.UTC().Format(time.RFC3339), resetsAt.Format(time.RFC3339), resetsAt.Add(time.Minute).Format(time.RFC3339))
	if err := os.WriteFile(providersignal.QuotaSignalPath(root, "codex"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// conditionTick is the provider-condition part of one watcher tick: reconcile,
// raise, and log as the watch loop does.
func conditionTick(root string, cache map[string]time.Time, candidates []ProviderBlockedRoleWork, block ProviderBlock) (providerConditions, error) {
	now := time.Now().UTC()
	conditions := reconcileProviderConditions(root, candidates, block, paths.New(root).AlertsLogPath(), now)
	raised := append(slices.Clone(conditions.lifted), autoRepairProviderBlockedAlerts(candidates, conditions, cache, now)...)
	for _, alert := range raised {
		var ledgerErr *alerts.LedgerUnavailableError
		if err := WriteAlert(paths.New(root).AlertsLogPath(), alert); err != nil && !errors.As(err, &ledgerErr) {
			return conditions, err
		}
	}
	return conditions, nil
}

func mustConditionTick(t *testing.T, root string, cache map[string]time.Time, candidates []ProviderBlockedRoleWork, block ProviderBlock) providerConditions {
	t.Helper()
	conditions, err := conditionTick(root, cache, candidates, block)
	if err != nil {
		t.Fatal(err)
	}
	return conditions
}

func codexCondition(t *testing.T, root string) (providerCondition, bool) {
	t.Helper()
	record, exists, err := readProviderCondition(providerConditionPath(root, "codex"))
	if err != nil {
		t.Fatal(err)
	}
	return record, exists
}

func wantLines(t *testing.T, root, category string, want int) []string {
	t.Helper()
	lines := alertLogLines(t, root, category)
	if len(lines) != want {
		t.Fatalf("%s lines = %d, want %d:\n%s", category, len(lines), want, strings.Join(lines, "\n"))
	}
	return lines
}

// GIVEN a block that was cleared and re-raised with the same announced reset
// WHEN a watcher observes the lift in between, and later a re-raise without
// an observed lift
// THEN the first re-raise opens a new condition and alerts again; the second
// is a new event of the open condition and alerts again with the event count.
func TestProviderCondition_SameResetRelatchAlertsAgain(t *testing.T) {
	h := newPoolRepairHarness(t)
	detected := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	work := []ProviderBlockedRoleWork{codexCoderWork}
	block := QuotaProviderBlock(h.root)

	writeCodexSignal(t, h.root, detected)
	mustConditionTick(t, h.root, h.cache, work, block)
	if err := providersignal.ClearQuotaSignal(h.root, "codex"); err != nil {
		t.Fatal(err)
	}
	mustConditionTick(t, h.root, h.cache, work, block)
	wantLines(t, h.root, providerBlockLiftedCategory, 1)

	writeCodexSignal(t, h.root, detected.Add(time.Minute))
	mustConditionTick(t, h.root, h.cache, work, block)
	wantLines(t, h.root, noUsableProviderCategory, 2)

	writeCodexSignal(t, h.root, detected.Add(2*time.Minute))
	mustConditionTick(t, h.root, h.cache, work, block)
	lines := wantLines(t, h.root, noUsableProviderCategory, 3)
	if !strings.Contains(lines[2], "(2 quota events observed)") {
		t.Fatalf("re-raise within a condition does not count its events: %s", lines[2])
	}
}

// GIVEN one quota event whose admission deadline changes or becomes unknown,
// as a recovery reservation or probe makes it (#114)
// WHEN restarted watchers observe it
// THEN no new NO USABLE PROVIDER line is written: the event is unchanged.
func TestProviderCondition_RestartDuringRecoveryKeepsTheEvent(t *testing.T) {
	h := newPoolRepairHarness(t)
	writeCodexSignal(t, h.root, time.Now().UTC().Add(-time.Hour))
	deadlines := []time.Time{time.Now().Add(time.Hour), {}, time.Now().Add(10 * time.Minute)}
	for _, until := range deadlines {
		block := func(string) (time.Time, bool) { return until, true }
		work := codexCoderWork
		work.Until = until
		mustConditionTick(t, h.root, make(map[string]time.Time), []ProviderBlockedRoleWork{work}, block)
	}
	wantLines(t, h.root, noUsableProviderCategory, 1)
}

// GIVEN a running watcher that announced a block
// WHEN the signal is removed between two of its ticks
// THEN one lift names the held work and the event count, and the record is gone.
func TestProviderCondition_RemovalBetweenTicksIsAnnouncedWithHeldWork(t *testing.T) {
	h := newPoolRepairHarness(t)
	state := coderBlockedDemand()
	blockProviderQuota(t, h.root, "codex")
	logOutcome(t, h.root, h.tick(state))
	if err := providersignal.ClearQuotaSignal(h.root, "codex"); err != nil {
		t.Fatal(err)
	}
	logOutcome(t, h.root, h.tick(state))

	lines := wantLines(t, h.root, providerBlockLiftedCategory, 1)
	for _, want := range []string{"coder@codex", "1 quota event(s) observed", "does not prove the provider has capacity", "latest: You've hit your usage limit"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("lift line missing %q: %s", want, lines[0])
		}
	}
	if _, exists := codexCondition(t, h.root); exists {
		t.Fatal("record kept after the lift")
	}
}

// GIVEN two roles held by one quota event and an unrelated critical alert
// WHEN watchers tick repeatedly during the block
// THEN each role is announced once and the unrelated alert is logged.
func TestProviderCondition_UnrelatedAlertsStayVisible(t *testing.T) {
	h := newPoolRepairHarness(t)
	writeCodexSignal(t, h.root, time.Now().UTC().Add(-time.Hour))
	orchestrator := ProviderBlockedRoleWork{Role: "orchestrator", CLI: "codex", Reason: "no live orchestrator"}
	work := []ProviderBlockedRoleWork{codexCoderWork, orchestrator}
	for range 3 {
		mustConditionTick(t, h.root, make(map[string]time.Time), work, blockedAs(true))
	}
	unrelated := Alert{Timestamp: time.Now().UTC(), Level: AlertLevelCritical, Category: alerts.CategoryAutoRepairFailed.String(), Message: "reviewer: spawn failed"}
	if err := WriteAlert(paths.New(h.root).AlertsLogPath(), unrelated); err != nil {
		t.Fatal(err)
	}
	wantLines(t, h.root, noUsableProviderCategory, 2)
	wantLines(t, h.root, alerts.CategoryAutoRepairFailed.String(), 1)
}

// GIVEN an open condition whose expired signal stays on disk
// WHEN starts are allowed, then blocked again on the same signal (#114's
// recovery reservation)
// THEN the lift is announced and the record removed while the signal stays,
// and the new block opens a new condition that alerts.
func TestProviderCondition_LiftWithExpiredSignalRetained(t *testing.T) {
	h := newPoolRepairHarness(t)
	writeCodexSignal(t, h.root, time.Now().UTC().Add(-time.Hour))
	work := []ProviderBlockedRoleWork{codexCoderWork}
	mustConditionTick(t, h.root, h.cache, work, blockedAs(true))
	mustConditionTick(t, h.root, h.cache, nil, blockedAs(false))

	wantLines(t, h.root, providerBlockLiftedCategory, 1)
	if _, exists := codexCondition(t, h.root); exists {
		t.Fatal("record kept after the lift")
	}
	if _, err := os.Stat(providersignal.QuotaSignalPath(h.root, "codex")); err != nil {
		t.Fatalf("lift touched the signal: %v", err)
	}

	mustConditionTick(t, h.root, h.cache, work, blockedAs(true))
	wantLines(t, h.root, noUsableProviderCategory, 2)
}

// GIVEN several watchers of one project
// WHEN they tick concurrently during a block, and again after its removal
// THEN one NO USABLE PROVIDER line, one record, and one lift line result.
func TestProviderCondition_ConcurrentWatchersAgree(t *testing.T) {
	h := newPoolRepairHarness(t)
	writeCodexSignal(t, h.root, time.Now().UTC().Add(-time.Hour))
	block := QuotaProviderBlock(h.root)
	work := []ProviderBlockedRoleWork{codexCoderWork}
	concurrently := func() {
		var wg sync.WaitGroup
		errs := make(chan error, 8)
		for range 8 {
			wg.Go(func() {
				if _, err := conditionTick(h.root, make(map[string]time.Time), work, block); err != nil {
					errs <- err
				}
			})
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatal(err)
		}
	}

	concurrently()
	wantLines(t, h.root, noUsableProviderCategory, 1)
	if record, exists := codexCondition(t, h.root); !exists || record.Events != 1 {
		t.Fatalf("record = %+v, %t; want one open condition with one event", record, exists)
	}

	if err := providersignal.ClearQuotaSignal(h.root, "codex"); err != nil {
		t.Fatal(err)
	}
	concurrently()
	wantLines(t, h.root, providerBlockLiftedCategory, 1)
}

// GIVEN a record lock held by another process
// WHEN a watcher ticks a blocked provider, then ticks again after release
// THEN the first tick raises nothing and warns, and the second announces the
// block: the transition is retried, not lost.
func TestProviderCondition_LockedRecordIsRetriedNextTick(t *testing.T) {
	h := newPoolRepairHarness(t)
	writeCodexSignal(t, h.root, time.Now().UTC().Add(-time.Hour))
	previous := providerConditionLockTimeout
	providerConditionLockTimeout = 50 * time.Millisecond
	t.Cleanup(func() { providerConditionLockTimeout = previous })

	held, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- filelock.New(providerConditionPath(h.root, "codex")).WithLockOperation("test-hold", func() error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held
	work := []ProviderBlockedRoleWork{codexCoderWork}
	conditions := mustConditionTick(t, h.root, h.cache, work, blockedAs(true))
	if !conditions.skipped["codex"] || len(conditions.warnings) != 1 {
		t.Fatalf("locked provider: skipped=%v warnings=%v, want codex skipped with one warning", conditions.skipped, conditions.warnings)
	}
	wantLines(t, h.root, noUsableProviderCategory, 0)

	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	mustConditionTick(t, h.root, h.cache, work, blockedAs(true))
	wantLines(t, h.root, noUsableProviderCategory, 1)
}

// GIVEN a lift written to alerts.log by a watcher that crashed before
// removing the record
// WHEN the next tick observes the provider unblocked
// THEN the record is removed without a second lift line.
func TestProviderCondition_CrashAfterLiftWriteIsCompletedOnce(t *testing.T) {
	h := newPoolRepairHarness(t)
	writeCodexSignal(t, h.root, time.Now().UTC().Add(-time.Hour))
	mustConditionTick(t, h.root, h.cache, []ProviderBlockedRoleWork{codexCoderWork}, blockedAs(true))
	record, _ := codexCondition(t, h.root)
	if err := alerts.Write(paths.New(h.root).AlertsLogPath(), providerBlockLiftedAlert(record, time.Now().UTC())); err != nil {
		t.Fatal(err)
	}

	mustConditionTick(t, h.root, make(map[string]time.Time), nil, blockedAs(false))
	wantLines(t, h.root, providerBlockLiftedCategory, 1)
	if _, exists := codexCondition(t, h.root); exists {
		t.Fatal("record kept after the completed lift")
	}
}

// GIVEN a record that cannot be parsed
// WHEN a watcher ticks the blocked provider
// THEN it warns, raises nothing and leaves the record as it is.
func TestProviderCondition_UnparseableRecordIsLeftAndWarned(t *testing.T) {
	h := newPoolRepairHarness(t)
	writeCodexSignal(t, h.root, time.Now().UTC().Add(-time.Hour))
	path := providerConditionPath(h.root, "codex")
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	conditions := mustConditionTick(t, h.root, h.cache, []ProviderBlockedRoleWork{codexCoderWork}, blockedAs(true))
	if len(conditions.warnings) != 1 || !strings.Contains(conditions.warnings[0], "delete") {
		t.Fatalf("warnings = %v, want one naming the remedy", conditions.warnings)
	}
	wantLines(t, h.root, noUsableProviderCategory, 0)
	if data, err := os.ReadFile(path); err != nil || string(data) != "{" {
		t.Fatalf("record = %q, %v; want it untouched", data, err)
	}
}

// GIVEN an announced block
// WHEN auto repair is disabled and re-enabled with the event unchanged, and
// later disabled while the block lifts, then re-enabled
// THEN the unchanged event is not re-logged and the lift is announced once.
func TestProviderCondition_DisablingRepairKeepsTheCondition(t *testing.T) {
	h := newPoolRepairHarness(t)
	state := coderBlockedDemand()
	blockProviderQuota(t, h.root, "codex")
	logOutcome(t, h.root, h.tick(state))

	t.Setenv(EnvAutoRepairAgentPool, "0")
	logOutcome(t, h.root, h.tick(state))
	t.Setenv(EnvAutoRepairAgentPool, "1")
	logOutcome(t, h.root, h.tick(state))
	wantLines(t, h.root, noUsableProviderCategory, 1)

	t.Setenv(EnvAutoRepairAgentPool, "0")
	if err := providersignal.ClearQuotaSignal(h.root, "codex"); err != nil {
		t.Fatal(err)
	}
	logOutcome(t, h.root, h.tick(state))
	wantLines(t, h.root, providerBlockLiftedCategory, 0)
	t.Setenv(EnvAutoRepairAgentPool, "1")
	logOutcome(t, h.root, h.tick(state))
	wantLines(t, h.root, providerBlockLiftedCategory, 1)
}

// GIVEN watcher A's snapshot taken while the provider blocked
// WHEN watcher B observes the lift and closes the condition (the expired
// signal stays), and A then reconciles its stale snapshot
// THEN A opens nothing and raises nothing: block state is what A reads under
// the record lock, not its snapshot.
func TestProviderCondition_StaleBlockedSnapshotAfterLift(t *testing.T) {
	h := newPoolRepairHarness(t)
	writeCodexSignal(t, h.root, time.Now().UTC().Add(-time.Hour))
	work := []ProviderBlockedRoleWork{codexCoderWork}
	a, b := make(map[string]time.Time), make(map[string]time.Time)
	mustConditionTick(t, h.root, a, work, blockedAs(true))
	mustConditionTick(t, h.root, b, nil, blockedAs(false))

	conditions := mustConditionTick(t, h.root, make(map[string]time.Time), work, blockedAs(false))
	if len(conditions.open) != 0 {
		t.Fatalf("stale snapshot opened %v", conditions.open)
	}
	if _, exists := codexCondition(t, h.root); exists {
		t.Fatal("stale snapshot reopened the condition")
	}
	wantLines(t, h.root, noUsableProviderCategory, 1)
	wantLines(t, h.root, providerBlockLiftedCategory, 1)
}

// GIVEN watcher A whose snapshot found nothing blocked while a record is open
// WHEN watcher B re-raises before A reconciles, and A then reconciles
// THEN A keeps the renewed condition open: its close decision is the locked
// re-read, which still blocks.
func TestProviderCondition_DelayedCloseAfterRelatch(t *testing.T) {
	h := newPoolRepairHarness(t)
	detected := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	work := []ProviderBlockedRoleWork{codexCoderWork}
	writeCodexSignal(t, h.root, detected)
	mustConditionTick(t, h.root, make(map[string]time.Time), work, blockedAs(true))

	writeCodexSignal(t, h.root, detected.Add(time.Minute))
	mustConditionTick(t, h.root, make(map[string]time.Time), work, blockedAs(true))
	mustConditionTick(t, h.root, make(map[string]time.Time), nil, blockedAs(true))

	if record, exists := codexCondition(t, h.root); !exists || record.Events != 2 {
		t.Fatalf("record = %+v, %t; want the renewed condition open with two events", record, exists)
	}
	wantLines(t, h.root, providerBlockLiftedCategory, 0)
	wantLines(t, h.root, noUsableProviderCategory, 2)
}

// GIVEN a watcher whose pre-lock snapshot carries the old block's deadline
// WHEN a peer renews the exhaustion before the watcher reconciles, or the
// block's deadline becomes unknown (a running recovery probe)
// THEN the one durable line for the observed event states the deadline read
// with that event, not the snapshot's.
func TestProviderCondition_AlertStatesTheObservedDeadline(t *testing.T) {
	h := newPoolRepairHarness(t)
	detected := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	oldReset := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	newReset := time.Now().UTC().Add(50 * time.Hour).Truncate(time.Second)
	writeCodexSignalResetting(t, h.root, detected, oldReset)
	stale := codexCoderWork
	stale.Until = oldReset.Add(time.Minute)

	writeCodexSignalResetting(t, h.root, detected.Add(time.Minute), newReset)
	mustConditionTick(t, h.root, make(map[string]time.Time), []ProviderBlockedRoleWork{stale}, QuotaProviderBlock(h.root))
	lines := wantLines(t, h.root, noUsableProviderCategory, 1)
	if want := newReset.Add(time.Minute).Format(time.RFC3339); !strings.Contains(lines[0], want) {
		t.Fatalf("alert does not state the renewed deadline %s: %s", want, lines[0])
	}
	if strings.Contains(lines[0], stale.Until.Format(time.RFC3339)) {
		t.Fatalf("alert states the snapshot's stale deadline: %s", lines[0])
	}

	writeCodexSignalResetting(t, h.root, detected.Add(2*time.Minute), newReset)
	probing := func(string) (time.Time, bool) { return time.Time{}, true }
	mustConditionTick(t, h.root, make(map[string]time.Time), []ProviderBlockedRoleWork{stale}, probing)
	lines = wantLines(t, h.root, noUsableProviderCategory, 2)
	if !strings.Contains(lines[1], "until an unknown time") || strings.Contains(lines[1], stale.Until.Format(time.RFC3339)) {
		t.Fatalf("alert during an unknown deadline states a snapshot deadline: %s", lines[1])
	}
}
