package ops

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
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
		outcome, action, effect := models.LifecycleInvalidInput, LifecycleSafeActionCorrectInput, LifecycleEffectsNone
		if effects {
			outcome, action, effect = models.LifecycleStateChanged, LifecycleSafeActionRequery, LifecycleEffectsUnknown
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

// recoverAgentTestHooks injects interleavings between recovery phases. Nil in
// production; tests that set it must not run in parallel.
type recoverAgentTestHooks struct {
	// beforePrepare runs before the preparation transaction of an anchored recovery.
	beforePrepare func()
	// afterGitRemoval runs after the doer worktree removal, before the completion transaction.
	afterGitRemoval func()
}

var testRecoverAgentHooks *recoverAgentTestHooks

// recoverAgentRun carries the resolved context shared by the recovery phases.
type recoverAgentRun struct {
	agentID, reason, role string
	result                *RecoverAgentResult
	resolver              *pipeline.Resolver
	transitions           map[models.TaskStatus][]models.TaskStatus
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

	claims, err := captureRecoverAgentClaims(state, agentID, lockedIDs)
	if err != nil {
		return nil, err
	}
	agent, exists := claims.agent, claims.exists
	anchor, err := recoverAgentAnchor(state, agentID, claims.all, opts)
	if err != nil {
		return nil, err
	}
	var request LifecycleRequest
	if anchor != nil {
		var replay *RecoverAgentResult
		request, replay, err = recoverAgentRequest(anchor, agentID, force, reason, opts)
		if err != nil || replay != nil {
			return replay, err
		}
	}
	if !exists && len(claims.doers) == 0 && len(claims.reviewers) == 0 {
		return &RecoverAgentResult{
			LifecycleOutcome: NewLifecycleOutcome("recover-agent", anchor, models.LifecycleAlreadyCompleted, LifecycleSafeActionStop, LifecycleEffectsNone),
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
		if len(claims.doers) > 0 {
			taskID = claims.doers[0]
		} else if len(claims.reviewers) > 0 {
			taskID = claims.reviewers[0]
		}
	}

	result := &RecoverAgentResult{
		AgentID: agentID,
		Role:    role,
		TaskID:  taskID,
	}
	run := recoverAgentRun{agentID: agentID, reason: reason, role: role, result: result}

	// Load resolver early — needed for both worktree removal and claim release.
	resolver, _, resolverErr := loadResolver(projectRoot)
	if resolverErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("pipeline config: %v", resolverErr))
	} else {
		run.resolver, run.transitions = resolver, BuildPipelineTransitions(resolver)
	}
	if anchor != nil {
		if err := recoverAgentPrepare(bb, anchor.ID, agentID, exists, agent.Generation, request); err != nil {
			return nil, err
		}
	}

	// Phase 2: Git side effects (outside lock) — remove worktrees for doer claims
	if len(claims.doers) > 0 {
		*effects = true
		recoverAgentRemoveWorktrees(git.New(projectRoot), claims.doers, result)
	}

	// Phase 3: State modify (atomic)
	err = bb.Modify(func(state *models.State) error {
		return recoverAgentComplete(state, run, claims, anchor, request, time.Now().UTC())
	})
	if err != nil && !isLifecycleReplay(err) {
		return nil, fmt.Errorf("failed to recover agent: %w", err)
	}

	return result, nil
}

// recoverAgentPrepare rechecks the target registration and records the
// owner-ending preparation on the anchor task.
func recoverAgentPrepare(bb *db.Blackboard, anchorID, agentID string, exists bool, generation string, request LifecycleRequest) error {
	if testRecoverAgentHooks != nil && testRecoverAgentHooks.beforePrepare != nil {
		testRecoverAgentHooks.beforePrepare()
	}
	return bb.Modify(func(current *models.State) error {
		task := current.FindTask(anchorID)
		if task == nil {
			return fmt.Errorf("recovery task disappeared")
		}
		if currentAgent, ok := current.Agents[agentID]; ok != exists || currentAgent.Generation != generation {
			return WrapLifecycleError("recover-agent", nil, fmt.Errorf("target registration changed"), models.LifecycleStaleCaller, LifecycleSafeActionStop, LifecycleEffectsNone)
		}
		return prepareOwnerEndingRequest(task, request)
	})
}

// recoverAgentRemoveWorktrees removes the worktree of every doer claim; a failed
// removal is a warning, never an abort.
func recoverAgentRemoveWorktrees(g *git.Git, doerTaskIDs []string, result *RecoverAgentResult) {
	for _, doerTaskID := range doerTaskIDs {
		if err := g.RemoveWorktree(doerTaskID); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("worktree removal for %s: %v", doerTaskID, err))
		} else {
			result.WorktreeRemoved = true
		}
	}
	if testRecoverAgentHooks != nil && testRecoverAgentHooks.afterGitRemoval != nil {
		testRecoverAgentHooks.afterGitRemoval()
	}
}

// recoverAgentComplete is the completion transaction: it rechecks the
// registration generation and preparation, releases the claims and removes the
// agent unless a release failed.
func recoverAgentComplete(state *models.State, run recoverAgentRun, claims recoverAgentClaims, anchor *models.Task, request LifecycleRequest, now time.Time) error {
	agentID, result := run.agentID, run.result
	if currentAgent, ok := state.Agents[agentID]; ok != claims.exists || currentAgent.Generation != claims.agent.Generation {
		return WrapLifecycleError("recover-agent", nil, fmt.Errorf("target registration changed"), models.LifecycleStaleCaller, LifecycleSafeActionStop, LifecycleEffectsUnknown)
	}
	if anchor != nil {
		if err := ValidateLifecyclePreparation(state.FindTask(anchor.ID), request); err != nil {
			return err
		}
	}
	currentDoerTaskIDs, currentReviewerTaskIDs := TaskClaimsForAgent(state, agentID)
	_, agentStillExists := state.Agents[agentID]
	if !agentStillExists && len(currentDoerTaskIDs) == 0 && len(currentReviewerTaskIDs) == 0 {
		result.AlreadyClean = true
		result.LifecycleOutcome = NewLifecycleOutcome("recover-agent", nil, models.LifecycleAlreadyCompleted, LifecycleSafeActionStop, LifecycleEffectsNone)
		return errLifecycleReplay
	}

	preserveAgent := recoverAgentReleaseClaims(state, run, currentDoerTaskIDs, currentReviewerTaskIDs, now)

	if agentStillExists && !preserveAgent {
		delete(state.Agents, agentID)
		result.AgentDeleted = true
	} else if agentStillExists && preserveAgent {
		result.Warnings = append(result.Warnings, fmt.Sprintf("agent %s preserved because one or more claims could not be released", agentID))
	}

	recoveryNote := models.HumanNote{
		Timestamp: now,
		Message:   fmt.Sprintf("Agent %s recovered (%s): %s", agentID, run.role, run.reason),
		For:       agentID,
	}
	recoveryNote.MarkSeenByOrchestrator(now) // audit record, not a request
	state.HumanNotes = append(state.HumanNotes, recoveryNote)
	if anchor != nil {
		task := state.FindTask(anchor.ID)
		models.AdvanceLifecycle(task)
		var err error
		result.LifecycleOutcome, err = CompleteLifecycleRequest(task, request, models.LifecycleProjection{}, state.Agents)
		return err
	}
	result.LifecycleOutcome = NewLifecycleOutcome("recover-agent", nil, models.LifecycleCompleted, LifecycleSafeActionContinue, LifecycleEffectsCommitted)
	return nil
}
