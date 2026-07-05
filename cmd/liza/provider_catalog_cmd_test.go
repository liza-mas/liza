package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/providers"
)

func TestProvidersListCommandUsesCatalog(t *testing.T) {
	useCommandTestProviderCatalog(t)
	resetRootCmdForTest(t)

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetArgs([]string{"providers", "list"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("providers list error: %v", err)
	}
	if !strings.Contains(out.String(), "qwen\tQwen\tcli") {
		t.Fatalf("providers list output missing qwen:\n%s", out.String())
	}
}

func TestProvidersRefreshCommandWritesCache(t *testing.T) {
	useCommandTestProviderCatalog(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	resetRootCmdForTest(t)

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetArgs([]string{"providers", "refresh"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("providers refresh error: %v", err)
	}
	if !strings.Contains(out.String(), "provider catalog refreshed") {
		t.Fatalf("providers refresh output = %q", out.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".liza", "cache", "provider-catalog.yaml")); err != nil {
		t.Fatalf("provider catalog cache not written: %v", err)
	}
}

func TestDetectedProviderPickerOptionsPreselectsAllDetectedProviders(t *testing.T) {
	installed := []providers.DetectionResult{
		{ID: "claude", DisplayName: "Claude", Installed: true},
		{ID: "qwen", DisplayName: "Qwen", Installed: true},
	}
	selected, options := detectedProviderPickerOptions(installed)
	if strings.Join(selected, ",") != "claude,qwen" {
		t.Fatalf("selected = %v, want all detected provider ids", selected)
	}
	if len(options) != 2 {
		t.Fatalf("options len = %d, want 2", len(options))
	}
	if options[0].Key != "Claude (claude)" || options[0].Value != "claude" {
		t.Fatalf("first option = %#v", options[0])
	}
	if options[1].Key != "Qwen (qwen)" || options[1].Value != "qwen" {
		t.Fatalf("second option = %#v", options[1])
	}
}

func TestSetupDetectableProvidersSkipsRuntimeOnlyACPProviders(t *testing.T) {
	cat := providers.Catalog{Version: 1, Providers: []providers.Provider{
		{
			ID:          "codex",
			DisplayName: "Codex",
			Backend:     "cli",
			Setup: providers.Setup{
				ConfigDir: ".codex",
				SkillsDir: "skills",
			},
			Runtime: providers.Runtime{Executable: "codex", PromptTransport: "stdin"},
		},
		{
			ID:          "codex-acp",
			DisplayName: "Codex ACP",
			Backend:     "acpx",
			Runtime: providers.Runtime{
				Executable:          "acpx",
				PromptTransport:     "stdin",
				RequiredExecutables: []string{"acpx"},
			},
		},
	}}
	results := []providers.DetectionResult{
		{ID: "codex", DisplayName: "Codex", Installed: true},
		{ID: "codex-acp", DisplayName: "Codex ACP", Installed: true},
	}
	installed := setupDetectableProviders(cat, results)
	if len(installed) != 1 || installed[0].ID != "codex" {
		t.Fatalf("setupDetectableProviders = %#v, want only codex", installed)
	}
}

func TestPromptDetectedProvidersTextAcceptsNumbersAndIDs(t *testing.T) {
	installed := []providers.DetectionResult{
		{ID: "claude", DisplayName: "Claude", Installed: true},
		{ID: "qwen", DisplayName: "Qwen", Installed: true},
	}
	var out bytes.Buffer
	selected, err := promptDetectedProvidersText(installed, strings.NewReader("2, claude\n"), &out)
	if err != nil {
		t.Fatalf("promptDetectedProvidersText error: %v", err)
	}
	if strings.Join(selected, ",") != "qwen,claude" {
		t.Fatalf("selected = %v, want qwen, claude", selected)
	}
	if !strings.Contains(out.String(), "Detected providers:") {
		t.Fatalf("output missing prompt: %q", out.String())
	}
}

func useCommandTestProviderCatalog(t *testing.T) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`version: 1
providers:
  - id: qwen
    display_name: Qwen
    backend: cli
    detection:
      binaries: [qwen]
      version_args: [--version]
    setup:
      config_dir: .qwen
      skills_dir: skills
      contract:
        repo_file: QWEN.md
        global_fallback: .qwen/QWEN.md
    runtime:
      provider_key: qwen
      executable: qwen
      prompt_transport: stdin
      run_args: [-p]
      contract_key: qwen
`))
	}))
	t.Cleanup(server.Close)
	t.Setenv(providers.EnvCatalogURL, server.URL)
}
