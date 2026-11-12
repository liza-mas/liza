package commands

import (
	"os"
	"path/filepath"
	"testing"
)

// loadedContractHome returns a home holding CORE.md, with the provider
// configuration roots pointed back at their defaults.
func loadedContractHome(t *testing.T) string {
	t.Helper()
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	home := t.TempDir()
	core := globalContractTarget(home)
	if err := os.MkdirAll(filepath.Dir(core), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(core, []byte("# core\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return home
}

func symlinkContract(t *testing.T, path, target string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

func assertLoadedContract(t *testing.T, home, projectRoot, provider, want string) {
	t.Helper()
	got, err := LoadedContract(home, projectRoot, provider)
	if err != nil {
		t.Fatalf("LoadedContract(%s): %v", provider, err)
	}
	if got != want {
		t.Fatalf("LoadedContract(%s) = %q, want %q", provider, got, want)
	}
}

func TestLoadedContractGlobalLink(t *testing.T) {
	home := loadedContractHome(t)
	assertLoadedContract(t, home, "", "claude", "")
	link := filepath.Join(home, ".claude", "CLAUDE.md")
	symlinkContract(t, link, globalContractTarget(home))
	assertLoadedContract(t, home, "", "claude", link)
	assertLoadedContract(t, home, t.TempDir(), "claude", link)
}

func TestLoadedContractRepoOnlyProvider(t *testing.T) {
	home := loadedContractHome(t)
	root := t.TempDir()
	assertLoadedContract(t, home, root, "cursor", "")
	link := filepath.Join(root, "AGENTS.md")
	symlinkContract(t, link, globalContractTarget(home))
	assertLoadedContract(t, home, root, "cursor", link)
	assertLoadedContract(t, home, "", "cursor", "")
}

func TestLoadedContractCustomProviderHomes(t *testing.T) {
	home := loadedContractHome(t)
	claudeRoot, codexRoot := t.TempDir(), t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", claudeRoot)
	t.Setenv("CODEX_HOME", codexRoot)
	// A link at the default location is not the one the CLI reads.
	symlinkContract(t, filepath.Join(home, ".claude", "CLAUDE.md"), globalContractTarget(home))
	assertLoadedContract(t, home, "", "claude", "")
	claudeLink := filepath.Join(claudeRoot, "CLAUDE.md")
	codexLink := filepath.Join(codexRoot, "AGENTS.md")
	symlinkContract(t, claudeLink, globalContractTarget(home))
	symlinkContract(t, codexLink, globalContractTarget(home))
	assertLoadedContract(t, home, "", "claude", claudeLink)
	assertLoadedContract(t, home, "", "codex", codexLink)
}

// A link loads nothing when CORE.md is missing or is not a regular file, so
// it must not count, although its text names the contract.
func TestLoadedContractRequiresAnExistingCore(t *testing.T) {
	for _, tc := range []struct {
		name      string
		breakCore func(t *testing.T, core string)
	}{
		{"missing", func(t *testing.T, core string) {
			if err := os.Remove(core); err != nil {
				t.Fatal(err)
			}
		}},
		{"directory", func(t *testing.T, core string) {
			if err := os.Remove(core); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(core, 0o755); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := loadedContractHome(t)
			symlinkContract(t, filepath.Join(home, ".claude", "CLAUDE.md"), globalContractTarget(home))
			tc.breakCore(t, globalContractTarget(home))
			assertLoadedContract(t, home, "", "claude", "")
		})
	}
}

func TestLoadedContractIgnoresUserFiles(t *testing.T) {
	home := loadedContractHome(t)
	path := filepath.Join(home, ".claude", "CLAUDE.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("my own rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	assertLoadedContract(t, home, "", "claude", "")
}

func TestLoadedContractUnknownProvider(t *testing.T) {
	if _, err := LoadedContract(loadedContractHome(t), "", "no-such-cli"); err == nil {
		t.Fatal("want an error for an unknown provider")
	}
}
