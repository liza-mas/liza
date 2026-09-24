package agent

import (
	"runtime"
	"strings"
	"testing"
)

func TestDevinToolResultPlanPreservesNativeCommandAndTaskSession(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires POSIX command paths")
	}
	req := LLMAgentRunRequest{ProjectRoot: "/tmp/project with space", TaskID: "TASK-778", AgentID: "coder-1"}
	plan := LaunchPlan{ToolName: "devin-acp", ACPXAgent: "devin acp --model 'chosen model'", ACPXSessionName: "original-task", ACPXPromptArgs: []string{"--approve-all", "--agent", "devin acp --model 'chosen model'", "prompt", "-s", "original-task"}}
	got, err := prepareDevinToolResultPlan(req, plan, "/tmp/engine binary")
	if err != nil {
		t.Fatal(err)
	}
	if got.ACPXPromptArgs[1] != "--agent" || got.ACPXPromptArgs[2] != got.ACPXAgent || got.ACPXPromptArgs[5] != "original-task-tool-results-v1" {
		t.Fatalf("%v", got.ACPXPromptArgs)
	}
	if !strings.HasSuffix(got.ACPXAgent, " -- "+plan.ACPXAgent) || !strings.Contains(got.ACPXAgent, "'DEVIN_PERMISSION_MODE=bypass'") || !strings.Contains(got.ACPXAgent, "--task-id 'TASK-778'") {
		t.Fatalf("%s", got.ACPXAgent)
	}
	again, err := prepareDevinToolResultPlan(req, plan, "/tmp/engine binary")
	if err != nil || again.ACPXAgent != got.ACPXAgent {
		t.Fatal("unstable session command")
	}
	if plan.ACPXPromptArgs[2] != plan.ACPXAgent {
		t.Fatal("mutated original plan")
	}
}
