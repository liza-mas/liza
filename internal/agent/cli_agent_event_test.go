package agent

import (
	"context"
	"testing"
	"testing/synctest"
)

func TestCLIAgentOutputEventsWaitForStarted(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ready := make(chan struct{})
		sink := &recordingLLMAgentEventSink{}
		base := LLMAgentEvent{BackendName: "claude", AgentID: "coder-1", TaskID: "task-123"}
		for _, stream := range []string{"stdout", "stderr"} {
			go func() {
				writer := llmAgentEventWriter{
					ctx: context.Background(), sink: sink, base: base,
					stream: stream, ready: ready,
				}
				data := []byte(stream + " event\n")
				if n, err := writer.Write(data); err != nil || n != len(data) {
					t.Errorf("%s Write = %d, %v; want %d bytes", stream, n, err, len(data))
				}
			}()
		}
		// Both writes have reached their blocking boundary. This assertion
		// cannot pass merely because the output goroutines have not run yet.
		synctest.Wait()
		if events := sink.Events(); len(events) != 0 {
			t.Errorf("output arrived before Started completed: %+v", events)
		}
		started := base
		started.Kind = LLMAgentEventStarted
		emitLLMAgentEvent(context.Background(), sink, started)
		close(ready)
		synctest.Wait()

		events := sink.Events()
		if len(events) != 3 || events[0].Kind != LLMAgentEventStarted {
			t.Fatalf("events = %+v, want Started followed by both output streams", events)
		}
		seen := map[string]int{}
		for _, event := range events[1:] {
			stream, ok := event.Payload["stream"].(string)
			if !ok || (stream != "stdout" && stream != "stderr") || event.Kind != LLMAgentEventOutputChunk ||
				event.Message != stream+" event\n" || event.Payload["bytes"] != len(event.Message) {
				t.Fatalf("unexpected output event: %+v", event)
			}
			seen[stream]++
		}
		if seen["stdout"] != 1 || seen["stderr"] != 1 {
			t.Fatalf("output stream counts = %+v, want one event each", seen)
		}
	})
}
