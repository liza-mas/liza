package ops

import (
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/identity"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/roles"
)

// AssessHypothesisExhaustedResult contains the outcome of recording an orchestrator assessment.
type AssessHypothesisExhaustedResult struct {
	models.LifecycleOutcome
	TaskID   string   `json:"task_id"`
	Warnings []string `json:"warnings,omitempty"`
}

func (r *AssessHypothesisExhaustedResult) GetWarnings() []string {
	if r == nil {
		return nil
	}
	return r.Warnings
}

// AssessHypothesisExhausted records that the orchestrator has assessed a hypothesis-exhausted task.
// If the exhausted task is not already BLOCKED, it transitions the task to BLOCKED
// so the task cannot be reassigned unchanged.
func AssessHypothesisExhausted(projectRoot, taskID, note, agentID string) (*AssessHypothesisExhaustedResult, error) {
	return assessHypothesisExhaustedWithOptionalAuthority(projectRoot, taskID, note, agentID, nil)
}

// AssessHypothesisExhaustedWithAuthority fences the assessment write with the
// orchestrator's registration generation.
func AssessHypothesisExhaustedWithAuthority(projectRoot, taskID, note string, authority models.AgentAuthority) (*AssessHypothesisExhaustedResult, error) {
	return assessHypothesisExhaustedWithOptionalAuthority(projectRoot, taskID, note, authority.ID, &authority)
}

// AssessHypothesisExhaustedWithOptions identifies one assessment occurrence.
func AssessHypothesisExhaustedWithOptions(projectRoot, taskID, note, agentID string, opts LifecycleRequestOptions) (*AssessHypothesisExhaustedResult, error) {
	return assessHypothesisExhaustedWithOptionalAuthority(projectRoot, taskID, note, agentID, nil, opts)
}

// AssessHypothesisExhaustedWithAuthorityAndOptions fences an identified assessment.
func AssessHypothesisExhaustedWithAuthorityAndOptions(projectRoot, taskID, note string, authority models.AgentAuthority, opts LifecycleRequestOptions) (*AssessHypothesisExhaustedResult, error) {
	return assessHypothesisExhaustedWithOptionalAuthority(projectRoot, taskID, note, authority.ID, &authority, opts)
}

func assessHypothesisExhaustedWithOptionalAuthority(projectRoot, taskID, note, agentID string, authority *models.AgentAuthority, options ...LifecycleRequestOptions) (returned *AssessHypothesisExhaustedResult, retErr error) {
	invocation := NewLifecycleInvocation(projectRoot)
	const operation = "assess-hypothesis-exhausted"
	var opts LifecycleRequestOptions
	if len(options) > 0 {
		opts = options[0]
	}
	var observed *models.Task
	effects := "none"
	defer func() {
		retErr = WrapLifecycleError(operation, observed, retErr, models.LifecycleInvalidInput, "correct_input", "none")
		var outcome models.LifecycleOutcome
		var warnings *[]string
		if returned != nil {
			outcome = returned.LifecycleOutcome
			warnings = &returned.Warnings
		}
		invocation.FinishResult(operation, outcome, &retErr, warnings)
	}()
	if err := ValidateLifecycleRequestOptions(opts); err != nil {
		return nil, err
	}
	if taskID == "" {
		return nil, &PreconditionError{Reason: "task ID is required"}
	}
	if agentID == "" {
		return nil, &PreconditionError{Reason: "agent ID is required"}
	}
	// Defense-in-depth: orchestrator_assessment history entries suppress future wakes,
	// so this must be restricted to orchestrator agents even though the MCP handler
	// also gates via resolveOrchestratorID.
	if err := identity.ValidateRole(agentID, roles.Orchestrator); err != nil {
		return nil, WrapLifecycleError(operation, nil, &PreconditionError{Reason: fmt.Sprintf("only orchestrator agents can assess hypothesis-exhausted tasks: %v", err)}, models.LifecycleForbidden, "stop", "none")
	}
	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())
	now := time.Now().UTC()
	resolver, _, err := loadResolver(projectRoot)
	if err != nil {
		return nil, WrapLifecycleError(operation, nil, fmt.Errorf("failed to load pipeline config: %w", err), models.LifecycleStateChanged, "requery", "none")
	}
	pipelineTransitions := BuildPipelineTransitions(resolver)
	result := &AssessHypothesisExhaustedResult{TaskID: taskID}

	err = lifecycleMutation(bb, authority)(func(state *models.State) (callbackErr error) {
		defer func() {
			callbackErr = WrapLifecycleError(operation, observed, callbackErr, models.LifecycleInvalidInput, "correct_input", "none")
		}()
		task := state.FindTask(taskID)
		if task == nil {
			return &errors.NotFoundError{Entity: "task", ID: taskID}
		}
		copy := *task
		observed = &copy
		request, err := NewLifecycleRequest(operation, task, agentID, authority, opts, note)
		if err != nil {
			return err
		}
		receipt, err := CheckLifecycleRequest(task, request, state.Agents)
		if err != nil {
			return err
		}
		if receipt != nil {
			result.LifecycleOutcome = LifecycleReplayOutcome(task, receipt, agentID)
			return errLifecycleReplay
		}

		if len(task.FailedBy) < 2 {
			return &PreconditionError{Reason: fmt.Sprintf("task must have 2+ entries in failed_by to assess as hypothesis-exhausted, has %d", len(task.FailedBy))}
		}
		if task.Status.IsTerminal() {
			return WrapLifecycleError(operation, task, &PreconditionError{Reason: fmt.Sprintf("task must not be in terminal status, current status: %s", task.Status)}, models.LifecycleAlreadyTransitioned, "stop", "none")
		}

		if _, err := blockTaskForHypothesisExhaustion(state, task, agentID, pipelineTransitions, now); err != nil {
			return err
		}

		entry := models.TaskHistoryEntry{
			Time:  now,
			Event: models.TaskEventOrchestratorAssessment,
			Agent: &agentID,
		}
		if note != "" {
			entry.Note = &note
		}

		dropSupersededWakeSnapshots(task)
		task.History = append(task.History, entry)
		result.LifecycleOutcome, err = CompleteLifecycleRequest(task, request, models.LifecycleProjection{}, state.Agents)
		if err == nil {
			effects = "unknown"
		}
		return err
	})
	if isLifecycleReplay(err) {
		return result, nil
	}

	if err != nil {
		return nil, WrapLifecycleError(operation, observed, fmt.Errorf("failed to assess hypothesis-exhausted task: %w", err), models.LifecycleStateChanged, "requery", effects)
	}

	return result, nil
}
