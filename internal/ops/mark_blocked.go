package ops

import (
	"fmt"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/alerts"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// MarkBlockedResult contains the outcome of marking a task as blocked.
type MarkBlockedResult struct {
	TaskID        string                `json:"task_id"`
	Reason        string                `json:"reason"`
	DependsOn     []string              `json:"depends_on,omitempty"`
	RepairRequest *models.RepairRequest `json:"repair_request,omitempty"`
	Warnings      []string              `json:"warnings,omitempty"`
}

func (r *MarkBlockedResult) GetWarnings() []string {
	if r == nil {
		return nil
	}
	return r.Warnings
}

// MarkBlockedOptions contains optional metadata for a block transition.
type MarkBlockedOptions struct {
	RepairRequest *models.RepairRequest
	DependsOn     []string
}

// MarkBlocked transitions a task from an executing status to BLOCKED. Only the
// assigned agent can block its own task. Requires reason and 1-3 clarifying
// questions per the blocking protocol. Uses pipeline-defined executing statuses.
func MarkBlocked(projectRoot, taskID, reason string, questions []string, agentID string) (*MarkBlockedResult, error) {
	return MarkBlockedWithOptions(projectRoot, taskID, reason, questions, agentID, MarkBlockedOptions{})
}

// MarkBlockedWithOptions transitions a task from an executing status to BLOCKED
// and optionally records a structured repair request for orchestrator follow-up.
func MarkBlockedWithOptions(projectRoot, taskID, reason string, questions []string, agentID string, opts MarkBlockedOptions) (*MarkBlockedResult, error) {
	if taskID == "" {
		return nil, &PreconditionError{Reason: "task ID is required"}
	}
	if reason == "" {
		return nil, &PreconditionError{Reason: "reason is required"}
	}
	if agentID == "" {
		return nil, &PreconditionError{Reason: "agent ID is required"}
	}
	if len(questions) == 0 {
		return nil, &PreconditionError{Reason: "at least 1 question is required"}
	}
	if len(questions) > 3 {
		return nil, &PreconditionError{Reason: "maximum 3 questions allowed per blocking protocol"}
	}
	repairRequest, err := normalizeRepairRequest(opts.RepairRequest)
	if err != nil {
		return nil, err
	}
	dependsOn, err := normalizeDependsOn(opts.DependsOn)
	if err != nil {
		return nil, err
	}

	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())
	now := time.Now().UTC()
	var resultDependsOn []string

	// Load pipeline config for status checks and transitions.
	resolver, _, err := loadResolver(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load pipeline config: %w", err)
	}
	var pipelineExecuting []models.TaskStatus
	for _, rpName := range resolver.RolePairNames() {
		if es, err := resolver.ExecutingStatus(rpName); err == nil {
			pipelineExecuting = append(pipelineExecuting, es)
		}
	}
	pipelineTransitions := BuildPipelineTransitions(resolver)

	err = bb.Modify(func(state *models.State) error {
		task := state.FindTask(taskID)
		if task == nil {
			return &errors.NotFoundError{Entity: "task", ID: taskID}
		}

		if !isExecutingStatus(task.Status, pipelineExecuting) {
			return &PreconditionError{Reason: fmt.Sprintf("task must be in an executing status to be marked blocked, current status: %s", task.Status)}
		}

		if task.AssignedTo == nil || *task.AssignedTo != agentID {
			return &PreconditionError{Reason: "only the assigned agent can mark task as blocked"}
		}
		if err := validateDependsOnForBlockedTask(state, task, dependsOn); err != nil {
			return err
		}

		if err := task.TransitionWith(models.TaskStatusBlocked, pipelineTransitions); err != nil {
			return err
		}
		task.BlockedReason = &reason
		task.BlockedQuestions = questions
		task.RepairRequest = repairRequest
		if len(dependsOn) > 0 {
			task.DependsOn = append(task.DependsOn, dependsOn...)
		}
		resultDependsOn = append([]string(nil), task.DependsOn...)
		releaseAgentsForTask(state, taskID)
		task.AssignedTo = nil
		task.LeaseExpires = nil

		task.History = append(task.History, models.TaskHistoryEntry{
			Time:   now,
			Event:  models.TaskEventBlocked,
			Agent:  &agentID,
			Reason: &reason,
		})

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to mark task as blocked: %w", err)
	}

	var warnings []string
	if err := alerts.Write(lp.AlertsLogPath(), alerts.Alert{
		Timestamp: now,
		Level:     alerts.AlertLevelWarning,
		Category:  "BLOCKED",
		Message:   fmt.Sprintf("%s — %s", taskID, reason),
	}); err != nil {
		warnings = append(warnings, fmt.Sprintf("alert write failed: %v", err))
	}

	return &MarkBlockedResult{
		TaskID:        taskID,
		Reason:        reason,
		DependsOn:     resultDependsOn,
		RepairRequest: repairRequest,
		Warnings:      warnings,
	}, nil
}

func normalizeDependsOn(values []string) ([]string, error) {
	var normalized []string
	seen := make(map[string]bool)
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			depID := strings.TrimSpace(part)
			if depID == "" {
				return nil, &PreconditionError{Reason: "depends-on entries cannot be empty"}
			}
			if seen[depID] {
				return nil, &PreconditionError{Reason: fmt.Sprintf("duplicate depends-on entry %q", depID)}
			}
			seen[depID] = true
			normalized = append(normalized, depID)
		}
	}
	return normalized, nil
}

func validateDependsOnForBlockedTask(state *models.State, task *models.Task, deps []string) error {
	if len(deps) == 0 {
		return nil
	}
	existing := make(map[string]bool, len(task.DependsOn))
	for _, depID := range task.DependsOn {
		trimmed := strings.TrimSpace(depID)
		if trimmed == "" {
			return &PreconditionError{Reason: fmt.Sprintf("task %s has empty existing depends_on entry", task.ID)}
		}
		if existing[trimmed] {
			return &PreconditionError{Reason: fmt.Sprintf("task %s has duplicate existing depends_on entry %q", task.ID, trimmed)}
		}
		existing[trimmed] = true
	}
	for _, depID := range deps {
		if depID == task.ID {
			return &PreconditionError{Reason: fmt.Sprintf("task %s cannot depend on itself", task.ID)}
		}
		if existing[depID] {
			return &PreconditionError{Reason: fmt.Sprintf("depends-on entry %q already exists on task %s", depID, task.ID)}
		}
		if state.FindTask(depID) == nil {
			return &PreconditionError{Reason: fmt.Sprintf("depends-on references non-existent task %q", depID)}
		}
		if dependencyReachesTask(state, depID, task.ID, map[string]bool{}) {
			return &PreconditionError{Reason: fmt.Sprintf("depends-on entry %q would create a dependency cycle for task %s", depID, task.ID)}
		}
	}
	return nil
}

func dependencyReachesTask(state *models.State, currentID, targetID string, visited map[string]bool) bool {
	if currentID == targetID {
		return true
	}
	if visited[currentID] {
		return false
	}
	visited[currentID] = true
	current := state.FindTask(currentID)
	if current == nil {
		return false
	}
	for _, depID := range current.DependsOn {
		if dependencyReachesTask(state, depID, targetID, visited) {
			return true
		}
	}
	return false
}

func normalizeRepairRequest(request *models.RepairRequest) (*models.RepairRequest, error) {
	if request == nil {
		return nil, nil
	}

	normalized := &models.RepairRequest{
		Operation:  strings.TrimSpace(request.Operation),
		Target:     strings.TrimSpace(request.Target),
		Command:    strings.TrimSpace(request.Command),
		Evidence:   compactNonEmpty(request.Evidence),
		Validation: compactNonEmpty(request.Validation),
	}
	if normalized.Operation == "" {
		return nil, &PreconditionError{Reason: "repair request operation is required"}
	}
	if normalized.Target == "" {
		return nil, &PreconditionError{Reason: "repair request target is required"}
	}
	if normalized.Command == "" {
		return nil, &PreconditionError{Reason: "repair request command is required"}
	}
	if len(normalized.Evidence) == 0 {
		return nil, &PreconditionError{Reason: "repair request evidence is required"}
	}
	if len(normalized.Validation) == 0 {
		return nil, &PreconditionError{Reason: "repair request validation is required"}
	}
	if !normalized.HasStructuredFailureEvidence() {
		return nil, &PreconditionError{Reason: fmt.Sprintf(`repair requests require structured failure evidence; valid examples: "command=%s exit_code=1 stderr=command requires role type [orchestrator]", "command=provider-call exit_code=1 error=provider unavailable", or "error=provider session thread not found"`, brand.Command("add-task", "--json"))}
	}
	return normalized, nil
}

func compactNonEmpty(values []string) []string {
	var compacted []string
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			compacted = append(compacted, trimmed)
		}
	}
	return compacted
}
