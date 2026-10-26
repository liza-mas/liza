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

	"github.com/liza-mas/liza/internal/agentlaunch"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/envgate"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/providers"
	"github.com/liza-mas/liza/internal/providersignal"
	"github.com/liza-mas/liza/internal/sessionvalidation"
	"github.com/liza-mas/liza/internal/subprocess"
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
	_ = req.AdditionalDirs // CLI execution does not use this; ACP implementations may.
	eventBase := LLMAgentEvent{
		BackendName: req.BackendName,
		AgentID:     req.AgentID,
		TaskID:      req.TaskID,
		SessionID:   req.SessionID,
	}
	cmd, cleanup, err := d.buildRunCommand(ctx, req)
	if err != nil {
		return failCLIRun(ctx, req, eventBase, err)
	}
	defer cleanup()

	streams := d.attachRunStreams(ctx, cmd, req, eventBase)
	defer closeAgentOutputLogs(streams.stdoutLog, streams.stderrLog, req.AgentID)

	if err := req.LaunchGate.launch(ctx, cmd.Start); err != nil {
		close(streams.ready)
		return failCLIRun(ctx, req, eventBase, err)
	}
	started := eventBase
	started.Kind = LLMAgentEventStarted
	started.Payload = map[string]any{llmAgentPayloadMode: llmAgentModeRun}
	emitLLMAgentEvent(ctx, req.EventSink, started)
	close(streams.ready)
	return d.completeRun(ctx, req, eventBase, streams, cmd.Wait())
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

func cliRunResult(req LLMAgentRunRequest, exitCode int, output string) LLMAgentRunResult {
	return LLMAgentRunResult{ExitCode: exitCode, Output: output, Usage: LLMAgentUsage{}, WarmUsage: req.WarmSession, SessionID: req.SessionID}
}

// failCLIRun reports a run that never reached a provider outcome: its command
// could not be built or the launch gate refused the start.
func failCLIRun(ctx context.Context, req LLMAgentRunRequest, base LLMAgentEvent, err error) (LLMAgentRunResult, error) {
	emitLLMAgentUsage(ctx, req.EventSink, base, LLMAgentUsage{})
	emitLLMAgentFailed(ctx, req.EventSink, base, err.Error())
	return cliRunResult(req, 0, ""), err
}

// completeRun maps the provider's wait outcome to the run result and its
// completed event. The CLI transport reports no usage object, so the empty
// usage is emitted explicitly on every path: the supervisor sink then pins
// unknown provenance instead of inferring zero tokens from a missing event.
func (d *CLIAgent) completeRun(ctx context.Context, req LLMAgentRunRequest, base LLMAgentEvent, streams *cliRunStreams, waitErr error) (LLMAgentRunResult, error) {
	output := streams.stdout.String() + "\n" + streams.stderr.String()
	if d.masker != nil {
		output = d.masker.MaskText(output)
	}
	emitLLMAgentUsage(ctx, req.EventSink, base, LLMAgentUsage{})
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			exitCode := exitErr.ExitCode()
			emitLLMAgentExited(ctx, req.EventSink, base, exitCode)
			return cliRunResult(req, exitCode, output), nil
		}
		emitLLMAgentFailed(ctx, req.EventSink, base, waitErr.Error())
		return cliRunResult(req, 0, output), waitErr
	}
	// A CLI may print its quota failure and still exit 0. Report it as a failed
	// session, as the ACPX backend does, so the supervisor's quota path runs.
	if qe := providersignal.DetectQuotaExhaustion(output, req.BackendName); qe != nil {
		emitLLMAgentQuotaExhausted(ctx, req.EventSink, base, qe.Message)
		return cliRunResult(req, 1, output), nil
	}
	emitLLMAgentExited(ctx, req.EventSink, base, 0)
	return cliRunResult(req, 0, output), nil
}

func (d *CLIAgent) RunInteractive(ctx context.Context, req LLMAgentInteractiveRequest) (int, error) {
	cliName := req.BackendName
	agentID := req.AgentID
	projectRoot := req.ProjectRoot
	_ = req.AdditionalDirs // CLI execution does not use this; ACP implementations may.
	plan, err := agentlaunch.ResolveLaunchPlan(agentlaunch.LaunchPlanRequest{
		ToolName:      cliName,
		ProfileName:   req.ProfileName,
		ProfileVars:   req.ProfileVars,
		Model:         req.Model,
		ProjectRoot:   projectRoot,
		AgentID:       agentID,
		SessionID:     req.SessionID,
		RuntimeConfig: req.RuntimeConfig,
		Interactive:   true,
	})
	if err != nil {
		return 0, err
	}
	if plan.Backend != providers.BackendCLI {
		return 1, fmt.Errorf("interactive mode is not supported by %s", cliName)
	}
	cmdEnv, err := resolveLaunchEnvironment(plan, projectRoot, agentID, req.Generation, req.Environment)
	if err != nil {
		return 0, err
	}
	cmdEnv = agentTaskEnv(cmdEnv, req.TaskID)
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
	promptFile, cleanup, err := d.prepareRunPromptFile(req, disableSubagents)
	if err != nil {
		return nil, nil, err
	}
	plan, err := agentlaunch.ResolveLaunchPlan(d.runLaunchRequest(req, promptFile, disableSubagents))
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	if plan.Backend != providers.BackendCLI {
		cleanup()
		return nil, nil, fmt.Errorf("%s is not a CLI backend", req.BackendName)
	}

	cmdEnv, err = resolveLaunchEnvironment(plan, req.ProjectRoot, req.AgentID, req.Generation, req.Environment)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	cmdEnv = agentTaskEnv(cmdEnv, req.TaskID)
	if d.masker != nil {
		d.masker.AddEntries(cmdEnv)
	}

	cmd, err := newRunCommand(ctx, req, plan, cmdEnv)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	cmd.Dir = req.ProjectRoot
	subprocess.ConfigureCancellation(cmd)
	if plan.UsesStdin {
		cmd.Stdin = strings.NewReader(req.Prompt)
	}
	cmd.Env = cmdEnv
	return cmd, cleanup, nil
}

// runLaunchRequest is the launch-plan request of one run. The prompt file is
// empty while the prompt-file need itself is still being resolved.
func (d *CLIAgent) runLaunchRequest(req LLMAgentRunRequest, promptFile string, disableSubagents bool) agentlaunch.LaunchPlanRequest {
	return agentlaunch.LaunchPlanRequest{
		ToolName:         req.BackendName,
		ProfileName:      req.ProfileName,
		ProfileVars:      req.ProfileVars,
		Model:            req.Model,
		Prompt:           req.Prompt,
		PromptFile:       promptFile,
		ProjectRoot:      req.ProjectRoot,
		AgentID:          req.AgentID,
		TaskID:           req.TaskID,
		SessionID:        req.SessionID,
		OutputsDir:       d.outputsDir,
		RuntimeConfig:    req.RuntimeConfig,
		DisableSubagents: disableSubagents,
	}
}

// prepareRunPromptFile returns the prompt file the launch references: the
// caller's own, or a temporary one when the tool takes its prompt as a file.
// The returned cleanup removes only a file created here.
func (d *CLIAgent) prepareRunPromptFile(req LLMAgentRunRequest, disableSubagents bool) (string, func(), error) {
	if req.PromptFile != "" {
		return req.PromptFile, func() {}, nil
	}
	plan, err := agentlaunch.ResolveLaunchPlan(d.runLaunchRequest(req, "", disableSubagents))
	if err != nil {
		return "", nil, err
	}
	if !plan.UsesPromptFile {
		return "", func() {}, nil
	}
	return writeRunPromptFile(req.Prompt)
}

func writeRunPromptFile(prompt string) (string, func(), error) {
	file, err := os.CreateTemp("", brand.RuntimeValues().BinaryName+"-agent-prompt-*.md")
	if err != nil {
		return "", nil, fmt.Errorf("create prompt file: %w", err)
	}
	promptFile := file.Name()
	if _, err := file.WriteString(prompt); err != nil {
		_ = file.Close()
		_ = os.Remove(promptFile)
		return "", nil, fmt.Errorf("write prompt file: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(promptFile)
		return "", nil, fmt.Errorf("close prompt file: %w", err)
	}
	return promptFile, func() { _ = os.Remove(promptFile) }, nil
}

// newRunCommand builds the provider process of one run from its resolved plan.
func newRunCommand(ctx context.Context, req LLMAgentRunRequest, plan agentlaunch.LaunchPlan, cmdEnv []string) (*exec.Cmd, error) {
	if plan.RequiresCodexWrapper {
		return newCodexWrapperCommand(ctx, req, plan, cmdEnv)
	}
	return newToolCommand(ctx, req, plan, cmdEnv)
}

func newCodexWrapperCommand(ctx context.Context, req LLMAgentRunRequest, plan agentlaunch.LaunchPlan, cmdEnv []string) (*exec.Cmd, error) {
	codexConfig := resolveCodexLaunchConfig(req.RuntimeConfig, cmdEnv)
	base, err := codexCommandContext(ctx, codexConfig.PackageVersion, nil)
	if err != nil {
		return nil, err
	}
	engine, err := os.Executable()
	if err != nil {
		return nil, err
	}
	native, err := sessionvalidation.LookPath(base.Args[0], req.ProjectRoot, cmdEnv)
	if err != nil {
		return nil, err
	}
	launch, err := prepareCodexToolResultLaunch(req.ProjectRoot, engine, native, base.Args[1:]...)
	if err != nil {
		return nil, err
	}
	return snapshotCommand(ctx, launch.Wrapper, req.ProjectRoot, cmdEnv, plan.Args...)
}

func newToolCommand(ctx context.Context, req LLMAgentRunRequest, plan agentlaunch.LaunchPlan, cmdEnv []string) (*exec.Cmd, error) {
	args := append([]string(nil), plan.Args...)
	executable := plan.Executable
	if plan.ProviderKey == "claude" || plan.ToolName == "claude" {
		if lookupDir, absErr := filepath.Abs(req.ProjectRoot); absErr == nil {
			if resolved, lookErr := sessionvalidation.LookPath(plan.Executable, lookupDir, cmdEnv); lookErr == nil {
				executable = resolved
			}
		}
		if err := validateClaudeToolResultBoundary(ctx, executable, req.ProjectRoot, args); err != nil {
			return nil, err
		}
		engine, err := os.Executable()
		if err != nil {
			return nil, err
		}
		settings, err := ClaudeToolResultSettings(engine, req.ProjectRoot)
		if err != nil {
			return nil, err
		}
		args = append([]string{"--settings", settings}, args...)
	}
	return snapshotCommand(ctx, executable, req.ProjectRoot, cmdEnv, args...)
}

func resolveCodexLaunchConfig(config models.Config, env []string) codexLaunchConfig {
	version := strings.TrimSpace(config.CodexPackageVersion)
	if version == "" {
		version = strings.TrimSpace(brandedEnvListValue(env, "CODEX_VERSION"))
	}
	return codexLaunchConfig{PackageVersion: version}
}

func cliSupportsStdin(cliName string) bool {
	return cliName != "vibe"
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

func buildOpenCodeArgs(prompt string, useStdin bool, outputsDir string) []string {
	// OpenCode reads the prompt from stdin when no message positional is given.
	// Keeping the prompt out of argv avoids the Windows ~8k command-line limit
	// that made every large agent prompt fail with exit code 1.
	args := []string{"run", "--dangerously-skip-permissions"}
	if !useStdin {
		args = []string{"run", prompt, "--dangerously-skip-permissions"}
	}
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

// agentTaskEnv binds subprocess telemetry to this request, never inherited state.
// An empty task explicitly clears both aliases rather than inventing an identity.
func agentTaskEnv(base []string, taskID string) []string {
	name := brand.EnvName("TASK_ID")
	legacyName := brand.LegacyEnvName("TASK_ID")
	out := make([]string, 0, len(base)+2)
	for _, entry := range base {
		if !strings.HasPrefix(entry, name+"=") && !strings.HasPrefix(entry, legacyName+"=") {
			out = append(out, entry)
		}
	}
	out = append(out, name+"="+taskID)
	if name != legacyName {
		out = append(out, legacyName+"="+taskID)
	}
	return out
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
