package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/gitbash"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/spf13/cobra"
)

var launchCmd = &cobra.Command{
	Use:   "launch",
	Short: fmt.Sprintf("Launch grouped %s workflows", brand.NameTitle),
}

var launchWeztermCmd = &cobra.Command{
	Use:   "wezterm",
	Short: fmt.Sprintf("Launch %s workflows in WezTerm panes", brand.NameTitle),
}

var launchCmuxCmd = &cobra.Command{
	Use:   "cmux",
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
		preset, _ := cmd.Flags().GetString("preset")
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

		opts, err := weztermOptionsFromFlags(cmd, projectRoot, brand.BinaryName+"-mas-"+preset)
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
		doerPrompt := pairingSkillPrompt("doer", boardPath, yolo)
		panes := []interactivePane{pairingInteractivePane(doerCLI, doerPrompt)}
		for _, spec := range reviewerSpecs {
			id, cliName, err := parseReviewerLaunchSpec(spec)
			if err != nil {
				return err
			}
			if !containsString(agent.ValidCLIs(), cliName) {
				return cliValidationError(fmt.Sprintf("invalid reviewer CLI %q", cliName))
			}
			prompt := pairingSkillPrompt("reviewer-"+id, boardPath, false)
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
		preset, _ := cmd.Flags().GetString("preset")
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

		opts, err := cmuxOptionsFromFlags(cmd, projectRoot, brand.BinaryName+"-mas-"+preset)
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
		doerPrompt := pairingSkillPrompt("doer", boardPath, yolo)
		panes := []interactivePane{pairingInteractivePane(doerCLI, doerPrompt)}
		for _, spec := range reviewerSpecs {
			id, cliName, err := parseReviewerLaunchSpec(spec)
			if err != nil {
				return err
			}
			if !containsString(agent.ValidCLIs(), cliName) {
				return cliValidationError(fmt.Sprintf("invalid reviewer CLI %q", cliName))
			}
			prompt := pairingSkillPrompt("reviewer-"+id, boardPath, false)
			panes = append(panes, pairingInteractivePane(cliName, prompt))
		}

		opts, err := cmuxOptionsFromFlags(cmd, cwd, brand.BinaryName+"-adversarial")
		if err != nil {
			return err
		}
		return runCmuxInteractiveLaunch(cmd, opts, panes)
	},
}

type weztermLaunchOptions struct {
	Class       string
	Workspace   string
	CWD         string
	DryRun      bool
	PromptDelay time.Duration
}

type cmuxLaunchOptions struct {
	Workspace string
	CWD       string
	DryRun    bool
}

type interactivePane struct {
	CLIName string
	Command []string
	Prompt  string
}

func masLaunchPresets() map[string][]string {
	return map[string][]string{
		"technical-spec": {
			"orchestrator",
			"code-planner",
			"code-plan-reviewer",
			"coder",
			"code-reviewer",
		},
		"functional-spec": {
			"orchestrator",
			"architect",
			"architecture-reviewer",
			"code-planner",
			"code-plan-reviewer",
			"coder",
			"code-reviewer",
		},
		"general-objective": {
			"orchestrator",
			"epic-planner",
			"epic-plan-reviewer",
			"us-writer",
			"us-reviewer",
			"architect",
			"architecture-reviewer",
			"code-planner",
			"code-plan-reviewer",
			"coder",
			"code-reviewer",
		},
	}
}

func launchWorkingDir(cmd *cobra.Command, requireLizaProject bool) (string, error) {
	cwd, _ := cmd.Flags().GetString("cwd")
	if cwd != "" {
		abs, err := filepath.Abs(cwd)
		if err != nil {
			return "", cliValidationWrap("resolve --cwd", err)
		}
		if requireLizaProject {
			if err := validateLaunchProjectRoot(abs); err != nil {
				return "", err
			}
		}
		return abs, nil
	}
	if requireLizaProject {
		return requireProjectRoot()
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", cliValidationWrap("get current directory", err)
	}
	return wd, nil
}

func validateLaunchProjectRoot(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return cliValidationWrap("inspect --cwd", err)
	}
	if !info.IsDir() {
		return cliValidationError(fmt.Sprintf("--cwd %q is not a directory", path))
	}
	output, err := exec.Command("git", "-C", path, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return cliValidationWrap("resolve --cwd git root", err)
	}
	gitRoot := strings.TrimSpace(string(output))
	canonicalPath, err := canonicalDir(path)
	if err != nil {
		return cliValidationWrap("resolve --cwd", err)
	}
	canonicalRoot, err := canonicalDir(gitRoot)
	if err != nil {
		return cliValidationWrap("resolve --cwd git root", err)
	}
	if canonicalPath != canonicalRoot {
		return cliValidationError(fmt.Sprintf("--cwd must be the %s project root, got %s under git root %s", brand.NameTitle, path, gitRoot))
	}
	if _, err := os.Stat(filepath.Join(path, paths.ProjectDirName(), "state.yaml")); err != nil {
		if os.IsNotExist(err) {
			return cliValidationError(fmt.Sprintf("--cwd %s is not initialized as a %s project", path, brand.NameTitle))
		}
		return cliValidationWrap(fmt.Sprintf("inspect --cwd %s state", brand.NameTitle), err)
	}
	return nil
}

func resolveLaunchPath(cwd, path string) (string, error) {
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	return filepath.Abs(filepath.Join(cwd, path))
}

func ensureAdversarialPairingBlackboard(path, goal string, yolo bool, dryRun bool, cmd *cobra.Command) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return cliValidationWrap("inspect blackboard path", err)
	}
	if strings.TrimSpace(goal) == "" {
		return cliValidationError("blackboard does not exist; create it first or pass --goal to initialize it before launching reviewers")
	}
	if dryRun {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return cliValidationWrap("create blackboard directory", err)
	}
	content := initialAdversarialPairingBlackboard(goal, yolo, time.Now().UTC())
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return cliValidationWrap("create adversarial-pairing blackboard", err)
	}
	if _, err := file.WriteString(content); err != nil {
		_ = file.Close()
		return cliValidationWrap("write adversarial-pairing blackboard", err)
	}
	if err := file.Close(); err != nil {
		return cliValidationWrap("close adversarial-pairing blackboard", err)
	}
	cmd.Printf("Created adversarial-pairing blackboard: %s\n", path)
	return nil
}

func initialAdversarialPairingBlackboard(goal string, yolo bool, now time.Time) string {
	goal = strings.TrimSpace(goal)
	yoloValue := "false"
	if yolo {
		yoloValue = "true"
	}
	return fmt.Sprintf(`---
phase: DRAFT
yolo: %s
work_type: feature
rca_required: false
red_test_required: false
required_reviewers: []
plan_revision: 0
analysis_revision: 0
red_test_round: 0
code_review_round: 0
phase_updated_at: "%s"
worktree: null
agents:
  doer:
    role: doer
    status: DRAFT
    last_seen: null
    reviewed_analysis_revision: null
    analysis_verdict: null
    reviewed_plan_revision: null
    plan_verdict: null
    reviewed_red_test_round: null
    red_test_verdict: null
    reviewed_code_round: null
    code_verdict: null
---

# Adversarial Pairing Blackboard

## Goal

%s

## Evidence

## Plan Revisions

## Plan Reviews

## Implementation Notes

## Code Review Rounds

## Validation

## Decisions
`, yoloValue, now.Format(time.RFC3339), goal)
}

func weztermOptionsFromFlags(cmd *cobra.Command, cwd, defaultClass string) (weztermLaunchOptions, error) {
	className, _ := cmd.Flags().GetString("class")
	if className == "" {
		className = defaultClass
	}
	workspace, _ := cmd.Flags().GetString("workspace")
	if workspace == "" {
		workspace = className
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	promptDelay := 2 * time.Second
	if cmd.Flags().Lookup("prompt-delay") != nil {
		promptDelay, _ = cmd.Flags().GetDuration("prompt-delay")
		if promptDelay < 0 {
			return weztermLaunchOptions{}, cliValidationError("--prompt-delay must not be negative")
		}
	}
	return weztermLaunchOptions{Class: className, Workspace: workspace, CWD: cwd, DryRun: dryRun, PromptDelay: promptDelay}, nil
}

func cmuxOptionsFromFlags(cmd *cobra.Command, cwd, defaultWorkspace string) (cmuxLaunchOptions, error) {
	workspace, _ := cmd.Flags().GetString("workspace")
	if workspace == "" {
		className, _ := cmd.Flags().GetString("class")
		if className != "" {
			workspace = className
		} else {
			workspace = defaultWorkspace
		}
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	return cmuxLaunchOptions{Workspace: workspace, CWD: cwd, DryRun: dryRun}, nil
}

func launchAvailableCLIs(projectRoot string) []string {
	if projectRoot == "" {
		return agent.ValidCLIs()
	}
	state, err := db.For(paths.New(projectRoot).StatePath()).Read()
	if err != nil {
		return agent.ValidCLIs()
	}
	return agent.AvailableCLIs(state.Config)
}

func runWeztermLaunch(cmd *cobra.Command, opts weztermLaunchOptions, commands [][]string) error {
	if len(commands) == 0 {
		return cliValidationError("no commands to launch")
	}
	shell, err := launchShell()
	if err != nil {
		return cliValidationWrap("resolve launch shell", err)
	}
	script := buildWeztermPaneScript(opts, commands)
	args := []string{
		"start",
		"--class", opts.Class,
		"--workspace", opts.Workspace,
		"--cwd", opts.CWD,
		"--",
		shell,
		"-lc",
		script,
	}
	if opts.DryRun {
		cmd.Println(shellJoin(append([]string{"wezterm"}, args...)))
		return nil
	}
	if _, err := exec.LookPath("wezterm"); err != nil {
		return cliValidationWrap("wezterm not found on PATH", err)
	}
	launch := exec.Command("wezterm", args...)
	launch.Stdout = os.Stdout
	launch.Stderr = os.Stderr
	launch.Stdin = os.Stdin
	if err := launch.Start(); err != nil {
		return err
	}
	return launch.Process.Release()
}

func runWeztermInteractiveLaunch(cmd *cobra.Command, opts weztermLaunchOptions, panes []interactivePane) error {
	if len(panes) == 0 {
		return cliValidationError("no panes to launch")
	}
	shell, err := launchShell()
	if err != nil {
		return cliValidationWrap("resolve launch shell", err)
	}
	script := buildWeztermInteractivePaneScript(opts, panes)
	args := []string{
		"start",
		"--class", opts.Class,
		"--workspace", opts.Workspace,
		"--cwd", opts.CWD,
		"--",
		shell,
		"-lc",
		script,
	}
	if opts.DryRun {
		cmd.Println(shellJoin(append([]string{"wezterm"}, args...)))
		return nil
	}
	if _, err := exec.LookPath("wezterm"); err != nil {
		return cliValidationWrap("wezterm not found on PATH", err)
	}
	launch := exec.Command("wezterm", args...)
	launch.Stdout = os.Stdout
	launch.Stderr = os.Stderr
	launch.Stdin = os.Stdin
	if err := launch.Start(); err != nil {
		return err
	}
	return launch.Process.Release()
}

func runCmuxLaunch(cmd *cobra.Command, opts cmuxLaunchOptions, commands [][]string) error {
	if len(commands) == 0 {
		return cliValidationError("no commands to launch")
	}
	if opts.DryRun {
		cmuxCmds, err := buildCmuxLaunchCommands(opts, commands)
		if err != nil {
			return err
		}
		for _, c := range cmuxCmds {
			cmd.Println(shellJoin(c))
		}
		return nil
	}
	if _, err := exec.LookPath("cmux"); err != nil {
		return cliValidationWrap("cmux not found on PATH", err)
	}

	// Create workspace with first command
	createArgs := []string{
		"cmux", "new-workspace",
		"--name", opts.Workspace,
		"--cwd", opts.CWD,
		"--command", shellJoin(commands[0]),
	}
	output, err := exec.Command(createArgs[0], createArgs[1:]...).CombinedOutput()
	if err != nil {
		return cliValidationWrap("create cmux workspace", err)
	}
	workspaceRef, err := parseCmuxRef(output, "workspace")
	if err != nil {
		return err
	}

	// Create panes for additional commands
	for i, paneCommand := range commands[1:] {
		direction := "right"
		if i%2 == 1 {
			direction = "down"
		}
		paneArgs := []string{
			"cmux", "new-pane",
			"--type", "terminal",
			"--direction", direction,
			"--workspace", workspaceRef,
		}
		paneOutput, err := exec.Command(paneArgs[0], paneArgs[1:]...).CombinedOutput()
		if err != nil {
			return err
		}
		surfaceRef, err := parseCmuxRef(paneOutput, "surface")
		if err != nil {
			return err
		}
		if err := cmuxSendText(workspaceRef, surfaceRef, shellJoin(paneCommand)); err != nil {
			return cliValidationWrap("send command to cmux pane", err)
		}
		if err := cmuxSendKey(workspaceRef, surfaceRef, "enter"); err != nil {
			return cliValidationWrap("send enter key to cmux pane", err)
		}
	}
	return nil
}

func runCmuxInteractiveLaunch(cmd *cobra.Command, opts cmuxLaunchOptions, panes []interactivePane) error {
	if len(panes) == 0 {
		return cliValidationError("no panes to launch")
	}
	if opts.DryRun {
		cmuxCmds, err := buildCmuxInteractiveLaunchCommands(opts, panes)
		if err != nil {
			return err
		}
		for _, c := range cmuxCmds {
			cmd.Println(shellJoin(c))
		}
		return nil
	}
	if _, err := exec.LookPath("cmux"); err != nil {
		return cliValidationWrap("cmux not found on PATH", err)
	}

	// Create workspace with first pane
	createArgs := []string{
		"cmux", "new-workspace",
		"--name", opts.Workspace,
		"--cwd", opts.CWD,
		"--command", shellJoin(panes[0].Command),
	}
	output, err := exec.Command(createArgs[0], createArgs[1:]...).CombinedOutput()
	if err != nil {
		return cliValidationWrap("create cmux workspace", err)
	}
	workspaceRef, err := parseCmuxRef(output, "workspace")
	if err != nil {
		return err
	}
	primarySurfaceRef, err := cmuxWorkspaceSelectedSurfaceRef(workspaceRef)
	if err != nil {
		return err
	}

	// Create additional panes and collect surface references
	surfaceRefs := []string{primarySurfaceRef}
	for i, pane := range panes[1:] {
		direction := "right"
		if i%2 == 1 {
			direction = "down"
		}
		paneArgs := []string{
			"cmux", "new-pane",
			"--type", "terminal",
			"--direction", direction,
			"--workspace", workspaceRef,
		}
		paneOutput, err := exec.Command(paneArgs[0], paneArgs[1:]...).CombinedOutput()
		if err != nil {
			return cliValidationWrap("create cmux pane", err)
		}
		surfaceRef, err := parseCmuxRef(paneOutput, "surface")
		if err != nil {
			return err
		}
		surfaceRefs = append(surfaceRefs, surfaceRef)
		if err := cmuxSendText(workspaceRef, surfaceRef, shellJoin(pane.Command)); err != nil {
			return cliValidationWrap("send command to cmux pane", err)
		}
		if err := cmuxSendKey(workspaceRef, surfaceRef, "enter"); err != nil {
			return cliValidationWrap("send enter key to cmux pane", err)
		}
	}

	if err := waitForCmuxInteractiveSurfaces(workspaceRef, panes, surfaceRefs, 60*time.Second); err != nil {
		return err
	}
	time.Sleep(2 * time.Second)

	// Send prompts to all panes
	for i, pane := range panes {
		if err := cmuxSendPrompt(workspaceRef, surfaceRefs[i], pane); err != nil {
			return err
		}
	}

	return nil
}

func cmuxSendText(workspaceRef, surfaceRef, text string) error {
	return exec.Command("cmux", "send", "--workspace", workspaceRef, "--surface", surfaceRef, text).Run()
}

func cmuxSendKey(workspaceRef, surfaceRef, key string) error {
	return exec.Command("cmux", "send-key", "--workspace", workspaceRef, "--surface", surfaceRef, key).Run()
}

func cmuxSendPrompt(workspaceRef, surfaceRef string, pane interactivePane) error {
	if err := cmuxSendText(workspaceRef, surfaceRef, pane.Prompt); err != nil {
		return cliValidationWrap("send prompt to cmux pane", err)
	}
	for attempt := 0; attempt < 5; attempt++ {
		if err := cmuxSendKey(workspaceRef, surfaceRef, "enter"); err != nil {
			return cliValidationWrap("send enter key to cmux pane", err)
		}
		if !cmuxPaneUsesCodex(pane) {
			return nil
		}
		time.Sleep(2 * time.Second)
		screen, err := cmuxReadScreen(workspaceRef, surfaceRef)
		if err != nil {
			return cliValidationWrap("verify cmux prompt submission", err)
		}
		if !cmuxPromptStillPending(screen, pane.Prompt) {
			return nil
		}
	}
	return cliValidationError("cmux prompt remained pending after submit attempts")
}

func cmuxWorkspaceSelectedSurfaceRef(workspaceRef string) (string, error) {
	output, err := exec.Command("cmux", "list-pane-surfaces", "--workspace", workspaceRef).CombinedOutput()
	if err != nil {
		return "", cliValidationWrap("list cmux workspace surfaces", err)
	}
	return parseCmuxRef(output, "surface")
}

func waitForCmuxInteractiveSurfaces(workspaceRef string, panes []interactivePane, surfaceRefs []string, timeout time.Duration) error {
	if len(panes) != len(surfaceRefs) {
		return cliValidationError("internal error: cmux pane/surface count mismatch")
	}
	deadline := time.Now().Add(timeout)
	hostSelectionSent := map[string]bool{}
	for {
		allReady := true
		for i, surfaceRef := range surfaceRefs {
			if !cmuxPaneUsesCodex(panes[i]) {
				continue
			}
			screen, err := cmuxReadScreen(workspaceRef, surfaceRef)
			if err != nil {
				allReady = false
				break
			}
			if cmuxScreenNeedsHostSelection(screen) && !hostSelectionSent[surfaceRef] {
				if err := cmuxSendKey(workspaceRef, surfaceRef, "enter"); err != nil {
					return cliValidationWrap("select cmux codex host", err)
				}
				hostSelectionSent[surfaceRef] = true
				allReady = false
				break
			}
			if !cmuxScreenLooksInteractive(screen) {
				allReady = false
				break
			}
		}
		if allReady {
			return nil
		}
		if time.Now().After(deadline) {
			return cliValidationError("timed out waiting for cmux panes to become interactive")
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func cmuxReadScreen(workspaceRef, surfaceRef string) (string, error) {
	output, err := exec.Command("cmux", "read-screen", "--workspace", workspaceRef, "--surface", surfaceRef, "--lines", "80").CombinedOutput()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

func cmuxScreenNeedsHostSelection(screen string) bool {
	return strings.Contains(screen, "Pick host for agent") && strings.Contains(screen, "enter submit")
}

func cmuxPromptStillPending(screen, prompt string) bool {
	return strings.Contains(screen, "› "+prompt) && !strings.Contains(screen, "Working")
}

func cmuxScreenLooksInteractive(screen string) bool {
	if !strings.Contains(screen, "OpenAI Codex") {
		return false
	}
	for _, marker := range []string{
		"Starting MCP servers",
		"esc to interrupt",
		"Working",
	} {
		if strings.Contains(screen, marker) {
			return false
		}
	}
	return strings.Contains(screen, "›")
}

func cmuxPaneUsesCodex(pane interactivePane) bool {
	if pane.CLIName != "" {
		return agent.CLIExecutableName(pane.CLIName) == "codex"
	}
	return len(pane.Command) > 0 && pane.Command[0] == "codex"
}

func parseCmuxRef(output []byte, refType string) (string, error) {
	prefix := refType + ":"
	for _, field := range strings.Fields(strings.TrimSpace(string(output))) {
		if strings.HasPrefix(field, prefix) {
			return field, nil
		}
	}
	return "", cliValidationError(fmt.Sprintf("cmux output did not include %s ref: %s", refType, strings.TrimSpace(string(output))))
}

func buildWeztermPaneScript(opts weztermLaunchOptions, commands [][]string) string {
	var b strings.Builder
	for i, paneCommand := range commands[1:] {
		direction := "--right"
		if i%2 == 1 {
			direction = "--bottom"
		}
		b.WriteString("wezterm cli split-pane ")
		b.WriteString(direction)
		b.WriteString(" --cwd ")
		b.WriteString(shellQuote(opts.CWD))
		b.WriteString(" -- ")
		b.WriteString(shellJoin(paneCommand))
		b.WriteString("\n")
	}
	b.WriteString("exec ")
	b.WriteString(shellJoin(commands[0]))
	b.WriteString("\n")
	return b.String()
}

func buildWeztermInteractivePaneScript(opts weztermLaunchOptions, panes []interactivePane) string {
	var b strings.Builder
	sendPromptFunc := shellIdentifier("wezterm_" + brand.BinaryName + "_send_prompt")
	b.WriteString("set -e\n")
	b.WriteString(sendPromptFunc)
	b.WriteString("() {\n")
	b.WriteString("  pane_id=\"$1\"\n")
	b.WriteString("  prompt=\"$2\"\n")
	b.WriteString("  (\n")
	b.WriteString("    sleep ")
	b.WriteString(shellSleepSeconds(opts.PromptDelay))
	b.WriteString("\n")
	b.WriteString("    printf '%s' \"$prompt\" | wezterm cli --class ")
	b.WriteString(shellQuote(opts.Class))
	b.WriteString(" send-text --no-paste --pane-id \"$pane_id\"\n")
	b.WriteString("    printf '\\r' | wezterm cli --class ")
	b.WriteString(shellQuote(opts.Class))
	b.WriteString(" send-text --no-paste --pane-id \"$pane_id\"\n")
	b.WriteString("  ) &\n")
	b.WriteString("}\n")
	for i, pane := range panes[1:] {
		direction := "--right"
		if i%2 == 1 {
			direction = "--bottom"
		}
		paneVar := fmt.Sprintf("pane_id_%d", i+1)
		b.WriteString(paneVar)
		b.WriteString("=$(wezterm cli --class ")
		b.WriteString(shellQuote(opts.Class))
		b.WriteString(" split-pane ")
		b.WriteString(direction)
		b.WriteString(" --cwd ")
		b.WriteString(shellQuote(opts.CWD))
		b.WriteString(" -- ")
		b.WriteString(shellJoin(pane.Command))
		b.WriteString(")\n")
		b.WriteString(sendPromptFunc)
		b.WriteString(" \"$")
		b.WriteString(paneVar)
		b.WriteString("\" ")
		b.WriteString(shellQuote(pane.Prompt))
		b.WriteString("\n")
	}
	b.WriteString(sendPromptFunc)
	b.WriteString(" \"$WEZTERM_PANE\" ")
	b.WriteString(shellQuote(panes[0].Prompt))
	b.WriteString("\n")
	b.WriteString("exec ")
	b.WriteString(shellJoin(panes[0].Command))
	b.WriteString("\n")
	return b.String()
}

func buildCmuxLaunchCommands(opts cmuxLaunchOptions, commands [][]string) ([][]string, error) {
	if len(commands) == 0 {
		return nil, cliValidationError("no commands to launch")
	}

	var cmds [][]string

	// Create workspace with first command
	createArgs := []string{
		"cmux", "new-workspace",
		"--name", opts.Workspace,
		"--cwd", opts.CWD,
		"--command", shellJoin(commands[0]),
	}
	cmds = append(cmds, createArgs)

	// Use a placeholder workspace ref for dry-run (would be parsed from output at runtime)
	workspaceRef := "<workspace-ref-from-new-workspace-output>"

	// Create panes for additional commands
	for i, paneCommand := range commands[1:] {
		direction := "right"
		if i%2 == 1 {
			direction = "down"
		}
		paneArgs := []string{
			"cmux", "new-pane",
			"--type", "terminal",
			"--direction", direction,
			"--workspace", workspaceRef,
		}
		cmds = append(cmds, paneArgs)
		cmds = append(cmds, []string{"cmux", "send", "--workspace", workspaceRef, "--surface", fmt.Sprintf("<surface-ref-%d-from-new-pane-output>", i+1), shellJoin(paneCommand)})
		cmds = append(cmds, []string{"cmux", "send-key", "--workspace", workspaceRef, "--surface", fmt.Sprintf("<surface-ref-%d-from-new-pane-output>", i+1), "enter"})
	}

	return cmds, nil
}

func buildCmuxInteractiveLaunchCommands(opts cmuxLaunchOptions, panes []interactivePane) ([][]string, error) {
	if len(panes) == 0 {
		return nil, cliValidationError("no panes to launch")
	}

	var cmds [][]string

	// Create workspace with first pane (interactive CLI)
	createArgs := []string{
		"cmux", "new-workspace",
		"--name", opts.Workspace,
		"--cwd", opts.CWD,
		"--command", shellJoin(panes[0].Command),
	}
	cmds = append(cmds, createArgs)

	// Use a placeholder workspace ref for dry-run (would be parsed from output at runtime)
	workspaceRef := "<workspace-ref-from-new-workspace-output>"

	// Create additional panes
	for i, pane := range panes[1:] {
		direction := "right"
		if i%2 == 1 {
			direction = "down"
		}
		paneArgs := []string{
			"cmux", "new-pane",
			"--type", "terminal",
			"--direction", direction,
			"--workspace", workspaceRef,
		}
		cmds = append(cmds, paneArgs)
		surfaceRef := fmt.Sprintf("<surface-ref-%d-from-new-pane-output>", i+1)
		cmds = append(cmds, []string{"cmux", "send", "--workspace", workspaceRef, "--surface", surfaceRef, shellJoin(pane.Command)})
		cmds = append(cmds, []string{"cmux", "send-key", "--workspace", workspaceRef, "--surface", surfaceRef, "enter"})
	}

	// Send prompts to all panes
	for i, pane := range panes {
		surfaceRef := fmt.Sprintf("<surface-ref-%d-from-new-pane-output>", i)
		if i == 0 {
			surfaceRef = "<surface-ref-from-new-workspace-output>"
		}
		// Send prompt as text (not as argv to the CLI)
		sendArgs := []string{
			"cmux", "send",
			"--workspace", workspaceRef,
			"--surface", surfaceRef,
			pane.Prompt,
		}
		cmds = append(cmds, sendArgs)

		// Send enter key to submit
		enterArgs := []string{
			"cmux", "send-key",
			"--workspace", workspaceRef,
			"--surface", surfaceRef,
			"enter",
		}
		cmds = append(cmds, enterArgs)
	}

	return cmds, nil
}

func pairingSkillPrompt(roleOrReviewerID, boardPath string, yolo bool) string {
	prompt := "$adversarial-pairing " + roleOrReviewerID + " " + boardPath
	if yolo {
		prompt += " yolo"
	}
	return prompt
}

func pairingInteractivePane(cliName, prompt string) interactivePane {
	return interactivePane{CLIName: cliName, Command: agent.InteractiveCLICommand(cliName), Prompt: prompt}
}

func parseReviewerLaunchSpec(spec string) (string, string, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", "", cliValidationError("--reviewer values must not be empty")
	}
	if strings.Contains(spec, "=") {
		parts := strings.SplitN(spec, "=", 2)
		id := strings.TrimPrefix(strings.TrimSpace(parts[0]), "reviewer-")
		cliName := strings.TrimSpace(parts[1])
		if id == "" || cliName == "" {
			return "", "", cliValidationError(fmt.Sprintf("invalid reviewer spec %q; use id=cli or cli", spec))
		}
		return id, cliName, nil
	}
	id := strings.TrimPrefix(spec, "reviewer-")
	return id, id, nil
}

func shellJoin(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = shellQuote(arg)
	}
	return strings.Join(quoted, " ")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func shellSleepSeconds(duration time.Duration) string {
	if duration%time.Second == 0 {
		return fmt.Sprintf("%d", int(duration/time.Second))
	}
	value := fmt.Sprintf("%.3f", duration.Seconds())
	value = strings.TrimRight(value, "0")
	return strings.TrimRight(value, ".")
}

// launchShell returns the shell the terminal is asked to run the pane script
// with. The script is POSIX, so Windows always needs the shell Git for Windows
// ships: SHELL may name the WSL launcher, and /bin/sh names nothing the OS can
// execute.
func launchShell() (string, error) {
	if runtime.GOOS == "windows" {
		return gitbash.Resolve()
	}
	if shell := os.Getenv("SHELL"); shell != "" {
		return shell, nil
	}
	return "/bin/sh", nil
}

func shellIdentifier(value string) string {
	var b strings.Builder
	for i, r := range value {
		valid := r == '_' || ('A' <= r && r <= 'Z') || ('a' <= r && r <= 'z') || (i > 0 && '0' <= r && r <= '9')
		if valid {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "launcher_fn"
	}
	return b.String()
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

	launchWeztermMASCmd.Flags().String("preset", "technical-spec", "role preset: technical-spec, functional-spec, general-objective")
	launchWeztermMASCmd.Flags().StringArray("role", nil, "role to launch; repeat to override --preset")
	launchWeztermMASCmd.Flags().String("cli", "", fmt.Sprintf("CLI to pass to %s for every launched role", brand.Command("agent")))
	launchWeztermMASCmd.Flags().Bool("no-tui", false, fmt.Sprintf("do not launch %s in the first pane", brand.Command("tui")))
	registerCompletion(launchWeztermMASCmd, "preset", completeValues("technical-spec", "functional-spec", "general-objective"))
	registerCompletion(launchWeztermMASCmd, "role", completeAgentRoles)
	registerCompletion(launchWeztermMASCmd, "cli", completeCLINames)

	launchWeztermAdversarialPairingCmd.Flags().String("doer-cli", "codex", "coding CLI for the doer session")
	launchWeztermAdversarialPairingCmd.Flags().String("goal", "", "create the blackboard with this goal when it does not exist")
	launchWeztermAdversarialPairingCmd.Flags().StringArray("reviewer", nil, "reviewer CLI or id=cli; repeat for multiple reviewers (default: codex, codex-2=codex)")
	launchWeztermAdversarialPairingCmd.Flags().Duration("prompt-delay", 2*time.Second, "delay before injecting adversarial-pairing prompts into WezTerm panes")
	launchWeztermAdversarialPairingCmd.Flags().Bool("yolo", false, "pass yolo to the doer adversarial-pairing invocation")
	registerCompletion(launchWeztermAdversarialPairingCmd, "doer-cli", completeCLINames)
	registerCompletion(launchWeztermAdversarialPairingCmd, "reviewer", completeCLINames)

	launchCmuxMASCmd.Flags().String("preset", "technical-spec", "role preset: technical-spec, functional-spec, general-objective")
	launchCmuxMASCmd.Flags().StringArray("role", nil, "role to launch; repeat to override --preset")
	launchCmuxMASCmd.Flags().String("cli", "", fmt.Sprintf("CLI to pass to %s for every launched role", brand.Command("agent")))
	launchCmuxMASCmd.Flags().Bool("no-tui", false, fmt.Sprintf("do not launch %s in the first pane", brand.Command("tui")))
	registerCompletion(launchCmuxMASCmd, "preset", completeValues("technical-spec", "functional-spec", "general-objective"))
	registerCompletion(launchCmuxMASCmd, "role", completeAgentRoles)
	registerCompletion(launchCmuxMASCmd, "cli", completeCLINames)

	launchCmuxAdversarialPairingCmd.Flags().String("doer-cli", "codex", "coding CLI for the doer session")
	launchCmuxAdversarialPairingCmd.Flags().String("goal", "", "create the blackboard with this goal when it does not exist")
	launchCmuxAdversarialPairingCmd.Flags().StringArray("reviewer", nil, "reviewer CLI or id=cli; repeat for multiple reviewers (default: codex, codex-2=codex)")
	launchCmuxAdversarialPairingCmd.Flags().Bool("yolo", false, "pass yolo to the doer adversarial-pairing invocation")
	registerCompletion(launchCmuxAdversarialPairingCmd, "doer-cli", completeCLINames)
	registerCompletion(launchCmuxAdversarialPairingCmd, "reviewer", completeCLINames)
}
