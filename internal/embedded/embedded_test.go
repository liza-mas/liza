package embedded

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
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
			name: "additionalDirectories preserved from existing",
			liza: map[string]any{
				"additionalDirectories": []any{},
			},
			existing: map[string]any{
				"additionalDirectories": []any{"/custom/path"},
			},
			checks: func(t *testing.T, result map[string]any) {
				dirs, ok := result["additionalDirectories"].([]any)
				if !ok {
					t.Fatalf("additionalDirectories is not []any")
				}
				if len(dirs) != 1 || dirs[0] != "/custom/path" {
					t.Errorf("additionalDirectories not preserved from existing")
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

	// Verify additionalDirectories preserved
	dirs, ok := merged["additionalDirectories"].([]any)
	if !ok || len(dirs) != 1 || dirs[0] != "/custom/path" {
		t.Errorf("additionalDirectories not preserved from existing")
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

func TestWriteHooks(t *testing.T) {
	tmpDir := t.TempDir()

	if err := WriteHooks(tmpDir); err != nil {
		t.Fatalf("WriteHooks failed: %v", err)
	}

	for name, wantContent := range map[string][]byte{
		"enforce-init.sh": enforceInitHookContent,
		"git-guard.sh":    gitGuardHookContent,
		"rtk-guard.sh":    rtkGuardHookContent,
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
	for _, name := range []string{"enforce-init.sh", "git-guard.sh", "rtk-guard.sh"} {
		if err := os.WriteFile(filepath.Join(hooksDir, name), []byte("old"), 0755); err != nil {
			t.Fatal(err)
		}
	}

	// WriteHooks should overwrite
	if err := WriteHooks(tmpDir); err != nil {
		t.Fatalf("WriteHooks failed: %v", err)
	}

	for _, name := range []string{"enforce-init.sh", "git-guard.sh", "rtk-guard.sh"} {
		content, err := os.ReadFile(filepath.Join(hooksDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(content) == "old" {
			t.Errorf("hook %s was not overwritten", name)
		}
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
