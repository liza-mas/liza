package agent

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

const envLizaCodexVersion = "LIZA_CODEX_VERSION"

type codexLaunchConfig struct {
	PackageVersion string
}

// CLIAgent implements LLMAgent by executing CLI-based agent backends as subprocesses.
type CLIAgent struct {
	outputsDir string
	masker     *SecretMasker
}

// NewCLIAgent creates a CLI-backed LLM agent.
func NewCLIAgent(outputsDir string) *CLIAgent {
	var masker *SecretMasker
	if outputsDir != "" {
		masker = NewSecretMasker()
	}
	return &CLIAgent{outputsDir: outputsDir, masker: masker}
}

// NewDefaultCLIExecutor is the legacy constructor name.
//
// Deprecated: use NewCLIAgent.
func NewDefaultCLIExecutor(outputsDir string) *CLIAgent {
	return NewCLIAgent(outputsDir)
}

func (d *CLIAgent) Run(ctx context.Context, req LLMAgentRunRequest) (LLMAgentRunResult, error) {
	cliName := req.BackendName
	agentID := req.AgentID
	prompt := req.Prompt
	projectRoot := req.ProjectRoot
	runtimeConfig := req.RuntimeConfig
	_ = req.AdditionalDirs // CLI execution does not use this; ACP implementations may.
	eventBase := LLMAgentEvent{
		BackendName: cliName,
		AgentID:     agentID,
		TaskID:      req.TaskID,
		SessionID:   req.SessionID,
	}
	emitLLMAgentEvent(ctx, req.EventSink, LLMAgentEvent{
		Kind:        LLMAgentEventStarted,
		BackendName: cliName,
		AgentID:     agentID,
		TaskID:      req.TaskID,
		SessionID:   req.SessionID,
		Payload: map[string]any{
			"mode": "run",
		},
	})

	cmd, err := d.buildRunCommand(ctx, cliName, agentID, prompt, projectRoot, runtimeConfig)
	if err != nil {
		emitLLMAgentEvent(ctx, req.EventSink, LLMAgentEvent{
			Kind:        LLMAgentEventCompleted,
			BackendName: cliName,
			AgentID:     agentID,
			TaskID:      req.TaskID,
			SessionID:   req.SessionID,
			Message:     err.Error(),
			Payload: map[string]any{
				"error": err.Error(),
			},
		})
		return LLMAgentRunResult{Usage: LLMAgentUsage{}, WarmUsage: req.WarmSession, SessionID: req.SessionID}, err
	}

	var stdoutBuf, stderrBuf strings.Builder
	var stdoutLog, stderrLog *streamingOutputFile
	progress := executionProgressCallback(ctx)
	if d.outputsDir != "" {
		timestamp := time.Now().UTC().Format("20060102-150405")
		stdoutLog = newStreamingOutputFile(d.outputsDir, agentID, "txt", timestamp, d.masker)
		stderrLog = newStreamingOutputFile(d.outputsDir, agentID, "err", timestamp, d.masker)
		stdoutWriters := []io.Writer{os.Stdout, &stdoutBuf, stdoutLog, llmAgentEventWriter{ctx: ctx, sink: req.EventSink, base: eventBase, stream: "stdout"}}
		stderrWriters := []io.Writer{os.Stderr, &stderrBuf, stderrLog, llmAgentEventWriter{ctx: ctx, sink: req.EventSink, base: eventBase, stream: "stderr"}}
		if progress != nil {
			pw := progressWriter{mark: progress}
			stdoutWriters = append(stdoutWriters, pw)
			stderrWriters = append(stderrWriters, pw)
		}
		cmd.Stdout = io.MultiWriter(stdoutWriters...)
		cmd.Stderr = io.MultiWriter(stderrWriters...)
	} else {
		stdoutEventWriter := llmAgentEventWriter{ctx: ctx, sink: req.EventSink, base: eventBase, stream: "stdout"}
		stderrEventWriter := llmAgentEventWriter{ctx: ctx, sink: req.EventSink, base: eventBase, stream: "stderr"}
		if progress != nil {
			pw := progressWriter{mark: progress}
			cmd.Stdout = io.MultiWriter(os.Stdout, &stdoutBuf, pw, stdoutEventWriter)
			cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf, pw, stderrEventWriter)
		} else {
			cmd.Stdout = io.MultiWriter(os.Stdout, &stdoutBuf, stdoutEventWriter)
			cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf, stderrEventWriter)
		}
	}

	defer closeAgentOutputLogs(stdoutLog, stderrLog, agentID)

	err = cmd.Run()
	stdout := stdoutBuf.String()
	stderr := stderrBuf.String()
	output := stdout + "\n" + stderr
	if d.masker != nil {
		output = d.masker.MaskText(output)
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode := exitErr.ExitCode()
			emitLLMAgentEvent(ctx, req.EventSink, LLMAgentEvent{
				Kind:        LLMAgentEventCompleted,
				BackendName: cliName,
				AgentID:     agentID,
				TaskID:      req.TaskID,
				SessionID:   req.SessionID,
				Payload: map[string]any{
					"exit_code": exitCode,
				},
			})
			return LLMAgentRunResult{ExitCode: exitCode, Output: output, Usage: LLMAgentUsage{}, WarmUsage: req.WarmSession, SessionID: req.SessionID}, nil
		}
		emitLLMAgentEvent(ctx, req.EventSink, LLMAgentEvent{
			Kind:        LLMAgentEventCompleted,
			BackendName: cliName,
			AgentID:     agentID,
			TaskID:      req.TaskID,
			SessionID:   req.SessionID,
			Message:     err.Error(),
			Payload: map[string]any{
				"error": err.Error(),
			},
		})
		return LLMAgentRunResult{Output: output, Usage: LLMAgentUsage{}, WarmUsage: req.WarmSession, SessionID: req.SessionID}, err
	}

	emitLLMAgentEvent(ctx, req.EventSink, LLMAgentEvent{
		Kind:        LLMAgentEventCompleted,
		BackendName: cliName,
		AgentID:     agentID,
		TaskID:      req.TaskID,
		SessionID:   req.SessionID,
		Payload: map[string]any{
			"exit_code": 0,
		},
	})
	return LLMAgentRunResult{ExitCode: 0, Output: output, Usage: LLMAgentUsage{}, WarmUsage: req.WarmSession, SessionID: req.SessionID}, nil
}

func (d *CLIAgent) RunInteractive(ctx context.Context, req LLMAgentInteractiveRequest) (int, error) {
	cliName := req.BackendName
	agentID := req.AgentID
	projectRoot := req.ProjectRoot
	_ = req.AdditionalDirs // CLI execution does not use this; ACP implementations may.
	emitLLMAgentEvent(ctx, req.EventSink, LLMAgentEvent{
		Kind:        LLMAgentEventStarted,
		BackendName: cliName,
		AgentID:     agentID,
		TaskID:      "",
		SessionID:   req.SessionID,
		Payload: map[string]any{
			"mode": "interactive",
		},
	})

	actualCLI := cliName
	if cliName == "mistral" {
		actualCLI = "vibe"
	}

	cmdEnv := agentProcessEnv(os.Environ(), agentID)
	var cmd *exec.Cmd
	switch actualCLI {
	case "codex":
		var err error
		cmd, err = codexCommandContext(ctx, "", codexInteractiveArgs())
		if err != nil {
			return 0, err
		}
	default:
		cmd = exec.CommandContext(ctx, actualCLI)
	}

	cmd.Dir = projectRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = cmdEnv

	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode := exitErr.ExitCode()
			emitLLMAgentEvent(ctx, req.EventSink, LLMAgentEvent{
				Kind:        LLMAgentEventCompleted,
				BackendName: cliName,
				AgentID:     agentID,
				TaskID:      "",
				SessionID:   req.SessionID,
				Payload: map[string]any{
					"exit_code": exitCode,
				},
			})
			return exitCode, nil
		}
		emitLLMAgentEvent(ctx, req.EventSink, LLMAgentEvent{
			Kind:        LLMAgentEventCompleted,
			BackendName: cliName,
			AgentID:     agentID,
			TaskID:      "",
			SessionID:   req.SessionID,
			Message:     err.Error(),
			Payload: map[string]any{
				"error": err.Error(),
			},
		})
		return 0, err
	}

	emitLLMAgentEvent(ctx, req.EventSink, LLMAgentEvent{
		Kind:        LLMAgentEventCompleted,
		BackendName: cliName,
		AgentID:     agentID,
		TaskID:      "",
		SessionID:   req.SessionID,
		Payload: map[string]any{
			"exit_code": 0,
		},
	})
	return 0, nil
}

type llmAgentEventWriter struct {
	ctx    context.Context
	sink   LLMAgentEventSink
	base   LLMAgentEvent
	stream string
}

func (w llmAgentEventWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	event := w.base
	event.Kind = LLMAgentEventOutputChunk
	event.Message = string(p)
	event.Payload = map[string]any{
		"stream": w.stream,
		"bytes":  len(p),
	}
	emitLLMAgentEvent(w.ctx, w.sink, event)
	return len(p), nil
}

// Execute is the legacy method name.
//
// Deprecated: use Run.
func (d *CLIAgent) Execute(ctx context.Context, cliName string, agentID string, prompt string, projectRoot string, additionalDirs []string, runtimeConfig models.Config) (CLIExecutionResult, error) {
	return d.Run(ctx, LLMAgentRunRequest{
		BackendName:    cliName,
		AgentID:        agentID,
		Prompt:         prompt,
		ProjectRoot:    projectRoot,
		AdditionalDirs: additionalDirs,
		RuntimeConfig:  runtimeConfig,
	})
}

// ExecuteInteractive is the legacy method name.
//
// Deprecated: use RunInteractive.
func (d *CLIAgent) ExecuteInteractive(ctx context.Context, cliName string, agentID string, projectRoot string, additionalDirs []string) (int, error) {
	return d.RunInteractive(ctx, LLMAgentInteractiveRequest{
		BackendName:    cliName,
		AgentID:        agentID,
		ProjectRoot:    projectRoot,
		AdditionalDirs: additionalDirs,
	})
}

func (d *CLIAgent) buildRunCommand(ctx context.Context, cliName, agentID, prompt, projectRoot string, runtimeConfig models.Config) (*exec.Cmd, error) {
	actualCLI := cliName
	if cliName == "mistral" {
		actualCLI = "vibe"
	}

	cmdEnv := os.Environ()
	if actualCLI == "claude" {
		envFile := filepath.Join(projectRoot, "claude.env")
		if extra := loadEnvFile(envFile); len(extra) > 0 {
			cmdEnv = append(cmdEnv, extra...)
			if d.masker != nil {
				d.masker.AddEntries(extra)
			}
		}
	}
	cmdEnv = agentProcessEnv(cmdEnv, agentID)

	useStdin := cliSupportsStdin(actualCLI)
	var cmd *exec.Cmd
	switch actualCLI {
	case "claude":
		disableSubagents := envValue(cmdEnv, "LIZA_DISABLE_CLAUDE_SUBAGENTS") == "1"
		cmd = exec.CommandContext(ctx, "claude", buildClaudeArgs(prompt, useStdin, d.outputsDir, disableSubagents)...)
	case "codex":
		codexConfig := resolveCodexLaunchConfig(runtimeConfig, cmdEnv)
		var err error
		cmd, err = codexCommandContext(ctx, codexConfig.PackageVersion, buildCodexArgs(prompt, useStdin, d.outputsDir))
		if err != nil {
			return nil, err
		}
	case "opencode":
		cmd = exec.CommandContext(ctx, "opencode", buildOpenCodeArgs(prompt, d.outputsDir)...)
	case "gemini":
		args := []string{"-p"}
		if !useStdin {
			args = append(args, prompt)
		}
		if d.outputsDir != "" {
			args = append(args, "--output-format", "stream-json")
		}
		cmd = exec.CommandContext(ctx, "gemini", args...)
	case "vibe":
		args := []string{"-p", prompt}
		if d.outputsDir != "" {
			args = append(args, "--output", "streaming")
		}
		cmd = exec.CommandContext(ctx, "vibe", args...)
	case "kimi":
		args := []string{"-p"}
		if !useStdin {
			args = append(args, prompt)
		}
		if d.outputsDir != "" {
			args = append(args, "--verbose", "--output-format", "stream-json")
		}
		cmd = exec.CommandContext(ctx, "kimi", args...)
	default:
		return nil, fmt.Errorf("unknown CLI: %s", cliName)
	}

	cmd.Dir = projectRoot
	if useStdin {
		cmd.Stdin = strings.NewReader(prompt)
	}
	cmd.Env = cmdEnv
	return cmd, nil
}

func resolveCodexLaunchConfig(config models.Config, env []string) codexLaunchConfig {
	version := strings.TrimSpace(config.CodexPackageVersion)
	if version == "" {
		version = strings.TrimSpace(envValue(env, envLizaCodexVersion))
	}
	return codexLaunchConfig{PackageVersion: version}
}

func cliSupportsStdin(cliName string) bool {
	return cliName != "vibe" && cliName != "opencode"
}

func buildClaudeArgs(prompt string, useStdin bool, outputsDir string, disableSubagents bool) []string {
	args := []string{"-p"}
	if !useStdin {
		args = append(args, prompt)
	}
	if disableSubagents {
		args = append(args, "--disallowedTools", "Task")
	}
	if outputsDir != "" {
		args = append(args, "--verbose", "--output-format", "stream-json")
	}
	return args
}

func buildCodexArgs(prompt string, useStdin bool, outputsDir string) []string {
	args := []string{"exec"}
	if outputsDir != "" {
		args = append(args, "--json")
	}
	if useStdin {
		args = append(args, "-")
	} else {
		args = append(args, prompt)
	}
	return args
}

func buildOpenCodeArgs(prompt string, outputsDir string) []string {
	// OpenCode documents `opencode run [message..]`; keep the prompt positional
	// until a stdin/file prompt mode exists. Very large prompts remain bounded by
	// the host OS argv limit.
	args := []string{"run", prompt, "--dangerously-skip-permissions"}
	if outputsDir != "" {
		args = append(args, "--format", "json")
	}
	return args
}

func codexInteractiveArgs() []string {
	return nil
}

func codexCommandContext(ctx context.Context, version string, args []string) (*exec.Cmd, error) {
	version = strings.TrimSpace(version)
	if version == "" {
		return exec.CommandContext(ctx, "codex", args...), nil
	}
	if strings.ContainsAny(version, " \t\r\n") {
		return nil, fmt.Errorf("codex package version must not contain whitespace: %q", version)
	}
	npmArgs := []string{"exec", "--yes", "--package", "@openai/codex@" + version, "--", "codex"}
	npmArgs = append(npmArgs, args...)
	return exec.CommandContext(ctx, "npm", npmArgs...), nil
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for i := len(env) - 1; i >= 0; i-- {
		if val, ok := strings.CutPrefix(env[i], prefix); ok {
			return val
		}
	}
	return ""
}

func agentProcessEnv(base []string, agentID string) []string {
	out := make([]string, 0, len(base)+1)
	for _, entry := range base {
		if strings.HasPrefix(entry, "LIZA_AGENT_ID=") {
			continue
		}
		out = append(out, entry)
	}
	return append(out, "LIZA_AGENT_ID="+agentID)
}

func loadEnvFile(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var env []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.Index(line, " #"); idx >= 0 {
			line = strings.TrimRight(line[:idx], " ")
		}
		if strings.Contains(line, "=") {
			env = append(env, line)
		}
	}
	return env
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
