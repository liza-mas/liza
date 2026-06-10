package main

import (
	"strings"
	"testing"
)

func TestAgentHelpListsAllRuntimeRoles(t *testing.T) {
	helpText := agentCmd.Long

	required := []string{
		"coder",
		"code-reviewer",
		"orchestrator",
		"code-planner",
		"code-plan-reviewer",
	}

	for _, role := range required {
		if !strings.Contains(helpText, role) {
			t.Fatalf("agent help missing role %q", role)
		}
	}
}

func TestContractInitFlagForCLI(t *testing.T) {
	tests := map[string]string{
		"claude":       "claude",
		"codex":        "codex",
		"codex-acp":    "codex",
		"opencode":     "opencode",
		"opencode-acp": "opencode",
		"kimi":         "claude",
	}

	for cliName, want := range tests {
		if got := contractInitFlagForCLI(cliName); got != want {
			t.Fatalf("contractInitFlagForCLI(%q) = %q, want %q", cliName, got, want)
		}
	}
}
