// Package providersignal detects provider stop conditions (quota exhaustion,
// provider unavailability) in agent output and owns their signal files, reset
// interpretation, quota sweep and alert-line writes. It takes a project root,
// a tool identity and output text, and holds no blackboard handle: task
// ownership and runtime artifacts stay with the callers.
package providersignal

import (
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
	announced := "unknown"
	if !resetsAt.IsZero() {
		announced = resetsAt.UTC().Format(time.RFC3339)
	}
	content := fmt.Sprintf("provider: %s\ndetected: %s\nmessage: %s\nresets_at: %s\nexpires: %s\n",
		provider,
		detected.Format(time.RFC3339),
		message,
		announced,
		quotaSignalExpiry(resetsAt, detected).UTC().Format(time.RFC3339),
	)
	return os.WriteFile(QuotaSignalPath(projectRoot, provider), []byte(content), 0644)
}

// CheckQuotaSignal returns true while a quota signal for the provider exists
// and has not reached its expiry.
func CheckQuotaSignal(projectRoot, provider string) bool {
	_, blocked := QuotaBlockedUntil(projectRoot, provider)
	return blocked
}

// QuotaBlockedUntil reports whether a quota signal still blocks spawns on the
// provider, and its expiry when it does. An unreadable signal blocks (fail
// closed) with a zero expiry: none is known.
func QuotaBlockedUntil(projectRoot, provider string) (time.Time, bool) {
	now := time.Now()
	expires, ok, readable := readQuotaSignalState(QuotaSignalPath(projectRoot, provider), now)
	if !ok || !now.Before(expires) {
		return time.Time{}, false
	}
	if !readable {
		return time.Time{}, true
	}
	return expires, true
}

// HeldQuotaSignal is a quota signal that still blocks its provider.
type HeldQuotaSignal struct {
	Provider string
	Until    time.Time
	// File is the signal file relative to the project root.
	File string
}

// QuotaSignalSweep reports what ClearExpiredQuotaSignals did.
type QuotaSignalSweep struct {
	Cleared  []string
	Held     []HeldQuotaSignal
	Failures []string // "quota/<provider>: <error>"
}

// ClearExpiredQuotaSignals removes the quota signals that no longer block and
// keeps the others. Resume uses it: a provider-announced reset outlives a
// pause, and recovering one provider (an account switch) must not lift
// another's block, so the early lift is deleting that provider's file. An
// unreadable signal is held, as the spawn gate treats it.
func ClearExpiredQuotaSignals(projectRoot string) QuotaSignalSweep {
	var sweep QuotaSignalSweep
	matches, _ := filepath.Glob(QuotaSignalGlob(projectRoot)) // the pattern is well formed
	now := time.Now()
	for _, path := range matches {
		provider := ProviderFromSignalFile(path)
		expires, ok := readQuotaSignalExpiry(path, now)
		if !ok {
			continue // removed since the glob
		}
		if now.Before(expires) {
			sweep.Held = append(sweep.Held, HeldQuotaSignal{Provider: provider, Until: expires, File: QuotaSignalFile(provider)})
			continue
		}
		if err := ClearQuotaSignal(projectRoot, provider); err != nil {
			sweep.Failures = append(sweep.Failures, fmt.Sprintf("quota/%s: %v", provider, err))
			continue
		}
		sweep.Cleared = append(sweep.Cleared, provider)
	}
	return sweep
}

// LogAlert appends an alert line to alerts.log.
func LogAlert(projectRoot, level, category, message string) error {
	alertsPath := paths.New(projectRoot).AlertsLogPath()
	f, err := os.OpenFile(alertsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
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
	return LogAlert(projectRoot, "🚨", "PROVIDER QUOTA EXHAUSTED", qe.Provider+": "+qe.Message+" (spawns blocked until "+until+")")
}

// RaiseQuotaExhaustion records both human-visible and process-visible quota state.
func RaiseQuotaExhaustion(projectRoot string, qe *QuotaExhaustion) error {
	detected := time.Now().UTC()
	return errors.Join(
		logQuotaAlert(projectRoot, qe, detected),
		writeQuotaSignal(projectRoot, qe.Provider, qe.Message, qe.ResetsAt, detected),
	)
}

// LogQuotaSpawnBlockedAlert appends an alert when an unexpired quota signal blocks spawn.
func LogQuotaSpawnBlockedAlert(projectRoot, provider, role string) error {
	now := time.Now()
	until := "its expiry"
	if expires, ok := readQuotaSignalExpiry(QuotaSignalPath(projectRoot, provider), now); ok {
		until = expires.UTC().Format(time.RFC3339)
	}
	message := fmt.Sprintf("%s: refused to spawn %s while quota signal is set until %s; it lifts automatically then, or delete %s to lift it earlier", provider, role, until, QuotaSignalFile(provider))
	return LogAlert(projectRoot, "🚨", "PROVIDER QUOTA SPAWN BLOCKED", message)
}

// ClearQuotaSignal removes the quota signal file for a provider.
// Intended for use by the resume command or manual recovery.
func ClearQuotaSignal(projectRoot, provider string) error {
	err := os.Remove(QuotaSignalPath(projectRoot, provider))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func canonicalQuotaProvider(cliName string) string {
	return agentlaunch.ACPXAgentName(cliName)
}
