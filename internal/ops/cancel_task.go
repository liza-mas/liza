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

func cancelTaskWithOptionalAuthority(projectRoot, taskID, reason, agentID string, authority *models.AgentAuthority, opts LifecycleRequestOptions) (result *CancelResult, retErr error) {
	invocation := NewLifecycleInvocation(projectRoot)
	var observed *models.Task
	defer func() {
		retErr = WrapLifecycleError("cancel-task", observed, retErr, models.LifecycleInvalidInput, "correct_input", "none")
		var outcome models.LifecycleOutcome
		var warnings *[]string
		if result != nil {
			outcome, warnings = result.LifecycleOutcome, &result.Warnings
		}
		invocation.FinishResult("cancel-task", outcome, &retErr, warnings)
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
	retErr = withOwnershipTaskLock(projectRoot, taskID, "cancel-task", func() error {
		var err error
		result, err = cancelTaskLifecycle(projectRoot, taskID, reason, agentID, authority, opts, &observed)
		return err
	})
	return result, retErr
}

func cancelTaskLifecycle(projectRoot, taskID, reason, agentID string, authority *models.AgentAuthority, opts LifecycleRequestOptions, observed **models.Task) (*CancelResult, error) {
	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())

	pb, err := loadPipelineBundle(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load pipeline config: %w", err)
	}

	// Read current state (no lock held) to capture original status and worktree info.
	state, task, err := readTaskState(bb, taskID)
	if err != nil {
		return nil, err
	}
	if authority != nil {
		if err := RequireAgentAuthority(state, *authority); err != nil {
			return nil, err
		}
	}
	*observed = task
	request, err := NewLifecycleRequest("cancel-task", task, agentID, authority, opts, reason)
	if err != nil {
		return nil, err
	}

	originalStatus := task.Status
	var outcome models.LifecycleOutcome

	// Atomic State Update
	err = lifecycleMutation(bb, authority)(func(state *models.State) error {
		currentTask := state.FindTask(taskID)
		if currentTask == nil {
			return &errors.NotFoundError{Entity: "task", ID: taskID}
		}
		*observed = currentTask
		receipt, err := checkOwnerEndingRequest(currentTask, request)
		if err != nil {
			return err
		}
		if receipt != nil {
			outcome = LifecycleReplayOutcome(currentTask, receipt, agentID)
			originalStatus = receipt.Projection.SourceStatus
			return errLifecycleReplay
		}

		if currentTask.Status != originalStatus {
			return WrapLifecycleError("cancel-task", currentTask, fmt.Errorf("task status changed before cancellation"), models.LifecycleStateChanged, "requery", "none")
		}
		if currentTask.Status.IsTerminal() {
			return WrapLifecycleError("cancel-task", currentTask, fmt.Errorf("task is already terminal"), models.LifecycleAlreadyTransitioned, "stop", "none")
		}
		// Cancellation ends ownership and retires pending work before recording completion.
		models.AdvanceLifecycle(currentTask)

		if err := currentTask.TransitionWith(models.TaskStatusAbandoned, pb.transitions); err != nil {
			return err
		}

		releaseAgentsForTask(state, taskID)
		currentTask.AssignedTo = nil
		currentTask.LeaseExpires = nil
		currentTask.ReviewingBy = nil
		currentTask.ReviewLeaseExpires = nil
		currentTask.Worktree = nil
		clearAttemptState(currentTask, attemptStateRetire)

		now := time.Now().UTC()
		currentTask.History = append(currentTask.History, models.TaskHistoryEntry{
			Time:   now,
			Event:  models.TaskEventAbandoned,
			Agent:  &agentID,
			Reason: &reason,
		})

		if err := rewriteActiveDependents(state, pb.resolver, taskID, nil, agentID, now); err != nil {
			return err
		}

		outcome, err = CompleteLifecycleRequest(currentTask, request, models.LifecycleProjection{SourceStatus: originalStatus}, state.Agents)
		return err
	})
	if isLifecycleReplay(err) {
		return &CancelResult{LifecycleOutcome: outcome, TaskID: taskID, OriginalStatus: originalStatus}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("failed to cancel task: %w", err)
	}

	// Best-effort cleanup (after state commit). Own branch is always deleted —
	// cancelled tasks have no successors that need it. Unconditional: branch
	// can outlive worktree directory after recovery or manual cleanup.
	var warnings []string
	gw := git.New(projectRoot)
	if rmErr := gw.RemoveWorktreeDir(taskID); rmErr != nil {
		warnings = append(warnings, fmt.Sprintf("failed to remove worktree directory: %v", rmErr))
	}
	taskBranch := paths.TaskBranchPrefix + taskID
	if exists, brErr := gw.BranchExists(taskBranch); brErr != nil {
		warnings = append(warnings, fmt.Sprintf("failed to check branch %s: %v", taskBranch, brErr))
	} else if exists {
		if delErr := gw.DeleteBranch(taskBranch); delErr != nil {
			warnings = append(warnings, fmt.Sprintf("failed to delete branch %s: %v", taskBranch, delErr))
		}
	}

	// This cancelled task may be a successor — check if its terminal
	// transition releases an older predecessor's branch.
	warnings = append(warnings, cleanupPredecessorBranches(bb, gw, taskID)...)

	return &CancelResult{
		LifecycleOutcome: outcome,
		TaskID:           taskID,
		OriginalStatus:   originalStatus,
		Warnings:         warnings,
	}, nil
}
