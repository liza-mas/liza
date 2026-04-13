package ops

import (
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// ScopeExtensionEntry represents a file outside the declared task scope
// that must be modified, along with the justification for why.
type ScopeExtensionEntry struct {
	File          string
	Justification string
}

// WriteCheckpointInput contains the parameters for writing a pre-execution checkpoint.
type WriteCheckpointInput struct {
	TaskID          string
	AgentID         string
	Intent          string
	ValidationPlan  string
	FilesToModify   []string
	Assumptions     []string
	Risks           string
	TDDNotRequired  string
	ScopeExtensions []ScopeExtensionEntry
	Impact          string
}

// WriteCheckpoint writes a pre-execution checkpoint to a task's history.
// The checkpoint must be written before submitting for review.
func WriteCheckpoint(projectRoot string, input *WriteCheckpointInput) error {
	if input.TaskID == "" {
		return &PreconditionError{Reason: "task_id is required"}
	}
	if input.AgentID == "" {
		return &PreconditionError{Reason: "agent_id is required"}
	}
	if input.Intent == "" {
		return &PreconditionError{Reason: "intent is required"}
	}
	if input.ValidationPlan == "" {
		return &PreconditionError{Reason: "validation_plan is required"}
	}
	// files_to_modify may be empty for read-only analysis tasks (e.g. integration-analyst)
	if !IsValidImpact(input.Impact) {
		return &PreconditionError{Reason: fmt.Sprintf("invalid impact value %q: must be empty, standard, significant, or architecture", input.Impact)}
	}

	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())

	// Collect pipeline executing statuses
	resolver, _, err := loadResolver(projectRoot)
	if err != nil {
		return fmt.Errorf("failed to load pipeline config: %w", err)
	}
	var pipelineExecuting []models.TaskStatus
	for _, rpName := range resolver.RolePairNames() {
		if es, err := resolver.ExecutingStatus(rpName); err == nil {
			pipelineExecuting = append(pipelineExecuting, es)
		}
	}

	now := time.Now().UTC()

	return bb.Modify(func(state *models.State) error {
		task := state.FindTask(input.TaskID)
		if task == nil {
			return fmt.Errorf("task %s not found", input.TaskID)
		}

		if !isExecutingStatus(task.Status, pipelineExecuting) {
			return &PreconditionError{Reason: fmt.Sprintf("task %s is not in an executing state (current status: %s)", input.TaskID, task.Status)}
		}

		if task.AssignedTo == nil || *task.AssignedTo != input.AgentID {
			currentAgent := "none"
			if task.AssignedTo != nil {
				currentAgent = *task.AssignedTo
			}
			return &PreconditionError{Reason: fmt.Sprintf("task %s is not assigned to agent %s (currently assigned to: %s)", input.TaskID, input.AgentID, currentAgent)}
		}

		extra := map[string]any{
			"intent":          input.Intent,
			"validation_plan": input.ValidationPlan,
			"files_to_modify": input.FilesToModify,
		}
		if len(input.Assumptions) > 0 {
			extra["assumptions"] = input.Assumptions
		}
		if input.Risks != "" {
			extra["risks"] = input.Risks
		}
		if input.TDDNotRequired != "" {
			extra["tdd_not_required"] = input.TDDNotRequired
		}
		if input.Impact != "" {
			extra["impact"] = input.Impact
		}
		if len(input.ScopeExtensions) > 0 {
			entries := make([]map[string]string, len(input.ScopeExtensions))
			for i, se := range input.ScopeExtensions {
				entries[i] = map[string]string{
					"file":          se.File,
					"justification": se.Justification,
				}
			}
			extra["scope_extensions"] = entries
		}

		task.History = append(task.History, models.TaskHistoryEntry{
			Time:  now,
			Event: models.TaskEventPreExecutionCheckpoint,
			Agent: &input.AgentID,
			Extra: extra,
		})

		return nil
	})
}

// HasCheckpoint checks whether a task's history contains a pre_execution_checkpoint
// event from the specified agent.
func HasCheckpoint(history []models.TaskHistoryEntry, agentID string) bool {
	for _, entry := range history {
		if entry.Event == models.TaskEventPreExecutionCheckpoint && entry.Agent != nil && *entry.Agent == agentID {
			return true
		}
	}
	return false
}

// GetTDDWaiver returns the tdd_not_required justification from the latest
// pre_execution_checkpoint by agentID, or "" if none was declared.
func GetTDDWaiver(history []models.TaskHistoryEntry, agentID string) string {
	for i := len(history) - 1; i >= 0; i-- {
		entry := history[i]
		if entry.Event == models.TaskEventPreExecutionCheckpoint && entry.Agent != nil && *entry.Agent == agentID {
			if v, ok := entry.Extra["tdd_not_required"].(string); ok {
				return v
			}
			return ""
		}
	}
	return ""
}

// GetCheckpointImpact returns the impact classification from the latest
// pre_execution_checkpoint by agentID, or "" if none was declared.
func GetCheckpointImpact(history []models.TaskHistoryEntry, agentID string) string {
	for i := len(history) - 1; i >= 0; i-- {
		entry := history[i]
		if entry.Event == models.TaskEventPreExecutionCheckpoint && entry.Agent != nil && *entry.Agent == agentID {
			if v, ok := entry.Extra["impact"].(string); ok {
				return v
			}
			return ""
		}
	}
	return ""
}

// GetValidationPlan returns the validation_plan from the latest
// pre_execution_checkpoint by agentID, or "" if none was declared.
func GetValidationPlan(history []models.TaskHistoryEntry, agentID string) string {
	for i := len(history) - 1; i >= 0; i-- {
		entry := history[i]
		if entry.Event == models.TaskEventPreExecutionCheckpoint && entry.Agent != nil && *entry.Agent == agentID {
			if v, ok := entry.Extra["validation_plan"].(string); ok {
				return v
			}
			return ""
		}
	}
	return ""
}

// GetLatestScopeExtensions returns scope_extensions from the latest
// pre_execution_checkpoint by agentID, or nil if none were declared.
func GetLatestScopeExtensions(history []models.TaskHistoryEntry, agentID string) []map[string]string {
	for i := len(history) - 1; i >= 0; i-- {
		entry := history[i]
		if entry.Event != models.TaskEventPreExecutionCheckpoint || entry.Agent == nil || *entry.Agent != agentID {
			continue
		}
		raw, ok := entry.Extra["scope_extensions"]
		if !ok {
			return nil
		}
		// Handle []map[string]string (direct Go usage)
		if typed, ok := raw.([]map[string]string); ok {
			return typed
		}
		// Handle []any (after YAML round-trip)
		arr, ok := raw.([]any)
		if !ok {
			return nil
		}
		result := make([]map[string]string, 0, len(arr))
		for _, item := range arr {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			file, _ := m["file"].(string)
			justification, _ := m["justification"].(string)
			if file != "" && justification != "" {
				result = append(result, map[string]string{
					"file":          file,
					"justification": justification,
				})
			}
		}
		if len(result) > 0 {
			return result
		}
		return nil
	}
	return nil
}
