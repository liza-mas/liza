// Package embedded provides embedded resource files (contracts, skills, settings)
// used during workspace initialization.
package embedded

import (
	"bufio"
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/liza-mas/liza/internal/paths"
)

// Build-time variables (set via -ldflags during build)
var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
)

//go:embed contracts/*.md
var contractsFS embed.FS

//go:embed skills
var skillsFS embed.FS

//go:embed "claude-settings.json"
var claudeSettingsContent []byte

//go:embed "hooks/enforce-init.sh"
var enforceInitHookContent []byte

//go:embed "hooks/git-guard.sh"
var gitGuardHookContent []byte

//go:embed "hooks/rtk-guard.sh"
var rtkGuardHookContent []byte

//go:embed "guardrails-template.md"
var guardrailsTemplateContent []byte

//go:embed "support.md"
var supportDocContent []byte

//go:embed "pipeline.yaml"
var pipelineConfigContent []byte

// PipelineConfigContent returns the raw embedded pipeline.yaml content.
// Used by init to auto-freeze when --config is not provided.
func PipelineConfigContent() []byte {
	return pipelineConfigContent
}

// PlanGlobalFiles returns the list of absolute paths that WriteGlobalFiles would create,
// without actually writing anything. Useful for pre-flight checks and verbose output.
func PlanGlobalFiles(targetDir string) []string {
	var paths []string
	for _, pair := range []struct {
		fs      embed.FS
		destDir string
	}{
		{contractsFS, targetDir},
		{skillsFS, filepath.Join(targetDir, "skills")},
	} {
		_ = fs.WalkDir(pair.fs, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || path == "." {
				return nil
			}
			parts := strings.Split(path, "/")
			if len(parts) <= 1 {
				return nil
			}
			paths = append(paths, filepath.Join(pair.destDir, filepath.Join(parts[1:]...)))
			return nil
		})
	}
	return paths
}

// WriteGlobalFiles writes contracts and skills to the global Liza directory (~/.liza/).
// Contracts are written flat into targetDir/ and skills into targetDir/skills/.
// Markdown files are prepended with YAML frontmatter containing version metadata.
// Files whose absolute path appears in skipFiles are silently skipped.
// Returns the list of absolute paths written.
func WriteGlobalFiles(targetDir string, skipFiles map[string]bool) ([]string, error) {
	var written []string
	contractPaths, err := writeEmbeddedFS(contractsFS, targetDir, skipFiles)
	if err != nil {
		return nil, fmt.Errorf("failed to write contracts: %w", err)
	}
	written = append(written, contractPaths...)

	skillPaths, err := writeEmbeddedFS(skillsFS, filepath.Join(targetDir, "skills"), skipFiles)
	if err != nil {
		return nil, fmt.Errorf("failed to write skills: %w", err)
	}
	written = append(written, skillPaths...)

	return written, nil
}

// writeEmbeddedFS writes an entire embedded filesystem to the target directory.
// Files whose absolute path appears in skipFiles are silently skipped.
// Returns the list of absolute paths of files written (directories excluded).
func writeEmbeddedFS(embeddedFS embed.FS, targetDir string, skipFiles map[string]bool) ([]string, error) {
	var written []string
	err := fs.WalkDir(embeddedFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if path == "." {
			return nil
		}

		// embed.FS paths include the top-level dir (e.g. "contracts/CORE.md"),
		// but targetDir already points there — strip the first component.
		parts := strings.Split(path, "/")
		if len(parts) == 1 {
			return nil
		}
		relativePath := filepath.Join(parts[1:]...)
		targetPath := filepath.Join(targetDir, relativePath)

		if d.IsDir() {
			return os.MkdirAll(targetPath, 0755)
		}

		if skipFiles[targetPath] {
			return nil
		}

		content, err := embeddedFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read embedded file %s: %w", path, err)
		}

		// Only prepend version frontmatter to Markdown files;
		// scripts and other artifacts are written verbatim.
		output := content
		if strings.HasSuffix(relativePath, ".md") {
			output = PrependFrontmatter(content)
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", targetPath, err)
		}

		if err := os.WriteFile(targetPath, output, 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", targetPath, err)
		}

		written = append(written, targetPath)
		return nil
	})
	return written, err
}

// PrependFrontmatter merges version metadata into existing YAML frontmatter,
// or prepends a new frontmatter block if none exists.
// Existing non-liza fields (e.g., skill name/description) are preserved;
// old liza_* fields are replaced with current build values.
func PrependFrontmatter(content []byte) []byte {
	prefix := []byte("---\n")
	if !bytes.HasPrefix(content, prefix) {
		return append([]byte(frontmatter()), content...)
	}

	rest := content[len(prefix):]
	existingBlockBytes, after, found := bytes.Cut(rest, []byte("\n---\n"))
	if !found {
		return append([]byte(frontmatter()), content...) // malformed, prepend fresh
	}

	existingBlock := string(existingBlockBytes)

	var kept []string
	for line := range strings.SplitSeq(existingBlock, "\n") {
		if !strings.HasPrefix(line, "liza_") {
			kept = append(kept, line)
		}
	}

	var buf bytes.Buffer
	buf.WriteString("---\n")
	for _, line := range kept {
		buf.WriteString(line)
		buf.WriteByte('\n')
	}
	buf.WriteString(versionFields())
	buf.WriteString("---\n")

	// Preserve blank line between frontmatter and body
	if len(after) > 0 && after[0] != '\n' {
		buf.WriteByte('\n')
	}
	buf.Write(after)

	return buf.Bytes()
}

// stripFrontmatter removes a leading YAML frontmatter block ("---\n...---\n") if present.
func stripFrontmatter(content []byte) []byte {
	prefix := []byte("---\n")
	if !bytes.HasPrefix(content, prefix) {
		return content
	}
	rest := content[len(prefix):]
	_, after, found := bytes.Cut(rest, []byte("\n---\n"))
	if !found {
		return content // malformed frontmatter, leave as-is
	}
	if len(after) > 0 && after[0] == '\n' {
		after = after[1:]
	}
	return after
}

// versionFields returns the liza_* key-value lines without YAML delimiters.
func versionFields() string {
	return fmt.Sprintf("liza_version: \"%s\"\nliza_git_commit: \"%s\"\nliza_build_date: \"%s\"\n",
		Version, GitCommit, BuildDate)
}

// frontmatter generates a complete YAML frontmatter block from build-time variables.
func frontmatter() string {
	return "---\n" + versionFields() + "---\n\n"
}

// ListEmbeddedFiles returns a list of all embedded file paths (for testing).
func ListEmbeddedFiles() ([]string, error) {
	var files []string
	for _, fsys := range []embed.FS{contractsFS, skillsFS} {
		collected, err := collectFiles(fsys)
		if err != nil {
			return nil, err
		}
		files = append(files, collected...)
	}
	return files, nil
}

// collectFiles returns all file paths from an embedded filesystem.
func collectFiles(fsys embed.FS) ([]string, error) {
	var files []string
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && path != "." {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

// BackupFile copies src to src.bak using streaming I/O.
func BackupFile(src string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(src + ".bak")
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

// confirmMerge prompts the user for yes/no confirmation and returns true if accepted.
func confirmMerge(prompt string, reader *bufio.Reader) (bool, error) {
	fmt.Print(prompt)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("failed to read user input: %w", err)
	}
	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes", nil
}

// WriteClaudeSettings writes the embedded claude-settings.json to .claude/settings.json
// and deploys hooks referenced by the settings.
// If the file already exists, prompts the user to merge settings.
// Returns nil on success or if user declines merge.
// The stdin parameter allows for injected input in tests; pass os.Stdin for CLI usage.
func WriteClaudeSettings(projectRoot string, reader *bufio.Reader) error {
	lizaPaths := paths.New(projectRoot)
	claudeDir := lizaPaths.ClaudeDir()
	settingsPath := lizaPaths.ClaudeSettingsPath()

	var existingSettings map[string]any
	if existingData, err := os.ReadFile(settingsPath); err == nil {
		ok, err := confirmMerge("Should the Liza claude settings be merged into the existing settings file? (y/n): ", reader)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}

		if err := json.Unmarshal(existingData, &existingSettings); err != nil {
			return fmt.Errorf("failed to parse existing claude-settings.json: %w", err)
		}
	}

	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		return fmt.Errorf("failed to create .claude directory: %w", err)
	}

	var lizaSettings map[string]any
	if err := json.Unmarshal(claudeSettingsContent, &lizaSettings); err != nil {
		return fmt.Errorf("failed to parse embedded claude-settings.json: %w", err)
	}

	var finalSettings map[string]any
	if existingSettings != nil {
		finalSettings = mergeSettings(lizaSettings, existingSettings)
	} else {
		finalSettings = lizaSettings
	}

	output, err := json.MarshalIndent(finalSettings, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal claude-settings.json: %w", err)
	}

	if err := os.WriteFile(settingsPath, output, 0644); err != nil {
		return fmt.Errorf("failed to write claude-settings.json: %w", err)
	}

	// Deploy hooks referenced by the settings file.
	if err := WriteHooks(projectRoot); err != nil {
		return fmt.Errorf("failed to write hooks: %w", err)
	}

	return nil
}

// CleanStaleMCPEntry removes the "liza" key from the "mcpServers" object in
// .mcp.json at the project root. Prior Liza versions wrote this entry during
// init; the MCP server has since been removed. If the file becomes empty
// (no remaining servers), it is deleted entirely.
func CleanStaleMCPEntry(projectRoot string) error {
	mcpPath := filepath.Join(projectRoot, ".mcp.json")
	data, err := os.ReadFile(mcpPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading .mcp.json: %w", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil // not valid JSON — leave it alone
	}

	servers, ok := doc["mcpServers"].(map[string]any)
	if !ok {
		return nil
	}
	if _, hasLiza := servers["liza"]; !hasLiza {
		return nil
	}

	delete(servers, "liza")

	if len(servers) == 0 && len(doc) == 1 {
		// Only mcpServers key remained, and it's now empty — remove file
		return os.Remove(mcpPath)
	}

	if len(servers) == 0 {
		delete(doc, "mcpServers")
	}

	output, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling .mcp.json: %w", err)
	}
	return os.WriteFile(mcpPath, append(output, '\n'), 0644)
}

// mergeSettings merges liza settings into existing settings.
// Existing settings take precedence (user customizations preserved).
// Special handling for permissions.allow (union) and hooks.PreToolUse (union).
func mergeSettings(liza, existing map[string]any) map[string]any {
	result := make(map[string]any)
	maps.Copy(result, liza)

	// Existing settings override liza defaults (preserve user customizations),
	// except "permissions" and "hooks" which get deep-merged.
	for k, v := range existing {
		switch k {
		case "permissions":
			lizaPerms, lizaOk := liza[k].(map[string]any)
			existingPerms, existingOk := v.(map[string]any)
			if lizaOk && existingOk {
				result[k] = mergePermissions(lizaPerms, existingPerms)
			} else {
				result[k] = v
			}
		case "hooks":
			lizaHooks, lizaOk := liza[k].(map[string]any)
			existingHooks, existingOk := v.(map[string]any)
			if lizaOk && existingOk {
				result[k] = mergeHooks(lizaHooks, existingHooks)
			} else {
				result[k] = v
			}
		default:
			result[k] = v
		}
	}

	return result
}

// mergePermissions merges permission objects.
// Existing values override liza defaults, except "allow" which is unioned.
func mergePermissions(liza, existing map[string]any) map[string]any {
	result := make(map[string]any)
	maps.Copy(result, liza)

	for k, v := range existing {
		if k == "allow" {
			lizaAllow, lizaOk := liza[k].([]any)
			existingAllow, existingOk := v.([]any)
			if lizaOk && existingOk {
				result[k] = unionStringArrays(lizaAllow, existingAllow)
			} else if existingOk {
				result[k] = v
			}
		} else {
			result[k] = v
		}
	}

	return result
}

// mergeHooks merges hook configurations.
// For each hook event (e.g. "PreToolUse"), the entry arrays are concatenated.
// Duplicate entries (same command string) are deduplicated.
func mergeHooks(liza, existing map[string]any) map[string]any {
	result := make(map[string]any)
	maps.Copy(result, liza)
	maps.Copy(result, existing)

	// For events present in both, concatenate and deduplicate entry arrays.
	for event, lizaVal := range liza {
		existingVal, ok := existing[event]
		if !ok {
			continue
		}
		lizaEntries, lizaOk := lizaVal.([]any)
		existingEntries, existingOk := existingVal.([]any)
		if !lizaOk || !existingOk {
			continue
		}
		result[event] = unionHookEntries(lizaEntries, existingEntries)
	}

	return result
}

// unionHookEntries deduplicates hook entries by the command string inside
// each entry's hooks array. Existing entries (b) take precedence on command
// collision — preserving user customizations (e.g. different timeout).
func unionHookEntries(a, b []any) []any {
	commandsOf := func(entry any) []string {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			return nil
		}
		hooks, ok := entryMap["hooks"].([]any)
		if !ok {
			return nil
		}
		var cmds []string
		for _, h := range hooks {
			hm, ok := h.(map[string]any)
			if !ok {
				continue
			}
			if cmd, ok := hm["command"].(string); ok {
				cmds = append(cmds, cmd)
			}
		}
		return cmds
	}

	// Index existing (b) commands — these win on collision.
	existingCmds := make(map[string]bool)
	for _, entry := range b {
		for _, cmd := range commandsOf(entry) {
			existingCmds[cmd] = true
		}
	}

	// Add liza entries whose commands don't collide with existing.
	var result []any
	for _, entry := range a {
		cmds := commandsOf(entry)
		collides := false
		for _, cmd := range cmds {
			if existingCmds[cmd] {
				collides = true
				break
			}
		}
		if !collides {
			result = append(result, entry)
		}
	}

	// Add all existing entries (they win on collision).
	result = append(result, b...)

	return result
}

// unionStringArrays returns the union of two string arrays (deduplicated).
func unionStringArrays(a, b []any) []any {
	seen := make(map[string]bool)
	result := []any{}

	for _, item := range a {
		str, ok := item.(string)
		if !ok {
			continue
		}
		if !seen[str] {
			seen[str] = true
			result = append(result, item)
		}
	}

	for _, item := range b {
		str, ok := item.(string)
		if !ok {
			continue
		}
		if !seen[str] {
			seen[str] = true
			result = append(result, item)
		}
	}

	return result
}

// WritePipelineConfig writes the embedded pipeline.yaml to the target directory.
// If the file doesn't exist, writes it unconditionally.
// If the file exists and stdin is nil, skips silently (test callers).
// If the file exists and stdin is non-nil, prompts the user to overwrite.
func WritePipelineConfig(targetDir string, stdin *bufio.Reader) error {
	pipelinePath := filepath.Join(targetDir, "pipeline.yaml")
	if _, err := os.Stat(pipelinePath); err == nil {
		if stdin == nil {
			return nil
		}
		ok, err := confirmMerge("pipeline.yaml already exists. Overwrite with embedded version? (y/n): ", stdin)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		// Back up existing file before overwriting
		if err := BackupFile(pipelinePath); err != nil {
			return fmt.Errorf("failed to backup pipeline.yaml: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("checking pipeline.yaml: %w", err)
	}
	if err := os.WriteFile(pipelinePath, pipelineConfigContent, 0644); err != nil {
		return fmt.Errorf("failed to write pipeline.yaml: %w", err)
	}
	return nil
}

// WriteGuardrails writes the embedded guardrails template to GUARDRAILS.md
// in the project root. Only writes if the file doesn't already exist.
// Non-fatal: returns nil if the file already exists.
func WriteGuardrails(projectRoot string) error {
	guardrailsPath := filepath.Join(projectRoot, "GUARDRAILS.md")
	if _, err := os.Stat(guardrailsPath); err == nil {
		// File already exists, don't overwrite
		return nil
	}
	if err := os.WriteFile(guardrailsPath, guardrailsTemplateContent, 0644); err != nil {
		return fmt.Errorf("failed to write GUARDRAILS.md: %w", err)
	}
	return nil
}

// WriteSupportDoc writes the embedded SUPPORT.md to the .liza/ directory.
// Always overwrites — content tracks the Liza version.
func WriteSupportDoc(lizaDir string) error {
	supportPath := filepath.Join(lizaDir, "SUPPORT.md")
	if err := os.WriteFile(supportPath, supportDocContent, 0644); err != nil {
		return fmt.Errorf("failed to write SUPPORT.md: %w", err)
	}
	return nil
}

// WriteHooks writes embedded hook scripts to .claude/hooks/ in the project root.
// Always overwrites — hooks are Liza infrastructure, not user-customizable.
func WriteHooks(projectRoot string) error {
	hooksDir := filepath.Join(projectRoot, ".claude", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		return fmt.Errorf("failed to create .claude/hooks directory: %w", err)
	}

	for name, content := range map[string][]byte{
		"enforce-init.sh": enforceInitHookContent,
		"git-guard.sh":    gitGuardHookContent,
		"rtk-guard.sh":    rtkGuardHookContent,
	} {
		hookPath := filepath.Join(hooksDir, name)
		if err := os.WriteFile(hookPath, content, 0755); err != nil {
			return fmt.Errorf("failed to write %s: %w", name, err)
		}
	}

	return nil
}
