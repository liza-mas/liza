package agent

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"sort"
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
	return claimDoerTaskWithOptionalAuthority(projectRoot, agentID, role, nil, bb)
}

func claimDoerTaskWithAuthority(projectRoot string, authority models.AgentAuthority, role string, bb *db.Blackboard, sessions ...*ops.ValidationSession) (taskID, worktree string, err error) {
	return claimDoerTaskWithOptionalAuthority(projectRoot, authority.ID, role, &authority, bb, sessions...)
}

func claimDoerTaskWithOptionalAuthority(projectRoot, agentID, role string, authority *models.AgentAuthority, bb *db.Blackboard, sessions ...*ops.ValidationSession) (taskID, worktree string, err error) {
	logger := GetLogger()
	var session *ops.ValidationSession
	if len(sessions) > 0 {
		session = sessions[0]
	}

	handoffResult, err := ops.ResumeHandoff(ops.ResumeHandoffInput{
		ProjectRoot: projectRoot,
		AgentID:     agentID,
		Authority:   authority,
		Session:     session,
	})
	if err != nil {
		return "", "", err
	}
	if handoffResult.Found {
		logger.Info("Resuming claimed task from handoff", "task_id", handoffResult.TaskID, "agent_id", agentID)
		if setupErr := ensureDoerWorktreeSetup(
			projectRoot, agentID, role, handoffResult.TaskID, handoffResult.Worktree, authority,
		); setupErr != nil {
			return "", "", setupErr
		}
		return handoffResult.TaskID, handoffResult.Worktree, nil
	}

	ownedResult, err := ops.ResumeOwnedTask(ops.ResumeOwnedTaskInput{
		ProjectRoot: projectRoot,
		AgentID:     agentID,
		Authority:   authority,
		Session:     session,
	})
	if err != nil {
		return "", "", err
	}
	if ownedResult.Found {
		logger.Info("Resuming owned executing task", "task_id", ownedResult.TaskID, "agent_id", agentID)
		if setupErr := ensureDoerWorktreeSetup(
			projectRoot, agentID, role, ownedResult.TaskID, ownedResult.Worktree, authority,
		); setupErr != nil {
			return "", "", setupErr
		}
		return ownedResult.TaskID, ownedResult.Worktree, nil
	}
	if ownedResult.Blocked {
		logger.Warn("Blocked owned executing task during resume", "task_id", ownedResult.BlockedTaskID, "agent_id", agentID, "reason", ownedResult.BlockReason)
	}

	state, err := bb.Read()
	if err != nil {
		return "", "", fmt.Errorf("failed to read state: %w", err)
	}
	if authority != nil {
		if err := ops.RequireAgentAuthority(state, *authority); err != nil {
			return "", "", err
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
	tier := shuffledByPriorityTier(candidates)

	if len(tier) == 0 {
		return "", "", fmt.Errorf("no claimable tasks found")
	}

	// Try each candidate in the shuffled tier until one succeeds.
	var lastErr error
	candidateIDs := taskIDsFromCandidates(tier)
	for _, task := range tier {
		var result *ops.ClaimResult
		var claimErr error
		if authority == nil {
			result, claimErr = ops.ClaimTask(projectRoot, task.ID, agentID)
		} else {
			result, claimErr = ops.ClaimTaskWithAuthority(projectRoot, task.ID, *authority, session)
		}
		if claimErr != nil {
			logger.Warn("Claim attempt failed, trying next candidate",
				"task_id", task.ID, "error", claimErr)
			if classifyErr := markAgentDegradedForInfraClaim(projectRoot, agentID, role, task.ID, candidateIDs, claimErr, authority); classifyErr != nil {
				return "", "", classifyErr
			}
			lastErr = claimErr
			continue
		}
		var clearErr error
		if authority == nil {
			clearErr = ops.ClearAgentDegraded(projectRoot, agentID)
		} else {
			clearErr = ops.ClearAgentDegradedWithAuthority(projectRoot, *authority)
		}
		if clearErr != nil {
			if ops.IsAgentAuthorityError(clearErr) {
				return "", "", clearErr
			}
			logger.Warn("Failed to clear degraded agent health after successful claim",
				"agent_id", agentID, "error", clearErr)
		}
		for _, w := range result.Warnings {
			logger.Warn("Claim warning", "task_id", result.TaskID, "warning", w)
		}
		return result.TaskID, result.WorktreeRel, nil
	}

	return "", "", fmt.Errorf("all %d candidates in top priority tier failed to claim: %w", len(tier), lastErr)
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
	state, err := db.For(paths.New(projectRoot).StatePath()).Read()
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
	setupErr := ops.RunPostWorktreeCmd(*state.Config.PostWorktreeCmd, filepath.Join(projectRoot, worktreeRel))
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
	if markErr := ops.MarkAgentDegraded(ops.MarkAgentDegradedInput{
		ProjectRoot:    projectRoot,
		AgentID:        agentID,
		Role:           role,
		Reason:         classification.Reason,
		LastTask:       taskID,
		CandidateTasks: candidateTaskIDs,
		LastError:      err.Error(),
		RecoverHint:    classification.RecoverHint,
		DegradedBy:     "claim_loop",
		Authority:      authority,
	}); markErr != nil {
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
// where blockReviewerTask was NOT called.
func releaseReviewerClaimQuietly(projectRoot, taskID string, authority models.AgentAuthority) error {
	_, err := ops.ReleaseClaimWithAuthority(projectRoot, taskID, roles.ClaimReviewer, true, "supervisor: worktree check transient failure", authority)
	if err != nil {
		GetLogger().Warn("Failed to release reviewer claim during recovery",
			"task_id", taskID, "error", err)
		if ops.IsAgentAuthorityError(err) {
			return err
		}
	}
	return nil
}

// mergeGateInput holds the inputs for the merge gate evaluation.
type mergeGateInput struct {
	task              *models.Task
	agents            map[string]models.Agent
	effectiveQuorum   int
	providerDiversity string // "preferred" or ""
	reviewerRole      string // workflow role name for reviewers in this role-pair
}

// mergeGateResult holds the outcome of the merge gate evaluation.
type mergeGateResult struct {
	proceed    bool
	extra      map[string]any // diversity fields for merge history
	skipReason string         // non-empty when proceed is false
}

// evaluateMergeGate checks quorum defense-in-depth and evaluates provider diversity.
// Pure function — all inputs are passed explicitly for testability.
func evaluateMergeGate(input mergeGateInput) *mergeGateResult {
	result := &mergeGateResult{proceed: true}

	// Defense-in-depth: quorum check
	if input.task.ApprovalCount() < input.effectiveQuorum {
		result.proceed = false
		result.skipReason = fmt.Sprintf("approval count %d < effective quorum %d",
			input.task.ApprovalCount(), input.effectiveQuorum)
		return result
	}

	// No diversity evaluation when not configured
	if input.providerDiversity != "preferred" {
		return result
	}

	// Diversity achieved — approvals come from different providers
	if input.task.HasProviderDiversity() {
		result.extra = map[string]any{"diversity_achieved": true}
		return result
	}

	// Diversity not achieved — check if it's achievable in the reviewer pool
	providers := make(map[string]bool)
	for _, agent := range input.agents {
		if agent.Role == input.reviewerRole {
			providers[agent.Provider] = true
		}
	}

	if len(providers) <= 1 {
		// All reviewers share one provider (or no reviewers registered)
		reason := "no reviewer agents registered"
		for p := range providers {
			reason = fmt.Sprintf("all reviewers use provider %s", p)
		}
		result.extra = map[string]any{
			"diversity_not_achievable": true,
			"diversity_reason":         reason,
		}
	} else {
		// Different providers exist but diversity wasn't achieved in approvals
		result.extra = map[string]any{"diversity_not_met": true}
	}

	return result
}

func handleApprovedMerges(projectRoot, agentID string, bb *db.Blackboard, pr models.PipelineResolver) error {
	return handleApprovedMergesWithOptionalAuthority(projectRoot, agentID, nil, bb, pr)
}

func handleApprovedMergesWithAuthority(projectRoot string, authority models.AgentAuthority, bb *db.Blackboard, pr models.PipelineResolver) error {
	return handleApprovedMergesWithOptionalAuthority(projectRoot, authority.ID, &authority, bb, pr)
}

func handleApprovedMergesWithOptionalAuthority(projectRoot, agentID string, authority *models.AgentAuthority, bb *db.Blackboard, pr models.PipelineResolver) error {
	logger := GetLogger()
	state, err := bb.Read()
	if err != nil {
		return err
	}
	if authority != nil {
		if err := ops.RequireAgentAuthority(state, *authority); err != nil {
			return err
		}
	}

	// Load the concrete resolver once for quorum and diversity lookups.
	cfg, cfgErr := pipeline.LoadFrozen(projectRoot)
	if cfgErr != nil {
		return fmt.Errorf("failed to load pipeline config: %w", cfgErr)
	}
	resolver := pipeline.NewResolver(cfg)

	for i := range state.Tasks {
		task := &state.Tasks[i]
		if approvedMergePending(task, state, agentID, pr) {

			// Resolve effective impact and quorum for merge gate
			effectiveImpact := ops.ResolveEffectiveImpact(task.History)
			effectiveQuorum, qErr := resolver.EffectiveQuorum(task.RolePair, effectiveImpact)
			if qErr != nil {
				logger.Warn("Failed to resolve quorum, skipping merge",
					"task_id", task.ID, "error", qErr)
				continue
			}

			// Get provider diversity config for this impact level
			diversity, dErr := resolver.ProviderDiversity(task.RolePair, effectiveImpact)
			if dErr != nil {
				logger.Warn("Failed to resolve diversity policy, skipping merge",
					"task_id", task.ID, "error", dErr)
				continue
			}

			// Get reviewer role for this role-pair
			reviewerRole, rErr := pr.ReviewerRole(task.RolePair)
			if rErr != nil {
				logger.Warn("Failed to resolve reviewer role, skipping merge",
					"task_id", task.ID, "error", rErr)
				continue
			}

			// Evaluate merge gate: quorum defense-in-depth + provider diversity
			gate := evaluateMergeGate(mergeGateInput{
				task:              task,
				agents:            state.Agents,
				effectiveQuorum:   effectiveQuorum,
				providerDiversity: diversity,
				reviewerRole:      reviewerRole,
			})

			if !gate.proceed {
				logger.Warn("Merge gate: quorum defense-in-depth failed, skipping merge",
					"task_id", task.ID, "reason", gate.skipReason)
				continue
			}

			logger.Info("Merging approved task", "task_id", task.ID)

			var result *ops.MergeResult
			if authority == nil {
				result, err = ops.MergeWorktree(projectRoot, task.ID, agentID, gate.extra)
			} else {
				result, err = ops.MergeWorktreeWithAuthority(projectRoot, task.ID, *authority, gate.extra)
			}
			if err != nil {
				if ops.IsAgentAuthorityError(err) {
					return err
				}
				var integrationErr *ops.IntegrationFailedError
				if errors.As(err, &integrationErr) {
					logArgs := []any{
						"task_id", task.ID,
						"reason", integrationErr.Reason,
					}
					if integrationErr.TestOutput != "" {
						logArgs = append(logArgs, "test_output", integrationErr.TestOutput)
					}
					if integrationErr.RollbackError != nil {
						logArgs = append(logArgs, "rollback_error", integrationErr.RollbackError)
					}
					logger.Warn("Integration failed", logArgs...)
					continue
				}
				logger.Warn("Failed to merge task, will retry",
					"task_id", task.ID,
					"error", err)
				continue
			}

			for _, w := range result.Warnings {
				logger.Warn("Merge cleanup warning", "task_id", task.ID, "warning", w)
			}

			logger.Info("Successfully merged task", "task_id", task.ID)
		}
	}

	return nil
}

func hasPendingMerges(bb *db.Blackboard, agentID string, pr models.PipelineResolver) bool {
	state, err := bb.ReadCached()
	if err != nil {
		return false // Safe default: proceed to normal wait
	}
	return hasPendingMergesInState(state, agentID, pr)
}

// hasPendingMergesInState answers the same question against a state the caller
// already holds — wait predicates run on every watcher event and must not read.
func hasPendingMergesInState(state *models.State, agentID string, pr models.PipelineResolver) bool {
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if approvedMergePending(task, state, agentID, pr) {
			return true
		}
	}
	return false
}

func approvedMergePending(task *models.Task, state *models.State, agentID string, pr models.PipelineResolver) bool {
	if !models.IsApprovedForMerge(task, pr) || task.LastApprover() == "" {
		return false
	}
	if task.MergeCommit != nil && len(task.IntegrationFailure) == 0 {
		return false
	}
	return approvedMergeOwner(task, state, pr) == agentID
}

func approvedMergeOwner(task *models.Task, state *models.State, pr models.PipelineResolver) string {
	reviewerRole, err := pr.ReviewerRole(task.RolePair)
	if err != nil {
		return ""
	}

	eligible := make([]string, 0, len(state.Agents))
	now := time.Now().UTC()
	nilLeaseHeartbeatWindow := models.NormalizeHeartbeatInterval(state.Config.HeartbeatInterval) + models.LeaseExpiryGracePeriod
	for id, agent := range state.Agents {
		if agent.Role != reviewerRole || agent.Generation == "" {
			continue
		}
		liveRegistration := agent.LeaseExpires != nil && agent.LeaseExpires.After(now)
		if agent.LeaseExpires == nil {
			liveRegistration = !agent.Heartbeat.IsZero() && agent.Heartbeat.After(now.Add(-nilLeaseHeartbeatWindow))
		}
		if !liveRegistration {
			continue
		}
		if health, ok := state.AgentHealth[id]; ok && health.IsCurrentDegradedFor(agent) {
			continue
		}
		eligible = append(eligible, id)
	}
	sort.Strings(eligible)

	lastApprover := task.LastApprover()
	for _, id := range eligible {
		if id == lastApprover {
			return id
		}
	}
	if len(eligible) == 0 {
		return ""
	}
	return eligible[0]
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
		if err := ops.ModifyWithAgentAuthority(bb, authority, func(s *models.State) error {
			t := s.FindTask(taskID)
			if t == nil {
				return nil
			}
			if t.AssignedTo != nil {
				if a, ok := s.Agents[*t.AssignedTo]; ok {
					if a.CurrentTask != nil && *a.CurrentTask == taskID {
						s.ReleaseAgent(*t.AssignedTo)
					}
				}
			}
			t.HandoffEvents = append(t.HandoffEvents, models.HandoffEvent{
				Timestamp: time.Now(),
				Agent:     "system",
				Trigger:   models.HandoffTriggerCompletion,
			})
			return nil
		}); err != nil {
			logger.Warn("Failed to finalize clean-terminal task", "task_id", taskID, "error", err)
		}
	}
	return nil
}

func logTaskSubmissionIfCompleted(bb *db.Blackboard, taskID, agentID string, pr models.PipelineResolver) error {
	state, err := bb.Read()
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
