package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/pipeline"
)

const defaultMaxMergeRetries = 3

const pendingMergeStallAnomalyType = "retry_loop"

// Overridable in tests, which must not spend the production cadence to observe
// one round of it.
var (
	// pendingMergeWakeInterval bounds how long a reviewer holding an unmerged
	// approved task waits before retrying. The role's normal wait is hours long
	// and wakes only on reviewable work, which an owned pending merge is not —
	// and downstream work is gated on that merge, so nothing would ever wake it.
	pendingMergeWakeInterval = 30 * time.Second

	// maxPendingMergeStallRounds bounds the retry loop itself. A merge that has
	// not converged after this many rounds needs attention rather than another
	// attempt, so the reviewer records an anomaly and returns to its normal wait
	// instead of retrying forever and never reviewing again.
	maxPendingMergeStallRounds = 20
)

// reviewerStrategy handles review roles: code-reviewer, code-plan-reviewer,
// epic-plan-reviewer, us-reviewer.
type reviewerStrategy struct {
	role             string             // role name in hyphenated form
	resolver         *pipeline.Resolver // pipeline resolver for context sections
	mergeRetries     int                // current retry counter (mutable per-loop state)
	mergeStallRounds int                // bounded wake rounds spent on an unconverged owned merge
	maxRetries       int                // max merge retries before proceeding (0 = use default)
	executionTimeout time.Duration      // from YAML; 0 = use type default
	yamlPollSec      int                // from YAML; 0 = use type default
	yamlMaxWaitSec   int                // from YAML; 0 = use type default

	// breaker quarantines candidates whose claim keeps failing the same way.
	// Built on first use: NewRoleStrategy and tests construct the struct
	// directly, and the supervisor loop reads and writes it from one goroutine.
	breaker *claimBreaker
	// claimConfig is the config of the latest ClaimTask; the observer writes
	// the quarantine anomaly under that claim's project and authority.
	claimConfig SupervisorConfig
	// seenBoundaries is the boundary version last observed per failing
	// scope, so a failure that survived a state change is logged loudly.
	seenBoundaries map[claimBreakerScope]string
}

func (s *reviewerStrategy) activeBreaker() *claimBreaker {
	if s.breaker == nil {
		s.breaker = newClaimBreaker()
	}
	return s.breaker
}

func (s *reviewerStrategy) effectiveMaxRetries() int {
	if s.maxRetries > 0 {
		return s.maxRetries
	}
	return defaultMaxMergeRetries
}

const defaultReviewerTimeout = 2 * time.Hour

func (s *reviewerStrategy) DefaultTimeout() time.Duration {
	if s.executionTimeout > 0 {
		return s.executionTimeout
	}
	return defaultReviewerTimeout
}

func (s *reviewerStrategy) WaitConfig(state *models.State) (pollInterval, maxWait time.Duration) {
	poll := nonZeroOr(state.Config.ReviewerPollInterval, nonZeroOr(s.yamlPollSec, models.DefaultReviewerPollInterval))
	max := nonZeroOr(state.Config.ReviewerMaxWait, nonZeroOr(s.yamlMaxWaitSec, models.DefaultReviewerMaxWait))
	return time.Duration(poll) * time.Second, time.Duration(max) * time.Second
}

func (s *reviewerStrategy) PreWork(ctx context.Context, bb *db.Blackboard, config SupervisorConfig) (bool, error) {
	logger := GetLogger()

	pr, prErr := ops.LoadResolverForModels(config.ProjectRoot)
	if prErr != nil {
		logger.Warn("Failed to load pipeline resolver — skipping merge handling", "error", prErr)
	} else {
		if err := handleApprovedMergesWithAuthority(config.ProjectRoot, config.Authority, bb, pr); err != nil {
			if ops.IsAgentAuthorityError(err) {
				return false, err
			}
			logger.Warn("Merge handler error", "error", err)
		}
	}

	// Execute auto transitions for newly-merged tasks (e.g., integration-to-fix).
	// Manual transitions remain gated by orchestrator PLANNING_COMPLETE checkpoint.
	if err := handleAutoTransitions(config.ProjectRoot); err != nil {
		logger.Warn("Auto transition handler error", "error", err)
	}

	// Clean up worktrees for tasks at pipeline-defined clean terminal states.
	if err := handleCleanTaskCleanup(config.ProjectRoot, config.Authority); err != nil {
		logger.Warn("Clean task cleanup error", "error", err)
	}

	// If there are still pending merges (transient errors), retry with
	// backoff up to a max count, then keep retrying on a bounded wake.
	if prErr == nil && hasPendingMerges(bb, config.AgentID, pr) {
		s.mergeRetries++
		if s.mergeRetries <= s.effectiveMaxRetries() {
			delay := time.Duration(s.mergeRetries) * time.Second
			logger.Info("Pending merges remain, retrying after delay",
				"agent_id", config.AgentID,
				"retry", s.mergeRetries,
				"delay", delay)
			time.Sleep(delay)
			return true, nil // shouldContinue: restart loop iteration
		}

		// Quick retries are spent. Returning to the role's normal wait here
		// parks the reviewer for hours on a predicate that cannot see the merge
		// it owns, while every downstream task waits on that merge. Retry on a
		// bounded wake instead, yielding as soon as there is review work to do.
		s.mergeStallRounds++
		if s.mergeStallRounds <= maxPendingMergeStallRounds {
			yield, err := s.awaitPendingMergeWake(ctx, bb, config, pr)
			if err != nil {
				return false, err
			}
			if yield {
				s.resetMergeCounters()
				return false, nil
			}
			s.mergeRetries = s.effectiveMaxRetries()
			return true, nil // shouldContinue: retry the merge
		}

		logger.Warn("Pending merge has not converged, returning to normal wait",
			"agent_id", config.AgentID,
			"rounds", s.mergeStallRounds)
		s.recordPendingMergeStall(bb, config)
		s.resetMergeCounters()
	} else {
		s.resetMergeCounters()
	}

	return false, nil
}

func (s *reviewerStrategy) resetMergeCounters() {
	s.mergeRetries = 0
	s.mergeStallRounds = 0
}

// awaitPendingMergeWake waits up to pendingMergeWakeInterval for the situation
// to change. It returns yield=true when the reviewer should stop retrying and
// go through its normal wait — because review work appeared, or because the
// merge it was retrying is no longer pending for it.
func (s *reviewerStrategy) awaitPendingMergeWake(ctx context.Context, bb *db.Blackboard, config SupervisorConfig, pr models.PipelineResolver) (bool, error) {
	logger := GetLogger()

	// waitForWorkEventDriven reports ABORT as "no work" without consulting the
	// predicate below, which would read as "keep retrying". Checking before the
	// wait keeps a stopped system from collecting a merge attempt per round.
	// An ABORT arriving mid-wait costs one further attempt, caught here on the
	// next round or by the supervisor loop's own ABORT check.
	if state, err := bb.ReadCached(); err == nil {
		if stopped, reason := isSystemStopped(state); stopped {
			logger.Info("ABORT detected while a merge is pending", "agent_id", config.AgentID, "reason", reason)
			return true, nil
		}
	}

	return waitForWorkEventDriven(ctx, bb, config.ProjectRoot, pendingMergeWakeInterval, pendingMergeWakeInterval,
		func(state *models.State) (bool, string) {
			if !hasPendingMergesInState(state, config.AgentID, pr) {
				logger.Info("Pending merge resolved, returning to normal wait", "agent_id", config.AgentID)
				return true, ""
			}
			if config.InitialTask != "" || models.CountReviewableTasksForAgent(state, s.role, config.AgentID, pr) > 0 {
				logger.Info("Review work available while a merge is pending, yielding merge retry",
					"agent_id", config.AgentID)
				return true, ""
			}
			return false, ""
		})
}

// recordPendingMergeStall leaves durable evidence that automatic merge
// convergence gave up, so the stall is visible without reading supervisor logs.
func (s *reviewerStrategy) recordPendingMergeStall(bb *db.Blackboard, config SupervisorConfig) {
	err := ops.ModifyWithAgentAuthority(bb, config.Authority, func(state *models.State) error {
		state.Anomalies = append(state.Anomalies, models.Anomaly{
			Timestamp: time.Now().UTC(),
			Reporter:  config.AgentID,
			Type:      pendingMergeStallAnomalyType,
			Details: map[string]any{
				"agent_id": config.AgentID,
				"role":     s.role,
				"rounds":   s.mergeStallRounds,
				"impact":   "an approved task this reviewer owns has not merged; downstream work stays gated until it does",
			},
		})
		return nil
	})
	if err != nil {
		GetLogger().Warn("Failed to record pending merge stall anomaly", "agent_id", config.AgentID, "error", err)
	}
}

func (s *reviewerStrategy) WaitForWork(ctx context.Context, bb *db.Blackboard, config SupervisorConfig, pollInterval, maxWait time.Duration) (bool, error) {
	if cleared, err := ops.ClearStaleReviewClaims(config.ProjectRoot); err != nil {
		GetLogger().Warn("Failed to clear stale review claims before reviewer wait", "error", err)
	} else if cleared > 0 {
		GetLogger().Info("Cleared stale review claims before reviewer wait", "count", cleared)
	}

	pr := loadResolver(config.ProjectRoot)
	return waitForWorkEventDriven(ctx, bb, config.ProjectRoot, pollInterval, maxWait,
		func(state *models.State) (bool, string) {
			breaker := s.activeBreaker()
			if config.InitialTask != "" {
				task := state.FindTask(config.InitialTask)
				if task == nil {
					return false, fmt.Sprintf("Initial review task %s not found", config.InitialTask)
				}
				if !task.IsClaimable(s.role, state.Tasks, pr) {
					return false, fmt.Sprintf("Initial review task %s is not %s-reviewable", config.InitialTask, s.role)
				}
				if task.HasApprovalFromAgent(config.AgentID) {
					return false, fmt.Sprintf("Initial review task %s was already approved by %s", config.InitialTask, config.AgentID)
				}
				if breaker.Quarantined(task, s.role, time.Now()) {
					return false, fmt.Sprintf("Initial review task %s is quarantined after repeated claim failures", config.InitialTask)
				}
				return true, fmt.Sprintf("Found initial %s-reviewable task %s", s.role, config.InitialTask)
			}

			// Agent-aware count so a reviewer that just approved a task
			// doesn't keep seeing it as "reviewable" — round-2 must go to a
			// different reviewer (see filterAlreadyApprovedByAgent in ops) —
			// minus the candidates the breaker has quarantined, so the loop
			// parks here instead of re-claiming a boundary it cannot repair.
			count := breaker.ClaimableAfterQuarantine(state, s.role, config.AgentID, pr, time.Now())
			if count > 0 {
				return true, fmt.Sprintf("Found %d %s-reviewable task(s)", count, s.role)
			}
			if quarantined := models.CountReviewableTasksForAgent(state, s.role, config.AgentID, pr); quarantined > 0 {
				return false, fmt.Sprintf("%d %s-reviewable task(s) quarantined after repeated claim failures; waiting for the cooldown or a boundary change", quarantined, s.role)
			}

			// Use richer diagnostics for code-reviewer role
			if s.role == "code-reviewer" {
				return false, models.GetReviewerWorkDiagnostics(state, pr)
			}
			return false, fmt.Sprintf("No %s-reviewable tasks", s.role)
		})
}

func (s *reviewerStrategy) ClaimTask(config SupervisorConfig, bb *db.Blackboard) (string, string, error) {
	logger := GetLogger()
	s.claimConfig = config

	session, err := prepareClaimSession(config, bb)
	if err != nil {
		return "", "", err
	}
	result, err := claimReviewerTaskForRoleWithOptionalAuthority(config.ProjectRoot, config.Authority.ID, s.role, config.InitialTask, 1800, &config.Authority, bb, session)
	if err != nil {
		return "", "", err
	}
	if len(result.CandidateFailures) > 0 {
		// These failures were classified where each candidate was removed;
		// healthy progress must not hide them or clear another task's key.
		decision := s.ObserveClaimFailure(&ops.ReviewClaimFailure{
			Role: s.role, Class: ops.ReviewClaimClassCandidateFailures,
			Candidates: result.CandidateFailures,
		})
		if decision.Stop {
			return "", "", &ops.AgentAuthorityError{AgentID: config.AgentID}
		}
	}
	taskID := result.TaskID
	s.activeBreaker().ObserveSuccess(s.role, taskID)

	logger.Info("Reviewer claimed task for review",
		"agent_id", config.AgentID,
		"task_id", taskID,
		"review_commit", result.ReviewCommit)

	// Verify the worktree exists and is prepared before launching agent.
	_, wtErr := ensureReviewerWorktree(config.ProjectRoot, bb, taskID, config.Authority)
	if wtErr != nil {
		logger.Warn("Reviewer worktree check failed",
			"task_id", taskID, "error", wtErr)
		if !errors.Is(wtErr, errTaskBlocked) {
			// Transient error (bb.Read, BranchExists, AttachWorktree) or a
			// post_worktree_cmd failure. blockReviewerTask was NOT called, so the
			// reviewer claim and agent state are still dangling — release them.
			// Release restores the task to its reviewable status, preserving the
			// doer's completed work for the next reviewer.
			if releaseErr := releaseReviewerClaimQuietly(config.ProjectRoot, taskID, config.Authority); releaseErr != nil {
				return "", "", releaseErr
			}
		}
		// For errTaskBlocked, blockReviewerTask already cleared
		// claim fields and released agent state.
		//
		// A failed setup command is project-level, so this reviewer cannot make
		// progress on any task: degrade it, mirroring the doer claim policy. The
		// supervisor exits on ErrAgentDegraded before any provider session starts.
		if degradedErr := markAgentDegradedForInfraClaim(
			config.ProjectRoot, config.AgentID, s.role, taskID, []string{taskID}, wtErr, &config.Authority,
		); degradedErr != nil {
			return "", "", degradedErr
		}
		return "", "", wtErr
	}

	return taskID, "", nil
}

// ObserveClaimFailure feeds one failed claim to the breaker and records every
// key it opened. The error arrives typed from ops.ClaimReviewerTask with its
// candidates intact; only a candidate-free error is classified here.
func (s *reviewerStrategy) ObserveClaimFailure(err error) claimBreakerDecision {
	failure := ops.ClassifyReviewClaimError(s.role, err)
	if failure == nil {
		return claimBreakerDecision{}
	}
	breaker := s.activeBreaker()
	decision := breaker.Observe(failure, time.Now().UTC())

	if s.claimFailureNoteworthy(failure, decision) {
		GetLogger().Error("Review claim error", "agent_id", s.claimConfig.AgentID, "role", s.role, "error", failure)
	} else {
		GetLogger().Debug("Review claim error", "agent_id", s.claimConfig.AgentID, "role", s.role, "error", failure)
	}

	for _, key := range decision.Opened {
		if s.recordCircuitOpen(breaker, key, failure) {
			decision.Stop = true
		}
	}
	return decision
}

// claimFailureNoteworthy decides whether the failure earns the error-level
// line: a key opened, or a counted candidate failed again against a moved
// boundary. Identical repeats stay at debug so the log keeps the first
// actionable cause instead of one line per loop iteration.
func (s *reviewerStrategy) claimFailureNoteworthy(failure *ops.ReviewClaimFailure, decision claimBreakerDecision) bool {
	noteworthy := len(decision.Opened) > 0
	if s.seenBoundaries == nil {
		s.seenBoundaries = make(map[claimBreakerScope]string)
	}
	for _, candidate := range failure.Candidates {
		scope := claimBreakerScope{role: failure.Role, taskID: candidate.TaskID, class: candidate.Class}
		if seen, ok := s.seenBoundaries[scope]; ok && seen != candidate.BoundaryVersion {
			noteworthy = true
		}
		s.seenBoundaries[scope] = candidate.BoundaryVersion
	}
	return noteworthy
}

// recordCircuitOpen writes the durable anomaly for one opened key. It reports
// true when the write was rejected for authority: the supervisor has lost its
// generation and must stop rather than keep observing.
func (s *reviewerStrategy) recordCircuitOpen(breaker *claimBreaker, key claimBreakerKey, failure *ops.ReviewClaimFailure) bool {
	counters, ok := breaker.Counters(key)
	if !ok {
		return false
	}
	config := s.claimConfig
	var authority *models.AgentAuthority
	if config.Authority.ID != "" {
		authority = &config.Authority
	}
	_, err := ops.RecordReviewerClaimCircuitOpen(ops.ReviewerClaimCircuitOpenInput{
		ProjectRoot:     config.ProjectRoot,
		AgentID:         config.AgentID,
		Authority:       authority,
		Role:            key.Role,
		TaskID:          key.TaskID,
		FailureClass:    key.Class,
		BoundaryVersion: key.BoundaryVersion,
		Recovery:        candidateRecovery(failure, key),
		Err:             failure,
		Attempts:        counters.Attempts,
		FirstFailure:    counters.FirstFailure,
		LastFailure:     counters.LastFailure,
		CooldownUntil:   counters.CooldownUntil,
	})
	if err == nil {
		return false
	}
	if ops.IsAgentAuthorityError(err) {
		GetLogger().Error("Reviewer claim quarantine record rejected, exiting supervisor",
			"agent_id", config.AgentID, "task_id", key.TaskID, "error", err)
		return true
	}
	GetLogger().Warn("Failed to record reviewer claim quarantine",
		"agent_id", config.AgentID, "task_id", key.TaskID, "error", err)
	return false
}

// candidateRecovery returns the recovery hint of the candidate the key names.
func candidateRecovery(failure *ops.ReviewClaimFailure, key claimBreakerKey) string {
	for _, candidate := range failure.Candidates {
		if candidate.TaskID == key.TaskID && candidate.Class == key.Class {
			return candidate.Recovery
		}
	}
	return ""
}

func (s *reviewerStrategy) PreExecution(_ *db.Blackboard, _ SupervisorConfig) error {
	return nil
}

func (s *reviewerStrategy) BuildPrompt(state *models.State, config SupervisorConfig, taskID string) (string, error) {
	return buildPromptWithContext(state, config, taskID, s.resolver)
}

func (s *reviewerStrategy) PostExecution(_ *db.Blackboard, _ SupervisorConfig, _, _ string, _ *models.State) error {
	return nil
}

func ensureReviewerPromptClaimed(state *models.State, agentID, taskID string, resolver *pipeline.Resolver) error {
	task := state.FindTask(taskID)
	if task == nil {
		return fmt.Errorf("review prompt requires claimed task %s, but the task was not found", taskID)
	}
	if task.RolePair == "" {
		return fmt.Errorf("review prompt for task %s requires role_pair", taskID)
	}

	reviewing, err := resolver.ReviewingStatus(task.RolePair)
	if err != nil {
		return fmt.Errorf("review prompt for task %s cannot resolve reviewing status: %w", taskID, err)
	}
	reviewing2, reviewing2Err := resolver.Reviewing2Status(task.RolePair)
	inReviewingState := task.Status == reviewing || (reviewing2Err == nil && task.Status == reviewing2)
	if !inReviewingState {
		return fmt.Errorf("review prompt for task %s requires reviewing state; current status: %s", taskID, task.Status)
	}
	if task.ReviewingBy == nil || *task.ReviewingBy != agentID {
		return fmt.Errorf("review prompt for task %s requires reviewer claim by %s", taskID, agentID)
	}
	return nil
}
