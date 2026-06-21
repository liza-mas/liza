package brandrender

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
)

func TestRenderBytesRejectsUnknownAndStrayMacros(t *testing.T) {
	values := brand.RuntimeValues()
	if _, err := RenderBytes([]byte("hello §BRAND_UNKNOWN§"), values); err == nil {
		t.Fatal("RenderBytes accepted unknown macro")
	}
	if _, err := RenderBytes([]byte("hello §"), values); err == nil {
		t.Fatal("RenderBytes accepted stray delimiter")
	}
}

func TestRenderBytesUsesBrandValues(t *testing.T) {
	values := brand.RuntimeValues()
	values.NameTitle = "Acme Agent"
	got, err := RenderBytes([]byte("Product: §BRAND_NAME_TITLE§"), values)
	if err != nil {
		t.Fatalf("RenderBytes: %v", err)
	}
	if string(got) != "Product: Acme Agent" {
		t.Fatalf("rendered = %q", got)
	}
}

func TestValidateRenderedFileParsesJSONAndTOML(t *testing.T) {
	if err := ValidateRenderedFile("settings.json", []byte(`{"name":"ok"}`)); err != nil {
		t.Fatalf("valid JSON rejected: %v", err)
	}
	if err := ValidateRenderedFile("config.toml", []byte("name = \"ok\"\n")); err != nil {
		t.Fatalf("valid TOML rejected: %v", err)
	}
	if err := ValidateRenderedFile("config.toml", []byte("name = \"unterminated\n")); err == nil {
		t.Fatal("invalid TOML accepted")
	}
}

func TestRenderPathAppliesGeneratedNameMap(t *testing.T) {
	values := brand.RuntimeValues()
	values.NameLower = "acme-agent"
	values.BinaryName = "acme-agent"
	values.ProjectDirName = ".acme-agent"
	got := RenderPath("liza-logs/tools/liza-session-analyzer.html/scripts/liza-index.sh/.liza-hooks/pre-commit", values)
	if got != "acme-agent-logs/tools/acme-agent-session-analyzer.html/scripts/acme-agent-index.sh/.acme-agent-hooks/pre-commit" {
		t.Fatalf("RenderPath = %q", got)
	}
}

func TestRenderPathUsesDerivedDefaults(t *testing.T) {
	values := brand.Values{
		NameLower: "acme-agent",
		NameUpper: "ACME_AGENT",
		NameTitle: "Acme Agent",
		Repo:      "acme/agent",
	}
	got := RenderPath("liza-index/.liza-hooks/pre-commit", values)
	if got != "acme-agent-index/.acme-agent-hooks/pre-commit" {
		t.Fatalf("RenderPath = %q, want derived binary and project dir names", got)
	}
}

func TestExpectedEmbeddedFilesRendersMacros(t *testing.T) {
	root := t.TempDir()
	mkdirAll(t, filepath.Join(root, "contracts"))
	mkdirAll(t, filepath.Join(root, "skills", "liza-logs"))
	mkdirAll(t, filepath.Join(root, "support-docs"))
	writeFile(t, filepath.Join(root, "contracts", "CORE.md"), "# §BRAND_NAME_TITLE§\n")
	writeFile(t, filepath.Join(root, "skills", "liza-logs", "SKILL.md"), "name: §BRAND_NAME_LOWER§\n")
	writeFile(t, filepath.Join(root, "support-docs", "USAGE.md"), "run §BRAND_BINARY_NAME§\n")

	values := brand.RuntimeValues()
	values.NameLower = "acme-agent"
	values.NameTitle = "Acme Agent"
	values.BinaryName = "acme-agent"

	files, err := ExpectedEmbeddedFiles(root, values)
	if err != nil {
		t.Fatalf("ExpectedEmbeddedFiles: %v", err)
	}
	var sawRenamedSkill bool
	for _, file := range files {
		if strings.Contains(file.RelPath, "acme-agent-logs") {
			sawRenamedSkill = true
		}
		if strings.Contains(string(file.Content), "§") || strings.Contains(string(file.Content), "BRAND_") {
			t.Fatalf("unrendered macro in %s: %s", file.RelPath, file.Content)
		}
	}
	if !sawRenamedSkill {
		t.Fatalf("expected generated skill path rename, got %+v", files)
	}
}

func TestExpectedEmbeddedMarkdownUsesNonDefaultBrand(t *testing.T) {
	files, err := ExpectedEmbeddedFiles(findRepoRoot(t), brand.ValuesFromEnv(func(key string) string {
		switch key {
		case "BRAND_NAME_LOWER", "BRAND_BINARY_NAME", "BRAND_ARCHIVE_PREFIX", "BRAND_MISTRAL_PROMPT_ID":
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
	}))
	if err != nil {
		t.Fatalf("ExpectedEmbeddedFiles: %v", err)
	}

	rawDefaultRE := regexp.MustCompile(`\b(Liza|LIZA|liza)\b|liza-mas/liza`)
	for _, file := range files {
		if !strings.HasSuffix(file.RelPath, ".md") {
			continue
		}
		rendered := string(file.Content)
		if match := rawDefaultRE.FindString(rendered); match != "" {
			t.Fatalf("%s contains raw default brand token %q", file.RelPath, match)
		}
	}
}

func mkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root")
		}
		dir = parent
	}
}
