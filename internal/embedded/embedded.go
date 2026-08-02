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
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/codexconfig"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/termutil"
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

//go:embed support-docs/*.md
var supportDocsFS embed.FS

//go:embed "claude-settings.json"
var claudeSettingsContent []byte

//go:embed "codex-config.toml"
var codexConfigContent []byte

//go:embed "codex-hooks.json"
var codexHooksContent []byte

//go:embed "bash-policy.yaml"
var bashPolicyContent []byte

//go:embed "opencode-tools/exec.ts"
var opencodeExecToolContent []byte

//go:embed "hooks/enforce-init.sh"
var enforceInitHookContent []byte

//go:embed "hooks/session-context.sh"
var sessionContextHookContent []byte

//go:embed "hooks/git-guard.sh"
var gitGuardHookContent []byte

//go:embed "hooks/rtk-guard.sh"
var rtkGuardHookContent []byte

//go:embed "hooks/worktree-path-guard.sh"
var worktreePathGuardHookContent []byte

var codexHookContents = map[string][]byte{
	"enforce-init.sh":        enforceInitHookContent,
	"session-context.sh":     sessionContextHookContent,
	"git-guard.sh":           gitGuardHookContent,
	"worktree-path-guard.sh": worktreePathGuardHookContent,
}

// Git-level pre-commit hook for task worktrees. Deliberately NOT in hooks/
// — that directory holds Claude Code PreToolUse hooks that get written to
// .claude/hooks/ and referenced from claude-settings.json. This one is a
// git-native pre-commit hook rendered per-worktree by
// RenderWorktreePreCommitHook and installed into the branded worktree hook directory
// (see ops.InstallWorktreePreCommitHook). Different transport, different
// lifecycle, different directory.
//
//go:embed "git-hooks/worktree-pre-commit.sh"
var worktreePreCommitHookContent []byte

//go:embed "guardrails-template.md"
var guardrailsTemplateContent []byte

//go:embed "claudeignore"
var claudeIgnoreContent []byte

//go:embed "pipeline.yaml"
var pipelineConfigContent []byte

const supportDocEmbeddedPath = "support-docs/SUPPORT.md"

const openCodeExecToolManagedHeaderTemplate = "// __BRAND_NAME_UPPER__ MANAGED FILE: OpenCode exec compatibility tool. Safe for __BRAND_NAME_TITLE__ to overwrite."

// ConfirmOptions controls yes/no approval prompts in embedded file writers.
type ConfirmOptions struct {
	AutoConfirm bool
}

// OpenCodeExecToolManagedHeader identifies the project-local OpenCode exec
// compatibility tool as owned by the current brand.
func OpenCodeExecToolManagedHeader() string {
	return string(renderEmbeddedAsset([]byte(openCodeExecToolManagedHeaderTemplate)))
}

// OpenCodeExecToolContent returns the embedded OpenCode exec compatibility tool.
func OpenCodeExecToolContent() []byte {
	return renderEmbeddedAsset(opencodeExecToolContent)
}

// PipelineConfigContent returns the raw embedded pipeline.yaml content.
// Used by init to auto-freeze when --config is not provided.
func PipelineConfigContent() []byte {
	return pipelineConfigContent
}

func renderEmbeddedAsset(content []byte) []byte {
	values := brand.RuntimeValues()
	replacer := strings.NewReplacer(
		"__BRAND_NAME_LOWER__", values.NameLower,
		"__BRAND_NAME_UPPER__", values.NameUpper,
		"__BRAND_NAME_TITLE__", values.NameTitle,
		"__BRAND_BINARY_NAME__", values.BinaryName,
		"__BRAND_GLOBAL_DIRNAME__", values.GlobalDirName,
		"__BRAND_PROJECT_DIRNAME__", values.ProjectDirName,
		"__BRAND_ENV_PREFIX__", values.EnvPrefix,
	)
	return []byte(replacer.Replace(string(content)))
}

type embeddedCorpus struct {
	name    string
	fs      embed.FS
	destDir string
}

func globalCorpora(targetDir string) []embeddedCorpus {
	return []embeddedCorpus{
		{name: "contracts", fs: contractsFS, destDir: targetDir},
		{name: "skills", fs: skillsFS, destDir: filepath.Join(targetDir, "skills")},
		{name: "support docs", fs: supportDocsFS, destDir: filepath.Join(targetDir, "support-docs")},
	}
}

// PlanGlobalFiles returns the list of absolute paths that WriteGlobalFiles would
// create, without actually writing anything. Useful for pre-flight checks and
// verbose output.
func PlanGlobalFiles(targetDir string) []string {
	var paths []string
	for _, pair := range globalCorpora(targetDir) {
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

// WriteGlobalFiles writes contracts, skills, and support docs to the global
// runtime directory. Contracts are written flat into targetDir/,
// skills into targetDir/skills/, and support docs into
// targetDir/support-docs/. Markdown files are prepended with YAML frontmatter
// containing version metadata. Files whose absolute path appears in skipFiles
// are silently skipped. Returns the list of absolute paths written.
func WriteGlobalFiles(targetDir string, skipFiles map[string]bool) ([]string, error) {
	var written []string
	for _, corpus := range globalCorpora(targetDir) {
		paths, err := writeEmbeddedFS(corpus.fs, corpus.destDir, skipFiles)
		if err != nil {
			return nil, fmt.Errorf("failed to write %s: %w", corpus.name, err)
		}
		written = append(written, paths...)
	}

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
// Existing non-brand fields (e.g., skill name/description) are preserved;
// old brand metadata fields are replaced with current build values.
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
	metadataPrefix := brand.MetadataPrefix() + "_"
	for line := range strings.SplitSeq(existingBlock, "\n") {
		if !strings.HasPrefix(line, "liza_") && !strings.HasPrefix(line, metadataPrefix) {
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

// versionFields returns the brand-prefixed key-value lines without YAML delimiters.
func versionFields() string {
	return fmt.Sprintf("%s: \"%s\"\n%s: \"%s\"\n%s: \"%s\"\n",
		brand.MetadataKey("version"), Version,
		brand.MetadataKey("git_commit"), GitCommit,
		brand.MetadataKey("build_date"), BuildDate)
}

// frontmatter generates a complete YAML frontmatter block from build-time variables.
func frontmatter() string {
	return "---\n" + versionFields() + "---\n\n"
}

// ListEmbeddedFiles returns a list of all embedded file paths (for testing).
func ListEmbeddedFiles() ([]string, error) {
	var files []string
	for _, fsys := range []embed.FS{contractsFS, skillsFS, supportDocsFS} {
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

func confirmMergeWithOptions(prompt string, reader *bufio.Reader, opts ConfirmOptions) (bool, error) {
	fmt.Print(prompt)
	if opts.AutoConfirm {
		fmt.Println("yes")
		return true, nil
	}
	response, err := termutil.ReadSingleKey(reader)
	if err != nil {
		return false, fmt.Errorf("failed to read user input: %w", err)
	}
	return response == "y", nil
}

// WriteClaudeSettings writes the embedded claude-settings.json to .claude/settings.json
// and deploys hooks referenced by the settings.
// If the file already exists, prompts the user to merge settings.
// Returns nil on success or if user declines merge.
// The stdin parameter allows for injected input in tests; pass os.Stdin for CLI usage.
func WriteClaudeSettings(projectRoot string, reader *bufio.Reader) error {
	return WriteClaudeSettingsWithOptions(projectRoot, reader, ConfirmOptions{})
}

func WriteClaudeSettingsWithOptions(projectRoot string, reader *bufio.Reader, opts ConfirmOptions) error {
	lizaPaths := paths.New(projectRoot)
	claudeDir := lizaPaths.ClaudeDir()
	settingsPath := lizaPaths.ClaudeSettingsPath()

	var existingSettings map[string]any
	if existingData, err := os.ReadFile(settingsPath); err == nil {
		ok, err := confirmMergeWithOptions(fmt.Sprintf("Should the %s claude settings be merged into the existing settings file? (y/n): ", brand.NameTitle), reader, opts)
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
	if err := json.Unmarshal(renderEmbeddedAsset(claudeSettingsContent), &lizaSettings); err != nil {
		return fmt.Errorf("failed to parse embedded claude-settings.json: %w", err)
	}

	var finalSettings map[string]any
	if existingSettings != nil {
		if mode, ok := existingDefaultMode(existingSettings); ok {
			fmt.Printf("Removing permissions.defaultMode (%q) from %s — project-scope settings do not reliably set the permission mode; set it in your own Claude settings instead.\n", mode, settingsPath)
		}
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

// WriteCodexProjectPermissions merges the active project root, its .git
// directory, and Liza's noninteractive workspace permission baseline into
// ~/.codex/config.toml. Project-local Codex config remains reserved for hooks.
func WriteCodexProjectPermissions(projectRoot string, reader *bufio.Reader) error {
	return WriteCodexProjectPermissionsWithOptions(projectRoot, reader, ConfirmOptions{})
}

func WriteCodexProjectPermissionsWithOptions(projectRoot string, reader *bufio.Reader, opts ConfirmOptions) error {
	if reader == nil {
		reader = bufio.NewReader(os.Stdin)
	}

	configPath, err := codexConfigPath()
	if err != nil {
		return err
	}

	roots, err := codexProjectWritableRoots(projectRoot)
	if err != nil {
		return err
	}
	if err := ensureCodexDir(filepath.Dir(configPath)); err != nil {
		return fmt.Errorf("failed to create .codex directory: %w", err)
	}

	existingData, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		content := renderCodexProjectConfig(roots)
		if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to write codex config: %w", err)
		}
		warnIncompleteCodexBaseline(content)
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read codex config: %w", err)
	}

	merged, changed, err := mergeCodexProjectConfig(string(existingData), roots)
	if err != nil {
		return err
	}
	if !changed {
		warnIncompleteCodexBaseline(string(existingData))
		return nil
	}

	ok, err := confirmMergeWithOptions(fmt.Sprintf("Should %s update ~/.codex/config.toml with this project's Codex permissions? (y/n): ", brand.NameTitle), reader, opts)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	if err := os.WriteFile(configPath, []byte(merged), 0644); err != nil {
		return fmt.Errorf("failed to write codex config: %w", err)
	}
	warnIncompleteCodexBaseline(merged)
	return nil
}

// WriteCodexProjectHooks writes repo-local Codex hook configuration and hook
// scripts to .codex/. Codex also requires the hooks feature flag in an
// active config layer, so this manages the project-local config.toml feature.
func WriteCodexProjectHooks(projectRoot string, reader *bufio.Reader) error {
	return WriteCodexProjectHooksWithOptions(projectRoot, reader, ConfirmOptions{})
}

func WriteCodexProjectHooksWithOptions(projectRoot string, reader *bufio.Reader, opts ConfirmOptions) error {
	if reader == nil {
		reader = bufio.NewReader(os.Stdin)
	}
	codexDir := filepath.Join(projectRoot, ".codex")
	if err := ensureCodexDir(codexDir); err != nil {
		return fmt.Errorf("failed to create .codex directory: %w", err)
	}

	configPath := filepath.Join(codexDir, "config.toml")
	install, configContent, err := prepareCodexHooksFeature(configPath, reader, opts)
	if err != nil {
		return err
	}
	if !install {
		return nil
	}
	hooksOutput, installed, err := renderCodexHooksJSON(filepath.Join(codexDir, "hooks.json"), reader, opts)
	if err != nil {
		return err
	}
	if !installed {
		return nil
	}
	if err := WriteCodexHooks(projectRoot); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(codexDir, "hooks.json"), hooksOutput, 0644); err != nil {
		return fmt.Errorf("failed to write codex hooks.json: %w", err)
	}
	if configContent != "" {
		if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
			return fmt.Errorf("failed to write codex project config: %w", err)
		}
	}
	return nil
}

func ensureCodexDir(codexDir string) error {
	return ensureProviderDir(codexDir)
}

func ensureProviderDir(dir string) error {
	info, err := os.Lstat(dir)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			targetInfo, targetErr := os.Stat(dir)
			if targetErr == nil && targetInfo.IsDir() {
				return nil
			}
			if targetErr != nil {
				return fmt.Errorf("%s exists as symlink: %w", dir, targetErr)
			}
			return fmt.Errorf("%s exists as symlink and is not a directory", dir)
		}
		if info.IsDir() {
			return nil
		}
		if info.Mode().IsRegular() && info.Size() == 0 {
			if err := os.Remove(dir); err != nil {
				return err
			}
			return os.MkdirAll(dir, 0755)
		}
		return fmt.Errorf("%s exists and is not a directory", dir)
	}
	if !os.IsNotExist(err) {
		return err
	}
	return os.MkdirAll(dir, 0755)
}

func prepareCodexHooksFeature(configPath string, reader *bufio.Reader, opts ConfirmOptions) (bool, string, error) {
	existing, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return true, "[features]\nhooks = true\n", nil
	}
	if err != nil {
		return false, "", fmt.Errorf("failed to read codex project config: %w", err)
	}

	merged, changed := mergeCodexHooksFeature(string(existing))
	if !changed {
		return true, "", nil
	}

	ok, err := confirmMergeWithOptions(fmt.Sprintf("Should %s enable Codex hooks in .codex/config.toml? (y/n): ", brand.NameTitle), reader, opts)
	if err != nil {
		return false, "", err
	}
	if !ok {
		return false, "", nil
	}
	return true, merged, nil
}

func mergeCodexHooksFeature(content string) (string, bool) {
	lines := strings.Split(content, "\n")
	sectionStart, sectionEnd := findTomlSection(lines, "features")
	if sectionStart == -1 {
		return appendTomlBlock(content, "[features]\nhooks = true\n"), true
	}

	featureLineStart, featureLineEnd := findTomlAssignment(lines, sectionStart+1, sectionEnd, "hooks")
	if featureLineStart == -1 {
		updated := insertLines(lines, sectionEnd, []string{"hooks = true"})
		return ensureTrailingNewline(strings.Join(updated, "\n")), true
	}

	assignment := strings.TrimSpace(stripTomlLineComment(strings.Join(lines[featureLineStart:featureLineEnd+1], "\n")))
	if assignment == "hooks = true" {
		return content, false
	}

	lines[featureLineStart] = "hooks = true"
	if featureLineEnd > featureLineStart {
		lines = append(lines[:featureLineStart+1], lines[featureLineEnd+1:]...)
	}
	return ensureTrailingNewline(strings.Join(lines, "\n")), true
}

func renderCodexHooksJSON(hooksPath string, reader *bufio.Reader, opts ConfirmOptions) ([]byte, bool, error) {
	var lizaHooks map[string]any
	if err := json.Unmarshal(renderEmbeddedAsset(codexHooksContent), &lizaHooks); err != nil {
		return nil, false, fmt.Errorf("failed to parse embedded codex-hooks.json: %w", err)
	}

	finalHooks := lizaHooks
	if existingData, err := os.ReadFile(hooksPath); err == nil {
		ok, err := confirmMergeWithOptions(fmt.Sprintf("Should the %s Codex hooks be merged into .codex/hooks.json? (y/n): ", brand.NameTitle), reader, opts)
		if err != nil {
			return nil, false, err
		}
		if !ok {
			return nil, false, nil
		}

		var existingHooks map[string]any
		if err := json.Unmarshal(existingData, &existingHooks); err != nil {
			return nil, false, fmt.Errorf("failed to parse existing codex hooks.json: %w", err)
		}
		reconcileCodexHookRegistrations(lizaHooks, existingHooks)
		finalHooks = mergeSettings(lizaHooks, existingHooks)
	} else if err != nil && !os.IsNotExist(err) {
		return nil, false, fmt.Errorf("failed to read codex hooks.json: %w", err)
	}
	output, err := json.MarshalIndent(finalHooks, "", "  ")
	if err != nil {
		return nil, false, fmt.Errorf("failed to marshal codex hooks.json: %w", err)
	}
	return append(output, '\n'), true, nil
}

func codexConfigPath() (string, error) {
	homeDir, err := paths.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, ".codex", "config.toml"), nil
}

func codexSupportWritableRoots() []string {
	homeDir, _ := paths.UserHomeDir()
	cacheDir, _ := os.UserCacheDir()
	return codexconfig.SupportWritableRoots(homeDir, cacheDir)
}

func codexProjectWritableRoots(projectRoot string) ([]string, error) {
	if projectRoot == "" {
		return nil, fmt.Errorf("project root is required")
	}
	absRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve project root: %w", err)
	}
	roots := []string{absRoot, filepath.Join(absRoot, ".git")}
	roots = append(roots, codexSupportWritableRoots()...)
	if runtime.GOOS != "windows" {
		roots = append(roots, "/tmp")
	}
	return codexconfig.UniqueNonEmptyStrings(roots), nil
}

func renderCodexProjectConfig(roots []string) string {
	content := string(codexConfigContent)
	content = strings.ReplaceAll(content, "# {{WRITABLE_ROOTS_BLOCK}}", strings.TrimSuffix(renderWritableRootsBlock("", roots), "\n"))
	return ensureTrailingNewline(content)
}

func mergeCodexProjectConfig(content string, roots []string) (string, bool, error) {
	merged, rootsChanged, err := mergeCodexWritableRoots(content, roots)
	if err != nil {
		return "", false, err
	}
	merged, baselineChanged := mergeCodexWorkspacePermissionBaseline(merged)
	return merged, rootsChanged || baselineChanged, nil
}

func mergeCodexWorkspacePermissionBaseline(content string) (string, bool) {
	recommendedDefaults := []struct {
		section string
		key     string
		value   string
	}{
		{"", "approvals_reviewer", `"auto_review"`},
	}
	requiredAssignments := []struct {
		section string
		key     string
		value   string
	}{
		{"", "approval_policy", `"never"`},
		{"", "sandbox_mode", `"workspace-write"`},
		{"permissions.workspace.network", "enabled", "true"},
		{"sandbox_workspace_write", "network_access", "true"},
		{"sandbox_workspace_write", "exclude_tmpdir_env_var", "false"},
		{"sandbox_workspace_write", "exclude_slash_tmp", "false"},
	}

	updated := content
	changed := false
	for _, assignment := range recommendedDefaults {
		var assignmentChanged bool
		updated, assignmentChanged = ensureTomlAssignmentIfMissing(updated, assignment.section, assignment.key, assignment.value)
		changed = changed || assignmentChanged
	}
	for _, assignment := range requiredAssignments {
		var assignmentChanged bool
		updated, assignmentChanged = ensureTomlAssignment(updated, assignment.section, assignment.key, assignment.value)
		changed = changed || assignmentChanged
	}
	return updated, changed
}

func mergeCodexWritableRoots(content string, roots []string) (string, bool, error) {
	lines := strings.Split(content, "\n")
	sectionStart, sectionEnd := findTomlSection(lines, "sandbox_workspace_write")
	if sectionStart == -1 {
		addition := "[sandbox_workspace_write]\n" + renderWritableRootsBlock("", roots)
		return appendTomlBlock(content, addition), true, nil
	}

	rootLineStart, rootLineEnd := findTomlAssignment(lines, sectionStart+1, sectionEnd, "writable_roots")
	if rootLineStart == -1 {
		block := renderWritableRootsBlock("", roots)
		updated := insertLines(lines, sectionEnd, strings.Split(strings.TrimSuffix(block, "\n"), "\n"))
		return strings.Join(updated, "\n"), true, nil
	}

	existingRoots := extractTomlStrings(strings.Join(lines[rootLineStart:rootLineEnd+1], "\n"))
	existingSet := map[string]bool{}
	for _, root := range existingRoots {
		existingSet[root] = true
	}

	var missing []string
	for _, root := range roots {
		if !existingSet[root] {
			missing = append(missing, root)
		}
	}
	if len(missing) == 0 {
		return content, false, nil
	}

	if rootLineStart == rootLineEnd {
		lines[rootLineStart] = appendToInlineTomlArray(lines[rootLineStart], missing)
		return strings.Join(lines, "\n"), true, nil
	}

	indent := inferArrayValueIndent(lines[rootLineStart : rootLineEnd+1])
	var additions []string
	for _, root := range missing {
		additions = append(additions, indent+tomlStringValue(root)+",")
	}
	updated := insertLines(lines, rootLineEnd, additions)
	return strings.Join(updated, "\n"), true, nil
}

func ensureTomlAssignment(content, section, key, value string) (string, bool) {
	lines := strings.Split(content, "\n")
	start, end := tomlAssignmentRange(lines, section)
	if start == -1 {
		block := fmt.Sprintf("[%s]\n%s = %s\n", section, key, value)
		return appendTomlBlock(content, block), true
	}

	lineStart, lineEnd := findTomlAssignment(lines, start, end, key)
	wanted := key + " = " + value
	if lineStart == -1 {
		updated := insertLines(lines, end, []string{wanted})
		return ensureTrailingNewline(strings.Join(updated, "\n")), true
	}
	if lineEnd > lineStart {
		return content, false
	}

	current := strings.TrimSpace(stripTomlLineComment(strings.Join(lines[lineStart:lineEnd+1], "\n")))
	if current == wanted {
		return content, false
	}

	lines[lineStart] = wanted
	if lineEnd > lineStart {
		lines = append(lines[:lineStart+1], lines[lineEnd+1:]...)
	}
	return ensureTrailingNewline(strings.Join(lines, "\n")), true
}

func ensureTomlAssignmentIfMissing(content, section, key, value string) (string, bool) {
	lines := strings.Split(content, "\n")
	start, end := tomlAssignmentRange(lines, section)
	if start == -1 {
		block := fmt.Sprintf("[%s]\n%s = %s\n", section, key, value)
		return appendTomlBlock(content, block), true
	}

	lineStart, _ := findTomlAssignment(lines, start, end, key)
	if lineStart != -1 {
		return content, false
	}

	updated := insertLines(lines, end, []string{key + " = " + value})
	return ensureTrailingNewline(strings.Join(updated, "\n")), true
}

func tomlAssignmentRange(lines []string, section string) (int, int) {
	if section != "" {
		sectionStart, sectionEnd := findTomlSection(lines, section)
		if sectionStart == -1 {
			return -1, -1
		}
		return sectionStart + 1, sectionEnd
	}

	for i, line := range lines {
		if _, ok := tomlHeaderName(line); ok {
			return 0, i
		}
	}
	return 0, len(lines)
}

func findTomlSection(lines []string, name string) (int, int) {
	start := -1
	for i, line := range lines {
		header, ok := tomlHeaderName(line)
		if !ok {
			continue
		}
		if start == -1 {
			if header == name {
				start = i
			}
			continue
		}
		return start, i
	}
	if start == -1 {
		return -1, -1
	}
	return start, len(lines)
}

func tomlHeaderName(line string) (string, bool) {
	trimmed := strings.TrimSpace(stripTomlLineComment(line))
	if !strings.HasPrefix(trimmed, "[") || !strings.HasSuffix(trimmed, "]") {
		return "", false
	}
	if strings.HasPrefix(trimmed, "[[") {
		return "", false
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "["), "]")), true
}

func findTomlAssignment(lines []string, start, end int, key string) (int, int) {
	for i := start; i < end; i++ {
		trimmed := strings.TrimSpace(stripTomlLineComment(lines[i]))
		if !strings.HasPrefix(trimmed, key) {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(trimmed, key))
		if !strings.HasPrefix(rest, "=") {
			continue
		}
		arrayEnd := i
		if strings.Contains(rest, "[") && !strings.Contains(rest, "]") {
			for arrayEnd+1 < end {
				arrayEnd++
				if strings.Contains(stripTomlLineComment(lines[arrayEnd]), "]") {
					break
				}
			}
		}
		return i, arrayEnd
	}
	return -1, -1
}

func stripTomlLineComment(line string) string {
	inString := false
	escaped := false
	for i, r := range line {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && inString {
			escaped = true
			continue
		}
		if r == '"' {
			inString = !inString
			continue
		}
		if r == '#' && !inString {
			return line[:i]
		}
	}
	return line
}

var tomlStringRE = regexp.MustCompile(`"([^"\\]|\\.)*"`)

func extractTomlStrings(content string) []string {
	matches := tomlStringRE.FindAllString(content, -1)
	values := make([]string, 0, len(matches))
	for _, match := range matches {
		value, err := strconv.Unquote(match)
		if err == nil {
			values = append(values, value)
		}
	}
	return values
}

func appendToInlineTomlArray(line string, roots []string) string {
	closeIndex := strings.LastIndex(line, "]")
	if closeIndex == -1 {
		return line
	}

	before := strings.TrimRight(line[:closeIndex], " \t")
	after := line[closeIndex:]
	separator := ""
	if !strings.HasSuffix(strings.TrimSpace(before), "[") {
		separator = ","
	}

	values := make([]string, 0, len(roots))
	for _, root := range roots {
		values = append(values, tomlStringValue(root))
	}
	return before + separator + " " + strings.Join(values, ", ") + after
}

func inferArrayValueIndent(lines []string) string {
	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "]") {
			continue
		}
		return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	}
	return "  "
}

func renderWritableRootsBlock(indent string, roots []string) string {
	var builder strings.Builder
	builder.WriteString(indent)
	builder.WriteString("writable_roots = [\n")
	for _, root := range roots {
		builder.WriteString(indent)
		builder.WriteString("  ")
		builder.WriteString(tomlStringValue(root))
		builder.WriteString(",\n")
	}
	builder.WriteString(indent)
	builder.WriteString("]\n")
	return builder.String()
}

func insertLines(lines []string, index int, insert []string) []string {
	updated := make([]string, 0, len(lines)+len(insert))
	updated = append(updated, lines[:index]...)
	updated = append(updated, insert...)
	updated = append(updated, lines[index:]...)
	return updated
}

func appendTomlBlock(content, block string) string {
	if strings.TrimSpace(content) == "" {
		return ensureTrailingNewline(block)
	}
	return ensureTrailingNewline(content) + "\n" + ensureTrailingNewline(block)
}

func ensureTrailingNewline(content string) string {
	if strings.HasSuffix(content, "\n") {
		return content
	}
	return content + "\n"
}

func tomlStringValue(value string) string {
	return strconv.Quote(value)
}

func warnIncompleteCodexBaseline(content string) {
	if codexBaselineLooksComplete(content) {
		return
	}
	fmt.Fprintf(os.Stderr, "Warning: Codex config is missing part of the recommended %s baseline. For the full recommended Codex setup, see support-docs/CONFIGURATION.md#codex-project-permissions.\n", brand.NameTitle)
}

func codexBaselineLooksComplete(content string) bool {
	requiredAssignments := []struct {
		section string
		key     string
		value   string
	}{
		{"", "approval_policy", `"never"`},
		{"", "sandbox_mode", `"workspace-write"`},
		{"permissions.workspace.network", "enabled", "true"},
		{"sandbox_workspace_write", "network_access", "true"},
		{"sandbox_workspace_write", "exclude_tmpdir_env_var", "false"},
		{"sandbox_workspace_write", "exclude_slash_tmp", "false"},
	}
	recommendedKeys := []struct {
		section string
		key     string
	}{
		{"", "approvals_reviewer"},
	}
	requiredSnippets := []string{
		".codex",
		paths.GlobalDirName(),
		".npm",
		".pyenv",
	}
	for _, assignment := range requiredAssignments {
		if !tomlAssignmentMatches(content, assignment.section, assignment.key, assignment.value) {
			return false
		}
	}
	for _, recommendedKey := range recommendedKeys {
		if !tomlAssignmentExists(content, recommendedKey.section, recommendedKey.key) {
			return false
		}
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(content, snippet) {
			return false
		}
	}
	return true
}

func tomlAssignmentExists(content, section, key string) bool {
	lines := strings.Split(content, "\n")
	start, end := tomlAssignmentRange(lines, section)
	if start == -1 {
		return false
	}
	lineStart, _ := findTomlAssignment(lines, start, end, key)
	return lineStart != -1
}

func tomlAssignmentMatches(content, section, key, value string) bool {
	lines := strings.Split(content, "\n")
	start, end := tomlAssignmentRange(lines, section)
	if start == -1 {
		return false
	}
	lineStart, lineEnd := findTomlAssignment(lines, start, end, key)
	if lineStart == -1 || lineEnd > lineStart {
		return false
	}
	current := strings.TrimSpace(stripTomlLineComment(strings.Join(lines[lineStart:lineEnd+1], "\n")))
	return current == key+" = "+value
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
		case "additionalDirectories":
			result[k] = v
		default:
			result[k] = v
		}
	}
	mergeTopLevelAdditionalDirectories(result, liza["additionalDirectories"])
	mergeTopLevelAdditionalDirectories(result, existing["additionalDirectories"])
	delete(result, "additionalDirectories")

	return result
}

// mergePermissions merges permission objects.
// Existing values override liza defaults, except "allow" which is unioned.
//
// "defaultMode" is dropped unconditionally: project-scope settings are only
// inconsistently honored by Claude Code (some values activate, others are
// ignored) and an entry here shadows the mode the user set in their own
// settings. Agents get their mode from the launch flag instead — see the
// claude entry in internal/providers/embedded.go.
func mergePermissions(liza, existing map[string]any) map[string]any {
	result := make(map[string]any)
	maps.Copy(result, liza)

	for k, v := range existing {
		switch k {
		case "allow", "additionalDirectories":
			lizaAllow, lizaOk := liza[k].([]any)
			existingAllow, existingOk := v.([]any)
			if lizaOk && existingOk {
				result[k] = unionStringArrays(lizaAllow, existingAllow)
			} else if existingOk {
				result[k] = v
			}
		default:
			result[k] = v
		}
	}
	delete(result, "defaultMode")

	return result
}

// existingDefaultMode reports the permissions.defaultMode value already present
// in a settings file, so the caller can tell the user it is being removed.
func existingDefaultMode(settings map[string]any) (string, bool) {
	perms, ok := settings["permissions"].(map[string]any)
	if !ok {
		return "", false
	}
	mode, ok := perms["defaultMode"].(string)
	return mode, ok
}

func mergeTopLevelAdditionalDirectories(result map[string]any, value any) {
	dirs, ok := value.([]any)
	if !ok {
		return
	}

	perms, ok := result["permissions"].(map[string]any)
	if !ok {
		perms = make(map[string]any)
		result["permissions"] = perms
	}

	existingDirs, _ := perms["additionalDirectories"].([]any)
	perms["additionalDirectories"] = unionStringArrays(existingDirs, dirs)
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

func generatedCodexHookCommand(script string) string {
	return fmt.Sprintf(
		`root=$(git rev-parse --show-toplevel 2>/dev/null) || exit 0; bash "$root/.codex/hooks/%s"`,
		script,
	)
}

func brandedCodexHookCommand(script, nameTitle, binaryName string) string {
	return fmt.Sprintf(
		`root=$(git rev-parse --show-toplevel 2>/dev/null) || exit 0; hook="$root/.codex/hooks/%s"; [ -x "$hook" ] || { echo "Missing %s Codex hook: $hook. Run '%s init --codex' to repair project hooks, or remove .codex/hooks.json to disable them." >&2; exit 1; }; bash "$hook"`,
		script,
		nameTitle,
		binaryName,
	)
}

func hookEntryCommands(entry any) []string {
	entryMap, ok := entry.(map[string]any)
	if !ok {
		return nil
	}
	if cmd, ok := entryMap["command"].(string); ok {
		return []string{cmd}
	}
	hooks, ok := entryMap["hooks"].([]any)
	if !ok {
		return nil
	}
	var commands []string
	for _, hook := range hooks {
		hookMap, ok := hook.(map[string]any)
		if !ok {
			continue
		}
		if cmd, ok := hookMap["command"].(string); ok {
			commands = append(commands, cmd)
		}
	}
	return commands
}

func currentGeneratedCodexHookID(command string) string {
	for script := range codexHookContents {
		if command == generatedCodexHookCommand(script) {
			return script
		}
	}
	return ""
}

func obsoleteGeneratedCodexHookID(command string) string {
	values := brand.RuntimeValues()
	for script := range codexHookContents {
		knownObsolete := []string{
			fmt.Sprintf(`bash "$(git rev-parse --show-toplevel)/.codex/hooks/%s"`, script),
			brandedCodexHookCommand(script, values.NameTitle, values.BinaryName),
		}
		if values.NameLower != values.BinaryName {
			knownObsolete = append(knownObsolete, brandedCodexHookCommand(script, values.NameTitle, values.NameLower))
		}
		if values.NameTitle != "Liza" || values.BinaryName != "liza" {
			knownObsolete = append(knownObsolete, brandedCodexHookCommand(script, "Liza", "liza"))
		}
		for _, known := range knownObsolete {
			if command == known {
				return script
			}
		}
	}
	return ""
}

// reconcileCodexHookRegistrations removes only exact, known obsolete generated
// registrations when the current Codex hook for the same event and script exists.
func reconcileCodexHookRegistrations(current, existing map[string]any) {
	currentHooks, currentOK := current["hooks"].(map[string]any)
	existingHooks, existingOK := existing["hooks"].(map[string]any)
	if !currentOK || !existingOK {
		return
	}

	for event, existingValue := range existingHooks {
		existingEntries, existingOK := existingValue.([]any)
		currentEntries, currentOK := currentHooks[event].([]any)
		if !existingOK || !currentOK {
			continue
		}

		currentIDs := make(map[string]bool)
		for _, entry := range currentEntries {
			commands := hookEntryCommands(entry)
			if len(commands) == 1 {
				if id := currentGeneratedCodexHookID(commands[0]); id != "" {
					currentIDs[id] = true
				}
			}
		}

		retained := make([]any, 0, len(existingEntries))
		for _, entry := range existingEntries {
			commands := hookEntryCommands(entry)
			if len(commands) == 1 {
				if id := obsoleteGeneratedCodexHookID(commands[0]); id != "" && currentIDs[id] {
					continue
				}
			}
			retained = append(retained, entry)
		}
		existingHooks[event] = retained
	}
}

// unionHookEntries preserves every existing entry verbatim. Existing commands
// take precedence over generated entries on exact collision.
func unionHookEntries(a, b []any) []any {
	existingCmds := make(map[string]bool)
	for _, entry := range b {
		for _, cmd := range hookEntryCommands(entry) {
			existingCmds[cmd] = true
		}
	}

	// Add liza entries whose commands don't collide with existing.
	var result []any
	for _, entry := range a {
		cmds := hookEntryCommands(entry)
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
	return WritePipelineConfigWithOptions(targetDir, stdin, ConfirmOptions{})
}

func WritePipelineConfigWithOptions(targetDir string, stdin *bufio.Reader, opts ConfirmOptions) error {
	pipelinePath := filepath.Join(targetDir, "pipeline.yaml")
	if _, err := os.Stat(pipelinePath); err == nil {
		if stdin == nil {
			return nil
		}
		ok, err := confirmMergeWithOptions("pipeline.yaml already exists. Overwrite with embedded version? (y/n): ", stdin, opts)
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
	if err := os.WriteFile(guardrailsPath, renderEmbeddedAsset(guardrailsTemplateContent), 0644); err != nil {
		return fmt.Errorf("failed to write GUARDRAILS.md: %w", err)
	}
	return nil
}

// WriteClaudeIgnore writes the embedded .claudeignore template to the project root.
// If the file already exists, prompts the user to overwrite.
// reader may be nil (non-interactive/test callers) — skips silently if file exists.
func WriteClaudeIgnore(projectRoot string, reader *bufio.Reader) error {
	return WriteClaudeIgnoreWithOptions(projectRoot, reader, ConfirmOptions{})
}

func WriteClaudeIgnoreWithOptions(projectRoot string, reader *bufio.Reader, opts ConfirmOptions) error {
	ignorePath := filepath.Join(projectRoot, ".claudeignore")
	if _, err := os.Stat(ignorePath); err == nil {
		if reader == nil {
			return nil
		}
		ok, err := confirmMergeWithOptions(fmt.Sprintf(".claudeignore already exists. Overwrite with %s template? (y/n): ", brand.NameTitle), reader, opts)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
	}
	if err := os.WriteFile(ignorePath, renderEmbeddedAsset(claudeIgnoreContent), 0644); err != nil {
		return fmt.Errorf("failed to write .claudeignore: %w", err)
	}
	return nil
}

// WriteBashPolicyConfig writes the embedded bash-policy.yaml to .bash-policy.yaml
// in the project root. Existing files are overwritten only after confirmation.
func WriteBashPolicyConfig(projectRoot string, reader *bufio.Reader) error {
	return WriteBashPolicyConfigWithOptions(projectRoot, reader, ConfirmOptions{})
}

func WriteBashPolicyConfigWithOptions(projectRoot string, reader *bufio.Reader, opts ConfirmOptions) error {
	if reader == nil {
		reader = bufio.NewReader(os.Stdin)
	}
	policyPath := filepath.Join(projectRoot, ".bash-policy.yaml")
	if _, err := os.Stat(policyPath); err == nil {
		ok, err := confirmMergeWithOptions(fmt.Sprintf(".bash-policy.yaml already exists. Overwrite with %s template? (y/n): ", brand.NameTitle), reader, opts)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("checking .bash-policy.yaml: %w", err)
	}
	if err := os.WriteFile(policyPath, bashPolicyContent, 0644); err != nil {
		return fmt.Errorf("failed to write .bash-policy.yaml: %w", err)
	}
	return nil
}

// WriteSupportDoc writes the canonical SUPPORT.md body to the runtime
// directory. Always overwrites — content tracks the binary version.
func WriteSupportDoc(lizaDir string) error {
	content, err := supportDocsFS.ReadFile(supportDocEmbeddedPath)
	if err != nil {
		return fmt.Errorf("failed to read embedded SUPPORT.md: %w", err)
	}
	supportPath := filepath.Join(lizaDir, "SUPPORT.md")
	if err := os.WriteFile(supportPath, content, 0644); err != nil {
		return fmt.Errorf("failed to write SUPPORT.md: %w", err)
	}
	return nil
}

// RenderWorktreePreCommitHook returns the rendered pre-commit hook script for a
// task worktree with the liza binary path and task ID baked in. Callers write
// the result to the worktree's hooks directory (chmod 0755) and configure
// git's core.hooksPath to point at it.
func RenderWorktreePreCommitHook(lizaBin, taskID string) []byte {
	out := bytes.ReplaceAll(worktreePreCommitHookContent, []byte("__LIZA_BIN__"), []byte(lizaBin))
	out = bytes.ReplaceAll(out, []byte("__TASK_ID__"), []byte(taskID))
	return renderEmbeddedAsset(out)
}

// WriteHooks writes embedded hook scripts to .claude/hooks/ in the project root.
// Always overwrites — hooks are Liza infrastructure, not user-customizable.
func WriteHooks(projectRoot string) error {
	hooksDir := filepath.Join(projectRoot, ".claude", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		return fmt.Errorf("failed to create .claude/hooks directory: %w", err)
	}

	for name, content := range map[string][]byte{
		"enforce-init.sh":        enforceInitHookContent,
		"session-context.sh":     sessionContextHookContent,
		"git-guard.sh":           gitGuardHookContent,
		"rtk-guard.sh":           rtkGuardHookContent,
		"worktree-path-guard.sh": worktreePathGuardHookContent,
	} {
		hookPath := filepath.Join(hooksDir, name)
		if err := os.WriteFile(hookPath, renderEmbeddedAsset(content), 0755); err != nil {
			return fmt.Errorf("failed to write %s: %w", name, err)
		}
	}

	return nil
}

// WriteCodexHooks writes embedded hook scripts to .codex/hooks/ in the project
// root. Always overwrites — hooks are Liza infrastructure, not user-customizable.
func WriteCodexHooks(projectRoot string) error {
	hooksDir := filepath.Join(projectRoot, ".codex", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		return fmt.Errorf("failed to create .codex/hooks directory: %w", err)
	}

	for name, content := range codexHookContents {
		hookPath := filepath.Join(hooksDir, name)
		if err := os.WriteFile(hookPath, renderEmbeddedAsset(content), 0755); err != nil {
			return fmt.Errorf("failed to write %s: %w", name, err)
		}
	}

	return nil
}

// WriteOpenCodeExecTool writes Liza's project-local OpenCode exec compatibility
// tool to .opencode/tools/exec.ts. Existing user-owned files are preserved; only
// Liza-managed copies are overwritten.
func WriteOpenCodeExecTool(projectRoot string) error {
	toolPath := filepath.Join(projectRoot, ".opencode", "tools", "exec.ts")
	existing, err := os.ReadFile(toolPath)
	if err == nil {
		if !bytes.HasPrefix(existing, []byte(OpenCodeExecToolManagedHeader())) {
			return nil
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to read opencode exec tool: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(toolPath), 0755); err != nil {
		return fmt.Errorf("failed to create .opencode/tools directory: %w", err)
	}
	if err := os.WriteFile(toolPath, renderEmbeddedAsset(opencodeExecToolContent), 0644); err != nil {
		return fmt.Errorf("failed to write opencode exec tool: %w", err)
	}
	return nil
}
