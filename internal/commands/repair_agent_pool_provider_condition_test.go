package commands

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/providersignal"
)

// Issue #115: a quota-blocked role's NO USABLE PROVIDER condition is one
// operational condition across every watcher of a project and across watcher
// restarts, and its end is announced once.

const providerBlockLiftedCategory = "PROVIDER BLOCK LIFTED"

// watcherFor returns another watcher over h's project: its own cache, as a
// second watcher process or a restarted one has.
func (h *poolRepairHarness) watcherFor() *poolRepairHarness {
	return &poolRepairHarness{t: h.t, root: h.root, cache: make(map[string]time.Time), nextPID: h.nextPID + 1000, status: h.status}
}

// logOutcome writes a tick's repair alerts to alerts.log as the watch loop
// does, and returns the log's lines.
func logOutcome(t *testing.T, root string, outcome AutoRepairAgentPoolOutcome) {
	t.Helper()
	for _, alert := range outcome.Alerts {
		if err := WriteAlert(paths.New(root).AlertsLogPath(), alert); err != nil {
			t.Fatalf("write alert: %v", err)
		}
	}
}

func alertLogLines(t *testing.T, root, category string) []string {
	t.Helper()
	data, err := os.ReadFile(paths.New(root).AlertsLogPath())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for line := range strings.Lines(string(data)) {
		if strings.Contains(line, " "+category+": ") {
			out = append(out, strings.TrimSuffix(line, "\n"))
		}
	}
	return out
}

func signalDetected(t *testing.T, root, provider string) string {
	t.Helper()
	data, err := os.ReadFile(providersignal.QuotaSignalPath(root, provider))
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.Lines(string(data)) {
		if value, ok := strings.CutPrefix(line, "detected: "); ok {
			return strings.TrimSpace(value)
		}
	}
	t.Fatal("signal has no detected line")
	return ""
}

// GIVEN coder demand whose provider is quota-blocked
// WHEN two watchers tick the project, then a restarted watcher ticks it
// THEN alerts.log holds one NO USABLE PROVIDER line for the one quota event.
func TestAutoRepair_NoUsableProviderLoggedOnceAcrossWatchersAndRestart(t *testing.T) {
	a := newPoolRepairHarness(t)
	b := a.watcherFor()
	state := coderBlockedDemand()
	blockProviderQuota(t, a.root, "codex")

	for range 2 {
		logOutcome(t, a.root, a.tick(state))
		logOutcome(t, a.root, b.tick(state))
	}
	restarted := a.watcherFor()
	logOutcome(t, a.root, restarted.tick(state))

	if len(a.spawned) != 0 {
		t.Fatalf("spawned %d agent(s) on the quota-blocked provider, want none", len(a.spawned))
	}
	if lines := alertLogLines(t, a.root, noUsableProviderCategory); len(lines) != 1 {
		t.Fatalf("NO USABLE PROVIDER lines = %d, want 1 across two watchers and a restart:\n%s", len(lines), strings.Join(lines, "\n"))
	}
}

// GIVEN a watcher that logged a quota block and stopped
// WHEN the signal is removed and a new watcher runs its first tick
// THEN one PROVIDER BLOCK LIFTED line names the provider, the original block
// start and the role it held.
func TestAutoRepair_LiftAfterRestartAndSignalRemovalIsAnnounced(t *testing.T) {
	a := newPoolRepairHarness(t)
	state := coderBlockedDemand()
	blockProviderQuota(t, a.root, "codex")
	detected := signalDetected(t, a.root, "codex")
	logOutcome(t, a.root, a.tick(state))

	if err := providersignal.ClearQuotaSignal(a.root, "codex"); err != nil {
		t.Fatal(err)
	}
	restarted := a.watcherFor()
	logOutcome(t, a.root, restarted.tick(state))

	lines := alertLogLines(t, a.root, providerBlockLiftedCategory)
	if len(lines) != 1 {
		t.Fatalf("PROVIDER BLOCK LIFTED lines = %d, want 1:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	for _, want := range []string{"codex", detected, "coder"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("lift line missing %q: %s", want, lines[0])
		}
	}
}
