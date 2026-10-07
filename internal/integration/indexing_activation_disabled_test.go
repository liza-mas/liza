package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"

	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/functionalclusters"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/scipsearch"
	"github.com/liza-mas/liza/internal/semble"
	"github.com/liza-mas/liza/internal/stacklit"
)

func TestIndexingActivationFreshSetupInstallsGenericOptionalIndexGuidance(t *testing.T) {
	targetDir := t.TempDir()

	if err := commands.SetupCommand(commands.SetupParams{TargetDir: targetDir}); err != nil {
		t.Fatalf("SetupCommand(): %v", err)
	}

	content, err := os.ReadFile(filepath.Join(targetDir, "AGENT_TOOLS.md"))
	if err != nil {
		t.Fatalf("ReadFile(AGENT_TOOLS.md): %v", err)
	}

	text := string(content)
	assertIndexingActivationContainsAll(t, text,
		"Supplied Index/Search Command Shapes",
		"concrete Liza-supplied values",
		"Use the shell-quoted value when one is provided",
		"stacklit derive --ai-summary -i <index-path>",
		"scip-search symbols --index <index-path>",
		"scip-search packages --index <index-path>",
		"scip-search impact --index <index-path>",
		"disabled, unavailable, or not advertised",
		"fall back to `rg`, `ast-grep`, direct reads",
		"Morph MCP only when policy exposes it",
	)
	assertIndexingActivationContainsNone(t, text,
		"/home/",
		".worktrees/",
		paths.ProjectDirName()+"/scip/",
		"<task-worktree-path>",
	)
}

func TestIndexingActivationDisabledPairingInitLeavesNoOptionalIndexingHooks(t *testing.T) {
	disableOptionalIndexingForTest(t)
	projectDir := newIndexingActivationProject(t)

	if err := commands.InitPairingCommand(commands.InitPairingParams{
		Agents:         []string{"claude", "codex"},
		ScipSearch:     []string{"go"},
		Stdin:          strings.NewReader(""),
		ContractAction: "global",
	}); err != nil {
		t.Fatalf("InitPairingCommand(): %v", err)
	}

	assertNoOptionalIndexHook(t, projectDir)
}

func TestIndexingActivationDisabledSessionStartOmitsOptionalCommandBlocksWithStaleArtifacts(t *testing.T) {
	disableOptionalIndexingForTest(t)
	projectDir := newIndexingActivationProject(t)

	if err := commands.InitPairingCommand(commands.InitPairingParams{
		Agents:         []string{"claude"},
		ScipSearch:     []string{"go"},
		Stdin:          strings.NewReader(""),
		ContractAction: "global",
	}); err != nil {
		t.Fatalf("InitPairingCommand(): %v", err)
	}
	writeIndexingActivationFile(t, filepath.Join(projectDir, "stacklit.json"), `{"project":{"name":"stale"}}`)
	writeIndexingActivationFile(t, filepath.Join(projectDir, "go.scip"), "stale go index")
	writeIndexingActivationFile(t, filepath.Join(projectDir, ".sembleignore"), semble.DefaultIgnorePayload())

	output := runSessionStartContextHook(t, projectDir)

	assertIndexingActivationContainsAll(t, output,
		"SessionStart",
		"MANDATORY: Read ~/"+paths.GlobalDirName()+"/CORE.md",
	)
	assertIndexingActivationContainsNone(t, output, optionalIndexCommandBlocks()...)
}

func TestIndexingActivationDisabledMASPromptOmitsOptionalSectionsWithStaleArtifactsAndScipConfig(t *testing.T) {
	projectRoot := t.TempDir()
	prompt := buildDisabledOptionalIndexPrompt(t, projectRoot)

	assertIndexingActivationContainsAll(t, prompt,
		"Indexing activation",
		"task-1",
	)
	assertIndexingActivationContainsNone(t, prompt, append(optionalIndexCommandBlocks(),
		"=== STACKLIT INDEX ===",
		"=== SCIP-SEARCH INDEXES ===",
		"=== FUNCTIONAL CLUSTERS ===",
		"=== SEMBLE SEARCH ===",
	)...)
}

func assertNoOptionalIndexHook(t *testing.T, projectDir string) {
	t.Helper()

	for _, rel := range []string{
		filepath.Join(".git", "hooks", brand.BinaryName+"-index.sh"),
		filepath.Join(".git", "hooks", "post-commit"),
		filepath.Join(".git", "hooks", "post-checkout"),
		filepath.Join(".git", "hooks", "post-merge"),
		filepath.Join(".git", "hooks", "post-rewrite"),
	} {
		path := filepath.Join(projectDir, rel)
		content, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatalf("ReadFile(%q): %v", path, err)
		}
		if strings.Contains(string(content), "liza-index") {
			t.Fatalf("disabled optional indexing wrote hook content in %s:\n%s", rel, string(content))
		}
	}
}

func optionalIndexCommandBlocks() []string {
	return []string{
		"stacklit derive",
		"stacklit find-module",
		"stacklit get-module",
		"scip-search symbols",
		"scip-search references",
		"functional-clusters list",
		"functional-clusters explain",
		"semble search",
		"semble find-related",
		stacklit.EnvEnableStacklit + "=true",
		scipsearch.EnvEnableScipSearch + "=true",
		functionalclusters.EnvEnableFunctionalClusters + "=true",
		semble.EnvEnableSemble + "=true",
	}
}
