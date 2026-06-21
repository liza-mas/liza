package embedded

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/brandrender"
)

// TestArtifactConsistency verifies that rendered repo master files match their
// embedded copies under internal/embedded/. This catches drift when a master is
// modified without running `make sync-embedded`.
func TestArtifactConsistency(t *testing.T) {
	repoRoot := findRepoRoot(t)
	embeddedDir := filepath.Join(repoRoot, "internal", "embedded")

	expected, err := brandrender.ExpectedEmbeddedFiles(repoRoot, brand.RuntimeValues())
	if err != nil {
		t.Fatalf("building expected embedded files: %v", err)
	}
	if len(expected) == 0 {
		t.Fatal("no rendered embedded files found")
	}
	for _, file := range expected {
		compareRenderedToEmbedded(t, file, filepath.Join(embeddedDir, filepath.FromSlash(file.RelPath)))
	}

	t.Run("docs support stubs resolve", func(t *testing.T) {
		stubs := map[string]string{
			"docs/CONFIGURATION.md":           "support-docs/CONFIGURATION.md",
			"docs/CUSTOMIZING_AGENT_TOOLS.md": "support-docs/CUSTOMIZING_AGENT_TOOLS.md",
			"docs/TROUBLESHOOTING.md":         "support-docs/TROUBLESHOOTING.md",
			"docs/USAGE_MULTI_AGENTS.md":      "support-docs/USAGE_MULTI_AGENTS.md",
			"docs/USAGE_PAIRING.md":           "support-docs/USAGE_PAIRING.md",
			"docs/how-to-produce-a-goal.md":   "support-docs/how-to-produce-a-goal.md",
		}

		for stub, target := range stubs {
			stubPath := filepath.Join(repoRoot, stub)
			targetPath := filepath.Join(repoRoot, target)
			content, err := os.ReadFile(stubPath)
			if err != nil {
				t.Fatalf("reading stub %s: %v", stub, err)
			}
			if _, err := os.Stat(targetPath); err != nil {
				t.Fatalf("stub target missing for %s -> %s: %v", stub, target, err)
			}
			if !strings.Contains(string(content), target) {
				t.Fatalf("stub %s does not point to %s", stub, target)
			}
		}
	})
}

func TestArtifactConsistencyRendersNonDefaultBrand(t *testing.T) {
	repoRoot := t.TempDir()
	mkdirAllConsistency(t, filepath.Join(repoRoot, "contracts"))
	mkdirAllConsistency(t, filepath.Join(repoRoot, "skills", "liza-logs"))
	mkdirAllConsistency(t, filepath.Join(repoRoot, "support-docs"))
	writeConsistencyFile(t, filepath.Join(repoRoot, "contracts", "CORE.md"), "You are a §BRAND_NAME_TITLE§ agent.\n")
	writeConsistencyFile(t, filepath.Join(repoRoot, "skills", "liza-logs", "SKILL.md"), "name: §BRAND_NAME_LOWER§-logs\n")
	writeConsistencyFile(t, filepath.Join(repoRoot, "support-docs", "USAGE.md"), "Run §BRAND_BINARY_NAME§.\n")

	values := brand.ValuesFromEnv(func(key string) string {
		switch key {
		case "BRAND_NAME_LOWER", "BRAND_BINARY_NAME":
			return "acme-agent"
		case "BRAND_NAME_UPPER", "BRAND_ENV_PREFIX":
			return "ACME_AGENT"
		case "BRAND_NAME_TITLE":
			return "Acme Agent"
		case "BRAND_REPO", "BRAND_RELEASE_REPO", "BRAND_INSTALL_REPO":
			return "acme/agent"
		default:
			return ""
		}
	})

	expected, err := brandrender.ExpectedEmbeddedFiles(repoRoot, values)
	if err != nil {
		t.Fatalf("building expected embedded files: %v", err)
	}
	var sawRenamedSkill bool
	for _, file := range expected {
		if strings.Contains(file.RelPath, "acme-agent-logs") {
			sawRenamedSkill = true
		}
		if strings.Contains(string(file.Content), "§") || strings.Contains(string(file.Content), "BRAND_") {
			t.Fatalf("unrendered macro in %s: %s", file.RelPath, file.Content)
		}
	}
	if !sawRenamedSkill {
		t.Fatalf("expected rendered skill path rename, got %+v", expected)
	}
}

func compareRenderedToEmbedded(t *testing.T, expected brandrender.RenderedFile, embeddedPath string) {
	t.Helper()

	embedded, err := os.ReadFile(embeddedPath)
	if err != nil {
		t.Errorf("reading embedded copy %s: %v", embeddedPath, err)
		return
	}

	if string(expected.Content) != string(embedded) {
		t.Errorf("DRIFT: rendered source %s differs from embedded copy %s — run `make sync-embedded`",
			expected.RelPath, embeddedPath)
	}
}

func mkdirAllConsistency(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func writeConsistencyFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// findRepoRoot walks up from the working directory to find the directory
// containing go.mod (the repository root).
func findRepoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getting working directory: %v", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (no go.mod found in any parent directory)")
		}
		dir = parent
	}
}
