package ops

import (
	"context"
	"fmt"
	"log"
	"slices"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// RecoverAgentResult contains the outcome of recovering a crashed agent.
type RecoverAgentResult struct {
	models.LifecycleOutcome
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
	return RecoverAgentWithOptions(projectRoot, agentID, force, reason, LifecycleRequestOptions{})
}

func RecoverAgentWithOptions(projectRoot, agentID string, force bool, reason string, opts LifecycleRequestOptions) (result *RecoverAgentResult, retErr error) {
	invocation := NewLifecycleInvocation(projectRoot)
	effects := false
	defer func() {
		outcome, action, effect := models.LifecycleInvalidInput, "correct_input", "none"
		if effects {
			outcome, action, effect = models.LifecycleStateChanged, "requery", "unknown"
		}
		retErr = WrapLifecycleError("recover-agent", nil, retErr, outcome, action, effect)
		var value models.LifecycleOutcome
		var warnings *[]string
		if result != nil {
			value, warnings = result.LifecycleOutcome, &result.Warnings
		}
		invocation.FinishResult("recover-agent", value, &retErr, warnings)
	}()
	if agentID == "" {
		return nil, &PreconditionError{Reason: "agent ID required"}
	}
	if err := ValidateLifecycleRequestOptions(opts); err != nil {
		return nil, err
	}
	retErr = WithProjectLifecycleSharedLock(projectRoot, "recover-agent", func() error {
		return WithAgentLifecycleLock(context.Background(), projectRoot, agentID, "recover-agent", func() error {
			bb := db.For(paths.New(projectRoot).StatePath())
			state, err := bb.Read()
			if err != nil {
				return err
			}
			doers, reviewers := TaskClaimsForAgent(state, agentID)
			ids := append(append([]string(nil), doers...), reviewers...)
			slices.Sort(ids)
			ids = slices.Compact(ids)
			var locked func(int) error
			locked = func(index int) error {
				if index == len(ids) {
					var err error
					result, err = recoverAgentLifecycle(projectRoot, agentID, force, reason, opts, ids, &effects)
					return err
				}
				lock := filelock.New(claimTaskWorktreeLockPath(paths.New(projectRoot).StatePath(), ids[index]))
				return lock.WithLockOperation("recover-agent", func() error { return locked(index + 1) })
			}
			return locked(0)
		})
	})
	return result, retErr
}

func recoverAgentLifecycle(projectRoot, agentID string, force bool, reason string, opts LifecycleRequestOptions, lockedIDs []string, effects *bool) (*RecoverAgentResult, error) {
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
	claims := append(append([]string(nil), doerTaskIDs...), reviewerTaskIDs...)
	slices.Sort(claims)
	claims = slices.Compact(claims)
	if !slices.Equal(claims, lockedIDs) {
		return nil, WrapLifecycleError("recover-agent", nil, fmt.Errorf("agent task claims changed before recovery"), models.LifecycleStateChanged, "requery", "none")
	}
	// Receipts are task scoped; an agent-wide or taskless request cannot invent a boundary.
	var anchor *models.Task
	if len(claims) == 1 {
		anchor = state.FindTask(claims[0])
	}
	if opts.RequestID != "" && anchor == nil {
		for index := range state.Tasks {
			task := &state.Tasks[index]
			if task.Lifecycle == nil {
				continue
			}
			for _, receipt := range task.Lifecycle.Receipts {
				if receipt.Operation == "recover-agent" && receipt.Actor == agentID && receipt.RequestID == opts.RequestID && receipt.ExpectedTransition == opts.ExpectedTransition {
					if anchor != nil && anchor.ID != task.ID {
						return nil, &PreconditionError{Reason: "request_id requires one task boundary for recover-agent"}
					}
					anchor = task
				}
			}
		}
		if anchor == nil {
			return nil, &PreconditionError{Reason: "expected_transition requires a single affected task for recover-agent"}
		}
	}
	var request LifecycleRequest
	if anchor != nil {
		request, err = NewLifecycleRequest("recover-agent", anchor, agentID, nil, opts, struct {
			Force  bool
			Reason string
		}{force, reason})
		if err != nil {
			return nil, err
		}
		receipt, err := checkOwnerEndingRequest(anchor, request)
		if err != nil {
			return nil, err
		}
		if receipt != nil {
			return &RecoverAgentResult{LifecycleOutcome: LifecycleReplayOutcome(anchor, receipt, agentID), AgentID: agentID, TaskID: anchor.ID, AlreadyClean: true}, nil
		}
	}
	if !exists && len(doerTaskIDs) == 0 && len(reviewerTaskIDs) == 0 {
		return &RecoverAgentResult{
			LifecycleOutcome: NewLifecycleOutcome("recover-agent", anchor, models.LifecycleAlreadyCompleted, "stop", "none"),
			AgentID:          agentID,
			AlreadyClean:     true,
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
	if anchor != nil {
		err := bb.Modify(func(current *models.State) error {
			task := current.FindTask(anchor.ID)
			if task == nil {
				return fmt.Errorf("recovery task disappeared")
			}
			if currentAgent, ok := current.Agents[agentID]; ok != exists || currentAgent.Generation != agent.Generation {
				return WrapLifecycleError("recover-agent", nil, fmt.Errorf("target registration changed"), models.LifecycleStaleCaller, "stop", "none")
			}
			return prepareOwnerEndingRequest(task, request)
		})
		if err != nil {
			return nil, err
		}
	}

	// Phase 2: Git side effects (outside lock) — remove worktrees for doer claims
	if len(doerTaskIDs) > 0 {
		*effects = true
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
		if currentAgent, ok := state.Agents[agentID]; ok != exists || currentAgent.Generation != agent.Generation {
			return WrapLifecycleError("recover-agent", nil, fmt.Errorf("target registration changed"), models.LifecycleStaleCaller, "stop", "unknown")
		}
		if anchor != nil {
			if err := ValidateLifecyclePreparation(state.FindTask(anchor.ID), request); err != nil {
				return err
			}
		}
		currentDoerTaskIDs, currentReviewerTaskIDs := TaskClaimsForAgent(state, agentID)
		agentStillExists := false
		if _, ok := state.Agents[agentID]; ok {
			agentStillExists = true
		}
		if !agentStillExists && len(currentDoerTaskIDs) == 0 && len(currentReviewerTaskIDs) == 0 {
			result.AlreadyClean = true
			result.LifecycleOutcome = NewLifecycleOutcome("recover-agent", nil, models.LifecycleAlreadyCompleted, "stop", "none")
			return errLifecycleReplay
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

		state.HumanNotes = append(state.HumanNotes, models.HumanNote{
			Timestamp: now,
			Message:   fmt.Sprintf("Agent %s recovered (%s): %s", agentID, role, reason),
			For:       agentID,
		})
		if anchor != nil {
			task := state.FindTask(anchor.ID)
			models.AdvanceLifecycle(task)
			var err error
			result.LifecycleOutcome, err = CompleteLifecycleRequest(task, request, models.LifecycleProjection{})
			return err
		}
		result.LifecycleOutcome = NewLifecycleOutcome("recover-agent", nil, models.LifecycleCompleted, "continue", "committed")
		return nil
	})

	if err != nil && !isLifecycleReplay(err) {
		return nil, fmt.Errorf("failed to recover agent: %w", err)
	}

	return result, nil
}
