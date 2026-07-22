package commands

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/providers"
	"github.com/liza-mas/liza/internal/termutil"
)

// userCustomizableFiles are files that users are expected to edit.
// These get individual overwrite prompts even after the bulk confirmation,
// because losing customizations is more costly than re-running setup.
var userCustomizableFiles = map[string]bool{
	"AGENT_TOOLS.md": true,
}

// retiredSkillNames are formerly deployed skills that setup removes during an
// upgrade. Keeping this list explicit prevents deleting user-created skills.
var retiredSkillNames = []string{
	"code-cleaning",
}

// SetupParams holds all parameters for the setup command.
type SetupParams struct {
	TargetDir      string    // target directory (typically the branded global directory)
	Force          bool      // overwrite existing files
	AutoConfirm    bool      // auto-confirm interactive approval prompts
	AgentToolsPath string    // path to custom AGENT_TOOLS.md (empty = use embedded)
	ProviderIDs    []string  // explicit catalog provider IDs from --provider
	Agents         []string  // shortcut agent flags to create skill symlinks for (e.g. "claude", "codex", "opencode")
	HomeDir        string    // home directory override (empty = os.UserHomeDir())
	Stdin          io.Reader // input for interactive prompts (nil = os.Stdin)
}

// SetupCommand performs one-time global setup by writing contracts and skills
// to the target directory.
func SetupCommand(params SetupParams) error {
	rawStdin := params.Stdin
	if rawStdin == nil {
		rawStdin = os.Stdin
	}
	// Single shared buffered reader — avoids multiple bufio.NewReader instances
	// consuming from the same underlying reader (which causes EOF for later readers).
	stdin := bufio.NewReader(rawStdin)

	// Early validation: read custom agent-tools file before any filesystem changes.
	var customAgentTools []byte
	if params.AgentToolsPath != "" {
		content, err := os.ReadFile(params.AgentToolsPath)
		if err != nil {
			return fmt.Errorf("failed to read custom agent-tools file %s: %w", params.AgentToolsPath, err)
		}
		customAgentTools = content
	}

	if err := os.MkdirAll(params.TargetDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", params.TargetDir, err)
	}
	warnLegacyGlobalRoot(params.HomeDir, params.TargetDir)

	planned := embedded.PlanGlobalFiles(params.TargetDir)
	existing, fresh := partitionByExistence(planned)

	// When --agent-tools is provided, auto-skip the embedded AGENT_TOOLS.md
	// so confirmOverwrites won't prompt for it.
	var autoSkip map[string]bool
	if customAgentTools != nil {
		autoSkip = map[string]bool{
			filepath.Join(params.TargetDir, "AGENT_TOOLS.md"): true,
		}
	}
	catalog := loadProviderCatalog(params.HomeDir)
	setupProviderIDs := append([]string{}, params.ProviderIDs...)
	setupProviderIDs = append(setupProviderIDs, canonicalSetupProviderIDs(params.Agents)...)
	selectedProviders, err := resolveCatalogProviders(catalog, setupProviderIDs)
	if err != nil {
		return err
	}

	skipFiles, err := confirmOverwrites(existing, fresh, params.Force, params.AutoConfirm, params.TargetDir, stdin, autoSkip)
	if err != nil {
		return err
	}

	for _, p := range existing {
		if skipFiles[p] {
			continue
		}
		if err := backupFile(p); err != nil {
			return fmt.Errorf("failed to backup %s: %w", p, err)
		}
	}

	written, err := embedded.WriteGlobalFiles(params.TargetDir, skipFiles)
	if err != nil {
		return fmt.Errorf("failed to write global files: %w", err)
	}
	homeDir := params.HomeDir
	if len(selectedProviders) > 0 && homeDir == "" {
		homeDir, err = os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to determine home directory: %w", err)
		}
	}
	if err := removeRetiredSkills(params.TargetDir, homeDir, selectedProviders); err != nil {
		return fmt.Errorf("failed to remove retired skills: %w", err)
	}

	// Write custom AGENT_TOOLS.md, replacing the embedded version.
	var autoReplaced []string
	if customAgentTools != nil {
		agentToolsTarget := filepath.Join(params.TargetDir, "AGENT_TOOLS.md")

		// Back up existing file before overwriting (data-loss protection).
		if _, err := os.Stat(agentToolsTarget); err == nil {
			if err := backupFile(agentToolsTarget); err != nil {
				return fmt.Errorf("failed to backup %s: %w", agentToolsTarget, err)
			}
		}

		contentWithFrontmatter := embedded.PrependFrontmatter(customAgentTools)
		if err := os.WriteFile(agentToolsTarget, contentWithFrontmatter, 0644); err != nil {
			return fmt.Errorf("failed to write custom AGENT_TOOLS.md: %w", err)
		}
		written = append(written, agentToolsTarget)
		autoReplaced = append(autoReplaced, agentToolsTarget)
	}

	if err := embedded.WritePipelineConfigWithOptions(params.TargetDir, stdin, embedded.ConfirmOptions{AutoConfirm: params.AutoConfirm}); err != nil {
		return fmt.Errorf("failed to write pipeline.yaml: %w", err)
	}

	// Create agent skill symlinks (after main setup so sources exist).
	if len(selectedProviders) > 0 {
		if err := setupAgentSymlinks(homeDir, params.TargetDir, selectedProviders, stdin, params.AutoConfirm); err != nil {
			return fmt.Errorf("agent symlink setup failed: %w", err)
		}
	}

	printSetupSummary(params.TargetDir, written, skipFiles, autoReplaced, setupProviderIDs)
	return nil
}

func canonicalSetupProviderIDs(ids []string) []string {
	out := make([]string, 0, len(ids)+1)
	seen := make(map[string]bool, len(ids)+1)
	appendID := func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	for _, id := range ids {
		// Cursor's global setup path reuses Claude/Codex skill discovery; project-local
		// Cursor hooks and contract activation stay in init --cursor.
		if id == "cursor" {
			appendID("claude")
			appendID("codex")
			continue
		}
		appendID(id)
	}
	return out
}

func warnLegacyGlobalRoot(homeDir, targetDir string) {
	if brand.RuntimeValues().GlobalDirName == paths.LizaDirName {
		return
	}
	if homeDir == "" {
		var err error
		homeDir, err = os.UserHomeDir()
		if err != nil {
			return
		}
	}
	legacyDir := filepath.Join(homeDir, paths.LizaDirName)
	if sameCleanPath(legacyDir, targetDir) {
		return
	}
	if _, err := os.Stat(legacyDir); err == nil {
		fmt.Fprintf(os.Stderr, "Warning: legacy global root %s detected; using %s. Legacy state is ignored until explicitly migrated.\n", legacyDir, targetDir)
	}
}

func warnLegacyProjectRoot(projectRoot, activeDir string) {
	if paths.ProjectDirName() == paths.LizaDirName {
		return
	}
	legacyDir := filepath.Join(projectRoot, paths.LizaDirName)
	if sameCleanPath(legacyDir, activeDir) {
		return
	}
	if _, err := os.Stat(legacyDir); err == nil {
		fmt.Fprintf(os.Stderr, "Warning: legacy project root %s detected; using %s. Legacy state is ignored until explicitly migrated.\n", legacyDir, activeDir)
	}
}

func sameCleanPath(left, right string) bool {
	return filepath.Clean(left) == filepath.Clean(right)
}

// partitionByExistence splits paths into those that exist on disk and those that don't.
func partitionByExistence(paths []string) (existing, fresh []string) {
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			existing = append(existing, p)
		} else {
			fresh = append(fresh, p)
		}
	}
	return
}

// confirmOverwrites handles interactive confirmation for overwriting existing files.
// Returns the set of files to skip (user-customizable files the user declined to overwrite,
// plus any auto-skipped files from the autoSkip set).
// Files in autoSkip are added to skipFiles without prompting (e.g., when replaced by --agent-tools).
func confirmOverwrites(existing, fresh []string, force bool, autoConfirm bool, targetDir string, reader *bufio.Reader, autoSkip map[string]bool) (map[string]bool, error) {
	skipFiles := make(map[string]bool)

	// Seed with auto-skipped files (no prompt needed).
	for p := range autoSkip {
		skipFiles[p] = true
	}

	if len(existing) == 0 {
		return skipFiles, nil
	}

	if !force {
		return nil, fmt.Errorf("global config already exists at %s (%d files), use --force to overwrite",
			targetDir, len(existing))
	}

	fmt.Printf("%d existing files will be overwritten:\n", len(existing))
	for _, p := range existing {
		fmt.Printf("  %s\n", relDisplay(targetDir, p))
	}
	if len(fresh) > 0 {
		fmt.Printf("%d new files will be added.\n", len(fresh))
	}
	fmt.Printf("\nOverwrite? (y/n): ")

	if autoConfirm {
		fmt.Println("yes")
	} else {
		response, err := termutil.ReadSingleKey(reader)
		if err != nil {
			return nil, fmt.Errorf("failed to read input: %w", err)
		}
		if response != "y" {
			fmt.Println() // Print newline for clean terminal output
			return nil, fmt.Errorf("aborted by user")
		}
	}
	fmt.Println()

	// Per-file prompts for user-customizable files (skip if auto-skipped).
	for _, p := range existing {
		if autoSkip[p] {
			continue
		}
		base := filepath.Base(p)
		if !userCustomizableFiles[base] {
			continue
		}
		fmt.Fprintf(os.Stderr, "%s is user-customizable and has local changes.\n", base)
		fmt.Fprintf(os.Stderr, "Overwrite %s? (y/n): ", relDisplay(targetDir, p))
		if autoConfirm {
			fmt.Fprintln(os.Stderr, "yes")
			continue
		}
		response, err := termutil.ReadSingleKey(reader)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to read input for %s: %v\n", base, err)
			skipFiles[p] = true
			continue
		}
		if response != "y" {
			fmt.Fprintln(os.Stderr) // Print newline for clean terminal output
			skipFiles[p] = true
			fmt.Fprintf(os.Stderr, "  Skipped %s (kept existing)\n", base)
		}
	}

	return skipFiles, nil
}

// printSetupSummary prints the final setup results to stdout.
// autoReplaced tracks files replaced by custom versions (not "kept existing").
func printSetupSummary(targetDir string, written []string, skipFiles map[string]bool, autoReplaced []string, agents []string) {
	fmt.Printf("%s global config written to %s (%d files + pipeline.yaml):\n", brand.NameTitle, targetDir, len(written))
	for _, p := range written {
		fmt.Printf("  %s\n", relDisplay(targetDir, p))
	}
	fmt.Printf("  %s (pipeline config)\n", relDisplay(targetDir, filepath.Join(targetDir, "pipeline.yaml")))

	// Count user-skipped files (excluding auto-replaced ones).
	keptExisting := 0
	for p := range skipFiles {
		if !slices.Contains(autoReplaced, p) {
			keptExisting++
		}
	}
	if keptExisting > 0 {
		fmt.Printf("Skipped %d user-customized files (kept existing).\n", keptExisting)
	}
	if len(autoReplaced) == 1 {
		fmt.Printf("Replaced 1 file with custom version.\n")
	} else if len(autoReplaced) > 1 {
		fmt.Printf("Replaced %d files with custom versions.\n", len(autoReplaced))
	}

	// Show manual config instructions only for agents that need it.
	hasNonClaude := false
	for _, a := range agents {
		if a != "claude" {
			hasNonClaude = true
			break
		}
	}
	if hasNonClaude {
		fmt.Printf("\nSome agents require manual configuration.\n")
		fmt.Printf("See: https://github.com/%s/blob/main/GETTING_STARTED.md\n", brand.Repo)
	}

	docPath := relDisplay(targetDir, filepath.Join(targetDir, "support-docs", "CUSTOMIZING_AGENT_TOOLS.md"))
	content := fmt.Sprintf(`%s global setup complete

Next steps:
  1. Customize agent tools for your project:
       %s
  2. Enable %s in a project:
       cd your-project && %s init --claude`, brand.NameTitle, docPath, brand.NameTitle, brand.BinaryName)

	style := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("2")).
		Padding(1, 2)

	fmt.Println()
	fmt.Println(style.Render(content))
	fmt.Println()
}

// backupFile copies src to src.bak using streaming I/O.
// Delegates to embedded.BackupFile.
func backupFile(src string) error {
	return embedded.BackupFile(src)
}

// setupAgentSymlinks creates skill symlinks in each agent's config directory.
// For each agent, it symlinks every entry in the global skills directory into the agent's
// skills directory, plus any extra links defined in the agent config.
func setupAgentSymlinks(homeDir, lizaDir string, agents []providers.Provider, reader *bufio.Reader, autoConfirm bool) error {

	for _, provider := range agents {
		cfg := provider.Setup
		if cfg.ConfigDir == "" || cfg.SkillsDir == "" {
			return fmt.Errorf("provider %s does not define setup skill symlinks", provider.ID)
		}

		configDir := filepath.Join(homeDir, cfg.ConfigDir)
		skillsDir := filepath.Join(configDir, cfg.SkillsDir)

		if err := os.MkdirAll(skillsDir, 0755); err != nil {
			return fmt.Errorf("failed to create %s: %w", skillsDir, err)
		}

		for _, dir := range cfg.ExtraDirs {
			dirPath := filepath.Join(configDir, dir)
			if err := os.MkdirAll(dirPath, 0755); err != nil {
				return fmt.Errorf("failed to create %s: %w", dirPath, err)
			}
		}

		// Symlink each skill directory
		sourceSkillsDir := filepath.Join(lizaDir, "skills")
		entries, err := os.ReadDir(sourceSkillsDir)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", sourceSkillsDir, err)
		}

		for _, entry := range entries {
			target := filepath.Join(sourceSkillsDir, entry.Name())
			linkPath := filepath.Join(skillsDir, entry.Name())

			if err := createSymlinkIdempotent(target, linkPath, reader, false, autoConfirm); err != nil {
				return err
			}
		}

		// Create extra links
		for _, extra := range cfg.Symlinks {
			target := filepath.Join(lizaDir, extra.Source)
			linkPath := filepath.Join(configDir, extra.Target)

			if err := os.MkdirAll(filepath.Dir(linkPath), 0755); err != nil {
				return fmt.Errorf("failed to create parent dir for %s: %w", linkPath, err)
			}

			if err := createSymlinkIdempotent(target, linkPath, reader, false, autoConfirm); err != nil {
				return err
			}
		}

		fmt.Printf("Agent %s: skill symlinks created in %s\n", provider.ID, configDir)
	}

	return nil
}

// removeRetiredSkills removes legacy global skill directories and matching
// provider symlinks. The global skills directory is Liza-managed, while
// provider paths are removed only when they target the retired global skill.
func removeRetiredSkills(lizaDir, homeDir string, agents []providers.Provider) error {
	sourceSkillsDir := filepath.Join(lizaDir, "skills")
	for _, skillName := range retiredSkillNames {
		if err := os.RemoveAll(filepath.Join(sourceSkillsDir, skillName)); err != nil {
			return err
		}
	}
	for _, provider := range agents {
		cfg := provider.Setup
		if cfg.ConfigDir == "" || cfg.SkillsDir == "" {
			continue
		}
		skillsDir := filepath.Join(homeDir, cfg.ConfigDir, cfg.SkillsDir)
		for _, skillName := range retiredSkillNames {
			linkPath := filepath.Join(skillsDir, skillName)
			info, err := os.Lstat(linkPath)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return fmt.Errorf("inspect retired skill link %s: %w", linkPath, err)
			}
			if info.Mode()&os.ModeSymlink == 0 {
				continue
			}
			target, err := os.Readlink(linkPath)
			if err != nil {
				return fmt.Errorf("read retired skill link %s: %w", linkPath, err)
			}
			if target == filepath.Join(sourceSkillsDir, skillName) {
				if err := os.Remove(linkPath); err != nil {
					return fmt.Errorf("remove retired skill link %s: %w", linkPath, err)
				}
			}
		}
	}

	return nil
}

// createSymlinkIdempotent creates a symlink at linkPath pointing to target.
// If a correct symlink already exists, it's a no-op.
// If a symlink exists pointing elsewhere, prompts user before replacing.
// If a regular file/dir exists: when promptRegularFiles is true, prompts to
// overwrite; when false, warns and skips.
func createSymlinkIdempotent(target, linkPath string, reader *bufio.Reader, promptRegularFiles bool, autoConfirm bool) error {
	fi, err := os.Lstat(linkPath)
	if err == nil {
		// Something exists at linkPath
		if fi.Mode()&os.ModeSymlink != 0 {
			// It's a symlink — check where it points
			existing, err := os.Readlink(linkPath)
			if err != nil {
				return fmt.Errorf("failed to read symlink %s: %w", linkPath, err)
			}
			if existing == target {
				return nil // already correct
			}
			// Wrong target — ask before replacing
			fmt.Fprintf(os.Stderr, "%s → %s (expected %s)\n", linkPath, existing, target)
			fmt.Fprintf(os.Stderr, "Replace symlink? (y/n): ")
		} else if promptRegularFiles {
			// Regular file/dir — prompt to overwrite
			fmt.Fprintf(os.Stderr, "%s already exists.\n", linkPath)
			fmt.Fprintf(os.Stderr, "Overwrite with symlink to %s? (y/n): ", target)
		} else {
			// Regular file or directory — warn and skip
			fmt.Fprintf(os.Stderr, "Warning: %s exists as regular file/dir, skipping symlink\n", linkPath)
			return nil
		}

		if autoConfirm {
			fmt.Fprintln(os.Stderr, "yes")
		} else {
			response, err := termutil.ReadSingleKey(reader)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to read input, skipping %s\n", linkPath)
				return nil
			}
			if response != "y" {
				return nil
			}
		}
		if err := os.Remove(linkPath); err != nil {
			return fmt.Errorf("failed to remove %s: %w", linkPath, err)
		}
	}

	if err := os.Symlink(target, linkPath); err != nil {
		return fmt.Errorf("failed to create symlink %s → %s: %w\n  On Windows: enable Developer Mode (Settings > System > For developers) or run the shell as Administrator, then retry", linkPath, target, err)
	}
	return nil
}

// relDisplay returns a display path using the targetDir as prefix.
func relDisplay(targetDir, path string) string {
	rel, err := filepath.Rel(targetDir, path)
	if err != nil {
		return path
	}
	return fmt.Sprintf("%s/%s", strings.TrimRight(targetDir, "/"), rel)
}
