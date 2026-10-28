package embedded

import (
	"bufio"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/paths"
)

// setupSystemPromptProject installs the embedded contracts into a temp HOME
// and returns a project root holding a GUARDRAILS.md.
func setupSystemPromptProject(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	globalDir, err := paths.GlobalLizaDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := WriteGlobalFiles(globalDir, nil); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "GUARDRAILS.md"), []byte("# guardrails\n\nproject rule\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return root
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestBuildSystemPromptIsVerbatimAndDeterministic(t *testing.T) {
	root := setupSystemPromptProject(t)
	first, sources, err := BuildSystemPrompt(root, SystemPromptModeMultiAgent)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := BuildSystemPrompt(root, SystemPromptModeMultiAgent)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("system prompt bytes differ between builds")
	}
	for _, name := range []string{"CORE.md", "MULTI_AGENT_MODE.md", "AGENT_TOOLS.md"} {
		source, err := contractsFS.ReadFile("contracts/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(first), strings.TrimRight(string(source), " \t\r\n")) {
			t.Errorf("multi_agent prompt does not contain %s verbatim", name)
		}
	}
	if !strings.Contains(string(first), "do not take precedence over the user's own instructions") {
		t.Error("header lost the contract-weight sentence")
	}
	if !strings.Contains(string(first), "project rule") || len(sources) != 4 {
		t.Errorf("GUARDRAILS.md missing or wrong source count %d", len(sources))
	}
	for _, unwanted := range []string{brand.MetadataKey("build_date"), root, os.Getenv("HOME"), "Source: ~/" + paths.GlobalDirName() + "/PAIRING_MODE.md"} {
		if strings.Contains(string(first), unwanted) {
			t.Errorf("multi_agent prompt contains variable or foreign content %q", unwanted)
		}
	}

	if err := os.Remove(filepath.Join(root, "GUARDRAILS.md")); err != nil {
		t.Fatal(err)
	}
	if _, sources, err = BuildSystemPrompt(root, SystemPromptModeMultiAgent); err != nil || len(sources) != 3 {
		t.Fatalf("absent GUARDRAILS.md must be skipped: sources=%d err=%v", len(sources), err)
	}
}

func TestBuildSystemPromptFailsWithoutInstalledContracts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, _, err := BuildSystemPrompt(t.TempDir(), SystemPromptModeMultiAgent); err == nil {
		t.Fatal("expected an error when CORE.md is not installed")
	}
}

func TestSystemPromptModeRoundTrip(t *testing.T) {
	root := setupSystemPromptProject(t)
	if err := WriteClaudeSettings(root, nil); err != nil {
		t.Fatal(err)
	}
	if err := WriteCodexProjectHooks(root, bufio.NewReader(strings.NewReader(""))); err != nil {
		t.Fatal(err)
	}
	claudePath := filepath.Join(root, ".claude", "settings.json")
	codexPath := filepath.Join(root, ".codex", "hooks.json")
	claudeBefore, codexBefore := readFile(t, claudePath), readFile(t, codexPath)
	hooksUntouched := func(step string) {
		t.Helper()
		if readFile(t, claudePath) != claudeBefore || readFile(t, codexPath) != codexBefore {
			t.Fatalf("%s changed the hook files; the enforce-init gate must stay installed", step)
		}
	}

	if SystemPromptModeEnabled(root) {
		t.Fatal("mode must be off by default")
	}
	if _, err := EnableSystemPromptMode(root, true); err != nil {
		t.Fatal(err)
	}
	if SystemPromptModeEnabled(root) {
		t.Fatal("dry run wrote the marker")
	}

	report, err := EnableSystemPromptMode(root, false)
	if err != nil || report.WasEnabled || !SystemPromptModeEnabled(root) {
		t.Fatalf("enable: %+v err=%v", report, err)
	}
	hooksUntouched("enable")
	for _, mode := range SystemPromptModes {
		if _, err := os.Stat(SystemPromptFilePath(root, mode)); err != nil {
			t.Fatalf("mode file %s missing: %v", mode, err)
		}
	}
	marker := readFile(t, SystemPromptMarkerPath(root))

	again, err := EnableSystemPromptMode(root, false)
	if err != nil || !again.WasEnabled || readFile(t, SystemPromptMarkerPath(root)) != marker {
		t.Fatalf("rerun not idempotent: %+v err=%v", again, err)
	}

	if err := WriteClaudeSettingsWithOptions(root, nil, ConfirmOptions{AutoConfirm: true}); err != nil {
		t.Fatal(err)
	}
	if err := WriteCodexProjectHooksWithOptions(root, nil, ConfirmOptions{AutoConfirm: true}); err != nil {
		t.Fatal(err)
	}
	hooksUntouched("re-install")

	reverted, err := DisableSystemPromptMode(root, false)
	if err != nil || len(reverted.Files) != len(SystemPromptModes) {
		t.Fatalf("revert: %+v err=%v", reverted, err)
	}
	hooksUntouched("revert")
	if SystemPromptModeEnabled(root) {
		t.Fatal("marker survived revert")
	}
	if _, err := os.Stat(filepath.Join(paths.New(root).LizaDir(), systemPromptDirName)); !os.IsNotExist(err) {
		t.Fatalf("system-prompts dir survived revert: %v", err)
	}
	if again, err := DisableSystemPromptMode(root, false); err != nil || again.WasEnabled {
		t.Fatalf("second revert: %+v err=%v", again, err)
	}
}

func TestEnableSystemPromptModeRefreshesStaleGateHooks(t *testing.T) {
	root := setupSystemPromptProject(t)
	if err := WriteHooks(root); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(root, ".claude", "hooks", "enforce-init.sh")
	guard := filepath.Join(root, ".claude", "hooks", "git-guard.sh")
	if err := os.WriteFile(stale, []byte("#!/bin/bash\nexit 2\n"), 0755); err != nil { // a pre-bypass install
		t.Fatal(err)
	}
	guardBefore := readFile(t, guard)
	dry, err := EnableSystemPromptMode(root, true)
	if err != nil || !slices.Equal(dry.RefreshedHooks, []string{stale}) || readFile(t, stale) != "#!/bin/bash\nexit 2\n" {
		t.Fatalf("dry run: refreshed %v err=%v", dry.RefreshedHooks, err)
	}
	report, err := EnableSystemPromptMode(root, false)
	if err != nil || !slices.Equal(report.RefreshedHooks, []string{stale}) {
		t.Fatalf("enable: refreshed %v err=%v", report.RefreshedHooks, err)
	}
	if readFile(t, stale) != string(renderEmbeddedAsset(enforceInitHookContent)) || readFile(t, guard) != guardBefore {
		t.Fatal("want only the stale gate script rewritten")
	}
	if again, err := EnableSystemPromptMode(root, false); err != nil || len(again.RefreshedHooks) != 0 {
		t.Fatalf("rerun refreshed %v err=%v", again.RefreshedHooks, err)
	}
	customised := readFile(t, stale) + "# local addition\n"
	if err := os.WriteFile(stale, []byte(customised), 0755); err != nil {
		t.Fatal(err)
	}
	if again, err := EnableSystemPromptMode(root, false); err != nil || len(again.RefreshedHooks) != 0 || readFile(t, stale) != customised {
		t.Fatalf("a script with the bypass must be left alone: refreshed %v err=%v", again.RefreshedHooks, err)
	}
}

func TestEnforceInitHookPassesOnlyInjectedSessions(t *testing.T) {
	hook := writeEnforceInitHook(t)
	root := t.TempDir()
	sessionID := "system-prompt-" + time.Now().Format("150405.000000000")
	t.Cleanup(func() { os.RemoveAll(filepath.Join(os.TempDir(), brand.BinaryName+"-init-gate-"+sessionID)) })
	payload := bashPayload(t, sessionID, root, "git status --short")
	// The suite itself may run inside an injected session: the helper must not inherit the bypass.
	t.Setenv(brand.EnvName("SYSTEM_PROMPT_CONTRACTS"), "1")
	runHook(t, hook, payload, 2) // ACPX, pairing, forced-read launches: still gated
	runHookEnv(t, hook, payload, []string{brand.EnvName("SYSTEM_PROMPT_CONTRACTS") + "=1"}, 0)
}

func TestEnsureSystemPromptFileTracksSourceChanges(t *testing.T) {
	root := setupSystemPromptProject(t)
	path, _, err := EnsureSystemPromptFile(root, SystemPromptModeMultiAgent)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "GUARDRAILS.md"), []byte("# guardrails\n\nnew lesson\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := EnsureSystemPromptFile(root, SystemPromptModeMultiAgent); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readFile(t, path), "new lesson") {
		t.Fatal("prompt file not refreshed after GUARDRAILS.md changed")
	}
}

func TestSessionContextHookSystemPromptContracts(t *testing.T) {
	hook := writeSessionContextHook(t)
	root := t.TempDir()
	agent := brand.EnvName("AGENT_ID") + "=coder-1"
	injected := brand.EnvName("SYSTEM_PROMPT_CONTRACTS") + "=1"
	t.Setenv(brand.EnvName("SYSTEM_PROMPT_CONTRACTS"), "1") // leaked from an injected session: forced-read cases must ignore it
	for _, tc := range []struct {
		name      string
		env       []string
		mandatory bool
	}{
		{"multi-agent injected", []string{agent, injected}, false},
		{"multi-agent forced read", []string{agent}, true},
		{"pairing injected", []string{injected}, false},
		{"pairing forced read", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			context := sessionStartAdditionalContext(t, runSessionContextHook(t, hook, sessionStartPayload(t, root), tc.env, 0))
			if got := strings.Contains(context, "MANDATORY: Read"); got != tc.mandatory {
				t.Fatalf("MANDATORY present = %v, want %v:\n%s", got, tc.mandatory, context)
			}
			if !tc.mandatory && !strings.Contains(context, "are in your system prompt") {
				t.Fatalf("missing system-prompt note:\n%s", context)
			}
		})
	}
}
