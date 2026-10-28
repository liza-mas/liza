package embedded

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/codexconfig"
	"github.com/liza-mas/liza/internal/paths"
)

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
		warnCodexAppArmorSandboxIfNeeded()
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
		warnCodexAppArmorSandboxIfNeeded()
		return nil
	}

	ok, err := confirmMergeWithOptions(fmt.Sprintf("Should %s update ~/.codex/config.toml with this project's Codex permissions? (y/n): ", brand.NameTitle), reader, opts)
	if err != nil {
		return err
	}
	if !ok {
		warnCodexAppArmorSandboxIfNeeded()
		return nil
	}

	if err := os.WriteFile(configPath, []byte(merged), 0644); err != nil {
		return fmt.Errorf("failed to write codex config: %w", err)
	}
	warnIncompleteCodexBaseline(merged)
	warnCodexAppArmorSandboxIfNeeded()
	return nil
}

func codexConfigPath() (string, error) {
	homeDir, err := paths.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, ".codex", "config.toml"), nil
}

const (
	appArmorRestrictUnprivilegedUsernsPath = "/proc/sys/kernel/apparmor_restrict_unprivileged_userns"
	appArmorLoadedProfilesPath             = "/sys/kernel/security/apparmor/profiles"
	bwrapUsernsRestrictProfilePath         = "/etc/apparmor.d/bwrap-userns-restrict"
	systemBwrapPath                        = "/usr/bin/bwrap"
)

var warnCodexAppArmorSandboxIfNeeded = defaultWarnCodexAppArmorSandboxIfNeeded

func defaultWarnCodexAppArmorSandboxIfNeeded() {
	for _, warning := range codexAppArmorSandboxWarnings(runtime.GOOS, os.ReadFile, os.Stat) {
		fmt.Fprintln(os.Stderr, warning)
	}
}

func codexAppArmorSandboxWarnings(
	goos string,
	readFile func(string) ([]byte, error),
	stat func(string) (os.FileInfo, error),
) []string {
	if goos != "linux" {
		return nil
	}

	data, err := readFile(appArmorRestrictUnprivilegedUsernsPath)
	if err != nil || strings.TrimSpace(string(data)) != "1" {
		return nil
	}

	var details []string
	if _, err := stat(systemBwrapPath); err != nil {
		details = append(details, "Install the bubblewrap package so Codex can use /usr/bin/bwrap.")
	}

	profileInstalled := false
	if _, err := stat(bwrapUsernsRestrictProfilePath); err != nil {
		details = append(details, "Install and load the bwrap-userns-restrict AppArmor profile.")
	} else {
		profileInstalled = true
	}

	if profileInstalled {
		loadedProfiles, err := readFile(appArmorLoadedProfilesPath)
		switch {
		case err != nil:
			details = append(details, "Could not verify that bwrap-userns-restrict is loaded in enforce mode.")
		case !appArmorProfileEnforced(loadedProfiles, "bwrap-userns-restrict"):
			details = append(details, "The bwrap-userns-restrict profile exists but does not appear to be loaded in enforce mode.")
		}
	}

	if len(details) == 0 {
		return nil
	}

	warnings := []string{
		"Warning: Codex sandbox may fail on Ubuntu 24.04+ systems because AppArmor restricts unprivileged user namespaces.",
	}
	warnings = append(warnings, details...)
	warnings = append(warnings, "See support-docs/CONFIGURATION.md for manual setup.")
	return warnings
}

func appArmorProfileEnforced(loadedProfiles []byte, profile string) bool {
	for _, line := range strings.Split(string(loadedProfiles), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 1 && (fields[0] == profile || filepath.Base(fields[0]) == profile) && fields[1] == "(enforce)" {
			return true
		}
	}
	return false
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
