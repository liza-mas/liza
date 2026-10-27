package agent

import (
	"errors"
	"fmt"

	"github.com/liza-mas/liza/internal/agentlaunch"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/pipeline"
)

// Approved-merge handling: the merge gate (quorum defense-in-depth and provider
// diversity), the per-candidate merge batch a reviewer runs for the merges it
// owns, and the pending-merge predicates the wait loops share.

// mergeGateInput holds the inputs for the merge gate evaluation.
type mergeGateInput struct {
	task              *models.Task
	agents            map[string]models.Agent
	effectiveQuorum   int
	providerDiversity string // "preferred" or ""
	reviewerRole      string // workflow role name for reviewers in this role-pair
	// keys derives the provider keys diversity compares; the zero value compares
	// recorded tool names exactly.
	keys agentlaunch.ProviderKeys
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
	if input.providerDiversity != string(pipeline.ProviderDiversityPreferred) {
		return result
	}

	// Diversity achieved — approvals come from different providers
	if ops.ApprovalsHaveProviderDiversity(input.task.Approvals, input.keys) {
		result.extra = map[string]any{"diversity_achieved": true}
		return result
	}

	// Diversity not achieved — check if it's achievable in the reviewer pool
	// Pool members group by provider key; a group is named by its smallest
	// recorded provider so a mixed alias group renders deterministically.
	providers := make(map[string]string)
	for _, agent := range input.agents {
		if agent.Role != input.reviewerRole {
			continue
		}
		key := input.keys.Key(agent.Provider)
		if label, ok := providers[key]; !ok || agent.Provider < label {
			providers[key] = agent.Provider
		}
	}

	if len(providers) <= 1 {
		// All reviewers share one provider (or no reviewers registered)
		reason := "no reviewer agents registered"
		for _, label := range providers {
			reason = fmt.Sprintf("all reviewers use provider %s", label)
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
	state, err := bb.ReadSnapshot()
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
	// One catalog capture and one key derivation from the evaluated snapshot's
	// config serve every candidate of the batch; no blackboard transaction is open.
	keys := agentlaunch.NewProviderKeys(agentlaunch.BuiltInAgentTools(), state.Config)

	for i := range state.Tasks {
		task := &state.Tasks[i]
		if !ops.ApprovedMergePending(task, state, agentID, pr) {
			continue
		}
		gate, ok := resolveApprovedMergeGate(task, state, resolver, pr, keys)
		if !ok {
			continue
		}
		if err := mergeApprovedTask(projectRoot, agentID, authority, task, gate.extra); err != nil {
			return err
		}
	}

	return nil
}

// resolveApprovedMergeGate resolves the effective quorum, provider-diversity
// policy and reviewer role of an approved task and evaluates the merge gate.
// It reports false, after logging why, when the task must be skipped.
func resolveApprovedMergeGate(task *models.Task, state *models.State, resolver *pipeline.Resolver, pr models.PipelineResolver, keys agentlaunch.ProviderKeys) (*mergeGateResult, bool) {
	logger := GetLogger()

	// Resolve effective impact and quorum for merge gate
	effectiveImpact := ops.ResolveEffectiveImpact(task.History)
	effectiveQuorum, qErr := resolver.EffectiveQuorum(task.RolePair, pipeline.ReviewImpact(effectiveImpact))
	if qErr != nil {
		logger.Warn("Failed to resolve quorum, skipping merge",
			"task_id", task.ID, "error", qErr)
		return nil, false
	}

	// Get provider diversity config for this impact level
	diversity, dErr := resolver.ProviderDiversity(task.RolePair, pipeline.ReviewImpact(effectiveImpact))
	if dErr != nil {
		logger.Warn("Failed to resolve diversity policy, skipping merge",
			"task_id", task.ID, "error", dErr)
		return nil, false
	}

	// Get reviewer role for this role-pair
	reviewerRole, rErr := pr.ReviewerRole(task.RolePair)
	if rErr != nil {
		logger.Warn("Failed to resolve reviewer role, skipping merge",
			"task_id", task.ID, "error", rErr)
		return nil, false
	}

	// Evaluate merge gate: quorum defense-in-depth + provider diversity
	gate := evaluateMergeGate(mergeGateInput{
		task:              task,
		agents:            state.Agents,
		effectiveQuorum:   effectiveQuorum,
		providerDiversity: string(diversity),
		reviewerRole:      reviewerRole,
		keys:              keys,
	})
	if !gate.proceed {
		logger.Warn("Merge gate: quorum defense-in-depth failed, skipping merge",
			"task_id", task.ID, "reason", gate.skipReason)
		return nil, false
	}
	return gate, true
}

// mergeApprovedTask merges one approved task. It returns an error only when
// the batch must stop (lost authority or a halt); any other failure is logged
// and the next candidate still runs.
func mergeApprovedTask(projectRoot, agentID string, authority *models.AgentAuthority, task *models.Task, extra map[string]any) error {
	logger := GetLogger()
	logger.Info("Merging approved task", "task_id", task.ID)

	var result *ops.MergeResult
	var err error
	if authority == nil {
		result, err = ops.MergeWorktree(projectRoot, task.ID, agentID, extra)
	} else {
		result, err = ops.MergeWorktreeWithAuthority(projectRoot, task.ID, *authority, extra)
	}
	if err != nil {
		// Halted: stop the batch; no later merge would be admitted.
		if ops.IsAgentAuthorityError(err) || errors.Is(err, ops.ErrSystemHalted) {
			return err
		}
		logMergeFailure(task.ID, err)
		return nil
	}

	for _, w := range result.Warnings {
		logger.Warn("Merge cleanup warning", "task_id", task.ID, "warning", w)
	}

	logger.Info("Successfully merged task", "task_id", task.ID)
	return nil
}

// logMergeFailure reports a merge failure the batch survives.
func logMergeFailure(taskID string, err error) {
	logger := GetLogger()
	var integrationErr *ops.IntegrationFailedError
	if errors.As(err, &integrationErr) {
		logArgs := []any{
			"task_id", taskID,
			"reason", integrationErr.Reason,
		}
		if integrationErr.TestOutput != "" {
			logArgs = append(logArgs, "test_output", integrationErr.TestOutput)
		}
		if integrationErr.RollbackError != nil {
			logArgs = append(logArgs, "rollback_error", integrationErr.RollbackError)
		}
		logger.Warn("Integration failed", logArgs...)
		return
	}
	logger.Warn("Failed to merge task, will retry",
		"task_id", taskID,
		"error", err)
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
		if ops.ApprovedMergePending(task, state, agentID, pr) {
			return true
		}
	}
	return false
}
