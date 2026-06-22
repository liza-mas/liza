package ops

import (
	stderrors "errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	gitpkg "github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/identity"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/scipsearch"
	"github.com/liza-mas/liza/internal/stacklit"
)

const integrationOperationSubmitForReview = "submit-for-review"

// SubmitForReviewResult contains the outcome of submitting a task for review.
type SubmitForReviewResult struct {
	TaskID       string   `json:"task_id"`
	ReviewCommit string   `json:"review_commit"`
	AgentID      string   `json:"agent_id"`
	Warnings     []string `json:"warnings,omitempty"`
}

var (
	submitReviewRefreshIndexes       = scipsearch.RefreshIndexes
	submitReviewRefreshStacklitIndex = stacklit.RefreshIndex
)

// SubmitForReview validates that commitRef resolves to the worktree HEAD before rebase,
// rebases the task branch onto the integration branch to catch conflicts early,
// then atomically transitions the task to READY_FOR_REVIEW.
// No terminal I/O.
func SubmitForReview(projectRoot, taskID, commitRef, agentID string) (*SubmitForReviewResult, error) {
	if taskID == "" {
		return nil, &PreconditionError{Reason: "task ID is required"}
	}
	if commitRef == "" {
		return nil, &PreconditionError{Reason: "commit ref is required"}
	}
	if agentID == "" {
		return nil, &PreconditionError{Reason: fmt.Sprintf("%s is required", brand.EnvName("AGENT_ID"))}
	}

	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())

	runtimeRole, err := identity.ExtractRole(agentID)
	if err != nil {
		return nil, &PreconditionError{Reason: fmt.Sprintf("invalid agent ID format: %s (%v)", agentID, err)}
	}

	// Phase 1: Read state to get config and validate preconditions
	state, task, err := readTaskState(bb, taskID)
	if err != nil {
		return nil, err
	}

	// Resolve expected statuses from pipeline config
	resolver, _, resolverErr := loadResolver(projectRoot)
	if resolverErr != nil {
		return nil, resolverErr
	}
	if task.RolePair == "" {
		return nil, &PreconditionError{Reason: fmt.Sprintf("task %s has no role_pair set", taskID)}
	}
	expectedCurrentStatus, err := resolver.ExecutingStatus(task.RolePair)
	if err != nil {
		return nil, &PreconditionError{Reason: fmt.Sprintf("unrecognized role-pair %q — check pipeline.yaml config", task.RolePair)}
	}
	targetSubmittedStatus, err := resolver.SubmittedStatus(task.RolePair)
	if err != nil {
		return nil, &PreconditionError{Reason: fmt.Sprintf("unrecognized role-pair %q — check pipeline.yaml config", task.RolePair)}
	}
	pipelineTransitions := BuildPipelineTransitions(resolver)

	if task.Status != expectedCurrentStatus {
		return nil, &PreconditionError{Reason: fmt.Sprintf("task %s is not %s (current status: %s)", taskID, expectedCurrentStatus, task.Status)}
	}

	if task.AssignedTo == nil || *task.AssignedTo != agentID {
		currentAgent := "none"
		if task.AssignedTo != nil {
			currentAgent = *task.AssignedTo
		}
		return nil, &PreconditionError{Reason: fmt.Sprintf("task %s is not assigned to agent %s (currently assigned to: %s)", taskID, agentID, currentAgent)}
	}

	if task.Worktree == nil {
		return nil, &PreconditionError{Reason: fmt.Sprintf("task %s has no worktree", taskID)}
	}

	// Pre-execution checkpoint required before submission
	if !HasCheckpoint(task.History, agentID) {
		return nil, &PreconditionError{Reason: fmt.Sprintf("task %s: pre-execution checkpoint required before submission (use liza_write_checkpoint)", taskID)}
	}
	if err := validateOutputArtifactRefScalars(taskID, task.Output); err != nil {
		return nil, err
	}

	// Phase 2: Execute git operations outside the lock
	g := gitpkg.New(projectRoot)
	wtPath := g.GetWorktreePath(taskID)

	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		return nil, &errors.WorktreeContextError{
			Operation: integrationOperationSubmitForReview,
			TaskID:    taskID,
			Reason:    "worktree directory does not exist",
			Err:       err,
		}
	}

	wtBranch, err := g.GetWorktreeBranch(wtPath)
	if err != nil {
		return nil, &OperationalError{
			Code:    "git_operation",
			Phase:   "resolve-worktree-branch",
			Message: "failed to determine worktree branch",
			Details: map[string]any{
				"operation":     integrationOperationSubmitForReview,
				"task_id":       taskID,
				"recovery_hint": "Inspect the task worktree git metadata, ensure the branch resolves, then retry submit-for-review.",
			},
			Err: err,
		}
	}

	expectedBranch := paths.TaskBranchPrefix + taskID
	if wtBranch != expectedBranch {
		if wtBranch == "" {
			return nil, &PreconditionError{Reason: fmt.Sprintf("worktree is in detached HEAD state (expected branch: %s)", expectedBranch)}
		}
		return nil, &PreconditionError{Reason: fmt.Sprintf("worktree is on branch %s (expected: %s)", wtBranch, expectedBranch)}
	}

	preRebaseCommit, err := g.GetWorktreeHEAD(taskID)
	if err != nil {
		return nil, &OperationalError{
			Code:    "git_operation",
			Phase:   "pre-rebase-head",
			Message: "failed to read worktree HEAD",
			Details: map[string]any{
				"operation":     integrationOperationSubmitForReview,
				"task_id":       taskID,
				"recovery_hint": "Inspect the task worktree git state, ensure HEAD resolves, then retry submit-for-review.",
			},
			Err: err,
		}
	}
	resolvedCommit, err := g.ResolveWorktreeCommit(taskID, commitRef)
	if err != nil {
		return nil, &PreconditionError{Reason: fmt.Sprintf("provided commit ref %q could not be resolved in worktree", commitRef)}
	}
	if resolvedCommit != preRebaseCommit {
		return nil, &PreconditionError{Reason: fmt.Sprintf("provided commit ref %q resolved to %s, which does not match worktree HEAD %s", commitRef, resolvedCommit, preRebaseCommit)}
	}

	// TDD enforcement: code tasks must include test files (doer roles only).
	roleType, _ := resolver.RoleType(runtimeRole)
	requiresTDD, err := taskRequiresTDD(task, resolver)
	if err != nil {
		return nil, err
	}
	if roleType == "doer" && requiresTDD && task.BaseCommit != nil {
		testDiagnostics, err := AnalyzeTestFiles(g, taskID, *task.BaseCommit, preRebaseCommit)
		if err != nil {
			return nil, &OperationalError{
				Code:    "git_operation",
				Phase:   "check-test-files",
				Message: "failed to check for test files",
				Details: map[string]any{
					"operation":     integrationOperationSubmitForReview,
					"task_id":       taskID,
					"recovery_hint": "Inspect the task worktree git history and retry submit-for-review after the diff range can be analyzed.",
				},
				Err: err,
			}
		}
		if len(testDiagnostics.TestFilesMatched) == 0 && GetTDDWaiver(task.History, agentID) == "" {
			return nil, &PreconditionError{
				Reason:  fmt.Sprintf("task %s: code tasks must include test files (e.g. *_test.go, *.test.ts, test_*.py) — TDD is mandatory. For non-behavioral documentation/config/spec-only work, submit a pre-execution checkpoint with --tdd-not-required <justification>; reviewers verify the justification.", taskID),
				Details: testDiagnostics.Details(),
			}
		}
	}

	integrationBranch := state.Config.IntegrationBranch
	rebaseBase, err := g.GetCommitSHA(integrationBranch)
	if err != nil {
		return nil, &OperationalError{
			Code:    "git_operation",
			Phase:   "resolve-integration-head",
			Message: "failed to resolve integration branch HEAD",
			Details: map[string]any{
				"operation":          integrationOperationSubmitForReview,
				"task_id":            taskID,
				"integration_branch": integrationBranch,
				"recovery_hint":      "Ensure the configured integration branch exists and is fetchable, then retry submit-for-review.",
			},
			Err: err,
		}
	}

	if err := g.RebaseOnto(wtPath, rebaseBase); err != nil {
		// Abort rebase to restore clean worktree state — don't leave agents
		// in a mid-rebase state where they struggle with --continue/--abort.
		if abortErr := g.AbortRebase(wtPath); abortErr != nil {
			log.Printf("WARNING: failed to abort rebase in %s: %v", wtPath, abortErr)
		}

		// Only transition to INTEGRATION_FAILED for true merge conflicts.
		// Generic rebase failures (tool/env issues) are returned as-is so the
		// agent can retry without a state transition.
		var rebaseConflict *gitpkg.RebaseConflictError
		if !stderrors.As(err, &rebaseConflict) {
			return nil, &OperationalError{
				Code:    "git_operation",
				Phase:   "rebase",
				Message: "rebase failed (not a merge conflict)",
				Details: rebaseFailureDetails(err, integrationBranch, rebaseBase, preRebaseCommit),
				Err:     err,
			}
		}

		// Transition to INTEGRATION_FAILED so the orchestrator re-queues the task.
		// This catches conflicts early (before review), avoiding a wasted review cycle.
		// See also: markIntegrationFailed in wt_merge.go (sibling for post-review merge path).
		markErr := markSubmitRebaseConflict(bb, taskID, agentID, pipelineTransitions)
		if markErr != nil {
			return nil, &OperationalError{
				Code:    "state_write",
				Phase:   "mark-integration-failed",
				Message: fmt.Sprintf("rebase conflict on %s: transition to INTEGRATION_FAILED also failed — worktree is intact (rebase aborted), check task state with liza_get tasks/%s before retrying", taskID, taskID),
				Details: map[string]any{
					"operation":     integrationOperationSubmitForReview,
					"task_id":       taskID,
					"recovery_hint": "Re-read task state, resolve the state transition failure, then retry or mark the task blocked with the captured failure.",
				},
				Err: markErr,
			}
		}
		return nil, &IntegrationFailedError{Reason: IntegrationReasonMergeConflict}
	}

	postRebaseCommit, err := g.GetWorktreeHEAD(taskID)
	if err != nil {
		return nil, &OperationalError{
			Code:    "git_operation",
			Phase:   "post-rebase-head",
			Message: "failed to read worktree HEAD after rebase",
			Details: map[string]any{
				"operation":     integrationOperationSubmitForReview,
				"task_id":       taskID,
				"recovery_hint": "Inspect the task worktree git state, ensure HEAD resolves, then retry submit-for-review.",
			},
			Err: err,
		}
	}
	// Validate against the boundary that will be written below; the state copy
	// still contains the pre-submit claim base.
	reviewBoundaryTask := *task
	reviewBoundaryTask.BaseCommit = &rebaseBase
	if err := validateReviewBoundaryCommit(projectRoot, &reviewBoundaryTask, postRebaseCommit, rebaseBase); err != nil {
		return nil, err
	}

	indexWarnings := refreshSubmitReviewScipIndexes(wtPath, state.Config.ScipSearch)
	indexWarnings = append(indexWarnings, refreshSubmitReviewStacklitIndex(wtPath)...)

	// Phase 3: Atomic update with new commit SHA
	now := time.Now().UTC()

	err = bb.Modify(func(state *models.State) error {
		task := state.FindTask(taskID)
		if task == nil {
			return &errors.NotFoundError{Entity: "task", ID: taskID}
		}

		if task.Status != expectedCurrentStatus {
			return &PreconditionError{Reason: fmt.Sprintf("task %s is not %s (current status: %s)", taskID, expectedCurrentStatus, task.Status)}
		}

		if task.AssignedTo == nil || *task.AssignedTo != agentID {
			currentAgent := "none"
			if task.AssignedTo != nil {
				currentAgent = *task.AssignedTo
			}
			return &PreconditionError{Reason: fmt.Sprintf("task %s is not assigned to agent %s (currently assigned to: %s)", taskID, agentID, currentAgent)}
		}
		if err := validateOutputArtifactRefScalars(taskID, task.Output); err != nil {
			return err
		}

		if err := task.TransitionWith(targetSubmittedStatus, pipelineTransitions); err != nil {
			return err
		}
		task.ReviewCommit = &postRebaseCommit
		// Update BaseCommit from "branched from" to "rebased onto" — this is the
		// integration HEAD the rebase targeted, ensuring the reviewer diffs only
		// the coder's changes (not integration commits that landed since claim).
		task.BaseCommit = &rebaseBase

		task.History = append(task.History, models.TaskHistoryEntry{
			Time:   now,
			Event:  models.TaskEventSubmittedForReview,
			Agent:  &agentID,
			Commit: &postRebaseCommit,
		})

		task.HandoffEvents = append(task.HandoffEvents, models.HandoffEvent{
			Timestamp: now,
			Agent:     agentID,
			Trigger:   models.HandoffTriggerSubmission,
		})

		if agent, ok := state.Agents[agentID]; ok {
			agent.Status = models.AgentStatusWaiting
			agent.CurrentTask = nil
			agent.LeaseExpires = nil
			state.Agents[agentID] = agent
		}

		return nil
	})

	if err != nil {
		return nil, &OperationalError{
			Code:    "state_write",
			Phase:   "write-state",
			Message: "failed to submit task for review",
			Details: map[string]any{
				"operation":     integrationOperationSubmitForReview,
				"task_id":       taskID,
				"recovery_hint": "Re-read task state, resolve any concurrent state change or validation issue, then retry submit-for-review.",
			},
			Err: err,
		}
	}

	return &SubmitForReviewResult{
		TaskID:       taskID,
		ReviewCommit: postRebaseCommit,
		AgentID:      agentID,
		Warnings:     indexWarnings,
	}, nil
}

func refreshSubmitReviewScipIndexes(worktreePath string, configuredLanguages []string) []string {
	result, err := submitReviewRefreshIndexes(scipsearch.RefreshOptions{
		TargetRoot:          worktreePath,
		TargetKind:          scipsearch.TargetKindTaskWorktree,
		ConfiguredLanguages: configuredLanguages,
	})
	warnings := scipRefreshWarnings(result)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("scip-search: %v", err))
	}
	return warnings
}

func refreshSubmitReviewStacklitIndex(worktreePath string) []string {
	result, err := submitReviewRefreshStacklitIndex(stacklit.RefreshOptions{
		TargetRoot: worktreePath,
		TargetKind: stacklit.TargetKindTaskWorktree,
	})
	warnings := stacklitRefreshWarnings(result)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("stacklit: %v", err))
	}
	return warnings
}

func scipRefreshWarnings(result scipsearch.RefreshResult) []string {
	warnings := make([]string, 0, len(result.Failures))
	for _, failure := range result.Failures {
		warnings = append(warnings, fmt.Sprintf("scip-search %s: %s", failure.Language, failure.Diagnostic))
	}
	return warnings
}

func taskRequiresTDD(task *models.Task, resolver models.PipelineResolver) (bool, error) {
	if task.RolePair != "" {
		doerRole, err := resolver.DoerRole(task.RolePair)
		if err != nil {
			return false, &PreconditionError{Reason: fmt.Sprintf("task %s has invalid role_pair %q: %v", task.ID, task.RolePair, err)}
		}
		return models.TaskTypeForRole(doerRole) == models.TaskTypeCoding, nil
	}
	return task.EffectiveType() == models.TaskTypeCoding, nil
}

func rebaseFailureDetails(err error, integrationBranch, rebaseBase, preRebaseCommit string) map[string]any {
	details := map[string]any{
		"rebase_base_ref":       integrationBranch,
		"rebase_base_commit":    rebaseBase,
		"integration_branch":    integrationBranch,
		"integration_head":      rebaseBase,
		"pre_rebase_head":       preRebaseCommit,
		"recovery_hint":         "Inspect the task worktree, resolve the git rebase failure or abort any stale rebase state, then retry submit-for-review.",
		"stdout_stderr_excerpt": truncateForDiagnostics(err.Error(), 2000),
	}

	var rebaseErr *gitpkg.RebaseError
	if stderrors.As(err, &rebaseErr) {
		details["command"] = strings.Join(rebaseErr.Command, " ")
		details["stdout_stderr_excerpt"] = truncateForDiagnostics(rebaseErr.Output, 2000)
	}
	return details
}

func truncateForDiagnostics(s string, limit int) string {
	s = strings.TrimSpace(s)
	if len(s) <= limit {
		return s
	}
	if limit <= 3 {
		return s[:limit]
	}
	return s[:limit-3] + "..."
}

// markSubmitRebaseConflict transitions a task from IMPLEMENTING (or pipeline executing
// state) to INTEGRATION_FAILED when a rebase conflict is detected during submission.
// Releases the agent so the orchestrator can re-assign a coder for conflict resolution.
//
// Sibling: markIntegrationFailed in wt_merge.go handles the post-review merge path.
// Both share the pattern: transition → append FailedBy → write history entry.
// They differ in pre-conditions (approved vs implementing) and post-actions (agent release).
func markSubmitRebaseConflict(bb *db.Blackboard, taskID, agentID string, pipelineTransitions map[models.TaskStatus][]models.TaskStatus) error {
	reason := IntegrationReasonMergeConflict
	return bb.Modify(func(s *models.State) error {
		t := s.FindTask(taskID)
		if t == nil {
			return &errors.NotFoundError{Entity: "task", ID: taskID}
		}
		if err := t.TransitionWith(models.TaskStatusIntegrationFailed, pipelineTransitions); err != nil {
			return err
		}
		t.FailedBy = appendUniqueAgentID(t.FailedBy, agentID)
		t.IntegrationFix = false
		t.AssignedTo = nil
		t.LeaseExpires = nil

		now := time.Now().UTC()
		diagnostic := map[string]any{
			"operation":     integrationOperationSubmitForReview,
			"reason":        reason,
			"recovery_hint": "resolve the submit-time rebase conflict in the task worktree, then resubmit for review",
		}
		entry := models.TaskHistoryEntry{
			Time:   now,
			Event:  models.TaskEventIntegrationFailed,
			Agent:  &agentID,
			Reason: &reason,
			Extra: map[string]any{
				"diagnostic": diagnostic,
			},
		}
		t.IntegrationFailure = cloneMapForTaskDiagnostic(diagnostic)
		t.History = append(t.History, entry)
		if _, err := blockTaskForHypothesisExhaustion(s, t, agentID, pipelineTransitions, now); err != nil {
			return err
		}
		t.HandoffEvents = append(t.HandoffEvents, models.HandoffEvent{
			Timestamp: now,
			Agent:     agentID,
			Trigger:   models.HandoffTriggerSubmission,
			Failed:    []string{reason},
			NextStep:  "resolve integration conflict and resubmit",
		})

		// Release the agent so it can pick up other work
		if agent, ok := s.Agents[agentID]; ok {
			agent.Status = models.AgentStatusWaiting
			agent.CurrentTask = nil
			agent.LeaseExpires = nil
			s.Agents[agentID] = agent
		}

		return nil
	})
}
