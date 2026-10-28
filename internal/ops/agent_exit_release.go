package ops

import (
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/db"
	lizaerrors "github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/roles"
)

// ClaimReleasePipeline is the pipeline a claim release needs, loaded by the
// caller outside the state lock so that no disk I/O happens under it. Either
// field is nil when the pipeline config was unreadable; a release that needs
// the missing part then fails closed.
type ClaimReleasePipeline struct {
	Transitions map[models.TaskStatus][]models.TaskStatus
	Resolver    *pipeline.Resolver
}

// ReleasedTaskClaim names the task whose claim an exit released and that
// task's lifecycle token right after the release. A block decided after the
// release may act on exactly that unowned continuation and nothing else.
type ReleasedTaskClaim struct {
	TaskID       string
	TransitionID string
}

// UnregisterAgent releases any task claim held by the agent, then removes the
// agent from state. Both happen in a single atomic transaction so that an
// interrupt between them cannot leave a stuck task. The caller passes the
// handle that carries its lock-acquisition policy: a supervisor usually exits
// because an ordinary wait timed out, so it passes the patient handle, since
// failing here too would strand its claim until the lease expires.
func UnregisterAgent(bb *db.Blackboard, authority models.AgentAuthority, release ClaimReleasePipeline) error {
	now := time.Now().UTC()
	agentID := authority.ID

	return ModifyWithAgentAuthority(bb, authority, func(state *models.State) error {
		return unregisterInState(state, agentID, release.Transitions, release.Resolver, now)
	})
}

// unregisterInState releases every claim the agent holds, agent-side and
// task-side, then removes its row. It runs inside the caller's transaction.
func unregisterInState(state *models.State, agentID string, pipelineTransitions map[models.TaskStatus][]models.TaskStatus, resolver *pipeline.Resolver, now time.Time) error {
	agent, exists := state.Agents[agentID]
	if !exists {
		return nil
	}

	taskIDs := make(map[string]bool)
	if agent.CurrentTask != nil {
		taskIDs[*agent.CurrentTask] = true
	}
	doerTaskIDs, reviewerTaskIDs := TaskClaimsForAgent(state, agentID)
	for _, taskID := range doerTaskIDs {
		taskIDs[taskID] = true
	}
	for _, taskID := range reviewerTaskIDs {
		taskIDs[taskID] = true
	}
	for taskID := range taskIDs {
		if task := state.FindTask(taskID); task != nil {
			if err := releaseTaskClaim(state, task, agent.Role, agentID, pipelineTransitions, resolver, now); err != nil {
				return err
			}
		}
	}

	delete(state.Agents, agentID)
	return nil
}

// releaseTaskClaim transitions a task back to its unclaimed status and clears
// the claim fields. It fails closed when an active claim cannot be safely
// released, so callers preserve both task-side and agent-side ownership.
// pipelineTransitions and resolver are pre-loaded by the caller (outside the
// transaction) to avoid disk I/O under the state lock.
// Uses resolver.RoleType() for doer/reviewer classification.
func releaseTaskClaim(state *models.State, task *models.Task, role, agentID string, pipelineTransitions map[models.TaskStatus][]models.TaskStatus, resolver *pipeline.Resolver, now time.Time) error {
	reason := "agent interrupted"

	activeExecuting, releasedInitial := ResolveDoerReleaseStatus(task, resolver)

	resolveReviewerRelease := func() (models.TaskStatus, models.TaskStatus, error) {
		if resolver == nil {
			return "", "", fmt.Errorf("cannot release reviewer claim for task %s: pipeline resolver not loaded", task.ID)
		}
		return ResolveReviewerReleaseStatus(task, resolver)
	}

	transitionTask := func(to models.TaskStatus) error {
		if pipelineTransitions == nil {
			return fmt.Errorf("cannot transition task %s on claim release: pipeline transitions not loaded", task.ID)
		}
		if err := task.TransitionWith(to, pipelineTransitions); err != nil {
			return fmt.Errorf("transition task %s on claim release: %w", task.ID, err)
		}
		return nil
	}

	// Classify role using resolver for doer/reviewer determination.
	roleType := ""
	if resolver != nil {
		if rt, err := resolver.RoleType(role); err == nil {
			roleType = string(rt)
		}
	}

	switch roleType {
	case string(roles.RoleTypeDoer):
		if task.Status == activeExecuting {
			if err := transitionTask(releasedInitial); err != nil {
				return err
			}
			// An interrupted session continues its existing work cycle. Only
			// actual rework or an explicit new-iteration recovery consumes one.
			task.Continuation = task.Iteration > 0
		}
		task.AssignedTo = nil
		task.LeaseExpires = nil
		models.AdvanceLifecycle(task)

	case string(roles.RoleTypeReviewer):
		if task.ReviewingBy != nil && *task.ReviewingBy == agentID {
			activeReviewing, releasedSubmitted, err := resolveReviewerRelease()
			if err != nil {
				return err
			}
			if task.Status == activeReviewing {
				if err := transitionTask(releasedSubmitted); err != nil {
					return err
				}
			}
			task.ReviewingBy = nil
			task.ReviewLeaseExpires = nil
			models.AdvanceLifecycle(task)
		}

	default:
		if task.AssignedTo != nil && *task.AssignedTo == agentID {
			if task.Status == activeExecuting {
				if err := transitionTask(releasedInitial); err != nil {
					return err
				}
				task.Continuation = task.Iteration > 0
			}
			task.AssignedTo = nil
			task.LeaseExpires = nil
			models.AdvanceLifecycle(task)
		} else if task.ReviewingBy != nil && *task.ReviewingBy == agentID {
			activeReviewing, releasedSubmitted, err := resolveReviewerRelease()
			if err != nil {
				return err
			}
			if task.Status == activeReviewing {
				if err := transitionTask(releasedSubmitted); err != nil {
					return err
				}
			}
			task.ReviewingBy = nil
			task.ReviewLeaseExpires = nil
			models.AdvanceLifecycle(task)
		} else {
			return nil
		}
	}

	state.ReleaseAgent(agentID)

	task.History = append(task.History, models.TaskHistoryEntry{
		Time:   now,
		Event:  models.TaskEventClaimReleased,
		Agent:  &agentID,
		Reason: &reason,
	})
	return nil
}

// ResetAgentToIdle resets an agent's status to IDLE and clears CurrentTask.
func ResetAgentToIdle(bb *db.Blackboard, authority models.AgentAuthority) error {
	now := time.Now().UTC()
	agentID := authority.ID

	return ModifyWithAgentAuthority(bb, authority, func(state *models.State) error {
		agent, exists := state.Agents[agentID]
		if !exists {
			return &lizaerrors.NotFoundError{Entity: "agent", ID: agentID}
		}

		agent.Status = models.AgentStatusIdle
		agent.CurrentTask = nil
		agent.Heartbeat = now

		state.Agents[agentID] = agent
		return nil
	})
}

// ReleaseAgentAfterExit clears transient runtime states after a CLI exit while
// preserving explicit command-driven states that are meaningful between loops,
// and reports the claim it released, if any.
func ReleaseAgentAfterExit(bb *db.Blackboard, authority models.AgentAuthority, release ClaimReleasePipeline) (ReleasedTaskClaim, error) {
	now := time.Now().UTC()
	agentID := authority.ID

	var released ReleasedTaskClaim
	err := ModifyWithAgentAuthority(bb, authority, func(state *models.State) error {
		released = ReleasedTaskClaim{}
		releaseOne := func(task *models.Task, role string) error {
			before := len(task.History)
			if err := releaseTaskClaim(state, task, role, agentID, release.Transitions, release.Resolver, now); err != nil {
				return err
			}
			if len(task.History) > before {
				released = ReleasedTaskClaim{TaskID: task.ID, TransitionID: models.TaskTransitionID(task)}
			}
			return nil
		}

		agent, exists := state.Agents[agentID]
		if !exists {
			return &lizaerrors.NotFoundError{Entity: "agent", ID: agentID}
		}

		switch agent.Status {
		case models.AgentStatusWaiting:
			// A session never resumes after exit, so a WAITING agent holding a
			// task has nothing to come back to: fall through and release the
			// claim rather than pinning the task to a departed agent.
			if agent.CurrentTask != nil {
				task := state.FindTask(*agent.CurrentTask)
				if task != nil && task.ReviewingBy != nil && *task.ReviewingBy == agentID {
					return releaseOne(task, agent.Role)
				}
			}
		case models.AgentStatusHandoff:
			if agent.CurrentTask != nil {
				agent.Heartbeat = now
				state.Agents[agentID] = agent
				return nil
			}
			// CurrentTask already cleared — fall through to reset to IDLE
		}

		// Release any held task claim before clearing CurrentTask
		if agent.CurrentTask != nil {
			if task := state.FindTask(*agent.CurrentTask); task != nil {
				if err := releaseOne(task, agent.Role); err != nil {
					return err
				}
			}
		}

		agent.Status = models.AgentStatusIdle
		agent.CurrentTask = nil
		agent.Heartbeat = now
		state.Agents[agentID] = agent
		return nil
	})
	if err != nil {
		return ReleasedTaskClaim{}, err
	}
	return released, nil
}

// ReleaseClaimForHalt releases the claim of a provider that a halt refused to
// start. Only a claim this agent still holds on taskID is released. A doer's
// claim already started its iteration, so the task is marked as a continuation:
// the next claim resumes that iteration instead of charging a new one.
func ReleaseClaimForHalt(bb *db.Blackboard, authority models.AgentAuthority, release ClaimReleasePipeline, taskID string) error {
	now := time.Now().UTC()
	agentID := authority.ID

	return ModifyWithAgentAuthority(bb, authority, func(state *models.State) error {
		agent, exists := state.Agents[agentID]
		if !exists {
			return &lizaerrors.NotFoundError{Entity: "agent", ID: agentID}
		}
		if task := state.FindTask(taskID); task != nil && agent.CurrentTask != nil && *agent.CurrentTask == taskID {
			heldAsDoer := task.AssignedTo != nil && *task.AssignedTo == agentID
			heldAsReviewer := task.ReviewingBy != nil && *task.ReviewingBy == agentID
			if heldAsDoer || heldAsReviewer {
				if err := releaseTaskClaim(state, task, agent.Role, agentID, release.Transitions, release.Resolver, now); err != nil {
					return err
				}
				if heldAsDoer && task.AssignedTo == nil && task.Iteration > 0 {
					task.Continuation = true
				}
			}
		}
		agent = state.Agents[agentID]
		agent.Status = models.AgentStatusIdle
		agent.CurrentTask = nil
		agent.Heartbeat = now
		state.Agents[agentID] = agent
		return nil
	})
}

// EnterOrchestration sets an orchestrator agent's status to PLANNING and
// renews its lease.
func EnterOrchestration(bb *db.Blackboard, authority models.AgentAuthority) error {
	now := time.Now().UTC()
	agentID := authority.ID

	return ModifyWithAgentAuthority(bb, authority, func(state *models.State) error {
		agent, exists := state.Agents[agentID]
		if !exists {
			return &lizaerrors.NotFoundError{Entity: "agent", ID: agentID}
		}

		// Set to PLANNING state
		agent.Status = models.AgentStatusPlanning
		planning := "planning"
		agent.CurrentTask = &planning
		agent.Heartbeat = now

		// Renew lease
		leaseDuration := state.Config.LeaseDuration
		if leaseDuration <= 0 {
			leaseDuration = models.DefaultLeaseDurationSeconds
		}
		leaseExpires := now.Add(time.Duration(leaseDuration) * time.Second)
		agent.LeaseExpires = &leaseExpires

		state.Agents[agentID] = agent
		return nil
	})
}
