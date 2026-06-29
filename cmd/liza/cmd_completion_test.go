package main

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestCompletionCommandGeneratesBashScript(t *testing.T) {
	resetRootCmdForTest(t)

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs([]string{"completion", "bash"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("completion bash failed: %v", err)
	}

	text := out.String()
	for _, want := range []string{"__start_liza", "__complete"} {
		if !strings.Contains(text, want) {
			t.Fatalf("completion output missing %q:\n%s", want, text)
		}
	}
}

func TestShellCompleteCompletionCommandShellNames(t *testing.T) {
	output := executeShellComplete(t, "completion", "")
	for _, want := range []string{"bash", "zsh", "fish", "powershell"} {
		if !completionOutputContains(output, want) {
			t.Fatalf("completion command shell completion missing %q:\n%s", want, output)
		}
	}
}

func TestShellCompleteAgentRoleAndCLIFlag(t *testing.T) {
	roleOutput := executeShellComplete(t, "agent", "co")
	for _, want := range []string{"code-reviewer", "coder"} {
		if !completionOutputContains(roleOutput, want) {
			t.Fatalf("agent role completion missing %q:\n%s", want, roleOutput)
		}
	}

	cliOutput := executeShellComplete(t, "agent", "--cli", "c")
	for _, want := range []string{"claude", "codex", "codex-acp", "cursor-acp"} {
		if !completionOutputContains(cliOutput, want) {
			t.Fatalf("agent --cli completion missing %q:\n%s", want, cliOutput)
		}
	}
}

func TestShellCompleteToolchainAndLaunchFlags(t *testing.T) {
	toolchainOutput := executeShellComplete(t, "toolchain", "install", "--profile", "f")
	if !completionOutputContains(toolchainOutput, "full") {
		t.Fatalf("toolchain --profile completion missing full:\n%s", toolchainOutput)
	}

	launchOutput := executeShellComplete(t, "launch", "cmux", "mas", "--preset", "f")
	if !completionOutputContains(launchOutput, "functional-spec") {
		t.Fatalf("launch --preset completion missing functional-spec:\n%s", launchOutput)
	}
}

func TestShellCompleteSubmitVerdictSecondArgument(t *testing.T) {
	output := executeShellComplete(t, "submit-verdict", "task-1", "R")
	if !completionOutputContains(output, "REJECTED") {
		t.Fatalf("submit-verdict second argument completion missing REJECTED:\n%s", output)
	}
}

func TestShellCompleteRoleFallbackUsesCanonicalRoles(t *testing.T) {
	output := executeShellComplete(t, "--project-root", t.TempDir(), "agent", "integ")
	if !completionOutputContains(output, "integration-analyst") {
		t.Fatalf("agent role fallback completion missing integration-analyst:\n%s", output)
	}
}

func TestShellCompleteToolIDsOnlyIncludesAllForToolFlag(t *testing.T) {
	includeOutput := executeShellComplete(t, "toolchain", "install", "--include", "")
	if completionOutputContains(includeOutput, "all") {
		t.Fatalf("toolchain --include completion unexpectedly included all:\n%s", includeOutput)
	}

	toolOutput := executeShellComplete(t, "toolchain", "doctor", "--tool", "")
	if !completionOutputContains(toolOutput, "all") {
		t.Fatalf("toolchain --tool completion missing all:\n%s", toolOutput)
	}
}

func TestShellCompleteDoesNotSuggestTaskIDsForFreeTextArgs(t *testing.T) {
	projectRoot := setupCompletionProject(t)

	output := executeShellComplete(t, "--project-root", projectRoot, "cancel-task", "task-1", "")
	for _, unwanted := range []string{"task-1", "task-2"} {
		if completionOutputContains(output, unwanted) {
			t.Fatalf("cancel-task reason completion unexpectedly included %q:\n%s", unwanted, output)
		}
	}
}

func executeShellComplete(t *testing.T, args ...string) string {
	t.Helper()
	resetRootCmdForTest(t)

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs(append([]string{"__complete"}, args...))

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("__complete %s failed: %v\n%s", strings.Join(args, " "), err, out.String())
	}
	return out.String()
}

func completionOutputContains(output, want string) bool {
	for _, line := range strings.Split(output, "\n") {
		if line == want || strings.HasPrefix(line, want+"\t") {
			return true
		}
	}
	return false
}

func setupCompletionProject(t *testing.T) string {
	t.Helper()

	projectRoot := t.TempDir()
	cmd := exec.Command("git", "-C", projectRoot, "init")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}

	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		{ID: "task-1"},
		{ID: "task-2"},
	}
	state.Agents = map[string]models.Agent{
		"coder-1": {Role: "coder"},
	}
	testhelpers.WriteInitialState(t, statePath, state)
	return projectRoot
}
