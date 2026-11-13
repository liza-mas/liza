package embedded

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
	for _, name := range []string{"MULTI_AGENT_MODE.md", "AGENT_TOOLS.md"} {
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
	// The provider's contract link already loads CORE.md (ADR-0111); a copy here loads it twice.
	if !strings.Contains(string(first), "CORE.md is not repeated here") {
		t.Error("header does not say where CORE.md comes from")
	}
	if !strings.Contains(string(first), "project rule") || len(sources) != 3 {
		t.Errorf("GUARDRAILS.md missing or wrong source count %d", len(sources))
	}
	for _, unwanted := range []string{brand.MetadataKey("build_date"), root, os.Getenv("HOME"), "Source: ~/" + paths.GlobalDirName() + "/PAIRING_MODE.md", "Source: ~/" + paths.GlobalDirName() + "/CORE.md"} {
		if strings.Contains(string(first), unwanted) {
			t.Errorf("multi_agent prompt contains variable or foreign content %q", unwanted)
		}
	}

	if err := os.Remove(filepath.Join(root, "GUARDRAILS.md")); err != nil {
		t.Fatal(err)
	}
	if _, sources, err = BuildSystemPrompt(root, SystemPromptModeMultiAgent); err != nil || len(sources) != 2 {
		t.Fatalf("absent GUARDRAILS.md must be skipped: sources=%d err=%v", len(sources), err)
	}
}

func TestBuildSystemPromptFailsWithoutInstalledContracts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, _, err := BuildSystemPrompt(t.TempDir(), SystemPromptModeMultiAgent); err == nil {
		t.Fatal("expected an error when the contracts are not installed")
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

// The built files skip absent project documents, so the injected-contracts
// note must name only those present (DEV-951).
func TestSessionContextHookInjectedClaimNamesExistingDocs(t *testing.T) {
	hook := writeSessionContextHook(t)
	agent := brand.EnvName("AGENT_ID") + "=coder-1"
	injected := brand.EnvName("SYSTEM_PROMPT_CONTRACTS") + "=1"
	for _, tc := range []struct {
		name    string
		env     []string
		docs    bool
		present []string
		absent  []string
	}{
		{"pairing without project docs", []string{injected}, false,
			[]string{"CORE.md, PAIRING_MODE.md, AGENT_TOOLS.md, COLLABORATION_CONTINUITY.md)"},
			[]string{"GUARDRAILS.md", "REPOSITORY.md", "docs/USAGE.md"}},
		{"pairing with project docs", []string{injected}, true,
			[]string{"CORE.md, PAIRING_MODE.md, AGENT_TOOLS.md, COLLABORATION_CONTINUITY.md, GUARDRAILS.md, REPOSITORY.md, docs/USAGE.md)"},
			nil},
		{"multi-agent without project docs", []string{agent, injected}, false,
			[]string{"CORE.md, MULTI_AGENT_MODE.md, AGENT_TOOLS.md)"},
			[]string{"GUARDRAILS.md"}},
		{"multi-agent with project docs", []string{agent, injected}, true,
			[]string{"CORE.md, MULTI_AGENT_MODE.md, AGENT_TOOLS.md, GUARDRAILS.md)"},
			[]string{"REPOSITORY.md", "docs/USAGE.md"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if tc.docs {
				writePairingProjectDocs(t, root)
			}
			context := sessionStartAdditionalContext(t, runSessionContextHook(t, hook, sessionStartPayload(t, root), tc.env, 0))
			for _, want := range tc.present {
				if !strings.Contains(context, want) {
					t.Errorf("context lacks %q:\n%s", want, context)
				}
			}
			for _, unwanted := range tc.absent {
				if strings.Contains(context, unwanted) {
					t.Errorf("context names %q, which the project lacks or the mode does not carry:\n%s", unwanted, context)
				}
			}
		})
	}
}

// A subagent does not inherit the lane's system prompt, so its own file must
// carry the project rules (DEV-954).
func TestSubagentSystemPromptCarriesGuardrails(t *testing.T) {
	root := setupSystemPromptProject(t)
	content, sources, err := BuildSystemPrompt(root, SystemPromptModeSubagent)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "project rule") || len(sources) != 3 {
		t.Fatalf("subagent prompt lacks GUARDRAILS.md: %d sources", len(sources))
	}
	if err := os.Remove(filepath.Join(root, "GUARDRAILS.md")); err != nil {
		t.Fatal(err)
	}
	if _, sources, err = BuildSystemPrompt(root, SystemPromptModeSubagent); err != nil || len(sources) != 2 {
		t.Fatalf("absent GUARDRAILS.md must be skipped: sources=%d err=%v", len(sources), err)
	}
}

// pairing.md is rebuilt only by use-system-prompt, so a session must learn
// which of its sources changed since (DEV-951).
func TestStalePairingSources(t *testing.T) {
	root := setupSystemPromptProject(t)
	globalDir, err := paths.GlobalLizaDir()
	if err != nil {
		t.Fatal(err)
	}
	if changed, removed, err := StalePairingSources(root); err != nil || changed != nil || removed != nil {
		t.Fatalf("without a marker: changed=%v removed=%v err=%v, want nil", changed, removed, err)
	}
	if _, err := EnableSystemPromptMode(root, false); err != nil {
		t.Fatal(err)
	}
	if changed, removed, err := StalePairingSources(root); err != nil || len(changed)+len(removed) != 0 {
		t.Fatalf("fresh build: changed=%v removed=%v err=%v, want none", changed, removed, err)
	}

	letter := filepath.Join(globalDir, "COLLABORATION_CONTINUITY.md")
	original := readFile(t, letter)
	if !strings.HasPrefix(original, "---\n") {
		t.Fatalf("installed letter has no frontmatter; the frontmatter case below tests nothing")
	}
	// Only the frontmatter differs: the build strips it, so nothing is stale.
	if err := os.WriteFile(letter, []byte(strings.Replace(original, "---\n", "---\nextra: true\n", 1)), 0644); err != nil {
		t.Fatal(err)
	}
	if changed, removed, err := StalePairingSources(root); err != nil || len(changed)+len(removed) != 0 {
		t.Fatalf("frontmatter-only change: changed=%v removed=%v err=%v, want none", changed, removed, err)
	}

	if err := os.WriteFile(letter, []byte(original+"\nnew pattern\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "GUARDRAILS.md"), []byte("# guardrails\n\nnew lesson\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "REPOSITORY.md"), []byte("# repo\n"), 0644); err != nil {
		t.Fatal(err)
	}
	want := []string{"~/" + paths.GlobalDirName() + "/COLLABORATION_CONTINUITY.md", "GUARDRAILS.md", "REPOSITORY.md"}
	if changed, removed, err := StalePairingSources(root); err != nil || !slices.Equal(changed, want) || len(removed) != 0 {
		t.Fatalf("changed=%v removed=%v err=%v, want %v in build order and none removed", changed, removed, err, want)
	}

	// A project document built into the file and deleted since is reported as
	// removed, not as a source to read.
	if _, err := EnableSystemPromptMode(root, false); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "REPOSITORY.md")); err != nil {
		t.Fatal(err)
	}
	if changed, removed, err := StalePairingSources(root); err != nil || len(changed) != 0 || !slices.Equal(removed, []string{"REPOSITORY.md"}) {
		t.Fatalf("after deleting REPOSITORY.md: changed=%v removed=%v err=%v, want only REPOSITORY.md removed", changed, removed, err)
	}
}

// writeStaleProbeStub puts an engine stub first on PATH that answers
// system-prompt-stale with output and exit code, and logs its calls.
func writeStaleProbeStub(t *testing.T, output string, code int) (string, string) {
	t.Helper()
	binDir := t.TempDir()
	logPath := filepath.Join(binDir, "calls.log")
	outputPath := filepath.Join(binDir, "output.txt")
	if err := os.WriteFile(outputPath, []byte(output), 0644); err != nil {
		t.Fatal(err)
	}
	stub := fmt.Sprintf("#!/bin/sh\necho \"$*\" >> %q\ncat %q\nexit %d\n", logPath, outputPath, code)
	if err := os.WriteFile(filepath.Join(binDir, brand.BinaryName), []byte(stub), 0755); err != nil {
		t.Fatal(err)
	}
	return "PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH"), logPath
}

func TestSessionContextHookNamesStalePairingSources(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stub engine is a shell script, which Windows does not run by name")
	}
	hook := writeSessionContextHook(t)
	injected := brand.EnvName("SYSTEM_PROMPT_CONTRACTS") + "=1"
	agent := brand.EnvName("AGENT_ID") + "=coder-1"
	letter := "~/" + paths.GlobalDirName() + "/COLLABORATION_CONTINUITY.md"
	for _, tc := range []struct {
		name      string
		marker    bool
		env       []string
		output    string
		code      int
		probed    bool
		mandatory bool
		claim     string
		reads     []string
		removed   string
	}{
		{name: "no marker", env: []string{injected}, claim: "COLLABORATION_CONTINUITY.md, GUARDRAILS.md)"},
		{name: "current bundle", marker: true, env: []string{injected}, probed: true,
			claim: "CORE.md, PAIRING_MODE.md, AGENT_TOOLS.md, COLLABORATION_CONTINUITY.md, GUARDRAILS.md)"},
		{name: "stale bundle", marker: true, env: []string{injected}, output: "changed " + letter + "\nchanged GUARDRAILS.md\n", probed: true,
			claim: "(CORE.md, PAIRING_MODE.md, AGENT_TOOLS.md)", reads: []string{letter, "/GUARDRAILS.md"}},
		{name: "removed document", marker: true, env: []string{injected}, output: "removed REPOSITORY.md\n", probed: true,
			claim: "COLLABORATION_CONTINUITY.md, GUARDRAILS.md)", removed: "REPOSITORY.md"},
		{name: "probe failure", marker: true, env: []string{injected}, code: 1, probed: true, mandatory: true},
		{name: "multi-agent", marker: true, env: []string{agent, injected}, output: "changed GUARDRAILS.md\n",
			claim: "CORE.md, MULTI_AGENT_MODE.md, AGENT_TOOLS.md, GUARDRAILS.md)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "GUARDRAILS.md"), []byte("# guardrails\n"), 0644); err != nil {
				t.Fatal(err)
			}
			if tc.marker {
				if err := os.MkdirAll(filepath.Join(root, brand.RuntimeValues().ProjectDirName), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, brand.RuntimeValues().ProjectDirName, "system-prompt-mode.json"), []byte("{}\n"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			pathEnv, logPath := writeStaleProbeStub(t, tc.output, tc.code)
			env := append([]string{pathEnv}, tc.env...)
			context := sessionStartAdditionalContext(t, runSessionContextHook(t, hook, sessionStartPayload(t, root), env, 0))

			calls, _ := os.ReadFile(logPath)
			if got := strings.Contains(string(calls), "system-prompt-stale --project-root "+emitted(root)); got != tc.probed {
				t.Fatalf("probed = %v, want %v (calls: %q)", got, tc.probed, calls)
			}
			if got := strings.Contains(context, "MANDATORY: Read the documents listed as required"); got != tc.mandatory {
				t.Fatalf("forced reads = %v, want %v:\n%s", got, tc.mandatory, context)
			}
			if tc.mandatory {
				return
			}
			if !strings.Contains(context, tc.claim) {
				t.Fatalf("context lacks claim %q:\n%s", tc.claim, context)
			}
			changed := strings.Contains(context, "changed since")
			if changed != (len(tc.reads) > 0) {
				t.Fatalf("stale note present = %v, want %v:\n%s", changed, len(tc.reads) > 0, context)
			}
			if changed {
				_, note, _ := strings.Cut(context, "changed since")
				for _, want := range tc.reads {
					if !strings.Contains(note, want) {
						t.Fatalf("stale note lacks %q:\n%s", want, context)
					}
				}
				if !strings.Contains(note, "MANDATORY") || !strings.Contains(note, brand.BinaryName+" use-system-prompt") {
					t.Fatalf("stale note must order the reads and name the rebuild:\n%s", context)
				}
			}
			_, removedNote, hasRemoved := strings.Cut(context, "removed from the project")
			if hasRemoved != (tc.removed != "") {
				t.Fatalf("removed note present = %v, want %v:\n%s", hasRemoved, tc.removed != "", context)
			}
			if hasRemoved && (!strings.Contains(removedNote, tc.removed) || !strings.Contains(removedNote, "Disregard") ||
				!strings.Contains(removedNote, brand.BinaryName+" use-system-prompt")) {
				t.Fatalf("removed note must name %q, say to disregard it and name the rebuild:\n%s", tc.removed, context)
			}
		})
	}
}

func TestRebuildSystemPromptBundlesWithoutMarkerWritesNothing(t *testing.T) {
	root := setupSystemPromptProject(t)

	changed, err := RebuildSystemPromptBundles(root)

	if err != nil || len(changed) != 0 {
		t.Fatalf("changed %v err=%v, want nothing", changed, err)
	}
	if _, err := os.Stat(paths.New(root).LizaDir()); !os.IsNotExist(err) {
		t.Fatalf("project directory created without the marker: %v", err)
	}
}

func TestRebuildSystemPromptBundlesMatchesEnableWithoutTouchingHooks(t *testing.T) {
	// GIVEN an opted-in project whose sources changed since the build, and a
	// pre-bypass gate script EnableSystemPromptMode would rewrite
	root := setupSystemPromptProject(t)
	if _, err := EnableSystemPromptMode(root, false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "GUARDRAILS.md"), []byte("# guardrails\n\nnew rule\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gate := filepath.Join(root, ".claude", "hooks", "enforce-init.sh")
	if err := os.MkdirAll(filepath.Dir(gate), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gate, []byte("#!/bin/bash\nexit 2\n"), 0755); err != nil {
		t.Fatal(err)
	}

	// WHEN rebuilding
	changed, err := RebuildSystemPromptBundles(root)

	// THEN every file holding GUARDRAILS.md and the marker are rewritten, the gate is not
	if err != nil {
		t.Fatal(err)
	}
	want := []string{}
	for _, mode := range SystemPromptModes {
		want = append(want, SystemPromptFilePath(root, mode))
	}
	want = append(want, SystemPromptMarkerPath(root))
	if !slices.Equal(changed, want) {
		t.Fatalf("changed = %v, want %v", changed, want)
	}
	if readFile(t, gate) != "#!/bin/bash\nexit 2\n" {
		t.Fatal("rebuild wrote a hook script")
	}
	rebuilt := map[string]string{}
	for _, path := range want {
		rebuilt[path] = readFile(t, path)
	}

	// AND the files equal what EnableSystemPromptMode writes from the same sources
	if _, err := EnableSystemPromptMode(root, false); err != nil {
		t.Fatal(err)
	}
	for path, content := range rebuilt {
		if readFile(t, path) != content {
			t.Errorf("%s differs from EnableSystemPromptMode's", path)
		}
	}

	// AND a second rebuild changes nothing
	if again, err := RebuildSystemPromptBundles(root); err != nil || len(again) != 0 {
		t.Fatalf("second rebuild changed %v err=%v", again, err)
	}
}
