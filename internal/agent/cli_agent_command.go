package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/liza-mas/liza/internal/agentlaunch"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/providers"
	"github.com/liza-mas/liza/internal/sessionvalidation"
	"github.com/liza-mas/liza/internal/subprocess"
)

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
