package agent

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/providers"
)

// TestMain pins the provider catalog to the embedded copy for this package.
//
// BuiltInAgentTools memoizes providers.Load, which resolves through the
// on-disk cache and then the published catalog over the network. Tests that
// assert built-in launch shapes would otherwise pass or fail according to
// cache freshness and what is currently published, rather than according to
// the catalog in this worktree. Seeding the memo here — before any test runs,
// so the result does not depend on test order — makes those assertions
// deterministic and keeps the package off the network.
//
// Asserting against the embedded catalog is equivalent to asserting against
// the published one: TestRepositoryCatalogAddsRemoteProviders in
// internal/providers fails if their launch args diverge. Cache and refresh
// behavior is owned and tested by internal/providers.
func TestMain(m *testing.M) {
	runtimeCatalog = providers.EmbeddedCatalog()
	runtimeCatalogOnce.Do(func() {})
	os.Exit(m.Run())
}

func TestAgentToolRegistryMergesCustomTools(t *testing.T) {
	config := models.Config{
		AgentTools: map[string]models.AgentToolConfig{
			"cursor": {
				Executable:      "cursor-agent",
				PromptTransport: PromptTransportFile,
				RunArgs:         []string{"--cwd", "{{projectRoot}}", "--prompt-file", "{{promptFile}}", "--model", "{{profile.model}}"},
				ContractKey:     "codex",
			},
		},
	}

	tools := AvailableCLIs(config)
	if !slices.Contains(tools, "claude") || !slices.Contains(tools, "cursor") {
		t.Fatalf("AvailableCLIs() = %v, want built-ins plus cursor", tools)
	}

	plan, err := ResolveLaunchPlan(LaunchPlanRequest{
		ToolName:      "cursor",
		ProfileName:   "careful",
		ProfileVars:   map[string]string{"model": "gpt-5"},
		ProjectRoot:   "/repo",
		PromptFile:    "/tmp/prompt.md",
		RuntimeConfig: config,
	})
	if err != nil {
		t.Fatalf("ResolveLaunchPlan() error = %v", err)
	}

	want := []string{"--cwd", "/repo", "--prompt-file", "/tmp/prompt.md", "--model", "gpt-5"}
	if plan.Executable != "cursor-agent" || !slices.Equal(plan.Args, want) {
		t.Fatalf("plan = %+v, want executable cursor-agent args %v", plan, want)
	}
	if !plan.UsesPromptFile || plan.ContractKey != "codex" {
		t.Fatalf("plan = %+v, want prompt-file transport and codex contract key", plan)
	}
}

func TestAgentToolsFromCatalogsMergesLoadedOverEmbedded(t *testing.T) {
	embedded := providers.Catalog{
		Providers: []providers.Provider{
			{
				ID:      "cursor",
				Backend: "cli",
				Runtime: providers.Runtime{
					Executable:      "cursor",
					RunArgs:         []string{"--embedded"},
					ACPXSetModeArgs: []string{"embedded-set-mode"},
					ACPXPromptArgs:  []string{"embedded-prompt"},
				},
			},
			{
				ID:      "claude",
				Backend: "cli",
				Runtime: providers.Runtime{
					Executable: "claude",
					RunArgs:    []string{"-p"},
				},
			},
		},
	}
	loaded := providers.Catalog{
		Providers: []providers.Provider{
			{
				ID:      "cursor",
				Backend: "cli",
				Runtime: providers.Runtime{
					Executable:      "cursor-agent",
					RunArgs:         []string{"--loaded"},
					ACPXSetModeArgs: []string{"loaded-set-mode"},
				},
			},
			{
				ID:      "newtool",
				Backend: "cli",
				Runtime: providers.Runtime{
					Executable: "newtool",
					RunArgs:    []string{"--new"},
				},
			},
		},
	}

	registry := agentToolsFromCatalogs(embedded, loaded)

	cursor, ok := registry["cursor"]
	if !ok {
		t.Fatal("cursor tool missing from merged registry")
	}
	if cursor.Executable != "cursor-agent" {
		t.Fatalf("cursor executable = %q, want loaded catalog value", cursor.Executable)
	}
	if !slices.Equal(cursor.RunArgs, []string{"--loaded"}) {
		t.Fatalf("cursor run args = %v, want loaded catalog value", cursor.RunArgs)
	}
	if !slices.Equal(cursor.ACPXSetModeArgs, []string{"loaded-set-mode"}) {
		t.Fatalf("cursor set-mode args = %v, want loaded catalog value", cursor.ACPXSetModeArgs)
	}
	if !slices.Equal(cursor.ACPXPromptArgs, []string{"embedded-prompt"}) {
		t.Fatalf("cursor prompt args = %v, want embedded fallback value", cursor.ACPXPromptArgs)
	}

	if _, ok := registry["claude"]; !ok {
		t.Fatal("embedded-only tool missing from merged registry")
	}
	if newTool, ok := registry["newtool"]; !ok {
		t.Fatal("loaded-only tool missing from merged registry")
	} else if newTool.Executable != "newtool" {
		t.Fatalf("newtool executable = %q, want loaded catalog value", newTool.Executable)
	}
}

func TestResolveProfileAndCLIWithStructuredProfiles(t *testing.T) {
	config := models.Config{
		DefaultDoerProfile: "careful",
		DefaultCLI:         "claude",
		AgentProfiles: map[string]models.AgentProfileConfig{
			"careful": {CLI: "cursor", Vars: map[string]string{"model": "gpt-5"}},
			"cheap":   {CLI: "gemini"},
		},
	}

	profile, err := ResolveProfileForRole("", "doer", config)
	if err != nil {
		t.Fatalf("ResolveProfileForRole() error = %v", err)
	}
	if profile.Name != "careful" || profile.Vars["model"] != "gpt-5" {
		t.Fatalf("profile = %+v, want careful profile vars", profile)
	}

	cli, err := ResolveCLIWithProfile(false, "", profile, "doer", config)
	if err != nil {
		t.Fatalf("ResolveCLIWithProfile() error = %v", err)
	}
	if cli != "cursor" {
		t.Fatalf("cli = %q, want profile-selected cursor", cli)
	}

	cli, err = ResolveCLIWithProfile(true, "codex", profile, "doer", config)
	if err != nil {
		t.Fatalf("ResolveCLIWithProfile(explicit) error = %v", err)
	}
	if cli != "codex" {
		t.Fatalf("explicit cli = %q, want codex", cli)
	}
}

func TestResolveProfileForRoleRejectsUnknownProfile(t *testing.T) {
	_, err := ResolveProfileForRole("missing", "doer", models.Config{})
	if err == nil || !strings.Contains(err.Error(), "unknown agent profile") {
		t.Fatalf("error = %v, want unknown profile error", err)
	}
}

func TestResolveLaunchPlanRejectsUnknownTemplateVariable(t *testing.T) {
	config := models.Config{
		AgentTools: map[string]models.AgentToolConfig{
			"cursor": {
				RunArgs: []string{"--bad", "{{missing}}"},
			},
		},
	}

	_, err := ResolveLaunchPlan(LaunchPlanRequest{ToolName: "cursor", RuntimeConfig: config})
	if err == nil || !strings.Contains(err.Error(), "unknown template variable") {
		t.Fatalf("error = %v, want unknown template variable error", err)
	}
}

func TestBuiltInLaunchPlansPreserveCurrentArgShapes(t *testing.T) {
	tests := []struct {
		name       string
		outputsDir string
		wantExe    string
		wantArgs   []string
		wantStdin  bool
	}{
		{name: "claude", wantExe: "claude", wantArgs: []string{"-p", "--permission-mode", "auto"}, wantStdin: true},
		{name: "claude", outputsDir: "/logs", wantExe: "claude", wantArgs: []string{"-p", "--permission-mode", "auto", "--verbose", "--output-format", "stream-json"}, wantStdin: true},
		{name: "codex", wantExe: "codex", wantArgs: []string{"exec", "-"}, wantStdin: true},
		{name: "codex", outputsDir: "/logs", wantExe: "codex", wantArgs: []string{"exec", "--json", "-"}, wantStdin: true},
		{name: "opencode", wantExe: "opencode", wantArgs: []string{"run", "do it", "--dangerously-skip-permissions"}},
		{name: "opencode", outputsDir: "/logs", wantExe: "opencode", wantArgs: []string{"run", "do it", "--dangerously-skip-permissions", "--format", "json"}},
		{name: "gemini", outputsDir: "/logs", wantExe: "gemini", wantArgs: []string{"-p", "--output-format", "stream-json"}, wantStdin: true},
		{name: "mistral", outputsDir: "/logs", wantExe: "vibe", wantArgs: []string{"-p", "do it", "--output", "streaming"}},
		{name: "kimi", outputsDir: "/logs", wantExe: "kimi", wantArgs: []string{"-p", "--verbose", "--output-format", "stream-json"}, wantStdin: true},
	}

	for _, tt := range tests {
		t.Run(tt.name+"/"+tt.outputsDir, func(t *testing.T) {
			plan, err := ResolveLaunchPlan(LaunchPlanRequest{
				ToolName:      tt.name,
				Prompt:        "do it",
				OutputsDir:    tt.outputsDir,
				RuntimeConfig: models.Config{},
			})
			if err != nil {
				t.Fatalf("ResolveLaunchPlan() error = %v", err)
			}
			if plan.Executable != tt.wantExe || !slices.Equal(plan.Args, tt.wantArgs) || plan.UsesStdin != tt.wantStdin {
				t.Fatalf("plan = %+v, want exe %q args %v stdin %v", plan, tt.wantExe, tt.wantArgs, tt.wantStdin)
			}
		})
	}
}

func TestBuiltInInteractiveLaunchPlansUseInteractiveArgs(t *testing.T) {
	tests := []struct {
		name     string
		wantExe  string
		wantArgs []string
	}{
		{name: "claude", wantExe: "claude"},
		{name: "codex", wantExe: "codex"},
		{name: "opencode", wantExe: "opencode"},
		{name: "gemini", wantExe: "gemini"},
		{name: "mistral", wantExe: "vibe"},
		{name: "kimi", wantExe: "kimi"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := ResolveLaunchPlan(LaunchPlanRequest{
				ToolName:      tt.name,
				Prompt:        "do it",
				Interactive:   true,
				RuntimeConfig: models.Config{},
			})
			if err != nil {
				t.Fatalf("ResolveLaunchPlan() error = %v", err)
			}
			if plan.Executable != tt.wantExe || !slices.Equal(plan.Args, tt.wantArgs) {
				t.Fatalf("interactive plan = %+v, want exe %q args %v", plan, tt.wantExe, tt.wantArgs)
			}
		})
	}
}

func TestResolveLaunchPlanInsertsClaudeSubagentDisableBeforeLoggingFlags(t *testing.T) {
	plan, err := ResolveLaunchPlan(LaunchPlanRequest{
		ToolName:         "claude",
		OutputsDir:       "/logs",
		DisableSubagents: true,
	})
	if err != nil {
		t.Fatalf("ResolveLaunchPlan() error = %v", err)
	}

	want := []string{"-p", "--permission-mode", "auto", "--disallowedTools", "Task", "--verbose", "--output-format", "stream-json"}
	if !slices.Equal(plan.Args, want) {
		t.Fatalf("args = %v, want %v", plan.Args, want)
	}
}

func TestAppendDisallowedTaskArgInsertsBeforeFirstLoggingFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "empty args appends at end",
			args: nil,
			want: []string{"--disallowedTools", "Task"},
		},
		{
			name: "no logging flags appends at end",
			args: []string{"-p", "--model", "sonnet"},
			want: []string{"-p", "--model", "sonnet", "--disallowedTools", "Task"},
		},
		{
			name: "verbose at start",
			args: []string{"--verbose"},
			want: []string{"--disallowedTools", "Task", "--verbose"},
		},
		{
			name: "output-format only, preceded by other args",
			args: []string{"-p", "--model", "sonnet", "--output-format", "stream-json"},
			want: []string{"-p", "--model", "sonnet", "--disallowedTools", "Task", "--output-format", "stream-json"},
		},
		{
			name: "output-format appears before verbose, first occurrence wins",
			args: []string{"-p", "--output-format", "stream-json", "--verbose"},
			want: []string{"-p", "--disallowedTools", "Task", "--output-format", "stream-json", "--verbose"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := appendDisallowedTaskArg(tt.args)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("appendDisallowedTaskArg(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}
