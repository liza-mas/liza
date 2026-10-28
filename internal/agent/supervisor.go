package agent

import (
	"context"
	"time"

	"github.com/liza-mas/liza/internal/agentlaunch"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/providersignal"
)

// SupervisorConfig contains all configuration for the agent supervisor
type SupervisorConfig struct {
	AgentID          string
	Authority        models.AgentAuthority
	Role             string // runtime role name from pipeline YAML (e.g. "coder", "code-reviewer", "orchestrator")
	ProjectRoot      string
	StatePath        string
	LogPath          string
	SpecsDir         string // For prompt building
	CLIName, Model   string // CLI ("claude", "codex", ...); first-class model, "" for the tool default
	ProfileName      string // Optional structured launch profile name
	ProfileVars      map[string]string
	Interactive      bool          // Print prompt location, don't execute
	InitialTask      string        // Optional task ID the first claim targets; a successful claim spends it
	LLMAgent         LLMAgent      // Agent execution backend (CLI-based or future ACP-based)
	Executor         CLIExecutor   // Legacy field name, preserved for backward compatibility. Deprecated: Use LLMAgent.
	ExecutionTimeout time.Duration // Max time for agent execution before timeout
	// ExecutionProgressTimeout is the maximum time an executing task may go
	// without observable state, worktree, or provider-output progress.
	ExecutionProgressTimeout time.Duration
	sessionActivity          *providerSessionActivity
	// poolSurplus is set after registration for non-orchestrator roles; a
	// waiting strategy that passes it returns errPoolSurplus when it holds.
	poolSurplus surplusCheckFunc
}

var waitWhilePausedForSupervisor = waitWhilePaused
var newSupervisorDelayTimer = time.NewTimer // claim-failure pacing; overridable in tests

// claimFailureDelay paces the retry after a claim failure no breaker owns.
const claimFailureDelay = 5 * time.Second

// readStateSnapshot is the lock-free read for supervisor observation and gate
// paths (D100): it returns ctx's error when already cancelled, as the locked
// ReadContext it replaces did, then reads the last published state.
func readStateSnapshot(ctx context.Context, bb *db.Blackboard) (*models.State, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return bb.ReadSnapshot()
}

// promptFailureBlockTarget names the task a prompt-context build failure must
// block. Doers report the task they claimed; reviewers hold a review claim
// rather than a doer claim, so claimedTaskID is empty for them and the task
// under review is the target. Without a target the failure escapes the
// supervisor loop and ends the session, and auto-repair respawns into the same
// failure instead of surfacing a BLOCKED task carrying the reason.
//
// This currently equals effectiveTask for all three strategies, and the
// narrowing is deliberate: blocking is an ownership-bearing write, so a future
// strategy that reports a taskID it does not own must opt in here rather than
// inherit the fallback.
func promptFailureBlockTarget(strategy RoleStrategy, claimedTaskID, taskID string) string {
	if claimedTaskID != "" {
		return claimedTaskID
	}
	if _, isReviewer := strategy.(*reviewerStrategy); isReviewer {
		return taskID
	}
	return ""
}

func successfulTurnProgressSignature(_, _, taskSnapshot string) string {
	return taskSnapshot
}

// --- Effective config helpers ---

func effectiveAgentProgressTimeout(cfg models.Config) time.Duration {
	seconds := cfg.AgentProgressTimeout
	if seconds <= 0 {
		seconds = models.DefaultAgentProgressTimeoutSec
	}
	return time.Duration(seconds) * time.Second
}

// RunSupervisor is the main entry point for the agent supervisor. It orders the
// lifetime (identity, strategy, registration, heartbeat), then loops over turns
// until an exit condition; its deferred cleanup runs in reverse order of setup.
func RunSupervisor(ctx context.Context, config SupervisorConfig) error {
	supervisorCtx, cancelSupervisor := context.WithCancel(ctx)
	defer cancelSupervisor()

	lt, err := newSupervisorLifetime(ctx, supervisorCtx, cancelSupervisor, config)
	if err != nil {
		return err
	}
	if lt.isOrchestrator() {
		defer drainPendingCheckpointSummary(ctx, lt.bb, config.ProjectRoot)
	}
	if err := lt.selectStrategy(); err != nil {
		return err
	}
	if err := lt.register(); err != nil {
		return err
	}
	defer lt.unregister()
	if err := lt.start(); err != nil {
		return err
	}
	defer lt.shutdown()

	for {
		if flow, err := lt.iterate(); flow == flowExit {
			return err
		}
	}
}

// providerKeysOfConfig derives the provider keys policy compares from the
// catalog-derived tool registry under cfg, outside any blackboard transaction.
func providerKeysOfConfig(cfg models.Config) func() providersignal.MatchKeys {
	return func() providersignal.MatchKeys {
		return providersignal.NewMatchKeys(agentlaunch.BuiltInAgentTools(), cfg)
	}
}

// providerKeysOfSnapshot is providerKeysOfConfig for the configuration of the
// last published state, read only when the keys are needed. An unreadable
// snapshot compares under the catalog alone.
func providerKeysOfSnapshot(ctx context.Context, bb *db.Blackboard) func() providersignal.MatchKeys {
	return func() providersignal.MatchKeys {
		var cfg models.Config
		if bb != nil {
			if state, err := readStateSnapshot(ctx, bb); err == nil && state != nil {
				cfg = state.Config
			}
		}
		return providerKeysOfConfig(cfg)()
	}
}

func handleQuotaSignal(config SupervisorConfig) bool {
	if !providersignal.CheckQuotaSignal(config.ProjectRoot, config.CLIName) {
		return false
	}

	GetLogger().Info("Provider quota exhausted, shutting down",
		"provider", config.CLIName,
		"agent_id", config.AgentID)
	return true
}

// handleProviderUnavailableSignal reports whether a provider-unavailable record
// names a tool sharing this supervisor's provider key; keys is read only when a
// record exists.
func handleProviderUnavailableSignal(config SupervisorConfig, keys func() providersignal.MatchKeys) bool {
	if !providersignal.CheckProviderUnavailableSignal(config.ProjectRoot, config.CLIName, keys) {
		return false
	}

	GetLogger().Info("Provider unavailable, shutting down",
		"provider", config.CLIName,
		"agent_id", config.AgentID)
	return true
}

func handleClassifiedProviderCrash(config SupervisorConfig, output string) bool {
	if pu := providersignal.DetectProviderUnavailable(output, config.CLIName); pu != nil {
		GetLogger().Error("Provider unavailable, terminating",
			"provider", pu.Provider,
			"agent_id", config.AgentID,
			"message", pu.Message)
		if alertErr := providersignal.LogProviderUnavailableAlert(config.ProjectRoot, pu); alertErr != nil {
			GetLogger().Warn("Failed to write provider unavailable alert", "error", alertErr)
		}
		if err := providersignal.WriteProviderUnavailableSignal(config.ProjectRoot, pu); err != nil {
			GetLogger().Warn("Failed to write provider unavailable signal", "error", err)
		}
		return true
	}

	if qe := providersignal.DetectQuotaExhaustion(output, config.CLIName); qe != nil {
		GetLogger().Error("Provider quota exhausted, terminating",
			"provider", qe.Provider,
			"agent_id", config.AgentID,
			"message", qe.Message)
		if err := providersignal.RaiseQuotaExhaustion(config.ProjectRoot, qe); err != nil {
			GetLogger().Warn("Failed to raise quota exhaustion", "error", err)
		}
		return true
	}

	return false
}
