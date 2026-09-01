package main

import (
	"bytes"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/paths"

	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/testhelpers"
	"github.com/spf13/cobra"
)

func TestBuildWeztermPaneScriptLaunchesSplitsThenPrimary(t *testing.T) {
	opts := weztermLaunchOptions{
		Class: "liza-mas-test",
		CWD:   "/tmp/project root",
	}
	script := buildWeztermPaneScript(opts, [][]string{
		{"liza", "tui"},
		{"liza", "agent", "orchestrator"},
		{"liza", "agent", "coder"},
	})

	for _, want := range []string{
		"wezterm cli split-pane --right --cwd '/tmp/project root' -- 'liza' 'agent' 'orchestrator'",
		"wezterm cli split-pane --bottom --cwd '/tmp/project root' -- 'liza' 'agent' 'coder'",
		"exec 'liza' 'tui'",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q\nscript:\n%s", want, script)
		}
	}
}

func TestBuildWeztermInteractivePaneScriptStartsCLIsWithInitialPrompts(t *testing.T) {
	opts := weztermLaunchOptions{
		Class:       "liza-adversarial",
		CWD:         "/tmp/project",
		PromptDelay: 2 * time.Second,
	}
	script := buildWeztermInteractivePaneScript(opts, []interactivePane{
		{Command: agent.InteractiveCLICommand("codex"), Prompt: pairingSkillPrompt("doer", "/tmp/board.md", false)},
		{Command: agent.InteractiveCLICommand("codex"), Prompt: pairingSkillPrompt("reviewer-codex", "/tmp/board.md", false)},
	})

	for _, want := range []string{
		"printf '%s' \"$prompt\" | wezterm cli --class 'liza-adversarial' send-text --no-paste --pane-id \"$pane_id\"",
		"printf '\\r' | wezterm cli --class 'liza-adversarial' send-text --no-paste --pane-id \"$pane_id\"",
		"pane_id_1=$(wezterm cli --class 'liza-adversarial' split-pane --right --cwd '/tmp/project' -- 'codex')",
		"wezterm_liza_send_prompt \"$pane_id_1\" '$adversarial-pairing reviewer-codex /tmp/board.md'",
		"wezterm_liza_send_prompt \"$WEZTERM_PANE\" '$adversarial-pairing doer /tmp/board.md'",
		"exec 'codex'",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q\nscript:\n%s", want, script)
		}
	}
}

func TestBuildWeztermInteractivePaneScriptUsesPromptDelay(t *testing.T) {
	opts := weztermLaunchOptions{
		Class:       "liza-adversarial",
		CWD:         "/tmp/project",
		PromptDelay: 1500 * time.Millisecond,
	}
	script := buildWeztermInteractivePaneScript(opts, []interactivePane{
		pairingInteractivePane("codex", pairingSkillPrompt("doer", "/tmp/board.md", false)),
	})

	if !strings.Contains(script, "    sleep 1.5\n") {
		t.Fatalf("script missing custom prompt delay\nscript:\n%s", script)
	}
}

func TestPairingSkillPromptUsesCodexSkillTrigger(t *testing.T) {
	got := pairingSkillPrompt("doer", "/tmp/board.md", true)
	want := "$adversarial-pairing doer /tmp/board.md yolo"
	if got != want {
		t.Fatalf("prompt = %q, want %q", got, want)
	}
}

func TestAdversarialPairingDefaultsToThreeCodexPanes(t *testing.T) {
	tmpDir := t.TempDir()
	boardPath := filepath.Join(tmpDir, paths.ProjectDirName(), "adversarial", "board.md")
	resetRootCmdForTest(t)
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs([]string{
		"launch", "wezterm", "adversarial-pairing", boardPath,
		"--goal", "Fix retry client",
		"--dry-run",
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("command returned error: %v\noutput:\n%s", err, out.String())
	}
	output := out.String()
	for _, want := range []string{
		"'codex'",
		"$adversarial-pairing doer " + boardPath,
		"$adversarial-pairing reviewer-codex " + boardPath,
		"$adversarial-pairing reviewer-codex-2 " + boardPath,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("dry-run output missing %q\noutput:\n%s", want, output)
		}
	}
	if _, err := os.Stat(boardPath); !os.IsNotExist(err) {
		t.Fatalf("dry-run created blackboard or returned unexpected stat error: %v", err)
	}
}

func TestPairingInteractiveCLICommandMapsACPToInteractiveBaseCLI(t *testing.T) {
	cmd := agent.InteractiveCLICommand("codex-acp")
	got := strings.Join(cmd, "\x00")
	if got != "codex" {
		t.Fatalf("command = %q, want codex", got)
	}
}

// TestLaunchShellPrefersGitForWindowsOverAPathMatch stands in for the trap the
// probe exists for: System32\bash.exe is the WSL launcher, it is on the machine
// PATH, and the machine PATH is searched before the user's — so whatever is
// found by name may well be a shell that cannot see C:/... paths at all. The
// decoy here occupies the same position without needing WSL installed.
func TestLaunchShellPrefersGitForWindowsOverAPathMatch(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("a bash.exe on PATH that cannot run the pane script is a Windows-only situation")
	}

	gitBash := ""
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		gitBash = statFirstExisting(filepath.Join(local, "Programs", "Git", "bin", "bash.exe"))
	}
	if gitBash == "" {
		gitBash = statFirstExisting(
			filepath.Join(os.Getenv("ProgramFiles"), "Git", "bin", "bash.exe"),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "Git", "bin", "bash.exe"),
		)
	}
	if gitBash == "" {
		t.Skip("Git for Windows is not installed in a standard location")
	}

	decoyDir := t.TempDir()
	decoy := filepath.Join(decoyDir, "bash.exe")
	if err := os.WriteFile(decoy, []byte("not a usable shell"), 0755); err != nil {
		t.Fatalf("write decoy bash: %v", err)
	}
	t.Setenv("SHELL", "")
	t.Setenv("PATH", decoyDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	got, err := launchShell()
	if err != nil {
		t.Fatalf("launchShell: %v", err)
	}

	if strings.EqualFold(got, decoy) {
		t.Fatalf("launchShell() = %q, the first bash.exe on PATH, want the Git for Windows shell %q", got, gitBash)
	}
	if !strings.EqualFold(got, gitBash) {
		t.Fatalf("launchShell() = %q, want %q", got, gitBash)
	}
}

func statFirstExisting(candidates ...string) string {
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

func TestLaunchShellIsExecutableWhenShellIsUnset(t *testing.T) {
	// Git for Windows leaves SHELL unset, and the pane script is POSIX, so the
	// fallback has to name something the OS can actually start.
	testhelpers.ResolveBashForScripts(t)
	t.Setenv("SHELL", "")

	got, err := launchShell()
	if err != nil {
		t.Fatalf("launchShell: %v", err)
	}

	if runtime.GOOS != "windows" {
		if got != "/bin/sh" {
			t.Fatalf("launchShell() = %q, want /bin/sh", got)
		}
		return
	}
	if got == "/bin/sh" {
		t.Fatal("launchShell() = /bin/sh, which Windows cannot execute")
	}
	if _, err := exec.LookPath(got); err != nil {
		t.Fatalf("launchShell() = %q, which is not executable: %v", got, err)
	}
}

func TestRunWeztermInteractiveLaunchInjectsPromptsWithNoPasteSubmit(t *testing.T) {
	tmpDir := t.TempDir()
	// The fake wezterm starts its panes in the background on purpose, so those
	// processes outlive the test. On Windows a running executable cannot be
	// deleted, and t.TempDir fails the test when its cleanup cannot remove one —
	// the assertions pass and the test still goes red. Keep the stubs out of the
	// managed directory and clean up on a best-effort basis instead.
	binDir, err := os.MkdirTemp("", "wezterm-stubs-*")
	if err != nil {
		t.Fatalf("create fake bin dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(binDir) })
	logPath := filepath.Join(tmpDir, "wezterm.log")
	counterPath := filepath.Join(tmpDir, "pane-counter")
	if err := os.WriteFile(counterPath, []byte("100"), 0644); err != nil {
		t.Fatalf("write pane counter: %v", err)
	}
	writeExecutable(t, filepath.Join(binDir, "wezterm"), `#!/bin/sh
log="$LIZA_FAKE_WEZTERM_LOG"
counter="$LIZA_FAKE_WEZTERM_COUNTER"
if [ "$1" = "start" ]; then
  echo "START $*" >> "$log"
  while [ "$#" -gt 0 ] && [ "$1" != "--" ]; do shift; done
  shift
  PATH="$LIZA_FAKE_WEZTERM_BINDIR:$PATH" WEZTERM_PANE=primary "$@" >> "$log" 2>&1 &
  exit 0
fi
if [ "$1" = "cli" ]; then
  shift
  class=""
  if [ "$1" = "--class" ]; then
    class="$2"
    shift 2
  fi
  subcommand="$1"
  shift
  case "$subcommand" in
    split-pane)
      pane_id=$(cat "$counter")
      next=$((pane_id + 1))
      echo "$next" > "$counter"
      echo "SPLIT class=$class pane=$pane_id args=$*" >> "$log"
      echo "$pane_id"
      ;;
    send-text)
      pane_id=""
      while [ "$#" -gt 0 ]; do
        if [ "$1" = "--pane-id" ]; then
          pane_id="$2"
          shift 2
          continue
        fi
        shift
      done
      payload_hex=$(od -An -tx1 | tr -d ' \n')
      echo "SEND class=$class pane=$pane_id hex=$payload_hex" >> "$log"
      ;;
  esac
fi
`)
	writeExecutable(t, filepath.Join(binDir, "codex"), `#!/bin/sh
echo "CODEX $*" >> "$LIZA_FAKE_WEZTERM_LOG"
sleep 4
`)
	shellPath := writeExecutable(t, filepath.Join(binDir, "test-shell"), `#!/bin/sh
if [ "$1" = "-lc" ]; then
  shift
  exec /bin/sh -c "$1"
fi
exec /bin/sh "$@"
`)

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SHELL", shellPath)
	t.Setenv("LIZA_FAKE_WEZTERM_BINDIR", binDir)
	t.Setenv("LIZA_FAKE_WEZTERM_LOG", logPath)
	t.Setenv("LIZA_FAKE_WEZTERM_COUNTER", counterPath)

	primaryPrompt := pairingSkillPrompt("doer", "/tmp/board.md", false)
	splitPrompt := pairingSkillPrompt("reviewer-codex", "/tmp/board.md", false)
	err = runWeztermInteractiveLaunch(launchWeztermAdversarialPairingCmd, weztermLaunchOptions{
		Class:       "liza-adversarial-test",
		Workspace:   "liza-adversarial-test",
		CWD:         tmpDir,
		PromptDelay: 2 * time.Second,
	}, []interactivePane{
		pairingInteractivePane("codex", primaryPrompt),
		pairingInteractivePane("codex", splitPrompt),
	})
	if err != nil {
		t.Fatalf("runWeztermInteractiveLaunch returned error: %v", err)
	}

	log := waitForFileContent(t, logPath, func(content string) bool {
		return strings.Contains(content, "pane=primary hex="+hex.EncodeToString([]byte(primaryPrompt))) &&
			strings.Contains(content, "pane=100 hex="+hex.EncodeToString([]byte(splitPrompt))) &&
			strings.Count(content, "hex=0d") >= 2
	})
	for _, want := range []string{
		"SPLIT class=liza-adversarial-test pane=100",
		"SEND class=liza-adversarial-test pane=primary hex=" + hex.EncodeToString([]byte(primaryPrompt)),
		"SEND class=liza-adversarial-test pane=100 hex=" + hex.EncodeToString([]byte(splitPrompt)),
		"CODEX",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("fake wezterm log missing %q\nlog:\n%s", want, log)
		}
	}
}

func TestParseReviewerLaunchSpec(t *testing.T) {
	id, cliName, err := parseReviewerLaunchSpec("openai=codex")
	if err != nil {
		t.Fatalf("parseReviewerLaunchSpec returned error: %v", err)
	}
	if id != "openai" || cliName != "codex" {
		t.Fatalf("id, cli = %q, %q; want openai, codex", id, cliName)
	}

	id, cliName, err = parseReviewerLaunchSpec("reviewer-claude")
	if err != nil {
		t.Fatalf("parseReviewerLaunchSpec returned error: %v", err)
	}
	if id != "claude" || cliName != "claude" {
		t.Fatalf("id, cli = %q, %q; want claude, claude", id, cliName)
	}
}

func TestInitialAdversarialPairingBlackboardIncludesGoalAndYolo(t *testing.T) {
	now := time.Date(2026, 6, 11, 4, 50, 0, 0, time.UTC)
	content := initialAdversarialPairingBlackboard("Fix retry client", true, now)

	for _, want := range []string{
		"phase: DRAFT",
		"yolo: true",
		`phase_updated_at: "2026-06-11T04:50:00Z"`,
		"## Goal\n\nFix retry client",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("blackboard missing %q\ncontent:\n%s", want, content)
		}
	}
}

func TestEnsureAdversarialPairingBlackboardRequiresGoalWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), paths.ProjectDirName(), "adversarial", "retry-client.md")
	err := ensureAdversarialPairingBlackboard(path, "", false, false, launchWeztermAdversarialPairingCmd)
	if err == nil {
		t.Fatal("expected missing blackboard without goal to fail")
	}
	if !strings.Contains(err.Error(), "pass --goal") {
		t.Fatalf("error = %q, want --goal guidance", err.Error())
	}
}

func TestEnsureAdversarialPairingBlackboardCreatesMissingBoard(t *testing.T) {
	path := filepath.Join(t.TempDir(), paths.ProjectDirName(), "adversarial", "retry-client.md")
	if err := ensureAdversarialPairingBlackboard(path, "Fix retry client", false, false, launchWeztermAdversarialPairingCmd); err != nil {
		t.Fatalf("ensureAdversarialPairingBlackboard returned error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read created blackboard: %v", err)
	}
	if !strings.Contains(string(data), "Fix retry client") {
		t.Fatalf("created blackboard missing goal:\n%s", string(data))
	}
}

func TestEnsureAdversarialPairingBlackboardDryRunDoesNotCreateMissingBoard(t *testing.T) {
	path := filepath.Join(t.TempDir(), paths.ProjectDirName(), "adversarial", "retry-client.md")
	if err := ensureAdversarialPairingBlackboard(path, "Fix retry client", false, true, launchWeztermAdversarialPairingCmd); err != nil {
		t.Fatalf("ensureAdversarialPairingBlackboard returned error: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("dry-run created blackboard or returned unexpected stat error: %v", err)
	}
}

func TestResolveLaunchPathUsesProvidedWorkingDirectory(t *testing.T) {
	got, err := resolveLaunchPath("/tmp/project", paths.ProjectDirName()+"/adversarial/retry-client.md")
	if err != nil {
		t.Fatalf("resolveLaunchPath returned error: %v", err)
	}
	// Compare on forward-slash form: on Windows filepath.Abs prepends the
	// current drive to "/tmp/project" (-> C:\tmp\project), and uses backslashes,
	// so a literal equality check against the Unix-shaped expected path fails.
	// Assert the cwd and the relative tail are both present instead.
	gotSlash := filepath.ToSlash(got)
	if !strings.Contains(gotSlash, "tmp/project") {
		t.Fatalf("path = %q, want to contain the provided working directory", got)
	}
	wantSuffix := filepath.ToSlash(filepath.Join(paths.ProjectDirName(), "adversarial", "retry-client.md"))
	if !strings.HasSuffix(gotSlash, wantSuffix) {
		t.Fatalf("path = %q, want to end with the relative target", got)
	}
}

func TestBuildCmuxLaunchCommandsCreatesWorkspaceAndPanes(t *testing.T) {
	opts := cmuxLaunchOptions{
		Workspace: "liza-mas-test",
		CWD:       "/tmp/project root",
	}
	cmds, err := buildCmuxLaunchCommands(opts, [][]string{
		{"liza", "tui"},
		{"liza", "agent", "orchestrator"},
		{"liza", "agent", "coder"},
	})
	if err != nil {
		t.Fatalf("buildCmuxLaunchCommands returned error: %v", err)
	}

	// Check workspace creation command
	if len(cmds) < 1 {
		t.Fatal("expected at least 1 command")
	}
	createCmd := cmds[0]
	if createCmd[0] != "cmux" || createCmd[1] != "new-workspace" {
		t.Fatalf("first command should be cmux new-workspace, got %v", createCmd)
	}
	if !containsString(createCmd, "--name") || !containsString(createCmd, "liza-mas-test") {
		t.Fatal("workspace creation command missing --name or workspace name")
	}
	if !containsString(createCmd, "--cwd") || !containsString(createCmd, "/tmp/project root") {
		t.Fatal("workspace creation command missing --cwd or CWD")
	}

	// Check pane creation commands use workspace ref placeholder
	if len(cmds) < 3 {
		t.Fatal("expected at least 3 commands (workspace + 2 panes)")
	}
	pane1Cmd := cmds[1]
	if pane1Cmd[0] != "cmux" || pane1Cmd[1] != "new-pane" {
		t.Fatalf("second command should be cmux new-pane, got %v", pane1Cmd)
	}
	if !containsString(pane1Cmd, "--direction") || !containsString(pane1Cmd, "right") {
		t.Fatal("first pane command missing --direction right")
	}
	// Check that it uses the workspace ref placeholder, not the name
	if !containsString(pane1Cmd, "<workspace-ref-from-new-workspace-output>") {
		t.Fatal("pane command should use workspace ref placeholder")
	}
	if containsString(pane1Cmd, "liza-mas-test") && !containsString(pane1Cmd, "--name") {
		t.Fatal("pane command should not use workspace name in --workspace argument")
	}

	pane2Cmd := cmds[4]
	if pane2Cmd[0] != "cmux" || pane2Cmd[1] != "new-pane" {
		t.Fatalf("third command should be cmux new-pane, got %v", pane2Cmd)
	}
	if !containsString(pane2Cmd, "--direction") || !containsString(pane2Cmd, "down") {
		t.Fatal("second pane command missing --direction down")
	}
}

func TestBuildCmuxInteractiveLaunchCommandsSendsPromptsWithEnter(t *testing.T) {
	opts := cmuxLaunchOptions{
		Workspace: "liza-adversarial",
		CWD:       "/tmp/project",
	}
	cmds, err := buildCmuxInteractiveLaunchCommands(opts, []interactivePane{
		{Command: agent.InteractiveCLICommand("codex"), Prompt: pairingSkillPrompt("doer", "/tmp/board.md", false)},
		{Command: agent.InteractiveCLICommand("codex"), Prompt: pairingSkillPrompt("reviewer-codex", "/tmp/board.md", false)},
	})
	if err != nil {
		t.Fatalf("buildCmuxInteractiveLaunchCommands returned error: %v", err)
	}

	// Check workspace creation
	if len(cmds) < 1 {
		t.Fatal("expected at least 1 command")
	}
	createCmd := cmds[0]
	if createCmd[0] != "cmux" || createCmd[1] != "new-workspace" {
		t.Fatalf("first command should be cmux new-workspace, got %v", createCmd)
	}

	// Check that we send startup commands and prompts separately.
	promptSendCount := 0
	startupSendCount := 0
	enterCount := 0
	for _, cmd := range cmds {
		if len(cmd) > 0 && cmd[0] == "cmux" {
			if len(cmd) > 1 && cmd[1] == "send" {
				cmdStr := strings.Join(cmd, " ")
				if strings.Contains(cmdStr, "$adversarial-pairing") {
					promptSendCount++
				} else if strings.Contains(cmdStr, "'codex'") {
					startupSendCount++
				} else {
					t.Fatalf("unexpected cmux send command: %v", cmd)
				}
				// Check that it uses workspace ref placeholder, not the name
				if !containsString(cmd, "<workspace-ref-from-new-workspace-output>") {
					t.Fatal("send command should use workspace ref placeholder")
				}
				if containsString(cmd, "liza-adversarial") && !containsString(cmd, "--name") {
					t.Fatal("send command should not use workspace name in --workspace argument")
				}
			}
			if len(cmd) > 1 && cmd[1] == "send-key" {
				enterCount++
				if !containsString(cmd, "enter") {
					t.Fatal("send-key command missing enter")
				}
				// Check that it uses workspace ref placeholder, not the name
				if !containsString(cmd, "<workspace-ref-from-new-workspace-output>") {
					t.Fatal("send-key command should use workspace ref placeholder")
				}
				if containsString(cmd, "liza-adversarial") && !containsString(cmd, "--name") {
					t.Fatal("send-key command should not use workspace name in --workspace argument")
				}
			}
		}
	}

	if promptSendCount != 2 {
		t.Fatalf("expected 2 prompt send commands (one per pane), got %d", promptSendCount)
	}
	if startupSendCount != 1 {
		t.Fatalf("expected 1 startup send command for the additional pane, got %d", startupSendCount)
	}
	if enterCount != 3 {
		t.Fatalf("expected 3 send-key enter commands (startup plus one prompt per pane), got %d", enterCount)
	}
}

func TestCmuxMASDefaultsToTechnicalSpecPreset(t *testing.T) {
	tmpDir := t.TempDir()
	initGitRepoForLaunchTest(t, tmpDir)
	// Create a minimal Liza project structure
	if err := os.MkdirAll(filepath.Join(tmpDir, paths.ProjectDirName()), 0755); err != nil {
		t.Fatalf("create project runtime directory: %v", err)
	}
	statePath := filepath.Join(tmpDir, paths.ProjectDirName(), "state.yaml")
	stateContent := `version: 1
goal:
  id: goal-test
  description: "Test goal"
  status: IN_PROGRESS
tasks: []
agents: {}
`
	if err := os.WriteFile(statePath, []byte(stateContent), 0644); err != nil {
		t.Fatalf("write state.yaml: %v", err)
	}

	resetRootCmdForTest(t)
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs([]string{
		"launch", "cmux", "mas",
		"--cwd", tmpDir,
		"--dry-run",
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("command returned error: %v\noutput:\n%s", err, out.String())
	}
	output := out.String()
	for _, want := range []string{
		"cmux", "new-workspace",
		brand.BinaryName, "tui",
		brand.BinaryName, "agent", "orchestrator",
		brand.BinaryName, "agent", "code-planner",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("dry-run output missing %q\noutput:\n%s", want, output)
		}
	}
}

func TestMASLaunchRejectsExplicitCWDOutsideLizaProject(t *testing.T) {
	tmpDir := t.TempDir()
	resetRootCmdForTest(t)
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs([]string{
		"launch", "cmux", "mas",
		"--cwd", tmpDir,
		"--dry-run",
	})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatalf("expected command to reject non-project --cwd\noutput:\n%s", out.String())
	}
	if !strings.Contains(err.Error(), "resolve --cwd git root") {
		t.Fatalf("error = %q, want --cwd git root validation", err)
	}
}

func TestCmuxAdversarialPairingDefaultsToThreeCodexPanes(t *testing.T) {
	tmpDir := t.TempDir()
	boardPath := filepath.Join(tmpDir, paths.ProjectDirName(), "adversarial", "board.md")
	resetRootCmdForTest(t)
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs([]string{
		"launch", "cmux", "adversarial-pairing", boardPath,
		"--goal", "Fix retry client",
		"--cwd", tmpDir,
		"--dry-run",
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("command returned error: %v\noutput:\n%s", err, out.String())
	}
	output := out.String()
	for _, want := range []string{
		"cmux", "new-workspace",
		"codex",
		"$adversarial-pairing doer " + boardPath,
		"$adversarial-pairing reviewer-codex " + boardPath,
		"$adversarial-pairing reviewer-codex-2 " + boardPath,
		"send-key", "enter",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("dry-run output missing %q\noutput:\n%s", want, output)
		}
	}
	if _, err := os.Stat(boardPath); !os.IsNotExist(err) {
		t.Fatalf("dry-run created blackboard or returned unexpected stat error: %v", err)
	}
}

func TestRunCmuxInteractiveLaunchInjectsPromptsWithSendAndEnter(t *testing.T) {
	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create fake bin dir: %v", err)
	}
	logPath := filepath.Join(tmpDir, "cmux.log")
	writeExecutable(t, filepath.Join(binDir, "cmux"), `#!/bin/sh
log="$LIZA_FAKE_CMUX_LOG"
if [ "$1" = "new-workspace" ]; then
  echo "NEW_WORKSPACE $*" >> "$log"
  # Return a workspace reference
  echo "OK workspace:42"
  exit 0
fi
if [ "$1" = "new-pane" ]; then
  # Check that --workspace uses the ref, not the name
  for arg in "$@"; do
    if [ "$arg" = "--workspace" ]; then
      next_arg=""
      found=0
      for a in "$@"; do
        if [ "$found" = "1" ]; then
          next_arg="$a"
          break
        fi
        if [ "$a" = "--workspace" ]; then
          found=1
        fi
      done
	      if [ "$next_arg" != "workspace:42" ]; then
	        echo "ERROR: new-pane used wrong workspace handle: $next_arg" >> "$log"
	        exit 1
	      fi
    fi
  done
  echo "NEW_PANE $*" >> "$log"
  # Return a surface reference
  echo "OK surface:7 pane:7 workspace:42"
  exit 0
fi
if [ "$1" = "list-pane-surfaces" ]; then
  echo "LIST_PANE_SURFACES $*" >> "$log"
  echo "* surface:5 'codex' [selected]"
  exit 0
fi
if [ "$1" = "send" ]; then
  # Check that --workspace uses the ref, not the name
  for arg in "$@"; do
    if [ "$arg" = "--workspace" ]; then
      next_arg=""
      found=0
      for a in "$@"; do
        if [ "$found" = "1" ]; then
          next_arg="$a"
          break
        fi
        if [ "$a" = "--workspace" ]; then
          found=1
        fi
      done
	      if [ "$next_arg" != "workspace:42" ]; then
	        echo "ERROR: send used wrong workspace handle: $next_arg" >> "$log"
	        exit 1
	      fi
    fi
  done
  surface_arg=""
  found_surface=0
  for a in "$@"; do
    if [ "$found_surface" = "1" ]; then
      surface_arg="$a"
      break
    fi
    if [ "$a" = "--surface" ]; then
      found_surface=1
    fi
  done
  if [ "$surface_arg" != "surface:5" ] && [ "$surface_arg" != "surface:7" ]; then
    echo "ERROR: send used wrong surface handle: $surface_arg" >> "$log"
    exit 1
  fi
  echo "SEND $*" >> "$log"
  exit 0
fi
if [ "$1" = "send-key" ]; then
  # Check that --workspace uses the ref, not the name
  for arg in "$@"; do
    if [ "$arg" = "--workspace" ]; then
      next_arg=""
      found=0
      for a in "$@"; do
        if [ "$found" = "1" ]; then
          next_arg="$a"
          break
        fi
        if [ "$a" = "--workspace" ]; then
          found=1
        fi
      done
	      if [ "$next_arg" != "workspace:42" ]; then
	        echo "ERROR: send-key used wrong workspace handle: $next_arg" >> "$log"
	        exit 1
	      fi
    fi
  done
  surface_arg=""
  found_surface=0
  for a in "$@"; do
    if [ "$found_surface" = "1" ]; then
      surface_arg="$a"
      break
    fi
    if [ "$a" = "--surface" ]; then
      found_surface=1
    fi
  done
  if [ "$surface_arg" != "surface:5" ] && [ "$surface_arg" != "surface:7" ]; then
    echo "ERROR: send-key used wrong surface handle: $surface_arg" >> "$log"
    exit 1
  fi
  echo "SEND_KEY $*" >> "$log"
  exit 0
fi
if [ "$1" = "read-screen" ]; then
  echo "READ_SCREEN $*" >> "$log"
  echo "OpenAI Codex"
  echo "›"
  exit 0
fi
`)
	writeExecutable(t, filepath.Join(binDir, "codex"), `#!/bin/sh
echo "CODEX $*" >> "$LIZA_FAKE_CMUX_LOG"
sleep 1
`)

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("LIZA_FAKE_CMUX_LOG", logPath)

	primaryPrompt := pairingSkillPrompt("doer", "/tmp/board.md", false)
	splitPrompt := pairingSkillPrompt("reviewer-codex", "/tmp/board.md", false)
	err := runCmuxInteractiveLaunch(&cobra.Command{}, cmuxLaunchOptions{
		Workspace: "liza-adversarial-test",
		CWD:       tmpDir,
	}, []interactivePane{
		pairingInteractivePane("codex", primaryPrompt),
		pairingInteractivePane("codex", splitPrompt),
	})
	if err != nil {
		t.Fatalf("runCmuxInteractiveLaunch returned error: %v", err)
	}

	log := waitForFileContent(t, logPath, func(content string) bool {
		return strings.Contains(content, "NEW_WORKSPACE") &&
			strings.Contains(content, "NEW_PANE") &&
			strings.Contains(content, "SEND") &&
			strings.Contains(content, "SEND_KEY")
	})
	// Check that no errors occurred
	if strings.Contains(log, "ERROR:") {
		t.Fatalf("fake cmux detected errors:\nlog:\n%s", log)
	}
	for _, want := range []string{
		"NEW_WORKSPACE",
		"NEW_PANE",
		primaryPrompt,
		"SEND_KEY",
		splitPrompt,
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("fake cmux log missing %q\nlog:\n%s", want, log)
		}
	}
}

func TestRunCmuxInteractiveLaunchAllowsNonCodexPanes(t *testing.T) {
	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create fake bin dir: %v", err)
	}
	logPath := filepath.Join(tmpDir, "cmux.log")
	writeExecutable(t, filepath.Join(binDir, "cmux"), `#!/bin/sh
log="$LIZA_FAKE_CMUX_LOG"
if [ "$1" = "new-workspace" ]; then
  echo "NEW_WORKSPACE $*" >> "$log"
  echo "OK workspace:42"
  exit 0
fi
if [ "$1" = "new-pane" ]; then
  echo "NEW_PANE $*" >> "$log"
  echo "OK surface:7 pane:7 workspace:42"
  exit 0
fi
if [ "$1" = "list-pane-surfaces" ]; then
  echo "LIST_PANE_SURFACES $*" >> "$log"
  echo "* surface:5 'claude' [selected]"
  exit 0
fi
if [ "$1" = "send" ]; then
  echo "SEND $*" >> "$log"
  exit 0
fi
if [ "$1" = "send-key" ]; then
  echo "SEND_KEY $*" >> "$log"
  exit 0
fi
if [ "$1" = "read-screen" ]; then
  echo "READ_SCREEN $*" >> "$log"
  exit 23
fi
`)
	writeExecutable(t, filepath.Join(binDir, "claude"), `#!/bin/sh
echo "CLAUDE $*" >> "$LIZA_FAKE_CMUX_LOG"
sleep 1
`)

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("LIZA_FAKE_CMUX_LOG", logPath)

	primaryPrompt := pairingSkillPrompt("doer", "/tmp/board.md", false)
	splitPrompt := pairingSkillPrompt("reviewer-claude", "/tmp/board.md", false)
	err := runCmuxInteractiveLaunch(&cobra.Command{}, cmuxLaunchOptions{
		Workspace: "liza-adversarial-test",
		CWD:       tmpDir,
	}, []interactivePane{
		pairingInteractivePane("claude", primaryPrompt),
		pairingInteractivePane("claude", splitPrompt),
	})
	if err != nil {
		t.Fatalf("runCmuxInteractiveLaunch returned error: %v", err)
	}

	log := waitForFileContent(t, logPath, func(content string) bool {
		return strings.Contains(content, primaryPrompt) && strings.Contains(content, splitPrompt)
	})
	if strings.Contains(log, "READ_SCREEN") {
		t.Fatalf("non-codex panes should not use Codex read-screen readiness scraping\nlog:\n%s", log)
	}
}

func TestCmuxSendPromptReturnsReadScreenErrorForCodex(t *testing.T) {
	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create fake bin dir: %v", err)
	}
	writeExecutable(t, filepath.Join(binDir, "cmux"), `#!/bin/sh
if [ "$1" = "send" ]; then
  exit 0
fi
if [ "$1" = "send-key" ]; then
  exit 0
fi
if [ "$1" = "read-screen" ]; then
  exit 9
fi
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	err := cmuxSendPrompt("workspace:1", "surface:1", pairingInteractivePane("codex", "$adversarial-pairing doer /tmp/board.md"))
	if err == nil {
		t.Fatal("expected read-screen verification error")
	}
	if !strings.Contains(err.Error(), "verify cmux prompt submission") {
		t.Fatalf("error = %q, want verification context", err)
	}
}

func TestParseCmuxRef(t *testing.T) {
	for _, tt := range []struct {
		name    string
		output  string
		refType string
		want    string
	}{
		{name: "workspace ok prefix", output: "OK workspace:42\n", refType: "workspace", want: "workspace:42"},
		{name: "surface pane workspace", output: "OK surface:7 pane:7 workspace:42\n", refType: "surface", want: "surface:7"},
		{name: "bare ref", output: "workspace:3\n", refType: "workspace", want: "workspace:3"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCmuxRef([]byte(tt.output), tt.refType)
			if err != nil {
				t.Fatalf("parseCmuxRef returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("ref = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCmuxScreenLooksInteractiveWaitsForReadyPrompt(t *testing.T) {
	for _, tt := range []struct {
		name   string
		screen string
		ready  bool
	}{
		{
			name: "not codex",
			screen: `
zsh
›
`,
		},
		{
			name: "codex still starting mcps",
			screen: `
OpenAI Codex
• Starting MCP servers...
› Explain this codebase
`,
		},
		{
			name: "codex busy",
			screen: `
OpenAI Codex
esc to interrupt
• Working
`,
		},
		{
			name: "codex ready",
			screen: `
OpenAI Codex
› Explain this codebase
`,
			ready: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := cmuxScreenLooksInteractive(tt.screen); got != tt.ready {
				t.Fatalf("cmuxScreenLooksInteractive() = %v, want %v", got, tt.ready)
			}
		})
	}
}

func TestCmuxScreenNeedsHostSelection(t *testing.T) {
	screen := `
Pick host for agent
> local
  genesis
←↓↑→ navigate • enter submit
`
	if !cmuxScreenNeedsHostSelection(screen) {
		t.Fatal("expected host selection prompt to be detected")
	}
	if cmuxScreenNeedsHostSelection("OpenAI Codex\n› Explain this codebase") {
		t.Fatal("did not expect normal codex prompt to need host selection")
	}
}

func TestCmuxPromptStillPending(t *testing.T) {
	prompt := "$adversarial-pairing doer /tmp/board.md"
	if !cmuxPromptStillPending("› "+prompt+"\n\n  gpt-5.5 high", prompt) {
		t.Fatal("expected prompt to be detected as still pending")
	}
	if cmuxPromptStillPending("› "+prompt+"\n\n• Working (1s • esc to interrupt)", prompt) {
		t.Fatal("did not expect working prompt to be detected as still pending")
	}
}

// writeExecutable installs a stub command and returns the path that can be
// executed, which carries a .cmd extension on Windows.
func writeExecutable(t *testing.T, path, content string) string {
	t.Helper()
	return testhelpers.WriteShellStub(t, path, content)
}

func waitForFileContent(t *testing.T, path string, predicate func(string) bool) string {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(path)
		last = string(data)
		if predicate(last) {
			return last
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for expected file content in %s\nlast content:\n%s", path, last)
	return ""
}

func initGitRepoForLaunchTest(t *testing.T, dir string) {
	t.Helper()
	output, err := exec.Command("git", "-C", dir, "init").CombinedOutput()
	if err != nil {
		t.Fatalf("git init failed: %v\n%s", err, string(output))
	}
}
