package commands

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
)

func withTestBrandDirs(t *testing.T, globalDir, projectDir string) {
	t.Helper()
	previousGlobal := brand.GlobalDirName
	previousProject := brand.ProjectDirName
	brand.GlobalDirName = globalDir
	brand.ProjectDirName = projectDir
	t.Cleanup(func() {
		brand.GlobalDirName = previousGlobal
		brand.ProjectDirName = previousProject
	})
}

func TestSetupCommand_NewInstall(t *testing.T) {
	tmpDir := t.TempDir()

	err := SetupCommand(SetupParams{TargetDir: tmpDir})
	if err != nil {
		t.Fatalf("SetupCommand failed: %v", err)
	}

	// Verify contracts are flat in targetDir
	coreFile := filepath.Join(tmpDir, "CORE.md")
	if _, err := os.Stat(coreFile); os.IsNotExist(err) {
		t.Error("CORE.md not created in targetDir")
	}

	pairingFile := filepath.Join(tmpDir, "PAIRING_MODE.md")
	if _, err := os.Stat(pairingFile); os.IsNotExist(err) {
		t.Error("PAIRING_MODE.md not created in targetDir")
	}

	// Verify skills are in targetDir/skills/
	skillFile := filepath.Join(tmpDir, "skills", "code-review", "SKILL.md")
	if _, err := os.Stat(skillFile); os.IsNotExist(err) {
		t.Error("skills/code-review/SKILL.md not created")
	}

	// Verify nested skill files (clean-code/languages/)
	langFile := filepath.Join(tmpDir, "skills", "clean-code", "languages", "go.md")
	if _, err := os.Stat(langFile); os.IsNotExist(err) {
		t.Error("skills/clean-code/languages/go.md not created")
	}

	// Verify support docs are installed to targetDir/support-docs/
	supportFile := filepath.Join(tmpDir, "support-docs", "USAGE_MULTI_AGENTS.md")
	if _, err := os.Stat(supportFile); os.IsNotExist(err) {
		t.Error("support-docs/USAGE_MULTI_AGENTS.md not created")
	}

	supportIndex := filepath.Join(tmpDir, "support-docs", "SUPPORT.md")
	if _, err := os.Stat(supportIndex); os.IsNotExist(err) {
		t.Error("support-docs/SUPPORT.md not created")
	}
}

func TestWarnLegacyGlobalRootForNonDefaultBrand(t *testing.T) {
	withTestBrandDirs(t, ".acme-agent", ".acme-agent")
	homeDir := t.TempDir()
	legacyDir := filepath.Join(homeDir, ".liza")
	targetDir := filepath.Join(homeDir, ".acme-agent")
	if err := os.MkdirAll(legacyDir, 0755); err != nil {
		t.Fatal(err)
	}

	stderr := captureSetupStderr(t, func() {
		warnLegacyGlobalRoot(homeDir, targetDir)
	})
	for _, want := range []string{"legacy global root", legacyDir, targetDir, "ignored until explicitly migrated"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("legacy global warning = %q, want %q", stderr, want)
		}
	}
}

func TestWarnLegacyProjectRootForNonDefaultBrand(t *testing.T) {
	withTestBrandDirs(t, ".acme-agent", ".acme-agent")
	projectRoot := t.TempDir()
	legacyDir := filepath.Join(projectRoot, ".liza")
	activeDir := filepath.Join(projectRoot, ".acme-agent")
	if err := os.MkdirAll(legacyDir, 0755); err != nil {
		t.Fatal(err)
	}

	stderr := captureSetupStderr(t, func() {
		warnLegacyProjectRoot(projectRoot, activeDir)
	})
	for _, want := range []string{"legacy project root", legacyDir, activeDir, "ignored until explicitly migrated"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("legacy project warning = %q, want %q", stderr, want)
		}
	}
}

func captureSetupStderr(t *testing.T, fn func()) string {
	t.Helper()
	original := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe() error = %v", err)
	}
	os.Stderr = writer
	fn()
	_ = writer.Close()
	os.Stderr = original
	out, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll(stderr) error = %v", err)
	}
	return string(out)
}

func TestSetupCommand_NewInstallAgentToolsOptionalIndexGuidance(t *testing.T) {
	tmpDir := t.TempDir()

	err := SetupCommand(SetupParams{TargetDir: tmpDir})
	if err != nil {
		t.Fatalf("SetupCommand failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(tmpDir, "AGENT_TOOLS.md"))
	if err != nil {
		t.Fatalf("reading installed AGENT_TOOLS.md: %v", err)
	}
	assertAgentToolsOptionalIndexGuidance(t, string(content))
}

func TestSetupCommand_ExistingWithoutForce(t *testing.T) {
	tmpDir := t.TempDir()

	// Pre-create CORE.md
	if err := os.WriteFile(filepath.Join(tmpDir, "CORE.md"), []byte("existing"), 0644); err != nil {
		t.Fatal(err)
	}

	err := SetupCommand(SetupParams{TargetDir: tmpDir})
	if err == nil {
		t.Fatal("Expected error when existing config found without --force")
	}

	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("Expected 'already exists' in error, got: %v", err)
	}
}

func TestSetupCommand_ExistingWithForce(t *testing.T) {
	tmpDir := t.TempDir()

	// Pre-create CORE.md with old content
	if err := os.WriteFile(filepath.Join(tmpDir, "CORE.md"), []byte("old content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Provide "y\n" on stdin so the overwrite prompt is accepted
	stdin := strings.NewReader("y\n")

	err := SetupCommand(SetupParams{TargetDir: tmpDir, Force: true, Stdin: stdin})
	if err != nil {
		t.Fatalf("SetupCommand with --force failed: %v", err)
	}

	// Verify CORE.md was overwritten
	content, err := os.ReadFile(filepath.Join(tmpDir, "CORE.md"))
	if err != nil {
		t.Fatal(err)
	}

	if string(content) == "old content" {
		t.Error("CORE.md was not overwritten with --force")
	}

	// Verify .bak backup was created
	bakContent, err := os.ReadFile(filepath.Join(tmpDir, "CORE.md.bak"))
	if err != nil {
		t.Fatal("CORE.md.bak not created")
	}
	if string(bakContent) != "old content" {
		t.Errorf("CORE.md.bak has wrong content: %q", string(bakContent))
	}
}

func TestSetupCommand_ExistingWithForceAutoConfirm(t *testing.T) {
	tmpDir := t.TempDir()

	oldCore := []byte("old core")
	oldTools := []byte("my custom tools")
	oldPipeline := []byte("pipeline: old\n")
	for rel, content := range map[string][]byte{
		"CORE.md":        oldCore,
		"AGENT_TOOLS.md": oldTools,
		"pipeline.yaml":  oldPipeline,
	} {
		if err := os.WriteFile(filepath.Join(tmpDir, rel), content, 0644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	err := SetupCommand(SetupParams{
		TargetDir:   tmpDir,
		Force:       true,
		AutoConfirm: true,
		Stdin:       strings.NewReader(""),
	})
	if err != nil {
		t.Fatalf("SetupCommand with auto-confirm failed: %v", err)
	}

	for rel, oldContent := range map[string][]byte{
		"CORE.md":        oldCore,
		"AGENT_TOOLS.md": oldTools,
		"pipeline.yaml":  oldPipeline,
	} {
		content, err := os.ReadFile(filepath.Join(tmpDir, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if bytes.Equal(content, oldContent) {
			t.Fatalf("%s was not overwritten by auto-confirm setup", rel)
		}
		bakContent, err := os.ReadFile(filepath.Join(tmpDir, rel+".bak"))
		if err != nil {
			t.Fatalf("%s.bak not created: %v", rel, err)
		}
		if !bytes.Equal(bakContent, oldContent) {
			t.Fatalf("%s.bak = %q, want %q", rel, string(bakContent), string(oldContent))
		}
	}
}

func TestSetupCommand_CustomizableFileSkipped(t *testing.T) {
	tmpDir := t.TempDir()

	// Pre-create CORE.md and AGENT_TOOLS.md
	if err := os.WriteFile(filepath.Join(tmpDir, "CORE.md"), []byte("old core"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "AGENT_TOOLS.md"), []byte("my custom tools"), 0644); err != nil {
		t.Fatal(err)
	}

	// Provide "y\n" for bulk overwrite, then "n\n" to skip AGENT_TOOLS.md
	stdin := strings.NewReader("y\nn\n")

	err := SetupCommand(SetupParams{TargetDir: tmpDir, Force: true, Stdin: stdin})
	if err != nil {
		t.Fatalf("SetupCommand failed: %v", err)
	}

	// CORE.md should be overwritten
	coreContent, err := os.ReadFile(filepath.Join(tmpDir, "CORE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(coreContent) == "old core" {
		t.Error("CORE.md was not overwritten")
	}

	// AGENT_TOOLS.md should be preserved (user declined)
	toolsContent, err := os.ReadFile(filepath.Join(tmpDir, "AGENT_TOOLS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(toolsContent) != "my custom tools" {
		t.Error("AGENT_TOOLS.md was overwritten despite user declining")
	}
	if strings.Contains(string(toolsContent), "Stacklit") || strings.Contains(string(toolsContent), "Semble") {
		t.Error("AGENT_TOOLS.md custom content was not preserved exactly")
	}

	// AGENT_TOOLS.md.bak should NOT exist (file was skipped, not overwritten)
	if _, err := os.Stat(filepath.Join(tmpDir, "AGENT_TOOLS.md.bak")); err == nil {
		t.Error("AGENT_TOOLS.md.bak should not exist when file was skipped")
	}

	// CORE.md.bak should exist
	bakContent, err := os.ReadFile(filepath.Join(tmpDir, "CORE.md.bak"))
	if err != nil {
		t.Fatal("CORE.md.bak not created")
	}
	if string(bakContent) != "old core" {
		t.Errorf("CORE.md.bak has wrong content: %q", string(bakContent))
	}
}

func TestSetupCommand_CustomizableFileOverwritten(t *testing.T) {
	tmpDir := t.TempDir()

	// Pre-create CORE.md and AGENT_TOOLS.md
	if err := os.WriteFile(filepath.Join(tmpDir, "CORE.md"), []byte("old core"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "AGENT_TOOLS.md"), []byte("my custom tools"), 0644); err != nil {
		t.Fatal(err)
	}

	// Preserve line-oriented CLI muscle memory across consecutive prompts.
	// Provide "y<Enter>" for bulk overwrite, then "yes<Enter>" for AGENT_TOOLS.md.
	stdin := strings.NewReader("y\nyes\n")

	err := SetupCommand(SetupParams{TargetDir: tmpDir, Force: true, Stdin: stdin})
	if err != nil {
		t.Fatalf("SetupCommand failed: %v", err)
	}

	// AGENT_TOOLS.md should be overwritten (user accepted)
	toolsContent, err := os.ReadFile(filepath.Join(tmpDir, "AGENT_TOOLS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(toolsContent) == "my custom tools" {
		t.Error("AGENT_TOOLS.md was not overwritten despite user accepting")
	}

	// AGENT_TOOLS.md.bak should exist with original content
	bakContent, err := os.ReadFile(filepath.Join(tmpDir, "AGENT_TOOLS.md.bak"))
	if err != nil {
		t.Fatal("AGENT_TOOLS.md.bak not created")
	}
	if string(bakContent) != "my custom tools" {
		t.Errorf("AGENT_TOOLS.md.bak has wrong content: %q", string(bakContent))
	}
}

func TestSetupCommand_ExistingWithForceDeclined(t *testing.T) {
	tmpDir := t.TempDir()

	// Pre-create CORE.md
	if err := os.WriteFile(filepath.Join(tmpDir, "CORE.md"), []byte("keep me"), 0644); err != nil {
		t.Fatal(err)
	}

	// Provide "n\n" on stdin — user declines overwrite
	stdin := strings.NewReader("n\n")

	err := SetupCommand(SetupParams{TargetDir: tmpDir, Force: true, Stdin: stdin})
	if err == nil {
		t.Fatal("Expected error when user declines overwrite")
	}
	if !strings.Contains(err.Error(), "aborted") {
		t.Errorf("Expected 'aborted' in error, got: %v", err)
	}

	// Verify CORE.md was NOT overwritten
	content, err := os.ReadFile(filepath.Join(tmpDir, "CORE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "keep me" {
		t.Error("CORE.md was overwritten despite user declining")
	}
}

func TestSetupCommand_CustomAgentTools(t *testing.T) {
	tmpDir := t.TempDir()

	// Create custom agent-tools file
	customContent := "# My Custom Agent Tools\n\nCustom tool configuration."
	customFile := filepath.Join(t.TempDir(), "my-agent-tools.md")
	if err := os.WriteFile(customFile, []byte(customContent), 0644); err != nil {
		t.Fatal(err)
	}

	err := SetupCommand(SetupParams{
		TargetDir:      tmpDir,
		AgentToolsPath: customFile,
	})
	if err != nil {
		t.Fatalf("SetupCommand failed: %v", err)
	}

	// AGENT_TOOLS.md should have custom content (with frontmatter prepended)
	toolsContent, err := os.ReadFile(filepath.Join(tmpDir, "AGENT_TOOLS.md"))
	if err != nil {
		t.Fatal(err)
	}
	toolsStr := string(toolsContent)

	if !strings.Contains(toolsStr, "My Custom Agent Tools") {
		t.Error("AGENT_TOOLS.md does not contain custom content")
	}
	if !strings.Contains(toolsStr, "Custom tool configuration.") {
		t.Error("AGENT_TOOLS.md does not contain custom body")
	}

	// Should have frontmatter
	if !strings.HasPrefix(toolsStr, "---\n") {
		t.Error("Custom AGENT_TOOLS.md missing frontmatter")
	}
	if !strings.Contains(toolsStr, "liza_version:") {
		t.Error("Custom AGENT_TOOLS.md missing version metadata")
	}

	// Embedded AGENT_TOOLS.md content should NOT be present
	// (the embedded version starts with "# Agent Tools" typically)
	// Just verify our custom content is the body, not the embedded default
	if !strings.Contains(toolsStr, customContent) {
		t.Error("Custom content not fully preserved")
	}
}

func TestSetupCommand_CustomAgentToolsOverwrite(t *testing.T) {
	tmpDir := t.TempDir()

	// Pre-create CORE.md and AGENT_TOOLS.md with existing content
	if err := os.WriteFile(filepath.Join(tmpDir, "CORE.md"), []byte("old core"), 0644); err != nil {
		t.Fatal(err)
	}
	oldToolsContent := "my previous custom tools"
	if err := os.WriteFile(filepath.Join(tmpDir, "AGENT_TOOLS.md"), []byte(oldToolsContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create custom agent-tools file
	customContent := "# New Custom Tools\n\nNew configuration."
	customFile := filepath.Join(t.TempDir(), "new-tools.md")
	if err := os.WriteFile(customFile, []byte(customContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Only "y\n" for bulk overwrite — no per-file prompt should fire.
	stdin := strings.NewReader("y\n")

	// Capture stderr to verify the per-file AGENT_TOOLS.md prompt is suppressed.
	origStderr := os.Stderr
	stderrR, stderrW, _ := os.Pipe()
	os.Stderr = stderrW

	err := SetupCommand(SetupParams{
		TargetDir:      tmpDir,
		Force:          true,
		AgentToolsPath: customFile,
		Stdin:          stdin,
	})

	stderrW.Close()
	os.Stderr = origStderr
	var stderrBuf bytes.Buffer
	io.Copy(&stderrBuf, stderrR)

	if err != nil {
		t.Fatalf("SetupCommand failed: %v", err)
	}

	// The per-file "Overwrite AGENT_TOOLS.md?" prompt must NOT appear.
	if strings.Contains(stderrBuf.String(), "AGENT_TOOLS.md") {
		t.Errorf("Per-file prompt for AGENT_TOOLS.md should be suppressed when --agent-tools is used, stderr: %s", stderrBuf.String())
	}

	// Custom content should be written
	toolsContent, err := os.ReadFile(filepath.Join(tmpDir, "AGENT_TOOLS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(toolsContent), "New Custom Tools") {
		t.Error("AGENT_TOOLS.md does not contain new custom content")
	}

	// Backup should exist with old content
	bakContent, err := os.ReadFile(filepath.Join(tmpDir, "AGENT_TOOLS.md.bak"))
	if err != nil {
		t.Fatal("AGENT_TOOLS.md.bak not created")
	}
	if string(bakContent) != oldToolsContent {
		t.Errorf("AGENT_TOOLS.md.bak has wrong content: %q", string(bakContent))
	}

	// CORE.md should also be overwritten (bulk "y" covers it)
	coreContent, err := os.ReadFile(filepath.Join(tmpDir, "CORE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(coreContent) == "old core" {
		t.Error("CORE.md was not overwritten")
	}
}

func TestSetupCommand_CustomAgentToolsFileNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	err := SetupCommand(SetupParams{
		TargetDir:      tmpDir,
		AgentToolsPath: "/nonexistent/path/agent-tools.md",
	})
	if err == nil {
		t.Fatal("Expected error for missing custom agent-tools file")
	}
	if !strings.Contains(err.Error(), "failed to read custom agent-tools file") {
		t.Errorf("Expected 'failed to read custom agent-tools file' in error, got: %v", err)
	}

	// No files should have been written
	if _, err := os.Stat(filepath.Join(tmpDir, "CORE.md")); err == nil {
		t.Error("CORE.md should not exist — early validation should prevent any writes")
	}
}

func assertAgentToolsOptionalIndexGuidance(t *testing.T, content string) {
	t.Helper()

	required := []string{
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
	}
	for _, want := range required {
		if !strings.Contains(content, want) {
			t.Errorf("AGENT_TOOLS.md missing optional-index guidance: %q", want)
		}
	}

	forbidden := []string{
		"/home/tangi/",
		"/home/",
		".worktrees/",
		".liza/scip/",
		"stacklit.json in pairing mode",
		"<task-worktree-path>",
	}
	for _, text := range forbidden {
		if strings.Contains(content, text) {
			t.Errorf("AGENT_TOOLS.md contains project-specific generated path guidance: %q", text)
		}
	}
}

// setupWithAgents is a test helper that runs SetupCommand with agent flags,
// using tmpDir as both the liza dir (TargetDir) and homeDir.
func setupWithAgents(t *testing.T, agents []string) (lizaDir, homeDir string) {
	t.Helper()
	lizaDir = t.TempDir()
	homeDir = t.TempDir()

	err := SetupCommand(SetupParams{
		TargetDir: lizaDir,
		Agents:    agents,
		HomeDir:   homeDir,
	})
	if err != nil {
		t.Fatalf("SetupCommand failed: %v", err)
	}
	return lizaDir, homeDir
}

func TestSetupCommand_AgentClaude(t *testing.T) {
	lizaDir, homeDir := setupWithAgents(t, []string{"claude"})

	// Verify skills dir exists
	skillsDir := filepath.Join(homeDir, ".claude", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		t.Fatalf("failed to read skills dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no skill symlinks created")
	}

	// Verify each entry is a symlink pointing to the liza skills dir
	for _, entry := range entries {
		linkPath := filepath.Join(skillsDir, entry.Name())
		target, err := os.Readlink(linkPath)
		if err != nil {
			t.Errorf("%s is not a symlink: %v", entry.Name(), err)
			continue
		}
		expectedTarget := filepath.Join(lizaDir, "skills", entry.Name())
		if target != expectedTarget {
			t.Errorf("symlink %s points to %s, want %s", entry.Name(), target, expectedTarget)
		}
	}

	// Verify source skills match symlinked skills
	sourceEntries, _ := os.ReadDir(filepath.Join(lizaDir, "skills"))
	if len(entries) != len(sourceEntries) {
		t.Errorf("got %d symlinks, want %d (matching source skills)", len(entries), len(sourceEntries))
	}
}

func TestSetupCommand_AgentOpenCode(t *testing.T) {
	lizaDir, homeDir := setupWithAgents(t, []string{"opencode"})

	skillsDir := filepath.Join(homeDir, ".config", "opencode", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		t.Fatalf("failed to read OpenCode skills dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no OpenCode skill symlinks created")
	}

	for _, entry := range entries {
		linkPath := filepath.Join(skillsDir, entry.Name())
		target, err := os.Readlink(linkPath)
		if err != nil {
			t.Errorf("%s is not a symlink: %v", entry.Name(), err)
			continue
		}
		expectedTarget := filepath.Join(lizaDir, "skills", entry.Name())
		if target != expectedTarget {
			t.Errorf("symlink %s points to %s, want %s", entry.Name(), target, expectedTarget)
		}
	}
}

func TestSetupCommand_AgentCursor(t *testing.T) {
	lizaDir, homeDir := setupWithAgents(t, []string{"cursor"})
	sourceEntries, _ := os.ReadDir(filepath.Join(lizaDir, "skills"))

	for _, agent := range []struct {
		name      string
		configDir string
	}{
		{"claude", ".claude"},
		{"codex", ".codex"},
	} {
		skillsDir := filepath.Join(homeDir, agent.configDir, "skills")
		entries, err := os.ReadDir(skillsDir)
		if err != nil {
			t.Fatalf("agent %s: failed to read skills dir: %v", agent.name, err)
		}
		if len(entries) != len(sourceEntries) {
			t.Fatalf("agent %s: got %d symlinks, want %d", agent.name, len(entries), len(sourceEntries))
		}
		for _, entry := range entries {
			linkPath := filepath.Join(skillsDir, entry.Name())
			target, err := os.Readlink(linkPath)
			if err != nil {
				t.Errorf("agent %s: %s is not a symlink: %v", agent.name, entry.Name(), err)
				continue
			}
			expectedTarget := filepath.Join(lizaDir, "skills", entry.Name())
			if target != expectedTarget {
				t.Errorf("agent %s: symlink %s points to %s, want %s", agent.name, entry.Name(), target, expectedTarget)
			}
		}
	}

	if _, err := os.Stat(filepath.Join(homeDir, ".cursor")); !os.IsNotExist(err) {
		t.Fatalf("~/.cursor state = %v, want absent because setup --cursor has no Cursor-global file API", err)
	}
}

func TestSetupCommand_ProviderCursorStaysCatalogRequest(t *testing.T) {
	// Use an unreachable catalog URL so Load() falls back to the embedded
	// catalog instead of hitting the network. HomeDir is set to a temp dir
	// so the cache is isolated from any stale real-home cache entries.
	t.Setenv("LIZA_PROVIDER_CATALOG_URL", "https://invalid.test/catalog.yaml")

	lizaDir := t.TempDir()
	homeDir := t.TempDir()

	err := SetupCommand(SetupParams{
		TargetDir:   lizaDir,
		HomeDir:     homeDir,
		ProviderIDs: []string{"cursor"},
	})
	if err != nil {
		t.Fatalf("SetupCommand() error = %v, want nil for cursor provider with setup skills", err)
	}

	// cursor setup should create .cursor but NOT expand to .claude or .codex
	// shortcut setup.
	skillsDir := filepath.Join(homeDir, ".cursor", "skills")
	if _, statErr := os.Stat(skillsDir); os.IsNotExist(statErr) {
		t.Fatalf(".cursor/skills state = %v, want present because --provider cursor should set up cursor skills", statErr)
	}
	for _, configDir := range []string{".claude", ".codex"} {
		if _, statErr := os.Stat(filepath.Join(homeDir, configDir)); !os.IsNotExist(statErr) {
			t.Fatalf("%s state = %v, want absent because --provider cursor must not expand to shortcut setup", configDir, statErr)
		}
	}
}

func TestSetupCommand_AgentMistral(t *testing.T) {
	lizaDir, homeDir := setupWithAgents(t, []string{"mistral"})

	// Verify skills symlinks in .vibe/skills/
	skillsDir := filepath.Join(homeDir, ".vibe", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		t.Fatalf("failed to read .vibe/skills: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no skill symlinks in .vibe/skills/")
	}

	// Verify prompts/ dir exists
	promptsDir := filepath.Join(homeDir, ".vibe", "prompts")
	if _, err := os.Stat(promptsDir); os.IsNotExist(err) {
		t.Fatal("prompts/ dir not created for mistral")
	}

	// Verify prompts/liza.md symlink points to CORE.md
	lizaLink := filepath.Join(promptsDir, "liza.md")
	target, err := os.Readlink(lizaLink)
	if err != nil {
		t.Fatalf("prompts/liza.md is not a symlink: %v", err)
	}
	expectedTarget := filepath.Join(lizaDir, "CORE.md")
	if target != expectedTarget {
		t.Errorf("prompts/liza.md points to %s, want %s", target, expectedTarget)
	}
}

func TestSetupCommand_AgentIdempotent(t *testing.T) {
	lizaDir := t.TempDir()
	homeDir := t.TempDir()

	// Run setup twice
	for i := 0; i < 2; i++ {
		// Second run needs "y" for bulk overwrite + "y" for AGENT_TOOLS.md + "y" for pipeline overwrite
		input := "y\n"
		if i > 0 {
			input = "y\ny\ny\n"
		}
		err := SetupCommand(SetupParams{
			TargetDir: lizaDir,
			Agents:    []string{"claude"},
			HomeDir:   homeDir,
			Force:     i > 0, // second run needs --force since files exist
			Stdin:     strings.NewReader(input),
		})
		if err != nil {
			t.Fatalf("SetupCommand run %d failed: %v", i+1, err)
		}
	}

	// Verify symlinks are still correct
	skillsDir := filepath.Join(homeDir, ".claude", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		t.Fatalf("failed to read skills dir: %v", err)
	}
	for _, entry := range entries {
		linkPath := filepath.Join(skillsDir, entry.Name())
		target, err := os.Readlink(linkPath)
		if err != nil {
			t.Errorf("%s is not a symlink after idempotent run: %v", entry.Name(), err)
			continue
		}
		expectedTarget := filepath.Join(lizaDir, "skills", entry.Name())
		if target != expectedTarget {
			t.Errorf("symlink %s points to %s, want %s", entry.Name(), target, expectedTarget)
		}
	}
}

func TestSetupCommand_RemovesRetiredSkillAfterRename(t *testing.T) {
	lizaDir, homeDir := setupWithAgents(t, []string{"claude"})

	retiredSkillDir := filepath.Join(lizaDir, "skills", "code-cleaning")
	if err := os.Mkdir(retiredSkillDir, 0755); err != nil {
		t.Fatalf("create retired skill directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(retiredSkillDir, "SKILL.md"), []byte("legacy skill"), 0644); err != nil {
		t.Fatalf("write retired skill: %v", err)
	}

	retiredLink := filepath.Join(homeDir, ".claude", "skills", "code-cleaning")
	if err := os.Symlink(retiredSkillDir, retiredLink); err != nil {
		t.Fatalf("create retired skill symlink: %v", err)
	}
	userSkillTarget := t.TempDir()
	userSkillLink := filepath.Join(homeDir, ".codex", "skills", "code-cleaning")
	if err := os.MkdirAll(filepath.Dir(userSkillLink), 0755); err != nil {
		t.Fatalf("create user skill parent directory: %v", err)
	}
	if err := os.Symlink(userSkillTarget, userSkillLink); err != nil {
		t.Fatalf("create user skill symlink: %v", err)
	}

	err := SetupCommand(SetupParams{
		TargetDir:   lizaDir,
		Agents:      []string{"claude", "codex"},
		HomeDir:     homeDir,
		Force:       true,
		AutoConfirm: true,
	})
	if err != nil {
		t.Fatalf("SetupCommand failed: %v", err)
	}

	if _, err := os.Lstat(retiredSkillDir); !os.IsNotExist(err) {
		t.Fatalf("retired global skill = %v, want absent", err)
	}
	if _, err := os.Lstat(retiredLink); !os.IsNotExist(err) {
		t.Fatalf("retired agent skill symlink = %v, want absent", err)
	}
	if target, err := os.Readlink(userSkillLink); err != nil || target != userSkillTarget {
		t.Fatalf("user skill symlink = %q, %v; want %q, nil", target, err, userSkillTarget)
	}
	if _, err := os.Stat(filepath.Join(lizaDir, "skills", "clean-code", "SKILL.md")); err != nil {
		t.Fatalf("renamed skill = %v, want installed", err)
	}
}

func TestSetupCommand_AgentExistingWrongSymlink(t *testing.T) {
	lizaDir, homeDir := setupWithAgents(t, []string{"claude"})

	// Get a skill name to tamper with
	skillsDir := filepath.Join(homeDir, ".claude", "skills")
	entries, _ := os.ReadDir(skillsDir)
	if len(entries) == 0 {
		t.Fatal("no skills to test with")
	}
	targetSkill := entries[0].Name()
	linkPath := filepath.Join(skillsDir, targetSkill)

	// Replace with a wrong symlink
	os.Remove(linkPath)
	os.Symlink("/wrong/target", linkPath)

	// Run setup again — "y" for bulk overwrite, "y" for AGENT_TOOLS.md, "y" for pipeline overwrite, "y" for symlink replacement
	err := SetupCommand(SetupParams{
		TargetDir: lizaDir,
		Agents:    []string{"claude"},
		HomeDir:   homeDir,
		Force:     true,
		Stdin:     strings.NewReader("y\ny\ny\ny\n"),
	})
	if err != nil {
		t.Fatalf("SetupCommand failed: %v", err)
	}

	// Verify it now points to the correct target
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("not a symlink: %v", err)
	}
	expectedTarget := filepath.Join(lizaDir, "skills", targetSkill)
	if target != expectedTarget {
		t.Errorf("symlink %s points to %s, want %s", targetSkill, target, expectedTarget)
	}
}

func TestSetupCommand_MultipleAgents(t *testing.T) {
	lizaDir, homeDir := setupWithAgents(t, []string{"claude", "codex"})

	sourceEntries, _ := os.ReadDir(filepath.Join(lizaDir, "skills"))
	var expectedSkills []string
	for _, e := range sourceEntries {
		expectedSkills = append(expectedSkills, e.Name())
	}
	sort.Strings(expectedSkills)

	for _, agent := range []struct {
		name      string
		configDir string
	}{
		{"claude", ".claude"},
		{"codex", ".codex"},
	} {
		skillsDir := filepath.Join(homeDir, agent.configDir, "skills")
		entries, err := os.ReadDir(skillsDir)
		if err != nil {
			t.Fatalf("agent %s: failed to read skills dir: %v", agent.name, err)
		}

		var gotSkills []string
		for _, e := range entries {
			gotSkills = append(gotSkills, e.Name())
		}
		sort.Strings(gotSkills)

		if len(gotSkills) != len(expectedSkills) {
			t.Errorf("agent %s: got %d symlinks, want %d", agent.name, len(gotSkills), len(expectedSkills))
		}
		for i, name := range expectedSkills {
			if i >= len(gotSkills) {
				break
			}
			if gotSkills[i] != name {
				t.Errorf("agent %s: skill[%d] = %s, want %s", agent.name, i, gotSkills[i], name)
			}
		}
	}
}
