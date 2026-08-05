package interactive

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/providers"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestDetectContractConflicts_PreferredGlobalPathAvailable(t *testing.T) {
	projectRoot := t.TempDir()
	homeDir := t.TempDir()
	contractTarget := filepath.Join(homeDir, paths.GlobalDirName(), "CORE.md")
	if err := os.WriteFile(filepath.Join(projectRoot, "CLAUDE.md"), []byte("user-owned\n"), 0644); err != nil {
		t.Fatal(err)
	}

	conflicts := DetectContractConflicts(projectRoot, homeDir, []providers.Provider{
		contractTestProvider("claude", "CLAUDE.md", ".claude/CLAUDE.md", true),
	}, contractTarget)
	if len(conflicts) != 0 {
		t.Fatalf("DetectContractConflicts() = %+v, want no conflict", conflicts)
	}
}

func TestDetectContractConflicts_PreferredGlobalPathUnavailable(t *testing.T) {
	projectRoot := t.TempDir()
	homeDir := t.TempDir()
	contractTarget := filepath.Join(homeDir, paths.GlobalDirName(), "CORE.md")
	if err := os.WriteFile(filepath.Join(projectRoot, "CLAUDE.md"), []byte("repo instructions\n"), 0644); err != nil {
		t.Fatal(err)
	}
	globalPath := filepath.Join(homeDir, ".claude", "CLAUDE.md")
	if err := os.MkdirAll(filepath.Dir(globalPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(globalPath, []byte("global instructions\n"), 0644); err != nil {
		t.Fatal(err)
	}

	conflicts := DetectContractConflicts(projectRoot, homeDir, []providers.Provider{
		contractTestProvider("claude", "CLAUDE.md", ".claude/CLAUDE.md", true),
	}, contractTarget)
	if len(conflicts) != 1 || len(conflicts[0].Providers) != 1 || conflicts[0].Providers[0].ID != "claude" || conflicts[0].FileName != "CLAUDE.md" {
		t.Fatalf("DetectContractConflicts() = %+v, want Claude conflict", conflicts)
	}
	if got, want := contractConflictActions(conflicts[0]), []string{"rename", "skip"}; !slices.Equal(got, want) {
		t.Fatalf("occupied global actions = %v, want %v", got, want)
	}
}

func TestDetectContractConflicts_RepoOnlyProvider(t *testing.T) {
	projectRoot := t.TempDir()
	homeDir := t.TempDir()
	contractTarget := filepath.Join(homeDir, paths.GlobalDirName(), "CORE.md")
	if err := os.WriteFile(filepath.Join(projectRoot, "AGENTS.md"), []byte("user-owned\n"), 0644); err != nil {
		t.Fatal(err)
	}

	conflicts := DetectContractConflicts(projectRoot, homeDir, []providers.Provider{
		contractTestProvider("cursor", "AGENTS.md", "", false),
	}, contractTarget)
	if len(conflicts) != 1 || len(conflicts[0].Providers) != 1 || conflicts[0].Providers[0].ID != "cursor" {
		t.Fatalf("DetectContractConflicts() = %+v, want Cursor conflict", conflicts)
	}
}

func TestDetectContractConflicts_ManagedRepoSymlink(t *testing.T) {
	testhelpers.RequireSymlinkCapability(t)

	projectRoot := t.TempDir()
	homeDir := t.TempDir()
	contractTarget := filepath.Join(homeDir, paths.GlobalDirName(), "CORE.md")
	if err := os.Symlink(contractTarget, filepath.Join(projectRoot, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}

	conflicts := DetectContractConflicts(projectRoot, homeDir, []providers.Provider{
		contractTestProvider("cursor", "AGENTS.md", "", false),
	}, contractTarget)
	if len(conflicts) != 0 {
		t.Fatalf("DetectContractConflicts() = %+v, want no conflict", conflicts)
	}
}

func TestDetectContractConflicts_UnresolvablePreferredGlobalPath(t *testing.T) {
	projectRoot := t.TempDir()
	homeDir := t.TempDir()
	contractTarget := filepath.Join(homeDir, paths.GlobalDirName(), "CORE.md")
	t.Setenv("CLAUDE_CONFIG_DIR", "~/.claude")
	if err := os.WriteFile(filepath.Join(projectRoot, "CLAUDE.md"), []byte("user-owned\n"), 0644); err != nil {
		t.Fatal(err)
	}
	provider := contractTestProvider("claude", "CLAUDE.md", ".claude/CLAUDE.md", true)
	provider.Setup.Contract.GlobalFallbackEnv = "CLAUDE_CONFIG_DIR"
	provider.Setup.Contract.GlobalFallbackEnvSuffix = "CLAUDE.md"

	conflicts := DetectContractConflicts(projectRoot, homeDir, []providers.Provider{provider}, contractTarget)
	if len(conflicts) != 1 || len(conflicts[0].Providers) != 1 || conflicts[0].Providers[0].ID != "claude" {
		t.Fatalf("DetectContractConflicts() = %+v, want Claude conflict", conflicts)
	}
	if got, want := contractConflictActions(conflicts[0]), []string{"rename", "skip"}; !slices.Equal(got, want) {
		t.Fatalf("unresolvable global actions = %v, want %v", got, want)
	}
}

func TestDetectContractConflicts_OccupiedLocalFallbackIsUnavailable(t *testing.T) {
	projectRoot := t.TempDir()
	homeDir := t.TempDir()
	contractTarget := filepath.Join(homeDir, paths.GlobalDirName(), "CORE.md")
	for path, content := range map[string]string{
		filepath.Join(projectRoot, "CLAUDE.md"):        "repo instructions\n",
		filepath.Join(projectRoot, "CLAUDE.local.md"):  "local instructions\n",
		filepath.Join(homeDir, ".claude", "CLAUDE.md"): "global instructions\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	provider := contractTestProvider("claude", "CLAUDE.md", ".claude/CLAUDE.md", true)
	provider.Setup.Contract.LocalFallback = "CLAUDE.local.md"

	conflicts := DetectContractConflicts(projectRoot, homeDir, []providers.Provider{provider}, contractTarget)
	if len(conflicts) != 1 {
		t.Fatalf("DetectContractConflicts() = %+v, want Claude conflict", conflicts)
	}
	if got, want := contractConflictActions(conflicts[0]), []string{"rename", "skip"}; !slices.Equal(got, want) {
		t.Fatalf("occupied fallback actions = %v, want %v", got, want)
	}
}

func TestDetectContractConflicts_EmptyProjectRoot(t *testing.T) {
	conflicts := DetectContractConflicts("", t.TempDir(), []providers.Provider{
		contractTestProvider("claude", "CLAUDE.md", ".claude/CLAUDE.md", true),
	}, filepath.Join(t.TempDir(), "CORE.md"))
	if len(conflicts) != 0 {
		t.Fatalf("DetectContractConflicts() = %+v, want no conflict", conflicts)
	}
}

func TestDetectContractConflicts_GroupsProvidersSharingRepoPath(t *testing.T) {
	projectRoot := t.TempDir()
	homeDir := t.TempDir()
	contractTarget := filepath.Join(homeDir, paths.GlobalDirName(), "CORE.md")
	if err := os.WriteFile(filepath.Join(projectRoot, "AGENTS.md"), []byte("user-owned\n"), 0644); err != nil {
		t.Fatal(err)
	}

	custom := contractTestProvider("custom", "./AGENTS.md", ".custom/AGENTS.md", false)
	custom.Setup.Contract.LocalFallback = "AGENTS.local.md"
	conflicts := DetectContractConflicts(projectRoot, homeDir, []providers.Provider{
		contractTestProvider("cursor", "AGENTS.md", "", false), custom,
	}, contractTarget)
	if len(conflicts) != 1 {
		t.Fatalf("DetectContractConflicts() = %+v, want one shared-path conflict", conflicts)
	}
	gotIDs := []string{conflicts[0].Providers[0].ID, conflicts[0].Providers[1].ID}
	if want := []string{"cursor", "custom"}; !slices.Equal(gotIDs, want) {
		t.Fatalf("conflict provider IDs = %v, want %v", gotIDs, want)
	}
	if got, want := conflicts[0].RepoPath, filepath.Join(projectRoot, "AGENTS.md"); got != want {
		t.Fatalf("conflict repo path = %q, want %q", got, want)
	}
	if got, want := contractConflictActions(conflicts[0]), []string{"rename", "skip"}; !slices.Equal(got, want) {
		t.Fatalf("shared-path actions = %v, want %v", got, want)
	}
}

func TestContractConflictActionsFollowDestinationAvailability(t *testing.T) {
	tests := []struct {
		name     string
		conflict ContractConflict
		want     []string
	}{
		{name: "no fallback", conflict: ContractConflict{}, want: []string{"rename", "skip"}},
		{name: "global and local", conflict: ContractConflict{GlobalAvailable: true, LocalAvailable: true}, want: []string{"global", "rename", "local", "skip"}},
		{name: "local only", conflict: ContractConflict{LocalAvailable: true}, want: []string{"rename", "local", "skip"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := contractConflictActions(tt.conflict)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("contractConflictActions() = %v, want %v", got, tt.want)
			}
		})
	}
}

func contractTestProvider(id, repoFile, globalFallback string, preferGlobal bool) providers.Provider {
	return providers.Provider{
		ID: id,
		Setup: providers.Setup{Contract: providers.ContractLinks{
			RepoFile:       repoFile,
			GlobalFallback: globalFallback,
			PreferGlobal:   &preferGlobal,
		}},
	}
}
