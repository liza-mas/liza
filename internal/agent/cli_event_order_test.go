package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestCLIAgentStartedPrecedesOutputWhileLaunchGateReturns(t *testing.T) {
	root, bin := t.TempDir(), t.TempDir()
	testhelpers.WriteShellStub(t, filepath.Join(bin, "gemini"), "#!/bin/sh\nprintf 'immediate stdout\\n'\nprintf 'immediate stderr\\n' >&2\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	copyStarted, outputPublished := make(chan struct{}), make(chan struct{})
	var copyOnce, outputOnce sync.Once
	ctx = withExecutionProgressCallback(ctx, func() { copyOnce.Do(func() { close(copyStarted) }) })
	recorded := &recordingLLMAgentEventSink{}
	sink := LLMAgentEventFunc(func(ctx context.Context, event LLMAgentEvent) {
		recorded.RecordLLMAgentEvent(ctx, event)
		if event.Kind == LLMAgentEventOutputChunk {
			outputOnce.Do(func() { close(outputPublished) })
		}
	})
	gate := LLMAgentLaunchGate(func(ctx context.Context, start func() error) error {
		if err := start(); err != nil {
			return err
		}
		// Force the output-copy goroutine to run before Run can emit Started.
		select {
		case <-copyStarted:
		case <-ctx.Done():
			return ctx.Err()
		}
		// The old implementation publishes output here. A correctly ordered
		// writer waits until this gate returns and Started has been emitted.
		select {
		case <-outputPublished:
		case <-time.After(200 * time.Millisecond):
		}
		return nil
	})
	result, err := NewCLIAgent("").Run(ctx, LLMAgentRunRequest{
		BackendName: "gemini", AgentID: "coder-1", ProjectRoot: root,
		Prompt: "test prompt", EventSink: sink, LaunchGate: gate,
	})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("Run = (%d, %v)", result.ExitCode, err)
	}
	if !strings.Contains(result.Output, "immediate stdout") || !strings.Contains(result.Output, "immediate stderr") {
		t.Fatalf("output lost while ordering events: %q", result.Output)
	}
	events := recorded.Events()
	if len(events) < 4 || events[0].Kind != LLMAgentEventStarted || events[len(events)-1].Kind != LLMAgentEventCompleted {
		t.Fatalf("events are not ordered start/output/completed: %#v", events)
	}
}

func TestCLIAgentRejectedLaunchDoesNotEmitStarted(t *testing.T) {
	root, bin := t.TempDir(), t.TempDir()
	marker := filepath.Join(root, "started")
	testhelpers.WriteShellStub(t, filepath.Join(bin, "gemini"), fmt.Sprintf("#!/bin/sh\nprintf started > %q\n", filepath.ToSlash(marker)))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	rejected := errors.New("launch rejected")
	sink := &recordingLLMAgentEventSink{}
	_, err := NewCLIAgent("").Run(context.Background(), LLMAgentRunRequest{
		BackendName: "gemini", AgentID: "coder-1", ProjectRoot: root, Prompt: "test prompt", EventSink: sink,
		LaunchGate: func(context.Context, func() error) error { return rejected },
	})
	if !errors.Is(err, rejected) {
		t.Fatalf("Run error = %v, want rejected launch", err)
	}
	events := sink.Events()
	if len(events) != 1 || events[0].Kind != LLMAgentEventCompleted {
		t.Fatalf("rejected launch emitted successful-start or output events: %#v", events)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected launch started the process: %v", err)
	}
}
