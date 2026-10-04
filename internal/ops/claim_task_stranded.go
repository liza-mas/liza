package ops

import (
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

// takeOverStrandedDoerClaim releases an executing claim whose holder died
// without its exit release (models.StrandedDoerClaimReason) into the preserved
// continuation — initial status, worktree and base commit kept — and returns
// the re-read state for the caller to claim as an ordinary preserved task.
//
// It is an owner-ending release ahead of the claim's own request: the dead
// holder's pending preparation cannot block it and is retired with the
// boundary advance. Nothing changes unless every precondition still holds
// under the task's claim lock and the claimant's generation fence. A crash
// after the release leaves an ordinary preserved continuation, so the release
// needs no receipt of its own.
func takeOverStrandedDoerClaim(bb *db.Blackboard, statePath string, state *models.State, task *models.Task, agentID, runtimeRole string, resolver *pipeline.Resolver, authority *models.AgentAuthority, invocation *ownershipInvocation) (*models.State, *models.Task, error) {
	request := invocation.request
	receipt, err := checkOwnerEndingRequest(task, request)
	if err != nil || receipt != nil {
		return state, task, err // A receipt replays through the caller's ordinary check.
	}
	// A pinned request names the executing boundary the takeover replaces, and
	// its identity must survive retries unchanged, so it cannot follow the
	// takeover onto the new boundary.
	if invocation.opts.ExpectedTransition != "" {
		return nil, nil, lifecycleRequestError(task, request, models.LifecycleStateChanged, "requery", "none",
			"stranded claim takeover changes the task boundary; claim without an expected transition")
	}
	if err := requireFreeClaimant(state, agentID, runtimeRole, task.ID); err != nil {
		return nil, nil, err
	}
	initial, err := resolver.InitialStatus(task.RolePair)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid role-pair %q: %w", task.RolePair, err)
	}
	transitions := BuildPipelineTransitions(resolver)

	lock := filelock.New(claimTaskWorktreeLockPath(statePath, task.ID))
	err = lock.WithLockOperation("claim-task-takeover", func() error {
		return lifecycleMutation(bb, authority)(func(state *models.State) error {
			if err := RequireWorkAdmitted(state, "claim-task"); err != nil {
				return err
			}
			live := state.FindTask(task.ID)
			if live == nil {
				return &errors.NotFoundError{Entity: "task", ID: task.ID}
			}
			invocation.observe(live)
			now := time.Now().UTC()
			reason := models.StrandedDoerClaimReason(state, live, resolver, now)
			if reason == "" {
				return lifecycleRequestError(live, request, models.LifecycleStateChanged, "requery", "none",
					"stranded claim changed before it could be taken over")
			}
			if receipt, err := checkOwnerEndingRequest(live, request); err != nil || receipt != nil {
				return lifecycleRequestError(live, request, models.LifecycleStateChanged, "requery", "none",
					"stranded claim changed before it could be taken over")
			}
			if err := requireFreeClaimant(state, agentID, runtimeRole, live.ID); err != nil {
				return err
			}
			holder := *live.AssignedTo
			if err := live.TransitionWith(initial, transitions); err != nil {
				return err
			}
			live.AssignedTo = nil
			live.LeaseExpires = nil
			live.Continuation = live.Iteration > 0
			models.AdvanceLifecycle(live) // Retires the dead holder's preparation.
			// Not ReleaseAgent: it drops the registration lease, and a lease-less
			// registration falls back to heartbeat liveness, which could present
			// the dead holder as live again.
			if agent, ok := state.Agents[holder]; ok && agent.CurrentTask != nil && *agent.CurrentTask == live.ID {
				agent.Status = models.AgentStatusIdle
				agent.CurrentTask = nil
				state.Agents[holder] = agent
			}
			live.History = append(live.History, models.TaskHistoryEntry{
				Time:             now,
				Event:            models.TaskEventDoerClaimReleased,
				Agent:            &agentID,
				Reason:           &reason,
				PreviousAssignee: &holder,
			})
			return nil
		})
	})
	if err != nil {
		return nil, nil, err
	}
	invocation.effects = true
	return readTaskState(bb, task.ID)
}

// requireFreeClaimant refuses a claimant that is unregistered for its role or
// already working on another task.
func requireFreeClaimant(state *models.State, agentID, runtimeRole, taskID string) error {
	agent, err := requireRegisteredClaimAgent(state, agentID, runtimeRole)
	if err != nil {
		return err
	}
	if agent.CurrentTask != nil && *agent.CurrentTask != "" && *agent.CurrentTask != taskID {
		return &PreconditionError{Reason: fmt.Sprintf("agent %s is already working on task %s", agentID, *agent.CurrentTask)}
	}
	return nil
}
