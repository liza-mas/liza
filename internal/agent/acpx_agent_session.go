package agent

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/liza-mas/liza/internal/agentlaunch"
	"github.com/liza-mas/liza/internal/subprocess"
)

// prepareRun resolves the launch plan, environment and tool-result plan of one
// run and registers the plan's secrets with the masker. On failure it returns
// the result the failed run reports.
func (a *ACPXAgent) prepareRun(req LLMAgentRunRequest) (agentlaunch.LaunchPlan, LLMAgentRunResult, error) {
	plan, err := agentlaunch.ResolveLaunchPlan(agentlaunch.LaunchPlanRequest{
		ToolName:      req.BackendName,
		ProfileName:   req.ProfileName,
		ProfileVars:   req.ProfileVars,
		Model:         req.Model,
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
		return plan, LLMAgentRunResult{ExitCode: 1, Output: err.Error()}, err
	}
	plan.Environment, err = resolveLaunchEnvironment(plan, req.ProjectRoot, req.AgentID, req.Generation, req.Environment)
	if err != nil {
		return plan, LLMAgentRunResult{ExitCode: 1}, err
	}
	engine, err := os.Executable()
	if err != nil {
		return plan, LLMAgentRunResult{ExitCode: 1}, fmt.Errorf("resolve tool-result engine executable")
	}
	plan, err = prepareDevinToolResultPlan(req, plan, engine)
	if err != nil {
		return plan, LLMAgentRunResult{ExitCode: 1, Output: err.Error()}, err
	}
	if a.masker != nil {
		a.masker.AddEntries(plan.Environment)
	}
	return plan, LLMAgentRunResult{}, nil
}

// launchPrompt ensures and configures the session, then starts the prompt
// process, all inside the launch gate; the caller waits outside it. It reports
// whether the session was already warm, also when the launch fails.
func (a *ACPXAgent) launchPrompt(ctx context.Context, req LLMAgentRunRequest, plan agentlaunch.LaunchPlan, sessionName string) (*acpxPromptProcess, bool, error) {
	var warm bool
	var promptProcess *acpxPromptProcess
	err := req.LaunchGate.launch(ctx, func() error {
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
	return promptProcess, warm, err
}

func (a *ACPXAgent) ensureSession(ctx context.Context, agentID, generation, projectRoot string, plan agentlaunch.LaunchPlan) error {
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

func (a *ACPXAgent) configureSession(ctx context.Context, agentID, generation string, plan agentlaunch.LaunchPlan) error {
	if len(plan.ACPXSetModeArgs) == 0 {
		return nil
	}
	out, err := a.runACPX(ctx, plan, plan.ACPXSetModeArgs, "")
	if err != nil {
		return fmt.Errorf("acpx set-mode: %w\n%s", err, out)
	}
	return nil
}

func (a *ACPXAgent) sessionExists(ctx context.Context, _ string, agentID, generation string, plan agentlaunch.LaunchPlan) bool {
	if len(plan.ACPXShowArgs) == 0 {
		return false
	}
	_, err := a.runACPX(ctx, plan, plan.ACPXShowArgs, "")
	return err == nil
}

func (a *ACPXAgent) runACPX(ctx context.Context, plan agentlaunch.LaunchPlan, args []string, stdin string) (string, error) {
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
