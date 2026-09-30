package statevalidate

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

// validateAgentInvariants checks that agents with active leases or WORKING
// status have the metadata needed to operate: role, provider, pid, current task
// and lease expiry. It warns (not errors) on WORKING agents whose lease has
// expired beyond the grace period, since long-running operations may still be
// in progress. now is the instant leases are judged against, shared with every
// other time-dependent check of the same validation pass.
func validateAgentInvariants(v *violations, state *models.State, warnWriter io.Writer, resolver *pipeline.Resolver, now time.Time) {
	graceDeadline := now.Add(-models.LeaseExpiryGracePeriod)

	for _, agentID := range sortedAgentIDs(state) {
		agent := state.Agents[agentID]
		if agent.LeaseExpires != nil && agent.LeaseExpires.After(now) {
			if agent.Role == "" {
				v.add(fmt.Errorf("agent %s has active lease but no role", agentID))
			}
			if agent.Provider == "" {
				v.add(fmt.Errorf("agent %s has active lease but no provider", agentID))
			}
			if agent.PID <= 0 {
				v.add(fmt.Errorf("agent %s has active lease but no pid", agentID))
			}
		}

		// WORKING agent must have current_task
		if agent.Status == models.AgentStatusWorking && (agent.CurrentTask == nil || *agent.CurrentTask == "") {
			v.add(fmt.Errorf("agent %s has status WORKING but no current_task assigned", agentID))
		}

		// WORKING agent must have valid lease_expires
		if agent.Status == models.AgentStatusWorking {
			if agent.LeaseExpires == nil {
				v.add(fmt.Errorf("agent %s has status WORKING but no lease_expires", agentID))
			} else if agent.LeaseExpires.Before(graceDeadline) {
				// In bash this is a warning, but we'll treat it as an error for stricter validation
				// Could make this configurable if needed
				fmt.Fprintf(warnWriter, "WARNING: Agent %s has status WORKING but lease expired (may be long-running operation)\n", agentID)
			}
		}
	}

	validateActiveReviewOwnership(v, state, resolver)
	validateActiveDoerOwnership(v, state, resolver)
	validateReverseActiveOwnership(v, state, resolver, now)
}

func validateActiveDoerOwnership(v *violations, state *models.State, resolver *pipeline.Resolver) {
	if resolver == nil {
		return
	}

	for i := range state.Tasks {
		task := &state.Tasks[i]
		if !models.IsExecutingStatus(task, resolver) {
			continue
		}
		if task.AssignedTo == nil || *task.AssignedTo == "" || strings.HasPrefix(*task.AssignedTo, "$") {
			continue
		}
		doerID := *task.AssignedTo
		for _, defect := range models.ActiveDoerOwnershipDefects(state, task, doerID, resolver) {
			v.addID(fmt.Sprintf("task %s doer ownership: %s", task.ID, defect.Check),
				fmt.Errorf("%s task %s %s", task.Status, task.ID, defect.Reason))
		}
	}
}

func validateActiveReviewOwnership(v *violations, state *models.State, resolver *pipeline.Resolver) {
	if resolver == nil {
		return
	}

	for i := range state.Tasks {
		task := &state.Tasks[i]
		if !isActiveReviewingTask(task, resolver) {
			continue
		}
		if task.ReviewingBy == nil || *task.ReviewingBy == "" {
			continue
		}
		reviewerID := *task.ReviewingBy
		// Identities name the task, reviewer and constraint, not the status or
		// the values found, which a mutation may change without repairing.
		owner := fmt.Sprintf("task %s review ownership by %s: ", task.ID, reviewerID)
		if task.ReviewLeaseExpires == nil {
			v.addID(owner+"review_lease_expires", fmt.Errorf("%s task %s without review_lease_expires", task.Status, task.ID))
		}
		agent, exists := state.Agents[reviewerID]
		if !exists {
			// Guard: the remaining checks compare the task with its agent.
			v.addID(owner+"agent", fmt.Errorf("%s task %s reviewing_by %s has no matching agent", task.Status, task.ID, reviewerID))
			continue
		}

		if expectedRole, err := resolver.ReviewerRole(task.RolePair); err != nil {
			v.addID(owner+"role", fmt.Errorf("task %s cannot resolve reviewer role for role_pair %q: %w", task.ID, task.RolePair, err))
		} else if agent.Role != expectedRole {
			v.addID(owner+"role", fmt.Errorf("%s task %s reviewing_by %s has role %q, want %q", task.Status, task.ID, reviewerID, agent.Role, expectedRole))
		}
		if agent.Status != models.AgentStatusReviewing {
			v.addID(owner+"agent status", fmt.Errorf("%s task %s reviewing_by %s has agent status %s, want REVIEWING", task.Status, task.ID, reviewerID, agent.Status))
		}
		if agent.CurrentTask == nil || *agent.CurrentTask != task.ID {
			v.addID(owner+"current_task", fmt.Errorf("%s task %s reviewing_by %s has mismatched current_task", task.Status, task.ID, reviewerID))
		}
		if agent.LeaseExpires == nil {
			v.addID(owner+"agent lease_expires", fmt.Errorf("%s task %s reviewing_by %s has agent without lease_expires", task.Status, task.ID, reviewerID))
		}
		if agent.PID <= 0 {
			v.addID(owner+"agent pid", fmt.Errorf("%s task %s reviewing_by %s has agent without pid", task.Status, task.ID, reviewerID))
		}
	}
}

func isActiveReviewingTask(task *models.Task, resolver *pipeline.Resolver) bool {
	if task.RolePair == "" {
		return false
	}
	reviewing, err := resolver.ReviewingStatus(task.RolePair)
	if err == nil && task.Status == reviewing {
		return true
	}
	reviewing2, err := resolver.Reviewing2Status(task.RolePair)
	return err == nil && task.Status == reviewing2
}

func validateReverseActiveOwnership(v *violations, state *models.State, resolver *pipeline.Resolver, now time.Time) {
	if resolver == nil {
		return
	}

	for _, agentID := range sortedAgentIDs(state) {
		agent := state.Agents[agentID]
		if agent.CurrentTask == nil || *agent.CurrentTask == "" {
			continue
		}
		if models.IsOrchestratorAgent(agent, resolver) {
			continue
		}
		task := state.FindTask(*agent.CurrentTask)
		if task == nil {
			// Guard: every check below reads the task.
			v.add(fmt.Errorf("agent %s says %s %s, but task is missing", agentID, agent.Status, *agent.CurrentTask))
			continue
		}

		// Identities name the agent's claim and the constraint, not the task
		// status or owner found, which a mutation may change without repairing.
		claim := fmt.Sprintf("agent %s says %s %s: ", agentID, agent.Status, task.ID)
		switch agent.Status {
		case models.AgentStatusWorking:
			if !models.IsExecutingStatus(task, resolver) {
				v.addID(claim+"executing", fmt.Errorf("agent %s says WORKING %s, but task status %s is not executing", agentID, task.ID, task.Status))
			}
			if task.AssignedTo == nil || *task.AssignedTo != agentID {
				v.addID(claim+"assigned_to", fmt.Errorf("agent %s says WORKING %s, but task assigned_to is %s", agentID, task.ID, ownerValue(task.AssignedTo)))
			}
			if expectedRole, err := resolver.DoerRole(task.RolePair); err != nil {
				v.addID(claim+"role", fmt.Errorf("agent %s says WORKING %s, but doer role resolution failed for role_pair %q: %w", agentID, task.ID, task.RolePair, err))
			} else if agent.Role != expectedRole {
				v.addID(claim+"role", fmt.Errorf("agent %s says WORKING %s, but agent role %q, want %q", agentID, task.ID, agent.Role, expectedRole))
			}
		case models.AgentStatusReviewing:
			if !isActiveReviewingTask(task, resolver) {
				v.addID(claim+"active review", fmt.Errorf("agent %s says REVIEWING %s, but task status %s is not active review", agentID, task.ID, task.Status))
			}
			if task.ReviewingBy == nil || *task.ReviewingBy != agentID {
				v.addID(claim+"reviewing_by", fmt.Errorf("agent %s says REVIEWING %s, but task reviewing_by is %s", agentID, task.ID, ownerValue(task.ReviewingBy)))
			}
			if expectedRole, err := resolver.ReviewerRole(task.RolePair); err != nil {
				v.addID(claim+"role", fmt.Errorf("agent %s says REVIEWING %s, but reviewer role resolution failed for role_pair %q: %w", agentID, task.ID, task.RolePair, err))
			} else if agent.Role != expectedRole {
				v.addID(claim+"role", fmt.Errorf("agent %s says REVIEWING %s, but agent role %q, want %q", agentID, task.ID, agent.Role, expectedRole))
			}
		case models.AgentStatusWaiting:
			validateWaitingTaskReference(v, resolver, agentID, agent, task, now)
		}
	}
}

func validateWaitingTaskReference(v *violations, resolver *pipeline.Resolver, agentID string, agent models.Agent, task *models.Task, now time.Time) {
	claim := fmt.Sprintf("agent %s says WAITING %s: ", agentID, task.ID)
	if task.RolePair == "" {
		// Guard: the agent's side cannot be resolved without a role pair.
		v.addID(claim+"role_pair", fmt.Errorf("agent %s says WAITING %s, but task has no role_pair", agentID, task.ID))
		return
	}

	expectedDoer, doerErr := resolver.DoerRole(task.RolePair)
	if doerErr != nil {
		v.addID(claim+"role", fmt.Errorf("agent %s says WAITING %s, but doer role resolution failed for role_pair %q: %w", agentID, task.ID, task.RolePair, doerErr))
		return
	}
	if agent.Role == expectedDoer {
		if task.AssignedTo == nil || *task.AssignedTo != agentID {
			v.addID(claim+"doer assigned_to", fmt.Errorf("agent %s says WAITING %s as doer, but task assigned_to is %s", agentID, task.ID, ownerValue(task.AssignedTo)))
		}
		if !isAwaitingVerdictTask(task, resolver) && !isVerdictHandoff(task, resolver, now) {
			v.addID(claim+"doer awaiting verdict", fmt.Errorf("agent %s says WAITING %s as doer, but task status %s is not awaiting review verdict", agentID, task.ID, task.Status))
		}
		return
	}

	expectedReviewer, reviewerErr := resolver.ReviewerRole(task.RolePair)
	if reviewerErr != nil {
		v.addID(claim+"role", fmt.Errorf("agent %s says WAITING %s, but reviewer role resolution failed for role_pair %q: %w", agentID, task.ID, task.RolePair, reviewerErr))
		return
	}
	if agent.Role == expectedReviewer {
		if task.ReviewingBy == nil || *task.ReviewingBy != agentID {
			v.addID(claim+"reviewer reviewing_by", fmt.Errorf("agent %s says WAITING %s as reviewer, but task reviewing_by is %s", agentID, task.ID, ownerValue(task.ReviewingBy)))
		}
		if task.ReviewLeaseExpires == nil {
			v.addID(claim+"reviewer review_lease_expires", fmt.Errorf("agent %s says WAITING %s as reviewer, but task has no review_lease_expires", agentID, task.ID))
		} else if !task.ReviewLeaseExpires.After(now) {
			v.addID(claim+"reviewer review_lease_expires", fmt.Errorf("agent %s says WAITING %s as reviewer, but review_lease_expires is not in the future", agentID, task.ID))
		}
		if !isAwaitingResubmissionTask(task, resolver) {
			v.addID(claim+"reviewer awaiting resubmission", fmt.Errorf("agent %s says WAITING %s as reviewer, but task status %s is not awaiting resubmission", agentID, task.ID, task.Status))
		}
		return
	}

	v.addID(claim+"role", fmt.Errorf("agent %s says WAITING %s, but agent role %q is neither doer %q nor reviewer %q", agentID, task.ID, agent.Role, expectedDoer, expectedReviewer))
}

func isAwaitingVerdictTask(task *models.Task, resolver *pipeline.Resolver) bool {
	submitted, err := resolver.SubmittedStatus(task.RolePair)
	if err == nil && task.Status == submitted {
		return true
	}
	if isActiveReviewingTask(task, resolver) {
		return true
	}
	partiallyApproved, err := resolver.PartiallyApprovedStatus(task.RolePair)
	return err == nil && task.Status == partiallyApproved
}

// isVerdictHandoff accepts the doer's WAITING row between a verdict and its
// await-verdict call releasing current_task: the task sits in its role pair's
// approved or rejected status, set by a verdict within VerdictHandoffGrace of
// now.
func isVerdictHandoff(task *models.Task, resolver *pipeline.Resolver, now time.Time) bool {
	approved, approvedErr := resolver.ApprovedStatus(task.RolePair)
	rejected, rejectedErr := resolver.RejectedStatus(task.RolePair)
	verdictStatus := (approvedErr == nil && task.Status == approved) || (rejectedErr == nil && task.Status == rejected)
	return verdictStatus && models.InVerdictHandoff(task, now)
}

func isAwaitingResubmissionTask(task *models.Task, resolver *pipeline.Resolver) bool {
	rejected, err := resolver.RejectedStatus(task.RolePair)
	if err == nil && task.Status == rejected {
		return true
	}
	return models.IsExecutingStatus(task, resolver)
}

func ownerValue(owner *string) string {
	if owner == nil || *owner == "" {
		return "<none>"
	}
	return *owner
}
