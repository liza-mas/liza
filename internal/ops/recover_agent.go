package ops

import (
	"fmt"
	"log"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// RecoverAgentResult contains the outcome of recovering a crashed agent.
type RecoverAgentResult struct {
	AgentID         string
	Role            string
	TaskID          string // empty if no task was associated
	ClaimReleased   bool
	WorktreeRemoved bool
	AgentDeleted    bool
	AlreadyClean    bool // true if agent was not found (idempotent)
	Warnings        []string
}

// RecoverAgent performs full recovery for a crashed agent: releases task claims,
// removes worktrees (for doer roles), and deletes the agent from state.
// Idempotent: returns AlreadyClean=true if agent not found.
// Without force, refuses if the agent's PID is still alive.
// No terminal I/O.
func RecoverAgent(projectRoot, agentID string, force bool, reason string) (*RecoverAgentResult, error) {
	if agentID == "" {
		return nil, fmt.Errorf("agent ID required")
	}
	if reason == "" {
		reason = "agent recovery"
	}

	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())

	// Phase 1: Read — capture agent state
	state, err := bb.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read state: %w", err)
	}

	agent, exists := state.Agents[agentID]
	doerTaskIDs, reviewerTaskIDs := TaskClaimsForAgent(state, agentID)
	if !exists && len(doerTaskIDs) == 0 && len(reviewerTaskIDs) == 0 {
		return &RecoverAgentResult{
			AgentID:      agentID,
			AlreadyClean: true,
		}, nil
	}

	if exists && !force && agent.PID != 0 && IsProcessAlive(agent.PID) {
		return nil, fmt.Errorf("agent %s is still running with PID %d, use --force to recover", agentID, agent.PID)
	}

	role := agent.Role
	taskID := ""
	if agent.CurrentTask != nil {
		taskID = *agent.CurrentTask
	}
	if taskID == "" {
		if len(doerTaskIDs) > 0 {
			taskID = doerTaskIDs[0]
		} else if len(reviewerTaskIDs) > 0 {
			taskID = reviewerTaskIDs[0]
		}
	}

	result := &RecoverAgentResult{
		AgentID: agentID,
		Role:    role,
		TaskID:  taskID,
	}

	// Load resolver early — needed for both worktree removal and claim release.
	var pipelineTransitions map[models.TaskStatus][]models.TaskStatus
	resolver, _, resolverErr := loadResolver(projectRoot)
	if resolverErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("pipeline config: %v", resolverErr))
	} else {
		pipelineTransitions = BuildPipelineTransitions(resolver)
	}
	preserveAgent := false

	// Phase 2: Git side effects (outside lock) — remove worktrees for doer claims
	if len(doerTaskIDs) > 0 {
		g := git.New(projectRoot)
		for _, doerTaskID := range doerTaskIDs {
			if err := g.RemoveWorktree(doerTaskID); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("worktree removal for %s: %v", doerTaskID, err))
			} else {
				result.WorktreeRemoved = true
			}
		}
	}

	// Phase 3: State modify (atomic)
	now := time.Now().UTC()
	err = bb.Modify(func(state *models.State) error {
		currentDoerTaskIDs, currentReviewerTaskIDs := TaskClaimsForAgent(state, agentID)
		agentStillExists := false
		if _, ok := state.Agents[agentID]; ok {
			agentStillExists = true
		}
		if !agentStillExists && len(currentDoerTaskIDs) == 0 && len(currentReviewerTaskIDs) == 0 {
			result.AlreadyClean = true
			return nil
		}

		if resolver == nil {
			for _, ownedTaskID := range append(currentDoerTaskIDs, currentReviewerTaskIDs...) {
				log.Printf("WARNING: recover-agent %s: claim release skipped for task %s — resolver not loaded", agentID, ownedTaskID)
				result.Warnings = append(result.Warnings, fmt.Sprintf("claim release skipped for task %s — resolver not loaded", ownedTaskID))
			}
			if len(currentDoerTaskIDs) > 0 || len(currentReviewerTaskIDs) > 0 {
				preserveAgent = true
			}
		} else {
			for _, ownedTaskID := range currentDoerTaskIDs {
				task := state.FindTask(ownedTaskID)
				if task == nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("task %s not found in state", ownedTaskID))
					continue
				}
				effectiveCoderRelease := resolveDoerClaimReleaseStatus(task, resolver)
				released, err := releaseOneClaim(state, task, effectiveCoderRelease, pipelineTransitions, true, agentID, reason, now)
				if err != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("doer claim release for %s: %v", ownedTaskID, err))
					preserveAgent = true
					continue
				}
				if released {
					result.ClaimReleased = true
					// Clear worktree reference since we removed it
					task.Worktree = nil
				}
			}
			for _, ownedTaskID := range currentReviewerTaskIDs {
				task := state.FindTask(ownedTaskID)
				if task == nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("task %s not found in state", ownedTaskID))
					continue
				}
				effectiveReviewerRelease, err := resolveReviewerClaimReleaseStatus(task, resolver)
				if err != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("reviewer claim release for %s: %v", ownedTaskID, err))
					preserveAgent = true
					continue
				}
				released, err := releaseOneClaim(state, task, effectiveReviewerRelease, pipelineTransitions, true, agentID, reason, now)
				if err != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("reviewer claim release for %s: %v", ownedTaskID, err))
					preserveAgent = true
					continue
				}
				if released {
					result.ClaimReleased = true
				}
			}
		}

		if agentStillExists && !preserveAgent {
			delete(state.Agents, agentID)
			result.AgentDeleted = true
		} else if agentStillExists && preserveAgent {
			result.Warnings = append(result.Warnings, fmt.Sprintf("agent %s preserved because one or more claims could not be released", agentID))
		}

		recoveryNote := models.HumanNote{
			Timestamp: now,
			Message:   fmt.Sprintf("Agent %s recovered (%s): %s", agentID, role, reason),
			For:       agentID,
		}
		recoveryNote.MarkSeenByOrchestrator(now) // audit record, not a request
		state.HumanNotes = append(state.HumanNotes, recoveryNote)

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to recover agent: %w", err)
	}

	return result, nil
}
