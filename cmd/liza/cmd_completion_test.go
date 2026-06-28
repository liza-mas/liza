package main

import (
	"bytes"
	"strings"
	"testing"
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
	for _, want := range []string{"__start_liza", "completion", "toolchain"} {
		if !strings.Contains(text, want) {
			t.Fatalf("completion output missing %q:\n%s", want, text)
		}
	}
}

func TestCompletionCommandRejectsUnknownShell(t *testing.T) {
	resetRootCmdForTest(t)

	rootCmd.SetArgs([]string{"completion", "elvish"})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("completion elvish error = nil, want error")
	}
	if !strings.Contains(err.Error(), `invalid argument "elvish"`) {
		t.Fatalf("error = %q, want cobra valid-args error", err)
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
