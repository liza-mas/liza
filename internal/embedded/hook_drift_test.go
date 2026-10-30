package embedded

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
)

func managedHookSettings(t *testing.T, asset []byte) map[string]any {
	t.Helper()
	var settings map[string]any
	if err := json.Unmarshal(renderEmbeddedAsset(asset), &settings); err != nil {
		t.Fatalf("embedded hook settings do not parse: %v", err)
	}
	return settings
}

func readHookSettings(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("%s is invalid JSON: %v", path, err)
	}
	return settings
}

func writeHookSettings(t *testing.T, path string, settings map[string]any) {
	t.Helper()
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

// managedHookEntry returns the single entry under event whose matcher equals
// matcher ("" for none) and whose first command ends with script.
func managedHookEntry(t *testing.T, settings map[string]any, event, matcher, script string) map[string]any {
	t.Helper()
	entries, _ := settings["hooks"].(map[string]any)[event].([]any)
	var found map[string]any
	for _, entry := range entries {
		entryMap := entry.(map[string]any)
		entryMatcher, _ := entryMap["matcher"].(string)
		commands := hookEntryCommands(entry)
		if entryMatcher != matcher || len(commands) == 0 || !strings.HasSuffix(strings.TrimSuffix(commands[0], `"`), script) {
			continue
		}
		if found != nil {
			t.Fatalf("more than one %s[%s] %s entry", event, matcher, script)
		}
		found = entryMap
	}
	if found == nil {
		t.Fatalf("no %s[%s] %s entry", event, matcher, script)
	}
	return found
}

func hookTimeout(t *testing.T, settings map[string]any, event, matcher, script string) float64 {
	t.Helper()
	entry := managedHookEntry(t, settings, event, matcher, script)
	return entry["hooks"].([]any)[0].(map[string]any)["timeout"].(float64)
}

func setHookTimeout(t *testing.T, settings map[string]any, event, matcher, script string, timeout float64) {
	t.Helper()
	entry := managedHookEntry(t, settings, event, matcher, script)
	entry["hooks"].([]any)[0].(map[string]any)["timeout"] = timeout
}

func TestWriteClaudeSettings_DriftedManagedHooksPromptRefresh(t *testing.T) {
	managed := managedHookSettings(t, claudeSettingsContent)
	sessionTimeout := hookTimeout(t, managed, "SessionStart", "", "session-context.sh")
	readGuardTimeout := hookTimeout(t, managed, "PreToolUse", "Read", "worktree-path-guard.sh")
	writeGuardTimeout := hookTimeout(t, managed, "PreToolUse", "Write", "worktree-path-guard.sh")
	customEntry := map[string]any{
		"matcher": "Bash",
		"hooks":   []any{map[string]any{"type": "command", "command": "echo custom"}},
	}

	tests := []struct {
		name        string
		input       string
		opts        ConfirmOptions
		wantManaged bool
	}{
		{name: "accepted", input: "y\ny\n", wantManaged: true},
		{name: "declined keeps existing", input: "y\nn\n", wantManaged: false},
		{name: "auto-confirmed", opts: ConfirmOptions{AutoConfirm: true}, wantManaged: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// GIVEN settings matching the managed ones except two drifted timeouts,
			// one on a script shared by several matcher entries, plus a user entry
			projectRoot := t.TempDir()
			settingsPath := filepath.Join(projectRoot, ".claude", "settings.json")
			existing := managedHookSettings(t, claudeSettingsContent)
			setHookTimeout(t, existing, "SessionStart", "", "session-context.sh", sessionTimeout+1)
			setHookTimeout(t, existing, "PreToolUse", "Read", "worktree-path-guard.sh", readGuardTimeout+1)
			preToolUse := existing["hooks"].(map[string]any)["PreToolUse"].([]any)
			existing["hooks"].(map[string]any)["PreToolUse"] = append(preToolUse, customEntry)
			writeHookSettings(t, settingsPath, existing)

			// WHEN
			if err := WriteClaudeSettingsWithOptions(projectRoot, bufio.NewReader(strings.NewReader(tt.input)), tt.opts); err != nil {
				t.Fatalf("WriteClaudeSettingsWithOptions failed: %v", err)
			}

			// THEN
			written := readHookSettings(t, settingsPath)
			wantSession, wantReadGuard := sessionTimeout+1, readGuardTimeout+1
			if tt.wantManaged {
				wantSession, wantReadGuard = sessionTimeout, readGuardTimeout
			}
			if got := hookTimeout(t, written, "SessionStart", "", "session-context.sh"); got != wantSession {
				t.Errorf("SessionStart session-context.sh timeout = %v, want %v", got, wantSession)
			}
			if got := hookTimeout(t, written, "PreToolUse", "Read", "worktree-path-guard.sh"); got != wantReadGuard {
				t.Errorf("PreToolUse[Read] worktree-path-guard.sh timeout = %v, want %v", got, wantReadGuard)
			}
			if got := hookTimeout(t, written, "PreToolUse", "Write", "worktree-path-guard.sh"); got != writeGuardTimeout {
				t.Errorf("PreToolUse[Write] worktree-path-guard.sh timeout = %v, want %v", got, writeGuardTimeout)
			}
			if got := managedHookEntry(t, written, "PreToolUse", "Bash", "echo custom"); !reflect.DeepEqual(got, customEntry) {
				t.Errorf("custom entry = %#v, want %#v", got, customEntry)
			}
		})
	}
}

func TestFindDriftedHookEntries_SummarizesChangedFields(t *testing.T) {
	// GIVEN managed settings and a copy with two drifted timeouts
	managed := managedHookSettings(t, claudeSettingsContent)
	sessionTimeout := hookTimeout(t, managed, "SessionStart", "", "session-context.sh")
	readGuardTimeout := hookTimeout(t, managed, "PreToolUse", "Read", "worktree-path-guard.sh")
	existing := managedHookSettings(t, claudeSettingsContent)
	setHookTimeout(t, existing, "SessionStart", "", "session-context.sh", sessionTimeout+1)
	setHookTimeout(t, existing, "PreToolUse", "Read", "worktree-path-guard.sh", readGuardTimeout+1)

	// WHEN
	drifts := findDriftedHookEntries(managed, existing)

	// THEN
	var got []string
	for _, drift := range drifts {
		got = append(got, drift.summary)
	}
	want := []string{
		fmt.Sprintf("PreToolUse[Read] worktree-path-guard.sh: timeout %g → %g", readGuardTimeout+1, readGuardTimeout),
		fmt.Sprintf("SessionStart session-context.sh: timeout %g → %g", sessionTimeout+1, sessionTimeout),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("drift summaries = %q, want %q", got, want)
	}
}

func TestWriteClaudeSettings_UndriftedManagedHooksDoNotPrompt(t *testing.T) {
	// GIVEN settings identical to the managed ones
	projectRoot := t.TempDir()
	settingsPath := filepath.Join(projectRoot, ".claude", "settings.json")
	writeHookSettings(t, settingsPath, managedHookSettings(t, claudeSettingsContent))

	// WHEN only the merge prompt is answered (a drift prompt would hit EOF)
	err := WriteClaudeSettingsWithOptions(projectRoot, bufio.NewReader(strings.NewReader("y\n")), ConfirmOptions{})

	// THEN
	if err != nil {
		t.Fatalf("WriteClaudeSettingsWithOptions prompted beyond the merge confirmation: %v", err)
	}
}

func TestWriteCodexProjectHooks_DriftedManagedHooksPromptRefresh(t *testing.T) {
	managed := managedHookSettings(t, codexHooksContent)
	sessionTimeout := hookTimeout(t, managed, "SessionStart", "^startup$", "session-context.sh")
	obsoleteCommand := brandedCodexHookCommand("enforce-init.sh", brand.NameTitle, brand.BinaryName)

	tests := []struct {
		name        string
		input       string
		wantTimeout float64
	}{
		{name: "accepted", input: "y\ny\n", wantTimeout: sessionTimeout},
		{name: "declined keeps existing", input: "y\nn\n", wantTimeout: sessionTimeout + 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// GIVEN managed Codex hooks with a drifted timeout and an obsolete
			// generated registration
			projectRoot := t.TempDir()
			hooksPath := filepath.Join(projectRoot, ".codex", "hooks.json")
			existing := managedHookSettings(t, codexHooksContent)
			setHookTimeout(t, existing, "SessionStart", "^startup$", "session-context.sh", sessionTimeout+1)
			preToolUse := existing["hooks"].(map[string]any)["PreToolUse"].([]any)
			existing["hooks"].(map[string]any)["PreToolUse"] = append(preToolUse, map[string]any{
				"hooks": []any{map[string]any{"type": "command", "command": obsoleteCommand}},
			})
			writeHookSettings(t, hooksPath, existing)

			// WHEN
			if err := WriteCodexProjectHooks(projectRoot, bufio.NewReader(strings.NewReader(tt.input))); err != nil {
				t.Fatalf("WriteCodexProjectHooks failed: %v", err)
			}

			// THEN
			written := readHookSettings(t, hooksPath)
			if got := hookTimeout(t, written, "SessionStart", "^startup$", "session-context.sh"); got != tt.wantTimeout {
				t.Errorf("SessionStart session-context.sh timeout = %v, want %v", got, tt.wantTimeout)
			}
			data, _ := os.ReadFile(hooksPath)
			if strings.Contains(string(data), "Missing "+brand.NameTitle+" Codex hook") {
				t.Errorf("obsolete generated registration survived reconciliation:\n%s", data)
			}
		})
	}
}
