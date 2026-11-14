package embedded

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/brandrender"
	"github.com/liza-mas/liza/internal/paths"
)

func renderedSkeleton() string { return string(renderEmbeddedAsset(invariantsTemplateContent)) }

func renderedGuardrailsTemplate() string {
	return string(renderEmbeddedAsset(guardrailsTemplateContent))
}

func TestInvariantHome_SkeletonLayout(t *testing.T) {
	skeleton := renderedSkeleton()

	// Sections in the frozen order.
	last := -1
	for _, heading := range []string{"# Invariants\n", "\n## Altitude rule\n", "\n## Invariants\n", "\n## Protection Matrix\n"} {
		at := strings.Index(skeleton, heading)
		if at < 0 || at <= last {
			t.Fatalf("skeleton lacks %q after offset %d", strings.TrimSpace(heading), last)
		}
		last = at
	}
	if !strings.HasPrefix(skeleton, "# Invariants\n") {
		t.Fatal("skeleton does not start with '# Invariants'")
	}

	invariants, _ := markdownSectionBody([]byte(skeleton), "## Invariants")
	if !strings.Contains(string(invariants), "`[A-Za-z][A-Za-z0-9._-]*`") || !strings.Contains(string(invariants), "first token of its Invariant cell") {
		t.Fatalf("## Invariants lacks the invariant-ID sentence:\n%s", invariants)
	}
	if !strings.Contains(string(invariants), "| Invariant | Protects against | Enforced |\n|-----------|------------------|----------|") {
		t.Fatalf("## Invariants lacks the header-only three-column table:\n%s", invariants)
	}
	if strings.Count(string(invariants), "\n|") != 2 {
		t.Fatalf("## Invariants table must be header-only:\n%s", invariants)
	}

	matrix, _ := markdownSectionBody([]byte(skeleton), "## Protection Matrix")
	if got := strings.TrimSpace(string(matrix)); got != "| Threat | Primary defenses |\n|--------|------------------|" {
		t.Fatalf("## Protection Matrix must be the header-only table, got:\n%s", got)
	}
}

func TestInvariantHome_SkeletonAltitudeRuleEqualsRepositoryPreamble(t *testing.T) {
	want, ok := markdownSectionBody(readRepoFile(t, "INVARIANTS.md"), altitudeHeading)
	if !ok {
		t.Fatal("repository INVARIANTS.md has no altitude rule section")
	}
	got, ok := markdownSectionBody([]byte(renderedSkeleton()), altitudeHeading)
	if !ok {
		t.Fatal("rendered skeleton has no altitude rule section")
	}
	if string(got) != string(want) {
		t.Fatalf("skeleton altitude rule differs from the repository preamble\n got: %q\nwant: %q", got, want)
	}
	if strings.Join(strings.Fields(string(got)), " ") != strings.Join(strings.Fields(altitudeRuleText), " ") {
		t.Fatalf("skeleton altitude rule differs from the frozen text: %q", got)
	}
}

func TestInvariantHome_GuardrailsTemplateRule(t *testing.T) {
	template := renderedGuardrailsTemplate()
	tier1 := strings.Index(template, "## Tier 1 (Hard Constraints)")
	rule := strings.Index(template, "### Invariant compliance")
	tier2 := strings.Index(template, "## Tier 2")
	if tier1 < 0 || rule < tier1 || tier2 < rule {
		t.Fatalf("rule must sit under Tier 1 before Tier 2 (tier1=%d rule=%d tier2=%d)", tier1, rule, tier2)
	}
	section := template[rule:tier2]
	for _, want := range []string{"`INVARIANTS.md`", "**Triggers:** <!--", "**Test:** \"Does this change preserve every invariant it touches?\""} {
		if !strings.Contains(section, want) {
			t.Errorf("rule section lacks %q:\n%s", want, section)
		}
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "GUARDRAILS.md"), []byte(template), 0o644); err != nil {
		t.Fatal(err)
	}
	pointer, err := CheckGuardrailsPointer(root)
	if err != nil || pointer.Lacking {
		t.Fatalf("a root holding the rendered template: lacking=%v err=%v, want not lacking", pointer.Lacking, err)
	}

	// The rule the check reports is the template's section span.
	rule2, err := invariantComplianceRule()
	if err != nil {
		t.Fatal(err)
	}
	if rule2 != strings.TrimSpace(section) {
		t.Fatalf("reported rule differs from the template section\n got: %q\nwant: %q", rule2, strings.TrimSpace(section))
	}
}

func TestInvariantHome_CreateInvariants(t *testing.T) {
	t.Run("missing is created", func(t *testing.T) {
		root := t.TempDir()
		created, err := CreateInvariants(root)
		if err != nil || !created {
			t.Fatalf("created=%v err=%v, want created", created, err)
		}
		path := filepath.Join(root, InvariantsFileName)
		got, err := os.ReadFile(path)
		if err != nil || string(got) != renderedSkeleton() {
			t.Fatalf("file content differs from the rendered skeleton (err=%v)", err)
		}
		if runtime.GOOS != "windows" {
			if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o644 {
				t.Fatalf("mode = %v (%v), want 0644", info.Mode().Perm(), err)
			}
		}
	})

	t.Run("regular file is untouched", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, InvariantsFileName)
		if err := os.WriteFile(path, []byte("mine\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		created, err := CreateInvariants(root)
		if err != nil || created {
			t.Fatalf("created=%v err=%v, want left alone", created, err)
		}
		if got, _ := os.ReadFile(path); string(got) != "mine\n" {
			t.Fatalf("existing file rewritten: %q", got)
		}
	})

	t.Run("symlink is kept with its target", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlinks need privileges on Windows")
		}
		root := t.TempDir()
		target := filepath.Join(t.TempDir(), "shared-invariants.md")
		if err := os.WriteFile(target, []byte("shared\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(root, InvariantsFileName)
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		created, err := CreateInvariants(root)
		if err != nil || created {
			t.Fatalf("created=%v err=%v, want left alone", created, err)
		}
		if got, err := os.Readlink(link); err != nil || got != target {
			t.Fatalf("link now points to %q (%v), want %q", got, err, target)
		}
		if got, _ := os.ReadFile(target); string(got) != "shared\n" {
			t.Fatalf("symlink target rewritten: %q", got)
		}
	})

	t.Run("dangling symlink is kept", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlinks need privileges on Windows")
		}
		root := t.TempDir()
		target := filepath.Join(t.TempDir(), "absent.md")
		link := filepath.Join(root, InvariantsFileName)
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		created, err := CreateInvariants(root)
		if err != nil || created {
			t.Fatalf("created=%v err=%v, want left alone", created, err)
		}
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Fatalf("creation followed the dangling link and wrote its target (stat err=%v)", err)
		}
	})

	t.Run("directory is left alone", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, InvariantsFileName)
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
		created, err := CreateInvariants(root)
		if err != nil || created {
			t.Fatalf("created=%v err=%v, want left alone", created, err)
		}
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			t.Fatalf("directory replaced (err=%v)", err)
		}
	})

	t.Run("unusable root is an error naming the path", func(t *testing.T) {
		root := t.TempDir()
		blocker := filepath.Join(root, "file")
		if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		created, err := CreateInvariants(blocker)
		if err == nil || created {
			t.Fatalf("created=%v err=%v, want an error", created, err)
		}
		if !strings.Contains(err.Error(), InvariantsFileName) {
			t.Fatalf("error %q does not name %s", err, InvariantsFileName)
		}
	})
}

func TestInvariantHome_GuardrailsPointer(t *testing.T) {
	check := func(t *testing.T, content *string) (GuardrailsPointer, error) {
		t.Helper()
		root := t.TempDir()
		if content != nil {
			if err := os.WriteFile(filepath.Join(root, "GUARDRAILS.md"), []byte(*content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return CheckGuardrailsPointer(root)
	}
	text := func(s string) *string { return &s }

	t.Run("absent is not lacking", func(t *testing.T) {
		pointer, err := check(t, nil)
		if err != nil || pointer.Lacking {
			t.Fatalf("lacking=%v err=%v, want not lacking", pointer.Lacking, err)
		}
	})
	t.Run("naming the file is a pointer", func(t *testing.T) {
		pointer, err := check(t, text("# rules\n\nSee INVARIANTS.md before touching storage.\n"))
		if err != nil || pointer.Lacking {
			t.Fatalf("lacking=%v err=%v, want not lacking", pointer.Lacking, err)
		}
	})
	t.Run("without the name is lacking", func(t *testing.T) {
		pointer, err := check(t, text("# rules\n\nbe careful\n"))
		if err != nil || !pointer.Lacking {
			t.Fatalf("lacking=%v err=%v, want lacking", pointer.Lacking, err)
		}
		if filepath.Base(pointer.Path) != "GUARDRAILS.md" || !strings.HasPrefix(pointer.Rule, "### Invariant compliance") || !strings.Contains(pointer.Rule, InvariantsFileName) {
			t.Fatalf("pointer = %+v, want the GUARDRAILS.md path and the rule section", pointer)
		}
		if strings.Contains(pointer.Rule, "## Tier 2") {
			t.Fatalf("rule spills past its section:\n%s", pointer.Rule)
		}
	})
	t.Run("a directory is an error naming it", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Mkdir(filepath.Join(root, "GUARDRAILS.md"), 0o755); err != nil {
			t.Fatal(err)
		}
		_, err := CheckGuardrailsPointer(root)
		if err == nil || !strings.Contains(err.Error(), "GUARDRAILS.md") {
			t.Fatalf("err = %v, want one naming GUARDRAILS.md", err)
		}
	})
}

// TestInvariantHome_NotAnInitializationRead: INVARIANTS.md is read when a
// rule's trigger fires, so no init-gate surface may require or name it (SC4).
func TestInvariantHome_NotAnInitializationRead(t *testing.T) {
	hook := writeEnforceInitHook(t)
	projectRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectRoot, InvariantsFileName), []byte("# Invariants\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sessionID := "invariants-not-required-" + time.Now().Format("150405.000000000")
	stateDir := filepath.Join(os.TempDir(), brand.BinaryName+"-init-gate-"+sessionID)
	t.Cleanup(func() { os.RemoveAll(stateDir) })

	read := func(name string) {
		runHook(t, hook, bashPayload(t, sessionID, projectRoot, "cat ~/"+paths.GlobalDirName()+"/"+name), 0)
	}
	read("AGENT_TOOLS.md")
	read("PAIRING_MODE.md")
	blocked := runHook(t, hook, bashPayload(t, sessionID, projectRoot, "git status --short"), 2)
	if strings.Contains(blocked, "INVARIANTS") {
		t.Fatalf("block diagnostic names INVARIANTS.md:\n%s", blocked)
	}
	read("COLLABORATION_CONTINUITY.md")
	if _, err := os.Stat(filepath.Join(stateDir, "CLEARED")); err != nil {
		t.Fatalf("gate did not clear after the required reads although INVARIANTS.md was unread: %v", err)
	}
	runHook(t, hook, bashPayload(t, sessionID, projectRoot, "git status --short"), 0)

	surfaces := map[string]string{
		"enforce-init hook":    string(enforceInitHookContent),
		"session-context hook": string(sessionContextHookContent),
		"pi init gate":         string(PiInitGateContent()),
	}
	for mode, parts := range systemPromptParts {
		for _, part := range parts {
			surfaces["system prompt "+mode+" "+part.name] = part.name
		}
	}
	for name, content := range surfaces {
		if strings.Contains(content, "INVARIANTS") {
			t.Errorf("init-gate surface %q names INVARIANTS", name)
		}
	}
}

// defaultBrandLiterals finds the default-brand words G1.3 forbids on
// rendered surfaces, whatever their case. The word boundary keeps ordinary
// words such as "initialization" from matching.
var defaultBrandLiterals = regexp.MustCompile(`(?i)\b(omni|liza)`)

func assertNoBrandLeak(t *testing.T, surface, text string) {
	t.Helper()
	if loc := defaultBrandLiterals.FindStringIndex(text); loc != nil {
		t.Errorf("%s carries a default-brand literal %q", surface, text[loc[0]:loc[1]])
	}
	for _, leak := range []string{"__BRAND_", "§", "BRAND_"} {
		if strings.Contains(text, leak) {
			t.Errorf("%s carries unrendered macro text %q", surface, leak)
		}
	}
}

// TestInvariantHome_NonDefaultBrandRender renders the new surfaces under a
// fixture brand (G1.3) and scans them for default-brand literals.
func TestInvariantHome_NonDefaultBrandRender(t *testing.T) {
	t.Run("runtime render", func(t *testing.T) {
		original := [7]string{brand.NameLower, brand.NameUpper, brand.NameTitle, brand.BinaryName, brand.GlobalDirName, brand.ProjectDirName, brand.EnvPrefix}
		brand.NameLower, brand.NameUpper, brand.NameTitle = "acme-agent", "ACME_AGENT", "Acme Agent"
		brand.BinaryName, brand.GlobalDirName, brand.ProjectDirName, brand.EnvPrefix = "acme-cli", ".acme-cli", ".acme-cli", "ACME_AGENT"
		t.Cleanup(func() {
			brand.NameLower, brand.NameUpper, brand.NameTitle = original[0], original[1], original[2]
			brand.BinaryName, brand.GlobalDirName, brand.ProjectDirName, brand.EnvPrefix = original[3], original[4], original[5], original[6]
		})

		template := renderedGuardrailsTemplate()
		if !strings.Contains(template, "Acme Agent") {
			t.Fatal("the brand override did not reach the rendered template header")
		}
		assertNoBrandLeak(t, "skeleton", renderedSkeleton())
		rule, err := invariantComplianceRule()
		if err != nil {
			t.Fatal(err)
		}
		assertNoBrandLeak(t, "invariant-compliance rule", rule)

		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "GUARDRAILS.md"), []byte("# rules\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		pointer, err := CheckGuardrailsPointer(root)
		if err != nil || !pointer.Lacking {
			t.Fatalf("lacking=%v err=%v, want lacking", pointer.Lacking, err)
		}
		assertNoBrandLeak(t, "reported rule", pointer.Rule)

		// G1.1: the skeleton assumes no build system or language.
		for _, word := range []string{"make ", "go test", "go.mod", "Makefile", "npm ", "pytest"} {
			if strings.Contains(renderedSkeleton(), word) {
				t.Errorf("skeleton names a build system or language command %q", word)
			}
		}
	})

	t.Run("rendered usage docs", func(t *testing.T) {
		values := brand.ValuesFromEnv(func(key string) string {
			switch key {
			case "BRAND_NAME_LOWER":
				return "acme-agent"
			case "BRAND_BINARY_NAME":
				return "acme-cli"
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
		files, err := brandrender.ExpectedEmbeddedFiles(findRepoRoot(t), values)
		if err != nil {
			t.Fatal(err)
		}
		usageIndex := slices.IndexFunc(files, func(file brandrender.RenderedFile) bool {
			return file.RelPath == "support-docs/USAGE_MULTI_AGENTS.md"
		})
		if usageIndex < 0 {
			t.Fatal("rendered usage doc is missing")
		}
		usage := string(files[usageIndex].Content)
		section, ok := markdownSectionBody([]byte(usage), "### Project Guardrails")
		if !ok {
			t.Fatal("rendered usage doc has no Project Guardrails section")
		}
		if !strings.Contains(string(section), InvariantsFileName) {
			t.Fatal("Project Guardrails section does not describe INVARIANTS.md")
		}
		assertNoBrandLeak(t, "Project Guardrails section", string(section))
		var listed bool
		for line := range strings.SplitSeq(usage, "\n") {
			if strings.Contains(line, InvariantsFileName) && !strings.HasPrefix(line, "**Invariants:**") {
				listed = true
				assertNoBrandLeak(t, "usage line "+line, line)
			}
		}
		if !listed {
			t.Fatal("usage doc lists no INVARIANTS.md file line")
		}
	})
}
