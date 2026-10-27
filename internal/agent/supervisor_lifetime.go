package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/roles"
)

// surplusRetryDelay paces the retry after a locked surplus recheck disagreed
// with the snapshot, so a persistent disagreement cannot spin.
const surplusRetryDelay = 5 * time.Second

// supervisorLifetime is RunSupervisor's private, unpersisted record of one
// supervisor lifetime: the context, authority, strategy, trackers and cleanup
// state its setup, turns and shutdown share. It confers no authority; every
// mutation still carries config.Authority.
type supervisorLifetime struct {
	ctx           context.Context // the caller's; its cancellation is a signal, not a failure
	supervisorCtx context.Context // also cancelled when the heartbeat stops
	cancel        context.CancelFunc
	config        SupervisorConfig
	bb            *db.Blackboard
	resolver      *pipeline.Resolver
	roleType      roles.RoleType
	strategy      RoleStrategy
	authority     models.AgentAuthority

	// retired is set once releaseIfSurplus has removed this registration; the
	// deferred unregister would then fail its authority check on the missing row.
	retired        bool
	heartbeatErrCh chan error
	stopHeartbeat  func()
	// turnCleanups cancel the bounded cleanup contexts that replaced bb after
	// the supervisor context ended; they run when the lifetime ends.
	turnCleanups []context.CancelFunc

	pollInterval     time.Duration
	maxWait          time.Duration
	initialTaskSpent bool
	trackers         loopTrackers
}

// loopTrackers hold the in-memory restart, spin and failure budgets that the
// supervisor loop charges and resets per task.
type loopTrackers struct {
	exit42             *exit42RestartTracker
	crash              *crashRestartTracker
	spin               *spinningTracker
	runtimeFailure     *runtimeFailureTracker
	backgroundTask     *runtimeFailureTracker
	consecutiveCrashes int // across tasks; any exit 0 or 42 resets it
}

func newLoopTrackers() loopTrackers {
	return loopTrackers{
		exit42:         newExit42RestartTracker(),
		crash:          newCrashRestartTracker(),
		spin:           newSpinningTracker(),
		runtimeFailure: newRuntimeFailureTracker(),
		backgroundTask: newRuntimeFailureTracker(),
	}
}

// newSupervisorLifetime validates the identity, withholds denied host
// variables (ADR-0213) and resolves the role type from the frozen pipeline.
func newSupervisorLifetime(ctx, supervisorCtx context.Context, cancel context.CancelFunc, config SupervisorConfig) (*supervisorLifetime, error) {
	lt := &supervisorLifetime{
		ctx:           ctx,
		supervisorCtx: supervisorCtx,
		cancel:        cancel,
		config:        config,
		bb:            db.For(config.StatePath).WithLockRetryContext(supervisorCtx),
		trackers:      newLoopTrackers(),
	}
	if err := validateIdentityAndWithholdEnvironment(config.AgentID, config.Role, config.ProjectRoot); err != nil {
		return nil, err
	}

	// Load pipeline resolver for role type classification.
	// The resolver reads role definitions from the pipeline YAML,
	// enabling custom YAML-defined roles without Go code changes.
	pipelineCfg, pipelineErr := pipeline.LoadFrozen(config.ProjectRoot)
	if pipelineErr != nil {
		return nil, fmt.Errorf("loading pipeline config for strategy selection: %w", pipelineErr)
	}
	lt.resolver = pipeline.NewResolver(pipelineCfg)

	roleType, err := lt.resolver.RoleType(config.Role)
	if err != nil {
		return nil, fmt.Errorf("resolving role type for %q: %w", config.Role, err)
	}
	lt.roleType = roleType
	return lt, nil
}

func (lt *supervisorLifetime) isOrchestrator() bool {
	return lt.roleType == roles.RoleTypeOrchestrator
}

// selectStrategy picks the role strategy, applies the pipeline's YAML timeouts
// and replays the verdicts reached before this registration.
func (lt *supervisorLifetime) selectStrategy() error {
	strategy, err := NewRoleStrategy(lt.config.Role, lt.resolver)
	if err != nil {
		return err
	}
	lt.strategy = strategy

	// Apply YAML-sourced timeouts from pipeline config to the strategy.
	if timeouts, tErr := lt.resolver.RoleTimeouts(lt.config.Role); tErr == nil {
		ApplyYAMLTimeouts(strategy, timeouts.Execution, timeouts.PollInterval, timeouts.MaxWait)
	}

	if err := ops.ReplayPendingVerdicts(lt.supervisorCtx, lt.config.ProjectRoot, lt.config.AgentID); err != nil {
		return fmt.Errorf("replay reached verdicts before registration: %w", err)
	}
	return nil
}

// register records the agent and adopts its authority.
func (lt *supervisorLifetime) register() error {
	config := lt.config
	authority, err := registerAgentWithAuthority(lt.bb, config.ProjectRoot, config.AgentID, config.Role, "terminal-1", 1800, config.CLIName, config.Model, lt.resolver)
	if err != nil {
		return err
	}
	lt.authority = authority
	lt.config.Authority = authority
	// Registration enforces orchestrator singularity, so only the registered
	// orchestrator repairs hooks; a rejected duplicate launch never gets here.
	if lt.isOrchestrator() {
		ensureProjectRootIndexActivation(lt.bb, config.ProjectRoot)
	} else {
		lt.config.poolSurplus = func(state *models.State) bool {
			return ops.PoolSurplus(state, lt.resolver, authority.ID, time.Now().UTC())
		}
	}
	return nil
}

// unregister removes the registration at shutdown, unless surplus retirement
// already did.
func (lt *supervisorLifetime) unregister() {
	if lt.retired {
		return
	}
	// Shutdown cleanup has an independent bounded context.
	cleanupBB, cancelCleanup := supervisorCleanupBlackboard(lt.config.StatePath)
	defer cancelCleanup()
	if err := unregisterAgent(cleanupBB, lt.authority, lt.config.ProjectRoot); err != nil {
		GetLogger().Warn("Failed to unregister agent", "error", err, "agent_id", lt.authority.ID)
	}
}

// start begins the supervisor-lifetime heartbeat, which keeps the lease alive
// across the entire loop (including IDLE wait-for-work periods, not just
// during CLI execution). Without it an IDLE agent's lease can expire, causing
// auto-assigned ID collision with new agents. It also fixes the wait and
// execution budgets from the state it read.
func (lt *supervisorLifetime) start() error {
	state, err := lt.bb.ReadSnapshot()
	if err != nil {
		return fmt.Errorf("failed to read state: %w", err)
	}

	lt.heartbeatErrCh = make(chan error, 1)
	lt.config.sessionActivity = &providerSessionActivity{}

	hb := NewHeartbeat(HeartbeatConfig{
		Authority:          lt.config.Authority,
		StatePath:          lt.config.StatePath,
		State:              state,
		ActiveProviderTask: lt.config.sessionActivity.currentTask,
	})
	lt.stopHeartbeat = startSupervisorHeartbeat(lt.supervisorCtx, hb.Start, func(err error) {
		GetLogger().Error("Heartbeat error", "error", err, "agent_id", lt.config.AgentID)
		select {
		case lt.heartbeatErrCh <- err:
		default:
		}
		lt.cancel()
	})

	lt.pollInterval, lt.maxWait = lt.strategy.WaitConfig(state)

	// Set execution timeout if not configured
	if lt.config.ExecutionTimeout == 0 {
		lt.config.ExecutionTimeout = lt.strategy.DefaultTimeout()
	}
	if lt.config.ExecutionProgressTimeout == 0 {
		lt.config.ExecutionProgressTimeout = effectiveAgentProgressTimeout(state.Config)
	}
	return nil
}

// shutdown stops the heartbeat and releases the cleanup contexts of turns that
// ended after cancellation.
func (lt *supervisorLifetime) shutdown() {
	for _, cancel := range lt.turnCleanups {
		cancel()
	}
	if lt.stopHeartbeat != nil {
		lt.stopHeartbeat()
	}
}

// switchToCleanupBlackboard moves the rest of the loop to an independent
// bounded context once cancellation ended the supervisor's own.
func (lt *supervisorLifetime) switchToCleanupBlackboard() {
	cleanupBB, cancelCleanup := supervisorCleanupBlackboard(lt.config.StatePath)
	lt.turnCleanups = append(lt.turnCleanups, cancelCleanup)
	lt.bb = cleanupBB
}

func (lt *supervisorLifetime) checkHeartbeat() error {
	select {
	case err := <-lt.heartbeatErrCh:
		return fmt.Errorf("heartbeat stopped for agent %s: %w", lt.config.AgentID, err)
	default:
		return nil
	}
}

func (lt *supervisorLifetime) waitForDelay(delay time.Duration) error {
	timer := newSupervisorDelayTimer(delay)
	defer timer.Stop()

	select {
	case <-lt.supervisorCtx.Done():
		if hbErr := lt.checkHeartbeat(); hbErr != nil {
			return hbErr
		}
		if lt.ctx.Err() != nil {
			return nil
		}
		return lt.supervisorCtx.Err()
	case <-timer.C:
		return nil
	}
}

// retireIfSurplus removes this registration when the role is over its
// max-instances and the agent holds no in-flight work. A failed check
// leaves the registration and claims untouched, so the agent stays.
func (lt *supervisorLifetime) retireIfSurplus() bool {
	if lt.config.poolSurplus == nil {
		return false
	}
	released, err := ops.RetireSurplusAgent(lt.bb, lt.authority, lt.resolver)
	if err != nil {
		GetLogger().Warn("Surplus retirement check failed; staying in pool", "error", err, "agent_id", lt.config.AgentID)
		return false
	}
	if released {
		GetLogger().Info("Role over max-instances, supervisor exiting", "agent_id", lt.config.AgentID, "role", lt.config.Role)
	}
	return released
}
