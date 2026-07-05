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

func TestContractInitCommandForProvider(t *testing.T) {
	tests := map[string]string{
		"claude":       "liza init --claude",
		"codex":        "liza init --codex",
		"codex-acp":    "liza init --codex",
		"cursor-acp":   "liza init --codex",
		"opencode":     "liza init --opencode",
		"opencode-acp": "liza init --opencode",
		"kimi":         "liza init --claude",
		"qwen":         "liza init --provider qwen",
	}

	for cliName, want := range tests {
		if got := contractInitCommandForProvider(cliName); got != want {
			t.Fatalf("contractInitCommandForProvider(%q) = %q, want %q", cliName, got, want)
		}
	}
}
