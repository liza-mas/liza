package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/brand"
)

// Pane split directions: WezTerm takes a flag, cmux a word. Panes alternate
// between the first and the second.
const (
	weztermSplitRight  = "--right"
	weztermSplitBottom = "--bottom"
	cmuxSplitRight     = "right"
	cmuxSplitDown      = "down"
)

func buildWeztermPaneScript(opts weztermLaunchOptions, commands [][]string) string {
	var b strings.Builder
	for i, paneCommand := range commands[1:] {
		direction := weztermSplitRight
		if i%2 == 1 {
			direction = weztermSplitBottom
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
		direction := weztermSplitRight
		if i%2 == 1 {
			direction = weztermSplitBottom
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
		cmuxExecutable, "new-workspace",
		"--name", opts.Workspace,
		"--cwd", opts.CWD,
		"--command", shellJoin(commands[0]),
	}
	cmds = append(cmds, createArgs)

	// Use a placeholder workspace ref for dry-run (would be parsed from output at runtime)
	workspaceRef := "<workspace-ref-from-new-workspace-output>"

	// Create panes for additional commands
	for i, paneCommand := range commands[1:] {
		direction := cmuxSplitRight
		if i%2 == 1 {
			direction = cmuxSplitDown
		}
		paneArgs := []string{
			cmuxExecutable, "new-pane",
			"--type", "terminal",
			"--direction", direction,
			"--workspace", workspaceRef,
		}
		cmds = append(cmds, paneArgs)
		cmds = append(cmds, []string{cmuxExecutable, "send", "--workspace", workspaceRef, "--surface", fmt.Sprintf("<surface-ref-%d-from-new-pane-output>", i+1), shellJoin(paneCommand)})
		cmds = append(cmds, []string{cmuxExecutable, "send-key", "--workspace", workspaceRef, "--surface", fmt.Sprintf("<surface-ref-%d-from-new-pane-output>", i+1), "enter"})
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
		cmuxExecutable, "new-workspace",
		"--name", opts.Workspace,
		"--cwd", opts.CWD,
		"--command", shellJoin(panes[0].Command),
	}
	cmds = append(cmds, createArgs)

	// Use a placeholder workspace ref for dry-run (would be parsed from output at runtime)
	workspaceRef := "<workspace-ref-from-new-workspace-output>"

	// Create additional panes
	for i, pane := range panes[1:] {
		direction := cmuxSplitRight
		if i%2 == 1 {
			direction = cmuxSplitDown
		}
		paneArgs := []string{
			cmuxExecutable, "new-pane",
			"--type", "terminal",
			"--direction", direction,
			"--workspace", workspaceRef,
		}
		cmds = append(cmds, paneArgs)
		surfaceRef := fmt.Sprintf("<surface-ref-%d-from-new-pane-output>", i+1)
		cmds = append(cmds, []string{cmuxExecutable, "send", "--workspace", workspaceRef, "--surface", surfaceRef, shellJoin(pane.Command)})
		cmds = append(cmds, []string{cmuxExecutable, "send-key", "--workspace", workspaceRef, "--surface", surfaceRef, "enter"})
	}

	// Send prompts to all panes
	for i, pane := range panes {
		surfaceRef := fmt.Sprintf("<surface-ref-%d-from-new-pane-output>", i)
		if i == 0 {
			surfaceRef = "<surface-ref-from-new-workspace-output>"
		}
		// Send prompt as text (not as argv to the CLI)
		sendArgs := []string{
			cmuxExecutable, "send",
			"--workspace", workspaceRef,
			"--surface", surfaceRef,
			pane.Prompt,
		}
		cmds = append(cmds, sendArgs)

		// Send enter key to submit
		enterArgs := []string{
			cmuxExecutable, "send-key",
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
