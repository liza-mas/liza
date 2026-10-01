package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// TestCLIAgentEmitsUsageEvent pins the contract the supervisor sink depends on:
// the CLI transport reports no usage object, so every Run exit path emits one
// explicit (empty) usage event before its completed event. Without it the sink
// could not tell "no usage reported" from "zero tokens used".
func TestCLIAgentEmitsUsageEvent(t *testing.T) {
	binDir := t.TempDir()
	testhelpers.WriteShellStub(t, filepath.Join(binDir, "gemini"), "#!/bin/sh\nprintf 'provider output\\n'\nexit \"${FAKE_CLI_EXIT:-0}\"\n")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	assertSingleUsageEvent := func(t *testing.T, events []LLMAgentEvent) {
		t.Helper()
		if got := countLLMAgentEvents(events, LLMAgentEventUsage); got != 1 {
			t.Fatalf("usage event count = %d, want exactly one per run: %#v", got, events)
		}
		usageIndex, completedIndex := -1, -1
		for i, event := range events {
			if event.Kind == LLMAgentEventUsage && usageIndex < 0 {
				usageIndex = i
				if reported, ok := event.Payload["usage"].(LLMAgentUsage); !ok || reported != (LLMAgentUsage{}) {
					t.Fatalf("usage payload = %#v, want the empty usage the CLI transport reports", event.Payload["usage"])
				}
			}
			if event.Kind == LLMAgentEventCompleted && completedIndex < 0 {
				completedIndex = i
			}
		}
		if completedIndex < 0 || usageIndex > completedIndex {
			t.Fatalf("usage event must precede the completed event: %#v", events)
		}
	}

	t.Run("start_failure", func(t *testing.T) {
		sink := &recordingLLMAgentEventSink{}
		missing := models.Config{AgentTools: map[string]models.AgentToolConfig{
			"missing-usage-test-cli": {
				Backend:         "cli",
				Executable:      filepath.Join(binDir, "missing-executable"),
				PromptTransport: PromptTransportStdin,
			},
		}}
		_, err := NewCLIAgent("").Run(context.Background(), LLMAgentRunRequest{
			BackendName: "missing-usage-test-cli", AgentID: "coder-1", TaskID: "task-usage",
			Prompt: "prompt body", ProjectRoot: t.TempDir(), RuntimeConfig: missing,
			EventSink: sink, LaunchGate: immediateLaunchGate,
		})
		if err == nil {
			t.Fatal("Run() error = nil, want a start failure")
		}
		assertSingleUsageEvent(t, sink.Events())
	})

	t.Run("rejected_launch", func(t *testing.T) {
		rejected := errors.New("launch rejected")
		sink := &recordingLLMAgentEventSink{}
		_, err := NewCLIAgent("").Run(context.Background(), LLMAgentRunRequest{
			BackendName: "gemini", AgentID: "coder-1", TaskID: "task-usage",
			Prompt: "prompt body", ProjectRoot: t.TempDir(), EventSink: sink,
			LaunchGate: func(context.Context, func() error) error { return rejected },
		})
		if !errors.Is(err, rejected) {
			t.Fatalf("Run() error = %v, want the rejected launch", err)
		}
		assertSingleUsageEvent(t, sink.Events())
	})

	t.Run("non_zero_exit", func(t *testing.T) {
		t.Setenv("FAKE_CLI_EXIT", "7")
		sink := &recordingLLMAgentEventSink{}
		result, err := NewCLIAgent("").Run(context.Background(), LLMAgentRunRequest{
			BackendName: "gemini", AgentID: "coder-1", TaskID: "task-usage",
			Prompt: "prompt body", ProjectRoot: t.TempDir(), EventSink: sink, LaunchGate: immediateLaunchGate,
		})
		if err != nil || result.ExitCode != 7 {
			t.Fatalf("Run = (%d, %v), want exit code 7", result.ExitCode, err)
		}
		assertSingleUsageEvent(t, sink.Events())
	})

	t.Run("success", func(t *testing.T) {
		sink := &recordingLLMAgentEventSink{}
		result, err := NewCLIAgent("").Run(context.Background(), LLMAgentRunRequest{
			BackendName: "gemini", AgentID: "coder-1", TaskID: "task-usage",
			Prompt: "prompt body", ProjectRoot: t.TempDir(), EventSink: sink, LaunchGate: immediateLaunchGate,
		})
		if err != nil || result.ExitCode != 0 {
			t.Fatalf("Run = (%d, %v), want a successful run", result.ExitCode, err)
		}
		assertSingleUsageEvent(t, sink.Events())
	})
}

// A CLI that prints its quota failure and exits 0 must not pass for a completed
// turn: the supervisor only classifies quota on a failed session. Mirrors the
// ACPX backend's rule.
func TestCLIAgentReportsExitZeroQuotaTurnAsFailure(t *testing.T) {
	binDir := t.TempDir()
	testhelpers.WriteShellStub(t, filepath.Join(binDir, "codex"), "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 'codex-cli 0.154.0'; exit 0; fi\ncat \"$FAKE_CODEX_TURN\"\nexit \"$FAKE_CLI_EXIT\"\n")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	turns := map[string]string{
		"quota":  `{"type":"turn.failed","error":{"message":"You've hit your usage limit."}}`,
		"clean":  `{"type":"turn.completed"}`,
		"quoted": `{"type":"item.completed","item":{"type":"agent_message","text":"I saw: You've hit your usage limit."}}`,
	}

	for _, tt := range []struct {
		name, turn, exit string
		wantExit         int
		wantQuota        bool
	}{
		{name: "quota on exit 0", turn: "quota", exit: "0", wantExit: 1, wantQuota: true},
		{name: "clean exit 0", turn: "clean", exit: "0", wantExit: 0},
		{name: "quoted quota text on exit 0", turn: "quoted", exit: "0", wantExit: 0},
		{name: "graceful abort keeps exit 42", turn: "quota", exit: "42", wantExit: 42},
	} {
		t.Run(tt.name, func(t *testing.T) {
			turnFile := filepath.Join(t.TempDir(), "turn.jsonl")
			if err := os.WriteFile(turnFile, []byte(turns[tt.turn]+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Setenv("FAKE_CODEX_TURN", turnFile)
			t.Setenv("FAKE_CLI_EXIT", tt.exit)
			sink := &recordingLLMAgentEventSink{}
			result, err := NewCLIAgent("").Run(context.Background(), LLMAgentRunRequest{
				BackendName: "codex", AgentID: "coder-1", TaskID: "task-quota",
				Prompt: "prompt body", ProjectRoot: t.TempDir(), EventSink: sink, LaunchGate: immediateLaunchGate,
			})
			if err != nil || result.ExitCode != tt.wantExit {
				t.Fatalf("Run = (%d, %v), want exit code %d (output %q)", result.ExitCode, err, tt.wantExit, result.Output)
			}
			var completed *LLMAgentEvent
			for _, event := range sink.Events() {
				if event.Kind == LLMAgentEventCompleted {
					completed = &event
				}
			}
			if completed == nil {
				t.Fatal("no completed event")
			}
			if got := completed.Payload["exit_code"]; got != tt.wantExit {
				t.Fatalf("completed exit_code = %v, want %d to agree with the result", got, tt.wantExit)
			}
			if _, quota := completed.Payload["quota"]; quota != tt.wantQuota {
				t.Fatalf("completed quota flag present = %t, want %t", quota, tt.wantQuota)
			}
		})
	}
}
