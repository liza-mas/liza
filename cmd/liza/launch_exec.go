package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/gitbash"
	"github.com/spf13/cobra"
)

const (
	weztermExecutable = string(launchBackendWezterm)
	cmuxExecutable    = string(launchBackendCmux)
)

// Waits and bounds of the interactive cmux launch.
const (
	// cmuxInteractiveReadyTimeout bounds the wait for every Codex pane to look interactive.
	cmuxInteractiveReadyTimeout = 60 * time.Second
	// cmuxInteractiveSettleDelay is the pause between the panes being ready and the first prompt.
	cmuxInteractiveSettleDelay = 2 * time.Second
	// cmuxPromptSubmitDelay is the pause before a Codex pane's screen is read to see whether its prompt was submitted.
	cmuxPromptSubmitDelay = 2 * time.Second
	// cmuxScreenPollInterval separates two readiness polls.
	cmuxScreenPollInterval = 500 * time.Millisecond
	// cmuxPromptSubmitAttempts bounds the Enter presses sent for one prompt.
	cmuxPromptSubmitAttempts = 5
	// cmuxScreenReadLines is the number of screen lines read from a pane.
	cmuxScreenReadLines = "80"
)

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
		cmd.Println(shellJoin(append([]string{weztermExecutable}, args...)))
		return nil
	}
	if _, err := exec.LookPath(weztermExecutable); err != nil {
		return cliValidationWrap(weztermExecutable+" not found on PATH", err)
	}
	launch := exec.Command(weztermExecutable, args...)
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
		cmd.Println(shellJoin(append([]string{weztermExecutable}, args...)))
		return nil
	}
	if _, err := exec.LookPath(weztermExecutable); err != nil {
		return cliValidationWrap(weztermExecutable+" not found on PATH", err)
	}
	launch := exec.Command(weztermExecutable, args...)
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
	if _, err := exec.LookPath(cmuxExecutable); err != nil {
		return cliValidationWrap(cmuxExecutable+" not found on PATH", err)
	}

	// Create workspace with first command
	createArgs := []string{
		cmuxExecutable, "new-workspace",
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
			cmuxExecutable, "new-pane",
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
	if _, err := exec.LookPath(cmuxExecutable); err != nil {
		return cliValidationWrap(cmuxExecutable+" not found on PATH", err)
	}

	// Create workspace with first pane
	createArgs := []string{
		cmuxExecutable, "new-workspace",
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
			cmuxExecutable, "new-pane",
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

	if err := waitForCmuxInteractiveSurfaces(workspaceRef, panes, surfaceRefs, cmuxInteractiveReadyTimeout); err != nil {
		return err
	}
	time.Sleep(cmuxInteractiveSettleDelay)

	// Send prompts to all panes
	for i, pane := range panes {
		if err := cmuxSendPrompt(workspaceRef, surfaceRefs[i], pane); err != nil {
			return err
		}
	}

	return nil
}

func cmuxSendText(workspaceRef, surfaceRef, text string) error {
	return exec.Command(cmuxExecutable, "send", "--workspace", workspaceRef, "--surface", surfaceRef, text).Run()
}

func cmuxSendKey(workspaceRef, surfaceRef, key string) error {
	return exec.Command(cmuxExecutable, "send-key", "--workspace", workspaceRef, "--surface", surfaceRef, key).Run()
}

func cmuxSendPrompt(workspaceRef, surfaceRef string, pane interactivePane) error {
	if err := cmuxSendText(workspaceRef, surfaceRef, pane.Prompt); err != nil {
		return cliValidationWrap("send prompt to cmux pane", err)
	}
	for attempt := 0; attempt < cmuxPromptSubmitAttempts; attempt++ {
		if err := cmuxSendKey(workspaceRef, surfaceRef, "enter"); err != nil {
			return cliValidationWrap("send enter key to cmux pane", err)
		}
		if !cmuxPaneUsesCodex(pane) {
			return nil
		}
		time.Sleep(cmuxPromptSubmitDelay)
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
	output, err := exec.Command(cmuxExecutable, "list-pane-surfaces", "--workspace", workspaceRef).CombinedOutput()
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
		time.Sleep(cmuxScreenPollInterval)
	}
}

func cmuxReadScreen(workspaceRef, surfaceRef string) (string, error) {
	output, err := exec.Command(cmuxExecutable, "read-screen", "--workspace", workspaceRef, "--surface", surfaceRef, "--lines", cmuxScreenReadLines).CombinedOutput()
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
