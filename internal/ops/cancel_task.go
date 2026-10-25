package ops

import (
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// CancelResult contains the outcome of cancelling a task.
type CancelResult struct {
	models.LifecycleOutcome
	TaskID         string            `json:"task_id"`
	OriginalStatus models.TaskStatus `json:"original_status"`
	Warnings       []string          `json:"warnings"`
}

// CancelTask transitions a task to ABANDONED with a reason, preserving full audit trail.
// Cancellable states are determined by the pipeline transition map (TransitionWith).
// No terminal I/O.
func CancelTask(projectRoot, taskID, reason, agentID string) (*CancelResult, error) {
	return CancelTaskWithOptions(projectRoot, taskID, reason, agentID, LifecycleRequestOptions{})
}

func CancelTaskWithOptions(projectRoot, taskID, reason, agentID string, opts LifecycleRequestOptions) (*CancelResult, error) {
	return cancelTaskWithOptionalAuthority(projectRoot, taskID, reason, agentID, nil, opts)
}

// CancelTaskWithAuthority fences cancellation with the orchestrator's
// registration generation.
func CancelTaskWithAuthority(projectRoot, taskID, reason string, authority models.AgentAuthority) (*CancelResult, error) {
	return CancelTaskWithAuthorityAndOptions(projectRoot, taskID, reason, authority, LifecycleRequestOptions{})
}

func CancelTaskWithAuthorityAndOptions(projectRoot, taskID, reason string, authority models.AgentAuthority, opts LifecycleRequestOptions) (*CancelResult, error) {
	return cancelTaskWithOptionalAuthority(projectRoot, taskID, reason, authority.ID, &authority, opts)
}

const cancelTaskOperation = "cancel-task"

// cancelPass carries one cancel-task request through its phases: admission
// before the lock, the locked retirement, and the cleanup that follows
// persistence.
type cancelPass struct {
	projectRoot, taskID, reason, agentID string
	opts                                 LifecycleRequestOptions
	authority                            *models.AgentAuthority

	// Set by admit, before the lock.
	bb             *db.Blackboard
	pb             *pipelineBundle
	request        LifecycleRequest
	originalStatus models.TaskStatus

	// Set where the task is observed, before and inside the lock.
	observed *models.Task
	outcome  models.LifecycleOutcome
}

func cancelTaskWithOptionalAuthority(projectRoot, taskID, reason, agentID string, authority *models.AgentAuthority, opts LifecycleRequestOptions) (result *CancelResult, retErr error) {
	invocation := NewLifecycleInvocation(projectRoot)
	pass := &cancelPass{projectRoot: projectRoot, taskID: taskID, reason: reason, agentID: agentID, opts: opts, authority: authority}
	defer func() {
		retErr = WrapLifecycleError(cancelTaskOperation, pass.observed, retErr, models.LifecycleInvalidInput, LifecycleSafeActionCorrectInput, LifecycleEffectsNone)
		var outcome models.LifecycleOutcome
		var warnings *[]string
		if result != nil {
			outcome, warnings = result.LifecycleOutcome, &result.Warnings
		}
		invocation.FinishResult(cancelTaskOperation, outcome, &retErr, warnings)
	}()
	if err := ValidateLifecycleRequestOptions(opts); err != nil {
		return nil, err
	}
	if taskID == "" {
		return nil, &PreconditionError{Reason: "task ID is required"}
	}
	if reason == "" {
		return nil, &PreconditionError{Reason: "cancellation reason is required"}
	}
	if agentID == "" {
		return nil, &PreconditionError{Reason: "orchestrator agent ID is required"}
	}
	retErr = withOwnershipTaskLock(projectRoot, taskID, cancelTaskOperation, func() error {
		var err error
		result, err = pass.cancel()
		return err
	})
	return result, retErr
}

// cancel runs the request under the ownership task lock.
func (p *cancelPass) cancel() (*CancelResult, error) {
	if err := p.admit(); err != nil {
		return nil, err
	}
	err := lifecycleMutation(p.bb, p.authority)(p.commit)
	if isLifecycleReplay(err) {
		return p.result(nil), nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to cancel task: %w", err)
	}
	return p.result(p.cleanup()), nil
}

// admit reads without the state lock to capture the original status and the
// request identity.
func (p *cancelPass) admit() error {
	p.bb = RequestBlackboard(paths.New(p.projectRoot).StatePath(), p.authority, p.opts)

	var err error
	if p.pb, err = loadPipelineBundle(p.projectRoot); err != nil {
		return fmt.Errorf("failed to load pipeline config: %w", err)
	}
	state, task, err := readTaskState(p.bb, p.taskID)
	if err != nil {
		return err
	}
	if p.authority != nil {
		if err := RequireAgentAuthority(state, *p.authority); err != nil {
			return err
		}
	}
	p.observed = task
	if p.request, err = NewLifecycleRequest(cancelTaskOperation, task, p.agentID, p.authority, p.opts, p.reason); err != nil {
		return err
	}
	p.originalStatus = task.Status
	return nil
}

// commit is the locked transaction: lookup, receipt replay, status recheck,
// terminal refusal, the retirement itself and the lifecycle completion.
func (p *cancelPass) commit(state *models.State) error {
	currentTask := state.FindTask(p.taskID)
	if currentTask == nil {
		return &errors.NotFoundError{Entity: notFoundEntityTask, ID: p.taskID}
	}
	p.observed = currentTask
	receipt, err := checkOwnerEndingRequest(currentTask, p.request)
	if err != nil {
		return err
	}
	if receipt != nil {
		p.outcome = LifecycleReplayOutcome(currentTask, receipt, p.agentID)
		p.originalStatus = receipt.Projection.SourceStatus
		return errLifecycleReplay
	}

	if currentTask.Status != p.originalStatus {
		return WrapLifecycleError(cancelTaskOperation, currentTask, fmt.Errorf("task status changed before cancellation"), models.LifecycleStateChanged, LifecycleSafeActionRequery, LifecycleEffectsNone)
	}
	if currentTask.Status.IsTerminal() {
		return WrapLifecycleError(cancelTaskOperation, currentTask, fmt.Errorf("task is already terminal"), models.LifecycleAlreadyTransitioned, LifecycleSafeActionStop, LifecycleEffectsNone)
	}
	if err := p.retire(state, currentTask); err != nil {
		return err
	}

	p.outcome, err = CompleteLifecycleRequest(currentTask, p.request, models.LifecycleProjection{SourceStatus: p.originalStatus}, state.Agents)
	return err
}

// retire ends ownership and retires pending work before the completion is
// recorded: the ABANDONED transition, agent release, attempt and runtime-input
// retirement, the audit entry and the removal of the task from its dependents.
func (p *cancelPass) retire(state *models.State, task *models.Task) error {
	models.AdvanceLifecycle(task)

	if err := task.TransitionWith(models.TaskStatusAbandoned, p.pb.transitions); err != nil {
		return err
	}

	releaseAgentsForTask(state, p.taskID)
	task.AssignedTo = nil
	task.LeaseExpires = nil
	task.ReviewingBy = nil
	task.ReviewLeaseExpires = nil
	task.Worktree = nil
	clearAttemptState(task, attemptStateRetire)

	now := time.Now().UTC()
	retireRuntimeInputBindings(state, task, nil, now)
	agentID, reason := p.agentID, p.reason
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:   now,
		Event:  models.TaskEventAbandoned,
		Agent:  &agentID,
		Reason: &reason,
	})

	return rewriteActiveDependents(state, p.pb.resolver, p.taskID, nil, p.agentID, now)
}

// result projects the recorded outcome; replays carry no warnings.
func (p *cancelPass) result(warnings []string) *CancelResult {
	return &CancelResult{
		LifecycleOutcome: p.outcome,
		TaskID:           p.taskID,
		OriginalStatus:   p.originalStatus,
		Warnings:         warnings,
	}
}

// cleanup is best-effort and runs after the state commit. The task's own
// branch is always deleted — cancelled tasks have no successors that need it.
// Unconditional: the branch can outlive the worktree directory after recovery
// or manual cleanup.
func (p *cancelPass) cleanup() []string {
	var warnings []string
	gw := git.New(p.projectRoot)
	if rmErr := gw.RemoveWorktreeDir(p.taskID); rmErr != nil {
		warnings = append(warnings, fmt.Sprintf("failed to remove worktree directory: %v", rmErr))
	}
	warnings = append(warnings, deleteOwnTaskBranch(gw, p.taskID)...)

	// This cancelled task may be a successor — check if its terminal
	// transition releases an older predecessor's branch.
	return append(warnings, cleanupPredecessorBranches(p.bb, gw, p.taskID)...)
}
