package providersignal

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/agentlaunch"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
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

// signalNameEscape introduces a percent-encoded byte in a signal file name.
// Names are never decoded: a record whose file name differs from its tool
// carries the tool in its own tool line, because older records may hold a
// literal '%'.
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
	// order is the file name earlier versions gave this record, so rows keep
	// the order they were listed in whatever the physical name is.
	order string
}

// ListProviderUnavailableSignals enumerates the signal records in the order
// earlier versions listed them. An unlistable signal directory yields none.
func ListProviderUnavailableSignals(projectRoot string) []UnavailableSignal {
	signals, _ := listUnavailableSignals(projectRoot)
	return signals
}

func listUnavailableSignals(projectRoot string) ([]UnavailableSignal, error) {
	dir := paths.New(projectRoot).LizaDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var signals []UnavailableSignal
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), providerUnavailableSignalPrefix) {
			signals = append(signals, readUnavailableSignal(filepath.Join(dir, entry.Name())))
		}
	}
	sort.SliceStable(signals, func(i, j int) bool { return signals[i].order < signals[j].order })
	return signals, nil
}

// readUnavailableSignal reads a record, falling back to its file name for any
// field it does not carry or cannot read, so an unreadable file still blocks.
func readUnavailableSignal(path string) UnavailableSignal {
	name := filepath.Base(path)[len(providerUnavailableSignalPrefix):]
	signal := UnavailableSignal{Path: path, Provider: name, Tool: name, order: name}
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
		signal.order = agentlaunch.ACPXAgentName(signal.Provider)
	}
	return signal
}

// WriteProviderUnavailableSignal creates the signal record that stops every
// supervisor whose provider key matches pu.Tool until the provider environment
// is repaired. A record without a tool is attributed to its provider label, and
// a plain tool equal to the label is not repeated: that record reads as it always did.
func WriteProviderUnavailableSignal(projectRoot string, pu *ProviderUnavailable) error {
	tool := pu.Tool
	if tool == "" {
		tool = pu.Provider
	}
	content := recordLine(recordKeyProvider, pu.Provider)
	if tool != pu.Provider || encodeSignalName(tool) != tool {
		content += recordLine(recordKeyTool, tool)
	}
	content += recordLine(recordKeyDetected, time.Now().UTC().Format(time.RFC3339)) +
		recordLine(recordKeyMessage, pu.Message)
	return os.WriteFile(ProviderUnavailableSignalPath(projectRoot, tool), []byte(content), signalFileMode)
}

// MatchKeys is the provider-key view a check decides under: the C2 keys plus the
// names of the tools of the registry they were derived from, which locate the
// records of sibling tools when the signal directory cannot be listed. It is
// built from the same catalog-derived base and configuration, derives nothing
// beyond them and is never stored.
type MatchKeys struct {
	agentlaunch.ProviderKeys
	tools []string
}

// NewMatchKeys derives the view for base (as BuiltInAgentTools returns it)
// under config.
func NewMatchKeys(base map[string]models.AgentToolConfig, config models.Config) MatchKeys {
	seen := map[string]bool{}
	var tools []string
	add := func(name string) {
		if name = strings.TrimSpace(name); name != "" && !seen[name] {
			seen[name] = true
			tools = append(tools, name)
		}
	}
	for name := range base {
		add(name)
	}
	for name := range config.AgentTools {
		add(name)
	}
	sort.Strings(tools)
	return MatchKeys{ProviderKeys: agentlaunch.NewProviderKeys(base, config), tools: tools}
}

// CheckProviderUnavailableSignal reports whether a signal record names a tool
// sharing tool's provider key. keys is called at most once, and only when a
// record exists or the signal directory cannot be listed, so a caller with no
// signal pays no catalog or state read. When the directory cannot be listed, the records visible by name are
// compared instead (see knownUnavailableSignals), so a record that can still be
// stat'ed keeps stopping the tools a listing would stop it for.
func CheckProviderUnavailableSignal(projectRoot, tool string, keys func() MatchKeys) bool {
	signals, err := listUnavailableSignals(projectRoot)
	if os.IsNotExist(err) || (err == nil && len(signals) == 0) {
		return false
	}
	view := keys()
	if err != nil {
		// The candidates come from the registry, so the view is needed first.
		signals = knownUnavailableSignals(projectRoot, tool, view)
	}
	for _, signal := range signals {
		if view.Same(signal.Tool, tool) {
			return true
		}
	}
	return false
}

// knownUnavailableSignals returns the records visible by name when the signal
// directory cannot be listed. Candidates are the tools of the registry that
// share tool's provider key, since a sibling may have raised the record, plus
// tool and its alias-normalised name, which earlier versions stat'ed; each
// is probed under its literal legacy file name and its encoded one. Identity is
// read as for a listing, so the caller compares them through the same keys.
func knownUnavailableSignals(projectRoot, tool string, view MatchKeys) []UnavailableSignal {
	dir := paths.New(projectRoot).LizaDir()
	candidates := []string{tool, agentlaunch.ACPXAgentName(tool)}
	for _, name := range view.tools {
		if view.Same(name, tool) {
			candidates = append(candidates, name)
		}
	}
	var signals []UnavailableSignal
	seen := map[string]bool{}
	for _, candidate := range candidates {
		for _, name := range []string{candidate, encodeSignalName(candidate)} {
			if strings.ContainsAny(name, "/\\\x00") || seen[name] {
				continue
			}
			seen[name] = true
			path := filepath.Join(dir, providerUnavailableSignalPrefix+name)
			if _, err := os.Stat(path); err == nil {
				signals = append(signals, readUnavailableSignal(path))
			}
		}
	}
	return signals
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
