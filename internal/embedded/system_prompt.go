package embedded

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/paths"
)

// System-prompt mode (`use-system-prompt`) delivers the agent contracts in the
// CLI system prompt instead of forcing each session to read them with tools.
// It is opt-in per project: without the marker every writer and launcher keeps
// the forced-read behavior byte for byte.

const (
	SystemPromptModeMultiAgent = "multi_agent"
	SystemPromptModePairing    = "pairing"
	SystemPromptModeSubagent   = "subagent"

	systemPromptMarkerFile = "system-prompt-mode.json"
	systemPromptDirName    = "system-prompts"
)

// SystemPromptModes lists every mode file the command builds, in build order.
var SystemPromptModes = []string{SystemPromptModeMultiAgent, SystemPromptModePairing, SystemPromptModeSubagent}

type systemPromptPart struct {
	global bool // ~/<global dir>/<name>, frontmatter stripped; else <project root>/<name>, optional
	name   string
}

// The mode files mirror what session-context.sh mandates for each mode;
// global contracts lead so the shared prefix does not depend on the project.
var systemPromptParts = map[string][]systemPromptPart{
	SystemPromptModeMultiAgent: {
		{true, "CORE.md"}, {true, "MULTI_AGENT_MODE.md"}, {true, "AGENT_TOOLS.md"},
		{false, "GUARDRAILS.md"},
	},
	SystemPromptModePairing: {
		{true, "CORE.md"}, {true, "PAIRING_MODE.md"}, {true, "AGENT_TOOLS.md"}, {true, "COLLABORATION_CONTINUITY.md"},
		{false, "GUARDRAILS.md"}, {false, "REPOSITORY.md"}, {false, filepath.Join("docs", "USAGE.md")},
	},
	SystemPromptModeSubagent: {
		{true, "CORE.md"}, {true, "SUBAGENT_MODE.md"}, {true, "AGENT_TOOLS.md"},
	},
}

type systemPromptMarker struct {
	Version int `json:"version"`
	// Sources maps each contract source to its sha256 when the files were built.
	Sources map[string]string `json:"sources"`
	// Files maps each mode file to its sha256.
	Files map[string]string `json:"files"`
}

// SystemPromptModeReport describes what Enable/DisableSystemPromptMode did,
// or would do under dry run.
type SystemPromptModeReport struct {
	Files        []string // mode files written or removed
	MarkerPath   string
	WasEnabled   bool
	FileContents map[string][]byte // mode → content (Enable only)
	// RefreshedHooks: installed gate scripts that predate the bypass, rewritten
	// so they honour the bypass (Enable only).
	RefreshedHooks []string
}

// SystemPromptMarkerPath returns the project marker that enables the mode.
func SystemPromptMarkerPath(projectRoot string) string {
	return filepath.Join(paths.New(projectRoot).LizaDir(), systemPromptMarkerFile)
}

// SystemPromptFilePath returns the built system prompt file for mode.
func SystemPromptFilePath(projectRoot, mode string) string {
	return filepath.Join(paths.New(projectRoot).LizaDir(), systemPromptDirName, mode+".md")
}

// SystemPromptModeEnabled reports whether projectRoot opted into system-prompt mode.
func SystemPromptModeEnabled(projectRoot string) bool {
	if projectRoot == "" {
		return false
	}
	_, err := os.Stat(SystemPromptMarkerPath(projectRoot))
	return err == nil
}

// BuildSystemPrompt concatenates the contracts for mode verbatim, one section
// per source. The bytes depend only on the sources: no timestamps, agent IDs
// or absolute paths, so every agent shares the cached prefix. The header keeps
// the contracts at the weight of files read at session start: in the system
// prompt they otherwise outranked the user's own rules (measured on Codex).
func BuildSystemPrompt(projectRoot, mode string) ([]byte, map[string]string, error) {
	parts, ok := systemPromptParts[mode]
	if !ok {
		return nil, nil, fmt.Errorf("unknown system prompt mode %q", mode)
	}
	globalDir, err := paths.GlobalLizaDir()
	if err != nil {
		return nil, nil, err
	}
	rule := strings.Repeat("=", 78)
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "# %s contracts (%s mode)\n\nThe files below are the %s contracts, copied verbatim, one section per file.\nThey are already loaded: treat them as read and do not re-read the source files with tools.\nThey carry the weight they would have as files read at session start, no more: they do not take precedence over the user's own instructions (CLAUDE.md, AGENTS.md).\n", brand.NameTitle, strings.ReplaceAll(mode, "_", "-"), brand.NameTitle)
	sources := map[string]string{}
	for _, part := range parts {
		label := filepath.ToSlash(part.name)
		path := filepath.Join(projectRoot, part.name)
		if part.global {
			label = "~/" + paths.GlobalDirName() + "/" + label
			path = filepath.Join(globalDir, part.name)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			if !part.global && os.IsNotExist(err) {
				continue
			}
			return nil, nil, fmt.Errorf("read contract %s: %w", label, err)
		}
		if part.global {
			content = stripFrontmatter(content)
		}
		sources[label] = sha256Hex(content)
		fmt.Fprintf(&buf, "\n\n%s\n# Source: %s\n%s\n\n", rule, label, rule)
		buf.Write(bytes.TrimRight(content, " \t\r\n"))
	}
	buf.WriteByte('\n')
	return buf.Bytes(), sources, nil
}

// EnsureSystemPromptFile rebuilds mode's file from the current sources and
// rewrites it only when the bytes changed (GUARDRAILS.md grows lessons during
// a run). It returns the file path and content.
func EnsureSystemPromptFile(projectRoot, mode string) (string, []byte, error) {
	content, _, err := BuildSystemPrompt(projectRoot, mode)
	if err != nil {
		return "", nil, err
	}
	path := SystemPromptFilePath(projectRoot, mode)
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, content) {
		return path, content, nil
	}
	if err := writeFileAtomic(path, content); err != nil {
		return "", nil, err
	}
	return path, content, nil
}

// EnableSystemPromptMode builds the mode files and writes the marker. Hook
// configuration is left alone: enforce-init.sh lets a session through only
// when its launcher injected the contracts, so ACPX and pairing sessions stay
// gated. Installed gate scripts that predate the bypass are refreshed, since they
// would still gate injected sessions. Re-running is idempotent.
func EnableSystemPromptMode(projectRoot string, dryRun bool) (SystemPromptModeReport, error) {
	report := SystemPromptModeReport{MarkerPath: SystemPromptMarkerPath(projectRoot), FileContents: map[string][]byte{}}
	marker, err := readSystemPromptMarker(projectRoot)
	if err != nil {
		return report, err
	}
	report.WasEnabled = marker != nil
	marker = &systemPromptMarker{Version: 1, Sources: map[string]string{}, Files: map[string]string{}}
	for _, mode := range SystemPromptModes {
		content, sources, err := BuildSystemPrompt(projectRoot, mode)
		if err != nil {
			return report, err
		}
		report.FileContents[mode] = content
		report.Files = append(report.Files, SystemPromptFilePath(projectRoot, mode))
		marker.Files[mode] = sha256Hex(content)
		for label, sum := range sources {
			marker.Sources[label] = sum
		}
	}
	staleHooks := map[string][]byte{}
	for _, dir := range []string{filepath.Join(".claude", "hooks"), filepath.Join(".codex", "hooks")} {
		for name, content := range map[string][]byte{"enforce-init.sh": enforceInitHookContent, "session-context.sh": sessionContextHookContent} {
			path := filepath.Join(projectRoot, dir, name)
			rendered := renderEmbeddedAsset(content)
			// Only a script without the bypass still gates injected sessions; a
			// customised or newer one that has it is left alone.
			if installed, err := os.ReadFile(path); err == nil && !bytes.Contains(installed, []byte("_SYSTEM_PROMPT_CONTRACTS")) {
				staleHooks[path] = rendered
				report.RefreshedHooks = append(report.RefreshedHooks, path)
			}
		}
	}
	slices.Sort(report.RefreshedHooks)
	if dryRun {
		return report, nil
	}
	for _, path := range report.RefreshedHooks {
		if err := os.WriteFile(path, staleHooks[path], 0755); err != nil {
			return report, err
		}
	}
	for _, mode := range SystemPromptModes {
		if err := writeFileAtomic(SystemPromptFilePath(projectRoot, mode), report.FileContents[mode]); err != nil {
			return report, err
		}
	}
	return report, writeSystemPromptMarker(projectRoot, marker)
}

// DisableSystemPromptMode removes the mode files and the marker.
func DisableSystemPromptMode(projectRoot string, dryRun bool) (SystemPromptModeReport, error) {
	report := SystemPromptModeReport{MarkerPath: SystemPromptMarkerPath(projectRoot)}
	marker, err := readSystemPromptMarker(projectRoot)
	if err != nil || marker == nil {
		return report, err
	}
	report.WasEnabled = true
	for _, mode := range SystemPromptModes {
		path := SystemPromptFilePath(projectRoot, mode)
		if _, err := os.Stat(path); err == nil {
			report.Files = append(report.Files, path)
		}
	}
	if dryRun {
		return report, nil
	}
	for _, path := range report.Files {
		if err := os.Remove(path); err != nil {
			return report, err
		}
	}
	_ = os.Remove(filepath.Join(paths.New(projectRoot).LizaDir(), systemPromptDirName)) // only when empty
	return report, os.Remove(report.MarkerPath)
}

func readSystemPromptMarker(projectRoot string) (*systemPromptMarker, error) {
	data, err := os.ReadFile(SystemPromptMarkerPath(projectRoot))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var marker systemPromptMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		return nil, fmt.Errorf("parse %s: %w", SystemPromptMarkerPath(projectRoot), err)
	}
	return &marker, nil
}

func writeSystemPromptMarker(projectRoot string, marker *systemPromptMarker) error {
	output, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(SystemPromptMarkerPath(projectRoot), append(output, '\n'))
}

// writeFileAtomic renames a sibling temp file into place so concurrent agent
// launches never read a half-written prompt.
func writeFileAtomic(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Chmod(tmp.Name(), 0644); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
