package providers

import (
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
	wantRunArgs := []string{"-p", "-e", "{{globalDir}}/extensions/liza-init-gate.ts"}
	if !slices.Equal(runtime.RunArgs, wantRunArgs) {
		t.Fatalf("run args = %v, want %v", runtime.RunArgs, wantRunArgs)
	}
	wantLoggedArgs := []string{"-p", "--mode", "json", "-e", "{{globalDir}}/extensions/liza-init-gate.ts"}
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
