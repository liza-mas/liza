// Package providersignal detects provider stop conditions (quota exhaustion,
// provider unavailability) in agent output and owns their signal files, reset
// interpretation, quota sweep and alert-line writes. It takes a project root,
// a tool identity and output text, and holds no blackboard handle: task
// ownership and runtime artifacts stay with the callers.
package providersignal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/liza-mas/liza/internal/agentlaunch"
	"github.com/liza-mas/liza/internal/paths"
)

// quotaPattern defines a provider-specific pattern that indicates quota exhaustion.
type quotaPattern struct {
	// Provider is the canonical provider name (e.g. "codex", "claude", "cursor").
	Provider string
	// Pattern matches a single output line that indicates quota exhaustion.
	Pattern *regexp.Regexp
}

// quotaPatterns is the registry of known quota-exhaustion signatures.
// Add new entries here when a new provider's quota message is observed.
var quotaPatterns = []quotaPattern{
	{Provider: "codex", Pattern: regexp.MustCompile(`You've hit your .*limit`)},
	{Provider: "cursor", Pattern: regexp.MustCompile(`Upgrade your plan to continue`)},
	{Provider: "cursor", Pattern: regexp.MustCompile(`You've hit your .*usage limit`)},
	{Provider: "cursor", Pattern: regexp.MustCompile(`set a Spend Limit to continue`)},
	{Provider: "claude", Pattern: regexp.MustCompile(`You're out of extra usage`)},
	{Provider: "claude", Pattern: regexp.MustCompile(`You've hit your .*limit`)},
}

// QuotaExhaustion holds details about a detected quota event.
type QuotaExhaustion struct {
	Provider string
	Message  string    // the matching line from output
	ResetsAt time.Time // the provider's announced reset; zero when none was announced
}

// DetectQuotaExhaustion scans agent output for quota-exhaustion patterns.
// Returns non-nil if a known pattern is found.
func DetectQuotaExhaustion(output, cliName string) *QuotaExhaustion {
	return detectQuotaExhaustionAt(output, cliName, time.Now())
}

func detectQuotaExhaustionAt(output, cliName string, now time.Time) *QuotaExhaustion {
	provider := canonicalQuotaProvider(cliName)
	for _, line := range DiagnosticLines(output) {
		for _, p := range quotaPatterns {
			if p.Provider != provider {
				continue
			}
			if p.Pattern.MatchString(line) {
				return &QuotaExhaustion{
					Provider: p.Provider,
					Message:  line,
					ResetsAt: quotaResetTime(output, line, p.Provider, now),
				}
			}
		}
	}
	// A 429 is quota only when it announces a reset: a throttle without one
	// is left to the crash backoff rather than blocking the fleet for the
	// 30-minute fallback.
	if provider == "claude" {
		if message, ok := claudeRateLimitedResult(output); ok {
			if resetsAt := quotaResetTime(output, message, provider, now); !resetsAt.IsZero() {
				return &QuotaExhaustion{Provider: provider, Message: message, ResetsAt: resetsAt}
			}
		}
	}
	return nil
}

const quotaSignalPrefix = "provider-quota-exhausted-"

// QuotaSignalPath returns the path to the quota signal file for a provider.
func QuotaSignalPath(projectRoot, provider string) string {
	return filepath.Join(paths.New(projectRoot).LizaDir(), quotaSignalPrefix+canonicalQuotaProvider(provider))
}

// QuotaSignalFile names the quota signal file for a provider relative to the
// project root, as operator-facing remedies print it.
func QuotaSignalFile(provider string) string {
	return filepath.Join(paths.ProjectDirName(), quotaSignalPrefix+canonicalQuotaProvider(provider))
}

// QuotaSignalGlob returns a glob pattern matching all quota signal files.
func QuotaSignalGlob(projectRoot string) string {
	return filepath.Join(paths.New(projectRoot).LizaDir(), quotaSignalPrefix+"*")
}

// ProviderFromSignalFile extracts the provider name from a quota signal file path.
func ProviderFromSignalFile(path string) string {
	return filepath.Base(path)[len(quotaSignalPrefix):]
}

// WriteQuotaSignal creates a signal file that tells all supervisors using
// this provider to terminate gracefully. No reset is recorded, so the signal
// lifts after the bounded fallback.
func WriteQuotaSignal(projectRoot, provider, message string) error {
	return writeQuotaSignal(projectRoot, provider, message, time.Time{}, time.Now().UTC())
}

func writeQuotaSignal(projectRoot, provider, message string, resetsAt, detected time.Time) error {
	provider = canonicalQuotaProvider(provider)
	announced := resetsAtUnannounced
	if !resetsAt.IsZero() {
		announced = resetsAt.UTC().Format(time.RFC3339)
	}
	content := recordLine(recordKeyProvider, provider) +
		recordLine(recordKeyDetected, detected.Format(time.RFC3339)) +
		recordLine(recordKeyMessage, message) +
		recordLine(recordKeyResetsAt, announced) +
		recordLine(recordKeyExpires, quotaSignalExpiry(resetsAt, detected).UTC().Format(time.RFC3339))
	return os.WriteFile(QuotaSignalPath(projectRoot, provider), []byte(content), signalFileMode)
}

// CheckQuotaSignal reports whether a running agent on the provider must stop
// before its next provider start: the quota signal has not reached its
// expiry, or another agent's recovery probe is running. A spawn reservation
// does not stop it: the reserved agent must reach its first start.
func CheckQuotaSignal(projectRoot, provider string) bool {
	now := time.Now()
	expires, ok, readable := readQuotaSignalState(QuotaSignalPath(projectRoot, provider), now)
	if !ok {
		return false
	}
	if !readable || now.Before(expires) {
		return true
	}
	_, probing := liveQuotaProbe(projectRoot, provider, now)
	return probing
}

// QuotaBlockedUntil reports whether starting a new agent on the provider is
// blocked, and until when: while the quota signal has not expired, and after
// its expiry while a recovery attempt (reservation or probe) is coordinated.
// The expiry is zero when none is known: an unreadable signal (fail closed)
// or a running probe, which lasts as long as its turn.
func QuotaBlockedUntil(projectRoot, provider string) (time.Time, bool) {
	now := time.Now()
	expires, ok, readable := readQuotaSignalState(QuotaSignalPath(projectRoot, provider), now)
	if !ok {
		return time.Time{}, false
	}
	if !readable {
		return time.Time{}, true
	}
	if now.Before(expires) {
		return expires, true
	}
	if lease, live := readLiveQuotaLease(projectRoot, provider, now); live {
		return lease.until(), true
	}
	return time.Time{}, false
}

// HeldQuotaSignal is a quota signal that still blocks its provider.
type HeldQuotaSignal struct {
	Provider string
	Until    time.Time
	// File is the signal file relative to the project root.
	File string
}

// RecoveringQuotaSignal is an expired quota signal awaiting its recovery
// probe: the provider is not known to have recovered yet.
type RecoveringQuotaSignal struct {
	Provider string
	Expired  time.Time
	// File is the signal file relative to the project root.
	File string
	// Attempt names the coordinated reservation or probe; empty when the next
	// provider start will take it.
	Attempt string
}

// Describe states the signal's recovery state and the manual override, as
// resume prints it.
func (r RecoveringQuotaSignal) Describe() string {
	state := "recovery pending: the next provider start probes it"
	if r.Attempt != "" {
		state = "recovery in progress: " + r.Attempt
	}
	return fmt.Sprintf("Quota signal for provider: %s expired at %s; %s (delete %s to lift it now)",
		r.Provider, r.Expired.UTC().Format(time.RFC3339), state, r.File)
}

// QuotaSignalReport is what ReportQuotaSignals found.
type QuotaSignalReport struct {
	Held       []HeldQuotaSignal
	Recovering []RecoveringQuotaSignal
}

// ReportQuotaSignals reports the quota signals and removes none. Resume uses
// it: a provider-announced reset outlives a pause, and an expired signal
// leaves only through its recovery probe's proof, a renewed exhaustion, or a
// deliberate manual unblock (deleting that provider's file), so a resume
// cannot turn an unproven recovery into a spawn burst. An unreadable signal
// is held, as the spawn gate treats it.
func ReportQuotaSignals(projectRoot string) QuotaSignalReport {
	var report QuotaSignalReport
	matches, _ := filepath.Glob(QuotaSignalGlob(projectRoot)) // the pattern is well formed
	now := time.Now()
	for _, path := range matches {
		provider := ProviderFromSignalFile(path)
		expires, ok := readQuotaSignalExpiry(path, now)
		if !ok {
			continue // removed since the glob
		}
		if now.Before(expires) {
			report.Held = append(report.Held, HeldQuotaSignal{Provider: provider, Until: expires, File: QuotaSignalFile(provider)})
			continue
		}
		recovering := RecoveringQuotaSignal{Provider: provider, Expired: expires, File: QuotaSignalFile(provider)}
		if lease, live := readLiveQuotaLease(projectRoot, provider, now); live {
			recovering.Attempt = attemptHolderText(lease)
		}
		report.Recovering = append(report.Recovering, recovering)
	}
	return report
}

// LogAlert appends an alert line to alerts.log.
func LogAlert(projectRoot, level, category, message string) error {
	alertsPath := paths.New(projectRoot).AlertsLogPath()
	f, err := os.OpenFile(alertsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, signalFileMode)
	if err != nil {
		return fmt.Errorf("open alerts log: %w", err)
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "[%s] %s %s — %s\n",
		time.Now().UTC().Format(time.RFC3339), level, category, message)
	return err
}

// LogQuotaAlert appends a quota-exhaustion alert to alerts.log.
func LogQuotaAlert(projectRoot string, qe *QuotaExhaustion) error {
	return logQuotaAlert(projectRoot, qe, time.Now().UTC())
}

func logQuotaAlert(projectRoot string, qe *QuotaExhaustion, detected time.Time) error {
	until := quotaSignalExpiry(qe.ResetsAt, detected).UTC().Format(time.RFC3339)
	return LogAlert(projectRoot, alertLevel, alertCategoryQuotaExhausted, qe.Provider+": "+qe.Message+" (spawns blocked until "+until+")")
}

// RaiseQuotaExhaustion records both human-visible and process-visible quota
// state for an exhaustion no agent's probe observed.
func RaiseQuotaExhaustion(projectRoot string, qe *QuotaExhaustion) error {
	return RaiseQuotaExhaustionBy(projectRoot, qe, "")
}

// RaiseQuotaExhaustionBy is RaiseQuotaExhaustion for the agent raiserID that
// observed the exhaustion. The new signal is written before any recovery
// lease is dropped, so no start sees an expired signal without its attempt.
// The lease dropped is a spawn reservation (no provider ran under it) or the
// raiser's own probe; another agent's running probe stays, so no replacement
// overlaps it.
func RaiseQuotaExhaustionBy(projectRoot string, qe *QuotaExhaustion, raiserID string) error {
	// Only under the lifecycle lock, so a probe settling its success cannot
	// read the old signal and then remove this newer one. Contention is
	// retried for a bounded budget; past it the signal is not written outside
	// the lock, and the failure is alerted and returned instead.
	ctx, cancel := context.WithTimeout(context.Background(), quotaRaiseLockBudget)
	defer cancel()
	raised := false
	lockErr := quotaLifecycleLock(projectRoot, qe.Provider).WithRetryContext(ctx).WithLockOperation(quotaLifecycleLockOperation, func() error {
		raised = true
		detected := time.Now().UTC()
		if err := errors.Join(
			logQuotaAlert(projectRoot, qe, detected),
			writeQuotaSignal(projectRoot, qe.Provider, qe.Message, qe.ResetsAt, detected),
		); err != nil {
			return err
		}
		data, err := os.ReadFile(QuotaProbePath(projectRoot, qe.Provider))
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read quota recovery lease: %w", err)
		}
		if lease, parsed := parseQuotaLease(data); parsed && (lease.Kind == quotaLeaseReservation || lease.holds(raiserID)) {
			return removeQuotaLease(projectRoot, qe.Provider)
		}
		return nil
	})
	if raised {
		return lockErr
	}
	notWritten := fmt.Errorf("quota signal for %s not written: recovery lifecycle lock unavailable: %w", qe.Provider, lockErr)
	alertErr := LogAlert(projectRoot, alertLevel, alertCategoryQuotaExhausted,
		fmt.Sprintf("%s: %s (quota signal NOT written: the recovery lifecycle lock %s stayed unavailable; spawns are not blocked by this detection)",
			canonicalQuotaProvider(qe.Provider), qe.Message, quotaLifecycleLockFile(qe.Provider)))
	return errors.Join(notWritten, alertErr)
}

// LogQuotaSpawnBlockedAlert appends an alert when the quota lifecycle refuses
// a spawn: an unexpired signal, or an expired one whose recovery attempt is
// already coordinated.
func LogQuotaSpawnBlockedAlert(projectRoot, provider, role string) error {
	now := time.Now()
	path := QuotaSignalPath(projectRoot, provider)
	until := "its expiry"
	expires, ok := readQuotaSignalExpiry(path, now)
	if ok {
		until = expires.UTC().Format(time.RFC3339)
	}
	message := fmt.Sprintf("%s: refused to spawn %s while quota signal is set until %s; it lifts automatically then, or delete %s to lift it earlier", provider, role, until, QuotaSignalFile(provider))
	if ok && !now.Before(expires) {
		if lease, live := readLiveQuotaLease(projectRoot, provider, now); live {
			message = fmt.Sprintf("%s: refused to spawn %s while the quota recovery %s is in progress; spawns resume when it proves recovery, or delete %s and %s to lift the block now", provider, role, attemptHolderText(lease), QuotaSignalFile(provider), QuotaProbeFile(provider))
		}
	}
	return LogAlert(projectRoot, alertLevel, alertCategoryQuotaSpawnBlocked, message)
}

// ClearQuotaSignal is the deliberate manual unblock: it removes the
// provider's quota signal and its recovery lease, whatever their state.
func ClearQuotaSignal(projectRoot, provider string) error {
	remove := func(path string) error {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	exists := func(path string) bool {
		_, err := os.Lstat(path)
		return !os.IsNotExist(err)
	}
	if !exists(QuotaSignalPath(projectRoot, provider)) && !exists(QuotaProbePath(projectRoot, provider)) {
		return nil // nothing to unblock, and maybe no project directory to lock in
	}
	return withQuotaLifecycleLock(projectRoot, provider, func() error {
		return errors.Join(remove(QuotaSignalPath(projectRoot, provider)), remove(QuotaProbePath(projectRoot, provider)))
	})
}

func canonicalQuotaProvider(cliName string) string {
	return agentlaunch.ACPXAgentName(cliName)
}
