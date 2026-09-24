package statevalidate

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

// validateAgentInvariants checks that every WORKING agent has a current_task
// and a valid lease_expires timestamp. Warns (via warnWriter) when a lease has
// expired past the grace period, which may indicate a long-running operation
// rather than a stuck agent. Prevents orphaned agents that consume capacity
// without making progress.
func validateAgentInvariants(state *models.State, projectRoot string, skipSpecFileCheck bool, warnWriter io.Writer, resolver *pipeline.Resolver) error {
	now := time.Now().UTC()
	graceDeadline := now.Add(-models.LeaseExpiryGracePeriod)

	for agentID, agent := range state.Agents {
		if agent.LeaseExpires != nil && agent.LeaseExpires.After(now) {
			if agent.Role == "" {
				return fmt.Errorf("agent %s has active lease but no role", agentID)
			}
			if agent.Provider == "" {
				return fmt.Errorf("agent %s has active lease but no provider", agentID)
			}
			if agent.PID <= 0 {
				return fmt.Errorf("agent %s has active lease but no pid", agentID)
			}
		}

		// WORKING agent must have current_task
		if agent.Status == models.AgentStatusWorking && (agent.CurrentTask == nil || *agent.CurrentTask == "") {
			return fmt.Errorf("agent %s has status WORKING but no current_task assigned", agentID)
		}

		// WORKING agent must have valid lease_expires
		if agent.Status == models.AgentStatusWorking {
			if agent.LeaseExpires == nil {
				return fmt.Errorf("agent %s has status WORKING but no lease_expires", agentID)
			}

			// Check lease expiry with grace period (warning only in original script)
			if agent.LeaseExpires.Before(graceDeadline) {
				// In bash this is a warning, but we'll treat it as an error for stricter validation
				// Could make this configurable if needed
				fmt.Fprintf(warnWriter, "WARNING: Agent %s has status WORKING but lease expired (may be long-running operation)\n", agentID)
			}
		}
	}

	if err := validateActiveReviewOwnership(state, resolver); err != nil {
		return err
	}
	if err := validateActiveDoerOwnership(state, resolver); err != nil {
		return err
	}
	if err := validateReverseActiveOwnership(state, resolver); err != nil {
		return err
	}

	return nil
}

func validateActiveDoerOwnership(state *models.State, resolver *pipeline.Resolver) error {
	if resolver == nil {
		return nil
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
		if reason := models.ActiveDoerOwnershipReason(state, task, doerID, resolver); reason != "" {
			return fmt.Errorf("%s task %s %s", task.Status, task.ID, reason)
		}
	}

	return nil
}

func validateActiveReviewOwnership(state *models.State, resolver *pipeline.Resolver) error {
	if resolver == nil {
		return nil
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
		agent, exists := state.Agents[reviewerID]
		if !exists {
			return fmt.Errorf("%s task %s reviewing_by %s has no matching agent", task.Status, task.ID, reviewerID)
		}

		expectedRole, err := resolver.ReviewerRole(task.RolePair)
		if err != nil {
			return fmt.Errorf("task %s cannot resolve reviewer role for role_pair %q: %w", task.ID, task.RolePair, err)
		}
		if agent.Role != expectedRole {
			return fmt.Errorf("%s task %s reviewing_by %s has role %q, want %q", task.Status, task.ID, reviewerID, agent.Role, expectedRole)
		}
		if agent.Status != models.AgentStatusReviewing {
			return fmt.Errorf("%s task %s reviewing_by %s has agent status %s, want REVIEWING", task.Status, task.ID, reviewerID, agent.Status)
		}
		if agent.CurrentTask == nil || *agent.CurrentTask != task.ID {
			return fmt.Errorf("%s task %s reviewing_by %s has mismatched current_task", task.Status, task.ID, reviewerID)
		}
		if task.ReviewLeaseExpires == nil {
			return fmt.Errorf("%s task %s without review_lease_expires", task.Status, task.ID)
		}
		if agent.LeaseExpires == nil {
			return fmt.Errorf("%s task %s reviewing_by %s has agent without lease_expires", task.Status, task.ID, reviewerID)
		}
		if agent.PID <= 0 {
			return fmt.Errorf("%s task %s reviewing_by %s has agent without pid", task.Status, task.ID, reviewerID)
		}
	}

	return nil
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

func validateReverseActiveOwnership(state *models.State, resolver *pipeline.Resolver) error {
	if resolver == nil {
		return nil
	}

	for agentID, agent := range state.Agents {
		if agent.CurrentTask == nil || *agent.CurrentTask == "" {
			continue
		}
		if models.IsOrchestratorAgent(agent, resolver) {
			continue
		}
		task := state.FindTask(*agent.CurrentTask)
		if task == nil {
			return fmt.Errorf("agent %s says %s %s, but task is missing", agentID, agent.Status, *agent.CurrentTask)
		}

		switch agent.Status {
		case models.AgentStatusWorking:
			if !models.IsExecutingStatus(task, resolver) {
				return fmt.Errorf("agent %s says WORKING %s, but task status %s is not executing", agentID, task.ID, task.Status)
			}
			if task.AssignedTo == nil || *task.AssignedTo != agentID {
				return fmt.Errorf("agent %s says WORKING %s, but task assigned_to is %s", agentID, task.ID, ownerValue(task.AssignedTo))
			}
			expectedRole, err := resolver.DoerRole(task.RolePair)
			if err != nil {
				return fmt.Errorf("agent %s says WORKING %s, but doer role resolution failed for role_pair %q: %w", agentID, task.ID, task.RolePair, err)
			}
			if agent.Role != expectedRole {
				return fmt.Errorf("agent %s says WORKING %s, but agent role %q, want %q", agentID, task.ID, agent.Role, expectedRole)
			}
		case models.AgentStatusReviewing:
			if !isActiveReviewingTask(task, resolver) {
				return fmt.Errorf("agent %s says REVIEWING %s, but task status %s is not active review", agentID, task.ID, task.Status)
			}
			if task.ReviewingBy == nil || *task.ReviewingBy != agentID {
				return fmt.Errorf("agent %s says REVIEWING %s, but task reviewing_by is %s", agentID, task.ID, ownerValue(task.ReviewingBy))
			}
			expectedRole, err := resolver.ReviewerRole(task.RolePair)
			if err != nil {
				return fmt.Errorf("agent %s says REVIEWING %s, but reviewer role resolution failed for role_pair %q: %w", agentID, task.ID, task.RolePair, err)
			}
			if agent.Role != expectedRole {
				return fmt.Errorf("agent %s says REVIEWING %s, but agent role %q, want %q", agentID, task.ID, agent.Role, expectedRole)
			}
		case models.AgentStatusWaiting:
			if err := validateWaitingTaskReference(resolver, agentID, agent, task); err != nil {
				return err
			}
		}
	}

	return nil
}

func validateWaitingTaskReference(resolver *pipeline.Resolver, agentID string, agent models.Agent, task *models.Task) error {
	if task.RolePair == "" {
		return fmt.Errorf("agent %s says WAITING %s, but task has no role_pair", agentID, task.ID)
	}

	expectedDoer, doerErr := resolver.DoerRole(task.RolePair)
	if doerErr != nil {
		return fmt.Errorf("agent %s says WAITING %s, but doer role resolution failed for role_pair %q: %w", agentID, task.ID, task.RolePair, doerErr)
	}
	if agent.Role == expectedDoer {
		if task.AssignedTo == nil || *task.AssignedTo != agentID {
			return fmt.Errorf("agent %s says WAITING %s as doer, but task assigned_to is %s", agentID, task.ID, ownerValue(task.AssignedTo))
		}
		if !isAwaitingVerdictTask(task, resolver) && !isVerdictHandoff(task, resolver) {
			return fmt.Errorf("agent %s says WAITING %s as doer, but task status %s is not awaiting review verdict", agentID, task.ID, task.Status)
		}
		return nil
	}

	expectedReviewer, reviewerErr := resolver.ReviewerRole(task.RolePair)
	if reviewerErr != nil {
		return fmt.Errorf("agent %s says WAITING %s, but reviewer role resolution failed for role_pair %q: %w", agentID, task.ID, task.RolePair, reviewerErr)
	}
	if agent.Role == expectedReviewer {
		if task.ReviewingBy == nil || *task.ReviewingBy != agentID {
			return fmt.Errorf("agent %s says WAITING %s as reviewer, but task reviewing_by is %s", agentID, task.ID, ownerValue(task.ReviewingBy))
		}
		if task.ReviewLeaseExpires == nil {
			return fmt.Errorf("agent %s says WAITING %s as reviewer, but task has no review_lease_expires", agentID, task.ID)
		}
		if !task.ReviewLeaseExpires.After(time.Now().UTC()) {
			return fmt.Errorf("agent %s says WAITING %s as reviewer, but review_lease_expires is not in the future", agentID, task.ID)
		}
		if !isAwaitingResubmissionTask(task, resolver) {
			return fmt.Errorf("agent %s says WAITING %s as reviewer, but task status %s is not awaiting resubmission", agentID, task.ID, task.Status)
		}
		return nil
	}

	return fmt.Errorf("agent %s says WAITING %s, but agent role %q is neither doer %q nor reviewer %q", agentID, task.ID, agent.Role, expectedDoer, expectedReviewer)
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
// approved or rejected status, set by a verdict within VerdictHandoffGrace.
func isVerdictHandoff(task *models.Task, resolver *pipeline.Resolver) bool {
	approved, approvedErr := resolver.ApprovedStatus(task.RolePair)
	rejected, rejectedErr := resolver.RejectedStatus(task.RolePair)
	verdictStatus := (approvedErr == nil && task.Status == approved) || (rejectedErr == nil && task.Status == rejected)
	return verdictStatus && models.InVerdictHandoff(task, time.Now().UTC())
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
