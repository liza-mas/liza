package main

import (
	"fmt"
	"strings"

	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/roles"
	"github.com/spf13/cobra"
)

var launchCmd = &cobra.Command{
	Use:   "launch",
	Short: fmt.Sprintf("Launch grouped %s workflows", brand.NameTitle),
}

var launchWeztermCmd = &cobra.Command{
	Use:   string(launchBackendWezterm),
	Short: fmt.Sprintf("Launch %s workflows in WezTerm panes", brand.NameTitle),
}

var launchCmuxCmd = &cobra.Command{
	Use:   string(launchBackendCmux),
	Short: fmt.Sprintf("Launch %s workflows in CMUX panes", brand.NameTitle),
}

var launchWeztermMASCmd = &cobra.Command{
	Use:   "mas",
	Short: "Launch a multi-agent role set in WezTerm panes",
	Long: fmt.Sprintf(`Launch a %s multi-agent role set in one WezTerm window.

Preset role sets:
  technical-spec     orchestrator, code-planner, code-plan-reviewer, coder, code-reviewer
  functional-spec    technical-spec plus architect, architecture-reviewer
  general-objective  functional-spec plus epic-planner, epic-plan-reviewer, us-writer, us-reviewer`, brand.NameTitle),
	RunE: func(cmd *cobra.Command, args []string) error {
		projectRoot, err := launchWorkingDir(cmd, true)
		if err != nil {
			return err
		}
		presetName, _ := cmd.Flags().GetString("preset")
		preset := masLaunchPreset(presetName)
		roles, _ := cmd.Flags().GetStringArray("role")
		if len(roles) == 0 {
			var ok bool
			roles, ok = masLaunchPresets()[preset]
			if !ok {
				return cliValidationError(fmt.Sprintf("unknown MAS launch preset %q", preset))
			}
		}

		cliName, _ := cmd.Flags().GetString("cli")
		availableCLIs := launchAvailableCLIs(projectRoot)
		noTUI, _ := cmd.Flags().GetBool("no-tui")
		commands := make([][]string, 0, len(roles)+1)
		if !noTUI {
			commands = append(commands, []string{brand.BinaryName, "tui"})
		}
		for _, role := range roles {
			role = strings.TrimSpace(role)
			if role == "" {
				return cliValidationError("--role values must not be empty")
			}
			agentCmd := []string{brand.BinaryName, "agent", role}
			if cliName != "" {
				if !containsString(availableCLIs, cliName) {
					return cliValidationError(fmt.Sprintf("invalid --cli %q", cliName))
				}
				agentCmd = append(agentCmd, "--cli", cliName)
			}
			commands = append(commands, agentCmd)
		}
		if len(commands) == 0 {
			return cliValidationError("nothing to launch: provide --role or omit --no-tui")
		}

		opts, err := weztermOptionsFromFlags(cmd, projectRoot, brand.BinaryName+"-mas-"+string(preset))
		if err != nil {
			return err
		}
		return runWeztermLaunch(cmd, opts, commands)
	},
}

var launchWeztermAdversarialPairingCmd = &cobra.Command{
	Use:   "adversarial-pairing <blackboard-path>",
	Short: "Launch adversarial-pairing doer/reviewer sessions in WezTerm panes",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := launchWorkingDir(cmd, false)
		if err != nil {
			return err
		}
		boardPath, err := resolveLaunchPath(cwd, args[0])
		if err != nil {
			return cliValidationWrap("resolve blackboard path", err)
		}
		goal, _ := cmd.Flags().GetString("goal")
		yolo, _ := cmd.Flags().GetBool("yolo")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		if err := ensureAdversarialPairingBlackboard(boardPath, goal, yolo, dryRun, cmd); err != nil {
			return err
		}
		doerCLI, _ := cmd.Flags().GetString("doer-cli")
		if !containsString(agent.ValidCLIs(), doerCLI) {
			return cliValidationError(fmt.Sprintf("invalid --doer-cli %q", doerCLI))
		}
		reviewerSpecs, _ := cmd.Flags().GetStringArray("reviewer")
		if len(reviewerSpecs) == 0 {
			reviewerSpecs = []string{"codex", "codex-2=codex"}
		}
		doerPrompt := pairingSkillPrompt(string(roles.RoleTypeDoer), boardPath, yolo)
		panes := []interactivePane{pairingInteractivePane(doerCLI, doerPrompt)}
		for _, spec := range reviewerSpecs {
			id, cliName, err := parseReviewerLaunchSpec(spec)
			if err != nil {
				return err
			}
			if !containsString(agent.ValidCLIs(), cliName) {
				return cliValidationError(fmt.Sprintf("invalid reviewer CLI %q", cliName))
			}
			prompt := pairingSkillPrompt(pairingReviewerIDPrefix+id, boardPath, false)
			panes = append(panes, pairingInteractivePane(cliName, prompt))
		}

		opts, err := weztermOptionsFromFlags(cmd, cwd, brand.BinaryName+"-adversarial")
		if err != nil {
			return err
		}
		return runWeztermInteractiveLaunch(cmd, opts, panes)
	},
}

var launchCmuxMASCmd = &cobra.Command{
	Use:   "mas",
	Short: "Launch a multi-agent role set in CMUX panes",
	Long: fmt.Sprintf(`Launch a %s multi-agent role set in one CMUX workspace.

Preset role sets:
  technical-spec     orchestrator, code-planner, code-plan-reviewer, coder, code-reviewer
  functional-spec    technical-spec plus architect, architecture-reviewer
  general-objective  functional-spec plus epic-planner, epic-plan-reviewer, us-writer, us-reviewer`, brand.NameTitle),
	RunE: func(cmd *cobra.Command, args []string) error {
		projectRoot, err := launchWorkingDir(cmd, true)
		if err != nil {
			return err
		}
		presetName, _ := cmd.Flags().GetString("preset")
		preset := masLaunchPreset(presetName)
		roles, _ := cmd.Flags().GetStringArray("role")
		if len(roles) == 0 {
			var ok bool
			roles, ok = masLaunchPresets()[preset]
			if !ok {
				return cliValidationError(fmt.Sprintf("unknown MAS launch preset %q", preset))
			}
		}

		cliName, _ := cmd.Flags().GetString("cli")
		availableCLIs := launchAvailableCLIs(projectRoot)
		noTUI, _ := cmd.Flags().GetBool("no-tui")
		commands := make([][]string, 0, len(roles)+1)
		if !noTUI {
			commands = append(commands, []string{brand.BinaryName, "tui"})
		}
		for _, role := range roles {
			role = strings.TrimSpace(role)
			if role == "" {
				return cliValidationError("--role values must not be empty")
			}
			agentCmd := []string{brand.BinaryName, "agent", role}
			if cliName != "" {
				if !containsString(availableCLIs, cliName) {
					return cliValidationError(fmt.Sprintf("invalid --cli %q", cliName))
				}
				agentCmd = append(agentCmd, "--cli", cliName)
			}
			commands = append(commands, agentCmd)
		}
		if len(commands) == 0 {
			return cliValidationError("nothing to launch: provide --role or omit --no-tui")
		}

		opts, err := cmuxOptionsFromFlags(cmd, projectRoot, brand.BinaryName+"-mas-"+string(preset))
		if err != nil {
			return err
		}
		return runCmuxLaunch(cmd, opts, commands)
	},
}

var launchCmuxAdversarialPairingCmd = &cobra.Command{
	Use:   "adversarial-pairing <blackboard-path>",
	Short: "Launch adversarial-pairing doer/reviewer sessions in CMUX panes",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := launchWorkingDir(cmd, false)
		if err != nil {
			return err
		}
		boardPath, err := resolveLaunchPath(cwd, args[0])
		if err != nil {
			return cliValidationWrap("resolve blackboard path", err)
		}
		goal, _ := cmd.Flags().GetString("goal")
		yolo, _ := cmd.Flags().GetBool("yolo")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		if err := ensureAdversarialPairingBlackboard(boardPath, goal, yolo, dryRun, cmd); err != nil {
			return err
		}
		doerCLI, _ := cmd.Flags().GetString("doer-cli")
		if !containsString(agent.ValidCLIs(), doerCLI) {
			return cliValidationError(fmt.Sprintf("invalid --doer-cli %q", doerCLI))
		}
		reviewerSpecs, _ := cmd.Flags().GetStringArray("reviewer")
		if len(reviewerSpecs) == 0 {
			reviewerSpecs = []string{"codex", "codex-2=codex"}
		}
		doerPrompt := pairingSkillPrompt(string(roles.RoleTypeDoer), boardPath, yolo)
		panes := []interactivePane{pairingInteractivePane(doerCLI, doerPrompt)}
		for _, spec := range reviewerSpecs {
			id, cliName, err := parseReviewerLaunchSpec(spec)
			if err != nil {
				return err
			}
			if !containsString(agent.ValidCLIs(), cliName) {
				return cliValidationError(fmt.Sprintf("invalid reviewer CLI %q", cliName))
			}
			prompt := pairingSkillPrompt(pairingReviewerIDPrefix+id, boardPath, false)
			panes = append(panes, pairingInteractivePane(cliName, prompt))
		}

		opts, err := cmuxOptionsFromFlags(cmd, cwd, brand.BinaryName+"-adversarial")
		if err != nil {
			return err
		}
		return runCmuxInteractiveLaunch(cmd, opts, panes)
	},
}

func init() {
	rootCmd.AddCommand(launchCmd)
	launchCmd.AddCommand(launchWeztermCmd)
	launchWeztermCmd.AddCommand(launchWeztermMASCmd)
	launchWeztermCmd.AddCommand(launchWeztermAdversarialPairingCmd)
	launchCmd.AddCommand(launchCmuxCmd)
	launchCmuxCmd.AddCommand(launchCmuxMASCmd)
	launchCmuxCmd.AddCommand(launchCmuxAdversarialPairingCmd)

	for _, c := range []*cobra.Command{launchWeztermMASCmd, launchWeztermAdversarialPairingCmd} {
		c.Flags().String("class", "", "WezTerm window class (default depends on launch type)")
		c.Flags().String("workspace", "", "WezTerm workspace name (defaults to --class)")
		c.Flags().String("cwd", "", fmt.Sprintf("working directory for launched panes (default: current %s project for MAS, current directory for pairing)", brand.NameTitle))
		c.Flags().Bool("dry-run", false, "print the wezterm command without launching it")
	}

	for _, c := range []*cobra.Command{launchCmuxMASCmd, launchCmuxAdversarialPairingCmd} {
		c.Flags().String("class", "", "workspace name (defaults to launch type)")
		c.Flags().String("workspace", "", "CMUX workspace name (defaults to --class)")
		c.Flags().String("cwd", "", fmt.Sprintf("working directory for launched panes (default: current %s project for MAS, current directory for pairing)", brand.NameTitle))
		c.Flags().Bool("dry-run", false, "print the cmux commands without launching them")
	}

	launchWeztermMASCmd.Flags().String("preset", string(masPresetTechnicalSpec), "role preset: technical-spec, functional-spec, general-objective")
	launchWeztermMASCmd.Flags().StringArray("role", nil, "role to launch; repeat to override --preset")
	launchWeztermMASCmd.Flags().String("cli", "", fmt.Sprintf("CLI to pass to %s for every launched role", brand.Command("agent")))
	launchWeztermMASCmd.Flags().Bool("no-tui", false, fmt.Sprintf("do not launch %s in the first pane", brand.Command("tui")))
	registerCompletion(launchWeztermMASCmd, "preset", completeValues(string(masPresetTechnicalSpec), string(masPresetFunctionalSpec), string(masPresetGeneralObjective)))
	registerCompletion(launchWeztermMASCmd, "role", completeAgentRoles)
	registerCompletion(launchWeztermMASCmd, "cli", completeCLINames)

	launchWeztermAdversarialPairingCmd.Flags().String("doer-cli", "codex", "coding CLI for the doer session")
	launchWeztermAdversarialPairingCmd.Flags().String("goal", "", "create the blackboard with this goal when it does not exist")
	launchWeztermAdversarialPairingCmd.Flags().StringArray("reviewer", nil, "reviewer CLI or id=cli; repeat for multiple reviewers (default: codex, codex-2=codex)")
	launchWeztermAdversarialPairingCmd.Flags().Duration("prompt-delay", defaultWeztermPromptDelay, "delay before injecting adversarial-pairing prompts into WezTerm panes")
	launchWeztermAdversarialPairingCmd.Flags().Bool("yolo", false, "pass yolo to the doer adversarial-pairing invocation")
	registerCompletion(launchWeztermAdversarialPairingCmd, "doer-cli", completeCLINames)
	registerCompletion(launchWeztermAdversarialPairingCmd, "reviewer", completeCLINames)

	launchCmuxMASCmd.Flags().String("preset", string(masPresetTechnicalSpec), "role preset: technical-spec, functional-spec, general-objective")
	launchCmuxMASCmd.Flags().StringArray("role", nil, "role to launch; repeat to override --preset")
	launchCmuxMASCmd.Flags().String("cli", "", fmt.Sprintf("CLI to pass to %s for every launched role", brand.Command("agent")))
	launchCmuxMASCmd.Flags().Bool("no-tui", false, fmt.Sprintf("do not launch %s in the first pane", brand.Command("tui")))
	registerCompletion(launchCmuxMASCmd, "preset", completeValues(string(masPresetTechnicalSpec), string(masPresetFunctionalSpec), string(masPresetGeneralObjective)))
	registerCompletion(launchCmuxMASCmd, "role", completeAgentRoles)
	registerCompletion(launchCmuxMASCmd, "cli", completeCLINames)

	launchCmuxAdversarialPairingCmd.Flags().String("doer-cli", "codex", "coding CLI for the doer session")
	launchCmuxAdversarialPairingCmd.Flags().String("goal", "", "create the blackboard with this goal when it does not exist")
	launchCmuxAdversarialPairingCmd.Flags().StringArray("reviewer", nil, "reviewer CLI or id=cli; repeat for multiple reviewers (default: codex, codex-2=codex)")
	launchCmuxAdversarialPairingCmd.Flags().Bool("yolo", false, "pass yolo to the doer adversarial-pairing invocation")
	registerCompletion(launchCmuxAdversarialPairingCmd, "doer-cli", completeCLINames)
	registerCompletion(launchCmuxAdversarialPairingCmd, "reviewer", completeCLINames)
}
