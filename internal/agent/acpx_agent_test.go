package agent

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
)

func TestACPXAgentRunUsesPersistentCodexSession(t *testing.T) {
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "acpx.log")
	writeFakeACPX(t, filepath.Join(binDir, "acpx"), logPath)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var events []LLMAgentEvent
	sink := LLMAgentEventFunc(func(_ context.Context, event LLMAgentEvent) {
		events = append(events, event)
	})

	agent := NewACPXAgent("")
	req := LLMAgentRunRequest{
		BackendName: "codex-acp",
		AgentID:     "coder-1",
		TaskID:      "task-acp",
		Prompt:      "implement the requested change",
		ProjectRoot: t.TempDir(),
		EventSink:   sink,
	}

	first, err := agent.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
	if first.ExitCode != 0 {
		t.Fatalf("first ExitCode = %d, want 0", first.ExitCode)
	}
	if first.Output != "done from acpx" {
		t.Fatalf("first Output = %q, want fake acpx message", first.Output)
	}
	if first.SessionID != "liza-coder-1" {
		t.Fatalf("first SessionID = %q, want liza-coder-1", first.SessionID)
	}
	if first.WarmUsage {
		t.Fatal("first WarmUsage = true, want false")
	}
	wantUsage := LLMAgentUsage{InputTokens: 123, OutputTokens: 7, CachedReadTokens: 42}
	if first.Usage != wantUsage {
		t.Fatalf("first Usage = %+v, want %+v", first.Usage, wantUsage)
	}

	second, err := agent.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	if !second.WarmUsage {
		t.Fatal("second WarmUsage = false, want true")
	}

	log := readTextForTest(t, logPath)
	for _, want := range []string{
		"ENV_LIZA_AGENT_ID:coder-1",
		"ARGS:--cwd " + req.ProjectRoot + " codex sessions ensure --name liza-coder-1",
		"ARGS:--cwd " + req.ProjectRoot + " --format json --approve-all codex prompt -s liza-coder-1 --file -",
		"STDIN:implement the requested change",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("fake acpx log missing %q:\n%s", want, log)
		}
	}

	if !hasLLMAgentEvent(events, LLMAgentEventStarted) {
		t.Fatal("missing started event")
	}
	if !allLLMAgentEventsHaveTask(events, "task-acp") {
		t.Fatalf("events = %#v, want task-acp attribution", events)
	}
	if !hasLLMAgentEvent(events, LLMAgentEventMessage) {
		t.Fatal("missing message event")
	}
	if !hasLLMAgentEvent(events, LLMAgentEventUsage) {
		t.Fatal("missing usage event")
	}
	if !hasLLMAgentEvent(events, LLMAgentEventCompleted) {
		t.Fatal("missing completed event")
	}
}

func TestACPXAgentRunUsesOpenCodeTarget(t *testing.T) {
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "acpx.log")
	writeFakeACPX(t, filepath.Join(binDir, "acpx"), logPath)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	req := LLMAgentRunRequest{
		BackendName: "opencode-acp",
		AgentID:     "coder-1",
		Prompt:      "implement the requested change",
		ProjectRoot: t.TempDir(),
	}

	result, err := NewACPXAgent("").Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", result.ExitCode)
	}

	log := readTextForTest(t, logPath)
	for _, want := range []string{
		"ARGS:--cwd " + req.ProjectRoot + " opencode sessions ensure --name liza-coder-1",
		"ARGS:--cwd " + req.ProjectRoot + " --format json --approve-all opencode prompt -s liza-coder-1 --file -",
		"STDIN:implement the requested change",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("fake acpx log missing %q:\n%s", want, log)
		}
	}
}

func TestACPXSessionNameUsesBrandedBinaryName(t *testing.T) {
	withAgentBrandValues(t, func() {
		brand.BinaryName = "acme"
	})

	if got := acpxSessionName("coder-1"); got != "acme-coder-1" {
		t.Fatalf("acpxSessionName() = %q, want acme-coder-1", got)
	}
	if got := acpxSessionName(""); got != "acme-agent" {
		t.Fatalf("acpxSessionName(empty) = %q, want acme-agent", got)
	}
}

func TestACPXAgentRunUsesCursorTarget(t *testing.T) {
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "acpx.log")
	writeFakeACPX(t, filepath.Join(binDir, "acpx"), logPath)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	req := LLMAgentRunRequest{
		BackendName: "cursor-acp",
		AgentID:     "coder-1",
		Prompt:      "implement the requested change",
		ProjectRoot: t.TempDir(),
	}

	result, err := NewACPXAgent("").Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", result.ExitCode)
	}

	log := readTextForTest(t, logPath)
	for _, want := range []string{
		"ARGS:--cwd " + req.ProjectRoot + " cursor sessions ensure --name liza-coder-1",
		"ARGS:--cwd " + req.ProjectRoot + " --format json --approve-all cursor prompt -s liza-coder-1 --file -",
		"STDIN:implement the requested change",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("fake acpx log missing %q:\n%s", want, log)
		}
	}
}

func TestACPXAgentMasksReturnedOutputAndEvents(t *testing.T) {
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "acpx.log")
	writeFakeACPX(t, filepath.Join(binDir, "acpx"), logPath)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ANTHROPIC_API_KEY", "sk-acpx-secret")

	var events []LLMAgentEvent
	sink := LLMAgentEventFunc(func(_ context.Context, event LLMAgentEvent) {
		events = append(events, event)
	})

	result, err := NewACPXAgent("").Run(context.Background(), LLMAgentRunRequest{
		BackendName: "codex-acp",
		AgentID:     "coder-1",
		TaskID:      "task-acp",
		Prompt:      "emit secret",
		ProjectRoot: t.TempDir(),
		EventSink:   sink,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if strings.Contains(result.Output, "sk-acpx-secret") {
		t.Fatalf("Output leaked secret: %q", result.Output)
	}
	if !strings.Contains(result.Output, "***") {
		t.Fatalf("Output = %q, want masked secret placeholder", result.Output)
	}
	for _, event := range events {
		if strings.Contains(event.Message, "sk-acpx-secret") {
			t.Fatalf("event leaked secret: %#v", event)
		}
	}
}

func TestACPXAgentMasksReturnedErrors(t *testing.T) {
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "acpx.log")
	writeFakeACPX(t, filepath.Join(binDir, "acpx"), logPath)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ANTHROPIC_API_KEY", "sk-acpx-secret")

	result, err := NewACPXAgent("").Run(context.Background(), LLMAgentRunRequest{
		BackendName: "codex-acp",
		AgentID:     "coder-1",
		TaskID:      "task-acp",
		Prompt:      "fail secret",
		ProjectRoot: t.TempDir(),
	})
	if err == nil {
		t.Fatal("Run() error = nil, want ACPX failure")
	}
	if strings.Contains(err.Error(), "sk-acpx-secret") {
		t.Fatalf("error leaked secret: %v", err)
	}
	if strings.Contains(result.Output, "sk-acpx-secret") {
		t.Fatalf("Output leaked secret: %q", result.Output)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("errors.As(*exec.ExitError) = false for %T: %v", err, err)
	}
}

func TestACPXAgentTreatsQuotaMessageAsFailure(t *testing.T) {
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "acpx.log")
	writeFakeACPX(t, filepath.Join(binDir, "acpx"), logPath)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	result, err := NewACPXAgent("").Run(context.Background(), LLMAgentRunRequest{
		BackendName: "cursor-acp",
		AgentID:     "coder-1",
		TaskID:      "task-acp",
		Prompt:      "cursor quota",
		ProjectRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.ExitCode == 0 {
		t.Fatalf("ExitCode = 0, want non-zero for quota message; output=%q", result.Output)
	}
	if !strings.Contains(result.Output, "Upgrade your plan to continue") {
		t.Fatalf("Output = %q, want Cursor quota message", result.Output)
	}
}

func TestACPXAgentDetectsQuotaWithUnderlyingAgentName(t *testing.T) {
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "acpx.log")
	writeFakeACPX(t, filepath.Join(binDir, "acpx"), logPath)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	result, err := NewACPXAgent("").Run(context.Background(), LLMAgentRunRequest{
		BackendName: "codex-acp",
		AgentID:     "coder-1",
		TaskID:      "task-acp",
		Prompt:      "codex quota",
		ProjectRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.ExitCode == 0 {
		t.Fatalf("ExitCode = 0, want non-zero for quota message; output=%q", result.Output)
	}
	if !strings.Contains(result.Output, "You've hit your usage limit") {
		t.Fatalf("Output = %q, want Codex quota message", result.Output)
	}
}

func TestACPXAgentRunInteractiveDelegatesToUnderlyingCLI(t *testing.T) {
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "cursor-agent.log")
	writeFakeExecutable(t, filepath.Join(binDir, "cursor-agent"), logPath)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	exitCode, err := NewACPXAgent("").RunInteractive(context.Background(), LLMAgentInteractiveRequest{
		BackendName: "cursor-acp",
		AgentID:     "coder-1",
		ProjectRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("RunInteractive() error = %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", exitCode)
	}

	log := readTextForTest(t, logPath)
	for _, want := range []string{
		"ARGS:",
		"ENV_LIZA_AGENT_ID:coder-1",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("fake cursor-agent log missing %q:\n%s", want, log)
		}
	}
}

func TestACPXAgentDetectsPersistedWarmSession(t *testing.T) {
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "acpx.log")
	writeFakeACPX(t, filepath.Join(binDir, "acpx"), logPath)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_ACPX_SESSION_EXISTS", "1")

	result, err := NewACPXAgent("").Run(context.Background(), LLMAgentRunRequest{
		BackendName: "codex-acp",
		AgentID:     "coder-1",
		TaskID:      "task-acp",
		Prompt:      "implement the requested change",
		ProjectRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.WarmUsage {
		t.Fatal("WarmUsage = false, want true for existing ACPX session")
	}
}

func TestACPXAgentRunStreamsOutputToLogsEventsAndProgress(t *testing.T) {
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "acpx.log")
	writeFakeACPX(t, filepath.Join(binDir, "acpx"), logPath)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	outputsDir := t.TempDir()
	progressCh := make(chan struct{}, 4)
	ctx := withExecutionProgressCallback(context.Background(), func() {
		select {
		case progressCh <- struct{}{}:
		default:
		}
	})

	var mu sync.Mutex
	var events []LLMAgentEvent
	sink := LLMAgentEventFunc(func(_ context.Context, event LLMAgentEvent) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, event)
	})

	type runResult struct {
		result LLMAgentRunResult
		err    error
	}
	done := make(chan runResult, 1)
	go func() {
		result, err := NewACPXAgent(outputsDir).Run(ctx, LLMAgentRunRequest{
			BackendName: "codex-acp",
			AgentID:     "coder-1",
			TaskID:      "task-acp",
			Prompt:      "stream output",
			ProjectRoot: t.TempDir(),
			EventSink:   sink,
		})
		done <- runResult{result: result, err: err}
	}()

	select {
	case <-progressCh:
	case result := <-done:
		t.Fatalf("Run() completed before streaming progress; result=%+v err=%v", result.result, result.err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for streamed ACPX progress")
	}

	run := <-done
	if run.err != nil {
		t.Fatalf("Run() error = %v", run.err)
	}
	if run.result.Output != "first streamed chunksecond streamed chunk" {
		t.Fatalf("Output = %q, want streamed chunks", run.result.Output)
	}
	wantUsage := LLMAgentUsage{InputTokens: 321, OutputTokens: 11, CachedReadTokens: 99}
	if run.result.Usage != wantUsage {
		t.Fatalf("Usage = %+v, want %+v", run.result.Usage, wantUsage)
	}

	stdoutLog := readSingleGlobForTest(t, filepath.Join(outputsDir, "coder-1-*.txt"))
	for _, want := range []string{"first streamed chunk", "second streamed chunk", `"inputTokens":321`} {
		if !strings.Contains(stdoutLog, want) {
			t.Fatalf("stdout log missing %q:\n%s", want, stdoutLog)
		}
	}
	stderrLog := readSingleGlobForTest(t, filepath.Join(outputsDir, "coder-1-*.err"))
	if !strings.Contains(stderrLog, "streamed stderr diagnostic") {
		t.Fatalf("stderr log missing diagnostic:\n%s", stderrLog)
	}

	mu.Lock()
	defer mu.Unlock()
	if countLLMAgentEvents(events, LLMAgentEventMessage) != 2 {
		t.Fatalf("message event count = %d, want 2; events=%#v", countLLMAgentEvents(events, LLMAgentEventMessage), events)
	}
	if !hasLLMAgentEvent(events, LLMAgentEventUsage) {
		t.Fatal("missing usage event")
	}
	if !hasLLMAgentEvent(events, LLMAgentEventCompleted) {
		t.Fatal("missing completed event")
	}
}

func writeFakeACPX(t *testing.T, path, logPath string) {
	t.Helper()
	script := `#!/bin/sh
printf 'ARGS:%s\n' "$*" >> "` + logPath + `"
printf 'ENV_LIZA_AGENT_ID:%s\n' "$LIZA_AGENT_ID" >> "` + logPath + `"
case "$*" in
  *" sessions show "*)
    if [ "$FAKE_ACPX_SESSION_EXISTS" = "1" ]; then
      exit 0
    fi
    exit 2
    ;;
  *" sessions ensure "*)
    exit 0
    ;;
  *" prompt "*)
    prompt="$(cat)"
    printf 'STDIN:%s\n' "$prompt" >> "` + logPath + `"
    if [ "$prompt" = "stream output" ]; then
      printf '%s\n' '{"params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"text":"first streamed chunk"}}}}'
      printf '%s\n' 'streamed stderr diagnostic' >&2
      sleep 0.1
      printf '%s\n' '{"params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"text":"second streamed chunk"}}}}'
      printf '%s\n' '{"result":{"usage":{"inputTokens":321,"outputTokens":11,"cachedReadTokens":99}}}'
    elif [ "$prompt" = "fail secret" ]; then
      printf '%s\n' '{"params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"text":"secret sk-acpx-secret"}}}}'
      printf '%s\n' 'stderr sk-acpx-secret' >&2
      exit 7
    elif [ "$prompt" = "emit secret" ]; then
      printf '%s\n' '{"params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"text":"secret sk-acpx-secret"}}}}'
    elif [ "$prompt" = "cursor quota" ]; then
      printf '%s\n' '{"params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"text":"\n\nUpgrade your plan to continue"}}}}'
    elif [ "$prompt" = "codex quota" ]; then
      printf '%s\n' '{"params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"text":"You'\''ve hit your usage limit. Upgrade to Pro."}}}}'
    else
      printf '%s\n' '{"params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"text":"done from acpx"}}}}'
    fi
    if [ "$prompt" != "stream output" ]; then
      printf '%s\n' '{"result":{"usage":{"inputTokens":123,"outputTokens":7,"cachedReadTokens":42}}}'
    fi
    exit 0
    ;;
  *)
    exit 2
    ;;
esac
`
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake acpx: %v", err)
	}
}

func writeFakeExecutable(t *testing.T, path, logPath string) {
	t.Helper()
	script := `#!/bin/sh
printf 'ARGS:%s\n' "$*" >> "` + logPath + `"
printf 'ENV_LIZA_AGENT_ID:%s\n' "$LIZA_AGENT_ID" >> "` + logPath + `"
exit 0
`
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake executable: %v", err)
	}
}

func readTextForTest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func readSingleGlobForTest(t *testing.T, pattern string) string {
	t.Helper()
	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("glob %s: %v", pattern, err)
	}
	if len(matches) != 1 {
		t.Fatalf("glob %s matched %d files: %v", pattern, len(matches), matches)
	}
	return readTextForTest(t, matches[0])
}

func hasLLMAgentEvent(events []LLMAgentEvent, kind LLMAgentEventKind) bool {
	for _, event := range events {
		if event.Kind == kind {
			return true
		}
	}
	return false
}

func countLLMAgentEvents(events []LLMAgentEvent, kind LLMAgentEventKind) int {
	count := 0
	for _, event := range events {
		if event.Kind == kind {
			count++
		}
	}
	return count
}

func allLLMAgentEventsHaveTask(events []LLMAgentEvent, taskID string) bool {
	for _, event := range events {
		if event.TaskID != taskID {
			return false
		}
	}
	return true
}
