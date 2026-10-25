package ops

import (
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// RecoverTaskResult contains the outcome of recovering a task.
type RecoverTaskResult struct {
	models.LifecycleOutcome
	TaskID            string
	InState           bool   // true if task was found in state
	AgentID           string // primary agent that held the claim, if any
	AgentRole         string
	ClaimReleased     bool
	WorktreeRemoved   bool
	BranchRemoved     bool
	WorktreeRecovered bool
	WorktreeCreated   bool
	FreshReset        bool
	PreservedWorktree bool
	AgentRecovered    bool
	Warnings          []string
}

// RecoverTaskOptions configures task recovery.
type RecoverTaskOptions struct {
	RequestOptions LifecycleRequestOptions
	// Force bypasses live-PID checks. For tasks absent from state, it also enables
	// git-only artifact cleanup.
	Force bool
	// Fresh explicitly discards the task branch/worktree and resets an in-state
	// non-terminal task to its role-pair initial status.
	Fresh bool
}

type recoverTaskStatusSet struct {
	Initial           models.TaskStatus
	Executing         models.TaskStatus
	Submitted         models.TaskStatus
	Reviewing         models.TaskStatus
	Rejected          models.TaskStatus
	Approved          models.TaskStatus
	PartiallyApproved models.TaskStatus
	Reviewing2        models.TaskStatus
}

// recoverTaskPlan is the pipeline and claim evidence captured before recovery
// effects begin.
type recoverTaskPlan struct {
	resolver    models.PipelineResolver
	transitions map[models.TaskStatus][]models.TaskStatus
	statuses    recoverTaskStatusSet
	snapshot    recoverTaskSnapshot
}

type recoverTaskTestHooks struct {
	beforePreserveModify func()
	beforeFreshModify    func()
	cleanupGitArtifacts  func() error
	beforeFreshCreate    func()
	// beforeFreshComplete runs inside the fresh completion callback, after the Git
	// effects and the state mutation, before the receipt completes.
	beforeFreshComplete func(*models.Task)
}

var testRecoverTaskHooks *recoverTaskTestHooks

// RecoverTask performs task recovery. In-state tasks preserve recoverable work by
// default. Destructive branch/worktree reset requires RecoverTaskOptions.Fresh.
//
// Without force: requires the task to exist in state, and refuses if any claiming
// agent's PID is still alive.
//
// With force: cleans up git artifacts (worktree + branch) even if the task is not
// in state. For in-state tasks, force only bypasses PID liveness checks.
//
// Idempotent: safe to run multiple times. No terminal I/O.
func RecoverTask(projectRoot, taskID string, force bool, reason string) (*RecoverTaskResult, error) {
	return RecoverTaskWithOptions(projectRoot, taskID, reason, RecoverTaskOptions{Force: force})
}

// RecoverTaskWithOptions performs task recovery with explicit reset options.
func RecoverTaskWithOptions(projectRoot, taskID string, reason string, opts RecoverTaskOptions) (result *RecoverTaskResult, err error) {
	invocation := &ownershipInvocation{operation: "recover-task", opts: opts.RequestOptions, metrics: NewLifecycleInvocation(projectRoot)}
	defer func() { err = invocation.finish(projectRoot, err) }()
	if taskID == "" {
		return nil, &PreconditionError{Reason: "task ID required"}
	}
	if err := paths.ValidateTaskID(taskID); err != nil {
		return nil, &PreconditionError{Reason: fmt.Sprintf("invalid task ID: %v", err)}
	}
	if err := ValidateLifecycleRequestOptions(opts.RequestOptions); err != nil {
		return nil, &PreconditionError{Reason: err.Error()}
	}
	err = withOwnershipTaskLock(projectRoot, taskID, "task-recover-worktree", func() error {
		var recoverErr error
		result, recoverErr = recoverTaskWithOptions(projectRoot, taskID, reason, opts, invocation)
		return recoverErr
	})
	return result, err
}

func recoverTaskWithOptions(projectRoot, taskID string, reason string, opts RecoverTaskOptions, invocation *ownershipInvocation) (*RecoverTaskResult, error) {
	if taskID == "" {
		return nil, fmt.Errorf("task ID required")
	}
	if err := paths.ValidateTaskID(taskID); err != nil {
		return nil, fmt.Errorf("invalid task ID: %w", err)
	}
	if reason == "" {
		reason = "task recovery"
	}

	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())
	gitWrapper := git.New(projectRoot)
	result := &RecoverTaskResult{TaskID: taskID}

	state, err := bb.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read state: %w", err)
	}

	var task *models.Task
	if state != nil {
		task = state.FindTask(taskID)
	}
	if task != nil {
		replayed, err := recoverTaskResolveRequest(task, state, reason, opts, invocation, result)
		if err != nil {
			return nil, err
		}
		if replayed {
			return result, nil
		}
	} else if !opts.Force {
		return nil, fmt.Errorf("task %s not found in state, use --force to clean up git artifacts anyway", taskID)
	}

	artifact := inspectRecoverTaskArtifacts(gitWrapper, taskID, result)
	if !result.InState {
		return recoverTaskAbsentCleanup(gitWrapper, taskID, artifact, opts, invocation, result)
	}
	if task.Status.IsTerminal() {
		return nil, fmt.Errorf("task %s is terminal (%s); recover-task only operates on non-terminal in-state tasks", taskID, task.Status)
	}

	resolver, _, resolverErr := loadResolver(projectRoot)
	if resolverErr != nil {
		return nil, fmt.Errorf("pipeline config: %w", resolverErr)
	}
	pipelineTransitions := BuildPipelineTransitions(resolver)
	statuses := recoverTaskStatuses(task, resolver)
	if statuses.Initial == "" {
		return nil, fmt.Errorf("task %s role_pair %q has no initial status", taskID, task.RolePair)
	}
	plan := recoverTaskPlan{resolver: resolver, transitions: pipelineTransitions, statuses: statuses, snapshot: newRecoverTaskSnapshot(task)}
	preparation, err := recoverTaskPrepare(bb, taskID, plan.snapshot, opts, invocation, result)
	if err != nil {
		return nil, err
	}
	invocation.effects = true

	if opts.Fresh {
		return recoverTaskFreshReset(bb, gitWrapper, taskID, reason, statuses, artifact, plan.snapshot, result, invocation)
	}
	preserve, err := recoverTaskPreserveOrRetire(bb, gitWrapper, task, plan.statuses, &artifact, &preparation, invocation, result)
	if err != nil {
		return nil, err
	}
	return recoverTaskCompletePreserve(bb, taskID, reason, plan, artifact, preserve, invocation, result)
}

// recoverTaskResolveRequest binds the recovery request to the observed task and
// resolves a retained receipt (replay: true, no effects) or a live claim refusal.
func recoverTaskResolveRequest(task *models.Task, state *models.State, reason string, opts RecoverTaskOptions, invocation *ownershipInvocation, result *RecoverTaskResult) (replayed bool, err error) {
	taskID := task.ID
	result.InState = true
	invocation.observe(task)
	request, err := NewLifecycleRequest("recover-task", task, "human", nil, opts.RequestOptions, struct {
		Force, Fresh bool
		Reason       string
	}{opts.Force, opts.Fresh, reason})
	if err != nil {
		return false, err
	}
	invocation.request = request
	receipt, err := checkOwnerEndingRequest(task, request)
	if err != nil {
		return false, err
	}
	if receipt != nil {
		invocation.outcome = LifecycleReplayOutcome(task, receipt, "human")
		result.LifecycleOutcome = invocation.outcome
		result.ClaimReleased = receipt.Projection.ReleasedDoer || receipt.Projection.ReleasedReviewer
		result.FreshReset = opts.Fresh
		result.PreservedWorktree = !opts.Fresh && receipt.Projection.BaseCommit != ""
		return true, nil
	}
	if err := recoverTaskCheckLiveClaims(taskID, task, state, opts.Force, opts.Fresh, result); err != nil {
		return false, err
	}
	return false, nil
}

// recoverTaskAbsentCleanup removes the Git artifacts of a task absent from state.
// It is Git-only: no state transaction runs.
func recoverTaskAbsentCleanup(gitWrapper *git.Git, taskID string, artifact recoverTaskArtifactSnapshot, opts RecoverTaskOptions, invocation *ownershipInvocation, result *RecoverTaskResult) (*RecoverTaskResult, error) {
	if opts.RequestOptions.RequestID != "" {
		return nil, &PreconditionError{Reason: "cannot bind a recovery request to an absent task"}
	}
	invocation.effects = true
	if err := recoverTaskCleanupGitArtifacts(gitWrapper, taskID, artifact, result); err != nil {
		return result, fmt.Errorf("cleanup git artifacts: %w", err)
	}
	invocation.outcome = NewLifecycleOutcome("recover-task", nil, models.LifecycleCompleted, "continue", "committed")
	invocation.outcome.TaskID = taskID
	result.LifecycleOutcome = invocation.outcome
	return result, nil
}

// recoverTaskPrepare revalidates the snapshot and live claims under the state
// lock and records the owner-ending preparation.
func recoverTaskPrepare(bb *db.Blackboard, taskID string, snapshot recoverTaskSnapshot, opts RecoverTaskOptions, invocation *ownershipInvocation, result *RecoverTaskResult) (models.LifecyclePreparation, error) {
	var preparation models.LifecyclePreparation
	if err := bb.Modify(func(state *models.State) error {
		task := state.FindTask(taskID)
		if task == nil {
			return fmt.Errorf("task disappeared before recovery preparation")
		}
		if err := snapshot.validate(taskID, task); err != nil {
			return err
		}
		if err := recoverTaskCheckLiveClaims(taskID, task, state, opts.Force, opts.Fresh, result); err != nil {
			return err
		}
		if err := prepareOwnerEndingRequest(task, invocation.request); err != nil {
			return err
		}
		preparation = *task.Lifecycle.Preparation
		return nil
	}); err != nil {
		return models.LifecyclePreparation{}, err
	}
	return preparation, nil
}

// recoverTaskPreserveOrRetire checks the preserved artifacts. Without an attach,
// every preserve refusal is a read-only inspection, so the refused request
// retires its own reservation instead of leaving a marker no agent can retire
// (D60). An attach, even a failed one, may have changed Git state and keeps the
// fence.
func recoverTaskPreserveOrRetire(bb *db.Blackboard, gitWrapper *git.Git, task *models.Task, statuses recoverTaskStatusSet, artifact *recoverTaskArtifactSnapshot, preparation *models.LifecyclePreparation, invocation *ownershipInvocation, result *RecoverTaskResult) (bool, error) {
	attachAttempted := !artifact.Worktree && artifact.Branch
	preserve, err := recoverTaskPreserveArtifacts(gitWrapper, task, statuses, artifact, result)
	if err != nil {
		if attachAttempted {
			return false, err
		}
		invocation.effects = false
		return false, retireFailedLifecyclePreparation(bb.Patient(), task.ID, nil, preparation, err, "none")
	}
	if testRecoverTaskHooks != nil && testRecoverTaskHooks.beforePreserveModify != nil {
		testRecoverTaskHooks.beforePreserveModify()
	}
	return preserve, nil
}

// recoverTaskCompletePreserve releases the snapshot claims, records the
// preserved worktree and completes the receipt in one transaction.
func recoverTaskCompletePreserve(bb *db.Blackboard, taskID, reason string, plan recoverTaskPlan, artifact recoverTaskArtifactSnapshot, preserve bool, invocation *ownershipInvocation, result *RecoverTaskResult) (*RecoverTaskResult, error) {
	snapshot, statuses := plan.snapshot, plan.statuses
	now := time.Now().UTC()
	err := bb.Modify(func(state *models.State) error {
		task := state.FindTask(taskID)
		if task == nil {
			return fmt.Errorf("task disappeared before recovery completion")
		}
		invocation.observe(task)
		if err := snapshot.validate(taskID, task); err != nil {
			return err
		}
		if err := ValidateLifecyclePreparation(task, invocation.request); err != nil {
			return err
		}

		agentsToRecover := snapshot.agentsToRecover()
		claimReleaseFailed := recoverTaskReleaseClaims(state, task, plan.resolver, plan.transitions, statuses, reason, now, agentsToRecover, result)

		if preserve {
			task.Worktree = &artifact.WorktreeRel
			if artifact.BaseCommit != "" {
				task.BaseCommit = &artifact.BaseCommit
			}
			result.PreservedWorktree = true
		} else if !claimReleaseFailed {
			task.Worktree = nil
			task.BaseCommit = nil
		}

		recoverTaskResetMissingReviewCommit(task, statuses, result)

		for agentID := range agentsToRecover {
			if _, exists := state.Agents[agentID]; exists {
				delete(state.Agents, agentID)
				result.AgentRecovered = true
			}
		}
		state.HumanNotes = append(state.HumanNotes, recoverTaskHumanNote(taskID, reason, agentsToRecover, now))
		var completeErr error
		invocation.outcome, completeErr = CompleteLifecycleRequest(task, invocation.request, recoverTaskProjection(task, snapshot), state.Agents)
		result.LifecycleOutcome = invocation.outcome
		return completeErr
	})
	if err != nil {
		return nil, fmt.Errorf("failed to recover task: %w", err)
	}

	return result, nil
}

func recoverTaskStatuses(task *models.Task, resolver models.PipelineResolver) recoverTaskStatusSet {
	statuses := recoverTaskStatusSet{}
	statuses.Initial, _ = resolver.InitialStatus(task.RolePair)
	statuses.Executing, _ = resolver.ExecutingStatus(task.RolePair)
	statuses.Submitted, _ = resolver.SubmittedStatus(task.RolePair)
	statuses.Reviewing, _ = resolver.ReviewingStatus(task.RolePair)
	statuses.Rejected, _ = resolver.RejectedStatus(task.RolePair)
	statuses.Approved, _ = resolver.ApprovedStatus(task.RolePair)
	statuses.PartiallyApproved, _ = resolver.PartiallyApprovedStatus(task.RolePair)
	statuses.Reviewing2, _ = resolver.Reviewing2Status(task.RolePair)
	return statuses
}
