package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/liza-mas/liza/internal/agentlaunch"
	"github.com/liza-mas/liza/internal/providers"
	"github.com/liza-mas/liza/internal/subprocess"
)

// Sizes of the ACPX stdout line scanner: its initial buffer and the longest
// single JSON line it accepts.
const (
	acpxStdoutScanInitialBuffer = 64 * 1024
	acpxStdoutScanMaxLine       = 8 * 1024 * 1024
)

// ACPXAgent implements LLMAgent through the headless acpx ACP client.
type ACPXAgent struct {
	outputsDir string
	masker     *SecretMasker
	mu         sync.Mutex
	seen       map[string]bool
}

// NewACPXAgent creates an ACP-backed LLM agent using acpx.
func NewACPXAgent(outputsDir string) *ACPXAgent {
	return &ACPXAgent{outputsDir: outputsDir, masker: NewSecretMasker(), seen: make(map[string]bool)}
}

func (a *ACPXAgent) Run(ctx context.Context, req LLMAgentRunRequest) (LLMAgentRunResult, error) {
	plan, failed, err := a.prepareRun(req)
	if err != nil {
		return failed, err
	}
	sessionName := plan.ACPXSessionName
	if sessionName == "" {
		sessionName = agentlaunch.ACPXSessionName(req.AgentID)
	}
	turn := acpxTurn{
		req: req,
		base: LLMAgentEvent{
			BackendName: req.BackendName,
			AgentID:     req.AgentID,
			TaskID:      req.TaskID,
			SessionID:   sessionName,
		},
		acpxAgent: plan.ACPXAgent,
	}
	promptProcess, warm, err := a.launchPrompt(ctx, req, plan, sessionName)
	turn.warm = warm
	if err != nil {
		errText, maskedErr := a.boundedProviderFailure(err, req.BackendName)
		emitLLMAgentFailed(ctx, req.EventSink, turn.base, errText)
		return turn.result(1, errText, LLMAgentUsage{}), maskedErr
	}
	a.markSessionSeen(sessionName)
	started := turn.base
	started.Kind = LLMAgentEventStarted
	started.Payload = map[string]any{llmAgentPayloadMode: llmAgentModeACPX, llmAgentPayloadACPXAgent: turn.acpxAgent}
	emitLLMAgentEvent(ctx, req.EventSink, started)

	output, usage, failureEvidence, err := a.waitACPXPrompt(ctx, req, promptProcess)
	emitLLMAgentUsage(ctx, req.EventSink, turn.base, usage)
	return a.completeRun(ctx, turn, output, usage, failureEvidence, err)
}

func (a *ACPXAgent) RunInteractive(ctx context.Context, req LLMAgentInteractiveRequest) (int, error) {
	plan, err := agentlaunch.ResolveLaunchPlan(agentlaunch.LaunchPlanRequest{
		ToolName:      req.BackendName,
		ProfileName:   req.ProfileName,
		ProfileVars:   req.ProfileVars,
		Model:         req.Model,
		ProjectRoot:   req.ProjectRoot,
		AgentID:       req.AgentID,
		SessionID:     req.SessionID,
		RuntimeConfig: req.RuntimeConfig,
	})
	if err != nil {
		return 0, err
	}
	if plan.Backend != providers.BackendACPX {
		return NewCLIAgent(a.outputsDir).RunInteractive(ctx, req)
	}

	executable := interactiveExecutableForACPX(plan)
	if executable == "" {
		return 1, fmt.Errorf("interactive mode is not supported by %s", req.BackendName)
	}

	env, err := resolveLaunchEnvironment(plan, req.ProjectRoot, req.AgentID, req.Generation, req.Environment)
	if err != nil {
		return 0, err
	}
	env = agentTaskEnv(env, req.TaskID)
	cmd, err := snapshotCommand(ctx, executable, req.ProjectRoot, env)
	if err != nil {
		return 0, err
	}
	cmd.Dir = req.ProjectRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	err = req.LaunchGate.launch(ctx, cmd.Start)
	if err == nil {
		emitLLMAgentEvent(ctx, req.EventSink, LLMAgentEvent{
			Kind:        LLMAgentEventStarted,
			BackendName: req.BackendName,
			AgentID:     req.AgentID,
			SessionID:   req.SessionID,
			Payload: map[string]any{
				llmAgentPayloadMode: llmAgentModeInteractive,
			},
		})
		err = cmd.Wait()
	}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode := exitErr.ExitCode()
			emitLLMAgentEvent(ctx, req.EventSink, LLMAgentEvent{
				Kind:        LLMAgentEventCompleted,
				BackendName: req.BackendName,
				AgentID:     req.AgentID,
				SessionID:   req.SessionID,
				Payload: map[string]any{
					llmAgentPayloadExitCode: exitCode,
				},
			})
			return exitCode, nil
		}
		return 0, err
	}
	emitLLMAgentEvent(ctx, req.EventSink, LLMAgentEvent{
		Kind:        LLMAgentEventCompleted,
		BackendName: req.BackendName,
		AgentID:     req.AgentID,
		SessionID:   req.SessionID,
		Payload: map[string]any{
			llmAgentPayloadExitCode: 0,
		},
	})
	return 0, nil
}

func interactiveExecutableForACPX(plan agentlaunch.LaunchPlan) string {
	switch plan.ACPXAgent {
	case "cursor":
		return "cursor-agent"
	case "codex":
		return "codex"
	case "opencode":
		return "opencode"
	default:
		fields := strings.Fields(plan.ACPXAgent)
		if len(fields) == 0 {
			return ""
		}
		return fields[0]
	}
}

type acpxPromptProcess struct {
	cmd             *exec.Cmd
	stdout          io.ReadCloser
	stderr          io.ReadCloser
	stdoutLog       *streamingOutputFile
	stderrLog       *streamingOutputFile
	stdoutLogWriter io.Writer
	stderrLogWriter io.Writer
	eventBase       LLMAgentEvent
}

func (a *ACPXAgent) startACPXPrompt(ctx context.Context, req LLMAgentRunRequest, plan agentlaunch.LaunchPlan) (*acpxPromptProcess, error) {
	cmd, err := snapshotCommand(ctx, plan.Executable, req.ProjectRoot, plan.Environment, plan.ACPXPromptArgs...)
	if err != nil {
		return nil, err
	}
	subprocess.ConfigureCancellation(cmd)
	cmd.Stdin = strings.NewReader(req.Prompt)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open acpx stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("open acpx stderr: %w", err)
	}

	process := &acpxPromptProcess{
		cmd:    cmd,
		stdout: stdout,
		stderr: stderr,
		eventBase: LLMAgentEvent{
			BackendName: req.BackendName,
			AgentID:     req.AgentID,
			TaskID:      req.TaskID,
			SessionID:   plan.ACPXSessionName,
		},
	}
	if a.outputsDir != "" {
		process.stdoutLog, process.stderrLog = newAgentOutputLogs(a.outputsDir, req.AgentID, a.masker)
		process.stdoutLogWriter = process.stdoutLog
		process.stderrLogWriter = process.stderrLog
	}

	if err := cmd.Start(); err != nil {
		closeAgentOutputLogs(process.stdoutLog, process.stderrLog, req.AgentID)
		return nil, err
	}
	return process, nil
}

func (a *ACPXAgent) waitACPXPrompt(ctx context.Context, req LLMAgentRunRequest, process *acpxPromptProcess) (acpxOutput, LLMAgentUsage, string, error) {
	defer closeAgentOutputLogs(process.stdoutLog, process.stderrLog, req.AgentID)
	progress := executionProgressCallback(ctx)

	// These readers drain before cmd.Wait, so WaitDelay alone cannot release
	// them when a descendant escapes the owned group and retains a pipe.
	// Cancellation allows the same grace period, then closes only our readers.
	stopClosing := make(chan struct{})
	closingDone := make(chan struct{})
	go func() {
		defer close(closingDone)
		select {
		case <-stopClosing:
			return
		case <-ctx.Done():
		}
		timer := time.NewTimer(process.cmd.WaitDelay)
		defer timer.Stop()
		select {
		case <-stopClosing:
		case <-timer.C:
			_ = process.stdout.Close()
			_ = process.stderr.Close()
		}
	}()
	defer func() {
		close(stopClosing)
		<-closingDone
	}()

	var stderrRaw strings.Builder
	var output acpxOutput
	var usage LLMAgentUsage
	stdoutErrCh := make(chan error, 1)
	stderrErrCh := make(chan error, 1)
	go func() {
		stdoutErrCh <- a.scanACPXPromptStdout(ctx, process.stdout, process.stdoutLogWriter, &output, &usage, req.EventSink, process.eventBase, progress)
	}()
	go func() {
		stderrErrCh <- copyACPXPromptStderr(process.stderr, process.stderrLogWriter, &stderrRaw, progress)
	}()

	stdoutErr := <-stdoutErrCh
	stderrErr := <-stderrErrCh
	waitErr := process.cmd.Wait()
	failureEvidence := stderrRaw.String()
	if stdoutErr != nil {
		return output, usage, failureEvidence, stdoutErr
	}
	if stderrErr != nil {
		return output, usage, failureEvidence, stderrErr
	}
	if waitErr != nil {
		return output, usage, failureEvidence, fmt.Errorf("acpx prompt: %w", waitErr)
	}
	return output, usage, "", nil
}

func (a *ACPXAgent) scanACPXPromptStdout(
	ctx context.Context,
	stdout io.Reader,
	stdoutLog io.Writer,
	output *acpxOutput,
	usage *LLMAgentUsage,
	sink LLMAgentEventSink,
	eventBase LLMAgentEvent,
	markProgress func(),
) error {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, acpxStdoutScanInitialBuffer), acpxStdoutScanMaxLine)
	for scanner.Scan() {
		line := scanner.Text()
		if stdoutLog != nil {
			if _, err := stdoutLog.Write([]byte(line + "\n")); err != nil {
				return err
			}
		}
		if markProgress != nil {
			markProgress()
		}
		a.ingestACPXPromptLine(ctx, line, output, usage, sink, eventBase)
	}
	return scanner.Err()
}

func copyACPXPromptStderr(stderr io.Reader, stderrLog io.Writer, raw *strings.Builder, markProgress func()) error {
	writers := []io.Writer{raw}
	if stderrLog != nil {
		writers = append(writers, stderrLog)
	}
	if markProgress != nil {
		writers = append(writers, progressWriter{mark: markProgress})
	}
	_, err := io.Copy(io.MultiWriter(writers...), stderr)
	return err
}

func (a *ACPXAgent) ingestACPXPromptLine(ctx context.Context, line string, output *acpxOutput, usage *LLMAgentUsage, sink LLMAgentEventSink, eventBase LLMAgentEvent) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	var msg map[string]any
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		return
	}
	if params, ok := msg["params"].(map[string]any); ok {
		if update, ok := params["update"].(map[string]any); ok {
			if chunk := acpxAgentMessageChunk(update); chunk != "" {
				chunk = a.maskText(chunk)
				output.Chunks = append(output.Chunks, chunk)
				output.Text += chunk
				event := eventBase
				event.Kind = LLMAgentEventMessage
				event.Message = chunk
				emitLLMAgentEvent(ctx, sink, event)
			}
		}
	}
	if result, ok := msg["result"].(map[string]any); ok {
		if parsed, ok := acpxAgentUsage(result["usage"]); ok {
			*usage = parsed
		}
	}
}

func (a *ACPXAgent) hasSeenSession(sessionName string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.seen[sessionName]
}

func (a *ACPXAgent) markSessionSeen(sessionName string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.seen[sessionName] = true
}

func (a *ACPXAgent) maskText(text string) string {
	if a.masker == nil {
		return text
	}
	return a.masker.MaskText(text)
}

type acpxOutput struct {
	Text   string
	Chunks []string
}

type maskedError struct {
	message string
	err     error
}

func (e maskedError) Error() string {
	return e.message
}

func (e maskedError) Unwrap() error {
	return e.err
}

func acpxAgentMessageChunk(update map[string]any) string {
	if update["sessionUpdate"] != "agent_message_chunk" {
		return ""
	}
	content, ok := update["content"].(map[string]any)
	if !ok {
		return ""
	}
	text, ok := content["text"].(string)
	if !ok {
		return ""
	}
	return text
}

func acpxAgentUsage(raw any) (LLMAgentUsage, bool) {
	usage, ok := raw.(map[string]any)
	if !ok {
		return LLMAgentUsage{}, false
	}
	return LLMAgentUsage{
		InputTokens:      acpxInt(usage["inputTokens"]),
		OutputTokens:     acpxInt(usage["outputTokens"]),
		CachedReadTokens: acpxInt(usage["cachedReadTokens"]),
	}, true
}

func acpxInt(raw any) int {
	switch v := raw.(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}
