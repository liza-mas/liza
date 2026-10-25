package prompts

import (
	"strings"
	"testing"
)

func TestBuildBasePromptSystemPromptModeDropsContractReads(t *testing.T) {
	config := BasePromptConfig{Role: "coder", AgentID: "coder-1", TaskID: "task-1", ProjectRoot: "/project"}
	forced, err := BuildBasePrompt(config)
	if err != nil {
		t.Fatal(err)
	}
	config.SystemPromptMode = true
	injected, err := BuildBasePrompt(config)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(forced, "1. Read these contract files before anything else:") {
		t.Fatal("default prompt lost the contract-read step")
	}
	for _, unwanted := range []string{"Read these contract files", "session-init hook BLOCKS", "read_multiple_files"} {
		if strings.Contains(injected, unwanted) {
			t.Errorf("system-prompt mode prompt still contains %q", unwanted)
		}
	}
	for _, want := range []string{"are in your system prompt. Treat them as read", "Agent ID: coder-1", "2. Query your assigned task:"} {
		if !strings.Contains(injected, want) {
			t.Errorf("system-prompt mode prompt missing %q", want)
		}
	}
	// Only the FIRST ACTIONS step 1 changes.
	_, forcedTail, _ := strings.Cut(forced, "2. Query your assigned task:")
	_, injectedTail, _ := strings.Cut(injected, "2. Query your assigned task:")
	if forcedTail != injectedTail {
		t.Error("system-prompt mode changed the prompt after step 1")
	}
}
