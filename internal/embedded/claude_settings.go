package embedded

import (
	"bufio"
	"encoding/json"
	"fmt"
	"maps"
	"os"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/paths"
)

// WriteClaudeSettings writes the embedded claude-settings.json to .claude/settings.json
// and deploys hooks referenced by the settings.
// If the file already exists, prompts the user to merge settings.
// Returns nil on success or if user declines merge.
// The stdin parameter allows for injected input in tests; pass os.Stdin for CLI usage.
func WriteClaudeSettings(projectRoot string, reader *bufio.Reader) error {
	return WriteClaudeSettingsWithOptions(projectRoot, reader, ConfirmOptions{})
}

func WriteClaudeSettingsWithOptions(projectRoot string, reader *bufio.Reader, opts ConfirmOptions) error {
	lizaPaths := paths.New(projectRoot)
	claudeDir := lizaPaths.ClaudeDir()
	settingsPath := lizaPaths.ClaudeSettingsPath()

	var existingSettings map[string]any
	if existingData, err := os.ReadFile(settingsPath); err == nil {
		ok, err := confirmMergeWithOptions(fmt.Sprintf("Should the %s claude settings be merged into the existing settings file? (y/n): ", brand.NameTitle), reader, opts)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}

		if err := json.Unmarshal(existingData, &existingSettings); err != nil {
			return fmt.Errorf("failed to parse existing claude-settings.json: %w", err)
		}
	}

	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		return fmt.Errorf("failed to create .claude directory: %w", err)
	}

	var lizaSettings map[string]any
	if err := json.Unmarshal(renderEmbeddedAsset(claudeSettingsContent), &lizaSettings); err != nil {
		return fmt.Errorf("failed to parse embedded claude-settings.json: %w", err)
	}

	var finalSettings map[string]any
	if existingSettings != nil {
		if mode, ok := existingDefaultMode(existingSettings); ok {
			fmt.Printf("Removing permissions.defaultMode (%q) from %s — project-scope settings do not reliably set the permission mode; set it in your own Claude settings instead.\n", mode, settingsPath)
		}
		finalSettings = mergeSettings(lizaSettings, existingSettings)
	} else {
		finalSettings = lizaSettings
	}

	output, err := json.MarshalIndent(finalSettings, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal claude-settings.json: %w", err)
	}

	if err := os.WriteFile(settingsPath, output, 0644); err != nil {
		return fmt.Errorf("failed to write claude-settings.json: %w", err)
	}

	// Deploy hooks referenced by the settings file.
	if err := WriteHooks(projectRoot); err != nil {
		return fmt.Errorf("failed to write hooks: %w", err)
	}

	return nil
}

// mergeSettings merges liza settings into existing settings.
// Existing settings take precedence (user customizations preserved).
// Special handling for permissions.allow (union) and hooks.PreToolUse (union).
func mergeSettings(liza, existing map[string]any) map[string]any {
	result := make(map[string]any)
	maps.Copy(result, liza)

	// Existing settings override liza defaults (preserve user customizations),
	// except "permissions" and "hooks" which get deep-merged.
	for k, v := range existing {
		switch k {
		case "permissions":
			lizaPerms, lizaOk := liza[k].(map[string]any)
			existingPerms, existingOk := v.(map[string]any)
			if lizaOk && existingOk {
				result[k] = mergePermissions(lizaPerms, existingPerms)
			} else {
				result[k] = v
			}
		case "hooks":
			lizaHooks, lizaOk := liza[k].(map[string]any)
			existingHooks, existingOk := v.(map[string]any)
			if lizaOk && existingOk {
				result[k] = mergeHooks(lizaHooks, existingHooks)
			} else {
				result[k] = v
			}
		case "additionalDirectories":
			result[k] = v
		default:
			result[k] = v
		}
	}
	mergeTopLevelAdditionalDirectories(result, liza["additionalDirectories"])
	mergeTopLevelAdditionalDirectories(result, existing["additionalDirectories"])
	delete(result, "additionalDirectories")

	return result
}

// mergePermissions merges permission objects.
// Existing values override liza defaults, except "allow" which is unioned.
//
// "defaultMode" is dropped unconditionally: project-scope settings are only
// inconsistently honored by Claude Code (some values activate, others are
// ignored) and an entry here shadows the mode the user set in their own
// settings. Agents get their mode from the launch flag instead — see the
// claude entry in internal/providers/embedded.go.
func mergePermissions(liza, existing map[string]any) map[string]any {
	result := make(map[string]any)
	maps.Copy(result, liza)

	for k, v := range existing {
		switch k {
		case "allow", "additionalDirectories":
			lizaAllow, lizaOk := liza[k].([]any)
			existingAllow, existingOk := v.([]any)
			if lizaOk && existingOk {
				result[k] = unionStringArrays(lizaAllow, existingAllow)
			} else if existingOk {
				result[k] = v
			}
		default:
			result[k] = v
		}
	}
	delete(result, "defaultMode")

	return result
}

// existingDefaultMode reports the permissions.defaultMode value already present
// in a settings file, so the caller can tell the user it is being removed.
func existingDefaultMode(settings map[string]any) (string, bool) {
	perms, ok := settings["permissions"].(map[string]any)
	if !ok {
		return "", false
	}
	mode, ok := perms["defaultMode"].(string)
	return mode, ok
}

func mergeTopLevelAdditionalDirectories(result map[string]any, value any) {
	dirs, ok := value.([]any)
	if !ok {
		return
	}

	perms, ok := result["permissions"].(map[string]any)
	if !ok {
		perms = make(map[string]any)
		result["permissions"] = perms
	}

	existingDirs, _ := perms["additionalDirectories"].([]any)
	perms["additionalDirectories"] = unionStringArrays(existingDirs, dirs)
}

// mergeHooks merges hook configurations.
// For each hook event (e.g. "PreToolUse"), the entry arrays are concatenated.
// Duplicate entries (same command string) are deduplicated.
func mergeHooks(liza, existing map[string]any) map[string]any {
	result := make(map[string]any)
	maps.Copy(result, liza)
	maps.Copy(result, existing)

	// For events present in both, concatenate and deduplicate entry arrays.
	for event, lizaVal := range liza {
		existingVal, ok := existing[event]
		if !ok {
			continue
		}
		lizaEntries, lizaOk := lizaVal.([]any)
		existingEntries, existingOk := existingVal.([]any)
		if !lizaOk || !existingOk {
			continue
		}
		result[event] = unionHookEntries(lizaEntries, existingEntries)
	}

	return result
}

func hookEntryCommands(entry any) []string {
	entryMap, ok := entry.(map[string]any)
	if !ok {
		return nil
	}
	if cmd, ok := entryMap["command"].(string); ok {
		return []string{cmd}
	}
	hooks, ok := entryMap["hooks"].([]any)
	if !ok {
		return nil
	}
	var commands []string
	for _, hook := range hooks {
		hookMap, ok := hook.(map[string]any)
		if !ok {
			continue
		}
		if cmd, ok := hookMap["command"].(string); ok {
			commands = append(commands, cmd)
		}
	}
	return commands
}

// unionHookEntries preserves every existing entry verbatim. Existing commands
// take precedence over generated entries on exact collision.
func unionHookEntries(a, b []any) []any {
	existingCmds := make(map[string]bool)
	for _, entry := range b {
		for _, cmd := range hookEntryCommands(entry) {
			existingCmds[cmd] = true
		}
	}

	// Add liza entries whose commands don't collide with existing.
	var result []any
	for _, entry := range a {
		cmds := hookEntryCommands(entry)
		collides := false
		for _, cmd := range cmds {
			if existingCmds[cmd] {
				collides = true
				break
			}
		}
		if !collides {
			result = append(result, entry)
		}
	}

	// Add all existing entries (they win on collision).
	result = append(result, b...)

	return result
}

// unionStringArrays returns the union of two string arrays (deduplicated).
func unionStringArrays(a, b []any) []any {
	seen := make(map[string]bool)
	result := []any{}

	for _, item := range a {
		str, ok := item.(string)
		if !ok {
			continue
		}
		if !seen[str] {
			seen[str] = true
			result = append(result, item)
		}
	}

	for _, item := range b {
		str, ok := item.(string)
		if !ok {
			continue
		}
		if !seen[str] {
			seen[str] = true
			result = append(result, item)
		}
	}

	return result
}
