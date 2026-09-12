package agent

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/sessionvalidation"
)

// resolveLaunchEnvironment refreshes configured overlays once. Subsequent
// subprocesses consume this snapshot, including executable lookup against PATH.
func resolveLaunchEnvironment(plan LaunchPlan, root, agentID, generation string, frozen []string) ([]string, error) {
	if frozen != nil {
		return append([]string{}, frozen...), nil
	}
	env, err := sessionvalidation.ResolveEnvironment(os.Environ(), root, plan.EnvFiles, plan.OptionalEnvFiles)
	if err != nil {
		return nil, err
	}
	return agentProcessEnv(env, agentID, generation), nil
}

func snapshotCommand(ctx context.Context, executable, cwd string, env []string, args ...string) (*exec.Cmd, error) {
	lookupDir, err := filepath.Abs(cwd)
	if err != nil {
		return nil, &sessionvalidation.Error{Code: "context_unavailable"}
	}
	resolved, err := sessionvalidation.LookPath(executable, lookupDir, env)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, resolved, args...)
	cmd.Dir = cwd
	cmd.Env = append([]string{}, env...)
	return cmd, nil
}

func prepareSupervisorSession(config SupervisorConfig, runtime models.Config) (*ops.ValidationSession, error) {
	// An injected adapter does not promise to use a supplied process environment.
	// Legacy tasks still work; protected tasks fail closed on this unset policy.
	switch config.LLMAgent.(type) {
	case *CLIAgent, *ACPXAgent:
	default:
		return &ops.ValidationSession{}, nil
	}
	plan, err := ResolveLaunchPlan(LaunchPlanRequest{
		ToolName: config.CLIName, ProfileName: config.ProfileName,
		ProfileVars: config.ProfileVars, ProjectRoot: config.ProjectRoot,
		AgentID: config.AgentID, RuntimeConfig: runtime, Interactive: config.Interactive,
	})
	if err != nil {
		return nil, err
	}
	env, err := resolveLaunchEnvironment(plan, config.ProjectRoot, config.AgentID, config.Authority.Generation, nil)
	return &ops.ValidationSession{
		Environment: env, Execution: plan.ValidationExecution, PreparationError: err,
		ToolName: plan.ToolName, ConfigDigest: ops.ValidationConfigDigest(runtime),
	}, nil
}

func prepareClaimSession(config SupervisorConfig, bb *db.Blackboard) (*ops.ValidationSession, error) {
	state, err := bb.Read()
	if err != nil {
		return nil, err
	}
	return prepareSupervisorSession(config, state.Config)
}

func releaseFailedValidation(config SupervisorConfig, taskID string, err error) error {
	if !errors.Is(err, sessionvalidation.ErrPreflight) {
		return err
	}
	if releaseErr := ops.ReleaseValidationOwnership(config.ProjectRoot, taskID, config.AgentID, &config.Authority); releaseErr != nil {
		return errors.Join(err, releaseErr)
	}
	return err
}
