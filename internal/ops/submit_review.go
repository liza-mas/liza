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
	"github.com/liza-mas/liza/internal/functionalclusters"
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
	models.LifecycleOutcome
	TaskID       string   `json:"task_id"`
	ReviewCommit string   `json:"review_commit"`
	AgentID      string   `json:"agent_id"`
	Warnings     []string `json:"warnings,omitempty"`
}

var (
	submitReviewRefreshIndexes                 = scipsearch.RefreshIndexes
	submitReviewRefreshStacklitIndex           = stacklit.RefreshIndex
	submitReviewRefreshFunctionalClustersIndex = functionalclusters.RefreshIndex
	submitReviewBeforeModifyTestHook           func()
)

// SubmitForReview validates that commitRef resolves to the worktree HEAD before rebase,
// rebases the task branch onto the integration branch to catch conflicts early,
// then atomically transitions the task to READY_FOR_REVIEW.
// No terminal I/O.
func SubmitForReview(projectRoot, taskID, commitRef, agentID string) (*SubmitForReviewResult, error) {
	return submitForReviewLifecycle(projectRoot, taskID, commitRef, agentID, nil, LifecycleRequestOptions{})
}

// SubmitForReviewWithAuthority is the authenticated command entry point. The
// caller-held generation is revalidated inside every state transaction.
func SubmitForReviewWithAuthority(projectRoot, taskID, commitRef string, authority models.AgentAuthority) (*SubmitForReviewResult, error) {
	return submitForReviewLifecycle(projectRoot, taskID, commitRef, authority.ID, &authority, LifecycleRequestOptions{})
}

func prepareSubmitForReview(projectRoot, taskID, commitRef, agentID string, authority *models.AgentAuthority, opts LifecycleRequestOptions, invocation *submissionInvocation) (*preparedSubmission, error) {
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
	if authority != nil {
		if err := RequireAgentAuthority(state, *authority); err != nil {
			return nil, err
		}
	}
	invocation.task = task
	request, receipt, err := submissionRequest(task, commitRef, agentID, authority, opts)
	if err != nil {
		return nil, err
	}
	if receipt != nil {
		return replaySubmission(task, receipt, agentID), nil
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
		return nil, &LifecycleError{
			Outcome: NewLifecycleOutcome(integrationOperationSubmitForReview, task, models.LifecycleAlreadyTransitioned, "stop", "none"),
			Err:     &PreconditionError{Reason: fmt.Sprintf("task %s is not %s (current status: %s)", taskID, expectedCurrentStatus, task.Status)},
		}
	}

	if task.AssignedTo == nil || *task.AssignedTo != agentID {
		currentAgent := "none"
		if task.AssignedTo != nil {
			currentAgent = *task.AssignedTo
		}
		return nil, &LifecycleError{
			Outcome: NewLifecycleOutcome(integrationOperationSubmitForReview, task, models.LifecycleStaleCaller, "stop", "none"),
			Err:     &PreconditionError{Reason: fmt.Sprintf("task %s is not assigned to agent %s (currently assigned to: %s)", taskID, agentID, currentAgent)},
		}
	}

	if task.Worktree == nil {
		return nil, &PreconditionError{Reason: fmt.Sprintf("task %s has no worktree", taskID)}
	}

	// Pre-execution checkpoint required before submission
	if !HasCheckpoint(task.History, agentID) {
		return nil, &PreconditionError{Reason: fmt.Sprintf("task %s: pre-execution checkpoint required before submission (use %q)", taskID, brand.Command("write-checkpoint", taskID))}
	}
	if err := validateOutputArtifactRefScalars(taskID, task.Output); err != nil {
		return nil, err
	}

	// Git work holds the task lock, never the blackboard lock.
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
	if opts.RequestID == "" {
		// Persist the immutable input even when the first legacy call used HEAD.
		request, receipt, err = submissionRequest(task, resolvedCommit, agentID, authority, opts)
		if err != nil {
			return nil, err
		}
		if receipt != nil {
			return replaySubmission(task, receipt, agentID), nil
		}
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

	acceptance, err := loadAcceptanceInput(projectRoot, state, task, rebaseBase)
	if err != nil {
		return nil, err
	}
	if _, err := prepareAcceptanceReceipt(projectRoot, task, acceptance, preRebaseCommit); err != nil {
		return nil, err
	}
	if acceptance != nil {
		if err := checkAcceptanceWorktree(projectRoot, task.ID, preRebaseCommit); err != nil {
			return nil, err
		}
	}

	var preparation models.LifecyclePreparation
	if err := modifyLifecycleState(bb, authority, func(current *models.State) error {
		live := current.FindTask(taskID)
		if live == nil {
			return &errors.NotFoundError{Entity: "task", ID: taskID}
		}
		if live.Status != expectedCurrentStatus || live.AssignedTo == nil || *live.AssignedTo != agentID {
			return &LifecycleError{Outcome: NewLifecycleOutcome(request.Operation, live, models.LifecycleStateChanged, "requery", "none"), Err: fmt.Errorf("submission ownership changed before preparation")}
		}
		if err := validateOutputArtifactRefScalars(taskID, live.Output); err != nil {
			return err
		}
		if err := PrepareLifecycleRequest(live, request); err != nil {
			return err
		}
		preparation = *live.Lifecycle.Preparation
		return nil
	}); err != nil {
		return nil, err
	}
	invocation.preparation = &preparation
	invocation.effects = true
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
		markErr := markSubmitRebaseConflict(bb, taskID, agentID, authority, pipelineTransitions, request)
		if markErr != nil {
			return nil, &OperationalError{
				Code:    "state_write",
				Phase:   "mark-integration-failed",
				Message: fmt.Sprintf("rebase conflict on %s: transition to INTEGRATION_FAILED also failed — worktree is intact (rebase aborted), check task state with %q before retrying", taskID, brand.Command("get", "tasks/"+taskID)),
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

	return &preparedSubmission{
		refresh: func() []string {
			warnings := refreshSubmitReviewScipIndexes(wtPath, state.Config.ScipSearch)
			warnings = append(warnings, refreshSubmitReviewStacklitIndex(wtPath)...)
			return append(warnings, refreshSubmitReviewFunctionalClustersIndex(wtPath, state.Config.ScipSearch)...)
		},
		complete: func() (*SubmitForReviewResult, error) {
			// Indexing ran without task locking. Recheck Git before publishing its candidate.
			liveBranch, err := g.GetWorktreeBranch(wtPath)
			if err != nil {
				return nil, err
			}
			liveHEAD, err := g.GetWorktreeHEAD(taskID)
			if err != nil {
				return nil, err
			}
			if liveBranch != expectedBranch || liveHEAD != postRebaseCommit {
				return nil, &LifecycleError{Outcome: NewLifecycleOutcome(request.Operation, invocation.task, models.LifecycleStateChanged, "requery", "unknown"), Err: fmt.Errorf("worktree candidate changed while refreshing indexes")}
			}

			current, err := bb.Read()
			if err != nil {
				return nil, err
			}
			if authority != nil {
				if err := RequireAgentAuthority(current, *authority); err != nil {
					return nil, err
				}
			}
			if err := ValidateLifecyclePreparation(current.FindTask(taskID), request); err != nil {
				return nil, err
			}
			acceptanceReceipt, err := executeAcceptanceReceipt(projectRoot, task, acceptance, postRebaseCommit)
			if err != nil {
				return nil, err
			}

			// Phase 3: Atomic update with new commit SHA
			var outcome models.LifecycleOutcome
			now := time.Now().UTC()
			if submitReviewBeforeModifyTestHook != nil {
				submitReviewBeforeModifyTestHook()
			}

			err = modifyLifecycleState(bb, authority, func(state *models.State) error {
				task := state.FindTask(taskID)
				if task == nil {
					return &errors.NotFoundError{Entity: "task", ID: taskID}
				}
				invocation.task = task
				if err := ValidateLifecyclePreparation(task, request); err != nil {
					return err
				}
				if task.Worktree == nil || *task.Worktree != *reviewBoundaryTask.Worktree {
					return &LifecycleError{Outcome: NewLifecycleOutcome(request.Operation, task, models.LifecycleStateChanged, "requery", "unknown"), Err: fmt.Errorf("task worktree changed before submission")}
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

				if err := recheckAcceptanceReceipt(projectRoot, state, task, acceptance, acceptanceReceipt); err != nil {
					return err
				}
				if acceptanceReceipt != nil {
					task.AcceptanceSource = &acceptanceReceipt.Source
				}
				task.AcceptanceReceipt = acceptanceReceipt
				if err := task.TransitionWith(targetSubmittedStatus, pipelineTransitions); err != nil {
					return err
				}
				task.ReviewCommit = &postRebaseCommit
				// Update BaseCommit from "branched from" to "rebased onto" — this is the
				// integration HEAD the rebase targeted, ensuring the reviewer diffs only
				// the coder's changes (not integration commits that landed since claim).
				task.BaseCommit = &rebaseBase

				task.History = append(task.History, models.TaskHistoryEntry{
					Time:                  now,
					Event:                 models.TaskEventSubmittedForReview,
					Agent:                 &agentID,
					Commit:                &postRebaseCommit,
					SubmissionInputCommit: preRebaseCommit,
					SubmissionAttempt:     task.EffectiveAttempt(),
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

				var completeErr error
				outcome, completeErr = CompleteLifecycleRequest(task, request, models.LifecycleProjection{
					InputCommit: preRebaseCommit, ReviewCommit: postRebaseCommit, BaseCommit: rebaseBase,
					Attempt: task.EffectiveAttempt(), Iteration: task.Iteration, SourceStatus: expectedCurrentStatus,
				})
				return completeErr
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
				LifecycleOutcome: outcome,
				TaskID:           taskID,
				ReviewCommit:     postRebaseCommit,
				AgentID:          agentID,
			}, nil
		},
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

func refreshSubmitReviewFunctionalClustersIndex(worktreePath string, configuredLanguages []string) []string {
	result, err := submitReviewRefreshFunctionalClustersIndex(functionalclusters.RefreshOptions{
		TargetRoot:          worktreePath,
		TargetKind:          functionalclusters.TargetKindTaskWorktree,
		ConfiguredLanguages: configuredLanguages,
	})
	warnings := functionalClustersRefreshWarnings(result)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("functional-clusters: %v", err))
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
func markSubmitRebaseConflict(bb *db.Blackboard, taskID, agentID string, authority *models.AgentAuthority, pipelineTransitions map[models.TaskStatus][]models.TaskStatus, request LifecycleRequest) error {
	reason := IntegrationReasonMergeConflict
	return modifyLifecycleState(bb, authority, func(s *models.State) error {
		t := s.FindTask(taskID)
		if t == nil {
			return &errors.NotFoundError{Entity: "task", ID: taskID}
		}
		if err := ValidateLifecyclePreparation(t, request); err != nil {
			return err
		}
		if err := t.TransitionWith(models.TaskStatusIntegrationFailed, pipelineTransitions); err != nil {
			return err
		}
		t.FailedBy = appendUniqueAgentID(t.FailedBy, agentID)
		t.IntegrationFix = false
		t.AssignedTo = nil
		t.LeaseExpires = nil
		models.AdvanceLifecycle(t)

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
