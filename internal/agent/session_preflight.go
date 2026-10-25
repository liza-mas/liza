package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/liza-mas/liza/internal/agentlaunch"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/sessionvalidation"
)

// resolveLaunchEnvironment refreshes configured overlays once. Subsequent
// subprocesses consume this snapshot, including executable lookup against PATH.
// It never carries the system-prompt bypass: an injected parent would otherwise
// open the gate for every session it starts. CLI launches that inject re-add it.
func resolveLaunchEnvironment(plan agentlaunch.LaunchPlan, root, agentID, generation string, frozen []string) ([]string, error) {
	if frozen != nil {
		env, err := scrubRuntimeInputNames(root, append([]string{}, frozen...))
		return systemPromptContractsEnv(env, false), err
	}
	env, err := sessionvalidation.ResolveEnvironment(os.Environ(), root, plan.EnvFiles, plan.OptionalEnvFiles)
	if err != nil {
		return nil, err
	}
	overlay, err := sessionvalidation.ResolveEnvironment(nil, root, plan.EnvFiles, plan.OptionalEnvFiles)
	if err != nil {
		return nil, err
	}
	state, err := withheldNameState(root)
	if err != nil {
		return nil, err
	}
	deny := models.ReservedEnvNames(state)
	// An agent env file configures the agent CLI itself. A name it sets that is
	// also withheld would be stripped silently, for example the provider's own
	// API key reused by the project: refuse the launch instead.
	for _, entry := range overlay {
		name, _, _ := strings.Cut(entry, "=")
		if models.EnvNameIn(models.RuntimeInputDenyNames(state), name) {
			return nil, fmt.Errorf("runtime_input_provider_collision:%s: an agent env file sets %s, which is declared as a runtime input and stripped from sessions; remove it from the env file, or rename the runtime input if the agent CLI needs it", name, name)
		}
		if models.EnvNameIn(deny, name) {
			return nil, fmt.Errorf("agent_env_denied:%s: an agent env file sets %s, which config.agent_env_deny withholds from sessions; authenticate the agent CLI another way, or remove the name from the deny list", name, name)
		}
	}
	env = withClaudeCommandCeiling(plan, env)
	return systemPromptContractsEnv(scrubDeniedNames(agentProcessEnv(env, agentID, generation), deny), false), nil
}

// claudeCommandCeilingName caps the timeout a Claude Bash call may request,
// which is also the lifetime of a command started with run_in_background.
const claudeCommandCeilingName = "BASH_MAX_TIMEOUT_MS"

// withClaudeCommandCeiling lets a Claude session's command live as long as the
// supervisor lets the agent's own submit run (ADR-0176). Otherwise the provider
// stops a legitimate up-to-4 h canonical batch at its 2 h default ceiling
// (D-90). A larger value already set is kept; the default timeout of ordinary
// commands is left alone, so commands that ask for nothing stay bounded.
func withClaudeCommandCeiling(plan agentlaunch.LaunchPlan, env []string) []string {
	if plan.ProviderKey != "claude" && plan.ToolName != "claude" {
		return env
	}
	ceiling := ops.MaxInflightSubmitGrace.Milliseconds()
	if value, ok := envLookup(env, claudeCommandCeilingName); ok {
		if current, err := strconv.ParseInt(value, 10, 64); err == nil && current > ceiling {
			ceiling = current
		}
	}
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, claudeCommandCeilingName+"=") {
			out = append(out, entry)
		}
	}
	return append(out, claudeCommandCeilingName+"="+strconv.FormatInt(ceiling, 10))
}

// withheldNameState reads the state that declares withheld names: runtime
// inputs and config.agent_env_deny. A project without state withholds
// nothing (nil state); an unreadable state fails the attempt rather than
// launch unscrubbed.
func withheldNameState(root string) (*models.State, error) {
	statePath := paths.New(root).StatePath()
	if _, err := os.Stat(statePath); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	// This is declaration discovery, not mutation authorization. Writers
	// publish complete snapshots atomically; launch gates separately revalidate
	// the task and session contract under the state lock before provider start.
	state, err := db.For(statePath).ReadSnapshot()
	if err != nil {
		return nil, fmt.Errorf("read withheld variable names before launch: %w", err)
	}
	return state, nil
}

// scrubRuntimeInputNames removes every withheld variable (runtime inputs,
// ADR-0169, and config.agent_env_deny, ADR-0213) from an agent session
// environment, so an ambient value cannot reach a session. It logs names only.
func scrubRuntimeInputNames(root string, env []string) ([]string, error) {
	state, err := withheldNameState(root)
	if err != nil {
		return nil, err
	}
	return scrubDeniedNames(env, models.ReservedEnvNames(state)), nil
}

func scrubDeniedNames(env []string, deny map[string]bool) []string {
	scrubbed := models.ScrubEnvironment(env, deny)
	if len(scrubbed) != len(env) {
		var removed []string
		for _, entry := range env {
			if name, _, _ := strings.Cut(entry, "="); models.EnvNameIn(deny, name) {
				removed = append(removed, name)
			}
		}
		GetLogger().Warn("Removed withheld variables (runtime inputs or config.agent_env_deny) from the session environment", "names", strings.Join(removed, ","))
	}
	return scrubbed
}

// validateIdentityAndWithholdEnvironment validates the supervisor identity,
// then removes config.agent_env_deny names from the supervisor's own
// environment before anything starts a process, so no session, setup command,
// merge test or index tool inherits them (ADR-0213).
func validateIdentityAndWithholdEnvironment(agentID, role, root string) error {
	if err := validateIdentity(agentID, role); err != nil {
		return err
	}
	withheld, err := ops.WithholdDeniedEnvironment(root)
	if err != nil {
		return err
	}
	if len(withheld) > 0 {
		GetLogger().Info("Withheld config.agent_env_deny variables from this supervisor", "names", strings.Join(withheld, ","))
	}
	return nil
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
	plan, err := agentlaunch.ResolveLaunchPlan(agentlaunch.LaunchPlanRequest{
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
