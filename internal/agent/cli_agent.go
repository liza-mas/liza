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

	"github.com/liza-mas/liza/internal/brand"
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

	cmd, cleanup, err := d.buildRunCommand(ctx, LLMAgentRunRequest{
		BackendName:    cliName,
		AgentID:        agentID,
		TaskID:         req.TaskID,
		SessionID:      req.SessionID,
		ProfileName:    req.ProfileName,
		ProfileVars:    req.ProfileVars,
		Prompt:         prompt,
		PromptFile:     req.PromptFile,
		ProjectRoot:    projectRoot,
		AdditionalDirs: req.AdditionalDirs,
		RuntimeConfig:  runtimeConfig,
		EventSink:      req.EventSink,
	})
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
	defer cleanup()

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

	cmdEnv := agentProcessEnv(os.Environ(), agentID)
	plan, err := ResolveLaunchPlan(LaunchPlanRequest{
		ToolName:      cliName,
		ProfileName:   req.ProfileName,
		ProfileVars:   req.ProfileVars,
		ProjectRoot:   projectRoot,
		AgentID:       agentID,
		SessionID:     req.SessionID,
		RuntimeConfig: req.RuntimeConfig,
		Interactive:   true,
	})
	if err != nil {
		return 0, err
	}
	if plan.Backend != ToolBackendCLI {
		return 1, fmt.Errorf("interactive mode is not supported by %s", cliName)
	}
	cmd := exec.CommandContext(ctx, plan.Executable, plan.Args...)

	cmd.Dir = projectRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = cmdEnv

	err = cmd.Run()
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

func (d *CLIAgent) buildRunCommand(ctx context.Context, req LLMAgentRunRequest) (*exec.Cmd, func(), error) {
	cmdEnv := os.Environ()
	disableSubagents := envValue(cmdEnv, "LIZA_DISABLE_CLAUDE_SUBAGENTS") == "1"
	promptFile := req.PromptFile
	cleanup := func() {}
	if promptFile == "" {
		plan, err := ResolveLaunchPlan(LaunchPlanRequest{
			ToolName:         req.BackendName,
			ProfileName:      req.ProfileName,
			ProfileVars:      req.ProfileVars,
			Prompt:           req.Prompt,
			ProjectRoot:      req.ProjectRoot,
			AgentID:          req.AgentID,
			TaskID:           req.TaskID,
			SessionID:        req.SessionID,
			OutputsDir:       d.outputsDir,
			RuntimeConfig:    req.RuntimeConfig,
			DisableSubagents: disableSubagents,
		})
		if err != nil {
			return nil, nil, err
		}
		if plan.UsesPromptFile {
			file, err := os.CreateTemp("", "liza-agent-prompt-*.md")
			if err != nil {
				return nil, nil, fmt.Errorf("create prompt file: %w", err)
			}
			promptFile = file.Name()
			if _, err := file.WriteString(req.Prompt); err != nil {
				_ = file.Close()
				_ = os.Remove(promptFile)
				return nil, nil, fmt.Errorf("write prompt file: %w", err)
			}
			if err := file.Close(); err != nil {
				_ = os.Remove(promptFile)
				return nil, nil, fmt.Errorf("close prompt file: %w", err)
			}
			cleanup = func() {
				_ = os.Remove(promptFile)
			}
		}
	}
	plan, err := ResolveLaunchPlan(LaunchPlanRequest{
		ToolName:         req.BackendName,
		ProfileName:      req.ProfileName,
		ProfileVars:      req.ProfileVars,
		Prompt:           req.Prompt,
		PromptFile:       promptFile,
		ProjectRoot:      req.ProjectRoot,
		AgentID:          req.AgentID,
		TaskID:           req.TaskID,
		SessionID:        req.SessionID,
		OutputsDir:       d.outputsDir,
		RuntimeConfig:    req.RuntimeConfig,
		DisableSubagents: disableSubagents,
	})
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	if plan.Backend != ToolBackendCLI {
		cleanup()
		return nil, nil, fmt.Errorf("%s is not a CLI backend", req.BackendName)
	}

	for _, envFile := range plan.EnvFiles {
		if envFile == "" {
			continue
		}
		if !filepath.IsAbs(envFile) {
			envFile = filepath.Join(req.ProjectRoot, envFile)
		}
		if extra := loadEnvFile(envFile); len(extra) > 0 {
			cmdEnv = append(cmdEnv, extra...)
			if d.masker != nil {
				d.masker.AddEntries(extra)
			}
		}
	}
	cmdEnv = agentProcessEnv(cmdEnv, req.AgentID)

	var cmd *exec.Cmd
	if plan.RequiresCodexWrapper {
		codexConfig := resolveCodexLaunchConfig(req.RuntimeConfig, cmdEnv)
		var err error
		cmd, err = codexCommandContext(ctx, codexConfig.PackageVersion, plan.Args)
		if err != nil {
			cleanup()
			return nil, nil, err
		}
	} else {
		cmd = exec.CommandContext(ctx, plan.Executable, plan.Args...)
	}

	cmd.Dir = req.ProjectRoot
	if plan.UsesStdin {
		cmd.Stdin = strings.NewReader(req.Prompt)
	}
	cmd.Env = cmdEnv
	return cmd, cleanup, nil
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
	brandedName := brand.EnvName("AGENT_ID")
	legacyName := "LIZA_AGENT_ID"
	out := make([]string, 0, len(base)+2)
	for _, entry := range base {
		if strings.HasPrefix(entry, brandedName+"=") || strings.HasPrefix(entry, legacyName+"=") {
			continue
		}
		out = append(out, entry)
	}
	out = append(out, brandedName+"="+agentID)
	if brandedName != legacyName {
		out = append(out, legacyName+"="+agentID)
	}
	return out
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
