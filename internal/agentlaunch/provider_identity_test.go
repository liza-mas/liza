package agentlaunch

import (
	"maps"
	"reflect"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/providers"
)

// C2 P1: the effective provider key is the catalog's provider_key, overridden
// by agent_tools, falling back to the tool name; it is derived per evaluation
// from supplied inputs and never persisted.

func TestC2ProviderKeyBuiltInAliasesCollapse(t *testing.T) {
	keys := NewProviderKeys(BuiltInAgentTools(), models.Config{})

	if got, want := keys.Key("codex-acp"), keys.Key("codex"); got != want || got != "codex" {
		t.Fatalf("Key(codex-acp)=%q Key(codex)=%q, want one shared key %q", got, want, "codex")
	}
	if !keys.Same("codex", "codex-acp") {
		t.Fatal("codex and codex-acp must be the same provider")
	}
	if keys.Same("codex", "claude") {
		t.Fatal("codex and claude must stay distinct providers")
	}
}

func TestC2ProviderKeyAgentToolsOverrideWinsOnBuiltIn(t *testing.T) {
	keys := NewProviderKeys(BuiltInAgentTools(), models.Config{AgentTools: map[string]models.AgentToolConfig{
		"codex-acp": {ProviderKey: "acp-lane"},
	}})

	if got := keys.Key("codex-acp"); got != "acp-lane" {
		t.Fatalf("Key(codex-acp) = %q, want the agent_tools override", got)
	}
	if keys.Same("codex", "codex-acp") {
		t.Fatal("an override that splits the alias must stop them counting as one provider")
	}
	if got := keys.Key("codex"); got != "codex" {
		t.Fatalf("Key(codex) = %q, want the override to touch only its own tool", got)
	}
}

func TestC2ProviderKeyCustomToolsShareDeclaredKey(t *testing.T) {
	keys := NewProviderKeys(BuiltInAgentTools(), models.Config{AgentTools: map[string]models.AgentToolConfig{
		"zz-one":     {Executable: "one", ProviderKey: "shared"},
		"zz-two":     {Executable: "two", ProviderKey: "shared"},
		"zz-keyless": {Executable: "three"},
		"zz-other":   {Executable: "four"},
	}})

	if !keys.Same("zz-one", "zz-two") {
		t.Fatal("custom tools declaring one provider_key must be one provider")
	}
	if keys.Same("zz-keyless", "zz-other") {
		t.Fatal("keyless custom tools must stay distinct, each keyed by its own name")
	}
	if got := keys.Key("zz-keyless"); got != "zz-keyless" {
		t.Fatalf("Key(zz-keyless) = %q, want the tool name", got)
	}
}

func TestC2ProviderKeyUnknownToolFallsBackToName(t *testing.T) {
	keys := NewProviderKeys(BuiltInAgentTools(), models.Config{})

	if got := keys.Key("retired-tool"); got != "retired-tool" {
		t.Fatalf("Key(unregistered) = %q, want the tool name", got)
	}
	if keys.Same("retired-tool", "other-retired-tool") {
		t.Fatal("unregistered tools must stay distinct")
	}
	if got := (ProviderKeys{}).Key("codex-acp"); got != "codex-acp" {
		t.Fatalf("zero-value Key(codex-acp) = %q, want exact tool-name comparison", got)
	}
}

func TestC2ProviderKeyEmptyAndWhitespaceMatchLaunch(t *testing.T) {
	config := models.Config{AgentTools: map[string]models.AgentToolConfig{
		"zz-blank":  {Executable: "blank", ProviderKey: "   "},
		"zz-padded": {Executable: "padded", ProviderKey: "  shared  "},
		"zz-peer":   {Executable: "peer", ProviderKey: "shared"},
	}}
	keys := NewProviderKeys(BuiltInAgentTools(), config)

	if got := keys.Key(""); got != "" {
		t.Fatalf("Key(empty) = %q, want empty", got)
	}
	if got := keys.Key("   "); got != "" {
		t.Fatalf("Key(whitespace) = %q, want empty", got)
	}
	if !keys.Same("zz-padded", "zz-peer") {
		t.Fatal("a padded provider_key must trim to its peer's key")
	}
	if got := keys.Key("  codex-acp  "); got != "codex" {
		t.Fatalf("Key(padded recorded tool) = %q, want the trimmed tool's key", got)
	}
	for _, tool := range []string{"codex", "codex-acp", "claude", "zz-blank", "zz-padded", "zz-peer"} {
		plan, err := ResolveLaunchPlan(LaunchPlanRequest{ToolName: tool, RuntimeConfig: config})
		if err != nil {
			t.Fatalf("ResolveLaunchPlan(%s): %v", tool, err)
		}
		if got := keys.Key(tool); got != plan.ProviderKey {
			t.Errorf("Key(%s) = %q, launch ProviderKey = %q", tool, got, plan.ProviderKey)
		}
	}
}

func TestC2ProviderKeyHonoursLoadedCatalogKey(t *testing.T) {
	t.Cleanup(UseRuntimeCatalog(providers.Catalog{Providers: []providers.Provider{{
		ID: "codex-acp", Backend: providers.BackendACPX,
		Runtime: providers.Runtime{ProviderKey: "loaded-lane"},
	}, {
		ID: "zz-loaded", Backend: providers.BackendCLI,
		Runtime: providers.Runtime{Executable: "zz-loaded", ProviderKey: "loaded-lane"},
	}}}))
	keys := NewProviderKeys(BuiltInAgentTools(), models.Config{})

	if got := keys.Key("codex-acp"); got != "loaded-lane" {
		t.Fatalf("Key(codex-acp) = %q, want the loaded catalog's provider_key, not the embedded one", got)
	}
	if !keys.Same("codex-acp", "zz-loaded") {
		t.Fatal("a tool only the loaded catalog knows must share its key")
	}
	if keys.Same("codex", "codex-acp") {
		t.Fatal("the loaded catalog moved codex-acp off codex's key")
	}
}

func TestC2ProviderKeyDoesNotModifyBase(t *testing.T) {
	base := BuiltInAgentTools()
	before := maps.Clone(base)
	config := models.Config{AgentTools: map[string]models.AgentToolConfig{
		"codex":  {ProviderKey: "changed"},
		"zz-new": {ProviderKey: "new"},
	}}

	keys := NewProviderKeys(base, config)

	if !reflect.DeepEqual(base, before) {
		t.Fatal("NewProviderKeys modified the captured base registry")
	}
	if keys.Key("codex") != "changed" || keys.Key("zz-new") != "new" {
		t.Fatalf("overrides not applied: codex=%q zz-new=%q", keys.Key("codex"), keys.Key("zz-new"))
	}
	// The same base serves a later evaluation under another config.
	if got := NewProviderKeys(base, models.Config{}).Key("codex"); got != "codex" {
		t.Fatalf("a second derivation from the shared base saw the first config: %q", got)
	}
	if got := NewProviderKeys(nil, config).Key("zz-new"); got != "new" {
		t.Fatalf("nil base: Key(zz-new) = %q", got)
	}
}
