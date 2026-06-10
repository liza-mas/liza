package embedded

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestListEmbeddedFiles(t *testing.T) {
	files, err := ListEmbeddedFiles()
	if err != nil {
		t.Fatalf("ListEmbeddedFiles failed: %v", err)
	}

	// Verify we have a reasonable number of files
	if len(files) < 20 {
		t.Errorf("Expected at least 20 files, got %d", len(files))
	}

	// Verify key files exist
	requiredFiles := map[string]bool{
		"contracts/CORE.md":                     false,
		"contracts/PAIRING_MODE.md":             false,
		"contracts/MULTI_AGENT_MODE.md":         false,
		"contracts/AGENT_TOOLS.md":              false,
		"contracts/COLLABORATION_CONTINUITY.md": false,
		"skills/adr-backfill/SKILL.md":          false,
		"skills/code-review/SKILL.md":           false,
		"skills/debugging/SKILL.md":             false,
		"skills/clean-code/languages/go.md":     false,
		"support-docs/SUPPORT.md":               false,
		"support-docs/USAGE_MULTI_AGENTS.md":    false,
	}

	for _, file := range files {
		if _, ok := requiredFiles[file]; ok {
			requiredFiles[file] = true
		}
	}

	for required, found := range requiredFiles {
		if !found {
			t.Errorf("Required file not found in embedded files: %s", required)
		}
	}
}

func TestAgentToolsOptionalIndexGuidance(t *testing.T) {
	content, err := contractsFS.ReadFile("contracts/AGENT_TOOLS.md")
	if err != nil {
		t.Fatalf("reading embedded AGENT_TOOLS.md: %v", err)
	}

	assertAgentToolsOptionalIndexGuidance(t, string(content))
}

func TestFrontmatter(t *testing.T) {
	// Set test values
	Version = "1.2.3"
	GitCommit = "abc123"
	BuildDate = "2026-02-03T10:00:00Z"

	fm := frontmatter()

	// Verify frontmatter contains all metadata
	if !strings.Contains(fm, `liza_version: "1.2.3"`) {
		t.Errorf("Frontmatter missing version: %s", fm)
	}
	if !strings.Contains(fm, `liza_git_commit: "abc123"`) {
		t.Errorf("Frontmatter missing git commit: %s", fm)
	}
	if !strings.Contains(fm, `liza_build_date: "2026-02-03T10:00:00Z"`) {
		t.Errorf("Frontmatter missing build date: %s", fm)
	}

	// Verify frontmatter is valid YAML
	if !strings.HasPrefix(fm, "---\n") {
		t.Errorf("Frontmatter should start with ---")
	}
	if !strings.Contains(fm, "\n---\n") {
		t.Errorf("Frontmatter should end with ---")
	}

	// Reset to defaults
	Version = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
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
			t.Errorf("embedded AGENT_TOOLS.md missing optional-index guidance: %q", want)
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
			t.Errorf("embedded AGENT_TOOLS.md contains project-specific generated path guidance: %q", text)
		}
	}
}

func TestPrependFrontmatter(t *testing.T) {
	// Set test values
	Version = "1.0.0"
	GitCommit = "test123"
	BuildDate = "2026-01-01T00:00:00Z"

	originalContent := []byte("# Test Content\n\nThis is a test file.")
	result := PrependFrontmatter(originalContent)

	resultStr := string(result)

	// Verify frontmatter is prepended
	if !strings.HasPrefix(resultStr, "---\n") {
		t.Errorf("Result should start with frontmatter")
	}

	// Verify original content is preserved after frontmatter
	if !strings.Contains(resultStr, "# Test Content") {
		t.Errorf("Original content should be preserved")
	}

	// Verify frontmatter comes before content
	frontmatterEnd := strings.Index(resultStr, "---\n\n")
	contentStart := strings.Index(resultStr, "# Test Content")
	if frontmatterEnd == -1 || contentStart == -1 || frontmatterEnd >= contentStart {
		t.Errorf("Frontmatter should come before content")
	}

	// Reset to defaults
	Version = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
}

func TestPrependFrontmatter_ReplacesExisting(t *testing.T) {
	Version = "2.0.0"
	GitCommit = "new456"
	BuildDate = "2026-02-01T00:00:00Z"
	defer func() {
		Version = "dev"
		GitCommit = "unknown"
		BuildDate = "unknown"
	}()

	// Content that already has frontmatter
	input := []byte("---\nliza_version: \"1.0.0\"\nliza_git_commit: \"old123\"\nliza_build_date: \"2026-01-01T00:00:00Z\"\n---\n\n# Real Content\n\nBody text.")
	result := PrependFrontmatter(input)
	resultStr := string(result)

	// Should have exactly one frontmatter block with the NEW values
	if strings.Count(resultStr, "liza_version:") != 1 {
		t.Errorf("Expected exactly one liza_version, got:\n%s", resultStr)
	}
	if !strings.Contains(resultStr, `liza_version: "2.0.0"`) {
		t.Error("Expected new version in frontmatter")
	}
	if strings.Contains(resultStr, "old123") {
		t.Error("Old frontmatter values should be stripped")
	}

	// Original content should be preserved
	if !strings.Contains(resultStr, "# Real Content") {
		t.Error("Original content lost")
	}
	if !strings.Contains(resultStr, "Body text.") {
		t.Error("Body text lost")
	}
}

func TestPrependFrontmatter_PreservesNonLizaFields(t *testing.T) {
	Version = "2.0.0"
	GitCommit = "new456"
	BuildDate = "2026-02-01T00:00:00Z"
	defer func() {
		Version = "dev"
		GitCommit = "unknown"
		BuildDate = "unknown"
	}()

	// Skill-style frontmatter with name/description
	input := []byte("---\nname: testing\ndescription: Test Protocol\n---\n\nTests are the immune system.")
	result := PrependFrontmatter(input)
	resultStr := string(result)

	// Original fields must survive
	if !strings.Contains(resultStr, "name: testing") {
		t.Error("Skill name field was lost")
	}
	if !strings.Contains(resultStr, "description: Test Protocol") {
		t.Error("Skill description field was lost")
	}

	// Version metadata must be present
	if !strings.Contains(resultStr, `liza_version: "2.0.0"`) {
		t.Error("Version metadata missing")
	}
	if !strings.Contains(resultStr, `liza_git_commit: "new456"`) {
		t.Error("Git commit metadata missing")
	}

	// Body must be preserved
	if !strings.Contains(resultStr, "Tests are the immune system.") {
		t.Error("Body content lost")
	}

	// Should have exactly one frontmatter block
	if strings.Count(resultStr, "---\n") != 2 {
		t.Errorf("Expected exactly one frontmatter block (2 delimiters), got:\n%s", resultStr)
	}
}

func TestPrependFrontmatter_ReplacesOldLizaFieldsInMixed(t *testing.T) {
	Version = "3.0.0"
	GitCommit = "fresh789"
	BuildDate = "2026-03-01T00:00:00Z"
	defer func() {
		Version = "dev"
		GitCommit = "unknown"
		BuildDate = "unknown"
	}()

	// Simulate re-running setup on an already-merged skill file
	input := []byte("---\nname: testing\ndescription: Test Protocol\nliza_version: \"2.0.0\"\nliza_git_commit: \"old456\"\nliza_build_date: \"2026-02-01T00:00:00Z\"\n---\n\nBody.")
	result := PrependFrontmatter(input)
	resultStr := string(result)

	// Non-liza fields preserved
	if !strings.Contains(resultStr, "name: testing") {
		t.Error("Skill name field was lost")
	}

	// Old liza values replaced
	if strings.Contains(resultStr, "old456") {
		t.Error("Old liza_git_commit should be replaced")
	}
	if strings.Count(resultStr, "liza_version:") != 1 {
		t.Errorf("Expected exactly one liza_version, got:\n%s", resultStr)
	}
	if !strings.Contains(resultStr, `liza_version: "3.0.0"`) {
		t.Error("New version not present")
	}
}

func TestStripFrontmatter(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no frontmatter",
			input:    "# Title\n\nBody",
			expected: "# Title\n\nBody",
		},
		{
			name:     "with frontmatter",
			input:    "---\nkey: value\n---\n\n# Title\n\nBody",
			expected: "# Title\n\nBody",
		},
		{
			name:     "malformed frontmatter (no closing)",
			input:    "---\nkey: value\n# Title",
			expected: "---\nkey: value\n# Title",
		},
		{
			name:     "empty after frontmatter",
			input:    "---\nkey: value\n---\n\n",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := string(stripFrontmatter([]byte(tt.input)))
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestSupportDocCanonicalContent(t *testing.T) {
	globalDir := t.TempDir()
	projectLizaDir := t.TempDir()

	_, err := WriteGlobalFiles(globalDir, map[string]bool{})
	if err != nil {
		t.Fatalf("WriteGlobalFiles failed: %v", err)
	}
	if err := WriteSupportDoc(projectLizaDir); err != nil {
		t.Fatalf("WriteSupportDoc failed: %v", err)
	}

	masterContent, err := supportDocsFS.ReadFile(supportDocEmbeddedPath)
	if err != nil {
		t.Fatalf("reading embedded support doc: %v", err)
	}

	globalContent, err := os.ReadFile(filepath.Join(globalDir, "support-docs", "SUPPORT.md"))
	if err != nil {
		t.Fatalf("reading global support doc: %v", err)
	}

	projectContent, err := os.ReadFile(filepath.Join(projectLizaDir, "SUPPORT.md"))
	if err != nil {
		t.Fatalf("reading project support doc: %v", err)
	}

	if !bytes.Equal(stripFrontmatter(globalContent), masterContent) {
		t.Fatal("global installed SUPPORT.md does not match canonical support-doc body")
	}
	if !bytes.Equal(projectContent, masterContent) {
		t.Fatal("project-local SUPPORT.md does not match canonical support-doc body")
	}
}

func TestWriteGlobalFiles(t *testing.T) {
	// Create temporary directory for testing (acts as ~/.liza/)
	tmpDir := t.TempDir()

	// Write global files
	written, err := WriteGlobalFiles(tmpDir, nil)
	if err != nil {
		t.Fatalf("WriteGlobalFiles failed: %v", err)
	}

	// Verify returned file list is non-empty
	if len(written) == 0 {
		t.Error("WriteGlobalFiles returned empty file list")
	}

	// Verify key files exist and have content (contracts flat in targetDir)
	expectedFiles := []string{
		filepath.Join(tmpDir, "CORE.md"),
		filepath.Join(tmpDir, "PAIRING_MODE.md"),
		filepath.Join(tmpDir, "skills", "adr-backfill", "SKILL.md"),
		filepath.Join(tmpDir, "skills", "code-review", "SKILL.md"),
		filepath.Join(tmpDir, "skills", "clean-code", "languages", "go.md"),
	}

	for _, file := range expectedFiles {
		info, err := os.Stat(file)
		if os.IsNotExist(err) {
			t.Errorf("Expected file not created: %s", file)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("File is empty: %s", file)
		}

		// Verify file permissions
		if info.Mode().Perm() != 0644 {
			t.Errorf("File %s has wrong permissions: got %o, want 0644", file, info.Mode().Perm())
		}
	}

	// Verify contracts are flat in targetDir (not in a contracts/ subdir)
	contractFiles, _ := filepath.Glob(filepath.Join(tmpDir, "*.md"))
	if len(contractFiles) == 0 {
		t.Error("Expected contract files flat in targetDir, got none")
	}

	// Verify skills directory has subdirectories
	skillDirs, _ := filepath.Glob(filepath.Join(tmpDir, "skills", "*"))
	skillDirCount := 0
	for _, dir := range skillDirs {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			skillDirCount++
		}
	}
	if skillDirCount == 0 {
		t.Error("Expected skill directories, got none")
	}
}

func TestWriteGlobalFiles_FrontmatterInAllFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Set test values for frontmatter
	Version = "test-version"
	GitCommit = "test-commit"
	BuildDate = "test-date"

	// Write global files
	_, err := WriteGlobalFiles(tmpDir, nil)
	if err != nil {
		t.Fatalf("WriteGlobalFiles failed: %v", err)
	}

	// Check a few sample files for frontmatter (contracts flat in targetDir)
	sampleFiles := []string{
		filepath.Join(tmpDir, "CORE.md"),
		filepath.Join(tmpDir, "skills", "code-review", "SKILL.md"),
	}

	for _, file := range sampleFiles {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Errorf("Failed to read file %s: %v", file, err)
			continue
		}

		contentStr := string(content)

		// Verify frontmatter is present
		if !strings.HasPrefix(contentStr, "---\n") {
			t.Errorf("File %s missing frontmatter", file)
		}

		// Verify metadata fields are present
		if !strings.Contains(contentStr, "liza_version:") {
			t.Errorf("File %s missing liza_version field", file)
		}
		if !strings.Contains(contentStr, "liza_git_commit:") {
			t.Errorf("File %s missing liza_git_commit field", file)
		}
		if !strings.Contains(contentStr, "liza_build_date:") {
			t.Errorf("File %s missing liza_build_date field", file)
		}

		// Verify test values are present
		if !strings.Contains(contentStr, "test-version") {
			t.Errorf("File %s missing test version value", file)
		}
		if !strings.Contains(contentStr, "test-commit") {
			t.Errorf("File %s missing test commit value", file)
		}
		if !strings.Contains(contentStr, "test-date") {
			t.Errorf("File %s missing test date value", file)
		}
	}

	// Reset to defaults
	Version = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
}

func TestWriteGlobalFiles_OverwritesExisting(t *testing.T) {
	tmpDir := t.TempDir()

	// Write files first time
	_, err := WriteGlobalFiles(tmpDir, nil)
	if err != nil {
		t.Fatalf("First WriteGlobalFiles failed: %v", err)
	}

	// Modify a file (contracts are flat in targetDir now)
	testFile := filepath.Join(tmpDir, "CORE.md")
	originalContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read test file: %v", err)
	}

	modifiedContent := []byte("MODIFIED CONTENT")
	err = os.WriteFile(testFile, modifiedContent, 0644)
	if err != nil {
		t.Fatalf("Failed to modify test file: %v", err)
	}

	// Write files second time
	_, err = WriteGlobalFiles(tmpDir, nil)
	if err != nil {
		t.Fatalf("Second WriteGlobalFiles failed: %v", err)
	}

	// Verify file was overwritten (frontmatter should be present again)
	currentContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read test file after overwrite: %v", err)
	}

	currentStr := string(currentContent)

	// Should have frontmatter again
	if !strings.HasPrefix(currentStr, "---\n") {
		t.Errorf("File was not overwritten - missing frontmatter")
	}

	// Should not have modified content
	if strings.Contains(currentStr, "MODIFIED CONTENT") {
		t.Errorf("File was not overwritten - still contains modified content")
	}

	// Should have original content (after frontmatter)
	if !strings.Contains(currentStr, string(originalContent)) {
		if !strings.Contains(currentStr, "liza_version:") {
			t.Errorf("File does not contain expected content after overwrite")
		}
	}
}

// Test unionStringArrays helper function
func TestUnionStringArrays(t *testing.T) {
	tests := []struct {
		name     string
		a        []any
		b        []any
		expected []string
	}{
		{
			name:     "empty arrays",
			a:        []any{},
			b:        []any{},
			expected: []string{},
		},
		{
			name:     "no duplicates",
			a:        []any{"a", "b"},
			b:        []any{"c", "d"},
			expected: []string{"a", "b", "c", "d"},
		},
		{
			name:     "with duplicates",
			a:        []any{"a", "b", "c"},
			b:        []any{"b", "c", "d"},
			expected: []string{"a", "b", "c", "d"},
		},
		{
			name:     "all duplicates",
			a:        []any{"a", "b"},
			b:        []any{"a", "b"},
			expected: []string{"a", "b"},
		},
		{
			name:     "first empty",
			a:        []any{},
			b:        []any{"a", "b"},
			expected: []string{"a", "b"},
		},
		{
			name:     "second empty",
			a:        []any{"a", "b"},
			b:        []any{},
			expected: []string{"a", "b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := unionStringArrays(tt.a, tt.b)

			// Convert result to strings for comparison
			resultStrs := make([]string, len(result))
			for i, v := range result {
				resultStrs[i] = v.(string)
			}

			// Check length
			if len(resultStrs) != len(tt.expected) {
				t.Errorf("Expected %d items, got %d", len(tt.expected), len(resultStrs))
			}

			// Check all expected items are present
			resultMap := make(map[string]bool)
			for _, s := range resultStrs {
				resultMap[s] = true
			}

			for _, expected := range tt.expected {
				if !resultMap[expected] {
					t.Errorf("Expected item %q not found in result", expected)
				}
			}
		})
	}
}

// Test mergePermissions helper function
func TestMergePermissions(t *testing.T) {
	tests := []struct {
		name     string
		liza     map[string]any
		existing map[string]any
		wantMode string
		wantLen  int // expected length of allow array
	}{
		{
			name: "merge with different defaultMode",
			liza: map[string]any{
				"defaultMode": "acceptEdits",
				"allow":       []any{"Bash(liza:*)"},
			},
			existing: map[string]any{
				"defaultMode": "prompt",
				"allow":       []any{"Read(**)"},
			},
			wantMode: "prompt", // existing takes precedence
			wantLen:  2,        // union of allows
		},
		{
			name: "merge with overlapping permissions",
			liza: map[string]any{
				"defaultMode": "acceptEdits",
				"allow":       []any{"Bash(liza:*)", "Bash(git:*)"},
			},
			existing: map[string]any{
				"defaultMode": "acceptEdits",
				"allow":       []any{"Bash(git:*)", "Read(**)"},
			},
			wantMode: "acceptEdits",
			wantLen:  3, // Bash(liza:*), Bash(git:*), Read(**) - deduplicated
		},
		{
			name: "existing has no allow",
			liza: map[string]any{
				"defaultMode": "acceptEdits",
				"allow":       []any{"Bash(liza:*)"},
			},
			existing: map[string]any{
				"defaultMode": "prompt",
			},
			wantMode: "prompt",
			wantLen:  1, // only liza allows
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergePermissions(tt.liza, tt.existing)

			// Check defaultMode
			if result["defaultMode"] != tt.wantMode {
				t.Errorf("Expected defaultMode %q, got %q", tt.wantMode, result["defaultMode"])
			}

			// Check allow array length
			allow, ok := result["allow"].([]any)
			if !ok {
				t.Fatalf("allow is not []any")
			}
			if len(allow) != tt.wantLen {
				t.Errorf("Expected allow length %d, got %d", tt.wantLen, len(allow))
			}
		})
	}
}

// Test mergeSettings helper function
func TestMergeSettings(t *testing.T) {
	tests := []struct {
		name     string
		liza     map[string]any
		existing map[string]any
		checks   func(t *testing.T, result map[string]any)
	}{
		{
			name: "simple merge - existing overrides",
			liza: map[string]any{
				"foo": "liza-value",
				"bar": "liza-bar",
			},
			existing: map[string]any{
				"foo": "existing-value",
			},
			checks: func(t *testing.T, result map[string]any) {
				if result["foo"] != "existing-value" {
					t.Errorf("Expected foo=existing-value, got %v", result["foo"])
				}
				if result["bar"] != "liza-bar" {
					t.Errorf("Expected bar=liza-bar, got %v", result["bar"])
				}
			},
		},
		{
			name: "permissions are merged specially",
			liza: map[string]any{
				"permissions": map[string]any{
					"defaultMode": "acceptEdits",
					"allow":       []any{"Bash(liza:*)"},
				},
			},
			existing: map[string]any{
				"permissions": map[string]any{
					"defaultMode": "prompt",
					"allow":       []any{"Read(**)"},
				},
			},
			checks: func(t *testing.T, result map[string]any) {
				perms, ok := result["permissions"].(map[string]any)
				if !ok {
					t.Fatalf("permissions is not map[string]any")
				}

				// defaultMode should be from existing
				if perms["defaultMode"] != "prompt" {
					t.Errorf("Expected defaultMode=prompt, got %v", perms["defaultMode"])
				}

				// allow should be union
				allow, ok := perms["allow"].([]any)
				if !ok {
					t.Fatalf("allow is not []any")
				}
				if len(allow) != 2 {
					t.Errorf("Expected 2 permissions, got %d", len(allow))
				}
			},
		},
		{
			name: "hooks are deep-merged",
			liza: map[string]any{
				"hooks": map[string]any{
					"PreToolUse": []any{
						map[string]any{
							"hooks": []any{
								map[string]any{"command": "bash enforce-init.sh", "type": "command"},
							},
						},
					},
				},
			},
			existing: map[string]any{
				"hooks": map[string]any{
					"PreToolUse": []any{
						map[string]any{
							"hooks": []any{
								map[string]any{"command": "bash my-custom-hook.sh", "type": "command"},
							},
						},
					},
				},
			},
			checks: func(t *testing.T, result map[string]any) {
				hooks, ok := result["hooks"].(map[string]any)
				if !ok {
					t.Fatalf("hooks is not map[string]any")
				}
				entries, ok := hooks["PreToolUse"].([]any)
				if !ok {
					t.Fatalf("PreToolUse is not []any")
				}
				// Both liza and existing hooks should be present
				if len(entries) != 2 {
					t.Errorf("Expected 2 PreToolUse entries, got %d", len(entries))
				}
			},
		},
		{
			name: "hooks collision preserves existing customizations",
			liza: map[string]any{
				"hooks": map[string]any{
					"PreToolUse": []any{
						map[string]any{
							"hooks": []any{
								map[string]any{"command": "bash enforce-init.sh", "type": "command", "timeout": float64(5)},
							},
						},
					},
				},
			},
			existing: map[string]any{
				"hooks": map[string]any{
					"PreToolUse": []any{
						map[string]any{
							"hooks": []any{
								map[string]any{"command": "bash enforce-init.sh", "type": "command", "timeout": float64(30)},
							},
						},
					},
				},
			},
			checks: func(t *testing.T, result map[string]any) {
				hooks := result["hooks"].(map[string]any)
				entries := hooks["PreToolUse"].([]any)
				if len(entries) != 1 {
					t.Errorf("Expected 1 deduplicated entry, got %d", len(entries))
				}
				// Existing entry (timeout=30) should win over liza (timeout=5)
				entry := entries[0].(map[string]any)
				hooksList := entry["hooks"].([]any)
				hook := hooksList[0].(map[string]any)
				if hook["timeout"] != float64(30) {
					t.Errorf("Expected existing timeout=30 to win, got %v", hook["timeout"])
				}
			},
		},
		{
			name: "additionalDirectories unioned - liza dirs preserved when existing is empty",
			liza: map[string]any{
				"permissions": map[string]any{
					"additionalDirectories": []any{"~/.liza"},
				},
			},
			existing: map[string]any{
				"permissions": map[string]any{
					"additionalDirectories": []any{},
				},
			},
			checks: func(t *testing.T, result map[string]any) {
				dirs := claudeSettingsAdditionalDirectories(t, result)
				if len(dirs) != 1 || dirs[0] != "~/.liza" {
					t.Errorf("Expected [~/.liza], got %v", dirs)
				}
			},
		},
		{
			name: "additionalDirectories unioned - both sources merged",
			liza: map[string]any{
				"permissions": map[string]any{
					"additionalDirectories": []any{"~/.liza"},
				},
			},
			existing: map[string]any{
				"permissions": map[string]any{
					"additionalDirectories": []any{"/custom/path"},
				},
			},
			checks: func(t *testing.T, result map[string]any) {
				dirs := claudeSettingsAdditionalDirectories(t, result)
				if len(dirs) != 2 {
					t.Errorf("Expected 2 dirs, got %d: %v", len(dirs), dirs)
				}
			},
		},
		{
			name: "legacy top-level additionalDirectories migrated into permissions",
			liza: map[string]any{
				"permissions": map[string]any{
					"additionalDirectories": []any{"~/.liza"},
				},
			},
			existing: map[string]any{
				"additionalDirectories": []any{"/custom/path"},
			},
			checks: func(t *testing.T, result map[string]any) {
				if _, ok := result["additionalDirectories"]; ok {
					t.Fatalf("top-level additionalDirectories should be removed")
				}
				dirs := claudeSettingsAdditionalDirectories(t, result)
				dirSet := stringSet(dirs)
				if !dirSet["~/.liza"] || !dirSet["/custom/path"] {
					t.Errorf("Expected merged dirs under permissions, got %v", dirs)
				}
			},
		},
		{
			name: "legacy top-level additionalDirectories preserved alongside existing permissions",
			liza: map[string]any{
				"permissions": map[string]any{
					"additionalDirectories": []any{"~/.liza"},
				},
			},
			existing: map[string]any{
				"permissions": map[string]any{
					"additionalDirectories": []any{"/custom/path"},
				},
				"additionalDirectories": []any{"/legacy/path"},
			},
			checks: func(t *testing.T, result map[string]any) {
				if _, ok := result["additionalDirectories"]; ok {
					t.Fatalf("top-level additionalDirectories should be removed")
				}
				dirs := claudeSettingsAdditionalDirectories(t, result)
				dirSet := stringSet(dirs)
				for _, expected := range []string{"~/.liza", "/custom/path", "/legacy/path"} {
					if !dirSet[expected] {
						t.Errorf("Expected %s in merged dirs, got %v", expected, dirs)
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeSettings(tt.liza, tt.existing)
			tt.checks(t, result)
		})
	}
}

// Test WriteClaudeSettings creates new file
func TestWriteClaudeSettings_NewFile(t *testing.T) {
	// Set test metadata
	Version = "1.0.0"
	GitCommit = "test123"
	BuildDate = "2026-01-01T00:00:00Z"
	defer func() {
		Version = "dev"
		GitCommit = "unknown"
		BuildDate = "unknown"
	}()

	tmpDir := t.TempDir()

	err := WriteClaudeSettings(tmpDir, bufio.NewReader(strings.NewReader("")))
	if err != nil {
		t.Fatalf("WriteClaudeSettings failed: %v", err)
	}

	// Verify .claude directory was created
	claudeDir := filepath.Join(tmpDir, ".claude")
	if _, err := os.Stat(claudeDir); os.IsNotExist(err) {
		t.Errorf(".claude directory was not created")
	}

	// Verify settings.json was created
	settingsPath := filepath.Join(claudeDir, "settings.json")
	info, err := os.Stat(settingsPath)
	if os.IsNotExist(err) {
		t.Fatalf("settings.json was not created")
	}

	// Verify file permissions
	if info.Mode().Perm() != 0644 {
		t.Errorf("File has wrong permissions: got %o, want 0644", info.Mode().Perm())
	}

	// Read and parse JSON
	content, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("Failed to read settings.json: %v", err)
	}

	var settings map[string]any
	if err := json.Unmarshal(content, &settings); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	// Verify permissions exist
	perms, ok := settings["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("permissions field missing or invalid")
	}

	allow, ok := perms["allow"].([]any)
	if !ok {
		t.Fatalf("permissions.allow missing or invalid")
	}

	// Verify allow array is not empty
	if len(allow) == 0 {
		t.Errorf("Expected non-empty permissions.allow array")
	}

	// Verify liza CLI permission is in allow array
	foundLizaCLI := false
	for _, perm := range allow {
		permStr, ok := perm.(string)
		if !ok {
			continue
		}
		if permStr == "Bash(liza:*)" {
			foundLizaCLI = true
			break
		}
	}
	if !foundLizaCLI {
		t.Errorf("Expected Bash(liza:*) in allow array")
	}
}

// Test WriteClaudeSettings with existing file - merge accepted
func TestWriteClaudeSettings_MergeAccepted(t *testing.T) {
	tmpDir := t.TempDir()
	claudeDir := filepath.Join(tmpDir, ".claude")
	settingsPath := filepath.Join(claudeDir, "settings.json")

	// Create .claude directory and existing settings file
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatalf("Failed to create .claude dir: %v", err)
	}

	existingSettings := map[string]any{
		"permissions": map[string]any{
			"defaultMode": "prompt",
			"allow": []any{
				"Read(**)",
				"Bash(git:*)",
			},
		},
		"additionalDirectories": []any{"/custom/path"},
	}

	existingJSON, _ := json.MarshalIndent(existingSettings, "", "  ")
	if err := os.WriteFile(settingsPath, existingJSON, 0644); err != nil {
		t.Fatalf("Failed to write existing file: %v", err)
	}

	// Inject stdin to accept merge
	stdin := strings.NewReader("y\n")

	// Capture stdout to verify prompt
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := WriteClaudeSettings(tmpDir, bufio.NewReader(stdin))

	w.Close()
	os.Stdout = oldStdout

	// Read captured output
	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	if err != nil {
		t.Fatalf("WriteClaudeSettings failed: %v", err)
	}

	// Verify prompt was shown
	if !strings.Contains(output, "merge") {
		t.Errorf("Expected merge prompt in output, got: %s", output)
	}

	// Read and verify merged file
	content, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("Failed to read merged file: %v", err)
	}

	var merged map[string]any
	if err := json.Unmarshal(content, &merged); err != nil {
		t.Fatalf("Failed to parse merged JSON: %v", err)
	}

	// Verify existing defaultMode is preserved
	perms, ok := merged["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("permissions missing")
	}
	if perms["defaultMode"] != "prompt" {
		t.Errorf("Expected defaultMode=prompt (from existing), got %v", perms["defaultMode"])
	}

	// Verify permissions are unioned
	allow, ok := perms["allow"].([]any)
	if !ok {
		t.Fatalf("allow missing")
	}

	allowMap := make(map[string]bool)
	for _, perm := range allow {
		allowMap[perm.(string)] = true
	}

	// Should have permissions from both existing and embedded
	expectedPerms := []string{"Read(**)", "Bash(git:*)"}
	for _, expected := range expectedPerms {
		if !allowMap[expected] {
			t.Errorf("Expected permission %q not found after merge", expected)
		}
	}

	// Verify permissions.additionalDirectories unioned (existing + embedded)
	dirs := claudeSettingsAdditionalDirectories(t, merged)
	dirSet := stringSet(dirs)
	if !dirSet["/custom/path"] {
		t.Errorf("existing /custom/path not preserved after merge")
	}
	if !dirSet["~/.liza"] {
		t.Errorf("embedded ~/.liza not preserved after merge")
	}
	if !dirSet["/tmp"] {
		t.Errorf("embedded /tmp not preserved after merge")
	}

}

// Test WriteClaudeSettings with existing file - merge declined
func TestWriteClaudeSettings_MergeDeclined(t *testing.T) {
	tmpDir := t.TempDir()
	claudeDir := filepath.Join(tmpDir, ".claude")
	settingsPath := filepath.Join(claudeDir, "settings.json")

	// Create .claude directory and existing settings file
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatalf("Failed to create .claude dir: %v", err)
	}

	existingSettings := map[string]any{
		"permissions": map[string]any{
			"defaultMode": "prompt",
			"allow": []any{
				"Read(**)",
			},
		},
	}

	existingJSON, _ := json.MarshalIndent(existingSettings, "", "  ")
	originalContent := string(existingJSON)

	if err := os.WriteFile(settingsPath, existingJSON, 0644); err != nil {
		t.Fatalf("Failed to write existing file: %v", err)
	}

	// Inject stdin to decline merge
	stdin := strings.NewReader("n\n")

	err := WriteClaudeSettings(tmpDir, bufio.NewReader(stdin))
	if err != nil {
		t.Fatalf("WriteClaudeSettings failed: %v", err)
	}

	// Read file and verify it's unchanged
	content, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	if string(content) != originalContent {
		t.Errorf("File was modified despite declining merge")
	}

	// Verify it still has original content (not merged)
	var settings map[string]any
	if err := json.Unmarshal(content, &settings); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

}

// Test WriteClaudeSettings JSON validity
func TestWriteClaudeSettings_JSONValidity(t *testing.T) {
	tmpDir := t.TempDir()

	err := WriteClaudeSettings(tmpDir, bufio.NewReader(strings.NewReader("")))
	if err != nil {
		t.Fatalf("WriteClaudeSettings failed: %v", err)
	}

	settingsPath := filepath.Join(tmpDir, ".claude", "settings.json")
	content, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	// Verify JSON is valid and properly formatted
	var settings map[string]any
	if err := json.Unmarshal(content, &settings); err != nil {
		t.Fatalf("Generated JSON is invalid: %v", err)
	}

	// Verify JSON can be re-marshaled (no special characters that break JSON)
	_, err = json.MarshalIndent(settings, "", "  ")
	if err != nil {
		t.Errorf("Generated JSON cannot be re-marshaled: %v", err)
	}
}

func TestEmbeddedClaudeSettingsScipSearchPermissions(t *testing.T) {
	var settings map[string]any
	if err := json.Unmarshal(claudeSettingsContent, &settings); err != nil {
		t.Fatalf("embedded claude-settings.json is invalid JSON: %v", err)
	}

	allow := claudeSettingsAllowPermissions(t, settings)
	assertClaudeSettingsToolPermissions(t, allow)
}

func TestEmbeddedClaudeSettingsTmpPermissions(t *testing.T) {
	var settings map[string]any
	if err := json.Unmarshal(claudeSettingsContent, &settings); err != nil {
		t.Fatalf("embedded claude-settings.json is invalid JSON: %v", err)
	}

	if _, ok := settings["additionalDirectories"]; ok {
		t.Fatalf("additionalDirectories must be nested under permissions")
	}

	dirs := claudeSettingsAdditionalDirectories(t, settings)
	dirSet := stringSet(dirs)
	if !dirSet["~/.liza"] {
		t.Errorf("permissions.additionalDirectories missing ~/.liza: %v", dirs)
	}
	if !dirSet["/tmp"] {
		t.Errorf("permissions.additionalDirectories missing /tmp: %v", dirs)
	}

	allow := claudeSettingsAllowPermissions(t, settings)
	allowSet := stringSet(allow)
	if !allowSet["Write(//tmp/**)"] {
		t.Errorf("permissions.allow missing Write(//tmp/**)")
	}
	if !allowSet["Edit(//tmp/**)"] {
		t.Errorf("permissions.allow missing Edit(//tmp/**)")
	}
	if allowSet["Write(/tmp/**)"] {
		t.Errorf("permissions.allow must not use project-relative Write(/tmp/**)")
	}
}

func TestWriteClaudeSettings_InheritsScipSearchPermission(t *testing.T) {
	tmpDir := t.TempDir()

	err := WriteClaudeSettings(tmpDir, bufio.NewReader(strings.NewReader("")))
	if err != nil {
		t.Fatalf("WriteClaudeSettings failed: %v", err)
	}

	settingsPath := filepath.Join(tmpDir, ".claude", "settings.json")
	content, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("Failed to read generated settings.json: %v", err)
	}

	var settings map[string]any
	if err := json.Unmarshal(content, &settings); err != nil {
		t.Fatalf("generated settings.json is invalid JSON: %v", err)
	}

	allow := claudeSettingsAllowPermissions(t, settings)
	assertClaudeSettingsToolPermissions(t, allow)
}

func TestWriteClaudeSettings_MergeInheritsScipSearchPermission(t *testing.T) {
	tmpDir := t.TempDir()
	claudeDir := filepath.Join(tmpDir, ".claude")
	settingsPath := filepath.Join(claudeDir, "settings.json")

	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatalf("Failed to create .claude dir: %v", err)
	}

	existingSettings := map[string]any{
		"permissions": map[string]any{
			"allow": []any{
				"Bash(git:*)",
			},
		},
	}
	existingJSON, err := json.MarshalIndent(existingSettings, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal existing settings: %v", err)
	}
	if err := os.WriteFile(settingsPath, existingJSON, 0644); err != nil {
		t.Fatalf("Failed to write existing settings: %v", err)
	}

	err = WriteClaudeSettings(tmpDir, bufio.NewReader(strings.NewReader("y\n")))
	if err != nil {
		t.Fatalf("WriteClaudeSettings failed: %v", err)
	}

	content, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("Failed to read merged settings.json: %v", err)
	}

	var settings map[string]any
	if err := json.Unmarshal(content, &settings); err != nil {
		t.Fatalf("merged settings.json is invalid JSON: %v", err)
	}

	allow := claudeSettingsAllowPermissions(t, settings)
	assertClaudeSettingsToolPermissions(t, allow)
}

func claudeSettingsAllowPermissions(t *testing.T, settings map[string]any) []string {
	t.Helper()

	perms, ok := settings["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("permissions field missing or invalid")
	}

	allowValues, ok := perms["allow"].([]any)
	if !ok {
		t.Fatalf("permissions.allow missing or invalid")
	}

	allow := make([]string, 0, len(allowValues))
	for _, value := range allowValues {
		permission, ok := value.(string)
		if !ok {
			t.Fatalf("permissions.allow contains non-string value %T", value)
		}
		allow = append(allow, permission)
	}

	return allow
}

func claudeSettingsAdditionalDirectories(t *testing.T, settings map[string]any) []string {
	t.Helper()

	perms, ok := settings["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("permissions field missing or invalid")
	}

	dirValues, ok := perms["additionalDirectories"].([]any)
	if !ok {
		t.Fatalf("permissions.additionalDirectories missing or invalid")
	}

	dirs := make([]string, 0, len(dirValues))
	for _, value := range dirValues {
		dir, ok := value.(string)
		if !ok {
			t.Fatalf("permissions.additionalDirectories contains non-string value %T", value)
		}
		dirs = append(dirs, dir)
	}

	return dirs
}

func stringSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

func assertClaudeSettingsToolPermissions(t *testing.T, allow []string) {
	t.Helper()

	counts := make(map[string]int, len(allow))
	for _, permission := range allow {
		counts[permission]++
	}

	required := []string{
		"Bash(node:*)",
		"Bash(rg:*)",
		"Bash(ast-grep:*)",
	}
	for _, permission := range required {
		if counts[permission] == 0 {
			t.Errorf("Expected %s in permissions.allow", permission)
		}
	}

	if count := counts["Bash(scip-search:*)"]; count != 1 {
		t.Errorf("Bash(scip-search:*) count = %d, want 1", count)
	}
	if count := counts["Bash(stacklit:*)"]; count != 1 {
		t.Errorf("Bash(stacklit:*) count = %d, want 1", count)
	}
	if count := counts["Bash(/usr/bin/test:*)"]; count != 1 {
		t.Errorf("Bash(/usr/bin/test:*) count = %d, want 1", count)
	}
}

func TestWriteCodexProjectPermissions_NewFile(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	projectRoot := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(projectRoot, 0755); err != nil {
		t.Fatal(err)
	}

	err := WriteCodexProjectPermissions(projectRoot, bufio.NewReader(strings.NewReader("")))
	if err != nil {
		t.Fatalf("WriteCodexProjectPermissions failed: %v", err)
	}

	configPath := filepath.Join(fakeHome, ".codex", "config.toml")
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read codex config: %v", err)
	}

	text := string(content)
	for _, want := range expectedCodexConfigSnippets(projectRoot) {
		if !strings.Contains(text, want) {
			t.Errorf("config missing %q:\n%s", want, text)
		}
	}
	if !strings.Contains(text, "[sandbox_workspace_write]") {
		t.Errorf("config missing sandbox_workspace_write section:\n%s", text)
	}
	if !strings.Contains(text, `approval_policy = "never"`) {
		t.Errorf("new config should set noninteractive approval policy:\n%s", text)
	}
}

func expectedCodexPermissionSnippets() []string {
	return []string{
		`model = "gpt-5.5"`,
		`approval_policy = "never"`,
		`sandbox_mode = "workspace-write"`,
		`model_reasoning_effort = "high"`,
		`personality = "pragmatic"`,
		`[permissions.workspace.network]`,
		`enabled = true`,
		`network_access = true`,
		`exclude_tmpdir_env_var = false`,
		`exclude_slash_tmp = false`,
	}
}

func expectedCodexRequiredSnippets() []string {
	return []string{
		`approval_policy = "never"`,
		`sandbox_mode = "workspace-write"`,
		`[permissions.workspace.network]`,
		`enabled = true`,
		`network_access = true`,
		`exclude_tmpdir_env_var = false`,
		`exclude_slash_tmp = false`,
	}
}

func expectedCodexWritableRootSnippets(projectRoot string) []string {
	snippets := []string{
		`"` + projectRoot + `"`,
		`"` + filepath.Join(projectRoot, ".git") + `"`,
	}
	if runtime.GOOS != "windows" {
		snippets = append(snippets, `"/tmp"`)
	}
	return snippets
}

func expectedCodexConfigSnippets(projectRoot string, extra ...string) []string {
	snippets := append([]string{}, extra...)
	snippets = append(snippets, expectedCodexPermissionSnippets()...)
	return append(snippets, expectedCodexWritableRootSnippets(projectRoot)...)
}

func TestRenderCodexProjectConfig_RendersWritableRootsExactly(t *testing.T) {
	fakeHome := setCodexHomeForTest(t)
	projectRoot := "/tmp/project"
	gitDir := filepath.Join(projectRoot, ".git")
	writeRoots := codexSupportWritableRoots()

	got := renderCodexProjectConfig(append([]string{projectRoot, gitDir}, append(writeRoots, "/tmp")...))
	want := `model = "gpt-5.5"
approval_policy = "never"
sandbox_mode = "workspace-write"
model_reasoning_effort = "high"
personality = "pragmatic"

[permissions.workspace.network]
enabled = true

[sandbox_workspace_write]
network_access = true
exclude_tmpdir_env_var = false
exclude_slash_tmp = false
writable_roots = [
  "/tmp/project",
  "/tmp/project/.git",
` + renderWritableRootsForTest(writeRoots, "  ") + `  "/tmp",
]
`
	if got != want {
		t.Fatalf("rendered config mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
	if strings.Contains(got, tomlStringValue(filepath.Join(fakeHome, ".liza"))+` = "write"`) {
		t.Fatalf("rendered config should only grant ~/.liza through writable_roots, not permissions.workspace.filesystem:\n%s", got)
	}
	if !strings.Contains(got, tomlStringValue(filepath.Join(fakeHome, ".liza"))+",") {
		t.Fatalf("rendered config should include ~/.liza in writable_roots:\n%s", got)
	}
}

func TestRenderCodexProjectConfig_RemovesTmpPlaceholderWhenAbsent(t *testing.T) {
	setCodexHomeForTest(t)
	projectRoot := "/tmp/project"
	gitDir := filepath.Join(projectRoot, ".git")
	writeRoots := codexSupportWritableRoots()

	got := renderCodexProjectConfig(append([]string{projectRoot, gitDir}, writeRoots...))
	want := `model = "gpt-5.5"
approval_policy = "never"
sandbox_mode = "workspace-write"
model_reasoning_effort = "high"
personality = "pragmatic"

[permissions.workspace.network]
enabled = true

[sandbox_workspace_write]
network_access = true
exclude_tmpdir_env_var = false
exclude_slash_tmp = false
writable_roots = [
  "/tmp/project",
  "/tmp/project/.git",
` + renderWritableRootsForTest(writeRoots, "  ") + `]
`
	if got != want {
		t.Fatalf("rendered config mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestWriteCodexProjectPermissions_ReplacesEmptyCodexFile(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	codexPath := filepath.Join(fakeHome, ".codex")
	if err := os.WriteFile(codexPath, nil, 0644); err != nil {
		t.Fatal(err)
	}
	projectRoot := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(projectRoot, 0755); err != nil {
		t.Fatal(err)
	}

	err := WriteCodexProjectPermissions(projectRoot, bufio.NewReader(strings.NewReader("")))
	if err != nil {
		t.Fatalf("WriteCodexProjectPermissions failed: %v", err)
	}

	info, err := os.Stat(codexPath)
	if err != nil {
		t.Fatalf("failed to stat .codex path: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf(".codex should be a directory, got mode %v", info.Mode())
	}
	if _, err := os.Stat(filepath.Join(codexPath, "config.toml")); err != nil {
		t.Fatalf("expected config.toml to be created: %v", err)
	}
}

func TestWriteCodexProjectPermissions_MergeAccepted(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	codexDir := filepath.Join(fakeHome, ".codex")
	configPath := filepath.Join(codexDir, "config.toml")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatal(err)
	}
	existing := `model = "gpt-5"
# keep this comment

[sandbox_workspace_write]
network_access = true
writable_roots = [
  "/home/test/.npm",
]
`
	if err := os.WriteFile(configPath, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}
	projectRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(projectRoot, 0755); err != nil {
		t.Fatal(err)
	}

	err := WriteCodexProjectPermissions(projectRoot, bufio.NewReader(strings.NewReader("y\n")))
	if err != nil {
		t.Fatalf("WriteCodexProjectPermissions failed: %v", err)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read codex config: %v", err)
	}
	text := string(content)
	snippets := append(expectedCodexRequiredSnippets(),
		`model = "gpt-5"`,
		`model_reasoning_effort = "high"`,
		`personality = "pragmatic"`,
		"# keep this comment",
		`"/home/test/.npm"`,
	)
	snippets = append(snippets, expectedCodexWritableRootSnippets(projectRoot)...)
	for _, want := range snippets {
		if !strings.Contains(text, want) {
			t.Errorf("merged config missing %q:\n%s", want, text)
		}
	}
}

func TestWriteCodexProjectPermissions_MergeDeclined(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	codexDir := filepath.Join(fakeHome, ".codex")
	configPath := filepath.Join(codexDir, "config.toml")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatal(err)
	}
	original := "unrelated = true\n"
	if err := os.WriteFile(configPath, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	err := WriteCodexProjectPermissions(t.TempDir(), bufio.NewReader(strings.NewReader("n\n")))
	if err != nil {
		t.Fatalf("WriteCodexProjectPermissions failed: %v", err)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read codex config: %v", err)
	}
	if string(content) != original {
		t.Errorf("config changed despite declined merge:\n%s", string(content))
	}
}

func TestWriteCodexProjectPermissions_AppendsMissingSection(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	codexDir := filepath.Join(fakeHome, ".codex")
	configPath := filepath.Join(codexDir, "config.toml")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatal(err)
	}
	original := "unrelated = true\n"
	if err := os.WriteFile(configPath, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	projectRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(projectRoot, 0755); err != nil {
		t.Fatal(err)
	}

	err := WriteCodexProjectPermissions(projectRoot, bufio.NewReader(strings.NewReader("y\n")))
	if err != nil {
		t.Fatalf("WriteCodexProjectPermissions failed: %v", err)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read codex config: %v", err)
	}
	text := string(content)
	for _, want := range expectedCodexConfigSnippets(projectRoot,
		original,
		"[sandbox_workspace_write]",
	) {
		if !strings.Contains(text, want) {
			t.Errorf("config missing %q:\n%s", want, text)
		}
	}
}

func TestWriteCodexProjectPermissions_ReplacesManagedBaselineWithoutDuplicate(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	codexDir := filepath.Join(fakeHome, ".codex")
	configPath := filepath.Join(codexDir, "config.toml")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatal(err)
	}
	original := `model = "gpt-5"
approval_policy = "on-failure"
sandbox_mode = "read-only"
model_reasoning_effort = "minimal"
personality = "friendly"
unrelated = true

[permissions.workspace.network]
enabled = false

[sandbox_workspace_write]
network_access = false
exclude_tmpdir_env_var = true
exclude_slash_tmp = true
`
	if err := os.WriteFile(configPath, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	projectRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(projectRoot, 0755); err != nil {
		t.Fatal(err)
	}

	err := WriteCodexProjectPermissions(projectRoot, bufio.NewReader(strings.NewReader("y\n")))
	if err != nil {
		t.Fatalf("WriteCodexProjectPermissions failed: %v", err)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read codex config: %v", err)
	}
	text := string(content)
	if count := strings.Count(text, "sandbox_mode"); count != 1 {
		t.Fatalf("sandbox_mode count = %d, want 1:\n%s", count, text)
	}
	if count := strings.Count(text, "enabled"); count != 1 {
		t.Fatalf("network enabled count = %d, want 1:\n%s", count, text)
	}
	for _, want := range []string{
		`model = "gpt-5"`,
		`approval_policy = "never"`,
		`sandbox_mode = "workspace-write"`,
		`model_reasoning_effort = "minimal"`,
		`personality = "friendly"`,
		`unrelated = true`,
		`enabled = true`,
		`network_access = true`,
		`exclude_tmpdir_env_var = false`,
		`exclude_slash_tmp = false`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("config missing corrected value %q:\n%s", want, text)
		}
	}
}

func TestCodexBaselineLooksComplete_AllowsUserPreferenceValues(t *testing.T) {
	content := `model = "gpt-5"
approval_policy = "never"
sandbox_mode = "workspace-write"
model_reasoning_effort = "minimal"
personality = "friendly"

[permissions.workspace.network]
enabled = true

[sandbox_workspace_write]
network_access = true
exclude_tmpdir_env_var = false
exclude_slash_tmp = false
writable_roots = [
  "/home/test/.codex",
  "/home/test/.liza",
  "/home/test/.npm",
  "/home/test/.pyenv/shims",
]
`
	if !codexBaselineLooksComplete(content) {
		t.Fatalf("baseline completeness should allow user preference values:\n%s", content)
	}
}

func TestCodexBaselineLooksComplete_RequiresOperationalValues(t *testing.T) {
	content := `model = "gpt-5"
approval_policy = "on-failure"
sandbox_mode = "workspace-write"
model_reasoning_effort = "minimal"
personality = "friendly"

[permissions.workspace.network]
enabled = true

[sandbox_workspace_write]
network_access = true
exclude_tmpdir_env_var = false
exclude_slash_tmp = false
writable_roots = [
  "/home/test/.codex",
  "/home/test/.liza",
  "/home/test/.npm",
  "/home/test/.pyenv/shims",
]
`
	if codexBaselineLooksComplete(content) {
		t.Fatalf("baseline completeness should reject insufficient operational values:\n%s", content)
	}
}

func TestEnsureTomlAssignment_DoesNotReplaceMultilineAssignment(t *testing.T) {
	original := `[sandbox_workspace_write]
writable_roots = [
  "/home/test/.npm",
]
`

	got, changed := ensureTomlAssignment(original, "sandbox_workspace_write", "writable_roots", `["/tmp"]`)
	if changed {
		t.Fatalf("ensureTomlAssignment changed a multiline assignment:\n%s", got)
	}
	if got != original {
		t.Fatalf("multiline assignment was rewritten:\nwant:\n%s\ngot:\n%s", original, got)
	}
}

func TestWriteCodexProjectPermissions_AppendsInlineWritableRoots(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	codexDir := filepath.Join(fakeHome, ".codex")
	configPath := filepath.Join(codexDir, "config.toml")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(`[sandbox_workspace_write]
writable_roots = ["/home/test/.npm"] # keep inline comment
`), 0644); err != nil {
		t.Fatal(err)
	}
	projectRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(projectRoot, 0755); err != nil {
		t.Fatal(err)
	}

	err := WriteCodexProjectPermissions(projectRoot, bufio.NewReader(strings.NewReader("y\n")))
	if err != nil {
		t.Fatalf("WriteCodexProjectPermissions failed: %v", err)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read codex config: %v", err)
	}
	text := string(content)
	for _, want := range expectedCodexConfigSnippets(projectRoot, `"/home/test/.npm"`) {
		if !strings.Contains(text, want) {
			t.Errorf("config missing %q:\n%s", want, text)
		}
	}
	if !strings.Contains(text, "# keep inline comment") {
		t.Errorf("config should preserve inline comment:\n%s", text)
	}
}

func TestWriteCodexProjectPermissions_NoDuplicateRoots(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	projectRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(projectRoot, 0755); err != nil {
		t.Fatal(err)
	}
	codexDir := filepath.Join(fakeHome, ".codex")
	configPath := filepath.Join(codexDir, "config.toml")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatal(err)
	}
	original := renderCodexProjectConfig(codexProjectRootsForTest(projectRoot))
	if err := os.WriteFile(configPath, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	err := WriteCodexProjectPermissions(projectRoot, bufio.NewReader(strings.NewReader("")))
	if err != nil {
		t.Fatalf("WriteCodexProjectPermissions failed: %v", err)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read codex config: %v", err)
	}
	if string(content) != original {
		t.Errorf("already-configured roots should not be rewritten:\n%s", string(content))
	}
}

func codexProjectRootsForTest(projectRoot string) []string {
	roots := []string{projectRoot, filepath.Join(projectRoot, ".git")}
	roots = append(roots, codexSupportWritableRoots()...)
	if runtime.GOOS != "windows" {
		roots = append(roots, "/tmp")
	}
	return roots
}

func setCodexHomeForTest(t *testing.T) string {
	t.Helper()
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(fakeHome, ".cache"))
	return fakeHome
}

func renderWritableRootsForTest(roots []string, indent string) string {
	var builder strings.Builder
	for _, root := range roots {
		builder.WriteString(indent)
		builder.WriteString(tomlStringValue(root))
		builder.WriteString(",\n")
	}
	return builder.String()
}

func TestWriteHooks(t *testing.T) {
	tmpDir := t.TempDir()

	if err := WriteHooks(tmpDir); err != nil {
		t.Fatalf("WriteHooks failed: %v", err)
	}

	for name, wantContent := range map[string][]byte{
		"enforce-init.sh":        enforceInitHookContent,
		"session-context.sh":     sessionContextHookContent,
		"git-guard.sh":           gitGuardHookContent,
		"rtk-guard.sh":           rtkGuardHookContent,
		"worktree-path-guard.sh": worktreePathGuardHookContent,
	} {
		hookPath := filepath.Join(tmpDir, ".claude", "hooks", name)
		info, err := os.Stat(hookPath)
		if err != nil {
			t.Fatalf("hook file %s not found: %v", name, err)
		}

		// Verify executable permission
		if info.Mode()&0111 == 0 {
			t.Errorf("hook file %s is not executable: %v", name, info.Mode())
		}

		// Verify content matches embedded source
		content, err := os.ReadFile(hookPath)
		if err != nil {
			t.Fatalf("failed to read hook %s: %v", name, err)
		}
		if !bytes.Equal(content, wantContent) {
			t.Errorf("hook %s content does not match embedded source", name)
		}
	}
}

func TestWriteHooks_Overwrites(t *testing.T) {
	tmpDir := t.TempDir()

	// Write stale hooks
	hooksDir := filepath.Join(tmpDir, ".claude", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"enforce-init.sh", "session-context.sh", "git-guard.sh", "rtk-guard.sh", "worktree-path-guard.sh"} {
		if err := os.WriteFile(filepath.Join(hooksDir, name), []byte("old"), 0755); err != nil {
			t.Fatal(err)
		}
	}

	// WriteHooks should overwrite
	if err := WriteHooks(tmpDir); err != nil {
		t.Fatalf("WriteHooks failed: %v", err)
	}

	for _, name := range []string{"enforce-init.sh", "session-context.sh", "git-guard.sh", "rtk-guard.sh", "worktree-path-guard.sh"} {
		content, err := os.ReadFile(filepath.Join(hooksDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(content) == "old" {
			t.Errorf("hook %s was not overwritten", name)
		}
	}
}

func TestWriteCodexProjectHooks_NewFile(t *testing.T) {
	projectRoot := t.TempDir()

	if err := WriteCodexProjectHooks(projectRoot, bufio.NewReader(strings.NewReader(""))); err != nil {
		t.Fatalf("WriteCodexProjectHooks failed: %v", err)
	}

	configPath := filepath.Join(projectRoot, ".codex", "config.toml")
	configContent, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read codex config: %v", err)
	}
	if string(configContent) != "[features]\nhooks = true\n" {
		t.Errorf("unexpected codex config:\n%s", string(configContent))
	}

	hooksPath := filepath.Join(projectRoot, ".codex", "hooks.json")
	hooksContent, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatalf("failed to read codex hooks.json: %v", err)
	}
	var hooks map[string]any
	if err := json.Unmarshal(hooksContent, &hooks); err != nil {
		t.Fatalf("hooks.json is invalid JSON: %v", err)
	}
	text := string(hooksContent)
	for _, want := range []string{
		`"SessionStart"`,
		`.codex/hooks/session-context.sh`,
		`.codex/hooks/enforce-init.sh`,
		`"matcher": "^Bash$"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("hooks.json missing %q:\n%s", want, text)
		}
	}
	assertHookScripts(t, filepath.Join(projectRoot, ".codex", "hooks"))
}

func TestWriteCodexProjectHooks_ReplacesEmptyCodexFile(t *testing.T) {
	projectRoot := t.TempDir()
	codexPath := filepath.Join(projectRoot, ".codex")
	if err := os.WriteFile(codexPath, nil, 0644); err != nil {
		t.Fatal(err)
	}

	if err := WriteCodexProjectHooks(projectRoot, bufio.NewReader(strings.NewReader(""))); err != nil {
		t.Fatalf("WriteCodexProjectHooks failed: %v", err)
	}

	info, err := os.Stat(codexPath)
	if err != nil {
		t.Fatalf("failed to stat .codex path: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf(".codex should be a directory, got mode %v", info.Mode())
	}
	if _, err := os.Stat(filepath.Join(codexPath, "config.toml")); err != nil {
		t.Fatalf("expected config.toml to be created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(codexPath, "hooks.json")); err != nil {
		t.Fatalf("expected hooks.json to be created: %v", err)
	}
}

func TestWriteCodexProjectHooks_NonEmptyCodexFileErrors(t *testing.T) {
	projectRoot := t.TempDir()
	codexPath := filepath.Join(projectRoot, ".codex")
	original := []byte("not empty")
	if err := os.WriteFile(codexPath, original, 0644); err != nil {
		t.Fatal(err)
	}

	err := WriteCodexProjectHooks(projectRoot, bufio.NewReader(strings.NewReader("")))
	if err == nil {
		t.Fatal("expected WriteCodexProjectHooks to fail when .codex is a non-empty file")
	}

	info, statErr := os.Stat(codexPath)
	if statErr != nil {
		t.Fatalf("failed to stat .codex path: %v", statErr)
	}
	if info.IsDir() {
		t.Fatal(".codex should remain a file on failure")
	}
	content, readErr := os.ReadFile(codexPath)
	if readErr != nil {
		t.Fatalf("failed to read .codex file: %v", readErr)
	}
	if string(content) != string(original) {
		t.Fatalf(".codex content changed on failure: got %q want %q", string(content), string(original))
	}
}

func TestWriteCodexProjectHooks_SymlinkedCodexFileErrors(t *testing.T) {
	projectRoot := t.TempDir()
	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, "codex-target")
	original := []byte("keep me")
	if err := os.WriteFile(targetPath, original, 0644); err != nil {
		t.Fatal(err)
	}

	codexPath := filepath.Join(projectRoot, ".codex")
	if err := os.Symlink(targetPath, codexPath); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	err := WriteCodexProjectHooks(projectRoot, bufio.NewReader(strings.NewReader("")))
	if err == nil {
		t.Fatal("expected WriteCodexProjectHooks to fail when .codex is a symlink to a file")
	}

	info, statErr := os.Lstat(codexPath)
	if statErr != nil {
		t.Fatalf("failed to lstat .codex path: %v", statErr)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf(".codex should remain a symlink, got mode %v", info.Mode())
	}
	linkTarget, readlinkErr := os.Readlink(codexPath)
	if readlinkErr != nil {
		t.Fatalf("failed to read .codex symlink: %v", readlinkErr)
	}
	if linkTarget != targetPath {
		t.Fatalf(".codex symlink target changed: got %q want %q", linkTarget, targetPath)
	}
	content, readErr := os.ReadFile(targetPath)
	if readErr != nil {
		t.Fatalf("failed to read symlink target file: %v", readErr)
	}
	if string(content) != string(original) {
		t.Fatalf("symlink target content changed on failure: got %q want %q", string(content), string(original))
	}
}

func TestWriteCodexProjectHooks_MergesExistingFiles(t *testing.T) {
	projectRoot := t.TempDir()
	codexDir := filepath.Join(projectRoot, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte("model = \"gpt-5\"\n[features]\nother = true\n"), 0644); err != nil {
		t.Fatal(err)
	}
	existingHooks := `{
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "echo done"
          }
        ]
      }
    ]
  }
}`
	if err := os.WriteFile(filepath.Join(codexDir, "hooks.json"), []byte(existingHooks), 0644); err != nil {
		t.Fatal(err)
	}

	if err := WriteCodexProjectHooks(projectRoot, bufio.NewReader(strings.NewReader("y\ny\n"))); err != nil {
		t.Fatalf("WriteCodexProjectHooks failed: %v", err)
	}

	configContent, err := os.ReadFile(filepath.Join(codexDir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"model = \"gpt-5\"", "[features]", "other = true", "hooks = true"} {
		if !strings.Contains(string(configContent), want) {
			t.Errorf("config missing %q:\n%s", want, string(configContent))
		}
	}

	hooksContent, err := os.ReadFile(filepath.Join(codexDir, "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"echo done", ".codex/hooks/enforce-init.sh"} {
		if !strings.Contains(string(hooksContent), want) {
			t.Errorf("hooks.json missing %q:\n%s", want, string(hooksContent))
		}
	}
}

func TestWriteCodexProjectHooks_EnableDeclinedSkipsArtifacts(t *testing.T) {
	projectRoot := t.TempDir()
	codexDir := filepath.Join(projectRoot, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatal(err)
	}
	originalConfig := "model = \"gpt-5\"\n"
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(originalConfig), 0644); err != nil {
		t.Fatal(err)
	}

	if err := WriteCodexProjectHooks(projectRoot, bufio.NewReader(strings.NewReader("n\n"))); err != nil {
		t.Fatalf("WriteCodexProjectHooks failed: %v", err)
	}

	configContent, err := os.ReadFile(filepath.Join(codexDir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(configContent) != originalConfig {
		t.Errorf("config changed despite declined hook enablement:\n%s", string(configContent))
	}
	if _, err := os.Stat(filepath.Join(codexDir, "hooks.json")); !os.IsNotExist(err) {
		t.Fatalf("hooks.json should not be written when enablement is declined, stat err: %v", err)
	}
	if _, err := os.Stat(filepath.Join(codexDir, "hooks")); !os.IsNotExist(err) {
		t.Fatalf("hooks directory should not be written when enablement is declined, stat err: %v", err)
	}
}

func TestWriteCodexProjectHooks_HooksMergeDeclinedSkipsArtifactsAndConfigEnable(t *testing.T) {
	projectRoot := t.TempDir()
	codexDir := filepath.Join(projectRoot, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatal(err)
	}
	originalConfig := "model = \"gpt-5\"\n"
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(originalConfig), 0644); err != nil {
		t.Fatal(err)
	}
	existingHooks := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"echo done"}]}]}}`
	if err := os.WriteFile(filepath.Join(codexDir, "hooks.json"), []byte(existingHooks), 0644); err != nil {
		t.Fatal(err)
	}

	if err := WriteCodexProjectHooks(projectRoot, bufio.NewReader(strings.NewReader("y\nn\n"))); err != nil {
		t.Fatalf("WriteCodexProjectHooks failed: %v", err)
	}

	configContent, err := os.ReadFile(filepath.Join(codexDir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(configContent) != originalConfig {
		t.Errorf("config changed despite declined hooks.json merge:\n%s", string(configContent))
	}

	hooksContent, err := os.ReadFile(filepath.Join(codexDir, "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(hooksContent) != existingHooks {
		t.Errorf("hooks.json changed despite declined merge:\n%s", string(hooksContent))
	}
	if _, err := os.Stat(filepath.Join(codexDir, "hooks")); !os.IsNotExist(err) {
		t.Fatalf("hooks directory should not be written when hooks.json merge is declined, stat err: %v", err)
	}
}

func TestWriteCodexProjectHooks_InvalidExistingHooksJSONLeavesConfigUnchanged(t *testing.T) {
	projectRoot := t.TempDir()
	codexDir := filepath.Join(projectRoot, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatal(err)
	}
	originalConfig := "model = \"gpt-5\"\n"
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(originalConfig), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "hooks.json"), []byte("{not-json"), 0644); err != nil {
		t.Fatal(err)
	}

	err := WriteCodexProjectHooks(projectRoot, bufio.NewReader(strings.NewReader("y\ny\n")))
	if err == nil {
		t.Fatal("expected WriteCodexProjectHooks to fail on invalid existing hooks.json")
	}

	configContent, readErr := os.ReadFile(filepath.Join(codexDir, "config.toml"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(configContent) != originalConfig {
		t.Errorf("config changed despite hooks.json merge failure:\n%s", string(configContent))
	}
}

func TestWriteCodexProjectHooks_HookInstallFailureDoesNotWriteHooksJSON(t *testing.T) {
	projectRoot := t.TempDir()
	codexDir := filepath.Join(projectRoot, ".codex")
	hooksDir := filepath.Join(codexDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatal(err)
	}
	originalHooks := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"echo done"}]}]}}`
	if err := os.WriteFile(filepath.Join(codexDir, "hooks.json"), []byte(originalHooks), 0644); err != nil {
		t.Fatal(err)
	}
	blockingPath := filepath.Join(hooksDir, "git-guard.sh")
	if err := os.MkdirAll(blockingPath, 0755); err != nil {
		t.Fatal(err)
	}

	err := WriteCodexProjectHooks(projectRoot, bufio.NewReader(strings.NewReader("")))
	if err == nil {
		t.Fatal("expected WriteCodexProjectHooks to fail when hook script deployment fails")
	}

	hooksContent, readErr := os.ReadFile(filepath.Join(codexDir, "hooks.json"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(hooksContent) != originalHooks {
		t.Errorf("hooks.json changed despite hook install failure:\n%s", string(hooksContent))
	}
}

func TestWriteCodexHooks_Overwrites(t *testing.T) {
	tmpDir := t.TempDir()
	hooksDir := filepath.Join(tmpDir, ".codex", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"enforce-init.sh", "session-context.sh", "git-guard.sh", "worktree-path-guard.sh"} {
		if err := os.WriteFile(filepath.Join(hooksDir, name), []byte("old"), 0755); err != nil {
			t.Fatal(err)
		}
	}

	if err := WriteCodexHooks(tmpDir); err != nil {
		t.Fatalf("WriteCodexHooks failed: %v", err)
	}

	assertHookScripts(t, hooksDir)
}

func TestWriteOpenCodeExecTool_NewFile(t *testing.T) {
	projectRoot := t.TempDir()

	if err := WriteOpenCodeExecTool(projectRoot); err != nil {
		t.Fatalf("WriteOpenCodeExecTool failed: %v", err)
	}

	toolPath := filepath.Join(projectRoot, ".opencode", "tools", "exec.ts")
	content, err := os.ReadFile(toolPath)
	if err != nil {
		t.Fatalf("failed to read opencode exec tool: %v", err)
	}
	if !bytes.HasPrefix(content, []byte(OpenCodeExecToolManagedHeader)) {
		t.Fatalf("exec tool missing managed header:\n%s", string(content))
	}
}

func TestWriteOpenCodeExecTool_PreservesUserFile(t *testing.T) {
	projectRoot := t.TempDir()
	toolPath := filepath.Join(projectRoot, ".opencode", "tools", "exec.ts")
	original := []byte("// user-owned exec tool\nexport default {}\n")
	if err := os.MkdirAll(filepath.Dir(toolPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(toolPath, original, 0644); err != nil {
		t.Fatal(err)
	}

	if err := WriteOpenCodeExecTool(projectRoot); err != nil {
		t.Fatalf("WriteOpenCodeExecTool failed: %v", err)
	}

	content, err := os.ReadFile(toolPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != string(original) {
		t.Fatalf("user-owned exec tool was overwritten:\n%s", string(content))
	}
}

func TestWriteOpenCodeExecTool_OverwritesManagedFile(t *testing.T) {
	projectRoot := t.TempDir()
	toolPath := filepath.Join(projectRoot, ".opencode", "tools", "exec.ts")
	if err := os.MkdirAll(filepath.Dir(toolPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(toolPath, []byte(OpenCodeExecToolManagedHeader+"\n// old\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := WriteOpenCodeExecTool(projectRoot); err != nil {
		t.Fatalf("WriteOpenCodeExecTool failed: %v", err)
	}

	content, err := os.ReadFile(toolPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "// old") {
		t.Fatalf("managed exec tool was not overwritten:\n%s", string(content))
	}
}

func TestOpenCodeExecToolSchemaAllowsOmittedAndNullOptionals(t *testing.T) {
	content := string(OpenCodeExecToolContent())
	for _, want := range []string{
		"cmd: tool.schema.string()",
		".string()\n      .nullable()\n      .optional()",
		".number()\n      .nullable()\n      .optional()",
		"const cwd = args.workdir ?? defaultWorkdir(context)",
		"typeof args.timeout_ms === \"number\"",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("exec tool content missing %q:\n%s", want, content)
		}
	}
}

func TestOpenCodeExecToolInstructionsMentionExecAndAntiLoop(t *testing.T) {
	content := string(OpenCodeExecToolContent())
	for _, want := range []string{
		"trusted Liza bridge work",
		"executed through the system shell",
		"not safe for less-trusted contexts",
		"Prefer this exec tool",
		"Omit optional fields",
		"null is tolerated",
		"Do not repeat the same successful command",
		"move to the next Liza protocol step",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("exec tool instructions missing %q:\n%s", want, content)
		}
	}
}

func TestOpenCodeExecToolBoundsAccumulatedOutput(t *testing.T) {
	content := string(OpenCodeExecToolContent())
	for _, want := range []string{
		"function appendLimited",
		"OUTPUT_LIMIT - value.length",
		"stdoutTruncated += truncated",
		"stderrTruncated += truncated",
		"formatOutput(\"stdout\", stdout, stdoutTruncated)",
		"formatOutput(\"stderr\", stderr, stderrTruncated)",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("exec tool output bounding missing %q:\n%s", want, content)
		}
	}
}

func TestOpenCodeExecToolForceKillsTimedOutCommands(t *testing.T) {
	content := string(OpenCodeExecToolContent())
	for _, want := range []string{
		"const FORCE_KILL_DELAY_MS",
		"child.kill(\"SIGTERM\")",
		"child.kill(\"SIGKILL\")",
		"if (forceKill) clearTimeout(forceKill)",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("exec tool timeout enforcement missing %q:\n%s", want, content)
		}
	}
}

func assertHookScripts(t *testing.T, hooksDir string) {
	t.Helper()
	for name, wantContent := range hookScriptContents() {
		hookPath := filepath.Join(hooksDir, name)
		info, err := os.Stat(hookPath)
		if err != nil {
			t.Fatalf("hook file %s not found: %v", name, err)
		}
		if info.Mode()&0111 == 0 {
			t.Errorf("hook file %s is not executable: %v", name, info.Mode())
		}
		content, err := os.ReadFile(hookPath)
		if err != nil {
			t.Fatalf("failed to read hook %s: %v", name, err)
		}
		if !bytes.Equal(content, wantContent) {
			t.Errorf("hook %s content does not match embedded source", name)
		}
	}
}

func hookScriptContents() map[string][]byte {
	return map[string][]byte{
		"enforce-init.sh":        enforceInitHookContent,
		"session-context.sh":     sessionContextHookContent,
		"git-guard.sh":           gitGuardHookContent,
		"worktree-path-guard.sh": worktreePathGuardHookContent,
	}
}

func TestCleanStaleMCPEntry(t *testing.T) {
	t.Run("no file", func(t *testing.T) {
		if err := CleanStaleMCPEntry(t.TempDir()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("removes liza entry", func(t *testing.T) {
		dir := t.TempDir()
		mcpPath := filepath.Join(dir, ".mcp.json")
		os.WriteFile(mcpPath, []byte(`{
  "mcpServers": {
    "liza": {"command": "liza-mcp", "args": ["--project-root", "."]},
    "other": {"command": "other-server"}
  }
}`), 0644)

		if err := CleanStaleMCPEntry(dir); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		data, err := os.ReadFile(mcpPath)
		if err != nil {
			t.Fatalf("file should still exist: %v", err)
		}
		var doc map[string]any
		json.Unmarshal(data, &doc)
		servers := doc["mcpServers"].(map[string]any)
		if _, hasLiza := servers["liza"]; hasLiza {
			t.Error("liza entry should have been removed")
		}
		if _, hasOther := servers["other"]; !hasOther {
			t.Error("other entry should be preserved")
		}
	})

	t.Run("deletes file when only liza entry", func(t *testing.T) {
		dir := t.TempDir()
		mcpPath := filepath.Join(dir, ".mcp.json")
		os.WriteFile(mcpPath, []byte(`{
  "mcpServers": {
    "liza": {"command": "liza-mcp", "args": ["--project-root", "."]}
  }
}`), 0644)

		if err := CleanStaleMCPEntry(dir); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, err := os.Stat(mcpPath); !os.IsNotExist(err) {
			t.Error("file should have been deleted")
		}
	})

	t.Run("no liza entry is no-op", func(t *testing.T) {
		dir := t.TempDir()
		mcpPath := filepath.Join(dir, ".mcp.json")
		original := `{"mcpServers": {"other": {"command": "x"}}}`
		os.WriteFile(mcpPath, []byte(original), 0644)

		if err := CleanStaleMCPEntry(dir); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		data, _ := os.ReadFile(mcpPath)
		if string(data) != original {
			t.Errorf("file should be unchanged, got %s", data)
		}
	})
}
