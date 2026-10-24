package embedded

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
)

var codexHookContents = map[string][]byte{
	"enforce-init.sh":        enforceInitHookContent,
	"session-context.sh":     sessionContextHookContent,
	"git-guard.sh":           gitGuardHookContent,
	"worktree-path-guard.sh": worktreePathGuardHookContent,
}

// WriteCodexProjectHooks writes repo-local Codex hook configuration and hook
// scripts to .codex/. Codex also requires the hooks feature flag in an
// active config layer, so this manages the project-local config.toml feature.
func WriteCodexProjectHooks(projectRoot string, reader *bufio.Reader) error {
	return WriteCodexProjectHooksWithOptions(projectRoot, reader, ConfirmOptions{})
}

func WriteCodexProjectHooksWithOptions(projectRoot string, reader *bufio.Reader, opts ConfirmOptions) error {
	if reader == nil {
		reader = bufio.NewReader(os.Stdin)
	}
	codexDir := filepath.Join(projectRoot, ".codex")
	if err := ensureCodexDir(codexDir); err != nil {
		return fmt.Errorf("failed to create .codex directory: %w", err)
	}

	configPath := filepath.Join(codexDir, "config.toml")
	install, configContent, err := prepareCodexHooksFeature(configPath, reader, opts)
	if err != nil {
		return err
	}
	if !install {
		return nil
	}
	hooksOutput, installed, err := renderCodexHooksJSON(filepath.Join(codexDir, "hooks.json"), reader, opts)
	if err != nil {
		return err
	}
	if !installed {
		return nil
	}
	if err := WriteCodexHooks(projectRoot); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(codexDir, "hooks.json"), hooksOutput, 0644); err != nil {
		return fmt.Errorf("failed to write codex hooks.json: %w", err)
	}
	if configContent != "" {
		if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
			return fmt.Errorf("failed to write codex project config: %w", err)
		}
	}
	return nil
}

func prepareCodexHooksFeature(configPath string, reader *bufio.Reader, opts ConfirmOptions) (bool, string, error) {
	existing, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return true, "[features]\nhooks = true\n", nil
	}
	if err != nil {
		return false, "", fmt.Errorf("failed to read codex project config: %w", err)
	}

	merged, changed := mergeCodexHooksFeature(string(existing))
	if !changed {
		return true, "", nil
	}

	ok, err := confirmMergeWithOptions(fmt.Sprintf("Should %s enable Codex hooks in .codex/config.toml? (y/n): ", brand.NameTitle), reader, opts)
	if err != nil {
		return false, "", err
	}
	if !ok {
		return false, "", nil
	}
	return true, merged, nil
}

func mergeCodexHooksFeature(content string) (string, bool) {
	lines := strings.Split(content, "\n")
	sectionStart, sectionEnd := findTomlSection(lines, "features")
	if sectionStart == -1 {
		return appendTomlBlock(content, "[features]\nhooks = true\n"), true
	}

	featureLineStart, featureLineEnd := findTomlAssignment(lines, sectionStart+1, sectionEnd, "hooks")
	if featureLineStart == -1 {
		updated := insertLines(lines, sectionEnd, []string{"hooks = true"})
		return ensureTrailingNewline(strings.Join(updated, "\n")), true
	}

	assignment := strings.TrimSpace(stripTomlLineComment(strings.Join(lines[featureLineStart:featureLineEnd+1], "\n")))
	if assignment == "hooks = true" {
		return content, false
	}

	lines[featureLineStart] = "hooks = true"
	if featureLineEnd > featureLineStart {
		lines = append(lines[:featureLineStart+1], lines[featureLineEnd+1:]...)
	}
	return ensureTrailingNewline(strings.Join(lines, "\n")), true
}

func renderCodexHooksJSON(hooksPath string, reader *bufio.Reader, opts ConfirmOptions) ([]byte, bool, error) {
	var lizaHooks map[string]any
	if err := json.Unmarshal(renderEmbeddedAsset(codexHooksContent), &lizaHooks); err != nil {
		return nil, false, fmt.Errorf("failed to parse embedded codex-hooks.json: %w", err)
	}

	finalHooks := lizaHooks
	if existingData, err := os.ReadFile(hooksPath); err == nil {
		ok, err := confirmMergeWithOptions(fmt.Sprintf("Should the %s Codex hooks be merged into .codex/hooks.json? (y/n): ", brand.NameTitle), reader, opts)
		if err != nil {
			return nil, false, err
		}
		if !ok {
			return nil, false, nil
		}

		var existingHooks map[string]any
		if err := json.Unmarshal(existingData, &existingHooks); err != nil {
			return nil, false, fmt.Errorf("failed to parse existing codex hooks.json: %w", err)
		}
		reconcileCodexHookRegistrations(lizaHooks, existingHooks)
		finalHooks = mergeSettings(lizaHooks, existingHooks)
	} else if err != nil && !os.IsNotExist(err) {
		return nil, false, fmt.Errorf("failed to read codex hooks.json: %w", err)
	}
	output, err := json.MarshalIndent(finalHooks, "", "  ")
	if err != nil {
		return nil, false, fmt.Errorf("failed to marshal codex hooks.json: %w", err)
	}
	return append(output, '\n'), true, nil
}

func generatedCodexHookCommand(script string) string {
	return fmt.Sprintf(
		`root=$(git rev-parse --show-toplevel 2>/dev/null) || exit 0; bash "$root/.codex/hooks/%s"`,
		script,
	)
}

func brandedCodexHookCommand(script, nameTitle, binaryName string) string {
	return fmt.Sprintf(
		`root=$(git rev-parse --show-toplevel 2>/dev/null) || exit 0; hook="$root/.codex/hooks/%s"; [ -x "$hook" ] || { echo "Missing %s Codex hook: $hook. Run '%s init --codex' to repair project hooks, or remove .codex/hooks.json to disable them." >&2; exit 1; }; bash "$hook"`,
		script,
		nameTitle,
		binaryName,
	)
}

func currentGeneratedCodexHookID(command string) string {
	for script := range codexHookContents {
		if command == generatedCodexHookCommand(script) {
			return script
		}
	}
	return ""
}

func obsoleteGeneratedCodexHookID(command string) string {
	values := brand.RuntimeValues()
	for script := range codexHookContents {
		knownObsolete := []string{
			fmt.Sprintf(`bash "$(git rev-parse --show-toplevel)/.codex/hooks/%s"`, script),
			brandedCodexHookCommand(script, values.NameTitle, values.BinaryName),
		}
		if values.NameLower != values.BinaryName {
			knownObsolete = append(knownObsolete, brandedCodexHookCommand(script, values.NameTitle, values.NameLower))
		}
		if values.NameTitle != "Liza" || values.BinaryName != "liza" {
			knownObsolete = append(knownObsolete, brandedCodexHookCommand(script, "Liza", "liza"))
		}
		for _, known := range knownObsolete {
			if command == known {
				return script
			}
		}
	}
	return ""
}

// reconcileCodexHookRegistrations removes only exact, known obsolete generated
// registrations when the current Codex hook for the same event and script exists.
func reconcileCodexHookRegistrations(current, existing map[string]any) {
	currentHooks, currentOK := current["hooks"].(map[string]any)
	existingHooks, existingOK := existing["hooks"].(map[string]any)
	if !currentOK || !existingOK {
		return
	}

	for event, existingValue := range existingHooks {
		existingEntries, existingOK := existingValue.([]any)
		currentEntries, currentOK := currentHooks[event].([]any)
		if !existingOK || !currentOK {
			continue
		}

		currentIDs := make(map[string]bool)
		for _, entry := range currentEntries {
			commands := hookEntryCommands(entry)
			if len(commands) == 1 {
				if id := currentGeneratedCodexHookID(commands[0]); id != "" {
					currentIDs[id] = true
				}
			}
		}

		retained := make([]any, 0, len(existingEntries))
		for _, entry := range existingEntries {
			commands := hookEntryCommands(entry)
			if len(commands) == 1 {
				if id := obsoleteGeneratedCodexHookID(commands[0]); id != "" && currentIDs[id] {
					continue
				}
			}
			retained = append(retained, entry)
		}
		existingHooks[event] = retained
	}
}

// WriteCodexHooks writes embedded hook scripts to .codex/hooks/ in the project
// root. Always overwrites — hooks are Liza infrastructure, not user-customizable.
func WriteCodexHooks(projectRoot string) error {
	hooksDir := filepath.Join(projectRoot, ".codex", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		return fmt.Errorf("failed to create .codex/hooks directory: %w", err)
	}

	for name, content := range codexHookContents {
		hookPath := filepath.Join(hooksDir, name)
		if err := os.WriteFile(hookPath, renderEmbeddedAsset(content), 0755); err != nil {
			return fmt.Errorf("failed to write %s: %w", name, err)
		}
	}

	return nil
}
