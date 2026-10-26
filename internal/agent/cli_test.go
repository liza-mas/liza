package agent

import "testing"

func TestNewLLMAgentForCLI(t *testing.T) {
	if _, ok := NewLLMAgentForCLI("codex-acp", "").(*ACPXAgent); !ok {
		t.Fatalf("NewLLMAgentForCLI(codex-acp) did not return *ACPXAgent")
	}
	if _, ok := NewLLMAgentForCLI("cursor-acp", "").(*ACPXAgent); !ok {
		t.Fatalf("NewLLMAgentForCLI(cursor-acp) did not return *ACPXAgent")
	}
	if _, ok := NewLLMAgentForCLI("opencode-acp", "").(*ACPXAgent); !ok {
		t.Fatalf("NewLLMAgentForCLI(opencode-acp) did not return *ACPXAgent")
	}
	if _, ok := NewLLMAgentForCLI("codex", "").(*CLIAgent); !ok {
		t.Fatalf("NewLLMAgentForCLI(codex) did not return *CLIAgent")
	}
	if _, ok := NewLLMAgentForCLI("opencode", "").(*CLIAgent); !ok {
		t.Fatalf("NewLLMAgentForCLI(opencode) did not return *CLIAgent")
	}
}
