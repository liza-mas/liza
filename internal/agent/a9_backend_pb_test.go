package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/providers"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// Characterization of the CLI and ACPX backend Run paths (A9 backend PB-1..PB-6).
// These tests were written green on the unchanged tree before the backend run
// concerns were separated, and pin the event order, launch-gate placement, mask
// boundaries, completion mapping, command preparation and cleanup ownership.

const (
	pbSecret = "sk-pb-backend-secret"
	// pbACPXSecret is the literal the fake acpx prints for its secret prompts.
	pbACPXSecret = "sk-acpx-secret"
)

// pbKinds lists event kinds in order, collapsing repeated output chunks whose
// count depends on pipe scheduling.
func pbKinds(events []LLMAgentEvent) []LLMAgentEventKind {
	var out []LLMAgentEventKind
	for _, event := range events {
		repeatable := event.Kind == LLMAgentEventOutputChunk || event.Kind == LLMAgentEventMessage
		if repeatable && len(out) > 0 && out[len(out)-1] == event.Kind {
			continue
		}
		out = append(out, event.Kind)
	}
	return out
}

func pbEventOf(t *testing.T, events []LLMAgentEvent, kind LLMAgentEventKind) LLMAgentEvent {
	t.Helper()
	for _, event := range events {
		if event.Kind == kind {
			return event
		}
	}
	t.Fatalf("no %s event in %#v", kind, events)
	return LLMAgentEvent{}
}

func pbInstallStub(t *testing.T, name, script string) {
	t.Helper()
	binDir := t.TempDir()
	testhelpers.WriteShellStub(t, filepath.Join(binDir, name), script)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func pbCLIRequest(t *testing.T, sink LLMAgentEventSink) LLMAgentRunRequest {
	t.Helper()
	return LLMAgentRunRequest{
		BackendName: "gemini", AgentID: "coder-1", TaskID: "task-pb", SessionID: "session-pb", WarmSession: true,
		Prompt: "pb prompt", ProjectRoot: t.TempDir(), EventSink: sink, LaunchGate: immediateLaunchGate,
	}
}

func pbACPXRequest(t *testing.T, prompt string, sink LLMAgentEventSink) LLMAgentRunRequest {
	t.Helper()
	return LLMAgentRunRequest{
		BackendName: "codex-acp", AgentID: "coder-1", TaskID: "task-pb",
		Prompt: prompt, ProjectRoot: t.TempDir(), EventSink: sink, LaunchGate: immediateLaunchGate,
	}
}

func pbInstallFakeACPX(t *testing.T) string {
	t.Helper()
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "acpx.log")
	writeFakeACPX(t, filepath.Join(binDir, "acpx"), logPath)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath
}

// blockingACPXScript answers session probes at once and holds the prompt until
// the release file appears.
func blockingACPXScript(logPath, release string) string {
	return `#!/bin/sh
printf 'ARGS:%s\n' "$*" >> "` + filepath.ToSlash(logPath) + `"
case "$*" in
  *" sessions show "*) exit 2 ;;
  *" sessions ensure "*) exit 0 ;;
  *" prompt "*)
    cat > /dev/null
    while [ ! -f "` + filepath.ToSlash(release) + `" ]; do sleep 0.05; done
    printf '%s\n' '{"params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"text":"released"}}}}'
    exit 0 ;;
  *) exit 2 ;;
esac
`
}

func TestA9BackendPB1EventOrder(t *testing.T) {
	t.Run("cli_success", func(t *testing.T) {
		pbInstallStub(t, "gemini", "#!/bin/sh\nprintf 'out\\n'\nprintf 'err\\n' >&2\n")
		sink := &recordingLLMAgentEventSink{}
		result, err := NewCLIAgent("").Run(context.Background(), pbCLIRequest(t, sink))
		if err != nil || result.ExitCode != 0 {
			t.Fatalf("Run = (%d, %v)", result.ExitCode, err)
		}
		events := sink.Events()
		want := []LLMAgentEventKind{LLMAgentEventStarted, LLMAgentEventOutputChunk, LLMAgentEventUsage, LLMAgentEventCompleted}
		if got := pbKinds(events); !slices.Equal(got, want) {
			t.Fatalf("kinds = %v, want %v", got, want)
		}
		if mode := events[0].Payload["mode"]; mode != "run" {
			t.Fatalf("started mode = %v, want run", mode)
		}
		completed := pbEventOf(t, events, LLMAgentEventCompleted)
		if completed.Payload["exit_code"] != 0 || completed.Message != "" {
			t.Fatalf("completed = %#v", completed)
		}
		for _, event := range events {
			if event.BackendName != "gemini" || event.AgentID != "coder-1" || event.TaskID != "task-pb" || event.SessionID != "session-pb" {
				t.Fatalf("event attribution = %#v", event)
			}
		}
	})

	t.Run("cli_nonzero_exit", func(t *testing.T) {
		pbInstallStub(t, "gemini", "#!/bin/sh\nprintf 'out\\n'\nexit 7\n")
		sink := &recordingLLMAgentEventSink{}
		result, err := NewCLIAgent("").Run(context.Background(), pbCLIRequest(t, sink))
		if err != nil || result.ExitCode != 7 {
			t.Fatalf("Run = (%d, %v)", result.ExitCode, err)
		}
		want := []LLMAgentEventKind{LLMAgentEventStarted, LLMAgentEventOutputChunk, LLMAgentEventUsage, LLMAgentEventCompleted}
		if got := pbKinds(sink.Events()); !slices.Equal(got, want) {
			t.Fatalf("kinds = %v, want %v", got, want)
		}
		if completed := pbEventOf(t, sink.Events(), LLMAgentEventCompleted); completed.Payload["exit_code"] != 7 {
			t.Fatalf("completed = %#v", completed)
		}
	})

	t.Run("cli_prepare_failure_emits_no_started", func(t *testing.T) {
		sink := &recordingLLMAgentEventSink{}
		req := pbCLIRequest(t, sink)
		req.BackendName = "pb-missing-cli"
		req.RuntimeConfig = models.Config{AgentTools: map[string]models.AgentToolConfig{
			"pb-missing-cli": {Backend: "cli", Executable: filepath.Join(t.TempDir(), "absent"), PromptTransport: string(providers.PromptTransportStdin)},
		}}
		result, err := NewCLIAgent("").Run(context.Background(), req)
		if err == nil {
			t.Fatal("Run error = nil, want a start failure")
		}
		want := []LLMAgentEventKind{LLMAgentEventUsage, LLMAgentEventCompleted}
		if got := pbKinds(sink.Events()); !slices.Equal(got, want) {
			t.Fatalf("kinds = %v, want %v", got, want)
		}
		completed := pbEventOf(t, sink.Events(), LLMAgentEventCompleted)
		if completed.Message != err.Error() || completed.Payload["error"] != err.Error() {
			t.Fatalf("completed = %#v, want the error text as message and payload", completed)
		}
		if result.ExitCode != 0 || result.Output != "" || result.SessionID != "session-pb" || !result.WarmUsage {
			t.Fatalf("result = %#v", result)
		}
	})

	t.Run("acpx_success", func(t *testing.T) {
		pbInstallFakeACPX(t)
		sink := &recordingLLMAgentEventSink{}
		result, err := NewACPXAgent("").Run(context.Background(), pbACPXRequest(t, "implement the requested change", sink))
		if err != nil || result.ExitCode != 0 {
			t.Fatalf("Run = (%d, %v)", result.ExitCode, err)
		}
		events := sink.Events()
		want := []LLMAgentEventKind{LLMAgentEventStarted, LLMAgentEventMessage, LLMAgentEventUsage, LLMAgentEventCompleted}
		if got := pbKinds(events); !slices.Equal(got, want) {
			t.Fatalf("kinds = %v, want %v", got, want)
		}
		started := pbEventOf(t, events, LLMAgentEventStarted)
		if started.Payload["mode"] != "acpx" || started.Payload["acpx_agent"] != "codex" {
			t.Fatalf("started payload = %#v", started.Payload)
		}
		session := brand.BinaryName + "-coder-1-task-pb"
		for _, event := range events {
			if event.SessionID != session || event.TaskID != "task-pb" || event.BackendName != "codex-acp" {
				t.Fatalf("event attribution = %#v", event)
			}
		}
		if usage, ok := pbEventOf(t, events, LLMAgentEventUsage).Payload["usage"].(LLMAgentUsage); !ok || usage.InputTokens != 123 {
			t.Fatalf("usage payload = %#v", pbEventOf(t, events, LLMAgentEventUsage).Payload)
		}
		if completed := pbEventOf(t, events, LLMAgentEventCompleted); completed.Payload["exit_code"] != 0 {
			t.Fatalf("completed = %#v", completed)
		}
	})

	t.Run("acpx_prompt_failure", func(t *testing.T) {
		pbInstallFakeACPX(t)
		sink := &recordingLLMAgentEventSink{}
		_, err := NewACPXAgent("").Run(context.Background(), pbACPXRequest(t, "unrelated failure", sink))
		if err == nil {
			t.Fatal("Run error = nil")
		}
		want := []LLMAgentEventKind{LLMAgentEventStarted, LLMAgentEventUsage, LLMAgentEventCompleted}
		if got := pbKinds(sink.Events()); !slices.Equal(got, want) {
			t.Fatalf("kinds = %v, want %v", got, want)
		}
		completed := pbEventOf(t, sink.Events(), LLMAgentEventCompleted)
		if completed.Message != err.Error() || completed.Payload["error"] != err.Error() {
			t.Fatalf("completed = %#v, want the masked error", completed)
		}
	})

	t.Run("acpx_setup_failure_emits_no_started_or_usage", func(t *testing.T) {
		pbInstallFakeACPX(t)
		t.Setenv("FAKE_ACPX_ENSURE_PROVIDER_UNAVAILABLE", "1")
		sink := &recordingLLMAgentEventSink{}
		result, err := NewACPXAgent("").Run(context.Background(), pbACPXRequest(t, "implement the requested change", sink))
		if err == nil {
			t.Fatal("Run error = nil")
		}
		if got := pbKinds(sink.Events()); !slices.Equal(got, []LLMAgentEventKind{LLMAgentEventCompleted}) {
			t.Fatalf("kinds = %v, want only completed", got)
		}
		completed := sink.Events()[0]
		if completed.Message == "" || completed.Message != completed.Payload["error"] || result.Output != completed.Message || result.ExitCode != 1 {
			t.Fatalf("completed = %#v result = %#v", completed, result)
		}
	})

	t.Run("acpx_rejected_gate", func(t *testing.T) {
		logPath := pbInstallFakeACPX(t)
		rejected := errors.New("launch rejected")
		sink := &recordingLLMAgentEventSink{}
		req := pbACPXRequest(t, "implement the requested change", sink)
		req.LaunchGate = func(context.Context, func() error) error { return rejected }
		result, err := NewACPXAgent("").Run(context.Background(), req)
		if !errors.Is(err, rejected) || result.ExitCode != 1 || result.Output != rejected.Error() {
			t.Fatalf("Run = (%#v, %v)", result, err)
		}
		if got := pbKinds(sink.Events()); !slices.Equal(got, []LLMAgentEventKind{LLMAgentEventCompleted}) {
			t.Fatalf("kinds = %v, want only completed", got)
		}
		if _, statErr := os.Stat(logPath); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("a rejected gate ran acpx: %v", statErr)
		}
	})
}

func TestA9BackendPB2GateBoundaries(t *testing.T) {
	t.Run("cli_start_inside_gate_wait_outside", func(t *testing.T) {
		release := filepath.Join(t.TempDir(), "release")
		pbInstallStub(t, "gemini", "#!/bin/sh\nwhile [ ! -f \""+filepath.ToSlash(release)+"\" ]; do sleep 0.05; done\nprintf 'released\\n'\n")
		sink := &recordingLLMAgentEventSink{}
		req := pbCLIRequest(t, sink)
		gateReturned := make(chan struct{})
		req.LaunchGate = func(ctx context.Context, start func() error) error {
			err := start()
			close(gateReturned)
			return err
		}
		done := make(chan error, 1)
		go func() { _, err := NewCLIAgent("").Run(context.Background(), req); done <- err }()
		select {
		case <-gateReturned:
		case <-time.After(10 * time.Second):
			t.Fatal("the gate never returned while the provider was still running")
		}
		select {
		case err := <-done:
			t.Fatalf("Run finished before the provider was released: %v", err)
		case <-time.After(100 * time.Millisecond):
		}
		if err := os.WriteFile(release, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Run error = %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("Run did not finish after release")
		}
		if got := pbKinds(sink.Events()); got[0] != LLMAgentEventStarted {
			t.Fatalf("kinds = %v, want started first", got)
		}
	})

	t.Run("cli_rejected_gate_starts_nothing", func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), "started")
		pbInstallStub(t, "gemini", "#!/bin/sh\nprintf started > \""+filepath.ToSlash(marker)+"\"\n")
		sink := &recordingLLMAgentEventSink{}
		req := pbCLIRequest(t, sink)
		req.LaunchGate = func(context.Context, func() error) error { return errors.New("refused") }
		if _, err := NewCLIAgent("").Run(context.Background(), req); err == nil {
			t.Fatal("Run error = nil")
		}
		if hasLLMAgentEvent(sink.Events(), LLMAgentEventStarted) {
			t.Fatal("a rejected gate emitted started")
		}
		if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("a rejected gate started the process: %v", err)
		}
	})

	t.Run("acpx_session_and_prompt_start_inside_gate_wait_outside", func(t *testing.T) {
		logPath := filepath.Join(t.TempDir(), "acpx.log")
		release := filepath.Join(t.TempDir(), "release")
		pbInstallStub(t, "acpx", blockingACPXScript(logPath, release))
		sink := &recordingLLMAgentEventSink{}
		req := pbACPXRequest(t, "hold", sink)
		var insideGate string
		gateReturned := make(chan struct{})
		req.LaunchGate = func(ctx context.Context, start func() error) error {
			err := start()
			// Only a prompt process started inside the gate can appear before it returns.
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				data, _ := os.ReadFile(logPath)
				if strings.Contains(string(data), " prompt ") {
					break
				}
				<-time.After(20 * time.Millisecond)
			}
			data, _ := os.ReadFile(logPath)
			insideGate = string(data)
			close(gateReturned)
			return err
		}
		done := make(chan error, 1)
		go func() { _, err := NewACPXAgent("").Run(context.Background(), req); done <- err }()
		select {
		case <-gateReturned:
		case <-time.After(10 * time.Second):
			t.Fatal("the gate never returned")
		}
		if !strings.Contains(insideGate, " sessions ensure ") || !strings.Contains(insideGate, " prompt ") {
			t.Fatalf("session ensure and prompt start did not run inside the gate:\n%s", insideGate)
		}
		select {
		case err := <-done:
			t.Fatalf("Run finished before the prompt was released: %v", err)
		case <-time.After(100 * time.Millisecond):
		}
		if err := os.WriteFile(release, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Run error = %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("Run did not finish after release")
		}
	})
}

func TestA9BackendPB3MaskBoundaries(t *testing.T) {
	t.Run("cli_result_and_output_files", func(t *testing.T) {
		t.Setenv("ANTHROPIC_API_KEY", pbSecret)
		pbInstallStub(t, "gemini", "#!/bin/sh\nprintf 'out %s\\n' \"$ANTHROPIC_API_KEY\"\nprintf 'err %s\\n' \"$ANTHROPIC_API_KEY\" >&2\n")
		outputs := t.TempDir()
		result, err := NewCLIAgent(outputs).Run(context.Background(), pbCLIRequest(t, nil))
		if err != nil || result.ExitCode != 0 {
			t.Fatalf("Run = (%d, %v)", result.ExitCode, err)
		}
		if strings.Contains(result.Output, pbSecret) || !strings.Contains(result.Output, "***") {
			t.Fatalf("result output = %q, want the secret masked", result.Output)
		}
		for _, ext := range []string{"txt", "err"} {
			data := readSingleGlobForTest(t, filepath.Join(outputs, "coder-1-*."+ext))
			if strings.Contains(data, pbSecret) || !strings.Contains(data, "***") {
				t.Fatalf("%s log = %q, want the secret masked", ext, data)
			}
		}
	})

	t.Run("cli_without_outputs_dir_has_no_masker", func(t *testing.T) {
		if NewCLIAgent("").masker != nil || NewCLIAgent(t.TempDir()).masker == nil {
			t.Fatal("the CLI masker must exist exactly when outputs are persisted")
		}
	})

	t.Run("acpx_output_file_result_and_events", func(t *testing.T) {
		t.Setenv("ANTHROPIC_API_KEY", pbACPXSecret)
		pbInstallFakeACPX(t)
		outputs := t.TempDir()
		sink := &recordingLLMAgentEventSink{}
		result, err := NewACPXAgent(outputs).Run(context.Background(), pbACPXRequest(t, "emit secret", sink))
		if err != nil {
			t.Fatalf("Run error = %v", err)
		}
		if strings.Contains(result.Output, pbACPXSecret) {
			t.Fatalf("result output leaked the secret: %q", result.Output)
		}
		for _, event := range sink.Events() {
			if strings.Contains(event.Message, pbACPXSecret) {
				t.Fatalf("event leaked the secret: %#v", event)
			}
		}
		data := readSingleGlobForTest(t, filepath.Join(outputs, "coder-1-*.txt"))
		if strings.Contains(data, pbACPXSecret) || !strings.Contains(data, "***") {
			t.Fatalf("stdout log = %q, want the secret masked", data)
		}
	})
}

func TestA9BackendPB4CompletionMapping(t *testing.T) {
	cliCases := []struct {
		name       string
		script     string
		wantExit   int
		wantErr    bool
		wantOutput string
	}{
		{name: "success", script: "#!/bin/sh\nprintf 'done\\n'\n", wantOutput: "done"},
		{name: "non_zero_exit", script: "#!/bin/sh\nprintf 'bad\\n'\nexit 7\n", wantExit: 7, wantOutput: "bad"},
	}
	for _, tc := range cliCases {
		t.Run("cli_"+tc.name, func(t *testing.T) {
			pbInstallStub(t, "gemini", tc.script)
			sink := &recordingLLMAgentEventSink{}
			result, err := NewCLIAgent("").Run(context.Background(), pbCLIRequest(t, sink))
			if (err != nil) != tc.wantErr || result.ExitCode != tc.wantExit || !strings.Contains(result.Output, tc.wantOutput) {
				t.Fatalf("Run = (%#v, %v)", result, err)
			}
			if result.Usage != (LLMAgentUsage{}) || !result.WarmUsage || result.SessionID != "session-pb" {
				t.Fatalf("result shape = %#v", result)
			}
			if got := countLLMAgentEvents(sink.Events(), LLMAgentEventCompleted); got != 1 {
				t.Fatalf("completed events = %d, want one", got)
			}
		})
	}

	t.Run("cli_quota_exit_zero", func(t *testing.T) {
		pbInstallStub(t, "codex", "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 'codex-cli 0.154.0'; exit 0; fi\nprintf '%s\\n' '{\"type\":\"turn.failed\",\"error\":{\"message\":\"You'\"'\"'ve hit your usage limit.\"}}'\n")
		sink := &recordingLLMAgentEventSink{}
		req := pbCLIRequest(t, sink)
		req.BackendName = "codex"
		result, err := NewCLIAgent("").Run(context.Background(), req)
		if err != nil || result.ExitCode != 1 || !strings.Contains(result.Output, "usage limit") {
			t.Fatalf("Run = (%#v, %v), want a failed quota turn", result, err)
		}
		completed := pbEventOf(t, sink.Events(), LLMAgentEventCompleted)
		if completed.Payload["quota"] != true || completed.Payload["exit_code"] != 1 || completed.Message == "" {
			t.Fatalf("completed = %#v", completed)
		}
	})

	t.Run("cli_cancellation", func(t *testing.T) {
		pbInstallStub(t, "gemini", "#!/bin/sh\nsleep 30\n")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		started := make(chan struct{})
		sink := LLMAgentEventFunc(func(_ context.Context, event LLMAgentEvent) {
			if event.Kind == LLMAgentEventStarted {
				close(started)
			}
		})
		type outcome struct {
			result LLMAgentRunResult
			err    error
		}
		done := make(chan outcome, 1)
		go func() {
			result, err := NewCLIAgent("").Run(ctx, pbCLIRequest(t, sink))
			done <- outcome{result, err}
		}()
		<-started
		cancel()
		select {
		case got := <-done:
			if got.err == nil && got.result.ExitCode == 0 {
				t.Fatalf("a cancelled provider reported success: %#v", got.result)
			}
			if got.result.SessionID != "session-pb" || !got.result.WarmUsage {
				t.Fatalf("result shape = %#v", got.result)
			}
		case <-time.After(20 * time.Second):
			t.Fatal("cancellation did not end the run")
		}
	})

	t.Run("acpx_failure_and_quota_shapes", func(t *testing.T) {
		pbInstallFakeACPX(t)
		session := brand.BinaryName + "-coder-1-task-pb"

		failed, err := NewACPXAgent("").Run(context.Background(), pbACPXRequest(t, "unrelated failure", nil))
		if err == nil || failed.ExitCode != 1 || failed.Output != "" || failed.SessionID != session || failed.WarmUsage {
			t.Fatalf("prompt failure = (%#v, %v)", failed, err)
		}

		quota, err := NewACPXAgent("").Run(context.Background(), pbACPXRequest(t, "codex quota", nil))
		if err != nil || quota.ExitCode != 1 || !strings.Contains(quota.Output, "usage limit") || quota.SessionID != session || quota.WarmUsage {
			t.Fatalf("quota = (%#v, %v)", quota, err)
		}

		sink := &recordingLLMAgentEventSink{}
		_, err = NewACPXAgent("").Run(context.Background(), pbACPXRequest(t, "codex quota", sink))
		if err != nil {
			t.Fatal(err)
		}
		completed := pbEventOf(t, sink.Events(), LLMAgentEventCompleted)
		if completed.Payload["quota"] != true || completed.Payload["exit_code"] != 1 || completed.Message == "" {
			t.Fatalf("completed = %#v", completed)
		}
	})

	t.Run("acpx_cancellation", func(t *testing.T) {
		logPath := filepath.Join(t.TempDir(), "acpx.log")
		pbInstallStub(t, "acpx", blockingACPXScript(logPath, filepath.Join(t.TempDir(), "never")))
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		started := make(chan struct{})
		sink := LLMAgentEventFunc(func(_ context.Context, event LLMAgentEvent) {
			if event.Kind == LLMAgentEventStarted {
				close(started)
			}
		})
		done := make(chan error, 1)
		go func() {
			_, err := NewACPXAgent("").Run(ctx, pbACPXRequest(t, "hold", sink))
			done <- err
		}()
		<-started
		cancel()
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("a cancelled ACPX prompt reported success")
			}
		case <-time.After(20 * time.Second):
			t.Fatal("cancellation did not end the run")
		}
	})
}

func pbFileTransportConfig(t *testing.T, name string, transport providers.PromptTransport, args ...string) models.Config {
	t.Helper()
	exe := filepath.Join(t.TempDir(), name)
	testhelpers.WriteShellStub(t, exe, "#!/bin/sh\nfor a in \"$@\"; do printf 'ARG:%s\\n' \"$a\"; done\ncat\n")
	return models.Config{AgentTools: map[string]models.AgentToolConfig{
		name: {Backend: "cli", Executable: exe, PromptTransport: string(transport), RunArgs: args},
	}}
}

func pbPromptFiles(t *testing.T, dir string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, brand.RuntimeValues().BinaryName+"-agent-prompt-*.md"))
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

func TestA9BackendPB5CommandPreparation(t *testing.T) {
	t.Run("file_transport_writes_and_removes_prompt_file", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("TMPDIR", tmp)
		req := pbCLIRequest(t, nil)
		req.BackendName = "pb-file-cli"
		req.RuntimeConfig = pbFileTransportConfig(t, "pb-file-cli", providers.PromptTransportFile, "--prompt-file", "{{promptFile}}")
		cmd, cleanup, err := NewCLIAgent("").buildRunCommand(context.Background(), req)
		if err != nil {
			t.Fatalf("buildRunCommand error = %v", err)
		}
		files := pbPromptFiles(t, tmp)
		if len(files) != 1 || len(cmd.Args) != 3 || cmd.Args[1] != "--prompt-file" || cmd.Args[2] != files[0] {
			t.Fatalf("args = %v, prompt files = %v", cmd.Args, files)
		}
		if data, readErr := os.ReadFile(files[0]); readErr != nil || string(data) != "pb prompt" {
			t.Fatalf("prompt file = %q, %v", data, readErr)
		}
		if cmd.Stdin != nil || cmd.Dir != req.ProjectRoot {
			t.Fatalf("stdin = %v dir = %q", cmd.Stdin, cmd.Dir)
		}
		cleanup()
		if left := pbPromptFiles(t, tmp); len(left) != 0 {
			t.Fatalf("cleanup left %v", left)
		}
	})

	t.Run("stdin_transport_writes_no_file", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("TMPDIR", tmp)
		req := pbCLIRequest(t, nil)
		req.BackendName = "pb-stdin-cli"
		req.RuntimeConfig = pbFileTransportConfig(t, "pb-stdin-cli", providers.PromptTransportStdin, "--run")
		cmd, cleanup, err := NewCLIAgent("").buildRunCommand(context.Background(), req)
		if err != nil {
			t.Fatalf("buildRunCommand error = %v", err)
		}
		defer cleanup()
		if cmd.Stdin == nil {
			t.Fatal("stdin transport left Stdin unset")
		}
		if data, _ := io.ReadAll(cmd.Stdin); string(data) != "pb prompt" {
			t.Fatalf("stdin = %q", data)
		}
		if files := pbPromptFiles(t, tmp); len(files) != 0 {
			t.Fatalf("stdin transport wrote %v", files)
		}
	})

	t.Run("supplied_prompt_file_is_used_and_kept", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("TMPDIR", tmp)
		supplied := filepath.Join(t.TempDir(), "supplied.md")
		if err := os.WriteFile(supplied, []byte("saved prompt"), 0o600); err != nil {
			t.Fatal(err)
		}
		req := pbCLIRequest(t, nil)
		req.BackendName = "pb-file-cli"
		req.PromptFile = supplied
		req.RuntimeConfig = pbFileTransportConfig(t, "pb-file-cli", providers.PromptTransportFile, "--prompt-file", "{{promptFile}}")
		cmd, cleanup, err := NewCLIAgent("").buildRunCommand(context.Background(), req)
		if err != nil {
			t.Fatalf("buildRunCommand error = %v", err)
		}
		cleanup()
		if cmd.Args[2] != supplied {
			t.Fatalf("args = %v, want the supplied prompt file", cmd.Args)
		}
		if _, statErr := os.Stat(supplied); statErr != nil {
			t.Fatalf("cleanup removed the caller's prompt file: %v", statErr)
		}
		if files := pbPromptFiles(t, tmp); len(files) != 0 {
			t.Fatalf("a supplied prompt file still wrote %v", files)
		}
	})

	t.Run("acpx_backend_is_not_a_cli_backend", func(t *testing.T) {
		req := pbCLIRequest(t, nil)
		req.BackendName = "codex-acp"
		_, cleanup, err := NewCLIAgent("").buildRunCommand(context.Background(), req)
		if err == nil || cleanup != nil || err.Error() != "codex-acp is not a CLI backend" {
			t.Fatalf("buildRunCommand = (%v, cleanup nil=%v)", err, cleanup == nil)
		}
	})

	t.Run("codex_package_version_with_whitespace_is_refused", func(t *testing.T) {
		if _, err := codexCommandContext(context.Background(), "1 2", nil); err == nil {
			t.Fatal("whitespace in the codex package version must be refused")
		}
	})
}

func TestA9BackendPB6CleanupOwnership(t *testing.T) {
	t.Run("prompt_file_removed_after_run_and_after_refused_gate", func(t *testing.T) {
		for _, refuse := range []bool{false, true} {
			t.Run(fmt.Sprintf("refused=%v", refuse), func(t *testing.T) {
				tmp := t.TempDir()
				t.Setenv("TMPDIR", tmp)
				sink := &recordingLLMAgentEventSink{}
				req := pbCLIRequest(t, sink)
				req.BackendName = "pb-file-cli"
				req.RuntimeConfig = pbFileTransportConfig(t, "pb-file-cli", providers.PromptTransportFile, "--prompt-file", "{{promptFile}}")
				if refuse {
					req.LaunchGate = func(context.Context, func() error) error { return errors.New("refused") }
				}
				_, err := NewCLIAgent("").Run(context.Background(), req)
				if (err != nil) != refuse {
					t.Fatalf("Run error = %v, refused = %v", err, refuse)
				}
				if files := pbPromptFiles(t, tmp); len(files) != 0 {
					t.Fatalf("Run left prompt files %v", files)
				}
			})
		}
	})

	t.Run("output_logs_are_flushed_when_run_returns", func(t *testing.T) {
		pbInstallStub(t, "gemini", "#!/bin/sh\nprintf 'logged out\\n'\nprintf 'logged err\\n' >&2\n")
		outputs := t.TempDir()
		if _, err := NewCLIAgent(outputs).Run(context.Background(), pbCLIRequest(t, nil)); err != nil {
			t.Fatal(err)
		}
		if got := readSingleGlobForTest(t, filepath.Join(outputs, "coder-1-*.txt")); !strings.Contains(got, "logged out") {
			t.Fatalf("stdout log = %q", got)
		}
		if got := readSingleGlobForTest(t, filepath.Join(outputs, "coder-1-*.err")); !strings.Contains(got, "logged err") {
			t.Fatalf("stderr log = %q", got)
		}
	})

	t.Run("acpx_logs_are_flushed_when_run_returns", func(t *testing.T) {
		pbInstallFakeACPX(t)
		outputs := t.TempDir()
		if _, err := NewACPXAgent(outputs).Run(context.Background(), pbACPXRequest(t, "stream output", nil)); err != nil {
			t.Fatal(err)
		}
		if got := readSingleGlobForTest(t, filepath.Join(outputs, "coder-1-*.txt")); !strings.Contains(got, "first streamed chunk") {
			t.Fatalf("stdout log = %q", got)
		}
		if got := readSingleGlobForTest(t, filepath.Join(outputs, "coder-1-*.err")); !strings.Contains(got, "streamed stderr diagnostic") {
			t.Fatalf("stderr log = %q", got)
		}
	})

	t.Run("cli_interactive_refuses_acpx_backend", func(t *testing.T) {
		pbInstallStub(t, "acpx", "#!/bin/sh\nexit 0\n")
		code, err := NewCLIAgent("").RunInteractive(context.Background(), LLMAgentInteractiveRequest{
			BackendName: "codex-acp", AgentID: "coder-1", ProjectRoot: t.TempDir(), LaunchGate: immediateLaunchGate,
		})
		if code != 1 || err == nil || err.Error() != "interactive mode is not supported by codex-acp" {
			t.Fatalf("RunInteractive = (%d, %v)", code, err)
		}
	})
}
