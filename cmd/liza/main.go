package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/liza-mas/liza/internal/brand"
	lizaerrors "github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/identity"
	"github.com/liza-mas/liza/internal/interactive"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/updater"
	"github.com/spf13/cobra"
)

var (
	// Version information (set via ldflags during build)
	Version   = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
)

var rootCmd = &cobra.Command{
	Use:   brand.BinaryName,
	Short: fmt.Sprintf("%s - Multi-agent task execution system", brand.NameTitle),
	Long: fmt.Sprintf(`%s is a multi-agent task execution system that uses a YAML-based
"blackboard" pattern with file locking for state management, git worktrees
for task isolation, and agent supervisors with restart logic.`, brand.NameTitle),
	SilenceUsage:  true,
	SilenceErrors: true,
}

var deleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete agents or tasks from the state database",
	Long:  `Delete agents that crashed or tasks that are no longer needed.`,
}

func requireProjectRoot() (string, error) {
	explicitRoot, _ := rootCmd.PersistentFlags().GetString("project-root")
	if explicitRoot != "" {
		return requireExplicitProjectRoot(explicitRoot)
	}

	projectRoot, err := paths.GetProjectRoot()
	if err != nil {
		return "", &lizaerrors.ProjectRootError{Operation: rootCmd.CommandPath(), Err: err}
	}
	currentDir, err := os.Getwd()
	if err != nil {
		return "", &lizaerrors.ProjectRootError{Operation: rootCmd.CommandPath(), Err: err}
	}
	canonicalProjectRoot, err := canonicalDir(projectRoot)
	if err != nil {
		return "", &lizaerrors.ProjectRootError{Operation: rootCmd.CommandPath(), Err: err}
	}
	canonicalCurrentDir, err := canonicalDir(currentDir)
	if err != nil {
		return "", &lizaerrors.ProjectRootError{Operation: rootCmd.CommandPath(), Err: err}
	}
	if canonicalCurrentDir != canonicalProjectRoot && !paths.IsLizaTaskWorktree(projectRoot, currentDir) {
		return "", &lizaerrors.ProjectRootError{
			Operation:    rootCmd.CommandPath(),
			CurrentDir:   currentDir,
			ExpectedRoot: projectRoot,
			Err:          fmt.Errorf("current directory is not project root"),
		}
	}
	return projectRoot, nil
}

func requireExplicitProjectRoot(root string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", &lizaerrors.ProjectRootError{
			Message:   fmt.Sprintf("failed to resolve --project-root %q", root),
			Operation: rootCmd.CommandPath(),
			Err:       err,
		}
	}
	projectRoot, err := paths.GetProjectRootFromDir(absRoot)
	if err != nil {
		return "", &lizaerrors.ProjectRootError{
			Message:      fmt.Sprintf("--project-root %q is not a git repository", root),
			Operation:    rootCmd.CommandPath(),
			ExpectedRoot: root,
			Err:          err,
		}
	}
	canonicalFlagRoot, err := canonicalDir(absRoot)
	if err != nil {
		return "", &lizaerrors.ProjectRootError{
			Message:      fmt.Sprintf("failed to resolve --project-root %q", root),
			Operation:    rootCmd.CommandPath(),
			ExpectedRoot: root,
			Err:          err,
		}
	}
	canonicalProjectRoot, err := canonicalDir(projectRoot)
	if err != nil {
		return "", &lizaerrors.ProjectRootError{Operation: rootCmd.CommandPath(), Err: err}
	}
	if canonicalFlagRoot != canonicalProjectRoot {
		return "", &lizaerrors.ProjectRootError{
			Message:      fmt.Sprintf("--project-root must point at the %s project root %s, got %s", brand.NameTitle, projectRoot, absRoot),
			Operation:    rootCmd.CommandPath(),
			ExpectedRoot: projectRoot,
			Err:          fmt.Errorf("explicit project root is not repository root"),
		}
	}
	projectDir := paths.New(projectRoot).LizaDir()
	if info, err := os.Stat(projectDir); err != nil || !info.IsDir() {
		if err == nil {
			err = fmt.Errorf("path exists but is not a directory")
		}
		return "", &lizaerrors.ProjectRootError{
			Message:      fmt.Sprintf("--project-root %s is not a %s project root: missing %s directory", projectRoot, brand.NameTitle, paths.ProjectDirName()),
			Operation:    rootCmd.CommandPath(),
			ExpectedRoot: projectRoot,
			Err:          err,
		}
	}
	return projectRoot, nil
}

func canonicalDir(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(absPath)
}

func requireAgentID(cmd *cobra.Command) (string, error) {
	flagValue, _ := cmd.Flags().GetString("agent-id")
	agentID, err := identity.Resolve(identity.Config{
		FlagValue: flagValue,
		Required:  true,
	})
	if err != nil {
		return "", fmt.Errorf("agent ID required (use --agent-id flag or %s env var; legacy env alias is also accepted): %w", brand.EnvName("AGENT_ID"), err)
	}
	return agentID, nil
}

func cliValidationError(message string) error {
	return &lizaerrors.CLIInputError{Message: message}
}

func cliValidationWrap(message string, err error) error {
	return &lizaerrors.CLIInputError{
		Message: fmt.Sprintf("%s: %v", message, err),
		Err:     err,
	}
}

func checkSupportedPlatform(goos string) error {
	if goos == "windows" {
		return cliValidationError(fmt.Sprintf("native Windows is not supported; run %s under WSL2", brand.BinaryName))
	}
	return nil
}

// resolveOrchestratorID resolves the orchestrator agent ID from flag, env var,
// or workspace state (the registered orchestrator). Used by commands that default
// to the orchestrator identity when no explicit agent ID is provided.
func resolveOrchestratorID(cmd *cobra.Command) (string, error) {
	flagValue, _ := cmd.Flags().GetString("agent-id")
	agentID, _ := identity.Resolve(identity.Config{
		FlagValue: flagValue,
		Required:  false,
	})
	if agentID != "" {
		return agentID, nil
	}

	projectRoot, err := requireProjectRoot()
	if err != nil {
		return "", err
	}

	// Load resolver for type-based orchestrator resolution.
	// If loading fails, pass nil to fall back to literal role-name match.
	var resolver *pipeline.Resolver
	if cfg, loadErr := pipeline.LoadFrozen(projectRoot); loadErr == nil {
		resolver = pipeline.NewResolver(cfg)
	}

	lp := paths.New(projectRoot)
	resolved, err := ops.ResolveOrchestratorFromState(lp.StatePath(), resolver)
	if err != nil {
		return "", fmt.Errorf("--agent-id not provided and auto-resolution failed: %w", err)
	}
	return resolved, nil
}

func resolveChangedBy(cmd *cobra.Command) string {
	flagValue, _ := cmd.Flags().GetString("changed-by")
	changedBy, _ := identity.Resolve(identity.Config{
		FlagValue:    flagValue,
		DefaultValue: "human",
		Required:     false,
	})
	return changedBy
}

// defaultPipelineConfigPath returns the global pipeline.yaml if it exists,
// or empty string otherwise (no global setup, or home dir unresolvable).
func defaultPipelineConfigPath() string {
	globalDir, err := paths.GlobalLizaDir()
	if err != nil {
		return ""
	}
	p := filepath.Join(globalDir, "pipeline.yaml")
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

func init() {
	rootCmd.AddCommand(deleteCmd)

	// Global flags
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().StringP("project-root", "C", "", brand.NameTitle+" project root for state commands")
	// Note: --check-update and --update-channel are registered here for Cobra help visibility,
	// but the updater package manually pre-parses these flags before Cobra command execution.
	// This allows update checks to run before the main command, while still showing these flags
	// in --help output. The manual parsing in internal/updater implements stop-at-double-dash
	// and last-flag-wins semantics to match pflag/Cobra behavior for these specific flags.
	// Update channel validation (stable/main) is performed during this pre-parsing phase,
	// and invalid values cause a fatal error before Cobra command execution.
	rootCmd.PersistentFlags().Bool("check-update", false, "check for a "+brand.NameTitle+" update before running")
	rootCmd.PersistentFlags().String("update-channel", "stable", "update check channel: stable or main")
}

// addAgentIDFlag registers --agent-id on a specific command.
func addAgentIDFlag(cmd *cobra.Command) {
	cmd.Flags().String("agent-id", "", "agent identifier (overrides "+brand.EnvName("AGENT_ID")+" env var; legacy env alias is also accepted)")
}

// addJSONFlag registers --json on a specific command.
func addJSONFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("json", false, "output result as structured JSON")
}

// isJSON returns true if --json flag is set on the command.
func isJSON(cmd *cobra.Command) bool {
	v, _ := cmd.Flags().GetBool("json")
	return v
}

// addChangedByFlag registers --changed-by on a specific command.
func addChangedByFlag(cmd *cobra.Command) {
	cmd.Flags().String("changed-by", "", "identifier for audit trail (overrides "+brand.EnvName("AGENT_ID")+" env var, defaults to 'human')")
}

func main() {
	if err := checkSupportedPlatform(runtime.GOOS); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if err := updater.MaybeUpdateAndReexec(context.Background(), updater.Config{
		CurrentVersion: Version,
		CurrentCommit:  GitCommit,
		IsInteractive:  interactive.IsInteractive,
	}); err != nil {
		// FatalError represents invalid CLI input/config that should exit before command execution
		var fatalErr *updater.FatalError
		if errors.As(err, &fatalErr) {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		// Non-fatal update failures: MaybeUpdateAndReexec already logged to stderr and returned nil
		// If we get here with a non-fatal error, it's a reexec error or similar - log and continue
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}
	if updater.UpdateSettingsOnly(os.Args) {
		fmt.Fprint(os.Stdout, updater.SavedUpdateSettingsSummary())
		return
	}
	if err := rootCmd.Execute(); err != nil {
		if !errors.Is(err, jsonout.ErrAlreadyWritten) {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		os.Exit(1)
	}
}
