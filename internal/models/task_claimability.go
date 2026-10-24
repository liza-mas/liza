package models

import (
	"fmt"
	"strings"
	"time"
)

// PipelineResolver provides pipeline state resolution for tasks with role-pairs.
// Implemented by pipeline.Resolver.
type PipelineResolver interface {
	RoleType(role string) (string, error)
	AllRoleNames() []string
	DoerRole(rolePair string) (string, error)
	ReviewerRole(rolePair string) (string, error)
	InitialStatus(rolePair string) (TaskStatus, error)
	RejectedStatus(rolePair string) (TaskStatus, error)
	SubmittedStatus(rolePair string) (TaskStatus, error)
	ReviewingStatus(rolePair string) (TaskStatus, error)
	ExecutingStatus(rolePair string) (TaskStatus, error)
	ApprovedStatus(rolePair string) (TaskStatus, error)
	PartiallyApprovedStatus(rolePair string) (TaskStatus, error)
	Reviewing2Status(rolePair string) (TaskStatus, error)
}

// IsClaimable checks if a task is claimable by the given role based on its
// pipeline-defined states, type, and dependencies.
// The role parameter uses the unified hyphenated form (e.g. "code-reviewer").
func (t *Task) IsClaimable(role string, allTasks []Task, pr PipelineResolver) bool {
	if t.RolePair == "" || pr == nil {
		return false
	}
	if t.AssignedTo != nil && strings.HasPrefix(*t.AssignedTo, "$") {
		return false
	}
	if !t.isClaimablePipeline(role, pr) {
		return false
	}
	return checkDependencies(t, allTasks, pr)
}

// isClaimablePipeline checks claimability using pipeline-resolved states.
// The role parameter uses the unified hyphenated form, matching what the
// pipeline resolver returns from DoerRole/ReviewerRole.
func (t *Task) isClaimablePipeline(role string, pr PipelineResolver) bool {
	doerRole, err := pr.DoerRole(t.RolePair)
	if err != nil {
		return false
	}
	reviewerRole, err := pr.ReviewerRole(t.RolePair)
	if err != nil {
		return false
	}

	switch role {
	case doerRole:
		initial, err := pr.InitialStatus(t.RolePair)
		if err != nil {
			return false
		}
		rejected, err := pr.RejectedStatus(t.RolePair)
		if err != nil {
			return false
		}
		return t.Status == initial || t.Status == rejected || t.Status == TaskStatusIntegrationFailed

	case reviewerRole:
		// Defense-in-depth: tasks in review-eligible states must have review_commit.
		// Without it, the task is corrupted and should not be claimable.
		if t.ReviewCommit == nil {
			return false
		}
		if t.ReviewingBy != nil {
			if t.ReviewLeaseExpires == nil || t.ReviewLeaseExpires.After(time.Now()) {
				return false // another reviewer is actively awaiting resubmission
			}
			// Lease expired — stale claim, allow reclaiming
		}
		submitted, err := pr.SubmittedStatus(t.RolePair)
		if err != nil {
			return false
		}
		if t.Status == submitted {
			return true
		}
		// Quorum > 1: partially_approved tasks need a second reviewer.
		partiallyApproved, err := pr.PartiallyApprovedStatus(t.RolePair)
		if err == nil && t.Status == partiallyApproved {
			return true
		}
		return false

	default:
		return false
	}
}

// IsOrchestratorAgent reports whether the agent's configured role resolves to
// the pipeline orchestrator type.
func IsOrchestratorAgent(agent Agent, pr PipelineResolver) bool {
	if pr == nil {
		return false
	}
	roleType, err := pr.RoleType(agent.Role)
	return err == nil && roleType == "orchestrator"
}

// IsResumableOwnedTask is the shared state predicate for a doer supervisor
// resuming an executing task already assigned to its agent ID after a child CLI
// exits or the supervisor restarts.
func IsResumableOwnedTask(state *State, task *Task, agentID string, pr PipelineResolver) bool {
	return ResumableOwnedTaskReason(state, task, agentID, pr) == ""
}

// ActiveDoerOwnershipReason returns "" when an executing task's assigned doer
// row is a valid active owner. It accepts both ordinary WORKING ownership and
// the explicit HANDOFF window.
func ActiveDoerOwnershipReason(state *State, task *Task, agentID string, pr PipelineResolver) string {
	if defects := ActiveDoerOwnershipDefects(state, task, agentID, pr); len(defects) > 0 {
		return defects[0].Reason
	}
	return ""
}

// OwnershipDefect is one reason an owner row is not a valid active owner.
// Check names the constraint without the values the reason reports, so a
// defect keeps its identity while those values change.
type OwnershipDefect struct {
	Check  string
	Reason string
}

// ActiveDoerOwnershipDefects returns every defect ActiveDoerOwnershipReason
// checks, in its order, so the first carries the reason it returns. It stops
// only where a later check has nothing to judge: no task, no matching
// assignment, no agent row.
func ActiveDoerOwnershipDefects(state *State, task *Task, agentID string, pr PipelineResolver) []OwnershipDefect {
	one := func(check, reason string) []OwnershipDefect {
		return []OwnershipDefect{{Check: check, Reason: reason}}
	}
	if state == nil {
		return one("state", "missing state")
	}
	if task == nil {
		return one("task", "missing task")
	}
	if agentID == "" {
		return one("agent_id", "missing agent ID")
	}
	if pr == nil {
		return one("resolver", "missing pipeline resolver")
	}
	if !IsExecutingStatus(task, pr) {
		return one("executing", fmt.Sprintf("task status %s is not executing", task.Status))
	}
	if task.AssignedTo == nil || *task.AssignedTo == "" {
		return one("assigned_to", "missing assigned_to")
	}
	if *task.AssignedTo != agentID {
		return one("assigned_to", fmt.Sprintf("assigned_to %s, want %s", *task.AssignedTo, agentID))
	}
	var defects []OwnershipDefect
	add := func(check, reason string) {
		defects = append(defects, OwnershipDefect{Check: check, Reason: reason})
	}
	if task.LeaseExpires == nil {
		add("task_lease", "without lease_expires")
	}

	agent, ok := state.Agents[agentID]
	if !ok {
		add("agent", fmt.Sprintf("assigned_to %s has no matching agent", agentID))
		return defects
	}

	doerRole, err := pr.DoerRole(task.RolePair)
	if err != nil {
		add("agent_role", fmt.Sprintf("doer role resolution failed for role_pair %q: %v", task.RolePair, err))
	} else if agent.Role != doerRole {
		add("agent_role", fmt.Sprintf("assigned_to %s has role %q, want %q", agentID, agent.Role, doerRole))
	}
	if agent.Provider == "" {
		add("agent_provider", fmt.Sprintf("assigned_to %s has agent without provider", agentID))
	}
	if agent.PID <= 0 {
		add("agent_pid", fmt.Sprintf("assigned_to %s has agent without pid", agentID))
	}
	if agent.LeaseExpires == nil {
		add("agent_lease", fmt.Sprintf("assigned_to %s has agent without lease_expires", agentID))
	}

	mismatched := fmt.Sprintf("assigned_to %s has mismatched current_task", agentID)
	if task.HandoffPending {
		if agent.Status != AgentStatusHandoff {
			add("agent_status", fmt.Sprintf("assigned_to %s has agent status %s, want HANDOFF", agentID, agent.Status))
		}
		if agent.CurrentTask == nil || *agent.CurrentTask != task.ID {
			add("agent_current_task", mismatched)
		}
		return defects
	}

	switch agent.Status {
	case AgentStatusWorking:
		if agent.CurrentTask == nil || *agent.CurrentTask != task.ID {
			add("agent_current_task", mismatched)
		}
	case AgentStatusIdle:
		if agent.CurrentTask != nil && *agent.CurrentTask != "" && *agent.CurrentTask != task.ID {
			add("agent_current_task", mismatched)
		}
	default:
		add("agent_status", fmt.Sprintf("assigned_to %s has agent status %s, want WORKING or resumable IDLE", agentID, agent.Status))
	}
	return defects
}

// ResumableOwnedTaskReason returns "" when IsResumableOwnedTask would return
// true, otherwise it returns the structural reason the owned task cannot be
// resumed.
func ResumableOwnedTaskReason(state *State, task *Task, agentID string, pr PipelineResolver) string {
	if task != nil && task.HandoffPending {
		return "task has handoff_pending true"
	}
	return ActiveDoerOwnershipReason(state, task, agentID, pr)
}

// checkDependencies returns true if all dependencies of the task are satisfied.
// Ordinary edges are canonicalized at mutation boundaries and need direct
// MERGED checks; provider declarations resolve their selected future children.
func checkDependencies(t *Task, allTasks []Task, resolvers ...PipelineResolver) bool {
	var pr PipelineResolver
	if len(resolvers) > 0 {
		pr = resolvers[0]
	}
	if len(UnmetProviderDependencies(t, allTasks, pr)) > 0 {
		return false
	}
	if allTasks == nil || len(t.DependsOn) == 0 {
		return true
	}
	for _, depID := range t.DependsOn {
		found := false
		for i := range allTasks {
			if allTasks[i].ID != depID {
				continue
			}
			found = true
			if allTasks[i].Status != TaskStatusMerged {
				return false
			}
			break
		}
		if !found {
			return false
		}
	}
	return true
}

// TopPriorityTier returns all candidates that share the highest priority
// (lowest number). Returns nil if candidates is empty.
func TopPriorityTier(candidates []*Task) []*Task {
	if len(candidates) == 0 {
		return nil
	}

	bestPriority := candidates[0].Priority
	for _, t := range candidates[1:] {
		if t.Priority < bestPriority {
			bestPriority = t.Priority
		}
	}

	var tier []*Task
	for _, t := range candidates {
		if t.Priority == bestPriority {
			tier = append(tier, t)
		}
	}

	return tier
}
