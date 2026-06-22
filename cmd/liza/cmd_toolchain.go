package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/interactive"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/toolchain"
	"github.com/spf13/cobra"
)

var toolchainCmd = &cobra.Command{
	Use:   "toolchain",
	Short: fmt.Sprintf("Install and configure optional %s support tools", brand.NameTitle),
	Long:  fmt.Sprintf("Install, verify, and configure optional local tools that reduce %s context usage and improve navigation.", brand.NameTitle),
}

var toolchainListCmd = &cobra.Command{
	Use:   "list",
	Short: fmt.Sprintf("List known %s toolchain tools", brand.NameTitle),
	RunE: func(cmd *cobra.Command, args []string) error {
		tools := toolchain.Catalog()
		if isJSON(cmd) {
			return jsonout.WriteResult(os.Stdout, tools, nil, nil)
		}
		for _, tool := range tools {
			defaults := toolDefaults(tool)
			if defaults == "" {
				defaults = "optional"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%-22s %-16s %-10s %s\n", tool.ID, tool.Category, defaults, tool.Purpose)
		}
		return nil
	},
}

var toolchainDoctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check selected toolchain tools",
	RunE: func(cmd *cobra.Command, args []string) error {
		profile, include, exclude, err := toolchainSelectionFlags(cmd)
		if err != nil {
			return err
		}
		toolID, _ := cmd.Flags().GetString("tool")
		result, err := toolchain.Doctor(toolchain.DoctorOptions{
			Profile: profile,
			Include: include,
			Exclude: exclude,
			ToolID:  toolID,
		})
		if isJSON(cmd) {
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		if err != nil {
			return err
		}
		for _, check := range result.Checks {
			fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s", strings.ToUpper(string(check.Status)), check.ToolID)
			if check.Path != "" {
				fmt.Fprintf(cmd.OutOrStdout(), ": %s", check.Path)
			}
			if check.Message != "" {
				fmt.Fprintf(cmd.OutOrStdout(), " — %s", check.Message)
			}
			fmt.Fprintln(cmd.OutOrStdout())
		}
		return nil
	},
}

var toolchainInstallCmd = &cobra.Command{
	Use:   "install",
	Short: fmt.Sprintf("Install selected local %s toolchain CLIs", brand.NameTitle),
	RunE: func(cmd *cobra.Command, args []string) error {
		profile, include, exclude, err := toolchainSelectionFlags(cmd)
		if err != nil {
			return err
		}
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		yes, _ := cmd.Flags().GetBool("yes")
		installDir, _ := cmd.Flags().GetString("install-dir")
		if !dryRun && !yes && interactive.IsInteractive() {
			include, exclude, err = runToolchainChecklist(profile, include, exclude)
			if err != nil {
				if errors.Is(err, huh.ErrUserAborted) {
					return nil
				}
				return err
			}
		}
		if !dryRun && !yes && !interactive.IsInteractive() {
			return cliValidationError("toolchain install is non-interactive; pass --yes or --dry-run")
		}
		result, err := toolchain.Install(toolchain.InstallOptions{
			Profile:    profile,
			Include:    include,
			Exclude:    exclude,
			InstallDir: installDir,
			DryRun:     dryRun,
		})
		if isJSON(cmd) {
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		return printInstallResultAndReturn(cmd.OutOrStdout(), result, err)
	},
}

var toolchainConfigureCmd = &cobra.Command{
	Use:   "configure",
	Short: "Write machine-local toolchain env and optional project activation",
	RunE: func(cmd *cobra.Command, args []string) error {
		profile, include, exclude, err := toolchainSelectionFlags(cmd)
		if err != nil {
			return err
		}
		globalDir, _ := cmd.Flags().GetString("global-dir")
		installDir, _ := cmd.Flags().GetString("install-dir")
		agentToolsMode, _ := cmd.Flags().GetString("agent-tools")
		writeShellProfile, _ := cmd.Flags().GetBool("write-shell-profile")
		agentsRaw, _ := cmd.Flags().GetString("agents")
		projectRoot, _ := cmd.Flags().GetString("project")
		if (projectRoot == "") != (agentsRaw == "") {
			return cliValidationError("--project and --agents must be supplied together")
		}
		agents := splitCSV(agentsRaw)
		if agentsRaw != "" && len(agents) == 0 {
			return cliValidationError("--agents must contain at least one provider")
		}
		result, err := toolchain.Configure(toolchain.ConfigureOptions{
			Profile:           profile,
			Include:           include,
			Exclude:           exclude,
			GlobalDir:         globalDir,
			InstallDir:        installDir,
			AgentToolsMode:    agentToolsMode,
			WriteShellProfile: writeShellProfile,
		})
		if err != nil {
			if isJSON(cmd) {
				return jsonout.WriteResult(os.Stdout, nil, nil, err)
			}
			return err
		}

		if projectRoot != "" && agentsRaw != "" {
			applyToolchainEnv(result)
			if err := runProjectActivation(projectRoot, agents); err != nil {
				if isJSON(cmd) {
					return jsonout.WriteResult(os.Stdout, result, nil, err)
				}
				return err
			}
		}

		if isJSON(cmd) {
			return jsonout.WriteResult(os.Stdout, result, nil, nil)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Wrote %s\n", result.ProfilePath)
		fmt.Fprintf(cmd.OutOrStdout(), "Wrote %s\n", result.EnvPath)
		for _, path := range result.ShellProfilePaths {
			fmt.Fprintf(cmd.OutOrStdout(), "Updated %s\n", path)
		}
		if result.AgentToolsPath != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "Checked %s\n", result.AgentToolsPath)
		}
		if projectRoot != "" && agentsRaw != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "Activated project tools in %s for %s\n", projectRoot, agentsRaw)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(toolchainCmd)
	toolchainCmd.AddCommand(toolchainListCmd)
	toolchainCmd.AddCommand(toolchainDoctorCmd)
	toolchainCmd.AddCommand(toolchainInstallCmd)
	toolchainCmd.AddCommand(toolchainConfigureCmd)

	addToolchainSelectionFlags(toolchainDoctorCmd)
	addToolchainSelectionFlags(toolchainInstallCmd)
	addToolchainSelectionFlags(toolchainConfigureCmd)
	toolchainDoctorCmd.Flags().String("tool", "all", "tool id to check, or all")
	toolchainInstallCmd.Flags().String("install-dir", "", "directory for managed binaries (default: ~/.local/bin)")
	toolchainInstallCmd.Flags().Bool("dry-run", false, "print planned install commands without executing them")
	toolchainInstallCmd.Flags().Bool("yes", false, "run selected installs without interactive confirmation")
	toolchainConfigureCmd.Flags().String("global-dir", "", fmt.Sprintf("global %s config directory (default: ~/%s)", brand.NameTitle, paths.GlobalDirName()))
	toolchainConfigureCmd.Flags().String("install-dir", "", "directory for managed binaries in generated env (default: ~/.local/bin)")
	toolchainConfigureCmd.Flags().String("agent-tools", "auto", "AGENT_TOOLS.md handling: auto, skip, or force")
	toolchainConfigureCmd.Flags().Bool("write-shell-profile", false, "source generated env.sh from the current shell startup file")
	toolchainConfigureCmd.Flags().String("agents", "", "comma-separated provider contracts to activate in --project")
	toolchainConfigureCmd.Flags().String("project", "", "project root where provider contracts and optional indexing hooks should be activated")

	addJSONFlag(toolchainListCmd)
	addJSONFlag(toolchainDoctorCmd)
	addJSONFlag(toolchainInstallCmd)
	addJSONFlag(toolchainConfigureCmd)
}

func addToolchainSelectionFlags(cmd *cobra.Command) {
	cmd.Flags().String("profile", string(toolchain.ProfileBalanced), "tool profile: balanced, lean, or full")
	cmd.Flags().StringArray("include", nil, "tool id to include (repeatable)")
	cmd.Flags().StringArray("exclude", nil, "tool id to exclude (repeatable)")
}

func toolchainSelectionFlags(cmd *cobra.Command) (toolchain.Profile, []string, []string, error) {
	rawProfile, _ := cmd.Flags().GetString("profile")
	include, _ := cmd.Flags().GetStringArray("include")
	exclude, _ := cmd.Flags().GetStringArray("exclude")
	profile := toolchain.Profile(rawProfile)
	if _, err := toolchain.ResolveSelection(profile, include, exclude); err != nil {
		return "", nil, nil, cliValidationWrap("invalid toolchain selection", err)
	}
	return profile, include, exclude, nil
}

func runToolchainChecklist(profile toolchain.Profile, include, exclude []string) ([]string, []string, error) {
	initial, err := toolchain.ResolveSelection(profile, include, exclude)
	if err != nil {
		return nil, nil, err
	}
	selected := toolIDsForChecklist(initial.Tools)
	options := make([]huh.Option[string], 0, len(toolchain.Catalog()))
	for _, tool := range toolchain.Catalog() {
		label := fmt.Sprintf("%s — %s", tool.ID, tool.Purpose)
		options = append(options, huh.NewOption(label, tool.ID).Selected(containsString(selected, tool.ID)))
	}
	if err := huh.NewMultiSelect[string]().
		Title(fmt.Sprintf("Select %s toolchain tools", brand.NameTitle)).
		Options(options...).
		Value(&selected).
		Run(); err != nil {
		return nil, nil, err
	}
	selectedSet := map[string]bool{}
	for _, id := range selected {
		selectedSet[id] = true
	}
	var newInclude []string
	var newExclude []string
	for _, tool := range toolchain.Catalog() {
		if selectedSet[tool.ID] {
			newInclude = append(newInclude, tool.ID)
		} else {
			newExclude = append(newExclude, tool.ID)
		}
	}
	return newInclude, newExclude, nil
}

func printInstallResult(w io.Writer, result toolchain.InstallResult) {
	for _, step := range result.Steps {
		fmt.Fprintf(w, "[%s] %s", strings.ToUpper(string(step.Status)), step.ToolID)
		if step.Message != "" {
			fmt.Fprintf(w, " — %s", step.Message)
		}
		if step.Command.Name != "" {
			fmt.Fprintf(w, " :: %s %s", step.Command.Name, strings.Join(step.Command.Args, " "))
		}
		fmt.Fprintln(w)
	}
}

func printInstallResultAndReturn(w io.Writer, result toolchain.InstallResult, err error) error {
	printInstallResult(w, result)
	return err
}

func runProjectActivation(projectRoot string, agents []string) error {
	if len(agents) == 0 {
		return nil
	}
	previous, err := os.Getwd()
	if err != nil {
		return err
	}
	if err := os.Chdir(projectRoot); err != nil {
		return err
	}
	defer os.Chdir(previous)
	return commands.InitPairingCommand(commands.InitPairingParams{
		Agents:         agents,
		Stdin:          os.Stdin,
		ContractAction: "global",
	})
}

func applyToolchainEnv(result toolchain.ConfigureResult) {
	if result.InstallDir != "" {
		_ = os.Setenv("PATH", result.InstallDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	for _, entry := range result.ActivationEnv {
		name, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		_ = os.Setenv(name, value)
	}
}

func toolDefaults(tool toolchain.Tool) string {
	var parts []string
	if tool.LeanDefault {
		parts = append(parts, "lean")
	}
	if tool.BalancedDefault {
		parts = append(parts, "balanced")
	}
	if tool.FullDefault {
		parts = append(parts, "full")
	}
	return strings.Join(parts, ",")
}

func toolIDsForChecklist(tools []toolchain.Tool) []string {
	ids := make([]string, len(tools))
	for i, tool := range tools {
		ids[i] = tool.ID
	}
	return ids
}

func splitCSV(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	var out []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
