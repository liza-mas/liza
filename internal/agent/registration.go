package agent

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/identity"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/roles"
)

// validateIdentity validates agent ID format: {role}-{number}
func validateIdentity(agentID, role string) error {
	if agentID == "" {
		return fmt.Errorf("agent ID required")
	}

	lastHyphen := -1
	for i := len(agentID) - 1; i >= 0; i-- {
		if agentID[i] == '-' {
			lastHyphen = i
			break
		}
	}

	if lastHyphen == -1 {
		return fmt.Errorf("invalid agent ID format (expected {role}-{number}): %s", agentID)
	}

	idRole := agentID[:lastHyphen]
	numStr := agentID[lastHyphen+1:]

	if _, err := strconv.Atoi(numStr); err != nil {
		return fmt.Errorf("agent ID suffix must be numeric: %s", agentID)
	}

	if idRole != role {
		return fmt.Errorf("agent ID role mismatch (ID=%s, config=%s)", idRole, role)
	}

	return nil
}

// registerAgent registers an agent with collision detection.
// provider identifies the CLI provider (e.g. "claude", "codex") and is persisted
// for review quorum provider-diversity checks.
// resolver is used for role classification (singularity, reviewer detection).
func registerAgent(bb *db.Blackboard, projectRoot, agentID, role, terminal string, leaseDuration int, provider string, resolver *pipeline.Resolver) error {
	_, err := registerAgentWithAuthority(bb, projectRoot, agentID, role, terminal, leaseDuration, provider, "", resolver)
	return err
}

// model is the first-class model the agent launches with, empty for the
// tool default; it is recorded for provenance only.
func registerAgentWithAuthority(bb *db.Blackboard, projectRoot, agentID, role, terminal string, leaseDuration int, provider, model string, resolver *pipeline.Resolver) (models.AgentAuthority, error) {
	var authority models.AgentAuthority
	err := ops.WithProjectLifecycleSharedLock(projectRoot, "agent-register", func() error {
		return ops.WithAgentLifecycleLock(context.Background(), projectRoot, agentID, "agent-register", func() error {
			var err error
			authority, err = registerAgentLocked(bb, projectRoot, agentID, role, terminal, leaseDuration, provider, model, resolver)
			return err
		})
	})
	return authority, err
}

func registerAgentLocked(bb *db.Blackboard, projectRoot, agentID, role, terminal string, leaseDuration int, provider, model string, resolver *pipeline.Resolver) (models.AgentAuthority, error) {
	logger := GetLogger()

	// Single atomic registration - skip STARTING state, go directly to IDLE
	registered, err := ops.RegisterAgent(bb, ops.RegisterAgentInput{
		AgentID:              agentID,
		Role:                 role,
		Terminal:             terminal,
		Provider:             provider,
		Model:                model,
		LeaseDurationSeconds: leaseDuration,
		Resolver:             resolver,
	})
	if err != nil {
		return models.AgentAuthority{}, err
	}
	if registered.TookOver {
		logger.Info("Taking over expired or stale agent lease", "agent_id", agentID, "ownership_state", registered.PreviousOwnership)
	}
	if registered.SweepErr != nil {
		logger.Warn("Failed to sweep liveness records", "error", registered.SweepErr, "agent_id", agentID)
	}

	// If reviewer role: clear stale review claims
	if resolver != nil {
		roleType, rtErr := resolver.RoleType(role)
		if rtErr == nil && roleType == roles.RoleTypeReviewer {
			if _, err := ops.ClearStaleReviewClaims(projectRoot); err != nil {
				logger.Warn("Failed to clear stale review claims", "error", err, "role", role)
			}
		}
	}

	return registered.Authority, nil
}

// AutoAssignAgentID reads state, picks the first available <role>-N, and calls
// tryFn with the candidate ID. On AgentCollisionError, it re-reads state and
// retries (up to maxRetries). Returns the assigned ID or the first non-collision error.
func AutoAssignAgentID(bb *db.Blackboard, role string, maxRetries int, tryFn func(agentID string) error) (string, error) {
	for attempt := range maxRetries {
		state, err := bb.ReadSnapshot()
		if err != nil {
			return "", fmt.Errorf("failed to read state for agent ID auto-generation: %w", err)
		}
		now := time.Now()
		// A superset of registration's Occupied, so no candidate is one the
		// collision check would refuse.
		livenessWindow := models.AgentLivenessWindow(state.Config)
		var activeIDs []string
		for id, a := range state.Agents {
			if models.AgentRegistrationLive(a, now, livenessWindow) {
				activeIDs = append(activeIDs, id)
			}
		}
		agentID := identity.NextAvailableID(role, activeIDs)
		err = tryFn(agentID)
		if errors.IsAgentCollision(err) && attempt < maxRetries-1 {
			continue
		}
		return agentID, err
	}
	return "", fmt.Errorf("exhausted %d retries for agent ID auto-assignment", maxRetries)
}

// unregisterAgent releases any task claim held by the agent, then removes
// the agent from state in one atomic operation. It uses the patient lock wait:
// a supervisor usually exits because an ordinary wait timed out, and failing
// here too would strand its claim until the lease expires.
func unregisterAgent(bb *db.Blackboard, authority models.AgentAuthority, projectRoot string) error {
	// Load pipeline config outside the lock to avoid disk I/O under the state lock
	return ops.UnregisterAgent(bb.Patient(), authority, loadPipelineForRelease(projectRoot))
}

// loadPipelineForRelease loads pipeline resolver and transitions, logging
// warnings on failure. Returns nil parts when pipeline config is unreadable.
func loadPipelineForRelease(projectRoot string) ops.ClaimReleasePipeline {
	if projectRoot == "" {
		return ops.ClaimReleasePipeline{}
	}
	cfg, err := pipeline.LoadFrozen(projectRoot)
	if err != nil {
		GetLogger().Warn("Failed to load pipeline config for claim release", "error", err)
		return ops.ClaimReleasePipeline{}
	}
	resolver := pipeline.NewResolver(cfg)
	return ops.ClaimReleasePipeline{Transitions: ops.BuildPipelineTransitions(resolver), Resolver: resolver}
}

// resetAgentAfterExit clears transient runtime states after CLI exit while preserving
// explicit command-driven states that are meaningful between loops.
func resetAgentAfterExit(bb *db.Blackboard, authority models.AgentAuthority, projectRoot string) error {
	_, err := releaseAgentAfterExit(bb, authority, projectRoot)
	return err
}

// releaseAgentAfterExit is resetAgentAfterExit reporting the claim it
// released, if any.
func releaseAgentAfterExit(bb *db.Blackboard, authority models.AgentAuthority, projectRoot string) (ops.ReleasedTaskClaim, error) {
	// Load pipeline config outside the lock to avoid disk I/O under the state lock
	return ops.ReleaseAgentAfterExit(bb, authority, loadPipelineForRelease(projectRoot))
}

// supervisorCleanupBlackboard lets exit bookkeeping finish after cancellation,
// with the same bounded acquisition budget as deferred unregistration.
func supervisorCleanupBlackboard(statePath string) (*db.Blackboard, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	return db.For(statePath).WithLockRetryContext(ctx), cancel
}

// releaseUnstartedTurn releases a halted/contended start or a lost review claim.
func releaseUnstartedTurn(bb *db.Blackboard, config SupervisorConfig, taskID string, halted, contended bool) error {
	if halted {
		GetLogger().Info("Provider start refused by halt, releasing claim", "agent_id", config.AgentID, "task_id", taskID)
		if err := ops.ReleaseClaimForHalt(bb, config.Authority, loadPipelineForRelease(config.ProjectRoot), taskID); err != nil {
			return fmt.Errorf("release claim after halted launch: %w", err)
		}
		return nil
	}
	message := "Review ownership lost, checking for more work"
	if contended {
		message = "Provider start contended, checking for more work"
	}
	GetLogger().Info(message, "agent_id", config.AgentID, "task_id", taskID)
	if err := resetAgentAfterExit(bb, config.Authority, config.ProjectRoot); err != nil {
		return fmt.Errorf("reset after uncompleted provider turn: %w", err)
	}
	return nil
}
