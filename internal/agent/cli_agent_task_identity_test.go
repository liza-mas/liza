package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
)

func TestCLIAgentRunTaskIdentityEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI shell script test requires /bin/sh")
	}
	for _, taskID := range []string{"task-778", ""} {
		t.Run("task="+taskID, func(t *testing.T) {
			binDir, project := t.TempDir(), t.TempDir()
			output := filepath.Join(project, "identity.txt")
			name, legacy := brand.EnvName("TASK_ID"), brand.LegacyEnvName("TASK_ID")
			script := fmt.Sprintf("#!/bin/sh\nprintf '%%s|%%s|%%s' \"$%s\" \"$%s\" \"$%s\" > identity.txt\n", name, legacy, brand.EnvName("AGENT_ID"))
			if err := os.WriteFile(filepath.Join(binDir, "opencode"), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", binDir)
			t.Setenv(name, "stale-task")
			t.Setenv(legacy, "older-task")
			result, err := NewCLIAgent("").Run(context.Background(), LLMAgentRunRequest{
				BackendName: "opencode", AgentID: "coder-1", TaskID: taskID,
				SessionID: "provider-session-778", Prompt: "run the focused test",
				ProjectRoot: project, LaunchGate: immediateLaunchGate,
			})
			if err != nil || result.ExitCode != 0 {
				t.Fatalf("Run exit=%d err=%v", result.ExitCode, err)
			}
			got, err := os.ReadFile(output)
			if err != nil || string(got) != taskID+"|"+taskID+"|coder-1" {
				t.Fatalf("subprocess identity=%q err=%v", got, err)
			}
		})
	}
}

func TestCLIAgentInteractiveClearsTaskIdentityEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI shell script test requires /bin/sh")
	}
	binDir, project := t.TempDir(), t.TempDir()
	name, legacy := brand.EnvName("TASK_ID"), brand.LegacyEnvName("TASK_ID")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s|%%s' \"$%s\" \"$%s\" > identity.txt\n", name, legacy)
	if err := os.WriteFile(filepath.Join(binDir, "opencode"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv(name, "stale-task")
	t.Setenv(legacy, "older-task")
	code, err := NewCLIAgent("").RunInteractive(context.Background(), LLMAgentInteractiveRequest{
		BackendName: "opencode", AgentID: "coder-1", ProjectRoot: project, LaunchGate: immediateLaunchGate,
	})
	if err != nil || code != 0 {
		t.Fatalf("RunInteractive exit=%d err=%v", code, err)
	}
	got, err := os.ReadFile(filepath.Join(project, "identity.txt"))
	if err != nil || string(got) != "|" {
		t.Fatalf("subprocess identity=%q err=%v", got, err)
	}
}
