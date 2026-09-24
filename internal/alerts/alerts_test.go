package alerts

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/testhelpers/perm"
)

func blockedAlert(at time.Time, episodeStart time.Time, reason string) Alert {
	message := "task-1 — " + reason
	return Alert{
		Timestamp: at,
		Level:     AlertLevelWarning,
		Category:  "BLOCKED",
		Message:   message,
		OnceKey:   BlockedEpisodeKey("task-1", episodeStart, message),
	}
}

func loggedLines(t *testing.T, alertsLog string) []string {
	t.Helper()
	data, err := os.ReadFile(alertsLog)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("read alerts log: %v", err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

func TestWriteOnceKeyLogsOneLinePerEpisodeInEitherArrivalOrder(t *testing.T) {
	episode := time.Date(2026, 9, 24, 8, 2, 14, 123456789, time.UTC)
	lifecycle := blockedAlert(episode, episode, "needs spec")
	watcher := blockedAlert(episode.Add(5*time.Second), episode, "needs spec")

	orders := map[string][]Alert{
		"lifecycle first": {lifecycle, watcher},
		"watcher first":   {watcher, lifecycle},
		// A writer whose line carries an earlier timestamp still dedupes:
		// nothing compares timestamps.
		"out-of-order timestamps": {watcher, blockedAlert(episode.Add(-time.Minute), episode, "needs spec")},
	}
	for name, writes := range orders {
		t.Run(name, func(t *testing.T) {
			alertsLog := filepath.Join(t.TempDir(), "alerts.log")
			for _, alert := range writes {
				if err := Write(alertsLog, alert); err != nil {
					t.Fatalf("Write() error = %v", err)
				}
			}
			if got := loggedLines(t, alertsLog); len(got) != 1 {
				t.Fatalf("logged lines = %d, want 1:\n%s", len(got), strings.Join(got, "\n"))
			}
		})
	}
}

func TestWriteOnceKeyKeepsDistinctEpisodesAndReasons(t *testing.T) {
	first := time.Date(2026, 9, 24, 8, 2, 14, 0, time.UTC)
	alertsLog := filepath.Join(t.TempDir(), "alerts.log")

	writes := []Alert{
		blockedAlert(first, first, "needs spec"),
		// A second episode within the same second is still a new episode.
		blockedAlert(first, first.Add(time.Millisecond), "needs spec"),
		// A reason change within an episode is a new message.
		blockedAlert(first, first.Add(time.Millisecond), "needs a decision"),
	}
	for _, alert := range writes {
		if err := Write(alertsLog, alert); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}
	if got := loggedLines(t, alertsLog); len(got) != 3 {
		t.Fatalf("logged lines = %d, want 3:\n%s", len(got), strings.Join(got, "\n"))
	}
}

func TestWriteOnceKeyConcurrentWritersLogOnce(t *testing.T) {
	episode := time.Date(2026, 9, 24, 8, 2, 14, 0, time.UTC)
	alertsLog := filepath.Join(t.TempDir(), "alerts.log")

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- Write(alertsLog, blockedAlert(episode.Add(time.Duration(i)*time.Second), episode, "needs spec"))
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}
	if got := loggedLines(t, alertsLog); len(got) != 1 {
		t.Fatalf("logged lines = %d, want 1:\n%s", len(got), strings.Join(got, "\n"))
	}
}

func TestWriteWithoutOnceKeyAlwaysAppends(t *testing.T) {
	alertsLog := filepath.Join(t.TempDir(), "alerts.log")
	alert := Alert{Timestamp: time.Now().UTC(), Level: AlertLevelWarning, Category: "STALLED", Message: "no task progress for 30 minutes"}

	for range 2 {
		if err := Write(alertsLog, alert); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}
	if got := loggedLines(t, alertsLog); len(got) != 2 {
		t.Fatalf("logged lines = %d, want 2", len(got))
	}
	if _, err := os.Stat(OnceLedgerPath(alertsLog)); !os.IsNotExist(err) {
		t.Fatalf("plain alerts must not create the once-ledger; stat err = %v", err)
	}
}

func TestWriteOnceKeyAppendsWhenLedgerIsLocked(t *testing.T) {
	episode := time.Date(2026, 9, 24, 8, 2, 14, 0, time.UTC)
	alertsLog := filepath.Join(t.TempDir(), "alerts.log")
	alert := blockedAlert(episode, episode, "needs spec")

	held, acquired, err := filelock.New(OnceLedgerPath(alertsLog)).TryHold("test-hold")
	if err != nil || !acquired {
		t.Fatalf("TryHold() acquired = %v, err = %v", acquired, err)
	}
	t.Cleanup(func() { _ = held.Release() })

	err = Write(alertsLog, alert)
	var ledgerErr *LedgerUnavailableError
	if !errors.As(err, &ledgerErr) {
		t.Fatalf("Write() error = %v, want LedgerUnavailableError", err)
	}
	if got := loggedLines(t, alertsLog); len(got) != 1 {
		t.Fatalf("logged lines = %d, want 1: a locked ledger must not lose the alert", len(got))
	}
}

func TestGateKeyPrefersIdentityOverMessage(t *testing.T) {
	base := Alert{Category: "STALLED", Message: "no task progress for 31 minutes", Identity: "episode|step 0"}
	later := base
	later.Message = "no task progress for 45 minutes"

	if GateKey(base) != GateKey(later) {
		t.Fatalf("GateKey changed with the message: %q vs %q", GateKey(base), GateKey(later))
	}
	if Key(base) == Key(later) {
		t.Fatal("Key must keep tracking the message for freshness resolution")
	}
	plain := Alert{Category: "BLOCKED", Message: "task-1 — needs spec"}
	if GateKey(plain) != Key(plain) {
		t.Fatalf("GateKey without identity = %q, want Key %q", GateKey(plain), Key(plain))
	}
}

func TestStringOmitsInMemoryFields(t *testing.T) {
	alert := blockedAlert(time.Date(2026, 9, 24, 8, 2, 14, 0, time.UTC), time.Now(), "needs spec")
	alert.Identity = "identity"

	line := alert.String()
	if strings.Contains(line, alert.OnceKey) || strings.Contains(line, "identity") {
		t.Fatalf("String() = %q leaks in-memory fields", line)
	}
	parsed, ok := ParseLine(line)
	if !ok || parsed.Category != alert.Category || parsed.Message != alert.Message {
		t.Fatalf("ParseLine(String()) = %+v, %v", parsed, ok)
	}
}

// ledgerUncreatable returns an alerts log whose directory refuses new entries:
// the log and the ledger lock exist and stay writable, the ledger cannot be
// created.
func ledgerUncreatable(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	alertsLog := filepath.Join(dir, "alerts.log")
	for _, path := range []string{alertsLog, OnceLedgerPath(alertsLog) + ".lock"} {
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatalf("pre-create %s: %v", path, err)
		}
	}
	restore, err := perm.DenyWrites(dir)
	if err != nil {
		t.Fatalf("DenyWrites() error = %v", err)
	}
	t.Cleanup(func() { _ = restore() })
	return alertsLog
}

func TestWriteOnceKeyReportsLedgerAppendFailureAsLedgerUnavailable(t *testing.T) {
	episode := time.Date(2026, 9, 24, 8, 2, 14, 0, time.UTC)
	alertsLog := ledgerUncreatable(t)

	err := Write(alertsLog, blockedAlert(episode, episode, "needs spec"))
	var ledgerErr *LedgerUnavailableError
	if !errors.As(err, &ledgerErr) {
		t.Fatalf("Write() error = %v, want LedgerUnavailableError: the alert was logged, only its key was not", err)
	}
	if got := loggedLines(t, alertsLog); len(got) != 1 {
		t.Fatalf("logged lines = %d, want 1", len(got))
	}
}

func TestWriteReportsAlertLogFailureAsOrdinaryError(t *testing.T) {
	dir := t.TempDir()
	restore, err := perm.DenyWrites(dir)
	if err != nil {
		t.Fatalf("DenyWrites() error = %v", err)
	}
	t.Cleanup(func() { _ = restore() })
	alertsLog := filepath.Join(dir, "alerts.log")

	err = Write(alertsLog, Alert{Timestamp: time.Now().UTC(), Level: AlertLevelWarning, Category: "STALLED", Message: "m"})
	var ledgerErr *LedgerUnavailableError
	if err == nil || errors.As(err, &ledgerErr) {
		t.Fatalf("Write() error = %v, want an ordinary error when the alert log cannot be written", err)
	}
}
