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
	Message  string // the matching line from output
}

// DetectQuotaExhaustion scans agent output for quota-exhaustion patterns.
// Returns non-nil if a known pattern is found.
func DetectQuotaExhaustion(output, cliName string) *QuotaExhaustion {
	provider := canonicalQuotaProvider(cliName)
	for _, line := range strings.Split(output, "\n") {
		for _, p := range quotaPatterns {
			if p.Provider != provider {
				continue
			}
			if p.Pattern.MatchString(line) {
				return &QuotaExhaustion{
					Provider: p.Provider,
					Message:  line,
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
// this provider to terminate gracefully.
func WriteQuotaSignal(projectRoot, provider, message string) error {
	provider = canonicalQuotaProvider(provider)
	signalPath := QuotaSignalPath(projectRoot, provider)
	content := fmt.Sprintf("provider: %s\ndetected: %s\nmessage: %s\n",
		provider,
		time.Now().UTC().Format(time.RFC3339),
		message,
	)
	return os.WriteFile(signalPath, []byte(content), 0644)
}

// CheckQuotaSignal returns true if a quota signal file exists for the provider.
func CheckQuotaSignal(projectRoot, provider string) bool {
	_, err := os.Stat(QuotaSignalPath(projectRoot, provider))
	return err == nil
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
	return LogAlert(projectRoot, "🚨", "PROVIDER QUOTA EXHAUSTED", qe.Provider+": "+qe.Message)
}

// RaiseQuotaExhaustion records both human-visible and process-visible quota state.
func RaiseQuotaExhaustion(projectRoot string, qe *QuotaExhaustion) error {
	return errors.Join(
		LogQuotaAlert(projectRoot, qe),
		WriteQuotaSignal(projectRoot, qe.Provider, qe.Message),
	)
}

// LogQuotaSpawnBlockedAlert appends an alert when a stale quota signal blocks spawn.
func LogQuotaSpawnBlockedAlert(projectRoot, provider, role string) error {
	message := fmt.Sprintf("%s: refused to spawn %s while quota signal is set; delete the flag file or run liza pause then liza resume before spawning again", provider, role)
	return LogAlert(projectRoot, "🚨", "PROVIDER QUOTA SPAWN BLOCKED", message)
}

// ClearQuotaSignal removes the quota signal file for a provider.
// Intended for use by `liza resume` or manual recovery.
func ClearQuotaSignal(projectRoot, provider string) error {
	err := os.Remove(QuotaSignalPath(projectRoot, provider))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func canonicalQuotaProvider(cliName string) string {
	return acpxAgentName(cliName)
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
