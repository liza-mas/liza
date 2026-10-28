package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/liza-mas/liza/internal/agentlaunch"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/envgate"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/providers"
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
	cmdEnv = systemPromptContractsEnv(agentTaskEnv(cmdEnv, req.TaskID), plan.SystemPromptContracts)
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
				BackendName: cliName,
				AgentID:     agentID,
				TaskID:      "",
				SessionID:   req.SessionID,
				Payload: map[string]any{
					llmAgentPayloadExitCode: exitCode,
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
				llmAgentPayloadError: err.Error(),
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
			llmAgentPayloadExitCode: 0,
		},
	})
	return 0, nil
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
