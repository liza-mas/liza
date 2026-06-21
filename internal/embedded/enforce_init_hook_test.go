package embedded

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

	runHook(t, hookPath, `{"session_id":"`+sessionID+`","cwd":"`+projectRoot+`","tool_name":"Bash","tool_input":{"command":"sed -n '1,120p' ~/.liza/AGENT_TOOLS.md"}}`, 0)
	runHook(t, hookPath, `{"session_id":"`+sessionID+`","cwd":"`+projectRoot+`","tool_name":"Bash","tool_input":{"command":"cat ~/.liza/PAIRING_MODE.md"}}`, 0)
	runHook(t, hookPath, `{"session_id":"`+sessionID+`","cwd":"`+projectRoot+`","tool_name":"Bash","tool_input":{"command":"sed -n '1,260p' `+projectRoot+`/REPOSITORY.md"}}`, 0)
	runHook(t, hookPath, `{"session_id":"`+sessionID+`","cwd":"`+projectRoot+`","tool_name":"Bash","tool_input":{"command":"head -n 20 `+projectRoot+`/docs/USAGE.md"}}`, 0)
	runHook(t, hookPath, `{"session_id":"`+sessionID+`","cwd":"`+projectRoot+`","tool_name":"Bash","tool_input":{"command":"cat ~/.liza/COLLABORATION_CONTINUITY.md"}}`, 0)

	if _, err := os.Stat(filepath.Join(stateDir, "CLEARED")); err != nil {
		t.Fatalf("expected init gate to clear after all Pairing init doc reads: %v", err)
	}

	runHook(t, hookPath, `{"session_id":"`+sessionID+`","cwd":"`+projectRoot+`","tool_name":"Bash","tool_input":{"command":"git status --short"}}`, 0)
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

	runHook(t, hookPath, `{"session_id":"`+sessionID+`","cwd":"`+projectRoot+`","tool_name":"Bash","tool_input":{"command":"sed -n '1,120p' ~/.liza/AGENT_TOOLS.md"}}`, 0)
	runHook(t, hookPath, `{"session_id":"`+sessionID+`","cwd":"`+projectRoot+`","tool_name":"Bash","tool_input":{"command":"cat ~/.liza/PAIRING_MODE.md"}}`, 0)
	runHook(t, hookPath, `{"session_id":"`+sessionID+`","cwd":"`+projectRoot+`","tool_name":"Bash","tool_input":{"command":"if [ -f `+projectRoot+`/GUARDRAILS.md ]; then sed -n '1,260p' `+projectRoot+`/GUARDRAILS.md; fi"}}`, 0)
	runHook(t, hookPath, `{"session_id":"`+sessionID+`","cwd":"`+projectRoot+`","tool_name":"Bash","tool_input":{"command":"sed -n '1,260p' `+projectRoot+`/REPOSITORY.md"}}`, 0)
	runHook(t, hookPath, `{"session_id":"`+sessionID+`","cwd":"`+projectRoot+`","tool_name":"Bash","tool_input":{"command":"head -n 20 `+projectRoot+`/docs/USAGE.md"}}`, 0)
	runHook(t, hookPath, `{"session_id":"`+sessionID+`","cwd":"`+projectRoot+`","tool_name":"Bash","tool_input":{"command":"cat ~/.liza/COLLABORATION_CONTINUITY.md"}}`, 0)

	if _, err := os.Stat(filepath.Join(stateDir, "CLEARED")); err != nil {
		t.Fatalf("expected init gate to clear after full Pairing init including conditional guardrails read: %v", err)
	}

	runHook(t, hookPath, `{"session_id":"`+sessionID+`","cwd":"`+projectRoot+`","tool_name":"Bash","tool_input":{"command":"git status --short"}}`, 0)
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

	runHook(t, hookPath, `{"session_id":"`+sessionID+`","cwd":"`+projectRoot+`","tool_name":"Bash","tool_input":{"command":"sed -n '1,260p' `+projectRoot+`/REPOSITORY.md"}}`, 0)
	runHook(t, hookPath, `{"session_id":"`+sessionID+`","cwd":"`+projectRoot+`","tool_name":"Bash","tool_input":{"command":"head -n 20 `+projectRoot+`/docs/USAGE.md"}}`, 0)
	runHook(t, hookPath, `{"session_id":"`+sessionID+`","cwd":"`+projectRoot+`","tool_name":"Bash","tool_input":{"command":"cat ~/.liza/COLLABORATION_CONTINUITY.md"}}`, 0)

	if _, err := os.Stat(filepath.Join(stateDir, "CLEARED")); !os.IsNotExist(err) {
		t.Fatalf("companion init doc reads should not clear the gate, stat err: %v", err)
	}
}

func TestEnforceInitHook_AllowsMultiFileInitDocReads(t *testing.T) {
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
			name:        "multi-file wc",
			command:     "wc -l " + projectRoot + "/REPOSITORY.md " + projectRoot + "/docs/USAGE.md",
			expectClear: false,
		},
		{
			name:        "multi-file cat",
			command:     "cat ~/.liza/AGENT_TOOLS.md ~/.liza/PAIRING_MODE.md",
			expectClear: false,
		},
		{
			name:        "multi-file head",
			command:     "head -n 5 " + projectRoot + "/REPOSITORY.md " + projectRoot + "/docs/USAGE.md",
			expectClear: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sessionID := "test-codex-bash-multifile-init-read-" + strings.ReplaceAll(t.Name(), "/", "-") + "-" + time.Now().Format("150405.000000000")
			stateDir := filepath.Join(os.TempDir(), "liza-init-gate-"+sessionID)
			defer os.RemoveAll(stateDir)

			runHook(t, hookPath, `{"session_id":"`+sessionID+`","cwd":"`+projectRoot+`","tool_name":"Bash","tool_input":{"command":"`+tc.command+`"}}`, 0)
			_, err := os.Stat(filepath.Join(stateDir, "CLEARED"))
			if tc.expectClear {
				if err != nil {
					t.Fatalf("multi-file init read should clear gate, stat err: %v", err)
				}
			} else if !os.IsNotExist(err) {
				t.Fatalf("multi-file init reads should not clear gate, stat err: %v", err)
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

			runHook(t, hookPath, `{"session_id":"`+sessionID+`","cwd":"`+projectRoot+`","tool_name":"Bash","tool_input":{"command":"`+tc.command+`"}}`, 0)
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

	runHook(t, hookPath, `{"session_id":"`+sessionID+`","cwd":"`+projectRoot+`","tool_name":"Bash","tool_input":{"command":"cat ~/.liza/AGENT_TOOLS.md ~/.liza/PAIRING_MODE.md"}}`, 0)
	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, `test -f `+projectRoot+`/GUARDRAILS.md && cat `+projectRoot+`/GUARDRAILS.md || echo ABSENT`), 0)
	runHook(t, hookPath, `{"session_id":"`+sessionID+`","cwd":"`+projectRoot+`","tool_name":"Bash","tool_input":{"command":"sed -n '1,260p' `+projectRoot+`/REPOSITORY.md"}}`, 0)
	runHook(t, hookPath, `{"session_id":"`+sessionID+`","cwd":"`+projectRoot+`","tool_name":"Bash","tool_input":{"command":"head -n 20 `+projectRoot+`/docs/USAGE.md"}}`, 0)
	runHook(t, hookPath, `{"session_id":"`+sessionID+`","cwd":"`+projectRoot+`","tool_name":"Bash","tool_input":{"command":"cat ~/.liza/COLLABORATION_CONTINUITY.md"}}`, 0)

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

	runHook(t, hookPath, `{"session_id":"`+sessionID+`","cwd":"`+projectRoot+`","tool_name":"Bash","tool_input":{"command":"cat ~/.liza/AGENT_TOOLS.md"}}`, 0)
	runHook(t, hookPath, `{"session_id":"`+sessionID+`","cwd":"`+projectRoot+`","tool_name":"Bash","tool_input":{"command":"cat ~/.liza/PAIRING_MODE.md"}}`, 0)

	output := runHook(t, hookPath, `{"session_id":"`+sessionID+`","cwd":"`+projectRoot+`","tool_name":"Bash","tool_input":{"command":"git diff --cached"}}`, 2)
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

	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "cat ~/.liza/AGENT_TOOLS.md ~/.liza/PAIRING_MODE.md"), 0)

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

	runHook(t, hookPath, `{"session_id":"`+sessionID+`","cwd":"`+projectRoot+`","tool_name":"Bash","tool_input":{"command":"cat ~/.liza/AGENT_TOOLS.md"}}`, 0)
	runHook(t, hookPath, `{"session_id":"`+sessionID+`","cwd":"`+projectRoot+`","tool_name":"Bash","tool_input":{"command":"cat ~/.liza/SUBAGENT_MODE.md"}}`, 0)

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

	output := runHook(t, hookPath, `{"session_id":"`+sessionID+`","cwd":"`+projectRoot+`","tool_name":"Bash","tool_input":{"command":"git diff --cached"}}`, 2)
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

	output := runHook(t, hookPath, `{"session_id":"`+sessionID+`","cwd":"`+projectRoot+`","tool_name":"Bash","tool_input":{"command":"git diff --cached"}}`, 2)
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

	output := runHook(t, hookPath, `{"session_id":"`+sessionID+`","cwd":"`+projectRoot+`","tool_name":"Bash","tool_input":{"command":"sed -n '1p' ~/.liza/AGENT_TOOLS.md; rm -rf /tmp/not-real"}}`, 2)
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

			payload := `{"session_id":"` + sessionID + `","cwd":"` + projectRoot + `","tool_name":"Bash","tool_input":{"command":"` + tc.command + `"}}`
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

	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "cat ~/.liza/AGENT_TOOLS.md ~/.liza/PAIRING_MODE.md"), 0)
	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "sed -n '1,260p' "+projectRoot+"/REPOSITORY.md"), 0)
	runHook(t, hookPath, bashPayload(t, sessionID, projectRoot, "head -n 20 "+projectRoot+"/docs/USAGE.md"), 0)
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

func runHook(t *testing.T, hookPath, payload string, wantCode int) string {
	t.Helper()
	cmd := exec.Command("bash", hookPath)
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
