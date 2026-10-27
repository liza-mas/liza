package providersignal

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/agentlaunch"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/paths"
)

type providerUnavailablePattern struct {
	Provider   string
	Needles    []string
	Diagnostic string
}

const codexSessionAccessDiagnostic = "thread/start failed: error creating thread: Codex cannot access session files under .codex/sessions (permission denied)"

var providerUnavailablePatterns = []providerUnavailablePattern{
	{
		Provider:   "codex",
		Needles:    []string{"Codex cannot access session files", ".codex/sessions", "permission denied"},
		Diagnostic: codexSessionAccessDiagnostic,
	},
	{
		Provider:   "codex",
		Needles:    []string{"thread/start failed", "error creating thread", ".codex/sessions"},
		Diagnostic: "thread/start failed: error creating thread using .codex/sessions",
	},
	{
		// An expired login fails every session until a human logs in again.
		Provider:   "claude",
		Needles:    []string{"Failed to authenticate", "OAuth session expired"},
		Diagnostic: "Failed to authenticate: OAuth session expired and could not be refreshed",
	},
}

// ProviderUnavailable holds details about a provider startup/readiness failure.
// Provider is the classifier's diagnostic label and the value operators see;
// Tool is the launch tool that failed, the identity policy compares. They differ
// when one diagnostic serves several tools (codex and codex-acp are both
// "codex").
type ProviderUnavailable struct {
	Provider string
	Tool     string
	Message  string
}

// DetectProviderUnavailable scans agent output for provider startup failures
// that make the provider unable to create a usable agent session.
func DetectProviderUnavailable(output, cliName string) *ProviderUnavailable {
	providerUnavailable, _ := ClassifyProviderUnavailable(output, cliName)
	return providerUnavailable
}

// ClassifyProviderUnavailable is DetectProviderUnavailable that also returns the
// bounded diagnostic of the matching pattern, the only evidence a caller may
// persist. Patterns are tried in registry order, each against every
// diagnostic line.
func ClassifyProviderUnavailable(output, cliName string) (*ProviderUnavailable, string) {
	provider := agentlaunch.ACPXAgentName(cliName)
	for _, p := range providerUnavailablePatterns {
		if p.Provider != provider {
			continue
		}
		for _, line := range DiagnosticLines(output) {
			matched := true
			for _, needle := range p.Needles {
				if !strings.Contains(line, needle) {
					matched = false
					break
				}
			}
			if matched {
				return &ProviderUnavailable{
					Provider: p.Provider,
					Tool:     cliName,
					Message:  p.Diagnostic,
				}, p.Diagnostic
			}
		}
	}
	return nil, ""
}

const providerUnavailableSignalPrefix = "provider-unavailable-"

// ProviderUnavailableSignalPath returns the signal file path for tool. The file
// name encodes the tool as launched, so tools whose provider keys differ never
// share a record; whether two tools stop each other is decided when a signal
// is checked, from their provider keys.
func ProviderUnavailableSignalPath(projectRoot, tool string) string {
	return filepath.Join(paths.New(projectRoot).LizaDir(), providerUnavailableSignalPrefix+encodeSignalName(tool))
}

func providerUnavailableSignalGlob(projectRoot string) string {
	return filepath.Join(paths.New(projectRoot).LizaDir(), providerUnavailableSignalPrefix+"*")
}

// signalNameEscape introduces a percent-encoded byte in a signal file name.
const signalNameEscape = '%'

// encodeSignalName maps a configured tool name to a file-name component: the
// ASCII letters, digits, '-', '_' and non-leading '.' stay, every other byte is
// percent-encoded. The mapping is injective and cannot produce a separator or a
// dot-segment, so a tool name never addresses a file outside the signal directory.
func encodeSignalName(tool string) string {
	var b strings.Builder
	for i := 0; i < len(tool); i++ {
		c := tool[i]
		plain := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || (c == '.' && i > 0)
		if plain {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%c%02X", signalNameEscape, c)
	}
	return b.String()
}

// decodeSignalName inverts encodeSignalName; a name it did not produce (an
// older signal file) decodes to itself where it holds no valid escape.
func decodeSignalName(name string) string {
	var b strings.Builder
	for i := 0; i < len(name); i++ {
		if name[i] == signalNameEscape && i+2 < len(name) && isHex(name[i+1]) && isHex(name[i+2]) {
			b.WriteByte(hexValue(name[i+1])<<4 | hexValue(name[i+2]))
			i += 2
			continue
		}
		b.WriteByte(name[i])
	}
	return b.String()
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'A' && c <= 'F'
}

func hexValue(c byte) byte {
	if c <= '9' {
		return c - '0'
	}
	return c - 'A' + 10
}

// UnavailableSignal is one enumerated provider-unavailable record.
type UnavailableSignal struct {
	// Path is the physical handle: clearing removes exactly this file.
	Path string
	// Provider is the display value operators see.
	Provider string
	// Tool is the identity policy compares: the launch tool a new record
	// carries, or the file's own name for a record written before tools were
	// recorded.
	Tool string
}

// ListProviderUnavailableSignals enumerates the signal records in file-name order.
func ListProviderUnavailableSignals(projectRoot string) []UnavailableSignal {
	matches, err := filepath.Glob(providerUnavailableSignalGlob(projectRoot))
	if err != nil {
		return nil
	}
	signals := make([]UnavailableSignal, 0, len(matches))
	for _, path := range matches {
		signals = append(signals, readUnavailableSignal(path))
	}
	return signals
}

// readUnavailableSignal reads a record, falling back to its file name for any
// field it does not carry or cannot read, so an unreadable file still blocks.
func readUnavailableSignal(path string) UnavailableSignal {
	name := decodeSignalName(filepath.Base(path)[len(providerUnavailableSignalPrefix):])
	signal := UnavailableSignal{Path: path, Provider: name, Tool: name}
	content, err := os.ReadFile(path)
	if err != nil {
		return signal
	}
	fields := parseRecord(string(content))
	if tool := fields[recordKeyTool]; tool != "" {
		signal.Tool = tool
		if provider := fields[recordKeyProvider]; provider != "" {
			signal.Provider = provider
		}
	}
	return signal
}

// WriteProviderUnavailableSignal creates the signal record that stops every
// supervisor whose provider key matches pu.Tool until the provider environment
// is repaired. A record without a tool is attributed to its provider label, and
// a tool equal to the label is not repeated: that record reads as it always did.
func WriteProviderUnavailableSignal(projectRoot string, pu *ProviderUnavailable) error {
	tool := pu.Tool
	if tool == "" {
		tool = pu.Provider
	}
	content := recordLine(recordKeyProvider, pu.Provider)
	if tool != pu.Provider {
		content += recordLine(recordKeyTool, tool)
	}
	content += recordLine(recordKeyDetected, time.Now().UTC().Format(time.RFC3339)) +
		recordLine(recordKeyMessage, pu.Message)
	return os.WriteFile(ProviderUnavailableSignalPath(projectRoot, tool), []byte(content), signalFileMode)
}

// CheckProviderUnavailableSignal reports whether a signal record names a tool
// sharing tool's provider key. keys is called at most once, and only when a
// record exists, so a caller with no signal pays no catalog or state read.
func CheckProviderUnavailableSignal(projectRoot, tool string, keys func() agentlaunch.ProviderKeys) bool {
	signals := ListProviderUnavailableSignals(projectRoot)
	if len(signals) == 0 {
		return false
	}
	provider := keys()
	for _, signal := range signals {
		if provider.Same(signal.Tool, tool) {
			return true
		}
	}
	return false
}

// ClearProviderUnavailableSignal removes the record at path, a handle
// ListProviderUnavailableSignals returned.
func ClearProviderUnavailableSignal(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove provider-unavailable signal %q: %w", path, err)
	}
	return nil
}

// UnavailableClearResult is the outcome of clearing every record of one
// displayed provider; Err joins the failures of its records.
type UnavailableClearResult struct {
	Provider string
	Err      error
}

// ClearProviderUnavailableSignals removes every record and reports one result
// per displayed provider, in the order its first record is enumerated: records
// that display the same provider are one row, as they were when they shared a file.
func ClearProviderUnavailableSignals(projectRoot string) []UnavailableClearResult {
	var results []UnavailableClearResult
	row := map[string]int{}
	for _, signal := range ListProviderUnavailableSignals(projectRoot) {
		i, seen := row[signal.Provider]
		if !seen {
			i = len(results)
			row[signal.Provider] = i
			results = append(results, UnavailableClearResult{Provider: signal.Provider})
		}
		results[i].Err = errors.Join(results[i].Err, ClearProviderUnavailableSignal(signal.Path))
	}
	return results
}

// LogProviderUnavailableAlert appends a provider-unavailable alert to alerts.log.
func LogProviderUnavailableAlert(projectRoot string, pu *ProviderUnavailable) error {
	return LogAlert(projectRoot, alertLevel, alertCategoryUnavailable, pu.Provider+": "+pu.Message)
}

// LogProviderUnavailableSpawnBlockedAlert appends an alert when a provider-unavailable signal blocks spawn.
func LogProviderUnavailableSpawnBlockedAlert(projectRoot, provider, role string) error {
	message := fmt.Sprintf("%s: refused to spawn %s while provider-unavailable signal is set; repair the provider, then delete the flag file or run %s then %s before spawning again", provider, role, brand.Command("pause"), brand.Command("resume"))
	return LogAlert(projectRoot, alertLevel, alertCategoryUnavailableSpawnBlocked, message)
}
