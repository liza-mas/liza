package agent

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/identity"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/pipeline"
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
	_, err := registerAgentWithAuthority(bb, projectRoot, agentID, role, terminal, leaseDuration, provider, resolver)
	return err
}

func registerAgentWithAuthority(bb *db.Blackboard, projectRoot, agentID, role, terminal string, leaseDuration int, provider string, resolver *pipeline.Resolver) (models.AgentAuthority, error) {
	var authority models.AgentAuthority
	err := ops.WithProjectLifecycleSharedLock(projectRoot, "agent-register", func() error {
		return ops.WithAgentLifecycleLock(context.Background(), projectRoot, agentID, "agent-register", func() error {
			var err error
			authority, err = registerAgentLocked(bb, projectRoot, agentID, role, terminal, leaseDuration, provider, resolver)
			return err
		})
	})
	return authority, err
}

func registerAgentLocked(bb *db.Blackboard, projectRoot, agentID, role, terminal string, leaseDuration int, provider string, resolver *pipeline.Resolver) (models.AgentAuthority, error) {
	logger := GetLogger()
	now := time.Now().UTC()
	leaseExpires := now.Add(time.Duration(leaseDuration) * time.Second)
	var authority models.AgentAuthority

	// Single atomic registration - skip STARTING state, go directly to IDLE
	err := bb.Modify(func(state *models.State) error {
		// Check for collision
		if existing, exists := state.Agents[agentID]; exists {
			observation := ops.AgentProcessOwnership(agentID, existing, now)
			if observation.Occupied() {
				return fmt.Errorf("%w; %s", &errors.AgentCollisionError{AgentID: agentID}, observation.Diagnostic(existing.PID))
			} else {
				logger.Info("Taking over expired or stale agent lease", "agent_id", agentID, "ownership_state", observation.Effective)
			}
		}

		// Singularity check via resolver: at most N instances per role.
		// For orchestrator roles, singularity is enforced by resolved type
		// (not role key) so that two different orchestrator role keys cannot
		// coexist. Non-orchestrator roles use per-role-key counting.
		if resolver != nil {
			maxInst, err := resolver.MaxInstances(role)
			if err == nil && maxInst > 0 {
				roleType, _ := resolver.RoleType(role)
				agentIDs := make([]string, 0, len(state.Agents))
				for id := range state.Agents {
					agentIDs = append(agentIDs, id)
				}
				sort.Strings(agentIDs)

				occupiedCount := 0
				var occupiedDetails []string
				for _, id := range agentIDs {
					if id == agentID {
						continue
					}
					agent := state.Agents[id]
					matchesRole := agent.Role == role
					if roleType == "orchestrator" {
						resolvedType, resolveErr := resolver.RoleType(agent.Role)
						matchesRole = resolveErr == nil && resolvedType == "orchestrator"
					}
					if !matchesRole {
						continue
					}
					if roleType == "orchestrator" {
						observation := ops.AgentProcessOwnership(id, agent, now)
						if !observation.Occupied() {
							continue
						}
						occupiedCount++
						occupiedDetails = append(occupiedDetails, fmt.Sprintf("%s: %s", id, observation.Diagnostic(agent.PID)))
						continue
					}
					observation := ops.AgentProcessOwnership(id, agent, now)
					if !observation.Occupied() {
						continue
					}
					occupiedCount++
					occupiedDetails = append(occupiedDetails, fmt.Sprintf("%s: %s", id, observation.Diagnostic(agent.PID)))
				}
				if occupiedCount >= maxInst {
					if roleType == "orchestrator" {
						return fmt.Errorf("type orchestrator already has %d live agent(s); only %d instance(s) allowed; %s",
							occupiedCount, maxInst, strings.Join(occupiedDetails, "; "))
					}
					return fmt.Errorf("role %s already has %d live agent(s) (max %d); only %d instance(s) allowed; %s",
						role, occupiedCount, maxInst, maxInst, strings.Join(occupiedDetails, "; "))
				}
			}
		}

		generation, err := models.NewAgentGeneration()
		if err != nil {
			return err
		}

		// Register agent directly as IDLE (atomic operation)
		pid := os.Getpid()
		state.Agents[agentID] = models.Agent{
			Role:         role,
			Status:       models.AgentStatusIdle,
			Generation:   generation,
			Heartbeat:    now,
			RegisteredAt: now,
			Terminal:     terminal,
			Provider:     provider,
			LeaseExpires: &leaseExpires,
			PID:          pid,
		}
		authority = models.AgentAuthority{ID: agentID, Generation: generation}

		return nil
	})

	if err != nil {
		return models.AgentAuthority{}, err
	}

	// If reviewer role: clear stale review claims
	if resolver != nil {
		roleType, rtErr := resolver.RoleType(role)
		if rtErr == nil && roleType == "reviewer" {
			if _, err := ops.ClearStaleReviewClaims(projectRoot); err != nil {
				logger.Warn("Failed to clear stale review claims", "error", err, "role", role)
			}
		}
	}

	return authority, nil
}

// AutoAssignAgentID reads state, picks the first available <role>-N, and calls
// tryFn with the candidate ID. On AgentCollisionError, it re-reads state and
// retries (up to maxRetries). Returns the assigned ID or the first non-collision error.
func AutoAssignAgentID(bb *db.Blackboard, role string, maxRetries int, tryFn func(agentID string) error) (string, error) {
	for attempt := range maxRetries {
		state, err := bb.ReadContextPatient(context.Background())
		if err != nil {
			return "", fmt.Errorf("failed to read state for agent ID auto-generation: %w", err)
		}
		now := time.Now()
		var activeIDs []string
		for id, a := range state.Agents {
			if a.LeaseExpires != nil && a.LeaseExpires.After(now) {
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
// the agent from state. Both operations happen in a single atomic modify
// so that an interrupt between them cannot leave a stuck task. The modify
// uses the patient lock wait: a supervisor usually exits because an ordinary
// wait timed out, and failing here too would strand its claim until the
// lease expires.
func unregisterAgent(bb *db.Blackboard, authority models.AgentAuthority, projectRoot string) error {
	now := time.Now().UTC()
	agentID := authority.ID

	// Load pipeline config outside the lock to avoid disk I/O under bb.Modify
	pipelineTransitions, resolver := loadPipelineForRelease(projectRoot)

	err := ops.ModifyWithAgentAuthority(bb.Patient(), authority, func(state *models.State) error {
		agent, exists := state.Agents[agentID]
		if !exists {
			return nil
		}

		// Release task claims from both agent-side and task-side ownership.
		taskIDs := make(map[string]bool)
		if agent.CurrentTask != nil {
			taskIDs[*agent.CurrentTask] = true
		}
		doerTaskIDs, reviewerTaskIDs := ops.TaskClaimsForAgent(state, agentID)
		for _, taskID := range doerTaskIDs {
			taskIDs[taskID] = true
		}
		for _, taskID := range reviewerTaskIDs {
			taskIDs[taskID] = true
		}
		for taskID := range taskIDs {
			if task := state.FindTask(taskID); task != nil {
				if err := releaseTaskClaim(state, task, agent.Role, agentID, pipelineTransitions, resolver, now); err != nil {
					return err
				}
			}
		}

		delete(state.Agents, agentID)
		return nil
	})

	return err
}

// releaseTaskClaim transitions a task back to its unclaimed status and clears
// the claim fields. It fails closed when an active claim cannot be safely
// released, so callers preserve both task-side and agent-side ownership.
// pipelineTransitions and resolver are pre-loaded by the caller (outside bb.Modify)
// to avoid disk I/O under the state lock.
// Uses resolver.RoleType() for doer/reviewer classification.
func releaseTaskClaim(state *models.State, task *models.Task, role, agentID string, pipelineTransitions map[models.TaskStatus][]models.TaskStatus, resolver *pipeline.Resolver, now time.Time) error {
	reason := "agent interrupted"

	activeExecuting, releasedInitial := ops.ResolveDoerReleaseStatus(task, resolver)

	resolveReviewerRelease := func() (models.TaskStatus, models.TaskStatus, error) {
		if resolver == nil {
			return "", "", fmt.Errorf("cannot release reviewer claim for task %s: pipeline resolver not loaded", task.ID)
		}
		return ops.ResolveReviewerReleaseStatus(task, resolver)
	}

	transitionTask := func(to models.TaskStatus) error {
		if pipelineTransitions == nil {
			return fmt.Errorf("cannot transition task %s on claim release: pipeline transitions not loaded", task.ID)
		}
		if err := task.TransitionWith(to, pipelineTransitions); err != nil {
			return fmt.Errorf("transition task %s on claim release: %w", task.ID, err)
		}
		return nil
	}

	// Classify role using resolver for doer/reviewer determination.
	roleType := ""
	if resolver != nil {
		if rt, err := resolver.RoleType(role); err == nil {
			roleType = rt
		}
	}

	switch roleType {
	case "doer":
		if task.Status == activeExecuting {
			if err := transitionTask(releasedInitial); err != nil {
				return err
			}
		}
		task.AssignedTo = nil
		task.LeaseExpires = nil
		models.AdvanceLifecycle(task)

	case "reviewer":
		if task.ReviewingBy != nil && *task.ReviewingBy == agentID {
			activeReviewing, releasedSubmitted, err := resolveReviewerRelease()
			if err != nil {
				return err
			}
			if task.Status == activeReviewing {
				if err := transitionTask(releasedSubmitted); err != nil {
					return err
				}
			}
			task.ReviewingBy = nil
			task.ReviewLeaseExpires = nil
			models.AdvanceLifecycle(task)
		}

	default:
		if task.AssignedTo != nil && *task.AssignedTo == agentID {
			if task.Status == activeExecuting {
				if err := transitionTask(releasedInitial); err != nil {
					return err
				}
			}
			task.AssignedTo = nil
			task.LeaseExpires = nil
			models.AdvanceLifecycle(task)
		} else if task.ReviewingBy != nil && *task.ReviewingBy == agentID {
			activeReviewing, releasedSubmitted, err := resolveReviewerRelease()
			if err != nil {
				return err
			}
			if task.Status == activeReviewing {
				if err := transitionTask(releasedSubmitted); err != nil {
					return err
				}
			}
			task.ReviewingBy = nil
			task.ReviewLeaseExpires = nil
			models.AdvanceLifecycle(task)
		} else {
			return nil
		}
	}

	state.ReleaseAgent(agentID)

	task.History = append(task.History, models.TaskHistoryEntry{
		Time:   now,
		Event:  models.TaskEventClaimReleased,
		Agent:  &agentID,
		Reason: &reason,
	})
	return nil
}

// loadPipelineForRelease loads pipeline resolver and transitions, logging
// warnings on failure. Returns nil values when pipeline config is unreadable.
func loadPipelineForRelease(projectRoot string) (map[models.TaskStatus][]models.TaskStatus, *pipeline.Resolver) {
	if projectRoot == "" {
		return nil, nil
	}
	cfg, err := pipeline.LoadFrozen(projectRoot)
	if err != nil {
		GetLogger().Warn("Failed to load pipeline config for claim release", "error", err)
		return nil, nil
	}
	resolver := pipeline.NewResolver(cfg)
	return ops.BuildPipelineTransitions(resolver), resolver
}

// resetAgentToIdle resets an agent's status to IDLE and clears CurrentTask
func resetAgentToIdle(bb *db.Blackboard, authority models.AgentAuthority) error {
	now := time.Now().UTC()
	agentID := authority.ID

	return ops.ModifyWithAgentAuthority(bb, authority, func(state *models.State) error {
		agent, exists := state.Agents[agentID]
		if !exists {
			return &errors.NotFoundError{Entity: "agent", ID: agentID}
		}

		agent.Status = models.AgentStatusIdle
		agent.CurrentTask = nil
		agent.Heartbeat = now

		state.Agents[agentID] = agent
		return nil
	})
}

// resetAgentAfterExit clears transient runtime states after CLI exit while preserving
// explicit command-driven states that are meaningful between loops.
func resetAgentAfterExit(bb *db.Blackboard, authority models.AgentAuthority, projectRoot string) error {
	now := time.Now().UTC()
	agentID := authority.ID

	// Load pipeline config outside the lock to avoid disk I/O under bb.Modify
	pipelineTransitions, resolver := loadPipelineForRelease(projectRoot)

	return ops.ModifyWithAgentAuthority(bb, authority, func(state *models.State) error {
		agent, exists := state.Agents[agentID]
		if !exists {
			return &errors.NotFoundError{Entity: "agent", ID: agentID}
		}

		switch agent.Status {
		case models.AgentStatusWaiting:
			// A session never resumes after exit, so a WAITING agent holding a
			// task has nothing to come back to: fall through and release the
			// claim rather than pinning the task to a departed agent.
			if agent.CurrentTask != nil {
				task := state.FindTask(*agent.CurrentTask)
				if task != nil && task.ReviewingBy != nil && *task.ReviewingBy == agentID {
					return releaseTaskClaim(state, task, agent.Role, agentID, pipelineTransitions, resolver, now)
				}
			}
		case models.AgentStatusHandoff:
			if agent.CurrentTask != nil {
				agent.Heartbeat = now
				state.Agents[agentID] = agent
				return nil
			}
			// CurrentTask already cleared — fall through to reset to IDLE
		}

		// Release any held task claim before clearing CurrentTask
		if agent.CurrentTask != nil {
			if task := state.FindTask(*agent.CurrentTask); task != nil {
				if err := releaseTaskClaim(state, task, agent.Role, agentID, pipelineTransitions, resolver, now); err != nil {
					return err
				}
			}
		}

		agent.Status = models.AgentStatusIdle
		agent.CurrentTask = nil
		agent.Heartbeat = now
		state.Agents[agentID] = agent
		return nil
	})
}

// setAgentToOrchestratingStatus sets an orchestrator agent's status to PLANNING
func setAgentToOrchestratingStatus(bb *db.Blackboard, authority models.AgentAuthority) error {
	now := time.Now().UTC()
	agentID := authority.ID

	return ops.ModifyWithAgentAuthority(bb, authority, func(state *models.State) error {
		agent, exists := state.Agents[agentID]
		if !exists {
			return &errors.NotFoundError{Entity: "agent", ID: agentID}
		}

		// Set to PLANNING state
		agent.Status = models.AgentStatusPlanning
		planning := "planning"
		agent.CurrentTask = &planning
		agent.Heartbeat = now

		// Renew lease
		leaseDuration := state.Config.LeaseDuration
		if leaseDuration <= 0 {
			leaseDuration = models.DefaultLeaseDurationSeconds
		}
		leaseExpires := now.Add(time.Duration(leaseDuration) * time.Second)
		agent.LeaseExpires = &leaseExpires

		state.Agents[agentID] = agent
		return nil
	})
}
