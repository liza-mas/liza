package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/sessionvalidation"
)

// resolveLaunchEnvironment refreshes configured overlays once. Subsequent
// subprocesses consume this snapshot, including executable lookup against PATH.
func resolveLaunchEnvironment(plan LaunchPlan, root, agentID, generation string, frozen []string) ([]string, error) {
	if frozen != nil {
		return scrubRuntimeInputNames(root, append([]string{}, frozen...))
	}
	env, err := sessionvalidation.ResolveEnvironment(os.Environ(), root, plan.EnvFiles, plan.OptionalEnvFiles)
	if err != nil {
		return nil, err
	}
	overlay, err := sessionvalidation.ResolveEnvironment(nil, root, plan.EnvFiles, plan.OptionalEnvFiles)
	if err != nil {
		return nil, err
	}
	deny, err := runtimeInputDenyNames(root)
	if err != nil {
		return nil, err
	}
	// An agent env file configures the agent CLI itself. A name it sets that is
	// also a runtime input would be stripped silently, for example the
	// provider's own API key reused by the project: refuse the launch instead.
	for _, entry := range overlay {
		if name, _, _ := strings.Cut(entry, "="); deny[name] {
			return nil, fmt.Errorf("runtime_input_provider_collision:%s: an agent env file sets %s, which is declared as a runtime input and stripped from sessions; remove it from the env file, or rename the runtime input if the agent CLI needs it", name, name)
		}
	}
	return scrubDeniedNames(agentProcessEnv(env, agentID, generation), deny), nil
}

// runtimeInputDenyNames reads the project's reserved runtime-input names. A
// project without state has none; an unreadable state fails the attempt
// rather than launch unscrubbed.
func runtimeInputDenyNames(root string) (map[string]bool, error) {
	statePath := paths.New(root).StatePath()
	if _, err := os.Stat(statePath); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	state, err := db.For(statePath).Read()
	if err != nil {
		return nil, fmt.Errorf("read runtime-input declarations before launch: %w", err)
	}
	return models.RuntimeInputDenyNames(state), nil
}

// scrubRuntimeInputNames removes every runtime-input variable from an agent
// session environment (ADR-0169), so an ambient value cannot reach a session.
// It logs names only.
func scrubRuntimeInputNames(root string, env []string) ([]string, error) {
	deny, err := runtimeInputDenyNames(root)
	if err != nil {
		return nil, err
	}
	return scrubDeniedNames(env, deny), nil
}

func scrubDeniedNames(env []string, deny map[string]bool) []string {
	scrubbed := models.ScrubEnvironment(env, deny)
	if len(scrubbed) != len(env) {
		var removed []string
		for _, entry := range env {
			if name, _, _ := strings.Cut(entry, "="); deny[name] {
				removed = append(removed, name)
			}
		}
		GetLogger().Warn("Removed runtime-input variables from the session environment; deliver them through the runtime-input ledger", "names", strings.Join(removed, ","))
	}
	return scrubbed
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
		ProfileVars: config.ProfileVars, Model: config.Model, ProjectRoot: config.ProjectRoot,
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
	state, err := bb.ReadSnapshot()
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
