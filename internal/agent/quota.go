package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/brand"
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
	for _, line := range providerDiagnosticLines(output) {
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
	return nil
}

const quotaSignalPrefix = "provider-quota-exhausted-"

// QuotaSignalPath returns the path to the quota signal file for a provider.
func QuotaSignalPath(projectRoot, provider string) string {
	return filepath.Join(paths.New(projectRoot).LizaDir(), quotaSignalPrefix+canonicalQuotaProvider(provider))
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
	now := time.Now()
	expires, ok := readQuotaSignalExpiry(QuotaSignalPath(projectRoot, provider), now)
	return ok && now.Before(expires)
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
	message := fmt.Sprintf("%s: refused to spawn %s while quota signal is set until %s; it lifts automatically then, or delete the flag file or run %s then %s to lift it earlier", provider, role, until, brand.Command("pause"), brand.Command("resume"))
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
	return acpxAgentNameFromTool(cliName)
}

// tailReadSize is the maximum bytes to read from the end of an output file.
// Quota messages appear near the end; reading the full file is wasteful.
const tailReadSize = 8 * 1024

// latestAgentOutputContent reads tails from stdout/stderr files belonging to
// the same most-recent agent output timestamp. Provider startup failures may be
// emitted on stderr before a structured stdout stream exists.
func latestAgentOutputContent(outputsDir, agentID string) string {
	base := latestOutputBase(outputsDir, agentID)
	if base == "" {
		return ""
	}

	parts := []string{
		outputContent(base + ".txt"),
		outputContent(base + ".err"),
	}
	return strings.Join(parts, "\n")
}

func latestOutputBase(outputsDir, agentID string) string {
	matches := make([]string, 0, 2)
	for _, ext := range []string{".txt", ".err"} {
		pattern := filepath.Join(outputsDir, agentID+"-*"+ext)
		extMatches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		matches = append(matches, extMatches...)
	}
	if len(matches) == 0 {
		return ""
	}
	sort.Strings(matches)
	latest := matches[len(matches)-1]
	return strings.TrimSuffix(latest, filepath.Ext(latest))
}

// latestOutputContent reads the tail of the most recent agent output file with
// the requested extension. Returns empty string if no file is found or read fails.
func latestOutputContent(outputsDir, agentID, ext string) string {
	pattern := filepath.Join(outputsDir, agentID+"-*"+ext)
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return ""
	}
	// Glob returns sorted by name; timestamp format ensures lexicographic = chronological.
	latest := matches[len(matches)-1]
	return outputContent(latest)
}

func outputContent(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return ""
	}

	size := info.Size()
	readSize := int64(tailReadSize)
	if size < readSize {
		readSize = size
	}
	buf := make([]byte, readSize)
	if _, err := f.ReadAt(buf, size-readSize); err != nil {
		return ""
	}
	return string(buf)
}
