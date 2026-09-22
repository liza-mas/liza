package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/subprocess"
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
	plan, err := ResolveLaunchPlan(LaunchPlanRequest{
		ToolName:      req.BackendName,
		ProfileName:   req.ProfileName,
		ProfileVars:   req.ProfileVars,
		Prompt:        req.Prompt,
		ProjectRoot:   req.ProjectRoot,
		AgentID:       req.AgentID,
		TaskID:        req.TaskID,
		SessionID:     req.SessionID,
		SessionScope:  req.SessionScope,
		OutputsDir:    a.outputsDir,
		RuntimeConfig: req.RuntimeConfig,
	})
	if err != nil {
		return LLMAgentRunResult{ExitCode: 1, Output: err.Error()}, err
	}
	plan.Environment, err = resolveLaunchEnvironment(plan, req.ProjectRoot, req.AgentID, req.Generation, req.Environment)
	if err != nil {
		return LLMAgentRunResult{ExitCode: 1}, err
	}
	if a.masker != nil {
		a.masker.AddEntries(plan.Environment)
	}
	acpxAgent := plan.ACPXAgent
	sessionName := plan.ACPXSessionName
	if sessionName == "" {
		sessionName = acpxSessionName(req.AgentID)
	}
	var warm bool
	var promptProcess *acpxPromptProcess
	err = req.LaunchGate.launch(ctx, func() error {
		warm = a.hasSeenSession(sessionName) || a.sessionExists(ctx, req.ProjectRoot, req.AgentID, req.Generation, plan)
		if err := a.ensureSession(ctx, req.AgentID, req.Generation, req.ProjectRoot, plan); err != nil {
			return err
		}
		if err := a.configureSession(ctx, req.AgentID, req.Generation, plan); err != nil {
			return err
		}
		var err error
		promptProcess, err = a.startACPXPrompt(ctx, req, plan)
		return err
	})
	if err != nil {
		errText, maskedErr := a.boundedProviderFailure(err, req.BackendName)
		emitLLMAgentEvent(ctx, req.EventSink, LLMAgentEvent{
			Kind:        LLMAgentEventCompleted,
			BackendName: req.BackendName,
			AgentID:     req.AgentID,
			TaskID:      req.TaskID,
			SessionID:   sessionName,
			Message:     errText,
			Payload: map[string]any{
				"error": errText,
			},
		})
		return LLMAgentRunResult{ExitCode: 1, Output: errText, WarmUsage: warm, SessionID: sessionName}, maskedErr
	}
	a.markSessionSeen(sessionName)
	emitLLMAgentEvent(ctx, req.EventSink, LLMAgentEvent{
		Kind:        LLMAgentEventStarted,
		BackendName: req.BackendName,
		AgentID:     req.AgentID,
		TaskID:      req.TaskID,
		SessionID:   sessionName,
		Payload: map[string]any{
			"mode":       "acpx",
			"acpx_agent": acpxAgent,
		},
	})

	output, usage, failureEvidence, err := a.waitACPXPrompt(ctx, req, promptProcess)
	emitLLMAgentEvent(ctx, req.EventSink, LLMAgentEvent{
		Kind:        LLMAgentEventUsage,
		BackendName: req.BackendName,
		AgentID:     req.AgentID,
		TaskID:      req.TaskID,
		SessionID:   sessionName,
		Payload: map[string]any{
			"usage": usage,
		},
	})

	if err != nil {
		errText := a.maskText(err.Error())
		maskedErr := maskedError{message: errText, err: err}
		resultOutput := ""
		if _, diagnostic := classifyProviderUnavailable(a.maskText(failureEvidence), req.BackendName); diagnostic != "" {
			resultOutput = diagnostic
		}
		emitLLMAgentEvent(ctx, req.EventSink, LLMAgentEvent{
			Kind:        LLMAgentEventCompleted,
			BackendName: req.BackendName,
			AgentID:     req.AgentID,
			TaskID:      req.TaskID,
			SessionID:   sessionName,
			Message:     errText,
			Payload: map[string]any{
				"error": errText,
			},
		})
		return LLMAgentRunResult{ExitCode: 1, Output: resultOutput, Usage: usage, WarmUsage: warm, SessionID: sessionName}, maskedErr
	}

	if qe := DetectQuotaExhaustion(output.Text, acpxAgent); qe != nil {
		emitLLMAgentEvent(ctx, req.EventSink, LLMAgentEvent{
			Kind:        LLMAgentEventCompleted,
			BackendName: req.BackendName,
			AgentID:     req.AgentID,
			TaskID:      req.TaskID,
			SessionID:   sessionName,
			Message:     qe.Message,
			Payload: map[string]any{
				"exit_code": 1,
				"quota":     true,
			},
		})
		return LLMAgentRunResult{ExitCode: 1, Output: output.Text, Usage: usage, WarmUsage: warm, SessionID: sessionName}, nil
	}

	emitLLMAgentEvent(ctx, req.EventSink, LLMAgentEvent{
		Kind:        LLMAgentEventCompleted,
		BackendName: req.BackendName,
		AgentID:     req.AgentID,
		TaskID:      req.TaskID,
		SessionID:   sessionName,
		Payload: map[string]any{
			"exit_code": 0,
		},
	})
	return LLMAgentRunResult{ExitCode: 0, Output: output.Text, Usage: usage, WarmUsage: warm, SessionID: sessionName}, nil
}

func (a *ACPXAgent) RunInteractive(ctx context.Context, req LLMAgentInteractiveRequest) (int, error) {
	plan, err := ResolveLaunchPlan(LaunchPlanRequest{
		ToolName:      req.BackendName,
		ProfileName:   req.ProfileName,
		ProfileVars:   req.ProfileVars,
		ProjectRoot:   req.ProjectRoot,
		AgentID:       req.AgentID,
		SessionID:     req.SessionID,
		RuntimeConfig: req.RuntimeConfig,
	})
	if err != nil {
		return 0, err
	}
	if plan.Backend != ToolBackendACPX {
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
				"mode": "interactive",
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
					"exit_code": exitCode,
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
			"exit_code": 0,
		},
	})
	return 0, nil
}

func interactiveExecutableForACPX(plan LaunchPlan) string {
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

func (a *ACPXAgent) ensureSession(ctx context.Context, agentID, generation, projectRoot string, plan LaunchPlan) error {
	args := plan.ACPXEnsureArgs
	if len(args) == 0 {
		if strings.TrimSpace(projectRoot) == "" {
			return fmt.Errorf("acpx ensure args are required when project root is empty")
		}
		args = []string{"--cwd", projectRoot, plan.ACPXAgent, "sessions", "ensure", "--name", plan.ACPXSessionName}
	}
	out, err := a.runACPX(ctx, plan, args, "")
	if err != nil {
		return fmt.Errorf("acpx sessions ensure: %w\n%s", err, out)
	}
	return nil
}

func (a *ACPXAgent) configureSession(ctx context.Context, agentID, generation string, plan LaunchPlan) error {
	if len(plan.ACPXSetModeArgs) == 0 {
		return nil
	}
	out, err := a.runACPX(ctx, plan, plan.ACPXSetModeArgs, "")
	if err != nil {
		return fmt.Errorf("acpx set-mode: %w\n%s", err, out)
	}
	return nil
}

func (a *ACPXAgent) boundedProviderFailure(err error, backendName string) (string, maskedError) {
	errText := a.maskText(err.Error())
	if _, diagnostic := classifyProviderUnavailable(errText, backendName); diagnostic != "" {
		errText = diagnostic
	}
	return errText, maskedError{message: errText, err: err}
}

func (a *ACPXAgent) sessionExists(ctx context.Context, _ string, agentID, generation string, plan LaunchPlan) bool {
	if len(plan.ACPXShowArgs) == 0 {
		return false
	}
	_, err := a.runACPX(ctx, plan, plan.ACPXShowArgs, "")
	return err == nil
}

func (a *ACPXAgent) runACPX(ctx context.Context, plan LaunchPlan, args []string, stdin string) (string, error) {
	cmd, err := snapshotCommand(ctx, plan.Executable, plan.Directory, plan.Environment, args...)
	if err != nil {
		return "", err
	}
	subprocess.ConfigureCancellation(cmd)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	return stdout.String() + stderr.String(), err
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

func (a *ACPXAgent) startACPXPrompt(ctx context.Context, req LLMAgentRunRequest, plan LaunchPlan) (*acpxPromptProcess, error) {
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
		timestamp := time.Now().UTC().Format("20060102-150405")
		process.stdoutLog = newStreamingOutputFile(a.outputsDir, req.AgentID, "txt", timestamp, a.masker)
		process.stderrLog = newStreamingOutputFile(a.outputsDir, req.AgentID, "err", timestamp, a.masker)
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
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
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

func acpxSessionName(agentID string) string {
	binaryName := brand.RuntimeValues().BinaryName
	if agentID == "" {
		return binaryName + "-agent"
	}
	return binaryName + "-" + agentID
}
