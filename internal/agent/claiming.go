package agent

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/roles"
)

// ErrAgentDegraded aliases the ops sentinel so a claim degraded inside
// ops.ClaimTask and one degraded here are indistinguishable to the supervisor.
var ErrAgentDegraded = ops.ErrAgentDegraded

func claimDoerTask(projectRoot, agentID, role string, bb *db.Blackboard) (taskID, worktree string, err error) {
	return claimDoerTaskWithOptionalAuthority(projectRoot, agentID, role, nil, nil, bb)
}

func claimDoerTaskWithAuthority(projectRoot string, authority models.AgentAuthority, role string, bb *db.Blackboard, sessions ...*ops.ValidationSession) (taskID, worktree string, err error) {
	return claimDoerTaskWithOptionalAuthority(projectRoot, authority.ID, role, &authority, nil, bb, sessions...)
}

// claimDoerTaskEscalating is claimDoerTaskWithAuthority for a supervisor that
// escalates refused candidates through refusals instead of retrying them.
func claimDoerTaskEscalating(projectRoot string, authority models.AgentAuthority, role string, bb *db.Blackboard, refusals *acceptanceRefusalTracker, sessions ...*ops.ValidationSession) (taskID, worktree string, err error) {
	return claimDoerTaskWithOptionalAuthority(projectRoot, authority.ID, role, &authority, refusals, bb, sessions...)
}

func claimDoerTaskWithOptionalAuthority(projectRoot, agentID, role string, authority *models.AgentAuthority, refusals *acceptanceRefusalTracker, bb *db.Blackboard, sessions ...*ops.ValidationSession) (taskID, worktree string, err error) {
	var session *ops.ValidationSession
	if len(sessions) > 0 {
		session = sessions[0]
	}

	taskID, worktree, resumed, err := resumeDoerTask(projectRoot, agentID, role, authority, session)
	if err != nil || resumed {
		return taskID, worktree, err
	}

	tier, err := claimableDoerTier(projectRoot, agentID, role, authority, session, bb)
	if err != nil {
		return "", "", err
	}
	return claimFirstDoerCandidate(projectRoot, agentID, role, authority, refusals, session, tier)
}

// resumeDoerTask resumes work the agent already holds: a handoff first, then an
// owned executing task. It reports resumed=false when neither exists, so the
// caller goes on to a fresh claim.
func resumeDoerTask(projectRoot, agentID, role string, authority *models.AgentAuthority, session *ops.ValidationSession) (taskID, worktree string, resumed bool, err error) {
	logger := GetLogger()

	handoffResult, err := ops.ResumeHandoff(ops.ResumeHandoffInput{
		ProjectRoot: projectRoot,
		AgentID:     agentID,
		Authority:   authority,
		Session:     session,
	})
	if err != nil {
		return "", "", false, err
	}
	if handoffResult.Found {
		logger.Info("Resuming claimed task from handoff", "task_id", handoffResult.TaskID, "agent_id", agentID)
		return resumedDoerTaskSetup(projectRoot, agentID, role, handoffResult.TaskID, handoffResult.Worktree, authority)
	}

	ownedResult, err := ops.ResumeOwnedTask(ops.ResumeOwnedTaskInput{
		ProjectRoot: projectRoot,
		AgentID:     agentID,
		Authority:   authority,
		Session:     session,
	})
	if err != nil {
		return "", "", false, err
	}
	if ownedResult.Found {
		logger.Info("Resuming owned executing task", "task_id", ownedResult.TaskID, "agent_id", agentID)
		return resumedDoerTaskSetup(projectRoot, agentID, role, ownedResult.TaskID, ownedResult.Worktree, authority)
	}
	if ownedResult.Blocked {
		logger.Warn("Blocked owned executing task during resume", "task_id", ownedResult.BlockedTaskID, "agent_id", agentID, "reason", ownedResult.BlockReason)
	}
	return "", "", false, nil
}

// resumedDoerTaskSetup fails a resume closed on an unprepared worktree.
func resumedDoerTaskSetup(projectRoot, agentID, role, taskID, worktree string, authority *models.AgentAuthority) (string, string, bool, error) {
	if setupErr := ensureDoerWorktreeSetup(projectRoot, agentID, role, taskID, worktree, authority); setupErr != nil {
		return "", "", false, setupErr
	}
	return taskID, worktree, true, nil
}

// claimableDoerTier returns the highest-priority tier of tasks this doer may
// claim, in scheduling order. An empty tier is an error.
func claimableDoerTier(projectRoot, agentID, role string, authority *models.AgentAuthority, session *ops.ValidationSession, bb *db.Blackboard) ([]*models.Task, error) {
	state, err := bb.ReadSnapshot()
	if err != nil {
		return nil, fmt.Errorf("failed to read state: %w", err)
	}
	if authority != nil {
		if err := ops.RequireAgentAuthority(state, *authority); err != nil {
			return nil, err
		}
	}

	pr := loadResolver(projectRoot)

	var candidates []*models.Task
	now := time.Now().UTC()
	for i := range state.Tasks {
		if models.IsDoerClaimableByAgent(state, &state.Tasks[i], role, agentID, pr, now) &&
			!ops.ValidationRetryPending(projectRoot, state, &state.Tasks[i], agentID, session) {
			candidates = append(candidates, &state.Tasks[i])
		}
	}
	tier := orderDoerCandidates(candidates, state, agentID, pr)

	if len(tier) == 0 {
		return nil, fmt.Errorf("no claimable tasks found")
	}
	return tier, nil
}

// claimFirstDoerCandidate tries each candidate in scheduling order until one
// succeeds. A halt or an authority loss ends the attempt; any other failure
// moves on to the next candidate.
func claimFirstDoerCandidate(projectRoot, agentID, role string, authority *models.AgentAuthority, refusals *acceptanceRefusalTracker, session *ops.ValidationSession, tier []*models.Task) (taskID, worktree string, err error) {
	var lastErr error
	candidateIDs := taskIDsFromCandidates(tier)
	for _, task := range tier {
		result, claimErr, stopErr := claimDoerCandidate(projectRoot, agentID, role, authority, refusals, session, task, candidateIDs)
		if stopErr != nil {
			return "", "", stopErr
		}
		if claimErr != nil {
			lastErr = claimErr
			continue
		}
		return result.TaskID, result.WorktreeRel, nil
	}

	return "", "", fmt.Errorf("all %d candidates in top priority tier failed to claim: %w", len(tier), lastErr)
}

// claimDoerCandidate claims one candidate. claimErr is a failure the caller
// survives by trying the next candidate; stopErr ends the whole attempt.
func claimDoerCandidate(projectRoot, agentID, role string, authority *models.AgentAuthority, refusals *acceptanceRefusalTracker, session *ops.ValidationSession, task *models.Task, candidateIDs []string) (result *ops.ClaimResult, claimErr, stopErr error) {
	logger := GetLogger()
	if authority == nil {
		result, claimErr = ops.ClaimTask(projectRoot, task.ID, agentID)
	} else {
		result, claimErr = ops.ClaimTaskWithAuthority(projectRoot, task.ID, *authority, session)
	}
	if claimErr != nil {
		if errors.Is(claimErr, ops.ErrSystemHalted) {
			return nil, nil, claimErr // Halted, not failed: no candidate is claimable.
		}
		logger.Warn("Claim attempt failed, trying next candidate",
			"task_id", task.ID, "error", claimErr)
		if classifyErr := markAgentDegradedForInfraClaim(projectRoot, agentID, role, task.ID, candidateIDs, claimErr, authority); classifyErr != nil {
			return nil, nil, classifyErr
		}
		if refusals != nil && authority != nil {
			if escalateErr := escalateAcceptanceRefusal(projectRoot, *authority, refusals, task.ID, claimErr); escalateErr != nil {
				return nil, nil, escalateErr
			}
		}
		return nil, claimErr, nil
	}
	if refusals != nil {
		refusals.forget(result.TaskID)
	}
	if clearErr := clearDegradedAfterClaim(projectRoot, agentID, authority); clearErr != nil {
		return nil, nil, clearErr
	}
	for _, w := range result.Warnings {
		logger.Warn("Claim warning", "task_id", result.TaskID, "warning", w)
	}
	return result, nil, nil
}

// clearDegradedAfterClaim clears the agent's degraded health after a successful
// claim. Only a lost authority is returned; any other failure is logged.
func clearDegradedAfterClaim(projectRoot, agentID string, authority *models.AgentAuthority) error {
	var clearErr error
	if authority == nil {
		clearErr = ops.ClearAgentDegraded(projectRoot, agentID)
	} else {
		clearErr = ops.ClearAgentDegradedWithAuthority(projectRoot, *authority)
	}
	if clearErr == nil {
		return nil
	}
	if ops.IsAgentAuthorityError(clearErr) {
		return clearErr
	}
	GetLogger().Warn("Failed to clear degraded agent health after successful claim",
		"agent_id", agentID, "error", clearErr)
	return nil
}

// ensureDoerWorktreeSetup runs the configured post_worktree_cmd for a resumed
// task worktree. Resume paths bypass ClaimTask, so without this a handoff or
// restart would hand a coder an unprepared worktree — the failure mode the
// claim path already fails closed on.
//
// Enforcement lives here rather than in doerStrategy.PreExecution because the
// supervisor logs PreExecution failures and continues (supervisor.go), so a
// PreExecution hook would not fail closed without changing that contract for
// every strategy.
func ensureDoerWorktreeSetup(projectRoot, agentID, role, taskID, worktreeRel string, authority *models.AgentAuthority) error {
	if worktreeRel == "" {
		return nil
	}
	state, err := db.For(paths.New(projectRoot).StatePath()).ReadSnapshot()
	if err != nil {
		return fmt.Errorf("read state for worktree setup: %w", err)
	}
	if state.Config.PostWorktreeCmd == nil {
		return nil
	}
	if task := state.FindTask(taskID); task != nil && len(task.ValidationPrerequisites) > 0 {
		// Protected resumes ran setup before preflight and ownership renewal.
		return nil
	}
	setupErr := ops.RunPostWorktreeCmd(projectRoot, *state.Config.PostWorktreeCmd, filepath.Join(projectRoot, worktreeRel), models.EffectivePostWorktreeCmdTimeout(state.Config), authority)
	if setupErr == nil {
		return nil
	}
	GetLogger().Error("post-worktree-cmd failed on resume",
		"task_id", taskID, "agent_id", agentID, "error", setupErr)
	if degradedErr := markAgentDegradedForInfraClaim(
		projectRoot, agentID, role, taskID, []string{taskID}, setupErr, authority,
	); degradedErr != nil {
		return degradedErr
	}
	return setupErr
}

func taskIDsFromCandidates(candidates []*models.Task) []string {
	taskIDs := make([]string, 0, len(candidates))
	for _, task := range candidates {
		taskIDs = append(taskIDs, task.ID)
	}
	return taskIDs
}

func markAgentDegradedForInfraClaim(projectRoot, agentID, role, taskID string, candidateTaskIDs []string, err error, authority *models.AgentAuthority) error {
	// ops.ClaimTask already degraded and wrapped this one; re-marking would
	// duplicate the anomaly.
	if errors.Is(err, ops.ErrAgentDegraded) {
		return err
	}
	classification := ops.ClassifyInfraClaimError(err)
	if !classification.IsInfra {
		return nil
	}
	var setupErr *ops.PostWorktreeSetupError
	errors.As(err, &setupErr)
	if markErr := ops.MarkAgentDegraded(ops.MarkAgentDegradedInput{
		ProjectRoot:    projectRoot,
		AgentID:        agentID,
		Role:           role,
		Reason:         string(classification.Reason),
		LastTask:       taskID,
		CandidateTasks: candidateTaskIDs,
		LastError:      err.Error(),
		RecoverHint:    classification.RecoverHint,
		DegradedBy:     "claim_loop",
		Authority:      authority,
		SetupOutcome:   setupErr,
	}); markErr != nil {
		if errors.Is(markErr, ops.ErrSetupFailureSuperseded) {
			// A later setup of this worktree passed: not an infrastructure
			// fault of this agent, so its caller handles the plain error.
			GetLogger().Info("Setup failure superseded by a later passing setup; not degrading",
				"agent_id", agentID, "task_id", taskID)
			return nil
		}
		return fmt.Errorf("failed to mark agent degraded after claim infrastructure failure: %w", markErr)
	}
	// Wrap rather than return the bare sentinel: callers log this error, and the
	// cause names the command and worktree. Mirrors ops.ClaimTask's shape so both
	// sides of the ErrAgentDegraded alias render identically.
	return fmt.Errorf("%w: %w", ErrAgentDegraded, err)
}

// claimCoderTask wraps claimDoerTask for backward compatibility.
func claimCoderTask(projectRoot, agentID string, bb *db.Blackboard) (taskID, worktree string, err error) {
	return claimDoerTask(projectRoot, agentID, models.RoleCoder, bb)
}

// shuffledByPriorityTier returns candidates in the highest-priority tier,
// shuffled randomly. This prevents multiple agents from deterministically
// converging on the same task.
func shuffledByPriorityTier(candidates []*models.Task) []*models.Task {
	tier := models.TopPriorityTier(candidates)
	rand.Shuffle(len(tier), func(i, j int) {
		tier[i], tier[j] = tier[j], tier[i]
	})
	return tier
}

func claimReviewerTaskForRole(projectRoot, agentID, role, targetTaskID string, leaseDuration int, bb *db.Blackboard) (taskID, worktree, reviewCommit string, err error) {
	return reviewerClaimTuple(claimReviewerTaskForRoleWithOptionalAuthority(projectRoot, agentID, role, targetTaskID, leaseDuration, nil, bb))
}

func claimReviewerTaskForRoleWithAuthority(projectRoot string, authority models.AgentAuthority, role, targetTaskID string, leaseDuration int, bb *db.Blackboard, sessions ...*ops.ValidationSession) (taskID, worktree, reviewCommit string, err error) {
	return reviewerClaimTuple(claimReviewerTaskForRoleWithOptionalAuthority(projectRoot, authority.ID, role, targetTaskID, leaseDuration, &authority, bb, sessions...))
}

// reviewerClaimAttemptHook observes every reviewer claim attempt before the
// operation runs. Nil outside tests, which use it to count real attempts
// across supervisor loop iterations.
var reviewerClaimAttemptHook func()

// reviewerClaimTuple preserves the legacy adapters' results.
func reviewerClaimTuple(result *ops.ClaimReviewerTaskResult, err error) (taskID, worktree, reviewCommit string, claimErr error) {
	if err != nil {
		return "", "", "", err
	}
	return result.TaskID, result.Worktree, result.ReviewCommit, nil
}

// claimReviewerTaskForRoleWithOptionalAuthority keeps candidate failure evidence available to
// the strategy while the compatibility adapters retain their tuple results.
func claimReviewerTaskForRoleWithOptionalAuthority(projectRoot, agentID, role, targetTaskID string, leaseDuration int, authority *models.AgentAuthority, bb *db.Blackboard, sessions ...*ops.ValidationSession) (*ops.ClaimReviewerTaskResult, error) {
	logger := GetLogger()
	var session *ops.ValidationSession
	if len(sessions) > 0 {
		session = sessions[0]
	}

	if reviewerClaimAttemptHook != nil {
		reviewerClaimAttemptHook()
	}
	result, err := ops.ClaimReviewerTask(ops.ClaimReviewerTaskInput{
		ProjectRoot:   projectRoot,
		AgentID:       agentID,
		Role:          role,
		TaskID:        targetTaskID,
		LeaseDuration: leaseDuration,
		Authority:     authority,
		Session:       session,
	})
	if err != nil {
		// Debug only: the reviewer strategy owns the error-level line and emits
		// it once per quarantined key, not once per loop iteration.
		logger.Debug("Review claim error", "error", err)
		return nil, err
	}

	return result, nil
}

// claimReviewerTask wraps claimReviewerTaskForRole for backward compatibility.
func claimReviewerTask(projectRoot, agentID string, leaseDuration int, bb *db.Blackboard) (taskID, worktree, reviewCommit string, err error) {
	return claimReviewerTaskForRole(projectRoot, agentID, models.RoleCodeReviewer, "", leaseDuration, bb)
}

// releaseReviewerClaimQuietly releases a reviewer claim, logging but not
// propagating errors. Used in supervisor recovery paths for transient failures
// where ops.ForceBlockReviewerTask was NOT called.
func releaseReviewerClaimQuietly(projectRoot, taskID string, authority models.AgentAuthority) error {
	_, err := ops.ReleaseClaimWithAuthority(projectRoot, taskID, string(roles.ClaimReviewer), true, "supervisor: worktree check transient failure", authority)
	if err != nil {
		GetLogger().Warn("Failed to release reviewer claim during recovery",
			"task_id", taskID, "error", err)
		if ops.IsAgentAuthorityError(err) {
			return err
		}
	}
	return nil
}

// handleAvailableTransitions creates child tasks from pipeline transitions
// and adds them to the current sprint's scope.
// Called from orchestrator PreWork after checkpoint acknowledgment, which may
// be automatic, so a reviewed plan hand-off needs the orchestrator's pass.
func handleAvailableTransitions(projectRoot string) error {
	report, err := ops.ExecuteTransitionsReportWith(projectRoot, "", ops.AdmitReviewed)
	if err != nil {
		return err
	}

	logger := GetLogger()
	for _, r := range report.Results {
		logger.Info("Pipeline transition executed",
			"source_task", r.SourceTaskID,
			"transition", r.TransitionName,
			"children_created", len(r.ChildTaskIDs))
	}
	for _, f := range report.Failures {
		logger.Warn("Pipeline transition failed",
			"source_task", f.SourceTaskID,
			"transition", f.Transition,
			"error", f.Error)
	}

	return nil
}

// handleAutoTransitions executes only auto-trigger pipeline transitions
// (e.g., integration-to-fix) for merged tasks. Called from reviewer PreWork
// after merges, so children are created in the same PreWork cycle.
// Manual transitions remain gated by the orchestrator checkpoint flow.
func handleAutoTransitions(projectRoot string) error {
	report, err := ops.ExecuteTransitionsReportWith(projectRoot, "auto", ops.AdmitReviewed)
	if err != nil {
		return err
	}

	logger := GetLogger()
	for _, r := range report.Results {
		logger.Info("Auto transition executed",
			"source_task", r.SourceTaskID,
			"transition", r.TransitionName,
			"children_created", len(r.ChildTaskIDs))
	}
	for _, f := range report.Failures {
		logger.Warn("Auto transition failed",
			"source_task", f.SourceTaskID,
			"transition", f.Transition,
			"error", f.Error)
	}

	return nil
}

// handleCleanTaskCleanup removes worktrees for tasks at pipeline-defined clean
// terminal states (e.g., INTEGRATION_ANALYSIS_CLEAN). These tasks bypass the
// merge path that normally handles worktree cleanup.
func handleCleanTaskCleanup(projectRoot string, authority models.AgentAuthority) error {
	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())
	// Locked read, unlike the other supervisor reads (D100): the authority check
	// below is the only fence in front of DeleteWorktree's effects, so it must
	// observe a generation replacement published by a writer holding the lock.
	state, err := bb.Read()
	if err != nil {
		return err
	}

	cfg, cfgErr := pipeline.LoadFrozen(projectRoot)
	if cfgErr != nil {
		return nil // no pipeline, skip
	}
	resolver := pipeline.NewResolver(cfg)

	logger := GetLogger()
	for _, task := range state.Tasks {
		if task.Worktree == nil || task.RolePair == "" {
			continue
		}
		cleanStatus, err := resolver.CleanStatus(task.RolePair)
		if err != nil || task.Status != cleanStatus {
			continue
		}
		if err := ops.RequireAgentAuthority(state, authority); err != nil {
			return err
		}
		result, err := ops.DeleteWorktree(projectRoot, task.ID)
		if err != nil {
			logger.Warn("Failed to cleanup clean task worktree", "task_id", task.ID, "error", err)
			continue
		}
		for _, w := range result.Warnings {
			logger.Warn("Clean task worktree cleanup warning", "task_id", task.ID, "warning", w)
		}
		if result.Existed {
			logger.Info("Cleaned up worktree for clean-terminal task", "task_id", task.ID)
		}

		// Finalize: mirror non-git cleanup from the merge path.
		// Records completion handoff event and releases the assigned agent.
		taskID := task.ID
		if err := ops.FinalizeCleanTerminalTask(bb, authority, taskID); err != nil {
			logger.Warn("Failed to finalize clean-terminal task", "task_id", taskID, "error", err)
		}
	}
	return nil
}

func logTaskSubmissionIfCompleted(bb *db.Blackboard, taskID, agentID string, pr models.PipelineResolver) error {
	state, err := bb.ReadSnapshot()
	if err != nil {
		return fmt.Errorf("failed to read state: %w", err)
	}

	if task := state.FindTask(taskID); task != nil {
		if models.IsSubmittedStatus(task, pr) {
			reviewCommit := "unknown"
			if task.ReviewCommit != nil {
				reviewCommit = *task.ReviewCommit
			}

			GetLogger().Info("Task submitted for review",
				"task_id", task.ID,
				"review_commit", reviewCommit,
				"agent_id", agentID,
				"integration_fix", task.IntegrationFix)

			return nil
		}

		if models.IsExecutingStatus(task, pr) {
			GetLogger().Warn("Agent exited with task still claimed",
				"task_id", task.ID,
				"agent_id", agentID,
				"hint", "Agent may have been interrupted or encountered an issue")
			return nil
		}

		if task.Status == models.TaskStatusBlocked {
			GetLogger().Info("Agent blocked task due to dependency issue",
				"task_id", task.ID,
				"agent_id", agentID)
			return nil
		}

		return nil
	}

	return nil
}
