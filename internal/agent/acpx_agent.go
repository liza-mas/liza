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
		OutputsDir:    a.outputsDir,
		RuntimeConfig: req.RuntimeConfig,
	})
	if err != nil {
		return LLMAgentRunResult{ExitCode: 1, Output: err.Error()}, err
	}
	acpxAgent := plan.ACPXAgent
	sessionName := plan.ACPXSessionName
	if sessionName == "" {
		sessionName = acpxSessionName(req.AgentID)
	}
	warm := a.hasSeenSession(sessionName) || a.sessionExists(ctx, req.ProjectRoot, req.AgentID, plan)

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

	if err := a.ensureSession(ctx, req.AgentID, req.ProjectRoot, plan); err != nil {
		errText := a.maskText(err.Error())
		maskedErr := maskedError{message: errText, err: err}
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

	output, usage, err := a.prompt(ctx, req, plan)
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
		return LLMAgentRunResult{ExitCode: 1, Output: output.Text, Usage: usage, WarmUsage: warm, SessionID: sessionName}, maskedErr
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

	emitLLMAgentEvent(ctx, req.EventSink, LLMAgentEvent{
		Kind:        LLMAgentEventStarted,
		BackendName: req.BackendName,
		AgentID:     req.AgentID,
		SessionID:   req.SessionID,
		Payload: map[string]any{
			"mode": "interactive",
		},
	})

	cmd := exec.CommandContext(ctx, executable)
	cmd.Dir = req.ProjectRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = agentProcessEnv(os.Environ(), req.AgentID)

	err = cmd.Run()
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

func (a *ACPXAgent) ensureSession(ctx context.Context, agentID string, projectRoot string, plan LaunchPlan) error {
	args := plan.ACPXEnsureArgs
	if len(args) == 0 {
		if strings.TrimSpace(projectRoot) == "" {
			return fmt.Errorf("acpx ensure args are required when project root is empty")
		}
		args = []string{"--cwd", projectRoot, plan.ACPXAgent, "sessions", "ensure", "--name", plan.ACPXSessionName}
	}
	out, err := a.runACPX(ctx, plan.Executable, agentID, args, "")
	if err != nil {
		return fmt.Errorf("acpx sessions ensure: %w\n%s", err, out)
	}
	return nil
}

func (a *ACPXAgent) prompt(ctx context.Context, req LLMAgentRunRequest, plan LaunchPlan) (acpxOutput, LLMAgentUsage, error) {
	output, usage, raw, err := a.runACPXPrompt(ctx, req, plan)
	if err != nil {
		return output, usage, fmt.Errorf("acpx prompt: %w\n%s", err, raw)
	}
	return output, usage, nil
}

func (a *ACPXAgent) sessionExists(ctx context.Context, _ string, agentID string, plan LaunchPlan) bool {
	if len(plan.ACPXShowArgs) == 0 {
		return false
	}
	_, err := a.runACPX(ctx, plan.Executable, agentID, plan.ACPXShowArgs, "")
	return err == nil
}

func (a *ACPXAgent) runACPX(ctx context.Context, executable, agentID string, args []string, stdin string) (string, error) {
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Env = agentProcessEnv(os.Environ(), agentID)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String() + stderr.String(), err
}

func (a *ACPXAgent) runACPXPrompt(ctx context.Context, req LLMAgentRunRequest, plan LaunchPlan) (acpxOutput, LLMAgentUsage, string, error) {
	cmd := exec.CommandContext(ctx, plan.Executable, plan.ACPXPromptArgs...)
	cmd.Env = agentProcessEnv(os.Environ(), req.AgentID)
	cmd.Stdin = strings.NewReader(req.Prompt)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return acpxOutput{}, LLMAgentUsage{}, "", fmt.Errorf("open acpx stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return acpxOutput{}, LLMAgentUsage{}, "", fmt.Errorf("open acpx stderr: %w", err)
	}

	var stdoutLog, stderrLog *streamingOutputFile
	var stdoutLogWriter, stderrLogWriter io.Writer
	if a.outputsDir != "" {
		timestamp := time.Now().UTC().Format("20060102-150405")
		stdoutLog = newStreamingOutputFile(a.outputsDir, req.AgentID, "txt", timestamp, a.masker)
		stderrLog = newStreamingOutputFile(a.outputsDir, req.AgentID, "err", timestamp, a.masker)
		stdoutLogWriter = stdoutLog
		stderrLogWriter = stderrLog
	}
	defer closeAgentOutputLogs(stdoutLog, stderrLog, req.AgentID)

	eventBase := LLMAgentEvent{
		BackendName: req.BackendName,
		AgentID:     req.AgentID,
		TaskID:      req.TaskID,
		SessionID:   plan.ACPXSessionName,
	}
	progress := executionProgressCallback(ctx)

	if err := cmd.Start(); err != nil {
		return acpxOutput{}, LLMAgentUsage{}, "", err
	}

	var stdoutRaw, stderrRaw strings.Builder
	var output acpxOutput
	var usage LLMAgentUsage
	stdoutErrCh := make(chan error, 1)
	stderrErrCh := make(chan error, 1)
	go func() {
		stdoutErrCh <- a.scanACPXPromptStdout(ctx, stdout, stdoutLogWriter, &stdoutRaw, &output, &usage, req.EventSink, eventBase, progress)
	}()
	go func() {
		stderrErrCh <- copyACPXPromptStderr(stderr, stderrLogWriter, &stderrRaw, progress)
	}()

	stdoutErr := <-stdoutErrCh
	stderrErr := <-stderrErrCh
	waitErr := cmd.Wait()
	raw := stdoutRaw.String() + stderrRaw.String()
	if stdoutErr != nil {
		return output, usage, raw, stdoutErr
	}
	if stderrErr != nil {
		return output, usage, raw, stderrErr
	}
	if waitErr != nil {
		return output, usage, raw, waitErr
	}
	return output, usage, raw, nil
}

func (a *ACPXAgent) scanACPXPromptStdout(
	ctx context.Context,
	stdout io.Reader,
	stdoutLog io.Writer,
	raw *strings.Builder,
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
		raw.WriteString(line)
		raw.WriteByte('\n')
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
