package embedded

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
)

func TestEnforceInitHook_AllowsCodexBashDocReads(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	hookPath := writeEnforceInitHook(t)
	projectRoot := t.TempDir()
	sessionID := "test-codex-bash-doc-reads-" + strings.ReplaceAll(t.Name(), "/", "-") + "-" + time.Now().Format("150405.000000000")
	stateDir := filepath.Join(os.TempDir(), "liza-init-gate-"+sessionID)
	defer os.RemoveAll(stateDir)

	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "cat ~/.liza/CORE.md"), 0)
	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "sed -n '1,120p' ~/.liza/AGENT_TOOLS.md"), 0)
	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "cat ~/.liza/PAIRING_MODE.md"), 0)
	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "sed -n '1,260p' "+projectRoot+"/REPOSITORY.md"), 0)
	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "sed -n '1,20p' "+projectRoot+"/docs/USAGE.md"), 0)
	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "cat ~/.liza/COLLABORATION_CONTINUITY.md"), 0)

	if _, err := os.Stat(filepath.Join(stateDir, "CLEARED")); err != nil {
		t.Fatalf("expected init gate to clear after all Pairing init doc reads: %v", err)
	}

	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "git status --short"), 0)
}

func TestEnforceInitHook_NativeReadsClearPairingGate(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	hookPath := writeEnforceInitHook(t)
	projectRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectRoot, "docs"), 0755); err != nil {
		t.Fatalf("create project docs directory: %v", err)
	}
	for _, path := range []string{
		filepath.Join(projectRoot, "GUARDRAILS.md"),
		filepath.Join(projectRoot, "REPOSITORY.md"),
		filepath.Join(projectRoot, "docs", "USAGE.md"),
	} {
		if err := os.WriteFile(path, []byte("required\n"), 0644); err != nil {
			t.Fatalf("write required project document %s: %v", path, err)
		}
	}
	sessionID := "test-native-pairing-init-" + strings.ReplaceAll(t.Name(), "/", "-") + "-" + time.Now().Format("150405.000000000")
	stateDir := filepath.Join(os.TempDir(), "liza-init-gate-"+sessionID)
	defer os.RemoveAll(stateDir)
	for _, path := range []string{
		filepath.Join(homeDir, ".liza", "AGENT_TOOLS.md"),
		filepath.Join(homeDir, ".liza", "PAIRING_MODE.md"),
		filepath.Join(projectRoot, "GUARDRAILS.md"),
		filepath.Join(projectRoot, "REPOSITORY.md"),
		filepath.Join(projectRoot, "docs", "USAGE.md"),
		filepath.Join(homeDir, ".liza", "COLLABORATION_CONTINUITY.md"),
	} {
		payload, err := json.Marshal(map[string]any{
			"session_id": sessionID,
			"cwd":        projectRoot,
			"tool_name":  "Read",
			"tool_input": map[string]any{"file_path": path},
		})
		if err != nil {
			t.Fatalf("marshal native read payload: %v", err)
		}
		runHook(t, hookPath, string(payload), 0)
	}

	if _, err := os.Stat(filepath.Join(stateDir, "CLEARED")); err != nil {
		t.Fatalf("expected native Read calls to clear the Pairing init gate: %v", err)
	}
	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "git status --short"), 0)
}

func TestEnforceInitHook_WrongPathDocumentBasenamesDoNotClearGate(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	hookPath := writeEnforceInitHook(t)
	projectRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectRoot, "docs"), 0755); err != nil {
		t.Fatalf("create project docs directory: %v", err)
	}
	for _, path := range []string{
		filepath.Join(projectRoot, "GUARDRAILS.md"),
		filepath.Join(projectRoot, "REPOSITORY.md"),
		filepath.Join(projectRoot, "docs", "USAGE.md"),
	} {
		if err := os.WriteFile(path, []byte("required\n"), 0644); err != nil {
			t.Fatalf("write required project document %s: %v", path, err)
		}
	}

	sessionID := "test-wrong-path-doc-basenames-" + strings.ReplaceAll(t.Name(), "/", "-") + "-" + time.Now().Format("150405.000000000")
	stateDir := filepath.Join(os.TempDir(), "liza-init-gate-"+sessionID)
	defer os.RemoveAll(stateDir)
	wrongRoot := t.TempDir()
	for _, path := range []string{
		filepath.Join(wrongRoot, "AGENT_TOOLS.md"),
		filepath.Join(wrongRoot, "PAIRING_MODE.md"),
		filepath.Join(wrongRoot, "GUARDRAILS.md"),
		filepath.Join(wrongRoot, "REPOSITORY.md"),
		filepath.Join(wrongRoot, "docs", "USAGE.md"),
		filepath.Join(wrongRoot, "COLLABORATION_CONTINUITY.md"),
	} {
		payload, err := json.Marshal(map[string]any{
			"session_id": sessionID,
			"cwd":        projectRoot,
			"tool_name":  "Read",
			"tool_input": map[string]any{"file_path": path},
		})
		if err != nil {
			t.Fatalf("marshal wrong-path read payload: %v", err)
		}
		runHook(t, hookPath, string(payload), 0)
	}

	if _, err := os.Stat(filepath.Join(stateDir, "CLEARED")); !os.IsNotExist(err) {
		t.Fatalf("wrong-path document basenames should not clear init gate, stat err: %v", err)
	}
	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "git status --short"), 2)
}

func TestEnforceInitHook_AllowsConditionalGuardrailsRead(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	hookPath := writeEnforceInitHook(t)
	projectRoot := t.TempDir()
	sessionID := "test-codex-bash-guardrails-conditional-" + strings.ReplaceAll(t.Name(), "/", "-") + "-" + time.Now().Format("150405.000000000")
	stateDir := filepath.Join(os.TempDir(), "liza-init-gate-"+sessionID)
	defer os.RemoveAll(stateDir)

	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "sed -n '1,120p' ~/.liza/AGENT_TOOLS.md"), 0)
	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "cat ~/.liza/PAIRING_MODE.md"), 0)
	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "if [ -f "+projectRoot+"/GUARDRAILS.md ]; then sed -n '1,260p' "+projectRoot+"/GUARDRAILS.md; fi"), 0)
	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "sed -n '1,260p' "+projectRoot+"/REPOSITORY.md"), 0)
	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "sed -n '1,20p' "+projectRoot+"/docs/USAGE.md"), 0)
	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "cat ~/.liza/COLLABORATION_CONTINUITY.md"), 0)

	if _, err := os.Stat(filepath.Join(stateDir, "CLEARED")); err != nil {
		t.Fatalf("expected init gate to clear after full Pairing init including conditional guardrails read: %v", err)
	}

	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "git status --short"), 0)
}

func TestEnforceInitHook_AllowsPairingInitCompanionDocReadsBeforeGateClear(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	hookPath := writeEnforceInitHook(t)
	projectRoot := t.TempDir()
	sessionID := "test-codex-bash-pairing-init-docs-" + strings.ReplaceAll(t.Name(), "/", "-") + "-" + time.Now().Format("150405.000000000")
	stateDir := filepath.Join(os.TempDir(), "liza-init-gate-"+sessionID)
	defer os.RemoveAll(stateDir)

	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "sed -n '1,260p' "+projectRoot+"/REPOSITORY.md"), 0)
	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "sed -n '1,20p' "+projectRoot+"/docs/USAGE.md"), 0)
	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "cat ~/.liza/COLLABORATION_CONTINUITY.md"), 0)

	if _, err := os.Stat(filepath.Join(stateDir, "CLEARED")); !os.IsNotExist(err) {
		t.Fatalf("companion init doc reads should not clear the gate, stat err: %v", err)
	}
}

func TestEnforceInitHook_BlocksMultiFileInitDocReads(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	hookPath := writeEnforceInitHook(t)
	projectRoot := t.TempDir()

	cases := []struct {
		name    string
		command string
	}{
		{
			name:    "multi-file project cat",
			command: "cat " + projectRoot + "/REPOSITORY.md " + projectRoot + "/docs/USAGE.md",
		},
		{
			name:    "multi-file global cat",
			command: "cat ~/.liza/AGENT_TOOLS.md ~/.liza/PAIRING_MODE.md",
		},
		{
			name:    "multi-file sed",
			command: "sed -n '1,5p' " + projectRoot + "/REPOSITORY.md " + projectRoot + "/docs/USAGE.md",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sessionID := "test-codex-bash-multifile-init-read-" + strings.ReplaceAll(t.Name(), "/", "-") + "-" + time.Now().Format("150405.000000000")
			stateDir := filepath.Join(os.TempDir(), "liza-init-gate-"+sessionID)
			defer os.RemoveAll(stateDir)

			output := runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, ""+tc.command+""), 2)
			if !strings.Contains(output, "one file per command") {
				t.Fatalf("multi-file read should explain the single-file rule, got:\n%s", output)
			}
			if _, err := os.Stat(filepath.Join(stateDir, "CLEARED")); !os.IsNotExist(err) {
				t.Fatalf("multi-file init reads should not clear gate, stat err: %v", err)
			}
		})
	}
}

func TestEnforceInitHook_RejectsStaleGlobalRootAndStopsRepeatedRetries(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	previousNameLower, previousBinaryName, previousGlobalDirName := brand.NameLower, brand.BinaryName, brand.GlobalDirName
	brand.NameLower = "omni"
	brand.BinaryName = "omni-ee"
	brand.GlobalDirName = ".omni-ee"
	t.Cleanup(func() {
		brand.NameLower = previousNameLower
		brand.BinaryName = previousBinaryName
		brand.GlobalDirName = previousGlobalDirName
	})

	hookPath := writeEnforceInitHook(t)
	projectRoot := t.TempDir()
	sessionID := "test-codex-stale-global-root-" + strings.ReplaceAll(t.Name(), "/", "-") + "-" + time.Now().Format("150405.000000000")
	stateDir := filepath.Join(os.TempDir(), "liza-init-gate-"+sessionID)
	defer os.RemoveAll(stateDir)
	payload := bashPayload(t, sessionID, projectRoot, "cat ~/.omni/PAIRING_MODE.md")

	first := runHook(t, hookPath, payload, 2)
	if !strings.Contains(first, "Expected global contract root: ~/.omni-ee/") {
		t.Fatalf("stale root rejection should name the authoritative root, got:\n%s", first)
	}
	if strings.Contains(first, "STOP_RETRYING") {
		t.Fatalf("first invalid read should allow one correction, got:\n%s", first)
	}

	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "cat ~/.omni-ee/PAIRING_MODE.md"), 0)

	second := runHook(t, hookPath, payload, 2)
	if strings.Contains(second, "STOP_RETRYING") {
		t.Fatalf("invalid read after a successful correction should allow another correction, got:\n%s", second)
	}

	third := runHook(t, hookPath, payload, 2)
	if !strings.Contains(third, "STOP_RETRYING") {
		t.Fatalf("consecutive invalid read should stop further retries, got:\n%s", third)
	}
}

func TestEnforceInitHook_SuccessfulNativeReadsResetRepeatedInvalidMarker(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	hookPath := writeEnforceInitHook(t)
	projectRoot := t.TempDir()
	cases := []struct {
		name      string
		toolName  string
		toolInput map[string]any
	}{
		{
			name:      "native Read",
			toolName:  "Read",
			toolInput: map[string]any{"file_path": "~/.liza/AGENT_TOOLS.md"},
		},
		{
			name:      "MCP filesystem read",
			toolName:  "mcp__filesystem__read_text_file",
			toolInput: map[string]any{"path": "~/.liza/AGENT_TOOLS.md"},
		},
		{
			name:      "MCP filesystem multiple-file read",
			toolName:  "mcp__filesystem__read_multiple_files",
			toolInput: map[string]any{"paths": []string{projectRoot + "/unrelated.txt", "~/.liza/AGENT_TOOLS.md"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sessionID := "test-codex-native-read-reset-" + strings.ReplaceAll(t.Name(), "/", "-") + "-" + time.Now().Format("150405.000000000")
			stateDir := filepath.Join(os.TempDir(), "liza-init-gate-"+sessionID)
			defer os.RemoveAll(stateDir)
			invalidPayload := bashPayload(t, sessionID, projectRoot, "cat ~/.wrong/PAIRING_MODE.md")

			first := runHook(t, hookPath, invalidPayload, 2)
			if strings.Contains(first, "STOP_RETRYING") {
				t.Fatalf("first invalid read should allow correction, got:\n%s", first)
			}

			payload, err := json.Marshal(map[string]any{
				"session_id": sessionID,
				"cwd":        projectRoot,
				"tool_name":  tc.toolName,
				"tool_input": tc.toolInput,
			})
			if err != nil {
				t.Fatalf("marshal native read payload: %v", err)
			}
			runHook(t, hookPath, string(payload), 0)

			second := runHook(t, hookPath, invalidPayload, 2)
			if strings.Contains(second, "STOP_RETRYING") {
				t.Fatalf("invalid read after successful %s should allow correction, got:\n%s", tc.name, second)
			}

			third := runHook(t, hookPath, invalidPayload, 2)
			if !strings.Contains(third, "STOP_RETRYING") {
				t.Fatalf("consecutive invalid read should stop further retries, got:\n%s", third)
			}
		})
	}
}

func TestEnforceInitHook_UnrelatedNativeReadsDoNotResetRepeatedInvalidMarker(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	hookPath := writeEnforceInitHook(t)
	projectRoot := t.TempDir()
	cases := []struct {
		name      string
		toolName  string
		toolInput map[string]any
	}{
		{
			name:      "native Read with matching basename at wrong path",
			toolName:  "Read",
			toolInput: map[string]any{"file_path": "/tmp/AGENT_TOOLS.md"},
		},
		{
			name:      "MCP filesystem read",
			toolName:  "mcp__filesystem__read_text_file",
			toolInput: map[string]any{"path": projectRoot + "/unrelated.txt"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sessionID := "test-codex-unrelated-read-marker-" + strings.ReplaceAll(t.Name(), "/", "-") + "-" + time.Now().Format("150405.000000000")
			stateDir := filepath.Join(os.TempDir(), "liza-init-gate-"+sessionID)
			defer os.RemoveAll(stateDir)
			invalidPayload := bashPayload(t, sessionID, projectRoot, "cat ~/.wrong/PAIRING_MODE.md")

			first := runHook(t, hookPath, invalidPayload, 2)
			if strings.Contains(first, "STOP_RETRYING") {
				t.Fatalf("first invalid read should allow correction, got:\n%s", first)
			}

			payload, err := json.Marshal(map[string]any{
				"session_id": sessionID,
				"cwd":        projectRoot,
				"tool_name":  tc.toolName,
				"tool_input": tc.toolInput,
			})
			if err != nil {
				t.Fatalf("marshal unrelated read payload: %v", err)
			}
			runHook(t, hookPath, string(payload), 0)

			second := runHook(t, hookPath, invalidPayload, 2)
			if !strings.Contains(second, "STOP_RETRYING") {
				t.Fatalf("unrelated %s should not reset invalid-read marker, got:\n%s", tc.name, second)
			}
		})
	}
}

func TestEnforceInitHook_AllowsGuardrailsExistenceProbe(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	hookPath := writeEnforceInitHook(t)
	projectRoot := t.TempDir()

	cases := []struct {
		name    string
		command string
	}{
		{
			name:    "test builtin style",
			command: "test -f " + projectRoot + "/GUARDRAILS.md",
		},
		{
			name:    "single bracket style",
			command: "[ -f " + projectRoot + "/GUARDRAILS.md ]",
		},
		{
			name:    "double bracket style",
			command: "[[ -f " + projectRoot + "/GUARDRAILS.md ]]",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sessionID := "test-codex-bash-guardrails-probe-" + strings.ReplaceAll(t.Name(), "/", "-") + "-" + time.Now().Format("150405.000000000")
			stateDir := filepath.Join(os.TempDir(), "liza-init-gate-"+sessionID)
			defer os.RemoveAll(stateDir)

			runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, ""+tc.command+""), 0)
			if _, err := os.Stat(filepath.Join(stateDir, "CLEARED")); !os.IsNotExist(err) {
				t.Fatalf("pure existence probe should not clear gate, stat err: %v", err)
			}
		})
	}
}

func TestEnforceInitHook_AllowsGuardrailsProbeWrappers(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	hookPath := writeEnforceInitHook(t)
	projectRoot := t.TempDir()

	cases := []struct {
		name        string
		command     string
		expectClear bool
	}{
		{
			name:        "probe with echo branches",
			command:     `test -f ` + projectRoot + `/GUARDRAILS.md && echo "EXISTS" || echo "ABSENT"`,
			expectClear: false,
		},
		{
			name:        "probe with read then echo",
			command:     `test -f ` + projectRoot + `/GUARDRAILS.md && cat ` + projectRoot + `/GUARDRAILS.md || echo "GUARDRAILS.md ABSENT"`,
			expectClear: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sessionID := "test-codex-bash-guardrails-wrapper-" + strings.ReplaceAll(t.Name(), "/", "-") + "-" + time.Now().Format("150405.000000000")
			stateDir := filepath.Join(os.TempDir(), "liza-init-gate-"+sessionID)
			defer os.RemoveAll(stateDir)

			runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, tc.command), 0)
			_, err := os.Stat(filepath.Join(stateDir, "CLEARED"))
			if tc.expectClear {
				if err != nil {
					t.Fatalf("guardrails wrapper should clear gate, stat err: %v", err)
				}
			} else if !os.IsNotExist(err) {
				t.Fatalf("guardrails wrapper should not clear gate, stat err: %v", err)
			}
		})
	}
}

func TestEnforceInitHook_GuardrailsWrapperClearsAfterRequiredDocs(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	hookPath := writeEnforceInitHook(t)
	projectRoot := t.TempDir()
	sessionID := "test-codex-bash-guardrails-wrapper-clears-" + strings.ReplaceAll(t.Name(), "/", "-") + "-" + time.Now().Format("150405.000000000")
	stateDir := filepath.Join(os.TempDir(), "liza-init-gate-"+sessionID)
	defer os.RemoveAll(stateDir)

	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "cat ~/.liza/AGENT_TOOLS.md"), 0)
	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "cat ~/.liza/PAIRING_MODE.md"), 0)
	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, `test -f `+projectRoot+`/GUARDRAILS.md && cat `+projectRoot+`/GUARDRAILS.md || echo ABSENT`), 0)
	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "sed -n '1,260p' "+projectRoot+"/REPOSITORY.md"), 0)
	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "sed -n '1,20p' "+projectRoot+"/docs/USAGE.md"), 0)
	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "cat ~/.liza/COLLABORATION_CONTINUITY.md"), 0)

	if _, err := os.Stat(filepath.Join(stateDir, "CLEARED")); err != nil {
		t.Fatalf("guardrails wrapper should clear gate once the full Pairing init set is read: %v", err)
	}
}

func TestEnforceInitHook_DoesNotBlockIndexedPairingRepoAfterInit(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	hookPath := writeEnforceInitHook(t)
	projectRoot := t.TempDir()
	writeIndexedRepoMarkers(t, projectRoot)
	sessionID := "test-codex-bash-pairing-index-advisory-" + strings.ReplaceAll(t.Name(), "/", "-") + "-" + time.Now().Format("150405.000000000")
	stateDir := filepath.Join(os.TempDir(), "liza-init-gate-"+sessionID)
	defer os.RemoveAll(stateDir)

	completePairingInit(t, hookPath, sessionID, projectRoot)

	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "git status --short"), 0)
}

func TestEnforceInitHook_AllowsPairingRepoWithIndexFileButNoLizaIndexHook(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	hookPath := writeEnforceInitHook(t)
	projectRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectRoot, "stacklit.json"), []byte("{}\n"), 0644); err != nil {
		t.Fatalf("write stacklit index: %v", err)
	}
	sessionID := "test-codex-bash-no-pairing-index-advisory-" + strings.ReplaceAll(t.Name(), "/", "-") + "-" + time.Now().Format("150405.000000000")
	stateDir := filepath.Join(os.TempDir(), "liza-init-gate-"+sessionID)
	defer os.RemoveAll(stateDir)

	completePairingInit(t, hookPath, sessionID, projectRoot)
	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "git status --short"), 0)
}

func TestEnforceInitHook_PairingModeListsCompanionDocsUntilRead(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	hookPath := writeEnforceInitHook(t)
	projectRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectRoot, "REPOSITORY.md"), []byte("# repo\n"), 0644); err != nil {
		t.Fatalf("write repository doc: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(projectRoot, "docs"), 0755); err != nil {
		t.Fatalf("create docs dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "docs", "USAGE.md"), []byte("# usage\n"), 0644); err != nil {
		t.Fatalf("write usage doc: %v", err)
	}
	sessionID := "test-codex-bash-pairing-missing-companions-" + strings.ReplaceAll(t.Name(), "/", "-") + "-" + time.Now().Format("150405.000000000")
	stateDir := filepath.Join(os.TempDir(), "liza-init-gate-"+sessionID)
	defer os.RemoveAll(stateDir)

	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "cat ~/.liza/AGENT_TOOLS.md"), 0)
	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "cat ~/.liza/PAIRING_MODE.md"), 0)

	output := runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "git diff --cached"), 2)
	if !strings.Contains(output, "REPOSITORY.md (repo root)") ||
		!strings.Contains(output, "docs/USAGE.md (from repo root)") ||
		!strings.Contains(output, "~/.liza/COLLABORATION_CONTINUITY.md") {
		t.Fatalf("missing-doc message should mention Pairing companion docs after Pairing mode is selected, got:\n%s", output)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "CLEARED")); !os.IsNotExist(err) {
		t.Fatalf("pairing mode should remain blocked until companion docs are read, stat err: %v", err)
	}
}

func TestEnforceInitHook_PairingModeOmitsAbsentProjectCompanionDocs(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	hookPath := writeEnforceInitHook(t)
	projectRoot := t.TempDir()
	sessionID := "test-codex-bash-pairing-absent-project-companions-" + strings.ReplaceAll(t.Name(), "/", "-") + "-" + time.Now().Format("150405.000000000")
	stateDir := filepath.Join(os.TempDir(), "liza-init-gate-"+sessionID)
	defer os.RemoveAll(stateDir)

	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "cat ~/.liza/AGENT_TOOLS.md"), 0)
	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "cat ~/.liza/PAIRING_MODE.md"), 0)

	output := runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "git diff --cached"), 2)
	if strings.Contains(output, "REPOSITORY.md (repo root)") ||
		strings.Contains(output, "docs/USAGE.md (from repo root)") {
		t.Fatalf("missing-doc message should omit absent Pairing project companion docs, got:\n%s", output)
	}
	if !strings.Contains(output, "~/.liza/COLLABORATION_CONTINUITY.md") {
		t.Fatalf("missing-doc message should still require collaboration continuity, got:\n%s", output)
	}

	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "cat ~/.liza/COLLABORATION_CONTINUITY.md"), 0)
	if _, err := os.Stat(filepath.Join(stateDir, "CLEARED")); err != nil {
		t.Fatalf("pairing mode should clear when absent project companion docs are the only unread docs: %v", err)
	}
}

func TestEnforceInitHook_SubagentModeDoesNotRequirePairingCompanionDocs(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	hookPath := writeEnforceInitHook(t)
	projectRoot := t.TempDir()
	sessionID := "test-codex-bash-subagent-no-pairing-docs-" + strings.ReplaceAll(t.Name(), "/", "-") + "-" + time.Now().Format("150405.000000000")
	stateDir := filepath.Join(os.TempDir(), "liza-init-gate-"+sessionID)
	defer os.RemoveAll(stateDir)

	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "cat ~/.liza/AGENT_TOOLS.md"), 0)
	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "cat ~/.liza/SUBAGENT_MODE.md"), 0)

	if _, err := os.Stat(filepath.Join(stateDir, "CLEARED")); err != nil {
		t.Fatalf("subagent mode should clear without Pairing companion docs: %v", err)
	}
}

func TestEnforceInitHook_OmitsAbsentGuardrailsFromMissingList(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	hookPath := writeEnforceInitHook(t)
	projectRoot := t.TempDir()
	sessionID := "test-codex-bash-absent-guardrails-" + strings.ReplaceAll(t.Name(), "/", "-") + "-" + time.Now().Format("150405.000000000")
	stateDir := filepath.Join(os.TempDir(), "liza-init-gate-"+sessionID)
	defer os.RemoveAll(stateDir)

	output := runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "git diff --cached"), 2)
	if strings.Contains(output, "GUARDRAILS.md") {
		t.Fatalf("missing-doc message should omit absent GUARDRAILS.md, got:\n%s", output)
	}
	if !strings.Contains(output, "~/.liza/AGENT_TOOLS.md") || !strings.Contains(output, "The applicable mode contract from the Mode Selection Gate") {
		t.Fatalf("missing-doc message should still mention remaining required docs, got:\n%s", output)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "GUARDRAILS.done")); err != nil {
		t.Fatalf("absent guardrails should mark GUARDRAILS.done: %v", err)
	}
}

func TestEnforceInitHook_ListsPresentGuardrailsInMissingList(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	hookPath := writeEnforceInitHook(t)
	projectRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectRoot, "GUARDRAILS.md"), []byte("# guardrails\n"), 0644); err != nil {
		t.Fatalf("write guardrails: %v", err)
	}
	sessionID := "test-codex-bash-present-guardrails-" + strings.ReplaceAll(t.Name(), "/", "-") + "-" + time.Now().Format("150405.000000000")
	stateDir := filepath.Join(os.TempDir(), "liza-init-gate-"+sessionID)
	defer os.RemoveAll(stateDir)

	output := runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "git diff --cached"), 2)
	if !strings.Contains(output, "GUARDRAILS.md (project root)") {
		t.Fatalf("missing-doc message should mention present GUARDRAILS.md, got:\n%s", output)
	}
	if strings.Contains(output, "confirm absent") {
		t.Fatalf("missing-doc message should not use the old GUARDRAILS wording, got:\n%s", output)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "GUARDRAILS.done")); !os.IsNotExist(err) {
		t.Fatalf("present guardrails should not auto-mark GUARDRAILS.done, stat err: %v", err)
	}
}

func TestEnforceInitHook_BlocksComplexCodexBashDocReads(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	hookPath := writeEnforceInitHook(t)
	projectRoot := t.TempDir()
	sessionID := "test-codex-bash-complex-read-" + strings.ReplaceAll(t.Name(), "/", "-") + "-" + time.Now().Format("150405.000000000")
	stateDir := filepath.Join(os.TempDir(), "liza-init-gate-"+sessionID)
	defer os.RemoveAll(stateDir)

	output := runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "sed -n '1p' ~/.liza/AGENT_TOOLS.md; rm -rf /tmp/not-real"), 2)
	if !strings.Contains(output, "simple read-only doc commands") {
		t.Fatalf("expected complex Bash doc read to explain the restriction, got:\n%s", output)
	}
}

func TestEnforceInitHook_BlocksUnsafeCodexBashDocReads(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	hookPath := writeEnforceInitHook(t)
	projectRoot := t.TempDir()

	cases := []struct {
		name    string
		command string
	}{
		{
			name:    "head",
			command: "head -n 20 ~/.liza/AGENT_TOOLS.md",
		},
		{
			name:    "tail",
			command: "tail -n 20 ~/.liza/AGENT_TOOLS.md",
		},
		{
			name:    "wc",
			command: "wc -l ~/.liza/AGENT_TOOLS.md",
		},
		{
			name:    "cat extra operand",
			command: "cat ~/.liza/AGENT_TOOLS.md /tmp/not-a-doc",
		},
		{
			name:    "sed in-place",
			command: "sed -i s/x/y/ ~/.liza/AGENT_TOOLS.md",
		},
		{
			name:    "cat redirected",
			command: "cat ~/.liza/AGENT_TOOLS.md > /tmp/copy",
		},
		{
			name:    "guardrails conditional with extra command",
			command: "if [ -f GUARDRAILS.md ]; then sed -n '1,20p' GUARDRAILS.md; rm -rf /tmp/not-real; fi",
		},
		{
			name:    "guardrails wrapper reads different file",
			command: "test -f GUARDRAILS.md && cat ~/.liza/AGENT_TOOLS.md || echo absent",
		},
		{
			name:    "guardrails wrapper with non-echo else branch",
			command: "test -f GUARDRAILS.md && cat GUARDRAILS.md || cat ~/.liza/AGENT_TOOLS.md",
		},
		{
			name:    "multi-file read with non-init path",
			command: "wc -l ~/.liza/AGENT_TOOLS.md /tmp/not-a-doc",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sessionID := "test-codex-bash-unsafe-read-" + strings.ReplaceAll(t.Name(), "/", "-") + "-" + time.Now().Format("150405.000000000")
			stateDir := filepath.Join(os.TempDir(), "liza-init-gate-"+sessionID)
			defer os.RemoveAll(stateDir)

			payload := bashPayload(t, sessionID, projectRoot, tc.command)
			output := runHook(t, hookPath, payload, 2)
			if !strings.Contains(output, "simple read-only doc commands") {
				t.Fatalf("expected unsafe Bash doc read to explain the restriction, got:\n%s", output)
			}
			if _, err := os.Stat(filepath.Join(stateDir, "CLEARED")); !os.IsNotExist(err) {
				t.Fatalf("unsafe command should not clear gate, stat err: %v", err)
			}
		})
	}
}

func writeEnforceInitHook(t *testing.T) string {
	t.Helper()
	hookPath := filepath.Join(t.TempDir(), "enforce-init.sh")
	if err := os.WriteFile(hookPath, renderEmbeddedAsset(enforceInitHookContent), 0755); err != nil {
		t.Fatalf("write hook: %v", err)
	}
	return hookPath
}

func completePairingInit(t *testing.T, hookPath, sessionID, projectRoot string) {
	t.Helper()

	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "cat ~/.liza/AGENT_TOOLS.md"), 0)
	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "cat ~/.liza/PAIRING_MODE.md"), 0)
	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "sed -n '1,260p' "+projectRoot+"/REPOSITORY.md"), 0)
	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "sed -n '1,20p' "+projectRoot+"/docs/USAGE.md"), 0)
	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "cat ~/.liza/COLLABORATION_CONTINUITY.md"), 0)
}

func writeIndexedRepoMarkers(t *testing.T, projectRoot string) {
	t.Helper()

	hooksDir := filepath.Join(projectRoot, ".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatalf("create git hooks dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hooksDir, "post-commit"), []byte("#!/bin/sh\nliza-index\n"), 0755); err != nil {
		t.Fatalf("write post-commit hook: %v", err)
	}
	for _, name := range []string{"stacklit.json", "go.scip", "python.scip"} {
		if err := os.WriteFile(filepath.Join(projectRoot, name), []byte("index\n"), 0644); err != nil {
			t.Fatalf("write index %s: %v", name, err)
		}
	}
}

func bashPayload(t *testing.T, sessionID, cwd, command string) string {
	t.Helper()

	payload := map[string]any{
		"session_id": sessionID,
		"cwd":        cwd,
		"tool_name":  "Bash",
		"tool_input": map[string]any{
			"command": command,
		},
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(payload); err != nil {
		t.Fatalf("marshal bash payload: %v", err)
	}
	return strings.TrimSpace(buf.String())
}

// resolveBashForScripts mirrors testhelpers.ResolveBashForScripts. It is
// duplicated here because internal/testhelpers imports internal/embedded
// (testhelpers/pipeline.go), and these tests are in package embedded, so
// importing testhelpers back would create an import cycle.
func resolveBashForScripts(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "windows" {
		if p, err := exec.LookPath("bash"); err == nil {
			return p
		}
		t.Skip("bash not available")
		return ""
	}
	for _, candidate := range []string{
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Git", "bin", "bash.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "Git", "bin", "bash.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Git", "bin", "bash.exe"),
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	if p, err := exec.LookPath("bash"); err == nil {
		return p
	}
	t.Skip("bash not available")
	return ""
}

func runHook(t *testing.T, hookPath, payload string, wantCode int) string {
	t.Helper()
	// Git Bash treats backslashes as escapes, so pass the hook path in
	// forward-slash form for it to be found on Windows as well as Unix.
	cmd := exec.Command(resolveBashForScripts(t), filepath.ToSlash(hookPath))
	cmd.Stdin = strings.NewReader(payload)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	if wantCode == 0 {
		if err != nil {
			t.Fatalf("hook exited non-zero: %v\n%s", err, output.String())
		}
		return output.String()
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("hook exit = %v, want code %d\n%s", err, wantCode, output.String())
	}
	if exitErr.ExitCode() != wantCode {
		t.Fatalf("hook exit code = %d, want %d\n%s", exitErr.ExitCode(), wantCode, output.String())
	}
	return output.String()
}
