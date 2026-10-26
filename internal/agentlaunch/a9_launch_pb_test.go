package agentlaunch

import (
	"slices"
	"testing"

	"github.com/liza-mas/liza/internal/models"
)

// A9 preserved-behaviour (PB) tests for the launch capability, written on the
// tree before the agentlaunch extraction and moved with the code. They observe
// only exported entry points, so the extraction cannot change what they assert.

// PB-1: the effective registry merges catalog and agent_tools override, falls
// back to the tool name for an unknown tool, and keeps ProviderKey and
// ContractKey distinct from each other and from the tool name.
func TestA9EffectiveRegistryKeys(t *testing.T) {
	registry := AgentToolRegistry(models.Config{AgentTools: map[string]models.AgentToolConfig{
		"zz-custom": {Executable: "zz-bin"},
		"zz-keyed":  {ProviderKey: "shared", ContractKey: "codex"},
		"claude":    {ContractKey: "override-contract"},
		"  ":        {Executable: "ignored"},
	}})

	custom := registry["zz-custom"]
	if custom.ProviderKey != "zz-custom" || custom.ContractKey != "" || custom.Backend != ToolBackendCLI || custom.Executable != "zz-bin" || custom.PromptTransport != PromptTransportStdin {
		t.Fatalf("custom tool fell back to %+v, want tool-name provider key, cli backend and stdin transport", custom)
	}
	if keyed := registry["zz-keyed"]; keyed.ProviderKey != "shared" || keyed.ContractKey != "codex" || keyed.Executable != "zz-keyed" {
		t.Fatalf("declared keys = %+v", keyed)
	}
	if claude := registry["claude"]; claude.ProviderKey != "claude" || claude.ContractKey != "override-contract" || len(claude.RunArgs) == 0 {
		t.Fatalf("override lost the catalog entry: %+v", claude)
	}
	if _, blank := registry["  "]; blank {
		t.Fatal("a blank tool name must be skipped")
	}
	for tool, want := range map[string][2]string{
		"codex-acp":  {"codex", "codex"},
		"cursor-acp": {"cursor", "codex"},
		"kimi":       {"kimi", "claude"},
	} {
		if got := registry[tool]; got.ProviderKey != want[0] || got.ContractKey != want[1] {
			t.Fatalf("%s keys = (%s, %s), want %v", tool, got.ProviderKey, got.ContractKey, want)
		}
	}

	// The registry is rebuilt per call: a caller's edit never reaches the next one.
	delete(registry, "claude")
	if _, ok := AgentToolRegistry(models.Config{})["claude"]; !ok {
		t.Fatal("editing a returned registry changed a later one")
	}
}

// PB-2: a returned plan, profile or registry shares no slice or map with the
// request, a second resolution or the registry.
func TestA9LaunchPlanDoesNotAliasInputs(t *testing.T) {
	config := models.Config{
		AgentTools: map[string]models.AgentToolConfig{
			"zz-custom": {
				Executable:          "zz-bin",
				RunArgs:             []string{"--root", "{{projectRoot}}"},
				EnvFiles:            []string{"a.env"},
				RequiredExecutables: []string{"zz-bin"},
				ModelArgs:           []string{"--model", "{{model}}"},
			},
		},
		AgentProfiles: map[string]models.AgentProfileConfig{"p": {Vars: map[string]string{"k": "v"}}},
	}
	req := LaunchPlanRequest{ToolName: "zz-custom", Model: "m1", ProjectRoot: "/repo", RuntimeConfig: config, ProfileVars: map[string]string{"k": "v"}}

	first, err := ResolveLaunchPlan(req)
	if err != nil {
		t.Fatalf("ResolveLaunchPlan() error = %v", err)
	}
	wantArgs := []string{"--model", "m1", "--root", "/repo"}
	if !slices.Equal(first.Args, wantArgs) || first.OptionalEnvFiles {
		t.Fatalf("plan = %+v", first)
	}
	for i := range first.Args {
		first.Args[i] = "mutated"
	}
	first.EnvFiles[0] = "mutated"
	first.RequiredExecutables[0] = "mutated"

	second, err := ResolveLaunchPlan(req)
	if err != nil {
		t.Fatalf("second ResolveLaunchPlan() error = %v", err)
	}
	if !slices.Equal(second.Args, wantArgs) || second.EnvFiles[0] != "a.env" || second.RequiredExecutables[0] != "zz-bin" {
		t.Fatalf("a mutated plan leaked into a later one: %+v", second)
	}
	tool := config.AgentTools["zz-custom"]
	if tool.RunArgs[1] != "{{projectRoot}}" || tool.EnvFiles[0] != "a.env" || tool.RequiredExecutables[0] != "zz-bin" || tool.ModelArgs[1] != "{{model}}" {
		t.Fatalf("a mutated plan changed the request config: %+v", tool)
	}
	if registry := AgentToolRegistry(config)["zz-custom"]; registry.RunArgs[1] != "{{projectRoot}}" || registry.EnvFiles[0] != "a.env" {
		t.Fatalf("a mutated plan changed the registry: %+v", registry)
	}

	profile, err := ResolveProfileForRole("p", "doer", config)
	if err != nil {
		t.Fatalf("ResolveProfileForRole() error = %v", err)
	}
	profile.Vars["k"] = "mutated"
	if config.AgentProfiles["p"].Vars["k"] != "v" {
		t.Fatal("a mutated resolved profile changed the configured profile")
	}
}

// PB-3: error text and order. Each failure is observed alone, then a request
// with two failures reports the earlier stage.
func TestA9LaunchPlanErrorOrder(t *testing.T) {
	tools := func(tool models.AgentToolConfig) models.Config {
		return models.Config{AgentTools: map[string]models.AgentToolConfig{"zz-custom": tool}}
	}
	for _, tt := range []struct {
		name string
		req  LaunchPlanRequest
		want string
	}{
		{"missing", LaunchPlanRequest{ToolName: "  "}, "missing CLI name"},
		{"unknown tool", LaunchPlanRequest{ToolName: "no-such-tool"}, "unknown CLI: no-such-tool"},
		{"invalid transport", LaunchPlanRequest{ToolName: "zz-custom", RuntimeConfig: tools(models.AgentToolConfig{PromptTransport: "pigeon"})}, "unsupported prompt transport: pigeon"},
		{"unknown backend", LaunchPlanRequest{ToolName: "zz-custom", RuntimeConfig: tools(models.AgentToolConfig{Backend: "weird"})}, "unsupported backend for zz-custom: weird"},
		{"model unsupported", LaunchPlanRequest{ToolName: "zz-custom", Model: "m", RuntimeConfig: tools(models.AgentToolConfig{})}, "zz-custom does not support model selection"},
		{"unknown variable", LaunchPlanRequest{ToolName: "zz-custom", RuntimeConfig: tools(models.AgentToolConfig{RunArgs: []string{"{{nosuch}}"}})}, "zz-custom args: unknown template variable(s): nosuch"},
		{"transport before variable", LaunchPlanRequest{ToolName: "zz-custom", RuntimeConfig: tools(models.AgentToolConfig{PromptTransport: "pigeon", RunArgs: []string{"{{nosuch}}"}})}, "unsupported prompt transport: pigeon"},
		{"backend before model", LaunchPlanRequest{ToolName: "zz-custom", Model: "m", RuntimeConfig: tools(models.AgentToolConfig{Backend: "weird"})}, "unsupported backend for zz-custom: weird"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ResolveLaunchPlan(tt.req)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("ResolveLaunchPlan() error = %v, want %q", err, tt.want)
			}
		})
	}
}
