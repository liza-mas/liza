package agent

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

type llmAgentEventWriter struct {
	ctx    context.Context
	sink   LLMAgentEventSink
	base   LLMAgentEvent
	stream string
	ready  <-chan struct{}
}

func (w llmAgentEventWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	// Start activates exec's copy goroutines before its caller can emit Started.
	// Keep output events behind that successful start observation.
	if w.ready != nil {
		<-w.ready
	}
	event := w.base
	event.Kind = LLMAgentEventOutputChunk
	event.Message = string(p)
	event.Payload = map[string]any{
		llmAgentPayloadStream: w.stream,
		llmAgentPayloadBytes:  len(p),
	}
	emitLLMAgentEvent(w.ctx, w.sink, event)
	return len(p), nil
}

// cliRunStreams holds the output sinks of one CLI run.
type cliRunStreams struct {
	stdout, stderr       strings.Builder
	stdoutLog, stderrLog *streamingOutputFile
	// ready releases output events once the run's Started event was emitted.
	ready chan struct{}
}

// attachRunStreams wires the provider's stdout and stderr to the console, the
// returned-output buffers, the persisted logs (when outputs are kept), the
// event sink and the progress watchdog.
func (d *CLIAgent) attachRunStreams(ctx context.Context, cmd *exec.Cmd, req LLMAgentRunRequest, base LLMAgentEvent) *cliRunStreams {
	s := &cliRunStreams{ready: make(chan struct{})}
	events := func(stream string) io.Writer {
		return llmAgentEventWriter{ctx: ctx, sink: req.EventSink, base: base, stream: stream, ready: s.ready}
	}
	stdout := []io.Writer{os.Stdout, &s.stdout}
	stderr := []io.Writer{os.Stderr, &s.stderr}
	var progress []io.Writer
	if mark := executionProgressCallback(ctx); mark != nil {
		progress = []io.Writer{progressWriter{mark: mark}}
	}
	if d.outputsDir != "" {
		s.stdoutLog, s.stderrLog = newAgentOutputLogs(d.outputsDir, req.AgentID, d.masker)
		stdout = append(stdout, s.stdoutLog, events(llmAgentStreamStdout))
		stderr = append(stderr, s.stderrLog, events(llmAgentStreamStderr))
		stdout, stderr = append(stdout, progress...), append(stderr, progress...)
	} else {
		stdout, stderr = append(stdout, progress...), append(stderr, progress...)
		stdout = append(stdout, events(llmAgentStreamStdout))
		stderr = append(stderr, events(llmAgentStreamStderr))
	}
	cmd.Stdout = io.MultiWriter(stdout...)
	cmd.Stderr = io.MultiWriter(stderr...)
	return s
}

// newAgentOutputLogs opens the timestamped stdout and stderr logs of one run.
func newAgentOutputLogs(dir, agentID string, masker *SecretMasker) (stdoutLog, stderrLog *streamingOutputFile) {
	timestamp := time.Now().UTC().Format(agentOutputLogTimestampLayout)
	return newStreamingOutputFile(dir, agentID, agentOutputLogStdoutExt, timestamp, masker),
		newStreamingOutputFile(dir, agentID, agentOutputLogStderrExt, timestamp, masker)
}

func closeAgentOutputLogs(stdoutLog, stderrLog *streamingOutputFile, agentID string) {
	if stdoutLog != nil {
		if closeErr := stdoutLog.Close(); closeErr != nil {
			GetLogger().Warn("Failed to stream agent stdout", "error", closeErr, "agent_id", agentID, "ext", "txt")
		}
	}
	if stderrLog != nil {
		if closeErr := stderrLog.Close(); closeErr != nil {
			GetLogger().Warn("Failed to stream agent stderr", "error", closeErr, "agent_id", agentID, "ext", "err")
		}
	}
}
