package models

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// RoleTaskReadiness is the number of tasks currently claimable by one
// configured pipeline role.
type RoleTaskReadiness struct {
	Role  string `json:"role" yaml:"role"`
	Count int    `json:"count" yaml:"count"`
}

// TaskReadiness projects task availability independently of agent capacity.
// Claimable contains doer work and Reviewable contains reviewer work.
type TaskReadiness struct {
	Claimable        int
	Reviewable       int
	ClaimableByRole  []RoleTaskReadiness
	ReviewableByRole []RoleTaskReadiness
}

// GetTaskReadiness returns aggregate and per-role task availability for every
// configured doer and reviewer role. Role counts use IsRoleTaskReady so this
// projection shares lifecycle, dependency, and ownership semantics with task
// selection.
func GetTaskReadiness(state *State, pr PipelineResolver) TaskReadiness {
	var readiness TaskReadiness
	if state == nil || pr == nil {
		return readiness
	}

	roles := slices.Clone(pr.AllRoleNames())
	slices.Sort(roles)
	roles = slices.Compact(roles)
	now := time.Now().UTC()
	for _, role := range roles {
		roleType, err := pr.RoleType(role)
		if err != nil {
			continue
		}

		switch roleType {
		case "doer":
			count := countRoleReadyTasks(state, role, pr, now)
			readiness.ClaimableByRole = append(readiness.ClaimableByRole, RoleTaskReadiness{Role: role, Count: count})
			readiness.Claimable += count
		case "reviewer":
			count := countRoleReadyTasks(state, role, pr, now)
			readiness.ReviewableByRole = append(readiness.ReviewableByRole, RoleTaskReadiness{Role: role, Count: count})
			readiness.Reviewable += count
		}
	}

	return readiness
}

// countRoleReadyTasks counts tasks immediately available to the given role.
func countRoleReadyTasks(state *State, role string, pr PipelineResolver, now time.Time) int {
	if state == nil {
		return 0
	}
	count := 0
	for i := range state.Tasks {
		if IsRoleTaskReady(state, &state.Tasks[i], role, pr, now) {
			count++
		}
	}
	return count
}

// IsRoleTaskReady reports whether a task is immediately available to a role's
// next claimant. A rejected doer task reserved by an active ownership lease is
// available only to its current owner, not to the role queue; malformed
// ownership without a lease fails closed.
func IsRoleTaskReady(state *State, task *Task, role string, pr PipelineResolver, now time.Time) bool {
	if state == nil || task == nil || pr == nil {
		return false
	}
	if !task.IsClaimable(role, state.Tasks, pr) {
		return strandedForRole(state, task, role, pr, now)
	}

	doerRole, err := pr.DoerRole(task.RolePair)
	if err != nil {
		return false
	}
	if role != doerRole {
		return true
	}
	rejected, err := pr.RejectedStatus(task.RolePair)
	if err != nil {
		return false
	}
	if task.Status != rejected || task.AssignedTo == nil || *task.AssignedTo == "" {
		return true
	}
	if task.LeaseExpires == nil {
		return false
	}
	return !task.LeaseExpires.After(now)
}

// StrandedDoerClaimReason explains why an executing doer claim is stranded, or
// returns "" when it is not. Stranded means the task lease expired and the
// holder has no live registration — it died without its exit release — so the
// claim can pass to another doer as a preserved continuation, which needs the
// worktree and base commit it reuses. Liveness is lease-first: a holder whose
// registration is live keeps its claim, and no process evidence is consulted.
func StrandedDoerClaimReason(state *State, task *Task, pr PipelineResolver, now time.Time) string {
	if state == nil || task == nil || pr == nil {
		return ""
	}
	executing, err := pr.ExecutingStatus(task.RolePair)
	if err != nil || task.Status != executing {
		return ""
	}
	if task.AssignedTo == nil || *task.AssignedTo == "" || strings.HasPrefix(*task.AssignedTo, "$") {
		return ""
	}
	if task.LeaseExpires == nil || task.LeaseExpires.After(now) {
		return ""
	}
	if task.Worktree == nil || *task.Worktree == "" || task.BaseCommit == nil || *task.BaseCommit == "" {
		return ""
	}
	if !checkDependencies(task, state.Tasks) {
		return ""
	}
	holder := *task.AssignedTo
	expired := task.LeaseExpires.UTC().Format(time.RFC3339)
	agent, registered := state.Agents[holder]
	if !registered {
		return fmt.Sprintf("holder %s is no longer registered and the task lease expired at %s", holder, expired)
	}
	if AgentRegistrationLive(agent, now, AgentLivenessWindow(state.Config)) {
		return ""
	}
	return fmt.Sprintf("holder %s has no live registration and the task lease expired at %s", holder, expired)
}

// strandedForRole reports whether a task is a stranded claim the given role
// may take over: only the task's own doer role can. A nil resolver sees no
// work, as everywhere else.
func strandedForRole(state *State, task *Task, role string, pr PipelineResolver, now time.Time) bool {
	if pr == nil {
		return false
	}
	doerRole, err := pr.DoerRole(task.RolePair)
	return err == nil && role == doerRole && StrandedDoerClaimReason(state, task, pr, now) != ""
}

// AgentLivenessWindow is how long a registration without a lease stays live on
// heartbeat alone.
func AgentLivenessWindow(config Config) time.Duration {
	return NormalizeHeartbeatInterval(config.HeartbeatInterval) + LeaseExpiryGracePeriod
}

// AgentRegistrationLive reports whether an agent's registration is live: an
// unexpired lease or, for a registration without one, a heartbeat within
// nilLeaseHeartbeatWindow. Every reader of "is this agent live" shares it so
// they agree by construction.
func AgentRegistrationLive(agent Agent, now time.Time, nilLeaseHeartbeatWindow time.Duration) bool {
	if agent.Role == "" {
		return false
	}
	if agent.LeaseExpires != nil {
		return agent.LeaseExpires.After(now)
	}
	if agent.Heartbeat.IsZero() {
		return false
	}
	return agent.Heartbeat.After(now.Add(-nilLeaseHeartbeatWindow))
}

// CountClaimableTasks preserves the legacy lifecycle-level count for a role.
// It checks task status and dependencies but not rejected-task ownership.
func CountClaimableTasks(state *State, role string, pr PipelineResolver) int {
	count := 0
	for i := range state.Tasks {
		if state.Tasks[i].IsClaimable(role, state.Tasks, pr) {
			count++
		}
	}
	return count
}

// DoerClaimBlockedReason returns a human-readable reason when the given agent
// cannot claim a doer task. An empty string means claimable by this agent.
func DoerClaimBlockedReason(state *State, task *Task, role, agentID string, pr PipelineResolver, now time.Time) string {
	if state == nil {
		return "state is required"
	}
	if task == nil {
		return "task is required"
	}
	if agentID == "" {
		return "agent ID is required"
	}
	if !task.IsClaimable(role, state.Tasks, pr) {
		if strandedForRole(state, task, role, pr, now) {
			return ""
		}
		return fmt.Sprintf("task %s is %s (not claimable by %s)", task.ID, task.Status, role)
	}

	doerRole, err := pr.DoerRole(task.RolePair)
	if err != nil {
		return fmt.Sprintf("invalid role-pair %q: %v", task.RolePair, err)
	}
	if role != doerRole {
		return fmt.Sprintf("task %s is %s (not claimable by %s)", task.ID, task.Status, role)
	}

	rejected, err := pr.RejectedStatus(task.RolePair)
	if err != nil {
		return fmt.Sprintf("invalid role-pair %q: %v", task.RolePair, err)
	}
	if task.Status != rejected {
		return ""
	}
	if task.AssignedTo == nil || *task.AssignedTo == "" {
		return ""
	}
	if IsRoleTaskReady(state, task, role, pr, now) {
		return ""
	}

	assignedTo := *task.AssignedTo
	if strings.HasPrefix(assignedTo, "$") {
		return fmt.Sprintf("task %s is in transition (assigned_to: %s)", task.ID, assignedTo)
	}
	if task.LeaseExpires == nil {
		return fmt.Sprintf("task %s has rejected ownership assigned_to=%s without lease_expires; repair ownership before reclaiming", task.ID, assignedTo)
	}
	if assignedTo == agentID {
		return ""
	}
	if task.LeaseExpires.After(now) {
		return fmt.Sprintf("task %s is assigned to %s until %s; %s cannot claim rejected work before the lease expires",
			task.ID, assignedTo, task.LeaseExpires.UTC().Format(time.RFC3339), agentID)
	}

	return fmt.Sprintf("task %s is %s (not claimable by %s)", task.ID, task.Status, role)
}

// IsDoerClaimableByAgent is the agent-aware doer claimability predicate used by
// supervisors and claim selection. It preserves role-level claimability while
// honoring rejected-task ownership leases.
func IsDoerClaimableByAgent(state *State, task *Task, role, agentID string, pr PipelineResolver, now time.Time) bool {
	return DoerClaimBlockedReason(state, task, role, agentID, pr, now) == ""
}

// CountDoerClaimableTasksForAgent counts tasks a specific doer agent can claim.
func CountDoerClaimableTasksForAgent(state *State, role, agentID string, pr PipelineResolver) int {
	if state == nil {
		return 0
	}
	now := time.Now().UTC()
	count := 0
	for i := range state.Tasks {
		if IsDoerClaimableByAgent(state, &state.Tasks[i], role, agentID, pr, now) {
			count++
		}
	}
	return count
}

// CountReviewableTasks counts tasks immediately claimable by the reviewer role.
// Uses IsClaimable so each role-pair's reviewer states are honored.
func CountReviewableTasks(state *State, role string, pr PipelineResolver) int {
	count := 0
	for i := range state.Tasks {
		if state.Tasks[i].IsClaimable(role, state.Tasks, pr) {
			count++
		}
	}
	return count
}

// CountReviewableTasksForAgent is the agent-aware variant of CountReviewableTasks:
// it excludes tasks that the given agent has already approved. The reviewer
// supervisor's wait loop uses this so an agent that just approved doesn't
// spin "found 1 reviewable task" → claim → reject (self-approval) → repeat.
func CountReviewableTasksForAgent(state *State, role, agentID string, pr PipelineResolver) int {
	count := 0
	for i := range state.Tasks {
		t := &state.Tasks[i]
		if !t.IsClaimable(role, state.Tasks, pr) {
			continue
		}
		if t.HasApprovalFromAgent(agentID) {
			continue
		}
		count++
	}
	return count
}

// GetCoderWorkDiagnostics returns detailed diagnostic information about task availability for coders.
func GetCoderWorkDiagnostics(state *State, pr PipelineResolver) string {
	claimable := CountClaimableTasks(state, RoleCoder, pr)
	return getCoderWorkDiagnostics(state, pr, claimable)
}

// GetCoderWorkDiagnosticsForAgent returns coder diagnostics using agent-aware
// claimability, so protected rejected tasks do not wake the wrong coder.
func GetCoderWorkDiagnosticsForAgent(state *State, agentID string, pr PipelineResolver) string {
	claimable := CountDoerClaimableTasksForAgent(state, RoleCoder, agentID, pr)
	return getCoderWorkDiagnostics(state, pr, claimable)
}

func getCoderWorkDiagnostics(state *State, pr PipelineResolver, claimable int) string {
	if claimable > 0 {
		return fmt.Sprintf("Found %d claimable task(s)", claimable)
	}

	blockedByDeps := 0
	blocked := 0
	inProgress := 0

	depResolver := NewDependencyResolver(state)

	for _, task := range state.Tasks {
		if task.Status == TaskStatusBlocked {
			blocked++
		}

		if BlockedByDependencies(&task, pr, depResolver) {
			blockedByDeps++
		}

		// Pipeline path: use resolver to classify statuses dynamically.
		if task.RolePair != "" && pr != nil {
			if isInProgressPipeline(&task, pr) {
				inProgress++
			}
			continue
		}

		if task.Status == TaskStatusImplementing ||
			task.Status.IsReadyForReviewStatus() ||
			task.Status == TaskStatusReviewing ||
			task.Status == TaskStatusApproved {
			inProgress++
		}
	}

	parts := []string{"No claimable tasks"}
	if blocked > 0 {
		parts = append(parts, fmt.Sprintf("%d blocked tasks", blocked))
	}
	if blockedByDeps > 0 {
		parts = append(parts, fmt.Sprintf("%d blocked by dependencies", blockedByDeps))
	}
	if inProgress > 0 {
		parts = append(parts, fmt.Sprintf("%d in progress", inProgress))
	}

	return strings.Join(parts, "; ")
}

// BlockedByDependencies reports whether a task is in a claimable/reclaimable
// status but held back by dependencies that the resolver does not satisfy.
func BlockedByDependencies(task *Task, pr PipelineResolver, depResolver *DependencyResolver) bool {
	if task == nil || depResolver == nil {
		return false
	}
	if depResolver.state == nil {
		return false
	}
	if task.RolePair != "" && pr != nil {
		return isBlockedByDepsPipeline(task, pr, depResolver)
	}
	if task.Status != TaskStatusReady &&
		task.Status != TaskStatusRejected &&
		task.Status != TaskStatusIntegrationFailed {
		return false
	}
	return !checkDependencies(task, depResolver.state.Tasks)
}

// isBlockedByDepsPipeline checks if a pipeline task is in an initial/rejected status
// with unsatisfied dependencies.
func isBlockedByDepsPipeline(task *Task, pr PipelineResolver, depResolver *DependencyResolver) bool {
	initial, err := pr.InitialStatus(task.RolePair)
	if err != nil {
		return false
	}
	rejected, err := pr.RejectedStatus(task.RolePair)
	if err != nil {
		return false
	}
	if task.Status != initial && task.Status != rejected && task.Status != TaskStatusIntegrationFailed {
		return false
	}
	return !checkDependencies(task, depResolver.state.Tasks)
}

// isInProgressPipeline checks if a pipeline task is in a pipeline-defined in-progress state.
func isInProgressPipeline(task *Task, pr PipelineResolver) bool {
	executing, _ := pr.ExecutingStatus(task.RolePair)
	submitted, _ := pr.SubmittedStatus(task.RolePair)
	reviewing, _ := pr.ReviewingStatus(task.RolePair)
	if task.Status == executing || task.Status == submitted || task.Status == reviewing {
		return true
	}
	// Quorum states are also in-progress (task is in the review pipeline).
	partiallyApproved, err := pr.PartiallyApprovedStatus(task.RolePair)
	if err == nil && task.Status == partiallyApproved {
		return true
	}
	reviewing2, err := pr.Reviewing2Status(task.RolePair)
	if err == nil && task.Status == reviewing2 {
		return true
	}
	return false
}

// GetReviewerWorkDiagnostics returns detailed diagnostic information about review availability.
// For pipeline tasks, filters by the resolved reviewer role to avoid counting tasks
// belonging to a different reviewer role (e.g. code-plan-reviewer).
func GetReviewerWorkDiagnostics(state *State, pr PipelineResolver) string {
	now := time.Now().UTC()

	unassigned := 0
	expiredLeases := 0
	activelyReviewing := 0
	awaitingSecondReview := 0
	inSecondReview := 0

	for _, task := range state.Tasks {
		// Pipeline path: use resolver to classify statuses dynamically.
		if task.RolePair != "" && pr != nil {
			// Gate by reviewer role: only count tasks whose pipeline reviewer
			// matches the code-reviewer runtime role.
			reviewerRole, err := pr.ReviewerRole(task.RolePair)
			if err != nil {
				continue
			}
			if reviewerRole != RoleCodeReviewer {
				continue
			}

			submitted, _ := pr.SubmittedStatus(task.RolePair)
			reviewing, _ := pr.ReviewingStatus(task.RolePair)
			partiallyApproved, errPA := pr.PartiallyApprovedStatus(task.RolePair)
			reviewing2, errR2 := pr.Reviewing2Status(task.RolePair)

			switch {
			case task.Status == submitted && task.ReviewCommit != nil:
				unassigned++
			case task.Status == reviewing:
				if task.ReviewLeaseExpires != nil && task.ReviewLeaseExpires.Before(now) {
					expiredLeases++
				} else {
					activelyReviewing++
				}
			case errPA == nil && task.Status == partiallyApproved && task.ReviewCommit != nil:
				awaitingSecondReview++
			case errR2 == nil && task.Status == reviewing2:
				if task.ReviewLeaseExpires != nil && task.ReviewLeaseExpires.Before(now) {
					expiredLeases++
				} else {
					inSecondReview++
				}
			}
			continue
		}

		// Fallback: hardcoded status checks when resolver is unavailable.
		if task.Status.IsReadyForReviewStatus() && task.EffectiveType().HasRole(RoleCodeReviewer) && task.ReviewCommit != nil {
			unassigned++
		}
		if task.Status == TaskStatusReviewing && task.EffectiveType().HasRole(RoleCodeReviewer) {
			if task.ReviewLeaseExpires != nil && task.ReviewLeaseExpires.Before(now) {
				expiredLeases++
			} else {
				activelyReviewing++
			}
		}
	}

	if unassigned > 0 || awaitingSecondReview > 0 {
		parts := []string{fmt.Sprintf("Found %d reviewable task(s)", unassigned+awaitingSecondReview)}
		if awaitingSecondReview > 0 {
			parts = append(parts, fmt.Sprintf("%d awaiting second review", awaitingSecondReview))
		}
		if expiredLeases > 0 {
			parts = append(parts, fmt.Sprintf("%d with stale leases (pending reclamation)", expiredLeases))
		}
		if inSecondReview > 0 {
			parts = append(parts, fmt.Sprintf("%d in second review", inSecondReview))
		}
		return strings.Join(parts, "; ")
	}

	parts := []string{"No reviewable tasks"}
	if expiredLeases > 0 {
		parts = append(parts, fmt.Sprintf("%d with stale leases (pending reclamation)", expiredLeases))
	}
	if activelyReviewing > 0 {
		parts = append(parts, fmt.Sprintf("%d actively being reviewed", activelyReviewing))
	}
	if inSecondReview > 0 {
		parts = append(parts, fmt.Sprintf("%d in second review", inSecondReview))
	}

	return strings.Join(parts, "; ")
}
