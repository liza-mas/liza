package alerts

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/filelock"
)

// AlertLevel is the severity of a watch or lifecycle alert.
type AlertLevel string

const (
	AlertLevelWarning  AlertLevel = "⚠️"
	AlertLevelCritical AlertLevel = "🚨"
)

// Alert represents a single anomaly or lifecycle alert.
//
// Identity and OnceKey are in-memory only; String does not write them.
type Alert struct {
	Timestamp time.Time
	Level     AlertLevel
	Category  string
	Message   string
	// Identity names the condition when Message is not a stable digest of it,
	// e.g. because the message carries an elapsed time or omits a field whose
	// change makes a new condition. Empty means the message is the identity.
	Identity string
	// OnceKey, when set, makes Write log the alert at most once per key across
	// all writers of the same alerts log.
	OnceKey string
}

func (a Alert) String() string {
	return fmt.Sprintf("[%s] %s %s: %s",
		a.Timestamp.UTC().Format(time.RFC3339),
		a.Level,
		a.Category,
		a.Message)
}

// ParseLine parses a line written by Alert.String() back into an Alert.
func ParseLine(line string) (Alert, bool) {
	if !strings.HasPrefix(line, "[") {
		return Alert{}, false
	}
	closeBracket := strings.IndexByte(line, ']')
	if closeBracket < 0 {
		return Alert{}, false
	}
	ts, err := time.Parse(time.RFC3339, line[1:closeBracket])
	if err != nil {
		return Alert{}, false
	}

	rest := strings.TrimLeft(line[closeBracket+1:], " ")
	colonIdx := strings.Index(rest, ": ")
	if colonIdx < 0 {
		return Alert{}, false
	}
	levelAndCategory := rest[:colonIdx]
	message := rest[colonIdx+2:]

	spaceIdx := strings.IndexByte(levelAndCategory, ' ')
	if spaceIdx < 0 {
		return Alert{}, false
	}
	level := AlertLevel(levelAndCategory[:spaceIdx])
	category := strings.TrimSpace(levelAndCategory[spaceIdx+1:])

	return Alert{
		Timestamp: ts,
		Level:     level,
		Category:  category,
		Message:   message,
	}, true
}

// Key returns a stable identity for an alert condition.
func Key(alert Alert) string {
	return alert.Category + ":" + alert.Message
}

// GateKey returns the identity under which repeated emissions of a condition
// are suppressed. Unlike Key, it survives messages that carry elapsed time.
func GateKey(alert Alert) string {
	if alert.Identity != "" {
		return alert.Category + ":" + alert.Identity
	}
	return Key(alert)
}

// BlockedEpisodeKey identifies one BLOCKED alert per blocked episode and
// message. The episode is the task's blocked history entry, persisted in state,
// so the lifecycle writer and every watcher derive the same key.
func BlockedEpisodeKey(taskID string, episodeStart time.Time, message string) string {
	digest := sha256.Sum256([]byte(message))
	return fmt.Sprintf("BLOCKED|%s|%s|%x", taskID, episodeStart.UTC().Format(time.RFC3339Nano), digest[:8])
}

// onceLedgerLockTimeout bounds the wait of a watch tick or lifecycle writer on
// the once-ledger; past it the alert is written without the ledger.
const onceLedgerLockTimeout = 2 * time.Second

// LedgerUnavailableError reports that an alert carrying an OnceKey was written
// without consulting the once-ledger, so it may duplicate an earlier line.
type LedgerUnavailableError struct {
	Err error
}

func (e *LedgerUnavailableError) Error() string {
	return fmt.Sprintf("alert written without once-ledger (may duplicate): %v", e.Err)
}

func (e *LedgerUnavailableError) Unwrap() error { return e.Err }

// OnceLedgerPath returns the ledger of OnceKeys already written to alertsLog.
func OnceLedgerPath(alertsLog string) string {
	return alertsLog + ".once"
}

// Write appends an alert to the alerts log file. An alert with an OnceKey is
// appended only if no writer of the same log has recorded that key yet.
//
// Once-semantics are best-effort, not exactly-once: the alert is appended
// before its key, so a crash between the two can duplicate it later; and when
// the ledger cannot be locked, read or appended, the alert is still appended
// and a LedgerUnavailableError returned. Both prefer a duplicate line to a
// lost alert.
func Write(alertsLog string, alert Alert) error {
	if alert.OnceKey == "" {
		return appendAlert(alertsLog, alert)
	}
	ledger := OnceLedgerPath(alertsLog)
	lockErr := filelock.New(ledger).WithTimeout(onceLedgerLockTimeout).WithLockOperation("alert-once", func() error {
		recorded, err := ledgerHasKey(ledger, alert.OnceKey)
		if err != nil {
			if appendErr := appendAlert(alertsLog, alert); appendErr != nil {
				return appendErr
			}
			return &LedgerUnavailableError{Err: err}
		}
		if recorded {
			return nil
		}
		if err := appendAlert(alertsLog, alert); err != nil {
			return err
		}
		if err := appendLedgerKey(ledger, alert.OnceKey); err != nil {
			// The alert is logged; only its key is missing.
			return &LedgerUnavailableError{Err: err}
		}
		return nil
	})
	var lockFailure *filelock.LockError
	if errors.As(lockErr, &lockFailure) {
		if err := appendAlert(alertsLog, alert); err != nil {
			return err
		}
		return &LedgerUnavailableError{Err: lockErr}
	}
	return lockErr
}

func ledgerHasKey(ledger, key string) (bool, error) {
	data, err := os.ReadFile(ledger)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to read alert once-ledger: %w", err)
	}
	for line := range strings.Lines(string(data)) {
		if strings.TrimSuffix(line, "\n") == key {
			return true, nil
		}
	}
	return false, nil
}

func appendLedgerKey(ledger, key string) error {
	f, err := os.OpenFile(ledger, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open alert once-ledger: %w", err)
	}
	defer f.Close()
	if _, err := fmt.Fprintln(f, key); err != nil {
		return fmt.Errorf("failed to record alert once-key: %w", err)
	}
	return nil
}

func appendAlert(alertsLog string, alert Alert) error {
	f, err := os.OpenFile(alertsLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open alerts log: %w", err)
	}
	defer f.Close()

	if _, err := fmt.Fprintln(f, alert.String()); err != nil {
		return fmt.Errorf("failed to write alert: %w", err)
	}
	return nil
}
