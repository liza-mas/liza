package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/alerts"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/rolemodels"
	"github.com/liza-mas/liza/internal/roles"
)

// missingRoleCachePrefix keys the roles already alerted as missing.
const missingRoleCachePrefix = "missing-role:"

func checkExpiredLeases(state *models.State) []Alert {
	var raised []Alert
	now := time.Now().UTC()
	graceDeadline := now.Add(-models.LeaseExpiryGracePeriod)

	for agentID, agent := range state.Agents {
		if agent.CurrentTask == nil {
			continue
		}
		if agent.LeaseExpires == nil {
			continue
		}
		if agent.LeaseExpires.Before(graceDeadline) {
			raised = append(raised, Alert{
				Timestamp: now,
				Level:     AlertLevelWarning,
				Category:  alerts.CategoryLeaseExpired.String(),
				Message:   fmt.Sprintf("%s on %s", agentID, *agent.CurrentTask),
				// A different lease expiring is a new condition even when no
				// renewal was observed in between.
				Identity: fmt.Sprintf("%s on %s|%s", agentID, *agent.CurrentTask, agent.LeaseExpires.UTC().Format(time.RFC3339Nano)),
			})
		}
	}

	for _, task := range state.Tasks {
		if task.Status != models.TaskStatusReviewing {
			continue
		}
		if task.ReviewingBy == nil {
			continue
		}
		if task.ReviewLeaseExpires == nil {
			continue
		}
		if task.ReviewLeaseExpires.Before(graceDeadline) {
			raised = append(raised, Alert{
				Timestamp: now,
				Level:     AlertLevelWarning,
				Category:  alerts.CategoryReviewLeaseExpired.String(),
				Message:   fmt.Sprintf("%s on %s — review can be reclaimed", *task.ReviewingBy, task.ID),
			})
		}
	}

	return raised
}

func checkRegisteredAgentsWithoutLiveProcess(state *models.State) []Alert {
	var raised []Alert
	now := time.Now().UTC()
	for agentID, agent := range state.Agents {
		if agent.LeaseExpires == nil || !agent.LeaseExpires.After(now) {
			continue
		}
		if agent.CurrentTask != nil && *agent.CurrentTask != "" {
			switch agent.Status {
			case models.AgentStatusWorking, models.AgentStatusReviewing, models.AgentStatusHandoff:
				continue
			}
		}
		observation := ops.AgentProcessOwnership(agentID, agent, now, models.AgentLivenessWindow(state.Config))
		if observation.Raw.IsLiveOrUnknown() {
			continue
		}
		processDescription := "no live process"
		if observation.Raw.State == "mismatched" {
			processDescription = "mismatched process"
		}
		raised = append(raised, Alert{
			Timestamp: now,
			Level:     AlertLevelCritical,
			Category:  alerts.CategoryRegisteredAgentProcess.String(),
			Message: fmt.Sprintf("agent %s has active lease but %s; %s",
				agentID, processDescription, observation.Diagnostic(agent.PID)),
		})
	}
	return raised
}

func checkRunningTasksWithoutLiveProcess(state *models.State, pr models.PipelineResolver) []Alert {
	if pr == nil {
		return nil
	}

	var raised []Alert
	now := time.Now().UTC()
	skipReverseAgentIDs := make(map[string]bool)

	for i := range state.Tasks {
		task := &state.Tasks[i]
		ownerID, ownerKind, ok := runningTaskOwner(task, pr)
		if !ok {
			continue
		}

		agent, exists := state.Agents[ownerID]
		if !exists {
			raised = append(raised, Alert{
				Timestamp: now,
				Level:     AlertLevelCritical,
				Category:  alerts.CategoryDeadAgentProcess.String(),
				Message: fmt.Sprintf("%s — status %s has %s %s but no registered agent",
					task.ID, task.Status, ownerKind, ownerID),
			})
			continue
		}
		if reason := activeTaskOwnerMismatch(state, task, ownerID, ownerKind, agent, pr); reason != "" {
			skipReverseAgentIDs[ownerID] = true
			raised = append(raised, Alert{
				Timestamp: now,
				Level:     AlertLevelCritical,
				Category:  alerts.CategoryInvalidAgentOwnership.String(),
				Message: fmt.Sprintf("%s — status %s has %s %s with invalid agent row: %s",
					task.ID, task.Status, ownerKind, ownerID, reason),
			})
		}
		processStatus := ops.AgentProcessStatus(ownerID, agent)
		if processStatus.IsLiveOrUnknown() {
			continue
		}
		processDescription := "no live process"
		if processStatus.State == "mismatched" {
			processDescription = "mismatched process"
		}

		raised = append(raised, Alert{
			Timestamp: now,
			Level:     AlertLevelCritical,
			Category:  alerts.CategoryDeadAgentProcess.String(),
			Message: fmt.Sprintf("%s — status %s has %s %s but %s (pid %d: %s)",
				task.ID, task.Status, ownerKind, ownerID, processDescription, agent.PID, processStatus.Detail),
		})
	}

	raised = append(raised, checkReverseActiveAgentOwnership(state, pr, now, skipReverseAgentIDs)...)

	return raised
}

func activeTaskOwnerMismatch(state *models.State, task *models.Task, ownerID string, ownerKind roles.RoleType, agent models.Agent, pr models.PipelineResolver) string {
	if ownerKind == roles.RoleTypeDoer {
		return models.ActiveDoerOwnershipReason(state, task, ownerID, pr)
	}

	expectedRole, ok := expectedOwnerRole(task, ownerKind, pr)
	if ok && agent.Role != expectedRole {
		return fmt.Sprintf("agent role %q, want %q", agent.Role, expectedRole)
	}

	expectedStatus, ok := expectedOwnerStatus(ownerKind)
	if ok && agent.Status != expectedStatus {
		return fmt.Sprintf("agent status %s, want %s", agent.Status, expectedStatus)
	}

	if agent.CurrentTask == nil {
		return fmt.Sprintf("current_task <none>, want %q", task.ID)
	}
	if *agent.CurrentTask != task.ID {
		return fmt.Sprintf("current_task %q, want %q", *agent.CurrentTask, task.ID)
	}

	return ""
}

func expectedOwnerRole(task *models.Task, ownerKind roles.RoleType, pr models.PipelineResolver) (string, bool) {
	switch ownerKind {
	case roles.RoleTypeDoer:
		role, err := pr.DoerRole(task.RolePair)
		return role, err == nil
	case roles.RoleTypeReviewer:
		role, err := pr.ReviewerRole(task.RolePair)
		return role, err == nil
	default:
		return "", false
	}
}

func expectedOwnerStatus(ownerKind roles.RoleType) (models.AgentStatus, bool) {
	switch ownerKind {
	case roles.RoleTypeDoer:
		return models.AgentStatusWorking, true
	case roles.RoleTypeReviewer:
		return models.AgentStatusReviewing, true
	default:
		return "", false
	}
}

func checkReverseActiveAgentOwnership(state *models.State, pr models.PipelineResolver, now time.Time, skipAgentIDs map[string]bool) []Alert {
	tasksByID := make(map[string]*models.Task, len(state.Tasks))
	for i := range state.Tasks {
		task := &state.Tasks[i]
		tasksByID[task.ID] = task
	}

	var raised []Alert
	for agentID, agent := range state.Agents {
		if skipAgentIDs[agentID] {
			continue
		}
		if agent.CurrentTask == nil || *agent.CurrentTask == "" {
			continue
		}
		if models.IsOrchestratorAgent(agent, pr) {
			continue
		}
		taskID := *agent.CurrentTask
		task, exists := tasksByID[taskID]
		if !exists {
			raised = append(raised, invalidReverseOwnershipAlert(now, agentID, agent.Status, taskID, "task is missing"))
			continue
		}

		switch agent.Status {
		case models.AgentStatusWorking:
			if !models.IsExecutingStatus(task, pr) {
				raised = append(raised, invalidReverseOwnershipAlert(now, agentID, agent.Status, taskID,
					fmt.Sprintf("task status %s is not executing", task.Status)))
				continue
			}
			if task.AssignedTo == nil || *task.AssignedTo != agentID {
				raised = append(raised, invalidReverseOwnershipAlert(now, agentID, agent.Status, taskID,
					fmt.Sprintf("task has doer %s", ownerValue(task.AssignedTo))))
			}
		case models.AgentStatusReviewing:
			if !isReviewerActiveStatus(task, pr) {
				raised = append(raised, invalidReverseOwnershipAlert(now, agentID, agent.Status, taskID,
					fmt.Sprintf("task status %s is not active review", task.Status)))
				continue
			}
			if task.ReviewingBy == nil || *task.ReviewingBy != agentID {
				raised = append(raised, invalidReverseOwnershipAlert(now, agentID, agent.Status, taskID,
					fmt.Sprintf("task has reviewer %s", ownerValue(task.ReviewingBy))))
			}
		}
	}

	return raised
}

func invalidReverseOwnershipAlert(now time.Time, agentID string, status models.AgentStatus, taskID string, reason string) Alert {
	return Alert{
		Timestamp: now,
		Level:     AlertLevelCritical,
		Category:  alerts.CategoryInvalidAgentOwnership.String(),
		Message:   fmt.Sprintf("agent %s says %s %s, but %s", agentID, status, taskID, reason),
	}
}

func ownerValue(owner *string) string {
	if owner == nil || *owner == "" {
		return "<none>"
	}
	return *owner
}

func runningTaskOwner(task *models.Task, pr models.PipelineResolver) (string, roles.RoleType, bool) {
	if models.IsExecutingStatus(task, pr) {
		if task.AssignedTo == nil || *task.AssignedTo == "" {
			return "", "", false
		}
		if strings.HasPrefix(*task.AssignedTo, "$") {
			return "", "", false
		}
		return *task.AssignedTo, roles.RoleTypeDoer, true
	}
	if isReviewerActiveStatus(task, pr) {
		if task.ReviewingBy == nil || *task.ReviewingBy == "" {
			return "", "", false
		}
		return *task.ReviewingBy, roles.RoleTypeReviewer, true
	}
	return "", "", false
}

func isReviewerActiveStatus(task *models.Task, pr models.PipelineResolver) bool {
	if task.RolePair == "" || pr == nil {
		return false
	}
	reviewing, err := pr.ReviewingStatus(task.RolePair)
	if err == nil && task.Status == reviewing {
		return true
	}
	reviewing2, err := pr.Reviewing2Status(task.RolePair)
	return err == nil && task.Status == reviewing2
}

// checkMissingRoles alerts when claimable tasks exist but no agent of the
// required role is registered. This catches a common first-user mistake (e.g.,
// starting only a coder but not a code-planner).
//
// Design trade-off: Uses IsClaimable which checks both status AND dependency
// satisfaction, so this only alerts when tasks are *immediately* stuck. Tasks
// blocked by unmet deps won't trigger an alert even if the needed role is
// missing — the alert fires later when deps resolve. This is conservative
// (fewer false positives) at the cost of delayed detection.
func checkMissingRoles(state *models.State, pr models.PipelineResolver, roleModels rolemodels.File, cache map[string]time.Time) []Alert {
	if pr == nil {
		return nil
	}

	// Emit alerts for each missing role, throttled by cache.
	var raised []Alert
	now := time.Now().UTC()

	missingRoles := FindMissingRolesWithClaimableWork(state, pr, roleModels)
	missingRoleSet := make(map[string]bool, len(missingRoles))
	for _, roleWork := range missingRoles {
		role := roleWork.Role
		taskIDs := roleWork.TaskIDs
		missingRoleSet[role] = true
		cacheKey := missingRoleCachePrefix + role
		if _, seen := cache[cacheKey]; seen {
			continue
		}

		// Format task list, capping at 5 IDs.
		const maxListed = 5
		listed := taskIDs
		suffix := ""
		if len(taskIDs) > maxListed {
			listed = taskIDs[:maxListed]
			suffix = fmt.Sprintf("... and %d more", len(taskIDs)-maxListed)
		}
		msg := fmt.Sprintf("no registered agent for role %s — %d task(s) waiting (%s",
			role, roleWork.TaskCount, strings.Join(listed, ", "))
		if suffix != "" {
			msg += ", " + suffix
		}
		msg += fmt.Sprintf("); the TUI auto-repairs by default; run `%s` to preview manually", brand.Command("repair-agent-pool", "--dry-run"))

		raised = append(raised, Alert{
			Timestamp: now,
			Level:     AlertLevelWarning,
			Category:  alerts.CategoryMissingRole.String(),
			Message:   msg,
		})
		cache[cacheKey] = now
	}

	// Clear cache entries for roles no longer in the missing set — either because
	// an agent appeared or because the waiting tasks stopped being claimable
	// (merged, abandoned, deps unmet, etc.). Without this, a stale cache entry
	// would suppress the alert if a *new* task later becomes claimable for the
	// same absent role.
	for key := range cache {
		if !strings.HasPrefix(key, missingRoleCachePrefix) {
			continue
		}
		role := strings.TrimPrefix(key, missingRoleCachePrefix)
		if !missingRoleSet[role] {
			delete(cache, key)
		}
	}

	return raised
}
