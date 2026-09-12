package agent

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/envgate"
	"github.com/liza-mas/liza/internal/models"
)

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
	cmd, cleanup, err := d.buildRunCommand(ctx, LLMAgentRunRequest{
		BackendName:    cliName,
		AgentID:        agentID,
		Generation:     req.Generation,
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
		LaunchGate:     req.LaunchGate,
		Environment:    req.Environment,
		SessionScope:   req.SessionScope,
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
	outputEventsReady := make(chan struct{})
	progress := executionProgressCallback(ctx)
	if d.outputsDir != "" {
		timestamp := time.Now().UTC().Format("20060102-150405")
		stdoutLog = newStreamingOutputFile(d.outputsDir, agentID, "txt", timestamp, d.masker)
		stderrLog = newStreamingOutputFile(d.outputsDir, agentID, "err", timestamp, d.masker)
		stdoutWriters := []io.Writer{os.Stdout, &stdoutBuf, stdoutLog, llmAgentEventWriter{ctx: ctx, sink: req.EventSink, base: eventBase, stream: "stdout", ready: outputEventsReady}}
		stderrWriters := []io.Writer{os.Stderr, &stderrBuf, stderrLog, llmAgentEventWriter{ctx: ctx, sink: req.EventSink, base: eventBase, stream: "stderr", ready: outputEventsReady}}
		if progress != nil {
			pw := progressWriter{mark: progress}
			stdoutWriters = append(stdoutWriters, pw)
			stderrWriters = append(stderrWriters, pw)
		}
		cmd.Stdout = io.MultiWriter(stdoutWriters...)
		cmd.Stderr = io.MultiWriter(stderrWriters...)
	} else {
		stdoutEventWriter := llmAgentEventWriter{ctx: ctx, sink: req.EventSink, base: eventBase, stream: "stdout", ready: outputEventsReady}
		stderrEventWriter := llmAgentEventWriter{ctx: ctx, sink: req.EventSink, base: eventBase, stream: "stderr", ready: outputEventsReady}
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

	err = req.LaunchGate.launch(ctx, cmd.Start)
	if err != nil {
		close(outputEventsReady)
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
	close(outputEventsReady)
	err = cmd.Wait()
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
	cmdEnv, err := resolveLaunchEnvironment(plan, projectRoot, agentID, req.Generation, req.Environment)
	if err != nil {
		return 0, err
	}
	cmd, err := snapshotCommand(ctx, plan.Executable, projectRoot, cmdEnv, plan.Args...)
	if err != nil {
		return 0, err
	}

	cmd.Dir = projectRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = cmdEnv

	err = req.LaunchGate.launch(ctx, cmd.Start)
	if err == nil {
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
		err = cmd.Wait()
	}
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
		LaunchGate:     immediateLaunchGate,
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
		LaunchGate:     immediateLaunchGate,
	})
}

func (d *CLIAgent) buildRunCommand(ctx context.Context, req LLMAgentRunRequest) (*exec.Cmd, func(), error) {
	cmdEnv := req.Environment
	if cmdEnv == nil {
		cmdEnv = os.Environ()
	}
	disableSubagents := brandedEnvListGateValue(cmdEnv, "DISABLE_CLAUDE_SUBAGENTS") == "1"
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
			file, err := os.CreateTemp("", brand.RuntimeValues().BinaryName+"-agent-prompt-*.md")
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

	cmdEnv, err = resolveLaunchEnvironment(plan, req.ProjectRoot, req.AgentID, req.Generation, req.Environment)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	if d.masker != nil {
		d.masker.AddEntries(cmdEnv)
	}

	var cmd *exec.Cmd
	if plan.RequiresCodexWrapper {
		codexConfig := resolveCodexLaunchConfig(req.RuntimeConfig, cmdEnv)
		cmd, err = codexCommandContext(ctx, codexConfig.PackageVersion, plan.Args)
		if err != nil {
			cleanup()
			return nil, nil, err
		}
		cmd, err = snapshotCommand(ctx, cmd.Args[0], req.ProjectRoot, cmdEnv, cmd.Args[1:]...)
	} else {
		cmd, err = snapshotCommand(ctx, plan.Executable, req.ProjectRoot, cmdEnv, plan.Args...)
	}
	if err != nil {
		cleanup()
		return nil, nil, err
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
		version = strings.TrimSpace(brandedEnvListValue(env, "CODEX_VERSION"))
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
	value, _ := envLookup(env, key)
	return value
}

func envLookup(env []string, key string) (string, bool) {
	prefix := key + "="
	for i := len(env) - 1; i >= 0; i-- {
		if val, ok := strings.CutPrefix(env[i], prefix); ok {
			return val, true
		}
	}
	return "", false
}

func brandedEnvListValue(env []string, suffix string) string {
	lookup := brand.LookupEnv(func(key string) string {
		return envValue(env, key)
	}, suffix)
	return warnBrandedEnvLookup(lookup)
}

func brandedEnvListGateValue(env []string, suffix string) string {
	lookup := envgate.LookupFunc(func(key string) (string, bool) {
		return envLookup(env, key)
	}, suffix)
	return warnBrandedEnvLookup(lookup)
}

func warnBrandedEnvLookup(lookup brand.EnvLookup) string {
	if lookup.Warning != "" {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", lookup.Warning)
	}
	return lookup.Value
}

func agentProcessEnv(base []string, agentID, generation string) []string {
	brandedIDName := brand.EnvName("AGENT_ID")
	legacyIDName := brand.LegacyEnvName("AGENT_ID")
	brandedGenerationName := brand.EnvName("AGENT_GENERATION")
	legacyGenerationName := brand.LegacyEnvName("AGENT_GENERATION")
	out := make([]string, 0, len(base)+4)
	for _, entry := range base {
		if strings.HasPrefix(entry, brandedIDName+"=") ||
			strings.HasPrefix(entry, legacyIDName+"=") ||
			strings.HasPrefix(entry, brandedGenerationName+"=") ||
			strings.HasPrefix(entry, legacyGenerationName+"=") {
			continue
		}
		out = append(out, entry)
	}
	out = append(out, brandedIDName+"="+agentID, brandedGenerationName+"="+generation)
	if brandedIDName != legacyIDName {
		out = append(out, legacyIDName+"="+agentID)
	}
	if brandedGenerationName != legacyGenerationName {
		out = append(out, legacyGenerationName+"="+generation)
	}
	return out
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
