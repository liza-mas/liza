package ops

import (
	stderrors "errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	gitpkg "github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/identity"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// SubmitForReviewResult contains the outcome of submitting a task for review.
type SubmitForReviewResult struct {
	TaskID       string `json:"task_id"`
	ReviewCommit string `json:"review_commit"`
	AgentID      string `json:"agent_id"`
}

// SubmitForReview validates that commitSHA matches the worktree HEAD before rebase,
// rebases the task branch onto the integration branch to catch conflicts early,
// then atomically transitions the task to READY_FOR_REVIEW.
// No terminal I/O.
func SubmitForReview(projectRoot, taskID, commitSHA, agentID string) (*SubmitForReviewResult, error) {
	if taskID == "" {
		return nil, &PreconditionError{Reason: "task ID is required"}
	}
	if commitSHA == "" {
		return nil, &PreconditionError{Reason: "commit SHA is required"}
	}
	if agentID == "" {
		return nil, &PreconditionError{Reason: "LIZA_AGENT_ID is required"}
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
		return nil, &OperationalError{Message: "failed to load pipeline config", Err: resolverErr}
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

	// Phase 2: Execute git operations outside the lock
	g := gitpkg.New(projectRoot)
	wtPath := g.GetWorktreePath(taskID)

	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		return nil, &PreconditionError{Reason: fmt.Sprintf("worktree directory does not exist: %s", wtPath)}
	}

	wtBranch, err := g.GetWorktreeBranch(wtPath)
	if err != nil {
		return nil, &OperationalError{Message: "failed to determine worktree branch", Err: err}
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
		return nil, &OperationalError{Message: "failed to read worktree HEAD", Err: err}
	}
	if commitSHA != preRebaseCommit {
		return nil, &PreconditionError{Reason: fmt.Sprintf("provided commit SHA %s does not match worktree HEAD %s", commitSHA, preRebaseCommit)}
	}

	// TDD enforcement: code tasks must include test files (doer roles only).
	roleType, _ := resolver.RoleType(runtimeRole)
	if roleType == "doer" && task.EffectiveType() == models.TaskTypeCoding && task.BaseCommit != nil {
		hasTests, err := HasTestFiles(g, taskID, *task.BaseCommit)
		if err != nil {
			return nil, &OperationalError{Message: "failed to check for test files", Err: err}
		}
		if !hasTests && GetTDDWaiver(task.History, agentID) == "" {
			return nil, &PreconditionError{Reason: fmt.Sprintf("task %s: code tasks must include test files (e.g. *_test.go, *.test.ts, test_*.py) — TDD is mandatory", taskID)}
		}
	}

	integrationBranch := state.Config.IntegrationBranch
	if err := g.FetchFromLocal(wtPath, integrationBranch); err != nil {
		return nil, &OperationalError{Message: fmt.Sprintf("failed to fetch integration branch %s", integrationBranch), Err: err}
	}

	// Capture integration HEAD immediately after fetch — this is the exact ref
	// the rebase targets. Must be before rebase to avoid TOCTOU if integration
	// advances between fetch and rebase completion.
	rebaseBase, err := g.GetCommitSHA(integrationBranch)
	if err != nil {
		return nil, &OperationalError{Message: "failed to resolve integration branch HEAD", Err: err}
	}

	if err := g.RebaseOnto(wtPath, "FETCH_HEAD"); err != nil {
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
			return nil, &OperationalError{Message: "rebase failed (not a merge conflict)", Err: err}
		}

		// Transition to INTEGRATION_FAILED so the orchestrator re-queues the task.
		// This catches conflicts early (before review), avoiding a wasted review cycle.
		// See also: markIntegrationFailed in wt_merge.go (sibling for post-review merge path).
		markErr := markSubmitRebaseConflict(bb, taskID, agentID, pipelineTransitions)
		if markErr != nil {
			return nil, &OperationalError{
				Message: fmt.Sprintf("rebase conflict on %s: transition to INTEGRATION_FAILED also failed — worktree is intact (rebase aborted), check task state with liza_get tasks/%s before retrying", taskID, taskID),
				Err:     markErr,
			}
		}
		return nil, &IntegrationFailedError{Reason: IntegrationReasonMergeConflict}
	}

	postRebaseCommit, err := g.GetWorktreeHEAD(taskID)
	if err != nil {
		return nil, &OperationalError{Message: "failed to read worktree HEAD after rebase", Err: err}
	}

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

		if err := task.TransitionWith(targetSubmittedStatus, pipelineTransitions); err != nil {
			return err
		}
		task.ReviewCommit = &postRebaseCommit
		// Update BaseCommit from "branched from" to "rebased onto" — this is the
		// integration HEAD the rebase targeted, ensuring the reviewer diffs only
		// the coder's changes (not integration commits that landed since claim).
		task.BaseCommit = &rebaseBase

		task.History = append(task.History, models.TaskHistoryEntry{
			Time:  now,
			Event: models.TaskEventSubmittedForReview,
			Agent: &agentID,
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
		return nil, fmt.Errorf("failed to submit task for review: %w", err)
	}

	return &SubmitForReviewResult{
		TaskID:       taskID,
		ReviewCommit: postRebaseCommit,
		AgentID:      agentID,
	}, nil
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

		entry := models.TaskHistoryEntry{
			Time:   time.Now().UTC(),
			Event:  models.TaskEventIntegrationFailed,
			Agent:  &agentID,
			Reason: &reason,
		}
		t.History = append(t.History, entry)

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
