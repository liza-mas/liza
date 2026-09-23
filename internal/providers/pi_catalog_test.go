package providers

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestEmbeddedCatalogIncludesPiProvider(t *testing.T) {
	cat := EmbeddedCatalog()
	provider, ok := cat.Resolve("pi")
	if !ok {
		t.Fatal("embedded catalog does not resolve provider id pi")
	}

	if provider.DisplayName != "Pi" || provider.Backend != "cli" {
		t.Fatalf("provider = %+v, want display name Pi with cli backend", provider)
	}
	if !slices.Equal(provider.Detection.Binaries, []string{"pi"}) {
		t.Fatalf("detection binaries = %v, want [pi]", provider.Detection.Binaries)
	}

	contract := provider.Setup.Contract
	if contract.RepoFile != "AGENTS.md" {
		t.Fatalf("contract repo file = %q, want AGENTS.md (pi auto-discovers it)", contract.RepoFile)
	}
	if contract.GlobalFallback != ".pi/agent/AGENTS.md" {
		t.Fatalf("contract global fallback = %q, want .pi/agent/AGENTS.md", contract.GlobalFallback)
	}
	if contract.PreferGlobal == nil || !*contract.PreferGlobal {
		t.Fatalf("contract prefer_global = %v, want true", contract.PreferGlobal)
	}

	if !provider.Setup.ActivationAssets.PiExtension {
		t.Fatal("activation assets should request the pi_extension init gate")
	}

	runtime := provider.Runtime
	if runtime.Executable != "pi" || runtime.PromptTransport != "stdin" {
		t.Fatalf("runtime = %+v, want pi executable with stdin prompt transport", runtime)
	}
	wantRunArgs := []string{"-p", "-e", "{{globalDir}}/extensions/init-gate.ts"}
	if !slices.Equal(runtime.RunArgs, wantRunArgs) {
		t.Fatalf("run args = %v, want %v", runtime.RunArgs, wantRunArgs)
	}
	wantLoggedArgs := []string{"-p", "--mode", "json", "-e", "{{globalDir}}/extensions/init-gate.ts"}
	if !slices.Equal(runtime.LoggedRunArgs, wantLoggedArgs) {
		t.Fatalf("logged run args = %v, want %v", runtime.LoggedRunArgs, wantLoggedArgs)
	}
	if !slices.Equal(runtime.EnvFiles, []string{"pi.env"}) {
		t.Fatalf("env files = %v, want [pi.env]", runtime.EnvFiles)
	}
	if runtime.ContractKey != "pi" {
		t.Fatalf("contract key = %q, want pi", runtime.ContractKey)
	}
}

// TestPublishedCatalogOmitsPiUntilReleased guards catalog version skew.
// provider-catalog.yaml is fetched by already-installed binaries, whose strict
// decoder (KnownFields) rejects the whole file on any unknown field and then
// silently falls back to the cache or the embedded catalog. pi depends on the
// pi_extension activation asset, which released binaries do not know, so pi
// ships embedded-only until the oldest supported binary understands the field.
func TestPublishedCatalogOmitsPiUntilReleased(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "provider-catalog.yaml"))
	if err != nil {
		t.Fatalf("read provider-catalog.yaml: %v", err)
	}
	if bytes.Contains(data, []byte("pi_extension")) {
		t.Fatal("provider-catalog.yaml must not publish pi_extension: released binaries reject the catalog on unknown fields")
	}
	cat, err := ParseCatalog(data)
	if err != nil {
		t.Fatalf("ParseCatalog(provider-catalog.yaml) error = %v", err)
	}
	if _, ok := cat.Resolve("pi"); ok {
		t.Fatal("provider-catalog.yaml must not publish pi yet; it resolves from the embedded catalog")
	}
}

func TestWithEmbeddedBuiltinsBackfillsEmbeddedOnlyProviders(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "provider-catalog.yaml"))
	if err != nil {
		t.Fatalf("read provider-catalog.yaml: %v", err)
	}
	published, err := ParseCatalog(data)
	if err != nil {
		t.Fatalf("ParseCatalog(provider-catalog.yaml) error = %v", err)
	}
	if _, ok := published.Resolve("pi"); ok {
		t.Fatal("precondition: published catalog should not define pi")
	}

	merged := WithEmbeddedBuiltins(published)
	pi, ok := merged.Resolve("pi")
	if !ok || !pi.Setup.ActivationAssets.PiExtension {
		t.Fatalf("merged Resolve(pi) = %+v, %v; want embedded pi provider", pi, ok)
	}
	for _, p := range published.Providers {
		got, ok := merged.Resolve(p.ID)
		if !ok || got.Runtime.Executable != p.Runtime.Executable || !slices.Equal(got.Runtime.RunArgs, p.Runtime.RunArgs) {
			t.Errorf("merged %s = %+v, want the published definition to win", p.ID, got.Runtime)
		}
	}
	if len(merged.Providers) <= len(published.Providers) {
		t.Fatalf("merged providers = %d, want more than published %d", len(merged.Providers), len(published.Providers))
	}

	// A collision skips only the colliding embedded provider. The fetched
	// catalog defines an explicit devin-acp (colliding with embedded devin's
	// synthesized ACP id) and an id qwen-code (colliding with embedded qwen's
	// alias): devin and qwen are skipped, the other built-ins still merge.
	base, ok := published.Resolve("claude")
	if !ok {
		t.Fatal("published catalog missing claude")
	}
	fetchedAs := func(id string) Provider {
		p := base
		p.ID, p.Aliases, p.ACPRuntime = id, nil, nil
		return p
	}
	colliding := Catalog{Version: 2, Providers: []Provider{base, fetchedAs("devin-acp"), fetchedAs("qwen-code")}}
	if err := colliding.Validate(); err != nil {
		t.Fatalf("colliding catalog invalid: %v", err)
	}
	partial := WithEmbeddedBuiltins(colliding)
	for _, skipped := range []string{"devin", "qwen"} {
		if _, ok := partial.Resolve(skipped); ok {
			t.Errorf("Resolve(%s) succeeded; want it skipped for colliding with the fetched catalog", skipped)
		}
	}
	for _, kept := range []string{"pi", "codex", "kimi"} {
		if _, ok := partial.Resolve(kept); !ok {
			t.Errorf("Resolve(%s) failed; non-colliding embedded providers must still be backfilled", kept)
		}
	}
	for _, id := range []string{"devin-acp", "qwen-code"} {
		if got, _ := partial.Resolve(id); got.Runtime.Executable != base.Runtime.Executable {
			t.Errorf("Resolve(%s) = %+v, want the fetched definition", id, got.Runtime)
		}
	}

	embedded := EmbeddedCatalog()
	if again := WithEmbeddedBuiltins(embedded); len(again.Providers) != len(embedded.Providers) {
		t.Fatalf("WithEmbeddedBuiltins(embedded) providers = %d, want unchanged %d", len(again.Providers), len(embedded.Providers))
	}
}
